package webconfig

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// decodeJSON decodes r's body as JSON into v.
func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

// authedRequest issues an authenticated request to the test server, setting
// the bearer token (and, when a body is present, a JSON Content-Type).
func authedRequest(t *testing.T, ts *testServer, method, path string, body []byte) *http.Response {
	t.Helper()
	var r *bytes.Reader
	if body != nil {
		r = bytes.NewReader(body)
	} else {
		r = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, ts.baseURL+path, r)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+ts.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	return resp
}

// testServer bundles what a test needs to talk to a running configurator:
// its base URL, valid bearer token, and bound port. done is closed once
// Serve returns, with the returned error stashed in err - both wait() and
// the automatic test cleanup can observe it that way, however many times
// they're called.
type testServer struct {
	baseURL string
	token   string
	port    int

	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// wait blocks until Serve returns (which the caller triggers via
// shutdown, idle timeout, or by calling s.cancel), and fails the test if
// it doesn't happen within timeout.
func (s *testServer) wait(t *testing.T, timeout time.Duration) error {
	t.Helper()
	select {
	case <-s.done:
		return s.err
	case <-time.After(timeout):
		t.Fatal("Serve did not return in time")
		return nil
	}
}

// startTestServer starts a configurator server on a random port and
// returns once its startup line has been printed (so the port and token
// are known). The server is torn down automatically at test end.
func startTestServer(t *testing.T, idleTimeout time.Duration) *testServer {
	t.Helper()

	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	ts := &testServer{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	go func() {
		ts.err = Serve(ctx, Options{
			Port:        0,
			Stdout:      pw,
			IdleTimeout: idleTimeout,
		})
		close(ts.done)
	}()

	line, err := bufio.NewReader(pr).ReadString('\n')
	if err != nil {
		t.Fatalf("reading startup line: %v", err)
	}
	line = strings.TrimSpace(line)

	const prefix = "Statusloom configurator: "
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("unexpected startup line: %q", line)
	}
	rawURL := strings.TrimPrefix(line, prefix)

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parsing startup URL %q: %v", rawURL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parsing port from startup URL %q: %v", rawURL, err)
	}
	frag, err := url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatalf("parsing fragment from startup URL %q: %v", rawURL, err)
	}
	token := frag.Get("token")
	if token == "" {
		t.Fatalf("no token in startup URL %q", rawURL)
	}

	ts.baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	ts.token = token
	ts.port = port

	t.Cleanup(func() {
		cancel()
		select {
		case <-ts.done:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not shut down during cleanup")
		}
	})

	return ts
}
