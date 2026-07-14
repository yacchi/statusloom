package dsl

import (
	"strings"
	"testing"
)

// mustParse parses src, fails the test on any fatal (doc==nil) error, and
// returns the document plus its diagnostics.
func mustParse(t *testing.T, src string) (*Document, []Diagnostic) {
	t.Helper()
	doc, diags := Parse(src)
	if doc == nil {
		t.Fatalf("Parse returned nil doc (fatal); diags=%v", diags)
	}
	return doc, diags
}

// parseClean parses src and fails if there is any diagnostic at all.
func parseClean(t *testing.T, src string) *Document {
	t.Helper()
	doc, diags := Parse(src)
	if doc == nil {
		t.Fatalf("Parse returned nil doc; diags=%v", diags)
	}
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	return doc
}

// wrap embeds line children inside a minimal valid document skeleton.
func wrap(inner string) string {
	return `<statusloom version="1" tool="claude-code">` +
		`<layout name="default" active="true"><line>` + inner +
		`</line></layout></statusloom>`
}

func onlyLine(t *testing.T, doc *Document) *LineNode {
	t.Helper()
	if doc.Root == nil || len(doc.Root.Layouts) != 1 || len(doc.Root.Layouts[0].Lines) != 1 {
		t.Fatalf("expected exactly one layout with one line")
	}
	return doc.Root.Layouts[0].Lines[0]
}

func hasErrorContaining(diags []Diagnostic, substr string) bool {
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Message, substr) {
			return true
		}
	}
	return false
}

