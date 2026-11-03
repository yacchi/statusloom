package dsl

import "testing"

func TestFieldSourceAttrsParse(t *testing.T) {
	doc := parseClean(t, wrap(`<field name="source" source="weather" select="$.temp" fallback="model"/>`))
	f, ok := onlyLine(t, doc).Children[0].(*FieldNode)
	if !ok {
		t.Fatalf("child is not *FieldNode")
	}
	if f.Name != "source" || f.Source != "weather" || f.Select != "$.temp" || f.Fallback != "model" {
		t.Fatalf("got name=%q source=%q select=%q fallback=%q", f.Name, f.Source, f.Select, f.Fallback)
	}
}

func TestFieldSourceAttrsEntities(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{`a[?(@.x==&quot;1&quot; &amp;&amp; @.y)]`, `a[?(@.x=="1" && @.y)]`},
		{`&amp;`, `&`},
		{`&lt;`, `<`},
		{`&gt;`, `>`},
	}
	for _, c := range cases {
		doc := parseClean(t, wrap(`<field name="source" source="s" select="`+c.raw+`"/>`))
		f := onlyLine(t, doc).Children[0].(*FieldNode)
		if f.Select != c.want {
			t.Errorf("select %q: got %q, want %q", c.raw, f.Select, c.want)
		}
	}
}

func TestFieldSourceAttrsEmptyValues(t *testing.T) {
	doc := parseClean(t, wrap(`<field name="source" source="" select="" fallback=""/>`))
	f := onlyLine(t, doc).Children[0].(*FieldNode)
	if f.Source != "" || f.Select != "" || f.Fallback != "" {
		t.Fatalf("empty attrs not empty: source=%q select=%q fallback=%q", f.Source, f.Select, f.Fallback)
	}
}

func TestFieldSourceAttrsAbsent(t *testing.T) {
	doc := parseClean(t, wrap(`<field name="model"/>`))
	f := onlyLine(t, doc).Children[0].(*FieldNode)
	if f.Source != "" || f.Select != "" || f.Fallback != "" {
		t.Fatalf("absent attrs not empty: source=%q select=%q fallback=%q", f.Source, f.Select, f.Fallback)
	}
}

func TestFieldSourceAttrsWithOptional(t *testing.T) {
	doc := parseClean(t, wrap(`<field name="source" source="s" optional="source:s" format="number"/>`))
	f := onlyLine(t, doc).Children[0].(*FieldNode)
	if f.Source != "s" || f.Common.Optional != "source:s" {
		t.Fatalf("source=%q optional=%q", f.Source, f.Common.Optional)
	}
}
