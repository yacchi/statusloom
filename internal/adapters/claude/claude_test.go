package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "fixtures", "claude", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return b
}

func TestDecode_Full(t *testing.T) {
	raw := loadFixture(t, "full.json")
	snap, err := New().Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if snap.Tool.Version != "2.1.200" {
		t.Errorf("Tool.Version = %q, want 2.1.200", snap.Tool.Version)
	}
	if snap.Session.Model == nil || snap.Session.Model.DisplayName != "Opus 4.8" {
		t.Errorf("Session.Model.DisplayName = %+v, want Opus 4.8", snap.Session.Model)
	}
	if snap.Session.ReasoningEffort == nil || *snap.Session.ReasoningEffort != "high" {
		t.Errorf("Session.ReasoningEffort = %v, want high", snap.Session.ReasoningEffort)
	}
	if snap.Session.ThinkingEnabled == nil || *snap.Session.ThinkingEnabled != true {
		t.Errorf("Session.ThinkingEnabled = %v, want true", snap.Session.ThinkingEnabled)
	}
	if snap.Session.Context == nil {
		t.Fatalf("Session.Context is nil")
	}
	if snap.Session.Context.TotalInputTokens != 64000 {
		t.Errorf("TotalInputTokens = %d, want 64000", snap.Session.Context.TotalInputTokens)
	}
	if snap.Session.Context.WindowSize != 200000 {
		t.Errorf("WindowSize = %d, want 200000", snap.Session.Context.WindowSize)
	}
	if snap.Session.Context.UsedPercentage == nil || *snap.Session.Context.UsedPercentage != 32 {
		t.Errorf("UsedPercentage = %v, want 32", snap.Session.Context.UsedPercentage)
	}
	if snap.Session.Cost == nil || snap.Session.Cost.USD != 1.2345 {
		t.Errorf("Session.Cost = %v, want 1.2345", snap.Session.Cost)
	}
	if snap.Account.FiveHour == nil {
		t.Fatalf("Account.FiveHour is nil")
	}
	if snap.Account.FiveHour.UsedPercentage != 27 {
		t.Errorf("FiveHour.UsedPercentage = %v, want 27", snap.Account.FiveHour.UsedPercentage)
	}
	wantReset := time.Unix(1783731600, 0)
	if !snap.Account.FiveHour.ResetsAt.Equal(wantReset) {
		t.Errorf("FiveHour.ResetsAt = %v, want %v", snap.Account.FiveHour.ResetsAt, wantReset)
	}
	if snap.Account.SevenDay == nil || snap.Account.SevenDay.UsedPercentage != 79 {
		t.Errorf("Account.SevenDay = %v, want 79", snap.Account.SevenDay)
	}
	if snap.Repository != nil {
		t.Errorf("Repository = %v, want nil", snap.Repository)
	}
	if snap.Session.Name == nil || *snap.Session.Name != "refactor-render-pipeline" {
		t.Errorf("Session.Name = %v, want refactor-render-pipeline", snap.Session.Name)
	}
	if snap.Session.AgentName == nil || *snap.Session.AgentName != "code-reviewer" {
		t.Errorf("Session.AgentName = %v, want code-reviewer", snap.Session.AgentName)
	}
	if snap.Session.VimMode == nil || *snap.Session.VimMode != "NORMAL" {
		t.Errorf("Session.VimMode = %v, want NORMAL", snap.Session.VimMode)
	}
	if snap.PullRequest == nil {
		t.Fatalf("PullRequest is nil")
	}
	if snap.PullRequest.Number != 42 {
		t.Errorf("PullRequest.Number = %d, want 42", snap.PullRequest.Number)
	}
	if snap.PullRequest.URL != "https://github.com/yacchi/statusloom/pull/42" {
		t.Errorf("PullRequest.URL = %q, want https://github.com/yacchi/statusloom/pull/42", snap.PullRequest.URL)
	}
	if snap.PullRequest.ReviewState != "pending" {
		t.Errorf("PullRequest.ReviewState = %q, want pending", snap.PullRequest.ReviewState)
	}
	if snap.System.Worktree != "feature-worktree" {
		t.Errorf("System.Worktree = %q, want feature-worktree", snap.System.Worktree)
	}
	if snap.System.Repo == nil {
		t.Fatalf("System.Repo is nil")
	}
	if snap.System.Repo.Host != "github.com" || snap.System.Repo.Owner != "yacchi" || snap.System.Repo.Name != "statusloom" {
		t.Errorf("System.Repo = %+v, want {github.com yacchi statusloom}", snap.System.Repo)
	}
	if snap.Session.Cost == nil {
		t.Fatalf("Session.Cost is nil")
	}
	if snap.Session.Cost.Duration != 4523000*time.Millisecond {
		t.Errorf("Session.Cost.Duration = %v, want %v", snap.Session.Cost.Duration, 4523000*time.Millisecond)
	}
	if snap.Session.Cost.APIDuration != 123*time.Second {
		t.Errorf("Session.Cost.APIDuration = %v, want %v", snap.Session.Cost.APIDuration, 123*time.Second)
	}
	if snap.Session.Cost.LinesAdded != 156 {
		t.Errorf("Session.Cost.LinesAdded = %d, want 156", snap.Session.Cost.LinesAdded)
	}
	if snap.Session.Cost.LinesRemoved != 23 {
		t.Errorf("Session.Cost.LinesRemoved = %d, want 23", snap.Session.Cost.LinesRemoved)
	}
	if snap.Session.Context.Current == nil {
		t.Fatalf("Session.Context.Current is nil")
	}
	if snap.Session.Context.Current.Input != 4000 || snap.Session.Context.Current.Output != 1200 ||
		snap.Session.Context.Current.CacheCreation != 12000 || snap.Session.Context.Current.CacheRead != 48000 {
		t.Errorf("Session.Context.Current = %+v, want {4000 1200 12000 48000}", snap.Session.Context.Current)
	}
}

