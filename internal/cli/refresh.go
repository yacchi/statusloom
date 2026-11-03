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
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/claudeaccount"
	"github.com/yacchi/statusloom/internal/sessionexport"
	"github.com/yacchi/statusloom/internal/sources"
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

// fetchSource is the external-source fetcher. Only the worker's sources step
// (runRefresh --once --sources) and the --probe child mode call it; the render
// path never does. Tests replace it with a fake (TestMain does not stub it, so
// a test that reaches the worker's sources step must install its own).
var fetchSource sources.Fetcher = sources.Fetch

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

// mergeSessions merges the per-process session-state detail files into the
// public sessions.json and drops ended processes. Only the --once worker calls
// it, while it holds the refresh lease. Tests replace it with a stub.
var mergeSessions = sessionexport.Merge

// sessionMergePending reports whether a render left the marker that asks for a
// session-state merge. Tests replace it with a stub.
var sessionMergePending = sessionexport.MergePending

// refreshRequest is everything a render hands to the refresh scheduler.
type refreshRequest struct {
	Raw             []byte // stdin payload: session_id, transcript_path, version
	Now             time.Time
	ForceUsage      bool                // a five-hour/seven-day window rolled over (bypasses the usage schedule)
	AccountKey      string              // cache.ResolveAccountCacheKey of the rendering profile
	UsesTranscript  bool                // dsl.UsesTranscript(doc)
	SourceRefs      []string            // dsl.ReferencedSources(doc)
	Sources         func() sources.File // definitions; called only when SourceRefs is non-empty
	RefreshInterval time.Duration       // Claude Code statusLine.refreshInterval; 0 = unknown
}

