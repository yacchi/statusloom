package webconfig

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestASTJSON_Refresh_RoundTrip: the <refresh/> element reaches the AST JSON
// and survives both the canonical and the minimal-diff serializer, so saving
// from the visual editor (which has no refresh panel) never drops it.
func TestASTJSON_Refresh_RoundTrip(t *testing.T) {
	isolateClaudeConfig(t)
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	ts := startTestServer(t, time.Hour)

	const src = `<statusloom version="1" tool="claude-code">
  <refresh wait="git source:mybar" timeout-ms="1200" wait-on="due"/>
  <layout name="Default"><line><field name="git-branch"/></line></layout>
</statusloom>
`
	doc := parseSourceFields(t, src)
	ast, _ := buildAST(doc)
	r, ok := ast["refresh"].(map[string]any)
	if !ok {
		t.Fatalf("AST has no refresh node: %v", ast)
	}
	if r["id"] != "refresh" || r["kind"] != "refresh" || r["wait"] != "git source:mybar" || r["timeout-ms"] != 1200 || r["wait-on"] != "due" {
		t.Fatalf("refresh node = %v", r)
	}

	const want = `<refresh wait="git source:mybar" timeout-ms="1200" wait-on="due"/>`
	if out := serializePost(t, ts, ast, nil); !strings.Contains(out, want) {
		t.Fatalf("canonical serialize lost <refresh>:\n%s", out)
	}
	base := src
	if out := serializePost(t, ts, ast, &base); out != src {
		t.Fatalf("minimal serialize of a clean AST is not byte-identical:\n%s", out)
	}

	noRefresh, _ := buildAST(parseSourceFields(t, `<statusloom version="1" tool="claude-code"><layout name="Default"><line><field name="model"/></line></layout></statusloom>`))
	if _, ok := noRefresh["refresh"]; ok {
		t.Fatalf("document without <refresh> emitted a refresh key")
	}
}
