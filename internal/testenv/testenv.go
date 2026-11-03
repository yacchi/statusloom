// Package testenv is a safety net for test binaries: Main points every
// location statusloom (and Claude Code path resolution) reads or writes at a
// throwaway directory before any test runs, so a test that forgets its own
// isolation can never touch the developer's real config, state, cache or
// Claude settings. It also unsets CLAUDE_PID, which names the developer's real
// Claude Code process. Tests still isolate themselves per test (t.Setenv
// overrides these defaults); this only guarantees the fallback is harmless.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// isolatedVars are the environment variables that decide where statusloom and
// claudecfg resolve files. HOME covers the XDG defaults (~/.config,
// ~/.cache, ~/.local/state) and ~/.claude / ~/.claude.json. XDG_STATE_HOME
// decides the state dir. CLAUDE_PID names the user's real Claude Code process,
// so it is unset.
var isolatedVars = []string{
	"HOME",
	"XDG_CONFIG_HOME",
	"XDG_CACHE_HOME",
	"XDG_STATE_HOME",
	"STATUSLOOM_CONFIG",
	"STATUSLOOM_CACHE_DIR",
	"CLAUDE_CONFIG_DIR",
	"CLAUDE_PID",
}

// Main isolates the environment, runs the tests and exits. Use it as the
// whole body of a package's TestMain: `func TestMain(m *testing.M) { testenv.Main(m) }`.
func Main(m *testing.M) {
	os.Exit(run(m))
}

// Run isolates the environment and runs the tests, returning the exit code,
// for packages whose TestMain has more setup of its own.
func Run(m *testing.M) int {
	return run(m)
}

func run(m *testing.M) int {
	root, err := os.MkdirTemp("", "statusloom-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "testenv: %v\n", err)
		return 1
	}
	defer os.RemoveAll(root)
	home := filepath.Join(root, "home")
	values := map[string]string{
		"HOME":                 home,
		"XDG_CONFIG_HOME":      filepath.Join(home, ".config"),
		"XDG_CACHE_HOME":       filepath.Join(home, ".cache"),
		"XDG_STATE_HOME":       filepath.Join(home, ".local", "state"),
		"STATUSLOOM_CONFIG":    filepath.Join(home, ".config", "statusloom", "statusloom.json"),
		"STATUSLOOM_CACHE_DIR": filepath.Join(home, ".cache", "statusloom"),
		"CLAUDE_CONFIG_DIR":    "",
		"CLAUDE_PID":           "",
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "testenv: %v\n", err)
		return 1
	}
	for _, k := range isolatedVars {
		if v := values[k]; v == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, v)
		}
	}
	return m.Run()
}