// maybeStartRefresh spawns one detached background refresh worker when any
// work item is due. It keeps the render path network-free and command-free:
// the due-ness checks and lease acquisition are local disk reads (the refresh
// schedule is loaded once), and every network fetch or external command runs
// only inside the spawned subprocess (runRefresh).
//
// Four independent work items can make a refresh due, each with its own
// schedule entry:
//   - transcript analytics for the current session, only when the document
//     uses transcript-derived values (Transcripts[session_id]);
//   - the account-usage OAuth poll for the rendering account (gated by the
//     STATUSLOOM_NO_USAGE_API kill switch), due on Usage[AccountKey]'s
//     schedule or immediately when ForceUsage is set;
//   - the command/http external sources the document references that are
//     due per sources.DueIDs;
//   - a pending session-state merge (sessionexport.MergePending, the marker a
//     render leaves when its session summary changed). It is checked last,
//     only when nothing else is due, since every worker run merges anyway.
//
// ForceUsage is set by renderDocFromRaw (via cache.ApplyAccountCache's
// rolledOver return) when a five-hour/seven-day window has definitely reset
// with no fresh replacement available - a known fact, not a mere schedule
// elapsing, so it bypasses the usage schedule (and any accumulated failure
// backoff). This is naturally rate-limited by AcquireRefreshLease's 2-minute
// lease TTL, same as any other spawn here.
//
// Nothing due -> no lease and no spawn. Otherwise a single machine-wide lease
// covers the one spawned worker, which handles every item it was handed and
// re-checks each item's schedule itself.
func maybeStartRefresh(req refreshRequest) {
	var id refreshIdentity
	if json.Unmarshal(req.Raw, &id) != nil {
		return
	}
	s := cache.LoadRefreshSchedule()
	transcriptDue := req.UsesTranscript && id.SessionID != "" && id.Transcript != "" && s.Transcripts[id.SessionID].Due(req.Now)
	usageDue := usageAPIEnabled() && (req.ForceUsage || s.Usage[req.AccountKey].Due(req.Now))
	var due []string
	if len(req.SourceRefs) > 0 && req.Sources != nil {
		due = sources.DueIDs(req.Sources(), req.SourceRefs, s, req.Now)
	}
	if !transcriptDue && !usageDue && len(due) == 0 && !sessionMergePending() {
		return
	}
	leaseID := randomLeaseID()
	ok, err := cache.AcquireRefreshLease(leaseID, req.Now)
	if err != nil || !ok {
		return
	}
	args := []string{"refresh", "--once", "--lease-id", leaseID, "--cc-version", id.Version}
	if transcriptDue {
		args = append(args, "--session-id", id.SessionID, "--transcript", id.Transcript)
	}
	if req.ForceUsage {
		args = append(args, "--force-usage")
	}
	if len(due) > 0 {
		args = append(args, "--sources", strings.Join(due, ","))
	}
	if req.RefreshInterval > 0 {
		args = append(args, "--refresh-interval", strconv.FormatFloat(req.RefreshInterval.Seconds(), 'f', -1, 64))
	}
	if startRefreshProcess(args...) != nil {
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
	forceUsage := fs.Bool("force-usage", false, "bypass the account-usage poll's normal schedule (set by the caller when a window is known to have just reset)")
	sourceIDs := fs.String("sources", "", "comma-separated external source ids to refresh")
	refreshSecs := fs.Float64("refresh-interval", 0, "Claude Code statusLine.refreshInterval in seconds (0 = unknown)")
	probe := fs.String("probe", "", "fetch the saved source <id> once and print one JSON line")
	// Note: session-id/transcript may legitimately be empty (a usage-only
	// spawn). Only --once (or --probe) and a clean arg list are hard
	// requirements.
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return 2
	}
	// A flag counts as given when it appears on the command line, whatever
	// its value (e.g. `--sources ""` next to --probe is still a usage error).
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })

	if given["probe"] {
		// Probe child mode (`statusloom source test` / the configurator's test
		// endpoint): an explicit one-shot fetch that never takes or checks the
		// refresh lease and never touches the schedule or cache envelopes.
		for _, name := range []string{"once", "session-id", "transcript", "force-usage", "sources"} {
			if given[name] {
				return 2
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		if sources.ServeProbe(ctx, *probe, stdout, fetchSource) != nil {
			return 1
		}
		return 0
	}

	if !*once {
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
	ri := refreshIntervalFromSeconds(*refreshSecs)

	// Transcript analytics: only when both the session id and transcript path
	// were handed over (the render passes them only for documents that use
	// transcript-derived values), and only when this session's entry is due.
	if *sessionID != "" && *transcript != "" {
		s := cache.LoadRefreshSchedule()
		w := s.Transcripts[*sessionID]
		if w.Due(now) {
			err := cache.RefreshTranscript(*sessionID, *transcript, now)
			if s.Transcripts == nil {
				s.Transcripts = map[string]cache.WorkSchedule{}
			}
			s.Transcripts[*sessionID] = w.RecordTranscript(now, err == nil, ri)
			s.Prune(now)
			_ = cache.StoreRefreshSchedule(s)
		}
	}

	// Account-usage poll: gated by the kill switch and the account's own
	// schedule entry, unless the caller already determined a fetch is
	// warranted right now (--force-usage - see maybeStartRefresh).
	if usageAPIEnabled() {
		// The account cache key is resolved from this subprocess's own
		// environment (os.Getenv, since it is a detached child process that
		// inherited the invoking shell's CLAUDE_CONFIG_DIR - the same profile
		// the render pass that spawned it was rendering for), so a Team/Max
		// profile switch on the same machine keys the refreshed
		// extra-usage/per-model-weekly data and its schedule separately per
		// account instead of one clobbering the other's entry.
		key := cache.ResolveAccountCacheKey(claudeaccount.Load(os.Getenv))
		if *forceUsage || cache.LoadRefreshSchedule().Usage[key].Due(now) {
			refreshAccountUsage(now, key, *ccVersion)
		}
	}

	// External sources: the render already filtered these to the due ones;
	// sources.Refresh re-checks each against the schedule before fetching.
	var ids []string
	for _, part := range strings.Split(*sourceIDs, ",") {
		if p := strings.TrimSpace(part); p != "" {
			ids = append(ids, p)
		}
	}
	if len(ids) > 0 {
		sources.Refresh(context.Background(), ids, now, ri, fetchSource)
	}

	// Session-state merge: every --once worker merges the detail files into
	// sessions.json and drops ended processes while still holding the lease
	// (the deferred release above runs after this). A merge error never
	// changes the exit code.
	_ = mergeSessions(time.Now())
	return 0
}

// refreshIntervalFromSeconds converts the --refresh-interval flag into a
// duration. Zero, negative, NaN, infinite and overflowing values all mean
// "unknown" (0).
func refreshIntervalFromSeconds(sec float64) time.Duration {
	if sec > 0 && sec < float64(math.MaxInt64)/float64(time.Second) {
		return time.Duration(sec * float64(time.Second))
	}
	return 0
}

// refreshAccountUsage performs one account-usage poll for accountCacheKey and
// records the outcome in that key's refresh schedule entry (other accounts'
// entries are left as loaded). It is called only from the spawned worker
// subprocess, never from the render path, so the network fetch here never
// blocks a status-line render.
func refreshAccountUsage(now time.Time, accountCacheKey, ccVersion string) {
	s := cache.LoadRefreshSchedule()
	w := s.Usage[accountCacheKey]
	defer func() {
		if s.Usage == nil {
			s.Usage = map[string]cache.WorkSchedule{}
		}
		s.Usage[accountCacheKey] = w
		_ = cache.StoreRefreshSchedule(s)
	}()

	token, err := acquireUsageToken(os.Getenv)
	if errors.Is(err, usage.ErrNoToken) || token == "" {
		// Nothing to do: no credential available. Reschedule at the normal
		// cadence without counting it as a failure (no backoff).
		w = w.SkipUsage(now)
		return
	}

	report, _, ferr := fetchUsage(context.Background(), token, ccVersion)
	if ferr != nil || report == nil {
		// Fetch failed (401, 429, transport error, empty body). Keep the last
		// good cache; count the failure and back off. The HTTP status is
		// intentionally not logged.
		w = w.RecordUsage(now, false)
		return
	}

	env := usageReportToEnvelope(report, now)
	if env.ExtraUsage == nil {
		// This poll's response omitted extra_usage (the API frequently does).
		// Carry the last-observed value (and its own ObservedAt) forward
		// unchanged so a merely-missing field in one poll never drops a
		// value that is otherwise still fresh under its own long-lived
		// retention window (cache.LoadExtraUsage).
		if prev, ok := cache.LoadAccountUsageRaw(accountCacheKey); ok && prev != nil && prev.ExtraUsage != nil {
			env.ExtraUsage = prev.ExtraUsage
		}
	}
	_ = cache.StoreAccountUsage(accountCacheKey, env)
	w = w.RecordUsage(now, true)
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
			ObservedAt:   now,
		}
	}
	return env
}
