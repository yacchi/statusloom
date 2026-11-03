package sources

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
)

var (
	refreshNow = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	refreshRI  = 45 * time.Second
)

type fetchCall struct {
	id   string
	etag string
}

// setupRefresh isolates config and cache, then saves the given definitions.
func setupRefresh(t *testing.T, defs map[string]Definition) {
	t.Helper()
	t.Setenv("STATUSLOOM_CONFIG", t.TempDir())
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	if err := Save(File{Sources: defs}); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func httpDef(id string) Definition {
	return Definition{ID: id, Kind: KindHTTP, URL: "http://127.0.0.1:1/" + id + ".json"}
}

func commandDef(id string) Definition {
	return Definition{ID: id, Kind: KindCommand, Argv: []string{"true"}, TTLSeconds: 30}
}

func fileDef(t *testing.T, id string) Definition {
	t.Helper()
	return Definition{ID: id, Kind: KindFile, Path: filepath.Join(t.TempDir(), id+".json")}
}

// recordingFetch returns a Fetcher that records each call and returns the fixed result.
func recordingFetch(calls *[]fetchCall, res FetchResult, err error) Fetcher {
	return func(_ context.Context, def Definition, etag string) (FetchResult, error) {
		*calls = append(*calls, fetchCall{id: def.ID, etag: etag})
		return res, err
	}
}

// jsonBodyIs compares a stored body semantically: the envelope file is indented, so
// the raw bytes differ from the fetched ones.
func jsonBodyIs(got []byte, want string) bool {
	var g, w any
	if json.Unmarshal(got, &g) != nil || json.Unmarshal([]byte(want), &w) != nil {
		return false
	}
	return reflect.DeepEqual(g, w)
}

func sameWork(t *testing.T, got, want cache.WorkSchedule) {
	t.Helper()
	if !got.NextDueAt.Equal(want.NextDueAt) ||
		!got.LastAttempt.Equal(want.LastAttempt) ||
		!got.LastSuccess.Equal(want.LastSuccess) ||
		got.Failures != want.Failures ||
		got.DefHash != want.DefHash {
		t.Fatalf("schedule = %+v, want %+v", got, want)
	}
}

func TestDueIDsSelectsDefinedCommandAndHTTPOnly(t *testing.T) {
	setupRefresh(t, map[string]Definition{
		"a": httpDef("a"),
		"b": commandDef("b"),
		"f": fileDef(t, "f"),
	})
	f, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := DueIDs(f, []string{"b", "a", "a", "f", "nope"}, cache.RefreshSchedule{}, refreshNow)
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DueIDs = %v, want %v", got, want)
	}
}

func TestDueIDsScheduleBoundary(t *testing.T) {
	setupRefresh(t, map[string]Definition{"a": httpDef("a")})
	f, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a := f.Sources["a"]

	cases := []struct {
		name  string
		entry cache.WorkSchedule
		due   bool
	}{
		{"future entry is not due", cache.WorkSchedule{NextDueAt: refreshNow.Add(time.Minute), DefHash: a.Hash()}, false},
		{"definition changed is due", cache.WorkSchedule{NextDueAt: refreshNow.Add(time.Minute), DefHash: "old"}, true},
		{"now equal to NextDueAt is due", cache.WorkSchedule{NextDueAt: refreshNow, DefHash: a.Hash()}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := cache.RefreshSchedule{Sources: map[string]cache.WorkSchedule{"a": tc.entry}}
			got := len(DueIDs(f, []string{"a"}, s, refreshNow)) == 1
			if got != tc.due {
				t.Fatalf("due = %v, want %v", got, tc.due)
			}
		})
	}
}

func TestRefreshSuccessStoresEnvelopeAndSchedule(t *testing.T) {
	setupRefresh(t, map[string]Definition{"a": httpDef("a")})
	var calls []fetchCall
	fetch := recordingFetch(&calls, FetchResult{Body: []byte(`{"v":1}`), ETag: "e1"}, nil)

	Refresh(context.Background(), []string{"a"}, refreshNow, refreshRI, fetch)

	if len(calls) != 1 || calls[0].etag != "" {
		t.Fatalf("calls = %+v, want one call with empty etag", calls)
	}
	a := httpDef("a")
	env, ok := LoadEnvelope("a")
	if !ok || !env.OK || !jsonBodyIs(env.Body, `{"v":1}`) || env.ETag != "e1" ||
		!env.FetchedAt.Equal(refreshNow) || !env.AttemptedAt.Equal(refreshNow) || env.DefHash != a.Hash() {
		t.Fatalf("envelope = %+v (ok=%v)", env, ok)
	}
	s := cache.LoadRefreshSchedule()
	sameWork(t, s.Sources["a"], cache.WorkSchedule{
		NextDueAt:   refreshNow.Add(refreshRI),
		LastAttempt: refreshNow,
		LastSuccess: refreshNow,
		DefHash:     a.Hash(),
	})
}

