package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

const transcriptRefreshTTL = 3 * time.Second
const refreshLeaseTTL = 2 * time.Minute

type RefreshManifest struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Sessions      map[string]RefreshSession `json:"sessions"`
}

type RefreshSession struct {
	NextDueAt   time.Time `json:"nextDueAt"`
	LastAttempt time.Time `json:"lastAttempt,omitempty"`
	LastSuccess time.Time `json:"lastSuccess,omitempty"`
}

type RefreshLease struct {
	SchemaVersion int       `json:"schemaVersion"`
	ID            string    `json:"id"`
	PID           int       `json:"pid"`
	StartedAt     time.Time `json:"startedAt"`
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
func manifestPath() (string, error) {
	d, err := refreshDir()
	return filepath.Join(d, "manifest.json"), err
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

func LoadRefreshManifest() RefreshManifest {
	p, err := manifestPath()
	if err != nil {
		return RefreshManifest{SchemaVersion: 1, Sessions: map[string]RefreshSession{}}
	}
	var m RefreshManifest
	if ok, err := ReadJSON(p, &m); err != nil || !ok {
		m = RefreshManifest{SchemaVersion: 1}
	}
	if m.Sessions == nil {
		m.Sessions = map[string]RefreshSession{}
	}
	return m
}

func RefreshDue(sessionID string, now time.Time) bool {
	m := LoadRefreshManifest()
	s, ok := m.Sessions[sessionID]
	return !ok || !now.Before(s.NextDueAt)
}

func StoreRefreshManifest(m RefreshManifest) error {
	p, err := manifestPath()
	if err != nil {
		return err
	}
	m.SchemaVersion = 1
	return WriteJSONAtomic(p, m)
}

// AcquireRefreshLease elects one short-lived worker without blocking.
func AcquireRefreshLease(id string, now time.Time) (bool, error) {
	p, err := leasePath()
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return false, err
	}
	b := []byte(fmt.Sprintf("{\"schemaVersion\":1,\"id\":%q,\"pid\":%d,\"startedAt\":%q}\n", id, os.Getpid(), now.Format(time.RFC3339Nano)))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, werr := f.Write(b)
		cerr := f.Close()
		if werr != nil {
			_ = os.Remove(p)
			return false, werr
		}
		return true, cerr
	}
	if !errors.Is(err, os.ErrExist) {
		return false, err
	}
	var old RefreshLease
	if ok, _ := ReadJSON(p, &old); ok && now.Sub(old.StartedAt) < refreshLeaseTTL {
		return false, nil
	}
	stale := p + ".stale." + id
	if os.Rename(p, stale) != nil {
		return false, nil
	}
	defer os.Remove(stale)
	return AcquireRefreshLease(id, now)
}

func ReleaseRefreshLease(id string) {
	p, err := leasePath()
	if err != nil {
		return
	}
	var l RefreshLease
	if ok, _ := ReadJSON(p, &l); ok && l.ID == id {
		_ = os.Remove(p)
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
