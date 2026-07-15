package webconfig

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
)

// richSource exercises most node kinds and attributes: tool settings, a git
// element, root and layout comments, text, raw text, spans, fields with
// formatters/hyperlink, color-rules, a flex, and two layouts.
const richSource = `<statusloom version="1" tool="claude-code" color-level="ansi16" output-style="powerline" compact-threshold="60" context-percentage-mode="usable" context-reserve-tokens="1000">
  <git cache-ttl-ms="3000" timeout-ms="200" include-untracked="true" collect-numstat="false"/>
  <!-- top comment -->
  <layout name="Default" active="true">
    <!-- layout comment -->
    <line>
      <text>Model: </text>
      <field name="model" color="cyan" bold="true"/>
      <span optional="thinking-effort" prefix=" (" suffix=")" color="bright-black">
        <field name="thinking-effort" color="yellow"/>
      </span>
      <text role="separator" padding="1">|</text>
      <field name="context-percentage-usable" format="percent" precision="0">
        <color-rule when="self ge 90" color="red"/>
        <color-rule when="self ge 70" color="yellow"/>
      </field>
      <flex size="full-minus-2"/>
      <field name="pr-number" hyperlink="true"/>
    </line>
    <line>
      Cost:
      <field name="session-cost" format="currency" currency="USD"/>
    </line>
  </layout>
  <layout name="Compact">
    <line>
      <field name="model"/>
    </line>
  </layout>
</statusloom>
`

func mustParse(t *testing.T, src string) *dsl.Document {
	t.Helper()
	doc, diags := dsl.Parse(src)
	if doc == nil || doc.Root == nil {
		t.Fatalf("parse failed: %+v", diags)
	}
	doc2 := doc
	if diags := dsl.Validate(doc2); dsl.HasErrors(diags) {
		t.Fatalf("validate errors: %+v", diags)
	}
	return doc
}

// astJSONRoundTrip marshals a doc to AST JSON and back through the exported
// jsonToDocument path, exactly as POST /api/dsl/serialize does.
func astJSONRoundTrip(t *testing.T, doc *dsl.Document) *dsl.Document {
	t.Helper()
	astMap, _ := buildAST(doc)
	raw, err := json.Marshal(astMap)
	if err != nil {
		t.Fatalf("marshal AST: %v", err)
	}
	var node astNodeJSON
	if err := json.Unmarshal(raw, &node); err != nil {
		t.Fatalf("unmarshal AST: %v", err)
	}
	got, err := jsonToDocument(node)
	if err != nil {
		t.Fatalf("jsonToDocument: %v", err)
	}
	return got
}

func TestDSL_ASTRoundTrip_SemanticEquivalence(t *testing.T) {
	for name, src := range map[string]string{
		"default": config.DefaultDocument("claude-code"),
		"rich":    richSource,
	} {
		t.Run(name, func(t *testing.T) {
			doc := mustParse(t, src)
			want := dsl.Serialize(doc)

			// Canonical serialize must be idempotent.
			if reparsed, _ := dsl.Parse(want); dsl.Serialize(reparsed) != want {
				t.Fatal("canonical serialize is not idempotent")
			}

			got := dsl.Serialize(astJSONRoundTrip(t, doc))
			if got != want {
				t.Errorf("AST JSON round trip changed the document:\n--- want ---\n%s\n--- got ---\n%s", want, got)
			}
		})
	}
}

func TestDSL_NodeIDs_DeterministicAndPathShaped(t *testing.T) {
	doc := mustParse(t, richSource)

	a, _ := buildAST(doc)
	b, _ := buildAST(doc)
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		t.Fatal("buildAST is not deterministic")
	}

	ids := map[string]bool{}
	collectIDs(a, ids)
	for _, want := range []string{
		"root", "git", "L0", "L0.0", "L0.0.0", "L0.0.1", "L0.0.2", "L0.0.2.0",
		"L0.0.3", "L0.0.4", "L0.0.4.cr0", "L0.0.4.cr1", "L0.0.5", "L0.0.6",
		"L0.1", "L1", "L1.0", "L1.0.0",
	} {
		if !ids[want] {
			t.Errorf("expected node ID %q in AST, present IDs: %v", want, sortedKeysOf(ids))
		}
	}
}

