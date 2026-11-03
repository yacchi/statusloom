package webconfig

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
)

const sourceFieldSource = `<statusloom version="1" tool="claude-code"><layout name="Default"><line><field name="source" source="mybar" select="$.a.b" fallback="model"/><field name="model"/></line></layout></statusloom>`

// sourceFieldMaps builds the AST for sourceFieldSource and returns the two
// field maps (the source field at index 0 and the plain model field at 1).
func sourceFieldMaps(t *testing.T, doc *dsl.Document) (map[string]any, map[string]any) {
	t.Helper()
	ast, _ := buildAST(doc)
	layouts := ast["layouts"].([]any)
	line := layouts[0].(map[string]any)["children"].([]any)[0].(map[string]any)
	children := line["children"].([]any)
	return children[0].(map[string]any), children[1].(map[string]any)
}

func TestASTJSON_FieldSourceAttrs_RoundTrip(t *testing.T) {
	isolateClaudeConfig(t)
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	ts := startTestServer(t, time.Hour)

	doc := parseSourceFields(t, sourceFieldSource)
	src, _ := sourceFieldMaps(t, doc)

	want := map[string]string{"source": "mybar", "select": "$.a.b", "fallback": "model"}
	for k, v := range want {
		if got := src[k]; got != v {
			t.Fatalf("AST %s = %v, want %q", k, got, v)
		}
	}

	ast, _ := buildAST(doc)
	out := serializePost(t, ts, ast, nil)
	if !strings.Contains(out, `source="mybar"`) {
		t.Fatalf("serialized DSL lacks source attribute:\n%s", out)
	}

	doc2 := parseSourceFields(t, out)
	src2, _ := sourceFieldMaps(t, doc2)
	for k, v := range want {
		if got := src2[k]; got != v {
			t.Fatalf("after round trip AST %s = %v, want %q", k, got, v)
		}
	}
}

func TestASTJSON_FieldSourceAttrs_OmittedWhenEmpty(t *testing.T) {
	isolateClaudeConfig(t)
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	doc := parseSourceFields(t, sourceFieldSource)
	_, model := sourceFieldMaps(t, doc)
	if model["name"] != "model" {
		t.Fatalf("expected model field at index 1, got %v", model["name"])
	}
	for _, k := range []string{"source", "select", "fallback"} {
		if _, ok := model[k]; ok {
			t.Fatalf("field without source attrs emitted key %q", k)
		}
	}
}

func TestASTJSON_FieldSourceAttrs_EmptyInputIsUnset(t *testing.T) {
	isolateClaudeConfig(t)
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var j astNodeJSON
	if err := json.Unmarshal([]byte(`{"kind":"field","name":"model","source":""}`), &j); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	f, ok := jsonToNode(j).(*dsl.FieldNode)
	if !ok {
		t.Fatalf("jsonToNode did not return *dsl.FieldNode")
	}
	if f.Source != "" || f.Select != "" || f.Fallback != "" {
		t.Fatalf("empty/absent keys should be unset, got source=%q select=%q fallback=%q", f.Source, f.Select, f.Fallback)
	}
}

// parseSourceFields parses without validation: name="source" is registered by
// the field catalog, which this test does not depend on. Only parse
// diagnostics fail the test.
func parseSourceFields(t *testing.T, src string) *dsl.Document {
	t.Helper()
	doc, diags := dsl.Parse(src)
	if len(diags) > 0 {
		t.Fatalf("parse diagnostics: %v", diags)
	}
	return doc
}