func TestParseRootAttributes(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="ansi16" compact-threshold="60" context-percentage-mode="usable" context-reserve-tokens="0">` +
		`<layout name="default" active="true"><line><field name="model"/></line></layout></statusloom>`
	doc := parseClean(t, src)
	r := doc.Root
	if r.Version != "1" || r.Tool != "claude-code" {
		t.Fatalf("version/tool: %q %q", r.Version, r.Tool)
	}
	if r.Settings.ColorLevel != "ansi16" || r.Settings.ContextPercentageMode != "usable" {
		t.Fatalf("settings enums: %+v", r.Settings)
	}
	if r.Settings.CompactThreshold == nil || *r.Settings.CompactThreshold != 60 {
		t.Fatalf("compact-threshold: %v", r.Settings.CompactThreshold)
	}
	if r.Settings.ContextReserveTokens == nil || *r.Settings.ContextReserveTokens != 0 {
		t.Fatalf("context-reserve-tokens: %v", r.Settings.ContextReserveTokens)
	}
}

func TestParseMixedContentAndField(t *testing.T) {
	doc := parseClean(t, wrap(`Model: <field name="model"/>`))
	line := onlyLine(t, doc)
	if len(line.Children) != 2 {
		t.Fatalf("want 2 children, got %d: %#v", len(line.Children), line.Children)
	}
	raw, ok := line.Children[0].(*RawTextNode)
	if !ok {
		t.Fatalf("child 0 is %T, want *RawTextNode", line.Children[0])
	}
	if raw.RenderText != "Model:" {
		t.Fatalf("RenderText = %q, want %q", raw.RenderText, "Model:")
	}
	f, ok := line.Children[1].(*FieldNode)
	if !ok || f.Name != "model" {
		t.Fatalf("child 1 = %#v", line.Children[1])
	}
}

func TestParseTextElementPreservesWhitespace(t *testing.T) {
	doc := parseClean(t, wrap(`<text>Model: </text><field name="model"/>`))
	line := onlyLine(t, doc)
	txt, ok := line.Children[0].(*TextNode)
	if !ok {
		t.Fatalf("child 0 is %T", line.Children[0])
	}
	if txt.Value != "Model: " {
		t.Fatalf("Value = %q, want %q", txt.Value, "Model: ")
	}
}

func TestParseTextSeparatorRoleAndPadding(t *testing.T) {
	doc := parseClean(t, wrap(`<text role="separator" padding="1">|</text>`))
	line := onlyLine(t, doc)
	txt := line.Children[0].(*TextNode)
	if txt.Role != "separator" {
		t.Fatalf("Role = %q", txt.Role)
	}
	if txt.Value != "|" {
		t.Fatalf("Value = %q", txt.Value)
	}
	if txt.Common.Box.PaddingLeft == nil || *txt.Common.Box.PaddingLeft != 1 ||
		txt.Common.Box.PaddingRight == nil || *txt.Common.Box.PaddingRight != 1 {
		t.Fatalf("padding not expanded to both sides: %+v", txt.Common.Box)
	}
}

func TestParseOutputStyle(t *testing.T) {
	doc := parseClean(t, strings.Replace(wrap(`<text role="separator"/>`), `tool="claude-code"`, `tool="claude-code" output-style="powerline"`, 1))
	if doc.Root.Settings.OutputStyle != "powerline" {
		t.Fatalf("output style = %q", doc.Root.Settings.OutputStyle)
	}
}

func TestParsePaddingIndividualOverridesShorthand(t *testing.T) {
	doc := parseClean(t, wrap(`<text padding="1" padding-left="4">x</text>`))
	box := onlyLine(t, doc).Children[0].(*TextNode).Common.Box
	if box.PaddingLeft == nil || *box.PaddingLeft != 4 {
		t.Fatalf("PaddingLeft = %v, want 4", box.PaddingLeft)
	}
	if box.PaddingRight == nil || *box.PaddingRight != 1 {
		t.Fatalf("PaddingRight = %v, want 1 (from shorthand)", box.PaddingRight)
	}
}

func TestParseSpanNestingAndStyleAndPrefixSuffix(t *testing.T) {
	doc := parseClean(t, wrap(
		`<span optional="thinking-effort" prefix=" (" suffix=")" color="bright-black">`+
			`<field name="thinking-effort" color="yellow"/></span>`))
	span := onlyLine(t, doc).Children[0].(*SpanNode)
	if span.Common.Optional != "thinking-effort" {
		t.Fatalf("optional = %q", span.Common.Optional)
	}
	if span.Common.Prefix != " (" || span.Common.Suffix != ")" {
		t.Fatalf("prefix/suffix = %q/%q", span.Common.Prefix, span.Common.Suffix)
	}
	if span.Common.Style.Color != "bright-black" {
		t.Fatalf("span color = %q", span.Common.Style.Color)
	}
	if len(span.Children) != 1 {
		t.Fatalf("span children = %d", len(span.Children))
	}
	f := span.Children[0].(*FieldNode)
	if f.Common.Style.Color != "yellow" {
		t.Fatalf("field color = %q", f.Common.Style.Color)
	}
}

func TestParseFieldFormatterAndFlags(t *testing.T) {
	doc := parseClean(t, wrap(`<field name="context-length" format="compact-number" precision="1" raw="true"/>`))
	f := onlyLine(t, doc).Children[0].(*FieldNode)
	if f.Formatter.Name != "compact-number" || f.Formatter.Precision != "1" {
		t.Fatalf("formatter = %+v", f.Formatter)
	}
	if !f.Raw {
		t.Fatalf("raw not set")
	}
}

func TestParseFieldStyleBoolAttrs(t *testing.T) {
	doc := parseClean(t, wrap(`<field name="model" bold="true" italic="false"/>`))
	f := onlyLine(t, doc).Children[0].(*FieldNode)
	if f.Common.Style.Bold == nil || !*f.Common.Style.Bold {
		t.Fatalf("bold = %v", f.Common.Style.Bold)
	}
	if f.Common.Style.Italic == nil || *f.Common.Style.Italic {
		t.Fatalf("italic = %v", f.Common.Style.Italic)
	}
	if f.Common.Style.Dim != nil {
		t.Fatalf("dim should be nil (unspecified)")
	}
}

func TestParseFlex(t *testing.T) {
	doc := parseClean(t, wrap(`<field name="model"/><flex/><field name="tool-version"/>`))
	children := onlyLine(t, doc).Children
	flex, ok := children[1].(*FlexNode)
	if !ok {
		t.Fatalf("child 1 is %T", children[1])
	}
	if flex.Size != "" {
		t.Fatalf("default flex size = %q, want empty", flex.Size)
	}

	doc2 := parseClean(t, wrap(`<flex size="full-minus-2"/>`))
	if s := onlyLine(t, doc2).Children[0].(*FlexNode).Size; s != "full-minus-2" {
		t.Fatalf("flex size = %q", s)
	}
}

func TestParseColorRulesOnField(t *testing.T) {
	doc := parseClean(t, wrap(
		`<field name="context-percentage" color="green">`+
			`<color-rule when="self ge 90" color="red"/>`+
			`<color-rule when="self ge 70" color="yellow"/>`+
			`</field>`))
	f := onlyLine(t, doc).Children[0].(*FieldNode)
	if len(f.Common.ColorRules) != 2 {
		t.Fatalf("color rules = %d", len(f.Common.ColorRules))
	}
	if f.Common.ColorRules[0].When != "self ge 90" || f.Common.ColorRules[0].Color != "red" {
		t.Fatalf("rule 0 = %+v", f.Common.ColorRules[0])
	}
}

func TestParseComments(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code">` +
		`<!-- root comment -->` +
		`<layout name="default" active="true">` +
		`<!-- layout comment -->` +
		`<line><!-- line comment --><field name="model"/></line>` +
		`</layout></statusloom>`
	doc := parseClean(t, src)
	if len(doc.Root.Comments) != 1 || doc.Root.Comments[0].Text != " root comment " {
		t.Fatalf("root comments = %#v", doc.Root.Comments)
	}
	layout := doc.Root.Layouts[0]
	if len(layout.Comments) != 1 || layout.Comments[0].Text != " layout comment " {
		t.Fatalf("layout comments = %#v", layout.Comments)
	}
	line := layout.Lines[0]
	if _, ok := line.Children[0].(*CommentNode); !ok {
		t.Fatalf("line child 0 is %T, want *CommentNode", line.Children[0])
	}
}