// collectIDs walks a decoded AST (map / []any) collecting every "id" string.
func collectIDs(v any, out map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		if id, ok := t["id"].(string); ok {
			out[id] = true
		}
		for _, child := range t {
			collectIDs(child, out)
		}
	case []any:
		for _, e := range t {
			collectIDs(e, out)
		}
	}
}

func sortedKeysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDSL_GetDocument_DefaultWhenMissing(t *testing.T) {
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	ts := startTestServer(t, time.Hour)

	resp := authedRequest(t, ts, "GET", "/api/dsl/document?tool=claude-code", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Source  string `json:"source"`
		Version string `json:"version"`
		Exists  bool   `json:"exists"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Exists {
		t.Error("exists = true, want false for a missing document")
	}
	if !strings.Contains(body.Source, "<statusloom") {
		t.Errorf("source is not the default document: %q", body.Source)
	}
	if body.Version == "" {
		t.Error("version empty")
	}
}

func TestDSL_PutDocument_ErrorNotSaved_ValidSaved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	ts := startTestServer(t, time.Hour)
	docPath := config.DocumentPath("claude-code")

	// An invalid document (unknown field) must be rejected with 409, no write.
	bad := `<statusloom version="1" tool="claude-code"><layout name="D" active="true"><line><field name="nope"/></line></layout></statusloom>`
	resp := putJSON(t, ts, "/api/dsl/document", map[string]any{"tool": "claude-code", "source": bad})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("invalid PUT status = %d, want 409", resp.StatusCode)
	}
	var errBody struct {
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	_ = decodeJSON(resp.Body, &errBody)
	resp.Body.Close()
	if len(errBody.Diagnostics) == 0 {
		t.Error("expected diagnostics on invalid PUT")
	}
	if _, err := os.Stat(docPath); !os.IsNotExist(err) {
		t.Errorf("invalid PUT wrote the document (stat err = %v), want absent", err)
	}

	// A valid document is saved (200) with the exact source bytes.
	good := config.DefaultDocument("claude-code")
	resp = putJSON(t, ts, "/api/dsl/document", map[string]any{"tool": "claude-code", "source": good})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid PUT status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
	saved, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("document not saved: %v", err)
	}
	if string(saved) != good {
		t.Errorf("saved document != submitted source")
	}
}

func TestDSL_PutDraft_InvalidStillSaved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	ts := startTestServer(t, time.Hour)

	bad := `<statusloom version="1" tool="claude-code"><layout name="D" active="true"><line><field name="nope"/></line></layout></statusloom>`
	resp := putJSON(t, ts, "/api/dsl/draft", map[string]any{"tool": "claude-code", "source": bad})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("draft PUT status = %d, want 200 (invalid drafts still share)", resp.StatusCode)
	}
	var body struct {
		Diagnostics []map[string]any `json:"diagnostics"`
	}
	_ = decodeJSON(resp.Body, &body)
	resp.Body.Close()
	if len(body.Diagnostics) == 0 {
		t.Error("expected diagnostics reported for the invalid draft")
	}
	saved, err := os.ReadFile(config.DraftDocumentPath("claude-code"))
	if err != nil {
		t.Fatalf("invalid draft not saved: %v", err)
	}
	if string(saved) != bad {
		t.Error("saved draft != submitted source")
	}
}

func TestDSL_Fields_FromRegistry(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	resp := authedRequest(t, ts, "GET", "/api/dsl/fields?tool=claude-code", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Fields []struct {
			Name         string `json:"name"`
			DisplayName  string `json:"displayName"`
			Category     string `json:"category"`
			SelfMetric   string `json:"selfMetric"`
			Descriptions struct {
				EN, JA string
			} `json:"descriptions"`
			Preview struct {
				Text string `json:"text"`
			} `json:"preview"`
		} `json:"fields"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	reg := dsl.Fields("claude-code")
	if len(body.Fields) != len(reg) {
		t.Fatalf("fields count = %d, want %d (registry)", len(body.Fields), len(reg))
	}
	for i, f := range body.Fields {
		if f.Name != reg[i].Name {
			t.Errorf("field[%d].name = %q, want %q (registry order)", i, f.Name, reg[i].Name)
		}
		if f.DisplayName == "" || f.Descriptions.EN == "" || f.Descriptions.JA == "" || f.Category == "" {
			t.Errorf("field %q missing registry metadata: %+v", f.Name, f)
		}
	}
}

func TestDSL_Metrics_FromRegistry(t *testing.T) {
	ts := startTestServer(t, time.Hour)
	resp := authedRequest(t, ts, "GET", "/api/dsl/metrics?tool=claude-code", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Metrics []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"metrics"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	reg := dsl.Metrics("claude-code")
	if len(body.Metrics) != len(reg) {
		t.Fatalf("metrics count = %d, want %d", len(body.Metrics), len(reg))
	}
	for i, m := range body.Metrics {
		if m.Name != reg[i].Name || m.DisplayName == "" {
			t.Errorf("metric[%d] = %+v, want name %q with a displayName", i, m, reg[i].Name)
		}
	}
}

// TestDSL_E2E_DocumentPreview is the smoke test the task requires: GET/PUT the
// document, then preview it, asserting every preview segment's nodeId matches a
// node in the parsed AST.
func TestDSL_E2E_DocumentPreview(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	// GET default, PUT it back (persist), then preview it.
	resp := authedRequest(t, ts, "GET", "/api/dsl/document?tool=claude-code", nil)
	var doc struct {
		Source string `json:"source"`
	}
	_ = decodeJSON(resp.Body, &doc)
	resp.Body.Close()

	resp = putJSON(t, ts, "/api/dsl/document", map[string]any{"tool": "claude-code", "source": doc.Source})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Parse to learn the AST node-ID set.
	resp = putPOST(t, ts, "/api/dsl/parse", map[string]any{"source": doc.Source})
	var parsed struct {
		AST map[string]any `json:"ast"`
	}
	_ = decodeJSON(resp.Body, &parsed)
	resp.Body.Close()
	if parsed.AST == nil {
		t.Fatal("parse returned no AST")
	}
	astIDs := map[string]bool{}
	collectIDs(parsed.AST, astIDs)

	// Preview and check every segment nodeId is an AST node (empty allowed for
	// decoration segments with no owning node).
	resp = putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code", "source": doc.Source, "width": 120, "sample": "full",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", resp.StatusCode)
	}
	var pv struct {
		Lines []struct {
			Segments []struct {
				NodeID  string `json:"nodeId"`
				Visible bool   `json:"visible"`
			} `json:"segments"`
		} `json:"lines"`
	}
	if err := decodeJSON(resp.Body, &pv); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	resp.Body.Close()
	if len(pv.Lines) == 0 {
		t.Fatal("preview returned no lines")
	}
	sawContent := false
	for _, ln := range pv.Lines {
		for _, seg := range ln.Segments {
			if seg.NodeID == "" {
				continue
			}
			sawContent = true
			if !astIDs[seg.NodeID] {
				t.Errorf("preview segment nodeId %q not present in AST", seg.NodeID)
			}
		}
	}
	if !sawContent {
		t.Error("no preview segment carried a nodeId")
	}
}

// putJSON PUTs a JSON body and returns the response (caller closes Body).
func putJSON(t *testing.T, ts *testServer, path string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return authedRequest(t, ts, "PUT", path, b)
}

func putPOST(t *testing.T, ts *testServer, path string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return authedRequest(t, ts, "POST", path, b)
}
