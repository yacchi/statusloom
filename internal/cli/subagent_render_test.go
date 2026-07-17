package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// These tests write their own inline claude-code documents rather than
// relying on the shipped default document, so each case is self-contained
// about exactly which <subagent> region it exercises rather than depending
// on the default document's exact shape. `claude-subagent` renders the same
// single claude-code document `claude` does (there is no separate subagent
// tool or document anymore), so every case here installs whatever
// <subagent> region it needs via the writeDocument / writeDraft store
// helpers (cli_test.go).

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

// writeSubagentDoc installs src as the saved claude-code current document.
func writeSubagentDoc(t *testing.T, src string) {
	t.Helper()
	writeDocument(t, "claude-code", src)
}

// subagentStdin builds a minimal subagentStatusLine stdin payload with the
// given columns and tasks (each a JSON-encodable map, matching the tasks[]
// shape claude.DecodeSubagent expects).
func subagentStdin(t *testing.T, columns int, tasks ...map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"session_id": "test-session",
		"columns":    columns,
		"tasks":      tasks,
	})
	if err != nil {
		t.Fatalf("marshal subagent stdin: %v", err)
	}
	return data
}

// subagentDocBasic has no <responsive>: the layout's own <subagent> region
// renders task-description, task-model, task-tokens, and
// task-context-percent, exercising field-value reflection through the CLI.
const subagentDocBasic = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <line><field name="model"/></line>
    <subagent>
      <line>
        <field name="task-description"/>
        <text>  </text>
        <field name="task-model"/>
        <text> - </text>
        <field name="task-tokens"/>
        <text> (</text>
        <field name="task-context-percent"/>
        <text>)</text>
      </line>
    </subagent>
  </layout>
</statusloom>`

// subagentDocBasicDraftMarker is subagentDocBasic with an extra literal
// suffix, so a draft written from it is distinguishable from the saved
// subagentDocBasic document without touching color-level (subagent fields
// carry no color attribute in these fixtures, so toggling color-level would
// not change subagent output).
const subagentDocBasicDraftMarker = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <line><field name="model"/></line>
    <subagent>
      <line>
        <field name="task-description"/>
        <text>  </text>
        <field name="task-model"/>
        <text> - </text>
        <field name="task-tokens"/>
        <text> (</text>
        <field name="task-context-percent"/>
        <text>) [draft]</text>
      </line>
    </subagent>
  </layout>
</statusloom>`

