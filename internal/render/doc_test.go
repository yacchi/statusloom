package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

// parseDoc parses DSL source, failing the test on any error diagnostic.
func parseDoc(t *testing.T, src string) *dsl.Document {
	t.Helper()
	doc, diags := dsl.Parse(src)
	if dsl.HasErrors(diags) {
		t.Fatalf("parse errors: %v\nsrc:\n%s", diags, src)
	}
	if doc == nil || doc.Root == nil {
		t.Fatalf("parse returned no root:\n%s", src)
	}
	return doc
}

// renderDocStr is a convenience wrapper: parse + RenderDocumentString.
func renderDocStr(t *testing.T, src string, snap schema.StatusSnapshot, opts Options) string {
	t.Helper()
	return RenderDocumentString(snap, parseDoc(t, src), opts)
}

const renderingExample = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <line>
      <text>Model: </text>
      <field name="model" color="cyan" bold="true"/>

      <span
        optional="thinking-effort"
        prefix=" ("
        suffix=")"
        color="bright-black"
      >
        <field name="thinking-effort" color="yellow"/>
      </span>

      <text role="separator" padding="1">|</text>

      <text>Context: </text>
      <field
        name="context-percentage-usable"
        format="percent"
        precision="0"
      />
    </line>
  </layout>
</statusloom>`

// TestRenderDocument_RenderingForm exercises markup.md's "Rendering" example:
// the shape "Model: <model> (<effort>) | Context: <pct>%", and with the
// effort missing "Model: <model> | Context: <pct>%" (the separator survives
// because visible content still flanks it).
func TestRenderDocument_RenderingForm(t *testing.T) {
	opts := Options{Width: 120, Now: fixedNow}

	got := renderDocStr(t, renderingExample, fullSnapshot(), opts)
	// fullSnapshot: model "Opus", effort "high", usable = 64000/167000*100
	// = 38.3% -> precision 0 -> "38%".
	if want := "Model: Opus (high) | Context: 38%"; got != want {
		t.Errorf("with effort:\n got  %q\n want %q", got, want)
	}

	snap := fullSnapshot()
	snap.Session.ReasoningEffort = nil
	got = renderDocStr(t, renderingExample, snap, opts)
	if want := "Model: Opus | Context: 38%"; got != want {
		t.Errorf("effort missing (separator must survive):\n got  %q\n want %q", got, want)
	}
}

func TestRenderDocument_StyleInheritance(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="ansi16">
  <layout name="a" active="true">
    <line>
      <span color="cyan" prefix="(" suffix=")">
        <field name="thinking-effort" color="yellow"/>
      </span>
    </line>
  </layout>
</statusloom>`
	lines := RenderDocument(fullSnapshot(), parseDoc(t, src), Options{Width: 120, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	segs := lines[0].Segments
	// Expect: "(" cyan, "high" yellow, ")" cyan.
	const cyan = "\x1b[36m"
	const yellow = "\x1b[33m"
	joinedANSI := ""
	for _, s := range segs {
		joinedANSI += s.ANSI
	}
	if !strings.Contains(joinedANSI, cyan+"(") {
		t.Errorf("prefix should be cyan (span style): %q", joinedANSI)
	}
	if !strings.Contains(joinedANSI, yellow+"high") {
		t.Errorf("child should override to yellow: %q", joinedANSI)
	}
	if !strings.Contains(joinedANSI, cyan+")") {
		t.Errorf("suffix should be cyan (span style): %q", joinedANSI)
	}
}

func TestRenderDocument_PowerlineTransition(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="ansi16" output-style="powerline">
  <layout name="a" active="true">
    <line>
      <span background="blue"><text>A</text></span>
      <text role="separator"></text>
      <text when="five-hour-percent gt 100" background="red">hidden</text>
      <span background="green"><text>B</text></span>
    </line>
  </layout>
</statusloom>`
	lines := RenderDocument(richSnapshot(), parseDoc(t, src), Options{Width: 120, Now: fixedNow})
	if len(lines) != 1 || lines[0].Omitted {
		t.Fatalf("expected one visible line: %#v", lines)
	}
	var powerline *DocSegment
	for i := range lines[0].Segments {
		if lines[0].Segments[i].Text == "\ue0b0" {
			powerline = &lines[0].Segments[i]
			break
		}
	}
	if powerline == nil {
		t.Fatal("powerline separator was not rendered")
	}
	if want := "\x1b[34;42m\ue0b0\x1b[0m"; powerline.ANSI != want {
		t.Errorf("powerline ANSI = %q, want %q", powerline.ANSI, want)
	}
}

func TestRenderDocument_PowerlineCollapses(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none" output-style="powerline">
  <layout name="a" active="true">
    <line><text role="separator"></text><text>A</text><text role="separator"></text></line>
  </layout>
</statusloom>`
	if got := renderDocStr(t, src, fullSnapshot(), Options{Width: 120, Now: fixedNow}); got != " A " {
		t.Errorf("edge powerline separators should collapse: got %q", got)
	}
}

func TestRenderDocument_PowerlineIgnoresManualSeparatorText(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none" output-style="powerline">
  <layout name="a" active="true">
    <line><text>A</text><text role="separator"> / </text><text>B</text></line>
  </layout>
</statusloom>`
	if got := renderDocStr(t, src, fullSnapshot(), Options{Width: 120, Now: fixedNow}); got != " A \ue0b0 B " {
		t.Errorf("Powerline manual separator filtering = %q, want %q", got, " A \ue0b0 B ")
	}
}

func TestRenderDocument_LegacyDefaultSeparatorUsesPowerlineStyle(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none" output-style="powerline">
  <layout name="a" active="true">
    <line><text>A</text><text role="separator" padding="1">|</text><text>B</text></line>
  </layout>
</statusloom>`
	if got := renderDocStr(t, src, fullSnapshot(), Options{Width: 120, Now: fixedNow}); got != " A \ue0b0 B " {
		t.Errorf("legacy default separator = %q, want %q", got, " A \ue0b0 B ")
	}
}

func TestRenderDocument_PowerlineAppliesBuiltInTheme(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="ansi16" output-style="powerline">
  <layout name="a" active="true">
    <line><text color="blue">A</text><text color="green">B</text></line>
  </layout>
</statusloom>`
	got := renderDocStr(t, src, fullSnapshot(), Options{Width: 120, Now: fixedNow})
	if !strings.Contains(got, "\x1b[30;46m A ") || !strings.Contains(got, "\x1b[36;45m\ue0b0") || !strings.Contains(got, "\x1b[97;45m B ") {
		t.Errorf("built-in Powerline theme was not applied: %q", got)
	}
	lines := RenderDocument(fullSnapshot(), parseDoc(t, src), Options{Width: 120, Now: fixedNow})
	var transition, secondContent *DocSegment
	for i := range lines[0].Segments {
		seg := &lines[0].Segments[i]
		if seg.Text == "\ue0b0" {
			transition = seg
		}
		if seg.Text == " B " {
			secondContent = seg
		}
	}
	if transition == nil || secondContent == nil || transition.Node != secondContent.Node {
		t.Errorf("generated transition must belong to the following segment node")
	}
}

func TestRenderDocument_PowerlineFlexClosesAndOpensRuns(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="ansi16" output-style="powerline">
  <layout name="a" active="true">
    <line><text>A</text><text>B</text><flex/><text>C</text><text>D</text></line>
  </layout>
</statusloom>`
	lines := RenderDocument(fullSnapshot(), parseDoc(t, src), Options{Width: 30, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	var plain, flexANSI string
	for _, seg := range lines[0].Segments {
		if seg.Visible {
			plain += seg.Text
		}
		if strings.Contains(seg.Text, "\ue0b2") {
			flexANSI = seg.ANSI
		}
	}
	if visibleWidth(plain) != 30 {
		t.Errorf("Powerline flex width = %d, want 30: %q", visibleWidth(plain), plain)
	}
	if !strings.Contains(flexANSI, "\x1b[35m\ue0b0") || !strings.Contains(flexANSI, "\x1b[33m\ue0b2") {
		t.Errorf("flex caps do not close/open the colored runs: %q", flexANSI)
	}
}

func TestRenderDocument_PowerlineNestedSpanIsOneSegment(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="ansi16" output-style="powerline">
  <layout name="a" active="true">
    <line>
      <span prefix="[" suffix="]"><text>A</text><span prefix="("><text>B</text><span><text>C</text></span></span></span>
      <text role="separator"/>
      <text>D</text>
    </line>
  </layout>
</statusloom>`
	lines := RenderDocument(fullSnapshot(), parseDoc(t, src), Options{Width: 40, Now: fixedNow})
	var plain, ansi string
	for _, seg := range lines[0].Segments {
		if seg.Visible {
			plain += seg.Text
			ansi += seg.ANSI
		}
	}
	if plain != " [A(BC] \ue0b0 D " {
		t.Errorf("nested span plain output = %q", plain)
	}
	if strings.Count(ansi, "48;") != 0 { // ANSI16 backgrounds use 40-47, not extended 48.
		t.Errorf("unexpected extended background sequence: %q", ansi)
	}
	for _, text := range []string{" [", "A", "(", "B", "C", "] "} {
		if !strings.Contains(ansi, "\x1b[30;46m"+text) {
			t.Errorf("nested span piece %q did not keep the segment theme: %q", text, ansi)
		}
	}
}

func TestRenderDocument_Optional(t *testing.T) {
	tmpl := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <line>
      <text>X</text>
      <span optional="%s" prefix="[" suffix="]"><field name="%s"/></span>
    </line>
  </layout>
</statusloom>`
	opts := Options{Width: 120, Now: fixedNow}

	// session-cost renders "$0.00" even at zero -> present -> shown.
	snap := fullSnapshot()
	snap.Session.Cost = money(0)
	got := RenderDocumentString(snap, parseDoc(t, fmt.Sprintf(tmpl, "session-cost", "session-cost")), opts)
	if want := "X[$0.00]"; got != want {
		t.Errorf("optional zero-value present:\n got %q\n want %q", got, want)
	}

	// thinking-effort missing -> whole span (incl. prefix/suffix) hidden.
	snap = fullSnapshot()
	snap.Session.ReasoningEffort = nil
	got = RenderDocumentString(snap, parseDoc(t, fmt.Sprintf(tmpl, "thinking-effort", "thinking-effort")), opts)
	if want := "X"; got != want {
		t.Errorf("optional missing hides span:\n got %q\n want %q", got, want)
	}
}

func TestRenderDocument_When(t *testing.T) {
	opts := Options{Width: 120, Now: fixedNow}
	tmpl := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <line><text>A</text><text when="%s">B</text></line>
  </layout>
</statusloom>`

	// five-hour-percent = 27 in richSnapshot.
	cases := []struct {
		name string
		when string
		want string
	}{
		{"word true", "five-hour-percent lt 50", "AB"},
		{"word false", "five-hour-percent gt 50", "A"},
		{"symbolic true", "five-hour-percent &lt; 50", "AB"},
		{"unresolvable metric hides", "seven-day-percent gt 0", "AB"}, // 79 > 0
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RenderDocumentString(richSnapshot(), parseDoc(t, fmt.Sprintf(tmpl, c.when)), opts)
			if got != c.want {
				t.Errorf("when %q: got %q want %q", c.when, got, c.want)
			}
		})
	}

	// A missing metric (five-hour nil) hides the node.
	snap := richSnapshot()
	snap.Account.FiveHour = nil
	got := RenderDocumentString(snap, parseDoc(t, fmt.Sprintf(tmpl, "five-hour-percent lt 50")), opts)
	if got != "A" {
		t.Errorf("unresolvable metric should hide node: got %q want %q", got, "A")
	}
}

func TestRenderDocument_GitDirty(t *testing.T) {
	opts := Options{Width: 120, Now: fixedNow}
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <line><field name="git-branch"/><text when="git-dirty eq true" prefix=" ">*</text></line>
  </layout>
</statusloom>`
	snap := fullSnapshot()
	snap.Repository.Dirty = true
	if got := RenderDocumentString(snap, parseDoc(t, src), opts); got != "main *" {
		t.Errorf("git-dirty true: got %q want %q", got, "main *")
	}
	snap.Repository.Dirty = false
	if got := RenderDocumentString(snap, parseDoc(t, src), opts); got != "main" {
		t.Errorf("git-dirty false: got %q want %q", got, "main")
	}
}

func TestRenderDocument_NewBooleanMetricConditions(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none"><layout name="x" active="true"><line><field name="thinking-enabled" when="self eq true"/><text when="git-clean eq false"> dirty</text><text when="exceeds-200k eq false"> under-200k</text></line></layout></statusloom>`
	snap := richSnapshot()
	got := RenderDocumentString(snap, parseDoc(t, src), Options{})
	if got != "true dirty under-200k" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderDocument_ColorRule(t *testing.T) {
	opts := Options{Width: 120, Now: fixedNow}
	// five-hour-usage self metric = five-hour-percent = 27 -> matches
	// "self ge 20" (second rule), not "self ge 80".
	src := `<statusloom version="1" tool="claude-code" color-level="ansi16">
  <layout name="a" active="true">
    <line>
      <field name="five-hour-usage" color="green">
        <color-rule when="self ge 80" color="red"/>
        <color-rule when="self ge 20" color="yellow"/>
      </field>
    </line>
  </layout>
</statusloom>`
	got := RenderDocumentString(richSnapshot(), parseDoc(t, src), opts)
	if !strings.Contains(got, "\x1b[33m") { // yellow
		t.Errorf("color-rule first-match should be yellow: %q", got)
	}
	if strings.Contains(got, "\x1b[31m") || strings.Contains(got, "\x1b[32m") {
		t.Errorf("should not be red or green fallback: %q", got)
	}

	// No rule matches (percent 27, both rules require >=80/>=90) -> fall back
	// to base green.
	src2 := strings.ReplaceAll(src, `when="self ge 20" color="yellow"`, `when="self ge 90" color="yellow"`)
	got = RenderDocumentString(richSnapshot(), parseDoc(t, src2), opts)
	if !strings.Contains(got, "\x1b[32m") { // green fallback
		t.Errorf("no match should fall back to green: %q", got)
	}
}

func TestRenderDocument_Hyperlink(t *testing.T) {
	opts := Options{Width: 120, Now: fixedNow}
	src := `<statusloom version="1" tool="claude-code" color-level="ansi16">
  <layout name="a" active="true">
    <line><field name="repo-name" hyperlink="true"/></line>
  </layout>
</statusloom>`
	got := RenderDocumentString(richSnapshot(), parseDoc(t, src), opts)
	if !strings.Contains(got, "\x1b]8;;https://github.com/yacchi/statusloom\x07") {
		t.Errorf("expected OSC 8 hyperlink: %q", got)
	}
	if !strings.Contains(got, "\x1b]8;;\x07") {
		t.Errorf("expected OSC 8 terminator: %q", got)
	}
}

func TestRenderDocument_Raw(t *testing.T) {
	opts := Options{Width: 120, Now: fixedNow}
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <line><field name="context-length" raw="true"/></line>
  </layout>
</statusloom>`
	got := renderDocStr(t, src, fullSnapshot(), opts)
	if got != "64000" {
		t.Errorf("raw context-length: got %q want %q", got, "64000")
	}
	// Non-raw renders thousands separators.
	src = strings.ReplaceAll(src, ` raw="true"`, "")
	got = renderDocStr(t, src, fullSnapshot(), opts)
	if got != "64,000" {
		t.Errorf("non-raw context-length: got %q want %q", got, "64,000")
	}
}

func TestRenderDocument_Flex(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <line><field name="model"/><flex size="full-minus-40"/><field name="git-branch"/></line>
  </layout>
</statusloom>`
	got := renderDocStr(t, src, fullSnapshot(), Options{Width: 80, Now: fixedNow})
	if w := visibleWidth(got); w != 40 {
		t.Errorf("flex full-minus-40 of 80 -> width %d, want 40: %q", w, got)
	}

	// Terminal width unknown -> each flex is a single space.
	got = renderDocStr(t, src, fullSnapshot(), Options{Width: 0, Now: fixedNow})
	if want := "Opus main"; got != want {
		t.Errorf("flex fallback (width unknown): got %q want %q", got, want)
	}
}

func TestRenderDocument_SeparatorCollapsing(t *testing.T) {
	opts := Options{Width: 120, Now: fixedNow}
	// Leading/trailing/doubled separators collapse; only the interior one
	// between two visible fields survives.
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <line>
      <text role="separator" padding="1">|</text>
      <field name="model"/>
      <text role="separator" padding="1">|</text>
      <text role="separator" padding="1">|</text>
      <field name="git-branch"/>
      <text role="separator" padding="1">|</text>
    </line>
  </layout>
</statusloom>`
	got := renderDocStr(t, src, fullSnapshot(), opts)
	if want := "Opus | main"; got != want {
		t.Errorf("collapsing: got %q want %q", got, want)
	}
}

func TestRenderDocument_Compact(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none" compact-threshold="60">
  <layout name="a" active="true">
    <line>
      <field name="context-length"/>
      <text role="separator" padding="1">|</text>
      <field name="context-length" raw="true"/>
    </line>
  </layout>
</statusloom>`
	// Width 40 < 60 -> compact: context-length "64.0k", separator drops
	// padding to "|"; raw stays 64000.
	got := renderDocStr(t, src, fullSnapshot(), Options{Width: 40, Now: fixedNow})
	if want := "64.0k|64000"; got != want {
		t.Errorf("compact: got %q want %q", got, want)
	}
	// Wide terminal -> no compaction.
	got = renderDocStr(t, src, fullSnapshot(), Options{Width: 120, Now: fixedNow})
	if want := "64,000 | 64000"; got != want {
		t.Errorf("non-compact: got %q want %q", got, want)
	}
}

func TestRenderDocument_OmitAndFallback(t *testing.T) {
	opts := Options{Width: 120, Now: fixedNow}
	// A line whose only field has no data is omitted; with two lines the
	// remaining populated one still renders.
	src := `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <line><field name="session-name"/></line>
    <line><field name="model"/></line>
  </layout>
</statusloom>`
	snap := fullSnapshot() // no session name
	lines := RenderDocument(snap, parseDoc(t, src), opts)
	if len(lines) != 2 {
		t.Fatalf("want 2 doc lines, got %d", len(lines))
	}
	if !lines[0].Omitted {
		t.Errorf("empty first line should be omitted")
	}
	if lines[1].Omitted {
		t.Errorf("populated second line should not be omitted")
	}
	if got := RenderDocumentString(snap, parseDoc(t, src), opts); got != "Opus" {
		t.Errorf("string output should drop omitted line: got %q", got)
	}

	// Every line omitted -> fallback (model + tool-version).
	src = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="a" active="true">
    <line><field name="session-name"/></line>
  </layout>
</statusloom>`
	if got := RenderDocumentString(snap, parseDoc(t, src), opts); got != "Opus | v2.1.153" {
		t.Errorf("all-omitted fallback: got %q want %q", got, "Opus | v2.1.153")
	}
}

// ---- min-width / align (markup.md "min-width"/"align") ----

// TestRenderDocument_FieldMinWidthAlign covers the two align directions and
// the "min-width never truncates" rule: a shorter value is padded to the
// requested width, a value already at/over the width is untouched, and
// padding always lands on the field's own content, outside prefix/suffix.
func TestRenderDocument_FieldMinWidthAlign(t *testing.T) {
	snap := fullSnapshot() // Session.Model.DisplayName == "Opus" (4 columns)

	cases := []struct {
		name string
		attr string
		want string
	}{
		{"left align (default), pads on the right", `min-width="8"`, "[Opus    ]"},
		{"explicit align=left, pads on the right", `min-width="8" align="left"`, "[Opus    ]"},
		{"align=right pads on the left", `min-width="8" align="right"`, "[    Opus]"},
		{"value at width is unchanged", `min-width="4"`, "[Opus]"},
		{"value over width is not truncated", `min-width="2"`, "[Opus]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := `<statusloom version="1" tool="claude-code" color-level="none">` +
				`<layout name="a" active="true"><line>` +
				`<field name="model" prefix="[" suffix="]" ` + c.attr + `/>` +
				`</line></layout></statusloom>`
			if got := renderDocStr(t, src, snap, Options{Now: fixedNow}); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestRenderDocument_FieldMinWidthNoop confirms a field with no min-width
// attribute is completely unaffected (nil MinWidth is a no-op, not
// min-width="0").
func TestRenderDocument_FieldMinWidthNoop(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="none">` +
		`<layout name="a" active="true"><line><field name="model" prefix="[" suffix="]"/></line></layout></statusloom>`
	if got := renderDocStr(t, src, fullSnapshot(), Options{Now: fixedNow}); got != "[Opus]" {
		t.Errorf("got %q, want %q", got, "[Opus]")
	}
}

// TestRenderDocument_FieldMinWidthEastAsianWidth confirms min-width padding
// is computed with the East Asian Width-aware visibleWidth (markup.md "表示
// 幅計算"): a full-width (2-column-per-rune) Japanese task-description is
// padded fewer *characters* than an equal-length-in-runes ASCII value, but
// both reach the same *column* width, so right-hand fields following them
// (via a subsequent right-aligned field) line up.
func TestRenderDocument_FieldMinWidthEastAsianWidth(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code-subagent" color-level="none">` +
		`<layout name="a" active="true"><line>` +
		`<field name="task-description" min-width="12"/>` +
		`<field name="task-model" prefix="|"/>` +
		`</line></layout></statusloom>`

	// "レビュー中" is 5 runes, each a 2-column Hiragana/Katakana/Kanji glyph
	// -> 10 display columns -> needs 2 more spaces to reach min-width=12.
	jaSnap := schema.StatusSnapshot{
		Tool:     schema.ToolSnapshot{ID: schema.ToolClaudeCodeSubagent},
		Subagent: &schema.SubagentSnapshot{Description: "レビュー中", ModelDisplay: "Opus"},
	}
	if got, want := renderDocStr(t, src, jaSnap, Options{Now: fixedNow}), "レビュー中  |Opus"; got != want {
		t.Errorf("Japanese description: got %q, want %q", got, want)
	}

	// "review PR" is 9 ASCII columns -> needs 3 more spaces to reach 12.
	enSnap := schema.StatusSnapshot{
		Tool:     schema.ToolSnapshot{ID: schema.ToolClaudeCodeSubagent},
		Subagent: &schema.SubagentSnapshot{Description: "review PR", ModelDisplay: "Opus"},
	}
	if got, want := renderDocStr(t, src, enSnap, Options{Now: fixedNow}), "review PR   |Opus"; got != want {
		t.Errorf("ASCII description: got %q, want %q", got, want)
	}
}

// ---- Golden tests (DSL pipeline; doc_ prefix, byte-compared) ----

func TestRenderDocument_GoldenFull(t *testing.T) {
	got := renderDocStr(t, renderingExampleAnsi, richSnapshot(), Options{Width: 120, Now: fixedNow})
	compareGolden(t, "doc_rendering", []string{got})
}

func TestRenderDocument_GoldenStyles(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="truecolor">
  <layout name="a" active="true">
    <line>
      <text bold="true" underline="true">Head</text>
      <text role="separator" padding="1">|</text>
      <field name="git-branch" color="#00 ff00" background="ansi256:52" italic="true"/>
    </line>
  </layout>
</statusloom>`
	// (deliberately keep colors truecolor to exercise the full SGR builder)
	src = strings.ReplaceAll(src, "#00 ff00", "#00ff00")
	got := renderDocStr(t, src, fullSnapshot(), Options{Width: 120, Now: fixedNow})
	compareGolden(t, "doc_styles", []string{got})
}

const renderingExampleAnsi = `<statusloom version="1" tool="claude-code" color-level="ansi16">
  <layout name="default" active="true">
    <line>
      <text>Model: </text>
      <field name="model" color="cyan" bold="true"/>
      <span optional="thinking-effort" prefix=" (" suffix=")" color="bright-black">
        <field name="thinking-effort" color="yellow"/>
      </span>
      <text role="separator" padding="1">|</text>
      <text>Context: </text>
      <field name="context-percentage-usable" format="percent" precision="0"/>
    </line>
  </layout>
</statusloom>`
