package dsl

// This file implements the canonical serializer (markup.md "serializerは
// 自前実装"): it regenerates a Document into a normalized XML string with a
// fixed per-type attribute order and self-closing childless elements, which
// encoding/xml's Encoder cannot do. It is the foundation for the Phase-3
// minimal-diff save and `statusloom fmt`; this phase only implements the
// whole-document canonical form (element source-slice reuse is Phase 3).

import (
	"sort"
	"strconv"
	"strings"
)

// Serialize regenerates doc into its canonical XML form. The output uses
// two-space indentation, one element per line (except <text>, whose content
// stays on a single line to preserve significant whitespace), a fixed
// attribute order per element type, self-closing tags for childless
// field/flex/git/color-rule elements, and preserved comments. It returns ""
// for a nil document or a document with no root.
//
// Comments and (for the root and each layout) their sibling elements are
// re-interleaved by source-range start offset so their relative position is
// preserved; nodes with a zero range (hand-built ASTs) keep insertion order
// via a stable sort.
func Serialize(doc *Document) string {
	if doc == nil || doc.Root == nil {
		return ""
	}
	var b strings.Builder
	serializeStatusloom(&b, doc.Root)
	return b.String()
}

// indentStr returns depth levels of two-space indentation.
func indentStr(depth int) string {
	return strings.Repeat("  ", depth)
}

// escapeAttr escapes a value for use inside a double-quoted attribute:
// & < " become entities (> and ' are left as-is). This also naturally
// encodes the symbolic when-expression forms that are illegal raw in XML
// attribute text (`<`, `&&`).
func escapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", "\"", "&quot;")
	return r.Replace(s)
}

// escapeText escapes character data: only & and < must be encoded.
func escapeText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;")
	return r.Replace(s)
}

func writeAttr(b *strings.Builder, name, val string) {
	b.WriteString(" ")
	b.WriteString(name)
	b.WriteString("=\"")
	b.WriteString(escapeAttr(val))
	b.WriteString("\"")
}

func writeIntAttr(b *strings.Builder, name string, v *int) {
	if v != nil {
		writeAttr(b, name, strconv.Itoa(*v))
	}
}

func writeBoolAttr(b *strings.Builder, name string, v *bool) {
	if v != nil {
		if *v {
			writeAttr(b, name, "true")
		} else {
			writeAttr(b, name, "false")
		}
	}
}

// writeTrueAttr writes name="true" only when v is true. It is used for the
// non-inheriting plain bools raw and hyperlink, where false is semantically
// identical to absent and is therefore dropped (markup.md "raw/hyperlinkは
// true時のみ").
func writeTrueAttr(b *strings.Builder, name string, v bool) {
	if v {
		writeAttr(b, name, "true")
	}
}

// writeStyleAttrs writes the character-decoration common attributes in
// canonical order: color, background, bold, dim, italic, underline,
// strikethrough.
//
// Decoration bools are *bool because they participate in style inheritance
// (markup.md "文字装飾"): nil means "inherit from the parent" while an
// explicit false OVERRIDES an inherited true (see mergeStyle in
// internal/render/doc_style.go). The serializer therefore emits the
// attribute whenever the pointer is non-nil — including bold="false" —
// since dropping an explicit false would change meaning.
func writeStyleAttrs(b *strings.Builder, s Style) {
	if s.Color != "" {
		writeAttr(b, "color", s.Color)
	}
	if s.Background != "" {
		writeAttr(b, "background", s.Background)
	}
	writeBoolAttr(b, "bold", s.Bold)
	writeBoolAttr(b, "dim", s.Dim)
	writeBoolAttr(b, "italic", s.Italic)
	writeBoolAttr(b, "underline", s.Underline)
	writeBoolAttr(b, "strikethrough", s.Strikethrough)
}

// writePadding writes padding attributes: a single padding="N" when both
// sides are set to the same value, otherwise the individual padding-left /
// padding-right that are present.
func writePadding(b *strings.Builder, box Box) {
	pl, pr := box.PaddingLeft, box.PaddingRight
	if pl != nil && pr != nil && *pl == *pr {
		writeAttr(b, "padding", strconv.Itoa(*pl))
		return
	}
	if pl != nil {
		writeAttr(b, "padding-left", strconv.Itoa(*pl))
	}
	if pr != nil {
		writeAttr(b, "padding-right", strconv.Itoa(*pr))
	}
}

// writeCommon writes the shared common attributes in canonical order:
// decorations, padding, prefix, suffix, optional, when. (ColorRules are
// emitted as child elements, not attributes.)
func writeCommon(b *strings.Builder, c CommonAttributes) {
	writeStyleAttrs(b, c.Style)
	writePadding(b, c.Box)
	if c.Prefix != "" {
		writeAttr(b, "prefix", c.Prefix)
	}
	if c.Suffix != "" {
		writeAttr(b, "suffix", c.Suffix)
	}
	if c.Optional != "" {
		writeAttr(b, "optional", c.Optional)
	}
	if c.When != "" {
		writeAttr(b, "when", c.When)
	}
}

