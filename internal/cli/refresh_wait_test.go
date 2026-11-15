package cli

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/gitstatus"
	"github.com/yacchi/statusloom/internal/schema"
	"github.com/yacchi/statusloom/internal/sources"
	"github.com/yacchi/statusloom/internal/testenv"
	"github.com/yacchi/statusloom/internal/usage"
)

// Render-side <refresh wait> (plans/refresh-wait.md). The worker is never a
// real subprocess here: startRefreshProcess is replaced by a fake that runs
// the real worker (runRefresh) or a scripted stand-in on a goroutine, and the
// wait's sleep is counted so a test can tell whether the render waited at all.

// waitDoc is a git-branch document with the given <refresh> element.
func waitDoc(refresh string) string {
	return `<statusloom version="1" tool="claude-code" color-level="none">
  ` + refresh + `
  <layout name="default" active="true"><line><field name="git-branch"/></line></layout>
</statusloom>`
}

// setupWait isolates the environment, saves waitDoc(refresh), shortens the
// poll interval and counts the wait's sleeps. Background goroutines a test
// starts through goWait are joined before the temp dirs are removed.
func setupWait(t *testing.T, refresh string) (sleeps *atomic.Int32, goWait func(func())) {
	t.Helper()
	setupEnv(t)
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	writeDocument(t, "claude-code", waitDoc(refresh))

	var n atomic.Int32
	prevSleep, prevPoll := waitSleep, waitPollInterval
	waitSleep = func(d time.Duration) { n.Add(1); time.Sleep(d) }
	waitPollInterval = 2 * time.Millisecond
	var wg sync.WaitGroup
	t.Cleanup(func() {
		wg.Wait()
		waitSleep, waitPollInterval = prevSleep, prevPoll
	})
	return &n, func(f func()) {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
}

// fakeSpawn replaces startRefreshProcess: every spawn is counted and handed
// to worker on a goroutine (after delay). The TestMain stub is restored by
// t.Cleanup.
func fakeSpawn(t *testing.T, goWait func(func()), delay time.Duration, worker func(args []string)) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	prev := startRefreshProcess
	startRefreshProcess = func(args ...string) (int, error) {
		n.Add(1)
		args = slices.Clone(args)
		goWait(func() {
			time.Sleep(delay)
			worker(args)
		})
		return 0, nil
	}
	t.Cleanup(func() { startRefreshProcess = prev })
	return &n
}

// realWorker runs the refresh worker with the spawn's arguments.
func realWorker(args []string) { runRefresh(args[1:], io.Discard, io.Discard) }

// idleWorker stands in for a worker that ends without attempting anything.
func idleWorker(args []string) { cache.ReleaseRefreshLease(args[3]) }

// afterFirstWaitSleep returns a channel closed once the render has slept in
// its wait loop for the first time, i.e. has observed the lease/state it is
// waiting on. A scripted "other worker" blocks on it before acting, so the
// ordering never depends on wall-clock timing. Call after setupWait (its
// cleanup restores waitSleep). The wait is capped so a bug cannot hang.
func afterFirstWaitSleep() <-chan struct{} {
	ch := make(chan struct{})
	var once sync.Once
	inner := waitSleep
	waitSleep = func(d time.Duration) {
		once.Do(func() { close(ch) })
		inner(d)
	}
	return ch
}

func awaitSignal(ch <-chan struct{}) {
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
	}
}

// gitRepoAt makes the stubbed git collector report branch for every cwd.
func gitRepoAt(t *testing.T, branch string) {
	stubCollectGit(t, func(_ context.Context, dir string, _ gitstatus.Options) (*schema.RepositorySnapshot, error) {
		return &schema.RepositorySnapshot{Root: dir, Branch: branch}, nil
	})
}

func timedRenderGit(t *testing.T, cwd string, env map[string]string) (string, time.Duration) {
	t.Helper()
	start := time.Now()
	stdout, stderr, code := runCLI(t, []string{"claude"}, gitPayload(t, cwd), env)
	if code != 0 {
		t.Fatalf("claude exit = %d (stderr: %s)", code, stderr)
	}
	return stdout, time.Since(start)
}

