// Package schema defines the normalized, tool-agnostic status snapshot
// model. Adapters decode tool-specific input (e.g. Claude Code statusLine
// stdin JSON) into these types so that the rest of the pipeline (cache,
// git integration, rendering) never needs to know which coding agent
// produced the data.
package schema

import "time"

// ToolID identifies the coding agent / tool a snapshot originated from.
type ToolID string

const (
	ToolClaudeCode         ToolID = "claude-code"
	ToolClaudeCodeSubagent ToolID = "claude-code-subagent"
	ToolCodex              ToolID = "codex"
	ToolCopilot            ToolID = "github-copilot"
)

// StatusSnapshot is the normalized, tool-agnostic representation of a
// single status-line render invocation.
type StatusSnapshot struct {
	Tool        ToolSnapshot
	Session     SessionSnapshot
	Repository  *RepositorySnapshot // nil = not in a git repo / not collected
	PullRequest *PullRequestInfo    // nil = no open PR for the current branch
	Account     AccountSnapshot
	System      SystemSnapshot
	Subagent    *SubagentSnapshot // nil = not a subagentStatusLine invocation
}

// SubagentSnapshot is the normalized data for a single task reported by
// Claude Code's subagentStatusLine feature (one agent-panel row).
type SubagentSnapshot struct {
	ID                string
	Type              string // "local_agent"
	Status            string // "running"|"completed"
	Description       string
	Label             string
	StartedAt         time.Time // decoded from startTime (epoch ms); zero = unset
	ModelID           string    // e.g. "claude-opus-4-8"
	ModelDisplay      string    // e.g. "Opus 4.8" (see claude.PrettyModelName)
	ContextWindowSize int
	TokenCount        int
	Effort            *string // nil today; reserved for future transcript-derived effort
	Cwd               string
}

// ToolSnapshot describes the tool that produced the snapshot.
type ToolSnapshot struct {
	ID      ToolID
	Version string // e.g. Claude Code version from stdin
}

// ModelInfo identifies the model in use for the current session.
type ModelInfo struct {
	ID          string
	DisplayName string
}

// SessionSnapshot holds session-scoped state that is read fresh on every
// invocation and never persisted across processes.
type SessionSnapshot struct {
	ID              string
	Name            *string // custom session name (--name / /rename); nil = unset
	AgentName       *string // agent.name; nil when not running with --agent
	VimMode         *string // "NORMAL"|"INSERT"|"VISUAL"|"VISUAL LINE"; nil when vim mode disabled
	Model           *ModelInfo
	ReasoningEffort *string // "low"|"medium"|"high"|"xhigh"|"max"; nil when absent
	ThinkingEnabled *bool
	Context         *ContextUsage
	Cost            *CostUsage
	OutputStyle     *string
	Analytics       *SessionAnalytics
}

// SessionAnalytics contains transcript-derived, asynchronously refreshed
// session totals. The render path only reads these values from the cache.
type SessionAnalytics struct {
	Compactions           int
	CompactionsAuto       int
	CompactionsManual     int
	CompactionsUnknown    int
	TokensReclaimed       int
	InputTokens           int
	OutputTokens          int
	CacheCreationTokens   int
	CacheReadTokens       int
	TotalTokens           int
	InputTokensPerSecond  float64
	OutputTokensPerSecond float64
	TotalTokensPerSecond  float64
}

// ContextUsage describes context-window consumption for the session.
type ContextUsage struct {
	TotalInputTokens    int
	TotalOutputTokens   int
	WindowSize          int
	UsedPercentage      *float64 // nil early in session
	RemainingPercentage *float64
	Exceeds200K         bool
	Current             *TokenBreakdown // last API call usage; nil before first API call and right after /compact
}

// TokenBreakdown is the per-category token usage of the last API call.
type TokenBreakdown struct {
	Input         int
	Output        int
	CacheCreation int
	CacheRead     int
}

// CostUsage holds session cost and work-volume statistics.
type CostUsage struct {
	USD          float64
	Duration     time.Duration // total wall-clock time since session start
	APIDuration  time.Duration // time spent waiting for API responses
	LinesAdded   int
	LinesRemoved int
}

// AccountSnapshot holds account-scoped rate-limit information that is
// shared across sessions via the local cache.
type AccountSnapshot struct {
	FiveHour       *RateWindow
	SevenDay       *RateWindow
	SevenDayOpus   *RateWindow
	SevenDaySonnet *RateWindow
	ExtraUsage     *ExtraUsage
	Stale          bool // true when values came from shared cache, not stdin
}

// RateWindow describes usage within a rolling rate-limit window.
type RateWindow struct {
	UsedPercentage float64
	ResetsAt       time.Time
}

// ExtraUsage holds subscription-overage ("extra usage" / usage credits)
// billing state, sourced from the authenticated OAuth usage API and shared
// across sessions via the local cache. Pointer fields are nil when the API
// reported null (e.g. usage credits disabled).
type ExtraUsage struct {
	Enabled         bool
	MonthlyLimitUSD *float64
	UsedCreditsUSD  *float64
	Utilization     *float64 // percent 0..100
	Stale           bool     // true when the value came from a cache entry past its fresh TTL
}

// RepositorySnapshot describes the state of the git repository the tool
// is currently operating in, if any.
type RepositorySnapshot struct {
	Root      string
	Branch    string
	Dirty     bool
	Staged    int
	Unstaged  int
	Untracked int
	Added     int // numstat added lines
	Deleted   int // numstat deleted lines
	Ahead     int
	Behind    int
}

// PullRequestInfo describes the open pull request for the current branch.
type PullRequestInfo struct {
	Number      int
	URL         string
	ReviewState string // "approved"|"pending"|"changes_requested"|"draft"; "" when absent
}

// RepoIdentity is the repository identity parsed from the origin remote.
type RepoIdentity struct {
	Host  string // e.g. "github.com"
	Owner string
	Name  string
}

// SystemSnapshot holds environment/filesystem context.
type SystemSnapshot struct {
	Cwd        string
	ProjectDir string
	Worktree   string        // linked git worktree name; "" = main working tree
	Repo       *RepoIdentity // nil outside a git repo or without origin remote
}