// serItem pairs an element's source-start offset with its emit closure so a
// mixed set of siblings (elements + comments) can be stably re-interleaved
// by position.
type serItem struct {
	start int
	emit  func()
}

func emitSorted(items []serItem) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].start < items[j].start })
	for _, it := range items {
		it.emit()
	}
}

// writeStatusloomOpenTag writes the <statusloom ...> open tag (no trailing
// newline) with the canonical attribute order. Shared by the canonical and
// minimal-diff serializers so the reconstructed open tag is identical.
func writeStatusloomOpenTag(b *strings.Builder, n *StatusloomNode) {
	b.WriteString("<statusloom")
	if n.Version != "" {
		writeAttr(b, "version", n.Version)
	}
	if n.Tool != "" {
		writeAttr(b, "tool", n.Tool)
	}
	if n.Settings.ColorLevel != "" {
		writeAttr(b, "color-level", n.Settings.ColorLevel)
	}
	if n.Settings.OutputStyle != "" {
		writeAttr(b, "output-style", n.Settings.OutputStyle)
	}
	writeIntAttr(b, "compact-threshold", n.Settings.CompactThreshold)
	if n.Settings.ContextPercentageMode != "" {
		writeAttr(b, "context-percentage-mode", n.Settings.ContextPercentageMode)
	}
	writeIntAttr(b, "context-reserve-tokens", n.Settings.ContextReserveTokens)
	b.WriteString(">")
}

func serializeStatusloom(b *strings.Builder, n *StatusloomNode) {
	writeStatusloomOpenTag(b, n)
	b.WriteString("\n")

	var items []serItem
	if n.Git != nil {
		g := n.Git
		items = append(items, serItem{g.Meta.SourceRange.Start, func() { serializeGit(b, 1, g) }})
	}
	for _, l := range n.Layouts {
		l := l
		items = append(items, serItem{l.Meta.SourceRange.Start, func() { serializeLayout(b, 1, l) }})
	}
	for _, c := range n.Comments {
		c := c
		items = append(items, serItem{c.Meta.SourceRange.Start, func() { serializeComment(b, 1, c) }})
	}
	emitSorted(items)

	b.WriteString("</statusloom>\n")
}

func serializeGit(b *strings.Builder, depth int, g *GitSettings) {
	b.WriteString(indentStr(depth))
	b.WriteString("<git")
	writeIntAttr(b, "cache-ttl-ms", g.CacheTTLMS)
	writeIntAttr(b, "timeout-ms", g.TimeoutMS)
	writeBoolAttr(b, "include-untracked", g.IncludeUntracked)
	writeBoolAttr(b, "collect-numstat", g.CollectNumstat)
	b.WriteString("/>\n")
}

// writeLayoutOpenTag writes the <layout ...> open tag (no indent, no trailing
// newline) with the canonical attribute order.
func writeLayoutOpenTag(b *strings.Builder, n *LayoutNode) {
	b.WriteString("<layout")
	if n.Name != "" {
		writeAttr(b, "name", n.Name)
	}
	writeBoolAttr(b, "active", n.Active)
	b.WriteString(">")
}

func serializeLayout(b *strings.Builder, depth int, n *LayoutNode) {
	ind := indentStr(depth)
	b.WriteString(ind)
	writeLayoutOpenTag(b, n)
	b.WriteString("\n")

	var items []serItem
	for _, l := range n.Lines {
		l := l
		items = append(items, serItem{l.Meta.SourceRange.Start, func() { serializeLine(b, depth+1, l) }})
	}
	for _, c := range n.Comments {
		c := c
		items = append(items, serItem{c.Meta.SourceRange.Start, func() { serializeComment(b, depth+1, c) }})
	}
	emitSorted(items)

	b.WriteString(ind)
	b.WriteString("</layout>\n")
}

// writeLineOpenTag writes "<line ...>" up to but not including the closing
// ">" (no indent), with the canonical common-attribute order.
func writeLineOpenTag(b *strings.Builder, n *LineNode) {
	b.WriteString("<line")
	writeCommon(b, n.Common)
}

func serializeLine(b *strings.Builder, depth int, n *LineNode) {
	ind := indentStr(depth)
	b.WriteString(ind)
	writeLineOpenTag(b, n)
	// <line> never carries color-rules (the parser rejects them there), so
	// its body is exactly its children.
	if len(n.Children) == 0 {
		b.WriteString("></line>\n")
		return
	}
	b.WriteString(">\n")
	serializeChildren(b, depth+1, n.Children)
	b.WriteString(ind)
	b.WriteString("</line>\n")
}

// serializeChildren emits a mixed-content child list (elements, raw text,
// comments) at the given depth, one node per line.
func serializeChildren(b *strings.Builder, depth int, children []Node) {
	for _, child := range children {
		serializeNode(b, depth, child)
	}
}

func serializeNode(b *strings.Builder, depth int, node Node) {
	switch n := node.(type) {
	case *SpanNode:
		serializeSpan(b, depth, n)
	case *TextNode:
		serializeText(b, depth, n)
	case *FieldNode:
		serializeField(b, depth, n)
	case *FlexNode:
		serializeFlex(b, depth, n)
	case *RawTextNode:
		serializeRawText(b, depth, n)
	case *CommentNode:
		serializeComment(b, depth, n)
	}
}

