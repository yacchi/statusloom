package cache

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// snapshotSchemaVersion is the current schema version written by
// StoreSnapshot.
const snapshotSchemaVersion = 1

// snapshotTTL bounds how long a stored session snapshot is considered
// usable by ListSnapshots. Entries older than this are excluded from the
// result and opportunistically removed from disk.
const snapshotTTL = 24 * time.Hour

// maxSnapshots bounds how many session snapshots ListSnapshots returns
// (and keeps on disk). Older entries beyond this limit are opportunistically
// removed from disk.
const maxSnapshots = 20

// SnapshotEntry is the on-disk representation of a single session's most
// recently observed status snapshot, stored at
// snapshots/<sanitized(session id)>.json. It backs the config UI's
// "real data preview" feature: rather than only previewing against
// synthetic samples, the UI can render against an actual recent session.
type SnapshotEntry struct {
	SchemaVersion int                   `json:"schemaVersion"` // 1
	ObservedAt    time.Time             `json:"observedAt"`
	Snapshot      schema.StatusSnapshot `json:"snapshot"`
}

// snapshotKeySanitizer replaces anything outside [a-z0-9-_] with "_" so a
// session id is always safe to use as a filename. Shared pattern with
// account.go's accountKeySanitizer, kept as a separate copy since the two
// key spaces (account key vs. session id) are conceptually unrelated even
// though the sanitization rule happens to be identical.
var snapshotKeySanitizer = accountKeySanitizer

func sanitizeSnapshotKey(key string) string {
	return snapshotKeySanitizer.ReplaceAllString(key, "_")
}

func snapshotPath(key string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "snapshots", sanitizeSnapshotKey(key)+".json"), nil
}

func snapshotDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "snapshots"), nil
}

// StoreSnapshot persists snap as the most recently observed snapshot for
// session key, observed at now.
//
// To avoid rewriting the cache on every render (renders fire roughly
// every ~300ms while a session is active), the write is skipped when an
// existing entry for key was already observed within 2 seconds of now.
// This is a plain recency throttle (unlike StoreAccount's value-equality
// check): unconditionally skipping any write within the window is
// sufficient here because the whole point of this cache is "the most
// recent snapshot", not precise change tracking.
//
// If the existing file is missing, unreadable, or corrupt, the throttle
// check is simply skipped and StoreSnapshot overwrites it.
func StoreSnapshot(key string, snap schema.StatusSnapshot, now time.Time) error {
	path, err := snapshotPath(key)
	if err != nil {
		return err
	}

	var existing SnapshotEntry
	if ok, err := ReadJSON(path, &existing); err == nil && ok {
		if now.Sub(existing.ObservedAt) < 2*time.Second {
			return nil
		}
	}

	entry := SnapshotEntry{
		SchemaVersion: snapshotSchemaVersion,
		ObservedAt:    now,
		Snapshot:      snap,
	}
	return WriteJSONAtomic(path, entry)
}

// LoadSnapshot returns the cached snapshot entry for session key. A
// missing file returns (nil, nil). A corrupt/unreadable file is also
// treated as absent: like the rest of this package, the cache is a
// best-effort convenience and must never fail a caller just because the
// on-disk entry is unreadable.
func LoadSnapshot(key string) (*SnapshotEntry, error) {
	path, err := snapshotPath(key)
	if err != nil {
		return nil, err
	}

	var entry SnapshotEntry
	ok, err := ReadJSON(path, &entry)
	if err != nil {
		// Corrupt file: treat as absent.
		return nil, nil
	}
	if !ok {
		return nil, nil
	}
	return &entry, nil
}

// ListSnapshots returns every non-expired stored session snapshot, newest
// (by ObservedAt) first.
//
// As a side effect, it opportunistically prunes the on-disk snapshot
// store on a best-effort basis: entries older than snapshotTTL are
// removed, as are entries beyond maxSnapshots once the remaining entries
// are sorted newest-first. Pruning errors are ignored - a failed removal
// just leaves an extra file on disk, which is corrected on the next call.
// Corrupt/unreadable entries are skipped entirely (neither returned nor
// pruned, since a corrupt file cannot be safely identified as this
// cache's own stale data vs. some other unrelated problem).
func ListSnapshots(now time.Time) ([]SnapshotEntry, error) {
	dir, err := snapshotDir()
	if err != nil {
		return nil, err
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	type namedEntry struct {
		path  string
		entry SnapshotEntry
	}

	var fresh []namedEntry
	var expired []string
	for _, f := range files {
		if f.IsDir() || filepath.Ext(f.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, f.Name())

		var entry SnapshotEntry
		ok, err := ReadJSON(path, &entry)
		if err != nil || !ok {
			// Corrupt or unreadable: skip, don't touch it.
			continue
		}

		if now.Sub(entry.ObservedAt) > snapshotTTL {
			expired = append(expired, path)
			continue
		}
		fresh = append(fresh, namedEntry{path: path, entry: entry})
	}

	sort.Slice(fresh, func(i, j int) bool {
		return fresh[i].entry.ObservedAt.After(fresh[j].entry.ObservedAt)
	})

	// Best-effort prune of expired entries.
	for _, path := range expired {
		_ = os.Remove(path)
	}

	result := make([]SnapshotEntry, 0, len(fresh))
	for i, ne := range fresh {
		if i >= maxSnapshots {
			// Beyond the cap: prune the file too, best-effort.
			_ = os.Remove(ne.path)
			continue
		}
		result = append(result, ne.entry)
	}

	return result, nil
}
