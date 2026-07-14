package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestRun_Monitor_RendersLikeClaude verifies that `statusloom monitor`
// produces the exact same rendered status line on stdout as `statusloom
// claude` for the same stdin (the monitor path must not change rendering).
func TestRun_Monitor_RendersLikeClaude(t *testing.T) {
	setupEnv(t)
	data := fixture(t, "full.json")

	// A capture server for the emit; its content is asserted elsewhere.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	claudeOut, _, claudeCode := runCLI(t, []string{"claude"}, data, nil)
	if claudeCode != 0 {
		t.Fatalf("claude exit = %d, want 0", claudeCode)
	}

	monitorOut, stderr, monitorCode := runCLI(t,
		[]string{"monitor", "--emit-url", srv.URL, "--token", "tok"}, data, nil)
	if monitorCode != 0 {
		t.Fatalf("monitor exit = %d, want 0 (stderr: %s)", monitorCode, stderr)
	}

	if monitorOut != claudeOut {
		t.Errorf("monitor stdout != claude stdout:\nmonitor=%q\nclaude =%q", monitorOut, claudeOut)
	}
}

// TestRun_Monitor_Emits verifies the best-effort POST carries the right
// method, URL path, bearer token, content type, and raw body.
func TestRun_Monitor_Emits(t *testing.T) {
	setupEnv(t)
	data := fixture(t, "full.json")

	var (
		mu       sync.Mutex
		gotBody  []byte
		gotAuth  string
		gotCT    string
		gotPath  string
		gotCount int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = b
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		gotCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, stderr, code := runCLI(t,
		[]string{"monitor", "--emit-url", srv.URL + "/api/live", "--token", "secret-token"}, data, nil)
	if code != 0 {
		t.Fatalf("monitor exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotCount != 1 {
		t.Fatalf("emit received %d times, want 1", gotCount)
	}
	if gotPath != "/api/live" {
		t.Errorf("emit path = %q, want /api/live", gotPath)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer secret-token")
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if !bytes.Equal(gotBody, data) {
		t.Errorf("emit body != raw stdin:\ngot  %q\nwant %q", gotBody, data)
	}
}

// TestRun_Monitor_EmitFailureIsIgnored verifies the render still succeeds
// (exit 0, output present) when the emit target is unreachable.
func TestRun_Monitor_EmitFailureIsIgnored(t *testing.T) {
	setupEnv(t)
	data := fixture(t, "full.json")

	// A server we immediately close, so the URL is valid but connections are
	// refused - a realistic "config server not running" case.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL + "/api/live"
	srv.Close()

	stdout, stderr, code := runCLI(t,
		[]string{"monitor", "--emit-url", url, "--token", "tok"}, data, nil)
	if code != 0 {
		t.Fatalf("monitor exit = %d, want 0 despite emit failure (stderr: %s)", code, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Errorf("expected rendered output despite emit failure, got empty stdout")
	}
}

// TestRun_Monitor_MissingFlags verifies usage errors when required flags are
// absent, and that nothing is written to stdout.
func TestRun_Monitor_MissingFlags(t *testing.T) {
	setupEnv(t)
	data := fixture(t, "full.json")

	for _, args := range [][]string{
		{"monitor"},
		{"monitor", "--emit-url", "http://127.0.0.1:1/api/live"},
		{"monitor", "--token", "tok"},
	} {
		stdout, _, code := runCLI(t, args, data, nil)
		if code != 2 {
			t.Errorf("args %v: exit = %d, want 2", args, code)
		}
		if stdout != "" {
			t.Errorf("args %v: stdout = %q, want empty", args, stdout)
		}
	}
}
