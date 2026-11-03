package dsl

import (
	"strings"
	"testing"
)

// TestSerializeField_SourceAttrsCanonicalOrder pins the canonical attribute
// order for the source/select/fallback attributes: name, var, source, select,
// fallback, then format. Input in a different order must come out canonical.
func TestSerializeField_SourceAttrsCanonicalOrder(t *testing.T) {
	src := wrap(`<field fallback="model" format="number" select="a.b[0]" source="gh" name="source" var="v" bold="true"/>`)
	out := Serialize(parseClean(t, src))

	want := `<field name="source" var="v" source="gh" select="a.b[0]" fallback="model" format="number" bold="true"/>`
	if !strings.Contains(out, want) {
		t.Fatalf("canonical field not found.\nwant substring: %s\ngot:\n%s", want, out)
	}
}

// TestSerializeField_SourceAttrsEscapingRoundTrip checks that values with XML
// specials are escaped on output, parse back identically, and that a second
// serialize is byte-identical (idempotent).
func TestSerializeField_SourceAttrsEscapingRoundTrip(t *testing.T) {
	src := wrap(`<field name="source" source="a&amp;b" select="x[?(@.k==&quot;a&amp;b&quot;)] &lt; ${HOME}" fallback="&lt;model&gt;"/>`)

	doc := parseClean(t, src)
	f := onlyLine(t, doc).Children[0].(*FieldNode)
	if f.Select != `x[?(@.k=="a&b")] < ${HOME}` {
		t.Fatalf("Select decoded wrong: %q", f.Select)
	}

	out1 := Serialize(doc)
	if !strings.Contains(out1, `select="x[?(@.k==&quot;a&amp;b&quot;)] &lt; ${HOME}"`) {
		t.Fatalf("Select not escaped as expected:\n%s", out1)
	}

	doc2 := parseClean(t, out1)
	f2 := onlyLine(t, doc2).Children[0].(*FieldNode)
	if f2.Source != f.Source || f2.Select != f.Select || f2.Fallback != f.Fallback {
		t.Fatalf("round-trip mismatch: got source=%q select=%q fallback=%q",
			f2.Source, f2.Select, f2.Fallback)
	}

	out2 := Serialize(doc2)
	if out1 != out2 {
		t.Fatalf("Serialize not idempotent:\n--- first\n%s\n--- second\n%s", out1, out2)
	}
}

// TestSerializeField_EmptySourceAttrsOmitted ensures empty source/select/
// fallback attributes are not written, so a bare name="source" field stays bare.
func TestSerializeField_EmptySourceAttrsOmitted(t *testing.T) {
	src := wrap(`<field name="source" source="" select="" fallback=""/>`)
	out := Serialize(parseClean(t, src))

	if !strings.Contains(out, `<field name="source"/>`) {
		t.Fatalf("empty attributes were not omitted:\n%s", out)
	}
	if strings.Contains(out, `source=""`) || strings.Contains(out, `select=""`) || strings.Contains(out, `fallback=""`) {
		t.Fatalf("empty attribute emitted:\n%s", out)
	}
}

// TestSerializeMinimal_SelectChangeEmitted verifies that editing only Select on
// a dirty source field is written through the minimal-diff serializer.
func TestSerializeMinimal_SelectChangeEmitted(t *testing.T) {
	doc := parseClean(t, wrap(`<field name="source" source="gh" select="a.b"/>`))
	line0 := doc.Root.Layouts[0].Children[0].(*LineNode)
	f := findField(t, line0, "source")

	f.Select = "c.d"
	f.Meta.Dirty = true

	out := SerializeMinimal(doc)
	if !strings.Contains(out, `<field name="source" source="gh" select="c.d"/>`) {
		t.Fatalf("changed select not emitted by SerializeMinimal:\n%s", out)
	}
}
