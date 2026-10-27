package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
)

// TestRun_Set_RejectsBadEntriesBeforeReadingStdin confirms every malformed
// --set entry exits 2 without reading stdin, on both claude and render.
func TestRun_Set_RejectsBadEntriesBeforeReadingStdin(t *testing.T) {
	setupEnv(t)

	cases := []struct {
		entry      string
		wantStderr string
	}{
		{"extra-usage-limit-usd", "METRIC=VALUE"},
		{"no-such-metric=1", "unknown metric"},
		{"context-percent=5", "extra-usage-limit-usd"}, // exists but unsupported: lists supported names
		{"five-hour-percent=abc", "numeric"},
		{"extra-usage-percent=NaN", "numeric"},
	}
	for _, cmd := range []string{"claude", "render"} {
		for _, c := range cases {
			args := []string{cmd, "--set", c.entry}
			var out, errBuf bytes.Buffer
			code := Run(args, readFailer{t}, &out, &errBuf, "test-version", envMap(nil))
			if code != 2 {
				t.Errorf("Run(%q) exit = %d, want 2 (stderr: %s)", args, code, errBuf.String())
			}
			if !strings.Contains(errBuf.String(), c.wantStderr) {
				t.Errorf("Run(%q) stderr = %q, want it to mention %q", args, errBuf.String(), c.wantStderr)
			}
		}
	}
}

// metricSetDoc shows each overridable value both as a field and through
// when/color-rule gates, so a test can tell whether the display and the
// condition evaluation agree.
const metricSetDoc = `<statusloom version="1" tool="claude-code" color-level="none"><layout name="default" active="true"><line>` +
	`<text when="extra-usage-limit-usd eq 0">Extra: none</text>` +
	`<span prefix="Extra: " when="extra-usage-limit-usd gt 0"><field name="extra-usage-limit"/></span>` +
	`<span prefix=" X%=" optional="extra-usage-percent"><field name="extra-usage-percent"/></span>` +
	`<span prefix=" 7d=" optional="weekly-usage"><field name="weekly-usage"/></span>` +
	`<text when="seven-day-percent gt 90"> HOT</text>` +
	`<text when="account-type eq &quot;claude_team&quot;"> TEAM</text>` +
	`<span prefix=" type="><field name="account-type"/></span>` +
	`</line></layout></statusloom>`

func TestRun_Set_ExtraUsageFieldAndWhenAgree(t *testing.T) {
	setupEnv(t)
	writeDocument(t, "claude-code", metricSetDoc)
	data := fixture(t, "full.json")

	out, stderr, code := runCLI(t, []string{"claude", "--set", "extra-usage-limit-usd=50", "--set", "extra-usage-percent=72"}, data, nil)
	if code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(out, "Extra: $50.00") || strings.Contains(out, "Extra: none") {
		t.Errorf("output %q: want the gt-0 branch showing the overridden limit", out)
	}
	if !strings.Contains(out, "X%=72%") {
		t.Errorf("output %q: want overridden extra-usage percent 72%%", out)
	}

	out, stderr, code = runCLI(t, []string{"claude", "--set", "extra-usage-limit-usd=0"}, data, nil)
	if code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(out, "Extra: none") || strings.Contains(out, "Extra: $") {
		t.Errorf("output %q: want only the eq-0 branch", out)
	}
}

// TestRun_Set_WinsOverNewerCacheAndNeverPersists seeds the account cache with
// a seven-day window stdin does not carry (an override applied before the
// merge would lose to it: its zero ResetsAt is older), then checks the override still wins for both the field and the when gate,
// and that a following render without --set sees the original value again
// (a leaked override would win the same-window merge as the higher percent).
func TestRun_Set_WinsOverNewerCacheAndNeverPersists(t *testing.T) {
	setupEnv(t)
	writeDocument(t, "claude-code", metricSetDoc)
	now := time.Now()
	if err := cache.StoreAccount(cache.AccountCacheKey, cache.AccountUsage{
		Source:     "test",
		ObservedAt: now,
		ExpiresAt:  now.Add(5 * time.Minute),
		SevenDay:   &cache.RateWindowState{UsedPercentage: 63, ResetsAt: now.Add(100 * 24 * time.Hour)},
	}); err != nil {
		t.Fatalf("StoreAccount: %v", err)
	}
	data := fixture(t, "no-rate-limits.json")

	out, stderr, code := runCLI(t, []string{"claude"}, data, nil)
	if code != 0 {
		t.Fatalf("baseline exit = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(out, "7d=63%") || strings.Contains(out, "HOT") {
		t.Fatalf("baseline output %q: want the newer cached 63%% and no HOT", out)
	}
	out, stderr, code = runCLI(t, []string{"render", "--tool", "claude-code", "--set", "seven-day-percent=95"}, data, nil)
	if code != 0 {
		t.Fatalf("--set exit = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(out, "7d=95%") || !strings.Contains(out, "HOT") {
		t.Errorf("--set output %q: want field 95%% and the HOT gate both from the override", out)
	}
	out, stderr, code = runCLI(t, []string{"claude"}, data, nil)
	if code != 0 {
		t.Fatalf("follow-up exit = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(out, "7d=63%") || strings.Contains(out, "HOT") {
		t.Errorf("follow-up output %q: want the original 63%% after a --set render", out)
	}
}

func TestRun_Set_TextMetricDrivesWhenAndField(t *testing.T) {
	setupEnv(t)
	writeDocument(t, "claude-code", metricSetDoc)
	cfgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfgDir, ".claude.json"), []byte(`{"oauthAccount":{"organizationType":"claude_max","organizationUuid":"org-1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"CLAUDE_CONFIG_DIR": cfgDir}
	data := fixture(t, "full.json")

	out, stderr, code := runCLI(t, []string{"claude"}, data, env)
	if code != 0 {
		t.Fatalf("baseline exit = %d (stderr: %s)", code, stderr)
	}
	if strings.Contains(out, "TEAM") || !strings.Contains(out, "type=claude_max") {
		t.Fatalf("baseline output %q: want claude_max without TEAM", out)
	}

	out, stderr, code = runCLI(t, []string{"claude", "--set", "account-type=claude_team"}, data, env)
	if code != 0 {
		t.Fatalf("--set exit = %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(out, "TEAM") || !strings.Contains(out, "type=claude_team") {
		t.Errorf("--set output %q: want the TEAM gate and the field both showing claude_team", out)
	}
}
