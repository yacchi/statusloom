package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupClaudeCode(t *testing.T) {
	tests := []struct {
		name        string
		initial     string
		args        []string
		stdin       string
		wantCode    int
		wantWritten bool
	}{
		{"fresh", "", nil, "", 0, true},
		{"replace yes", `{"theme":"dark","statusLine":{"type":"command","command":"old"}}`, []string{"--yes"}, "", 0, true},
		{"abort", `{"statusLine":{"command":"old"}}`, nil, "n\n", 1, false},
		{"dry run", `{"statusLine":{"command":"old"}}`, []string{"--dry-run"}, "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "nested", "settings.json")
			if tt.initial != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tt.initial), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			args := append([]string{"setup", "claude-code", "--settings", path}, tt.args...)
			_, _, code := runCLI(t, args, []byte(tt.stdin), nil)
			if code != tt.wantCode {
				t.Fatalf("code=%d want=%d", code, tt.wantCode)
			}
			got, err := os.ReadFile(path)
			if tt.wantWritten {
				if err != nil {
					t.Fatal(err)
				}
				var doc map[string]any
				if json.Unmarshal(got, &doc) != nil || !strings.Contains(string(got), "statusloom claude") {
					t.Fatalf("settings=%q", got)
				}
				if tt.initial != "" {
					if doc["theme"] != "dark" && tt.name == "replace yes" {
						t.Error("sibling key lost")
					}
					matches, _ := filepath.Glob(path + ".bak.*")
					if len(matches) != 1 {
						t.Fatalf("backups=%v", matches)
					}
					backup, _ := os.ReadFile(matches[0])
					if string(backup) != tt.initial {
						t.Errorf("backup=%q", backup)
					}
				}
			} else if tt.initial != "" && string(got) != tt.initial {
				t.Errorf("file changed: %q", got)
			}
		})
	}
}

func TestSetupClaudeCodeIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	args := []string{"setup", "claude-code", "--settings", path}
	if _, _, code := runCLI(t, args, nil, nil); code != 0 {
		t.Fatal(code)
	}
	before, _ := os.ReadFile(path)
	out, _, code := runCLI(t, args, nil, nil)
	after, _ := os.ReadFile(path)
	if code != 0 || out != "already configured\n" || string(before) != string(after) {
		t.Fatalf("code=%d out=%q", code, out)
	}
	matches, _ := filepath.Glob(path + ".bak.*")
	if len(matches) != 0 {
		t.Errorf("unexpected backups=%v", matches)
	}
}

func TestDoctorStatusesAndExit(t *testing.T) {
	const validDoc = `<statusloom version="1" tool="claude-code" color-level="ansi16">` +
		`<layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`
	tests := []struct {
		name         string
		document     string // claude-code.xml source ("" = absent)
		settings     string
		wantDocument string
		wantClaude   string
		wantCode     int
	}{
		{"defaults and missing setup", "", "", "PASS document - using built-in defaults", "WARN claude-code", 0},
		{"valid configured", validDoc, `{"statusLine":{"command":"statusloom claude"}}`, "PASS document", "PASS claude-code", 0},
		{"invalid document", `<statusloom>`, "", "FAIL document", "WARN claude-code", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.json")
			settingsPath := filepath.Join(dir, "settings.json")
			cachePath := filepath.Join(dir, "cache")
			if tt.document != "" {
				os.WriteFile(filepath.Join(dir, "claude-code.xml"), []byte(tt.document), 0o600)
			}
			if tt.settings != "" {
				os.WriteFile(settingsPath, []byte(tt.settings), 0o600)
			}
			t.Setenv("STATUSLOOM_CONFIG", configPath)
			t.Setenv("STATUSLOOM_CACHE_DIR", cachePath)
			out, stderr, code := runCLI(t, []string{"doctor", "--settings", settingsPath}, nil, nil)
			if code != tt.wantCode || stderr != "" {
				t.Fatalf("code=%d stderr=%q out=%q", code, stderr, out)
			}
			for _, want := range []string{"PASS binary", tt.wantDocument, "PASS cache", tt.wantClaude} {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q: %q", want, out)
				}
			}
			if _, err := os.Stat(filepath.Join(cachePath, ".doctor-probe")); err == nil {
				t.Error("probe remains")
			}
		})
	}
}

