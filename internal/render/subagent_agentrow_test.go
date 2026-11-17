package render

// Tests for the AgentRow mod fields (task-effort, task-agent-type, task-name,
// task-cost, task-depth, task-*-tokens). The legacy subagentStatusLine never
// sends these keys, so an absent value must render empty and resolve as
// unresolved in when= expressions, while a reported zero is a real value.

import (
	"testing"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/schema"
)

// agentRowSnapshot is subagentSnapshot with every AgentRow mod field set.
func agentRowSnapshot() schema.StatusSnapshot {
	snap := subagentSnapshot()
	effort := "high"
	cost := 0.42
	depth := 0
	in, out, cr, cc := 18200, 4100, 11800, 6200
	snap.Subagent.Effort = &effort
	snap.Subagent.AgentType = "general-purpose"
	snap.Subagent.Name = "render-review"
	snap.Subagent.CostUSD = &cost
	snap.Subagent.Depth = &depth
	snap.Subagent.InputTokens = &in
	snap.Subagent.OutputTokens = &out
	snap.Subagent.CacheReadTokens = &cr
	snap.Subagent.CacheCreationTokens = &cc
	return snap
}

func TestRenderContent_AgentRowFields_Values(t *testing.T) {
	snap := agentRowSnapshot()
	opts := Options{Width: 120, Now: fixedNow}
	cases := map[string]string{
		"task-effort":                "high",
		"task-agent-type":            "general-purpose",
		"task-name":                  "render-review",
		"task-cost":                  "$0.42",
		"task-depth":                 "0",
		"task-input-tokens":          "18,200",
		"task-output-tokens":         "4,100",
		"task-cache-read-tokens":     "11,800",
		"task-cache-creation-tokens": "6,200",
	}
	for field, want := range cases {
		if got := renderContent(config.WidgetSpec{Type: field}, snap, config.ToolConfig{}, opts, false); got != want {
			t.Errorf("renderContent(%s) = %q, want %q", field, got, want)
		}
	}
	// Raw mode: the bare number for cost, no thousands separators for tokens.
	if got := renderContent(config.WidgetSpec{Type: "task-cost", RawValue: true}, snap, config.ToolConfig{}, opts, false); got != "0.42" {
		t.Errorf("raw task-cost = %q, want 0.42", got)
	}
	if got := renderContent(config.WidgetSpec{Type: "task-input-tokens", RawValue: true}, snap, config.ToolConfig{}, opts, false); got != "18200" {
		t.Errorf("raw task-input-tokens = %q, want 18200", got)
	}
}

func TestRenderContent_AgentRowFields_AbsentIsEmpty(t *testing.T) {
	snap := subagentSnapshot()
	opts := Options{Width: 120, Now: fixedNow}
	for _, field := range []string{
		"task-effort", "task-agent-type", "task-name", "task-cost", "task-depth",
		"task-input-tokens", "task-output-tokens", "task-cache-read-tokens", "task-cache-creation-tokens",
	} {
		if got := renderContent(config.WidgetSpec{Type: field}, snap, config.ToolConfig{}, opts, false); got != "" {
			t.Errorf("renderContent(%s) with absent mod data = %q, want empty", field, got)
		}
	}
}

func TestRenderContent_AgentRowFields_ReportedZeroIsReal(t *testing.T) {
	snap := subagentSnapshot()
	zeroCost := 0.0
	zeroDepth, zeroTokens := 0, 0
	snap.Subagent.CostUSD = &zeroCost
	snap.Subagent.Depth = &zeroDepth
	snap.Subagent.InputTokens = &zeroTokens
	opts := Options{Width: 120, Now: fixedNow}

	if got := renderContent(config.WidgetSpec{Type: "task-cost"}, snap, config.ToolConfig{}, opts, false); got != "$0.00" {
		t.Errorf("task-cost zero = %q, want $0.00", got)
	}
	if got := renderContent(config.WidgetSpec{Type: "task-depth"}, snap, config.ToolConfig{}, opts, false); got != "0" {
		t.Errorf("task-depth zero = %q, want 0", got)
	}
	if got := renderContent(config.WidgetSpec{Type: "task-input-tokens"}, snap, config.ToolConfig{}, opts, false); got != "0" {
		t.Errorf("task-input-tokens zero = %q, want 0", got)
	}
}

