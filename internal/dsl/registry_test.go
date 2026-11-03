package dsl

import (
	"slices"
	"testing"
)

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
	// 84 base claude-code fields (67 + 7 account-* + env + 8 cache-* + source) + 9 merged
	// subagent task-* fields.
	fields := Fields("claude-code")
	if len(fields) != 93 {
		t.Errorf("Fields count = %d, want 93", len(fields))
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
		"total-token-speed":    "total-token-speed",
		"extra-usage-cost":     "extra-usage-cost-usd",
		"extra-usage-limit":    "extra-usage-limit-usd",
		"extra-usage-percent":  "extra-usage-percent",
		"weekly-usage-opus":    "seven-day-opus-percent",
		"weekly-usage-sonnet":  "seven-day-sonnet-percent",
		"weekly-reset-opus":    "seven-day-opus-reset-minutes",
		"weekly-reset-sonnet":  "seven-day-sonnet-reset-minutes",
		"cache-warm":           "cache-warm",
		"cache-ttl":            "cache-ttl",
		"cache-expires-at":     "cache-expires-minutes",
		"cache-hit-ratio":      "cache-hit-ratio-percent",
		"cache-requests":       "cache-requests",
		"cache-misses":         "cache-misses",
		"cache-recache-tokens": "cache-recache-tokens",
		"source":               SourceSelfMetric,
		// Merged subagent task-* fields with self metrics.
		"task-tokens":          "task-token-count",
		"task-context-size":    "task-context-window-tokens",
		"task-context-percent": "task-context-percent",
		"task-duration":        "task-duration-seconds",
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
		if f.Category != "common" && f.Category != "claude" && f.Category != "subagent" {
			t.Errorf("field %q Category = %q, want common|claude|subagent", f.Name, f.Category)
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
	// 69 base claude-code metrics (51 original + 4 *-projected-percent + 6
	// account-* identity metrics + 7 cache-* + source-stale) + 4 merged subagent task-* metrics (the
	// shared widthMetric is deduplicated across the two catalogs).
	m := Metrics("claude-code")
	if len(m) != 73 {
		t.Errorf("Metrics count = %d, want 73", len(m))
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
		// oauth-usage (extra-usage/weekly-*) and subagent-effort (merged
		// task-effort) are the only non-empty capabilities.
		if f.Capability != "" && f.Capability != "oauth-usage" && f.Capability != "subagent-effort" {
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

// TestFields_ClaudeCodeSubagentCatalog verifies the subagentStatusLine
// task-* fields merged into the unified tool="claude-code" catalog resolve
// with the expected self metrics, formats, and capability gate, and that
// every field carries full display metadata.
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
	for _, w := range want {
		f, ok := FieldByName("claude-code", w.name)
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
// merged into the unified tool="claude-code" catalog (plus the shared
// "width" metric) are registered with non-empty localized descriptions.
func TestMetrics_ClaudeCodeSubagentCatalog(t *testing.T) {
	names := []string{"task-token-count", "task-context-window-tokens", "task-context-percent", "task-duration-seconds", "width"}
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

// TestFields_CacheEntriesFollowCacheHitRate pins the prompt-cache block's
// position (directly after cache-hit-rate) and the self metric and formats the
// renderer and validator resolve for each entry.
func TestFields_CacheEntriesFollowCacheHitRate(t *testing.T) {
	want := []struct {
		name       string
		selfMetric string
		formats    []string
	}{
		{"cache-warm", "cache-warm", []string{"enum"}},
		{"cache-ttl", "cache-ttl", []string{"enum"}},
		{"cache-expires-at", "cache-expires-minutes", []string{"countdown", "datetime"}},
		{"cache-hit-ratio", "cache-hit-ratio-percent", []string{"percent"}},
		{"cache-requests", "cache-requests", []string{"number", "compact-number"}},
		{"cache-misses", "cache-misses", []string{"number", "compact-number"}},
		{"cache-recache-tokens", "cache-recache-tokens", []string{"number", "compact-number"}},
		{"cache-last-miss-cause", "", []string{"enum"}},
	}
	fields := Fields("claude-code")
	start := -1
	for i, f := range fields {
		if f.Name == "cache-hit-rate" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("cache-hit-rate not found")
	}
	for i, w := range want {
		pos := start + 1 + i
		if pos >= len(fields) {
			t.Fatalf("cache entry %q missing after cache-hit-rate", w.name)
		}
		f := fields[pos]
		if f.Name != w.name {
			t.Fatalf("fields[%d] = %q, want %q", pos, f.Name, w.name)
		}
		if f.SelfMetric != w.selfMetric {
			t.Errorf("field %q SelfMetric = %q, want %q", w.name, f.SelfMetric, w.selfMetric)
		}
		if !slices.Equal(f.Formats, w.formats) {
			t.Errorf("field %q Formats = %v, want %v", w.name, f.Formats, w.formats)
		}
	}
}

// TestMetrics_CacheEntriesFollowCacheHitPercent pins the cache-* metric block
// (directly after cache-hit-percent) and its Text/Percent kinds: cache-ttl is
// the only string metric and cache-hit-ratio-percent the only percentage.
func TestMetrics_CacheEntriesFollowCacheHitPercent(t *testing.T) {
	want := []string{
		"cache-warm", "cache-ttl", "cache-expires-minutes", "cache-hit-ratio-percent",
		"cache-requests", "cache-misses", "cache-recache-tokens",
	}
	metrics := Metrics("claude-code")
	start := -1
	for i, m := range metrics {
		if m.Name == "cache-hit-percent" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("cache-hit-percent not found")
	}
	for i, name := range want {
		pos := start + 1 + i
		if pos >= len(metrics) || metrics[pos].Name != name {
			t.Fatalf("metric after cache-hit-percent at offset %d = %q, want %q", i+1, metricNameAt(metrics, pos), name)
		}
		m := metrics[pos]
		if m.Text != (name == "cache-ttl") {
			t.Errorf("metric %q Text = %v", name, m.Text)
		}
		if m.Percent != (name == "cache-hit-ratio-percent") {
			t.Errorf("metric %q Percent = %v", name, m.Percent)
		}
	}
	ttl, _ := MetricByName("claude-code", "cache-ttl")
	if !slices.Equal(ttl.Values, []string{"5m", "1h"}) {
		t.Errorf("cache-ttl Values = %v, want [5m 1h]", ttl.Values)
	}
}

func metricNameAt(ms []MetricDef, pos int) string {
	if pos < 0 || pos >= len(ms) {
		return "<out of range>"
	}
	return ms[pos].Name
}

// TestFields_SourceFieldAfterEnv pins the source field's position and its
// parameterization: RequiresSource is set on source alone, so validation rejects
// the source/select/fallback attributes on every other field.
func TestFields_SourceFieldAfterEnv(t *testing.T) {
	fields := Fields("claude-code")
	envIdx, srcIdx := -1, -1
	for i, f := range fields {
		switch f.Name {
		case "env":
			envIdx = i
		case "source":
			srcIdx = i
		}
	}
	if envIdx < 0 || srcIdx != envIdx+1 {
		t.Fatalf("source at %d, env at %d; want source directly after env", srcIdx, envIdx)
	}
	src := fields[srcIdx]
	if !src.RequiresSource {
		t.Error("source field must set RequiresSource")
	}
	if src.SelfMetric != SourceSelfMetric {
		t.Errorf("source SelfMetric = %q, want %q", src.SelfMetric, SourceSelfMetric)
	}
	for _, f := range fields {
		if f.RequiresSource && f.Name != "source" {
			t.Errorf("field %q sets RequiresSource; only source may", f.Name)
		}
	}
}

// TestMetrics_SourceStaleBeforeWidth pins source-stale as a bool, source-parameterized
// metric placed directly before width, and checks that source-value is never a metric.
func TestMetrics_SourceStaleBeforeWidth(t *testing.T) {
	metrics := Metrics("claude-code")
	staleIdx, widthIdx := -1, -1
	for i, m := range metrics {
		switch m.Name {
		case "source-stale":
			staleIdx = i
		case "width":
			widthIdx = i
		}
	}
	if staleIdx < 0 || widthIdx != staleIdx+1 {
		t.Fatalf("source-stale at %d, width at %d; want source-stale directly before width", staleIdx, widthIdx)
	}
	stale := metrics[staleIdx]
	if stale.Param != "source" {
		t.Errorf("source-stale Param = %q, want %q", stale.Param, "source")
	}
	if stale.Text || stale.Percent {
		t.Errorf("source-stale must be a bool metric, got Text=%v Percent=%v", stale.Text, stale.Percent)
	}
}

func TestMetricByName_SourceValueIsNotAMetric(t *testing.T) {
	if _, ok := MetricByName("claude-code", SourceSelfMetric); ok {
		t.Fatalf("%q must not be registered as a metric", SourceSelfMetric)
	}
}

// TestTranscriptFlags pins the Transcript catalog flag to exactly the 13
// transcript-derived fields and same-named metrics, so the render path schedules
// transcript refreshes only for documents that actually use one.
func TestTranscriptFlags(t *testing.T) {
	want := map[string]bool{
		"compaction-count": true, "compaction-auto": true, "compaction-manual": true,
		"compaction-unknown": true, "compaction-tokens-reclaimed": true,
		"session-input-tokens": true, "session-output-tokens": true,
		"session-cache-creation-tokens": true, "session-cache-read-tokens": true,
		"session-total-tokens": true, "input-token-speed": true,
		"output-token-speed": true, "total-token-speed": true,
	}
	fieldHits, metricHits := 0, 0
	for _, f := range Fields("claude-code") {
		if f.Transcript != want[f.Name] {
			t.Errorf("field %q Transcript = %v, want %v", f.Name, f.Transcript, want[f.Name])
		}
		if f.Transcript {
			fieldHits++
		}
	}
	for _, m := range Metrics("claude-code") {
		if m.Transcript != want[m.Name] {
			t.Errorf("metric %q Transcript = %v, want %v", m.Name, m.Transcript, want[m.Name])
		}
		if m.Transcript {
			metricHits++
		}
	}
	if fieldHits != len(want) || metricHits != len(want) {
		t.Errorf("Transcript field/metric counts = %d/%d, want %d/%d", fieldHits, metricHits, len(want), len(want))
	}
}
