package webconfig

// Tests for the DSL's <subagent> region (markup.md "subagent";
// plans/subagent-region-dsl.md) at the webconfig layer: the merged
// "claude-code" field/metric catalog (task-* fields folded in) and the
// section="subagent" preview mode (subagentPreview, one row per task, keyed
// by container node ID). Parser/validator/serializer/render/CLI coverage for
// <subagent> itself lives in the dsl/render/cli packages.

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

// TestKnownTool_RejectsUnknownTool documents that knownTool's relaxation is
// scoped to exactly the one supported tool, not "anything goes".
func TestKnownTool_RejectsUnknownTool(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/document?tool=codex")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unknown tool", resp.StatusCode)
	}
}

// TestDSLFields_ClaudeCode_IncludesMergedTaskFields verifies GET
// /api/dsl/fields?tool=claude-code carries the task-* fields the merged
// catalog folds in (internal/dsl/registry.go's concatFields), matching the
// registry exactly, with task-effort still tagged capability=subagent-effort.
func TestDSLFields_ClaudeCode_IncludesMergedTaskFields(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/fields?tool=claude-code")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body fieldsResponse
	if err := decodeJSON(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	reg := dsl.Fields("claude-code")
	if len(body.Fields) != len(reg) {
		t.Fatalf("fields count = %d, want %d (registry)", len(body.Fields), len(reg))
	}

	sawTask, sawEffort := false, false
	for _, f := range body.Fields {
		if strings.HasPrefix(f.Name, "task-") {
			sawTask = true
		}
		if f.Name == "task-effort" {
			sawEffort = true
			if f.Capability != "subagent-effort" {
				t.Errorf("task-effort capability = %q, want subagent-effort", f.Capability)
			}
		}
	}
	if !sawTask {
		t.Error("no task-* field found in the claude-code field catalog")
	}
	if !sawEffort {
		t.Fatal("task-effort not present in the claude-code field catalog")
	}
}

// TestDSLFields_ClaudeCode_TaskFieldPreviewRendersAgainstSubagentSample
// verifies that the palette preview for a merged task-* field is not the
// "(no sample)" fallback: handleDSLFields's default sample (fullSample) must
// carry a Subagent task so task-* fields render real content end-to-end
// through the API, not just at the samples.go unit level.
func TestDSLFields_ClaudeCode_TaskFieldPreviewRendersAgainstSubagentSample(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/fields?tool=claude-code")
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

// TestDSLPreview_MainSection_IgnoresSubagentRegion confirms that previewing
// the default document with section unset (the main pass) never renders its
// <subagent> region's content and never returns subagentPreview — matching
// the real "claude" render pass exactly (markup.md "subagent" 2パス描画).
func TestDSLPreview_MainSection_IgnoresSubagentRegion(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/document?tool=claude-code")
	var doc struct {
		Source string `json:"source"`
	}
	_ = decodeJSON(resp.Body, &doc)
	resp.Body.Close()

	pv := putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code", "source": doc.Source, "width": 120, "sample": "full",
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
		SubagentPreview map[string]any `json:"subagentPreview"`
	}
	if err := decodeJSON(pv.Body, &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if body.SubagentPreview != nil {
		t.Errorf("subagentPreview = %+v, want absent for a main-section preview", body.SubagentPreview)
	}
	var rendered strings.Builder
	for _, ln := range body.Lines {
		for _, seg := range ln.Segments {
			if seg.Visible {
				rendered.WriteString(seg.Text)
			}
		}
	}
	if strings.Contains(rendered.String(), "Review render pipeline changes") {
		t.Errorf("main preview = %q, must not render <subagent> content", rendered.String())
	}
}

// TestDSLPreview_SubagentSection_ReturnsPerContainerLines is the subagent-
// section counterpart of TestDSL_E2E_DocumentPreview: previewing the default
// document (a responsive-free layout, so its single container is the layout
// itself, "L0") with section="subagent" returns one subagentPreview row per
// sample task, each row's nodeId present in the parsed AST.
func TestDSLPreview_SubagentSection_ReturnsPerContainerLines(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/document?tool=claude-code")
	var doc struct {
		Source string `json:"source"`
	}
	_ = decodeJSON(resp.Body, &doc)
	resp.Body.Close()

	parseResp := putPOST(t, ts, "/api/dsl/parse", map[string]any{"source": doc.Source})
	var parsed struct {
		AST map[string]any `json:"ast"`
	}
	_ = decodeJSON(parseResp.Body, &parsed)
	parseResp.Body.Close()
	astIDs := map[string]bool{}
	collectIDs(parsed.AST, astIDs)
	if !astIDs["L0.s"] {
		t.Fatalf("AST missing the layout-direct subagent line id L0.s, present ids: %v", sortedKeysOf(astIDs))
	}

	pv := putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code", "source": doc.Source, "width": 120, "section": "subagent",
	})
	defer pv.Body.Close()
	if pv.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", pv.StatusCode)
	}
	var body struct {
		Lines           []any `json:"lines"`
		SubagentPreview map[string][]struct {
			Omitted  bool `json:"omitted"`
			Segments []struct {
				NodeID  string `json:"nodeId"`
				Text    string `json:"text"`
				Visible bool   `json:"visible"`
			} `json:"segments"`
		} `json:"subagentPreview"`
	}
	if err := decodeJSON(pv.Body, &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if len(body.Lines) != 0 {
		t.Errorf("lines = %+v, want empty for a subagent-section preview", body.Lines)
	}
	rows, ok := body.SubagentPreview["L0"]
	if !ok {
		t.Fatalf("subagentPreview missing container %q, got keys %v", "L0", subagentPreviewKeys(body.SubagentPreview))
	}
	if len(rows) != 3 {
		t.Fatalf("subagentPreview[L0] has %d rows, want 3 (one per sample task)", len(rows))
	}

	var firstRow strings.Builder
	sawNodeID := false
	for _, seg := range rows[0].Segments {
		if seg.Visible {
			firstRow.WriteString(seg.Text)
		}
		if seg.NodeID != "" {
			sawNodeID = true
			if !astIDs[seg.NodeID] {
				t.Errorf("subagent preview segment nodeId %q not present in AST", seg.NodeID)
			}
		}
	}
	if !sawNodeID {
		t.Error("no subagent preview segment carried a nodeId")
	}
	if !strings.Contains(firstRow.String(), "Review render pipeline changes") {
		t.Errorf("subagentPreview[L0][0] = %q, want the first sample task's description", firstRow.String())
	}
}