func TestDoctorCacheFailureControlsExit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-directory")
	os.WriteFile(file, []byte("x"), 0o600)
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "missing.json"))
	t.Setenv("STATUSLOOM_CACHE_DIR", file)
	out, _, code := runCLI(t, []string{"doctor", "--settings", filepath.Join(dir, "settings.json")}, nil, nil)
	if code != 1 || !strings.Contains(out, "FAIL cache") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

// TestSetupClaudeCodeRefreshInterval covers --refresh-interval end to end:
// the flag is written into statusLine.refreshInterval, a second run with
// the same value is reported as "already configured" (no rewrite), and a
// different value goes through the existing diff/confirm/update flow.
func TestSetupClaudeCodeRefreshInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	args := []string{"setup", "claude-code", "--settings", path, "--refresh-interval", "60"}
	out, stderr, code := runCLI(t, args, nil, nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q out=%q", code, stderr, out)
	}
	statusLine := readStatusLine(t, path)
	if n, ok := statusLine["refreshInterval"].(float64); !ok || n != 60 {
		t.Fatalf("refreshInterval=%v (%q)", statusLine["refreshInterval"], out)
	}

	// Same value again: already configured, nothing rewritten, no backup.
	before, _ := os.ReadFile(path)
	out, _, code = runCLI(t, args, nil, nil)
	after, _ := os.ReadFile(path)
	if code != 0 || out != "already configured\n" || string(before) != string(after) {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if matches, _ := filepath.Glob(path + ".bak.*"); len(matches) != 0 {
		t.Errorf("unexpected backups=%v", matches)
	}

	// Different value: existing diff -> confirm -> update flow (via --yes).
	args = []string{"setup", "claude-code", "--settings", path, "--refresh-interval", "90", "--yes"}
	out, stderr, code = runCLI(t, args, nil, nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q out=%q", code, stderr, out)
	}
	if !strings.Contains(out, "-") || !strings.Contains(out, "+") {
		t.Errorf("expected a diff in output: %q", out)
	}
	statusLine = readStatusLine(t, path)
	if n, ok := statusLine["refreshInterval"].(float64); !ok || n != 90 {
		t.Fatalf("refreshInterval=%v", statusLine["refreshInterval"])
	}
}

// TestSetupClaudeCodeRefreshIntervalOmitted confirms that not passing the
// flag never writes a refreshInterval key, preserving prior behavior.
func TestSetupClaudeCodeRefreshIntervalOmitted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, _, code := runCLI(t, []string{"setup", "claude-code", "--settings", path}, nil, nil); code != 0 {
		t.Fatal(code)
	}
	statusLine := readStatusLine(t, path)
	if _, ok := statusLine["refreshInterval"]; ok {
		t.Errorf("refreshInterval should be absent: %v", statusLine)
	}
}

// TestSetupClaudeCodeRefreshIntervalInvalid confirms an explicit N < 1
// exits 2 with an error and never touches the settings file.
func TestSetupClaudeCodeRefreshIntervalInvalid(t *testing.T) {
	for _, n := range []string{"0", "-1"} {
		t.Run(n, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			_, stderr, code := runCLI(t, []string{"setup", "claude-code", "--settings", path, "--refresh-interval", n}, nil, nil)
			if code != 2 {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			if stderr == "" {
				t.Error("expected an error message on stderr")
			}
			if _, err := os.Stat(path); err == nil {
				t.Error("settings file should not have been created")
			}
		})
	}
}

// readStatusLine reads path as Claude Code settings JSON and returns the
// statusLine object, failing the test if either step is impossible.
func readStatusLine(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("invalid JSON %q: %v", b, err)
	}
	statusLine, ok := doc["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("no statusLine object: %q", b)
	}
	return statusLine
}

