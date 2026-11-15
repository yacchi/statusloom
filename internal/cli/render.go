package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/adapters/claude"
	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/claudeaccount"
	"github.com/yacchi/statusloom/internal/claudecfg"
	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/detect"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/render"
	"github.com/yacchi/statusloom/internal/schema"
	"github.com/yacchi/statusloom/internal/sources"
)

// maxStdinBytes caps how much of stdin the render pipeline will read, per
// statusloom-local-development-plan.md section 3.1.
const maxStdinBytes = 10 << 20 // 10MB

// unsupportedToolError is returned by renderDocFromRaw when a tool is detected
// (or forced) that statusloom cannot render yet. It is a distinct type so
// callers can reproduce the exact legacy stderr wording ("tool %q not
// implemented yet", without the "statusloom:" prefix that fail adds).
type unsupportedToolError struct{ tool schema.ToolID }

func (e unsupportedToolError) Error() string {
	return fmt.Sprintf("tool %q not implemented yet", e.tool)
}

// runRender implements `statusloom render [--tool ID] [--draft]`. Arguments
// and the stdin terminal check are validated before stdin is read.
func runRender(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tool := fs.String("tool", "", "explicit tool id (e.g. claude-code); detected from stdin when omitted")
	draft := fs.Bool("draft", false, "render the tool's draft working node instead of the saved document")
	var overrides metricOverrides
	fs.Var(&overrides, "set", setFlagUsage)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if rejectInvocation("render", fs, stdin, stderr) {
		return 2
	}
	return runRenderPipeline(stdin, stdout, stderr, getenv, *tool, *draft, &overrides)
}

// setFlagUsage is the shared usage text of the `--set` flag on `claude` and
// `render`.
const setFlagUsage = "override a metric for this render (repeatable), e.g. --set extra-usage-limit-usd=50"

// rejectInvocation reports whether a `claude` or `render` invocation must be
// refused before stdin is read. It refuses a leftover positional argument and
// an interactive terminal on stdin (nothing is piped in, so the read would
// block). On refusal it writes the reason to stderr and returns true.
func rejectInvocation(cmd string, fs *flag.FlagSet, stdin io.Reader, stderr io.Writer) bool {
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "statusloom %s: unexpected argument %q\n", cmd, fs.Arg(0))
		return true
	}
	if stdinIsTerminal(stdin) {
		fmt.Fprintf(stderr, "statusloom %s: stdin is a terminal; pipe a Claude Code statusline JSON payload (e.g. statusloom %s < sample.json)\n", cmd, cmd)
		return true
	}
	return false
}

// stdinIsTerminal reports whether r is an *os.File attached to a character
// device, i.e. an interactive terminal. Pipes, regular files, and any
// non-file reader are not terminals.
func stdinIsTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// runRenderPipeline implements the render pipeline described in plan
// section 3.1/5: read stdin, detect the tool, decode, load config, fold in
// the account/repo caches, and render. explicitTool is "" for `statusloom
// render` without --tool, or a fixed tool id for dedicated subcommands.
// draft selects the tool's draft working node (see renderDocFromRaw).
func runRenderPipeline(stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, explicitTool string, draft bool, overrides *metricOverrides) int {
	raw, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes))
	if err != nil {
		return fail(stderr, fmt.Errorf("reading stdin: %w", err))
	}

	lines, err := renderDocFromRaw(raw, getenv, explicitTool, stderr, draft, overrides, draft || overrides.active())
	return writeRenderResult(stdout, stderr, lines, nil, err)
}

