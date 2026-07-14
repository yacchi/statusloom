package render

// This file implements DSL-document rendering: the evaluation layer that
// turns a dsl.Document's active layout into styled spans and then ANSI, per
// markup.md ("DSL -> AST -> evaluation -> styled spans -> ANSI renderer").
// It reuses the shared layout plumbing in render.go: renderContent for the
// default field text (compact/raw handling), metricValue for when/color-rule
// metrics and formatter inputs, markSeparators + computeFlexWidths for
// collapsing and flex layout, hyperlinkURL for OSC 8 targets, and
// RenderFallback for the all-omitted fallback line.

import (
	"strings"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

// DocLine is one rendered status-line row from a DSL document.
type DocLine struct {
	Omitted  bool
	Segments []DocSegment
}

// DocSegment is one leaf node's rendered result within a DocLine. Node is
// the originating leaf (field/text/flex/raw-text; or the owning span for a
// span's prefix/suffix/padding decoration). The visual editor's preview
// endpoint maps Node to its node ID for click-to-source mapping (see
// webconfig's /api/dsl/preview).
type DocSegment struct {
	Node    dsl.Node // source node ("" ANSI/Text carry no node for the fallback line)
	Text    string   // plain text, no ANSI escapes ("" when !Visible)
	ANSI    string   // styled text incl. escapes ("" when !Visible)
	Visible bool
}

// RenderDocument renders the active layout of a DSL document into per-line,
// per-leaf segments. The result is 1:1 with the layout's lines, including
// omitted ones (a line with no visible content). It does not substitute the
// fallback line; RenderDocumentString does that when every line is omitted.
// A nil document, missing root, or layout-less document yields nil.
func RenderDocument(snap schema.StatusSnapshot, doc *dsl.Document, opts Options) []DocLine {
	if doc == nil || doc.Root == nil {
		return nil
	}
	layout := activeDocLayout(doc.Root)
	if layout == nil {
		return nil
	}
	e := newDocEval(snap, doc, opts)
	out := make([]DocLine, 0, len(layout.Lines))
	for _, ln := range layout.Lines {
		out = append(out, e.renderLineDoc(ln))
	}
	return out
}

// RenderDocumentString renders a DSL document to newline-joined output,
// omitting empty lines. When every line is omitted (or the document has no
// renderable layout) it returns the fallback line (model + tool-version) via
// RenderFallback.
func RenderDocumentString(snap schema.StatusSnapshot, doc *dsl.Document, opts Options) string {
	lines := RenderDocument(snap, doc, opts)
	var out []string
	for _, dl := range lines {
		if dl.Omitted {
			continue
		}
		var b strings.Builder
		for _, s := range dl.Segments {
			b.WriteString(s.ANSI)
		}
		out = append(out, b.String())
	}
	if len(out) == 0 {
		cfg := docToolConfig(doc)
		return RenderFallback(snap, cfg, opts)
	}
	return strings.Join(out, "\n")
}

// activeDocLayout returns the layout to render: the one with active="true",
// or the sole layout when there is exactly one (its active attribute may be
// omitted). Validation guarantees exactly one active layout; at render time
// we defensively fall back to the first layout.
func activeDocLayout(root *dsl.StatusloomNode) *dsl.LayoutNode {
	if len(root.Layouts) == 0 {
		return nil
	}
	for _, l := range root.Layouts {
		if l.Active != nil && *l.Active {
			return l
		}
	}
	return root.Layouts[0]
}

// docToolConfig derives a config.ToolConfig (the input shape renderContent /
// metricValue expect) from the document's tool-level settings, applying the
// built-in defaults for unspecified values (colorLevel ansi16,
// compactThreshold 60, percentageMode usable).
func docToolConfig(doc *dsl.Document) config.ToolConfig {
	cfg := config.ToolConfig{
		ColorLevel:       "ansi16",
		CompactThreshold: 60,
		Context:          config.ContextConfig{PercentageMode: "usable"},
	}
	if doc == nil || doc.Root == nil {
		return cfg
	}
	s := doc.Root.Settings
	if s.ColorLevel != "" {
		cfg.ColorLevel = s.ColorLevel
	}
	if s.CompactThreshold != nil {
		cfg.CompactThreshold = *s.CompactThreshold
	}
	if s.ContextPercentageMode != "" {
		cfg.Context.PercentageMode = s.ContextPercentageMode
	}
	if s.ContextReserveTokens != nil {
		cfg.Context.ReserveTokens = *s.ContextReserveTokens
	}
	return cfg
}

// docEval carries the resolved render context for a single RenderDocument
// call. tool is the document's root tool attribute, used to resolve field
// metadata (self metrics) through the dsl registry — the single source of
// truth for field/metric metadata.
type docEval struct {
	snap           schema.StatusSnapshot
	cfg            config.ToolConfig
	opts           Options
	tool           string
	level          colorLevel
	compact        bool
	separatorStyle string
}

func newDocEval(snap schema.StatusSnapshot, doc *dsl.Document, opts Options) docEval {
	cfg := docToolConfig(doc)
	return docEval{
		snap:    snap,
		cfg:     cfg,
		opts:    opts,
		tool:    doc.Root.Tool,
		level:   parseColorLevel(cfg.ColorLevel),
		compact: cfg.CompactThreshold > 0 && opts.Width > 0 && opts.Width < cfg.CompactThreshold,
		separatorStyle: func() string {
			if doc.Root.Settings.OutputStyle == "powerline" {
				return "powerline"
			}
			return "standard"
		}(),
	}
}

// selfMetric resolves a field's self metric through the dsl registry. ""
// means the field (or an unknown field/tool) exposes no self metric, which
// makes "self" references unresolvable for that node.
func (e *docEval) selfMetric(fieldName string) string {
	fd, ok := dsl.FieldByName(e.tool, fieldName)
	if !ok {
		return ""
	}
	return fd.SelfMetric
}

// flatPiece is one leaf's contribution to a line, before separator
// collapsing and flex sizing. It mirrors the shared `piece` type but carries
// a full docStyle and the originating dsl.Node.
type flatPiece struct {
	node      dsl.Node
	segment   int // top-level line child; nested span content keeps its parent's index
	kind      pieceKind
	plain     string
	style     docStyle
	link      string
	flex      string
	visible   bool // content: plain != ""; flex: true; separator: decided later
	powerline bool
}

// renderLineDoc flattens, collapses, and sizes one <line> into a DocLine.
func (e *docEval) renderLineDoc(line *dsl.LineNode) DocLine {
	pieces := e.flattenLine(line)
	pieces = e.preparePowerlinePieces(pieces)
	e.applyPowerlineSegments(pieces)

	// Reuse the shared separator collapsing and flex-width computation by
	// projecting onto the piece type (only kind/plain/visible/flex matter to
	// those functions).
	ps := make([]piece, len(pieces))
	for i, fp := range pieces {
		ps[i] = piece{kind: fp.kind, plain: fp.plain, visible: fp.visible, flex: fp.flex}
	}
	markSeparators(ps)
	e.resolvePowerlineStyles(pieces, ps)
	flexWidths := computeFlexWidths(ps, e.opts.Width)

	segs := make([]DocSegment, 0, len(pieces))
	hasContent := false
	fi := 0
	for i, fp := range pieces {
		seg := DocSegment{Node: fp.node}
		if ps[i].visible {
			seg.Visible = true
			if fp.kind == pkFlex {
				seg.Text, seg.ANSI = e.renderFlexPiece(pieces, ps, i, flexWidths[fi])
				fi++
			} else {
				seg.Text = fp.plain
				seg.ANSI = stylizeDoc(fp.plain, fp.style, e.level)
				if fp.link != "" {
					seg.ANSI = "\x1b]8;;" + fp.link + "\x07" + seg.ANSI + "\x1b]8;;\x07"
				}
				if fp.kind == pkContent {
					hasContent = true
				}
			}
		}
		segs = append(segs, seg)
	}
	return DocLine{Omitted: !hasContent, Segments: segs}
}

// preparePowerlinePieces removes manual separators and inserts one generated
// transition between adjacent top-level drawable nodes. A span and all of its
// descendants share one segment. Flex nodes split runs and supply their own
// caps, so transitions are not inserted around them.
func (e *docEval) preparePowerlinePieces(pieces []flatPiece) []flatPiece {
	if e.separatorStyle != "powerline" {
		return pieces
	}
	out := make([]flatPiece, 0, len(pieces)*2)
	previousSegment := -1
	for _, p := range pieces {
		if p.kind == pkSeparator {
			continue
		}
		if p.kind == pkFlex {
			out = append(out, p)
			previousSegment = -1
			continue
		}
		if p.kind == pkContent && p.segment >= 0 && p.segment != previousSegment {
			if previousSegment >= 0 {
				out = append(out, flatPiece{
					node: p.node, kind: pkSeparator, plain: "\ue0b0", powerline: true, segment: -1,
				})
			}
			previousSegment = p.segment
		}
		out = append(out, p)
	}
	return out
}

var powerlineTheme = []docStyle{
	{color: "black", background: "cyan"},
	{color: "bright-white", background: "magenta"},
	{color: "black", background: "yellow"},
	{color: "bright-white", background: "blue"},
	{color: "black", background: "green"},
}

// applyPowerlineSegments treats the content between separators/flex nodes as
// one themed segment. Explicit backgrounds win; otherwise a built-in theme
// supplies both foreground and background so colorless layouts still render
// as Powerline. One column of padding is added at each segment edge.
func (e *docEval) applyPowerlineSegments(pieces []flatPiece) {
	if e.separatorStyle != "powerline" {
		return
	}
	themeIndex := 0
	seen := map[int]bool{}
	for _, piece := range pieces {
		segment := piece.segment
		if segment < 0 || seen[segment] {
			continue
		}
		seen[segment] = true
		var content []int
		background := ""
		for i := range pieces {
			if pieces[i].segment != segment || pieces[i].kind != pkContent || !pieces[i].visible {
				continue
			}
			content = append(content, i)
			if background == "" && pieces[i].style.background != "" {
				background = pieces[i].style.background
			}
		}
		if len(content) == 0 {
			continue
		}
		theme := powerlineTheme[themeIndex%len(powerlineTheme)]
		for _, i := range content {
			if pieces[i].style.background == "" {
				if background != "" {
					pieces[i].style.background = background
					if pieces[i].style.color == "" {
						pieces[i].style.color = theme.color
					}
				} else {
					pieces[i].style = mergePowerlineTheme(pieces[i].style, theme)
				}
			}
		}
		first, last := content[0], content[len(content)-1]
		if !strings.HasPrefix(pieces[first].plain, " ") {
			pieces[first].plain = " " + pieces[first].plain
		}
		if !strings.HasSuffix(pieces[last].plain, " ") {
			pieces[last].plain += " "
		}
		themeIndex++
	}
}

func mergePowerlineTheme(st, theme docStyle) docStyle {
	st.color = theme.color
	st.background = theme.background
	return st
}

// renderFlexPiece closes the left run and opens the right run around the
// flexible default-background space. The caps consume columns from the flex
// width so the overall terminal-width calculation remains stable.
func (e *docEval) renderFlexPiece(pieces []flatPiece, projected []piece, index, width int) (string, string) {
	if e.separatorStyle != "powerline" || width < 2 {
		plain := strings.Repeat(" ", width)
		return plain, plain
	}
	left, right := "", ""
	for i := index - 1; i >= 0; i-- {
		if projected[i].visible && pieces[i].kind == pkContent {
			left = pieces[i].style.background
			break
		}
	}
	for i := index + 1; i < len(pieces); i++ {
		if projected[i].visible && pieces[i].kind == pkContent {
			right = pieces[i].style.background
			break
		}
	}
	middle := strings.Repeat(" ", width-2)
	plain := "\ue0b0" + middle + "\ue0b2"
	ansi := stylizeDoc("\ue0b0", docStyle{color: left}, e.level) + middle +
		stylizeDoc("\ue0b2", docStyle{color: right}, e.level)
	return plain, ansi
}

// resolvePowerlineStyles colors generated Powerline separators as background
// transitions. An absent neighboring background means the terminal default.
func (e *docEval) resolvePowerlineStyles(pieces []flatPiece, projected []piece) {
	for i := range pieces {
		if !projected[i].visible || pieces[i].kind != pkSeparator {
			continue
		}
		if !pieces[i].powerline {
			continue
		}
		left, right := "", ""
		for j := i - 1; j >= 0; j-- {
			if projected[j].visible && pieces[j].kind == pkContent {
				left = pieces[j].style.background
				break
			}
		}
		for j := i + 1; j < len(pieces); j++ {
			if projected[j].visible && pieces[j].kind == pkContent {
				right = pieces[j].style.background
				break
			}
		}
		pieces[i].style.color = left
		pieces[i].style.background = right
	}
}

// flattenLine produces the ordered leaf pieces of a line. A line gated off
// by its own optional/when yields no pieces (so the line is omitted). The
// line's own decoration (rare) wraps its children like a span.
func (e *docEval) flattenLine(line *dsl.LineNode) []flatPiece {
	base := mergeStyle(docStyle{}, line.Common.Style)
	if !e.gate(line.Common, "") {
		return nil
	}
	var out []flatPiece
	// <line> carries no color-rules (the parser rejects them there), so its
	// own decoration color is just its resolved style color. LineNode is not
	// a dsl.Node, so its decoration pieces carry a nil source node.
	e.appendDeco(&out, nil, spaces(line.Common.Box.PaddingLeft), base, -1)
	e.appendDeco(&out, nil, line.Common.Prefix, base, -1)
	for i, child := range line.Children {
		e.flattenNodes([]dsl.Node{child}, base, &out, i)
	}
	e.appendDeco(&out, nil, line.Common.Suffix, base, -1)
	e.appendDeco(&out, nil, spaces(line.Common.Box.PaddingRight), base, -1)
	return out
}

// flattenNodes walks a mixed-content child list, appending pieces.
func (e *docEval) flattenNodes(nodes []dsl.Node, inherited docStyle, out *[]flatPiece, segment int) {
	for _, node := range nodes {
		switch n := node.(type) {
		case *dsl.SpanNode:
			e.emitSpan(n, inherited, out, segment)
		case *dsl.TextNode:
			e.emitText(n, inherited, out, segment)
		case *dsl.FieldNode:
			e.emitField(n, inherited, out, segment)
		case *dsl.FlexNode:
			*out = append(*out, flatPiece{node: n, kind: pkFlex, flex: n.Size, visible: true, segment: segment})
		case *dsl.RawTextNode:
			// Raw text has no decoration/gating of its own; it inherits the
			// parent style and always renders its whitespace-collapsed text.
			*out = append(*out, flatPiece{
				node: n, kind: pkContent, plain: n.RenderText, segment: segment,
				style: inherited, visible: n.RenderText != "",
			})
		case *dsl.CommentNode:
			// Comments never render (markup.md "コメント").
		}
	}
}

// emitSpan renders a <span>: its children inherit the span's base style,
// while its own prefix/suffix/padding use that style with any color-rule
// override applied (markup.md item 3 & 7). A span gated off contributes
// nothing.
func (e *docEval) emitSpan(s *dsl.SpanNode, inherited docStyle, out *[]flatPiece, segment int) {
	base := mergeStyle(inherited, s.Common.Style)
	if !e.gate(s.Common, "") {
		return
	}
	own := base
	own.color = e.resolveColorRules(s.Common.ColorRules, base.color, "")
	e.appendDeco(out, s, spaces(s.Common.Box.PaddingLeft), own, segment)
	e.appendDeco(out, s, s.Common.Prefix, own, segment)
	e.flattenNodes(s.Children, base, out, segment)
	e.appendDeco(out, s, s.Common.Suffix, own, segment)
	e.appendDeco(out, s, spaces(s.Common.Box.PaddingRight), own, segment)
}

// emitText renders a <text>. A role="separator" text becomes a collapsible
// separator piece; in compact mode its padding is dropped so the canonical
// " | " separator shrinks to "|" in the default-separator compaction
// (prefix/suffix, if any, are kept).
func (e *docEval) emitText(t *dsl.TextNode, inherited docStyle, out *[]flatPiece, segment int) {
	st := mergeStyle(inherited, t.Common.Style)
	if !e.gate(t.Common, "") {
		return
	}
	own := st
	own.color = e.resolveColorRules(t.Common.ColorRules, st.color, "")

	if t.Role == "separator" {
		var plain string
		useDocumentStyle := t.Value == "" || isLegacyDefaultSeparator(t)
		powerline := useDocumentStyle && e.separatorStyle == "powerline"
		if powerline {
			plain = "\ue0b0"
		} else if useDocumentStyle && e.compact {
			plain = "|"
		} else if useDocumentStyle {
			plain = " | "
		} else if e.compact {
			plain = t.Common.Prefix + t.Value + t.Common.Suffix
		} else {
			plain = spaces(t.Common.Box.PaddingLeft) + t.Common.Prefix + t.Value +
				t.Common.Suffix + spaces(t.Common.Box.PaddingRight)
		}
		*out = append(*out, flatPiece{node: t, kind: pkSeparator, plain: plain, style: own, powerline: powerline, segment: segment})
		return
	}
	plain := spaces(t.Common.Box.PaddingLeft) + t.Common.Prefix + t.Value +
		t.Common.Suffix + spaces(t.Common.Box.PaddingRight)
	*out = append(*out, flatPiece{node: t, kind: pkContent, plain: plain, style: own, visible: plain != "", segment: segment})
}

// isLegacyDefaultSeparator recognizes the canonical separator emitted by
// Statusloom before output-style existed. It is indistinguishable from a
// user spelling the old default explicitly, so this narrow shape is treated
// as unspecified for migration compatibility.
func isLegacyDefaultSeparator(t *dsl.TextNode) bool {
	left, right := t.Common.Box.PaddingLeft, t.Common.Box.PaddingRight
	return t.Value == "|" && left != nil && right != nil && *left == 1 && *right == 1 &&
		t.Common.Prefix == "" && t.Common.Suffix == ""
}

// emitField renders a <field>. optional/when gate the whole field; when it
// passes, the value may be empty but prefix/suffix/padding still render
// (markup.md item 3). Layout order: padding-left, prefix, content, suffix,
// padding-right, all in the field's own (color-rule-resolved) style.
func (e *docEval) emitField(f *dsl.FieldNode, inherited docStyle, out *[]flatPiece, segment int) {
	st := mergeStyle(inherited, f.Common.Style)
	self := e.selfMetric(f.Name)
	if !e.gate(f.Common, self) {
		return
	}
	content := e.fieldText(f)
	own := st
	own.color = e.resolveColorRules(f.Common.ColorRules, st.color, self)

	plain := spaces(f.Common.Box.PaddingLeft) + f.Common.Prefix + content +
		f.Common.Suffix + spaces(f.Common.Box.PaddingRight)

	link := ""
	if content != "" && f.Hyperlink && e.level != levelNone {
		// hyperlinkURL returns "" for non-linkable fields, so a stray
		// hyperlink attribute (validation should have rejected it) is inert.
		link = hyperlinkURL(f.Name, e.snap)
	}
	*out = append(*out, flatPiece{node: f, kind: pkContent, plain: plain, style: own, link: link, visible: plain != "", segment: segment})
}

// appendDeco appends a content piece for a node's own prefix/suffix/padding
// text, skipping empty strings.
func (e *docEval) appendDeco(out *[]flatPiece, node dsl.Node, text string, style docStyle, segment int) {
	if text == "" {
		return
	}
	*out = append(*out, flatPiece{node: node, kind: pkContent, plain: text, style: style, visible: true, segment: segment})
}

// gate evaluates a node's optional + when attributes (AND). optional checks
// a field's existence (its default, non-compact/non-raw text is non-empty);
// when parses and evaluates the condition. A parse/eval error or an
// unresolvable metric hides the node (markup.md "条件表示"). self is the
// owning field's self metric ("" for span/text/line, where "self" cannot
// resolve).
func (e *docEval) gate(c dsl.CommonAttributes, self string) bool {
	if c.Optional != "" && !e.fieldExists(c.Optional) {
		return false
	}
	if c.When != "" {
		expr, err := dsl.ParseCondition(c.When)
		if err != nil {
			return false
		}
		ok, err := expr.Eval(docResolver{snap: e.snap, cfg: e.cfg, opts: e.opts, self: self})
		if err != nil || !ok {
			return false
		}
	}
	return true
}

// fieldExists reports whether a named field currently has data: its default
// (non-compact, non-raw) display text is non-empty. A numeric zero that
// still renders a non-empty string (e.g. "$0.00") counts as present
// (markup.md "optional").
func (e *docEval) fieldExists(name string) bool {
	spec := config.WidgetSpec{Type: name}
	return renderContent(spec, e.snap, e.cfg, e.opts, false) != ""
}

// fieldText resolves a field's display text: the renderContent default
// (honoring compact/raw), overridden by an explicit formatter on the field's
// self metric when one applies (markup.md item 4). raw takes precedence over
// a formatter, matching its precedence over compact.
func (e *docEval) fieldText(f *dsl.FieldNode) string {
	spec := config.WidgetSpec{Type: f.Name, RawValue: f.Raw}
	txt := renderContent(spec, e.snap, e.cfg, e.opts, e.compact)
	if !f.Raw && f.Formatter.Name != "" {
		if formatted, ok := e.applyFormat(f); ok {
			txt = formatted
		}
	}
	return txt
}

// resolveColorRules returns the first matching color-rule's color (first-win
// evaluation over the node's self resolver), falling back to base when none
// match. A rule whose condition fails to parse or errors is skipped
// (markup.md "color-rule").
func (e *docEval) resolveColorRules(rules []dsl.ColorRule, base, self string) string {
	for _, r := range rules {
		expr, err := dsl.ParseCondition(r.When)
		if err != nil {
			continue
		}
		ok, err := expr.Eval(docResolver{snap: e.snap, cfg: e.cfg, opts: e.opts, self: self})
		if err != nil {
			continue
		}
		if ok {
			return r.Color
		}
	}
	return base
}

// spaces renders a padding count (nil / 0 -> "").
func spaces(n *int) string {
	if n == nil || *n <= 0 {
		return ""
	}
	return strings.Repeat(" ", *n)
}
