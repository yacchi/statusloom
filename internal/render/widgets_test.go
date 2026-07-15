package render

import (
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/schema"
)

// richSnapshot extends fullSnapshot with the data backing the newer
// widgets (session identity, pull request, repo identity, cost breakdown,
// per-call token usage).
func richSnapshot() schema.StatusSnapshot {
	snap := fullSnapshot()
	snap.Session.ID = "session-123"
	snap.Session.Model.ID = "claude-opus-4-8"
	outputStyle, thinking, remaining := "default", true, 68.0
	snap.Session.OutputStyle = &outputStyle
	snap.Session.ThinkingEnabled = &thinking
	snap.Session.Context.WindowSize = 200000
	snap.Session.Context.TotalOutputTokens = 8000
	snap.Session.Context.RemainingPercentage = &remaining
	snap.Session.Name = strptr("refactor")
	snap.Session.AgentName = strptr("reviewer")
	snap.Session.VimMode = strptr("NORMAL")
	snap.Session.Cost = &schema.CostUsage{
		USD:          1.23,
		Duration:     1*time.Hour + 15*time.Minute,
		APIDuration:  5*time.Minute + 12*time.Second,
		LinesAdded:   40,
		LinesRemoved: 7,
	}
	snap.Session.Context.Current = &schema.TokenBreakdown{
		Input:         1000,
		Output:        500,
		CacheCreation: 2000,
		CacheRead:     7000,
	}
	snap.PullRequest = &schema.PullRequestInfo{
		Number:      1234,
		URL:         "https://github.com/yacchi/statusloom/pull/1234",
		ReviewState: "approved",
	}
	snap.System.Worktree = "feature-x"
	snap.System.ProjectDir = "/work/project"
	snap.System.Repo = &schema.RepoIdentity{Host: "github.com", Owner: "yacchi", Name: "statusloom"}
	snap.Repository.Root = "/work/project"
	snap.Repository.Staged, snap.Repository.Unstaged, snap.Repository.Untracked = 2, 3, 4
	snap.Repository.Ahead, snap.Repository.Behind = 5, 6
	snap.Repository.Dirty = true
	return snap
}

func TestRenderContent_NewWidgets(t *testing.T) {
	cases := []struct {
		name string
		spec config.WidgetSpec
		want string
	}{
		{"session-name", config.WidgetSpec{Type: "session-name"}, "refactor"},
		{"agent-name", config.WidgetSpec{Type: "agent-name"}, "reviewer"},
		{"vim-mode", config.WidgetSpec{Type: "vim-mode"}, "NORMAL"},
		{"pr-number", config.WidgetSpec{Type: "pr-number"}, "#1234"},
		{"pr-number raw", config.WidgetSpec{Type: "pr-number", RawValue: true}, "1234"},
		{"pr-review-state", config.WidgetSpec{Type: "pr-review-state"}, "approved"},
		{"repo-name", config.WidgetSpec{Type: "repo-name"}, "yacchi/statusloom"},
		{"repo-name raw", config.WidgetSpec{Type: "repo-name", RawValue: true}, "statusloom"},
		{"worktree", config.WidgetSpec{Type: "worktree"}, "feature-x"},
		{"session-duration", config.WidgetSpec{Type: "session-duration"}, "1h 15m"},
		{"session-duration raw", config.WidgetSpec{Type: "session-duration", RawValue: true}, "4500"},
		{"api-duration", config.WidgetSpec{Type: "api-duration"}, "5m 12s"},
		{"api-duration raw", config.WidgetSpec{Type: "api-duration", RawValue: true}, "312"},
		{"lines-changed", config.WidgetSpec{Type: "lines-changed"}, "(+40,-7)"},
		{"cache-hit-rate", config.WidgetSpec{Type: "cache-hit-rate"}, "70%"}, // 7000/(1000+2000+7000)*100
		{"model-id", config.WidgetSpec{Type: "model-id"}, "claude-opus-4-8"},
		{"output-style", config.WidgetSpec{Type: "output-style"}, "default"},
		{"session-id", config.WidgetSpec{Type: "session-id"}, "session-123"},
		{"thinking-enabled", config.WidgetSpec{Type: "thinking-enabled"}, "true"},
		{"project-directory", config.WidgetSpec{Type: "project-directory"}, "/work/project"},
		{"git-root", config.WidgetSpec{Type: "git-root"}, "/work/project"},
		{"context-window-size", config.WidgetSpec{Type: "context-window-size"}, "200,000"},
		{"context-remaining", config.WidgetSpec{Type: "context-remaining"}, "68%"},
		{"context-output-tokens", config.WidgetSpec{Type: "context-output-tokens"}, "8,000"},
		{"current-input-tokens", config.WidgetSpec{Type: "current-input-tokens"}, "1,000"},
		{"current-output-tokens", config.WidgetSpec{Type: "current-output-tokens"}, "500"},
		{"cache-creation-tokens", config.WidgetSpec{Type: "cache-creation-tokens"}, "2,000"},
		{"cache-read-tokens", config.WidgetSpec{Type: "cache-read-tokens"}, "7,000"},
		{"lines-added", config.WidgetSpec{Type: "lines-added"}, "40"},
		{"lines-removed", config.WidgetSpec{Type: "lines-removed"}, "7"},
		{"git-staged", config.WidgetSpec{Type: "git-staged"}, "2"},
		{"git-unstaged", config.WidgetSpec{Type: "git-unstaged"}, "3"},
		{"git-untracked", config.WidgetSpec{Type: "git-untracked"}, "4"},
		{"git-ahead", config.WidgetSpec{Type: "git-ahead"}, "5"},
		{"git-behind", config.WidgetSpec{Type: "git-behind"}, "6"},
		{"git-clean", config.WidgetSpec{Type: "git-clean"}, "false"},
		{"exceeds-200k", config.WidgetSpec{Type: "exceeds-200k"}, "false"},
	}
	snap := richSnapshot()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renderContent(c.spec, snap, config.ToolConfig{}, Options{Width: 120, Now: fixedNow}, false)
			if got != c.want {
				t.Errorf("renderContent(%q) = %q, want %q", c.spec.Type, got, c.want)
			}
		})
	}
}

