package webconfig

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/adapters/claude"
)

// provisionViaLiveSession calls POST /api/live/session and returns the
// created temp dir. The live-session and terminal endpoints share
// provisionMonitorDir, so this exercises the workspace provisioning for both.
func provisionViaLiveSession(t *testing.T, ts *testServer) string {
	t.Helper()
	resp := authedRequest(t, ts, "POST", "/api/live/session", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		TmpDir string `json:"tmpDir"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.TmpDir == "" {
		t.Fatal("tmpDir empty")
	}
	return body.TmpDir
}

func TestProvision_StatusLineHasDraftFlag(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // isolate the ~/.claude.json janitor
	ts := startTestServer(t, time.Hour)
	tmp := provisionViaLiveSession(t, ts)

	data, err := os.ReadFile(filepath.Join(tmp, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings struct {
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	cmd := settings.StatusLine.Command
	if !strings.Contains(cmd, "monitor") || !strings.Contains(cmd, "--draft") {
		t.Errorf("statusLine.command %q missing monitor/--draft", cmd)
	}
}

// TestProvision_SubagentStatusLineRegistered confirms provisionMonitorDir
// also registers Claude Code's subagentStatusLine (agent-panel task rows),
// not just statusLine: without it, embedded monitor sessions render the
// default subagent line instead of the user's claude-code-subagent document.
func TestProvision_SubagentStatusLineRegistered(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // isolate the ~/.claude.json janitor
	ts := startTestServer(t, time.Hour)
	tmp := provisionViaLiveSession(t, ts)

	data, err := os.ReadFile(filepath.Join(tmp, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings struct {
		SubagentStatusLine struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"subagentStatusLine"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if settings.SubagentStatusLine.Type != "command" {
		t.Errorf("subagentStatusLine.type = %q, want command", settings.SubagentStatusLine.Type)
	}
	cmd := settings.SubagentStatusLine.Command
	if !strings.Contains(cmd, "claude-subagent") || !strings.Contains(cmd, "--draft") {
		t.Errorf("subagentStatusLine.command %q missing claude-subagent/--draft", cmd)
	}
	// Unlike statusLine, the subagent line needs no live-preview forwarding.
	if strings.Contains(cmd, "--emit-url") || strings.Contains(cmd, "--token") {
		t.Errorf("subagentStatusLine.command %q should not forward emit-url/token", cmd)
	}
}

func TestProvision_WritesWorkspaceDocs(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // isolate the ~/.claude.json janitor
	ts := startTestServer(t, time.Hour)
	tmp := provisionViaLiveSession(t, ts)

	// CLAUDE.md explains the draft loop.
	claudeMD, err := os.ReadFile(filepath.Join(tmp, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	for _, want := range []string{
		"statusloom draft pull", "statusloom draft push", "statusloom claude < sample.json", "<statusloom",
		"claude-code-subagent", "subagentStatusLine", "statusloom claude-subagent --draft",
	} {
		if !strings.Contains(string(claudeMD), want) {
			t.Errorf("CLAUDE.md missing %q", want)
		}
	}

	// sample.json is a valid, decodable Claude payload.
	sample, err := os.ReadFile(filepath.Join(tmp, "sample.json"))
	if err != nil {
		t.Fatalf("read sample.json: %v", err)
	}
	snap, err := claude.New().Decode(sample)
	if err != nil {
		t.Fatalf("sample.json does not decode as a Claude payload: %v", err)
	}
	if snap.Session.Model == nil || snap.Session.Model.DisplayName == "" {
		t.Error("sample.json snapshot missing model display name")
	}
}

func TestProvision_StableReusedDir(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", cacheDir)
	t.Setenv("HOME", t.TempDir()) // isolate the ~/.claude.json janitor

	s := newServer("test-token", 0, time.Hour)
	t.Cleanup(s.stopIdleTimer)

	first, err := s.provisionMonitorDir()
	if err != nil {
		t.Fatalf("first provision: %v", err)
	}
	// Stable path under the cache dir.
	want := filepath.Join(cacheDir, "monitor-workspace")
	if first != want {
		t.Fatalf("provisionMonitorDir = %q, want %q", first, want)
	}
	for _, name := range []string{"CLAUDE.md", "sample.json", filepath.Join(".claude", "settings.local.json")} {
		if _, err := os.Stat(filepath.Join(first, name)); err != nil {
			t.Errorf("expected %s in workspace: %v", name, err)
		}
	}

	// A second call reuses the same directory and does not error.
	second, err := s.provisionMonitorDir()
	if err != nil {
		t.Fatalf("second provision: %v", err)
	}
	if second != first {
		t.Errorf("second provision path = %q, want the same stable path %q", second, first)
	}
}

func TestProvision_GitInitBestEffort(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // isolate the ~/.claude.json janitor
	ts := startTestServer(t, time.Hour)
	tmp := provisionViaLiveSession(t, ts)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available; provisioning must still succeed (files asserted elsewhere)")
	}

	// git present: expect a repo with exactly one commit.
	if _, err := os.Stat(filepath.Join(tmp, ".git")); err != nil {
		t.Fatalf(".git not created despite git being available: %v", err)
	}
	cmd := exec.Command("git", "log", "--oneline")
	cmd.Dir = tmp
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 1 {
		t.Fatalf("git log has %d commits, want 1:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "statusloom monitor workspace") {
		t.Errorf("commit subject = %q, want it to contain the workspace message", lines[0])
	}
}