// TestDSLPreview_SubagentSection_ExplicitCompletedSample verifies
// "subagent-completed" is selectable explicitly for the subagent section, and
// that its (higher) token counts appear in the rendered rows.
func TestDSLPreview_SubagentSection_ExplicitCompletedSample(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/document?tool=claude-code")
	var doc struct {
		Source string `json:"source"`
	}
	_ = decodeJSON(resp.Body, &doc)
	resp.Body.Close()

	pv := putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code", "source": doc.Source, "width": 120,
		"section": "subagent", "sample": "subagent-completed",
	})
	defer pv.Body.Close()
	if pv.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, want 200", pv.StatusCode)
	}
	var body struct {
		SubagentPreview map[string][]struct {
			Segments []struct {
				Text    string `json:"text"`
				Visible bool   `json:"visible"`
			} `json:"segments"`
		} `json:"subagentPreview"`
	}
	if err := decodeJSON(pv.Body, &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	rows := body.SubagentPreview["L0"]
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	var rendered strings.Builder
	for _, row := range rows {
		for _, seg := range row.Segments {
			if seg.Visible {
				rendered.WriteString(seg.Text)
			}
		}
	}
	// The completed sample's first task reports 28663 tokens ("28.7k") vs the
	// running sample's 28454 ("28.5k"); at width 120 every width breakpoint is
	// met, so the token stat is present.
	if !strings.Contains(rendered.String(), "28.7k") {
		t.Errorf("rendered = %q, want it to contain the completed sample's token count (28.7k)", rendered.String())
	}
}

