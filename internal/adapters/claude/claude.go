// Package claude implements the statusloom adapter for Claude Code's
// statusLine stdin JSON payload.
package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// Adapter decodes Claude Code's statusLine stdin JSON into a normalized
// schema.StatusSnapshot.
type Adapter struct{}

// New returns a Claude Code adapter.
func New() *Adapter {
	return &Adapter{}
}

// ID implements adapters.Adapter.
func (a *Adapter) ID() schema.ToolID {
	return schema.ToolClaudeCode
}

// Detect implements adapters.Adapter. It reports true only when
// session_id, transcript_path, and context_window are all present —
// a single field alone is never sufficient.
func (a *Adapter) Detect(raw map[string]json.RawMessage) bool {
	_, hasSessionID := raw["session_id"]
	_, hasTranscript := raw["transcript_path"]
	_, hasContext := raw["context_window"]
	return hasSessionID && hasTranscript && hasContext
}

// payload mirrors the shape of Claude Code's statusLine stdin JSON.
// Unknown fields are ignored by encoding/json by default.
type payload struct {
	SessionID      string `json:"session_id"`
	SessionName    string `json:"session_name"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Version        string `json:"version"`

	OutputStyle *struct {
		Name string `json:"name"`
	} `json:"output_style"`

	Model *struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`

	Workspace *struct {
		CurrentDir  string `json:"current_dir"`
		ProjectDir  string `json:"project_dir"`
		GitWorktree string `json:"git_worktree"`
		Repo        *struct {
			Host  string `json:"host"`
			Owner string `json:"owner"`
			Name  string `json:"name"`
		} `json:"repo"`
	} `json:"workspace"`

	Worktree *struct {
		Name string `json:"name"`
	} `json:"worktree"`

	Agent *struct {
		Name string `json:"name"`
	} `json:"agent"`

	Vim *struct {
		Mode string `json:"mode"`
	} `json:"vim"`

	PR *struct {
		Number      int    `json:"number"`
		URL         string `json:"url"`
		ReviewState string `json:"review_state"`
	} `json:"pr"`

	Effort *struct {
		Level string `json:"level"`
	} `json:"effort"`

	Thinking *struct {
		Enabled bool `json:"enabled"`
	} `json:"thinking"`

	Cost *struct {
		TotalCostUSD       float64 `json:"total_cost_usd"`
		TotalDurationMS    int64   `json:"total_duration_ms"`
		TotalAPIDurationMS int64   `json:"total_api_duration_ms"`
		TotalLinesAdded    int     `json:"total_lines_added"`
		TotalLinesRemoved  int     `json:"total_lines_removed"`
	} `json:"cost"`

	Exceeds200kTokens bool `json:"exceeds_200k_tokens"`

	ContextWindow *struct {
		TotalInputTokens    int      `json:"total_input_tokens"`
		TotalOutputTokens   int      `json:"total_output_tokens"`
		ContextWindowSize   int      `json:"context_window_size"`
		UsedPercentage      *float64 `json:"used_percentage"`
		RemainingPercentage *float64 `json:"remaining_percentage"`
		CurrentUsage        *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"current_usage"`
	} `json:"context_window"`

	RateLimits *struct {
		FiveHour *rateWindow `json:"five_hour"`
		SevenDay *rateWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

type rateWindow struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
}

// Decode implements adapters.Adapter.
func (a *Adapter) Decode(raw []byte) (schema.StatusSnapshot, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return schema.StatusSnapshot{}, fmt.Errorf("claude: invalid JSON: %w", err)
	}
	if p.SessionID == "" {
		return schema.StatusSnapshot{}, errors.New("claude: not a Claude Code payload")
	}

	snap := schema.StatusSnapshot{
		Tool: schema.ToolSnapshot{
			ID:      schema.ToolClaudeCode,
			Version: p.Version,
		},
		Session: schema.SessionSnapshot{
			ID: p.SessionID,
		},
		Repository: nil,
		System: schema.SystemSnapshot{
			Cwd: p.Cwd,
		},
	}

	if p.SessionName != "" {
		name := p.SessionName
		snap.Session.Name = &name
	}

	if p.Agent != nil && p.Agent.Name != "" {
		name := p.Agent.Name
		snap.Session.AgentName = &name
	}

	if p.Vim != nil && p.Vim.Mode != "" {
		mode := p.Vim.Mode
		snap.Session.VimMode = &mode
	}

	if p.PR != nil {
		snap.PullRequest = &schema.PullRequestInfo{
			Number:      p.PR.Number,
			URL:         p.PR.URL,
			ReviewState: p.PR.ReviewState,
		}
	}

	if p.Workspace != nil {
		snap.System.ProjectDir = p.Workspace.ProjectDir
		snap.System.Worktree = p.Workspace.GitWorktree
		if p.Workspace.Repo != nil {
			snap.System.Repo = &schema.RepoIdentity{
				Host:  p.Workspace.Repo.Host,
				Owner: p.Workspace.Repo.Owner,
				Name:  p.Workspace.Repo.Name,
			}
		}
	}
	if snap.System.Worktree == "" && p.Worktree != nil {
		snap.System.Worktree = p.Worktree.Name
	}

	if p.Model != nil {
		snap.Session.Model = &schema.ModelInfo{
			ID:          p.Model.ID,
			DisplayName: p.Model.DisplayName,
		}
	}

	if p.Effort != nil {
		level := p.Effort.Level
		snap.Session.ReasoningEffort = &level
	}

	if p.Thinking != nil {
		enabled := p.Thinking.Enabled
		snap.Session.ThinkingEnabled = &enabled
	}

	if p.OutputStyle != nil {
		name := p.OutputStyle.Name
		snap.Session.OutputStyle = &name
	}

	if p.ContextWindow != nil {
		snap.Session.Context = &schema.ContextUsage{
			TotalInputTokens:    p.ContextWindow.TotalInputTokens,
			TotalOutputTokens:   p.ContextWindow.TotalOutputTokens,
			WindowSize:          p.ContextWindow.ContextWindowSize,
			UsedPercentage:      p.ContextWindow.UsedPercentage,
			RemainingPercentage: p.ContextWindow.RemainingPercentage,
			Exceeds200K:         p.Exceeds200kTokens,
		}
		if cu := p.ContextWindow.CurrentUsage; cu != nil {
			snap.Session.Context.Current = &schema.TokenBreakdown{
				Input:         cu.InputTokens,
				Output:        cu.OutputTokens,
				CacheCreation: cu.CacheCreationInputTokens,
				CacheRead:     cu.CacheReadInputTokens,
			}
		}
	}

	if p.Cost != nil {
		snap.Session.Cost = &schema.CostUsage{
			USD:          p.Cost.TotalCostUSD,
			Duration:     time.Duration(p.Cost.TotalDurationMS) * time.Millisecond,
			APIDuration:  time.Duration(p.Cost.TotalAPIDurationMS) * time.Millisecond,
			LinesAdded:   p.Cost.TotalLinesAdded,
			LinesRemoved: p.Cost.TotalLinesRemoved,
		}
	}

	if p.RateLimits != nil {
		if p.RateLimits.FiveHour != nil {
			snap.Account.FiveHour = toRateWindow(p.RateLimits.FiveHour)
		}
		if p.RateLimits.SevenDay != nil {
			snap.Account.SevenDay = toRateWindow(p.RateLimits.SevenDay)
		}
	}
	snap.Account.Stale = false

	return snap, nil
}

func toRateWindow(rw *rateWindow) *schema.RateWindow {
	return &schema.RateWindow{
		UsedPercentage: rw.UsedPercentage,
		ResetsAt:       time.Unix(rw.ResetsAt, 0),
	}
}
