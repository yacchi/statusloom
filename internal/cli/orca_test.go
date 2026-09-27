package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubEnviron temporarily replaces the package-level environ func (used by
// hasEnvWithPrefix) with one that returns kv, restoring it to os.Environ via
// t.Cleanup. Tests that need forwarding to actually happen use this to
// simulate being inside an Orca-hooked pane (ORCA_* variables present)
// without depending on, or polluting, the real process environment; tests
// that need forwarding to be skipped use it to guarantee no ORCA_* variable
// leaks in from the real environment the test binary happens to run under.
func stubEnviron(t *testing.T, kv []string) {
	t.Helper()
	orig := environ
	environ = func() []string { return kv }
	t.Cleanup(func() { environ = orig })
}

// writeOrcaStatusLineHook writes an executable ~/.orca/agent-hooks/
// claude-statusline.sh under home that copies its stdin to capturePath, and
// returns home for use as the HOME env passed to runCLI.
func writeOrcaStatusLineHook(t *testing.T, capturePath string) (home string) {
	t.Helper()
	home = t.TempDir()
	dir := filepath.Join(home, ".orca", "agent-hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	script := "#!/bin/sh\ncat >" + capturePath + "\n"
	path := filepath.Join(dir, "claude-statusline.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return home
}

// TestRun_Claude_ForwardsToOrcaHook verifies that `statusloom claude` hands
// the raw payload to ~/.orca/agent-hooks/claude-statusline.sh when it
// exists and is executable, without changing its own rendered output.
func TestRun_Claude_ForwardsToOrcaHook(t *testing.T) {
	setupEnv(t)
	stubEnviron(t, []string{"ORCA_PANE_KEY=test-pane"})
	data := fixture(t, "full.json")

	capture := filepath.Join(t.TempDir(), "captured.json")
	home := writeOrcaStatusLineHook(t, capture)

	withoutHook, _, code := runCLI(t, []string{"claude"}, data, nil)
	if code != 0 {
		t.Fatalf("claude (no hook) exit = %d, want 0", code)
	}

	withHook, stderr, code := runCLI(t, []string{"claude"}, data, map[string]string{"HOME": home})
	if code != 0 {
		t.Fatalf("claude (with hook) exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if withHook != withoutHook {
		t.Errorf("stdout changed by hook forwarding:\nwith hook   =%q\nwithout hook=%q", withHook, withoutHook)
	}

	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("hook was not invoked (reading %s): %v", capture, err)
	}
	if string(got) != string(data) {
		t.Errorf("hook received %q, want raw stdin %q", got, data)
	}
}

// TestRun_Claude_NoOrcaHookIsNoop verifies that `statusloom claude` renders
// normally when HOME has no ~/.orca/agent-hooks/claude-statusline.sh at all
// (the common case: Orca not installed).
func TestRun_Claude_NoOrcaHookIsNoop(t *testing.T) {
	setupEnv(t)
	data := fixture(t, "full.json")

	stdout, stderr, code := runCLI(t, []string{"claude"}, data, map[string]string{"HOME": t.TempDir()})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Errorf("expected rendered output, got empty stdout")
	}
}

// TestRun_Claude_OrcaHookFailureIsIgnored verifies that a hook script which
// exits non-zero never affects statusloom's own exit code or output.
func TestRun_Claude_OrcaHookFailureIsIgnored(t *testing.T) {
	setupEnv(t)
	stubEnviron(t, []string{"ORCA_PANE_KEY=test-pane"})
	data := fixture(t, "full.json")

	home := t.TempDir()
	dir := filepath.Join(home, ".orca", "agent-hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "claude-statusline.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stdout, stderr, code := runCLI(t, []string{"claude"}, data, map[string]string{"HOME": home})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 despite hook failure (stderr: %s)", code, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Errorf("expected rendered output despite hook failure, got empty stdout")
	}
}

// TestRun_Claude_OrcaHookNotExecutableIsSkipped verifies that a present but
// non-executable script is not run (and does not error).
func TestRun_Claude_OrcaHookNotExecutableIsSkipped(t *testing.T) {
	setupEnv(t)
	stubEnviron(t, []string{"ORCA_PANE_KEY=test-pane"})
	data := fixture(t, "full.json")

	home := t.TempDir()
	dir := filepath.Join(home, ".orca", "agent-hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	capture := filepath.Join(t.TempDir(), "captured.json")
	path := filepath.Join(dir, "claude-statusline.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\ncat >"+capture+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	stdout, stderr, code := runCLI(t, []string{"claude"}, data, map[string]string{"HOME": home})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Errorf("expected rendered output, got empty stdout")
	}
	if _, err := os.Stat(capture); err == nil {
		t.Errorf("non-executable hook was invoked, want skipped")
	}
}

// TestSetupClaudeCode_OrcaNote verifies that `statusloom setup claude-code`
// prints an explanatory note when the existing statusLine.command it is
// about to replace looks like Orca's own claude-statusline hook - and that
// this is judged purely from the existing command string, independent of
// whether ~/.orca/agent-hooks/claude-statusline.sh actually exists on disk
// (a machine can have the hook file present while statusLine already points
// at statusloom, or vice versa).
func TestSetupClaudeCode_OrcaNote(t *testing.T) {
	const orcaCommand = `case "$(uname -s)" in Darwin*|Linux*) exec "${HOME-}/.orca/agent-hooks/claude-statusline.sh";; *) exec "${HOME-}/.orca/agent-hooks/claude-statusline.cmd";; esac`

	tests := []struct {
		name     string
		command  string // "" -> no existing statusLine at all
		home     string // "" -> no HOME override, "hook" -> HOME with the hook file present
		wantNote bool
	}{
		{
			name:     "orca command without hook file on disk",
			command:  orcaCommand,
			home:     "",
			wantNote: true,
		},
		{
			name:     "orca command with hook file present",
			command:  orcaCommand,
			home:     "hook",
			wantNote: true,
		},
		{
			name:     "unrelated existing command, hook file present",
			command:  "old-status-line",
			home:     "hook",
			wantNote: false,
		},
		{
			name:     "unrelated existing command, no hook file",
			command:  "old-status-line",
			home:     "",
			wantNote: false,
		},
		{
			name:     "fresh install, hook file present",
			command:  "",
			home:     "hook",
			wantNote: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			if tt.command != "" {
				initial, err := json.Marshal(map[string]any{
					"statusLine": map[string]any{"type": "command", "command": tt.command},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, initial, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var env map[string]string
			if tt.home == "hook" {
				home := writeOrcaStatusLineHook(t, filepath.Join(t.TempDir(), "captured.json"))
				env = map[string]string{"HOME": home}
			}
			args := []string{"setup", "claude-code", "--settings", path, "--yes"}
			stdout, stderr, code := runCLI(t, args, nil, env)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
			}
			gotNote := strings.Contains(stdout, "Orca's")
			if gotNote != tt.wantNote {
				t.Errorf("note present = %v, want %v\nstdout=%q", gotNote, tt.wantNote, stdout)
			}
		})
	}
}

// TestForwardToKnownStatusLineHooks_MultipleEntries verifies that the
// registry generalization actually forwards to every registered hook, not
// just Orca's: it temporarily adds a second, fictitious hook to
// knownStatusLineHooks and checks that both it and Orca's hook receive the
// raw payload.
func TestForwardToKnownStatusLineHooks_MultipleEntries(t *testing.T) {
	setupEnv(t)
	stubEnviron(t, []string{"ORCA_PANE_KEY=test-pane"})
	data := fixture(t, "full.json")

	home := t.TempDir()
	orcaCapture := filepath.Join(t.TempDir(), "orca-captured.json")
	writeHookScript(t, filepath.Join(home, ".orca", "agent-hooks", "claude-statusline.sh"), orcaCapture)

	fakeCapture := filepath.Join(t.TempDir(), "fake-captured.json")
	fakePath := filepath.Join(home, ".fake-tool", "statusline.sh")
	writeHookScript(t, fakePath, fakeCapture)

	orig := knownStatusLineHooks
	knownStatusLineHooks = append(append([]knownStatusLineHook{}, orig...), knownStatusLineHook{
		name: "FakeTool",
		path: func(getenv func(string) string) string {
			return filepath.Join(getenv("HOME"), ".fake-tool", "statusline.sh")
		},
		marker: ".fake-tool/statusline.sh",
	})
	t.Cleanup(func() { knownStatusLineHooks = orig })

	stdout, stderr, code := runCLI(t, []string{"claude"}, data, map[string]string{"HOME": home})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Errorf("expected rendered output, got empty stdout")
	}

	for _, cap := range []struct {
		name, path string
	}{{"Orca", orcaCapture}, {"FakeTool", fakeCapture}} {
		got, err := os.ReadFile(cap.path)
		if err != nil {
			t.Fatalf("%s hook was not invoked (reading %s): %v", cap.name, cap.path, err)
		}
		if string(got) != string(data) {
			t.Errorf("%s hook received %q, want raw stdin %q", cap.name, got, data)
		}
	}
}

// TestRun_Claude_ForwardsToOrcaHook_WithOrcaEnvPrefix verifies that the
// envPrefix pre-fork shortcut lets forwarding through when an ORCA_*
// environment variable is present, even though its specific name is
// something forwardToKnownStatusLineHooks has never heard of - only the
// "ORCA_" prefix is checked (see the envPrefix field doc for why).
func TestRun_Claude_ForwardsToOrcaHook_WithOrcaEnvPrefix(t *testing.T) {
	setupEnv(t)
	stubEnviron(t, []string{"ORCA_SOME_FUTURE_VARIABLE=1"})
	data := fixture(t, "full.json")

	capture := filepath.Join(t.TempDir(), "captured.json")
	home := writeOrcaStatusLineHook(t, capture)

	stdout, stderr, code := runCLI(t, []string{"claude"}, data, map[string]string{"HOME": home})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Errorf("expected rendered output, got empty stdout")
	}

	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("hook was not invoked (reading %s): %v", capture, err)
	}
	if string(got) != string(data) {
		t.Errorf("hook received %q, want raw stdin %q", got, data)
	}
}

// TestRun_Claude_SkipsOrcaHookWithoutOrcaEnvPrefix verifies the envPrefix
// pre-fork shortcut: when no ORCA_*-prefixed environment variable is
// present, forwardToKnownStatusLineHooks skips forking Orca's hook
// altogether, even though the hook file exists and is executable.
func TestRun_Claude_SkipsOrcaHookWithoutOrcaEnvPrefix(t *testing.T) {
	setupEnv(t)
	stubEnviron(t, nil)
	data := fixture(t, "full.json")

	capture := filepath.Join(t.TempDir(), "captured.json")
	home := writeOrcaStatusLineHook(t, capture)

	stdout, stderr, code := runCLI(t, []string{"claude"}, data, map[string]string{"HOME": home})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Errorf("expected rendered output, got empty stdout")
	}

	if _, err := os.Stat(capture); err == nil {
		t.Errorf("hook was invoked despite no ORCA_* environment variable present, want skipped")
	}
}

// TestHasEnvWithPrefix covers hasEnvWithPrefix directly, including the
// empty-prefix "always true" case used by hooks without an envPrefix.
func TestHasEnvWithPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		env    []string
		want   bool
	}{
		{name: "empty prefix always matches", prefix: "", env: nil, want: true},
		{name: "no vars at all", prefix: "ORCA_", env: nil, want: false},
		{name: "unrelated vars only", prefix: "ORCA_", env: []string{"HOME=/x", "PATH=/y"}, want: false},
		{name: "matching var present", prefix: "ORCA_", env: []string{"ORCA_TAB_ID=1"}, want: true},
		{name: "matching var among others", prefix: "ORCA_", env: []string{"HOME=/x", "ORCA_AGENT_HOOK_PORT=123"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubEnviron(t, tt.env)
			if got := hasEnvWithPrefix(tt.prefix); got != tt.want {
				t.Errorf("hasEnvWithPrefix(%q) = %v, want %v", tt.prefix, got, tt.want)
			}
		})
	}
}

// writeHookScript writes an executable shell script at path that copies its
// stdin to capturePath, creating parent directories as needed.
func writeHookScript(t *testing.T, path, capturePath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	script := "#!/bin/sh\ncat >" + capturePath + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
