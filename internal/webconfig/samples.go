package webconfig

import (
	"time"

	"github.com/yacchi/statusloom/internal/adapters/claude"
	"github.com/yacchi/statusloom/internal/schema"
)

// sampleName identifiers accepted by POST /api/dsl/preview's "sample" field.
const (
	sampleFull              = "full"
	sampleEarlySession      = "early-session"
	sampleSubagentRunning   = "subagent-running"
	sampleSubagentCompleted = "subagent-completed"
)

// defaultSampleForTool is the sample name handlePreviewDSL and
// handleDSLFields fall back to when the request/context does not name one
// explicitly: a session-shaped sample for tool="claude-code", a
// subagentStatusLine-shaped one for tool="claude-code-subagent".
func defaultSampleForTool(tool string) string {
	if tool == string(schema.ToolClaudeCodeSubagent) {
		return sampleSubagentRunning
	}
	return sampleFull
}

// sampleSnapshot builds the named sample StatusSnapshot. Rate-limit reset
// times are computed relative to now so preview countdowns look alive.
// ok is false for an unrecognized name.
func sampleSnapshot(name string, now time.Time) (schema.StatusSnapshot, bool) {
	switch name {
	case sampleFull:
		return fullSample(now), true
	case sampleEarlySession:
		return earlySessionSample(now), true
	case sampleSubagentRunning:
		return subagentRunningSample(now), true
	case sampleSubagentCompleted:
		return subagentCompletedSample(now), true
	default:
		return schema.StatusSnapshot{}, false
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

// subagentSampleModel and subagentSampleStarted anchor both subagent samples
// to the same task identity (fixtures/claude/subagent-running.json's first
// task), so the "running" and "completed" samples read as two snapshots of
// one task's progress rather than unrelated tasks.
var subagentSampleStarted = time.UnixMilli(1784104398889)

const subagentSampleModelID = "claude-opus-4-8"

// subagentRunningSample is a subagentStatusLine preview snapshot (one
// agent-panel row) for a task still in progress, mirroring
// fixtures/claude/subagent-running.json's first task.
func subagentRunningSample(now time.Time) schema.StatusSnapshot {
	return schema.StatusSnapshot{
		Tool: schema.ToolSnapshot{ID: schema.ToolClaudeCodeSubagent, Version: "2.1.210"},
		System: schema.SystemSnapshot{
			Cwd: "/Users/dev/myapp",
		},
		Subagent: &schema.SubagentSnapshot{
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
	}
}

// subagentCompletedSample is the same task as subagentRunningSample once it
// has finished, mirroring fixtures/claude/subagent-completed.json's first
// task.
func subagentCompletedSample(now time.Time) schema.StatusSnapshot {
	snap := subagentRunningSample(now)
	snap.Subagent.Status = "completed"
	snap.Subagent.TokenCount = 28663
	return snap
}
