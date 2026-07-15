package cli

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/schema"
	"github.com/yacchi/statusloom/internal/usage"
)

func floatPtr(f float64) *float64 { return &f }

// TestApplyExtraUsageCache_Populates stores an OAuth-usage envelope and
// asserts applyExtraUsageCache folds the extra-usage state and the per-model
// seven-day windows into the snapshot, while leaving FiveHour/SevenDay alone.
func TestApplyExtraUsageCache_Populates(t *testing.T) {
	setupEnv(t)
	now := time.Now()
	env := cache.NewAccountUsageEnvelope(now)
	env.SevenDayOpus = &cache.RateWindowState{UsedPercentage: 42, ResetsAt: now.Add(3 * time.Hour)}
	env.SevenDaySonnet = &cache.RateWindowState{UsedPercentage: 7, ResetsAt: now.Add(4 * time.Hour)}
	env.ExtraUsage = &cache.ExtraUsageState{
		Enabled:      true,
		MonthlyLimit: floatPtr(100),
		UsedCredits:  floatPtr(12.5),
		Utilization:  floatPtr(12.5),
	}
	if err := cache.StoreAccountUsage(accountCacheKey, env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	var snap schema.StatusSnapshot
	applyExtraUsageCache(&snap, now.Add(1*time.Minute))

	if snap.Account.ExtraUsage == nil {
		t.Fatal("ExtraUsage = nil, want populated")
	}
	if !snap.Account.ExtraUsage.Enabled {
		t.Error("ExtraUsage.Enabled = false, want true")
	}
	if snap.Account.ExtraUsage.Stale {
		t.Error("ExtraUsage.Stale = true, want false (within fresh TTL)")
	}
	if got := snap.Account.ExtraUsage.MonthlyLimitUSD; got == nil || *got != 100 {
		t.Errorf("MonthlyLimitUSD = %v, want 100", got)
	}
	if snap.Account.SevenDayOpus == nil || snap.Account.SevenDayOpus.UsedPercentage != 42 {
		t.Errorf("SevenDayOpus = %v, want UsedPercentage 42", snap.Account.SevenDayOpus)
	}
	if snap.Account.SevenDaySonnet == nil || snap.Account.SevenDaySonnet.UsedPercentage != 7 {
		t.Errorf("SevenDaySonnet = %v, want UsedPercentage 7", snap.Account.SevenDaySonnet)
	}
	if snap.Account.FiveHour != nil || snap.Account.SevenDay != nil {
		t.Error("FiveHour/SevenDay must be left untouched by applyExtraUsageCache")
	}
}

// TestApplyExtraUsageCache_StaleBoundary asserts Stale reflects the
// ExpiresAt (fresh-TTL) boundary of the loaded envelope.
func TestApplyExtraUsageCache_StaleBoundary(t *testing.T) {
	setupEnv(t)
	now := time.Now()
	env := cache.NewAccountUsageEnvelope(now)
	env.ExtraUsage = &cache.ExtraUsageState{Enabled: true, Utilization: floatPtr(50)}
	if err := cache.StoreAccountUsage(accountCacheKey, env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	// 20 minutes later: past ExpiresAt (now+15m) but within StaleUntil (now+6h).
	var snap schema.StatusSnapshot
	applyExtraUsageCache(&snap, now.Add(20*time.Minute))
	if snap.Account.ExtraUsage == nil {
		t.Fatal("ExtraUsage = nil, want populated")
	}
	if !snap.Account.ExtraUsage.Stale {
		t.Error("ExtraUsage.Stale = false, want true (past ExpiresAt)")
	}
}

// TestApplyExtraUsageCache_NoFile asserts a missing envelope leaves the
// snapshot untouched.
func TestApplyExtraUsageCache_NoFile(t *testing.T) {
	setupEnv(t)
	var snap schema.StatusSnapshot
	applyExtraUsageCache(&snap, time.Now())
	if snap.Account.ExtraUsage != nil || snap.Account.SevenDayOpus != nil || snap.Account.SevenDaySonnet != nil {
		t.Error("snapshot must be untouched when no envelope exists")
	}
}

// swapUsageSeams overrides the account-usage seams for the duration of a test
// and restores them (and the TestMain defaults) via t.Cleanup.
func swapUsageSeams(t *testing.T, tok func(func(string) string) (string, error), fetch func(context.Context, string, string) (*usage.Report, int, error)) {
	t.Helper()
	prevTok, prevFetch := acquireUsageToken, fetchUsage
	acquireUsageToken = tok
	fetchUsage = fetch
	t.Cleanup(func() {
		acquireUsageToken = prevTok
		fetchUsage = prevFetch
	})
}

// TestRunRefresh_UsageSuccess: the fetch succeeds -> envelope is stored and
// the manifest schedule records success with Failures=0 and a normal-cadence
// NextDueAt.
func TestRunRefresh_UsageSuccess(t *testing.T) {
	setupEnv(t)
	report := &usage.Report{
		FiveHour:     &usage.Window{Utilization: 10, ResetsAt: time.Now().Add(1 * time.Hour)},
		SevenDayOpus: &usage.Window{Utilization: 20, ResetsAt: time.Now().Add(2 * time.Hour)},
		Extra:        &usage.Extra{IsEnabled: true, MonthlyLimit: floatPtr(100), UsedCredits: floatPtr(5), Utilization: floatPtr(5)},
	}
	swapUsageSeams(t,
		func(func(string) string) (string, error) { return "tok-abc", nil },
		func(context.Context, string, string) (*usage.Report, int, error) { return report, 200, nil },
	)

	if code := runRefresh([]string{"--once", "--cc-version", "9.9.9"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("runRefresh() code = %d, want 0", code)
	}

	now := time.Now()
	got, _, ok := cache.LoadAccountUsage(accountCacheKey, now)
	if !ok || got == nil {
		t.Fatal("expected an account-usage envelope to be stored")
	}
	if got.ExtraUsage == nil || !got.ExtraUsage.Enabled {
		t.Error("stored envelope missing extra-usage state")
	}
	if got.SevenDayOpus == nil || got.SevenDayOpus.UsedPercentage != 20 {
		t.Errorf("stored SevenDayOpus = %v, want 20", got.SevenDayOpus)
	}

	m := cache.LoadRefreshManifest()
	if m.AccountUsage.Failures != 0 {
		t.Errorf("Failures = %d, want 0", m.AccountUsage.Failures)
	}
	if m.AccountUsage.LastSuccess.IsZero() {
		t.Error("LastSuccess not set on success")
	}
	wantDue := now.Add(cache.UsageRefreshInterval)
	if diff := m.AccountUsage.NextDueAt.Sub(wantDue); diff > 2*time.Second || diff < -2*time.Second {
		t.Errorf("NextDueAt = %v, want ~%v", m.AccountUsage.NextDueAt, wantDue)
	}
}

// TestRunRefresh_UsageFailure: fetch returns ErrRateLimited -> Failures
// increments, NextDueAt backs off beyond the normal cadence, and NO envelope
// is stored (a previously good one survives).
func TestRunRefresh_UsageFailure(t *testing.T) {
	setupEnv(t)
	// Seed a prior good envelope to prove it is preserved.
	prior := cache.NewAccountUsageEnvelope(time.Now())
	prior.ExtraUsage = &cache.ExtraUsageState{Enabled: true, Utilization: floatPtr(33)}
	if err := cache.StoreAccountUsage(accountCacheKey, prior); err != nil {
		t.Fatalf("seed StoreAccountUsage() error = %v", err)
	}

	swapUsageSeams(t,
		func(func(string) string) (string, error) { return "tok-abc", nil },
		func(context.Context, string, string) (*usage.Report, int, error) {
			return nil, 429, usage.ErrRateLimited
		},
	)

	if code := runRefresh([]string{"--once", "--cc-version", "1.0.0"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("runRefresh() code = %d, want 0", code)
	}

	now := time.Now()
	m := cache.LoadRefreshManifest()
	if m.AccountUsage.Failures != 1 {
		t.Errorf("Failures = %d, want 1", m.AccountUsage.Failures)
	}
	if !m.AccountUsage.LastSuccess.IsZero() {
		t.Error("LastSuccess must not be set on failure")
	}
	// Backoff: NextDueAt must be strictly beyond the normal cadence.
	minBackoff := now.Add(cache.UsageRefreshInterval)
	if !m.AccountUsage.NextDueAt.After(minBackoff) {
		t.Errorf("NextDueAt = %v, want after %v (backoff)", m.AccountUsage.NextDueAt, minBackoff)
	}

	// Prior envelope must be intact (unchanged utilization).
	got, _, ok := cache.LoadAccountUsage(accountCacheKey, now)
	if !ok || got == nil || got.ExtraUsage == nil || got.ExtraUsage.Utilization == nil || *got.ExtraUsage.Utilization != 33 {
		t.Errorf("prior envelope not preserved: %+v", got)
	}
}

// TestRunRefresh_UsageNoToken: no OAuth token -> reschedule at normal cadence
// without a failure, and fetchUsage must NOT be called.
func TestRunRefresh_UsageNoToken(t *testing.T) {
	setupEnv(t)
	fetchCalled := false
	swapUsageSeams(t,
		func(func(string) string) (string, error) { return "", usage.ErrNoToken },
		func(context.Context, string, string) (*usage.Report, int, error) {
			fetchCalled = true
			return nil, 0, usage.ErrRateLimited
		},
	)

	if code := runRefresh([]string{"--once", "--cc-version", "1.0.0"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("runRefresh() code = %d, want 0", code)
	}

	if fetchCalled {
		t.Error("fetchUsage must NOT be called when no token is available")
	}

	now := time.Now()
	m := cache.LoadRefreshManifest()
	if m.AccountUsage.Failures != 0 {
		t.Errorf("Failures = %d, want 0 (no-token is not a failure)", m.AccountUsage.Failures)
	}
	if m.AccountUsage.LastAttempt.IsZero() {
		t.Error("LastAttempt should be set even on the no-token path")
	}
	wantDue := now.Add(cache.UsageRefreshInterval)
	if diff := m.AccountUsage.NextDueAt.Sub(wantDue); diff > 2*time.Second || diff < -2*time.Second {
		t.Errorf("NextDueAt = %v, want ~%v", m.AccountUsage.NextDueAt, wantDue)
	}

	// No envelope should have been stored.
	if _, _, ok := cache.LoadAccountUsage(accountCacheKey, now); ok {
		t.Error("no envelope should be stored on the no-token path")
	}
}
