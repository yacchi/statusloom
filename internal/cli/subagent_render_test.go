package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/config"
)

// decodeSubagentLines parses stdout as one {"id","content"} JSON object per
// line, failing the test on any malformed line.
func decodeSubagentLines(t *testing.T, stdout string) []subagentLine {
	t.Helper()
	var out []subagentLine
	for _, l := range lines(stdout) {
		var sl subagentLine
		if err := json.Unmarshal([]byte(l), &sl); err != nil {
			t.Fatalf("invalid JSON line %q: %v", l, err)
		}
		out = append(out, sl)
	}
	return out
}

func TestRun_ClaudeSubagent_Running(t *testing.T) {
	setupEnv(t)
	data := fixture(t, "subagent-running.json")

	stdout, stderr, code := runCLI(t, []string{"claude-subagent"}, data, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	got := decodeSubagentLines(t, stdout)
	if len(got) != 2 {
		t.Fatalf("stdout has %d JSON lines, want 2:\n%q", len(got), stdout)
	}

	// The default claude-code-subagent document renders:
	//   <description>  <model>  <flex>  <duration> · ↓ <tokens> (<context%>)
	// task-tokens is compact-number (28454 -> "28.5k") and task-context-percent
	// is percent precision=0 (28454/200000 -> "14%"). Duration is elapsed
	// wall-clock (time.Now() - startTime) so it is not asserted literally.
	if got[0].ID != "b1a2c3d4e5f60718" {
		t.Errorf("line 0 ID = %q, want b1a2c3d4e5f60718", got[0].ID)
	}
	// task-tokens/task-context-percent are right-aligned to min-width="6"/"4"
	// (markup.md "min-width"/"align") in the default document, so "28.5k"
	// (5 cols) and "14%" (3 cols) each carry one extra leading padding space
	// beyond their fixed prefix/suffix.
	for _, want := range []string{"Review render pipeline changes", "Opus 4.8", "↓  28.5k", "( 14%)"} {
		if !strings.Contains(got[0].Content, want) {
			t.Errorf("line 0 content missing %q: %q", want, got[0].Content)
		}
	}

	if got[1].ID != "0f1e2d3c4b5a6978" {
		t.Errorf("line 1 ID = %q, want 0f1e2d3c4b5a6978", got[1].ID)
	}
	for _, want := range []string{"Audit DSL validation paths", "Sonnet 5", "↓  27.5k"} {
		if !strings.Contains(got[1].Content, want) {
			t.Errorf("line 1 content missing %q: %q", want, got[1].Content)
		}
	}

	// The subagent render path must never fall back to the session
	// fallback line (model + tool-version): there is no snap.Session.Model
	// or snap.Tool.Version in this context, so "statusloom" (the last-resort
	// literal) must never appear.
	if strings.Contains(stdout, "statusloom") {
		t.Errorf("stdout unexpectedly contains the session fallback: %q", stdout)
	}
}

func TestRun_ClaudeSubagent_Completed(t *testing.T) {
	setupEnv(t)
	data := fixture(t, "subagent-completed.json")

	stdout, stderr, code := runCLI(t, []string{"claude-subagent"}, data, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	got := decodeSubagentLines(t, stdout)
	if len(got) != 2 {
		t.Fatalf("stdout has %d JSON lines, want 2:\n%q", len(got), stdout)
	}
	// The default document does not render the task status, so the two rows
	// are distinguished from the running fixture by their (larger) completed
	// token counts: task 0 -> 28663 ("28.7k"), task 1 -> 27694 ("27.7k").
	// The fixture's columns (257) is wide enough for every width breakpoint,
	// so the token stat is present on both rows. task-tokens is right-aligned
	// to min-width="6", so the 5-column value carries one extra leading
	// padding space beyond its fixed " · ↓ " prefix.
	if !strings.Contains(got[0].Content, "↓  28.7k") {
		t.Errorf("line 0 content missing completed token count %q: %q", "↓  28.7k", got[0].Content)
	}
	if !strings.Contains(got[1].Content, "↓  27.7k") {
		t.Errorf("line 1 content missing completed token count %q: %q", "↓  27.7k", got[1].Content)
	}
}

// TestRun_ClaudeSubagent_EmptyTasks confirms an empty tasks array produces
// no output lines (the CLI does not synthesize a fallback for a
// subagent render).
func TestRun_ClaudeSubagent_EmptyTasks(t *testing.T) {
	setupEnv(t)
	stdout, stderr, code := runCLI(t, []string{"claude-subagent"}, []byte(`{"session_id":"s","columns":80,"tasks":[]}`), nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

// TestRun_ClaudeSubagent_Malformed confirms invalid JSON fails loudly
// (exit 1, stderr message) rather than silently producing no output.
func TestRun_ClaudeSubagent_Malformed(t *testing.T) {
	setupEnv(t)
	stdout, stderr, code := runCLI(t, []string{"claude-subagent"}, []byte("{not json"), nil)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stderr == "" {
		t.Error("expected an error message on stderr")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

// TestRun_ClaudeSubagent_UsesPayloadColumns confirms the render width comes
// from the payload's "columns" field, not the COLUMNS environment variable
// (unlike the session `claude` command): a <flex/> in the default document
// should expand the line closer to the payload's width than to an unrelated
// COLUMNS override.
func TestRun_ClaudeSubagent_UsesPayloadColumns(t *testing.T) {
	setupEnv(t)
	data := fixture(t, "subagent-running.json") // columns: 257
	stdout, _, code := runCLI(t, []string{"claude-subagent"}, data, map[string]string{"COLUMNS": "10"})
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	got := decodeSubagentLines(t, stdout)
	if len(got) != 2 {
		t.Fatalf("stdout has %d JSON lines, want 2", len(got))
	}
	// With COLUMNS=10 ignored and the payload's columns=257 honored, the
	// flex-filled line is much longer than 10 visible characters (a strict
	// upper bound check without depending on ANSI-escape stripping logic).
	if len(got[0].Content) < 100 {
		t.Errorf("content len = %d, want a wide (payload columns=257) line: %q", len(got[0].Content), got[0].Content)
	}
}

// subagentDraftAltSeparatorSrc is claude-code-subagent's default document
// with the task-model prefix swapped from two spaces to " :: ", producing
// output distinguishable from the saved default. (The default document's
// fields carry no explicit color attribute, so toggling color-level - the
// trick draft_test.go uses for claude-code.xml - would not change subagent
// output at all; a literal-text change is used instead. The model prefix is
// used because task-model is the one stat always rendered regardless of
// width.)
func subagentDraftAltSeparatorSrc(t *testing.T) string {
	t.Helper()
	src := strings.Replace(config.DefaultDocument("claude-code-subagent"),
		`<field name="task-model" prefix="  "/>`,
		`<field name="task-model" prefix=" :: "/>`, 1)
	if !strings.Contains(src, "::") {
		t.Fatal("claude-code-subagent default document did not contain the task-model prefix to edit")
	}
	return src
}

// TestRun_ClaudeSubagent_Draft_RendersAgainstDraft confirms `claude-subagent
// --draft` prefers the shared claude-code-subagent.draft.xml over the saved
// document, the same fallback contract `monitor --draft` has for
// claude-code.xml.
func TestRun_ClaudeSubagent_Draft_RendersAgainstDraft(t *testing.T) {
	dir := setupDraftEnv(t)
	data := fixture(t, "subagent-running.json")

	if err := config.SaveDraftDocumentSource("claude-code-subagent", subagentDraftAltSeparatorSrc(t)); err != nil {
		t.Fatalf("SaveDraftDocumentSource: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "claude-code-subagent.draft.xml")); err != nil {
		t.Fatalf("draft not written: %v", err)
	}

	savedOut, _, code := runCLI(t, []string{"claude-subagent"}, data, nil)
	if code != 0 {
		t.Fatalf("claude-subagent (no --draft) exit = %d, want 0", code)
	}
	draftOut, stderr, code := runCLI(t, []string{"claude-subagent", "--draft"}, data, nil)
	if code != 0 {
		t.Fatalf("claude-subagent --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	if !strings.Contains(draftOut, "::") {
		t.Errorf("claude-subagent --draft output missing the draft's \"::\" separator: %q", draftOut)
	}
	if draftOut == savedOut {
		t.Errorf("claude-subagent --draft output equals the saved-document output; draft was not used")
	}
}

// TestRun_ClaudeSubagent_Preview confirms `claude-subagent --preview` never
// reads stdin (an empty reader must not error) and renders its built-in
// payload's three tasks against the default claude-code-subagent document,
// each producing a distinct, valid {"id","content"} JSON line.
func TestRun_ClaudeSubagent_Preview(t *testing.T) {
	setupEnv(t)

	stdout, stderr, code := runCLI(t, []string{"claude-subagent", "--preview"}, nil, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	got := decodeSubagentLines(t, stdout)
	if len(got) != 3 {
		t.Fatalf("stdout has %d JSON lines, want 3:\n%q", len(got), stdout)
	}

	for _, line := range got {
		if line.ID == "" {
			t.Errorf("line has empty id: %+v", line)
		}
		if line.Content == "" {
			t.Errorf("line %q has empty content", line.ID)
		}
	}

	// The three built-in tasks differ in model and description, so each
	// rendered line should be distinguishable from the others.
	if got[0].Content == got[1].Content || got[1].Content == got[2].Content || got[0].Content == got[2].Content {
		t.Errorf("preview lines are not distinct: %+v", got)
	}
	for _, want := range []string{"Investigate flaky render test", "Opus 4.8"} {
		if !strings.Contains(got[0].Content, want) {
			t.Errorf("line 0 content missing %q: %q", want, got[0].Content)
		}
	}
	for _, want := range []string{"Draft DSL validation fix", "Sonnet 5"} {
		if !strings.Contains(got[1].Content, want) {
			t.Errorf("line 1 content missing %q: %q", want, got[1].Content)
		}
	}
	for _, want := range []string{"Summarize test coverage gaps", "Haiku 4.5"} {
		if !strings.Contains(got[2].Content, want) {
			t.Errorf("line 2 content missing %q: %q", want, got[2].Content)
		}
	}
}

// TestRun_ClaudeSubagent_Preview_IgnoresStdin confirms --preview takes
// priority over stdin: piping the subagent-running fixture's tasks must not
// change the output from the empty-stdin case above.
func TestRun_ClaudeSubagent_Preview_IgnoresStdin(t *testing.T) {
	setupEnv(t)

	withStdin, stderr, code := runCLI(t, []string{"claude-subagent", "--preview"}, fixture(t, "subagent-running.json"), nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	withoutStdin, stderr, code := runCLI(t, []string{"claude-subagent", "--preview"}, nil, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if withStdin != withoutStdin {
		t.Errorf("--preview output changed when stdin carried real tasks; stdin should be ignored:\nwith stdin=%q\nwithout stdin=%q", withStdin, withoutStdin)
	}
}

// TestRun_ClaudeSubagent_PreviewDraft confirms `claude-subagent --preview
// --draft` renders the built-in preview payload against the shared draft
// document, isolated from any real config via a temp STATUSLOOM_CONFIG/
// STATUSLOOM_CACHE_DIR.
func TestRun_ClaudeSubagent_PreviewDraft(t *testing.T) {
	setupDraftEnv(t)

	if err := config.SaveDraftDocumentSource("claude-code-subagent", subagentDraftAltSeparatorSrc(t)); err != nil {
		t.Fatalf("SaveDraftDocumentSource: %v", err)
	}

	savedOut, _, code := runCLI(t, []string{"claude-subagent", "--preview"}, nil, nil)
	if code != 0 {
		t.Fatalf("claude-subagent --preview (no --draft) exit = %d, want 0", code)
	}
	draftOut, stderr, code := runCLI(t, []string{"claude-subagent", "--preview", "--draft"}, nil, nil)
	if code != 0 {
		t.Fatalf("claude-subagent --preview --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	if !strings.Contains(draftOut, "::") {
		t.Errorf("claude-subagent --preview --draft output missing the draft's \"::\" separator: %q", draftOut)
	}
	if draftOut == savedOut {
		t.Errorf("claude-subagent --preview --draft output equals the saved-document output; draft was not used")
	}
}

// TestRun_ClaudeSubagent_Draft_InvalidFallsBackToSaved confirms an invalid
// claude-code-subagent draft never blanks the agent-panel row: it falls back
// to the saved document, matching plain `claude-subagent`.
func TestRun_ClaudeSubagent_Draft_InvalidFallsBackToSaved(t *testing.T) {
	setupDraftEnv(t)
	data := fixture(t, "subagent-running.json")

	bad := `<statusloom version="1" tool="claude-code-subagent"><layout name="D" active="true"><line><field name="not-a-field"/></line></layout></statusloom>`
	if err := config.SaveDraftDocumentSource("claude-code-subagent", bad); err != nil {
		t.Fatalf("SaveDraftDocumentSource: %v", err)
	}

	savedOut, _, _ := runCLI(t, []string{"claude-subagent"}, data, nil)
	draftOut, stderr, code := runCLI(t, []string{"claude-subagent", "--draft"}, data, nil)
	if code != 0 {
		t.Fatalf("claude-subagent --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if draftOut != savedOut {
		t.Errorf("invalid draft did not fall back to the saved document:\ndraft=%q\nsaved=%q", draftOut, savedOut)
	}
	if !strings.Contains(stderr, "falling back") && !strings.Contains(stderr, "invalid") {
		t.Errorf("stderr %q should note the draft fallback", stderr)
	}
}
