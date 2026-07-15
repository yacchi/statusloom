package dsl

import "testing"

func TestFieldByName_Found(t *testing.T) {
	f, ok := FieldByName("claude-code", "context-percentage")
	if !ok {
		t.Fatal("expected context-percentage to be found")
	}
	if f.SelfMetric != "context-percent" {
		t.Errorf("SelfMetric = %q, want %q", f.SelfMetric, "context-percent")
	}
	if len(f.Formats) != 1 || f.Formats[0] != "percent" {
		t.Errorf("Formats = %v, want [percent]", f.Formats)
	}
	if f.Linkable {
		t.Error("context-percentage should not be Linkable")
	}
}

func TestFieldByName_UnknownField(t *testing.T) {
	if _, ok := FieldByName("claude-code", "does-not-exist"); ok {
		t.Fatal("expected unknown field to not be found")
	}
}

func TestFieldByName_UnknownTool(t *testing.T) {
	if _, ok := FieldByName("does-not-exist", "model"); ok {
		t.Fatal("expected unknown tool to not be found")
	}
}

func TestFields_Count(t *testing.T) {
	fields := Fields("claude-code")
	if len(fields) != 67 {
		t.Errorf("Fields count = %d, want 67", len(fields))
	}
}

func TestFields_UnknownToolReturnsNil(t *testing.T) {
	if got := Fields("does-not-exist"); got != nil {
		t.Errorf("Fields(unknown) = %v, want nil", got)
	}
}

func TestFields_DefensiveCopy(t *testing.T) {
	a := Fields("claude-code")
	a[0].Name = "mutated"
	b := Fields("claude-code")
	if b[0].Name == "mutated" {
		t.Error("Fields() must return a defensive copy, mutation leaked into the registry")
	}
}

func TestFields_LinkableSet(t *testing.T) {
	want := map[string]bool{"pr-number": true, "pr-review-state": true, "repo-name": true}
	for _, f := range Fields("claude-code") {
		if f.Linkable != want[f.Name] {
			t.Errorf("field %q Linkable = %v, want %v", f.Name, f.Linkable, want[f.Name])
		}
	}
}

func TestFields_SelfMetrics(t *testing.T) {
	want := map[string]string{
		"context-length":            "context-tokens",
		"context-percentage":        "context-percent",
		"context-percentage-usable": "context-usable-percent",
		"session-cost":              "session-cost-usd",
		"five-hour-usage":           "five-hour-percent",
		"weekly-usage":              "seven-day-percent",
		"five-hour-reset":           "five-hour-reset-minutes",
		"weekly-reset":              "seven-day-reset-minutes",
		"session-duration":          "session-duration-minutes",
		"api-duration":              "api-duration-minutes",
		"lines-changed":             "lines-changed-total",
		"cache-hit-rate":            "cache-hit-percent",
		"thinking-enabled":          "thinking-enabled", "context-window-size": "context-window-tokens",
		"context-remaining": "context-remaining-percent", "context-output-tokens": "context-output-tokens",
		"current-input-tokens": "current-input-tokens", "current-output-tokens": "current-output-tokens",
		"cache-creation-tokens": "cache-creation-tokens", "cache-read-tokens": "cache-read-tokens",
		"lines-added": "lines-added", "lines-removed": "lines-removed", "git-staged": "git-staged",
		"git-unstaged": "git-unstaged", "git-untracked": "git-untracked", "git-ahead": "git-ahead",
		"git-behind": "git-behind", "git-clean": "git-clean", "exceeds-200k": "exceeds-200k",
		"compaction-count": "compaction-count", "compaction-auto": "compaction-auto",
		"compaction-manual": "compaction-manual", "compaction-unknown": "compaction-unknown",
		"compaction-tokens-reclaimed": "compaction-tokens-reclaimed",
		"session-input-tokens":        "session-input-tokens", "session-output-tokens": "session-output-tokens",
		"session-cache-creation-tokens": "session-cache-creation-tokens",
		"session-cache-read-tokens":     "session-cache-read-tokens",
		"session-total-tokens":          "session-total-tokens",
		"input-token-speed":             "input-token-speed", "output-token-speed": "output-token-speed",
		"total-token-speed":   "total-token-speed",
		"extra-usage-cost":    "extra-usage-cost-usd",
		"extra-usage-limit":   "extra-usage-limit-usd",
		"extra-usage-percent": "extra-usage-percent",
		"weekly-usage-opus":   "seven-day-opus-percent",
		"weekly-usage-sonnet": "seven-day-sonnet-percent",
		"weekly-reset-opus":   "seven-day-opus-reset-minutes",
		"weekly-reset-sonnet": "seven-day-sonnet-reset-minutes",
	}
	fields := Fields("claude-code")
	if len(fields) == 0 {
		t.Fatal("expected fields")
	}
	gotWithSelf := 0
	for _, f := range fields {
		if got, wantSelf := f.SelfMetric, want[f.Name]; got != wantSelf {
			t.Errorf("field %q SelfMetric = %q, want %q", f.Name, got, wantSelf)
		}
		if f.SelfMetric != "" {
			gotWithSelf++
		}
	}
	if gotWithSelf != len(want) {
		t.Errorf("fields with a self metric = %d, want %d", gotWithSelf, len(want))
	}
}

