// Package render turns a normalized status snapshot and a configuration DSL
// document (internal/dsl) into the display lines consumed by a coding agent's
// statusLine feature. Each returned row carries no trailing newline; the
// caller prints one line per row.
//
// The public entry points live in doc.go (RenderDocument /
// RenderDocumentString), which evaluate a dsl.Document's active layout into
// styled spans and then ANSI, per markup.md
// ("DSL -> AST -> evaluation -> styled spans -> ANSI renderer"). This file
// holds the shared layout plumbing those entry points reuse: separator
// collapsing (markSeparators), flex sizing (computeFlexWidths /
// resolveFlexTarget), metric resolution for when/color-rule expressions and
// formatter inputs (metricValue), OSC 8 hyperlink targets (hyperlinkURL), and
// the all-omitted fallback line (RenderFallback).
//
// # Separator collapsing
//
// A role="separator" <text> is kept only when it sits directly between two
// visible pieces; leading, trailing, and doubled separators are dropped. A
// <flex/> is always retained and expands to fill horizontal space; its target
// width is configured per node via its size ("" = "full" = terminal width,
// "full-minus-N" = width-N), and when a line has several flex nodes the
// minimum resolved target wins for the whole line.
//
// # Color
//
// Colors (ANSI16 name, "ansi256:N", or "#rrggbb") are downgraded to the
// terminal's ColorLevel: truecolor -> nearest ansi256 -> nearest ansi16. At
// ColorLevel "none" no escape sequences are emitted at all.
package render

import (
	"strconv"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/schema"
)

// Options carries render-time context that is not part of the snapshot or
// configuration.
type Options struct {
	Width int       // terminal columns (resolved from COLUMNS); 0 = unknown
	Now   time.Time // injected clock for countdown widgets (testability)
}

// RenderFallback renders a built-in minimal single line used when the active
// layout would print nothing. It composes "model | tool-version" through the
// shared separator-collapsing plumbing (so the separator drops when a side is
// missing) and honors the document's ColorLevel. The result is guaranteed
// non-empty: with neither model nor version available it returns a plain
// literal so the status line is never blank.
//
// The blank condition is detected from RenderDocument: it holds when every
// DocLine.Omitted is true (or the slice is empty). RenderDocumentString calls
// this in that case.
func RenderFallback(snap schema.StatusSnapshot, cfg config.ToolConfig, opts Options) string {
	level := parseColorLevel(cfg.ColorLevel)
	compact := cfg.CompactThreshold > 0 && opts.Width > 0 && opts.Width < cfg.CompactThreshold

	modelText := renderContent(config.WidgetSpec{Type: "model"}, snap, cfg, opts, compact)
	verText := renderContent(config.WidgetSpec{Type: "tool-version"}, snap, cfg, opts, compact)

	pieces := []piece{
		{kind: pkContent, plain: modelText, visible: modelText != ""},
		{kind: pkSeparator, plain: " | "},
		{kind: pkContent, plain: verText, visible: verText != ""},
	}
	markSeparators(pieces)

	// Only the model is styled (cyan), matching the historical fallback line.
	styles := []docStyle{{color: "cyan"}, {}, {}}
	var b strings.Builder
	for i, p := range pieces {
		if !p.visible {
			continue
		}
		b.WriteString(stylizeDoc(p.plain, styles[i], level))
	}
	if s := b.String(); s != "" {
		return s
	}
	// Last resort: neither model nor version is available.
	return "statusloom"
}

// pieceKind classifies a rendered node within a line.
type pieceKind int

const (
	pkContent pieceKind = iota
	pkSeparator
	pkFlex
)

// piece is a rendered node ready for layout. plain is the display text before
// any ANSI styling (used for width accounting); visible marks whether the
// piece contributes to the printed line; flex is a flex node's size value.
// It carries only the fields markSeparators and computeFlexWidths need; the
// caller keeps the full style/link decoration alongside (see doc.go).
type piece struct {
	kind    pieceKind
	plain   string
	visible bool
	flex    string
}

