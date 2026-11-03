package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/sources"
	"github.com/yacchi/statusloom/internal/usage"
)

func floatPtr(f float64) *float64 { return &f }

// swapUsageSeams overrides the account-usage seams for the duration of a test
// and restores them (and the TestMain defaults) via t.Cleanup.
func swapUsageSeams(t *testing.T, tok func(func(string) string) (string, error), fetch func(context.Context, string, string) (*usage.Report, int, error)) {
	t.Helper()
	prevTok, prevFetch := acquireUsageToken, fetchUsage
	acquireUsageToken = tok
	fetchUsage = fetch
	t.Cleanup(func() {
		acquireUsageToken = prevTok
		fetchUsage = prevFetch
	})
}

// TestRunRefresh_UsageSuccess: the fetch succeeds -> envelope is stored and
// the account's refresh schedule entry records success with Failures=0 and a normal-cadence
// NextDueAt.
func TestRunRefresh_UsageSuccess(t *testing.T) {
	setupEnv(t)
	report := &usage.Report{
		FiveHour:     &usage.Window{Utilization: 10, ResetsAt: time.Now().Add(1 * time.Hour)},
		SevenDayOpus: &usage.Window{Utilization: 20, ResetsAt: time.Now().Add(2 * time.Hour)},
		Extra:        &usage.Extra{IsEnabled: true, MonthlyLimit: floatPtr(100), UsedCredits: floatPtr(5), Utilization: floatPtr(5)},
	}
	swapUsageSeams(t,
		func(func(string) string) (string, error) { return "tok-abc", nil },
		func(context.Context, string, string) (*usage.Report, int, error) { return report, 200, nil },
	)

	if code := runRefresh([]string{"--once", "--cc-version", "9.9.9"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("runRefresh() code = %d, want 0", code)
	}

	now := time.Now()
	got, _, ok := cache.LoadAccountUsage(cache.AccountCacheKey, now)
	if !ok || got == nil {
		t.Fatal("expected an account-usage envelope to be stored")
	}
	if got.ExtraUsage == nil || !got.ExtraUsage.Enabled {
		t.Error("stored envelope missing extra-usage state")
	}
	if got.SevenDayOpus == nil || got.SevenDayOpus.UsedPercentage != 20 {
		t.Errorf("stored SevenDayOpus = %v, want 20", got.SevenDayOpus)
	}

	w := cache.LoadRefreshSchedule().Usage[cache.AccountCacheKey]
	if w.Failures != 0 {
		t.Errorf("Failures = %d, want 0", w.Failures)
	}
	if w.LastSuccess.IsZero() {
		t.Error("LastSuccess not set on success")
	}
	wantDue := now.Add(cache.UsageRefreshInterval)
	if diff := w.NextDueAt.Sub(wantDue); diff > 2*time.Second || diff < -2*time.Second {
		t.Errorf("NextDueAt = %v, want ~%v", w.NextDueAt, wantDue)
	}
}

// TestRunRefresh_UsageFailure: fetch returns ErrRateLimited -> Failures
// increments, NextDueAt backs off beyond the normal cadence, and NO envelope
// is stored (a previously good one survives).
func TestRunRefresh_UsageFailure(t *testing.T) {
	setupEnv(t)
	// Seed a prior good envelope to prove it is preserved.
	prior := cache.NewAccountUsageEnvelope(time.Now())
	prior.ExtraUsage = &cache.ExtraUsageState{Enabled: true, Utilization: floatPtr(33)}
	if err := cache.StoreAccountUsage(cache.AccountCacheKey, prior); err != nil {
		t.Fatalf("seed StoreAccountUsage() error = %v", err)
	}

	swapUsageSeams(t,
		func(func(string) string) (string, error) { return "tok-abc", nil },
		func(context.Context, string, string) (*usage.Report, int, error) {
			return nil, 429, usage.ErrRateLimited
		},
	)

	if code := runRefresh([]string{"--once", "--cc-version", "1.0.0"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("runRefresh() code = %d, want 0", code)
	}

	now := time.Now()
	w := cache.LoadRefreshSchedule().Usage[cache.AccountCacheKey]
	if w.Failures != 1 {
		t.Errorf("Failures = %d, want 1", w.Failures)
	}
	if !w.LastSuccess.IsZero() {
		t.Error("LastSuccess must not be set on failure")
	}
	// Backoff: NextDueAt must be strictly beyond the normal cadence.
	minBackoff := now.Add(cache.UsageRefreshInterval)
	if !w.NextDueAt.After(minBackoff) {
		t.Errorf("NextDueAt = %v, want after %v (backoff)", w.NextDueAt, minBackoff)
	}

	// Prior envelope must be intact (unchanged utilization).
	got, _, ok := cache.LoadAccountUsage(cache.AccountCacheKey, now)
	if !ok || got == nil || got.ExtraUsage == nil || got.ExtraUsage.Utilization == nil || *got.ExtraUsage.Utilization != 33 {
		t.Errorf("prior envelope not preserved: %+v", got)
	}
}

// TestRunRefresh_UsageExtraCarriedForward: a poll that fetched extra_usage is
// followed by a later poll whose report omits it (Extra: nil). The second
// poll must NOT drop the previously observed ExtraUsage - it is carried
// forward unchanged (value and ObservedAt), proving extra-usage survives a
// poll that simply didn't get it back from the API.
func TestRunRefresh_UsageExtraCarriedForward(t *testing.T) {
	setupEnv(t)

	// First poll: report includes Extra.
	report1 := &usage.Report{
		FiveHour: &usage.Window{Utilization: 10, ResetsAt: time.Now().Add(1 * time.Hour)},
		Extra:    &usage.Extra{IsEnabled: true, MonthlyLimit: floatPtr(100), UsedCredits: floatPtr(5), Utilization: floatPtr(5)},
	}
	swapUsageSeams(t,
		func(func(string) string) (string, error) { return "tok-abc", nil },
		func(context.Context, string, string) (*usage.Report, int, error) { return report1, 200, nil },
	)
	if code := runRefresh([]string{"--once", "--cc-version", "1.0.0"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("runRefresh() (poll 1) code = %d, want 0", code)
	}

	first, ok := cache.LoadAccountUsageRaw(cache.AccountCacheKey)
	if !ok || first == nil || first.ExtraUsage == nil {
		t.Fatalf("expected ExtraUsage stored after poll 1: %+v", first)
	}
	firstObservedAt := first.ExtraUsage.ObservedAt
	if firstObservedAt.IsZero() {
		t.Fatal("expected ExtraUsage.ObservedAt to be set after poll 1")
	}

	// runRefresh polls usage only when Usage[key] is due, and the successful
	// poll above pushed its NextDueAt out by cache.UsageRefreshInterval (5m).
	// Force the entry due again so the second runRefresh call actually
	// reaches refreshAccountUsage instead of silently no-oping.
	s := cache.LoadRefreshSchedule()
	w := s.Usage[cache.AccountCacheKey]
	w.NextDueAt = time.Now().Add(-1 * time.Minute)
	s.Usage[cache.AccountCacheKey] = w
	if err := cache.StoreRefreshSchedule(s); err != nil {
		t.Fatalf("StoreRefreshSchedule() error = %v", err)
	}

	report2 := &usage.Report{
		FiveHour: &usage.Window{Utilization: 15, ResetsAt: time.Now().Add(1 * time.Hour)},
		// Extra omitted this time.
	}
	swapUsageSeams(t,
		func(func(string) string) (string, error) { return "tok-abc", nil },
		func(context.Context, string, string) (*usage.Report, int, error) { return report2, 200, nil },
	)
	if code := runRefresh([]string{"--once", "--cc-version", "1.0.0"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("runRefresh() (poll 2) code = %d, want 0", code)
	}

	second, ok := cache.LoadAccountUsageRaw(cache.AccountCacheKey)
	if !ok || second == nil {
		t.Fatalf("expected envelope still present after poll 2")
	}
	if second.ExtraUsage == nil {
		t.Fatal("ExtraUsage must be carried forward, not dropped, when poll 2 omits it")
	}
	if second.ExtraUsage.UsedCredits == nil || *second.ExtraUsage.UsedCredits != 5 {
		t.Errorf("UsedCredits = %v, want 5 (carried forward from poll 1)", second.ExtraUsage.UsedCredits)
	}
	if second.ExtraUsage.Utilization == nil || *second.ExtraUsage.Utilization != 5 {
		t.Errorf("Utilization = %v, want 5 (carried forward from poll 1)", second.ExtraUsage.Utilization)
	}
	if !second.ExtraUsage.ObservedAt.Equal(firstObservedAt) {
		t.Errorf("ExtraUsage.ObservedAt = %v, want unchanged from poll 1 (%v)", second.ExtraUsage.ObservedAt, firstObservedAt)
	}
	// The FiveHour window, in contrast, must reflect poll 2's fresh value.
	if second.FiveHour == nil || second.FiveHour.UsedPercentage != 15 {
		t.Errorf("FiveHour = %v, want UsedPercentage 15 (poll 2's fresh value)", second.FiveHour)
	}
}

// TestRunRefresh_UsageNoToken: no OAuth token -> reschedule at normal cadence
// without a failure, and fetchUsage must NOT be called.
func TestRunRefresh_UsageNoToken(t *testing.T) {
	setupEnv(t)
	fetchCalled := false
	swapUsageSeams(t,
		func(func(string) string) (string, error) { return "", usage.ErrNoToken },
		func(context.Context, string, string) (*usage.Report, int, error) {
			fetchCalled = true
			return nil, 0, usage.ErrRateLimited
		},
	)

	if code := runRefresh([]string{"--once", "--cc-version", "1.0.0"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("runRefresh() code = %d, want 0", code)
	}

	if fetchCalled {
		t.Error("fetchUsage must NOT be called when no token is available")
	}

	now := time.Now()
	w := cache.LoadRefreshSchedule().Usage[cache.AccountCacheKey]
	if w.Failures != 0 {
		t.Errorf("Failures = %d, want 0 (no-token is not a failure)", w.Failures)
	}
	if w.LastAttempt.IsZero() {
		t.Error("LastAttempt should be set even on the no-token path")
	}
	wantDue := now.Add(cache.UsageRefreshInterval)
	if diff := w.NextDueAt.Sub(wantDue); diff > 2*time.Second || diff < -2*time.Second {
		t.Errorf("NextDueAt = %v, want ~%v", w.NextDueAt, wantDue)
	}

	// No envelope should have been stored.
	if _, _, ok := cache.LoadAccountUsage(cache.AccountCacheKey, now); ok {
		t.Error("no envelope should be stored on the no-token path")
	}
}

// refreshUsageCaptureSpawns replaces startRefreshProcess with a recorder that
// returns ret, restoring the previous seam (the TestMain stub) on cleanup.
func refreshUsageCaptureSpawns(t *testing.T, ret error) *[][]string {
	t.Helper()
	var calls [][]string
	prev := startRefreshProcess
	startRefreshProcess = func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return ret
	}
	t.Cleanup(func() { startRefreshProcess = prev })
	return &calls
}

// refreshUsageFailFetchSource installs a fetchSource fake that fails the test
// when called.
func refreshUsageFailFetchSource(t *testing.T) {
	t.Helper()
	prev := fetchSource
	fetchSource = func(context.Context, sources.Definition, string) (sources.FetchResult, error) {
		t.Error("fetchSource must not be called")
		return sources.FetchResult{}, errors.New("unexpected fetch")
	}
	t.Cleanup(func() { fetchSource = prev })
}

// TestRefreshUsageTwoAccountKeys: usage due-ness is read for the rendering
// account's key only, and a poll for one key never rewrites another key's
// schedule entry or cache.
func TestRefreshUsageTwoAccountKeys(t *testing.T) {
	setupEnv(t)
	t.Setenv("STATUSLOOM_NO_USAGE_API", "")
	home := os.Getenv("HOME")
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"oauthAccount":{"organizationUuid":"org-a"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	seed := cache.WorkSchedule{NextDueAt: now.Add(time.Hour), LastAttempt: now.Add(-time.Minute), Failures: 2}
	s := cache.LoadRefreshSchedule()
	s.Usage["org-b"] = seed
	if err := cache.StoreRefreshSchedule(s); err != nil {
		t.Fatal(err)
	}
	calls := refreshUsageCaptureSpawns(t, nil)

	maybeStartRefresh(refreshRequest{Raw: []byte(`{}`), Now: now, AccountKey: "org-b"})
	if len(*calls) != 0 {
		t.Fatalf("org-b not due: spawns = %v, want none", *calls)
	}
	maybeStartRefresh(refreshRequest{Raw: []byte(`{}`), Now: now, AccountKey: "org-a"})
	if len(*calls) != 1 {
		t.Fatalf("org-a due: spawns = %d, want 1", len(*calls))
	}

	swapUsageSeams(t,
		func(func(string) string) (string, error) { return "tok-abc", nil },
		func(context.Context, string, string) (*usage.Report, int, error) {
			return &usage.Report{FiveHour: &usage.Window{Utilization: 10, ResetsAt: now.Add(time.Hour)}}, 200, nil
		},
	)
	if code := runRefresh((*calls)[0][1:], io.Discard, io.Discard); code != 0 {
		t.Fatalf("runRefresh() code = %d, want 0", code)
	}

	got := cache.LoadRefreshSchedule()
	a := got.Usage["org-a"]
	if a.LastSuccess.IsZero() || a.Failures != 0 {
		t.Errorf("Usage[org-a] = %+v, want LastSuccess set and Failures 0", a)
	}
	if _, ok := cache.LoadAccountUsageRaw("org-a"); !ok {
		t.Error("org-a usage envelope not stored")
	}
	b := got.Usage["org-b"]
	if !b.NextDueAt.Equal(seed.NextDueAt) || !b.LastAttempt.Equal(seed.LastAttempt) || !b.LastSuccess.Equal(seed.LastSuccess) || b.Failures != seed.Failures {
		t.Errorf("Usage[org-b] = %+v, want unchanged %+v", b, seed)
	}
	if _, ok := cache.LoadAccountUsageRaw("org-b"); ok {
		t.Error("org-b usage envelope must not be written")
	}
}

// TestRefreshUsageSpawnArgs: the optional argument groups appear in order,
// only when their condition holds.
func TestRefreshUsageSpawnArgs(t *testing.T) {
	raw := []byte(`{"session_id":"s1","transcript_path":"/t.jsonl","version":"2.1.200"}`)

	t.Run("all groups", func(t *testing.T) {
		setupEnv(t)
		t.Setenv("STATUSLOOM_NO_USAGE_API", "")
		calls := refreshUsageCaptureSpawns(t, nil)
		maybeStartRefresh(refreshRequest{
			Raw: raw, Now: time.Now(), ForceUsage: true, AccountKey: "k", UsesTranscript: true,
			SourceRefs: []string{"a"},
			Sources: func() sources.File {
				return sources.File{Sources: map[string]sources.Definition{
					"a": {ID: "a", Kind: sources.KindCommand, Argv: []string{"true"}},
				}}
			},
			RefreshInterval: 1500 * time.Millisecond,
		})
		if len(*calls) != 1 {
			t.Fatalf("spawns = %d, want 1", len(*calls))
		}
		args := (*calls)[0]
		if len(args) < 4 || args[3] == "" {
			t.Fatalf("args = %v, want a lease id at index 3", args)
		}
		want := []string{"refresh", "--once", "--lease-id", args[3], "--cc-version", "2.1.200",
			"--session-id", "s1", "--transcript", "/t.jsonl", "--force-usage", "--sources", "a", "--refresh-interval", "1.5"}
		if !slices.Equal(args, want) {
			t.Errorf("args = %q\nwant %q", args, want)
		}
	})

	t.Run("minimal", func(t *testing.T) {
		setupEnv(t)
		t.Setenv("STATUSLOOM_NO_USAGE_API", "")
		calls := refreshUsageCaptureSpawns(t, nil)
		maybeStartRefresh(refreshRequest{
			Raw: raw, Now: time.Now(), AccountKey: "k",
			Sources: func() sources.File {
				t.Error("Sources must not be called without SourceRefs")
				return sources.File{}
			},
		})
		if len(*calls) != 1 {
			t.Fatalf("spawns = %d, want 1", len(*calls))
		}
		args := (*calls)[0]
		if len(args) < 4 || args[3] == "" {
			t.Fatalf("args = %v, want a lease id at index 3", args)
		}
		want := []string{"refresh", "--once", "--lease-id", args[3], "--cc-version", "2.1.200"}
		if !slices.Equal(args, want) {
			t.Errorf("args = %q\nwant %q", args, want)
		}
	})
}

// TestRefreshUsageLeaseReleasedOnSpawnFailure: a failed spawn must not leave
// the machine-wide lease held for its full TTL.
func TestRefreshUsageLeaseReleasedOnSpawnFailure(t *testing.T) {
	setupEnv(t)
	t.Setenv("STATUSLOOM_NO_USAGE_API", "")
	calls := refreshUsageCaptureSpawns(t, errors.New("spawn failed"))
	maybeStartRefresh(refreshRequest{Raw: []byte(`{}`), Now: time.Now(), AccountKey: "k"})
	if len(*calls) != 1 {
		t.Fatalf("spawns = %d, want 1", len(*calls))
	}
	ok, err := cache.AcquireRefreshLease("next", time.Now())
	if err != nil || !ok {
		t.Errorf("AcquireRefreshLease after failed spawn = (%v, %v), want (true, nil)", ok, err)
	}
}

// TestRefreshUsageProbeFlags: --probe rejects --once and every work-item flag,
// and probe mode neither takes nor checks the refresh lease.
func TestRefreshUsageProbeFlags(t *testing.T) {
	for _, extra := range [][]string{
		{"--once"},
		{"--session-id", "x"},
		{"--transcript", "t"},
		{"--force-usage"},
		{"--sources", "a"},
	} {
		t.Run(extra[0], func(t *testing.T) {
			setupEnv(t)
			t.Setenv("STATUSLOOM_NO_USAGE_API", "")
			refreshUsageFailFetchSource(t)
			var out bytes.Buffer
			args := append([]string{"--probe", "a"}, extra...)
			if code := runRefresh(args, &out, io.Discard); code != 2 {
				t.Errorf("runRefresh(%v) = %d, want 2", args, code)
			}
			if out.Len() != 0 {
				t.Errorf("stdout = %q, want empty", out.String())
			}
		})
	}

	t.Run("lease held elsewhere", func(t *testing.T) {
		setupEnv(t)
		t.Setenv("STATUSLOOM_NO_USAGE_API", "")
		refreshUsageFailFetchSource(t)
		now := time.Now()
		if ok, err := cache.AcquireRefreshLease("other", now); err != nil || !ok {
			t.Fatalf("seed lease = (%v, %v)", ok, err)
		}
		var out bytes.Buffer
		if code := runRefresh([]string{"--probe", "nope"}, &out, io.Discard); code != 0 {
			t.Fatalf("runRefresh(--probe nope) = %d, want 0", code)
		}
		lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
		if len(lines) != 1 {
			t.Fatalf("stdout = %q, want one line", out.String())
		}
		var got sources.ProbeOutput
		if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
			t.Fatalf("decode %q: %v", lines[0], err)
		}
		if got.OK || got.Error != "unknown source" {
			t.Errorf("probe output = %+v, want {OK:false Error:unknown source}", got)
		}
		if ok, _ := cache.AcquireRefreshLease("again", now); ok {
			t.Error("probe mode must leave the other worker's lease held")
		}
	})
}

// TestRefreshUsageNoSpawnInPreview: the spawn now follows the render, but a
// render with overrides or in preview mode still never spawns.
func TestRefreshUsageNoSpawnInPreview(t *testing.T) {
	cases := []struct {
		name    string
		set     string
		preview bool
		want    int
	}{
		{name: "overrides", set: "extra-usage-limit-usd=50", want: 0},
		{name: "preview", preview: true, want: 0},
		{name: "live", want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupEnv(t)
			t.Setenv("STATUSLOOM_NO_USAGE_API", "")
			calls := refreshUsageCaptureSpawns(t, nil)
			var ov *metricOverrides
			if tc.set != "" {
				ov = &metricOverrides{}
				if err := ov.Set(tc.set); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := renderDocFromRaw(fixture(t, "full.json"), envMap(nil), "claude-code", io.Discard, false, ov, tc.preview); err != nil {
				t.Fatalf("renderDocFromRaw() error = %v", err)
			}
			if len(*calls) != tc.want {
				t.Errorf("spawns = %d, want %d", len(*calls), tc.want)
			}
		})
	}
}
