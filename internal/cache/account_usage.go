package cache

import (
	"path/filepath"
	"time"
)

// usageAccountSchemaVersion is the current schema version written by
// StoreAccountUsage.
const usageAccountSchemaVersion = 1

const (
	// usageFreshTTL is how long a fetched usage record is considered
	// fresh before LoadAccountUsage starts reporting it as stale.
	usageFreshTTL = 15 * time.Minute
	// usageStaleTTL is how long a stale usage record remains usable at
	// all before LoadAccountUsage treats it as absent.
	usageStaleTTL = 6 * time.Hour
	// UsageRefreshInterval is the target polling interval for the
	// account-usage worker under normal (no-failure) conditions.
	UsageRefreshInterval = 5 * time.Minute
	// usageBackoffMax caps the exponential backoff applied after
	// repeated fetch failures.
	usageBackoffMax = 60 * time.Minute
	// extraUsageRetentionTTL is how long a last-observed ExtraUsage value is
	// carried forward independent of the rate-window envelope's own
	// staleness (usageStaleTTL/6h): extra-usage credits are monotonic within
	// a billing month (never decrease), so a value that is merely old (the
	// API often omits extra_usage from a poll response) is still the best
	// information available and should not be dropped just because the
	// windows envelope expired. The retention cap only exists to stop an
	// indefinitely-stale value from surviving across a month boundary.
	extraUsageRetentionTTL = 30 * 24 * time.Hour
)

// ExtraUsageState is the persisted snapshot of subscription-overage
// ("extra usage" / usage credits) billing state.
type ExtraUsageState struct {
	Enabled      bool     `json:"enabled"`
	MonthlyLimit *float64 `json:"monthlyLimit,omitempty"`
	UsedCredits  *float64 `json:"usedCredits,omitempty"`
	Utilization  *float64 `json:"utilization,omitempty"`
	// ObservedAt is when this ExtraUsage value was actually fetched from the
	// OAuth usage API. It is independent of the enclosing envelope's
	// ObservedAt/ExpiresAt/StaleUntil: a poll that omits extra_usage carries
	// the previous ExtraUsageState (and its ObservedAt) forward unchanged,
	// so this field tracks the value's own age, not the envelope's.
	ObservedAt time.Time `json:"observedAt,omitempty"`
}

// AccountUsageEnvelope is the cached, cross-session account usage record
// stored at account/<key>-usage.json. It is owned by the account-usage
// worker (fed by the authenticated OAuth usage API) and is separate from
// account.go's AccountUsage record (fed by stdin), so the render path's
// stdin-driven StoreAccount never clobbers it.
type AccountUsageEnvelope struct {
	SchemaVersion  int              `json:"schemaVersion"`
	Source         string           `json:"source"` // "oauth-usage-api"
	ObservedAt     time.Time        `json:"observedAt"`
	ExpiresAt      time.Time        `json:"expiresAt"`  // ObservedAt + usageFreshTTL (Stale marker)
	StaleUntil     time.Time        `json:"staleUntil"` // ObservedAt + usageStaleTTL (drop after)
	FiveHour       *RateWindowState `json:"fiveHour,omitempty"`
	SevenDay       *RateWindowState `json:"sevenDay,omitempty"`
	SevenDayOpus   *RateWindowState `json:"sevenDayOpus,omitempty"`
	SevenDaySonnet *RateWindowState `json:"sevenDaySonnet,omitempty"`
	ExtraUsage     *ExtraUsageState `json:"extraUsage,omitempty"`
}

// NewAccountUsageEnvelope returns an envelope pre-filled for a fresh fetch
// observed at `now`: SchemaVersion set, Source = "oauth-usage-api",
// ObservedAt = now, ExpiresAt = now + usageFreshTTL, StaleUntil = now + usageStaleTTL.
// The caller fills in the window/extra-usage data fields, then passes it to
// StoreAccountUsage.
func NewAccountUsageEnvelope(now time.Time) AccountUsageEnvelope {
	return AccountUsageEnvelope{
		SchemaVersion: usageAccountSchemaVersion,
		Source:        "oauth-usage-api",
		ObservedAt:    now,
		ExpiresAt:     now.Add(usageFreshTTL),
		StaleUntil:    now.Add(usageStaleTTL),
	}
}

