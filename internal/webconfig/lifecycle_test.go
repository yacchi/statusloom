package webconfig

import (
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestShutdownEndpoint(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	resp := authedRequest(t, ts, "POST", "/api/shutdown", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		OK bool `json:"ok"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Errorf("ok = %v, want true", body.OK)
	}

	if err := ts.wait(t, 5*time.Second); err != nil {
		t.Errorf("Serve() error = %v, want nil", err)
	}
}

// TestShutdownDoesNotWaitForUnusedConnection guards against a connection that
// was opened but never sent a request (a browser preconnect, or a client
// transport's unused dial) holding Serve for its whole shutdown grace period.
func TestShutdownDoesNotWaitForUnusedConnection(t *testing.T) {
	ts := startTestServer(t, time.Hour)

	unused, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ts.port)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer unused.Close()

	// The request below opens its own connection. The accept queue is FIFO,
	// so once it has been answered the server has accepted the unused one.
	resp := authedRequest(t, ts, "GET", "/api/tools", nil)
	resp.Body.Close()

	ts.cancel()
	if err := ts.wait(t, 2*time.Second); err != nil {
		t.Errorf("Serve() error = %v, want nil", err)
	}
}

func TestIdleTimeout(t *testing.T) {
	ts := startTestServer(t, 100*time.Millisecond)

	if err := ts.wait(t, 5*time.Second); err != nil {
		t.Errorf("Serve() error = %v, want nil", err)
	}
}
