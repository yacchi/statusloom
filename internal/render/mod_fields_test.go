package render

// Tests for the statusloom-mod fields (subagents-*, autocompact-*, ...)
// and their self metrics. A nil snap.Mod must render empty and resolve as
// unresolved; a recorded zero is a real value; each field and metric pair must
// share one computation.

import (
	"strconv"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

var modFieldNames = []string{
	"subagents-running", "subagents-tokens", "subagents-by-type", "subagents-cost",
	"teammates-running", "teammates-idle", "last-turn-duration", "last-turn-reason",
	"autocompact-remaining", "context-breakdown", "permission-mode", "model-switches", "model-fallback",
	"background-tasks", "session-crons",
}

var modNumericMetricNames = []string{
	"subagents-running", "subagents-cost-usd", "subagents-tokens", "teammates-running",
	"teammates-idle", "last-turn-seconds", "model-switches",
	"autocompact-remaining-tokens", "background-tasks", "session-crons",
}

var modTypedMetricNames = []string{"model-fallback", "last-turn-reason", "permission-mode"}

// modRender renders one mod field through the same path the configurator uses.
func modRender(field, raw string, snap schema.StatusSnapshot, compact bool) string {
	return renderContent(config.WidgetSpec{Type: field, RawValue: raw == "raw"}, snap, config.ToolConfig{}, Options{Width: 120, Now: fixedNow}, compact)
}

func TestModFields_NilModIsEmptyAndUnresolved(t *testing.T) {
	snap := schema.StatusSnapshot{}
	for _, f := range modFieldNames {
		if got := modRender(f, "", snap, false); got != "" {
			t.Errorf("renderContent(%s) with nil Mod = %q, want empty", f, got)
		}
	}
	for _, name := range modNumericMetricNames {
		if _, ok := modMetricValue(name, snap); ok {
			t.Errorf("modMetricValue(%s) with nil Mod: want ok=false", name)
		}
	}
	for _, name := range modTypedMetricNames {
		_, ok, handled := modTypedMetric(name, snap)
		if ok || !handled {
			t.Errorf("modTypedMetric(%s) with nil Mod = ok %v handled %v, want false,true", name, ok, handled)
		}
	}
}

func TestModFields_UnknownNameIsNotHandled(t *testing.T) {
	snap := schema.StatusSnapshot{Mod: &schema.ModSnapshot{}}
	if got := modRender("nope", "", snap, false); got != "" {
		t.Errorf("renderContent(nope) = %q, want empty", got)
	}
	if _, ok := modMetricValue("nope", snap); ok {
		t.Error("modMetricValue(nope): want ok=false")
	}
	if _, _, handled := modTypedMetric("nope", snap); handled {
		t.Error("modTypedMetric(nope): want handled=false")
	}
}

func TestModFields_RecordedZeroIsReal(t *testing.T) {
	snap := schema.StatusSnapshot{Mod: &schema.ModSnapshot{}}
	for _, f := range []string{"model-switches"} {
		if got := modRender(f, "", snap, false); got != "0" {
			t.Errorf("renderContent(%s) on empty Mod = %q, want 0", f, got)
		}
	}
	if v, ok := modMetricValue("model-switches", snap); !ok || v != 0 {
		t.Errorf("modMetricValue(model-switches) = %v,%v, want 0,true", v, ok)
	}
	// Sub-structs that were never recorded stay unresolved, not zero.
	for _, f := range []string{"subagents-running", "background-tasks", "model-fallback", "last-turn-duration"} {
		if got := modRender(f, "", snap, false); got != "" {
			t.Errorf("renderContent(%s) on empty Mod = %q, want empty", f, got)
		}
	}
	if v, ok, handled := modTypedMetric("model-fallback", snap); !handled || !ok || v.Kind != dsl.ValueBool || v.Bool {
		t.Errorf("modTypedMetric(model-fallback) = %+v ok %v handled %v, want Bool false resolved", v, ok, handled)
	}
}

func TestModFields_LastTurnRawIsWholeSeconds(t *testing.T) {
	snap := schema.StatusSnapshot{Mod: &schema.ModSnapshot{LastTurn: &schema.ModTurn{Duration: 83500 * time.Millisecond, Reason: "answer"}}}
	if got := modRender("last-turn-duration", "", snap, false); got != "1m 23s" {
		t.Errorf("last-turn-duration = %q, want 1m 23s", got)
	}
	if got := modRender("last-turn-duration", "raw", snap, false); got != "83" {
		t.Errorf("raw last-turn-duration = %q, want 83", got)
	}
	if v, ok := modMetricValue("last-turn-seconds", snap); !ok || v != 83.5 {
		t.Errorf("last-turn-seconds = %v,%v, want 83.5,true", v, ok)
	}
	if v, ok, handled := modTypedMetric("last-turn-reason", snap); !handled || !ok || v.Str != "answer" {
		t.Errorf("last-turn-reason = %+v ok %v handled %v, want answer", v, ok, handled)
	}
}

func TestModFields_AgentsByTypeCostAndCompactTokens(t *testing.T) {
	cost := 0.4
	snap := schema.StatusSnapshot{Mod: &schema.ModSnapshot{Agents: &schema.ModAgents{
		Running: 3,
		ByType: []schema.ModTypeCount{
			{Type: "general-purpose", Count: 2},
			{Type: "Explore", Count: 1},
		},
		CostUSD: &cost,
		Tokens:  12345,
	}}}
	if got := modRender("subagents-by-type", "", snap, false); got != "general-purpose×2 Explore" {
		t.Errorf("subagents-by-type = %q, want %q", got, "general-purpose×2 Explore")
	}
	if got := modRender("subagents-cost", "", snap, false); got != "$0.40" {
		t.Errorf("subagents-cost = %q, want $0.40", got)
	}
	if got := modRender("subagents-cost", "raw", snap, false); got != "0.40" {
		t.Errorf("raw subagents-cost = %q, want 0.40", got)
	}
	if got := modRender("subagents-tokens", "", snap, false); got != "12,345" {
		t.Errorf("subagents-tokens = %q, want 12,345", got)
	}
	if got := modRender("subagents-tokens", "", snap, true); got != "12.3k" {
		t.Errorf("compact subagents-tokens = %q, want 12.3k", got)
	}
	// The mod's own ordering must survive rendering.
	if snap.Mod.Agents.ByType[0].Type != "general-purpose" {
		t.Error("subagents-by-type reordered the snapshot's ByType")
	}
}

func TestModFields_AutocompactRemaining(t *testing.T) {
	threshold := 160000
	withCurrent := schema.StatusSnapshot{
		Session: schema.SessionSnapshot{Context: &schema.ContextUsage{TotalInputTokens: 120000, Current: &schema.TokenBreakdown{}}},
		Mod:     &schema.ModSnapshot{Context: &schema.ModContext{AutoCompactThreshold: &threshold, TotalTokens: 150000}},
	}
	if got := modRender("autocompact-remaining", "", withCurrent, false); got != "40,000" {
		t.Errorf("autocompact-remaining with Current = %q, want 40,000", got)
	}

	// No per-call breakdown yet: fall back to the breakdown estimate.
	noCurrent := schema.StatusSnapshot{
		Session: schema.SessionSnapshot{Context: &schema.ContextUsage{TotalInputTokens: 120000}},
		Mod:     &schema.ModSnapshot{Context: &schema.ModContext{AutoCompactThreshold: &threshold, TotalTokens: 150000}},
	}
	if got := modRender("autocompact-remaining", "", noCurrent, false); got != "10,000" {
		t.Errorf("autocompact-remaining without Current = %q, want 10,000", got)
	}

	// Past the threshold it clamps to zero rather than going negative.
	over := schema.StatusSnapshot{
		Mod: &schema.ModSnapshot{Context: &schema.ModContext{AutoCompactThreshold: &threshold, TotalTokens: 170000}},
	}
	if got := modRender("autocompact-remaining", "", over, false); got != "0" {
		t.Errorf("autocompact-remaining over threshold = %q, want 0", got)
	}

	// Auto-compact off (no threshold) is unresolved.
	off := schema.StatusSnapshot{Mod: &schema.ModSnapshot{Context: &schema.ModContext{TotalTokens: 1}}}
	if got := modRender("autocompact-remaining", "", off, false); got != "" {
		t.Errorf("autocompact-remaining with no threshold = %q, want empty", got)
	}
}

func TestModFields_FieldAndSelfMetricAgree(t *testing.T) {
	threshold := 160000
	cost := 0.4
	snap := schema.StatusSnapshot{
		Session: schema.SessionSnapshot{Context: &schema.ContextUsage{TotalInputTokens: 120000, Current: &schema.TokenBreakdown{}}},
		Mod: &schema.ModSnapshot{
			Context: &schema.ModContext{AutoCompactThreshold: &threshold, TotalTokens: 150000},
			Agents:  &schema.ModAgents{Running: 2, CostUSD: &cost},
		},
	}
	// Raw field text must be the metric's number.
	if got, want := modRender("autocompact-remaining", "raw", snap, false), strconv.Itoa(int(mustMetric(t, "autocompact-remaining-tokens", snap))); got != want {
		t.Errorf("autocompact-remaining raw = %q, metric says %q", got, want)
	}
	if got, want := modRender("subagents-running", "raw", snap, false), strconv.Itoa(int(mustMetric(t, "subagents-running", snap))); got != want {
		t.Errorf("subagents-running raw = %q, metric says %q", got, want)
	}
}

func TestModFields_ContextBreakdownFiltersAndSortsWithoutReordering(t *testing.T) {
	cats := []schema.ModContextCategory{
		{Name: "System", Tokens: 3000, Kind: "used"},
		{Name: "Free", Tokens: 50000, Kind: "free"},
		{Name: "Tools", Tokens: 9000, Kind: "used"},
		{Name: "MCP", Tokens: 3000, Kind: "used"},
		{Name: "Skills", Tokens: 0, Kind: "used"},
	}
	snap := schema.StatusSnapshot{Mod: &schema.ModSnapshot{Context: &schema.ModContext{Categories: cats}}}
	if got := modRender("context-breakdown", "", snap, false); got != "Tools 9.0k · System 3.0k · MCP 3.0k" {
		t.Errorf("context-breakdown = %q, want %q", got, "Tools 9.0k · System 3.0k · MCP 3.0k")
	}
	if cats[0].Name != "System" || cats[1].Name != "Free" {
		t.Error("context-breakdown reordered the snapshot's categories in place")
	}
}

func TestModFields_ModelFallbackOnlyWhenTurnModelDiffers(t *testing.T) {
	fallback := schema.StatusSnapshot{Mod: &schema.ModSnapshot{SessionModel: "Opus 4.7", TurnModel: "Sonnet 4.6"}}
	if got := modRender("model-fallback", "", fallback, false); got != "Opus 4.7→Sonnet 4.6" {
		t.Errorf("model-fallback = %q, want Opus 4.7→Sonnet 4.6", got)
	}
	if v, ok, handled := modTypedMetric("model-fallback", fallback); !handled || !ok || !v.Bool {
		t.Errorf("model-fallback metric = %+v ok %v handled %v, want true", v, ok, handled)
	}

	for name, m := range map[string]*schema.ModSnapshot{
		"same model":       {SessionModel: "Opus 4.7", TurnModel: "Opus 4.7"},
		"no turn model":    {SessionModel: "Opus 4.7"},
		"no session model": {TurnModel: "Opus 4.7"},
	} {
		snap := schema.StatusSnapshot{Mod: m}
		if got := modRender("model-fallback", "", snap, false); got != "" {
			t.Errorf("model-fallback %s = %q, want empty", name, got)
		}
		if v, ok, handled := modTypedMetric("model-fallback", snap); !handled || !ok || v.Bool {
			t.Errorf("model-fallback metric %s = %+v ok %v handled %v, want false resolved", name, v, ok, handled)
		}
	}
}

// mustMetric returns a numeric mod metric, failing the test when unresolved.
func mustMetric(t *testing.T, name string, snap schema.StatusSnapshot) float64 {
	t.Helper()
	v, ok := modMetricValue(name, snap)
	if !ok {
		t.Fatalf("modMetricValue(%s): unresolved", name)
	}
	return v
}
