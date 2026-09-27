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
	"github.com/yacchi/statusloom/internal/claudeaccount"
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
	cache.ApplyAccountCache(accountCacheKey, &snap, now)
	cache.ApplyExtraUsageCache(accountCacheKey, &snap, now)
	applyTranscriptCache(&snap, now)
	applyGitStatus(&snap, config.DocumentGitConfig(doc), now)
	storeSessionSnapshot(&snap, now)
	maybeStartRefresh(raw, now)

	width := parseWidth(getenv("COLUMNS"))
	out := render.RenderDocumentString(snap, doc, render.Options{
		Width:   width,
		Now:     now,
		Env:     getenv,
		Profile: profileProvider,
	})
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
// to the saved document. Diagnostics (and a fallback notice, only when a
// draft was actually present but invalid) are written to stderr.
func loadDraftDocument(tool string, stderr io.Writer) (*dsl.Document, bool) {
	if !config.DraftExists(tool) {
		return nil, false
	}
	doc, diags, ok, err := config.LoadDraftDocument(tool)
	if err != nil {
		return nil, false
	}
	writeDiagnostics(stderr, tool+" draft", diags)
	if !ok {
		fmt.Fprintf(stderr, "statusloom: %s draft invalid; rendering the saved document\n", tool)
		return nil, false
	}
	return doc, true
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
