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
	ToolClaudeCode ToolID = "claude-code"
	ToolCodex      ToolID = "codex"
	ToolCopilot    ToolID = "github-copilot"
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
	Subagent    *SubagentSnapshot // nil = not a subagent row render (claude-subagent or claude-agent-row)
	Mod         *ModSnapshot      // nil = no statusloom-mod record for this session
}

// SubagentSnapshot is the normalized data for a single task reported by
// Claude Code's subagentStatusLine feature (one agent-panel row).
type SubagentSnapshot struct {
	ID                string
	Type              string // "local_agent"
	Status            string // an AgentRow's status (else taskStatus), or the legacy subagentStatusLine status ("running"|"completed"|...)
	Description       string
	Label             string
	StartedAt         time.Time // decoded from startTime (epoch ms); zero = unset
	ModelID           string    // e.g. "claude-opus-4-8"
	ModelDisplay      string    // e.g. "Opus 4.8" (see claude.PrettyModelName)
	ContextWindowSize int
	TokenCount        int
	Cwd               string

	// The fields below come only from claude-agent-row (adapters/claude
	// DecodeAgentRow); the legacy subagentStatusLine never sends them, so they
	// stay empty/nil under claude-subagent. A nil pointer means "absent"
	// (never fabricated); a non-nil zero is a real reported value.
	Effort              *string  // reasoning effort: a string or a number, kept as its literal text
	AgentType           string   // AgentInfo.type, e.g. "Explore", "general-purpose"
	Name                string   // AgentInfo.name, used by SendMessage
	CostUSD             *float64 // estimated cost in US dollars
	Depth               *int     // nesting depth (main child = 0)
	InputTokens         *int     // cumulative input tokens
	OutputTokens        *int     // cumulative output tokens
	CacheReadTokens     *int     // cumulative cache-read tokens
	CacheCreationTokens *int     // cumulative cache-creation tokens
}

// ModSnapshot is what the statusloom Claude Code mod recorded for one session
// (nil on StatusSnapshot.Mod when none). "Main loop" means an event without
// agentId / agent_id; "running" means status pending|running|waiting.
type ModSnapshot struct {
	UpdatedAt      time.Time   `json:"updatedAt"`                // last successful mod-ingest
	Agents         *ModAgents  `json:"agents,omitempty"`         // nil until the first agents query
	LastTurn       *ModTurn    `json:"lastTurn,omitempty"`       // nil until a main-loop turn.complete
	PermissionMode string      `json:"permissionMode,omitempty"` // latest main-loop permission mode (agent.spawn)
	Context        *ModContext `json:"context,omitempty"`        // nil until a usage query carried a breakdown
	ModelSwitches  int         `json:"modelSwitches"`            // changes of the session model seen between main-loop turns
	SessionModel   string      `json:"sessionModel,omitempty"`   // claude.PrettyModelName of the session model at the latest turn
	TurnModel      string      `json:"turnModel,omitempty"`      // claude.PrettyModelName of the model that answered the latest turn; empty when unreported
}

// ModAgents is the agent summary from the mod's agents query.
type ModAgents struct {
	At               time.Time      `json:"at"`                // the query's "at"
	Running          int            `json:"running"`           // kind local_agent and running
	ByType           []ModTypeCount `json:"byType,omitempty"`  // running local agents by AgentInfo.type: count desc, then type asc
	CostUSD          *float64       `json:"costUsd,omitempty"` // sum of cost.usd of running local agents; nil when none reports one
	Tokens           int            `json:"tokens"`            // running local agents' usage[]: input+output+cache_read+cache_creation
	TeammatesRunning int            `json:"teammatesRunning"`  // kind in_process_teammate and running
	TeammatesIdle    int            `json:"teammatesIdle"`     // kind in_process_teammate and status idle
}

