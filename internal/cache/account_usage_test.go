package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ptrFloat64(v float64) *float64 { return &v }

func TestAccountUsageRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	observed := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	env := AccountUsageEnvelope{
		Source:     "oauth-usage-api",
		ObservedAt: observed,
		ExpiresAt:  observed.Add(usageFreshTTL),
		StaleUntil: observed.Add(usageStaleTTL),
		FiveHour: &RateWindowState{
			UsedPercentage: 12,
			ResetsAt:       observed.Add(3 * time.Hour),
		},
		SevenDay: &RateWindowState{
			UsedPercentage: 34,
			ResetsAt:       observed.Add(48 * time.Hour),
		},
		SevenDayOpus: &RateWindowState{
			UsedPercentage: 56,
			ResetsAt:       observed.Add(48 * time.Hour),
		},
		SevenDaySonnet: nil,
		ExtraUsage: &ExtraUsageState{
			Enabled:      true,
			MonthlyLimit: ptrFloat64(100),
			UsedCredits:  ptrFloat64(25.5),
			Utilization:  ptrFloat64(25.5),
		},
	}

	if err := StoreAccountUsage("default", env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	got, stale, ok := LoadAccountUsage("default", observed.Add(1*time.Minute))
	if !ok {
		t.Fatalf("LoadAccountUsage() ok = false, want true")
	}
	if stale {
		t.Fatalf("LoadAccountUsage() stale = true, want false (within fresh TTL)")
	}
	if got == nil {
		t.Fatalf("LoadAccountUsage() env = nil, want value")
	}
	if got.SchemaVersion != usageAccountSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", got.SchemaVersion, usageAccountSchemaVersion)
	}
	if got.FiveHour == nil || got.FiveHour.UsedPercentage != 12 {
		t.Fatalf("FiveHour = %+v, want UsedPercentage 12", got.FiveHour)
	}
	if got.SevenDayOpus == nil || got.SevenDayOpus.UsedPercentage != 56 {
		t.Fatalf("SevenDayOpus = %+v, want UsedPercentage 56", got.SevenDayOpus)
	}
	if got.SevenDaySonnet != nil {
		t.Fatalf("SevenDaySonnet = %+v, want nil", got.SevenDaySonnet)
	}
	if got.ExtraUsage == nil || got.ExtraUsage.UsedCredits == nil || *got.ExtraUsage.UsedCredits != 25.5 {
		t.Fatalf("ExtraUsage = %+v, want UsedCredits 25.5", got.ExtraUsage)
	}
}

func TestLoadAccountUsage_Freshness(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	observed := now.Add(-20 * time.Minute)
	env := AccountUsageEnvelope{
		Source:     "oauth-usage-api",
		ObservedAt: observed,
		ExpiresAt:  now.Add(-5 * time.Minute),
		StaleUntil: now.Add(5 * time.Hour),
	}
	if err := StoreAccountUsage("default", env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	got, stale, ok := LoadAccountUsage("default", now)
	if !ok {
		t.Fatalf("LoadAccountUsage() ok = false, want true (within StaleUntil)")
	}
	if !stale {
		t.Fatalf("LoadAccountUsage() stale = false, want true (past ExpiresAt)")
	}
	if got == nil {
		t.Fatalf("LoadAccountUsage() env = nil, want value")
	}
}

func TestLoadAccountUsage_PastStaleUntil(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	observed := now.Add(-7 * time.Hour)
	env := AccountUsageEnvelope{
		Source:     "oauth-usage-api",
		ObservedAt: observed,
		ExpiresAt:  observed.Add(usageFreshTTL),
		StaleUntil: observed.Add(usageStaleTTL), // = now - 1h, already past
	}
	if err := StoreAccountUsage("default", env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	got, stale, ok := LoadAccountUsage("default", now)
	if ok {
		t.Fatalf("LoadAccountUsage() ok = true, want false (past StaleUntil)")
	}
	if stale {
		t.Fatalf("LoadAccountUsage() stale = true, want false when ok = false")
	}
	if got != nil {
		t.Fatalf("LoadAccountUsage() env = %+v, want nil", got)
	}
}

func TestLoadAccountUsage_Missing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	got, stale, ok := LoadAccountUsage("default", time.Now())
	if ok || stale || got != nil {
		t.Fatalf("LoadAccountUsage() = (%+v, %v, %v), want (nil, false, false)", got, stale, ok)
	}
}

func TestLoadAccountUsage_Corrupt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	path, err := accountUsagePath("default")
	if err != nil {
		t.Fatalf("accountUsagePath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, stale, ok := LoadAccountUsage("default", time.Now())
	if ok || stale || got != nil {
		t.Fatalf("LoadAccountUsage() = (%+v, %v, %v), want (nil, false, false)", got, stale, ok)
	}
}

func TestNewAccountUsageEnvelope(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)

	env := NewAccountUsageEnvelope(now)

	if env.SchemaVersion != usageAccountSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", env.SchemaVersion, usageAccountSchemaVersion)
	}
	if env.Source != "oauth-usage-api" {
		t.Fatalf("Source = %q, want %q", env.Source, "oauth-usage-api")
	}
	if !env.ObservedAt.Equal(now) {
		t.Fatalf("ObservedAt = %v, want %v", env.ObservedAt, now)
	}
	if !env.ExpiresAt.Equal(now.Add(usageFreshTTL)) {
		t.Fatalf("ExpiresAt = %v, want %v", env.ExpiresAt, now.Add(usageFreshTTL))
	}
	if !env.StaleUntil.Equal(now.Add(usageStaleTTL)) {
		t.Fatalf("StaleUntil = %v, want %v", env.StaleUntil, now.Add(usageStaleTTL))
	}
}

func TestNextUsageDue(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		failures int
		want     time.Time
	}{
		{"no failures", 0, now.Add(5 * time.Minute)},
		{"one failure", 1, now.Add(10 * time.Minute)},
		{"three failures", 3, now.Add(40 * time.Minute)},
		{"large failure count capped", 1000, now.Add(60 * time.Minute)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextUsageDue(now, tc.failures)
			if !got.Equal(tc.want) {
				t.Fatalf("NextUsageDue(now, %d) = %v, want %v", tc.failures, got, tc.want)
			}
		})
	}
}

func TestAccountUsageDue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)

	if !AccountUsageDue(now) {
		t.Fatalf("AccountUsageDue() = false, want true for zero-value manifest")
	}

	m := LoadRefreshManifest()
	m.AccountUsage.NextDueAt = now.Add(1 * time.Hour)
	if err := StoreRefreshManifest(m); err != nil {
		t.Fatalf("StoreRefreshManifest() error = %v", err)
	}
	if AccountUsageDue(now) {
		t.Fatalf("AccountUsageDue() = true, want false when NextDueAt is in the future")
	}

	m2 := LoadRefreshManifest()
	m2.AccountUsage.NextDueAt = now.Add(-1 * time.Hour)
	if err := StoreRefreshManifest(m2); err != nil {
		t.Fatalf("StoreRefreshManifest() error = %v", err)
	}
	if !AccountUsageDue(now) {
		t.Fatalf("AccountUsageDue() = false, want true when NextDueAt is in the past")
	}
}
