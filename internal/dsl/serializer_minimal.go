package dsl

// This file implements the Phase-3 minimal-diff serializer (markup.md
// "DSL表現の維持" / "完全なlossless CST実装が過大であれば、まずはノード単位
// のsource rangeとdirty管理で実現する"). Unlike Serialize (whole-document
// canonical form), SerializeMinimal reuses each unchanged node's original
// source slice verbatim, regenerating only nodes that were edited
// (Meta.Dirty) or lack a usable source range (client-inserted nodes). This
// keeps comments, raw text, symbolic-operator `when` expressions, and custom
// indentation intact through Visual-Editor round trips.

import (
	"math"
	"strings"
)

// SerializeMinimal regenerates doc, reusing unchanged nodes' source slices
// verbatim and only regenerating dirty / range-less nodes.
//
// Reconstruction rule (applied per node):
//
//   - A node that is not Dirty, whose SourceRange is valid for doc.Source,
//     and whose whole subtree (children + color-rules) is likewise clean, is
//     emitted verbatim as doc.Source[Start:End].
//   - Otherwise the node is regenerated: leaves (text/field/flex/raw-text/
//     comment/color-rule) via the canonical serializer; containers (line/
//     span/layout/statusloom) by re-emitting their own tag canonically and
//     processing each child by this same rule. Whitespace/indentation around
//     regenerated children is canonical.
//
// When the entire document is clean, SerializeMinimal(doc) == doc.Source
// byte-for-byte. A nil/rootless document yields "".
func SerializeMinimal(doc *Document) string {
	if doc == nil || doc.Root == nil {
		return ""
	}
	src := doc.Source
	if statusloomClean(doc.Root, src) {
		// Nothing changed: return the original source untouched (this also
		// preserves any leading/trailing bytes outside the root range).
		return src
	}
	var b strings.Builder
	serializeMinimalStatusloom(&b, doc.Root, src)
	return b.String()
}

// rangeUsable reports whether r can index a verbatim slice of a src of the
// given length: it must be non-zero, in bounds, and non-empty.
func rangeUsable(r SourceRange, srcLen int) bool {
	return !r.IsZero() && r.Start >= 0 && r.End <= srcLen && r.Start < r.End
}

// metaReusable reports whether a node with this meta may be emitted verbatim
// from src (not dirty and backed by a usable range).
func metaReusable(m NodeMeta, src string) bool {
	return !m.Dirty && rangeUsable(m.SourceRange, len(src))
}

// sortKey returns a node's re-interleaving key: its source start when the
// range is usable, else a sentinel that sorts client-inserted (range-less)
// nodes after all positioned ones, preserving their append order via the
// stable sort in emitSorted.
func sortKey(m NodeMeta, src string) int {
	if rangeUsable(m.SourceRange, len(src)) {
		return m.SourceRange.Start
	}
	return math.MaxInt32
}

// ---- subtree cleanliness ----

func nodeClean(node Node, src string) bool {
	switch n := node.(type) {
	case *SpanNode:
		if !metaReusable(n.Meta, src) {
			return false
		}
		for _, c := range n.Children {
			if !nodeClean(c, src) {
				return false
			}
		}
		return colorRulesClean(n.Common.ColorRules, src)
	case *TextNode:
		return metaReusable(n.Meta, src) && colorRulesClean(n.Common.ColorRules, src)
	case *FieldNode:
		return metaReusable(n.Meta, src) && colorRulesClean(n.Common.ColorRules, src)
	case *FlexNode:
		return metaReusable(n.Meta, src)
	case *RawTextNode:
		return metaReusable(n.Meta, src)
	case *CommentNode:
		return metaReusable(n.Meta, src)
	default:
		return false
	}
}

func colorRulesClean(rules []ColorRule, src string) bool {
	for _, cr := range rules {
		if !metaReusable(cr.Meta, src) {
			return false
		}
	}
	return true
}

func lineClean(l *LineNode, src string) bool {
	if !metaReusable(l.Meta, src) {
		return false
	}
	for _, c := range l.Children {
		if !nodeClean(c, src) {
			return false
		}
	}
	return true
}

func layoutClean(l *LayoutNode, src string) bool {
	if !metaReusable(l.Meta, src) {
		return false
	}
	for _, ln := range l.Lines {
		if !lineClean(ln, src) {
			return false
		}
	}
	for _, c := range l.Comments {
		if !metaReusable(c.Meta, src) {
			return false
		}
	}
	return true
}

func statusloomClean(n *StatusloomNode, src string) bool {
	if !metaReusable(n.Meta, src) {
		return false
	}
	if n.Git != nil && !metaReusable(n.Git.Meta, src) {
		return false
	}
	for _, l := range n.Layouts {
		if !layoutClean(l, src) {
			return false
		}
	}
	for _, c := range n.Comments {
		if !metaReusable(c.Meta, src) {
			return false
		}
	}
	return true
}

// ---- verbatim reuse helpers ----

// reuseSlice writes indent + the node's verbatim source slice + newline.
// Used for elements whose range spans exactly the element bytes (their range
// starts at '<'), so no surrounding whitespace leaks in.
func reuseSlice(b *strings.Builder, depth int, src string, r SourceRange) {
	b.WriteString(indentStr(depth))
	b.WriteString(r.Slice(src))
	b.WriteString("\n")
}

