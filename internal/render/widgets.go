package render

import (
	"path/filepath"
	"strconv"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/schema"
)

// autoReserveFraction is the fraction of the context window reserved for
// auto-compact headroom when ContextConfig.ReserveTokens is 0 (auto).
// 16.5% of a 200k window is 33,000 tokens; it scales for a 1M window.
const autoReserveFraction = 0.165

// renderContent renders a single content widget to its display string.
// An empty string means the widget is hidden (no data / not applicable).
//
// Layout widgets ("separator", "flex-separator") are handled by the
// caller and never reach this function.
func renderContent(spec config.WidgetSpec, snap schema.StatusSnapshot, cfg config.ToolConfig, opts Options, compact bool) string {
	sess := snap.Session
	switch spec.Type {
	case "compaction-count", "compaction-auto", "compaction-manual", "compaction-unknown", "compaction-tokens-reclaimed", "session-input-tokens", "session-output-tokens", "session-cache-creation-tokens", "session-cache-read-tokens", "session-total-tokens":
		v, ok := metricValue(spec.Type, snap, cfg, opts)
		if !ok {
			return ""
		}
		return renderInteger(int(v), spec.RawValue, compact)
	case "input-token-speed", "output-token-speed", "total-token-speed":
		v, ok := metricValue(spec.Type, snap, cfg, opts)
		if !ok {
			return ""
		}
		return strconv.FormatFloat(v, 'f', 1, 64)
	case "model":
		if sess.Model == nil {
			return ""
		}
		return sess.Model.DisplayName
	case "model-id":
		if sess.Model == nil {
			return ""
		}
		return sess.Model.ID
	case "output-style":
		if sess.OutputStyle == nil {
			return ""
		}
		return *sess.OutputStyle
	case "session-id":
		return sess.ID
	case "thinking-enabled":
		if sess.ThinkingEnabled == nil {
			return ""
		}
		return strconv.FormatBool(*sess.ThinkingEnabled)
	case "project-directory":
		return snap.System.ProjectDir
	case "git-root":
		if snap.Repository == nil {
			return ""
		}
		return snap.Repository.Root

	case "thinking-effort":
		if sess.ReasoningEffort == nil {
			return ""
		}
		return *sess.ReasoningEffort

	case "context-length":
		if sess.Context == nil {
			return ""
		}
		n := sess.Context.TotalInputTokens
		switch {
		case spec.RawValue:
			return strconv.Itoa(n)
		case compact:
			return formatCompactK(n)
		default:
			return formatThousands(n)
		}
	case "context-window-size":
		if sess.Context == nil {
			return ""
		}
		return renderInteger(sess.Context.WindowSize, spec.RawValue, compact)
	case "context-remaining":
		if sess.Context == nil || sess.Context.RemainingPercentage == nil {
			return ""
		}
		return formatPercent(*sess.Context.RemainingPercentage)
	case "context-output-tokens":
		if sess.Context == nil {
			return ""
		}
		return renderInteger(sess.Context.TotalOutputTokens, spec.RawValue, compact)
	case "current-input-tokens", "current-output-tokens", "cache-creation-tokens", "cache-read-tokens":
		if sess.Context == nil || sess.Context.Current == nil {
			return ""
		}
		n := map[string]int{"current-input-tokens": sess.Context.Current.Input, "current-output-tokens": sess.Context.Current.Output, "cache-creation-tokens": sess.Context.Current.CacheCreation, "cache-read-tokens": sess.Context.Current.CacheRead}[spec.Type]
		return renderInteger(n, spec.RawValue, compact)

	case "context-percentage":
		if sess.Context == nil || sess.Context.UsedPercentage == nil {
			return ""
		}
		return formatPercent(*sess.Context.UsedPercentage)

	case "context-percentage-usable":
		usable, ok := usablePercent(sess.Context, cfg.Context)
		if !ok {
			return ""
		}
		if cfg.Context.PercentageMode == "both" {
			return formatPercent(rawContextPercent(sess.Context)) + "/" + formatPercent(usable)
		}
		return formatPercent(usable)

	case "session-cost":
		if sess.Cost == nil {
			return ""
		}
		v := strconv.FormatFloat(sess.Cost.USD, 'f', 2, 64)
		if spec.RawValue {
			return v
		}
		return "$" + v

	case "git-branch":
		if snap.Repository == nil || snap.Repository.Branch == "" {
			return ""
		}
		return snap.Repository.Branch

	case "git-changes":
		if snap.Repository == nil {
			return ""
		}
		return "(+" + strconv.Itoa(snap.Repository.Added) + ",-" +
			strconv.Itoa(snap.Repository.Deleted) + ")"

	case "tool-version":
		if snap.Tool.Version == "" {
			return ""
		}
		return "v" + snap.Tool.Version

	case "current-directory":
		if snap.System.Cwd == "" {
			return ""
		}
		return filepath.Base(snap.System.Cwd)

	case "five-hour-usage":
		return usageWidget(snap.Account.FiveHour)

	case "weekly-usage":
		return usageWidget(snap.Account.SevenDay)

	case "five-hour-reset":
		return resetWidget(snap.Account.FiveHour, opts.Now)

	case "weekly-reset":
		return resetWidget(snap.Account.SevenDay, opts.Now)

	case "session-name":
		if sess.Name == nil || *sess.Name == "" {
			return ""
		}
		return *sess.Name

	case "agent-name":
		if sess.AgentName == nil || *sess.AgentName == "" {
			return ""
		}
		return *sess.AgentName

	case "vim-mode":
		if sess.VimMode == nil || *sess.VimMode == "" {
			return ""
		}
		return *sess.VimMode

	case "pr-number":
		if snap.PullRequest == nil {
			return ""
		}
		if spec.RawValue {
			return strconv.Itoa(snap.PullRequest.Number)
		}
		return "#" + strconv.Itoa(snap.PullRequest.Number)

	case "pr-review-state":
		if snap.PullRequest == nil || snap.PullRequest.ReviewState == "" {
			return ""
		}
		return snap.PullRequest.ReviewState

	case "repo-name":
		if snap.System.Repo == nil {
			return ""
		}
		if spec.RawValue {
			return snap.System.Repo.Name
		}
		return snap.System.Repo.Owner + "/" + snap.System.Repo.Name

	case "worktree":
		if snap.System.Worktree == "" {
			return ""
		}
		return snap.System.Worktree

	case "session-duration":
		if sess.Cost == nil {
			return ""
		}
		if spec.RawValue {
			return strconv.Itoa(int(sess.Cost.Duration / time.Second))
		}
		return formatDuration(sess.Cost.Duration)

	case "api-duration":
		if sess.Cost == nil {
			return ""
		}
		if spec.RawValue {
			return strconv.Itoa(int(sess.Cost.APIDuration / time.Second))
		}
		return formatDuration(sess.Cost.APIDuration)

	case "lines-changed":
		if sess.Cost == nil {
			return ""
		}
		return "(+" + strconv.Itoa(sess.Cost.LinesAdded) + ",-" +
			strconv.Itoa(sess.Cost.LinesRemoved) + ")"
	case "lines-added":
		if sess.Cost == nil {
			return ""
		}
		return renderInteger(sess.Cost.LinesAdded, spec.RawValue, compact)
	case "lines-removed":
		if sess.Cost == nil {
			return ""
		}
		return renderInteger(sess.Cost.LinesRemoved, spec.RawValue, compact)
	case "git-staged", "git-unstaged", "git-untracked", "git-ahead", "git-behind":
		if snap.Repository == nil {
			return ""
		}
		n := map[string]int{"git-staged": snap.Repository.Staged, "git-unstaged": snap.Repository.Unstaged, "git-untracked": snap.Repository.Untracked, "git-ahead": snap.Repository.Ahead, "git-behind": snap.Repository.Behind}[spec.Type]
		return renderInteger(n, spec.RawValue, compact)
	case "git-clean":
		if snap.Repository == nil {
			return ""
		}
		return strconv.FormatBool(!snap.Repository.Dirty)
	case "exceeds-200k":
		if sess.Context == nil {
			return ""
		}
		return strconv.FormatBool(sess.Context.Exceeds200K)

	case "cache-hit-rate":
		rate, ok := cacheHitRate(sess.Context)
		if !ok {
			return ""
		}
		return formatPercentValue(rate) + "%"

	default:
		return ""
	}
}