// markSeparators sets the visibility of separator pieces: a separator is
// visible only when the nearest preceding visible piece is content and a
// visible content piece or flex separator still follows. This collapses
// leading, trailing, and doubled separators, and drops a separator whose
// neighboring content is hidden.
func markSeparators(pieces []piece) {
	// prev tracks the kind of the nearest preceding visible piece; the
	// initial pkFlex value suppresses a separator at line start just as a
	// flex or another separator would.
	prev := pkFlex
	for i := range pieces {
		p := &pieces[i]
		if p.kind != pkSeparator {
			if p.visible {
				prev = p.kind
			}
			continue
		}
		if prev != pkContent {
			continue // stays invisible
		}
		later := false
		for j := i + 1; j < len(pieces); j++ {
			q := pieces[j]
			if q.kind == pkFlex || (q.kind == pkContent && q.visible) {
				later = true
				break
			}
		}
		if !later {
			continue
		}
		p.visible = true
		prev = pkSeparator
	}
}

// computeFlexWidths assigns a space count to each flex separator in the line.
// The returned slice is indexed by flex-separator order.
//
// Each flex separator carries its own target via its size ("" or "full"
// resolve to the terminal width, "full-minus-N" to width-N); the line's
// target is the MINIMUM resolved target among its flex separators. When the
// terminal width is unknown (<= 0) each flex renders as a single space.
// Otherwise the remaining columns after fixed content are shared evenly across
// the flex separators; if content already meets or exceeds the target, each
// flex renders as a single space.
func computeFlexWidths(pieces []piece, width int) []int {
	n := 0
	target := 0
	for _, p := range pieces {
		if p.kind != pkFlex {
			continue
		}
		t := resolveFlexTarget(p.flex, width)
		if n == 0 || t < target {
			target = t
		}
		n++
	}
	if n == 0 {
		return nil
	}
	widths := make([]int, n)

	if width <= 0 {
		for i := range widths {
			widths[i] = 1
		}
		return widths
	}

	base := 0
	for _, p := range pieces {
		if p.visible && (p.kind == pkContent || p.kind == pkSeparator) {
			base += visibleWidth(p.plain)
		}
	}
	remaining := target - base
	if remaining < n {
		for i := range widths {
			widths[i] = 1
		}
		return widths
	}
	per := remaining / n
	extra := remaining % n
	for i := range widths {
		widths[i] = per
		if i < extra {
			widths[i]++
		}
	}
	return widths
}

// resolveFlexTarget resolves one flex separator's size to a target line width.
// "" and "full" mean the full terminal width; "full-minus-N" subtracts N.
// Unparseable values (which validation rejects) fall back to the full width so
// rendering never fails.
func resolveFlexTarget(flex string, width int) int {
	if strings.HasPrefix(flex, "full-minus-") {
		if n, err := strconv.Atoi(flex[len("full-minus-"):]); err == nil {
			return width - n
		}
	}
	return width
}

