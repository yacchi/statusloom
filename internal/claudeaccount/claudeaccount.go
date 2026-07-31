// Package claudeaccount reads the identity of the account Claude Code is
// currently logged in as out of the local .claude.json it maintains.
//
// This is a plain local file read - no network, no credentials - so it is
// safe on the render path. .claude.json is large (hundreds of KB for a
// long-lived install: it also stores per-project state), so Load streams the
// top level and decodes only the "oauthAccount" object rather than
// unmarshalling the whole document.
package claudeaccount

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/yacchi/statusloom/internal/claudecfg"
	"github.com/yacchi/statusloom/internal/schema"
)

// Provider returns a memoized loader suitable for render.Options.Profile: it
// reads .claude.json at most once, and only if something actually asks for an
// account-* field. A status line whose document uses none never opens the
// file. Pass os.Getenv unless you are injecting an environment.
func Provider(getenv func(string) string) func() *schema.AccountProfile {
	return sync.OnceValue(func() *schema.AccountProfile { return Load(getenv) })
}

// oauthAccount is the subset of .claude.json's oauthAccount object
// statusloom surfaces. Unknown members are ignored, so Claude Code adding or
// removing keys never breaks the read.
type oauthAccount struct {
	EmailAddress      string `json:"emailAddress"`
	DisplayName       string `json:"displayName"`
	OrganizationName  string `json:"organizationName"`
	OrganizationRole  string `json:"organizationRole"`
	UserRateLimitTier string `json:"userRateLimitTier"`
	SeatTier          string `json:"seatTier"`
}

// Load reads the logged-in account profile. It returns nil when the profile
// cannot be determined for any reason - the file is missing, unreadable,
// malformed, or has no oauthAccount block. Callers treat nil as "no account
// fields available" and render those fields empty; this never errors, so a
// broken or half-written .claude.json can never fail a status line render.
//
// getenv is injected so callers (and tests) control which config dir is
// consulted; pass os.Getenv in production code.
func Load(getenv func(string) string) *schema.AccountProfile {
	path := claudecfg.GlobalConfigPath(getenv)
	if path == "" {
		return nil
	}
	acct, ok := scanOAuthAccount(path)
	if !ok {
		return nil
	}
	p := &schema.AccountProfile{
		Email:        acct.EmailAddress,
		DisplayName:  acct.DisplayName,
		Organization: acct.OrganizationName,
		Role:         acct.OrganizationRole,
		Plan:         acct.UserRateLimitTier,
		Seat:         acct.SeatTier,
	}
	// An oauthAccount present but empty of every surfaced key is
	// indistinguishable from absent, as far as the fields go.
	if *p == (schema.AccountProfile{}) {
		return nil
	}
	return p
}

// scanOAuthAccount streams path's top-level object and decodes the
// "oauthAccount" member, skipping every other value without materializing
// it. It stops as soon as the key is found.
func scanOAuthAccount(path string) (oauthAccount, bool) {
	var out oauthAccount

	f, err := os.Open(path)
	if err != nil {
		return out, false
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	// Consume the opening '{'. Anything else (a JSON array, a bare value,
	// truncated content) means this is not a config document we understand.
	tok, err := dec.Token()
	if err != nil {
		return out, false
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return out, false
	}

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return out, false
		}
		key, ok := tok.(string)
		if !ok {
			return out, false
		}
		if key == "oauthAccount" {
			if err := dec.Decode(&out); err != nil {
				return oauthAccount{}, false
			}
			return out, true
		}
		// Skip this member's value. RawMessage captures it without
		// building the Go values inside it.
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return out, false
		}
	}
	return out, false
}
