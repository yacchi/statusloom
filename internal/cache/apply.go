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
// cache, per plan section 3.3 (generalized - see FillAccountFromCache):
//
//  1. Whatever window this render's own stdin carries is stored to the
//     cache so other concurrently running sessions can observe it
//     (StoreAccountFromSnapshot).
//  2. The snapshot's window is then merged against the cache, keeping
//     whichever more currently reflects the account's true state, and
//     Account.Stale is set when the cache's copy wins that merge
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

// FillAccountFromCache merges the shared stdin-side account cache into
// snap's five-hour/seven-day windows and sets Account.Stale when the merge
// changes what snap already carried (plan section 3.3, step 2, generalized -
// see mergeRateWindow). It is read-only: it never writes to the shared
// cache, only to snap itself.
//
// The account's five-hour/seven-day usage is a single fact per account, not
// a fact "as this session's own stdin last heard it": Claude Code's stdin
// payload for this render can lag behind usage a concurrently running
// session just burned, or behind this session's own process not having
// refreshed its internal state yet. So rather than treating whatever snap
// already carries (this render's live stdin value, if any) as unconditionally
// authoritative and only consulting the cache when stdin omitted a window,
// it is merged against the cache - see mergeRateWindow for the merge rule -
// and the more current of the two survives, regardless of which one that is.
//
// Cache errors are deliberately ignored: the shared cache is a best-effort
// convenience, and a caller must never fail (or even go stale-empty) just
// because the cache directory is unreadable.
//
// rolledOver reports whether a window the cache has some record of came out
// of the merge as nil - i.e. every candidate for that window (this render's
// own stdin value, if any, and the cached one) has definitely already
// reset, so nothing safe to display exists yet anywhere. Unlike an ordinary
// "still within its freshness TTL" staleness, this is a known-for-certain
// fact, not a guess: the render path uses it to make an account-usage
// refresh due immediately, instead of guessing at the new window's shape
// (its length, its exact reset time) to paper over the gap - see
// internal/cli's maybeStartRefresh.
func FillAccountFromCache(key string, snap *schema.StatusSnapshot, now time.Time) (rolledOver bool) {
	cached, err := LoadAccount(key)
	if err != nil || cached == nil {
		return false
	}

	liveFiveHour := toRateWindowStateOrNil(snap.Account.FiveHour)
	liveSevenDay := toRateWindowStateOrNil(snap.Account.SevenDay)

	mergedFiveHour := mergeRateWindow(liveFiveHour, cached.FiveHour, now)
	mergedSevenDay := mergeRateWindow(liveSevenDay, cached.SevenDay, now)

	if !rateWindowEqual(mergedFiveHour, liveFiveHour) {
		snap.Account.FiveHour = toSchemaRateWindowOrNil(mergedFiveHour)
		snap.Account.Stale = true
	}
	if !rateWindowEqual(mergedSevenDay, liveSevenDay) {
		snap.Account.SevenDay = toSchemaRateWindowOrNil(mergedSevenDay)
		snap.Account.Stale = true
	}

	if mergedFiveHour == nil && cached.FiveHour != nil {
		rolledOver = true
	}
	if mergedSevenDay == nil && cached.SevenDay != nil {
		rolledOver = true
	}
	return rolledOver
}

// mergeRateWindow folds other - a candidate freshly read from some cache -
// into acc, an accumulator that is either nil, this render's own live stdin
// value (never expiry-filtered: stdin is Claude's own ground truth for
// whatever it reports, full stop, even in the rare case its own ResetsAt
// happens to already be in the past), or the already-validated result of a
// previous mergeRateWindow call. It returns whichever of the two most
// currently reflects the window's true state:
//
//   - other is dropped up front (treated as nil) if its own window has
//     already reset (ResetsAt not after now): unlike acc, a bare cache read
//     is not inherently trustworthy just for having arrived this render, so
//     a cached value that is definitely stale is never displayed rather
//     than guessed at.
//   - If both remain, a later ResetsAt always wins: it reflects a newer
//     window generation (e.g. one source has already picked up the
//     post-reset window while the other still reports the just-expired
//     one).
//   - Between two candidates for the *same* window instance (equal
//     ResetsAt), the higher UsedPercentage wins: usage only accumulates
//     within a window, so whichever observation saw more usage is the more
//     recently confirmed one (e.g. another concurrently running session, or
//     an independent background poll, observed usage this render's own
//     stdin has not caught up to yet).
//
// The account's five-hour/seven-day usage is a single fact per account, not
// a fact "as this session last heard it" - this is what lets ApplyAccountCache
// and ApplyExtraUsageCache each fold in one more source with a single,
// shared rule instead of layering ad hoc fallback-only-when-nil logic.
func mergeRateWindow(acc, other *RateWindowState, now time.Time) *RateWindowState {
	if other != nil && !other.ResetsAt.After(now) {
		other = nil
	}
	switch {
	case acc == nil:
		return other
	case other == nil:
		return acc
	case other.ResetsAt.After(acc.ResetsAt):
		return other
	case acc.ResetsAt.After(other.ResetsAt):
		return acc
	case other.UsedPercentage > acc.UsedPercentage:
		return other
	default:
		return acc
	}
}

