package cache

import (
	"os"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/testenv"
)

// writeLease replaces lease.json with l as-is (bypassing AcquireRefreshLease).
func writeLease(t *testing.T, l RefreshLease) {
	t.Helper()
	p, err := leasePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteJSONAtomic(p, l); err != nil {
		t.Fatal(err)
	}
}

func readLease(t *testing.T) RefreshLease {
	t.Helper()
	p, err := leasePath()
	if err != nil {
		t.Fatal(err)
	}
	var l RefreshLease
	if ok, err := ReadJSON(p, &l); !ok || err != nil {
		t.Fatalf("read lease.json: ok=%v err=%v", ok, err)
	}
	return l
}

func isolateLease(t *testing.T) {
	t.Helper()
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
}

func TestAcquireRefreshLease_RecordsHolder(t *testing.T) {
	isolateLease(t)
	now := time.Now()
	if ok, err := AcquireRefreshLease("a", now); !ok || err != nil {
		t.Fatalf("AcquireRefreshLease = %v, %v", ok, err)
	}
	l := readLease(t)
	if l.ID != "a" || l.PID != os.Getpid() || l.HolderPID != os.Getpid() || l.HolderHost != localHost() {
		t.Fatalf("lease = %+v, want id a, pid and holderPid = %d, holderHost = %q", l, os.Getpid(), localHost())
	}
}

func TestAcquireRefreshLease_DeadHolderTakenOver(t *testing.T) {
	isolateLease(t)
	now := time.Now()
	dead := testenv.ExitedPID(t)
	writeLease(t, RefreshLease{SchemaVersion: 1, ID: "old", PID: dead, StartedAt: now, HolderPID: dead, HolderHost: localHost()})
	if id, held := CurrentRefreshLeaseID(); held {
		t.Errorf("CurrentRefreshLeaseID = %q, true; want a dead holder's lease reported free", id)
	}
	if ok, err := AcquireRefreshLease("new", now); !ok || err != nil {
		t.Fatalf("AcquireRefreshLease over a dead holder = %v, %v; want taken over within the TTL", ok, err)
	}
	if l := readLease(t); l.ID != "new" {
		t.Fatalf("lease id = %q, want new", l.ID)
	}
}

func TestAcquireRefreshLease_LiveHolderRespected(t *testing.T) {
	isolateLease(t)
	now := time.Now()
	// The holder is this (live) process, while pid names a dead acquirer:
	// only the holder decides.
	dead := testenv.ExitedPID(t)
	writeLease(t, RefreshLease{SchemaVersion: 1, ID: "old", PID: dead, StartedAt: now, HolderPID: os.Getpid(), HolderHost: localHost()})
	if ok, _ := AcquireRefreshLease("new", now); ok {
		t.Fatalf("AcquireRefreshLease took a live holder's lease")
	}
	if id, held := CurrentRefreshLeaseID(); !held || id != "old" {
		t.Errorf("CurrentRefreshLeaseID = %q, %v; want old, true", id, held)
	}
	// The TTL still frees it.
	if ok, err := AcquireRefreshLease("new", now.Add(refreshLeaseTTL)); !ok || err != nil {
		t.Fatalf("AcquireRefreshLease after the TTL = %v, %v", ok, err)
	}
}

