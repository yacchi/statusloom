package cache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/atomicfile"
	"github.com/yacchi/statusloom/internal/schema"
)

// modRecordSchemaVersion is the schemaVersion written into every ModRecord.
// A file carrying any other value is treated as absent by LoadModRecord.
const modRecordSchemaVersion = 1

// modRecordRetention is how long a session's mod record is kept after its last
// write; PruneModRecords removes older *.json files under mod/sessions.
const modRecordRetention = 7 * 24 * time.Hour

// modPruneInterval is the minimum time between two PruneModRecords runs.
const modPruneInterval = time.Hour

// ModRecord is the on-disk record of one Claude Code session as folded from the
// statusloom mod's events. It is written only by `statusloom mod-ingest`; the
// render path only reads it.
type ModRecord struct {
	SchemaVersion int                     `json:"schemaVersion"` // 1
	SessionID     string                  `json:"sessionId"`     // as received from the mod
	Cursor        ModCursor               `json:"cursor"`        // last folded event
	Events        map[string]ModEventStat `json:"events"`        // per event name: {count, lastAt}
	Snapshot      schema.ModSnapshot      `json:"snapshot"`      // what the render path shows
}

// ModCursor identifies the last event folded into a ModRecord: the mod's
// per-load boot id and the last sequence number seen under that boot.
type ModCursor struct {
	Boot string `json:"boot"` // the mod's per-load boot id
	Seq  int64  `json:"seq"`  // last folded seq of that boot
}

// ModEventStat counts the events of one name folded into a ModRecord.
type ModEventStat struct {
	Count  int   `json:"count"`
	LastAt int64 `json:"lastAt"` // epoch ms
}

// modSessionsDir returns the directory holding one <session>.json mod record
// per session. It is a directory of its own, which no other cache writer uses.
func modSessionsDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mod", "sessions"), nil
}

// modRecordPath returns the file path of the mod record for sessionID. The id
// is sanitized the same way as snapshot keys, so it is always a plain file name.
func modRecordPath(sessionID string) (string, error) {
	dir, err := modSessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sanitizeSnapshotKey(sessionID)+".json"), nil
}

// LoadModRecord returns the mod record for sessionID. An empty sessionID, a
// missing file, an unreadable or corrupt file, and a file whose schemaVersion
// is not 1 all yield (nil, nil): a broken record must never fail a render.
// The only error returned is one from resolving the cache directory.
func LoadModRecord(sessionID string) (*ModRecord, error) {
	if sessionID == "" {
		return nil, nil
	}
	path, err := modRecordPath(sessionID)
	if err != nil {
		return nil, err
	}
	var rec ModRecord
	ok, err := ReadJSON(path, &rec)
	if err != nil || !ok || rec.SchemaVersion != modRecordSchemaVersion {
		return nil, nil
	}
	return &rec, nil
}

// StoreModRecord sets rec.SchemaVersion to 1 and writes rec atomically to its
// session's file. A nil rec or an empty SessionID is an error and creates
// nothing on disk.
func StoreModRecord(rec *ModRecord) error {
	if rec == nil || rec.SessionID == "" {
		return errors.New("cache: mod record requires a session id")
	}
	path, err := modRecordPath(rec.SessionID)
	if err != nil {
		return err
	}
	rec.SchemaVersion = modRecordSchemaVersion
	return WriteJSONAtomic(path, rec)
}

// PruneModRecords removes mod/sessions/*.json files whose mtime is older than
// the retention period (7 days). It runs at most once per hour, tracked by the
// mtime of the mod/.pruned marker, which is touched to now on every run. It is
// best effort: every error is ignored, and only *.json entries are removed.
func PruneModRecords(now time.Time) {
	base, err := Dir()
	if err != nil {
		return
	}
	modDir := filepath.Join(base, "mod")
	marker := filepath.Join(modDir, ".pruned")

	if info, err := os.Stat(marker); err == nil && now.Sub(info.ModTime()) < modPruneInterval {
		return
	}
	if err := os.MkdirAll(modDir, 0o700); err != nil {
		return
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		return
	}
	_ = os.Chtimes(marker, now, now)

	sessionsDir := filepath.Join(modDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return
	}
	cutoff := now.Add(-modRecordRetention)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = atomicfile.Remove(filepath.Join(sessionsDir, entry.Name()))
	}
}