// ApplyExtraUsageCache folds the OAuth-usage-API-sourced account cache
// (subscription-overage "extra usage" credits and the per-model seven-day
// windows) into the snapshot. This cache is populated exclusively by the
// background refresh worker (internal/cli's runRefresh -> usage.Fetch); most
// callers only read it, keeping rendering network-free.
//
// Every rate-limit window it touches - the model-scoped (Opus/Sonnet)
// seven-day windows, which have only this one source since Claude Code's
// stdin never carries those at all, and Account.FiveHour/Account.SevenDay,
// which by this point already reflect whatever ApplyAccountCache resolved
// from stdin and its own cache - goes through the same mergeRateWindow rule
// FillAccountFromCache uses, rather than a bespoke "assign unconditionally"
// for the single-source fields and a bespoke "only fall back when nil" for
// the others. One rule for every rate window, regardless of how many
// sources feed it, is easier to reason about and keep correct than one rule
// per field would be: an already-reset value is never displayed or guessed
// at, and when a field does have more than one source, the more current one
// wins even when that is this envelope rather than stdin - it is
// independently, genuinely fetched data (the worker polls the OAuth usage
// API on its own schedule), not a guess, and it is frequently already
// fresher than the stdin-side cache - e.g. right at the moment of a reset,
// or when another concurrently running session burned usage this session's
// own stdin has not caught up to yet.
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
		// SevenDayOpus/SevenDaySonnet have only this one source (Claude
		// Code's stdin never carries per-model windows), but they still go
		// through mergeRateWindow with a nil accumulator rather than being
		// assigned unconditionally: a single-source field is no exception to
		// "an already-reset value is never displayed, guessed at, or left to
		// go stale forever" - the same one rule this whole cache package
		// applies to every rate window, regardless of how many sources feed
		// it.
		snap.Account.SevenDayOpus = toSchemaRateWindowOrNil(mergeRateWindow(nil, env.SevenDayOpus, now))
		snap.Account.SevenDaySonnet = toSchemaRateWindowOrNil(mergeRateWindow(nil, env.SevenDaySonnet, now))

		beforeFiveHour := toRateWindowStateOrNil(snap.Account.FiveHour)
		beforeSevenDay := toRateWindowStateOrNil(snap.Account.SevenDay)
		mergedFiveHour := mergeRateWindow(beforeFiveHour, env.FiveHour, now)
		mergedSevenDay := mergeRateWindow(beforeSevenDay, env.SevenDay, now)

		if !rateWindowEqual(mergedFiveHour, beforeFiveHour) {
			snap.Account.FiveHour = toSchemaRateWindowOrNil(mergedFiveHour)
			snap.Account.Stale = true
		}
		if !rateWindowEqual(mergedSevenDay, beforeSevenDay) {
			snap.Account.SevenDay = toSchemaRateWindowOrNil(mergedSevenDay)
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

// toRateWindowStateOrNil is ToRateWindowState's nil-safe counterpart, for
// merge callers that may be handed a snapshot field that is itself nil
// (e.g. this render's stdin omitted the window).
func toRateWindowStateOrNil(w *schema.RateWindow) *RateWindowState {
	if w == nil {
		return nil
	}
	return ToRateWindowState(w)
}

// toSchemaRateWindowOrNil is ToSchemaRateWindow's nil-safe counterpart, for
// assigning a mergeRateWindow result (which may be nil) straight back onto a
// snapshot field.
func toSchemaRateWindowOrNil(w *RateWindowState) *schema.RateWindow {
	if w == nil {
		return nil
	}
	return ToSchemaRateWindow(w)
}