func TestRefreshWait_ColdGitShownOnFirstRender(t *testing.T) {
	sleeps, goWait := setupWait(t, `<refresh wait="git" timeout-ms="3000"/>`)
	gitRepoAt(t, "waited-branch")
	spawns := fakeSpawn(t, goWait, 30*time.Millisecond, realWorker)
	cwd := t.TempDir()

	out, took := timedRenderGit(t, cwd, nil)
	if !strings.Contains(out, "waited-branch") {
		t.Fatalf("first render = %q, want the branch the worker collected", out)
	}
	if took >= 2*time.Second {
		t.Errorf("render took %v, want it to end once the git item was attempted", took)
	}
	if spawns.Load() != 1 || sleeps.Load() == 0 {
		t.Errorf("spawns = %d, sleeps = %d; want one spawn (before drawing, none after) and a wait", spawns.Load(), sleeps.Load())
	}

	// The cwd is warm now: the next render does not wait.
	before := sleeps.Load()
	if out, _ := timedRenderGit(t, cwd, nil); !strings.Contains(out, "waited-branch") {
		t.Fatalf("second render = %q", out)
	}
	if sleeps.Load() != before {
		t.Errorf("a warm render waited")
	}
}

func TestRefreshWait_Timeout(t *testing.T) {
	sleeps, goWait := setupWait(t, `<refresh wait="git" timeout-ms="150"/>`)
	// The spawned worker never gets to the git item and keeps its lease.
	spawns := fakeSpawn(t, goWait, 0, func([]string) {})

	out, took := timedRenderGit(t, t.TempDir(), nil)
	if !isFallback(out) {
		t.Errorf("render = %q, want no git info", out)
	}
	if took < 150*time.Millisecond || took >= 2*time.Second {
		t.Errorf("render took %v, want about timeout-ms (150ms)", took)
	}
	if _, held := cache.CurrentRefreshLeaseID(); !held {
		t.Errorf("the render released the worker's lease")
	}
	if spawns.Load() != 1 || sleeps.Load() == 0 {
		t.Errorf("spawns = %d, sleeps = %d; want 1 spawn and a wait", spawns.Load(), sleeps.Load())
	}
}

func TestRefreshWait_FailureEndsWait(t *testing.T) {
	for name, collect := range map[string]func(context.Context, string, gitstatus.Options) (*schema.RepositorySnapshot, error){
		"git error": func(context.Context, string, gitstatus.Options) (*schema.RepositorySnapshot, error) {
			return nil, errors.New("git timed out")
		},
		"not a repository": func(context.Context, string, gitstatus.Options) (*schema.RepositorySnapshot, error) {
			return nil, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, goWait := setupWait(t, `<refresh wait="git" timeout-ms="3000"/>`)
			stubCollectGit(t, collect)
			fakeSpawn(t, goWait, 10*time.Millisecond, realWorker)
			out, took := timedRenderGit(t, t.TempDir(), nil)
			if !isFallback(out) {
				t.Errorf("render = %q, want no git info", out)
			}
			if took >= 2*time.Second {
				t.Errorf("render took %v, want the attempt to end the wait early", took)
			}
		})
	}
}

