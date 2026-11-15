package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yacchi/statusloom/internal/atomicfile"
	"github.com/yacchi/statusloom/internal/procinfo"
	"github.com/yacchi/statusloom/internal/schema"
)

const transcriptRefreshTTL = 3 * time.Second
const refreshLeaseTTL = 2 * time.Minute

// RefreshLease is <cache>/refresh/lease.json, shared by every installed
// version (v0.0.7 onward reads id and startedAt). Fields are only ever added:
//
//   - pid keeps its original meaning, the process that acquired the lease (a
//     render, which exits right after spawning the worker), so it is never
//     used for liveness;
//   - holderPid/holderHost (added later; absent in leases older versions
//     write) name the process that currently holds the lease, on which host.
//     The acquirer is the holder at first; each spawner hands the lease to
//     the process it started (SetRefreshLeaseHolder) before it can exit, and
//     the worker re-asserts itself on start.
//
// A lease is live while it is younger than refreshLeaseTTL and its holder is
// not provably dead (leaseLive).
type RefreshLease struct {
	SchemaVersion int       `json:"schemaVersion"`
	ID            string    `json:"id"`
	PID           int       `json:"pid"`
	StartedAt     time.Time `json:"startedAt"`
	HolderPID     int       `json:"holderPid,omitempty"`
	HolderHost    string    `json:"holderHost,omitempty"`
}

// processAlive is the holder liveness probe; tests replace it.
var processAlive = procinfo.Alive

// localHost is this machine's host name ("" when unknown). The holder pid is
// only checked on the host that recorded it: a cache directory shared between
// machines or containers (network home, mounted volume) would otherwise see
// every foreign holder as dead.
var localHost = sync.OnceValue(func() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
})

// leaseLive reports whether l still excludes other workers at now: younger
// than refreshLeaseTTL and its holder not provably dead. Every doubt keeps the
// lease (TTL fallback): no holder recorded (an older version's lease), a
// holder on another or unknown host, and processAlive's own "alive" default
// for permission errors and unsupported platforms. A recycled pid can only
// make a dead holder look alive, which also just falls back to the TTL.
func leaseLive(l RefreshLease, now time.Time) bool {
	if now.Sub(l.StartedAt) >= refreshLeaseTTL {
		return false
	}
	if l.HolderPID <= 0 || l.HolderHost == "" || l.HolderHost != localHost() {
		return true
	}
	return processAlive(l.HolderPID)
}

type TranscriptCursor struct {
	Path      string                     `json:"path"`
	Offset    int64                      `json:"offset"`
	Size      int64                      `json:"size"`
	StartedAt time.Time                  `json:"startedAt,omitempty"`
	Seen      []string                   `json:"seen,omitempty"`
	Usage     map[string]TranscriptUsage `json:"usage,omitempty"`
}

type TranscriptUsage struct {
	Input         int `json:"input"`
	Output        int `json:"output"`
	CacheCreation int `json:"cacheCreation"`
	CacheRead     int `json:"cacheRead"`
}

type TranscriptEnvelope struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Source        string                  `json:"source"`
	SessionID     string                  `json:"sessionId"`
	ObservedAt    time.Time               `json:"observedAt"`
	ExpiresAt     time.Time               `json:"expiresAt"`
	StaleUntil    time.Time               `json:"staleUntil"`
	Cursor        TranscriptCursor        `json:"cursor"`
	Value         schema.SessionAnalytics `json:"value"`
}

func refreshDir() (string, error) { d, err := Dir(); return filepath.Join(d, "refresh"), err }

// schedulePath is <cache>/refresh/schedule.json. It is deliberately not manifest.json:
// v0.0.7 owns manifest.json with a different (schemaVersion 1) shape, and every installed
// version shares this cache directory.
func schedulePath() (string, error) {
	d, err := refreshDir()
	return filepath.Join(d, "schedule.json"), err
}
func leasePath() (string, error) { d, err := refreshDir(); return filepath.Join(d, "lease.json"), err }
func transcriptPath(sessionID string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	s := sha256.Sum256([]byte(sessionID))
	return filepath.Join(d, "sources", "transcript-"+hex.EncodeToString(s[:8])+".json"), nil
}

