package dsl

import (
	"reflect"
	"strings"
	"testing"
)

// normalizeForCompare strips information the canonical serializer
// deliberately does not preserve, so a parse->serialize->parse round trip
// can be compared for *semantic* equivalence:
//   - every NodeMeta.SourceRange / Dirty (byte offsets change after
//     reformatting),
//   - RawTextNode.RawValue (only the whitespace-collapsed RenderText is
//     meaningful and re-emitted).
//
// Decoration *bool attributes are NOT normalized: an explicit false
// overrides an inherited true, so bold="false" must survive round-trips
// verbatim (see TestSerialize_ExplicitFalseDecoration).
func normalizeForCompare(doc *Document) *Document {
	if doc == nil || doc.Root == nil {
		return doc
	}
	r := doc.Root
	r.Meta = NodeMeta{}
	if r.Git != nil {
		r.Git.Meta = NodeMeta{}
	}
	for _, c := range r.Comments {
		c.Meta = NodeMeta{}
	}
	for _, l := range r.Layouts {
		l.Meta = NodeMeta{}
		for _, c := range l.Comments {
			c.Meta = NodeMeta{}
		}
		for _, ln := range l.Lines {
			ln.Meta = NodeMeta{}
			normalizeCommon(&ln.Common)
			normalizeChildren(ln.Children)
		}
	}
	// Keep Source out of the comparison; it necessarily differs.
	return &Document{Root: r}
}

func normalizeCommon(c *CommonAttributes) {
	for i := range c.ColorRules {
		c.ColorRules[i].Meta = NodeMeta{}
	}
}

func normalizeChildren(children []Node) {
	for _, child := range children {
		switch n := child.(type) {
		case *SpanNode:
			n.Meta = NodeMeta{}
			normalizeCommon(&n.Common)
			normalizeChildren(n.Children)
		case *TextNode:
			n.Meta = NodeMeta{}
			normalizeCommon(&n.Common)
		case *FieldNode:
			n.Meta = NodeMeta{}
			normalizeCommon(&n.Common)
		case *FlexNode:
			n.Meta = NodeMeta{}
		case *RawTextNode:
			n.Meta = NodeMeta{}
			n.RawValue = ""
		case *CommentNode:
			n.Meta = NodeMeta{}
		}
	}
}

// TestSerialize_RoundTrip parses, serializes, and re-parses a broad document
// and asserts (a) semantic AST equivalence and (b) serializer idempotence.
func TestSerialize_RoundTrip(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="ansi16" output-style="powerline" compact-threshold="60" context-percentage-mode="usable" context-reserve-tokens="0">
  <!-- shared git config -->
  <git cache-ttl-ms="3000" timeout-ms="200" include-untracked="true" collect-numstat="false"/>
  <layout name="default" active="true">
    <!-- primary row -->
    <line>
      <text>Model: </text>
      <field name="model" color="cyan" bold="true" min-width="6" align="right"/>
      <span optional="thinking-effort" prefix=" (" suffix=")" color="bright-black">
        <field name="thinking-effort" color="yellow"/>
      </span>
      <text role="separator" padding="1">|</text>
      Context:
      <field name="context-percentage-usable" format="percent" precision="0">
        <color-rule when="self ge 90" color="red"/>
        <color-rule when="self ge 70" color="yellow"/>
      </field>
      <flex size="full-minus-2"/>
    </line>
  </layout>
  <layout name="compact">
    <line>
      <field name="model"/>
    </line>
  </layout>
</statusloom>
`
	doc1 := parseClean(t, src)
	out1 := Serialize(doc1)

	doc2 := parseClean(t, out1)
	out2 := Serialize(doc2)

	if out1 != out2 {
		t.Errorf("serializer not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out1, out2)
	}

	n1 := normalizeForCompare(parseClean(t, src))
	n2 := normalizeForCompare(parseClean(t, out1))
	if !reflect.DeepEqual(n1, n2) {
		t.Errorf("round-trip AST mismatch:\n%#v\n\n%#v", n1.Root, n2.Root)
	}
}

func TestSerialize_SelfClosing(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code"><layout name="a"><line><field name="model"/><flex/></line></layout></statusloom>`
	out := Serialize(parseClean(t, src))
	for _, want := range []string{`<field name="model"/>`, `<flex/>`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected self-closing %q in output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "</field>") || strings.Contains(out, "</flex>") {
		t.Errorf("childless field/flex should be self-closing:\n%s", out)
	}
	// Single-layout active omission must round-trip without inventing active.
	if strings.Contains(out, "active=") {
		t.Errorf("did not expect an active attribute:\n%s", out)
	}
}