func TestRefreshWait_LeaseHeldByAnother(t *testing.T) {
	t.Run("waits for that worker", func(t *testing.T) {
		_, goWait := setupWait(t, `<refresh wait="git" timeout-ms="3000"/>`)
		spawns := fakeSpawn(t, goWait, 0, realWorker)
		cwd := t.TempDir()
		if ok, err := cache.AcquireRefreshLease("other", time.Now()); !ok || err != nil {
			t.Fatalf("AcquireRefreshLease = %v, %v", ok, err)
		}
		observed := afterFirstWaitSleep()
		goWait(func() {
			awaitSignal(observed)
			// The other worker records its own (earlier) start time.
			at := time.Now().Add(-time.Hour)
			_ = cache.StoreRepo(cwd, schema.RepositorySnapshot{Root: cwd, Branch: "other-worker"}, at)
			s := cache.LoadRefreshSchedule()
			s.Git[cwd] = cache.WorkSchedule{LastAttempt: at, LastSuccess: at, NextDueAt: time.Now().Add(time.Hour)}
			_ = cache.StoreRefreshSchedule(s)
		})
		out, took := timedRenderGit(t, cwd, nil)
		if !strings.Contains(out, "other-worker") {
			t.Fatalf("render = %q, want the other worker's result", out)
		}
		if took >= 2*time.Second {
			t.Errorf("render took %v", took)
		}
		if spawns.Load() != 0 {
			t.Errorf("spawns = %d, want 0 while another worker holds the lease", spawns.Load())
		}
	})

	t.Run("respawns once when that worker ends without the item", func(t *testing.T) {
		_, goWait := setupWait(t, `<refresh wait="git" timeout-ms="3000"/>`)
		spawns := fakeSpawn(t, goWait, 20*time.Millisecond, idleWorker)
		if ok, err := cache.AcquireRefreshLease("other", time.Now()); !ok || err != nil {
			t.Fatalf("AcquireRefreshLease = %v, %v", ok, err)
		}
		observed := afterFirstWaitSleep()
		goWait(func() {
			awaitSignal(observed)
			cache.ReleaseRefreshLease("other")
		})
		out, took := timedRenderGit(t, t.TempDir(), nil)
		if !isFallback(out) {
			t.Errorf("render = %q, want no git info", out)
		}
		if spawns.Load() != 1 {
			t.Errorf("spawns = %d, want exactly 1 retry", spawns.Load())
		}
		if took >= 2*time.Second {
			t.Errorf("render took %v, want it to stop after the retried worker ended", took)
		}
	})

	t.Run("at most two spawns", func(t *testing.T) {
		_, goWait := setupWait(t, `<refresh wait="git" timeout-ms="3000"/>`)
		spawns := fakeSpawn(t, goWait, 20*time.Millisecond, idleWorker)
		out, took := timedRenderGit(t, t.TempDir(), nil)
		if !isFallback(out) {
			t.Errorf("render = %q, want no git info", out)
		}
		if spawns.Load() != 2 {
			t.Errorf("spawns = %d, want 2 (initial + one retry)", spawns.Load())
		}
		if took >= 2*time.Second {
			t.Errorf("render took %v", took)
		}
	})
}

// TestRefreshWait_OldWorkerDropsGitKey: an older worker rewriting
// schedule.json without the "git" key never looks like a completed attempt.
func TestRefreshWait_OldWorkerDropsGitKey(t *testing.T) {
	sleeps, goWait := setupWait(t, `<refresh wait="git" timeout-ms="200"/>`)
	spawns := fakeSpawn(t, goWait, 0, realWorker)
	if ok, err := cache.AcquireRefreshLease("old-worker", time.Now()); !ok || err != nil {
		t.Fatalf("AcquireRefreshLease = %v, %v", ok, err)
	}
	dir, err := cache.Dir()
	if err != nil {
		t.Fatal(err)
	}
	observed := afterFirstWaitSleep()
	goWait(func() {
		awaitSignal(observed)
		body := `{"schemaVersion":2,"transcripts":{},"usage":{"default":{"nextDueAt":"` +
			time.Now().Add(time.Hour).Format(time.RFC3339Nano) + `","lastAttempt":"` + time.Now().Format(time.RFC3339Nano) + `"}},"sources":{}}`
		_ = os.WriteFile(filepath.Join(dir, "refresh", "schedule.json"), []byte(body), 0o600)
	})
	out, took := timedRenderGit(t, t.TempDir(), nil)
	if !isFallback(out) {
		t.Errorf("render = %q, want no git info", out)
	}
	if took < 200*time.Millisecond {
		t.Errorf("render took %v, want the full timeout (no false completion)", took)
	}
	if spawns.Load() != 0 || sleeps.Load() == 0 {
		t.Errorf("spawns = %d, sleeps = %d", spawns.Load(), sleeps.Load())
	}
}

