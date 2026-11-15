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
	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/gitstatus"
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

// collectGit is the git status collector. Only the worker's git step (runRefresh
// --once --git-cwd) calls it; the render path never runs git. Tests replace it.
var collectGit = gitstatus.Collect

// startRefreshProcess starts the background worker `statusloom <args...>`
// (args begin with "refresh") so that it outlives the render. Claude Code kills
// a statusline it cancels (superseded, or still running when it exits)
// together with its process tree, so relayRefreshArgs routes the start through
// a short-lived relay process (`refresh --relay`): the worker's parent is
// already gone and the tree walk never reaches it.
var startRefreshProcess = func(args ...string) (int, error) {
	return spawnSelf(relayRefreshArgs(args))
}

// relayRefreshArgs routes a worker start through `refresh --relay`, which
// starts the real worker and exits immediately. The worker's parent pid then
// names a dead process, so a process-tree kill of the statusline command
// cannot reach it. Verified against Claude Code v2.1.325: on Windows it runs
// `taskkill /PID <shell> /T /F`; on macOS it sends SIGTERM to the
// statusline's descendants (a worker in a session of its own was still killed
// while the render that started it was alive) as well as to its process group
// (which detachRefreshProcess keeps the worker out of). Started directly, the
// worker was killed mid-run, leaving the lease to a dead holder and no cache
// written. Only the relay's own few milliseconds of life remain exposed.
func relayRefreshArgs(args []string) []string {
	if len(args) == 0 || args[0] != "refresh" {
		return args
	}
	out := make([]string, 0, len(args)+1)
	out = append(out, "refresh", "--relay")
	return append(out, args[1:]...)
}