func TestDecode_NoRateLimits(t *testing.T) {
	raw := loadFixture(t, "no-rate-limits.json")
	snap, err := New().Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if snap.Account.FiveHour != nil {
		t.Errorf("Account.FiveHour = %v, want nil", snap.Account.FiveHour)
	}
	if snap.Account.SevenDay != nil {
		t.Errorf("Account.SevenDay = %v, want nil", snap.Account.SevenDay)
	}
	if snap.Session.ReasoningEffort != nil {
		t.Errorf("Session.ReasoningEffort = %v, want nil", snap.Session.ReasoningEffort)
	}
	if snap.Session.Name != nil {
		t.Errorf("Session.Name = %v, want nil", snap.Session.Name)
	}
	if snap.Session.AgentName != nil {
		t.Errorf("Session.AgentName = %v, want nil", snap.Session.AgentName)
	}
	if snap.Session.VimMode != nil {
		t.Errorf("Session.VimMode = %v, want nil", snap.Session.VimMode)
	}
	if snap.PullRequest != nil {
		t.Errorf("PullRequest = %v, want nil", snap.PullRequest)
	}
	if snap.System.Worktree != "" {
		t.Errorf("System.Worktree = %q, want empty", snap.System.Worktree)
	}
	if snap.System.Repo != nil {
		t.Errorf("System.Repo = %v, want nil", snap.System.Repo)
	}
}

func TestDecode_EarlySession(t *testing.T) {
	raw := loadFixture(t, "early-session.json")
	snap, err := New().Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if snap.Session.Context == nil {
		t.Fatalf("Session.Context is nil")
	}
	if snap.Session.Context.UsedPercentage != nil {
		t.Errorf("UsedPercentage = %v, want nil", snap.Session.Context.UsedPercentage)
	}
	if snap.Session.Cost == nil || snap.Session.Cost.USD != 0 {
		t.Errorf("Session.Cost = %v, want 0", snap.Session.Cost)
	}
	if snap.Session.Context.Current != nil {
		t.Errorf("Session.Context.Current = %+v, want nil", snap.Session.Context.Current)
	}
}

func TestDecode_OneMillion(t *testing.T) {
	raw := loadFixture(t, "one-million.json")
	snap, err := New().Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if snap.Session.Context == nil {
		t.Fatalf("Session.Context is nil")
	}
	if snap.Session.Context.WindowSize != 1000000 {
		t.Errorf("WindowSize = %d, want 1000000", snap.Session.Context.WindowSize)
	}
	if !snap.Session.Context.Exceeds200K {
		t.Errorf("Exceeds200K = false, want true")
	}
}

func TestDecode_PRReviewStateAbsent(t *testing.T) {
	raw := []byte(`{
		"session_id": "sess-pr",
		"transcript_path": "/tmp/x.jsonl",
		"cwd": "/tmp",
		"context_window": {},
		"pr": { "number": 7, "url": "https://github.com/yacchi/statusloom/pull/7" }
	}`)
	snap, err := New().Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if snap.PullRequest == nil {
		t.Fatalf("PullRequest is nil")
	}
	if snap.PullRequest.Number != 7 {
		t.Errorf("PullRequest.Number = %d, want 7", snap.PullRequest.Number)
	}
	if snap.PullRequest.ReviewState != "" {
		t.Errorf("PullRequest.ReviewState = %q, want empty", snap.PullRequest.ReviewState)
	}
}

func TestDecode_WorktreeFallback(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "git_worktree takes priority over worktree.name",
			raw: `{
				"session_id": "sess-wt-1", "transcript_path": "/tmp/x.jsonl", "cwd": "/tmp",
				"context_window": {},
				"workspace": { "git_worktree": "from-workspace" },
				"worktree": { "name": "from-worktree" }
			}`,
			want: "from-workspace",
		},
		{
			name: "falls back to worktree.name when git_worktree absent",
			raw: `{
				"session_id": "sess-wt-2", "transcript_path": "/tmp/x.jsonl", "cwd": "/tmp",
				"context_window": {},
				"worktree": { "name": "from-worktree" }
			}`,
			want: "from-worktree",
		},
		{
			name: "empty when neither present",
			raw: `{
				"session_id": "sess-wt-3", "transcript_path": "/tmp/x.jsonl", "cwd": "/tmp",
				"context_window": {}
			}`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap, err := New().Decode([]byte(tt.raw))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if snap.System.Worktree != tt.want {
				t.Errorf("System.Worktree = %q, want %q", snap.System.Worktree, tt.want)
			}
		})
	}
}

func TestDecode_Malformed(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{"empty input", []byte("")},
		{"invalid json", []byte("{not json")},
		{"missing session_id", []byte("{}")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New().Decode(tt.raw)
			if err == nil {
				t.Fatalf("Decode(%s): expected error, got nil", tt.name)
			}
		})
	}
}