// renderDocFromRaw is the DSL render path shared by `statusloom claude`,
// `statusloom render`, and `statusloom monitor`: it loads the tool's current
// committed document from the internal store, renders it, and returns the
// joined output lines. Diagnostics are written to stderr directly; a
// document with error diagnostics (or an unreadable/invalid one) never
// crashes or blanks the status line — it falls back to DefaultDocument.
//
// When draft is true (`statusloom monitor --draft`), the tool's draft working
// node is rendered instead, falling back to the saved document when the draft
// is absent or invalid. The saved-document path (draft=false) keeps
// `statusloom claude` output byte-identical regardless of caller.
//
// overrides (`--set`, nil for none) are applied after every cache merge and
// cache write of the pass, so they win over cached/stdin values and are never
// persisted; a render with overrides also never spawns the refresh
// subprocess, since it is a manual preview rather than a live status line.
//
// preview marks a manual preview (`--draft` / `--set` with a sample payload):
// the sample must not pollute the real shared caches, so the account cache is
// only read (FillAccountFromCache), the session snapshot is not stored, and
// the refresh subprocess is never spawned. Live sessions (including
// `monitor --draft`) pass false and keep feeding the caches.
//
// It performs no network I/O and runs no external command - in particular it
// never runs git (only the local account/repo/session caches and read-only external
// source lookups through the sources provider: sources.json, file sources and
// cache envelopes). Any due refresh work is handed to a detached refresh
// worker spawned after the render - or, when the document has a <refresh wait>
// naming a work item that is cold (see waitForRefresh), spawned before it,
// with the render waiting up to the element's timeout for the worker to
// attempt that item. Previews never wait.
func renderDocFromRaw(raw []byte, getenv func(string) string, explicitTool string, stderr io.Writer, draft bool, overrides *metricOverrides, preview bool) ([]string, error) {
	start := time.Now()
	toolID, err := detect.Detect(explicitTool, raw)
	if err != nil {
		return nil, err
	}
	if toolID != schema.ToolClaudeCode {
		return nil, unsupportedToolError{tool: toolID}
	}

	snap, err := claude.New().Decode(raw)
	if err != nil {
		return nil, err
	}

	tool := string(schema.ToolClaudeCode)
	doc := resolveRenderDocument(tool, draft, stderr)

	// Resolve the account profile once (claudeaccount.Provider memoizes the
	// .claude.json read via sync.OnceValue) and reuse the same memoized
	// provider for render.Options.Profile below, so a render never opens
	// .claude.json more than once regardless of how many places consult it.
	// Note this does make the account-cache-key resolution force that one
	// read on every render (needed to key the account/extra-usage caches by
	// organizationUuid), even for a document with no account-* field - a
	// deliberate tradeoff for correct per-account cache isolation.
	profileProvider := claudeaccount.Provider(getenv)
	accountCacheKey := cache.ResolveAccountCacheKey(profileProvider())

	now := time.Now()
	rolledOver := applyRenderCaches(&snap, accountCacheKey, now, !preview)
	if !preview {
		exportSession(raw, getenv, now)
	}

	// External sources are read-only here (sources.json, file sources and
	// cache envelopes); previews resolve them too so they show source values.
	ri, _ := cache.ClaudeRefreshInterval(claudecfg.SettingsPath(getenv))
	prov := sources.NewProvider(now, ri)

	live := !preview && !overrides.active()
	var req refreshRequest
	var wait refreshWaitResult
	if live {
		req = refreshRequest{Raw: raw, Now: now, ForceUsage: rolledOver, AccountKey: accountCacheKey,
			UsesTranscript: dsl.UsesTranscript(doc), SourceRefs: dsl.ReferencedSources(doc), Sources: prov.File,
			RefreshInterval: ri}
		if dsl.UsesGit(doc) {
			req.GitCwd = snap.System.Cwd
			req.Git = config.DocumentGitConfig(doc)
		}
		// <refresh wait>: wait (bounded, measured from the render's start) for
		// the named work items, then rebuild the snapshot from the refreshed
		// caches. Without <refresh>, or with nothing to wait for, this is a
		// no-op and the render proceeds exactly as before.
		if policy := dsl.DocumentRefreshPolicy(doc); policy.Waits() && !refreshWaitDisabled(getenv) {
			wait = waitForRefresh(req, policy, start.Add(time.Duration(policy.TimeoutMS)*time.Millisecond))
			if wait.Waited {
				if fresh, derr := claude.New().Decode(raw); derr == nil {
					snap = fresh
					// The stdin windows were stored by the first pass: read only.
					req.ForceUsage = applyRenderCaches(&snap, accountCacheKey, now, false)
				}
			}
		}
	}
	if !preview {
		storeSessionSnapshot(&snap, now)
	}
	profile := profileProvider
	if overrides.active() {
		overrides.applySnapshot(&snap)
		profile = overrides.wrapProfile(profileProvider)
	}

	width := parseWidth(getenv("COLUMNS"))
	out := render.RenderDocumentString(snap, doc, render.Options{
		Width:   width,
		Now:     now,
		Env:     getenv,
		Profile: profile,
		Sources: prov.Lookup,
	})
	if live && !wait.Spawned {
		maybeStartRefresh(req)
	}
	if out == "" {
		return nil, nil
	}
	return []string{out}, nil
}