// spawnSelf starts os.Executable() with args, detached from the caller's
// stdio and console (detachRefreshProcess), does not wait for it, and returns
// its pid (0 when nothing was started).
func spawnSelf(args []string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	// Never re-exec a `go test` binary: under test os.Executable() is the
	// test binary (e.g. cli.test, or cli.test.exe on Windows), and re-running it
	// re-runs the whole suite, which fork-bombs. Belt-and-suspenders alongside
	// the TestMain stub.
	if strings.HasSuffix(strings.TrimSuffix(exe, ".exe"), ".test") {
		return 0, nil
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detachRefreshProcess(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	return pid, cmd.Process.Release()
}

// runRefreshRelay is `refresh --relay <worker args>`: it starts the worker
// described by the remaining arguments (args without --relay) and exits at
// once, leaving the worker without a live parent. When the start fails, the
// lease the render handed over (--lease-id) is released again so the next
// render can retry instead of waiting out the lease TTL. When it succeeds,
// the relay hands the lease to the worker (cache.SetRefreshLeaseHolder)
// before exiting, so the lease never names the exited relay while the worker
// runs; a relay killed before that leaves a dead holder, which the next render
// takes over at once.
func runRefreshRelay(args []string, leaseID string) int {
	worker := []string{"refresh"}
	for _, a := range args {
		if a != "--relay" && a != "--relay=true" {
			worker = append(worker, a)
		}
	}
	pid, err := relaySpawn(worker)
	if err != nil {
		if leaseID != "" {
			cache.ReleaseRefreshLease(leaseID)
		}
		return 1
	}
	if leaseID != "" && pid > 0 {
		cache.SetRefreshLeaseHolder(leaseID, 0, pid)
	}
	return 0
}

// relaySpawn is the relay's worker start; tests replace it.
var relaySpawn = spawnSelf

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
	GitCwd          string              // session cwd, set only when dsl.UsesGit(doc); "" = no git work item
	Git             config.GitConfig    // config.DocumentGitConfig(doc); used only when GitCwd is set
}

// maybeStartRefresh spawns one detached background refresh worker when any
// work item is due. It keeps the render path network-free and command-free:
// the due-ness checks and lease acquisition are local disk reads (the refresh
// schedule is loaded once), and every network fetch or external command runs
// only inside the spawned subprocess (runRefresh).
//
// Nothing due -> no lease and no spawn. Otherwise a single machine-wide lease
// covers the one spawned worker, which handles every item it was handed and
// re-checks each item's schedule itself. See planRefresh for the work items
// and startPlannedRefresh for the lease + spawn.
func maybeStartRefresh(req refreshRequest) {
	plan, ok := planRefresh(req, cache.LoadRefreshSchedule())
	if !ok || (len(plan.Items) == 0 && !sessionMergePending()) {
		return
	}
	startPlannedRefresh(plan, req.Now)
}

// refreshPlan is the due-ness decision of one render: the due work items and
// the worker arguments that hand them over (without the lease id).
type refreshPlan struct {
	Items []cache.WorkItem
	args  []string
}

// planRefresh decides which work items are due in s. It is pure apart from
// reading sources.json through req.Sources; ok is false when the stdin
// payload cannot be decoded (nothing can be requested then).
//
// Four independent work items can be due, each with its own schedule entry:
//   - git status for the session cwd, only when the document uses git-derived
//     values (Git[GitCwd], interval = <git cache-ttl-ms>). The render itself only
//     reads the repo cache; git runs solely in the spawned worker;
//   - transcript analytics for the current session, only when the document
//     uses transcript-derived values (Transcripts[session_id]);
//   - the account-usage OAuth poll for the rendering account (gated by the
//     STATUSLOOM_NO_USAGE_API kill switch), due on Usage[AccountKey]'s
//     schedule or immediately when ForceUsage is set;
//   - the command/http external sources the document references that are
//     due per sources.DueIDs.
//
// A pending session-state merge (sessionexport.MergePending, the marker a
// render leaves when its session summary changed) is not a work item: the
// caller checks it last, only when nothing else is due, since every worker
// run merges anyway.
//
// ForceUsage is set by renderDocFromRaw (via cache.ApplyAccountCache's
// rolledOver return) when a five-hour/seven-day window has definitely reset
// with no fresh replacement available - a known fact, not a mere schedule
// elapsing, so it bypasses the usage schedule (and any accumulated failure
// backoff). This is naturally rate-limited by AcquireRefreshLease's 2-minute
// lease TTL, same as any other spawn here.
func planRefresh(req refreshRequest, s cache.RefreshSchedule) (refreshPlan, bool) {
	var id refreshIdentity
	if json.Unmarshal(req.Raw, &id) != nil {
		return refreshPlan{}, false
	}
	var p refreshPlan
	p.args = []string{"--cc-version", id.Version}
	if req.GitCwd != "" && s.Git[req.GitCwd].Due(req.Now) {
		p.Items = append(p.Items, cache.WorkItem{Kind: cache.WorkGit, Key: req.GitCwd})
		p.args = append(p.args, "--git-cwd", req.GitCwd,
			"--git-interval-ms", strconv.Itoa(req.Git.CacheTTLMs),
			"--git-timeout-ms", strconv.Itoa(req.Git.TimeoutMs),
			"--git-untracked="+strconv.FormatBool(req.Git.IncludeUntracked),
			"--git-numstat="+strconv.FormatBool(req.Git.CollectNumstat))
	}
	if req.UsesTranscript && id.SessionID != "" && id.Transcript != "" && s.Transcripts[id.SessionID].Due(req.Now) {
		p.Items = append(p.Items, cache.WorkItem{Kind: cache.WorkTranscript, Key: id.SessionID})
		p.args = append(p.args, "--session-id", id.SessionID, "--transcript", id.Transcript)
	}
	if usageAPIEnabled() && (req.ForceUsage || s.Usage[req.AccountKey].Due(req.Now)) {
		p.Items = append(p.Items, cache.WorkItem{Kind: cache.WorkUsage, Key: req.AccountKey})
	}
	if req.ForceUsage {
		p.args = append(p.args, "--force-usage")
	}
	if len(req.SourceRefs) > 0 && req.Sources != nil {
		f := req.Sources()
		if due := sources.DueIDs(f, req.SourceRefs, s, req.Now); len(due) > 0 {
			for _, sid := range due {
				def, _ := f.Get(sid)
				p.Items = append(p.Items, cache.WorkItem{Kind: cache.WorkSource, Key: sid, DefHash: def.Hash()})
			}
			p.args = append(p.args, "--sources", strings.Join(due, ","))
		}
	}
	if req.RefreshInterval > 0 {
		p.args = append(p.args, "--refresh-interval", strconv.FormatFloat(req.RefreshInterval.Seconds(), 'f', -1, 64))
	}
	return p, true
}

// spawnOutcome is what startPlannedRefresh did.
type spawnOutcome int

const (
	spawnLeaseHeld spawnOutcome = iota // the lease is held elsewhere (or unwritable): nothing spawned
	spawnFailed                        // the lease was taken but the spawn failed; the lease is released again
	spawnStarted                       // a worker was started under the returned lease id
)

// startPlannedRefresh takes the machine-wide refresh lease and spawns one
// worker for plan, returning the lease id when a worker was started.
func startPlannedRefresh(plan refreshPlan, now time.Time) (string, spawnOutcome) {
	leaseID := randomLeaseID()
	ok, err := cache.AcquireRefreshLease(leaseID, now)
	if err != nil || !ok {
		return "", spawnLeaseHeld
	}
	args := append([]string{"refresh", "--once", "--lease-id", leaseID}, plan.args...)
	pid, err := startRefreshProcess(args...)
	if err != nil {
		cache.ReleaseRefreshLease(leaseID)
		return "", spawnFailed
	}
	// Hand the lease to the child (the relay that starts the worker) before
	// this render can exit: the lease must keep naming a live process while
	// the worker runs, or a concurrent render would take it over as dead.
	// from = this process, so a hand-over the relay or the worker already
	// recorded is not undone (cache.SetRefreshLeaseHolder).
	if pid > 0 {
		cache.SetRefreshLeaseHolder(leaseID, os.Getpid(), pid)
	}
	return leaseID, spawnStarted
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
	gitCwd := fs.String("git-cwd", "", "session cwd whose git status to collect when due")
	gitIntervalMs := fs.Int("git-interval-ms", 0, "<git cache-ttl-ms>: minimum interval between git collections for --git-cwd")
	gitTimeoutMs := fs.Int("git-timeout-ms", 0, "<git timeout-ms>: timeout per git invocation (0 = gitstatus.DefaultTimeout)")
	gitUntracked := fs.Bool("git-untracked", false, "<git include-untracked>")
	gitNumstat := fs.Bool("git-numstat", false, "<git collect-numstat>")
	relay := fs.Bool("relay", false, "internal: start the worker described by the other arguments as a detached grandchild and exit")
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

	if *relay {
		// The relay only re-launches a --once worker (startRefreshProcess);
		// it never takes the lease or does any work itself.
		if !*once || given["probe"] {
			return 2
		}
		return runRefreshRelay(args, *leaseID)
	}

	if given["probe"] {
		// Probe child mode (`statusloom source test` / the configurator's test
		// endpoint): an explicit one-shot fetch that never takes or checks the
		// refresh lease and never touches the schedule or cache envelopes.
		for _, name := range []string{"once", "session-id", "transcript", "force-usage", "sources", "git-cwd"} {
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
	} else {
		// Re-assert the hand-over the spawner (render or relay) made: it
		// covers a spawner killed between starting this worker and recording
		// it (cache.SetRefreshLeaseHolder documents the chain).
		cache.SetRefreshLeaseHolder(id, 0, os.Getpid())
	}
	defer cache.ReleaseRefreshLease(id)
	now := time.Now()
	ri := refreshIntervalFromSeconds(*refreshSecs)

	// Git status: only when the render handed over a cwd (it does so only for
	// documents that use git-derived values); refreshGit re-checks the cwd's
	// schedule. It runs first since it is the cheapest, most visible item.
	if *gitCwd != "" {
		refreshGit(*gitCwd, gitstatus.Options{
			Timeout:          time.Duration(*gitTimeoutMs) * time.Millisecond,
			IncludeUntracked: *gitUntracked,
			CollectNumstat:   *gitNumstat,
		}, time.Duration(*gitIntervalMs)*time.Millisecond)
	}

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

// refreshGit collects git status for cwd when its schedule entry (Git[cwd]) is
// due, stores the result in the repo cache the render path reads, and records
// the outcome: a repository is stored, "not a git repository" removes any
// stale entry (and waits cache.GitNotRepoInterval before trying again), and a
// git failure or timeout keeps the last stored snapshot and backs off. It runs
// only inside the worker, under the refresh lease.
func refreshGit(cwd string, opts gitstatus.Options, interval time.Duration) {
	s := cache.LoadRefreshSchedule()
	w := s.Git[cwd]
	if !w.Due(time.Now()) {
		return
	}
	repo, err := collectGit(context.Background(), cwd, opts)
	done := time.Now()
	switch {
	case err != nil:
		// Keep whatever the cache already holds; the statusline stays quiet.
	case repo == nil:
		_ = cache.DeleteRepo(cwd)
	default:
		_ = cache.StoreRepo(cwd, *repo, done)
	}
	s.Git[cwd] = w.RecordGit(done, err == nil, repo != nil, interval)
	s.Prune(done)
	_ = cache.StoreRefreshSchedule(s)
}
