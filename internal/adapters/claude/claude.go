// Package claude implements the statusloom adapter for Claude Code's
// statusLine stdin JSON payload.
package claude

import (
	"bytes"
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

	// PromptCache is decoded separately by decodePromptCache so a malformed
	// object can never fail the whole payload.
	PromptCache json.RawMessage `json:"prompt_cache"`
}

type rateWindow struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
}

// promptCachePayload mirrors Claude Code's stdin prompt_cache object. Every
// number is a pointer or float so that null, fractional, and absent values
// are all accepted; conversion to schema types happens in decodePromptCache.
type promptCachePayload struct {
	Warm              bool     `json:"warm"`
	CachingObserved   bool     `json:"caching_observed"`
	TTL               string   `json:"ttl"`
	ExpiresAt         *float64 `json:"expires_at"`
	Requests          *float64 `json:"requests"`
	Misses            *float64 `json:"misses"`
	ExpectedRebuilds  *float64 `json:"expected_rebuilds"`
	HitRatio          *float64 `json:"hit_ratio"`
	CacheWriteTokens  *float64 `json:"cache_write_tokens"`
	MissRecacheTokens *float64 `json:"miss_recache_tokens"`
	LastMissAt        *float64 `json:"last_miss_at"`
	LastMissCause     *struct {
		Causes []string `json:"causes"`
	} `json:"last_miss_cause"`
	RecacheTokensIfCold *float64 `json:"recache_tokens_if_cold"`
}

// decodePromptCache converts the raw stdin prompt_cache value. It returns nil
// when the key is absent, JSON null, or does not decode into the expected
// shape; it never reports an error because prompt_cache is advisory.
func decodePromptCache(raw json.RawMessage) *schema.PromptCache {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	var p promptCachePayload
	if err := json.Unmarshal(trimmed, &p); err != nil {
		return nil
	}

	pc := &schema.PromptCache{
		Warm:              p.Warm,
		CachingObserved:   p.CachingObserved,
		TTL:               p.TTL,
		ExpiresAt:         toTime(p.ExpiresAt),
		Requests:          toInt(p.Requests),
		Misses:            toInt(p.Misses),
		ExpectedRebuilds:  toInt(p.ExpectedRebuilds),
		HitRatio:          p.HitRatio,
		CacheWriteTokens:  toInt(p.CacheWriteTokens),
		MissRecacheTokens: toInt(p.MissRecacheTokens),
		LastMissAt:        toTime(p.LastMissAt),
	}
	if p.LastMissCause != nil && len(p.LastMissCause.Causes) > 0 {
		pc.LastMissCauses = p.LastMissCause.Causes
	}
	if p.RecacheTokensIfCold != nil {
		v := int(*p.RecacheTokensIfCold)
		pc.RecacheTokensIfCold = &v
	}
	return pc
}

// toTime converts a Unix epoch-seconds value, returning nil for absent or
// null input rather than the zero time.
func toTime(v *float64) *time.Time {
	if v == nil {
		return nil
	}
	t := time.Unix(int64(*v), 0)
	return &t
}

// toInt truncates an optional counter, treating absent or null as zero.
func toInt(v *float64) int {
	if v == nil {
		return 0
	}
	return int(*v)
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

	snap.Session.PromptCache = decodePromptCache(p.PromptCache)

	return snap, nil
}

func toRateWindow(rw *rateWindow) *schema.RateWindow {
	return &schema.RateWindow{
		UsedPercentage: rw.UsedPercentage,
		ResetsAt:       time.Unix(rw.ResetsAt, 0),
	}
}