func accountUsagePath(key string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "account", sanitizeAccountKey(key)+"-usage.json"), nil
}

// LoadAccountUsage returns the cached account-usage record for key. A
// missing file, or one that is corrupt/unreadable, is treated as absent
// (ok=false), best-effort: callers must never fail just because the
// shared cache is unreadable. A record past its StaleUntil is also
// treated as absent. stale reports whether the record is past its fresh
// TTL (ExpiresAt) but still within StaleUntil.
func LoadAccountUsage(key string, now time.Time) (env *AccountUsageEnvelope, stale bool, ok bool) {
	path, err := accountUsagePath(key)
	if err != nil {
		return nil, false, false
	}

	var e AccountUsageEnvelope
	if readOK, err := ReadJSON(path, &e); err != nil || !readOK {
		return nil, false, false
	}
	if now.After(e.StaleUntil) {
		return nil, false, false
	}
	return &e, now.After(e.ExpiresAt), true
}

// LoadAccountUsageRaw returns the cached account-usage envelope for key
// without applying any TTL/staleness check - it is a plain best-effort disk
// read. A missing file, or one that is corrupt/unreadable, is treated as
// absent (ok=false). Callers that need TTL-aware semantics for the
// rate-window fields should use LoadAccountUsage instead; this exists for
// callers (LoadExtraUsage, the refresh worker's carry-forward) that need the
// envelope regardless of whether the windows portion has expired.
func LoadAccountUsageRaw(key string) (env *AccountUsageEnvelope, ok bool) {
	path, err := accountUsagePath(key)
	if err != nil {
		return nil, false
	}

	var e AccountUsageEnvelope
	if readOK, err := ReadJSON(path, &e); err != nil || !readOK {
		return nil, false
	}
	return &e, true
}

// LoadExtraUsage returns the cached ExtraUsage state for key, independent of
// the rate-window envelope's own 6h staleness (usageStaleTTL): extra-usage
// credits are monotonic within a billing month, so a value survives on its
// own long-lived retention window (extraUsageRetentionTTL) rather than
// expiring alongside the windows it happens to be stored next to.
//
// ok is false when there is no cached value at all, or when the cached
// value's own age exceeds extraUsageRetentionTTL (dropped so a stale
// pre-month value never displays forever). stale reports whether the value
// is older than usageFreshTTL but still within retention.
func LoadExtraUsage(key string, now time.Time) (eu *ExtraUsageState, stale bool, ok bool) {
	env, ok := LoadAccountUsageRaw(key)
	if !ok || env == nil || env.ExtraUsage == nil {
		return nil, false, false
	}

	obs := env.ExtraUsage.ObservedAt
	if obs.IsZero() {
		// Legacy record written before ObservedAt existed on ExtraUsageState:
		// fall back to the enclosing envelope's observation time.
		obs = env.ObservedAt
	}

	if !obs.IsZero() && now.Sub(obs) > extraUsageRetentionTTL {
		return nil, false, false
	}

	stale = !obs.IsZero() && now.After(obs.Add(usageFreshTTL))
	return env.ExtraUsage, stale, true
}

// StoreAccountUsage persists env under key. Unlike StoreAccount, there is
// no dedup/skip logic: the worker controls its own polling cadence via
// AccountUsageSchedule, so every call is expected to represent a genuine
// new observation.
func StoreAccountUsage(key string, env AccountUsageEnvelope) error {
	path, err := accountUsagePath(key)
	if err != nil {
		return err
	}

	if env.SchemaVersion == 0 {
		env.SchemaVersion = usageAccountSchemaVersion
	}

	return WriteJSONAtomic(path, env)
}