func TestRenderContent_NewWidgetsHidden(t *testing.T) {
	empty := ""
	cases := []struct {
		name   string
		typ    string
		mutate func(*schema.StatusSnapshot)
	}{
		{"session-name nil", "session-name", func(s *schema.StatusSnapshot) { s.Session.Name = nil }},
		{"session-name empty", "session-name", func(s *schema.StatusSnapshot) { s.Session.Name = &empty }},
		{"agent-name nil", "agent-name", func(s *schema.StatusSnapshot) { s.Session.AgentName = nil }},
		{"vim-mode nil", "vim-mode", func(s *schema.StatusSnapshot) { s.Session.VimMode = nil }},
		{"vim-mode empty", "vim-mode", func(s *schema.StatusSnapshot) { s.Session.VimMode = &empty }},
		{"pr-number no pr", "pr-number", func(s *schema.StatusSnapshot) { s.PullRequest = nil }},
		{"pr-review-state no pr", "pr-review-state", func(s *schema.StatusSnapshot) { s.PullRequest = nil }},
		{"pr-review-state empty", "pr-review-state", func(s *schema.StatusSnapshot) { s.PullRequest.ReviewState = "" }},
		{"repo-name nil", "repo-name", func(s *schema.StatusSnapshot) { s.System.Repo = nil }},
		{"worktree empty", "worktree", func(s *schema.StatusSnapshot) { s.System.Worktree = "" }},
		{"session-duration no cost", "session-duration", func(s *schema.StatusSnapshot) { s.Session.Cost = nil }},
		{"api-duration no cost", "api-duration", func(s *schema.StatusSnapshot) { s.Session.Cost = nil }},
		{"lines-changed no cost", "lines-changed", func(s *schema.StatusSnapshot) { s.Session.Cost = nil }},
		{"cache-hit-rate no context", "cache-hit-rate", func(s *schema.StatusSnapshot) { s.Session.Context = nil }},
		{"cache-hit-rate no current", "cache-hit-rate", func(s *schema.StatusSnapshot) { s.Session.Context.Current = nil }},
		{"cache-hit-rate zero denom", "cache-hit-rate", func(s *schema.StatusSnapshot) {
			s.Session.Context.Current = &schema.TokenBreakdown{}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snap := richSnapshot()
			c.mutate(&snap)
			got := renderContent(config.WidgetSpec{Type: c.typ}, snap, config.ToolConfig{}, Options{Width: 120, Now: fixedNow}, false)
			if got != "" {
				t.Errorf("renderContent(%q) = %q, want \"\" (hidden)", c.typ, got)
			}
		})
	}
}
