package webconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPruneDeadMonitorEntries(t *testing.T) {
	// exists reports true only for these live paths.
	live := map[string]bool{
		"/tmp/statusloom-monitor-alive":                true,
		"/home/me/.cache/statusloom/monitor-workspace": true,
		"/home/me/projects/real":                       true,
	}
	exists := func(p string) bool { return live[p] }

	raw := []byte(`{
	  "numAccounts": 3,
	  "projects": {
	    "/tmp/statusloom-monitor-dead1": {"allowedTools": []},
	    "/tmp/statusloom-monitor-dead2": {"hasTrustDialogAccepted": true},
	    "/tmp/statusloom-monitor-alive": {"allowedTools": ["x"]},
	    "/home/me/.cache/statusloom/monitor-workspace": {"hasTrustDialogAccepted": true},
	    "/home/me/projects/real": {"allowedTools": ["y"]}
	  }
	}`)

	out, changed, err := pruneDeadMonitorEntries(raw, exists)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true (two dead entries present)")
	}

	var doc struct {
		NumAccounts json.Number    `json:"numAccounts"`
		Projects    map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	// Dead monitor entries removed.
	for _, dead := range []string{"/tmp/statusloom-monitor-dead1", "/tmp/statusloom-monitor-dead2"} {
		if _, ok := doc.Projects[dead]; ok {
			t.Errorf("dead entry %q was not removed", dead)
		}
	}
	// Everything else preserved: live monitor dir, stable workspace, unrelated.
	for _, keep := range []string{
		"/tmp/statusloom-monitor-alive",
		"/home/me/.cache/statusloom/monitor-workspace",
		"/home/me/projects/real",
	} {
		if _, ok := doc.Projects[keep]; !ok {
			t.Errorf("entry %q was removed but should be preserved", keep)
		}
	}
	// Unrelated top-level keys preserved, and integer preserved exactly (no
	// float reformatting) thanks to UseNumber.
	if doc.NumAccounts.String() != "3" {
		t.Errorf("numAccounts = %q, want 3", doc.NumAccounts.String())
	}
}

func TestPruneDeadMonitorEntries_NoChange(t *testing.T) {
	exists := func(string) bool { return true } // everything alive

	cases := map[string][]byte{
		"all live":        []byte(`{"projects":{"/tmp/statusloom-monitor-x":{}}}`),
		"no projects key": []byte(`{"numAccounts":1}`),
		"empty object":    []byte(`{}`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			out, changed, err := pruneDeadMonitorEntries(raw, exists)
			if err != nil {
				t.Fatalf("prune: %v", err)
			}
			if changed {
				t.Fatalf("changed = true, want false")
			}
			if string(out) != string(raw) {
				t.Errorf("output mutated on no-op:\n got %s\nwant %s", out, raw)
			}
		})
	}
}

func TestPruneDeadMonitorEntries_OnlyDeleteDead(t *testing.T) {
	// A dead entry that does NOT match the statusloom-monitor- prefix must be
	// left alone (we only clean up our own leaked entries).
	exists := func(string) bool { return false } // everything dead
	raw := []byte(`{"projects":{"/tmp/some-other-tool-123":{},"/tmp/statusloom-monitor-9":{}}}`)

	out, changed, err := pruneDeadMonitorEntries(raw, exists)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if strings.Contains(string(out), "statusloom-monitor-9") {
		t.Error("dead statusloom-monitor entry not removed")
	}
	if !strings.Contains(string(out), "some-other-tool-123") {
		t.Error("unrelated (non-statusloom) entry was removed; janitor is over-reaching")
	}
}

func TestCleanupDeadMonitorEntries_IOWrapper(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// A live and a dead monitor entry. The live dir must exist on disk.
	liveDir := filepath.Join(home, "statusloom-monitor-live")
	if err := os.Mkdir(liveDir, 0o700); err != nil {
		t.Fatalf("mkdir live: %v", err)
	}
	deadDir := filepath.Join(home, "statusloom-monitor-dead")

	doc := map[string]any{
		"projects": map[string]any{
			liveDir: map[string]any{"allowedTools": []any{}},
			deadDir: map[string]any{"allowedTools": []any{}},
		},
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	cfg := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(cfg, b, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cleanupDeadMonitorEntries()

	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), "statusloom-monitor-dead") {
		t.Error("dead entry not removed from ~/.claude.json")
	}
	if !strings.Contains(string(raw), "statusloom-monitor-live") {
		t.Error("live entry removed from ~/.claude.json, want preserved")
	}
}

func TestCleanupDeadMonitorEntries_MissingFileNoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ~/.claude.json present
	// Must not panic or create anything.
	cleanupDeadMonitorEntries()
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".claude.json")); !os.IsNotExist(err) {
		t.Errorf("janitor created ~/.claude.json (stat err = %v), want no-op", err)
	}
}
