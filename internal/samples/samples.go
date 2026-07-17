// Package samples provides the synthetic schema.StatusSnapshot data used to
// preview a DSL document without a real Claude Code session: named "sample"
// snapshots (a mid-session snapshot with data for every widget, an
// just-started session, and subagentStatusLine task snapshots at different
// stages), keyed by name.
//
// This is the single source of truth for sample data: internal/webconfig's
// /api/dsl/preview and /api/dsl/fields endpoints and cmd/room-wasm's
// statusloomPreview both render against it, so a preview looks the same
// (and is exercised by the same fixtures) whether it runs in the local
// config UI or in the client-side Room WASM module
// (plans/room-site.md 3.4/3.5).
package samples

import (
	"sort"
	"time"

	"github.com/yacchi/statusloom/internal/adapters/claude"
	"github.com/yacchi/statusloom/internal/schema"
)

// Sample names accepted by Snapshot and (the two subagent ones) SubagentTasks.
const (
	Full              = "full"
	EarlySession      = "early-session"
	SubagentRunning   = "subagent-running"
	SubagentCompleted = "subagent-completed"
)

// Names returns every sample name Snapshot recognizes, sorted, for UI
// enumeration (e.g. a Room preview's sample picker).
func Names() []string {
	names := []string{Full, EarlySession, SubagentRunning, SubagentCompleted}
	sort.Strings(names)
	return names
}

// DefaultForSection is the sample name a preview falls back to when it does
// not name one explicitly, chosen by which region of the document is being
// previewed: the session-shaped Full sample for the main section, the
// subagentStatusLine-shaped SubagentRunning sample for the subagent section
// (one row per task in its <subagent> region).
func DefaultForSection(section string) string {
	if section == "subagent" {
		return SubagentRunning
	}
	return Full
}

// Snapshot builds the named sample StatusSnapshot. Rate-limit reset times are
// computed relative to now so preview countdowns look alive. ok is false for
// an unrecognized name.
func Snapshot(name string, now time.Time) (schema.StatusSnapshot, bool) {
	switch name {
	case Full:
		return fullSample(now), true
	case EarlySession:
		return earlySessionSample(now), true
	case SubagentRunning:
		return subagentRunningSample(now), true
	case SubagentCompleted:
		return subagentCompletedSample(now), true
	default:
		return schema.StatusSnapshot{}, false
	}
}

// SubagentTasks resolves a subagent-section preview sample name to its
// ordered per-task list (one rendered row per task in a <subagent> region
// preview). ok is false for a name that is neither subagent sample.
func SubagentTasks(name string, now time.Time) ([]schema.SubagentSnapshot, bool) {
	switch name {
	case SubagentRunning:
		return runningSubagentTasks(now), true
	case SubagentCompleted:
		return completedSubagentTasks(now), true
	default:
		return nil, false
	}
}

// fptr returns a pointer to v, for populating the optional float64 fields of
// schema.ExtraUsage in sample snapshots.
func fptr(v float64) *float64 {
	return &v
}

