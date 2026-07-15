package webconfig

// Tests for the second tool, tool="claude-code-subagent" (the
// subagentStatusLine DSL surface, see subagent-spec.md and DSL_API.md). These
// exercise the webconfig layer only: knownTool relaxation, the field/metric
// catalog for task-* fields, and the document/preview endpoints rendering
// against a subagent-shaped sample. Adapter/schema/registry/CLI coverage
// lives in their own packages.

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
)

// isolateConfigAndCache points STATUSLOOM_CONFIG / STATUSLOOM_CACHE_DIR at
// fresh per-test temp directories, so tests that GET/PUT documents never
// touch the real user config directory.
func isolateConfigAndCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
}

func TestDSLFields_SubagentTool_ReturnsTaskFields(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/fields?tool=claude-code-subagent")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body fieldsResponse
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	reg := dsl.Fields("claude-code-subagent")
	if len(body.Fields) != len(reg) {
		t.Fatalf("fields count = %d, want %d (registry)", len(body.Fields), len(reg))
	}
	if len(body.Fields) == 0 {
		t.Fatal("fields = [], want the task-* catalog")
	}
	for _, f := range body.Fields {
		if !strings.HasPrefix(f.Name, "task-") {
			t.Errorf("field %q does not have the task- prefix expected for claude-code-subagent", f.Name)
		}
	}

	// task-effort carries Capability "subagent-effort" today (unavailable in
	// every environment), matching the oauth-usage fields' pass-through
	// pattern: the server tags the capability, the client decides whether to
	// hide it.
	found := false
	for _, f := range body.Fields {
		if f.Name == "task-effort" {
			found = true
			if f.Capability != "subagent-effort" {
				t.Errorf("task-effort capability = %q, want subagent-effort", f.Capability)
			}
		}
	}
	if !found {
		t.Fatal("task-effort not present in claude-code-subagent field catalog")
	}
}

func TestDSLMetrics_SubagentTool_ReturnsTaskMetrics(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/metrics?tool=claude-code-subagent")
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
	reg := dsl.Metrics("claude-code-subagent")
	if len(body.Metrics) != len(reg) {
		t.Fatalf("metrics count = %d, want %d", len(body.Metrics), len(reg))
	}
	for i, m := range body.Metrics {
		if m.Name != reg[i].Name || m.DisplayName == "" {
			t.Errorf("metric[%d] = %+v, want name %q with a displayName", i, m, reg[i].Name)
		}
	}
}

// TestDSLFields_SubagentTool_PreviewsRenderAgainstSubagentSample verifies
// that the palette preview for a task-* field is not the "(no sample)"
// fallback: handleDSLFields must pick a subagent-shaped base sample for
// tool="claude-code-subagent" (defaultSampleForTool), not the session-shaped
// "full" sample (which leaves Subagent nil).
func TestDSLFields_SubagentTool_PreviewsRenderAgainstSubagentSample(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/fields?tool=claude-code-subagent")
	defer resp.Body.Close()
	var body fieldsResponse
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	got := previewTextFor(body, "task-model")
	if got == "" || got == previewFallback {
		t.Fatalf("task-model preview = %q, want a non-fallback rendering", got)
	}
	if !strings.Contains(got, "Opus") {
		t.Errorf("task-model preview = %q, want it to contain Opus", got)
	}
}

func TestDSLDocument_SubagentTool_GetPutRoundTrip(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/document?tool=claude-code-subagent")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	var doc struct {
		Source string `json:"source"`
		Exists bool   `json:"exists"`
	}
	if err := decodeJSON(resp.Body, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Exists {
		t.Error("exists = true for a never-saved claude-code-subagent document")
	}
	if doc.Source == "" {
		t.Fatal("source is empty, want the built-in default document")
	}

	putResp := putJSON(t, ts, "/api/dsl/document", map[string]any{"tool": "claude-code-subagent", "source": doc.Source})
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", putResp.StatusCode)
	}
}

// TestDSLPreview_SubagentTool_RendersTaskFields is the subagent-tool
// counterpart of TestDSL_E2E_DocumentPreview: it previews the built-in
// claude-code-subagent default document with no explicit sample and
// verifies it renders against a subagent sample (Model/status text present)
// rather than coming back empty (which would happen against the session-
// shaped "full" sample, since Subagent is nil there).
func TestDSLPreview_SubagentTool_RendersTaskFields(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/document?tool=claude-code-subagent")
	var doc struct {
		Source string `json:"source"`
	}
	_ = decodeJSON(resp.Body, &doc)
	resp.Body.Close()

	pv := putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code-subagent", "source": doc.Source, "width": 120,
	})
	defer pv.Body.Close()
	if pv.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", pv.StatusCode)
	}
	var body struct {
		Lines []struct {
			Omitted  bool `json:"omitted"`
			Segments []struct {
				Text    string `json:"text"`
				Visible bool   `json:"visible"`
			} `json:"segments"`
		} `json:"lines"`
		Fallback struct {
			Active bool `json:"active"`
		} `json:"fallback"`
	}
	if err := decodeJSON(pv.Body, &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if body.Fallback.Active {
		t.Fatal("fallback.active = true, want the default subagent document to render real content")
	}

	var rendered strings.Builder
	for _, ln := range body.Lines {
		for _, seg := range ln.Segments {
			if seg.Visible {
				rendered.WriteString(seg.Text)
			}
		}
	}
	text := rendered.String()
	if !strings.Contains(text, "Opus") {
		t.Errorf("rendered preview = %q, want it to contain the task's model (Opus)", text)
	}
	if !strings.Contains(text, "Review render pipeline changes") {
		t.Errorf("rendered preview = %q, want it to contain the sample task's description", text)
	}
}

// TestDSLPreview_SubagentTool_ExplicitCompletedSample verifies the
// "subagent-completed" sample name is selectable explicitly (independent of
// the tool-based default), so the configurator can offer a
// running/completed toggle in its preview controls.
func TestDSLPreview_SubagentTool_ExplicitCompletedSample(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/document?tool=claude-code-subagent")
	var doc struct {
		Source string `json:"source"`
	}
	_ = decodeJSON(resp.Body, &doc)
	resp.Body.Close()

	pv := putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code-subagent", "source": doc.Source, "width": 120, "sample": "subagent-completed",
	})
	defer pv.Body.Close()
	if pv.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", pv.StatusCode)
	}
	var body struct {
		Lines []struct {
			Segments []struct {
				Text    string `json:"text"`
				Visible bool   `json:"visible"`
			} `json:"segments"`
		} `json:"lines"`
	}
	if err := decodeJSON(pv.Body, &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	var rendered strings.Builder
	for _, ln := range body.Lines {
		for _, seg := range ln.Segments {
			if seg.Visible {
				rendered.WriteString(seg.Text)
			}
		}
	}
	// The default document does not render the task status, so the completed
	// sample is distinguished from the running one by its (larger) token
	// count: the completed sample reports 28663 ("28.7k") vs the running
	// sample's 28454 ("28.5k"). At width 120 every width breakpoint is met,
	// so the token stat is present.
	if !strings.Contains(rendered.String(), "28.7k") {
		t.Errorf("rendered preview = %q, want it to contain the completed sample's token count (28.7k)", rendered.String())
	}
}

// TestKnownTool_RejectsUnknownTool documents that knownTool's relaxation is
// scoped to exactly the two supported tools, not "anything goes".
func TestKnownTool_RejectsUnknownTool(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/document?tool=codex")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unknown tool", resp.StatusCode)
	}
}
