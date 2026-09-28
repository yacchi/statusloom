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
//
// rolledOver reports whether a cached window was found to have already
// reset (see FillAccountFromCache) - the render path uses this to know for
// certain that a fresh account-usage fetch is warranted right now, rather
// than waiting for the worker's normal polling schedule.
func ApplyAccountCache(key string, snap *schema.StatusSnapshot, now time.Time) (rolledOver bool) {
	StoreAccountFromSnapshot(key, snap, now)
	return FillAccountFromCache(key, snap, now)
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
//
// rolledOver reports whether a needed window was found in the cache but its
// ResetsAt is no longer after now - i.e. it has definitely reset, so its
// UsedPercentage is wrong and it is skipped rather than displayed (this
// applies only to cache fills; windows arriving via stdin are Claude's own
// ground truth and are always displayed as-is, however their ResetsAt
// compares to now). Unlike an ordinary "still within its freshness TTL"
// staleness, this is a known-for-certain fact, not a guess: the render path
// uses it to make an account-usage refresh due immediately, instead of
// guessing at the new window's shape (its length, its exact reset time) to
// paper over the gap - see internal/cli's maybeStartRefresh.
func FillAccountFromCache(key string, snap *schema.StatusSnapshot, now time.Time) (rolledOver bool) {
	needFiveHour := snap.Account.FiveHour == nil
	needSevenDay := snap.Account.SevenDay == nil
	if !needFiveHour && !needSevenDay {
		return false
	}

	cached, err := LoadAccount(key)
	if err != nil || cached == nil {
		return false
	}

	filled := false
	if needFiveHour && cached.FiveHour != nil {
		if cached.FiveHour.ResetsAt.After(now) {
			snap.Account.FiveHour = ToSchemaRateWindow(cached.FiveHour)
			filled = true
		} else {
			rolledOver = true
		}
	}
	if needSevenDay && cached.SevenDay != nil {
		if cached.SevenDay.ResetsAt.After(now) {
			snap.Account.SevenDay = ToSchemaRateWindow(cached.SevenDay)
			filled = true
		} else {
			rolledOver = true
		}
	}
	if filled {
		snap.Account.Stale = true
	}
	return rolledOver
}

// ApplyExtraUsageCache folds the OAuth-usage-API-sourced account cache
// (subscription-overage "extra usage" credits and the per-model seven-day
// windows) into the snapshot. This cache is populated exclusively by the
// background refresh worker (internal/cli's runRefresh -> usage.Fetch); most
// callers only read it, keeping rendering network-free.
//
// It only supplies the model-scoped (Opus/Sonnet) seven-day windows
// unconditionally, since Claude Code's stdin never carries those at all.
// Account.FiveHour/Account.SevenDay stay stdin/stdin-cache driven first
// (ApplyAccountCache is Claude's own ground truth and runs before this), but
// if a window is still missing after that - most often right after it has
// just reset and the stdin-side cache was deliberately not used for it (see
// FillAccountFromCache's rolledOver) - this fills it in from the same
// OAuth-usage-API envelope, provided that copy is itself a genuinely still-
// active window (ResetsAt after now). That is real, independently-fetched
// data (the worker polls this API on its own schedule), not a guess, and it
// is frequently already fresher than the stdin-side cache at the exact
// moment of a reset, since the two sources refresh independently.
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
		if snap.Account.FiveHour == nil && env.FiveHour != nil && env.FiveHour.ResetsAt.After(now) {
			snap.Account.FiveHour = ToSchemaRateWindow(env.FiveHour)
			snap.Account.Stale = true
		}
		if snap.Account.SevenDay == nil && env.SevenDay != nil && env.SevenDay.ResetsAt.After(now) {
			snap.Account.SevenDay = ToSchemaRateWindow(env.SevenDay)
			snap.Account.Stale = true
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
