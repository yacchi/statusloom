package dsl

import "testing"

func rawTexts(line *LineNode) []*RawTextNode {
	var out []*RawTextNode
	for _, ch := range line.Children {
		if rt, ok := ch.(*RawTextNode); ok {
			out = append(out, rt)
		}
	}
	return out
}

func TestWhitespaceOnlyRawNodeIgnored(t *testing.T) {
	doc := parseClean(t, wrap("\n    \n\t")+"")
	line := onlyLine(t, doc)
	if len(line.Children) != 0 {
		t.Fatalf("whitespace-only content should yield no children, got %#v", line.Children)
	}
}

func TestRawTextLeadingTrailingWhitespaceTrimmed(t *testing.T) {
	// Indentation and newlines around "Session Cost:" are dropped; the
	// rendered text is trimmed while the raw value is preserved.
	src := wrap("\n  Session Cost:\n  <field name=\"session-cost\"/>\n")
	doc := parseClean(t, src)
	raws := rawTexts(onlyLine(t, doc))
	if len(raws) != 1 {
		t.Fatalf("want 1 raw text, got %d", len(raws))
	}
	if raws[0].RenderText != "Session Cost:" {
		t.Fatalf("RenderText = %q, want %q", raws[0].RenderText, "Session Cost:")
	}
	if raws[0].RawValue != "\n  Session Cost:\n  " {
		t.Fatalf("RawValue = %q (should keep original whitespace)", raws[0].RawValue)
	}
}

func TestRawTextInternalWhitespacePreserved(t *testing.T) {
	doc := parseClean(t, wrap(`<field name="model"/>  a   b  <field name="tool-version"/>`))
	raws := rawTexts(onlyLine(t, doc))
	if len(raws) != 1 {
		t.Fatalf("want 1 raw text, got %d: %#v", len(raws), raws)
	}
	if raws[0].RenderText != "a   b" {
		t.Fatalf("RenderText = %q, want internal spaces preserved", raws[0].RenderText)
	}
}

func TestRawTextInternalNewlineCollapsedToSpace(t *testing.T) {
	// A raw text spanning multiple source lines collapses each internal
	// whitespace run that contains a newline into a single space, so the
	// rendered status line stays single-line.
	src := wrap("\n  Session\n  Cost:\n  <field name=\"session-cost\"/>\n")
	doc := parseClean(t, src)
	raws := rawTexts(onlyLine(t, doc))
	if len(raws) != 1 {
		t.Fatalf("want 1 raw text, got %d", len(raws))
	}
	if raws[0].RenderText != "Session Cost:" {
		t.Fatalf("RenderText = %q, want %q", raws[0].RenderText, "Session Cost:")
	}
	if raws[0].RawValue != "\n  Session\n  Cost:\n  " {
		t.Fatalf("RawValue = %q (should stay unmodified)", raws[0].RawValue)
	}
}

func TestRawTextInternalSpacesWithoutNewlinePreserved(t *testing.T) {
	// Internal runs of spaces/tabs that contain no newline are kept
	// verbatim; only newline-bearing runs collapse to one space.
	src := wrap("\n  a   b\tc\n  d\n  <field name=\"model\"/>\n")
	doc := parseClean(t, src)
	raws := rawTexts(onlyLine(t, doc))
	if len(raws) != 1 {
		t.Fatalf("want 1 raw text, got %d", len(raws))
	}
	if raws[0].RenderText != "a   b\tc d" {
		t.Fatalf("RenderText = %q, want %q", raws[0].RenderText, "a   b\tc d")
	}
}

func TestTextElementInternalWhitespacePreserved(t *testing.T) {
	doc := parseClean(t, wrap(`<text>  Model:  </text>`))
	txt := onlyLine(t, doc).Children[0].(*TextNode)
	if txt.Value != "  Model:  " {
		t.Fatalf("Value = %q, want leading/trailing whitespace preserved", txt.Value)
	}
}

func TestTextElementNewlineIsError(t *testing.T) {
	_, diags := mustParse(t, wrap("<text>a\nb</text>"))
	if !hasErrorContaining(diags, "may not contain a newline") {
		t.Fatalf("want newline error, got %v", diags)
	}
}

func TestIndentationDoesNotAffectStructure(t *testing.T) {
	// A heavily-indented document parses to the same children as a
	// compact one: no stray raw-text nodes from indentation.
	src := `<statusloom version="1" tool="claude-code">
    <layout name="default" active="true">
        <line>
            <text>Model: </text>
            <field name="model"/>
        </line>
    </layout>
</statusloom>`
	doc := parseClean(t, src)
	line := onlyLine(t, doc)
	if len(line.Children) != 2 {
		t.Fatalf("indentation leaked into children: got %d %#v", len(line.Children), line.Children)
	}
	if _, ok := line.Children[0].(*TextNode); !ok {
		t.Fatalf("child 0 = %T", line.Children[0])
	}
	if _, ok := line.Children[1].(*FieldNode); !ok {
		t.Fatalf("child 1 = %T", line.Children[1])
	}
}
