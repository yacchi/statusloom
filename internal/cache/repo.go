package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// repoSchemaVersion is the current schema version written by StoreRepo.
const repoSchemaVersion = 1

// repoEntry is the on-disk representation of a cached repository status
// snapshot, stored at repos/<sha256(root)[:16 hex]>.json.
type repoEntry struct {
	SchemaVersion int                       `json:"schemaVersion"`
	ObservedAt    time.Time                 `json:"observedAt"`
	Repo          schema.RepositorySnapshot `json:"repo"`
}

func repoPath(root string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(root))
	name := hex.EncodeToString(sum[:])[:16]
	return filepath.Join(dir, "repos", name+".json"), nil
}

// LoadRepo returns the cached RepositorySnapshot for root and whether it
// is still fresh (ObservedAt within ttl of now). A missing or corrupt
// cache file returns (nil, false, nil).
//
// Caller semantics: when fresh is true, use the returned snapshot without
// running git. When fresh is false, run git; if that run fails or times
// out, fall back to the (possibly nil) stale snapshot returned here
// instead of showing nothing.
func LoadRepo(root string, ttl time.Duration, now time.Time) (*schema.RepositorySnapshot, bool, error) {
	path, err := repoPath(root)
	if err != nil {
		return nil, false, err
	}

	var entry repoEntry
	ok, err := ReadJSON(path, &entry)
	if err != nil || !ok {
		return nil, false, nil
	}

	fresh := now.Sub(entry.ObservedAt) < ttl
	repo := entry.Repo
	return &repo, fresh, nil
}

// StoreRepo persists snap as the cached snapshot for root, observed at
// now.
func StoreRepo(root string, snap schema.RepositorySnapshot, now time.Time) error {
	path, err := repoPath(root)
	if err != nil {
		return err
	}

	entry := repoEntry{
		SchemaVersion: repoSchemaVersion,
		ObservedAt:    now,
		Repo:          snap,
	}
	return WriteJSONAtomic(path, entry)
}
