package sessionexport

import (
	"bytes"
	"encoding/json"
	"os"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
)

// Observe records this render's payload in the process's own detail file and, when the summary
// changed, leaves the merge-pending marker for the background worker. Steps, in order:
//  1. row, err := RowFromPayload(raw, pid, startedAt, configDir); err -> return it, write nothing.
//  2. prev := the Detail at row.DetailPath read with cache.ReadJSON; missing, unreadable,
//     unparsable or Version != detailVersion -> treated as absent.
//  3. prev present and prev.Row.SameSummary(row) -> row.SeenAt = prev.Row.SeenAt, changed = false;
//     otherwise                                   -> row.SeenAt = now.Unix(),     changed = true.
//  4. When !changed, return nil WITHOUT writing if either
//     a. the payload is unchanged: bytes.Equal(json.Compact(prev.Payload),
//     json.Marshal(json.RawMessage(raw))) — json.Marshal HTML-escapes <, >, & inside a
//     RawMessage exactly as the stored copy was escaped, so both sides are comparable; or
//     b. the last write is recent: d := now.Sub(prev.WrittenAt); 0 <= d && d < 2*time.Second
//     (same 2 s floor as cache.StoreSnapshot; a negative d — clock stepped back — writes).
//     A summary change is never throttled.
//  5. cache.WriteJSONAtomicNoSync(row.DetailPath, Detail{Version: detailVersion, WrittenAt: now,
//     Row: row, Payload: raw}) (0600 file, 0700 dirs, same-dir temp + rename, no fsync);
//     error -> return it (no marker).
//  6. Only when changed: create the marker pendingPath() with
//     os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o600) and Close it (empty content, an existing
//     marker is fine), AFTER step 5 succeeded; error -> return it.
func Observe(raw []byte, pid int, startedAt int64, configDir string, now time.Time) error {
	row, err := RowFromPayload(raw, pid, startedAt, configDir)
	if err != nil {
		return err
	}

	var prev Detail
	ok, err := cache.ReadJSON(row.DetailPath, &prev)
	hasPrev := err == nil && ok && prev.Version == detailVersion

	changed := true
	if hasPrev && prev.Row.SameSummary(row) {
		row.SeenAt = prev.Row.SeenAt
		changed = false
	} else {
		row.SeenAt = now.Unix()
	}

	if !changed {
		if observePayloadUnchanged(prev.Payload, raw) {
			return nil
		}
		d := now.Sub(prev.WrittenAt)
		if d >= 0 && d < 2*time.Second {
			return nil
		}
	}

	detail := Detail{
		Version:   detailVersion,
		WrittenAt: now,
		Row:       row,
		Payload:   json.RawMessage(raw),
	}
	if err := cache.WriteJSONAtomicNoSync(row.DetailPath, detail); err != nil {
		return err
	}

	if !changed {
		return nil
	}
	p, err := pendingPath()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

// observePayloadUnchanged reports whether the stored payload equals raw after canonical
// compaction. Any error means "changed".
func observePayloadUnchanged(stored json.RawMessage, raw []byte) bool {
	var buf bytes.Buffer
	if err := json.Compact(&buf, stored); err != nil {
		return false
	}
	cur, err := json.Marshal(json.RawMessage(raw))
	if err != nil {
		return false
	}
	return bytes.Equal(buf.Bytes(), cur)
}

// MergePending reports whether the marker pendingPath() exists: exactly one os.Stat, any error
// (including a BaseDir error) -> false.
func MergePending() bool {
	p, err := pendingPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}
