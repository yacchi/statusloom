package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"
)

// emitTimeout bounds the best-effort POST that `statusloom monitor` makes to
// the config server. It is deliberately short: emitting live data must never
// slow down (let alone fail) the status-line render, so the request is
// abandoned quickly if the server is unresponsive.
const emitTimeout = 500 * time.Millisecond

// runMonitor implements `statusloom monitor`, the only statusloom code path
// that performs an outbound network request. It behaves exactly like
// `statusloom claude` for rendering purposes - reading the same stdin,
// producing byte-identical stdout - and, as a best-effort side effect,
// forwards the raw stdin payload to the config server's POST /api/live so a
// browser watching the configurator can observe the session live.
//
// The emit is strictly best-effort: any failure (bad URL, connection
// refused, timeout, non-2xx response) is swallowed. The render is completed
// and written first, so emitting can never delay or fail the status line.
func runMonitor(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	emitURL := fs.String("emit-url", "", "config server URL to POST the raw payload to (required)")
	token := fs.String("token", "", "bearer token for the config server (required)")
	draft := fs.Bool("draft", false, "render against the shared draft document (<tool>.draft.xml) instead of the saved document")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *emitURL == "" || *token == "" {
		fmt.Fprintln(stderr, "statusloom monitor: --emit-url and --token are required")
		return 2
	}

	raw, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes))
	if err != nil {
		return fail(stderr, fmt.Errorf("reading stdin: %w", err))
	}

	// Render and emit output first so the status line is complete before any
	// network work happens. `monitor` is forced to the claude-code tool, the
	// same as `statusloom claude`; with --draft it renders the shared draft
	// document (<tool>.draft.xml), falling back to the saved document when the
	// draft is absent or invalid. Without --draft, output is byte-identical to
	// `statusloom claude`.
	lines, rerr := renderDocFromRaw(raw, getenv, "claude-code", stderr, *draft)
	code := writeRenderResult(stdout, stderr, lines, nil, rerr)

	// Best-effort emit of the raw payload; failures are intentionally ignored
	// and never affect the exit code.
	emitLivePayload(*emitURL, *token, raw)

	return code
}

// emitLivePayload POSTs raw to url with a bearer token and a short timeout.
// It returns nothing: every error is swallowed by design (see runMonitor).
func emitLivePayload(url, token string, raw []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), emitTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	// Drain and close so the connection can be reused; ignore any error.
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
