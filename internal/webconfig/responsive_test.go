package webconfig

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
)

// respSource has a fixed line followed by a <responsive> with two variants:
// variant 0 is wide (a 30-char text plus a field), variant 1 is a single short
// line. Node IDs are therefore L0.0 (line), L0.1 (responsive), L0.1.v0 /
// L0.1.v1 (variants), L0.1.v0.0 / L0.1.v1.0 (their lines).
const respSource = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="Default" active="true">
    <line><field name="model"/></line>
    <responsive>
      <variant><line><text>WWWWWWWWWWWWWWWWWWWWWWWWWWWWWW</text><field name="session-cost"/></line></variant>
      <variant><line><text>N</text></line></variant>
    </responsive>
  </layout>
</statusloom>`

func TestBuildAST_ResponsiveShape(t *testing.T) {
	doc := mustParse(t, respSource)
	ast, _ := buildAST(doc)

	layouts := ast["layouts"].([]any)
	l0 := layouts[0].(map[string]any)
	if _, ok := l0["lines"]; ok {
		t.Errorf("layout must expose children, not lines: %v", l0)
	}
	children := l0["children"].([]any)
	if len(children) != 2 {
		t.Fatalf("layout children = %d, want 2", len(children))
	}

	line0 := children[0].(map[string]any)
	if line0["kind"] != "line" || line0["id"] != "L0.0" {
		t.Errorf("child 0 = %v, want kind=line id=L0.0", line0)
	}

	resp := children[1].(map[string]any)
	if resp["kind"] != "responsive" || resp["id"] != "L0.1" {
		t.Fatalf("child 1 = %v, want kind=responsive id=L0.1", resp)
	}
	variants := resp["variants"].([]any)
	if len(variants) != 2 {
		t.Fatalf("variants = %d, want 2", len(variants))
	}
	v0 := variants[0].(map[string]any)
	if v0["kind"] != "variant" || v0["id"] != "L0.1.v0" {
		t.Errorf("variant 0 = %v, want kind=variant id=L0.1.v0", v0)
	}
	v0lines := v0["lines"].([]any)
	v0line0 := v0lines[0].(map[string]any)
	if v0line0["kind"] != "line" || v0line0["id"] != "L0.1.v0.0" {
		t.Errorf("variant 0 line 0 = %v, want kind=line id=L0.1.v0.0", v0line0)
	}
	v0field := v0line0["children"].([]any)[1].(map[string]any)
	if v0field["id"] != "L0.1.v0.0.1" || v0field["name"] != "session-cost" {
		t.Errorf("variant 0 line 0 child 1 = %v, want id=L0.1.v0.0.1 name=session-cost", v0field)
	}
	v1 := variants[1].(map[string]any)
	if v1["id"] != "L0.1.v1" {
		t.Errorf("variant 1 id = %v, want L0.1.v1", v1["id"])
	}
}

func TestBuildAST_ResponsiveRoundTrip(t *testing.T) {
	doc := mustParse(t, respSource)
	want := dsl.Serialize(doc)
	got := dsl.Serialize(astJSONRoundTrip(t, doc))
	if got != want {
		t.Errorf("responsive AST JSON round trip changed the document:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestBuildAST_ResponsiveNodeIDs(t *testing.T) {
	doc := mustParse(t, respSource)
	ast, _ := buildAST(doc)
	raw, _ := json.Marshal(ast)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	ids := map[string]bool{}
	collectIDs(m, ids)
	for _, want := range []string{
		"L0", "L0.0", "L0.0.0", "L0.1", "L0.1.v0", "L0.1.v1",
		"L0.1.v0.0", "L0.1.v0.0.0", "L0.1.v0.0.1", "L0.1.v1.0", "L0.1.v1.0.0",
	} {
		if !ids[want] {
			t.Errorf("expected node ID %q in AST, present: %v", want, sortedKeysOf(ids))
		}
	}
}

// TestPreviewDSL_ResponsiveSelectsVariant asserts the preview emits segments
// only for the width-selected variant's line children, and the non-selected
// variant contributes no segments.
func TestPreviewDSL_ResponsiveSelectsVariant(t *testing.T) {
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	ts := startTestServer(t, time.Hour)

	preview := func(width int) map[string]bool {
		t.Helper()
		resp := putPOST(t, ts, "/api/dsl/preview", map[string]any{
			"tool": "claude-code", "source": respSource, "width": width, "sample": "full",
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("preview status = %d, want 200", resp.StatusCode)
		}
		var pv struct {
			Lines []struct {
				Segments []struct {
					NodeID string `json:"nodeId"`
				} `json:"segments"`
			} `json:"lines"`
		}
		if err := decodeJSON(resp.Body, &pv); err != nil {
			t.Fatalf("decode: %v", err)
		}
		ids := map[string]bool{}
		for _, ln := range pv.Lines {
			for _, seg := range ln.Segments {
				if seg.NodeID != "" {
					ids[seg.NodeID] = true
				}
			}
		}
		return ids
	}

	// Wide: variant 0 (the 30-char line) fits, variant 1 is not rendered.
	wide := preview(400)
	if !wide["L0.1.v0.0.0"] {
		t.Errorf("wide preview missing selected variant-0 segment L0.1.v0.0.0: %v", sortedKeysOf(wide))
	}
	for id := range wide {
		if strings.HasPrefix(id, "L0.1.v1") {
			t.Errorf("wide preview leaked non-selected variant-1 segment %q", id)
		}
	}

	// Narrow: variant 0 overflows, variant 1 is selected.
	narrow := preview(20)
	if !narrow["L0.1.v1.0.0"] {
		t.Errorf("narrow preview missing selected variant-1 segment L0.1.v1.0.0: %v", sortedKeysOf(narrow))
	}
	for id := range narrow {
		if strings.HasPrefix(id, "L0.1.v0") {
			t.Errorf("narrow preview leaked non-selected variant-0 segment %q", id)
		}
	}
}

// TestPreviewDSL_AllVariantsRendersEveryVariant asserts that with
// allVariants:true the preview emits segments for EVERY variant's lines
// (not just the width-selected one, unlike the default in
// TestPreviewDSL_ResponsiveSelectsVariant above) and reports the
// width-selected index per responsive id in selectedVariants.
func TestPreviewDSL_AllVariantsRendersEveryVariant(t *testing.T) {
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	ts := startTestServer(t, time.Hour)

	preview := func(width int) (map[string]bool, map[string]int) {
		t.Helper()
		resp := putPOST(t, ts, "/api/dsl/preview", map[string]any{
			"tool": "claude-code", "source": respSource, "width": width, "sample": "full",
			"allVariants": true,
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("preview status = %d, want 200", resp.StatusCode)
		}
		var pv struct {
			Lines []struct {
				Segments []struct {
					NodeID string `json:"nodeId"`
				} `json:"segments"`
			} `json:"lines"`
			SelectedVariants map[string]int `json:"selectedVariants"`
		}
		if err := decodeJSON(resp.Body, &pv); err != nil {
			t.Fatalf("decode: %v", err)
		}
		ids := map[string]bool{}
		for _, ln := range pv.Lines {
			for _, seg := range ln.Segments {
				if seg.NodeID != "" {
					ids[seg.NodeID] = true
				}
			}
		}
		return ids, pv.SelectedVariants
	}

	// Wide: variant 0 fits, but BOTH variants' segments are now present.
	wide, wideSelected := preview(400)
	if !wide["L0.1.v0.0.0"] {
		t.Errorf("wide preview missing variant-0 segment L0.1.v0.0.0: %v", sortedKeysOf(wide))
	}
	if !wide["L0.1.v1.0.0"] {
		t.Errorf("wide preview missing variant-1 segment L0.1.v1.0.0 (allVariants should include it): %v", sortedKeysOf(wide))
	}
	if wideSelected["L0.1"] != 0 {
		t.Errorf("wide selectedVariants[L0.1] = %d, want 0: %v", wideSelected["L0.1"], wideSelected)
	}

	// Narrow: variant 0 overflows and variant 1 is selected, but both still
	// contribute segments.
	narrow, narrowSelected := preview(20)
	if !narrow["L0.1.v0.0.0"] {
		t.Errorf("narrow preview missing variant-0 segment L0.1.v0.0.0 (allVariants should include it): %v", sortedKeysOf(narrow))
	}
	if !narrow["L0.1.v1.0.0"] {
		t.Errorf("narrow preview missing variant-1 segment L0.1.v1.0.0: %v", sortedKeysOf(narrow))
	}
	if narrowSelected["L0.1"] != 1 {
		t.Errorf("narrow selectedVariants[L0.1] = %d, want 1: %v", narrowSelected["L0.1"], narrowSelected)
	}
}