// TestDSLPreview_SubagentSection_UnknownSample verifies an unrecognized
// sample name is rejected with 400, same as the main section.
func TestDSLPreview_SubagentSection_UnknownSample(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	resp := authedGet(t, ts, "/api/dsl/document?tool=claude-code")
	var doc struct {
		Source string `json:"source"`
	}
	_ = decodeJSON(resp.Body, &doc)
	resp.Body.Close()

	pv := putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code", "source": doc.Source, "width": 120,
		"section": "subagent", "sample": "nonexistent",
	})
	defer pv.Body.Close()
	if pv.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unknown sample", pv.StatusCode)
	}
}

// responsiveSubagentDoc places a <subagent> region inside one <responsive>
// variant, leaves the other variant without one, and gives the layout its
// own <subagent> too — exercising subagentContainers' variant -> layout
// fallback (render.RenderSubagentLine's resolveSubagentRegion rule) alongside
// a variant that has its own region.
const responsiveSubagentDoc = `<statusloom version="1" tool="claude-code">
  <layout name="Default" active="true">
    <responsive>
      <variant>
        <line>
          <field name="model"/>
          <flex/>
        </line>
        <subagent>
          <line>
            <field name="task-description"/>
            <flex/>
            <field name="task-tokens"/>
          </line>
        </subagent>
      </variant>
      <variant>
        <line>
          <field name="model"/>
        </line>
      </variant>
    </responsive>
    <subagent>
      <line>
        <field name="task-model"/>
      </line>
    </subagent>
  </layout>
</statusloom>
`

// TestDSLPreview_SubagentSection_ResponsiveKeyedByVariantIDs verifies that
// with a <responsive> layout, subagentPreview is keyed by each variant's own
// node ID (allVariants:true so every candidate is included): variant 0's own
// <subagent> renders under its own container id and its field ids nest under
// its own "L0.0.v0.s", while variant 1 (no <subagent> of its own) falls back
// to the layout-level region — same content, but its field ids are the
// layout-level "L0.s" (the shared underlying AST node), not a
// variant-specific id.
func TestDSLPreview_SubagentSection_ResponsiveKeyedByVariantIDs(t *testing.T) {
	isolateConfigAndCache(t)
	ts := startTestServer(t, time.Hour)

	pv := putPOST(t, ts, "/api/dsl/preview", map[string]any{
		"tool": "claude-code", "source": responsiveSubagentDoc, "width": 120,
		"section": "subagent", "allVariants": true,
	})
	defer pv.Body.Close()
	if pv.StatusCode != http.StatusOK {
		body := map[string]any{}
		_ = decodeJSON(pv.Body, &body)
		t.Fatalf("preview status = %d, want 200, body = %+v", pv.StatusCode, body)
	}
	var body struct {
		SubagentPreview map[string][]struct {
			Segments []struct {
				NodeID  string `json:"nodeId"`
				Text    string `json:"text"`
				Visible bool   `json:"visible"`
			} `json:"segments"`
		} `json:"subagentPreview"`
	}
	if err := decodeJSON(pv.Body, &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}

	v0, ok := body.SubagentPreview["L0.0.v0"]
	if !ok || len(v0) != 3 {
		t.Fatalf("subagentPreview[L0.0.v0] = %+v (present=%v), want 3 rows", v0, ok)
	}
	for _, seg := range v0[0].Segments {
		if seg.Visible && strings.HasPrefix(seg.NodeID, "L0.0.v0.s") == false && seg.NodeID != "" {
			t.Errorf("v0 segment nodeId = %q, want it under L0.0.v0.s.*", seg.NodeID)
		}
	}

	v1, ok := body.SubagentPreview["L0.0.v1"]
	if !ok || len(v1) != 3 {
		t.Fatalf("subagentPreview[L0.0.v1] = %+v (present=%v), want 3 rows (layout-level fallback)", v1, ok)
	}
	sawLayoutFallbackID := false
	for _, seg := range v1[0].Segments {
		if seg.NodeID == "L0.s.0" {
			sawLayoutFallbackID = true
		}
	}
	if !sawLayoutFallbackID {
		t.Error("v1 (no own <subagent>) does not carry the layout-level fallback's field id L0.s.0")
	}
}

// subagentPreviewKeys is a small helper for readable failure messages.
func subagentPreviewKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
