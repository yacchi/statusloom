package render

import (
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/schema"
)

// subagentSnapshot returns a StatusSnapshot representing one subagentStatusLine
// task row, with every task-* field's backing data populated.
func subagentSnapshot() schema.StatusSnapshot {
	return schema.StatusSnapshot{
		Tool: schema.ToolSnapshot{ID: schema.ToolClaudeCodeSubagent},
		Subagent: &schema.SubagentSnapshot{
			ID:                "b1a2c3d4e5f60718",
			Type:              "local_agent",
			Status:            "running",
			Description:       "Review render pipeline changes",
			Label:             "Review render pipeline changes (label)",
			StartedAt:         fixedNow.Add(-90 * time.Second),
			ModelID:           "claude-opus-4-8",
			ModelDisplay:      "Opus 4.8",
			ContextWindowSize: 200000,
			TokenCount:        28454,
			Cwd:               "/Users/dev/myapp",
		},
	}
}

func TestRenderContent_TaskFields(t *testing.T) {
	cases := []struct {
		name string
		spec config.WidgetSpec
		want string
	}{
		{"task-description", config.WidgetSpec{Type: "task-description"}, "Review render pipeline changes"},
		{"task-model", config.WidgetSpec{Type: "task-model"}, "Opus 4.8"},
		{"task-model-id", config.WidgetSpec{Type: "task-model-id"}, "claude-opus-4-8"},
		{"task-status", config.WidgetSpec{Type: "task-status"}, "running"},
		{"task-tokens", config.WidgetSpec{Type: "task-tokens"}, "28,454"},
		{"task-tokens raw", config.WidgetSpec{Type: "task-tokens", RawValue: true}, "28454"},
		{"task-context-size", config.WidgetSpec{Type: "task-context-size"}, "200,000"},
		{"task-context-percent", config.WidgetSpec{Type: "task-context-percent"}, "14.2%"}, // 28454/200000*100
		{"task-duration", config.WidgetSpec{Type: "task-duration"}, "1m 30s"},
		{"task-duration raw", config.WidgetSpec{Type: "task-duration", RawValue: true}, "90"},
	}
	snap := subagentSnapshot()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderContent(c.spec, snap, config.ToolConfig{}, Options{Width: 120, Now: fixedNow}, false)
			if got != c.want {
				t.Errorf("renderContent(%q) = %q, want %q", c.spec.Type, got, c.want)
			}
		})
	}
}

// TestRenderContent_TaskDescriptionFallsBackToLabel confirms Description
// empty falls back to Label, per markup.md field-value resolution for
// task-description.
func TestRenderContent_TaskDescriptionFallsBackToLabel(t *testing.T) {
	snap := subagentSnapshot()
	snap.Subagent.Description = ""
	got := renderContent(config.WidgetSpec{Type: "task-description"}, snap, config.ToolConfig{}, Options{Width: 120, Now: fixedNow}, false)
	want := "Review render pipeline changes (label)"
	if got != want {
		t.Errorf("renderContent(task-description) = %q, want %q", got, want)
	}
}

// TestRenderContent_TaskFieldsNilSubagent asserts every task-* field is
// nil-safe: with snap.Subagent == nil (the non-subagent render context),
// every field hides (renders "") rather than panicking.
func TestRenderContent_TaskFieldsNilSubagent(t *testing.T) {
	fields := []string{
		"task-description", "task-model", "task-model-id", "task-status",
		"task-tokens", "task-context-size", "task-context-percent",
		"task-duration", "task-effort",
	}
	snap := schema.StatusSnapshot{Tool: schema.ToolSnapshot{ID: schema.ToolClaudeCodeSubagent}}
	for _, name := range fields {
		t.Run(name, func(t *testing.T) {
			got := renderContent(config.WidgetSpec{Type: name}, snap, config.ToolConfig{}, Options{Width: 120, Now: fixedNow}, false)
			if got != "" {
				t.Errorf("renderContent(%q) with nil Subagent = %q, want \"\"", name, got)
			}
		})
	}
}