func TestParseWhenWordAndSymbolForms(t *testing.T) {
	// Word form and escaped symbolic form both survive parsing as raw
	// attribute text (symbol form is XML-decoded).
	doc := parseClean(t, wrap(`<text when="git-dirty eq true">A</text><text when="context-percent &gt;= 80">B</text>`))
	children := onlyLine(t, doc).Children
	if got := children[0].(*TextNode).Common.When; got != "git-dirty eq true" {
		t.Fatalf("when 0 = %q", got)
	}
	if got := children[1].(*TextNode).Common.When; got != "context-percent >= 80" {
		t.Fatalf("when 1 = %q (entity should be decoded)", got)
	}
}

func TestParseGitElement(t *testing.T) {
	// Omitted git -> nil.
	doc := parseClean(t, `<statusloom version="1" tool="claude-code"><layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`)
	if doc.Root.Git != nil {
		t.Fatalf("expected nil Git when omitted")
	}

	// Present git -> attributes parsed.
	src := `<statusloom version="1" tool="claude-code">` +
		`<git cache-ttl-ms="3000" timeout-ms="200" include-untracked="true" collect-numstat="false"/>` +
		`<layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`
	doc2 := parseClean(t, src)
	g := doc2.Root.Git
	if g == nil {
		t.Fatalf("expected Git")
	}
	if g.CacheTTLMS == nil || *g.CacheTTLMS != 3000 || g.TimeoutMS == nil || *g.TimeoutMS != 200 {
		t.Fatalf("git ints = %+v", g)
	}
	if g.IncludeUntracked == nil || !*g.IncludeUntracked || g.CollectNumstat == nil || *g.CollectNumstat {
		t.Fatalf("git bools = %+v", g)
	}
}

func TestParseGitDuplicateIsError(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code">` +
		`<git timeout-ms="100"/><git timeout-ms="200"/>` +
		`<layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`
	_, diags := mustParse(t, src)
	if !hasErrorContaining(diags, "at most one <git>") {
		t.Fatalf("want duplicate-git error, got %v", diags)
	}
}