func TestRefreshNotModifiedKeepsBodyAndMovesFetchedAt(t *testing.T) {
	a := httpDef("a")
	setupRefresh(t, map[string]Definition{"a": a})
	prevFetched := refreshNow.Add(-10 * time.Minute)
	if err := StoreEnvelope(Envelope{
		ID: "a", DefHash: a.Hash(), OK: true, Body: []byte(`{"v":1}`), ETag: "e1", FetchedAt: prevFetched,
	}); err != nil {
		t.Fatalf("StoreEnvelope: %v", err)
	}

	var calls []fetchCall
	Refresh(context.Background(), []string{"a"}, refreshNow, refreshRI, recordingFetch(&calls, FetchResult{NotModified: true}, nil))

	if len(calls) != 1 || calls[0].etag != "e1" {
		t.Fatalf("calls = %+v, want one call with etag e1", calls)
	}
	env, _ := LoadEnvelope("a")
	if !env.OK || !jsonBodyIs(env.Body, `{"v":1}`) || env.ETag != "e1" || !env.FetchedAt.Equal(refreshNow) {
		t.Fatalf("envelope = %+v, want body and etag kept with FetchedAt = now", env)
	}
}

func TestRefreshChangedDefinitionDropsOldETagAnd304IsFailure(t *testing.T) {
	a := httpDef("a")
	setupRefresh(t, map[string]Definition{"a": a})
	if err := StoreEnvelope(Envelope{
		ID: "a", DefHash: "old", OK: true, Body: []byte(`{"v":1}`), ETag: "e1", FetchedAt: refreshNow.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("StoreEnvelope: %v", err)
	}

	var calls []fetchCall
	Refresh(context.Background(), []string{"a"}, refreshNow, refreshRI, recordingFetch(&calls, FetchResult{NotModified: true}, nil))

	if len(calls) != 1 || calls[0].etag != "" {
		t.Fatalf("calls = %+v, want one call with empty etag", calls)
	}
	env, _ := LoadEnvelope("a")
	if env.OK || env.Error != "HTTP 304 without a cached body" || len(env.Body) != 0 || env.ETag != "" || !env.FetchedAt.IsZero() {
		t.Fatalf("envelope = %+v, want 304 failure with no body, etag or FetchedAt", env)
	}
}

func TestRefreshFailureDropsValueKeepsFetchedAtWhenReusable(t *testing.T) {
	a := httpDef("a")
	setupRefresh(t, map[string]Definition{"a": a})
	prevFetched := refreshNow.Add(-10 * time.Minute)
	if err := StoreEnvelope(Envelope{
		ID: "a", DefHash: a.Hash(), OK: true, Body: []byte(`{"v":1}`), ETag: "e1", FetchedAt: prevFetched,
	}); err != nil {
		t.Fatalf("StoreEnvelope: %v", err)
	}

	var calls []fetchCall
	Refresh(context.Background(), []string{"a"}, refreshNow, refreshRI, recordingFetch(&calls, FetchResult{}, errors.New("HTTP 500")))

	env, _ := LoadEnvelope("a")
	if env.OK || env.Error != "HTTP 500" || len(env.Body) != 0 || env.ETag != "" {
		t.Fatalf("envelope = %+v, want failure with body and etag dropped", env)
	}
	if !env.FetchedAt.Equal(prevFetched) || !env.AttemptedAt.Equal(refreshNow) {
		t.Fatalf("FetchedAt = %v, AttemptedAt = %v; want %v and %v", env.FetchedAt, env.AttemptedAt, prevFetched, refreshNow)
	}
}

func TestRefreshOlderDefinitionFailuresDoNotCountTowardBackoff(t *testing.T) {
	a := httpDef("a")
	setupRefresh(t, map[string]Definition{"a": a})
	seeded := cache.WorkSchedule{Failures: 5, DefHash: "old", NextDueAt: refreshNow.Add(time.Hour)}
	if err := cache.StoreRefreshSchedule(cache.RefreshSchedule{Sources: map[string]cache.WorkSchedule{"a": seeded}}); err != nil {
		t.Fatalf("StoreRefreshSchedule: %v", err)
	}

	var calls []fetchCall
	Refresh(context.Background(), []string{"a"}, refreshNow, refreshRI, recordingFetch(&calls, FetchResult{}, errors.New("HTTP 500")))

	want := cache.WorkSchedule{}.Record(refreshNow, false, refreshRI, MaxBackoff)
	want.DefHash = a.Hash()
	sameWork(t, cache.LoadRefreshSchedule().Sources["a"], want)
}

func TestRefreshBackoffCappedAtMaxBackoff(t *testing.T) {
	a := httpDef("a")
	setupRefresh(t, map[string]Definition{"a": a})
	seeded := cache.WorkSchedule{Failures: 20, DefHash: a.Hash(), NextDueAt: refreshNow.Add(-time.Second)}
	if err := cache.StoreRefreshSchedule(cache.RefreshSchedule{Sources: map[string]cache.WorkSchedule{"a": seeded}}); err != nil {
		t.Fatalf("StoreRefreshSchedule: %v", err)
	}

	var calls []fetchCall
	Refresh(context.Background(), []string{"a"}, refreshNow, refreshRI, recordingFetch(&calls, FetchResult{}, errors.New("HTTP 500")))

	got := cache.LoadRefreshSchedule().Sources["a"]
	if !got.NextDueAt.Equal(refreshNow.Add(MaxBackoff)) {
		t.Fatalf("NextDueAt = %v, want %v", got.NextDueAt, refreshNow.Add(MaxBackoff))
	}
}

func TestRefreshFetchesOnlyDueDedupedDefinedIDs(t *testing.T) {
	a := httpDef("a")
	b := commandDef("b")
	setupRefresh(t, map[string]Definition{
		"a": a,
		"b": b,
		"f": fileDef(t, "f"),
	})
	seeded := cache.RefreshSchedule{Sources: map[string]cache.WorkSchedule{
		"b": {NextDueAt: refreshNow.Add(time.Hour), DefHash: b.Hash()},
	}}
	if err := cache.StoreRefreshSchedule(seeded); err != nil {
		t.Fatalf("StoreRefreshSchedule: %v", err)
	}

	var calls []fetchCall
	Refresh(context.Background(), []string{"a", "a", "f", "nope", "b"}, refreshNow, refreshRI,
		recordingFetch(&calls, FetchResult{Body: []byte(`{}`)}, nil))

	if len(calls) != 1 || calls[0].id != "a" {
		t.Fatalf("calls = %+v, want exactly one fetch of a", calls)
	}
}

func TestRefreshKeepsTranscriptAndUsageSchedules(t *testing.T) {
	a := httpDef("a")
	setupRefresh(t, map[string]Definition{"a": a})
	transcript := cache.WorkSchedule{NextDueAt: refreshNow.Add(time.Minute), Failures: 2}
	usage := cache.WorkSchedule{NextDueAt: refreshNow.Add(2 * time.Minute), DefHash: "keep"}
	if err := cache.StoreRefreshSchedule(cache.RefreshSchedule{
		Transcripts: map[string]cache.WorkSchedule{"s1": transcript},
		Usage:       map[string]cache.WorkSchedule{"k": usage},
	}); err != nil {
		t.Fatalf("StoreRefreshSchedule: %v", err)
	}

	var calls []fetchCall
	Refresh(context.Background(), []string{"a"}, refreshNow, refreshRI, recordingFetch(&calls, FetchResult{Body: []byte(`{}`)}, nil))

	s := cache.LoadRefreshSchedule()
	sameWork(t, s.Transcripts["s1"], transcript)
	sameWork(t, s.Usage["k"], usage)
}

func TestRefreshUnreadableSourcesFileDoesNothing(t *testing.T) {
	setupRefresh(t, map[string]Definition{"a": httpDef("a")})
	p, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.WriteFile(p, []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var calls []fetchCall
	Refresh(context.Background(), []string{"a"}, refreshNow, refreshRI, recordingFetch(&calls, FetchResult{}, nil))

	if len(calls) != 0 {
		t.Fatalf("calls = %+v, want none when sources.json is unreadable", calls)
	}
}
