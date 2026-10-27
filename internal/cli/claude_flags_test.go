package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFailer is a stdin reader that fails the test if anything reads it. It
// proves an invocation is rejected by flag parsing or the terminal check
// before stdin is touched.
type readFailer struct{ t *testing.T }

func (r readFailer) Read(p []byte) (int, error) {
	r.t.Errorf("stdin was read; the invocation must be rejected before reading stdin")
	return 0, io.EOF
}

// claudeFlagsSavedDoc and claudeFlagsDraftDoc are claude-code documents whose
// only visible content is a distinct literal, so the test can tell which one
// rendered.
const claudeFlagsSavedDoc = `<statusloom version="1" tool="claude-code" color-level="none"><layout name="default" active="true"><line><text>SAVED</text></line></layout></statusloom>`

const claudeFlagsDraftDoc = `<statusloom version="1" tool="claude-code" color-level="none"><layout name="default" active="true"><line><text>DRAFT</text></line></layout></statusloom>`

// TestRun_ClaudeAndRender_RejectBadArgsBeforeReadingStdin confirms that a
// flag error, -h/--help, or a stray positional argument makes `claude` and
// `render` exit 2 without reading stdin.
func TestRun_ClaudeAndRender_RejectBadArgsBeforeReadingStdin(t *testing.T) {
	setupEnv(t)

	cases := [][]string{
		{"claude", "--bogus"},
		{"claude", "--help"},
		{"claude", "foo"},
		{"render", "--bogus"},
		{"render", "--help"},
		{"render", "foo"},
	}
	for _, args := range cases {
		var out, errBuf bytes.Buffer
		code := Run(args, readFailer{t}, &out, &errBuf, "test-version", envMap(nil))
		if code != 2 {
			t.Errorf("Run(%q) exit = %d, want 2 (stderr: %s)", args, code, errBuf.String())
		}
		if out.Len() != 0 {
			t.Errorf("Run(%q) wrote to stdout: %q", args, out.String())
		}
	}
}

