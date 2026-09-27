package webconfig

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/usage"
)

// writeOAuthAccount writes a minimal .claude.json carrying the given
// organizationUuid into dir/.claude.json, the shape claudeaccount.Load reads.
func writeOAuthAccount(t *testing.T, dir, organizationUuid string) {
	t.Helper()
	body := `{"oauthAccount": {"emailAddress": "fujie@ai2-jp.com", "organizationUuid": "` + organizationUuid + `"}}`
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write .claude.json: %v", err)
	}
}

// TestUsageProbe_DistinctOrganizationsDoNotShareCacheEntry is the ccprofile
// Team/Max integration test statusloom-local-development-plan.md's
// account-cache-key design calls for: two account profiles distinguished
// only by organizationUuid (same email - a real fujie@ai2-jp.com scenario:
// the same person's Team seat and Max subscription) must land in
// independent account-usage cache entries, so switching CLAUDE_CONFIG_DIR
// between them (as ccprofile does) never lets one account's extra-usage /
// per-model weekly data leak into the other's.
//
// It drives this through GET /api/usage/probe (handleUsageProbe ->
// persistAccountUsage) exactly as the real config UI would, switching
// CLAUDE_CONFIG_DIR to a different profile directory between the two probe
// calls - the same mechanism ccprofile itself uses to switch accounts.
func TestUsageProbe_DistinctOrganizationsDoNotShareCacheEntry(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	isolateClaudeConfig(t)

	profileMax := t.TempDir()
	writeOAuthAccount(t, profileMax, "ab0f74df-1d50-456a-85a0-cbde151d1bd6")
	profileTeam := t.TempDir()
	writeOAuthAccount(t, profileTeam, "055b1111-82d7-4f94-972c-0c6c799a9894")

	ts := startTestServer(t, time.Hour)

	// First probe: acting as the Max profile.
	t.Setenv("CLAUDE_CONFIG_DIR", profileMax)
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "tok-max", nil },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			return &usage.Report{
				Extra: &usage.Extra{IsEnabled: true, MonthlyLimit: fptr(20), UsedCredits: fptr(1), Utilization: fptr(5)},
			}, http.StatusOK, nil
		},
	)
	if resp := getUsageProbe(t, ts); !resp.Available {
		t.Fatalf("max profile probe = %+v, want Available", resp)
	}

	// Second probe: switch CLAUDE_CONFIG_DIR to the Team profile - the same
	// switch ccprofile performs on the real machine.
	t.Setenv("CLAUDE_CONFIG_DIR", profileTeam)
	withUsageProbeSeams(t,
		func(getenv func(string) string) (string, error) { return "tok-team", nil },
		func(ctx context.Context, token, version string) (*usage.Report, int, error) {
			return &usage.Report{
				Extra: &usage.Extra{IsEnabled: true, MonthlyLimit: fptr(500), UsedCredits: fptr(300), Utilization: fptr(60)},
			}, http.StatusOK, nil
		},
	)
	if resp := getUsageProbe(t, ts); !resp.Available {
		t.Fatalf("team profile probe = %+v, want Available", resp)
	}

	now := time.Now()
	maxEnv, _, ok := cache.LoadAccountUsage("ab0f74df-1d50-456a-85a0-cbde151d1bd6", now)
	if !ok || maxEnv == nil || maxEnv.ExtraUsage == nil {
		t.Fatalf("max profile's own cache entry missing: %+v", maxEnv)
	}
	if got := *maxEnv.ExtraUsage.UsedCredits; got != 1 {
		t.Errorf("max profile UsedCredits = %v, want 1 (must not have been overwritten by the team probe)", got)
	}

	teamEnv, _, ok := cache.LoadAccountUsage("055b1111-82d7-4f94-972c-0c6c799a9894", now)
	if !ok || teamEnv == nil || teamEnv.ExtraUsage == nil {
		t.Fatalf("team profile's own cache entry missing: %+v", teamEnv)
	}
	if got := *teamEnv.ExtraUsage.UsedCredits; got != 300 {
		t.Errorf("team profile UsedCredits = %v, want 300", got)
	}

	// The fallback/shared key must have received neither write.
	if _, _, ok := cache.LoadAccountUsage(cache.AccountCacheKey, now); ok {
		t.Error("cache.AccountCacheKey entry exists, want none: both probes had a resolvable organizationUuid and must not fall back to the shared key")
	}
}
