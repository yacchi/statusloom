package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/adapters/claude"
	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/detect"
	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/gitstatus"
	"github.com/yacchi/statusloom/internal/render"
	"github.com/yacchi/statusloom/internal/schema"
)

// maxStdinBytes caps how much of stdin the render pipeline will read, per
// statusloom-local-development-plan.md section 3.1.
const maxStdinBytes = 10 << 20 // 10MB

// accountCacheKey is the fixed account cache key used until statusloom can
// distinguish multiple accounts (plan section 11: stdin carries no account
// identifier in v0.1).
const accountCacheKey = "default"

// accountCacheTTL is the informational freshness window written into a
// stdin-sourced account cache entry's ExpiresAt.
const accountCacheTTL = 5 * time.Minute

// unsupportedToolError is returned by renderDocFromRaw when a tool is detected
// (or forced) that statusloom cannot render yet. It is a distinct type so
// callers can reproduce the exact legacy stderr wording ("tool %q not
// implemented yet", without the "statusloom:" prefix that fail adds).
type unsupportedToolError struct{ tool schema.ToolID }

func (e unsupportedToolError) Error() string {
	return fmt.Sprintf("tool %q not implemented yet", e.tool)
}

// runRenderPipeline implements the render pipeline described in plan
// section 3.1/5: read stdin, detect the tool, decode, load config, fold in
// the account/repo caches, and render. explicitTool is "" for `statusloom
// render` without --tool, or a fixed tool id for dedicated subcommands
// (e.g. "claude-code" for `statusloom claude`).
func runRenderPipeline(stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, explicitTool string) int {
	raw, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes))
	if err != nil {
		return fail(stderr, fmt.Errorf("reading stdin: %w", err))
	}

	lines, err := renderDocFromRaw(raw, getenv, explicitTool, stderr, false)
	return writeRenderResult(stdout, stderr, lines, nil, err)
}

