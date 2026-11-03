package jsonselect

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return v
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		src     string
		message string
		offset  int
	}{
		{"${foo}", `unknown variable "foo"`, 0},
		{"${}", `unknown variable ""`, 0},
		{".a${session_id", `unterminated "${"`, 2},
		{".", `empty key after "."`, 1},
		{"a..b", `empty key after "."`, 2},
		{"[0", `unterminated "["`, 0},
		{"[]", "expected an index or key=value", 0},
		{"[abc]", "expected an index or key=value", 0},
		{"[${session_id}]", "expected an index or key=value", 0},
		{"[=v]", "empty key in filter", 0},
		{"[a.b=c]", `invalid character "." in filter key`, 2},
		{"a]", `unexpected "]"`, 1},
		{"a=b", `unexpected "="`, 1},
		{"[0]a", `expected "." or "["`, 3},
		{"[99999999999999999999]", "index too large", 0},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, err := Parse(tt.src)
			if err == nil {
				t.Fatalf("Parse(%q): want error", tt.src)
			}
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("Parse(%q): error %T is not *SyntaxError", tt.src, err)
			}
			if se.Message != tt.message || se.Offset != tt.offset {
				t.Fatalf("Parse(%q) = (%q, %d), want (%q, %d)", tt.src, se.Message, se.Offset, tt.message, tt.offset)
			}
		})
	}
}

func TestParseErrorString(t *testing.T) {
	_, err := Parse("${foo}")
	if got, want := err.Error(), `select: unknown variable "foo" (at offset 0)`; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestParseOK(t *testing.T) {
	// Each source must compile; the cases below cover the grammar's edge
	// forms (root, leading bare key, template-with-dot, "=" and "]" in values).
	for _, src := range []string{
		"",
		"a.b",
		".a.b",
		".sessions[0].profile",
		"[id=${session_id}].keepalive.expiresAt",
		".${workspace.project_dir}.name",
		"[url=http://x/?a=b.c]",
		"[k=]",
		". a",
		"a$b",
		"[007]",
	} {
		if _, err := Parse(src); err != nil {
			t.Errorf("Parse(%q): %v", src, err)
		}
	}
}

func TestEval(t *testing.T) {
	const doc = `{"s":[{"id":"a]b.c=d","p":1},{"id":42,"p":2},{"id":"42","p":3}],"n":null,"x":1}`
	vars := func(name string) string {
		switch name {
		case "session_id":
			return "a]b.c=d"
		case "cwd":
			return "/p/q"
		}
		return ""
	}

	tests := []struct {
		name   string
		expr   string
		doc    string
		vars   Vars
		want   any
		wantOK bool
	}{
		{"template value containing ] . = matches literally", ".s[id=${session_id}].p", doc, vars, float64(1), true},
		{"first match wins for numeric id", ".s[id=42].p", doc, nil, float64(2), true},
		{"index into array", ".s[2].p", doc, nil, float64(3), true},
		{"JSON null at path", ".n", doc, nil, nil, true},
		{"key under null", ".n.k", doc, nil, nil, false},
		{"index out of range", ".s[9]", doc, nil, nil, false},
		{"key on array", ".s.id", doc, nil, nil, false},
		{"index on object", "[0]", doc, nil, nil, false},
		{"key on scalar", ".x.y", doc, nil, nil, false},
		{"missing key", ".missing", doc, nil, nil, false},
		{"filter without match", ".s[id=]", doc, nil, nil, false},
		{"root expression", "", doc, nil, decode(t, doc), true},
		{"root of nil doc", "", "null", nil, nil, true},
		{"key of nil doc", ".a", "null", nil, nil, false},
		{"bare key equals dotted key", "s[2].p", doc, nil, float64(3), true},
		{"template key with dot", ".${workspace.project_dir}", `{"/p/q":7}`, func(n string) string {
			if n == "workspace.project_dir" {
				return "/p/q"
			}
			return ""
		}, float64(7), true},
		{"nil vars resolve to empty", ".${cwd}", `{"":5}`, nil, float64(5), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Parse(tt.expr)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.expr, err)
			}
			got, ok := e.Eval(decode(t, tt.doc), tt.vars)
			if ok != tt.wantOK {
				t.Fatalf("Eval(%q) ok = %v, want %v", tt.expr, ok, tt.wantOK)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Eval(%q) = %#v, want %#v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestEvalScalarMatching(t *testing.T) {
	const doc = `{"b":[{"k":null},{"on":"true","v":"x"},{"on":false,"v":1.5e3}]}`
	tests := []struct {
		name   string
		expr   string
		want   any
		wantOK bool
	}{
		{"bool matches string true", ".b[on=true].v", "x", true},
		{"bool false matches bool", ".b[on=false].v", float64(1500), true},
		{"number 1500 matches 1.5e3", ".b[v=1500].on", false, true},
		{"JSON null never matches", ".b[k=null]", nil, false},
		{"literal 1500.0 does not match canonical 1500", ".b[v=1500.0].on", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Parse(tt.expr)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.expr, err)
			}
			got, ok := e.Eval(decode(t, doc), nil)
			if ok != tt.wantOK {
				t.Fatalf("Eval(%q) ok = %v, want %v", tt.expr, ok, tt.wantOK)
			}
			if ok && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Eval(%q) = %#v, want %#v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestEvalUseNumber(t *testing.T) {
	// json.Number elements must compare by their canonical form too.
	dec := json.NewDecoder(bytes.NewReader([]byte(`{"b":[{"v":1.5e3,"name":"hit"}]}`)))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	e, err := Parse(".b[v=1500].name")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := e.Eval(doc, nil)
	if !ok || got != "hit" {
		t.Fatalf("Eval = (%#v, %v), want (\"hit\", true)", got, ok)
	}
}

func TestVars(t *testing.T) {
	tests := []struct {
		expr string
		want []string
	}{
		{"[a=${cwd}].${session_id}[b=${cwd}]", []string{"cwd", "session_id"}},
		{".a", nil},
		{"", nil},
	}
	for _, tt := range tests {
		e, err := Parse(tt.expr)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.expr, err)
		}
		if got := e.Vars(); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Vars(%q) = %v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestCanonical(t *testing.T) {
	tests := []struct {
		name   string
		in     any
		want   string
		wantOK bool
	}{
		{"integral float has no exponent", 1793632539.0, "1793632539", true},
		{"float exponent is expanded", 1.5e3, "1500", true},
		{"fraction", 0.25, "0.25", true},
		{"json.Number exponent", json.Number("1.5e3"), "1500", true},
		{"unparsable json.Number kept as is", json.Number("abc"), "abc", true},
		{"bool", true, "true", true},
		{"string", "x", "x", true},
		{"nil", nil, "", false},
		{"map", map[string]any{}, "", false},
		{"slice", []any{}, "", false},
		{"int is not a JSON scalar", int(1), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Canonical(tt.in)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("Canonical(%#v) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
