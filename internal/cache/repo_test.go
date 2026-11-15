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

	got := LoadRepo("/repo/path")
	if got == nil || got.Branch != "main" || got.Staged != 2 || !got.Dirty {
		t.Fatalf("LoadRepo() = %+v, want matching snapshot", got)
	}
}

// TestLoadRepo_IgnoresAge pins that the render-side read returns the last
// stored snapshot however old it is: freshness is the refresh worker's
// per-cwd schedule, not the entry's age.
func TestLoadRepo_IgnoresAge(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := StoreRepo("/repo/path", schema.RepositorySnapshot{Root: "/repo/path", Branch: "main"}, old); err != nil {
		t.Fatalf("StoreRepo() error = %v", err)
	}
	if got := LoadRepo("/repo/path"); got == nil || got.Branch != "main" {
		t.Fatalf("LoadRepo() = %+v, want the old snapshot", got)
	}
}

func TestLoadRepo_Missing(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	if got := LoadRepo("/does/not/exist"); got != nil {
		t.Fatalf("LoadRepo() = %+v, want nil", got)
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
	if got := LoadRepo("/repo/path"); got != nil {
		t.Fatalf("LoadRepo() = %+v, want nil", got)
	}
}

func TestDeleteRepo(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	if err := DeleteRepo("/repo/path"); err != nil {
		t.Fatalf("DeleteRepo() on a missing entry error = %v, want nil", err)
	}
	if err := StoreRepo("/repo/path", schema.RepositorySnapshot{Branch: "main"}, time.Now()); err != nil {
		t.Fatalf("StoreRepo() error = %v", err)
	}
	if err := DeleteRepo("/repo/path"); err != nil {
		t.Fatalf("DeleteRepo() error = %v", err)
	}
	if got := LoadRepo("/repo/path"); got != nil {
		t.Fatalf("LoadRepo() after DeleteRepo = %+v, want nil", got)
	}
}