// TestRun_Claude_DraftRendersDraft confirms `claude --draft` renders the draft
// working node while plain `claude` keeps rendering the saved document.
func TestRun_Claude_DraftRendersDraft(t *testing.T) {
	setupEnv(t)
	writeDocument(t, "claude-code", claudeFlagsSavedDoc)
	writeDraft(t, "claude-code", claudeFlagsDraftDoc)
	data := fixture(t, "full.json")

	savedOut, stderr, code := runCLI(t, []string{"claude"}, data, nil)
	if code != 0 {
		t.Fatalf("claude exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(savedOut, "SAVED") || strings.Contains(savedOut, "DRAFT") {
		t.Errorf("claude output should be the saved document: %q", savedOut)
	}

	draftOut, stderr, code := runCLI(t, []string{"claude", "--draft"}, data, nil)
	if code != 0 {
		t.Fatalf("claude --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(draftOut, "DRAFT") || strings.Contains(draftOut, "SAVED") {
		t.Errorf("claude --draft output should be the draft document: %q", draftOut)
	}
}

// TestRun_Render_DraftRendersDraft confirms `render --tool claude-code --draft`
// renders the draft working node, as `claude --draft` does.
func TestRun_Render_DraftRendersDraft(t *testing.T) {
	setupEnv(t)
	writeDocument(t, "claude-code", claudeFlagsSavedDoc)
	writeDraft(t, "claude-code", claudeFlagsDraftDoc)
	data := fixture(t, "full.json")

	savedOut, stderr, code := runCLI(t, []string{"render", "--tool", "claude-code"}, data, nil)
	if code != 0 {
		t.Fatalf("render exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(savedOut, "SAVED") || strings.Contains(savedOut, "DRAFT") {
		t.Errorf("render output should be the saved document: %q", savedOut)
	}

	draftOut, stderr, code := runCLI(t, []string{"render", "--tool", "claude-code", "--draft"}, data, nil)
	if code != 0 {
		t.Fatalf("render --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(draftOut, "DRAFT") || strings.Contains(draftOut, "SAVED") {
		t.Errorf("render --draft output should be the draft document: %q", draftOut)
	}
}

// TestRun_Claude_DraftDoesNotForwardToHook confirms a manual `claude --draft`
// preview is never handed to a known third-party statusline hook, while plain
// `claude` still is (the sanity check that the hook seam is actually wired).
func TestRun_Claude_DraftDoesNotForwardToHook(t *testing.T) {
	setupEnv(t)
	stubEnviron(t, []string{"ORCA_PANE_KEY=test-pane"})
	writeDraft(t, "claude-code", claudeFlagsDraftDoc)
	data := fixture(t, "full.json")

	capture := filepath.Join(t.TempDir(), "captured.json")
	home := writeOrcaStatusLineHook(t, capture)
	env := map[string]string{"HOME": home}

	if _, stderr, code := runCLI(t, []string{"claude", "--draft"}, data, env); code != 0 {
		t.Fatalf("claude --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("claude --draft forwarded the payload to the hook (stat err: %v)", err)
	}

	if _, stderr, code := runCLI(t, []string{"claude"}, data, env); code != 0 {
		t.Fatalf("claude exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if _, err := os.Stat(capture); err != nil {
		t.Fatalf("claude did not forward to the hook (stat err: %v)", err)
	}
}

// TestRun_Claude_DraftDoesNotWriteAccountCache confirms a manual `claude
// --draft` preview never writes the sample payload's rate_limits into the real
// account cache, while plain `claude` does (guards that the write path is
// actually exercised).
func TestRun_Claude_DraftDoesNotWriteAccountCache(t *testing.T) {
	setupEnv(t)
	writeDraft(t, "claude-code", claudeFlagsDraftDoc)
	data := fixture(t, "full.json")
	accountFile := filepath.Join(os.Getenv("STATUSLOOM_CACHE_DIR"), "account", "default.json")

	if _, stderr, code := runCLI(t, []string{"claude", "--draft"}, data, nil); code != 0 {
		t.Fatalf("claude --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if _, err := os.Stat(accountFile); !os.IsNotExist(err) {
		t.Fatalf("claude --draft wrote the account cache (stat err: %v)", err)
	}

	if _, stderr, code := runCLI(t, []string{"claude"}, data, nil); code != 0 {
		t.Fatalf("claude exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if _, err := os.Stat(accountFile); err != nil {
		t.Fatalf("claude did not write the account cache (stat err: %v)", err)
	}
}

// TestRun_Claude_DraftAbsentFallsBackWithNotice confirms a requested but absent
// draft renders the saved document exactly, and writes one stderr notice.
func TestRun_Claude_DraftAbsentFallsBackWithNotice(t *testing.T) {
	setupEnv(t)
	writeDocument(t, "claude-code", claudeFlagsSavedDoc)
	data := fixture(t, "full.json")

	savedOut, stderr, code := runCLI(t, []string{"claude"}, data, nil)
	if code != 0 {
		t.Fatalf("claude exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	draftOut, stderr, code := runCLI(t, []string{"claude", "--draft"}, data, nil)
	if code != 0 {
		t.Fatalf("claude --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if draftOut != savedOut {
		t.Errorf("absent draft did not render the saved document:\ndraft=%q\nsaved=%q", draftOut, savedOut)
	}

	var notices []string
	for _, l := range lines(stderr) {
		if strings.Contains(l, "draft") {
			notices = append(notices, l)
		}
	}
	if len(notices) != 1 {
		t.Errorf("stderr has %d draft lines, want exactly 1: %q", len(notices), stderr)
	}
}

// TestRun_Claude_DraftInvalidFallsBackWithOneNotice confirms a present but
// invalid draft renders the saved document and writes exactly one stderr line
// mentioning the draft, with the diagnostics folded into that line.
func TestRun_Claude_DraftInvalidFallsBackWithOneNotice(t *testing.T) {
	setupEnv(t)
	writeDocument(t, "claude-code", claudeFlagsSavedDoc)
	writeDraft(t, "claude-code", `<statusloom version="1" tool="claude-code" color-level="none"><layout name="default" active="true"><line><oops`)
	data := fixture(t, "full.json")

	savedOut, stderr, code := runCLI(t, []string{"claude"}, data, nil)
	if code != 0 {
		t.Fatalf("claude exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	draftOut, stderr, code := runCLI(t, []string{"claude", "--draft"}, data, nil)
	if code != 0 {
		t.Fatalf("claude --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if draftOut != savedOut {
		t.Errorf("invalid draft did not render the saved document:\ndraft=%q\nsaved=%q", draftOut, savedOut)
	}

	var notices []string
	for _, l := range lines(stderr) {
		if strings.Contains(l, "draft") {
			notices = append(notices, l)
		}
	}
	if len(notices) != 1 {
		t.Errorf("stderr has %d draft lines, want exactly 1: %q", len(notices), stderr)
	}
}

// TestStdinIsTerminal_PipesAndReadersAreNotTerminals confirms a pipe and a
// non-file reader are never mistaken for an interactive terminal.
func TestStdinIsTerminal_PipesAndReadersAreNotTerminals(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	if stdinIsTerminal(r) {
		t.Errorf("stdinIsTerminal(pipe) = true, want false")
	}
	if stdinIsTerminal(strings.NewReader("{}")) {
		t.Errorf("stdinIsTerminal(strings.Reader) = true, want false")
	}
}