func TestFields_PlainStringFieldsHaveNoFormats(t *testing.T) {
	plain := []string{
		"model", "git-branch", "git-changes", "tool-version",
		"current-directory", "session-name", "agent-name", "repo-name",
		"worktree", "pr-number",
	}
	for _, name := range plain {
		f, ok := FieldByName("claude-code", name)
		if !ok {
			t.Fatalf("field %q not found", name)
		}
		if len(f.Formats) != 0 {
			t.Errorf("field %q Formats = %v, want empty", name, f.Formats)
		}
	}
}

func TestFields_DisplayMetadataPopulated(t *testing.T) {
	for _, f := range Fields("claude-code") {
		if f.DisplayName == "" {
			t.Errorf("field %q has no DisplayName", f.Name)
		}
		if f.Descriptions.EN == "" || f.Descriptions.JA == "" {
			t.Errorf("field %q missing localized descriptions: %+v", f.Name, f.Descriptions)
		}
		if f.Category != "common" && f.Category != "claude" {
			t.Errorf("field %q Category = %q, want common|claude", f.Name, f.Category)
		}
	}
}

func TestMetrics_DisplayMetadataPopulated(t *testing.T) {
	for _, m := range Metrics("claude-code") {
		if m.DisplayName == "" {
			t.Errorf("metric %q has no DisplayName", m.Name)
		}
		if m.Descriptions.EN == "" || m.Descriptions.JA == "" {
			t.Errorf("metric %q missing localized descriptions: %+v", m.Name, m.Descriptions)
		}
	}
}

func TestMetricByName_Found(t *testing.T) {
	if _, ok := MetricByName("claude-code", "git-dirty"); !ok {
		t.Fatal("expected git-dirty metric to be found")
	}
	if _, ok := MetricByName("claude-code", "context-percent"); !ok {
		t.Fatal("expected context-percent metric to be found")
	}
}

func TestMetricByName_Unknown(t *testing.T) {
	if _, ok := MetricByName("claude-code", "does-not-exist"); ok {
		t.Fatal("expected unknown metric to not be found")
	}
	if _, ok := MetricByName("does-not-exist", "context-percent"); ok {
		t.Fatal("expected unknown tool to not be found")
	}
}

func TestMetrics_Count(t *testing.T) {
	m := Metrics("claude-code")
	if len(m) != 51 {
		t.Errorf("Metrics count = %d, want 51", len(m))
	}
}

func TestMetrics_UnknownToolReturnsNil(t *testing.T) {
	if got := Metrics("does-not-exist"); got != nil {
		t.Errorf("Metrics(unknown) = %v, want nil", got)
	}
}

func TestMetrics_DefensiveCopy(t *testing.T) {
	a := Metrics("claude-code")
	a[0].Name = "mutated"
	b := Metrics("claude-code")
	if b[0].Name == "mutated" {
		t.Error("Metrics() must return a defensive copy, mutation leaked into the registry")
	}
}

// TestFields_OAuthUsageFields verifies the 7 new extra-usage/weekly-usage-opus/
// weekly-usage-sonnet fields resolve with the expected self metric, formats,
// and the "oauth-usage" capability gate, and carry non-empty descriptions.
func TestFields_OAuthUsageFields(t *testing.T) {
	want := []struct {
		name       string
		selfMetric string
		formats    []string
	}{
		{"extra-usage-cost", "extra-usage-cost-usd", []string{"currency"}},
		{"extra-usage-limit", "extra-usage-limit-usd", []string{"currency"}},
		{"extra-usage-percent", "extra-usage-percent", []string{"percent"}},
		{"weekly-usage-opus", "seven-day-opus-percent", []string{"percent"}},
		{"weekly-usage-sonnet", "seven-day-sonnet-percent", []string{"percent"}},
		{"weekly-reset-opus", "seven-day-opus-reset-minutes", []string{"countdown"}},
		{"weekly-reset-sonnet", "seven-day-sonnet-reset-minutes", []string{"countdown"}},
	}
	for _, w := range want {
		f, ok := FieldByName("claude-code", w.name)
		if !ok {
			t.Fatalf("field %q not found", w.name)
		}
		if f.SelfMetric != w.selfMetric {
			t.Errorf("field %q SelfMetric = %q, want %q", w.name, f.SelfMetric, w.selfMetric)
		}
		if len(f.Formats) != len(w.formats) || f.Formats[0] != w.formats[0] {
			t.Errorf("field %q Formats = %v, want %v", w.name, f.Formats, w.formats)
		}
		if f.Capability != "oauth-usage" {
			t.Errorf("field %q Capability = %q, want %q", w.name, f.Capability, "oauth-usage")
		}
		if f.Category != "claude" {
			t.Errorf("field %q Category = %q, want %q", w.name, f.Category, "claude")
		}
		if f.DisplayName == "" {
			t.Errorf("field %q has no DisplayName", w.name)
		}
		if f.Descriptions.EN == "" || f.Descriptions.JA == "" {
			t.Errorf("field %q missing localized descriptions: %+v", w.name, f.Descriptions)
		}
	}
}

