package cache

import (
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// TestResolveAccountCacheKey covers the three cases ResolveAccountCacheKey
// must handle: no profile, a profile with no OrganizationUUID (both fall back
// to AccountCacheKey), and a profile with an OrganizationUUID (used as-is).
func TestResolveAccountCacheKey(t *testing.T) {
	if got := ResolveAccountCacheKey(nil); got != AccountCacheKey {
		t.Errorf("ResolveAccountCacheKey(nil) = %q, want %q", got, AccountCacheKey)
	}
	if got := ResolveAccountCacheKey(&schema.AccountProfile{Email: "a@example.com"}); got != AccountCacheKey {
		t.Errorf("ResolveAccountCacheKey(profile with empty OrganizationUUID) = %q, want %q", got, AccountCacheKey)
	}
	profile := &schema.AccountProfile{OrganizationUUID: "org-uuid-123"}
	if got := ResolveAccountCacheKey(profile); got != "org-uuid-123" {
		t.Errorf("ResolveAccountCacheKey(profile) = %q, want org-uuid-123", got)
	}
}

// TestApplyExtraUsageCache_Populates stores an OAuth-usage envelope and
// asserts ApplyExtraUsageCache folds the extra-usage state and the per-model
// seven-day windows into the snapshot, while leaving FiveHour/SevenDay alone.
func TestApplyExtraUsageCache_Populates(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()
	env := NewAccountUsageEnvelope(now)
	env.SevenDayOpus = &RateWindowState{UsedPercentage: 42, ResetsAt: now.Add(3 * time.Hour)}
	env.SevenDaySonnet = &RateWindowState{UsedPercentage: 7, ResetsAt: now.Add(4 * time.Hour)}
	env.ExtraUsage = &ExtraUsageState{
		Enabled:      true,
		MonthlyLimit: ptrFloat64(100),
		UsedCredits:  ptrFloat64(12.5),
		Utilization:  ptrFloat64(12.5),
	}
	if err := StoreAccountUsage(AccountCacheKey, env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	var snap schema.StatusSnapshot
	ApplyExtraUsageCache(AccountCacheKey, &snap, now.Add(1*time.Minute))

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
		t.Error("FiveHour/SevenDay must be left untouched by ApplyExtraUsageCache")
	}
}

// TestApplyExtraUsageCache_StaleBoundary asserts Stale reflects the
// ExpiresAt (fresh-TTL) boundary of the loaded envelope.
func TestApplyExtraUsageCache_StaleBoundary(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()
	env := NewAccountUsageEnvelope(now)
	env.ExtraUsage = &ExtraUsageState{Enabled: true, Utilization: ptrFloat64(50)}
	if err := StoreAccountUsage(AccountCacheKey, env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	// 20 minutes later: past ExpiresAt (now+15m) but within StaleUntil (now+6h).
	var snap schema.StatusSnapshot
	ApplyExtraUsageCache(AccountCacheKey, &snap, now.Add(20*time.Minute))
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
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	var snap schema.StatusSnapshot
	ApplyExtraUsageCache(AccountCacheKey, &snap, time.Now())
	if snap.Account.ExtraUsage != nil || snap.Account.SevenDayOpus != nil || snap.Account.SevenDaySonnet != nil {
		t.Error("snapshot must be untouched when no envelope exists")
	}
}

// TestApplyAccountCache_StoresAndFillsFromCache asserts ApplyAccountCache
// stores stdin-derived windows to the shared account cache, and separately
// fills in a snapshot missing both windows from whatever was last stored,
// marking Account.Stale in that case.
func TestApplyAccountCache_StoresAndFillsFromCache(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()

	// First render: stdin carries both windows -> stored to the cache.
	var snap1 schema.StatusSnapshot
	snap1.Account.FiveHour = &schema.RateWindow{UsedPercentage: 25, ResetsAt: now.Add(2 * time.Hour)}
	snap1.Account.SevenDay = &schema.RateWindow{UsedPercentage: 60, ResetsAt: now.Add(48 * time.Hour)}
	ApplyAccountCache(AccountCacheKey, &snap1, now)

	cached, err := LoadAccount(AccountCacheKey)
	if err != nil || cached == nil {
		t.Fatalf("expected an account cache entry to be stored: err=%v cached=%v", err, cached)
	}
	if cached.FiveHour == nil || cached.FiveHour.UsedPercentage != 25 {
		t.Errorf("cached.FiveHour = %v, want UsedPercentage 25", cached.FiveHour)
	}

	// Second render (a few minutes later): stdin carries neither window ->
	// both are filled in from the cache and Stale is set.
	var snap2 schema.StatusSnapshot
	ApplyAccountCache(AccountCacheKey, &snap2, now.Add(1*time.Minute))
	if snap2.Account.FiveHour == nil || snap2.Account.FiveHour.UsedPercentage != 25 {
		t.Errorf("snap2.Account.FiveHour = %v, want filled from cache (25)", snap2.Account.FiveHour)
	}
	if snap2.Account.SevenDay == nil || snap2.Account.SevenDay.UsedPercentage != 60 {
		t.Errorf("snap2.Account.SevenDay = %v, want filled from cache (60)", snap2.Account.SevenDay)
	}
	if !snap2.Account.Stale {
		t.Error("Account.Stale = false, want true (windows filled from cache)")
	}
}

// TestFillAccountFromCache_DoesNotWrite asserts FillAccountFromCache is
// read-only: fed a snapshot carrying its own, different five-hour window, it
// must fill snap.Account from the cache without ever writing to it - unlike
// ApplyAccountCache (via StoreAccountFromSnapshot), which would store that
// window as newly observed.
func TestFillAccountFromCache_DoesNotWrite(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()

	seeded := AccountUsage{
		Source:     "claude-code-stdin",
		ObservedAt: now.Add(-time.Hour),
		ExpiresAt:  now.Add(4 * time.Minute),
		FiveHour:   &RateWindowState{UsedPercentage: 5, ResetsAt: now.Add(2 * time.Hour)},
	}
	if err := StoreAccount(AccountCacheKey, seeded); err != nil {
		t.Fatalf("StoreAccount() error = %v", err)
	}

	// A snapshot carrying its own, different FiveHour window - FillAccountFromCache
	// must leave it alone (only fills windows the snapshot is missing) and
	// must not write anything to the cache either way.
	snap := schema.StatusSnapshot{}
	snap.Account.FiveHour = &schema.RateWindow{UsedPercentage: 99, ResetsAt: now.Add(3 * time.Hour)}
	FillAccountFromCache(AccountCacheKey, &snap, now)

	if snap.Account.FiveHour.UsedPercentage != 99 {
		t.Errorf("snap.Account.FiveHour.UsedPercentage = %v, want unchanged (99): FillAccountFromCache must not overwrite a window already present", snap.Account.FiveHour.UsedPercentage)
	}

	cached, err := LoadAccount(AccountCacheKey)
	if err != nil || cached == nil {
		t.Fatalf("LoadAccount() error = %v, cached = %v", err, cached)
	}
	if !cached.ObservedAt.Equal(seeded.ObservedAt) {
		t.Errorf("cache ObservedAt changed: got %v, want unchanged %v (FillAccountFromCache must not write)", cached.ObservedAt, seeded.ObservedAt)
	}
	if cached.FiveHour == nil || cached.FiveHour.UsedPercentage != 5 {
		t.Errorf("cache FiveHour = %v, want unchanged (UsedPercentage 5)", cached.FiveHour)
	}
}