// AcquireRefreshLease elects one short-lived worker without blocking. The
// lease is free when lease.json is absent, expired, or names a holder that is
// provably dead (leaseLive): a worker or relay killed mid-run (Claude Code
// killing a cancelled statusline's process tree) then blocks the next
// refresh for one poll, not for the whole TTL. The calling process is
// recorded as the holder until it hands the lease on (SetRefreshLeaseHolder).
func AcquireRefreshLease(id string, now time.Time) (bool, error) {
	p, err := leasePath()
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return false, err
	}
	b, err := json.Marshal(RefreshLease{
		SchemaVersion: 1,
		ID:            id,
		PID:           os.Getpid(),
		StartedAt:     now,
		HolderPID:     os.Getpid(),
		HolderHost:    localHost(),
	})
	if err != nil {
		return false, err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, werr := f.Write(b)
		cerr := f.Close()
		if werr != nil {
			_ = atomicfile.Remove(p)
			return false, werr
		}
		return true, cerr
	}
	if !errors.Is(err, os.ErrExist) {
		return false, err
	}
	var old RefreshLease
	if ok, _ := ReadJSON(p, &old); ok && leaseLive(old, now) {
		return false, nil
	}
	stale := p + ".stale." + id
	// The lease is shared (every render reads it), so the reclaim goes through
	// atomicfile.Rename, which rides out a reader's momentary open on Windows.
	// The stale name is unique to this id and never read by anyone else, so its
	// cleanup is a plain os.Remove. Two renders reclaiming at once: only one
	// rename moves the file; the other fails here and reports the lease held.
	if atomicfile.Rename(p, stale) != nil {
		return false, nil
	}
	defer os.Remove(stale)
	// The other interleaving: a second render read the same dead lease, but
	// the first one's reclaim and new lease landed before this rename, which
	// then moved the first render's live lease aside. Put it back (only if no
	// lease has appeared since) and report the lease held. Dead-holder
	// takeovers make reclaims frequent, so this check matters more than it did
	// for TTL expiry alone.
	var moved RefreshLease
	if ok, _ := ReadJSON(stale, &moved); ok && moved.ID != old.ID {
		if raw, err := os.ReadFile(stale); err == nil {
			if f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err == nil {
				_, _ = f.Write(raw)
				_ = f.Close()
			}
		}
		return false, nil
	}
	return AcquireRefreshLease(id, now)
}

// SetRefreshLeaseHolder records pid (on this host) as the holder of lease id
// and reports whether it did. It rewrites lease.json atomically, keeping id,
// pid and startedAt (the TTL still counts from the acquisition). It does
// nothing when the lease is gone or has another id (released or taken over),
// or, when from > 0, when the recorded holder is not from.
//
// The hand-over chain (one lease id throughout) and its races:
//
//   - render: acquires (holder = render), starts the child, then hands over
//     with from = itself, before it can exit, so the lease never names an
//     exited render while its child runs. The from-check keeps the render's
//     write from undoing a later hand-over (relay or worker already
//     recorded), except for a lost update inside the microseconds between its
//     read and its rename, which the worker's own start-up claim repairs.
//   - relay: starts the worker, hands over to it with from = 0,
//     then exits. It does not wait for the render's hand-over, so the two may
//     land in either order; with the render's from-check the result is the
//     worker either way.
//   - worker: re-asserts itself (from = 0) on start. Every writer but the
//     render records the worker's pid, so those writes cannot conflict.
//
// A running worker is not the recorded holder only when its spawner was
// killed between the start and the hand-over (microseconds; the relay,
// whose tree Claude Code may kill) and before the worker's own claim:
// the lease then names a dead process and a concurrent render may take it over
// and start a second worker. That costs duplicate work only (every cache write
// is atomic, last-writer-wins), the same as a TTL takeover of a slow worker.
// Should the orphaned worker's claim land between such a takeover's id check
// and its write, it can overwrite the new lease; the lease then names that
// live worker and frees itself once it exits. A relay killed before it
// started the worker leaves a dead holder and no worker: exactly the case the
// takeover frees at once instead of after refreshLeaseTTL.
func SetRefreshLeaseHolder(id string, from, pid int) bool {
	p, err := leasePath()
	if err != nil || pid <= 0 {
		return false
	}
	var l RefreshLease
	if ok, _ := ReadJSON(p, &l); !ok || l.ID != id {
		return false
	}
	if from > 0 && l.HolderPID != from {
		return false
	}
	if l.HolderPID == pid && l.HolderHost == localHost() {
		return true
	}
	l.HolderPID, l.HolderHost = pid, localHost()
	return WriteJSONAtomicNoSync(p, l) == nil
}

func ReleaseRefreshLease(id string) {
	p, err := leasePath()
	if err != nil {
		return
	}
	var l RefreshLease
	if ok, _ := ReadJSON(p, &l); ok && l.ID == id {
		_ = atomicfile.Remove(p)
	}
}

func LoadTranscriptAnalytics(sessionID string, now time.Time) (*schema.SessionAnalytics, bool) {
	p, err := transcriptPath(sessionID)
	if err != nil {
		return nil, false
	}
	var e TranscriptEnvelope
	if ok, err := ReadJSON(p, &e); err != nil || !ok || now.After(e.StaleUntil) {
		return nil, false
	}
	v := e.Value
	return &v, now.After(e.ExpiresAt)
}

// CurrentRefreshLeaseID returns the id recorded in the refresh lease file and
// true while that lease is live (leaseLive), or "" and false when there is no
// readable lease or it could be taken over (expired, or its holder is dead).
// It only reads; the render's refresh wait uses it to notice when the worker
// it watches is gone, so a killed worker counts as gone at once.
func CurrentRefreshLeaseID() (string, bool) {
	p, err := leasePath()
	if err != nil {
		return "", false
	}
	var l RefreshLease
	if ok, _ := ReadJSON(p, &l); !ok || l.ID == "" || !leaseLive(l, time.Now()) {
		return "", false
	}
	return l.ID, true
}