// TestFields_CapabilityDefaultsEmpty verifies existing (pre-existing) fields
// keep an unset Capability, since only the new oauth-usage fields opt in.
func TestFields_CapabilityDefaultsEmpty(t *testing.T) {
	for _, f := range Fields("claude-code") {
		if f.Capability != "" && f.Capability != "oauth-usage" {
			t.Errorf("field %q has unexpected Capability = %q", f.Name, f.Capability)
		}
	}
}

// TestMetrics_OAuthUsageMetrics verifies the 7 new self metrics backing the
// extra-usage/weekly-usage-opus/weekly-usage-sonnet fields are registered
// with non-empty localized descriptions.
func TestMetrics_OAuthUsageMetrics(t *testing.T) {
	names := []string{
		"extra-usage-cost-usd",
		"extra-usage-limit-usd",
		"extra-usage-percent",
		"seven-day-opus-percent",
		"seven-day-sonnet-percent",
		"seven-day-opus-reset-minutes",
		"seven-day-sonnet-reset-minutes",
	}
	for _, name := range names {
		m, ok := MetricByName("claude-code", name)
		if !ok {
			t.Fatalf("metric %q not found", name)
		}
		if m.DisplayName == "" {
			t.Errorf("metric %q has no DisplayName", name)
		}
		if m.Descriptions.EN == "" || m.Descriptions.JA == "" {
			t.Errorf("metric %q missing localized descriptions: %+v", name, m.Descriptions)
		}
	}
}

// TestFields_ClaudeCodeSubagentCatalog verifies the tool="claude-code-subagent"
// field catalog (subagentStatusLine task fields) resolves with the expected
// self metrics, formats, and capability gate, and that every field carries
// full display metadata.
func TestFields_ClaudeCodeSubagentCatalog(t *testing.T) {
	want := []struct {
		name       string
		selfMetric string
		formats    []string
		capability string
	}{
		{"task-description", "", nil, ""},
		{"task-model", "", nil, ""},
		{"task-model-id", "", nil, ""},
		{"task-status", "", []string{"enum"}, ""},
		{"task-tokens", "task-token-count", []string{"number", "compact-number"}, ""},
		{"task-context-size", "task-context-window-tokens", []string{"number", "compact-number"}, ""},
		{"task-context-percent", "task-context-percent", []string{"percent"}, ""},
		{"task-duration", "task-duration-seconds", []string{"duration"}, ""},
		{"task-effort", "", []string{"enum"}, "subagent-effort"},
	}
	fields := Fields("claude-code-subagent")
	if len(fields) != len(want) {
		t.Fatalf("Fields(claude-code-subagent) count = %d, want %d", len(fields), len(want))
	}
	for _, w := range want {
		f, ok := FieldByName("claude-code-subagent", w.name)
		if !ok {
			t.Fatalf("field %q not found", w.name)
		}
		if f.SelfMetric != w.selfMetric {
			t.Errorf("field %q SelfMetric = %q, want %q", w.name, f.SelfMetric, w.selfMetric)
		}
		if len(f.Formats) != len(w.formats) {
			t.Errorf("field %q Formats = %v, want %v", w.name, f.Formats, w.formats)
		}
		if f.Capability != w.capability {
			t.Errorf("field %q Capability = %q, want %q", w.name, f.Capability, w.capability)
		}
		if f.Category != "subagent" {
			t.Errorf("field %q Category = %q, want subagent", w.name, f.Category)
		}
		if f.DisplayName == "" {
			t.Errorf("field %q has no DisplayName", w.name)
		}
		if f.Descriptions.EN == "" || f.Descriptions.JA == "" {
			t.Errorf("field %q missing localized descriptions: %+v", w.name, f.Descriptions)
		}
	}
}

// TestMetrics_ClaudeCodeSubagentCatalog verifies the subagent self metrics
// (plus the shared "width" metric) are registered with non-empty localized
// descriptions.
func TestMetrics_ClaudeCodeSubagentCatalog(t *testing.T) {
	names := []string{"task-token-count", "task-context-window-tokens", "task-context-percent", "task-duration-seconds", "width"}
	if got := Metrics("claude-code-subagent"); len(got) != len(names) {
		t.Fatalf("Metrics(claude-code-subagent) count = %d, want %d", len(got), len(names))
	}
	for _, name := range names {
		m, ok := MetricByName("claude-code-subagent", name)
		if !ok {
			t.Fatalf("metric %q not found", name)
		}
		if m.DisplayName == "" {
			t.Errorf("metric %q has no DisplayName", name)
		}
		if m.Descriptions.EN == "" || m.Descriptions.JA == "" {
			t.Errorf("metric %q missing localized descriptions: %+v", name, m.Descriptions)
		}
	}
}
