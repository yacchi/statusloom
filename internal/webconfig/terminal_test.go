package webconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// newTerminalTestServer builds a server whose routes are exposed via an
// httptest.Server. Unlike startTestServer, it hands back the *server itself
// so terminal tests can inspect internal state (live terminal count, temp
// dirs). Requests are sent without an Origin header, so validOrigin (which
// only fires when Origin is present) is not exercised; Host is 127.0.0.1 and
// passes validHost. The idle timer is stopped and any live terminals/temp
// dirs are cleaned up at test end.
func newTerminalTestServer(t *testing.T) (*server, *httptest.Server) {
	t.Helper()
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	s := newServer("test-token", 0, time.Hour)
	hs := httptest.NewServer(s.routes())
	t.Cleanup(func() {
		s.stopIdleTimer()
		s.closeAllTerminals()
		s.cleanupTmpDirs()
		hs.Close()
	})
	return s, hs
}

// useFakeTerminal replaces the spawned command with cmdline (argv) for the
// duration of the test, so tests never depend on a real `claude` binary.
func useFakeTerminal(t *testing.T, name string, args ...string) {
	t.Helper()
	prev := spawnTerminalCmd
	spawnTerminalCmd = func(dir string) *exec.Cmd {
		c := exec.Command(name, args...)
		c.Dir = dir
		return c
	}
	t.Cleanup(func() { spawnTerminalCmd = prev })
}

func termPost(t *testing.T, hs *httptest.Server, token, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", hs.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// createTerminal POSTs /api/terminal/session and returns the terminal id.
func createTerminal(t *testing.T, s *server, hs *httptest.Server) string {
	t.Helper()
	resp := termPost(t, hs, s.token, "/api/terminal/session")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/terminal/session status = %d, want 200", resp.StatusCode)
	}
	var body terminalSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.TerminalID == "" {
		t.Fatal("terminalId is empty")
	}
	return body.TerminalID
}

func termWSURL(hs *httptest.Server, token, id string) string {
	base := strings.Replace(hs.URL, "http://", "ws://", 1)
	return base + "/ws/terminal?token=" + token + "&id=" + id
}

// waitFor polls cond until it is true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", desc)
}

func TestTerminalSession_CreatesTmpDirAndSettings(t *testing.T) {
	useFakeTerminal(t, "cat")
	s, hs := newTerminalTestServer(t)

	id := createTerminal(t, s, hs)

	ts := s.lookupTerminal(id)
	if ts == nil {
		t.Fatal("terminal not registered")
	}
	settingsPath := ts.tmpDir + "/.claude/settings.local.json"
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", settingsPath, err)
	}
	if !strings.Contains(string(data), "monitor") || !strings.Contains(string(data), "--token "+s.token) {
		t.Errorf("settings.local.json missing monitor/token wiring: %s", data)
	}
}

func TestTerminalSession_MaxConcurrent(t *testing.T) {
	useFakeTerminal(t, "cat") // long-lived: stays until killed
	s, hs := newTerminalTestServer(t)

	for i := 0; i < maxTerminals; i++ {
		createTerminal(t, s, hs)
	}
	if got := s.terminalCount(); got != maxTerminals {
		t.Fatalf("terminalCount = %d, want %d", got, maxTerminals)
	}

	resp := termPost(t, hs, s.token, "/api/terminal/session")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("over-limit status = %d, want 429", resp.StatusCode)
	}
}

func TestTerminalSession_RequiresBearer(t *testing.T) {
	useFakeTerminal(t, "cat")
	s, hs := newTerminalTestServer(t)

	resp := termPost(t, hs, "", "/api/terminal/session")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if s.terminalCount() != 0 {
		t.Errorf("terminalCount = %d, want 0 (no terminal should spawn without auth)", s.terminalCount())
	}
}

func TestTerminalWS_RejectsBadToken(t *testing.T) {
	useFakeTerminal(t, "cat")
	s, hs := newTerminalTestServer(t)
	id := createTerminal(t, s, hs)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, termWSURL(hs, "wrong-token", id), nil)
	if err == nil {
		c.Close(websocket.StatusNormalClosure, "")
		t.Fatal("dial succeeded with bad token, want failure")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", resp)
	}
}

