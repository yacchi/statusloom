package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

var update = flag.Bool("update", false, "update golden files")

func strptr(s string) *string           { return &s }
func f64ptr(f float64) *float64         { return &f }
func money(v float64) *schema.CostUsage { return &schema.CostUsage{USD: v} }

// fixedNow is the injected clock used by countdown widgets in tests.
var fixedNow = time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)

// fullSnapshot returns a snapshot with every widget's data populated.
func fullSnapshot() schema.StatusSnapshot {
	return schema.StatusSnapshot{
		Tool: schema.ToolSnapshot{ID: schema.ToolClaudeCode, Version: "2.1.153"},
		Session: schema.SessionSnapshot{
			ID:              "sess-1",
			Model:           &schema.ModelInfo{ID: "claude-opus", DisplayName: "Opus"},
			ReasoningEffort: strptr("high"),
			Context: &schema.ContextUsage{
				TotalInputTokens: 64000,
				WindowSize:       200000,
				UsedPercentage:   f64ptr(32),
			},
			Cost: money(1.23),
		},
		Repository: &schema.RepositorySnapshot{
			Branch:  "main",
			Added:   12,
			Deleted: 3,
		},
		Account: schema.AccountSnapshot{
			FiveHour: &schema.RateWindow{
				UsedPercentage: 27,
				ResetsAt:       fixedNow.Add(1*time.Hour + 23*time.Minute),
			},
			SevenDay: &schema.RateWindow{
				UsedPercentage: 79,
				ResetsAt:       fixedNow.Add(2*24*time.Hour + 3*time.Hour),
			},
		},
		System: schema.SystemSnapshot{Cwd: "/Users/dev/statusloom"},
	}
}

func compareGolden(t *testing.T, name string, lines []string) {
	t.Helper()
	got := strings.Join(lines, "\n")
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden %s (run with -update to create): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %s\n got: %q\nwant: %q", name, got, string(want))
	}
}