// TestRefreshWait_WarmIsUnchanged: with the item already attempted once, a
// document with <refresh> renders and spawns exactly like one without.
func TestRefreshWait_WarmIsUnchanged(t *testing.T) {
	render := func(refresh string) (string, [][]string, int32) {
		sleeps, _ := setupWait(t, refresh)
		cwd := filepath.Join(os.TempDir(), "statusloom-warm-cwd")
		past := time.Now().Add(-time.Minute)
		if err := cache.StoreRepo(cwd, schema.RepositorySnapshot{Root: cwd, Branch: "cached-branch"}, past); err != nil {
			t.Fatal(err)
		}
		s := cache.LoadRefreshSchedule()
		s.Git[cwd] = cache.WorkSchedule{LastAttempt: past, LastSuccess: past, NextDueAt: past} // due again
		if err := cache.StoreRefreshSchedule(s); err != nil {
			t.Fatal(err)
		}
		spawns := captureSpawns(t)
		out, _ := timedRenderGit(t, cwd, nil)
		var norm [][]string
		for _, a := range *spawns {
			norm = append(norm, normalizeSpawn(t, a))
		}
		return out, norm, sleeps.Load()
	}
	plainOut, plainSpawns, _ := render("")
	waitOut, waitSpawns, sleeps := render(`<refresh wait="git"/>`)
	if waitOut != plainOut || !strings.Contains(plainOut, "cached-branch") {
		t.Errorf("output with <refresh> = %q, without = %q; want byte-identical", waitOut, plainOut)
	}
	if !slices.EqualFunc(waitSpawns, plainSpawns, slices.Equal) || len(plainSpawns) != 1 {
		t.Errorf("spawns with <refresh> = %q, without = %q", waitSpawns, plainSpawns)
	}
	if sleeps != 0 {
		t.Errorf("a warm render waited (%d sleeps)", sleeps)
	}
}

