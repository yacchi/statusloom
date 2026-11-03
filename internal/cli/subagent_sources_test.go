package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/sources"
)

// subagentDocSourceGated gates the main line and the subagent row on the
// external source "cc" through source-stale and optional="source:cc". Both
// the main pass and the claude-subagent pass must resolve the source the same
// way, so the subagent row's variant and its optional prefix follow the same
// availability the main line does.
const subagentDocSourceGated = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <responsive>
      <variant when="source-stale:cc eq true">
        <line><text>STALE</text></line>
        <subagent><line><field name="task-description" prefix="stale: "/></line></subagent>
      </variant>
      <variant when="source-stale:cc eq false">
        <line><span optional="source:cc"><text>src </text></span><text>FRESH</text></line>
        <subagent><line><span optional="source:cc"><text>src </text></span><field name="task-description" prefix="fresh: "/></line></subagent>
      </variant>
    </responsive>
  </layout>
</statusloom>`

// TestRun_ClaudeSubagent_SourceResolution: the subagent path resolves external
// sources read-only through the same provider as the main status line, and
// never spawns the refresh worker.
func TestRun_ClaudeSubagent_SourceResolution(t *testing.T) {
	present := filepath.Join(t.TempDir(), "cc.json")
	if err := os.WriteFile(present, []byte(`{"status":"ok"}`), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "absent.json")
	task := map[string]any{"id": "t1", "description": "My task"}

	cases := []struct {
		name         string
		defs         []sources.Definition
		mainWant     string // exact main stdout ("" = none)
		subagentWant []string
	}{
		{
			name:         "file present",
			defs:         []sources.Definition{{ID: "cc", Kind: sources.KindFile, Path: present}},
			mainWant:     "src FRESH",
			subagentWant: []string{"src fresh: My task"},
		},
		{
			name:         "file missing",
			defs:         []sources.Definition{{ID: "cc", Kind: sources.KindFile, Path: missing}},
			mainWant:     "STALE",
			subagentWant: []string{"stale: My task"},
		},
		{
			name:         "undefined source",
			mainWant:     "",
			subagentWant: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupEnv(t)
			t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
			if len(tc.defs) > 0 {
				saveSources(t, tc.defs...)
			}
			writeDocument(t, "claude-code", subagentDocSourceGated)

			mainOut, stderr, code := runCLI(t, []string{"claude"}, payload(t), nil)
			if code != 0 {
				t.Fatalf("claude exit = %d, want 0 (stderr: %s)", code, stderr)
			}
			if tc.mainWant == "" {
				// Every main line is omitted, so the renderer falls back to the
				// session line; the point is that neither variant rendered.
				if strings.Contains(mainOut, "STALE") || strings.Contains(mainOut, "FRESH") {
					t.Errorf("main stdout = %q, want neither STALE nor FRESH", mainOut)
				}
			} else if got := strings.TrimRight(mainOut, "\n"); got != tc.mainWant {
				t.Errorf("main stdout = %q, want %q", got, tc.mainWant)
			}

			spawns := captureSpawns(t)
			subOut, stderr, code := runCLI(t, []string{"claude-subagent"}, subagentStdin(t, 120, task), nil)
			if code != 0 {
				t.Fatalf("claude-subagent exit = %d, want 0 (stderr: %s)", code, stderr)
			}
			var got []string
			for _, sl := range decodeSubagentLines(t, subOut) {
				got = append(got, sl.Content)
			}
			if strings.Join(got, "|") != strings.Join(tc.subagentWant, "|") {
				t.Errorf("subagent contents = %q, want %q", got, tc.subagentWant)
			}
			if n := len(*spawns); n != 0 {
				t.Errorf("claude-subagent spawned %d refresh worker(s), want 0: %q", n, *spawns)
			}
		})
	}
}
