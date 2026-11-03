package webconfig

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupSourcesConfig isolates config, cache and Claude Code files, then writes
// a sources.json into the STATUSLOOM_CONFIG directory (which configDir resolves
// to, because t.TempDir() already exists as a directory).
func setupSourcesConfig(t *testing.T, sourcesJSON string) {
	t.Helper()
	isolateClaudeConfig(t)
	cfgDir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", cfgDir)
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(cfgDir, "sources.json"), []byte(sourcesJSON), 0o600); err != nil {
		t.Fatalf("write sources.json: %v", err)
	}
}

// previewSegmentTexts returns every segment text in a /api/dsl/preview
// response's main lines.
func previewSegmentTexts(t *testing.T, body []byte) []string {
	t.Helper()
	var out struct {
		Lines []struct {
			Segments []struct {
				Text string `json:"text"`
			} `json:"segments"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	var texts []string
	for _, l := range out.Lines {
		for _, s := range l.Segments {
			texts = append(texts, s.Text)
		}
	}
	return texts
}

func previewDoc(t *testing.T, ts *testServer, source string) []byte {
	t.Helper()
	resp := putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code", "source": source, "width": 120, "sample": "full",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", resp.StatusCode)
	}
	var raw json.RawMessage
	if err := decodeJSON(resp.Body, &raw); err != nil {
		t.Fatalf("decode preview body: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return raw
}

func TestDSLPreview_FileSourceShowsValue(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(dataPath, []byte(`{"name":"alpha"}`), 0o600); err != nil {
		t.Fatalf("write data: %v", err)
	}
	setupSourcesConfig(t, `{"schemaVersion":1,"sources":{"ccfile":{"kind":"file","path":`+jsonString(dataPath)+`}}}`)
	ts := startTestServer(t, time.Hour)

	doc := `<statusloom version="1" tool="claude-code"><layout name="Default" active="true"><line><field name="source" source="ccfile" select="name"/></line></layout></statusloom>`
	texts := previewSegmentTexts(t, previewDoc(t, ts, doc))
	if !containsSubstring(texts, "alpha") {
		t.Errorf("preview segment texts = %q, want one containing %q", texts, "alpha")
	}
}

// A command source is never fetched by the preview. With no cache envelope its
// value is unavailable, so the field falls back to the model field and the
// command's stdout never appears.
func TestDSLPreview_NeverFetchedCommandSourceFallsBack(t *testing.T) {
	setupSourcesConfig(t, `{"schemaVersion":1,"sources":{"mycmd":{"kind":"command","argv":["echo","{\"x\":\"CMDOUT\"}"]}}}`)
	ts := startTestServer(t, time.Hour)

	withFallback := `<statusloom version="1" tool="claude-code"><layout name="Default" active="true"><line><field name="source" source="mycmd" select="x" fallback="model"/></line></layout></statusloom>`
	modelOnly := `<statusloom version="1" tool="claude-code"><layout name="Default" active="true"><line><field name="model"/></line></layout></statusloom>`

	got := previewSegmentTexts(t, previewDoc(t, ts, withFallback))
	if containsSubstring(got, "CMDOUT") {
		t.Errorf("preview ran or read command output: %q", got)
	}
	want := previewSegmentTexts(t, previewDoc(t, ts, modelOnly))
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("fallback preview = %q, want the model-only preview %q", got, want)
	}
}

func TestDSLCatalog_RequiresSourceAndParamOmittedWhenUnset(t *testing.T) {
	setupSourcesConfig(t, `{"schemaVersion":1,"sources":{}}`)
	ts := startTestServer(t, time.Hour)

	fields := entriesByName(t, ts, "/api/dsl/fields?tool=claude-code", "fields")
	if v, ok := fields["source"]["requiresSource"]; !ok || v != true {
		t.Errorf("source field requiresSource = %v (present=%v), want true", v, ok)
	}
	if _, ok := fields["model"]["requiresSource"]; ok {
		t.Error("model field has a requiresSource key; it must be omitted when false")
	}

	metrics := entriesByName(t, ts, "/api/dsl/metrics?tool=claude-code", "metrics")
	if v := metrics["source-stale"]["param"]; v != "source" {
		t.Errorf("source-stale param = %v, want \"source\"", v)
	}
	if _, ok := metrics["width"]["param"]; ok {
		t.Error("width metric has a param key; it must be omitted when empty")
	}
}

func TestDSLPreview_SubagentSectionKeepsSources(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(dataPath, []byte(`{"name":"alpha"}`), 0o600); err != nil {
		t.Fatalf("write data: %v", err)
	}
	setupSourcesConfig(t, `{"schemaVersion":1,"sources":{"ccfile":{"kind":"file","path":`+jsonString(dataPath)+`}}}`)
	ts := startTestServer(t, time.Hour)

	doc := `<statusloom version="1" tool="claude-code"><layout name="Default" active="true"><line><field name="source" source="ccfile" select="name"/></line><subagent><line><field name="task-description"/></line></subagent></layout></statusloom>`
	resp := putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code", "source": doc, "width": 120, "section": "subagent",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subagent preview status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		SubagentPreview map[string]any `json:"subagentPreview"`
	}
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if body.SubagentPreview == nil {
		t.Error("subagentPreview missing from section=subagent response")
	}
}

// jsonString returns s as a JSON string literal (for embedding paths in sources.json).
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func containsSubstring(texts []string, sub string) bool {
	for _, t := range texts {
		if strings.Contains(t, sub) {
			return true
		}
	}
	return false
}

// entriesByName GETs a catalog endpoint and indexes each entry (by "name") as a
// generic map, so tests can check key presence.
func entriesByName(t *testing.T, ts *testServer, path, listKey string) map[string]map[string]any {
	t.Helper()
	resp := authedGet(t, ts, path)
	defer resp.Body.Close()
	var body map[string][]map[string]any
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	out := map[string]map[string]any{}
	for _, e := range body[listKey] {
		name, _ := e["name"].(string)
		out[name] = e
	}
	return out
}
