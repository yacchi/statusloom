package dsl

import "testing"

// TestNodeInterfaceImplementations pins down exactly which AST types are
// line/span/text children (Node) and which are not, per the AST design.
func TestNodeInterfaceImplementations(t *testing.T) {
	var _ Node = (*SpanNode)(nil)
	var _ Node = (*RawTextNode)(nil)
	var _ Node = (*TextNode)(nil)
	var _ Node = (*FieldNode)(nil)
	var _ Node = (*FlexNode)(nil)
	var _ Node = (*CommentNode)(nil)

	nodes := []any{
		(*SpanNode)(nil), (*RawTextNode)(nil), (*TextNode)(nil),
		(*FieldNode)(nil), (*FlexNode)(nil), (*CommentNode)(nil),
	}
	for _, n := range nodes {
		if _, ok := n.(Node); !ok {
			t.Fatalf("%T should implement Node", n)
		}
	}

	// These container/config types are intentionally NOT Nodes.
	nonNodes := []any{
		(*StatusloomNode)(nil), (*LayoutNode)(nil), (*LineNode)(nil),
		(*GitSettings)(nil),
	}
	for _, n := range nonNodes {
		if _, ok := n.(Node); ok {
			t.Fatalf("%T should not implement Node", n)
		}
	}
}