// TestAcquireRefreshLease_UnknownHolderFallsBackToTTL: whenever liveness cannot
// be decided the lease is kept until the TTL, never stolen.
func TestAcquireRefreshLease_UnknownHolderFallsBackToTTL(t *testing.T) {
	now := time.Now()
	dead := testenv.ExitedPID(t)
	cases := map[string]RefreshLease{
		// What v0.0.7-v0.0.9 (and this version before holderPid) write; pid is
		// the acquirer, never a liveness signal.
		"no holder (older version)": {SchemaVersion: 1, ID: "old", PID: dead, StartedAt: now},
		"holder on another host":    {SchemaVersion: 1, ID: "old", PID: dead, StartedAt: now, HolderPID: dead, HolderHost: localHost() + "-elsewhere"},
		"holder host unknown":       {SchemaVersion: 1, ID: "old", PID: dead, StartedAt: now, HolderPID: dead},
	}
	for name, l := range cases {
		t.Run(name, func(t *testing.T) {
			isolateLease(t)
			writeLease(t, l)
			if ok, _ := AcquireRefreshLease("new", now.Add(time.Second)); ok {
				t.Fatalf("AcquireRefreshLease took the lease before the TTL")
			}
			if ok, err := AcquireRefreshLease("new", now.Add(refreshLeaseTTL)); !ok || err != nil {
				t.Fatalf("AcquireRefreshLease after the TTL = %v, %v", ok, err)
			}
		})
	}
	t.Run("probe cannot tell", func(t *testing.T) {
		isolateLease(t)
		// procinfo.Alive reports "alive" for permission errors and unsupported
		// platforms; the lease must then hold until the TTL.
		prev := processAlive
		processAlive = func(int) bool { return true }
		t.Cleanup(func() { processAlive = prev })
		writeLease(t, RefreshLease{SchemaVersion: 1, ID: "old", StartedAt: now, HolderPID: dead, HolderHost: localHost()})
		if ok, _ := AcquireRefreshLease("new", now.Add(time.Second)); ok {
			t.Fatalf("AcquireRefreshLease took the lease before the TTL")
		}
	})
}

func TestSetRefreshLeaseHolder(t *testing.T) {
	isolateLease(t)
	now := time.Now()
	if ok, err := AcquireRefreshLease("a", now); !ok || err != nil {
		t.Fatalf("AcquireRefreshLease = %v, %v", ok, err)
	}
	before := readLease(t)

	// Wrong id: untouched.
	if SetRefreshLeaseHolder("b", 0, 4242) {
		t.Errorf("SetRefreshLeaseHolder with another id reported success")
	}
	// from must match the recorded holder.
	if SetRefreshLeaseHolder("a", os.Getpid()+1, 4242) {
		t.Errorf("SetRefreshLeaseHolder with a stale from reported success")
	}
	if l := readLease(t); l.HolderPID != os.Getpid() {
		t.Fatalf("holderPid = %d after rejected hand-overs, want %d", l.HolderPID, os.Getpid())
	}
	if !SetRefreshLeaseHolder("a", os.Getpid(), 4242) {
		t.Fatalf("SetRefreshLeaseHolder(a, self, 4242) = false")
	}
	l := readLease(t)
	if l.HolderPID != 4242 || l.HolderHost != localHost() || l.ID != "a" || l.PID != before.PID || !l.StartedAt.Equal(before.StartedAt) {
		t.Fatalf("lease = %+v, want holderPid 4242 with id/pid/startedAt kept from %+v", l, before)
	}
	// A late render hand-over (from = render) cannot undo the worker's claim.
	if SetRefreshLeaseHolder("a", os.Getpid(), 5151) {
		t.Errorf("late hand-over overwrote a newer holder")
	}
	// Gone lease: nothing written.
	ReleaseRefreshLease("a")
	if SetRefreshLeaseHolder("a", 0, 4242) {
		t.Errorf("SetRefreshLeaseHolder recreated a released lease")
	}
	p, _ := leasePath()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("lease.json exists after release: %v", err)
	}
}

// TestAcquireRefreshLease_ReclaimRaceRestoresLiveLease: a render that judged a
// dead lease but renamed away a lease another render had just created puts it
// back instead of stealing it.
func TestAcquireRefreshLease_ReclaimRaceRestoresLiveLease(t *testing.T) {
	isolateLease(t)
	now := time.Now()
	dead := testenv.ExitedPID(t)
	writeLease(t, RefreshLease{SchemaVersion: 1, ID: "old", StartedAt: now, HolderPID: dead, HolderHost: localHost()})
	// Simulate the interleaving: the probe runs after this render read "old"
	// and, at that moment, another render reclaims it and writes its own.
	prev := processAlive
	t.Cleanup(func() { processAlive = prev })
	processAlive = func(pid int) bool {
		processAlive = prev
		writeLease(t, RefreshLease{SchemaVersion: 1, ID: "winner", StartedAt: now, HolderPID: os.Getpid(), HolderHost: localHost()})
		return false
	}
	if ok, _ := AcquireRefreshLease("loser", now); ok {
		t.Fatalf("AcquireRefreshLease stole the winner's fresh lease")
	}
	if l := readLease(t); l.ID != "winner" {
		t.Fatalf("lease id = %q, want winner restored", l.ID)
	}
}
