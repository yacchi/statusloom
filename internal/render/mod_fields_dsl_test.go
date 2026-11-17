package render

// Tests that the mod-fields metrics reach the DSL evaluator: numeric ones
// through metricValue (format, when=, color-rule thresholds) and bool/text
// ones through docResolver.ResolveMetric (when=, color-rule on self).

import (
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/schema"
)

// modMetricsSnapshot is fullSnapshot with a populated mod summary. The
// permission mode, model-switch count and model fallback are the caller's choice; the
// agents summary is shared by every test here.
func modMetricsSnapshot(mode string, switches int, turnModel string) schema.StatusSnapshot {
	snap := fullSnapshot()
	cost := 0.42
	snap.Mod = &schema.ModSnapshot{
		PermissionMode: mode,
		ModelSwitches:  switches,
		SessionModel:   "Opus",
		TurnModel:      turnModel,
		Agents: &schema.ModAgents{
			Running: 2,
			CostUSD: &cost,
			Tokens:  18200,
		},
	}
	return snap
}

func TestRenderDocument_ModMetricsWhenAndFormat(t *testing.T) {
	const src = `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <line>
      <span when="permission-mode eq &quot;plan&quot;" prefix="[plan]"/>
      <span when="model-fallback" prefix="[fb]"/>
      <span when="model-switches ge 3" prefix="[fail]"/>
      <field name="subagents-cost" format="currency" precision="3"/>
      <text> </text>
      <field name="subagents-tokens" format="compact-number"/>
    </line>
  </layout>
</statusloom>`
	doc := parseDoc(t, src)
	fallback := "Sonnet"

	lines := RenderDocument(modMetricsSnapshot("plan", 4, fallback), doc, Options{Width: 120, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("populated: got %d lines, want 1", len(lines))
	}
	if got, want := lineText(lines[0]), "[plan][fb][fail]$0.420 18.2k"; got != want {
		t.Errorf("populated = %q, want %q", got, want)
	}

	sameModel := "Opus"
	lines = RenderDocument(modMetricsSnapshot("default", 1, sameModel), doc, Options{Width: 120, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("resolved-but-false: got %d lines, want 1", len(lines))
	}
	if got, want := lineText(lines[0]), "$0.420 18.2k"; got != want {
		t.Errorf("resolved-but-false = %q, want %q", got, want)
	}

	nilSnap := fullSnapshot()
	nilSnap.Mod = nil
	lines = RenderDocument(nilSnap, doc, Options{Width: 120, Now: fixedNow})
	var text strings.Builder
	for _, l := range lines {
		text.WriteString(lineText(l))
	}
	if got := strings.TrimSpace(text.String()); got != "" {
		t.Errorf("nil Mod = %q, want empty", got)
	}
}

// segmentANSI returns the ANSI text of the segment whose plain text is text.
func segmentANSI(t *testing.T, line DocLine, text string) string {
	t.Helper()
	for _, s := range line.Segments {
		if s.Text == text {
			return s.ANSI
		}
	}
	t.Fatalf("no segment with text %q in %q", text, lineText(line))
	return ""
}

func TestRenderDocument_ModMetricsColorRuleOnSelf(t *testing.T) {
	const src = `<statusloom version="1" tool="claude-code" color-level="ansi16">
  <layout name="default" active="true">
    <line>
      <field name="model-switches" color="green"><color-rule when="self ge 3" color="red"/></field>
      <text> </text>
      <field name="permission-mode" color="green"><color-rule when="self eq &quot;plan&quot;" color="red"/></field>
      <text> </text>
      <field name="model-fallback" color="green"><color-rule when="self" color="red"/></field>
    </line>
  </layout>
</statusloom>`
	const red = "\x1b[31m"
	const green = "\x1b[32m"
	doc := parseDoc(t, src)
	fallback := "Sonnet"

	lines := RenderDocument(modMetricsSnapshot("plan", 4, fallback), doc, Options{Width: 120, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("populated: got %d lines, want 1", len(lines))
	}
	for _, text := range []string{"4", "plan", "Opus→Sonnet"} {
		if got := segmentANSI(t, lines[0], text); !strings.Contains(got, red) {
			t.Errorf("populated %q ANSI = %q, want red", text, got)
		}
	}

	lines = RenderDocument(modMetricsSnapshot("default", 1, fallback), doc, Options{Width: 120, Now: fixedNow})
	if len(lines) != 1 {
		t.Fatalf("below thresholds: got %d lines, want 1", len(lines))
	}
	for _, text := range []string{"1", "default"} {
		got := segmentANSI(t, lines[0], text)
		if !strings.Contains(got, green) || strings.Contains(got, red) {
			t.Errorf("below thresholds %q ANSI = %q, want green without red", text, got)
		}
	}
}
