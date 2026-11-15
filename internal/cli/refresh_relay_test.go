package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/testenv"
)

func TestRelayRefreshArgs(t *testing.T) {
	in := []string{"refresh", "--once", "--lease-id", "abc"}
	got := relayRefreshArgs(in)
	want := []string{"refresh", "--relay", "--once", "--lease-id", "abc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relayRefreshArgs(%q) = %q, want %q", in, got, want)
	}
	if other := []string{"source", "x"}; !reflect.DeepEqual(relayRefreshArgs(other), other) {
		t.Fatalf("non-refresh args must pass through unchanged")
	}
}

func TestRunRefreshRelayUsage(t *testing.T) {
	// --relay only relaunches a --once worker; anything else is a usage error.
	if code := runRefresh([]string{"--relay"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("--relay without --once: exit %d, want 2", code)
	}
	if code := runRefresh([]string{"--relay", "--once", "--probe", "x"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("--relay with --probe: exit %d, want 2", code)
	}
	// Under `go test` spawnSelf never re-execs the test binary, so a valid
	// relay returns success without starting anything or touching the lease.
	if code := runRefresh([]string{"--relay", "--once", "--lease-id", "abc"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("valid relay: exit %d, want 0", code)
	}
}

// leaseHolder reads the holderPid recorded in lease.json (0 when absent).
func leaseHolder(t *testing.T) (string, int) {
	t.Helper()
	dir, err := cache.Dir()
	if err != nil {
		t.Fatal(err)
	}
	var l cache.RefreshLease
	if ok, err := cache.ReadJSON(filepath.Join(dir, "refresh", "lease.json"), &l); !ok || err != nil {
		return "", 0
	}
	return l.ID, l.HolderPID
}

// TestRunRefreshRelay_HandsLeaseToWorker: the relay records the worker it
// started as the lease holder before it exits, so the lease never names the
// exited relay while the worker runs.
func TestRunRefreshRelay_HandsLeaseToWorker(t *testing.T) {
	setupEnv(t)
	if ok, err := cache.AcquireRefreshLease("abc", time.Now()); !ok || err != nil {
		t.Fatalf("AcquireRefreshLease = %v, %v", ok, err)
	}
	var got []string
	prev := relaySpawn
	relaySpawn = func(args []string) (int, error) { got = args; return 4242, nil }
	t.Cleanup(func() { relaySpawn = prev })

	if code := runRefresh([]string{"--relay", "--once", "--lease-id", "abc"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("relay exit = %d", code)
	}
	if want := []string{"refresh", "--once", "--lease-id", "abc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("worker args = %q, want %q", got, want)
	}
	if id, holder := leaseHolder(t); id != "abc" || holder != 4242 {
		t.Errorf("lease = %q held by %d, want abc held by the worker 4242", id, holder)
	}

	// A failed start releases the lease instead.
	relaySpawn = func([]string) (int, error) { return 0, errors.New("no") }
	if code := runRefresh([]string{"--relay", "--once", "--lease-id", "abc"}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("failed relay exit = %d, want 1", code)
	}
	if id, _ := leaseHolder(t); id != "" {
		t.Errorf("lease %q still held after a failed relay start", id)
	}
}

// TestStartPlannedRefresh_HandsLeaseToChild: the render records its child
// (the relay that starts the worker) as the holder before it can exit.
func TestStartPlannedRefresh_HandsLeaseToChild(t *testing.T) {
	setupEnv(t)
	prev := startRefreshProcess
	startRefreshProcess = func(...string) (int, error) { return 4343, nil }
	t.Cleanup(func() { startRefreshProcess = prev })

	id, outcome := startPlannedRefresh(refreshPlan{}, time.Now())
	if outcome != spawnStarted {
		t.Fatalf("outcome = %v, want spawnStarted", outcome)
	}
	if got, holder := leaseHolder(t); got != id || holder != 4343 {
		t.Errorf("lease = %q held by %d, want %q held by the child 4343", got, holder, id)
	}
}

// TestRunRefreshOnce_WorkerClaimsLease: the worker re-asserts itself as the
// holder of the lease it was handed (--lease-id).
func TestRunRefreshOnce_WorkerClaimsLease(t *testing.T) {
	setupEnv(t)
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	if ok, err := cache.AcquireRefreshLease("w", time.Now()); !ok || err != nil {
		t.Fatalf("AcquireRefreshLease = %v, %v", ok, err)
	}
	// A spawner that died before handing over leaves a dead holder behind.
	cache.SetRefreshLeaseHolder("w", 0, testenv.ExitedPID(t))
	var holder int
	prev := mergeSessions
	mergeSessions = func(time.Time) error { _, holder = leaseHolder(t); return nil }
	t.Cleanup(func() { mergeSessions = prev })

	if code := runRefresh([]string{"--once", "--lease-id", "w"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("worker exit = %d", code)
	}
	if holder != os.Getpid() {
		t.Errorf("holder while the worker ran = %d, want the worker %d", holder, os.Getpid())
	}
}