// renderDocFromRaw is the DSL render path shared by `statusloom claude`,
// `statusloom render`, and `statusloom monitor`: it loads the <tool>.xml
// document (auto-migrating from a legacy config.json when the document is
// absent), renders it, and returns the joined output lines. Diagnostics and
// migration notices are written to stderr directly; a document with error
// diagnostics (or an unreadable/invalid one) never crashes or blanks the
// status line — it falls back to DefaultDocument.
//
// When draft is true (`statusloom monitor --draft`), the shared draft
// <tool>.draft.xml is rendered instead, falling back to the saved document when
// the draft is absent or invalid. The saved-document path (draft=false) keeps
// `statusloom claude` output byte-identical regardless of caller.
//
// It performs no network I/O (only the local account/repo/session caches and
// git status collection), keeping the render path network-free.
func renderDocFromRaw(raw []byte, getenv func(string) string, explicitTool string, stderr io.Writer, draft bool) ([]string, error) {
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
	// Explicit auto-migration step (kept out of LoadDocument for testability):
	// generate <tool>.xml from a legacy config.json the first time it is seen.
	autoMigrate(tool, stderr)

	doc := resolveRenderDocument(tool, draft, stderr)

	now := time.Now()
	applyAccountCache(&snap, now)
	applyTranscriptCache(&snap, now)
	applyGitStatus(&snap, config.DocumentGitConfig(doc), now)
	storeSessionSnapshot(&snap, now)
	maybeStartRefresh(raw, now)

	width := parseWidth(getenv("COLUMNS"))
	out := render.RenderDocumentString(snap, doc, render.Options{Width: width, Now: now})
	if out == "" {
		return nil, nil
	}
	return []string{out}, nil
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
// draft=true it prefers the shared draft (falling back to the saved document
// when the draft is absent or invalid); otherwise it loads the saved
// <tool>.xml. It always returns a non-nil, renderable document, falling back to
// the built-in DefaultDocument when the chosen source has error diagnostics or
// cannot be read (markup.md "fallback"). Diagnostics are written to stderr.
func resolveRenderDocument(tool string, draft bool, stderr io.Writer) *dsl.Document {
	if draft {
		if doc, ok := loadDraftDocument(tool, stderr); ok {
			return doc
		}
	}
	doc, diags, err := config.LoadDocument(tool)
	if err != nil {
		fmt.Fprintf(stderr, "statusloom: cannot load %s.xml: %v\n", tool, err)
		doc, _ = dsl.Parse(config.DefaultDocument(tool))
		return doc
	}
	writeDiagnostics(stderr, tool, diags)
	if doc == nil || doc.Root == nil || dsl.HasErrors(diags) {
		doc, _ = dsl.Parse(config.DefaultDocument(tool))
	}
	return doc
}

// loadDraftDocument parses the shared draft <tool>.draft.xml. ok is true only
// when a draft file exists and parses without error-severity diagnostics;
// otherwise the caller falls back to the saved document. Diagnostics (and a
// fallback notice) are written to stderr.
func loadDraftDocument(tool string, stderr io.Writer) (*dsl.Document, bool) {
	src, exists, err := config.LoadDraftDocumentSource(tool)
	if err != nil || !exists {
		return nil, false
	}
	doc, diags := dsl.Parse(src)
	if doc != nil && doc.Root != nil {
		diags = append(diags, dsl.Validate(doc)...)
	}
	writeDiagnostics(stderr, tool+".draft", diags)
	if doc == nil || doc.Root == nil || dsl.HasErrors(diags) {
		fmt.Fprintf(stderr, "statusloom: %s.draft.xml invalid; rendering the saved document\n", tool)
		return nil, false
	}
	return doc, true
}

// autoMigrate generates <tool>.xml from a legacy config.json when the DSL
// document is absent but the legacy config is present. It writes the migrated
// document atomically and prints a one-line notice plus any conversion
// warnings to stderr. The legacy config.json is left in place. All failures
// are surfaced as warnings and never abort the render.
func autoMigrate(tool string, stderr io.Writer) {
	if config.DocumentExists(tool) {
		return
	}
	legacy, ok, err := config.LoadLegacyConfig()
	if err != nil {
		fmt.Fprintf(stderr, "statusloom: cannot read legacy config for migration: %v\n", err)
		return
	}
	if !ok {
		return // no legacy config; LoadDocument will use the built-in default
	}
	doc, warnings := config.MigrateFromLegacy(*legacy, tool)
	if err := config.SaveDocumentSource(tool, doc.Source); err != nil {
		fmt.Fprintf(stderr, "statusloom: auto-migration failed to write %s: %v\n", config.DocumentPath(tool), err)
		return
	}
	fmt.Fprintf(stderr, "statusloom: migrated config.json to %s (original left in place)\n", config.DocumentPath(tool))
	for _, w := range warnings {
		fmt.Fprintf(stderr, "statusloom: migration: %s\n", w)
	}
}

// writeDiagnostics prints DSL parse/validation diagnostics to stderr, one per
// line, prefixed with the document name.
func writeDiagnostics(stderr io.Writer, tool string, diags []dsl.Diagnostic) {
	for _, d := range diags {
		fmt.Fprintf(stderr, "statusloom: %s.xml: %s: %s\n", tool, d.Severity, d.Message)
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

// applyAccountCache folds account-scoped rate-limit state (five-hour and
// seven-day usage) between the current snapshot and the shared account
// cache, per plan section 3.3:
//
//  1. Any window present in the stdin-derived snapshot is stored to the
//     cache so other concurrently running sessions can observe it.
//  2. Any window absent from the snapshot is filled in from the cache, if
//     available there, and Account.Stale is set when that happens.
//
// Cache errors are deliberately ignored in both directions: the shared
// cache is a best-effort convenience, and a render must never fail (or
// even go stale-empty) just because the cache directory is unwritable or
// unreadable.
func applyAccountCache(snap *schema.StatusSnapshot, now time.Time) {
	if snap.Account.FiveHour != nil || snap.Account.SevenDay != nil {
		u := cache.AccountUsage{
			Source:     "claude-code-stdin",
			ObservedAt: now,
			ExpiresAt:  now.Add(accountCacheTTL),
		}
		if snap.Account.FiveHour != nil {
			u.FiveHour = toRateWindowState(snap.Account.FiveHour)
		}
		if snap.Account.SevenDay != nil {
			u.SevenDay = toRateWindowState(snap.Account.SevenDay)
		}
		_ = cache.StoreAccount(accountCacheKey, u)
	}

	needFiveHour := snap.Account.FiveHour == nil
	needSevenDay := snap.Account.SevenDay == nil
	if !needFiveHour && !needSevenDay {
		return
	}

	cached, err := cache.LoadAccount(accountCacheKey)
	if err != nil || cached == nil {
		return
	}

	// Skip cached windows whose ResetsAt is not after now: the rate-limit
	// window they describe has since reset, so its UsedPercentage is
	// meaningless and the countdown widget would render a permanent "0m".
	// This applies only to cache fills - windows arriving via stdin are
	// Claude's ground truth and are displayed as-is.
	filled := false
	if needFiveHour && cached.FiveHour != nil && cached.FiveHour.ResetsAt.After(now) {
		snap.Account.FiveHour = toSchemaRateWindow(cached.FiveHour)
		filled = true
	}
	if needSevenDay && cached.SevenDay != nil && cached.SevenDay.ResetsAt.After(now) {
		snap.Account.SevenDay = toSchemaRateWindow(cached.SevenDay)
		filled = true
	}
	if filled {
		snap.Account.Stale = true
	}
}

func toRateWindowState(w *schema.RateWindow) *cache.RateWindowState {
	return &cache.RateWindowState{UsedPercentage: w.UsedPercentage, ResetsAt: w.ResetsAt}
}

func toSchemaRateWindow(w *cache.RateWindowState) *schema.RateWindow {
	return &schema.RateWindow{UsedPercentage: w.UsedPercentage, ResetsAt: w.ResetsAt}
}

// applyGitStatus resolves snap.Repository via the repo status cache and,
// on a cache miss/stale entry, a live gitstatus.Collect run, per plan
// sections 3.1/12. It is a no-op when the session did not report a cwd
// (snap.System.Cwd == ""): statusloom's contract is stdin-driven, so it
// deliberately never falls back to os.Getwd.
//
// The repo cache is keyed by the session's cwd rather than by the git
// repository root. The root is only known once git has actually resolved
// it (via `rev-parse --show-toplevel`), so there is no way to key by root
// before running git in the first place. Multiple cwd values that happen
// to resolve to the same repository root simply get independent cache
// entries with equivalent content; this is harmless (a little redundant
// storage), not a correctness problem.
func applyGitStatus(snap *schema.StatusSnapshot, gitCfg config.GitConfig, now time.Time) {
	cwd := snap.System.Cwd
	if cwd == "" {
		return
	}

	ttl := time.Duration(gitCfg.CacheTTLMs) * time.Millisecond
	timeout := time.Duration(gitCfg.TimeoutMs) * time.Millisecond

	cached, fresh, _ := cache.LoadRepo(cwd, ttl, now)
	if fresh {
		snap.Repository = cached
		return
	}

	result, err := gitstatus.Collect(context.Background(), cwd, gitstatus.Options{
		Timeout:          timeout,
		IncludeUntracked: gitCfg.IncludeUntracked,
		CollectNumstat:   gitCfg.CollectNumstat,
	})
	if err != nil {
		// git failed or timed out: fall back to whatever stale value the
		// cache had (nil if none). The statusline must stay quiet, so the
		// error itself is never surfaced; a debug env var is future work.
		snap.Repository = cached
		return
	}

	// success + nil means "not a git repository" - Repository stays nil.
	snap.Repository = result
	if result != nil {
		_ = cache.StoreRepo(cwd, *result, now)
	}
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
