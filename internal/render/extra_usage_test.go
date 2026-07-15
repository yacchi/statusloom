package render

import (
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/schema"
)

// extraUsageSnapshot extends richSnapshot with Account.ExtraUsage and the
// per-model weekly (7-day) rate windows for Opus/Sonnet.
func extraUsageSnapshot() schema.StatusSnapshot {
	snap := richSnapshot()
	snap.Account.ExtraUsage = &schema.ExtraUsage{
		Enabled:         true,
		MonthlyLimitUSD: f64ptr(50),
		UsedCreditsUSD:  f64ptr(12.5),
		Utilization:     f64ptr(25),
	}
	snap.Account.SevenDayOpus = &schema.RateWindow{
		UsedPercentage: 41,
		ResetsAt:       fixedNow.Add(3*24*time.Hour + 2*time.Hour),
	}
	snap.Account.SevenDaySonnet = &schema.RateWindow{
		UsedPercentage: 63,
		ResetsAt:       fixedNow.Add(1*24*time.Hour + 5*time.Hour),
	}
	return snap
}

func TestMetricValue_ExtraUsageAndWeeklyPerModel(t *testing.T) {
	snap := extraUsageSnapshot()
	opts := Options{Width: 120, Now: fixedNow}

	cases := []struct {
		name   string
		metric string
		want   float64
		wantOk bool
	}{
		{name: "extra-usage-cost-usd", metric: "extra-usage-cost-usd", want: 12.5, wantOk: true},
		{name: "extra-usage-limit-usd", metric: "extra-usage-limit-usd", want: 50, wantOk: true},
		{name: "extra-usage-percent", metric: "extra-usage-percent", want: 25, wantOk: true},
		{name: "seven-day-opus-percent", metric: "seven-day-opus-percent", want: 41, wantOk: true},
		{name: "seven-day-sonnet-percent", metric: "seven-day-sonnet-percent", want: 63, wantOk: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := metricValue(c.metric, snap, config.ToolConfig{}, opts)
			if ok != c.wantOk {
				t.Fatalf("metricValue(%q) ok = %v, want %v", c.metric, ok, c.wantOk)
			}
			if ok && got != c.want {
				t.Errorf("metricValue(%q) = %v, want %v", c.metric, got, c.want)
			}
		})
	}

	t.Run("seven-day-opus-reset-minutes", func(t *testing.T) {
		got, ok := metricValue("seven-day-opus-reset-minutes", snap, config.ToolConfig{}, opts)
		if !ok {
			t.Fatal("expected ok = true")
		}
		want := snap.Account.SevenDayOpus.ResetsAt.Sub(fixedNow).Minutes()
		if got < want-1 || got > want {
			t.Errorf("metricValue(seven-day-opus-reset-minutes) = %v, want ~%v", got, want)
		}
	})

	t.Run("seven-day-sonnet-reset-minutes", func(t *testing.T) {
		got, ok := metricValue("seven-day-sonnet-reset-minutes", snap, config.ToolConfig{}, opts)
		if !ok {
			t.Fatal("expected ok = true")
		}
		want := snap.Account.SevenDaySonnet.ResetsAt.Sub(fixedNow).Minutes()
		if got < want-1 || got > want {
			t.Errorf("metricValue(seven-day-sonnet-reset-minutes) = %v, want ~%v", got, want)
		}
	})
}

func TestMetricValue_ExtraUsageAndWeeklyPerModel_NilCases(t *testing.T) {
	opts := Options{Width: 120, Now: fixedNow}
	cases := []struct {
		name   string
		metric string
		mutate func(*schema.StatusSnapshot)
	}{
		{"extra-usage-cost-usd nil ExtraUsage", "extra-usage-cost-usd", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage = nil }},
		{"extra-usage-cost-usd nil UsedCreditsUSD", "extra-usage-cost-usd", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage.UsedCreditsUSD = nil }},
		{"extra-usage-limit-usd nil ExtraUsage", "extra-usage-limit-usd", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage = nil }},
		{"extra-usage-limit-usd nil MonthlyLimitUSD", "extra-usage-limit-usd", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage.MonthlyLimitUSD = nil }},
		{"extra-usage-percent nil ExtraUsage", "extra-usage-percent", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage = nil }},
		{"extra-usage-percent nil Utilization", "extra-usage-percent", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage.Utilization = nil }},
		{"seven-day-opus-percent nil", "seven-day-opus-percent", func(s *schema.StatusSnapshot) { s.Account.SevenDayOpus = nil }},
		{"seven-day-sonnet-percent nil", "seven-day-sonnet-percent", func(s *schema.StatusSnapshot) { s.Account.SevenDaySonnet = nil }},
		{"seven-day-opus-reset-minutes nil window", "seven-day-opus-reset-minutes", func(s *schema.StatusSnapshot) { s.Account.SevenDayOpus = nil }},
		{"seven-day-sonnet-reset-minutes nil window", "seven-day-sonnet-reset-minutes", func(s *schema.StatusSnapshot) { s.Account.SevenDaySonnet = nil }},
		{"seven-day-opus-reset-minutes zero clock", "seven-day-opus-reset-minutes", func(s *schema.StatusSnapshot) {}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snap := extraUsageSnapshot()
			c.mutate(&snap)
			useOpts := opts
			if c.name == "seven-day-opus-reset-minutes zero clock" {
				useOpts = Options{Width: 120}
			}
			_, ok := metricValue(c.metric, snap, config.ToolConfig{}, useOpts)
			if ok {
				t.Errorf("metricValue(%q) ok = true, want false", c.metric)
			}
		})
	}
}