func TestParseGitInvalidAttrValues(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code">` +
		`<git timeout-ms="-5" include-untracked="yes"/>` +
		`<layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`
	_, diags := mustParse(t, src)
	if !hasErrorContaining(diags, "non-negative integer") {
		t.Fatalf("want int error, got %v", diags)
	}
	if !hasErrorContaining(diags, "true") {
		t.Fatalf("want bool error, got %v", diags)
	}
}

func TestParseInvalidXMLIsFatal(t *testing.T) {
	doc, diags := Parse(`<statusloom version="1"><layout></statusloom>`)
	if doc != nil {
		t.Fatalf("expected nil doc for malformed XML")
	}
	if !hasErrorContaining(diags, "XML syntax error") {
		t.Fatalf("want syntax error diag, got %v", diags)
	}
}

func TestParseUnknownElement(t *testing.T) {
	_, diags := mustParse(t, wrap(`<blink>x</blink>`))
	if !hasErrorContaining(diags, "unknown element <blink>") {
		t.Fatalf("want unknown-element error, got %v", diags)
	}
}

func TestParseUnknownAttribute(t *testing.T) {
	_, diags := mustParse(t, wrap(`<field name="model" wobble="1"/>`))
	if !hasErrorContaining(diags, `unknown attribute "wobble"`) {
		t.Fatalf("want unknown-attr error, got %v", diags)
	}
}

func TestParseInvalidRoleAtParse(t *testing.T) {
	_, diags := mustParse(t, wrap(`<text role="fancy">x</text>`))
	if !hasErrorContaining(diags, "invalid role") {
		t.Fatalf("want role error, got %v", diags)
	}
}

func TestParseFlexInsideSpanIsInvalidNesting(t *testing.T) {
	_, diags := mustParse(t, wrap(`<span><flex/></span>`))
	if !hasErrorContaining(diags, "<flex> is only allowed directly inside <line>") {
		t.Fatalf("want invalid-nesting error, got %v", diags)
	}
}

func TestParseColorRuleInLineIsInvalidNesting(t *testing.T) {
	_, diags := mustParse(t, wrap(`<color-rule when="git-dirty" color="red"/>`))
	if !hasErrorContaining(diags, "<color-rule> is not allowed here") {
		t.Fatalf("want color-rule nesting error, got %v", diags)
	}
}

func TestParseWrongRootElement(t *testing.T) {
	_, diags := mustParse(t, `<config version="1"><layout name="d" active="true"><line><field name="model"/></line></layout></config>`)
	if !hasErrorContaining(diags, "root element must be <statusloom>") {
		t.Fatalf("want root error, got %v", diags)
	}
}

func TestParseSourceRanges(t *testing.T) {
	src := wrap(`<text>Model: </text><field name="model" color="cyan"/>`)
	doc := parseClean(t, src)
	line := onlyLine(t, doc)

	txt := line.Children[0].(*TextNode)
	if got := txt.Meta.SourceRange.Slice(src); got != `<text>Model: </text>` {
		t.Fatalf("text range slice = %q", got)
	}
	f := line.Children[1].(*FieldNode)
	if got := f.Meta.SourceRange.Slice(src); got != `<field name="model" color="cyan"/>` {
		t.Fatalf("field range slice = %q", got)
	}
	// Whole-document root range covers the entire source.
	if got := doc.Root.Meta.SourceRange.Slice(src); got != src {
		t.Fatalf("root range slice = %q", got)
	}
}

func TestParseXMLDeclarationIgnored(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8"?>` + wrap(`<field name="model"/>`)
	doc := parseClean(t, src)
	f := onlyLine(t, doc).Children[0].(*FieldNode)
	// The field's range should still slice correctly despite the leading
	// processing instruction shifting offsets.
	if got := f.Meta.SourceRange.Slice(src); got != `<field name="model"/>` {
		t.Fatalf("field range slice with xml decl = %q", got)
	}
}
