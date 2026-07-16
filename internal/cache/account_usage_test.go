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

// TestLoadExtraUsage_Fresh: an ExtraUsage record observed just now is
// reported fresh (stale=false) and ok=true.
func TestLoadExtraUsage_Fresh(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	env := NewAccountUsageEnvelope(now)
	env.ExtraUsage = &ExtraUsageState{Enabled: true, UsedCredits: ptrFloat64(5), ObservedAt: now}
	if err := StoreAccountUsage("default", env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	eu, stale, ok := LoadExtraUsage("default", now.Add(1*time.Minute))
	if !ok {
		t.Fatalf("LoadExtraUsage() ok = false, want true")
	}
	if stale {
		t.Fatalf("LoadExtraUsage() stale = true, want false (within fresh TTL)")
	}
	if eu == nil || eu.UsedCredits == nil || *eu.UsedCredits != 5 {
		t.Fatalf("LoadExtraUsage() eu = %+v, want UsedCredits 5", eu)
	}
}

// TestLoadExtraUsage_StaleButRetained: past the 15m fresh TTL but within the
// 30-day retention window, the value is still returned (ok=true) marked
// stale.
func TestLoadExtraUsage_StaleButRetained(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	observed := now.Add(-20 * time.Minute)
	env := NewAccountUsageEnvelope(observed)
	env.ExtraUsage = &ExtraUsageState{Enabled: true, UsedCredits: ptrFloat64(7), ObservedAt: observed}
	if err := StoreAccountUsage("default", env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	eu, stale, ok := LoadExtraUsage("default", now)
	if !ok {
		t.Fatalf("LoadExtraUsage() ok = false, want true (within retention)")
	}
	if !stale {
		t.Fatalf("LoadExtraUsage() stale = false, want true (past fresh TTL)")
	}
	if eu == nil || eu.UsedCredits == nil || *eu.UsedCredits != 7 {
		t.Fatalf("LoadExtraUsage() eu = %+v, want UsedCredits 7", eu)
	}
}

// TestLoadExtraUsage_SurvivesWindowsStaleUntil proves ExtraUsage is fully
// decoupled from the rate-window envelope's 6h StaleUntil: LoadAccountUsage
// reports the envelope absent (ok=false) once StaleUntil has passed, but
// LoadExtraUsage still returns the ExtraUsage value (ok=true).
func TestLoadExtraUsage_SurvivesWindowsStaleUntil(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	observed := now.Add(-7 * time.Hour) // past usageStaleTTL (6h)
	env := AccountUsageEnvelope{
		Source:     "oauth-usage-api",
		ObservedAt: observed,
		ExpiresAt:  observed.Add(usageFreshTTL),
		StaleUntil: observed.Add(usageStaleTTL),
		ExtraUsage: &ExtraUsageState{Enabled: true, UsedCredits: ptrFloat64(9), ObservedAt: observed},
	}
	if err := StoreAccountUsage("default", env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	// The windows envelope itself must be reported absent past StaleUntil.
	if _, _, ok := LoadAccountUsage("default", now); ok {
		t.Fatalf("LoadAccountUsage() ok = true, want false (past StaleUntil)")
	}

	// But ExtraUsage must still be available: it has its own retention TTL
	// (30 days), independent of the windows' 6h StaleUntil.
	eu, stale, ok := LoadExtraUsage("default", now)
	if !ok {
		t.Fatalf("LoadExtraUsage() ok = false, want true (independent of windows StaleUntil)")
	}
	if !stale {
		t.Fatalf("LoadExtraUsage() stale = false, want true (well past fresh TTL)")
	}
	if eu == nil || eu.UsedCredits == nil || *eu.UsedCredits != 9 {
		t.Fatalf("LoadExtraUsage() eu = %+v, want UsedCredits 9", eu)
	}

	// And the raw envelope must still be readable via LoadAccountUsageRaw,
	// ignoring TTLs entirely.
	raw, ok := LoadAccountUsageRaw("default")
	if !ok || raw == nil {
		t.Fatalf("LoadAccountUsageRaw() ok = %v, raw = %+v, want ok=true", ok, raw)
	}
}

// TestLoadExtraUsage_RetentionExpired: past extraUsageRetentionTTL (30 days)
// the value is dropped entirely (ok=false).
func TestLoadExtraUsage_RetentionExpired(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	observed := now.Add(-31 * 24 * time.Hour) // past 30-day retention
	env := AccountUsageEnvelope{
		Source:     "oauth-usage-api",
		ObservedAt: observed,
		ExpiresAt:  observed.Add(usageFreshTTL),
		StaleUntil: observed.Add(usageStaleTTL),
		ExtraUsage: &ExtraUsageState{Enabled: true, UsedCredits: ptrFloat64(11), ObservedAt: observed},
	}
	if err := StoreAccountUsage("default", env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	eu, stale, ok := LoadExtraUsage("default", now)
	if ok || stale || eu != nil {
		t.Fatalf("LoadExtraUsage() = (%+v, %v, %v), want (nil, false, false) past retention", eu, stale, ok)
	}
}

// TestLoadExtraUsage_LegacyFallback: an ExtraUsage record with a zero
// ObservedAt (written before the field existed) falls back to the enclosing
// envelope's ObservedAt for freshness/retention purposes.
func TestLoadExtraUsage_LegacyFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	observed := now.Add(-20 * time.Minute)
	env := NewAccountUsageEnvelope(observed)
	env.ExtraUsage = &ExtraUsageState{Enabled: true, UsedCredits: ptrFloat64(13)} // ObservedAt left zero

	if err := StoreAccountUsage("default", env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	eu, stale, ok := LoadExtraUsage("default", now)
	if !ok {
		t.Fatalf("LoadExtraUsage() ok = false, want true (legacy fallback to envelope ObservedAt)")
	}
	if !stale {
		t.Fatalf("LoadExtraUsage() stale = false, want true (envelope observed 20m ago > fresh TTL)")
	}
	if eu == nil || eu.UsedCredits == nil || *eu.UsedCredits != 13 {
		t.Fatalf("LoadExtraUsage() eu = %+v, want UsedCredits 13", eu)
	}
}

// TestLoadExtraUsage_NoEnvelope: a missing envelope reports ok=false.
func TestLoadExtraUsage_NoEnvelope(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	eu, stale, ok := LoadExtraUsage("default", time.Now())
	if ok || stale || eu != nil {
		t.Fatalf("LoadExtraUsage() = (%+v, %v, %v), want (nil, false, false)", eu, stale, ok)
	}
}

// TestLoadAccountUsageRaw_Missing: a missing envelope reports ok=false.
func TestLoadAccountUsageRaw_Missing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	env, ok := LoadAccountUsageRaw("default")
	if ok || env != nil {
		t.Fatalf("LoadAccountUsageRaw() = (%+v, %v), want (nil, false)", env, ok)
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