func TestMetricValue_AgentRowNumeric(t *testing.T) {
	cfg := config.ToolConfig{}
	opts := Options{Width: 120, Now: fixedNow}

	snap := agentRowSnapshot()
	if v, ok := metricValue("task-cost-usd", snap, cfg, opts); !ok || v != 0.42 {
		t.Errorf("task-cost-usd = %v,%v, want 0.42,true", v, ok)
	}
	if v, ok := metricValue("task-cache-creation-tokens", snap, cfg, opts); !ok || v != 6200 {
		t.Errorf("task-cache-creation-tokens = %v,%v, want 6200,true", v, ok)
	}

	absent := subagentSnapshot()
	for _, name := range []string{
		"task-cost-usd", "task-depth", "task-input-tokens", "task-output-tokens",
		"task-cache-read-tokens", "task-cache-creation-tokens",
	} {
		if _, ok := metricValue(name, absent, cfg, opts); ok {
			t.Errorf("%s with absent mod data: want ok=false", name)
		}
		if _, ok := metricValue(name, schema.StatusSnapshot{}, cfg, opts); ok {
			t.Errorf("%s with nil Subagent: want ok=false", name)
		}
	}

	// A reported zero resolves (it is a real value, not absence).
	zero := 0
	absent.Subagent.Depth = &zero
	if v, ok := metricValue("task-depth", absent, cfg, opts); !ok || v != 0 {
		t.Errorf("task-depth reported zero = %v,%v, want 0,true", v, ok)
	}
}

func TestRenderSubagentLine_AgentRowWhenAndColorRule(t *testing.T) {
	const src = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <subagent>
      <line>
        <field name="task-name" format="compact-number"/>
        <span when="task-agent-type eq &quot;general-purpose&quot;" prefix="[gp]"/>
        <span when="task-cost-usd gt 0.3" prefix="[pricey]"/>
        <span when="task-depth eq 0" prefix="[root]"/>
        <field name="task-input-tokens" format="compact-number" color="green"><color-rule when="self ge 10000" color="red"/></field>
      </line>
    </subagent>
  </layout>
</statusloom>`
	doc := parseDoc(t, src)

	lines := RenderSubagentLine(agentRowSnapshot(), doc, Options{Width: 120, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if got, want := lineText(lines[0]), "render-review[gp][pricey][root]18.2k"; got != want {
		t.Errorf("populated row = %q, want %q", got, want)
	}

	// Absent mod data: every when= on a new metric is unresolved, so it is
	// false and its span is hidden; the field itself renders empty.
	lines = RenderSubagentLine(subagentSnapshot(), doc, Options{Width: 120, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("absent row: got %d lines, want 1", len(lines))
	}
	if got := lineText(lines[0]); got != "" {
		t.Errorf("absent row = %q, want empty (all when= unresolved, fields absent)", got)
	}
}

func TestRenderSubagentLine_AgentRowFormatsAndColorRuleOnSelf(t *testing.T) {
	const src = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <subagent>
      <line>
        <field name="task-input-tokens" format="compact-number"/>
        <text> </text>
        <field name="task-cost"><color-rule when="self gt 1" color="red"/></field>
      </line>
    </subagent>
  </layout>
</statusloom>`
	doc := parseDoc(t, src)
	lines := RenderSubagentLine(agentRowSnapshot(), doc, Options{Width: 120, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if got, want := lineText(lines[0]), "18.2k $0.42"; got != want {
		t.Errorf("row = %q, want %q", got, want)
	}
}
