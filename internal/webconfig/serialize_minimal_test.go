package webconfig

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
)

// serializePost posts {ast, baseSource?} to /api/dsl/serialize and returns the
// resulting source.
func serializePost(t *testing.T, ts *testServer, ast map[string]any, baseSource *string) string {
	t.Helper()
	body := map[string]any{"ast": ast}
	if baseSource != nil {
		body["baseSource"] = *baseSource
	}
	resp := putPOST(t, ts, "/api/dsl/serialize", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("serialize status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Source string `json:"source"`
	}
	if err := decodeJSON(resp.Body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out.Source
}

// modelField navigates a buildAST map to layout 0, line 0, child 1 (the
// <field name="model"> in richSource).
func modelFieldMap(t *testing.T, ast map[string]any) map[string]any {
	t.Helper()
	layouts := ast["layouts"].([]any)
	l0 := layouts[0].(map[string]any)
	lines := l0["lines"].([]any)
	line0 := lines[0].(map[string]any)
	children := line0["children"].([]any)
	f := children[1].(map[string]any)
	if f["name"] != "model" {
		t.Fatalf("expected model field at L0.0.1, got %v", f["name"])
	}
	return f
}

func TestDSLSerialize_BaseSource_NoDirtyIsByteIdentical(t *testing.T) {
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	ts := startTestServer(t, time.Hour)

	doc := mustParse(t, richSource)
	ast, _ := buildAST(doc)

	base := richSource
	got := serializePost(t, ts, ast, &base)
	if got != richSource {
		t.Errorf("serialize with baseSource and no dirty changed the document:\n--- got ---\n%s", got)
	}
}

func TestDSLSerialize_BaseSource_OnlyDirtyRegenerated(t *testing.T) {
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	ts := startTestServer(t, time.Hour)

	doc := mustParse(t, richSource)
	ast, _ := buildAST(doc)

	// Edit the model field: mark it dirty and change its color.
	f := modelFieldMap(t, ast)
	f["dirty"] = true
	f["color"] = "green"

	base := richSource
	got := serializePost(t, ts, ast, &base)

	// Unchanged nodes (comments, separator text, second layout) survive verbatim.
	for _, want := range []string{
		"<!-- top comment -->",
		"<!-- layout comment -->",
		`<text role="separator" padding="1">|</text>`,
		`<field name="pr-number" hyperlink="true"/>`,
		`<layout name="Compact">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("unchanged fragment %q not preserved verbatim:\n%s", want, got)
		}
	}
	// The dirty field is regenerated with the new color.
	if !strings.Contains(got, `color="green"`) {
		t.Errorf("dirty field's new color missing:\n%s", got)
	}
	// The document must still re-parse cleanly.
	if _, diags := parseAndValidateSource(got); dsl.HasErrors(diags) {
		t.Errorf("minimal serialize output not valid: %v", diags)
	}
}
