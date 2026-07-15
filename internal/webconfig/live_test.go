package webconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// liveFixture reads a Claude Code stdin fixture from fixtures/claude/.
func liveFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "claude", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

// wsURL rewrites a testServer's http base URL to a ws:// URL for path with
// the given token query.
func wsURL(ts *testServer, path, token string) string {
	base := strings.Replace(ts.baseURL, "http://", "ws://", 1)
	return base + path + "?token=" + token
}

// dialLiveWS connects to /ws/live with token, returning the connection. It
// fails the test if the dial does not produce the wantStatus HTTP status.
func dialLiveWS(t *testing.T, ts *testServer, token string, wantStatus int) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, resp, err := websocket.Dial(ctx, wsURL(ts, "/ws/live", token), nil)
	if wantStatus == http.StatusSwitchingProtocols {
		if err != nil {
			t.Fatalf("dial /ws/live: unexpected error: %v", err)
		}
		return c
	}
	// Expecting a rejection before upgrade.
	if err == nil {
		c.Close(websocket.StatusNormalClosure, "")
		t.Fatalf("dial /ws/live: expected failure with status %d, but connected", wantStatus)
	}
	if resp == nil {
		t.Fatalf("dial /ws/live: expected HTTP response with status %d, got nil (err=%v)", wantStatus, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("dial /ws/live: status = %d, want %d", resp.StatusCode, wantStatus)
	}
	return nil
}

func TestLiveWS_RejectsBadToken(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	dialLiveWS(t, ts, "not-the-token", http.StatusUnauthorized)
}

func TestLiveWS_RejectsMissingToken(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	dialLiveWS(t, ts, "", http.StatusUnauthorized)
}

func TestLiveWS_AcceptsGoodToken(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	c := dialLiveWS(t, ts, ts.token, http.StatusSwitchingProtocols)
	c.Close(websocket.StatusNormalClosure, "")
}

func TestLive_BroadcastsToSubscribers(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	c := dialLiveWS(t, ts, ts.token, http.StatusSwitchingProtocols)
	defer c.Close(websocket.StatusNormalClosure, "")

	// POST /api/live with a real Claude payload.
	payload := liveFixture(t, "full.json")
	resp := authedRequest(t, ts, "POST", "/api/live", payload)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/live status = %d, want 200", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("reading broadcast: %v", err)
	}
	if typ != websocket.MessageText {
		t.Errorf("message type = %v, want text", typ)
	}

	var msg struct {
		Type       string `json:"type"`
		SessionID  string `json:"sessionId"`
		ObservedAt string `json:"observedAt"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal broadcast %q: %v", data, err)
	}
	if msg.Type != "live-update" {
		t.Errorf("type = %q, want live-update", msg.Type)
	}
	if msg.SessionID != "3f9a2b1c-4d5e-6f70-8a9b-0c1d2e3f4a5b" {
		t.Errorf("sessionId = %q, want the fixture's session id", msg.SessionID)
	}
	if _, err := time.Parse(time.RFC3339, msg.ObservedAt); err != nil {
		t.Errorf("observedAt = %q, not RFC3339: %v", msg.ObservedAt, err)
	}
}

func TestLive_RejectsInvalidPayload(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	resp := authedRequest(t, ts, "POST", "/api/live", []byte("not json"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestLive_RequiresBearer(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	// No Authorization header: withSecurity must reject /api/live.
	req, err := http.NewRequest("POST", ts.baseURL+"/api/live", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// liveSessionResp mirrors the POST /api/live/session response for decoding.
type liveSessionResp struct {
	LaunchCommand string `json:"launchCommand"`
	TmpDir        string `json:"tmpDir"`
}

func createLiveSession(t *testing.T, ts *testServer) liveSessionResp {
	t.Helper()
	resp := authedRequest(t, ts, "POST", "/api/live/session", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/live/session status = %d, want 200", resp.StatusCode)
	}
	var body liveSessionResp
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

func TestLiveSession_CreatesTmpDirAndSettings(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // isolate the ~/.claude.json janitor
	ts := startTestServer(t, time.Hour)

	body := createLiveSession(t, ts)
	if body.TmpDir == "" {
		t.Fatal("tmpDir is empty")
	}
	if !strings.Contains(body.LaunchCommand, body.TmpDir) || !strings.Contains(body.LaunchCommand, "claude") {
		t.Errorf("launchCommand = %q, want it to cd into %q and run claude", body.LaunchCommand, body.TmpDir)
	}

	settingsPath := filepath.Join(body.TmpDir, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", settingsPath, err)
	}

	var settings struct {
		StatusLine struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal settings %q: %v", data, err)
	}
	if settings.StatusLine.Type != "command" {
		t.Errorf("statusLine.type = %q, want command", settings.StatusLine.Type)
	}
	cmd := settings.StatusLine.Command
	for _, want := range []string{
		"monitor",
		"--emit-url",
		"/api/live",
		"--token " + ts.token,
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("statusLine.command %q missing %q", cmd, want)
		}
	}
}

func TestLive_ShutdownKeepsWorkspace(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // isolate the ~/.claude.json janitor
	ts := startTestServer(t, time.Hour)

	body := createLiveSession(t, ts)
	if _, err := os.Stat(body.TmpDir); err != nil {
		t.Fatalf("workspace should exist after creation: %v", err)
	}

	// Trigger shutdown and wait for Serve to return.
	resp := authedRequest(t, ts, "POST", "/api/shutdown", nil)
	resp.Body.Close()
	if err := ts.wait(t, 5*time.Second); err != nil {
		t.Fatalf("Serve() error = %v, want nil", err)
	}

	// The stable monitor workspace must PERSIST across shutdown so the
	// folder-trust prompt stays a one-time thing.
	if _, err := os.Stat(body.TmpDir); err != nil {
		t.Errorf("workspace removed after shutdown (stat err = %v), want it to persist", err)
	}
}

func TestSweepStaleMonitorDirs(t *testing.T) {
	tmpRoot := t.TempDir()
	now := time.Now()

	oldDir := filepath.Join(tmpRoot, "statusloom-monitor-old")
	freshDir := filepath.Join(tmpRoot, "statusloom-monitor-fresh")
	unrelatedDir := filepath.Join(tmpRoot, "unrelated-dir")
	matchingFile := filepath.Join(tmpRoot, "statusloom-monitor-file")

	for _, dir := range []string{oldDir, freshDir, unrelatedDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("Mkdir(%s): %v", dir, err)
		}
	}
	if err := os.WriteFile(matchingFile, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", matchingFile, err)
	}

	if err := os.Chtimes(oldDir, now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatalf("Chtimes(old): %v", err)
	}
	if err := os.Chtimes(freshDir, now.Add(-time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatalf("Chtimes(fresh): %v", err)
	}
	if err := os.Chtimes(matchingFile, now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatalf("Chtimes(matchingFile): %v", err)
	}

	sweepStaleMonitorDirs(tmpRoot, now)

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("oldDir still exists after sweep (stat err = %v), want removed", err)
	}
	if _, err := os.Stat(freshDir); err != nil {
		t.Errorf("freshDir missing after sweep: %v, want it to remain", err)
	}
	if _, err := os.Stat(unrelatedDir); err != nil {
		t.Errorf("unrelatedDir missing after sweep: %v, want it to remain", err)
	}
	if _, err := os.Stat(matchingFile); err != nil {
		t.Errorf("matchingFile missing after sweep: %v, want it to remain (files are not swept)", err)
	}
}