func TestRenderContent_ExtraUsageAndWeeklyPerModel(t *testing.T) {
	snap := extraUsageSnapshot()
	opts := Options{Width: 120, Now: fixedNow}

	cases := []struct {
		name string
		spec config.WidgetSpec
		want string
	}{
		{"extra-usage-cost", config.WidgetSpec{Type: "extra-usage-cost"}, "$12.50"},
		{"extra-usage-cost raw", config.WidgetSpec{Type: "extra-usage-cost", RawValue: true}, "12.50"},
		{"extra-usage-limit", config.WidgetSpec{Type: "extra-usage-limit"}, "$50.00"},
		{"extra-usage-limit raw", config.WidgetSpec{Type: "extra-usage-limit", RawValue: true}, "50.00"},
		{"extra-usage-percent", config.WidgetSpec{Type: "extra-usage-percent"}, "25%"},
		{"weekly-usage-opus", config.WidgetSpec{Type: "weekly-usage-opus"}, "41%"},
		{"weekly-usage-sonnet", config.WidgetSpec{Type: "weekly-usage-sonnet"}, "63%"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderContent(c.spec, snap, config.ToolConfig{}, opts, false)
			if got != c.want {
				t.Errorf("renderContent(%q) = %q, want %q", c.spec.Type, got, c.want)
			}
		})
	}

	// weekly-reset-opus/sonnet render a countdown string; assert it mirrors
	// resetWidget's non-empty output rather than pinning the exact format
	// (already covered by existing five-hour-reset/weekly-reset golden tests).
	t.Run("weekly-reset-opus", func(t *testing.T) {
		got := renderContent(config.WidgetSpec{Type: "weekly-reset-opus"}, snap, config.ToolConfig{}, opts, false)
		want := resetWidget(snap.Account.SevenDayOpus, opts.Now)
		if got != want {
			t.Errorf("renderContent(weekly-reset-opus) = %q, want %q", got, want)
		}
		if got == "" {
			t.Error("expected non-empty countdown text")
		}
	})
	t.Run("weekly-reset-sonnet", func(t *testing.T) {
		got := renderContent(config.WidgetSpec{Type: "weekly-reset-sonnet"}, snap, config.ToolConfig{}, opts, false)
		want := resetWidget(snap.Account.SevenDaySonnet, opts.Now)
		if got != want {
			t.Errorf("renderContent(weekly-reset-sonnet) = %q, want %q", got, want)
		}
		if got == "" {
			t.Error("expected non-empty countdown text")
		}
	})
}

func TestRenderContent_ExtraUsageAndWeeklyPerModel_Hidden(t *testing.T) {
	opts := Options{Width: 120, Now: fixedNow}
	cases := []struct {
		name   string
		typ    string
		mutate func(*schema.StatusSnapshot)
	}{
		{"extra-usage-cost nil ExtraUsage", "extra-usage-cost", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage = nil }},
		{"extra-usage-cost nil UsedCreditsUSD", "extra-usage-cost", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage.UsedCreditsUSD = nil }},
		{"extra-usage-limit nil ExtraUsage", "extra-usage-limit", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage = nil }},
		{"extra-usage-limit nil MonthlyLimitUSD", "extra-usage-limit", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage.MonthlyLimitUSD = nil }},
		{"extra-usage-percent nil ExtraUsage", "extra-usage-percent", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage = nil }},
		{"extra-usage-percent nil Utilization", "extra-usage-percent", func(s *schema.StatusSnapshot) { s.Account.ExtraUsage.Utilization = nil }},
		{"weekly-usage-opus nil", "weekly-usage-opus", func(s *schema.StatusSnapshot) { s.Account.SevenDayOpus = nil }},
		{"weekly-usage-sonnet nil", "weekly-usage-sonnet", func(s *schema.StatusSnapshot) { s.Account.SevenDaySonnet = nil }},
		{"weekly-reset-opus nil", "weekly-reset-opus", func(s *schema.StatusSnapshot) { s.Account.SevenDayOpus = nil }},
		{"weekly-reset-sonnet nil", "weekly-reset-sonnet", func(s *schema.StatusSnapshot) { s.Account.SevenDaySonnet = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snap := extraUsageSnapshot()
			c.mutate(&snap)
			got := renderContent(config.WidgetSpec{Type: c.typ}, snap, config.ToolConfig{}, opts, false)
			if got != "" {
				t.Errorf("renderContent(%q) = %q, want \"\" (hidden)", c.typ, got)
			}
		})
	}

	t.Run("weekly-reset-opus zero clock", func(t *testing.T) {
		snap := extraUsageSnapshot()
		got := renderContent(config.WidgetSpec{Type: "weekly-reset-opus"}, snap, config.ToolConfig{}, Options{Width: 120}, false)
		if got != "" {
			t.Errorf("renderContent(weekly-reset-opus) with zero clock = %q, want \"\"", got)
		}
	})
}