// writeSpanOpenTag writes "<span ...>" up to but not including the closing
// ">" (no indent), with the canonical common-attribute order.
func writeSpanOpenTag(b *strings.Builder, n *SpanNode) {
	b.WriteString("<span")
	writeCommon(b, n.Common)
}

func serializeSpan(b *strings.Builder, depth int, n *SpanNode) {
	ind := indentStr(depth)
	b.WriteString(ind)
	writeSpanOpenTag(b, n)
	if len(n.Children) == 0 && len(n.Common.ColorRules) == 0 {
		b.WriteString("></span>\n")
		return
	}
	b.WriteString(">\n")
	// Interleave element children and color-rule children by position so a
	// color-rule keeps its place relative to siblings.
	var items []serItem
	for _, child := range n.Children {
		child := child
		items = append(items, serItem{nodeStart(child), func() { serializeNode(b, depth+1, child) }})
	}
	for _, cr := range n.Common.ColorRules {
		cr := cr
		items = append(items, serItem{cr.Meta.SourceRange.Start, func() { serializeColorRule(b, depth+1, cr) }})
	}
	emitSorted(items)
	b.WriteString(ind)
	b.WriteString("</span>\n")
}

func serializeText(b *strings.Builder, depth int, n *TextNode) {
	b.WriteString(indentStr(depth))
	b.WriteString("<text")
	if n.Role != "" {
		writeAttr(b, "role", n.Role)
	}
	writeCommon(b, n.Common)
	b.WriteString(">")
	b.WriteString(escapeText(n.Value))
	// Color-rules on <text> stay on the same line so the element's
	// significant whitespace (its Value) round-trips unchanged.
	for _, cr := range n.Common.ColorRules {
		serializeColorRuleInline(b, cr)
	}
	b.WriteString("</text>\n")
}

func serializeField(b *strings.Builder, depth int, n *FieldNode) {
	ind := indentStr(depth)
	b.WriteString(ind)
	b.WriteString("<field")
	if n.Name != "" {
		writeAttr(b, "name", n.Name)
	}
	if n.Formatter.Name != "" {
		writeAttr(b, "format", n.Formatter.Name)
	}
	if n.Formatter.Precision != "" {
		writeAttr(b, "precision", n.Formatter.Precision)
	}
	if n.Formatter.Currency != "" {
		writeAttr(b, "currency", n.Formatter.Currency)
	}
	writeTrueAttr(b, "raw", n.Raw)
	writeTrueAttr(b, "hyperlink", n.Hyperlink)
	writeIntAttr(b, "min-width", n.MinWidth)
	if n.Align != "" {
		writeAttr(b, "align", n.Align)
	}
	writeCommon(b, n.Common)
	if len(n.Common.ColorRules) == 0 {
		b.WriteString("/>\n")
		return
	}
	b.WriteString(">\n")
	for _, cr := range n.Common.ColorRules {
		serializeColorRule(b, depth+1, cr)
	}
	b.WriteString(ind)
	b.WriteString("</field>\n")
}

func serializeFlex(b *strings.Builder, depth int, n *FlexNode) {
	b.WriteString(indentStr(depth))
	b.WriteString("<flex")
	if n.Size != "" {
		writeAttr(b, "size", n.Size)
	}
	b.WriteString("/>\n")
}

func serializeColorRule(b *strings.Builder, depth int, cr ColorRule) {
	b.WriteString(indentStr(depth))
	serializeColorRuleInline(b, cr)
	b.WriteString("\n")
}

func serializeColorRuleInline(b *strings.Builder, cr ColorRule) {
	b.WriteString("<color-rule")
	if cr.When != "" {
		writeAttr(b, "when", cr.When)
	}
	if cr.Color != "" {
		writeAttr(b, "color", cr.Color)
	}
	b.WriteString("/>")
}

func serializeRawText(b *strings.Builder, depth int, n *RawTextNode) {
	b.WriteString(indentStr(depth))
	b.WriteString(escapeText(n.RenderText))
	b.WriteString("\n")
}

func serializeComment(b *strings.Builder, depth int, n *CommentNode) {
	b.WriteString(indentStr(depth))
	b.WriteString("<!--")
	b.WriteString(n.Text)
	b.WriteString("-->\n")
}

// nodeStart returns a node's source-range start offset for stable
// re-interleaving; unknown node kinds sort first (0).
func nodeStart(node Node) int {
	switch n := node.(type) {
	case *SpanNode:
		return n.Meta.SourceRange.Start
	case *TextNode:
		return n.Meta.SourceRange.Start
	case *FieldNode:
		return n.Meta.SourceRange.Start
	case *FlexNode:
		return n.Meta.SourceRange.Start
	case *RawTextNode:
		return n.Meta.SourceRange.Start
	case *CommentNode:
		return n.Meta.SourceRange.Start
	default:
		return 0
	}
}