// TestSerialize_ExplicitFalseDecoration ensures explicit false decoration
// attributes are preserved: nil means "inherit", while bold="false" cancels
// an inherited bold (mergeStyle in the renderer), so the two are not
// interchangeable and the serializer may not drop the attribute.
func TestSerialize_ExplicitFalseDecoration(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><line><span bold="true" italic="true"><field name="model" bold="false" dim="false" italic="false" underline="false" strikethrough="false"/></span></line></layout><layout name="b" active="false"><line><field name="model"/></line></layout></statusloom>`
	out := Serialize(parseClean(t, src))
	for _, want := range []string{
		`bold="false"`, `dim="false"`, `italic="false"`,
		`underline="false"`, `strikethrough="false"`,
		`bold="true"`, `italic="true"`,
		`active="false"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("explicit %s dropped:\n%s", want, out)
		}
	}
	// The re-parsed AST must carry the same pointers-with-false, not nil:
	// round-trip semantic equality covers this via normalizeForCompare,
	// which no longer normalizes decoration bools.
	n1 := normalizeForCompare(parseClean(t, src))
	n2 := normalizeForCompare(parseClean(t, Serialize(parseClean(t, src))))
	if !reflect.DeepEqual(n1, n2) {
		t.Errorf("explicit-false round-trip mismatch:\n%#v\n\n%#v", n1.Root, n2.Root)
	}
}

// TestSerialize_MinWidthAndAlign confirms min-width/align round-trip through
// Serialize (attribute presence when set, absence when unset, and stable
// re-parse) as part of the same canonical-attribute-order contract as the
// other <field> attributes.
func TestSerialize_MinWidthAndAlign(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code-subagent"><layout name="a" active="true"><line>` +
		`<field name="task-tokens" format="compact-number" min-width="6" align="right"/>` +
		`<field name="task-description"/>` +
		`</line></layout></statusloom>`
	doc := parseClean(t, src)
	out := Serialize(doc)
	if !strings.Contains(out, `min-width="6"`) || !strings.Contains(out, `align="right"`) {
		t.Errorf("expected min-width/align in output:\n%s", out)
	}
	// A field without min-width/align must not gain either attribute.
	if strings.Contains(out, `<field name="task-description" min-width`) ||
		strings.Contains(out, `<field name="task-description" align`) {
		t.Errorf("unspecified min-width/align should not be serialized:\n%s", out)
	}

	doc2 := parseClean(t, out)
	f := onlyLine(t, doc2).Children[0].(*FieldNode)
	if f.MinWidth == nil || *f.MinWidth != 6 || f.Align != "right" {
		t.Errorf("round-tripped field = min-width=%v align=%q, want 6/right", f.MinWidth, f.Align)
	}

	n1 := normalizeForCompare(parseClean(t, src))
	n2 := normalizeForCompare(parseClean(t, out))
	if !reflect.DeepEqual(n1, n2) {
		t.Errorf("min-width/align round-trip AST mismatch:\n%#v\n\n%#v", n1.Root, n2.Root)
	}
}

func TestSerialize_PaddingFold(t *testing.T) {
	// Equal left/right folds to padding="N".
	out := Serialize(parseClean(t, `<statusloom version="1" tool="claude-code"><layout name="a"><line><text padding-left="1" padding-right="1">|</text></line></layout></statusloom>`))
	if !strings.Contains(out, `padding="1"`) {
		t.Errorf("equal padding should fold to padding=\"1\":\n%s", out)
	}
	if strings.Contains(out, "padding-left") || strings.Contains(out, "padding-right") {
		t.Errorf("folded padding should not emit individual sides:\n%s", out)
	}
	// Asymmetric padding stays split.
	out = Serialize(parseClean(t, `<statusloom version="1" tool="claude-code"><layout name="a"><line><text padding-left="1" padding-right="2">|</text></line></layout></statusloom>`))
	if !strings.Contains(out, `padding-left="1"`) || !strings.Contains(out, `padding-right="2"`) {
		t.Errorf("asymmetric padding should stay split:\n%s", out)
	}
	if strings.Contains(out, `padding="`) {
		t.Errorf("asymmetric padding should not fold:\n%s", out)
	}
}

func TestSerialize_Escaping(t *testing.T) {
	// & < " in an attribute; & < in text content. The when uses the word
	// form but we force a literal string with an ampersand and angle bracket.
	src := `<statusloom version="1" tool="claude-code"><layout name="a"><line><text when="model ne 'a&amp;b&lt;c'">A &amp; B &lt; C</text></line></layout></statusloom>`
	out := Serialize(parseClean(t, src))
	if !strings.Contains(out, `A &amp; B &lt; C`) {
		t.Errorf("text content not escaped:\n%s", out)
	}
	if !strings.Contains(out, `a&amp;b&lt;c`) {
		t.Errorf("attribute value not escaped:\n%s", out)
	}
	// Must re-parse cleanly (well-formed).
	if _, diags := Parse(out); HasErrors(diags) {
		t.Errorf("escaped output is not well-formed: %v\n%s", diags, out)
	}
}

func TestSerialize_CommentsPreserved(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code">
  <!-- root comment -->
  <layout name="a">
    <!-- layout comment -->
    <line>
      <!-- inline comment -->
      <field name="model"/>
    </line>
  </layout>
</statusloom>
`
	out := Serialize(parseClean(t, src))
	for _, want := range []string{"<!-- root comment -->", "<!-- layout comment -->", "<!-- inline comment -->"} {
		if !strings.Contains(out, want) {
			t.Errorf("comment %q dropped:\n%s", want, out)
		}
	}
}

func TestSerialize_Nil(t *testing.T) {
	if Serialize(nil) != "" {
		t.Error("Serialize(nil) should be empty")
	}
	if Serialize(&Document{}) != "" {
		t.Error("Serialize(doc without root) should be empty")
	}
}