func TestTerminalWS_RejectsUnknownID(t *testing.T) {
	useFakeTerminal(t, "cat")
	s, hs := newTerminalTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, termWSURL(hs, s.token, "does-not-exist"), nil)
	if err == nil {
		c.Close(websocket.StatusNormalClosure, "")
		t.Fatal("dial succeeded with unknown id, want failure")
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %v, want 404", resp)
	}
}

func TestTerminalWS_StreamsPTYOutput(t *testing.T) {
	// Prints a known marker immediately, then stays alive reading stdin.
	useFakeTerminal(t, "sh", "-c", "printf READYMARK; cat")
	s, hs := newTerminalTestServer(t)
	id := createTerminal(t, s, hs)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, termWSURL(hs, s.token, id), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	got := readUntil(t, ctx, c, "READYMARK")
	if !strings.Contains(got, "READYMARK") {
		t.Errorf("PTY output = %q, want it to contain READYMARK", got)
	}
}

func TestTerminalWS_InputReachesPTY(t *testing.T) {
	// Echo back a transformed copy of one input line, proving stdin flows in.
	useFakeTerminal(t, "sh", "-c", "IFS= read -r line; printf 'GOT:%s' \"$line\"")
	s, hs := newTerminalTestServer(t)
	id := createTerminal(t, s, hs)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, termWSURL(hs, s.token, id), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	if err := c.Write(ctx, websocket.MessageBinary, []byte("hello\n")); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	got := readUntil(t, ctx, c, "GOT:hello")
	if !strings.Contains(got, "GOT:hello") {
		t.Errorf("PTY output = %q, want it to contain GOT:hello", got)
	}
}

func TestTerminalWS_ResizeCallsSetsize(t *testing.T) {
	// Waits for one input line (our trigger), then reports the PTY size.
	useFakeTerminal(t, "sh", "-c", "IFS= read -r _; stty size")
	s, hs := newTerminalTestServer(t)
	id := createTerminal(t, s, hs)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, termWSURL(hs, s.token, id), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	resize, _ := json.Marshal(terminalResizeMessage{Type: "resize", Cols: 123, Rows: 45})
	if err := c.Write(ctx, websocket.MessageText, resize); err != nil {
		t.Fatalf("write resize: %v", err)
	}
	// Trigger the stty report only after the resize has been sent.
	if err := c.Write(ctx, websocket.MessageBinary, []byte("\n")); err != nil {
		t.Fatalf("write trigger: %v", err)
	}

	got := readUntil(t, ctx, c, "45 123")
	if !strings.Contains(got, "45 123") {
		t.Errorf("stty size output = %q, want it to contain \"45 123\" (rows cols)", got)
	}
}

func TestTerminalWS_DisconnectKillsAndCleansUp(t *testing.T) {
	useFakeTerminal(t, "cat") // stays alive until killed
	s, hs := newTerminalTestServer(t)
	id := createTerminal(t, s, hs)

	ts := s.lookupTerminal(id)
	if ts == nil {
		t.Fatal("terminal not registered")
	}
	tmpDir := ts.tmpDir

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, termWSURL(hs, s.token, id), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Disconnect abruptly.
	c.Close(websocket.StatusNormalClosure, "")

	waitFor(t, 5*time.Second, "terminal deregistered", func() bool {
		return s.lookupTerminal(id) == nil
	})
	waitFor(t, 5*time.Second, "temp dir removed", func() bool {
		_, statErr := os.Stat(tmpDir)
		return os.IsNotExist(statErr)
	})
}

func TestTerminal_ShutdownClosesAll(t *testing.T) {
	useFakeTerminal(t, "cat")
	s, hs := newTerminalTestServer(t)
	id := createTerminal(t, s, hs)
	tmpDir := s.lookupTerminal(id).tmpDir

	// Simulate Serve's shutdown path.
	s.closeAllTerminals()

	if s.terminalCount() != 0 {
		t.Errorf("terminalCount = %d after closeAllTerminals, want 0", s.terminalCount())
	}
	if _, statErr := os.Stat(tmpDir); !os.IsNotExist(statErr) {
		t.Errorf("temp dir still exists after shutdown (stat err = %v)", statErr)
	}
}

// readUntil reads frames from c, accumulating their text, until want appears
// or the context/timeout fires.
func readUntil(t *testing.T, ctx context.Context, c *websocket.Conn, want string) string {
	t.Helper()
	var acc strings.Builder
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read (looking for %q, have %q): %v", want, acc.String(), err)
		}
		acc.Write(data)
		if strings.Contains(acc.String(), want) {
			return acc.String()
		}
	}
}
