package dsl

import "strings"

// secretEnvSubstrings are the substrings that mark an environment variable
// name as likely holding a credential. A status line is rendered into a
// terminal that gets screen-shared and screenshotted, and Statusloom Room
// presets are authored by strangers, so IsSecretEnvName deliberately
// fails CLOSED: it over-matches (KEYBOARD_LAYOUT and SSH_AUTH_SOCK are
// treated as secret) because a masked non-secret is a cosmetic annoyance
// while an unmasked secret is a credential leak. Authors who hit a false
// positive opt out per-field with unmask (markup.md "env").
var secretEnvSubstrings = []string{
	"TOKEN",
	"SECRET",
	"KEY",
	"PASSWORD",
	"PASSWD",
	"CREDENTIAL",
	"AUTH",
	"SESSION",
	"COOKIE",
	"PRIVATE",
	"SIGNATURE",
}

// SecretMask is the placeholder rendered in place of a secret-looking
// environment variable's value. It is a fixed width so it leaks nothing about
// the real value, not even its length.
const SecretMask = "***"

// IsSecretEnvName reports whether an environment variable name looks like it
// holds a credential. Matching is case-insensitive and substring-based; see
// secretEnvSubstrings for the fail-closed rationale.
func IsSecretEnvName(name string) bool {
	upper := strings.ToUpper(name)
	for _, s := range secretEnvSubstrings {
		if strings.Contains(upper, s) {
			return true
		}
	}
	return false
}
