package cache

import (
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// AccountCacheKey is the fallback account cache key used when the caller
// could not resolve an account profile (e.g. .claude.json is missing,
// unreadable, or has no oauthAccount block - see ResolveAccountCacheKey).
// Historically (before organizationUuid-keying) this was also the single,
// permanently shared key every account used; it now only backstops the
// profile-less case.
const AccountCacheKey = "default"

// ResolveAccountCacheKey derives the account cache key to use for profile,
// per statusloom-local-development-plan.md's account-cache-key design: a
// ccprofile-style Team/Max switch on the same machine must not have one
// account's data clobber the other's. profile.OrganizationUUID
// (oauthAccount.organizationUuid) is a stable identifier unique to the
// logged-in account/organization - unlike email or organization display name,
// which can collide (the same person's Team and Max profiles can share an
// email) or be renamed. When profile is nil or its OrganizationUUID is empty
// (account undetermined), the fixed fallback AccountCacheKey is used instead,
// preserving pre-this-change behavior for that case.
func ResolveAccountCacheKey(profile *schema.AccountProfile) string {
	if profile == nil || profile.OrganizationUUID == "" {
		return AccountCacheKey
	}
	return profile.OrganizationUUID
}

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
func ApplyAccountCache(key string, snap *schema.StatusSnapshot, now time.Time) {
	StoreAccountFromSnapshot(key, snap, now)
	FillAccountFromCache(key, snap, now)
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
func StoreAccountFromSnapshot(key string, snap *schema.StatusSnapshot, now time.Time) {
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
	_ = StoreAccount(key, u)
}

// FillAccountFromCache fills in any five-hour/seven-day window snap is
// missing from the shared account cache, if available there, and sets
// Account.Stale when it does (plan section 3.3, step 2). It is read-only: it
// never writes to the shared cache, only to snap itself.
//
// Cache errors are deliberately ignored: the shared cache is a best-effort
// convenience, and a caller must never fail (or even go stale-empty) just
// because the cache directory is unreadable.
func FillAccountFromCache(key string, snap *schema.StatusSnapshot, now time.Time) {
	needFiveHour := snap.Account.FiveHour == nil
	needSevenDay := snap.Account.SevenDay == nil
	if !needFiveHour && !needSevenDay {
		return
	}

	cached, err := LoadAccount(key)
	if err != nil || cached == nil {
		return
	}

	// A cached window whose ResetsAt is not after now has since reset. Right
	// at that boundary, Claude Code's own stdin can lag by a few renders
	// before it reports the new window, so falling back to the raw cached
	// values would show a stale near-limit percentage, and the countdown
	// widget would render a permanent "0m". Neither of those is as wrong as
	// simply blanking the field, though: real post-reset usage restarts at
	// 0%, so rolledOverWindow synthesizes that 0%-used state (with an
	// estimated next reset) for a bounded grace period after the rollover,
	// bridging the gap until stdin catches up. This applies only to cache
	// fills - windows arriving via stdin are Claude's ground truth and are
	// displayed as-is.
	filled := false
	if needFiveHour && cached.FiveHour != nil {
		if w := rolledOverWindow(cached.FiveHour, fiveHourWindowLen, now); w != nil {
			snap.Account.FiveHour = w
			filled = true
		}
	}
	if needSevenDay && cached.SevenDay != nil {
		if w := rolledOverWindow(cached.SevenDay, sevenDayWindowLen, now); w != nil {
			snap.Account.SevenDay = w
			filled = true
		}
	}
	if filled {
		snap.Account.Stale = true
	}
}

// Window lengths used by rolledOverWindow to estimate a rolled-over window's
// next reset. Duplicated from internal/render's fiveHourWindow/sevenDayWindow
// rather than imported, to keep this cache-only package free of a
// render-package dependency.
const (
	fiveHourWindowLen = 5 * time.Hour
	sevenDayWindowLen = 7 * 24 * time.Hour
)

// rolledOverWindow returns the schema.RateWindow to display for a cached
// window that FillAccountFromCache is about to fill in.
//
// If the window is still active (ResetsAt after now), it is returned
// unchanged - stale, but still Claude's own last-reported values.
//
// If it has just rolled over - ResetsAt no more than one windowLen in the
// past - a fresh, 0%-used window is synthesized with an estimated next
// reset (the old ResetsAt plus windowLen). Actual usage restarts at 0% the
// moment a window resets, so this is a much closer approximation of the
// truth than either the stale near-limit percentage or an absent field, for
// however long it takes Claude Code's stdin to report the real new window.
//
// If the cache is older than that (e.g. statusloom hasn't run on this
// machine in a while), the one-windowLen guess is unreliable, so nil is
// returned and the caller leaves the field unset - matching prior behavior
// for genuinely stale caches.
func rolledOverWindow(w *RateWindowState, windowLen time.Duration, now time.Time) *schema.RateWindow {
	if w.ResetsAt.After(now) {
		return ToSchemaRateWindow(w)
	}
	if now.Sub(w.ResetsAt) > windowLen {
		return nil
	}
	return &schema.RateWindow{UsedPercentage: 0, ResetsAt: w.ResetsAt.Add(windowLen)}
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
func ApplyExtraUsageCache(key string, snap *schema.StatusSnapshot, now time.Time) {
	if eu, stale, ok := LoadExtraUsage(key, now); ok && eu != nil {
		snap.Account.ExtraUsage = &schema.ExtraUsage{
			Enabled:         eu.Enabled,
			MonthlyLimitUSD: eu.MonthlyLimit,
			UsedCreditsUSD:  eu.UsedCredits,
			Utilization:     eu.Utilization,
			Stale:           stale,
		}
	}
	if env, _, ok := LoadAccountUsage(key, now); ok && env != nil {
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
