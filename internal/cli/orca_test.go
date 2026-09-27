package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
