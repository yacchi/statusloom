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
)

// ExtraUsageState is the persisted snapshot of subscription-overage
// ("extra usage" / usage credits) billing state.
type ExtraUsageState struct {
	Enabled      bool     `json:"enabled"`
	MonthlyLimit *float64 `json:"monthlyLimit,omitempty"`
	UsedCredits  *float64 `json:"usedCredits,omitempty"`
	Utilization  *float64 `json:"utilization,omitempty"`
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
