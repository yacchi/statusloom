package claudeaccount

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yacchi/statusloom/internal/schema"
)

// writeConfig writes body as .claude.json inside a temp CLAUDE_CONFIG_DIR and
// returns a getenv pointing at it.
func writeConfig(t *testing.T, body string) func(string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return dir
		}
		return ""
	}
}

// TestLoad_TeamAccount covers a Team seat, where the user-scoped rate-limit
// tier is populated and seatTier exists.
func TestLoad_TeamAccount(t *testing.T) {
	getenv := writeConfig(t, `{
	  "numStartups": 42,
	  "projects": {"/a": {"allowedTools": []}},
	  "oauthAccount": {
	    "emailAddress": "dev@example.com",
	    "displayName": "Dev User",
	    "organizationName": "Example Inc",
	    "organizationRole": "primary_owner",
	    "organizationType": "claude_team",
	    "userRateLimitTier": "default_claude_max_5x",
	    "organizationRateLimitTier": "default_raven",
	    "seatTier": "team_tier_1",
	    "accountUuid": "ignored"
	  },
	  "tipsHistory": {"x": 1}
	}`)

	got := Load(getenv)
	if got == nil {
		t.Fatal("Load returned nil, want a profile")
	}
	want := schema.AccountProfile{
		Email:        "dev@example.com",
		DisplayName:  "Dev User",
		Organization: "Example Inc",
		Role:         "primary_owner",
		Type:         "claude_team",
		// The user-scoped tier wins over the org-wide one.
		Plan: "default_claude_max_5x",
		Seat: "team_tier_1",
	}
	if *got != want {
		t.Errorf("Load() = %+v, want %+v", *got, want)
	}
}

// TestLoad_IndividualAccount covers an individual (Max) subscription, whose
// oauthAccount carries JSON null for seatTier and userRateLimitTier and puts
// the actual tier in organizationRateLimitTier. Before the fallback,
// account-plan and account-seat both came out empty here, which is the bug
// this pins.
func TestLoad_IndividualAccount(t *testing.T) {
	getenv := writeConfig(t, `{
	  "oauthAccount": {
	    "emailAddress": "dev@example.com",
	    "displayName": "Dev User",
	    "organizationName": "dev@example.com's Organization",
	    "organizationRole": "admin",
	    "organizationType": "claude_max",
	    "userRateLimitTier": null,
	    "organizationRateLimitTier": "default_claude_max_20x",
	    "seatTier": null
	  }
	}`)

	got := Load(getenv)
	if got == nil {
		t.Fatal("Load returned nil, want a profile")
	}
	if got.Plan != "default_claude_max_20x" {
		t.Errorf("Plan = %q, want default_claude_max_20x (fallback to organizationRateLimitTier)", got.Plan)
	}
	if got.Type != "claude_max" {
		t.Errorf("Type = %q, want claude_max", got.Type)
	}
	// An individual subscription genuinely has no seat; empty is correct.
	if got.Seat != "" {
		t.Errorf("Seat = %q, want empty", got.Seat)
	}
}

// TestLoad_PartialAccount covers a personal (non-org) account, where the
// organization keys are absent entirely.
func TestLoad_PartialAccount(t *testing.T) {
	getenv := writeConfig(t, `{"oauthAccount": {"emailAddress": "solo@example.com"}}`)
	got := Load(getenv)
	if got == nil {
		t.Fatal("Load returned nil, want a profile")
	}
	if got.Email != "solo@example.com" {
		t.Errorf("Email = %q, want solo@example.com", got.Email)
	}
	if got.Organization != "" || got.Plan != "" {
		t.Errorf("absent keys populated: %+v", *got)
	}
}

func TestLoad_NilCases(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"no oauthAccount member", `{"numStartups": 1, "projects": {}}`},
		{"oauthAccount empty of surfaced keys", `{"oauthAccount": {"accountUuid": "u"}}`},
		{"oauthAccount is null", `{"oauthAccount": null}`},
		{"malformed json", `{"oauthAccount": {`},
		{"truncated mid-document", `{"projects": {"/a": {}}`},
		{"top level is an array", `[{"oauthAccount": {"emailAddress": "x@y"}}]`},
		{"empty file", ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Load(writeConfig(t, tt.body)); got != nil {
				t.Errorf("Load() = %+v, want nil", *got)
			}
		})
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir() // no .claude.json inside
	getenv := func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return dir
		}
		return ""
	}
	if got := Load(getenv); got != nil {
		t.Errorf("Load() = %+v, want nil", *got)
	}
}

// TestLoad_UnresolvableLocation covers the case where neither
// CLAUDE_CONFIG_DIR nor HOME is set: Load must not read anything.
func TestLoad_UnresolvableLocation(t *testing.T) {
	// os.UserHomeDir still resolves from the real environment, so this only
	// asserts Load does not panic and returns a nil-or-real result without
	// consulting an empty path.
	if got := Load(func(string) string { return "" }); got != nil && got.Email == "" && got.Plan == "" {
		t.Errorf("Load() = %+v, want nil or a populated profile", *got)
	}
}

// TestLoad_HonorsProfileSwitch pins that two config dirs yield two
// identities - the reason this reads through claudecfg rather than ~/.claude.
func TestLoad_HonorsProfileSwitch(t *testing.T) {
	a := writeConfig(t, `{"oauthAccount": {"emailAddress": "a@example.com"}}`)
	b := writeConfig(t, `{"oauthAccount": {"emailAddress": "b@example.com"}}`)
	if got := Load(a); got == nil || got.Email != "a@example.com" {
		t.Errorf("profile a = %+v, want a@example.com", got)
	}
	if got := Load(b); got == nil || got.Email != "b@example.com" {
		t.Errorf("profile b = %+v, want b@example.com", got)
	}
}