func renderInteger(n int, raw, compact bool) string {
	if raw {
		return strconv.Itoa(n)
	}
	if compact {
		return formatCompactK(n)
	}
	return formatThousands(n)
}

// cacheHitRate computes the cache-read hit rate of the last API call as a
// percentage: CacheRead / (Input + CacheCreation + CacheRead) * 100. ok is
// false when there is no context, no per-call breakdown, or a non-positive
// denominator.
func cacheHitRate(ctx *schema.ContextUsage) (float64, bool) {
	if ctx == nil || ctx.Current == nil {
		return 0, false
	}
	c := ctx.Current
	denom := c.Input + c.CacheCreation + c.CacheRead
	if denom <= 0 {
		return 0, false
	}
	return float64(c.CacheRead) / float64(denom) * 100, true
}

// usageWidget renders a rate-window usage percentage as a bare value,
// e.g. "27%". Labels such as "5h: " are not the renderer's business: they
// come from the widget's Template (the default preset uses "5h: {value}"
// / "7d: {value}"). RawValue is consequently a no-op for these widgets;
// the field remains accepted.
func usageWidget(w *schema.RateWindow) string {
	if w == nil {
		return ""
	}
	return strconv.Itoa(roundInt(w.UsedPercentage)) + "%"
}

// resetWidget renders the countdown to a rate window's reset time.
// Hidden when the window is nil or the injected clock is zero.
func resetWidget(w *schema.RateWindow, now time.Time) string {
	if w == nil || now.IsZero() {
		return ""
	}
	return countdown(now, w.ResetsAt)
}

// rawContextPercent returns the raw context percentage, preferring the
// stdin-provided UsedPercentage and falling back to tokens/window.
func rawContextPercent(ctx *schema.ContextUsage) float64 {
	if ctx.UsedPercentage != nil {
		return *ctx.UsedPercentage
	}
	if ctx.WindowSize <= 0 {
		return 0
	}
	return clampPercent(float64(ctx.TotalInputTokens) / float64(ctx.WindowSize) * 100)
}

// usablePercent computes the usable context percentage per plan §13.
// ok is false when the percentage cannot be computed (nil context or a
// zero window size).
func usablePercent(ctx *schema.ContextUsage, cc config.ContextConfig) (float64, bool) {
	if ctx == nil || ctx.WindowSize <= 0 {
		return 0, false
	}
	reserve := cc.ReserveTokens
	if reserve == 0 {
		reserve = roundInt(autoReserveFraction * float64(ctx.WindowSize))
	}
	usableSize := ctx.WindowSize - reserve
	if usableSize <= 0 {
		return 0, false
	}
	pct := float64(ctx.TotalInputTokens) / float64(usableSize) * 100
	return clampPercent(pct), true
}

// clampPercent clamps a percentage to [0, 100].
func clampPercent(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 100 {
		return 100
	}
	return f
}
