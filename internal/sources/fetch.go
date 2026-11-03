package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Fetch is the production Fetcher.
var _ Fetcher = Fetch

// fetchWaitDelay bounds how long Wait keeps the stdout pipe open after the
// command exits: a descendant that inherited stdout must not hang the fetch.
const fetchWaitDelay = time.Second

// fetchMaxRedirects is the number of redirect hops an http source may follow.
const fetchMaxRedirects = 3

// Fetch performs one live fetch of def. It runs external commands and opens
// network connections, so it is never called from the render path: only the
// refresh subprocess (`refresh --once` or `refresh --probe`) calls it.
// Error texts never include a response body or command output.
func Fetch(ctx context.Context, def Definition, etag string) (FetchResult, error) {
	// A hand-edited sources.json may bypass the CLI's validation; error out
	// instead of panicking on, e.g., an empty argv.
	if err := def.Validate(); err != nil {
		return FetchResult{}, err
	}
	switch def.Kind {
	case KindFile:
		p, err := def.ExpandedPath()
		if err != nil {
			return FetchResult{}, err
		}
		body, _, err := ReadFileSource(p)
		if err != nil {
			return FetchResult{}, err
		}
		return FetchResult{Body: body}, nil
	case KindCommand:
		return fetchCommand(ctx, def)
	case KindHTTP:
		return fetchHTTP(ctx, def, etag)
	default:
		return FetchResult{}, fmt.Errorf("source %q: unknown kind %q (want file, command or http)", def.ID, def.Kind)
	}
}

// fetchCommand runs def.Argv directly (no shell) with an empty stdin, stderr
// discarded and the environment inherited, and returns its stdout.
func fetchCommand(ctx context.Context, def Definition) (FetchResult, error) {
	tctx, cancel := context.WithTimeout(ctx, def.Timeout())
	defer cancel()

	cmd := exec.CommandContext(tctx, def.Argv[0], def.Argv[1:]...)
	cmd.WaitDelay = fetchWaitDelay
	out := &fetchLimitWriter{}
	cmd.Stdout = out

	if err := cmd.Start(); err != nil {
		return FetchResult{}, fmt.Errorf("command failed to start: %w", err)
	}
	werr := cmd.Wait()
	// out is safe to read now: Wait has joined the stdout copy goroutine.
	switch {
	case out.exceeded:
		return FetchResult{}, errors.New("output exceeds 1 MiB")
	case werr != nil && !errors.Is(werr, exec.ErrWaitDelay):
		if errors.Is(tctx.Err(), context.DeadlineExceeded) {
			return FetchResult{}, fmt.Errorf("command timed out after %s", def.Timeout())
		}
		if err := tctx.Err(); err != nil {
			return FetchResult{}, err
		}
		return FetchResult{}, fmt.Errorf("command exited: %v", werr)
	}
	// A bare ErrWaitDelay means the command exited 0 while a descendant kept
	// stdout open; os/exec closed the pipe, and what was written is kept.
	if !json.Valid(out.buf.Bytes()) {
		return FetchResult{}, errors.New("output is not JSON")
	}
	return FetchResult{Body: out.buf.Bytes()}, nil
}

// fetchLimitWriter buffers at most MaxBodyBytes. A write past the limit fails,
// so os/exec stops copying and closes the pipe (the child gets SIGPIPE instead
// of growing our memory).
type fetchLimitWriter struct {
	buf      bytes.Buffer
	exceeded bool
}

func (w *fetchLimitWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > MaxBodyBytes {
		w.exceeded = true
		return 0, errors.New("output exceeds 1 MiB")
	}
	return w.buf.Write(p)
}

// fetchHTTP performs one GET of def.URL. Without def.AllowRemote the host is
// checked before any connection, every dialed address is checked after DNS,
// and every redirect hop is re-checked.
func fetchHTTP(ctx context.Context, def Definition, etag string) (FetchResult, error) {
	u, err := url.Parse(def.URL)
	if err != nil {
		return FetchResult{}, err
	}
	if !def.AllowRemote {
		if err := loopbackHostError(u); err != nil {
			return FetchResult{}, err
		}
	}

	tctx, cancel := context.WithTimeout(ctx, def.Timeout())
	defer cancel()
	timedOut := func() bool { return errors.Is(tctx.Err(), context.DeadlineExceeded) }

	req, err := http.NewRequestWithContext(tctx, http.MethodGet, def.URL, nil)
	if err != nil {
		return FetchResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "statusloom")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := newFetchClient(def.AllowRemote).Do(req)
	if err != nil {
		if timedOut() {
			return FetchResult{}, fmt.Errorf("request timed out after %s", def.Timeout())
		}
		return FetchResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return FetchResult{NotModified: true, ETag: etag}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return FetchResult{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
	if err != nil {
		if timedOut() {
			return FetchResult{}, fmt.Errorf("request timed out after %s", def.Timeout())
		}
		return FetchResult{}, fmt.Errorf("read body: %w", err)
	}
	if len(body) > MaxBodyBytes {
		return FetchResult{}, errors.New("body exceeds 1 MiB")
	}
	if !json.Valid(body) {
		return FetchResult{}, errors.New("body is not JSON")
	}
	return FetchResult{Body: body, ETag: resp.Header.Get("ETag")}, nil
}

// loopbackHostError rejects a URL whose host is neither "localhost" nor a
// loopback IP literal. It looks at the parsed hostname only, never the raw
// string, so userinfo tricks like http://127.0.0.1@example.com/ are caught.
func loopbackHostError(u *url.URL) error {
	h := u.Hostname()
	if strings.EqualFold(h, "localhost") {
		return nil
	}
	if ip := net.ParseIP(h); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("host %q is not loopback; set allowRemote to allow it", h)
}

// loopbackDialControl is a net.Dialer.Control: it runs after DNS with the
// resolved ip:port, so a hostname resolving to a non-loopback address is
// rejected before the connection is made.
func loopbackDialControl(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("address %q is not loopback; set allowRemote to allow it", address)
}

// newFetchClient returns a dedicated client (never http.DefaultClient) whose
// connections do not outlive the call. The request context bounds the call,
// so the client has no Timeout of its own.
func newFetchClient(allowRemote bool) *http.Client {
	d := &net.Dialer{}
	if !allowRemote {
		d.Control = loopbackDialControl
	}
	tr := &http.Transport{
		DialContext:       d.DialContext,
		DisableKeepAlives: true,
	}
	if allowRemote {
		tr.Proxy = http.ProxyFromEnvironment
	}
	return &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > fetchMaxRedirects {
				return fmt.Errorf("stopped after %d redirects", fetchMaxRedirects)
			}
			if !allowRemote {
				return loopbackHostError(req.URL)
			}
			return nil
		},
	}
}
