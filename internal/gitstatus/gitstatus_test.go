package gitstatus

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
}

// runGitCmd is a small test helper for driving `git` while setting up
// fixture repositories. It intentionally does not go through the
// package's own runGit, so it exercises the package under test from
// the outside.
func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestCollect_NotARepo(t *testing.T) {
	requireGit(t)

	dir := t.TempDir()

	snap, err := Collect(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Collect returned error for non-repo dir: %v", err)
	}
	if snap != nil {
		t.Fatalf("Collect returned non-nil snapshot for non-repo dir: %+v", snap)
	}
}

func TestCollect_Integration(t *testing.T) {
	requireGit(t)

	dir := t.TempDir()
	runGitCmd(t, dir, "init", "-q", "-b", "main")
	runGitCmd(t, dir, "config", "user.name", "Test User")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")

	// Commit both files up front so later modifications can be staged /
	// left unstaged independently without one commit swallowing the
	// other's staged change.
	committedPath := filepath.Join(dir, "committed.txt")
	if err := os.WriteFile(committedPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	unstagedPath := filepath.Join(dir, "unstaged.txt")
	if err := os.WriteFile(unstagedPath, []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, "add", "committed.txt", "unstaged.txt")
	runGitCmd(t, dir, "commit", "-q", "-m", "initial commit")

	// Staged change: modify + git add.
	if err := os.WriteFile(committedPath, []byte("hello\nstaged change\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, "add", "committed.txt")

	// Unstaged change: modify the other committed file without staging.
	if err := os.WriteFile(unstagedPath, []byte("v1\nv2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Untracked file.
	untrackedPath := filepath.Join(dir, "untracked.txt")
	if err := os.WriteFile(untrackedPath, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	snap, err := Collect(context.Background(), dir, Options{
		IncludeUntracked: true,
		CollectNumstat:   true,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if snap == nil {
		t.Fatal("Collect returned nil snapshot for a real repo")
	}

	if snap.Branch != "main" {
		t.Errorf("Branch = %q, want %q", snap.Branch, "main")
	}
	if !snap.Dirty {
		t.Errorf("Dirty = false, want true")
	}
	if snap.Staged != 1 {
		t.Errorf("Staged = %d, want 1", snap.Staged)
	}
	if snap.Unstaged != 1 {
		t.Errorf("Unstaged = %d, want 1", snap.Unstaged)
	}
	if snap.Untracked != 1 {
		t.Errorf("Untracked = %d, want 1", snap.Untracked)
	}

	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(snap.Root)
	if err != nil {
		t.Fatalf("EvalSymlinks(root): %v", err)
	}
	if resolvedRoot != resolvedDir {
		t.Errorf("Root = %q, want %q", resolvedRoot, resolvedDir)
	}

	if snap.Added == 0 && snap.Deleted == 0 {
		t.Errorf("expected numstat to report some added/deleted lines, got Added=%d Deleted=%d", snap.Added, snap.Deleted)
	}
}

func TestCollect_WithoutUntracked(t *testing.T) {
	requireGit(t)

	dir := t.TempDir()
	runGitCmd(t, dir, "init", "-q", "-b", "main")
	runGitCmd(t, dir, "config", "user.name", "Test User")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitCmd(t, dir, "add", "file.txt")
	runGitCmd(t, dir, "commit", "-q", "-m", "initial")

	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	snap, err := Collect(context.Background(), dir, Options{IncludeUntracked: false})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if snap == nil {
		t.Fatal("Collect returned nil snapshot")
	}
	if snap.Untracked != 0 {
		t.Errorf("Untracked = %d, want 0 when IncludeUntracked is false", snap.Untracked)
	}
	if snap.Dirty {
		t.Errorf("Dirty = true, want false when the only change is an excluded untracked file")
	}
}

func TestCollect_Timeout(t *testing.T) {
	requireGit(t)

	dir := t.TempDir()
	runGitCmd(t, dir, "init", "-q")

	start := time.Now()
	_, err := Collect(context.Background(), dir, Options{Timeout: 1 * time.Nanosecond})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Collect with 1ns timeout returned nil error, want a timeout error")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Collect with 1ns timeout took %v, want it to fail quickly", elapsed)
	}
}
