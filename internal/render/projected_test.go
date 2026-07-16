package render

import (
	"math"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/schema"
)

// TestMetricValue_Projected checks the *-projected-percent metrics: usage
// extrapolated to the window reset at the current burn rate (usage divided by
// the elapsed fraction of the window).
func TestMetricValue_Projected(t *testing.T) {
	opts := Options{Width: 120, Now: fixedNow}

	// fullSnapshot: FiveHour = 27% used, resets in 1h23m (of a 5h window);
	// SevenDay = 79% used, resets in 2d3h (of a 7d window).
	//   5h:  elapsed frac = (300-83)/300 = 0.72333 -> 27/0.72333  ≈ 37.33
	//   7d:  elapsed frac = (168-51)/168 = 0.69643 -> 79/0.69643  ≈ 113.44
	t.Run("five-hour extrapolation", func(t *testing.T) {
		got, ok := metricValue("five-hour-projected-percent", fullSnapshot(), config.ToolConfig{}, opts)
		if !ok {
			t.Fatal("expected ok = true")
		}
		if math.Abs(got-37.33) > 0.5 {
			t.Errorf("five-hour-projected-percent = %v, want ~37.33", got)
		}
	})

	t.Run("seven-day extrapolation", func(t *testing.T) {
		got, ok := metricValue("seven-day-projected-percent", fullSnapshot(), config.ToolConfig{}, opts)
		if !ok {
			t.Fatal("expected ok = true")
		}
		if math.Abs(got-113.44) > 0.5 {
			t.Errorf("seven-day-projected-percent = %v, want ~113.44", got)
		}
	})

	// Per-model weekly windows (Opus/Sonnet) use the same 7d window length.
	t.Run("per-model opus extrapolation", func(t *testing.T) {
		snap := fullSnapshot()
		// 20% used, resets in 3d12h => elapsed frac = (168-84)/168 = 0.5 => 40.
		snap.Account.SevenDayOpus = &schema.RateWindow{
			UsedPercentage: 20,
			ResetsAt:       fixedNow.Add(3*24*time.Hour + 12*time.Hour),
		}
		got, ok := metricValue("seven-day-opus-projected-percent", snap, config.ToolConfig{}, opts)
		if !ok {
			t.Fatal("expected ok = true")
		}
		if math.Abs(got-40) > 0.5 {
			t.Errorf("seven-day-opus-projected-percent = %v, want ~40", got)
		}
	})

	// Early in the window (< 10% elapsed) the projection is unstable, so the
	// metric gates off and the color-rule falls back to the base color.
	t.Run("early-window gate", func(t *testing.T) {
		snap := fullSnapshot()
		snap.Account.FiveHour = &schema.RateWindow{
			UsedPercentage: 5,
			ResetsAt:       fixedNow.Add(5 * time.Hour * 95 / 100), // only 5% elapsed
		}
		if _, ok := metricValue("five-hour-projected-percent", snap, config.ToolConfig{}, opts); ok {
			t.Error("expected ok = false while too little of the window has elapsed")
		}
	})

	// A reset already in the past clamps elapsed to the full window: the
	// projection collapses to the raw used percentage.
	t.Run("past reset equals raw usage", func(t *testing.T) {
		snap := fullSnapshot()
		snap.Account.FiveHour = &schema.RateWindow{
			UsedPercentage: 88,
			ResetsAt:       fixedNow.Add(-time.Hour),
		}
		got, ok := metricValue("five-hour-projected-percent", snap, config.ToolConfig{}, opts)
		if !ok {
			t.Fatal("expected ok = true")
		}
		if got != 88 {
			t.Errorf("five-hour-projected-percent = %v, want 88", got)
		}
	})

	t.Run("nil window", func(t *testing.T) {
		snap := fullSnapshot()
		snap.Account.FiveHour = nil
		if _, ok := metricValue("five-hour-projected-percent", snap, config.ToolConfig{}, opts); ok {
			t.Error("expected ok = false for a nil window")
		}
	})

	t.Run("zero clock", func(t *testing.T) {
		if _, ok := metricValue("five-hour-projected-percent", fullSnapshot(), config.ToolConfig{}, Options{}); ok {
			t.Error("expected ok = false when the clock is unset")
		}
	})
}