// ModTypeCount is a running-agent count for one AgentInfo.type.
type ModTypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// ModTurn is the latest main-loop turn outcome.
type ModTurn struct {
	Duration time.Duration `json:"duration"` // durationMs
	Reason   string        `json:"reason"`   // answer|aborted|refusal|error
}

// ModContext is the latest context-window breakdown from a usage query.
type ModContext struct {
	At                   time.Time            `json:"at"`                             // the query's "at"
	AutoCompactThreshold *int                 `json:"autoCompactThreshold,omitempty"` // nil when auto-compact is off or unreported
	TotalTokens          int                  `json:"totalTokens"`                    // breakdown.totalTokens (an estimate)
	Categories           []ModContextCategory `json:"categories,omitempty"`           // breakdown order
}

// ModContextCategory is one category of a ModContext breakdown.
type ModContextCategory struct {
	Name   string `json:"name"`
	Tokens int    `json:"tokens"`
	Kind   string `json:"kind"` // used|free|buffer|deferred
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
	PromptCache     *PromptCache // stdin prompt_cache; nil before the first API response or on Claude Code < v2.1.251
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

// PromptCache is Claude Code's stdin prompt_cache object: prompt-cache
// statistics of the session's main conversation.
type PromptCache struct {
	Warm                bool       // warm
	CachingObserved     bool       // caching_observed
	TTL                 string     // ttl: "5m" | "1h"; "" when absent
	ExpiresAt           *time.Time // expires_at; nil when null/absent
	Requests            int        // requests
	Misses              int        // misses
	ExpectedRebuilds    int        // expected_rebuilds
	HitRatio            *float64   // hit_ratio, 0..1; nil when null/absent
	CacheWriteTokens    int        // cache_write_tokens
	MissRecacheTokens   int        // miss_recache_tokens
	LastMissAt          *time.Time // last_miss_at; nil when null/absent
	LastMissCauses      []string   // last_miss_cause.causes; nil when last_miss_cause is null/absent
	RecacheTokensIfCold *int       // recache_tokens_if_cold; nil when null/absent
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

// AccountProfile identifies the account Claude Code is currently logged in
// as. It is read from the local .claude.json Claude Code maintains (see
// internal/claudeaccount), never from the network, and is nil when that file
// is absent or carries no account block (e.g. a fresh install, or an
// enterprise setup that authenticates differently).
//
// It deliberately does NOT hang off AccountSnapshot: resolving it means
// reading a large file, so it is resolved lazily at render time through
// render.Options.Profile and only when an account-* field is present.
//
// Every field is the raw string Claude Code stored; statusloom does not
// prettify or map these values, so a DSL author sees exactly what the
// upstream API reported (e.g. Plan "default_claude_max_5x").
type AccountProfile struct {
	Email        string // oauthAccount.emailAddress
	DisplayName  string // oauthAccount.displayName
	Organization string // oauthAccount.organizationName
	Role         string // oauthAccount.organizationRole, e.g. "primary_owner"
	// Type is oauthAccount.organizationType: "claude_team" for a Team seat,
	// "claude_max" for an individual subscription. It is the one field that
	// distinguishes those two account kinds, and it is populated on both.
	Type string
	// Plan is the rate-limit tier, e.g. "default_claude_max_5x". Team
	// accounts carry it in userRateLimitTier; individual accounts leave that
	// null and put the tier in organizationRateLimitTier, so Load falls back
	// (see there).
	Plan string
	// Seat is oauthAccount.seatTier, e.g. "team_tier_1". Individual accounts
	// have no seat, so this is legitimately empty for them.
	Seat string
	// OrganizationUUID is oauthAccount.organizationUuid: a stable identifier
	// that uniquely distinguishes the account/organization currently logged
	// in, safe to use as a cache key across a ccprofile-style Team/Max
	// profile switch. oauthAccount.accountUuid (not surfaced here) is NOT
	// usable for this: it identifies the person and is the same value across
	// that same person's Team and Max profiles, so it cannot tell those two
	// apart.
	OrganizationUUID string
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