func TestRefreshWait_WaitOnDue(t *testing.T) {
	seed := func(t *testing.T, cwd string, failures int) {
		past := time.Now().Add(-time.Minute)
		if err := cache.StoreRepo(cwd, schema.RepositorySnapshot{Root: cwd, Branch: "old-branch"}, past); err != nil {
			t.Fatal(err)
		}
		s := cache.LoadRefreshSchedule()
		s.Git[cwd] = cache.WorkSchedule{LastAttempt: past, NextDueAt: past, Failures: failures}
		if err := cache.StoreRefreshSchedule(s); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("due item is waited for", func(t *testing.T) {
		sleeps, goWait := setupWait(t, `<refresh wait="git" wait-on="due" timeout-ms="3000"/>`)
		gitRepoAt(t, "new-branch")
		fakeSpawn(t, goWait, 10*time.Millisecond, realWorker)
		cwd := t.TempDir()
		seed(t, cwd, 0)
		if out, _ := timedRenderGit(t, cwd, nil); !strings.Contains(out, "new-branch") {
			t.Errorf("render = %q, want the refreshed branch", out)
		}
		if sleeps.Load() == 0 {
			t.Errorf("did not wait")
		}
	})
	t.Run("backing-off item is not", func(t *testing.T) {
		sleeps, _ := setupWait(t, `<refresh wait="git" wait-on="due" timeout-ms="3000"/>`)
		spawns := captureSpawns(t)
		cwd := t.TempDir()
		seed(t, cwd, 1)
		if out, _ := timedRenderGit(t, cwd, nil); !strings.Contains(out, "old-branch") {
			t.Errorf("render = %q, want the cached branch", out)
		}
		if sleeps.Load() != 0 || len(*spawns) != 1 {
			t.Errorf("sleeps = %d, spawns = %d; want no wait and the usual spawn after drawing", sleeps.Load(), len(*spawns))
		}
	})
	t.Run("cold mode ignores a due warm item", func(t *testing.T) {
		sleeps, _ := setupWait(t, `<refresh wait="git" timeout-ms="3000"/>`)
		captureSpawns(t)
		cwd := t.TempDir()
		seed(t, cwd, 0)
		timedRenderGit(t, cwd, nil)
		if sleeps.Load() != 0 {
			t.Errorf("cold mode waited for an attempted item")
		}
	})
}

// TestRefreshWait_NeverWaits: previews, the subagent panel, the kill switch
// and wait tokens for work the document does not use never wait.
func TestRefreshWait_NeverWaits(t *testing.T) {
	const refresh = `<refresh wait="git" timeout-ms="3000"/>`
	check := func(t *testing.T, sleeps *atomic.Int32, took time.Duration) {
		t.Helper()
		if sleeps.Load() != 0 || took >= 2*time.Second {
			t.Errorf("sleeps = %d, took %v; want no wait", sleeps.Load(), took)
		}
	}
	t.Run("kill switch", func(t *testing.T) {
		sleeps, _ := setupWait(t, refresh)
		spawns := captureSpawns(t)
		_, took := timedRenderGit(t, t.TempDir(), map[string]string{"STATUSLOOM_NO_WAIT": "1"})
		check(t, sleeps, took)
		if len(*spawns) != 1 {
			t.Errorf("spawns = %d, want the usual spawn after drawing", len(*spawns))
		}
	})
	t.Run("draft preview", func(t *testing.T) {
		sleeps, _ := setupWait(t, refresh)
		writeDraft(t, "claude-code", waitDoc(refresh))
		spawns := captureSpawns(t)
		start := time.Now()
		if _, stderr, code := runCLI(t, []string{"claude", "--draft"}, gitPayload(t, t.TempDir()), nil); code != 0 {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		check(t, sleeps, time.Since(start))
		if len(*spawns) != 0 {
			t.Errorf("a preview spawned")
		}
	})
	t.Run("--set preview", func(t *testing.T) {
		sleeps, _ := setupWait(t, refresh)
		start := time.Now()
		if _, stderr, code := runCLI(t, []string{"claude", "--set", "extra-usage-limit-usd=50"}, gitPayload(t, t.TempDir()), nil); code != 0 {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		check(t, sleeps, time.Since(start))
	})
	t.Run("claude-subagent", func(t *testing.T) {
		sleeps, _ := setupWait(t, refresh)
		start := time.Now()
		if _, stderr, code := runCLI(t, []string{"claude-subagent"}, subagentStdin(t, 40, map[string]any{"id": "t1", "description": "x"}), nil); code != 0 {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		check(t, sleeps, time.Since(start))
	})
	t.Run("unused work items", func(t *testing.T) {
		// git-branch only: transcript is unused and usage is switched off.
		sleeps, _ := setupWait(t, `<refresh wait="transcript usage source:nope" timeout-ms="3000"/>`)
		captureSpawns(t)
		_, took := timedRenderGit(t, t.TempDir(), nil)
		check(t, sleeps, took)
	})
}

// TestRefreshWait_MonitorWaits: monitor renders live sessions, so it waits.
func TestRefreshWait_MonitorWaits(t *testing.T) {
	sleeps, goWait := setupWait(t, `<refresh wait="git" timeout-ms="3000"/>`)
	gitRepoAt(t, "monitor-branch")
	fakeSpawn(t, goWait, 10*time.Millisecond, realWorker)
	stdout, stderr, code := runCLI(t, []string{"monitor", "--emit-url", "http://127.0.0.1:1/api/live", "--token", "x"}, gitPayload(t, t.TempDir()), nil)
	if code != 0 {
		t.Fatalf("monitor exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "monitor-branch") || sleeps.Load() == 0 {
		t.Errorf("monitor = %q (sleeps %d), want it to wait and show the branch", stdout, sleeps.Load())
	}
}

// TestRefreshWait_SourcesToken: `sources` waits for a cold command source.
func TestRefreshWait_SourcesToken(t *testing.T) {
	sleeps, goWait := setupWait(t, `<refresh wait="sources" timeout-ms="3000"/>`)
	writeDocument(t, "claude-code", `<statusloom version="1" tool="claude-code" color-level="none">
  <refresh wait="sources" timeout-ms="3000"/>
  <layout name="default" active="true"><line><field name="source" source="cmd" select=".v"/></line></layout>
</statusloom>`)
	saveSources(t, sources.Definition{ID: "cmd", Kind: sources.KindCommand, Argv: []string{"x"}})
	stubFetch(t, func(context.Context, sources.Definition, string) (sources.FetchResult, error) {
		return sources.FetchResult{Body: []byte(`{"v":"from-source"}`)}, nil
	})
	fakeSpawn(t, goWait, 10*time.Millisecond, realWorker)
	if out := renderClaude(t, nil); !strings.Contains(out, "from-source") || sleeps.Load() == 0 {
		t.Errorf("render = %q (sleeps %d), want the fetched source value on the first render", out, sleeps.Load())
	}
}

// TestRefreshWait_TokensCoverWorkKinds keeps the DSL wait tokens in step with
// the refresh work item kinds.
func TestRefreshWait_TokensCoverWorkKinds(t *testing.T) {
	for _, k := range cache.WorkKinds {
		tok, key := string(k), "x"
		if k == cache.WorkSource {
			tok = dsl.WaitTokenSourcePrefix + key
		}
		doc, diags := dsl.ParseAndValidate(waitDoc(`<refresh wait="` + tok + `"/>`))
		if dsl.HasErrors(diags) {
			t.Fatalf("%s: %v", tok, diags)
		}
		if !dsl.DocumentRefreshPolicy(doc).Wants(string(k), key) {
			t.Errorf("wait=%q does not match work kind %q", tok, k)
		}
	}
}

// TestRefreshWorker_DataBeforeSchedule pins the invariant the wait relies on:
// each worker step writes its data before the schedule entry that marks the
// attempt, so schedule.json is the newest file once an item completes.
func TestRefreshWorker_DataBeforeSchedule(t *testing.T) {
	steps := map[string]func(t *testing.T) []string{
		"git": func(t *testing.T) []string {
			gitRepoAt(t, "b")
			return []string{"--once", "--git-cwd", t.TempDir()}
		},
		"transcript": func(t *testing.T) []string {
			p := filepath.Join(t.TempDir(), "t.jsonl")
			if err := os.WriteFile(p, []byte(`{"type":"user"}`+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return []string{"--once", "--session-id", "s1", "--transcript", p}
		},
		"usage": func(t *testing.T) []string {
			t.Setenv("STATUSLOOM_NO_USAGE_API", "")
			prevTok, prevFetch := acquireUsageToken, fetchUsage
			acquireUsageToken = func(func(string) string) (string, error) { return "tok", nil }
			fetchUsage = func(context.Context, string, string) (*usage.Report, int, error) {
				return &usage.Report{FiveHour: &usage.Window{Utilization: 10, ResetsAt: time.Now().Add(time.Hour)}}, 200, nil
			}
			t.Cleanup(func() { acquireUsageToken, fetchUsage = prevTok, prevFetch })
			return []string{"--once"}
		},
		"sources": func(t *testing.T) []string {
			saveSources(t, sources.Definition{ID: "cmd", Kind: sources.KindCommand, Argv: []string{"x"}})
			stubFetch(t, func(context.Context, sources.Definition, string) (sources.FetchResult, error) {
				return sources.FetchResult{Body: []byte(`{}`)}, nil
			})
			return []string{"--once", "--sources", "cmd"}
		},
	}
	for name, step := range steps {
		t.Run(name, func(t *testing.T) {
			setupEnv(t)
			t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
			prevMerge := mergeSessions
			mergeSessions = func(time.Time) error { return nil }
			t.Cleanup(func() { mergeSessions = prevMerge })
			args := step(t)
			if code := runRefresh(args, io.Discard, io.Discard); code != 0 {
				t.Fatalf("runRefresh = %d", code)
			}
			dir, err := cache.Dir()
			if err != nil {
				t.Fatal(err)
			}
			sched, err := os.Stat(filepath.Join(dir, "refresh", "schedule.json"))
			if err != nil {
				t.Fatalf("schedule.json: %v", err)
			}
			data := 0
			_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || filepath.Base(p) == "schedule.json" {
					return nil
				}
				data++
				if info, err := d.Info(); err == nil && info.ModTime().After(sched.ModTime()) {
					t.Errorf("%s (%v) was written after schedule.json (%v)", p, info.ModTime(), sched.ModTime())
				}
				return nil
			})
			if data == 0 {
				t.Errorf("the %s step wrote no data file", name)
			}
		})
	}
}

// isFallback reports a render whose only line (git-branch) came out empty, so
// the built-in fallback line (model + tool version) was printed instead.
func isFallback(out string) bool { return strings.HasPrefix(out, "Opus 4.8 | v") }

// TestRefreshWait_DeadHolderSpawnsPromptly: a lease whose holder process is
// dead (a worker or relay killed mid-run) is not waited on: the render takes
// it over and spawns its own worker at once instead of waiting out the
// timeout (or, without <refresh wait>, the 2-minute lease TTL).
func TestRefreshWait_DeadHolderSpawnsPromptly(t *testing.T) {
	_, goWait := setupWait(t, `<refresh wait="git" timeout-ms="3000"/>`)
	gitRepoAt(t, "after-dead-holder")
	spawns := fakeSpawn(t, goWait, 0, realWorker)
	if ok, err := cache.AcquireRefreshLease("killed", time.Now()); !ok || err != nil {
		t.Fatalf("AcquireRefreshLease = %v, %v", ok, err)
	}
	if !cache.SetRefreshLeaseHolder("killed", 0, testenv.ExitedPID(t)) {
		t.Fatal("SetRefreshLeaseHolder failed")
	}
	out, took := timedRenderGit(t, t.TempDir(), nil)
	if !strings.Contains(out, "after-dead-holder") {
		t.Fatalf("render = %q, want the branch from the render's own worker", out)
	}
	if spawns.Load() != 1 {
		t.Errorf("spawns = %d, want 1 (the dead holder's lease taken over)", spawns.Load())
	}
	if took >= time.Second {
		t.Errorf("render took %v, want no wait on the dead holder", took)
	}
}

// TestRefreshWait_WatchedHolderDies: the render watches another process's
// live worker; when that worker dies without releasing the lease, the wait
// notices at the next poll and spawns its one retry.
func TestRefreshWait_WatchedHolderDies(t *testing.T) {
	_, goWait := setupWait(t, `<refresh wait="git" timeout-ms="3000"/>`)
	gitRepoAt(t, "after-holder-died")
	spawns := fakeSpawn(t, goWait, 0, realWorker)
	if ok, err := cache.AcquireRefreshLease("other", time.Now()); !ok || err != nil {
		t.Fatalf("AcquireRefreshLease = %v, %v", ok, err)
	}
	dead := testenv.ExitedPID(t)
	observed := afterFirstWaitSleep()
	goWait(func() {
		awaitSignal(observed)
		// The holder is killed: lease.json stays, naming a dead process.
		cache.SetRefreshLeaseHolder("other", 0, dead)
	})
	out, took := timedRenderGit(t, t.TempDir(), nil)
	if !strings.Contains(out, "after-holder-died") {
		t.Fatalf("render = %q, want the branch from the retried worker", out)
	}
	if spawns.Load() != 1 {
		t.Errorf("spawns = %d, want exactly the one retry", spawns.Load())
	}
	if took >= time.Second {
		t.Errorf("render took %v, want the retry right after the holder died", took)
	}
}
