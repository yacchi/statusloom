package dsl

import (
	"strings"
	"testing"
)

// docBody is a broad document exercising comments, a symbolic-operator when
// (entity-encoded &gt;=), non-canonical attribute order, a separator text, raw
// text, and idiosyncratic indentation — all things a minimal-diff round trip
// must preserve verbatim when nothing is dirty.
const docBody = `<statusloom version="1" tool="claude-code">
  <!-- top comment -->
  <layout name="default" active="true">
    <line>
        <text>Model: </text>
        <field color="cyan" name="model"/>
        <text role="separator" padding="1">|</text>
        Context:
        <field name="context-percentage" when="context-percent &gt;= 80"/>
    </line>
    <line>
      <field name="git-branch"/>
    </line>
  </layout>
</statusloom>
`

// TestSerializeMinimal_CleanIsByteIdentical asserts that a freshly parsed
// document with no dirty nodes serializes back to exactly its source, so the
// minimal path is a no-op when nothing changed. This also covers verbatim
// preservation of comments, the symbolic/entity-encoded when, non-canonical
// attribute order, and the custom indentation.
func TestSerializeMinimal_CleanIsByteIdentical(t *testing.T) {
	doc := parseClean(t, docBody)
	out := SerializeMinimal(doc)
	if out != docBody {
		t.Errorf("clean document not byte-identical.\n--- got ---\n%q\n--- want ---\n%q", out, docBody)
	}
}

// TestSerializeMinimal_OnlyDirtyNodeRegenerated marks a single field dirty and
// changes its color; every other node's exact source bytes must survive.
func TestSerializeMinimal_OnlyDirtyNodeRegenerated(t *testing.T) {
	doc := parseClean(t, docBody)
	line0 := doc.Root.Layouts[0].Lines[0]

	// Locate the two fields in the first line.
	modelField := findField(t, line0, "model")
	ctxField := findField(t, line0, "context-percentage")

	// Record the model field's verbatim source slice before editing.
	modelSlice := modelField.Meta.SourceRange.Slice(docBody)
	if modelSlice != `<field color="cyan" name="model"/>` {
		t.Fatalf("unexpected model slice: %q", modelSlice)
	}

	// Edit the context field: change color and mark it dirty (what the UI
	// stamps on an edited node).
	ctxField.Common.Style.Color = "green"
	ctxField.Meta.Dirty = true

	out := SerializeMinimal(doc)

	// The unchanged model field must appear verbatim, including its
	// non-canonical attribute order (color before name).
	if !strings.Contains(out, modelSlice) {
		t.Errorf("unchanged model field not preserved verbatim:\n%s", out)
	}
	// The separator text and raw text of the (reconstructed) line are reused
	// verbatim / by RenderText.
	if !strings.Contains(out, `<text role="separator" padding="1">|</text>`) {
		t.Errorf("unchanged separator text not preserved:\n%s", out)
	}
	if !strings.Contains(out, "Context:") {
		t.Errorf("unchanged raw text not preserved:\n%s", out)
	}
	// The second line was untouched: reused verbatim (with its 6-space indent).
	if !strings.Contains(out, "      <field name=\"git-branch\"/>") {
		t.Errorf("unchanged second line not preserved verbatim:\n%s", out)
	}
	// The dirty field is regenerated with the new color; the symbolic when
	// normalizes to '>=' entity-decoded on regeneration (canonical serializer).
	if !strings.Contains(out, `color="green"`) {
		t.Errorf("dirty field's new color missing:\n%s", out)
	}
	// The re-serialized document must be well-formed and re-parse cleanly.
	if _, diags := Parse(out); HasErrors(diags) {
		t.Errorf("minimal output not well-formed: %v\n%s", diags, out)
	}
}

// TestSerializeMinimal_InsertedNodeRegenerated inserts a client-built node
// (zero range) into a line and asserts it is regenerated while the existing
// siblings are reused verbatim (container-with-mixed-children case).
func TestSerializeMinimal_InsertedNodeRegenerated(t *testing.T) {
	doc := parseClean(t, docBody)
	line1 := doc.Root.Layouts[0].Lines[1]
	branchSlice := line1.Lines0Field(t).Meta.SourceRange.Slice(docBody)

	// Insert a new field with no source range (as the visual editor does).
	line1.Children = append(line1.Children, &FieldNode{Name: "tool-version"})

	out := SerializeMinimal(doc)
	if !strings.Contains(out, branchSlice) {
		t.Errorf("existing sibling not reused verbatim:\n%s", out)
	}
	if !strings.Contains(out, `<field name="tool-version"/>`) {
		t.Errorf("inserted node not regenerated:\n%s", out)
	}
	if _, diags := Parse(out); HasErrors(diags) {
		t.Errorf("minimal output not well-formed: %v\n%s", diags, out)
	}
}

