package sessionexport

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/procinfo"
)

// mergeNow is the fixed render clock used by every merge test.
var mergeNow = time.Unix(1793673034, 0)

// mergeIsolate points XDG_STATE_HOME at a fresh temp dir and returns it.
func mergeIsolate(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", base)
	return base
}

// mergeLookup returns a fake process reader: errs wins, then infos, otherwise ErrNotFound.
func mergeLookup(infos map[int]procinfo.Info, errs map[int]error) func(int) (procinfo.Info, error) {
	return func(pid int) (procinfo.Info, error) {
		if e, ok := errs[pid]; ok {
			return procinfo.Info{}, e
		}
		if i, ok := infos[pid]; ok {
			return i, nil
		}
		return procinfo.Info{}, procinfo.ErrNotFound
	}
}

// mergeRow builds a summary row for pid/startedAt.
func mergeRow(pid int, startedAt, seenAt int64) Row {
	return Row{PID: pid, StartedAt: startedAt, SessionID: "s", Model: "m", SeenAt: seenAt}
}

// mergeWriteDetail writes the detail file named after row's own pid/startedAt and sets its mtime.
func mergeWriteDetail(t *testing.T, row Row, mtime time.Time) string {
	t.Helper()
	path, err := DetailPath(row.PID, row.StartedAt)
	if err != nil {
		t.Fatalf("DetailPath: %v", err)
	}
	mergeWriteDetailAt(t, path, row, mtime)
	return path
}

// mergeWriteDetailAt writes row to an explicit path (for name/content mismatch fixtures).
func mergeWriteDetailAt(t *testing.T, path string, row Row, mtime time.Time) {
	t.Helper()
	if err := cache.WriteJSONAtomic(path, Detail{Version: detailVersion, WrittenAt: mergeNow, Row: row, Payload: json.RawMessage("{}")}); err != nil {
		t.Fatalf("write detail: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

// mergeReadPublic returns the raw bytes and parsed content of sessions.json. The file must exist.
func mergeReadPublic(t *testing.T) ([]byte, Public) {
	t.Helper()
	pub, err := PublicPath()
	if err != nil {
		t.Fatalf("PublicPath: %v", err)
	}
	data, err := os.ReadFile(pub)
	if err != nil {
		t.Fatalf("read sessions.json: %v", err)
	}
	var p Public
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("parse sessions.json: %v", err)
	}
	return data, p
}

// mergePublishedRows returns the published rows; a missing sessions.json means none were written.
func mergePublishedRows(t *testing.T) []Row {
	t.Helper()
	pub, err := PublicPath()
	if err != nil {
		t.Fatalf("PublicPath: %v", err)
	}
	if !mergeExists(pub) {
		return nil
	}
	_, p := mergeReadPublic(t)
	return p.Sessions
}

// mergePIDs returns the pids of the rows in order.
func mergePIDs(rows []Row) []int {
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.PID)
	}
	return out
}

func mergeExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestMergePublishesLiveDetail(t *testing.T) {
	mergeIsolate(t)
	row := mergeRow(200, 1000, 1793673000)
	detail := mergeWriteDetail(t, row, mergeNow)

	if err := merge(mergeNow, mergeLookup(map[int]procinfo.Info{200: {PID: 200, StartedAt: 1000}}, nil)); err != nil {
		t.Fatalf("merge: %v", err)
	}
	_, pub := mergeReadPublic(t)
	if pub.Version != PublicVersion || pub.MergedAt != mergeNow.Unix() {
		t.Fatalf("public header = %+v", pub)
	}
	if len(pub.Sessions) != 1 || pub.Sessions[0].PID != 200 {
		t.Fatalf("sessions = %+v, want one row for pid 200", pub.Sessions)
	}
	if !mergeExists(detail) {
		t.Fatalf("live detail file was removed")
	}
}

func TestMergeStartedAtTolerance(t *testing.T) {
	cases := []struct {
		delta     int64
		published bool
	}{
		{1, true}, {-1, true}, {2, false}, {-2, false},
	}
	for _, c := range cases {
		t.Run("delta", func(t *testing.T) {
			mergeIsolate(t)
			detail := mergeWriteDetail(t, mergeRow(200, 1000, 1), mergeNow)
			lookup := mergeLookup(map[int]procinfo.Info{200: {PID: 200, StartedAt: 1000 + c.delta}}, nil)
			if err := merge(mergeNow, lookup); err != nil {
				t.Fatalf("merge: %v", err)
			}
			rows := mergePublishedRows(t)
			published := len(rows) == 1
			if published != c.published {
				t.Fatalf("delta %d: published = %v, want %v (sessions %+v)", c.delta, published, c.published, rows)
			}
			if mergeExists(detail) != c.published {
				t.Fatalf("delta %d: detail exists = %v, want %v", c.delta, mergeExists(detail), c.published)
			}
		})
	}
}