func TestRun_ClaudeSubagent_BasicFields(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocBasic)
	data := fixture(t, "subagent-running.json")

	stdout, stderr, code := runCLI(t, []string{"claude-subagent"}, data, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	got := decodeSubagentLines(t, stdout)
	if len(got) != 2 {
		t.Fatalf("stdout has %d JSON lines, want 2:\n%q", len(got), stdout)
	}

	if got[0].ID != "b1a2c3d4e5f60718" {
		t.Errorf("line 0 ID = %q, want b1a2c3d4e5f60718", got[0].ID)
	}
	for _, want := range []string{"Review render pipeline changes", "Opus 4.8", "28,454", "14.2%"} {
		if !strings.Contains(got[0].Content, want) {
			t.Errorf("line 0 content missing %q: %q", want, got[0].Content)
		}
	}

	if got[1].ID != "0f1e2d3c4b5a6978" {
		t.Errorf("line 1 ID = %q, want 0f1e2d3c4b5a6978", got[1].ID)
	}
	for _, want := range []string{"Audit DSL validation paths", "Sonnet 5", "27,543", "13.8%"} {
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

// subagentDocResponsiveCLI has a <responsive> whose wide variant and narrow
// variant each carry their own, distinguishable <subagent> region, and no
// layout-level fallback: which region renders depends entirely on which
// variant the payload's columns select.
const subagentDocResponsiveCLI = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <responsive>
      <variant>
        <line><text>AAAAAAAAAAAAAAAAAAAA</text></line>
        <subagent><line><field name="task-description" prefix="wide: "/></line></subagent>
      </variant>
      <variant>
        <line><text>B</text></line>
        <subagent><line><field name="task-description" prefix="narrow: "/></line></subagent>
      </variant>
    </responsive>
  </layout>
</statusloom>`

// TestRun_ClaudeSubagent_ResponsiveByColumns confirms the subagent region
// resolves through the same <responsive> width selection the main pass
// uses, keyed off the subagentStatusLine payload's own "columns" field.
func TestRun_ClaudeSubagent_ResponsiveByColumns(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocResponsiveCLI)

	task := map[string]any{"id": "t1", "description": "My task"}

	wideStdout, stderr, code := runCLI(t, []string{"claude-subagent"}, subagentStdin(t, 30, task), nil)
	if code != 0 {
		t.Fatalf("wide columns exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	got := decodeSubagentLines(t, wideStdout)
	if len(got) != 1 || got[0].Content != "wide: My task" {
		t.Fatalf("wide columns: got %+v, want one line with content %q", got, "wide: My task")
	}

	narrowStdout, stderr, code := runCLI(t, []string{"claude-subagent"}, subagentStdin(t, 3, task), nil)
	if code != 0 {
		t.Fatalf("narrow columns exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	got = decodeSubagentLines(t, narrowStdout)
	if len(got) != 1 || got[0].Content != "narrow: My task" {
		t.Fatalf("narrow columns: got %+v, want one line with content %q", got, "narrow: My task")
	}
}

// subagentDocFallbackCLI has a <responsive> whose variants carry no
// <subagent> region of their own, plus a layout-level <subagent> that must
// be used regardless of which variant the width selects.
const subagentDocFallbackCLI = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <responsive>
      <variant><line><text>AAAAAAAAAAAAAAAAAAAA</text></line></variant>
      <variant><line><text>B</text></line></variant>
    </responsive>
    <subagent><line><field name="task-description" prefix="fallback: "/></line></subagent>
  </layout>
</statusloom>`

func TestRun_ClaudeSubagent_FallbackWhenVariantHasNoSubagent(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocFallbackCLI)

	task := map[string]any{"id": "t1", "description": "My task"}

	for _, columns := range []int{30, 3} {
		stdout, stderr, code := runCLI(t, []string{"claude-subagent"}, subagentStdin(t, columns, task), nil)
		if code != 0 {
			t.Fatalf("columns=%d exit = %d, want 0 (stderr: %s)", columns, code, stderr)
		}
		got := decodeSubagentLines(t, stdout)
		if len(got) != 1 || got[0].Content != "fallback: My task" {
			t.Fatalf("columns=%d: got %+v, want one line with content %q", columns, got, "fallback: My task")
		}
	}
}

// subagentDocUndefinedCLI defines no <subagent> region at all: no task
// produces an output line.
const subagentDocUndefinedCLI = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <line><text>Model line</text></line>
  </layout>
</statusloom>`

func TestRun_ClaudeSubagent_NoSubagentRegionProducesNoLines(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocUndefinedCLI)
	data := fixture(t, "subagent-running.json")

	stdout, stderr, code := runCLI(t, []string{"claude-subagent"}, data, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (no <subagent> region defined)", stdout)
	}
}

// subagentDocSkipCLI's <subagent> region is a bare task-description field
// with no literal text around it, so a task with neither description nor
// label renders an empty line and is skipped.
const subagentDocSkipCLI = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <line><text>Model line</text></line>
    <subagent><line><field name="task-description"/></line></subagent>
  </layout>
</statusloom>`

// TestRun_ClaudeSubagent_SkipsTaskWithEmptyRender confirms multiple tasks
// each produce their own {"id","content"} line, in payload order, and a
// task whose region renders no visible content (here: no description and no
// label) is skipped rather than emitting an empty line.
func TestRun_ClaudeSubagent_SkipsTaskWithEmptyRender(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocSkipCLI)

	stdin := subagentStdin(t, 80,
		map[string]any{"id": "t1", "description": "Alpha task"},
		map[string]any{"id": "t2"}, // no description, no label -> empty render -> skipped
		map[string]any{"id": "t3", "description": "Gamma task"},
	)

	stdout, stderr, code := runCLI(t, []string{"claude-subagent"}, stdin, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	got := decodeSubagentLines(t, stdout)
	if len(got) != 2 {
		t.Fatalf("stdout has %d JSON lines, want 2 (t2 should be skipped):\n%q", len(got), stdout)
	}
	if got[0].ID != "t1" || got[0].Content != "Alpha task" {
		t.Errorf("line 0 = %+v, want {t1 \"Alpha task\"}", got[0])
	}
	if got[1].ID != "t3" || got[1].Content != "Gamma task" {
		t.Errorf("line 1 = %+v, want {t3 \"Gamma task\"}", got[1])
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

// subagentDocFlexCLI has a <flex/> between two task-* fields, so its
// rendered width tracks the terminal width it is given.
const subagentDocFlexCLI = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <line><text>Model line</text></line>
    <subagent>
      <line>
        <field name="task-description"/>
        <flex/>
        <field name="task-model"/>
      </line>
    </subagent>
  </layout>
</statusloom>`

// TestRun_ClaudeSubagent_UsesPayloadColumns confirms the render width comes
// from the payload's "columns" field, not the COLUMNS environment variable
// (unlike the session `claude` command): a <flex/> in the document should
// expand the line closer to the payload's width than to an unrelated
// COLUMNS override.
func TestRun_ClaudeSubagent_UsesPayloadColumns(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocFlexCLI)
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

// TestRun_ClaudeSubagent_Draft_RendersAgainstDraft confirms `claude-subagent
// --draft` prefers the tool's draft working node over the saved document, the
// same fallback contract `monitor --draft` has.
func TestRun_ClaudeSubagent_Draft_RendersAgainstDraft(t *testing.T) {
	setupDraftEnv(t)
	writeSubagentDoc(t, subagentDocBasic)
	writeDraft(t, "claude-code", subagentDocBasicDraftMarker)
	data := fixture(t, "subagent-running.json")

	savedOut, _, code := runCLI(t, []string{"claude-subagent"}, data, nil)
	if code != 0 {
		t.Fatalf("claude-subagent (no --draft) exit = %d, want 0", code)
	}
	draftOut, stderr, code := runCLI(t, []string{"claude-subagent", "--draft"}, data, nil)
	if code != 0 {
		t.Fatalf("claude-subagent --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	if !strings.Contains(draftOut, "[draft]") {
		t.Errorf("claude-subagent --draft output missing the draft marker: %q", draftOut)
	}
	if strings.Contains(savedOut, "[draft]") {
		t.Errorf("claude-subagent (no --draft) output unexpectedly has the draft marker: %q", savedOut)
	}
	if draftOut == savedOut {
		t.Errorf("claude-subagent --draft output equals the saved-document output; draft was not used")
	}
}

// TestRun_ClaudeSubagent_Draft_InvalidFallsBackToSaved confirms an invalid
// claude-code draft never blanks the agent-panel row: it falls back to the
// saved document, matching plain `claude-subagent`.
func TestRun_ClaudeSubagent_Draft_InvalidFallsBackToSaved(t *testing.T) {
	setupDraftEnv(t)
	writeSubagentDoc(t, subagentDocBasic)
	data := fixture(t, "subagent-running.json")

	bad := `<statusloom version="1" tool="claude-code"><layout name="D" active="true"><line><field name="not-a-field"/></line></layout></statusloom>`
	writeDraft(t, "claude-code", bad)

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

// TestRun_ClaudeSubagent_Preview confirms `claude-subagent --preview` never
// reads stdin (an empty reader must not error) and renders its built-in
// payload's three tasks against the installed claude-code document, each
// producing a distinct, valid {"id","content"} JSON line.
func TestRun_ClaudeSubagent_Preview(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocBasic)

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
	writeSubagentDoc(t, subagentDocBasic)

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
// --draft` renders the built-in preview payload against the tool's draft
// working node, isolated from any real config via a temp STATUSLOOM_CONFIG/
// STATUSLOOM_CACHE_DIR.
func TestRun_ClaudeSubagent_PreviewDraft(t *testing.T) {
	setupDraftEnv(t)
	writeSubagentDoc(t, subagentDocBasic)
	writeDraft(t, "claude-code", subagentDocBasicDraftMarker)

	savedOut, _, code := runCLI(t, []string{"claude-subagent", "--preview"}, nil, nil)
	if code != 0 {
		t.Fatalf("claude-subagent --preview (no --draft) exit = %d, want 0", code)
	}
	draftOut, stderr, code := runCLI(t, []string{"claude-subagent", "--preview", "--draft"}, nil, nil)
	if code != 0 {
		t.Fatalf("claude-subagent --preview --draft exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	if !strings.Contains(draftOut, "[draft]") {
		t.Errorf("claude-subagent --preview --draft output missing the draft marker: %q", draftOut)
	}
	if draftOut == savedOut {
		t.Errorf("claude-subagent --preview --draft output equals the saved-document output; draft was not used")
	}
}
