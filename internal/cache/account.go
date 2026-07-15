package cache

import (
	"path/filepath"
	"regexp"
	"time"
)

// accountSchemaVersion is the current schema version written by
// StoreAccount.
const accountSchemaVersion = 1

// RateWindowState is the persisted snapshot of a single rolling
// rate-limit window (five-hour or seven-day).
type RateWindowState struct {
	UsedPercentage float64   `json:"usedPercentage"`
	ResetsAt       time.Time `json:"resetsAt"` // RFC3339
}

// AccountUsage is the cached, cross-session account usage record stored
// at account/<key>.json.
type AccountUsage struct {
	SchemaVersion int              `json:"schemaVersion"` // 1
	Source        string           `json:"source"`        // e.g. "claude-code-stdin"
	ObservedAt    time.Time        `json:"observedAt"`
	ExpiresAt     time.Time        `json:"expiresAt"` // ObservedAt + 5min, informational
	FiveHour      *RateWindowState `json:"fiveHour,omitempty"`
	SevenDay      *RateWindowState `json:"sevenDay,omitempty"`
}

// accountKeySanitizer replaces anything outside [a-z0-9-_] with "_" so
// the account key is always safe to use as a filename.
var accountKeySanitizer = regexp.MustCompile(`[^a-z0-9\-_]`)

func sanitizeAccountKey(key string) string {
	return accountKeySanitizer.ReplaceAllString(key, "_")
}

func accountPath(key string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "account", sanitizeAccountKey(key)+".json"), nil
}

// LoadAccount returns the cached usage record for key ("default" in
// v0.1). A missing file returns (nil, nil). A corrupt/unreadable file is
// also treated as absent, best-effort: the render path must never fail
// just because the shared cache is unreadable.
func LoadAccount(key string) (*AccountUsage, error) {
	path, err := accountPath(key)
	if err != nil {
		return nil, err
	}

	var u AccountUsage
	ok, err := ReadJSON(path, &u)
	if err != nil {
		// Corrupt file: treat as absent rather than failing the render.
		return nil, nil
	}
	if !ok {
		return nil, nil
	}
	return &u, nil
}

// StoreAccount persists u under key. To avoid rewriting the cache on
// every render (renders fire roughly every ~300ms), the write is skipped
// when the existing cached FiveHour/SevenDay values are identical to u's
// AND the existing ObservedAt is less than 30s older than u.ObservedAt.
//
// If the existing file is missing, unreadable, or corrupt, the comparison
// is simply skipped and StoreAccount overwrites it.
func StoreAccount(key string, u AccountUsage) error {
	path, err := accountPath(key)
	if err != nil {
		return err
	}

	var existing AccountUsage
	if ok, err := ReadJSON(path, &existing); err == nil && ok {
		sameValues := rateWindowEqual(existing.FiveHour, u.FiveHour) &&
			rateWindowEqual(existing.SevenDay, u.SevenDay)
		if sameValues && u.ObservedAt.Sub(existing.ObservedAt) < 30*time.Second {
			return nil
		}
	}

	if u.SchemaVersion == 0 {
		u.SchemaVersion = accountSchemaVersion
	}

	return WriteJSONAtomic(path, u)
}

func rateWindowEqual(a, b *RateWindowState) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.UsedPercentage == b.UsedPercentage && a.ResetsAt.Equal(b.ResetsAt)
}
