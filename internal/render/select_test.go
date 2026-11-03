package render

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/yacchi/statusloom/internal/jsonselect"
	"github.com/yacchi/statusloom/internal/schema"
)

func decodeDoc(t *testing.T, src string) any {
	t.Helper()
	var doc any
	if err := json.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("unmarshal %q: %v", src, err)
	}
	return doc
}

func TestEvalSelectScalarDisplay(t *testing.T) {
	snap := schema.StatusSnapshot{}
	tests := []struct {
		name    string
		doc     string
		sel     string
		display string
	}{
		{"string", `{"profile":"work"}`, ".profile", "work"},
		{"number", `{"n":1500}`, "n", "1500"},
		{"bool", `{"on":true}`, "on", "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := EvalSelect(decodeDoc(t, tt.doc), tt.sel, snap, nil)
			if !res.Matched || res.Err != nil {
				t.Fatalf("Matched=%v Err=%v, want matched", res.Matched, res.Err)
			}
			if res.Display != tt.display {
				t.Errorf("Display = %q, want %q", res.Display, tt.display)
			}
		})
	}
}

// A JSON null at the path is matched, but has no value and no display text.
func TestEvalSelectJSONNullIsMatchedWithoutDisplay(t *testing.T) {
	res := EvalSelect(decodeDoc(t, `{"x":null}`), ".x", schema.StatusSnapshot{}, nil)
	if !res.Matched {
		t.Fatal("JSON null should count as matched")
	}
	if res.Value != nil {
		t.Errorf("Value = %v, want nil", res.Value)
	}
	if res.Display != "" {
		t.Errorf("Display = %q, want empty", res.Display)
	}
}

// Objects and arrays keep their value but never get a display string.
func TestEvalSelectCompositeHasNoDisplay(t *testing.T) {
	doc := decodeDoc(t, `{"o":{"a":1},"l":[1]}`)
	for _, sel := range []string{".o", ".l"} {
		res := EvalSelect(doc, sel, schema.StatusSnapshot{}, nil)
		if !res.Matched || res.Value == nil {
			t.Fatalf("%s: Matched=%v Value=%v, want matched composite", sel, res.Matched, res.Value)
		}
		if res.Display != "" {
			t.Errorf("%s: Display = %q, want empty", sel, res.Display)
		}
	}
}

func TestEvalSelectMissingPathIsNotAnError(t *testing.T) {
	res := EvalSelect(decodeDoc(t, `{}`), ".missing", schema.StatusSnapshot{}, nil)
	if res.Matched || res.Err != nil {
		t.Errorf("Matched=%v Err=%v, want unmatched with no error", res.Matched, res.Err)
	}
}

func TestEvalSelectSyntaxErrorIsUnwrapped(t *testing.T) {
	res := EvalSelect(decodeDoc(t, `{"a":1}`), ".a[", schema.StatusSnapshot{}, nil)
	if res.Matched {
		t.Fatal("syntax error must not report Matched")
	}
	var se *jsonselect.SyntaxError
	if !errors.As(res.Err, &se) {
		t.Fatalf("Err = %v (%T), want *jsonselect.SyntaxError", res.Err, res.Err)
	}
}

// An empty select is the root selector: the whole document is selected.
func TestEvalSelectEmptyIsWholeDocument(t *testing.T) {
	snap := schema.StatusSnapshot{}
	res := EvalSelect(decodeDoc(t, `{"a":1}`), "", snap, nil)
	if !res.Matched || res.Err != nil || res.Display != "" {
		t.Errorf("map doc: Matched=%v Err=%v Display=%q, want matched with empty display", res.Matched, res.Err, res.Display)
	}
	res = EvalSelect(decodeDoc(t, `"hi"`), "", snap, nil)
	if !res.Matched || res.Display != "hi" {
		t.Errorf("scalar doc: Matched=%v Display=%q, want matched display hi", res.Matched, res.Display)
	}
}

// Substituted values are literal: a session id with ], . and = must not
// re-parse as select syntax.
func TestEvalSelectSessionIDIsLiteral(t *testing.T) {
	doc := decodeDoc(t, `[{"id":"s]1.x=y","v":"hit"},{"id":"other","v":"no"}]`)
	snap := schema.StatusSnapshot{Session: schema.SessionSnapshot{ID: "s]1.x=y"}}
	res := EvalSelect(doc, "[id=${session_id}].v", snap, nil)
	if !res.Matched || res.Display != "hit" {
		t.Errorf("Matched=%v Display=%q, want hit", res.Matched, res.Display)
	}
}

func TestEvalSelectProjectDirVariable(t *testing.T) {
	doc := decodeDoc(t, `[{"p":"/repo","v":"hit"},{"p":"/other","v":"no"}]`)
	snap := schema.StatusSnapshot{System: schema.SystemSnapshot{ProjectDir: "/repo"}}
	res := EvalSelect(doc, "[p=${workspace.project_dir}].v", snap, nil)
	if res.Display != "hit" {
		t.Errorf("Display = %q, want hit", res.Display)
	}
}

// The injected getenv answers CLAUDE_CONFIG_DIR and nothing else; the real
// environment must not leak in when a getenv is supplied.
func TestEvalSelectInjectedGetenvIsUsed(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/real-env")
	doc := decodeDoc(t, `[{"configDir":"/p","profile":"work"},{"configDir":"/real-env","profile":"leak"}]`)
	getenv := func(name string) string {
		if name == "CLAUDE_CONFIG_DIR" {
			return "/p"
		}
		return ""
	}
	res := EvalSelect(doc, "[configDir=${CLAUDE_CONFIG_DIR}].profile", schema.StatusSnapshot{}, getenv)
	if res.Display != "work" {
		t.Errorf("Display = %q, want work (injected getenv, not the process environment)", res.Display)
	}
}

// A nil getenv must fall back to the process environment.
func TestEvalSelectNilGetenvReadsProcessEnv(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/from-env")
	doc := decodeDoc(t, `[{"c":"/from-env","p":"ok"}]`)
	res := EvalSelect(doc, "[c=${CLAUDE_CONFIG_DIR}].p", schema.StatusSnapshot{}, nil)
	if res.Display != "ok" {
		t.Errorf("Display = %q, want ok", res.Display)
	}
}

func TestSelectVarsUnknownNameIsEmpty(t *testing.T) {
	vars := SelectVars(schema.StatusSnapshot{}, func(string) string { return "x" })
	if got := vars("nope"); got != "" {
		t.Errorf("unknown var = %q, want empty", got)
	}
}
