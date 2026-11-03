package cache

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var schedNow = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

// writeManifest writes raw JSON at manifestPath() inside an isolated cache dir.
func writeManifest(t *testing.T, body string) {
	t.Helper()
	path, err := manifestPath()
	if err != nil {
		t.Fatalf("manifestPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func isolateScheduleDir(t *testing.T) {
	t.Helper()
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
}

func assertEmptyV2(t *testing.T, s RefreshSchedule) {
	t.Helper()
	if s.SchemaVersion != 2 {
		t.Fatalf("SchemaVersion = %d, want 2", s.SchemaVersion)
	}
	if s.Transcripts == nil || s.Usage == nil || s.Sources == nil {
		t.Fatalf("maps must be non-nil: %+v", s)
	}
	if len(s.Transcripts) != 0 || len(s.Usage) != 0 || len(s.Sources) != 0 {
		t.Fatalf("expected empty schedule, got %+v", s)
	}
}

func TestLoadRefreshScheduleFallsBackToEmptyV2(t *testing.T) {
	cases := map[string]func(t *testing.T){
		"missing file": func(t *testing.T) {},
		"corrupt json": func(t *testing.T) { writeManifest(t, "not json") },
		// A v1 manifest must not be partially decoded into the v2 shape.
		"v1 manifest": func(t *testing.T) {
			writeManifest(t, `{"schemaVersion":1,"sessions":{"s1":{"nextDueAt":"2026-11-03T12:00:00Z"}},`+
				`"accountUsage":{"nextDueAt":"2026-11-03T12:05:00Z","failures":3}}`)
		},
		"unknown version": func(t *testing.T) { writeManifest(t, `{"schemaVersion":3}`) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			isolateScheduleDir(t)
			setup(t)
			assertEmptyV2(t, LoadRefreshSchedule())
		})
	}
}

func TestLoadRefreshScheduleNormalizesNullAndOmittedMaps(t *testing.T) {
	isolateScheduleDir(t)
	writeManifest(t, `{"schemaVersion":2,"usage":{"default":{"nextDueAt":"2026-11-03T12:05:00Z","failures":1}},`+
		`"transcripts":null}`)

	s := LoadRefreshSchedule()
	if s.Transcripts == nil || s.Sources == nil {
		t.Fatalf("null/omitted maps must be replaced with empty maps: %+v", s)
	}
	if got := s.Usage["default"].Failures; got != 1 {
		t.Fatalf("Usage[default].Failures = %d, want 1", got)
	}
}

func TestStoreRefreshScheduleForcesVersionAndRoundTrips(t *testing.T) {
	isolateScheduleDir(t)
	in := RefreshSchedule{
		SchemaVersion: 1,
		Sources: map[string]WorkSchedule{
			"weather": {DefHash: "abc", NextDueAt: schedNow},
		},
	}
	if err := StoreRefreshSchedule(in); err != nil {
		t.Fatalf("StoreRefreshSchedule: %v", err)
	}
	got := LoadRefreshSchedule()
	if got.SchemaVersion != 2 {
		t.Fatalf("SchemaVersion = %d, want 2", got.SchemaVersion)
	}
	w, ok := got.Sources["weather"]
	if !ok || w.DefHash != "abc" || !w.NextDueAt.Equal(schedNow) {
		t.Fatalf("Sources[weather] = %+v (ok=%v), want DefHash abc due at %v", w, ok, schedNow)
	}
}

func TestBackoff(t *testing.T) {
	cases := []struct {
		name     string
		base     time.Duration
		failures int
		max      time.Duration
		want     time.Duration
	}{
		{"no failures", 5 * time.Minute, 0, time.Hour, 5 * time.Minute},
		{"negative failures", 5 * time.Minute, -1, time.Hour, 5 * time.Minute},
		{"one failure doubles", 5 * time.Minute, 1, time.Hour, 10 * time.Minute},
		{"three failures", 5 * time.Minute, 3, time.Hour, 40 * time.Minute},
		{"capped at max", 5 * time.Minute, 4, time.Hour, time.Hour},
		{"large count caps", 5 * time.Minute, 1000, time.Hour, time.Hour},
		{"huge max never overflows", time.Hour, 70, math.MaxInt64, math.MaxInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Backoff(tc.base, tc.failures, tc.max); got != tc.want {
				t.Fatalf("Backoff(%v, %d, %v) = %v, want %v", tc.base, tc.failures, tc.max, got, tc.want)
			}
		})
	}
}

