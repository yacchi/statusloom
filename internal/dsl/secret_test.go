package dsl

import "testing"

func TestIsSecretEnvName(t *testing.T) {
	secret := []string{
		"CLAUDE_CODE_OAUTH_TOKEN",
		"AWS_SECRET_ACCESS_KEY",
		"aws_access_key_id", // lower case still matches
		"GITHUB_TOKEN",
		"DB_PASSWORD",
		"MY_PASSWD",
		"GOOGLE_CREDENTIALS",
		"SSH_AUTH_SOCK",     // fail-closed over-match, documented
		"KEYBOARD_LAYOUT",   // fail-closed over-match, documented
		"AWS_SESSION_TOKEN", //
		"HTTP_COOKIE",
		"PRIVATE_PEM",
		"REQUEST_SIGNATURE",
	}
	for _, name := range secret {
		if !IsSecretEnvName(name) {
			t.Errorf("IsSecretEnvName(%q) = false, want true", name)
		}
	}

	plain := []string{
		"AWS_PROFILE",
		"TERM_PROGRAM",
		"LANG",
		"SHELL",
		"KUBECONFIG",
		"NODE_ENV",
		"COLUMNS",
		"CLAUDE_CONFIG_DIR",
		"",
	}
	for _, name := range plain {
		if IsSecretEnvName(name) {
			t.Errorf("IsSecretEnvName(%q) = true, want false", name)
		}
	}
}