// fullSample is a mid-session snapshot with data for every widget: model,
// effort, thinking, context usage, cost, a dirty repository, and both
// rate-limit windows.
func fullSample(now time.Time) schema.StatusSnapshot {
	model := schema.ModelInfo{ID: "claude-opus-4-8", DisplayName: "Opus 4.8"}
	effort := "high"
	thinking := true
	outputStyle := "default"
	used := 32.0
	remaining := 68.0
	// Cost carries the work-volume/timing fields the session-duration,
	// api-duration, and lines-changed widgets read. USD is unchanged so the
	// session-cost preview stays "$1.23".
	cost := schema.CostUsage{
		USD:          1.23,
		Duration:     75 * time.Minute,
		APIDuration:  8*time.Minute + 30*time.Second,
		LinesAdded:   248,
		LinesRemoved: 57,
	}
	// Session identity fields (nil in a bare session): a custom name, a
	// running agent, and an active vim mode.
	sessionName := "auth-refactor"
	agentName := "code-reviewer"
	vimMode := "NORMAL"

	return schema.StatusSnapshot{
		Tool: schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.200"},
		Session: schema.SessionSnapshot{
			ID:              "sample-session-id",
			Model:           &model,
			Name:            &sessionName,
			AgentName:       &agentName,
			VimMode:         &vimMode,
			ReasoningEffort: &effort,
			ThinkingEnabled: &thinking,
			OutputStyle:     &outputStyle,
			Analytics:       &schema.SessionAnalytics{Compactions: 2, CompactionsAuto: 1, CompactionsManual: 1, TokensReclaimed: 28000, InputTokens: 12000, OutputTokens: 4000, CacheCreationTokens: 1000, CacheReadTokens: 30000, TotalTokens: 47000, InputTokensPerSecond: 3.2, OutputTokensPerSecond: 1.1, TotalTokensPerSecond: 12.5},
			Context: &schema.ContextUsage{
				TotalInputTokens:    64000,
				TotalOutputTokens:   1200,
				WindowSize:          200000,
				UsedPercentage:      &used,
				RemainingPercentage: &remaining,
				// Last-API-call breakdown feeds the cache-hit-rate widget:
				// 38000 / (1200 + 800 + 38000) = 95%.
				Current: &schema.TokenBreakdown{
					Input:         1200,
					Output:        400,
					CacheCreation: 800,
					CacheRead:     38000,
				},
			},
			Cost: &cost,
		},
		Repository: &schema.RepositorySnapshot{
			Root:      "/Users/dev/myapp",
			Branch:    "main",
			Dirty:     true,
			Staged:    2,
			Unstaged:  1,
			Untracked: 3,
			Added:     156,
			Deleted:   23,
			Ahead:     1,
		},
		PullRequest: &schema.PullRequestInfo{
			Number:      1234,
			URL:         "https://github.com/yacchi/statusloom/pull/1234",
			ReviewState: "approved",
		},
		Account: schema.AccountSnapshot{
			FiveHour:       &schema.RateWindow{UsedPercentage: 27, ResetsAt: now.Add(2 * time.Hour)},
			SevenDay:       &schema.RateWindow{UsedPercentage: 79, ResetsAt: now.Add(4 * 24 * time.Hour)},
			SevenDayOpus:   &schema.RateWindow{UsedPercentage: 63, ResetsAt: now.Add(3 * 24 * time.Hour)},
			SevenDaySonnet: &schema.RateWindow{UsedPercentage: 12, ResetsAt: now.Add(5 * 24 * time.Hour)},
			ExtraUsage: &schema.ExtraUsage{
				Enabled:         true,
				MonthlyLimitUSD: fptr(30),
				UsedCreditsUSD:  fptr(12.34),
				Utilization:     fptr(41),
			},
		},
		System: schema.SystemSnapshot{
			Cwd:        "/Users/dev/myapp",
			ProjectDir: "/Users/dev/myapp",
			Worktree:   "feature-auth",
			Repo:       &schema.RepoIdentity{Host: "github.com", Owner: "yacchi", Name: "statusloom"},
		},
		// Subagent carries one task's data so the "claude-code" catalog's
		// merged task-* fields (markup.md "subagent") also render a real
		// preview in GET /api/dsl/fields, instead of falling back to
		// previewFallback the way they would against a nil Subagent.
		Subagent: &runningSubagentTasks(now)[0],
	}
}

// earlySessionSample is a just-started session: same tool/model, but zero
// tokens, no computed percentages, zero cost, no repository, and no rate
// limit or reasoning-effort data yet.
func earlySessionSample(now time.Time) schema.StatusSnapshot {
	model := schema.ModelInfo{ID: "claude-opus-4-8", DisplayName: "Opus 4.8"}
	cost := schema.CostUsage{USD: 0}

	return schema.StatusSnapshot{
		Tool: schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.200"},
		Session: schema.SessionSnapshot{
			Model: &model,
			Context: &schema.ContextUsage{
				TotalInputTokens:  0,
				TotalOutputTokens: 0,
				WindowSize:        200000,
			},
			Cost: &cost,
		},
		System: schema.SystemSnapshot{
			Cwd:        "/Users/dev/myapp",
			ProjectDir: "/Users/dev/myapp",
		},
	}
}