// metricValue resolves a named metric to a numeric value for when-expression
// and color-rule evaluation (via docResolver) and for formatter inputs. ok is
// false when the metric is unknown or its backing data is absent (nil section,
// or a reset countdown with a zero clock). It reuses the same computations as
// the corresponding fields so thresholds compare against exactly what is
// shown.
func metricValue(name string, snap schema.StatusSnapshot, cfg config.ToolConfig, opts Options) (float64, bool) {
	sess := snap.Session
	switch name {
	case "compaction-count", "compaction-auto", "compaction-manual", "compaction-unknown", "compaction-tokens-reclaimed", "session-input-tokens", "session-output-tokens", "session-cache-creation-tokens", "session-cache-read-tokens", "session-total-tokens", "input-token-speed", "output-token-speed", "total-token-speed":
		if sess.Analytics == nil {
			return 0, false
		}
		a := sess.Analytics
		return map[string]float64{"compaction-count": float64(a.Compactions), "compaction-auto": float64(a.CompactionsAuto), "compaction-manual": float64(a.CompactionsManual), "compaction-unknown": float64(a.CompactionsUnknown), "compaction-tokens-reclaimed": float64(a.TokensReclaimed), "session-input-tokens": float64(a.InputTokens), "session-output-tokens": float64(a.OutputTokens), "session-cache-creation-tokens": float64(a.CacheCreationTokens), "session-cache-read-tokens": float64(a.CacheReadTokens), "session-total-tokens": float64(a.TotalTokens), "input-token-speed": a.InputTokensPerSecond, "output-token-speed": a.OutputTokensPerSecond, "total-token-speed": a.TotalTokensPerSecond}[name], true
	case "context-percent":
		if sess.Context == nil || (sess.Context.UsedPercentage == nil && sess.Context.WindowSize <= 0) {
			return 0, false
		}
		return rawContextPercent(sess.Context), true
	case "context-usable-percent":
		return usablePercent(sess.Context, cfg.Context)
	case "context-tokens":
		if sess.Context == nil {
			return 0, false
		}
		return float64(sess.Context.TotalInputTokens), true
	case "context-window-tokens":
		if sess.Context == nil {
			return 0, false
		}
		return float64(sess.Context.WindowSize), true
	case "context-remaining-percent":
		if sess.Context == nil || sess.Context.RemainingPercentage == nil {
			return 0, false
		}
		return *sess.Context.RemainingPercentage, true
	case "context-output-tokens":
		if sess.Context == nil {
			return 0, false
		}
		return float64(sess.Context.TotalOutputTokens), true
	case "current-input-tokens", "current-output-tokens", "cache-creation-tokens", "cache-read-tokens":
		if sess.Context == nil || sess.Context.Current == nil {
			return 0, false
		}
		return map[string]float64{"current-input-tokens": float64(sess.Context.Current.Input), "current-output-tokens": float64(sess.Context.Current.Output), "cache-creation-tokens": float64(sess.Context.Current.CacheCreation), "cache-read-tokens": float64(sess.Context.Current.CacheRead)}[name], true
	case "five-hour-percent":
		if snap.Account.FiveHour == nil {
			return 0, false
		}
		return snap.Account.FiveHour.UsedPercentage, true
	case "seven-day-percent":
		if snap.Account.SevenDay == nil {
			return 0, false
		}
		return snap.Account.SevenDay.UsedPercentage, true
	case "five-hour-reset-minutes":
		return resetMinutes(snap.Account.FiveHour, opts.Now)
	case "seven-day-reset-minutes":
		return resetMinutes(snap.Account.SevenDay, opts.Now)
	case "session-cost-usd":
		if sess.Cost == nil {
			return 0, false
		}
		return sess.Cost.USD, true
	case "session-duration-minutes":
		if sess.Cost == nil {
			return 0, false
		}
		return sess.Cost.Duration.Minutes(), true
	case "api-duration-minutes":
		if sess.Cost == nil {
			return 0, false
		}
		return sess.Cost.APIDuration.Minutes(), true
	case "lines-added":
		if sess.Cost == nil {
			return 0, false
		}
		return float64(sess.Cost.LinesAdded), true
	case "lines-removed":
		if sess.Cost == nil {
			return 0, false
		}
		return float64(sess.Cost.LinesRemoved), true
	case "lines-changed-total":
		if sess.Cost == nil {
			return 0, false
		}
		return float64(sess.Cost.LinesAdded + sess.Cost.LinesRemoved), true
	case "git-staged", "git-unstaged", "git-untracked", "git-ahead", "git-behind":
		if snap.Repository == nil {
			return 0, false
		}
		return map[string]float64{"git-staged": float64(snap.Repository.Staged), "git-unstaged": float64(snap.Repository.Unstaged), "git-untracked": float64(snap.Repository.Untracked), "git-ahead": float64(snap.Repository.Ahead), "git-behind": float64(snap.Repository.Behind)}[name], true
	case "cache-hit-percent":
		return cacheHitRate(sess.Context)
	default:
		return 0, false
	}
}

// resetMinutes returns the whole-minute count remaining until a rate window's
// reset, floored at zero for past resets. ok is false when the window is nil
// or the injected clock is zero, matching the reset field's visibility rule.
func resetMinutes(w *schema.RateWindow, now time.Time) (float64, bool) {
	if w == nil || now.IsZero() {
		return 0, false
	}
	d := w.ResetsAt.Sub(now)
	if d < 0 {
		d = 0
	}
	return d.Minutes(), true
}

// hyperlinkURL returns the OSC 8 target for a linkable field, or "" when no
// well-formed URL is available. pr-number and pr-review-state link to the pull
// request URL; repo-name links to the repository's web page, built only when
// host, owner, and name are all present.
func hyperlinkURL(fieldName string, snap schema.StatusSnapshot) string {
	switch fieldName {
	case "pr-number", "pr-review-state":
		if snap.PullRequest == nil {
			return ""
		}
		return snap.PullRequest.URL
	case "repo-name":
		r := snap.System.Repo
		if r == nil || r.Host == "" || r.Owner == "" || r.Name == "" {
			return ""
		}
		return "https://" + r.Host + "/" + r.Owner + "/" + r.Name
	default:
		return ""
	}
}