// TestSerializeMinimal_RemovedChildForcesReconstruct removes a child and marks
// the parent line dirty (as removeNode stamps the container). The removed
// child must be gone and remaining children reused verbatim.
func TestSerializeMinimal_RemovedChildForcesReconstruct(t *testing.T) {
	doc := parseClean(t, docBody)
	line0 := doc.Root.Layouts[0].Lines[0]
	sepSlice := `<text role="separator" padding="1">|</text>`

	// Remove the separator text (index 2 in this document's children order),
	// then mark the line dirty (the container whose child list changed).
	var kept []Node
	for _, c := range line0.Children {
		if tn, ok := c.(*TextNode); ok && tn.Role == "separator" {
			continue
		}
		kept = append(kept, c)
	}
	line0.Children = kept
	line0.Meta.Dirty = true

	out := SerializeMinimal(doc)
	if strings.Contains(out, sepSlice) {
		t.Errorf("removed separator still present:\n%s", out)
	}
	if !strings.Contains(out, `<field color="cyan" name="model"/>`) {
		t.Errorf("kept model field not reused verbatim:\n%s", out)
	}
	if _, diags := Parse(out); HasErrors(diags) {
		t.Errorf("minimal output not well-formed: %v\n%s", diags, out)
	}
}

// TestSerializeMinimal_NestedDirtyReconstructsAncestors ensures a dirty field
// inside a span reconstructs the span and line while reusing the span's other
// children and the sibling line verbatim.
func TestSerializeMinimal_NestedDirtyReconstructsAncestors(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code">
  <layout name="default" active="true">
    <line>
      <span color="bright-black" prefix=" (" suffix=")">
        <text>x</text>
        <field name="thinking-effort" color="yellow"/>
      </span>
    </line>
    <line>
      <field name="model"/>
    </line>
  </layout>
</statusloom>
`
	doc := parseClean(t, src)
	span := doc.Root.Layouts[0].Lines[0].Children[0].(*SpanNode)
	field := span.Children[1].(*FieldNode)
	textSlice := span.Children[0].(*TextNode).Meta.SourceRange.Slice(src)
	line1Slice := doc.Root.Layouts[0].Lines[1].Meta.SourceRange.Slice(src)

	field.Common.Style.Color = "red"
	field.Meta.Dirty = true

	out := SerializeMinimal(doc)
	if !strings.Contains(out, textSlice) {
		t.Errorf("span's unchanged text child not reused verbatim:\n%s", out)
	}
	if !strings.Contains(out, `color="red"`) {
		t.Errorf("dirty field regeneration missing:\n%s", out)
	}
	if !strings.Contains(out, line1Slice) {
		t.Errorf("untouched sibling line not reused verbatim:\n%s", out)
	}
	// The span open tag is reconstructed canonically but its attributes are
	// unchanged, so it must still carry the prefix/suffix.
	if !strings.Contains(out, `prefix=" ("`) || !strings.Contains(out, `suffix=")"`) {
		t.Errorf("reconstructed span lost its attributes:\n%s", out)
	}
	if _, diags := Parse(out); HasErrors(diags) {
		t.Errorf("minimal output not well-formed: %v\n%s", diags, out)
	}
}

func TestSerializeMinimal_Nil(t *testing.T) {
	if SerializeMinimal(nil) != "" {
		t.Error("SerializeMinimal(nil) should be empty")
	}
	if SerializeMinimal(&Document{}) != "" {
		t.Error("SerializeMinimal(rootless) should be empty")
	}
}

// --- helpers ---

func findField(t *testing.T, line *LineNode, name string) *FieldNode {
	t.Helper()
	for _, c := range line.Children {
		if f, ok := c.(*FieldNode); ok && f.Name == name {
			return f
		}
	}
	t.Fatalf("field %q not found in line", name)
	return nil
}

// Lines0Field returns the first field child of a line (test convenience).
func (l *LineNode) Lines0Field(t *testing.T) *FieldNode {
	t.Helper()
	for _, c := range l.Children {
		if f, ok := c.(*FieldNode); ok {
			return f
		}
	}
	t.Fatalf("no field child in line")
	return nil
}
