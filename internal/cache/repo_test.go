package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

func TestRepoRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	now := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	snap := schema.RepositorySnapshot{
		Root:   "/repo/path",
		Branch: "main",
		Dirty:  true,
		Staged: 2,
	}

	if err := StoreRepo("/repo/path", snap, now); err != nil {
		t.Fatalf("StoreRepo() error = %v", err)
	}

	got, fresh, err := LoadRepo("/repo/path", 3*time.Second, now.Add(1*time.Second))
	if err != nil {
		t.Fatalf("LoadRepo() error = %v", err)
	}
	if !fresh {
		t.Fatalf("fresh = false, want true")
	}
	if got == nil || got.Branch != "main" || got.Staged != 2 || !got.Dirty {
		t.Fatalf("LoadRepo() = %+v, want matching snapshot", got)
	}
}

func TestLoadRepo_TTL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	now := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	snap := schema.RepositorySnapshot{Root: "/repo/path", Branch: "main"}
	if err := StoreRepo("/repo/path", snap, now); err != nil {
		t.Fatalf("StoreRepo() error = %v", err)
	}

	// Within TTL: fresh.
	got, fresh, err := LoadRepo("/repo/path", 3*time.Second, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("LoadRepo() error = %v", err)
	}
	if !fresh {
		t.Fatalf("fresh = false within TTL, want true")
	}
	if got == nil || got.Branch != "main" {
		t.Fatalf("LoadRepo() = %+v", got)
	}

	// Past TTL: stale, but the caller still gets the stale snapshot back
	// so it can fall back to it if a fresh git run fails.
	got, fresh, err = LoadRepo("/repo/path", 3*time.Second, now.Add(4*time.Second))
	if err != nil {
		t.Fatalf("LoadRepo() error = %v", err)
	}
	if fresh {
		t.Fatalf("fresh = true past TTL, want false")
	}
	if got == nil || got.Branch != "main" {
		t.Fatalf("LoadRepo() should still return stale snapshot, got %+v", got)
	}
}

func TestLoadRepo_Missing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	got, fresh, err := LoadRepo("/does/not/exist", 3*time.Second, time.Now())
	if err != nil {
		t.Fatalf("LoadRepo() error = %v", err)
	}
	if got != nil || fresh {
		t.Fatalf("LoadRepo() = (%+v, %v), want (nil, false)", got, fresh)
	}
}

func TestLoadRepo_Corrupt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	path, err := repoPath("/repo/path")
	if err != nil {
		t.Fatalf("repoPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, fresh, err := LoadRepo("/repo/path", 3*time.Second, time.Now())
	if err != nil {
		t.Fatalf("LoadRepo() error = %v", err)
	}
	if got != nil || fresh {
		t.Fatalf("LoadRepo() = (%+v, %v), want (nil, false)", got, fresh)
	}
}
