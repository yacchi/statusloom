package cache

import (
	"time"
)

// RefreshSchedule is persisted at schedulePath() (<cache>/refresh/schedule.json) with
// SchemaVersion 2. Only the refresh worker holding the refresh lease writes it; the render
// path only reads it. manifest.json belongs to v0.0.7 and is never touched here.
type RefreshSchedule struct {
	SchemaVersion int                     `json:"schemaVersion"` // 2
	Transcripts   map[string]WorkSchedule `json:"transcripts"`   // key: Claude session id
	Usage         map[string]WorkSchedule `json:"usage"`         // key: account cache key
	Sources       map[string]WorkSchedule `json:"sources"`       // key: external source id
}

// WorkSchedule is one work item's schedule.
type WorkSchedule struct {
	NextDueAt   time.Time `json:"nextDueAt"`
	LastAttempt time.Time `json:"lastAttempt,omitempty"`
	LastSuccess time.Time `json:"lastSuccess,omitempty"`
	Failures    int       `json:"failures,omitempty"` // consecutive failures
	DefHash     string    `json:"defHash,omitempty"`  // sources only: Definition.Hash() of the last attempt
}

const (
	DefaultTranscriptInterval = 30 * time.Second // transcript interval when refreshInterval is unknown
	MinTranscriptInterval     = 5 * time.Second  // floor applied to a known refreshInterval
	TranscriptBackoffMax      = 10 * time.Minute
	TranscriptRetention       = 24 * time.Hour // Prune drops transcript entries idle this long
)

// scheduleSchemaVersion is the RefreshSchedule layout written by StoreRefreshSchedule.
// Any other version found at schedulePath() is ignored.
const scheduleSchemaVersion = 2

// newRefreshSchedule returns an empty schedule with all maps non-nil.
func newRefreshSchedule() RefreshSchedule {
	return RefreshSchedule{
		SchemaVersion: scheduleSchemaVersion,
		Transcripts:   map[string]WorkSchedule{},
		Usage:         map[string]WorkSchedule{},
		Sources:       map[string]WorkSchedule{},
	}
}

// LoadRefreshSchedule never fails: a missing/unreadable file or a SchemaVersion other
// than 2 yields an empty schedule (SchemaVersion 2, all three maps non-nil).
func LoadRefreshSchedule() RefreshSchedule {
	path, err := schedulePath()
	if err != nil {
		return newRefreshSchedule()
	}
	var s RefreshSchedule
	ok, err := ReadJSON(path, &s)
	if err != nil || !ok || s.SchemaVersion != scheduleSchemaVersion {
		return newRefreshSchedule()
	}
	if s.Transcripts == nil {
		s.Transcripts = map[string]WorkSchedule{}
	}
	if s.Usage == nil {
		s.Usage = map[string]WorkSchedule{}
	}
	if s.Sources == nil {
		s.Sources = map[string]WorkSchedule{}
	}
	return s
}

// StoreRefreshSchedule writes s with SchemaVersion forced to 2 (WriteJSONAtomic).
func StoreRefreshSchedule(s RefreshSchedule) error {
	path, err := schedulePath()
	if err != nil {
		return err
	}
	s.SchemaVersion = scheduleSchemaVersion
	return WriteJSONAtomic(path, s)
}

// Due: true when NextDueAt is zero or !now.Before(NextDueAt). A missing map entry is the
// zero WorkSchedule, hence due.
func (w WorkSchedule) Due(now time.Time) bool {
	return w.NextDueAt.IsZero() || !now.Before(w.NextDueAt)
}

// Backoff returns base << failures capped at max; failures <= 0 -> base. Never overflows
// (large failure counts return max).
func Backoff(base time.Duration, failures int, max time.Duration) time.Duration {
	if failures <= 0 {
		return base
	}
	d := base
	for i := 0; i < failures && i < 64; i++ {
		// Double only while the result stays within max, so the value can never overflow.
		if d > max/2 {
			return max
		}
		d *= 2
	}
	return min(d, max)
}

// Record returns w after an attempt at now. Success: Failures = 0, LastSuccess = now,
// NextDueAt = now + base. Failure: Failures+1, NextDueAt = now + Backoff(base, Failures, max).
// Both set LastAttempt = now; DefHash is left unchanged.
func (w WorkSchedule) Record(now time.Time, success bool, base, max time.Duration) WorkSchedule {
	w.LastAttempt = now
	if success {
		w.Failures = 0
		w.LastSuccess = now
		w.NextDueAt = now.Add(base)
		return w
	}
	w.Failures++
	w.NextDueAt = now.Add(Backoff(base, w.Failures, max))
	return w
}

// TranscriptInterval is the transcript refresh interval: refreshInterval when > 0
// (floored at MinTranscriptInterval), else DefaultTranscriptInterval. The fixed 3 s of the
// old worker is gone.
func TranscriptInterval(refreshInterval time.Duration) time.Duration {
	if refreshInterval <= 0 {
		return DefaultTranscriptInterval
	}
	if refreshInterval < MinTranscriptInterval {
		return MinTranscriptInterval
	}
	return refreshInterval
}

// Prune deletes Transcripts entries whose LastAttempt is older than now-TranscriptRetention
// (entries with a zero LastAttempt are kept). The worker calls it before storing.
func (s *RefreshSchedule) Prune(now time.Time) {
	cutoff := now.Add(-TranscriptRetention)
	for id, w := range s.Transcripts {
		if !w.LastAttempt.IsZero() && w.LastAttempt.Before(cutoff) {
			delete(s.Transcripts, id)
		}
	}
}

// RecordTranscript = Record(now, success, TranscriptInterval(refreshInterval), TranscriptBackoffMax).
func (w WorkSchedule) RecordTranscript(now time.Time, success bool, refreshInterval time.Duration) WorkSchedule {
	return w.Record(now, success, TranscriptInterval(refreshInterval), TranscriptBackoffMax)
}

// RecordUsage = Record(now, success, UsageRefreshInterval, usageBackoffMax) (the existing
// 5 min / 60 min constants of account_usage.go, which stay unexported).
func (w WorkSchedule) RecordUsage(now time.Time, success bool) WorkSchedule {
	return w.Record(now, success, UsageRefreshInterval, usageBackoffMax)
}

// SkipUsage records a usage poll that found no credential - NOT a failure:
// LastAttempt = now, NextDueAt = now + UsageRefreshInterval, Failures and LastSuccess unchanged.
func (w WorkSchedule) SkipUsage(now time.Time) WorkSchedule {
	w.LastAttempt = now
	w.NextDueAt = now.Add(UsageRefreshInterval)
	return w
}
