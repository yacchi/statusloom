package dsl

// This file implements Canonicalize, the whole-document normal form used by
// `statusloom fmt` (markup.md "serializerは自前実装" / "when式のワード形式
// 正規化"). It is Serialize plus word-form normalization of every `when`
// (and color-rule `when`) attribute. Unlike SerializeMinimal (which preserves
// the original DSL representation for unchanged nodes), Canonicalize
// deliberately rewrites the whole document into the canonical shape.

// Canonicalize returns the canonical form of doc: the same output as Serialize
// but with every `when` expression normalized to the first-class word form via
// FormatCondition. A `when` that fails to parse is left verbatim (fmt reports
// the diagnostic separately and refuses to write on errors).
//
// It mutates doc's When strings in place; callers pass a freshly parsed,
// otherwise-unused document (as `fmt` does).
func Canonicalize(doc *Document) string {
	if doc == nil || doc.Root == nil {
		return ""
	}
	normalizeStatusloomWhen(doc.Root)
	return Serialize(doc)
}

// normalizeWhen rewrites a single when expression to word form, keeping the
// original text if it does not parse.
func normalizeWhen(when string) string {
	if when == "" {
		return ""
	}
	out, err := FormatCondition(when)
	if err != nil {
		return when
	}
	return out
}

func normalizeStatusloomWhen(n *StatusloomNode) {
	for _, l := range n.Layouts {
		for _, ln := range l.Lines {
			normalizeCommonWhen(&ln.Common)
			normalizeChildrenWhen(ln.Children)
		}
	}
}

func normalizeChildrenWhen(children []Node) {
	for _, child := range children {
		switch c := child.(type) {
		case *SpanNode:
			normalizeCommonWhen(&c.Common)
			normalizeChildrenWhen(c.Children)
		case *TextNode:
			normalizeCommonWhen(&c.Common)
		case *FieldNode:
			normalizeCommonWhen(&c.Common)
		}
	}
}

func normalizeCommonWhen(c *CommonAttributes) {
	c.When = normalizeWhen(c.When)
	for i := range c.ColorRules {
		c.ColorRules[i].When = normalizeWhen(c.ColorRules[i].When)
	}
}
