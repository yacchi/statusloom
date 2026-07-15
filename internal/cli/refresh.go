package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/usage"
)

type refreshIdentity struct {
	SessionID  string `json:"session_id"`
	Transcript string `json:"transcript_path"`
	Version    string `json:"version"`
}

// usageAPIEnabled reports whether the background account-usage fetch is
// allowed. Setting STATUSLOOM_NO_USAGE_API (to any value) is a kill switch
// that disables all OAuth-usage-API network access.
func usageAPIEnabled() bool { return os.Getenv("STATUSLOOM_NO_USAGE_API") == "" }

// acquireUsageToken and fetchUsage are indirected through package-level vars
// so tests can substitute network-free / credential-free stubs. In
// production they wire straight through to the usage package. TestMain
// installs safe defaults for the whole cli test binary (see main_test.go),
// guaranteeing no cli test ever reads a real credential or makes a real HTTP
// request.
var acquireUsageToken = usage.Token

var fetchUsage = func(ctx context.Context, token, version string) (*usage.Report, int, error) {
	return usage.Fetch(ctx, nil, token, version)
}

var startRefreshProcess = func(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Never re-exec a `go test` binary: under test os.Executable() is the
	// test binary (e.g. cli.test), and re-running it re-runs the whole suite,
	// which fork-bombs. Belt-and-suspenders alongside the TestMain stub.
	if strings.HasSuffix(exe, ".test") {
		return nil
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// maybeStartRefresh spawns a detached background refresh worker when any
// refreshable source is due. It keeps the render path network-free: the
// due-ness checks and lease acquisition are local disk/manifest reads, and
// the actual network fetch happens only inside the spawned subprocess
// (runRefresh).
//
// Two independent work items can make a refresh due:
//   - transcript analytics for the current session, and
//   - the account-usage OAuth poll (gated by the STATUSLOOM_NO_USAGE_API
//     kill switch).
//
// A single lease covers both: the spawned worker re-checks each item's
// due-ness itself, so passing an empty session-id/transcript (usage-only
// spawn) is fine.
func maybeStartRefresh(raw []byte, now time.Time) {
	var id refreshIdentity
	if json.Unmarshal(raw, &id) != nil {
		return
	}
	transcriptDue := id.SessionID != "" && id.Transcript != "" && cache.RefreshDue(id.SessionID, now)
	usageDue := usageAPIEnabled() && cache.AccountUsageDue(now)
	if !transcriptDue && !usageDue {
		return
	}
	leaseID := randomLeaseID()
	ok, err := cache.AcquireRefreshLease(leaseID, now)
	if err != nil || !ok {
		return
	}
	if startRefreshProcess("refresh", "--once", "--session-id", id.SessionID, "--transcript", id.Transcript, "--lease-id", leaseID, "--cc-version", id.Version) != nil {
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
	ccVersion := fs.String("cc-version", "", "Claude Code version for the usage API User-Agent")
	// Note: session-id/transcript may legitimately be empty (a usage-only
	// spawn). Only --once and a clean arg list are hard requirements.
	if fs.Parse(args) != nil || !*once || fs.NArg() != 0 {
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

	// Transcript analytics (unchanged behavior): only when both the session
	// id and transcript path are present.
	if *sessionID != "" && *transcript != "" {
		m := cache.LoadRefreshManifest()
		s := m.Sessions[*sessionID]
		s.LastAttempt = now
		s.NextDueAt = now.Add(3 * time.Second)
		if err := cache.RefreshTranscript(*sessionID, *transcript, now); err == nil {
			s.LastSuccess = now
		}
		m.Sessions[*sessionID] = s
		_ = cache.StoreRefreshManifest(m)
	}

	// Account-usage poll: gated by the kill switch and its own schedule.
	if usageAPIEnabled() && cache.AccountUsageDue(now) {
		refreshAccountUsage(now, *ccVersion)
	}
	return 0
}

// refreshAccountUsage performs one account-usage poll and updates the
// refresh manifest's AccountUsageSchedule. It is called only from the
// spawned worker subprocess, never from the render path, so the network
// fetch here never blocks a status-line render.
func refreshAccountUsage(now time.Time, ccVersion string) {
	m := cache.LoadRefreshManifest()

	token, err := acquireUsageToken(os.Getenv)
	if errors.Is(err, usage.ErrNoToken) || token == "" {
		// Nothing to do: no credential available. Reschedule at the normal
		// cadence without counting it as a failure (no backoff).
		m.AccountUsage.NextDueAt = now.Add(cache.UsageRefreshInterval)
		m.AccountUsage.LastAttempt = now
		_ = cache.StoreRefreshManifest(m)
		return
	}

	report, _, ferr := fetchUsage(context.Background(), token, ccVersion)
	if ferr != nil || report == nil {
		// Fetch failed (401, 429, transport error, empty body). Keep the last
		// good cache; count the failure and back off. The HTTP status is
		// intentionally not logged.
		failures := m.AccountUsage.Failures + 1
		m.AccountUsage.Failures = failures
		m.AccountUsage.NextDueAt = cache.NextUsageDue(now, failures)
		m.AccountUsage.LastAttempt = now
		_ = cache.StoreRefreshManifest(m)
		return
	}

	env := usageReportToEnvelope(report, now)
	_ = cache.StoreAccountUsage(accountCacheKey, env)
	m.AccountUsage = cache.AccountUsageSchedule{
		NextDueAt:   cache.NextUsageDue(now, 0),
		LastAttempt: now,
		LastSuccess: now,
		Failures:    0,
	}
	_ = cache.StoreRefreshManifest(m)
}

// usageReportToEnvelope maps a usage.Report into the persisted account-usage
// envelope, translating the API's per-window Utilization into the cache's
// UsedPercentage field and copying the extra-usage credit state.
func usageReportToEnvelope(r *usage.Report, now time.Time) cache.AccountUsageEnvelope {
	env := cache.NewAccountUsageEnvelope(now)
	win := func(w *usage.Window) *cache.RateWindowState {
		if w == nil {
			return nil
		}
		return &cache.RateWindowState{UsedPercentage: w.Utilization, ResetsAt: w.ResetsAt}
	}
	env.FiveHour = win(r.FiveHour)
	env.SevenDay = win(r.SevenDay)
	env.SevenDayOpus = win(r.SevenDayOpus)
	env.SevenDaySonnet = win(r.SevenDaySonnet)
	if r.Extra != nil {
		env.ExtraUsage = &cache.ExtraUsageState{
			Enabled:      r.Extra.IsEnabled,
			MonthlyLimit: r.Extra.MonthlyLimit,
			UsedCredits:  r.Extra.UsedCredits,
			Utilization:  r.Extra.Utilization,
		}
	}
	return env
}
