// Package claudecfg resolves paths inside Claude Code's own configuration
// directory. It is the single source of truth for those locations; nothing
// else in statusloom may hardcode "~/.claude".
//
// Every function takes a getenv so callers can inject a fake environment in
// tests; pass os.Getenv in production code.
package claudecfg

import (
	"os"
	"path/filepath"
)

// Dir returns Claude Code's configuration directory, resolved the same way
// Claude Code itself resolves it:
//
//  1. $CLAUDE_CONFIG_DIR when set (used for profile switching).
//  2. $HOME/.claude otherwise.
//
// It returns "" when neither $CLAUDE_CONFIG_DIR nor a home directory can be
// determined; callers must treat that as "location unknown" rather than
// joining onto an empty path.
func Dir(getenv func(string) string) string {
	if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home := homeDir(getenv)
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// SettingsPath returns Claude Code's user settings.json, the file
// `statusloom setup claude-code` writes statusLine into. The file need not
// exist.
func SettingsPath(getenv func(string) string) string {
	dir := Dir(getenv)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "settings.json")
}

// CredentialsPath returns Claude Code's stored OAuth credentials file. On
// macOS the credentials usually live in the login Keychain instead, so a
// missing file here is normal.
func CredentialsPath(getenv func(string) string) string {
	dir := Dir(getenv)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, ".credentials.json")
}

// GlobalConfigPath returns Claude Code's machine-managed .claude.json (the
// file holding per-project entries and trust flags).
//
// Note the asymmetry with Dir: with $CLAUDE_CONFIG_DIR set the file lives
// INSIDE that directory ($CLAUDE_CONFIG_DIR/.claude.json), but by default it
// is a SIBLING of the config dir ($HOME/.claude.json, not
// $HOME/.claude/.claude.json). This mirrors Claude Code's own behaviour.
func GlobalConfigPath(getenv func(string) string) string {
	if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, ".claude.json")
	}
	home := homeDir(getenv)
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".claude.json")
}

// homeDir resolves the home directory from the injected environment first
// (so tests can redirect it without touching the real one) and falls back to
// os.UserHomeDir. It returns "" when the home directory is unknown.
func homeDir(getenv func(string) string) string {
	if h := getenv("HOME"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}
