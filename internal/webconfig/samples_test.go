package webconfig

import (
	"strings"
	"testing"
	"time"
)

// TestPreviewFor_ExtraUsageFields verifies that fullSample (samples.go)
// populates the account fields the new extra-usage / per-model weekly-usage
// DSL fields read (Account.ExtraUsage, Account.SevenDayOpus,
// Account.SevenDaySonnet), so previewFor (catalog.go) renders a real value
// for each of them instead of falling back to previewFallback
// ("(no sample)"). This is what makes the config UI's palette show something
// useful for these fields before the usage-API probe has ever run.
func TestPreviewFor_ExtraUsageFields(t *testing.T) {
	now := time.Now()
	snap := fullSample(now)

	cases := []struct {
		field string
		want  string // substring expected in the rendered preview text
	}{
		{"extra-usage-cost", "$"},
		{"extra-usage-limit", "$"},
		{"extra-usage-percent", "%"},
		{"weekly-usage-opus", "%"},
		{"weekly-usage-sonnet", "%"},
		{"weekly-reset-opus", ""},
		{"weekly-reset-sonnet", ""},
	}
	for _, c := range cases {
		got := previewFor("claude-code", c.field, snap, now)
		if got.Text == "" || got.Text == previewFallback {
			t.Errorf("previewFor(%q) = %q, want a non-empty, non-fallback preview", c.field, got.Text)
			continue
		}
		if c.want != "" && !strings.Contains(got.Text, c.want) {
			t.Errorf("previewFor(%q) = %q, want it to contain %q", c.field, got.Text, c.want)
		}
	}
}
