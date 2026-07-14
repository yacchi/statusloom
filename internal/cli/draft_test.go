package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/config"
)

// setupDraftEnv isolates STATUSLOOM_CONFIG (so the draft document resolves into
// a temp dir) and STATUSLOOM_CACHE_DIR, returning the config dir.
func setupDraftEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	return dir
}

func TestDraft_PullThenPush_RoundTrips(t *testing.T) {
	dir := setupDraftEnv(t)
	outFile := filepath.Join(t.TempDir(), "draft.xml")

	// pull with no draft/document falls back to the default DSL and writes it.
	stdout, stderr, code := runCLI(t, []string{"draft", "pull", outFile}, nil, nil)
	if code != 0 {
		t.Fatalf("pull exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, outFile) {
		t.Errorf("pull stdout %q missing path %q", stdout, outFile)
	}

	// The pulled file is the DSL default document.
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if !strings.Contains(string(data), "<statusloom") {
		t.Errorf("pulled file is not DSL markup:\n%s", data)
	}

	// Edit it: a valid change (color-level truecolor).
	edited := strings.Replace(string(data), `color-level="ansi16"`, `color-level="truecolor"`, 1)
	if edited == string(data) {
		t.Fatal("default document did not contain color-level=\"ansi16\" to edit")
	}
	if err := os.WriteFile(outFile, []byte(edited), 0o600); err != nil {
		t.Fatalf("write edited file: %v", err)
	}

	// push writes it to the shared draft.
	_, stderr, code = runCLI(t, []string{"draft", "push", outFile}, nil, nil)
	if code != 0 {
		t.Fatalf("push exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	// The shared draft file now reflects the edit.
	draftPath := filepath.Join(dir, "claude-code.draft.xml")
	saved, err := os.ReadFile(draftPath)
	if err != nil {
		t.Fatalf("shared draft not written: %v", err)
	}
	if !strings.Contains(string(saved), `color-level="truecolor"`) {
		t.Errorf("draft did not capture the edit:\n%s", saved)
	}
}

func TestDraft_PushInvalidStillShares(t *testing.T) {
	dir := setupDraftEnv(t)
	badFile := filepath.Join(t.TempDir(), "bad.xml")

	// An invalid document (unknown field): push must still share it (the draft
	// is a text-sharing channel), reporting diagnostics on stderr.
	bad := `<statusloom version="1" tool="claude-code"><layout name="Default" active="true"><line><field name="not-a-field"/></line></layout></statusloom>`
	if err := os.WriteFile(badFile, []byte(bad), 0o600); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	_, stderr, code := runCLI(t, []string{"draft", "push", badFile}, nil, nil)
	if code != 0 {
		t.Fatalf("push exit = %d, want 0 (invalid drafts still share)", code)
	}
	if !strings.Contains(stderr, "not-a-field") {
		t.Errorf("stderr %q should report the diagnostic", stderr)
	}
	// The invalid draft was still written (monitor --draft falls back on it).
	saved, err := os.ReadFile(filepath.Join(dir, "claude-code.draft.xml"))
	if err != nil {
		t.Fatalf("invalid push did not write the draft: %v", err)
	}
	if !strings.Contains(string(saved), "not-a-field") {
		t.Errorf("draft content mismatch:\n%s", saved)
	}
}

func TestDraft_UsageErrors(t *testing.T) {
	setupDraftEnv(t)
	for _, args := range [][]string{
		{"draft"},
		{"draft", "bogus"},
		{"draft", "pull", "a", "b"},
	} {
		_, _, code := runCLI(t, args, nil, nil)
		if code != 2 {
			t.Errorf("args %v: exit = %d, want 2", args, code)
		}
	}
}

// draftNoColorSrc is the default document with color-level switched to none,
// so rendering it produces no ANSI escapes (distinguishable from the saved
// ansi16 default).
func draftNoColorSrc(t *testing.T) string {
	t.Helper()
	src := strings.Replace(config.DefaultDocument("claude-code"), `color-level="ansi16"`, `color-level="none"`, 1)
	if !strings.Contains(src, `color-level="none"`) {
		t.Fatal("default document did not contain color-level=\"ansi16\" to edit")
	}
	return src
}

func TestMonitor_Draft_RendersAgainstDraft(t *testing.T) {
	dir := setupDraftEnv(t)
	data := fixture(t, "full.json")

	// Saved document = default (color-level ansi16 -> ANSI escapes present).
	// Draft = color-level none -> NO ANSI escapes. monitor --draft must use
	// the draft, so its output must differ from plain `claude`.
	if err := config.SaveDraftDocumentSource("claude-code", draftNoColorSrc(t)); err != nil {
		t.Fatalf("SaveDraftDocumentSource: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "claude-code.draft.xml")); err != nil {
		t.Fatalf("draft not written: %v", err)
	}

	claudeOut, _, _ := runCLI(t, []string{"claude"}, data, nil)
	draftOut, stderr, code := runCLI(t,
		[]string{"monitor", "--emit-url", "http://127.0.0.1:1/api/live", "--token", "tok", "--draft"}, data, nil)
	if code != 0 {
		t.Fatalf("monitor --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	if strings.Contains(draftOut, "\x1b[") {
		t.Errorf("monitor --draft output has ANSI escapes, but draft color-level=none: %q", draftOut)
	}
	if draftOut == claudeOut {
		t.Errorf("monitor --draft output equals claude output; draft was not used")
	}
}

func TestMonitor_Draft_InvalidFallsBackToSaved(t *testing.T) {
	setupDraftEnv(t)
	data := fixture(t, "full.json")

	// An invalid draft (unknown field) must not blank the status line: monitor
	// --draft falls back to the saved document, matching plain `claude`.
	bad := `<statusloom version="1" tool="claude-code"><layout name="D" active="true"><line><field name="not-a-field"/></line></layout></statusloom>`
	if err := config.SaveDraftDocumentSource("claude-code", bad); err != nil {
		t.Fatalf("SaveDraftDocumentSource: %v", err)
	}

	claudeOut, _, _ := runCLI(t, []string{"claude"}, data, nil)
	draftOut, stderr, code := runCLI(t,
		[]string{"monitor", "--emit-url", "http://127.0.0.1:1/api/live", "--token", "tok", "--draft"}, data, nil)
	if code != 0 {
		t.Fatalf("monitor --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if draftOut != claudeOut {
		t.Errorf("invalid draft did not fall back to the saved document:\ndraft =%q\nclaude=%q", draftOut, claudeOut)
	}
	if !strings.Contains(stderr, "falling back") && !strings.Contains(stderr, "invalid") {
		t.Errorf("stderr %q should note the draft fallback", stderr)
	}
}

func TestMonitor_NoDraft_MatchesClaude(t *testing.T) {
	setupDraftEnv(t)
	data := fixture(t, "full.json")

	// A draft exists (color-level none), but monitor WITHOUT --draft must
	// ignore it and render the saved document exactly like `claude`.
	if err := config.SaveDraftDocumentSource("claude-code", draftNoColorSrc(t)); err != nil {
		t.Fatalf("SaveDraftDocumentSource: %v", err)
	}

	claudeOut, _, _ := runCLI(t, []string{"claude"}, data, nil)
	monitorOut, stderr, code := runCLI(t,
		[]string{"monitor", "--emit-url", "http://127.0.0.1:1/api/live", "--token", "tok"}, data, nil)
	if code != 0 {
		t.Fatalf("monitor exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if monitorOut != claudeOut {
		t.Errorf("monitor (no --draft) output != claude output:\nmonitor=%q\nclaude =%q", monitorOut, claudeOut)
	}
}
