package render

import (
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/schema"
)

// promptCacheSnapshot returns a snapshot whose session carries a fully
// populated stdin prompt_cache object, with ExpiresAt 42 minutes after now.
func promptCacheSnapshot(now time.Time) schema.StatusSnapshot {
	snap := schema.StatusSnapshot{}
	hitRatio := 0.91
	recache := 45000
	expires := now.Add(42 * time.Minute)
	snap.Session.PromptCache = &schema.PromptCache{
		Warm:                true,
		TTL:                 "1h",
		ExpiresAt:           &expires,
		Requests:            14,
		Misses:              2,
		HitRatio:            &hitRatio,
		RecacheTokensIfCold: &recache,
		LastMissCauses:      []string{"tools_changed", "ttl"},
	}
	return snap
}

func renderPromptCacheField(t *testing.T, typ string, snap schema.StatusSnapshot, opts Options) string {
	t.Helper()
	return renderContent(config.WidgetSpec{Type: typ}, snap, config.ToolConfig{}, opts, false)
}

func TestPromptCacheFieldTexts(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	opts := Options{Width: 120, Now: now}
	snap := promptCacheSnapshot(now)

	cases := map[string]string{
		"cache-warm":            "warm",
		"cache-ttl":             "1h",
		"cache-expires-at":      "42m",
		"cache-hit-ratio":       "91%",
		"cache-requests":        "14",
		"cache-misses":          "2",
		"cache-recache-tokens":  "45,000",
		"cache-last-miss-cause": "tools_changed,ttl",
	}
	for typ, want := range cases {
		if got := renderPromptCacheField(t, typ, snap, opts); got != want {
			t.Errorf("%s = %q, want %q", typ, got, want)
		}
	}

	if got, want := renderContent(config.WidgetSpec{Type: "cache-recache-tokens"}, snap, config.ToolConfig{}, opts, true), renderInteger(45000, false, true); got != want {
		t.Errorf("compact cache-recache-tokens = %q, want %q", got, want)
	}
	if got := renderContent(config.WidgetSpec{Type: "cache-recache-tokens", RawValue: true}, snap, config.ToolConfig{}, opts, false); got != "45000" {
		t.Errorf("raw cache-recache-tokens = %q, want 45000", got)
	}

	cold := promptCacheSnapshot(now)
	cold.Session.PromptCache.Warm = false
	if got := renderPromptCacheField(t, "cache-warm", cold, opts); got != "cold" {
		t.Errorf("cache-warm with Warm=false = %q, want cold", got)
	}
}

func TestPromptCacheExpiredFieldAndMetric(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	opts := Options{Width: 120, Now: now}
	snap := promptCacheSnapshot(now)
	past := now.Add(-5 * time.Minute)
	snap.Session.PromptCache.ExpiresAt = &past

	if got := renderPromptCacheField(t, "cache-expires-at", snap, opts); got != "0m" {
		t.Errorf("cache-expires-at past = %q, want 0m", got)
	}
	v, ok := metricValue("cache-expires-minutes", snap, config.ToolConfig{}, opts)
	if !ok || v != 0 {
		t.Errorf("cache-expires-minutes past = (%v, %v), want (0, true)", v, ok)
	}
}

func TestPromptCacheZeroClockHidesExpiry(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	snap := promptCacheSnapshot(now)

	if got := renderPromptCacheField(t, "cache-expires-at", snap, Options{Width: 120}); got != "" {
		t.Errorf("cache-expires-at with zero clock = %q, want empty", got)
	}
	if _, ok := metricValue("cache-expires-minutes", snap, config.ToolConfig{}, Options{Width: 120}); ok {
		t.Error("cache-expires-minutes with zero clock resolved, want unresolved")
	}
}

func TestPromptCacheMetrics(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	opts := Options{Width: 120, Now: now}
	snap := promptCacheSnapshot(now)

	cases := map[string]float64{
		"cache-hit-ratio-percent": 91,
		"cache-expires-minutes":   42,
		"cache-requests":          14,
		"cache-misses":            2,
		"cache-recache-tokens":    45000,
	}
	for name, want := range cases {
		got, ok := metricValue(name, snap, config.ToolConfig{}, opts)
		if !ok {
			t.Errorf("%s unresolved, want %v", name, want)
			continue
		}
		if diff := got - want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

func TestPromptCacheNilOptionalsRenderEmpty(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	opts := Options{Width: 120, Now: now}
	snap := promptCacheSnapshot(now)
	snap.Session.PromptCache.ExpiresAt = nil
	snap.Session.PromptCache.HitRatio = nil
	snap.Session.PromptCache.RecacheTokensIfCold = nil
	snap.Session.PromptCache.LastMissCauses = nil

	for _, typ := range []string{"cache-expires-at", "cache-hit-ratio", "cache-recache-tokens", "cache-last-miss-cause"} {
		if got := renderPromptCacheField(t, typ, snap, opts); got != "" {
			t.Errorf("%s with nil/empty source = %q, want empty", typ, got)
		}
	}
	for _, name := range []string{"cache-expires-minutes", "cache-hit-ratio-percent", "cache-recache-tokens"} {
		if _, ok := metricValue(name, snap, config.ToolConfig{}, opts); ok {
			t.Errorf("%s resolved with nil source, want unresolved", name)
		}
	}
}

func TestPromptCacheAbsentSessionRendersNothing(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	opts := Options{Width: 120, Now: now}
	snap := schema.StatusSnapshot{}

	for _, typ := range []string{
		"cache-warm", "cache-ttl", "cache-expires-at", "cache-hit-ratio",
		"cache-requests", "cache-misses", "cache-recache-tokens", "cache-last-miss-cause",
	} {
		if got := renderPromptCacheField(t, typ, snap, opts); got != "" {
			t.Errorf("%s with nil PromptCache = %q, want empty", typ, got)
		}
	}
	for _, name := range []string{
		"cache-expires-minutes", "cache-hit-ratio-percent", "cache-requests",
		"cache-misses", "cache-recache-tokens",
	} {
		if _, ok := metricValue(name, snap, config.ToolConfig{}, opts); ok {
			t.Errorf("%s resolved with nil PromptCache, want unresolved", name)
		}
	}
}