func TestMergeLookupErrors(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		removed bool
	}{
		{"not found deletes", procinfo.ErrNotFound, true},
		{"permission denied keeps", errors.New("permission denied"), false},
		{"unsupported keeps", procinfo.ErrUnsupported, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mergeIsolate(t)
			detail := mergeWriteDetail(t, mergeRow(200, 1000, 1), mergeNow)
			lookup := mergeLookup(nil, map[int]error{200: c.err})
			if err := merge(mergeNow, lookup); err != nil {
				t.Fatalf("merge returned %v, want nil", err)
			}
			if rows := mergePublishedRows(t); len(rows) != 0 {
				t.Fatalf("published %+v, want no rows", rows)
			}
			if mergeExists(detail) == c.removed {
				t.Fatalf("detail exists = %v, removed want %v", mergeExists(detail), c.removed)
			}
		})
	}
}

func TestMergeRemovesMarkerBeforeReading(t *testing.T) {
	mergeIsolate(t)
	pending, err := pendingPath()
	if err != nil {
		t.Fatalf("pendingPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(pending), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	mergeWriteDetail(t, mergeRow(200, 1000, 1), mergeNow)

	lookup := func(pid int) (procinfo.Info, error) {
		if _, err := os.Stat(pending); !os.IsNotExist(err) {
			t.Errorf("marker still present when lookup(%d) ran: %v", pid, err)
		}
		return procinfo.Info{PID: pid, StartedAt: 1000}, nil
	}
	if err := merge(mergeNow, lookup); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if mergeExists(pending) {
		t.Fatalf("marker not removed")
	}
}

func TestMergeRemovesMarkerWhenReadDirFails(t *testing.T) {
	base := mergeIsolate(t)
	stateDir := filepath.Join(base, "statusloom")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A regular file where the detail directory should be makes ReadDir fail.
	detailDir, _ := DetailDir()
	if err := os.WriteFile(detailDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	pub, _ := PublicPath()
	before := []byte(`{"version":1,"mergedAt":5,"sessions":[]}`)
	if err := os.WriteFile(pub, before, 0o600); err != nil {
		t.Fatal(err)
	}
	pending, _ := pendingPath()
	if err := os.WriteFile(pending, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := merge(mergeNow, mergeLookup(nil, nil)); err == nil {
		t.Fatalf("merge returned nil, want ReadDir error")
	}
	got, err := os.ReadFile(pub)
	if err != nil || !bytes.Equal(got, before) {
		t.Fatalf("sessions.json changed: %q, err %v", got, err)
	}
	if mergeExists(pending) {
		t.Fatalf("marker not removed when ReadDir failed")
	}
}

func TestMergeNoRewriteWhenOnlyMergedAtChanges(t *testing.T) {
	mergeIsolate(t)
	mergeWriteDetail(t, mergeRow(200, 1000, 1), mergeNow)
	lookup := mergeLookup(map[int]procinfo.Info{200: {PID: 200, StartedAt: 1000}}, nil)
	if err := merge(mergeNow, lookup); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	pub, _ := PublicPath()
	old := mergeNow.Add(-time.Hour)
	if err := os.Chtimes(pub, old, old); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(pub)

	later := mergeNow.Add(time.Minute)
	if err := merge(later, lookup); err != nil {
		t.Fatalf("second merge: %v", err)
	}
	after, _ := os.ReadFile(pub)
	if !bytes.Equal(before, after) {
		t.Fatalf("sessions.json rewritten although only mergedAt would change")
	}
	st, _ := os.Stat(pub)
	if !st.ModTime().Equal(old) {
		t.Fatalf("sessions.json mtime changed to %v", st.ModTime())
	}
	_, p := mergeReadPublic(t)
	if p.MergedAt != mergeNow.Unix() {
		t.Fatalf("mergedAt = %d, want first merge %d", p.MergedAt, mergeNow.Unix())
	}
}

func TestMergeRewritesOnSeenAtOrSummaryChange(t *testing.T) {
	mergeIsolate(t)
	lookup := mergeLookup(map[int]procinfo.Info{200: {PID: 200, StartedAt: 1000}}, nil)
	mergeWriteDetail(t, mergeRow(200, 1000, 1), mergeNow)
	if err := merge(mergeNow, lookup); err != nil {
		t.Fatalf("merge: %v", err)
	}

	changed := mergeRow(200, 1000, 2) // SeenAt only
	mergeWriteDetail(t, changed, mergeNow)
	if err := merge(mergeNow, lookup); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, p := mergeReadPublic(t); p.Sessions[0].SeenAt != 2 {
		t.Fatalf("SeenAt change not published: %+v", p.Sessions)
	}

	changed.Model = "other" // summary change
	mergeWriteDetail(t, changed, mergeNow)
	if err := merge(mergeNow, lookup); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, p := mergeReadPublic(t); p.Sessions[0].Model != "other" {
		t.Fatalf("Model change not published: %+v", p.Sessions)
	}
}

func TestMergeEmptiedFileWritesEmptyArray(t *testing.T) {
	mergeIsolate(t)
	detail := mergeWriteDetail(t, mergeRow(200, 1000, 1), mergeNow)
	lookup := mergeLookup(map[int]procinfo.Info{200: {PID: 200, StartedAt: 1000}}, nil)
	if err := merge(mergeNow, lookup); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// The process ends: its detail is removed by the merge, sessions.json becomes empty.
	lookupGone := mergeLookup(nil, nil)
	if err := merge(mergeNow, lookupGone); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if mergeExists(detail) {
		t.Fatalf("detail of ended process kept")
	}
	data, _ := mergeReadPublic(t)
	if !strings.Contains(string(data), `"sessions": []`) || strings.Contains(string(data), "null") {
		t.Fatalf("emptied sessions.json = %s, want \"sessions\": []", data)
	}
}

func TestMergeNoFilesNoRowsCreatesNothing(t *testing.T) {
	mergeIsolate(t)
	if err := merge(mergeNow, mergeLookup(nil, nil)); err != nil {
		t.Fatalf("merge: %v", err)
	}
	pub, _ := PublicPath()
	dir, _ := DetailDir()
	if mergeExists(pub) || mergeExists(dir) {
		t.Fatalf("merge created sessions.json or the detail dir with nothing to publish")
	}
}

func TestMergeReplacesInvalidPublicFile(t *testing.T) {
	cases := map[string]string{
		"wrong version": `{"version":2,"mergedAt":5,"sessions":[]}`,
		"unparsable":    `not json`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			mergeIsolate(t)
			pub, _ := PublicPath()
			if err := os.MkdirAll(filepath.Dir(pub), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(pub, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := merge(mergeNow, mergeLookup(nil, nil)); err != nil {
				t.Fatalf("merge: %v", err)
			}
			_, p := mergeReadPublic(t)
			if p.Version != PublicVersion {
				t.Fatalf("version = %d, want %d", p.Version, PublicVersion)
			}
		})
	}
}

func TestMergeSortsByPID(t *testing.T) {
	mergeIsolate(t)
	mergeWriteDetail(t, mergeRow(300, 50, 1), mergeNow)
	mergeWriteDetail(t, mergeRow(100, 70, 1), mergeNow)
	lookup := mergeLookup(map[int]procinfo.Info{300: {PID: 300, StartedAt: 50}, 100: {PID: 100, StartedAt: 70}}, nil)
	if err := merge(mergeNow, lookup); err != nil {
		t.Fatalf("merge: %v", err)
	}
	_, p := mergeReadPublic(t)
	got := mergePIDs(p.Sessions)
	if len(got) != 2 || got[0] != 100 || got[1] != 300 {
		t.Fatalf("pid order = %v, want [100 300]", got)
	}
}

func TestMergeDuplicatePIDKeepsNewest(t *testing.T) {
	mergeIsolate(t)
	older := mergeWriteDetail(t, mergeRow(200, 1000, 1), mergeNow.Add(-5*time.Second))
	newer := mergeWriteDetail(t, mergeRow(200, 1001, 2), mergeNow.Add(-time.Second))
	lookup := mergeLookup(map[int]procinfo.Info{200: {PID: 200, StartedAt: 1000}}, nil)
	if err := merge(mergeNow, lookup); err != nil {
		t.Fatalf("merge: %v", err)
	}
	_, p := mergeReadPublic(t)
	if len(p.Sessions) != 1 || p.Sessions[0].StartedAt != 1001 {
		t.Fatalf("sessions = %+v, want only the newer startedAt 1001 row", p.Sessions)
	}
	if mergeExists(older) || !mergeExists(newer) {
		t.Fatalf("older duplicate not deleted or newer deleted")
	}
}

func TestMergeDuplicatePIDTieKeepsLargerStartedAt(t *testing.T) {
	mergeIsolate(t)
	mergeWriteDetail(t, mergeRow(200, 1000, 1), mergeNow)
	mergeWriteDetail(t, mergeRow(200, 1001, 2), mergeNow)
	lookup := mergeLookup(map[int]procinfo.Info{200: {PID: 200, StartedAt: 1000}}, nil)
	if err := merge(mergeNow, lookup); err != nil {
		t.Fatalf("merge: %v", err)
	}
	_, p := mergeReadPublic(t)
	if len(p.Sessions) != 1 || p.Sessions[0].StartedAt != 1001 {
		t.Fatalf("sessions = %+v, want startedAt 1001 on equal mtime", p.Sessions)
	}
}

func TestMergeKeepsMismatchedDetailUnpublished(t *testing.T) {
	cases := map[string]func(t *testing.T) string{
		"pid mismatch": func(t *testing.T) string {
			path, _ := DetailPath(200, 1000)
			mergeWriteDetailAt(t, path, mergeRow(201, 1000, 1), mergeNow)
			return path
		},
		"startedAt mismatch": func(t *testing.T) string {
			path, _ := DetailPath(200, 1000)
			mergeWriteDetailAt(t, path, mergeRow(200, 999, 1), mergeNow)
			return path
		},
		"wrong version": func(t *testing.T) string {
			path, _ := DetailPath(200, 1000)
			if err := cache.WriteJSONAtomic(path, Detail{Version: 2, Row: mergeRow(200, 1000, 1), Payload: json.RawMessage("{}")}); err != nil {
				t.Fatal(err)
			}
			return path
		},
		"unparsable": func(t *testing.T) string {
			path, _ := DetailPath(200, 1000)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		},
	}
	for name, write := range cases {
		t.Run(name, func(t *testing.T) {
			mergeIsolate(t)
			path := write(t)
			lookup := mergeLookup(map[int]procinfo.Info{200: {PID: 200, StartedAt: 1000}}, nil)
			if err := merge(mergeNow, lookup); err != nil {
				t.Fatalf("merge: %v", err)
			}
			if !mergeExists(path) {
				t.Fatalf("detail was deleted; it must be kept")
			}
			if rows := mergePublishedRows(t); len(rows) != 0 {
				t.Fatalf("mismatched detail published: %+v", rows)
			}
		})
	}
}

func TestMergeDebrisRule(t *testing.T) {
	mergeIsolate(t)
	dir, _ := DetailDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// name -> age. Only ".tmp." names older than 10 minutes go; exactly 10 minutes stays.
	files := map[string]time.Duration{
		"200-1000.json.tmp.9.1": 11 * time.Minute, // debris: removed
		"200-1000.json.tmp.9.2": 10 * time.Minute, // exactly the threshold: kept
		"200-1000.json.tmp.9.3": 9 * time.Minute,  // young: kept
		".DS_Store":             time.Hour,        // unknown names: kept
		"notes.txt":             time.Hour,
		"200-1000.json.bak":     time.Hour,
	}
	for name, age := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := mergeNow.Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}

	lookup := func(pid int) (procinfo.Info, error) {
		t.Errorf("lookup(%d) called; no valid detail name exists in this directory", pid)
		return procinfo.Info{}, procinfo.ErrNotFound
	}
	if err := merge(mergeNow, lookup); err != nil {
		t.Fatalf("merge: %v", err)
	}
	want := map[string]bool{
		"200-1000.json.tmp.9.1": false,
		"200-1000.json.tmp.9.2": true,
		"200-1000.json.tmp.9.3": true,
		".DS_Store":             true,
		"notes.txt":             true,
		"200-1000.json.bak":     true,
	}
	for name, keep := range want {
		if got := mergeExists(filepath.Join(dir, name)); got != keep {
			t.Errorf("%s exists = %v, want %v", name, got, keep)
		}
	}
}
