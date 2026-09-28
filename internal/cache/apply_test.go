package cache

import (
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// TestMergeRateWindow covers mergeRateWindow's decision table directly:
// nils on either side, later-ResetsAt-wins across window generations, and
// higher-UsedPercentage-wins within the same generation - and, critically,
// that an already-reset candidate is never dropped: mergeRateWindow must
// never hide a real value a caller holds, only decide which of two real
// values is more current.
func TestMergeRateWindow(t *testing.T) {
	now := time.Now()
	active := func(pct float64, in time.Duration) *RateWindowState {
		return &RateWindowState{UsedPercentage: pct, ResetsAt: now.Add(in)}
	}

	if got := mergeRateWindow(nil, nil); got != nil {
		t.Errorf("mergeRateWindow(nil, nil) = %v, want nil", got)
	}

	if got := mergeRateWindow(nil, active(10, time.Hour)); got == nil || got.UsedPercentage != 10 {
		t.Errorf("mergeRateWindow(nil, active) = %v, want the active candidate", got)
	}

	if got := mergeRateWindow(active(10, time.Hour), nil); got == nil || got.UsedPercentage != 10 {
		t.Errorf("mergeRateWindow(acc, nil) = %v, want the accumulator", got)
	}

	// An already-reset candidate is never dropped - it is still the best
	// available information until something more current shows up.
	expired := &RateWindowState{UsedPercentage: 99, ResetsAt: now.Add(-time.Minute)}
	if got := mergeRateWindow(nil, expired); got != expired {
		t.Errorf("mergeRateWindow(nil, expired-other) = %v, want the expired candidate itself, not nil (a known-stale value is never hidden)", got)
	}
	if got := mergeRateWindow(expired, nil); got != expired {
		t.Errorf("mergeRateWindow(expired-acc, nil) = %v, want the accumulator unchanged", got)
	}

	// Between two expired candidates, the less-overdue (later ResetsAt) one
	// still wins - it is the newer generation, even though both are stale.
	moreOverdue := &RateWindowState{UsedPercentage: 50, ResetsAt: now.Add(-2 * time.Hour)}
	lessOverdue := &RateWindowState{UsedPercentage: 10, ResetsAt: now.Add(-time.Hour)}
	if got := mergeRateWindow(moreOverdue, lessOverdue); got != lessOverdue {
		t.Errorf("mergeRateWindow(more-overdue-acc, less-overdue-other) = %v, want the less-overdue (newer generation) candidate", got)
	}

	newer := active(0, 3*24*time.Hour)
	older := active(90, 1*24*time.Hour)
	if got := mergeRateWindow(older, newer); got != newer {
		t.Errorf("mergeRateWindow(older-acc, newer-other) = %v, want the later ResetsAt to win regardless of usage", got)
	}
	if got := mergeRateWindow(newer, older); got != newer {
		t.Errorf("mergeRateWindow(newer-acc, older-other) = %v, want the later ResetsAt to win", got)
	}

	resetAt := 5 * time.Hour
	low := active(20, resetAt)
	high := active(80, resetAt)
	if got := mergeRateWindow(low, high); got != high {
		t.Errorf("mergeRateWindow(low-acc, high-other) = %v, want the higher UsedPercentage for the same window", got)
	}
	if got := mergeRateWindow(high, low); got != high {
		t.Errorf("mergeRateWindow(high-acc, low-other) = %v, want the accumulator to stay (tie/lower other doesn't win)", got)
	}
}

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

// TestApplyExtraUsageCache_DropsExpiredOpusSonnet asserts SevenDayOpus/
// SevenDaySonnet are held to the same "an already-reset window is never
// displayed" rule as every other rate window (via mergeRateWindow), even
// though they have only this one source - regression test for an
// inconsistency where they were assigned unconditionally instead of going
// through the same mergeRateWindow every other rate window uses. Since
// mergeRateWindow never hides an already-reset value, both still display -
// this test's point is that Opus/Sonnet share the exact same rule as
// FiveHour/SevenDay/each other, not that an expired one gets special-cased
// away.
func TestApplyExtraUsageCache_OpusSonnetShareOneRule(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()
	env := NewAccountUsageEnvelope(now)
	env.SevenDayOpus = &RateWindowState{UsedPercentage: 99, ResetsAt: now.Add(-time.Minute)}
	env.SevenDaySonnet = &RateWindowState{UsedPercentage: 3, ResetsAt: now.Add(4 * time.Hour)}
	if err := StoreAccountUsage(AccountCacheKey, env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	var snap schema.StatusSnapshot
	ApplyExtraUsageCache(AccountCacheKey, &snap, now)

	if snap.Account.SevenDayOpus == nil || snap.Account.SevenDayOpus.UsedPercentage != 99 {
		t.Errorf("SevenDayOpus = %v, want UsedPercentage 99 (an already-reset value is still the best known one, never hidden)", snap.Account.SevenDayOpus)
	}
	if snap.Account.SevenDaySonnet == nil || snap.Account.SevenDaySonnet.UsedPercentage != 3 {
		t.Errorf("SevenDaySonnet = %v, want UsedPercentage 3 (still active)", snap.Account.SevenDaySonnet)
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

	// A snapshot carrying its own FiveHour window that is more current than
	// the cache (a later ResetsAt - a newer window generation), so the merge
	// keeps it - and regardless of the merge outcome, FillAccountFromCache
	// must never write anything to the cache itself.
	snap := schema.StatusSnapshot{}
	snap.Account.FiveHour = &schema.RateWindow{UsedPercentage: 99, ResetsAt: now.Add(3 * time.Hour)}
	FillAccountFromCache(AccountCacheKey, &snap, now)

	if snap.Account.FiveHour.UsedPercentage != 99 {
		t.Errorf("snap.Account.FiveHour.UsedPercentage = %v, want unchanged (99): its ResetsAt is later than the cache's, so it wins the merge", snap.Account.FiveHour.UsedPercentage)
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

// TestFillAccountFromCache_RolledOverReportsSignal asserts that when the
// cached SevenDay window's ResetsAt has passed, FillAccountFromCache leaves
// the field unset (no guessed replacement) but reports rolledOver=true, so
// the caller (internal/cli's renderDocFromRaw) knows for certain a fresh
// account-usage fetch is warranted right now rather than waiting on the
// worker's normal schedule - see maybeStartRefresh's forceUsage.
func TestFillAccountFromCache_RolledOverReportsSignal(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()
	resetAt := now.Add(-10 * time.Minute)

	seeded := AccountUsage{
		Source:     "claude-code-stdin",
		ObservedAt: now.Add(-time.Hour),
		ExpiresAt:  now.Add(4 * time.Minute),
		SevenDay:   &RateWindowState{UsedPercentage: 97, ResetsAt: resetAt},
	}
	if err := StoreAccount(AccountCacheKey, seeded); err != nil {
		t.Fatalf("StoreAccount() error = %v", err)
	}

	snap := schema.StatusSnapshot{}
	rolledOver := FillAccountFromCache(AccountCacheKey, &snap, now)

	if snap.Account.SevenDay == nil || snap.Account.SevenDay.UsedPercentage != 97 {
		t.Errorf("snap.Account.SevenDay = %v, want the cached value (UsedPercentage 97) - the best known one, even though its own window has reset", snap.Account.SevenDay)
	}
	if !rolledOver {
		t.Error("rolledOver = false, want true (the value being displayed is itself already past its own ResetsAt - fresher data is warranted)")
	}
	if !snap.Account.Stale {
		t.Error("snap.Account.Stale = false, want true (filled from cache)")
	}
}

// TestFillAccountFromCache_StillActiveNoSignal asserts the common case: a
// still-active cached window fills in normally and rolledOver stays false.
func TestFillAccountFromCache_StillActiveNoSignal(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()

	seeded := AccountUsage{
		Source:     "claude-code-stdin",
		ObservedAt: now.Add(-time.Hour),
		ExpiresAt:  now.Add(4 * time.Minute),
		SevenDay:   &RateWindowState{UsedPercentage: 40, ResetsAt: now.Add(2 * 24 * time.Hour)},
	}
	if err := StoreAccount(AccountCacheKey, seeded); err != nil {
		t.Fatalf("StoreAccount() error = %v", err)
	}

	snap := schema.StatusSnapshot{}
	rolledOver := FillAccountFromCache(AccountCacheKey, &snap, now)

	if snap.Account.SevenDay == nil || snap.Account.SevenDay.UsedPercentage != 40 {
		t.Errorf("snap.Account.SevenDay = %v, want UsedPercentage 40 from cache", snap.Account.SevenDay)
	}
	if rolledOver {
		t.Error("rolledOver = true, want false (window is still active)")
	}
	if !snap.Account.Stale {
		t.Error("snap.Account.Stale = false, want true (filled from cache)")
	}
}

// TestFillAccountFromCache_CrossSessionUsageWins asserts the scenario the
// merge redesign exists for: this render's own live stdin reports a lower
// UsedPercentage than the shared stdin-cache holds for the *same* window
// (another concurrently running session updated the shared cache with usage
// this session's own stdin has not caught up to yet) - the higher, more
// recently confirmed value wins rather than this session's own stdin being
// trusted unconditionally.
func TestFillAccountFromCache_CrossSessionUsageWins(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()
	resetAt := now.Add(2 * time.Hour)

	seeded := AccountUsage{
		Source:     "claude-code-stdin",
		ObservedAt: now.Add(-time.Minute),
		ExpiresAt:  now.Add(4 * time.Minute),
		FiveHour:   &RateWindowState{UsedPercentage: 58, ResetsAt: resetAt},
	}
	if err := StoreAccount(AccountCacheKey, seeded); err != nil {
		t.Fatalf("StoreAccount() error = %v", err)
	}

	snap := schema.StatusSnapshot{}
	snap.Account.FiveHour = &schema.RateWindow{UsedPercentage: 30, ResetsAt: resetAt}
	rolledOver := FillAccountFromCache(AccountCacheKey, &snap, now)

	if snap.Account.FiveHour.UsedPercentage != 58 {
		t.Errorf("snap.Account.FiveHour.UsedPercentage = %v, want 58 (cross-session cache value, more current than this render's own stdin)", snap.Account.FiveHour.UsedPercentage)
	}
	if rolledOver {
		t.Error("rolledOver = true, want false (a usable window was found)")
	}
	if !snap.Account.Stale {
		t.Error("snap.Account.Stale = false, want true (the cache's value won the merge)")
	}
}

// TestApplyExtraUsageCache_FillsRolledOverSevenDayFromOAuthEnvelope asserts
// that when Account.SevenDay is still nil after the stdin-side cache (e.g.
// FillAccountFromCache just reported rolledOver because that cache had
// reset), ApplyExtraUsageCache fills it from the independently-fetched
// OAuth-usage-API envelope instead, provided that copy is itself still an
// active window - real fetched data, not a guess.
func TestApplyExtraUsageCache_FillsRolledOverSevenDayFromOAuthEnvelope(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()

	env := NewAccountUsageEnvelope(now)
	env.SevenDay = &RateWindowState{UsedPercentage: 0, ResetsAt: now.Add(7 * 24 * time.Hour)}
	if err := StoreAccountUsage(AccountCacheKey, env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	snap := schema.StatusSnapshot{}
	ApplyExtraUsageCache(AccountCacheKey, &snap, now)

	if snap.Account.SevenDay == nil || snap.Account.SevenDay.UsedPercentage != 0 {
		t.Errorf("snap.Account.SevenDay = %v, want UsedPercentage 0 from the OAuth envelope", snap.Account.SevenDay)
	}
	if !snap.Account.Stale {
		t.Error("snap.Account.Stale = false, want true (filled from the OAuth-usage cache)")
	}
}

// TestApplyExtraUsageCache_PrefersNewerWindowGeneration asserts
// ApplyExtraUsageCache keeps the accumulator's SevenDay when the OAuth
// envelope describes an *older* window generation (an earlier ResetsAt) for
// the same field - the two are merged by which one is more current, not by
// which source produced them.
func TestApplyExtraUsageCache_PrefersNewerWindowGeneration(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()

	env := NewAccountUsageEnvelope(now)
	env.SevenDay = &RateWindowState{UsedPercentage: 0, ResetsAt: now.Add(3 * 24 * time.Hour)}
	if err := StoreAccountUsage(AccountCacheKey, env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	snap := schema.StatusSnapshot{}
	snap.Account.SevenDay = &schema.RateWindow{UsedPercentage: 63, ResetsAt: now.Add(7 * 24 * time.Hour)}
	ApplyExtraUsageCache(AccountCacheKey, &snap, now)

	if snap.Account.SevenDay.UsedPercentage != 63 {
		t.Errorf("snap.Account.SevenDay.UsedPercentage = %v, want unchanged (63): the accumulator's window (ResetsAt +7d) is a newer generation than the OAuth envelope's (+3d)", snap.Account.SevenDay.UsedPercentage)
	}
}

// TestApplyExtraUsageCache_PrefersHigherUsageForSameWindow asserts that for
// the *same* window instance (equal ResetsAt), ApplyExtraUsageCache prefers
// whichever source reports the higher UsedPercentage - regression test for
// the scenario the account-cache-key design exists to handle: another
// concurrently running session (or this session's own independent
// OAuth-usage-API poll) can observe usage this render's own stdin has not
// caught up to yet, and the true value is a single per-account fact, not
// "whatever this session's own stdin last said."
func TestApplyExtraUsageCache_PrefersHigherUsageForSameWindow(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()
	resetAt := now.Add(3 * 24 * time.Hour)

	env := NewAccountUsageEnvelope(now)
	env.SevenDay = &RateWindowState{UsedPercentage: 71, ResetsAt: resetAt}
	if err := StoreAccountUsage(AccountCacheKey, env); err != nil {
		t.Fatalf("StoreAccountUsage() error = %v", err)
	}

	snap := schema.StatusSnapshot{}
	snap.Account.SevenDay = &schema.RateWindow{UsedPercentage: 63, ResetsAt: resetAt}
	ApplyExtraUsageCache(AccountCacheKey, &snap, now)

	if snap.Account.SevenDay.UsedPercentage != 71 {
		t.Errorf("snap.Account.SevenDay.UsedPercentage = %v, want 71 (the higher, more-recently-confirmed usage for the same window)", snap.Account.SevenDay.UsedPercentage)
	}
	if !snap.Account.Stale {
		t.Error("snap.Account.Stale = false, want true (the OAuth envelope's value won the merge)")
	}
}

// TestApplyExtraUsageCache_LiveStdinNeverExpiryFiltered asserts that a live
// stdin value (this render's own, on the accumulator already) is never
// dropped merely for its own ResetsAt being in the past - only cache-sourced
// candidates are expiry-filtered. Claude Code's stdin is ground truth for
// whatever it reports, full stop.
func TestApplyExtraUsageCache_LiveStdinNeverExpiryFiltered(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	now := time.Now()

	snap := schema.StatusSnapshot{}
	snap.Account.SevenDay = &schema.RateWindow{UsedPercentage: 99, ResetsAt: now.Add(-time.Minute)}
	ApplyExtraUsageCache(AccountCacheKey, &snap, now)

	if snap.Account.SevenDay == nil || snap.Account.SevenDay.UsedPercentage != 99 {
		t.Errorf("snap.Account.SevenDay = %v, want unchanged (UsedPercentage 99, live stdin value never dropped)", snap.Account.SevenDay)
	}
	if snap.Account.Stale {
		t.Error("snap.Account.Stale = true, want false (nothing from the cache won)")
	}
}
