package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/gitstatus"
	"github.com/yacchi/statusloom/internal/schema"
)

// Git status runs only in the refresh worker (runRefresh --once --git-cwd);
// the render path reads the repo cache and asks for a collection when the
// cwd's schedule entry is due.

const gitDoc = `<field name="git-branch"/>`

// gitPayload is payload(t) with its cwd (and workspace.current_dir) set to dir.
func gitPayload(t *testing.T, dir string) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(payload(t), &m); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	m["cwd"] = dir
	if ws, ok := m["workspace"].(map[string]any); ok {
		ws["current_dir"] = dir
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	return data
}

func renderGit(t *testing.T, dir string) string {
	t.Helper()
	stdout, stderr, code := runCLI(t, []string{"claude"}, gitPayload(t, dir), nil)
	if code != 0 {
		t.Fatalf("claude exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	return stdout
}

// stubCollectGit replaces the worker's git collector for the test's duration
// and counts its calls.
func stubCollectGit(t *testing.T, f func(ctx context.Context, dir string, opts gitstatus.Options) (*schema.RepositorySnapshot, error)) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	prev := collectGit
	collectGit = func(ctx context.Context, dir string, opts gitstatus.Options) (*schema.RepositorySnapshot, error) {
		calls.Add(1)
		return f(ctx, dir, opts)
	}
	t.Cleanup(func() { collectGit = prev })
	return &calls
}

// gitSpawn returns the first captured spawn that carries --git-cwd, or nil.
func gitSpawn(spawns [][]string) []string {
	for _, s := range spawns {
		if slices.Contains(s, "--git-cwd") {
			return s
		}
	}
	return nil
}

func setupGitEnv(t *testing.T, doc string) {
	t.Helper()
	setupEnv(t)
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	writeDocument(t, "claude-code", scheduleDoc(doc))
}

// TestRenderNeverRunsGit: the render shows the cached snapshot (however old)
// without running git - the collector is a test failure, and PATH holds no git
// executable - and hands the cwd plus the <git/> settings to the worker.
func TestRenderNeverRunsGit(t *testing.T) {
	setupGitEnv(t, gitDoc)
	writeDocument(t, "claude-code", `<statusloom version="1" tool="claude-code" color-level="none">
  <git cache-ttl-ms="1500" timeout-ms="900" include-untracked="false"/>
  <layout name="default" active="true"><line><field name="git-branch"/></line></layout>
</statusloom>`)
	t.Setenv("PATH", t.TempDir())
	calls := stubCollectGit(t, func(context.Context, string, gitstatus.Options) (*schema.RepositorySnapshot, error) {
		t.Error("render path ran git")
		return nil, nil
	})
	cwd := t.TempDir()
	old := time.Now().Add(-24 * time.Hour)
	if err := cache.StoreRepo(cwd, schema.RepositorySnapshot{Root: cwd, Branch: "seeded-branch"}, old); err != nil {
		t.Fatalf("StoreRepo: %v", err)
	}
	spawns := captureSpawns(t)

	if out := renderGit(t, cwd); !strings.Contains(out, "seeded-branch") {
		t.Fatalf("render = %q, want the cached branch", out)
	}
	if calls.Load() != 0 {
		t.Fatalf("collectGit calls = %d, want 0", calls.Load())
	}
	args := gitSpawn(*spawns)
	if args == nil {
		t.Fatalf("no spawn carried --git-cwd: %q", *spawns)
	}
	if argAfter(args, "--git-cwd") != cwd || argAfter(args, "--git-interval-ms") != "1500" ||
		argAfter(args, "--git-timeout-ms") != "900" ||
		!slices.Contains(args, "--git-untracked=false") || !slices.Contains(args, "--git-numstat=true") {
		t.Errorf("spawn args = %q, want cwd %q, interval 1500, timeout 900, untracked=false, numstat=true", args, cwd)
	}
}

// TestRenderGitGating: a document without git-derived values never asks for a
// git collection.
func TestRenderGitGating(t *testing.T) {
	setupGitEnv(t, `<field name="model"/><field name="current-directory"/>`)
	spawns := captureSpawns(t)
	renderGit(t, t.TempDir())
	if args := gitSpawn(*spawns); args != nil {
		t.Fatalf("spawn %q carried --git-cwd for a document without git fields", args)
	}
}

// TestRefreshGit_EndToEnd: the first render in a new repository cwd shows no
// git info and spawns the worker; the worker runs the real collector; the next
// render shows the branch and does not ask again while the cwd is not due.
func TestRefreshGit_EndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
	setupGitEnv(t, gitDoc)
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "feature-x"}, {"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	spawns := captureSpawns(t)

	if out := renderGit(t, repo); strings.Contains(out, "feature-x") {
		t.Fatalf("first render = %q, want no git info yet", out)
	}
	args := gitSpawn(*spawns)
	if args == nil {
		t.Fatalf("first render did not ask for a git collection: %q", *spawns)
	}
	runSpawned(t, args)

	if got := cache.LoadRepo(repo); got == nil || got.Branch != "feature-x" {
		t.Fatalf("repo cache after worker = %+v, want branch feature-x", got)
	}
	w := cache.LoadRefreshSchedule().Git[repo]
	if w.LastSuccess.IsZero() || w.NextDueAt.Sub(w.LastSuccess) != 3*time.Second {
		t.Fatalf("Git[%q] = %+v, want success due 3s later (default cache-ttl-ms)", repo, w)
	}

	*spawns = nil
	if out := renderGit(t, repo); !strings.Contains(out, "feature-x") {
		t.Fatalf("second render = %q, want branch feature-x", out)
	}
	if args := gitSpawn(*spawns); args != nil {
		t.Fatalf("second render asked for git again while not due: %q", args)
	}
}

