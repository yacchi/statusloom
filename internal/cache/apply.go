package cache

import (
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// AccountCacheKey is the fixed account cache key used until statusloom can
// distinguish multiple accounts (plan section 11: stdin carries no account
// identifier in v0.1). Shared by the render path (internal/cli) and the
// config UI's real-session preview path (internal/webconfig), both of which
// fold the same account/extra-usage caches into a snapshot.
const AccountCacheKey = "default"

// accountCacheTTL is the informational freshness window written into a
// stdin-sourced account cache entry's ExpiresAt.
const accountCacheTTL = 5 * time.Minute

// ApplyAccountCache folds account-scoped rate-limit state (five-hour and
// seven-day usage) between the current snapshot and the shared account
// cache, per plan section 3.3:
//
//  1. Any window present in the stdin-derived snapshot is stored to the
//     cache so other concurrently running sessions can observe it
//     (StoreAccountFromSnapshot).
//  2. Any window absent from the snapshot is filled in from the cache, if
//     available there, and Account.Stale is set when that happens
//     (FillAccountFromCache).
//
// It is a thin wrapper kept for the render path (internal/cli), which wants
// both halves in one call and in this order. A caller that must not write to
// the shared cache - e.g. the config UI's preview path, which only wants to
// look at a cached session snapshot, not rewrite the shared cache with
// whatever stale windows that snapshot happened to carry - should call
// FillAccountFromCache directly instead.
func ApplyAccountCache(snap *schema.StatusSnapshot, now time.Time) {
	StoreAccountFromSnapshot(snap, now)
	FillAccountFromCache(snap, now)
}

// StoreAccountFromSnapshot writes any five-hour/seven-day window present on
// snap to the shared account cache, so other concurrently running sessions
// can observe it (plan section 3.3, step 1). It has no effect on snap
// itself - it is a pure side effect (a cache write) - and is a no-op when
// snap carries neither window.
//
// Cache errors are deliberately ignored: the shared cache is a best-effort
// convenience, and a render must never fail just because the cache
// directory is unwritable.
func StoreAccountFromSnapshot(snap *schema.StatusSnapshot, now time.Time) {
	if snap.Account.FiveHour == nil && snap.Account.SevenDay == nil {
		return
	}
	u := AccountUsage{
		Source:     "claude-code-stdin",
		ObservedAt: now,
		ExpiresAt:  now.Add(accountCacheTTL),
	}
	if snap.Account.FiveHour != nil {
		u.FiveHour = ToRateWindowState(snap.Account.FiveHour)
	}
	if snap.Account.SevenDay != nil {
		u.SevenDay = ToRateWindowState(snap.Account.SevenDay)
	}
	_ = StoreAccount(AccountCacheKey, u)
}

// FillAccountFromCache fills in any five-hour/seven-day window snap is
// missing from the shared account cache, if available there, and sets
// Account.Stale when it does (plan section 3.3, step 2). It is read-only: it
// never writes to the shared cache, only to snap itself.
//
// Cache errors are deliberately ignored: the shared cache is a best-effort
// convenience, and a caller must never fail (or even go stale-empty) just
// because the cache directory is unreadable.
func FillAccountFromCache(snap *schema.StatusSnapshot, now time.Time) {
	needFiveHour := snap.Account.FiveHour == nil
	needSevenDay := snap.Account.SevenDay == nil
	if !needFiveHour && !needSevenDay {
		return
	}

	cached, err := LoadAccount(AccountCacheKey)
	if err != nil || cached == nil {
		return
	}

	// Skip cached windows whose ResetsAt is not after now: the rate-limit
	// window they describe has since reset, so its UsedPercentage is
	// meaningless and the countdown widget would render a permanent "0m".
	// This applies only to cache fills - windows arriving via stdin are
	// Claude's ground truth and are displayed as-is.
	filled := false
	if needFiveHour && cached.FiveHour != nil && cached.FiveHour.ResetsAt.After(now) {
		snap.Account.FiveHour = ToSchemaRateWindow(cached.FiveHour)
		filled = true
	}
	if needSevenDay && cached.SevenDay != nil && cached.SevenDay.ResetsAt.After(now) {
		snap.Account.SevenDay = ToSchemaRateWindow(cached.SevenDay)
		filled = true
	}
	if filled {
		snap.Account.Stale = true
	}
}

// ApplyExtraUsageCache folds the OAuth-usage-API-sourced account cache
// (subscription-overage "extra usage" credits and the per-model seven-day
// windows) into the snapshot. This cache is populated exclusively by the
// background refresh worker (internal/cli's runRefresh -> usage.Fetch); most
// callers only read it, keeping rendering network-free.
//
// Unlike ApplyAccountCache it never touches Account.FiveHour /
// Account.SevenDay: those stay stdin/stdin-cache driven. It only supplies
// the fields Claude Code's stdin never carries: extra-usage billing state
// and the model-scoped (Opus/Sonnet) seven-day windows.
//
// ExtraUsage and the seven-day windows are deliberately loaded from two
// independent sources with different lifetimes: the windows follow
// LoadAccountUsage's 6h staleness (usageStaleTTL), while ExtraUsage follows
// LoadExtraUsage's own long-lived retention window (extraUsageRetentionTTL)
// - extra-usage credits are monotonic within a billing month and the API
// frequently omits extra_usage from a poll response, so a once-observed
// value must survive well past any single windows-envelope expiry. Each
// source is best-effort and independent: a missing/corrupt/expired value
// from one never blocks the other, and neither ever errors or panics.
func ApplyExtraUsageCache(snap *schema.StatusSnapshot, now time.Time) {
	if eu, stale, ok := LoadExtraUsage(AccountCacheKey, now); ok && eu != nil {
		snap.Account.ExtraUsage = &schema.ExtraUsage{
			Enabled:         eu.Enabled,
			MonthlyLimitUSD: eu.MonthlyLimit,
			UsedCreditsUSD:  eu.UsedCredits,
			Utilization:     eu.Utilization,
			Stale:           stale,
		}
	}
	if env, _, ok := LoadAccountUsage(AccountCacheKey, now); ok && env != nil {
		if env.SevenDayOpus != nil {
			snap.Account.SevenDayOpus = ToSchemaRateWindow(env.SevenDayOpus)
		}
		if env.SevenDaySonnet != nil {
			snap.Account.SevenDaySonnet = ToSchemaRateWindow(env.SevenDaySonnet)
		}
	}
}

// ToRateWindowState converts a schema.RateWindow (the stdin/render-facing
// shape) into the cache's own RateWindowState (the on-disk shape).
func ToRateWindowState(w *schema.RateWindow) *RateWindowState {
	return &RateWindowState{UsedPercentage: w.UsedPercentage, ResetsAt: w.ResetsAt}
}

// ToSchemaRateWindow converts a cached RateWindowState back into a
// schema.RateWindow for use on a StatusSnapshot.
func ToSchemaRateWindow(w *RateWindowState) *schema.RateWindow {
	return &schema.RateWindow{UsedPercentage: w.UsedPercentage, ResetsAt: w.ResetsAt}
}
