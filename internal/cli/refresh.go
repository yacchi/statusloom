package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
)

type refreshIdentity struct {
	SessionID  string `json:"session_id"`
	Transcript string `json:"transcript_path"`
}

var startRefreshProcess = func(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func maybeStartRefresh(raw []byte, now time.Time) {
	var id refreshIdentity
	if json.Unmarshal(raw, &id) != nil || id.SessionID == "" || id.Transcript == "" || !cache.RefreshDue(id.SessionID, now) {
		return
	}
	leaseID := randomLeaseID()
	ok, err := cache.AcquireRefreshLease(leaseID, now)
	if err != nil || !ok {
		return
	}
	if startRefreshProcess("refresh", "--once", "--session-id", id.SessionID, "--transcript", id.Transcript, "--lease-id", leaseID) != nil {
		cache.ReleaseRefreshLease(leaseID)
	}
}

func randomLeaseID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func runRefresh(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("refresh", flag.ContinueOnError)
	fs.SetOutput(stderr)
	once := fs.Bool("once", false, "refresh due sources once and exit")
	sessionID := fs.String("session-id", "", "Claude session id")
	transcript := fs.String("transcript", "", "Claude transcript JSONL path")
	leaseID := fs.String("lease-id", "", "internal lease handoff")
	if fs.Parse(args) != nil || !*once || *sessionID == "" || *transcript == "" || fs.NArg() != 0 {
		return 2
	}
	id := *leaseID
	if id == "" {
		id = randomLeaseID()
		ok, err := cache.AcquireRefreshLease(id, time.Now())
		if err != nil {
			return 1
		}
		if !ok {
			return 0
		}
	}
	defer cache.ReleaseRefreshLease(id)
	now := time.Now()
	m := cache.LoadRefreshManifest()
	s := m.Sessions[*sessionID]
	s.LastAttempt = now
	s.NextDueAt = now.Add(3 * time.Second)
	if err := cache.RefreshTranscript(*sessionID, *transcript, now); err == nil {
		s.LastSuccess = now
	}
	m.Sessions[*sessionID] = s
	_ = cache.StoreRefreshManifest(m)
	return 0
}
