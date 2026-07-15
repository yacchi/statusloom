package claude

import (
	"testing"
	"time"
)

func TestPrettyModelName(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"claude-opus-4-8", "Opus 4.8"},
		{"claude-sonnet-5", "Sonnet 5"},
		{"claude-haiku-4-5-20251001", "Haiku 4.5"},
		{"claude-sonnet-5[1m]", "Sonnet 5"},
		{"claude-opus-4-8 (1M context)", "Opus 4.8"},
		{"claude-fable", "Fable"},
		{"gpt-4-turbo", "Gpt 4 Turbo"},
		{"custom-model-x", "Custom Model X"},
		{"", ""},
		{"   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := PrettyModelName(tt.id); got != tt.want {
				t.Errorf("PrettyModelName(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestDecodeSubagent_Running(t *testing.T) {
	raw := loadFixture(t, "subagent-running.json")
	tasks, err := DecodeSubagent(raw)
	if err != nil {
		t.Fatalf("DecodeSubagent: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(tasks))
	}

	t0 := tasks[0]
	if t0.ID != "b1a2c3d4e5f60718" {
		t.Errorf("tasks[0].ID = %q", t0.ID)
	}
	if t0.Type != "local_agent" {
		t.Errorf("tasks[0].Type = %q, want local_agent", t0.Type)
	}
	if t0.Status != "running" {
		t.Errorf("tasks[0].Status = %q, want running", t0.Status)
	}
	if t0.Description != "Review render pipeline changes" {
		t.Errorf("tasks[0].Description = %q", t0.Description)
	}
	if t0.Label != "Review render pipeline changes" {
		t.Errorf("tasks[0].Label = %q", t0.Label)
	}
	wantStart := time.UnixMilli(1784104398889)
	if !t0.StartedAt.Equal(wantStart) {
		t.Errorf("tasks[0].StartedAt = %v, want %v", t0.StartedAt, wantStart)
	}
	if t0.ModelID != "claude-opus-4-8" {
		t.Errorf("tasks[0].ModelID = %q", t0.ModelID)
	}
	if t0.ModelDisplay != "Opus 4.8" {
		t.Errorf("tasks[0].ModelDisplay = %q, want Opus 4.8", t0.ModelDisplay)
	}
	if t0.ContextWindowSize != 200000 {
		t.Errorf("tasks[0].ContextWindowSize = %d, want 200000", t0.ContextWindowSize)
	}
	if t0.TokenCount != 28454 {
		t.Errorf("tasks[0].TokenCount = %d, want 28454", t0.TokenCount)
	}
	if t0.Cwd != "/Users/dev/myapp" {
		t.Errorf("tasks[0].Cwd = %q", t0.Cwd)
	}
	if t0.Effort != nil {
		t.Errorf("tasks[0].Effort = %v, want nil", t0.Effort)
	}

	t1 := tasks[1]
	if t1.ModelID != "claude-sonnet-5" || t1.ModelDisplay != "Sonnet 5" {
		t.Errorf("tasks[1].ModelID/ModelDisplay = %q/%q, want claude-sonnet-5/Sonnet 5", t1.ModelID, t1.ModelDisplay)
	}
	if t1.TokenCount != 27543 {
		t.Errorf("tasks[1].TokenCount = %d, want 27543", t1.TokenCount)
	}
}

func TestDecodeSubagent_Completed(t *testing.T) {
	raw := loadFixture(t, "subagent-completed.json")
	tasks, err := DecodeSubagent(raw)
	if err != nil {
		t.Fatalf("DecodeSubagent: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(tasks))
	}
	for _, task := range tasks {
		if task.Status != "completed" {
			t.Errorf("task %s Status = %q, want completed", task.ID, task.Status)
		}
	}
	if tasks[0].TokenCount != 28663 {
		t.Errorf("tasks[0].TokenCount = %d, want 28663", tasks[0].TokenCount)
	}
}

func TestDecodeSubagent_EmptyTasks(t *testing.T) {
	tasks, err := DecodeSubagent([]byte(`{"session_id":"s","columns":80,"tasks":[]}`))
	if err != nil {
		t.Fatalf("DecodeSubagent: %v", err)
	}
	if tasks == nil {
		t.Error("tasks is nil, want non-nil empty slice")
	}
	if len(tasks) != 0 {
		t.Errorf("len(tasks) = %d, want 0", len(tasks))
	}
}

func TestDecodeSubagent_MissingTasks(t *testing.T) {
	tasks, err := DecodeSubagent([]byte(`{"session_id":"s","columns":80}`))
	if err != nil {
		t.Fatalf("DecodeSubagent: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("len(tasks) = %d, want 0", len(tasks))
	}
}

func TestDecodeSubagent_MissingModel(t *testing.T) {
	tasks, err := DecodeSubagent([]byte(`{"tasks":[{"id":"t1","status":"running"}]}`))
	if err != nil {
		t.Fatalf("DecodeSubagent: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}
	if tasks[0].ModelID != "" || tasks[0].ModelDisplay != "" {
		t.Errorf("ModelID/ModelDisplay = %q/%q, want empty", tasks[0].ModelID, tasks[0].ModelDisplay)
	}
	if !tasks[0].StartedAt.IsZero() {
		t.Errorf("StartedAt = %v, want zero", tasks[0].StartedAt)
	}
}

func TestDecodeSubagent_Malformed(t *testing.T) {
	if _, err := DecodeSubagent([]byte("{not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestDecodeSubagentColumns(t *testing.T) {
	raw := loadFixture(t, "subagent-running.json")
	if got := DecodeSubagentColumns(raw); got != 257 {
		t.Errorf("DecodeSubagentColumns = %d, want 257", got)
	}
	if got := DecodeSubagentColumns([]byte("{not json")); got != 0 {
		t.Errorf("DecodeSubagentColumns(malformed) = %d, want 0", got)
	}
}
