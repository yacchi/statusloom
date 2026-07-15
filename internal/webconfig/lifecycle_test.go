package webconfig

import (
	"net/http"
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

func TestIdleTimeout(t *testing.T) {
	ts := startTestServer(t, 100*time.Millisecond)

	if err := ts.wait(t, 5*time.Second); err != nil {
		t.Errorf("Serve() error = %v, want nil", err)
	}
}