// subagentSampleStarted anchors every subagent sample task to a fixed start
// time, so the "running" and "completed" samples read as snapshots of the
// same tasks' progress rather than unrelated ones, and previews are
// reproducible across renders.
var subagentSampleStarted = time.UnixMilli(1784104398889)

const subagentSampleModelID = "claude-opus-4-8"

// runningSubagentTasks is the ordered per-task sample data behind both the
// single-snapshot subagentRunningSample (task 0, for whole-tool / single-field
// previews) and the multi-row subagent-section preview (one rendered row per
// task — see SubagentTasks). Task 0 mirrors
// fixtures/claude/subagent-running.json's first task; tasks 1-2 are synthetic
// siblings at different stages/models, so a preview of the default document's
// <subagent> region (config.claudeCodeDefaultDocument) shows more than one row
// and exercises its width-gated stats (task-duration/-tokens/-context-percent)
// at different token counts.
func runningSubagentTasks(now time.Time) []schema.SubagentSnapshot {
	return []schema.SubagentSnapshot{
		{
			ID:                "b1a2c3d4e5f60718",
			Type:              "local_agent",
			Status:            "running",
			Description:       "Review render pipeline changes",
			Label:             "Review render pipeline changes",
			StartedAt:         subagentSampleStarted,
			ModelID:           subagentSampleModelID,
			ModelDisplay:      claude.PrettyModelName(subagentSampleModelID),
			ContextWindowSize: 200000,
			TokenCount:        28454,
			Cwd:               "/Users/dev/myapp",
		},
		{
			ID:                "c2b3d4e5f6071829",
			Type:              "local_agent",
			Status:            "running",
			Description:       "Fix flaky test in useDragEditing",
			Label:             "Fix flaky test in useDragEditing",
			StartedAt:         subagentSampleStarted.Add(2 * time.Minute),
			ModelID:           "claude-sonnet-4-6",
			ModelDisplay:      claude.PrettyModelName("claude-sonnet-4-6"),
			ContextWindowSize: 200000,
			TokenCount:        9120,
			Cwd:               "/Users/dev/myapp",
		},
		{
			ID:                "d3c4e5f607182930",
			Type:              "local_agent",
			Status:            "running",
			Description:       "Update DSL_API.md preview contract",
			Label:             "Update DSL_API.md preview contract",
			StartedAt:         subagentSampleStarted.Add(4 * time.Minute),
			ModelID:           subagentSampleModelID,
			ModelDisplay:      claude.PrettyModelName(subagentSampleModelID),
			ContextWindowSize: 200000,
			TokenCount:        640,
			Cwd:               "/Users/dev/myapp",
		},
	}
}

// completedSubagentTasks is runningSubagentTasks's tasks once each has
// finished (status flipped, token count increased), mirroring
// fixtures/claude/subagent-completed.json's relationship to
// subagent-running.json.
func completedSubagentTasks(now time.Time) []schema.SubagentSnapshot {
	tasks := runningSubagentTasks(now)
	tasks[0].Status, tasks[0].TokenCount = "completed", 28663
	tasks[1].Status, tasks[1].TokenCount = "completed", 9420
	tasks[2].Status, tasks[2].TokenCount = "completed", 812
	return tasks
}

// subagentRunningSample is a subagentStatusLine preview snapshot (one
// agent-panel row) for a task still in progress: runningSubagentTasks's first
// task, mirroring fixtures/claude/subagent-running.json's first task. Used
// for the whole-tool SubagentRunning sample name (Snapshot) and as the base
// for GET /api/dsl/fields' single-field task-* previews (fullSample's
// embedded Subagent).
func subagentRunningSample(now time.Time) schema.StatusSnapshot {
	return schema.StatusSnapshot{
		Tool: schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.210"},
		System: schema.SystemSnapshot{
			Cwd: "/Users/dev/myapp",
		},
		Subagent: &runningSubagentTasks(now)[0],
	}
}

// subagentCompletedSample is subagentRunningSample's task once it has
// finished, mirroring fixtures/claude/subagent-completed.json's first task.
func subagentCompletedSample(now time.Time) schema.StatusSnapshot {
	snap := subagentRunningSample(now)
	completed := completedSubagentTasks(now)[0]
	snap.Subagent = &completed
	return snap
}
