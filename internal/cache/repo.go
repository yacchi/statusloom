package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/yacchi/statusloom/internal/atomicfile"
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

// LoadRepo returns the last RepositorySnapshot stored for root, whatever its
// age, or nil when there is none (missing or corrupt file). The render path
// uses it as-is: freshness is driven by the refresh worker's per-cwd git
// schedule (RefreshSchedule.Git), not by the entry's age, so a stale value is
// shown rather than nothing until the next collection lands.
func LoadRepo(root string) *schema.RepositorySnapshot {
	path, err := repoPath(root)
	if err != nil {
		return nil
	}
	var entry repoEntry
	if ok, err := ReadJSON(path, &entry); err != nil || !ok {
		return nil
	}
	repo := entry.Repo
	return &repo
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

// DeleteRepo removes the stored snapshot for root (a no-op when there is none).
// The refresh worker calls it when a collection finds that root is no longer
// inside a git working tree, so a stale snapshot is not shown forever.
func DeleteRepo(root string) error {
	path, err := repoPath(root)
	if err != nil {
		return err
	}
	if err := atomicfile.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
