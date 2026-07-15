package render

import (
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/schema"
)

// TestMetricValue_Width covers the tool-agnostic "width" metric: it reports
// Options.Width verbatim when known, and an unbounded sentinel when the width
// is unknown (0), so a width breakpoint never hides content on a width-unaware
// host.
func TestMetricValue_Width(t *testing.T) {
	cfg := config.ToolConfig{}
	snap := schema.StatusSnapshot{}

	if v, ok := metricValue("width", snap, cfg, Options{Width: 120}); !ok || v != 120 {
		t.Errorf("width at 120 = %v,%v, want 120,true", v, ok)
	}
	if v, ok := metricValue("width", snap, cfg, Options{Width: 1}); !ok || v != 1 {
		t.Errorf("width at 1 = %v,%v, want 1,true", v, ok)
	}
	// Unknown width -> unbounded sentinel, still ok.
	v, ok := metricValue("width", snap, cfg, Options{Width: 0})
	if !ok {
		t.Fatalf("width at 0: ok=false, want true (unbounded)")
	}
	if v != widthUnbounded {
		t.Errorf("width at 0 = %v, want widthUnbounded (%d)", v, widthUnbounded)
	}
	// A negative width is treated the same as unknown.
	if v, ok := metricValue("width", snap, cfg, Options{Width: -5}); !ok || v != widthUnbounded {
		t.Errorf("width at -5 = %v,%v, want widthUnbounded,true", v, ok)
	}
}

// widthGateDoc is a minimal document whose single field is gated on
// when="width ge 80". The field renders the tool-version so its presence is
// easy to assert.
const widthGateDoc = `<statusloom version="1" tool="claude-code"><layout name="d" active="true"><line>` +
	`<field name="model"/><field name="tool-version" prefix=" v-gate:" when="width ge 80"/>` +
	`</line></layout></statusloom>`

// TestRenderDocument_WidthGate exercises a when="width ge 80" field across
// widths: hidden below the breakpoint, shown at or above it, and shown when
// the width is unknown (0 -> unbounded).
func TestRenderDocument_WidthGate(t *testing.T) {
	snap := fullSnapshot()
	cases := []struct {
		width int
		want  bool // whether the width-gated field should be visible
	}{
		{40, false},
		{79, false},
		{80, true},
		{120, true},
		{0, true}, // unknown width -> unbounded -> gate passes
	}
	for _, c := range cases {
		out := renderDocStr(t, widthGateDoc, snap, Options{Width: c.width, Now: fixedNow})
		got := strings.Contains(out, "v-gate:")
		if got != c.want {
			t.Errorf("width %d: gated field visible = %v, want %v (out=%q)", c.width, got, c.want, out)
		}
	}
}

// TestRenderDocument_WidthWithOptional confirms when= and optional= combine
// with AND semantics: the field shows only when BOTH the width breakpoint is
// met AND the optional field has data.
func TestRenderDocument_WidthWithOptional(t *testing.T) {
	const doc = `<statusloom version="1" tool="claude-code"><layout name="d" active="true"><line>` +
		`<field name="model"/>` +
		`<field name="session-cost" prefix=" cost:" optional="session-cost" when="width ge 80"/>` +
		`</line></layout></statusloom>`

	// Wide + cost present -> shown.
	withCost := fullSnapshot()
	withCost.Session.Cost = money(1.23)
	if out := renderDocStr(t, doc, withCost, Options{Width: 120, Now: fixedNow}); !strings.Contains(out, "cost:") {
		t.Errorf("wide + cost present: want the field shown, got %q", out)
	}
	// Narrow + cost present -> hidden (width fails).
	if out := renderDocStr(t, doc, withCost, Options{Width: 60, Now: fixedNow}); strings.Contains(out, "cost:") {
		t.Errorf("narrow + cost present: want the field hidden, got %q", out)
	}
	// Wide + cost absent -> hidden (optional fails).
	noCost := fullSnapshot()
	noCost.Session.Cost = nil
	if out := renderDocStr(t, doc, noCost, Options{Width: 120, Now: fixedNow}); strings.Contains(out, "cost:") {
		t.Errorf("wide + cost absent: want the field hidden, got %q", out)
	}
}

// TestRenderDocument_SubagentDefaultBreakpoints exercises the built-in
// claude-code-subagent default document across widths, confirming its width
// breakpoints reveal the duration (>=48), token count (>=64), and context
// percent (>=80) stats progressively while always showing the description
// and model.
func TestRenderDocument_SubagentDefaultBreakpoints(t *testing.T) {
	doc := parseDoc(t, config.DefaultDocument("claude-code-subagent"))
	// A subagent task snapshot with every stat's data populated.
	snap := schema.StatusSnapshot{
		Tool: schema.ToolSnapshot{ID: schema.ToolClaudeCodeSubagent},
		Subagent: &schema.SubagentSnapshot{
			Status:            "running",
			Description:       "Review render pipeline changes",
			ModelID:           "claude-opus-4-8",
			ModelDisplay:      "Opus 4.8",
			StartedAt:         fixedNow.Add(-90 * time.Second),
			ContextWindowSize: 200000,
			TokenCount:        28454,
		},
	}
	cases := []struct {
		width                             int
		wantDesc, wantModel               bool
		wantDuration, wantTokens, wantPct bool
	}{
		{40, true, true, false, false, false},
		{48, true, true, true, false, false},
		{63, true, true, true, false, false},
		{64, true, true, true, true, false},
		{79, true, true, true, true, false},
		{80, true, true, true, true, true},
		{120, true, true, true, true, true},
		{0, true, true, true, true, true}, // unknown width -> unbounded -> all stats
	}
	for _, c := range cases {
		out := RenderDocumentString(snap, doc, Options{Width: c.width, Now: fixedNow})
		checks := []struct {
			name string
			want bool
			got  bool
		}{
			{"description", c.wantDesc, strings.Contains(out, "Review render pipeline changes")},
			{"model", c.wantModel, strings.Contains(out, "Opus 4.8")},
			{"duration", c.wantDuration, strings.Contains(out, "1m 30s")},
			// task-tokens/task-context-percent are right-aligned to
			// min-width="6"/"4" (markup.md "min-width"/"align") in the
			// default document, so "28.5k" (5 cols) and "14%" (3 cols)
			// each carry one extra leading padding space beyond their
			// fixed prefix/suffix.
			{"tokens", c.wantTokens, strings.Contains(out, "↓  28.5k")},
			{"context-percent", c.wantPct, strings.Contains(out, "( 14%)")},
		}
		for _, ch := range checks {
			if ch.got != ch.want {
				t.Errorf("width %d: %s visible = %v, want %v (out=%q)", c.width, ch.name, ch.got, ch.want, out)
			}
		}
	}
}