// applyRenderCaches folds the local caches into a freshly decoded snapshot:
// the account windows (storeStdin also writes the stdin windows to the shared
// account cache; a preview or a second pass only reads it), the OAuth usage
// cache, transcript analytics and the repo cache. It returns whether a
// five-hour/seven-day window has rolled over (refreshRequest.ForceUsage).
func applyRenderCaches(snap *schema.StatusSnapshot, accountCacheKey string, now time.Time, storeStdin bool) bool {
	var rolledOver bool
	if storeStdin {
		rolledOver = cache.ApplyAccountCache(accountCacheKey, snap, now)
	} else {
		rolledOver = cache.FillAccountFromCache(accountCacheKey, snap, now)
	}
	cache.ApplyExtraUsageCache(accountCacheKey, snap, now)
	applyTranscriptCache(snap, now)
	applyRepoCache(snap)
	return rolledOver
}

func applyTranscriptCache(snap *schema.StatusSnapshot, now time.Time) {
	if snap.Session.ID == "" {
		return
	}
	if analytics, _ := cache.LoadTranscriptAnalytics(snap.Session.ID, now); analytics != nil {
		snap.Session.Analytics = analytics
	}
}

// resolveRenderDocument returns the renderable document for a render pass. With
// draft=true it prefers the tool's draft working node (falling back to the
// saved document when the draft is absent or invalid); otherwise it loads the
// saved current document from the store. It always returns a non-nil,
// renderable document, falling back to the built-in DefaultDocument when the
// chosen source has error diagnostics or cannot be read (markup.md
// "fallback"). Diagnostics are written to stderr.
func resolveRenderDocument(tool string, draft bool, stderr io.Writer) *dsl.Document {
	if draft {
		if doc, ok := loadDraftDocument(tool, stderr); ok {
			return doc
		}
	}
	doc, diags, err := config.LoadDocument(tool)
	if err != nil {
		fmt.Fprintf(stderr, "statusloom: cannot load %s document: %v\n", tool, err)
		doc, _ = dsl.Parse(config.DefaultDocument(tool))
		return doc
	}
	writeDiagnostics(stderr, tool, diags)
	if doc == nil || doc.Root == nil || dsl.HasErrors(diags) {
		doc, _ = dsl.Parse(config.DefaultDocument(tool))
	}
	return doc
}

// loadDraftDocument parses and validates the tool's draft working node
// (config.LoadDraftDocument). ok is true only when a draft is present and
// parses without error-severity diagnostics; otherwise the caller falls back
// to the saved document. Diagnostics are written to stderr, and every
// fallback (absent, unreadable, or invalid draft) emits exactly one notice
// line so a requested --draft never silently renders the saved document.
func loadDraftDocument(tool string, stderr io.Writer) (*dsl.Document, bool) {
	if !config.DraftExists(tool) {
		fmt.Fprintf(stderr, "statusloom: %s draft absent; rendering the saved document\n", tool)
		return nil, false
	}
	doc, diags, ok, err := config.LoadDraftDocument(tool)
	if err != nil {
		fmt.Fprintf(stderr, "statusloom: %s draft unreadable (%v); rendering the saved document\n", tool, err)
		return nil, false
	}
	if !ok {
		fmt.Fprintf(stderr, "statusloom: %s draft invalid%s; rendering the saved document\n", tool, draftDiagnosticSummary(diags))
		return nil, false
	}
	writeDiagnostics(stderr, tool+" draft", diags)
	return doc, true
}

