package dsl

import (
	"reflect"
	"strings"
	"testing"
)

func TestPathAttr_Accepted(t *testing.T) {
	for _, src := range []string{
		`<field name="current-directory" path="base"/>`,
		`<field name="current-directory" path="home"/>`,
		`<field name="current-directory" path="full"/>`,
		`<field name="project-directory" path="home"/>`,
		`<field name="git-root" path="full"/>`,
	} {
		if diags := validateSrc(t, src); HasErrors(diags) {
			t.Errorf("%s: unexpected errors %v", src, diags)
		}
	}
}

func TestPathAttr_Rejected(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"unsupported field", `<field name="model" path="home"/>`, `does not take a path attribute`},
		{"unknown value", `<field name="current-directory" path="tilde"/>`, `invalid path "tilde"`},
		{"unknown value on project-directory", `<field name="project-directory" path="base2"/>`, `invalid path "base2"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateSrc(t, tc.body)
			d, ok := findDiag(diags, tc.want)
			if !ok {
				t.Fatalf("want error containing %q, got %v", tc.want, diags)
			}
			if d.Severity != SeverityError {
				t.Errorf("severity = %v, want error", d.Severity)
			}
		})
	}
}

// An absent path is never a diagnostic, even on a field that has none.
func TestPathAttr_AbsentIsSilent(t *testing.T) {
	if diags := validateSrc(t, `<field name="model"/>`); len(diags) != 0 {
		t.Errorf("unexpected diagnostics for a plain model field: %v", diags)
	}
}

func TestPathAttr_ParseAndSerializeRoundTrip(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code"><layout name="l" active="true"><line>` +
		`<field name="current-directory" path="home"/>` +
		`</line></layout></statusloom>`
	doc := parseClean(t, src)
	if f := onlyLine(t, doc).Children[0].(*FieldNode); f.Path != "home" {
		t.Fatalf("parsed path = %q, want home", f.Path)
	}
	out := Serialize(doc)
	if !strings.Contains(out, `path="home"`) {
		t.Fatalf("serialized output lost path:\n%s", out)
	}
	doc2 := parseClean(t, out)
	if f := onlyLine(t, doc2).Children[0].(*FieldNode); f.Path != "home" {
		t.Errorf("round-tripped path = %q, want home", f.Path)
	}
	n1 := normalizeForCompare(parseClean(t, src))
	n2 := normalizeForCompare(doc2)
	if !reflect.DeepEqual(n1, n2) {
		t.Errorf("path round-trip AST mismatch:\n%#v\n\n%#v", n1.Root, n2.Root)
	}
}

func TestPathAttr_DefaultsInRegistry(t *testing.T) {
	cur, ok := FieldByName("claude-code", "current-directory")
	if !ok || cur.PathDefault != PathBase || !reflect.DeepEqual(cur.PathStyles, []string{PathBase, PathHome, PathFull}) {
		t.Errorf("current-directory path metadata = %+v", cur)
	}
	for _, name := range []string{"project-directory", "git-root"} {
		fd, ok := FieldByName("claude-code", name)
		if !ok || fd.PathDefault != PathFull || len(fd.PathStyles) != 3 {
			t.Errorf("%s path metadata = %+v", name, fd)
		}
	}
	if model, _ := FieldByName("claude-code", "model"); len(model.PathStyles) != 0 || model.PathDefault != "" {
		t.Errorf("model must not take a path style: %+v", model)
	}
}
