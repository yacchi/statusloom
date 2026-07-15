package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

func testSnapshot(cwd string) schema.StatusSnapshot {
	return schema.StatusSnapshot{
		Tool: schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.200"},
		Session: schema.SessionSnapshot{
			ID: "sess-1",
		},
		System: schema.SystemSnapshot{Cwd: cwd},
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	observed := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	snap := testSnapshot("/Users/dev/myapp")

	if err := StoreSnapshot("sess-1", snap, observed); err != nil {
		t.Fatalf("StoreSnapshot() error = %v", err)
	}

	got, err := LoadSnapshot("sess-1")
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if got == nil {
		t.Fatalf("LoadSnapshot() = nil, want value")
	}
	if got.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", got.SchemaVersion)
	}
	if !got.ObservedAt.Equal(observed) {
		t.Errorf("ObservedAt = %v, want %v", got.ObservedAt, observed)
	}
	if got.Snapshot.System.Cwd != "/Users/dev/myapp" {
		t.Errorf("Snapshot.System.Cwd = %q, want /Users/dev/myapp", got.Snapshot.System.Cwd)
	}
}

func TestLoadSnapshot_Missing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	got, err := LoadSnapshot("does-not-exist")
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if got != nil {
		t.Fatalf("LoadSnapshot() = %+v, want nil", got)
	}
}

func TestLoadSnapshot_Corrupt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	path, err := snapshotPath("sess-1")
	if err != nil {
		t.Fatalf("snapshotPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := LoadSnapshot("sess-1")
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if got != nil {
		t.Fatalf("LoadSnapshot() = %+v, want nil", got)
	}
}

func TestStoreSnapshot_ThrottlesWithin2s(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	base := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	snap1 := testSnapshot("/Users/dev/myapp")
	if err := StoreSnapshot("sess-1", snap1, base); err != nil {
		t.Fatalf("StoreSnapshot() error = %v", err)
	}

	// Within 2s, even with different content: must be skipped.
	snap2 := testSnapshot("/Users/dev/other")
	if err := StoreSnapshot("sess-1", snap2, base.Add(1*time.Second)); err != nil {
		t.Fatalf("StoreSnapshot() error = %v", err)
	}
	got, err := LoadSnapshot("sess-1")
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if got.Snapshot.System.Cwd != "/Users/dev/myapp" {
		t.Errorf("Snapshot.System.Cwd = %q, want unchanged /Users/dev/myapp (write should have been throttled)", got.Snapshot.System.Cwd)
	}

	// Past 2s: must be rewritten.
	snap3 := testSnapshot("/Users/dev/other")
	if err := StoreSnapshot("sess-1", snap3, base.Add(2001*time.Millisecond)); err != nil {
		t.Fatalf("StoreSnapshot() error = %v", err)
	}
	got, err = LoadSnapshot("sess-1")
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if got.Snapshot.System.Cwd != "/Users/dev/other" {
		t.Errorf("Snapshot.System.Cwd = %q, want /Users/dev/other after throttle window elapsed", got.Snapshot.System.Cwd)
	}
}

func TestListSnapshots_NewestFirst(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	base := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	if err := StoreSnapshot("sess-a", testSnapshot("/a"), base); err != nil {
		t.Fatalf("StoreSnapshot(a) error = %v", err)
	}
	if err := StoreSnapshot("sess-b", testSnapshot("/b"), base.Add(1*time.Hour)); err != nil {
		t.Fatalf("StoreSnapshot(b) error = %v", err)
	}
	if err := StoreSnapshot("sess-c", testSnapshot("/c"), base.Add(2*time.Hour)); err != nil {
		t.Fatalf("StoreSnapshot(c) error = %v", err)
	}

	entries, err := ListSnapshots(base.Add(2 * time.Hour))
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}
	if entries[0].Snapshot.System.Cwd != "/c" || entries[1].Snapshot.System.Cwd != "/b" || entries[2].Snapshot.System.Cwd != "/a" {
		t.Errorf("order = [%q, %q, %q], want [/c, /b, /a]",
			entries[0].Snapshot.System.Cwd, entries[1].Snapshot.System.Cwd, entries[2].Snapshot.System.Cwd)
	}
}

func TestListSnapshots_Missing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	entries, err := ListSnapshots(time.Now())
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if entries != nil {
		t.Fatalf("ListSnapshots() = %+v, want nil", entries)
	}
}

func TestListSnapshots_ExcludesAndPrunesExpired(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	base := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	if err := StoreSnapshot("sess-old", testSnapshot("/old"), base); err != nil {
		t.Fatalf("StoreSnapshot(old) error = %v", err)
	}
	if err := StoreSnapshot("sess-new", testSnapshot("/new"), base.Add(23*time.Hour)); err != nil {
		t.Fatalf("StoreSnapshot(new) error = %v", err)
	}

	now := base.Add(25 * time.Hour) // sess-old is now > 24h stale
	entries, err := ListSnapshots(now)
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Snapshot.System.Cwd != "/new" {
		t.Fatalf("entries = %+v, want only /new", entries)
	}

	oldPath, err := snapshotPath("sess-old")
	if err != nil {
		t.Fatalf("snapshotPath() error = %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("expired snapshot file still exists at %s, want pruned", oldPath)
	}
}

func TestListSnapshots_PrunesBeyondMax(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	base := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	total := maxSnapshots + 3
	for i := 0; i < total; i++ {
		key := "sess-" + string(rune('a'+i))
		observed := base.Add(time.Duration(i) * time.Minute)
		if err := StoreSnapshot(key, testSnapshot("/x"), observed); err != nil {
			t.Fatalf("StoreSnapshot(%s) error = %v", key, err)
		}
	}

	now := base.Add(time.Duration(total) * time.Minute)
	entries, err := ListSnapshots(now)
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if len(entries) != maxSnapshots {
		t.Fatalf("len(entries) = %d, want %d", len(entries), maxSnapshots)
	}

	// The oldest 3 should have been pruned from disk.
	for i := 0; i < 3; i++ {
		key := "sess-" + string(rune('a'+i))
		path, err := snapshotPath(key)
		if err != nil {
			t.Fatalf("snapshotPath(%s) error = %v", key, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("oldest snapshot file %s still exists, want pruned", path)
		}
	}
}

func TestSanitizeSnapshotKey(t *testing.T) {
	cases := map[string]string{
		"sess-1":  "sess-1",
		"a/b\\c":  "a_b_c",
		"Sess_ID": "_ess___",
	}
	for in, want := range cases {
		if got := sanitizeSnapshotKey(in); got != want {
			t.Fatalf("sanitizeSnapshotKey(%q) = %q, want %q", in, got, want)
		}
	}
}
