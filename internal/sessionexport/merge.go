package sessionexport

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/procinfo"
)

// mergeDebrisAge is how old a ".tmp." file must be before merge deletes it as debris of an
// interrupted atomic write. A file exactly this old is kept.
const mergeDebrisAge = 10 * time.Minute

// mergeCandidate is one detail file that survived the liveness checks.
type mergeCandidate struct {
	row   Row
	path  string
	mtime time.Time
}

// Merge rebuilds <base>/sessions.json from the detail files. It is called only by the
// background refresh worker while it holds the refresh lease.
func Merge(now time.Time) error {
	return merge(now, procinfo.Lookup)
}

// merge rebuilds <base>/sessions.json from the detail files. lookup is the process reader;
// tests inject a fake. See the sessionexport-merge-api contract for the step order.
func merge(now time.Time, lookup func(int) (procinfo.Info, error)) error {
	pp, err := pendingPath()
	if err != nil {
		return err
	}
	// Removed first: a detail written during this merge re-creates the marker.
	_ = os.Remove(pp)

	dir, err := DetailDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	best := make(map[int]mergeCandidate)
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		path := filepath.Join(dir, name)

		pid, startedAt, ok := ParseDetailName(name)
		if ok {
			info, err := lookup(pid)
			if errors.Is(err, procinfo.ErrNotFound) {
				_ = os.Remove(path)
				continue
			}
			if err != nil {
				continue
			}
			if d := info.StartedAt - startedAt; d > 1 || d < -1 {
				_ = os.Remove(path)
				continue
			}
			var det Detail
			found, err := cache.ReadJSON(path, &det)
			if !found || err != nil || det.Version != detailVersion ||
				det.Row.PID != pid || det.Row.StartedAt != startedAt {
				continue
			}
			c := mergeCandidate{row: det.Row, path: path, mtime: fi.ModTime()}
			if prev, dup := best[pid]; dup {
				if mergeWins(c, prev) {
					_ = os.Remove(prev.path)
					best[pid] = c
				} else {
					_ = os.Remove(path)
				}
			} else {
				best[pid] = c
			}
			continue
		}

		if strings.Contains(name, ".tmp.") && now.Sub(fi.ModTime()) > mergeDebrisAge {
			_ = os.Remove(path)
		}
	}

	rows := make([]Row, 0, len(best))
	for _, c := range best {
		rows = append(rows, c.row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].PID < rows[j].PID })

	pub, err := PublicPath()
	if err != nil {
		return err
	}
	var old Public
	found, rerr := cache.ReadJSON(pub, &old)
	valid := found && rerr == nil && old.Version == PublicVersion
	if valid && mergeSameRows(old.Sessions, rows) {
		return nil
	}
	if !found && rerr == nil && len(rows) == 0 {
		return nil
	}
	return cache.WriteJSONAtomic(pub, Public{Version: PublicVersion, MergedAt: now.Unix(), Sessions: rows})
}

// mergeWins reports whether candidate c should replace the current holder prev of the same PID:
// the later mtime wins; on equal mtime the larger StartedAt wins.
func mergeWins(c, prev mergeCandidate) bool {
	if !c.mtime.Equal(prev.mtime) {
		return c.mtime.After(prev.mtime)
	}
	return c.row.StartedAt > prev.row.StartedAt
}

// mergeSameRows reports whether a and b have the same length and, element by element, the same
// summary and SeenAt.
func mergeSameRows(a, b []Row) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].SameSummary(b[i]) || a[i].SeenAt != b[i].SeenAt {
			return false
		}
	}
	return true
}