// TestRenderContent_TaskContextPercentZeroWindow asserts the percent field
// hides when ContextWindowSize is 0 (division-by-zero guard).
func TestRenderContent_TaskContextPercentZeroWindow(t *testing.T) {
	snap := subagentSnapshot()
	snap.Subagent.ContextWindowSize = 0
	got := renderContent(config.WidgetSpec{Type: "task-context-percent"}, snap, config.ToolConfig{}, Options{Width: 120, Now: fixedNow}, false)
	if got != "" {
		t.Errorf("renderContent(task-context-percent) = %q, want \"\" (zero window)", got)
	}
}

// TestRenderContent_TaskDurationZeroStartedAt asserts the duration field
// hides when StartedAt is unset (zero time).
func TestRenderContent_TaskDurationZeroStartedAt(t *testing.T) {
	snap := subagentSnapshot()
	snap.Subagent.StartedAt = time.Time{}
	got := renderContent(config.WidgetSpec{Type: "task-duration"}, snap, config.ToolConfig{}, Options{Width: 120, Now: fixedNow}, false)
	if got != "" {
		t.Errorf("renderContent(task-duration) = %q, want \"\" (zero StartedAt)", got)
	}
}

// TestRenderContent_TaskEffort covers the forward-compatible task-effort
// field: hidden when Effort is nil (always true today), populated once a
// future transcript-derived value is set.
func TestRenderContent_TaskEffort(t *testing.T) {
	snap := subagentSnapshot()
	if got := renderContent(config.WidgetSpec{Type: "task-effort"}, snap, config.ToolConfig{}, Options{Width: 120, Now: fixedNow}, false); got != "" {
		t.Errorf("renderContent(task-effort) = %q, want \"\" (Effort nil)", got)
	}
	level := "high"
	snap.Subagent.Effort = &level
	if got := renderContent(config.WidgetSpec{Type: "task-effort"}, snap, config.ToolConfig{}, Options{Width: 120, Now: fixedNow}, false); got != "high" {
		t.Errorf("renderContent(task-effort) = %q, want high", got)
	}
}

func TestMetricValue_TaskMetrics(t *testing.T) {
	snap := subagentSnapshot()
	cfg := config.ToolConfig{}
	opts := Options{Width: 120, Now: fixedNow}

	if v, ok := metricValue("task-token-count", snap, cfg, opts); !ok || v != 28454 {
		t.Errorf("task-token-count = %v,%v, want 28454,true", v, ok)
	}
	if v, ok := metricValue("task-context-window-tokens", snap, cfg, opts); !ok || v != 200000 {
		t.Errorf("task-context-window-tokens = %v,%v, want 200000,true", v, ok)
	}
	if _, ok := metricValue("task-context-window-tokens", schema.StatusSnapshot{}, cfg, opts); ok {
		t.Error("task-context-window-tokens with nil Subagent: want ok=false")
	}
	if v, ok := metricValue("task-context-percent", snap, cfg, opts); !ok || v < 14.2 || v > 14.3 {
		t.Errorf("task-context-percent = %v,%v, want ~14.227,true", v, ok)
	}
	if v, ok := metricValue("task-duration-seconds", snap, cfg, opts); !ok || v != 90 {
		t.Errorf("task-duration-seconds = %v,%v, want 90,true", v, ok)
	}

	nilSnap := schema.StatusSnapshot{}
	if _, ok := metricValue("task-token-count", nilSnap, cfg, opts); ok {
		t.Error("task-token-count with nil Subagent: want ok=false")
	}
	if _, ok := metricValue("task-context-percent", nilSnap, cfg, opts); ok {
		t.Error("task-context-percent with nil Subagent: want ok=false")
	}
	if _, ok := metricValue("task-duration-seconds", nilSnap, cfg, opts); ok {
		t.Error("task-duration-seconds with nil Subagent: want ok=false")
	}

	zeroWindow := subagentSnapshot()
	zeroWindow.Subagent.ContextWindowSize = 0
	if _, ok := metricValue("task-context-percent", zeroWindow, cfg, opts); ok {
		t.Error("task-context-percent with zero window: want ok=false")
	}

	zeroStart := subagentSnapshot()
	zeroStart.Subagent.StartedAt = time.Time{}
	if _, ok := metricValue("task-duration-seconds", zeroStart, cfg, opts); ok {
		t.Error("task-duration-seconds with zero StartedAt: want ok=false")
	}
}