// TestDoctorDisableAllHooksWarns covers the doctor warning for
// disableAllHooks: true, which per Claude Code's documented behavior also
// disables the status line.
func TestDoctorDisableAllHooksWarns(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	os.WriteFile(settingsPath, []byte(`{"disableAllHooks":true,"statusLine":{"command":"statusloom claude"}}`), 0o600)
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "does-not-exist.json"))
	t.Setenv("STATUSLOOM_CACHE_DIR", filepath.Join(dir, "cache"))

	out, _, _ := runCLI(t, []string{"doctor", "--settings", settingsPath}, nil, nil)
	if !strings.Contains(out, "WARN claude-code - disableAllHooks") {
		t.Errorf("expected disableAllHooks warning: %q", out)
	}
	// Still reports the (otherwise valid) statusLine as configured.
	if !strings.Contains(out, "PASS claude-code") {
		t.Errorf("expected statusLine to still be reported PASS: %q", out)
	}
}

// TestDoctorRefreshInterval covers the "refresh" check: it should warn
// only when the active claude-code layout has a countdown widget
// (five-hour-reset / weekly-reset) and Claude Code's
// statusLine.refreshInterval is not usably set.
func TestDoctorRefreshInterval(t *testing.T) {
	const docWithReset = `<statusloom version="1" tool="claude-code"><layout name="d" active="true">` +
		`<line><field name="five-hour-reset"/></line></layout></statusloom>`
	const docWithoutReset = `<statusloom version="1" tool="claude-code"><layout name="d" active="true">` +
		`<line><field name="model"/></line></layout></statusloom>`

	tests := []struct {
		name     string
		document string
		settings string
		wantWarn bool
	}{
		{"reset widget, no refreshInterval", docWithReset, `{"statusLine":{"command":"statusloom claude"}}`, true},
		{"reset widget, refreshInterval set", docWithReset, `{"statusLine":{"command":"statusloom claude","refreshInterval":60}}`, false},
		{"reset widget, refreshInterval 0", docWithReset, `{"statusLine":{"command":"statusloom claude","refreshInterval":0}}`, true},
		{"no reset widget", docWithoutReset, `{"statusLine":{"command":"statusloom claude"}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			settingsPath := filepath.Join(dir, "settings.json")
			os.WriteFile(filepath.Join(dir, "claude-code.xml"), []byte(tt.document), 0o600)
			os.WriteFile(settingsPath, []byte(tt.settings), 0o600)
			t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
			t.Setenv("STATUSLOOM_CACHE_DIR", filepath.Join(dir, "cache"))

			out, _, _ := runCLI(t, []string{"doctor", "--settings", settingsPath}, nil, nil)
			gotWarn := strings.Contains(out, "WARN refresh")
			if gotWarn != tt.wantWarn {
				t.Errorf("WARN refresh present=%v want=%v: %q", gotWarn, tt.wantWarn, out)
			}
		})
	}
}

// TestDoctorRefreshIntervalSkipsWhenUnreadable covers the silent-skip
// cases: no <tool>.xml document on disk (built-in defaults / migration
// pending), and unreadable Claude settings. Both are handled by other doctor
// checks, so "refresh" must stay quiet to avoid double reporting.
func TestDoctorRefreshIntervalSkipsWhenUnreadable(t *testing.T) {
	t.Run("no document", func(t *testing.T) {
		dir := t.TempDir()
		settingsPath := filepath.Join(dir, "settings.json")
		os.WriteFile(settingsPath, []byte(`{"statusLine":{"command":"statusloom claude"}}`), 0o600)
		t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
		t.Setenv("STATUSLOOM_CACHE_DIR", filepath.Join(dir, "cache"))
		out, _, _ := runCLI(t, []string{"doctor", "--settings", settingsPath}, nil, nil)
		if strings.Contains(out, "WARN refresh") {
			t.Errorf("did not expect a refresh warning: %q", out)
		}
	})

	t.Run("claude settings unreadable", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "claude-code.xml"), []byte(
			`<statusloom version="1" tool="claude-code"><layout name="d" active="true"><line><field name="weekly-reset"/></line></layout></statusloom>`), 0o600)
		t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
		t.Setenv("STATUSLOOM_CACHE_DIR", filepath.Join(dir, "cache"))
		out, _, _ := runCLI(t, []string{"doctor", "--settings", filepath.Join(dir, "missing-settings.json")}, nil, nil)
		if strings.Contains(out, "WARN refresh") {
			t.Errorf("did not expect a refresh warning: %q", out)
		}
	})
}
