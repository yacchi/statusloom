package cli

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/sources"
)

// sessionMergeStub replaces both seams; restores them with t.Cleanup.
// The pending stub counts calls and returns pending; the merge stub records
// each call's now and returns mergeErr.
func sessionMergeStub(t *testing.T, pending bool, mergeErr error) (pendingCalls *int, merges *[]time.Time) {
	t.Helper()
	calls := 0
	var recorded []time.Time
	prevPending, prevMerge := sessionMergePending, mergeSessions
	sessionMergePending = func() bool {
		calls++
		return pending
	}
	mergeSessions = func(now time.Time) error {
		recorded = append(recorded, now)
		return mergeErr
	}
	t.Cleanup(func() {
		sessionMergePending, mergeSessions = prevPending, prevMerge
	})
	return &calls, &recorded
}

func TestSessionMergeMarkerAloneSpawns(t *testing.T) {
	setupEnv(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	spawns := captureSpawns(t)
	pendingCalls, _ := sessionMergeStub(t, true, nil)

	req := refreshRequest{Raw: payload(t), Now: time.Now()}
	maybeStartRefresh(req)

	if len(*spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(*spawns))
	}
	got := normalizeSpawn(t, (*spawns)[0])
	want := []string{"refresh", "--once", "--lease-id", "L", "--cc-version", testVersion}
	if len(got) != len(want) {
		t.Fatalf("spawn args = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("spawn args = %q, want %q", got, want)
		}
	}
	if *pendingCalls != 1 {
		t.Fatalf("pending calls = %d, want 1", *pendingCalls)
	}
}

func TestSessionMergeNothingDueDoesNotSpawn(t *testing.T) {
	setupEnv(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	spawns := captureSpawns(t)
	pendingCalls, _ := sessionMergeStub(t, false, nil)

	maybeStartRefresh(refreshRequest{Raw: payload(t), Now: time.Now()})

	if len(*spawns) != 0 {
		t.Fatalf("spawns = %d, want 0", len(*spawns))
	}
	if *pendingCalls != 1 {
		t.Fatalf("pending calls = %d, want 1", *pendingCalls)
	}
}

func TestSessionMergeOtherDueSkipsPendingCheck(t *testing.T) {
	setupEnv(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("STATUSLOOM_NO_USAGE_API", "")
	spawns := captureSpawns(t)
	pendingCalls, _ := sessionMergeStub(t, false, nil)

	maybeStartRefresh(refreshRequest{Raw: payload(t), Now: time.Now(), ForceUsage: true})

	if len(*spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(*spawns))
	}
	found := false
	for _, a := range (*spawns)[0] {
		if a == "--force-usage" {
			found = true
		}
	}
	if !found {
		t.Fatalf("spawn args = %q, want --force-usage", (*spawns)[0])
	}
	if *pendingCalls != 0 {
		t.Fatalf("pending calls = %d, want 0 (stat must run only when nothing else is due)", *pendingCalls)
	}
}

func TestSessionMergeWorkerTakesLease(t *testing.T) {
	setupEnv(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	_, merges := sessionMergeStub(t, false, nil)

	if code := runRefresh([]string{"--once"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if len(*merges) != 1 {
		t.Fatalf("merges = %d, want 1", len(*merges))
	}
	if (*merges)[0].IsZero() {
		t.Fatal("merge received a zero time")
	}
}

func TestSessionMergeHandedOverLease(t *testing.T) {
	setupEnv(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	_, merges := sessionMergeStub(t, false, nil)

	if code := runRefresh([]string{"--once", "--lease-id", "handed"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if len(*merges) != 1 {
		t.Fatalf("merges = %d, want 1", len(*merges))
	}
}

func TestSessionMergeLeaseBusySkipsMerge(t *testing.T) {
	setupEnv(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	_, merges := sessionMergeStub(t, false, nil)

	ok, err := cache.AcquireRefreshLease("other", time.Now())
	if err != nil || !ok {
		t.Fatalf("pre-acquire lease: ok=%v err=%v", ok, err)
	}
	if code := runRefresh([]string{"--once"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if len(*merges) != 0 {
		t.Fatalf("merges = %d, want 0 when the lease belongs to someone else", len(*merges))
	}
}

func TestSessionMergeErrorKeepsExitZero(t *testing.T) {
	setupEnv(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	_, merges := sessionMergeStub(t, false, errors.New("boom"))

	if code := runRefresh([]string{"--once"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if len(*merges) != 1 {
		t.Fatalf("merges = %d, want 1", len(*merges))
	}
}

func TestSessionMergeProbeDoesNotMerge(t *testing.T) {
	setupEnv(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	stubFetch(t, func(ctx context.Context, def sources.Definition, etag string) (sources.FetchResult, error) {
		return sources.FetchResult{}, errors.New("fail")
	})
	_, merges := sessionMergeStub(t, false, nil)

	_ = runRefresh([]string{"--probe", "nope"}, io.Discard, io.Discard)

	if len(*merges) != 0 {
		t.Fatalf("merges = %d, want 0 in --probe mode", len(*merges))
	}
}

func TestSessionMergeRequiresOnce(t *testing.T) {
	setupEnv(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	_, merges := sessionMergeStub(t, false, nil)

	if code := runRefresh(nil, io.Discard, io.Discard); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if len(*merges) != 0 {
		t.Fatalf("merges = %d, want 0 without --once", len(*merges))
	}
}

func TestSessionMergeRunsAfterSourcesWhileLeaseHeld(t *testing.T) {
	setupEnv(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	saveSources(t, sources.Definition{ID: "cmd", Kind: sources.KindCommand, Argv: []string{"never-run"}})

	var calls []string
	stubFetch(t, func(ctx context.Context, def sources.Definition, etag string) (sources.FetchResult, error) {
		calls = append(calls, "fetch")
		return sources.FetchResult{Body: []byte(`{"v":7}`)}, nil
	})
	sessionMergeStub(t, false, nil)

	prevMerge := mergeSessions
	leaseFree := true
	mergeSessions = func(now time.Time) error {
		calls = append(calls, "merge")
		ok, err := cache.AcquireRefreshLease("probe", now)
		leaseFree = err == nil && ok
		return nil
	}
	t.Cleanup(func() { mergeSessions = prevMerge })

	if code := runRefresh([]string{"--once", "--sources", "cmd"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if len(calls) != 2 || calls[0] != "fetch" || calls[1] != "merge" {
		t.Fatalf("call order = %q, want [fetch merge]", calls)
	}
	if leaseFree {
		t.Fatal("lease was free during merge; the worker must still hold it")
	}
	ok, err := cache.AcquireRefreshLease("after", time.Now())
	if err != nil || !ok {
		t.Fatalf("lease not released after worker: ok=%v err=%v", ok, err)
	}
}
