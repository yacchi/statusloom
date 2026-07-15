package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// credentialsPath resolves the path to Claude Code's stored OAuth
// credentials file. It is an unexported var (rather than a plain
// function) so tests can override it.
var credentialsPath = func(getenv func(string) string) string {
	home := getenv("HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	return filepath.Join(home, ".claude", ".credentials.json")
}

// credentialsFile is the minimal shape of Claude Code's
// .credentials.json needed to extract the OAuth access token. Other
// fields (refreshToken, expiresAt, scopes, ...) are tolerated but not
// parsed.
type credentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

// extractAccessToken parses raw JSON in the credentialsFile shape and
// returns the access token, if any.
func extractAccessToken(data []byte) string {
	var creds credentialsFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return ""
	}
	return creds.ClaudeAiOauth.AccessToken
}

// Token resolves an OAuth token for the usage endpoint, trying, in
// order:
//
//  1. The CLAUDE_CODE_OAUTH_TOKEN environment variable.
//  2. Claude Code's stored credentials file (see credentialsPath).
//  3. The macOS keychain entry Claude Code stores credentials under
//     (no-op on non-darwin platforms).
//
// It never includes the token value in any error it returns.
func Token(getenv func(string) string) (string, error) {
	if v := getenv("CLAUDE_CODE_OAUTH_TOKEN"); v != "" {
		return v, nil
	}

	if data, err := os.ReadFile(credentialsPath(getenv)); err == nil {
		if token := extractAccessToken(data); token != "" {
			return token, nil
		}
	}

	if data, err := readKeychain(); err == nil {
		if token := extractAccessToken(data); token != "" {
			return token, nil
		}
	}

	return "", ErrNoToken
}