// TestRefreshGit_NotARepository: "not a repository" drops a stale snapshot and
// is remembered for GitNotRepoInterval, so a non-repo cwd does not spawn the
// worker on every render.
func TestRefreshGit_NotARepository(t *testing.T) {
	setupGitEnv(t, gitDoc)
	calls := stubCollectGit(t, func(context.Context, string, gitstatus.Options) (*schema.RepositorySnapshot, error) {
		return nil, nil
	})
	cwd := t.TempDir()
	if err := cache.StoreRepo(cwd, schema.RepositorySnapshot{Branch: "gone"}, time.Now()); err != nil {
		t.Fatalf("StoreRepo: %v", err)
	}
	spawns := captureSpawns(t)
	renderGit(t, cwd)
	args := gitSpawn(*spawns)
	if args == nil {
		t.Fatal("render did not ask for a git collection")
	}
	runSpawned(t, args)

	if calls.Load() != 1 {
		t.Fatalf("collectGit calls = %d, want 1", calls.Load())
	}
	if got := cache.LoadRepo(cwd); got != nil {
		t.Fatalf("repo cache = %+v, want the stale snapshot removed", got)
	}
	w := cache.LoadRefreshSchedule().Git[cwd]
	if got := w.NextDueAt.Sub(w.LastAttempt); got != cache.GitNotRepoInterval {
		t.Fatalf("not-a-repo interval = %v, want %v", got, cache.GitNotRepoInterval)
	}
	*spawns = nil
	if out := renderGit(t, cwd); strings.Contains(out, "gone") {
		t.Fatalf("render = %q, want no git info", out)
	}
	if args := gitSpawn(*spawns); args != nil {
		t.Fatalf("non-repo cwd asked for git again within %v: %q", cache.GitNotRepoInterval, args)
	}
}

// TestRefreshGit_FailureKeepsSnapshot: a git error or timeout keeps the last
// snapshot and backs off; a second worker run before the backoff ends does
// not collect again.
func TestRefreshGit_FailureKeepsSnapshot(t *testing.T) {
	setupGitEnv(t, gitDoc)
	var gotOpts gitstatus.Options
	calls := stubCollectGit(t, func(_ context.Context, _ string, o gitstatus.Options) (*schema.RepositorySnapshot, error) {
		gotOpts = o
		return nil, errors.New("timeout")
	})
	cwd := filepath.Join(t.TempDir(), "repo")
	if err := cache.StoreRepo(cwd, schema.RepositorySnapshot{Branch: "kept"}, time.Now()); err != nil {
		t.Fatalf("StoreRepo: %v", err)
	}
	args := []string{"--once", "--git-cwd", cwd, "--git-interval-ms", "3000", "--git-timeout-ms", "250", "--git-untracked=true"}
	for range 2 {
		if code := runRefresh(args, os.Stdout, os.Stderr); code != 0 {
			t.Fatalf("runRefresh = %d, want 0", code)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("collectGit calls = %d, want 1 (second run is inside the backoff)", calls.Load())
	}
	if gotOpts.Timeout != 250*time.Millisecond || !gotOpts.IncludeUntracked || gotOpts.CollectNumstat {
		t.Errorf("collector options = %+v, want timeout 250ms, untracked, no numstat", gotOpts)
	}
	if got := cache.LoadRepo(cwd); got == nil || got.Branch != "kept" {
		t.Fatalf("repo cache = %+v, want the last snapshot kept", got)
	}
	w := cache.LoadRefreshSchedule().Git[cwd]
	if w.Failures != 1 || w.NextDueAt.Sub(w.LastAttempt) != 6*time.Second {
		t.Fatalf("Git[%q] = %+v, want 1 failure backed off to 6s", cwd, w)
	}
}

// TestRefreshProbeRejectsGitCwd: --probe is a standalone source fetch and
// refuses the worker's git flag like its other work-item flags.
func TestRefreshProbeRejectsGitCwd(t *testing.T) {
	setupEnv(t)
	if code := runRefresh([]string{"--probe", "x", "--git-cwd", t.TempDir()}, os.Stdout, os.Stderr); code != 2 {
		t.Fatalf("runRefresh = %d, want 2", code)
	}
}