// draftDiagnosticSummary folds an invalid draft's diagnostics into a single
// parenthesized suffix, so the fallback notice stays one stderr line. It
// returns "" when there are no diagnostics.
func draftDiagnosticSummary(diags []dsl.Diagnostic) string {
	if len(diags) == 0 {
		return ""
	}
	parts := make([]string, 0, len(diags))
	for _, d := range diags {
		parts = append(parts, fmt.Sprintf("%s: %s", d.Severity, d.Message))
	}
	return " (" + strings.Join(parts, "; ") + ")"
}

// writeDiagnostics prints DSL parse/validation diagnostics to stderr, one per
// line, prefixed with the document label (e.g. "claude-code" or "claude-code
// draft").
func writeDiagnostics(stderr io.Writer, label string, diags []dsl.Diagnostic) {
	for _, d := range diags {
		fmt.Fprintf(stderr, "statusloom: %s: %s: %s\n", label, d.Severity, d.Message)
	}
}

// writeRenderResult writes the outcome of a render pass to stdout/stderr and
// returns the process exit code, reproducing the exact stream layout the
// render pipeline has always used: config warnings (prefixed "statusloom:")
// on stderr, the joined status-line lines on stdout, and errors on stderr.
// Shared by runRenderPipeline and runMonitor so `statusloom claude` output
// stays byte-identical regardless of which subcommand produced it.
func writeRenderResult(stdout, stderr io.Writer, lines, warnings []string, err error) int {
	if err != nil {
		var ute unsupportedToolError
		if errors.As(err, &ute) {
			fmt.Fprintf(stderr, "%s\n", err)
			return 1
		}
		return fail(stderr, err)
	}

	for _, w := range warnings {
		fmt.Fprintf(stderr, "statusloom: %s\n", w)
	}

	if len(lines) > 0 {
		fmt.Fprintln(stdout, strings.Join(lines, "\n"))
	}
	return 0
}

// fail prints a fatal error to stderr, prefixed per plan convention, and
// returns the exit code callers should use. Nothing must be written to
// stdout before this is called.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "statusloom: %s\n", err)
	return 1
}

// parseWidth parses the COLUMNS environment value. An empty or unparsable
// value yields 0 ("unknown width"), matching render.Options.Width's zero
// meaning.
func parseWidth(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// applyRepoCache sets snap.Repository to the last git status the refresh
// worker collected for the session's cwd, whatever its age (nil when none has
// been collected yet). The render path never runs git: collection happens only
// in the detached `refresh --once` worker on the per-cwd schedule
// (RefreshSchedule.Git), which renderDocFromRaw requests through
// maybeStartRefresh. So git info lags by at most one render, and the first
// render in a new cwd has none unless the document waits for it
// (<refresh wait="git">, see waitForRefresh). It is a no-op when the session did not report
// a cwd: statusloom's contract is stdin-driven, so it never falls back to
// os.Getwd.
//
// The repo cache is keyed by the session's cwd rather than by the git
// repository root, which is only known once git has resolved it. Several cwd
// values inside one repository simply get independent, equivalent entries.
func applyRepoCache(snap *schema.StatusSnapshot) {
	if snap.System.Cwd == "" {
		return
	}
	snap.Repository = cache.LoadRepo(snap.System.Cwd)
}

// storeSessionSnapshot best-effort persists the fully-populated snapshot
// (post account/git enrichment) to the session snapshot cache, so the
// config UI's "real data preview" feature can render against an actual
// recent session instead of only synthetic samples. This is purely a
// local, network-free disk write - it does not affect what gets rendered
// to stdout and must never fail or slow down a render.
//
// It is a no-op when the session did not report an ID (nothing to key the
// cache entry by).
func storeSessionSnapshot(snap *schema.StatusSnapshot, now time.Time) {
	if snap.Session.ID == "" {
		return
	}
	_ = cache.StoreSnapshot(snap.Session.ID, *snap, now)
}