// ---- reconstruction ----

func serializeMinimalStatusloom(b *strings.Builder, n *StatusloomNode, src string) {
	writeStatusloomOpenTag(b, n)
	b.WriteString("\n")

	var items []serItem
	if n.Git != nil {
		g := n.Git
		items = append(items, serItem{sortKey(g.Meta, src), func() {
			if metaReusable(g.Meta, src) {
				reuseSlice(b, 1, src, g.Meta.SourceRange)
			} else {
				serializeGit(b, 1, g)
			}
		}})
	}
	for _, l := range n.Layouts {
		l := l
		items = append(items, serItem{sortKey(l.Meta, src), func() { emitMinimalLayout(b, 1, l, src) }})
	}
	for _, c := range n.Comments {
		c := c
		items = append(items, serItem{sortKey(c.Meta, src), func() {
			if metaReusable(c.Meta, src) {
				reuseSlice(b, 1, src, c.Meta.SourceRange)
			} else {
				serializeComment(b, 1, c)
			}
		}})
	}
	emitSorted(items)

	b.WriteString("</statusloom>\n")
}

func emitMinimalLayout(b *strings.Builder, depth int, n *LayoutNode, src string) {
	if layoutClean(n, src) {
		reuseSlice(b, depth, src, n.Meta.SourceRange)
		return
	}
	ind := indentStr(depth)
	b.WriteString(ind)
	writeLayoutOpenTag(b, n)
	b.WriteString("\n")

	var items []serItem
	for _, l := range n.Lines {
		l := l
		items = append(items, serItem{sortKey(l.Meta, src), func() { emitMinimalLine(b, depth+1, l, src) }})
	}
	for _, c := range n.Comments {
		c := c
		items = append(items, serItem{sortKey(c.Meta, src), func() {
			if metaReusable(c.Meta, src) {
				reuseSlice(b, depth+1, src, c.Meta.SourceRange)
			} else {
				serializeComment(b, depth+1, c)
			}
		}})
	}
	emitSorted(items)

	b.WriteString(ind)
	b.WriteString("</layout>\n")
}

func emitMinimalLine(b *strings.Builder, depth int, n *LineNode, src string) {
	if lineClean(n, src) {
		reuseSlice(b, depth, src, n.Meta.SourceRange)
		return
	}
	ind := indentStr(depth)
	b.WriteString(ind)
	writeLineOpenTag(b, n)
	if len(n.Children) == 0 {
		b.WriteString("></line>\n")
		return
	}
	b.WriteString(">\n")
	for _, child := range n.Children {
		emitMinimalNode(b, depth+1, child, src)
	}
	b.WriteString(ind)
	b.WriteString("</line>\n")
}

// emitMinimalNode emits a mixed-content child: verbatim when its subtree is
// clean, otherwise regenerated (canonical for leaves, reconstructed for span).
func emitMinimalNode(b *strings.Builder, depth int, node Node, src string) {
	if nodeClean(node, src) {
		switch n := node.(type) {
		case *RawTextNode:
			// A raw-text node's range includes the surrounding whitespace of
			// its CharData run, so a verbatim slice would leak newlines /
			// indentation into a reconstructed parent. Emit the already
			// whitespace-collapsed RenderText instead (identical to canonical,
			// and byte-stable for unchanged raw text).
			b.WriteString(indentStr(depth))
			b.WriteString(escapeText(n.RenderText))
			b.WriteString("\n")
		default:
			reuseSlice(b, depth, src, nodeMeta(node).SourceRange)
		}
		return
	}
	switch n := node.(type) {
	case *SpanNode:
		emitMinimalSpan(b, depth, n, src)
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

func emitMinimalSpan(b *strings.Builder, depth int, n *SpanNode, src string) {
	ind := indentStr(depth)
	b.WriteString(ind)
	writeSpanOpenTag(b, n)
	if len(n.Children) == 0 && len(n.Common.ColorRules) == 0 {
		b.WriteString("></span>\n")
		return
	}
	b.WriteString(">\n")
	// Children keep their array order (a moveChild edit reorders the array;
	// sorting by source position would undo the move). Color-rules do not
	// affect each other's position relative to children semantically, so they
	// follow the content in array order.
	for _, child := range n.Children {
		emitMinimalNode(b, depth+1, child, src)
	}
	for _, cr := range n.Common.ColorRules {
		if metaReusable(cr.Meta, src) {
			reuseSlice(b, depth+1, src, cr.Meta.SourceRange)
		} else {
			serializeColorRule(b, depth+1, cr)
		}
	}
	b.WriteString(ind)
	b.WriteString("</span>\n")
}

// nodeMeta returns a mixed-content node's NodeMeta.
func nodeMeta(node Node) NodeMeta {
	switch n := node.(type) {
	case *SpanNode:
		return n.Meta
	case *TextNode:
		return n.Meta
	case *FieldNode:
		return n.Meta
	case *FlexNode:
		return n.Meta
	case *RawTextNode:
		return n.Meta
	case *CommentNode:
		return n.Meta
	default:
		return NodeMeta{}
	}
}