func TestRecordFailureCountsAndKeepsLastSuccess(t *testing.T) {
	t0 := schedNow.Add(-time.Hour)
	w := WorkSchedule{LastSuccess: t0, DefHash: "h"}

	w = w.Record(schedNow, false, 30*time.Second, 10*time.Minute)
	if w.Failures != 1 || !w.NextDueAt.Equal(schedNow.Add(60*time.Second)) {
		t.Fatalf("first failure = %+v, want Failures 1 due +60s (base*2)", w)
	}
	if !w.LastSuccess.Equal(t0) || w.DefHash != "h" {
		t.Fatalf("failure must keep LastSuccess and DefHash: %+v", w)
	}

	w = w.Record(schedNow, false, 30*time.Second, 10*time.Minute)
	if w.Failures != 2 || !w.NextDueAt.Equal(schedNow.Add(120*time.Second)) {
		t.Fatalf("second failure = %+v, want Failures 2 due +120s", w)
	}
}

func TestRecordSuccessResetsFailuresKeepsDefHash(t *testing.T) {
	w := WorkSchedule{Failures: 2, DefHash: "h"}
	w = w.Record(schedNow, true, 30*time.Second, 10*time.Minute)
	if w.Failures != 0 || !w.LastSuccess.Equal(schedNow) || !w.NextDueAt.Equal(schedNow.Add(30*time.Second)) {
		t.Fatalf("success = %+v, want Failures 0, LastSuccess now, due +30s", w)
	}
	if w.DefHash != "h" {
		t.Fatalf("DefHash = %q, want h", w.DefHash)
	}
}

func TestSkipUsageIsNotAFailure(t *testing.T) {
	t0 := schedNow.Add(-time.Hour)
	w := WorkSchedule{Failures: 3, LastSuccess: t0}.SkipUsage(schedNow)
	if w.Failures != 3 || !w.LastSuccess.Equal(t0) {
		t.Fatalf("SkipUsage changed failure bookkeeping: %+v", w)
	}
	if !w.LastAttempt.Equal(schedNow) || !w.NextDueAt.Equal(schedNow.Add(UsageRefreshInterval)) {
		t.Fatalf("SkipUsage = %+v, want LastAttempt now, due in UsageRefreshInterval", w)
	}
}

func TestTranscriptIntervalFloorsAndDefaults(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		0:                DefaultTranscriptInterval,
		-time.Second:     DefaultTranscriptInterval,
		time.Second:      MinTranscriptInterval,
		5 * time.Second:  5 * time.Second,
		10 * time.Second: 10 * time.Second,
	}
	for in, want := range cases {
		if got := TranscriptInterval(in); got != want {
			t.Fatalf("TranscriptInterval(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestDueTreatsZeroAsDue(t *testing.T) {
	if !(WorkSchedule{}).Due(schedNow) {
		t.Fatal("zero schedule must be due")
	}
	if !(WorkSchedule{NextDueAt: schedNow}).Due(schedNow) {
		t.Fatal("NextDueAt == now must be due")
	}
	if (WorkSchedule{NextDueAt: schedNow.Add(time.Second)}).Due(schedNow) {
		t.Fatal("NextDueAt in the future must not be due")
	}
}

func TestPruneDropsOnlyOldTranscripts(t *testing.T) {
	s := RefreshSchedule{
		Transcripts: map[string]WorkSchedule{
			"recent": {LastAttempt: schedNow.Add(-time.Hour)},
			"exact":  {LastAttempt: schedNow.Add(-TranscriptRetention)},
			"old":    {LastAttempt: schedNow.Add(-TranscriptRetention - time.Second)},
			"never":  {},
		},
		Usage:   map[string]WorkSchedule{"acct": {LastAttempt: schedNow.Add(-240 * time.Hour)}},
		Sources: map[string]WorkSchedule{"weather": {LastAttempt: schedNow.Add(-240 * time.Hour)}},
	}
	s.Prune(schedNow)

	for _, id := range []string{"recent", "exact", "never"} {
		if _, ok := s.Transcripts[id]; !ok {
			t.Fatalf("Prune dropped %q, want kept", id)
		}
	}
	if _, ok := s.Transcripts["old"]; ok {
		t.Fatal("Prune kept an entry idle longer than TranscriptRetention")
	}
	if _, ok := s.Usage["acct"]; !ok {
		t.Fatal("Prune must not touch Usage")
	}
	if _, ok := s.Sources["weather"]; !ok {
		t.Fatal("Prune must not touch Sources")
	}
}

func TestPruneNilMapIsSafe(t *testing.T) {
	var s RefreshSchedule
	s.Prune(schedNow)
}
