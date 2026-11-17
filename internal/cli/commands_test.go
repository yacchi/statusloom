package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writeRawStoreDocument writes a minimal statusloom.json directly into dir
// (the resolved config directory), bypassing the store's Save validation
// boundary entirely. Documents in the store normally can never be invalid —
// Save rejects error-severity diagnostics before writing — so this simulates
// the one way an invalid document can still end up as a tool's current
// revision: a hand-edited or otherwise corrupted store file. Ordinary tests
// that need a *valid* current document should use writeDocument (cli_test.go)
// instead, which goes through the real Save validation boundary.
func writeRawStoreDocument(t *testing.T, dir, tool, src string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sf := map[string]any{
		"formatVersion": 1,
		"revisions": map[string]any{
			"01900000-0000-7000-8000-000000000000": map[string]any{
				"hash":    "raw-test",
				"tool":    tool,
				"source":  src,
				"savedAt": time.Now().UTC().Format(time.RFC3339),
				"parent":  nil,
				"origin":  "test",
				"meta":    map[string]any{},
			},
		},
		"refs": map[string]any{
			tool: map[string]any{"current": "01900000-0000-7000-8000-000000000000"},
		},
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "statusloom.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

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
				assertModEntries(t, doc)
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
			} else {
				if tt.initial != "" && string(got) != tt.initial {
					t.Errorf("file changed: %q", got)
				}
				if matches, _ := filepath.Glob(path + ".bak.*"); len(matches) != 0 {
					t.Errorf("unexpected backups=%v", matches)
				}
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
		document     string // claude-code current-document source ("" = absent)
		rawDocument  bool   // true: install document directly into the store, bypassing Save's validation boundary (simulates a corrupted store)
		settings     string
		wantDocument string
		wantClaude   string
		wantCode     int
	}{
		{"defaults and missing setup", "", false, "", "PASS document - using built-in defaults", "WARN claude-code", 0},
		{"valid configured", validDoc, false, `{"statusLine":{"command":"statusloom claude"}}`, "PASS document", "PASS claude-code", 0},
		{"invalid document", `<statusloom>`, true, "", "FAIL document", "WARN claude-code", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.json")
			settingsPath := filepath.Join(dir, "settings.json")
			cachePath := filepath.Join(dir, "cache")
			t.Setenv("STATUSLOOM_CONFIG", configPath)
			t.Setenv("STATUSLOOM_CACHE_DIR", cachePath)
			if tt.document != "" {
				if tt.rawDocument {
					writeRawStoreDocument(t, dir, "claude-code", tt.document)
				} else {
					writeDocument(t, "claude-code", tt.document)
				}
			}
			if tt.settings != "" {
				os.WriteFile(settingsPath, []byte(tt.settings), 0o600)
			}
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

// TestSetupClaudeCodeRefreshIntervalDefault confirms a fresh setup without
// the flag writes refreshInterval 30 to statusLine and writes no
// subagentStatusLine at all.
func TestSetupClaudeCodeRefreshIntervalDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, _, code := runCLI(t, []string{"setup", "claude-code", "--settings", path}, nil, nil); code != 0 {
		t.Fatal(code)
	}
	if n, ok := readStatusLine(t, path)["refreshInterval"].(float64); !ok || n != 30 {
		t.Fatalf("statusLine.refreshInterval = %v", n)
	}
	if _, ok := readSettingsDoc(t, path)["subagentStatusLine"]; ok {
		t.Error("setup must not write subagentStatusLine")
	}
}

// TestSetupClaudeCodeRefreshIntervalKeepsExisting confirms an existing
// refreshInterval >= 1 is kept when the flag is omitted: the settings
// already match, so the run reports "already configured" with no backup.
func TestSetupClaudeCodeRefreshIntervalKeepsExisting(t *testing.T) {
	initial := `{"statusLine":{"type":"command","command":"statusloom claude","refreshInterval":60},` +
		`"extraKnownMarketplaces":{"statusloom":{"source":{"source":"github","repo":"yacchi/statusloom"}}},` +
		`"enabledPlugins":{"statusloom@statusloom":true}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, code := runCLI(t, []string{"setup", "claude-code", "--settings", path}, nil, nil)
	got, _ := os.ReadFile(path)
	if code != 0 || out != "already configured\n" || string(got) != initial {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if matches, _ := filepath.Glob(path + ".bak.*"); len(matches) != 0 {
		t.Errorf("unexpected backups=%v", matches)
	}
}

// TestSetupClaudeCodeRefreshIntervalZeroUnsets confirms --refresh-interval 0
// removes an existing refreshInterval from both objects.
func TestSetupClaudeCodeRefreshIntervalZeroUnsets(t *testing.T) {
	initial := `{"statusLine":{"type":"command","command":"statusloom claude","refreshInterval":60}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runCLI(t, []string{"setup", "claude-code", "--settings", path, "--refresh-interval", "0", "--yes"}, nil, nil); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if _, ok := readStatusLine(t, path)["refreshInterval"]; ok {
		t.Error("statusLine.refreshInterval should be removed")
	}
}

// TestSetupClaudeCodeRefreshIntervalNegative confirms a negative N exits 2
// with an error and never touches the settings file.
func TestSetupClaudeCodeRefreshIntervalNegative(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	_, stderr, code := runCLI(t, []string{"setup", "claude-code", "--settings", path, "--refresh-interval", "-1"}, nil, nil)
	if code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "--refresh-interval must be >= 0") {
		t.Errorf("stderr=%q", stderr)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("settings file should not have been created")
	}
}

// readStatusLine reads path as Claude Code settings JSON and returns the
// statusLine object, failing the test if either step is impossible.
func readStatusLine(t *testing.T, path string) map[string]any {
	t.Helper()
	return readSettingsKey(t, path, "statusLine")
}

// readSettingsDoc reads path as Claude Code settings JSON and returns the
// whole top-level object, failing the test if either step is impossible.
func readSettingsDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("invalid JSON %q: %v", b, err)
	}
	return doc
}

// readSettingsKey reads path as Claude Code settings JSON and returns the
// named top-level object, failing the test if either step is impossible.
func readSettingsKey(t *testing.T, path, key string) map[string]any {
	t.Helper()
	obj, ok := readSettingsDoc(t, path)[key].(map[string]any)
	if !ok {
		t.Fatalf("no %s object in %s", key, path)
	}
	return obj
}

// assertModEntries fails the test unless doc carries the statusloom
// marketplace entry and enables the statusloom mod.
func assertModEntries(t *testing.T, doc map[string]any) {
	t.Helper()
	markets, _ := doc["extraKnownMarketplaces"].(map[string]any)
	if !reflect.DeepEqual(markets["statusloom"], map[string]any{"source": map[string]any{"source": "github", "repo": "yacchi/statusloom"}}) {
		t.Errorf("marketplace entry = %v", markets["statusloom"])
	}
	enabled, _ := doc["enabledPlugins"].(map[string]any)
	if enabled["statusloom@statusloom"] != true {
		t.Errorf("enabledPlugins = %v", enabled)
	}
}

// TestSetupClaudeCodeKeepsOtherModEntries confirms the marketplace and plugin
// merge keeps other extraKnownMarketplaces and enabledPlugins entries, and
// asks nothing since no existing value is replaced. The install notice is
// printed.
func TestSetupClaudeCodeKeepsOtherModEntries(t *testing.T) {
	initial := `{"enabledPlugins":{"a@b":true,"c@d":false},"extraKnownMarketplaces":{"o":{"source":{"source":"github","repo":"a/b"}}}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runCLI(t, []string{"setup", "claude-code", "--settings", path}, nil, nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q out=%q", code, stderr, out)
	}
	doc := readSettingsDoc(t, path)
	enabled, _ := doc["enabledPlugins"].(map[string]any)
	if enabled["a@b"] != true || enabled["c@d"] != false {
		t.Errorf("enabledPlugins = %v", enabled)
	}
	markets, _ := doc["extraKnownMarketplaces"].(map[string]any)
	if !reflect.DeepEqual(markets["o"], map[string]any{"source": map[string]any{"source": "github", "repo": "a/b"}}) {
		t.Errorf("marketplace o = %v", markets["o"])
	}
	assertModEntries(t, doc)
	if !strings.Contains(out, "claude plugin install") {
		t.Errorf("install notice missing: %q", out)
	}
}

// TestSetupClaudeCodeKeepsThirdPartySubagentLine confirms a subagentStatusLine
// that statusloom did not write is left byte-for-byte equal in value, and the
// run says so. A rerun prints the note and then "already configured".
func TestSetupClaudeCodeKeepsThirdPartySubagentLine(t *testing.T) {
	initial := `{"subagentStatusLine":{"command":"my-rows --x"}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"setup", "claude-code", "--settings", path}
	out, stderr, code := runCLI(t, args, nil, nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q out=%q", code, stderr, out)
	}
	if !strings.Contains(out, "Note: kept subagentStatusLine") {
		t.Errorf("note missing: %q", out)
	}
	want := map[string]any{"command": "my-rows --x"}
	if got := readSettingsDoc(t, path)["subagentStatusLine"]; !reflect.DeepEqual(got, want) {
		t.Errorf("subagentStatusLine = %v, want %v", got, want)
	}

	before, _ := os.ReadFile(path)
	out, _, code = runCLI(t, args, nil, nil)
	after, _ := os.ReadFile(path)
	if code != 0 || !strings.Contains(out, "Note: kept subagentStatusLine") || !strings.HasSuffix(out, "already configured\n") || string(before) != string(after) {
		t.Fatalf("rerun code=%d out=%q", code, out)
	}
}

// TestSetupClaudeCodeRemovesStatusloomSubagentLine confirms a subagentStatusLine
// statusloom wrote is removed without a prompt, and the removal is shown as a
// "- " line only.
func TestSetupClaudeCodeRemovesStatusloomSubagentLine(t *testing.T) {
	initial := `{"subagentStatusLine":{"type":"command","command":"statusloom claude-subagent","refreshInterval":30}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runCLI(t, []string{"setup", "claude-code", "--settings", path}, nil, nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q out=%q", code, stderr, out)
	}
	if !strings.Contains(out, "- subagentStatusLine: ") || strings.Contains(out, "+ subagentStatusLine") {
		t.Errorf("out=%q", out)
	}
	if _, ok := readSettingsDoc(t, path)["subagentStatusLine"]; ok {
		t.Error("statusloom subagentStatusLine should be removed")
	}
}

// TestSetupClaudeCodeDryRunKeepsSubagentLine confirms --dry-run reports the
// update of a statusloom subagentStatusLine without touching the file or
// writing a backup.
func TestSetupClaudeCodeDryRunKeepsSubagentLine(t *testing.T) {
	initial := `{"subagentStatusLine":{"type":"command","command":"statusloom claude-subagent"}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, code := runCLI(t, []string{"setup", "claude-code", "--settings", path, "--dry-run"}, nil, nil)
	got, _ := os.ReadFile(path)
	if code != 0 || !strings.Contains(out, "Would update "+path+".") || string(got) != initial {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if matches, _ := filepath.Glob(path + ".bak.*"); len(matches) != 0 {
		t.Errorf("unexpected backups=%v", matches)
	}
}

// TestSetupClaudeCodeFlipsDisabledModOnlyWithConfirm confirms an explicit
// enabledPlugins false is replaced only after confirmation: an abort leaves the
// file untouched, and --yes flips it.
func TestSetupClaudeCodeFlipsDisabledModOnlyWithConfirm(t *testing.T) {
	initial := `{"enabledPlugins":{"statusloom@statusloom":false}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runCLI(t, []string{"setup", "claude-code", "--settings", path}, []byte("n\n"), nil)
	got, _ := os.ReadFile(path)
	if code != 1 || !strings.Contains(stderr, "aborted") || string(got) != initial {
		t.Fatalf("abort code=%d stderr=%q out=%q", code, stderr, out)
	}

	out, stderr, code = runCLI(t, []string{"setup", "claude-code", "--settings", path, "--yes"}, nil, nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q out=%q", code, stderr, out)
	}
	if !strings.Contains(out, "- enabledPlugins.statusloom@statusloom: false") {
		t.Errorf("out=%q", out)
	}
	assertModEntries(t, readSettingsDoc(t, path))
}

// TestSetupClaudeCodeReplacesOtherMarketplaceOnlyWithConfirm confirms an
// existing statusloom marketplace entry that points elsewhere is replaced only
// after confirmation, and an abort leaves the file untouched.
func TestSetupClaudeCodeReplacesOtherMarketplaceOnlyWithConfirm(t *testing.T) {
	initial := `{"extraKnownMarketplaces":{"statusloom":{"source":{"source":"directory","path":"/src"}}}}`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, code := runCLI(t, []string{"setup", "claude-code", "--settings", path}, []byte("n\n"), nil)
	got, _ := os.ReadFile(path)
	if code != 1 || string(got) != initial {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

// TestSetupClaudeCodeNonObjectModKeys confirms a non-object enabledPlugins or
// extraKnownMarketplaces is replaced by an object holding only statusloom's
// entry, without a panic.
func TestSetupClaudeCodeNonObjectModKeys(t *testing.T) {
	initial := `{"enabledPlugins":[],"extraKnownMarketplaces":"x"}`
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runCLI(t, []string{"setup", "claude-code", "--settings", path, "--yes"}, nil, nil)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q out=%q", code, stderr, out)
	}
	doc := readSettingsDoc(t, path)
	assertModEntries(t, doc)
	if enabled, _ := doc["enabledPlugins"].(map[string]any); len(enabled) != 1 {
		t.Errorf("enabledPlugins = %v", enabled)
	}
	if markets, _ := doc["extraKnownMarketplaces"].(map[string]any); len(markets) != 1 {
		t.Errorf("extraKnownMarketplaces = %v", markets)
	}
}

// TestIsStatusloomSubagentStatusLine covers which subagentStatusLine values
// statusloom counts as its own. Anything else is third-party.
func TestIsStatusloomSubagentStatusLine(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want bool
	}{
		{"nil", nil, false},
		{"string value", "statusloom claude-subagent", false},
		{"missing command", map[string]any{"type": "command"}, false},
		{"non-string command", map[string]any{"command": 1}, false},
		{"bare", map[string]any{"command": "statusloom claude-subagent"}, true},
		{"unix path", map[string]any{"command": "/a/statusloom claude-subagent"}, true},
		{"windows path", map[string]any{"command": `C:\a\statusloom.exe claude-subagent`}, true},
		{"other subcommand", map[string]any{"command": "statusloom claude"}, false},
		{"other binary", map[string]any{"command": "statusloomx claude-subagent"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isStatusloomSubagentStatusLine(tt.v); got != tt.want {
				t.Errorf("isStatusloomSubagentStatusLine(%v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
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

// TestDoctorClaudeCodeWarnsOnKnownHook covers the doctor warning for a
// statusLine that has been overwritten by a known third-party tool's own
// hook (e.g. Orca's) instead of statusloom: doctor should name the tool,
// point at `statusloom setup claude-code`, and reassure that forwarding
// keeps working - not just report the generic "not configured" WARN.
func TestDoctorClaudeCodeWarnsOnKnownHook(t *testing.T) {
	const orcaCommand = `case "$(uname -s)" in Darwin*|Linux*) exec "${HOME-}/.orca/agent-hooks/claude-statusline.sh";; *) exec "${HOME-}/.orca/agent-hooks/claude-statusline.cmd";; esac`

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	settings, _ := json.Marshal(map[string]any{
		"statusLine": map[string]any{"type": "command", "command": orcaCommand},
	})
	os.WriteFile(settingsPath, settings, 0o600)
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "does-not-exist.json"))
	t.Setenv("STATUSLOOM_CACHE_DIR", filepath.Join(dir, "cache"))

	out, _, _ := runCLI(t, []string{"doctor", "--settings", settingsPath}, nil, nil)
	const want = "WARN claude-code - statusLine is Orca's own hook, not statusloom; run statusloom setup claude-code (statusloom will keep forwarding to Orca automatically)"
	if !strings.Contains(out, want) {
		t.Errorf("expected known-hook warning:\nwant substring: %q\ngot: %q", want, out)
	}
	if strings.Contains(out, "PASS claude-code") {
		t.Errorf("should not report PASS for a non-statusloom statusLine: %q", out)
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
			t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
			t.Setenv("STATUSLOOM_CACHE_DIR", filepath.Join(dir, "cache"))
			writeDocument(t, "claude-code", tt.document)
			os.WriteFile(settingsPath, []byte(tt.settings), 0o600)

			out, _, _ := runCLI(t, []string{"doctor", "--settings", settingsPath}, nil, nil)
			gotWarn := strings.Contains(out, "WARN refresh")
			if gotWarn != tt.wantWarn {
				t.Errorf("WARN refresh present=%v want=%v: %q", gotWarn, tt.wantWarn, out)
			}
		})
	}
}

// TestDoctorRefreshIntervalSkipsWhenUnreadable covers the silent-skip
// cases: no committed document in the store yet (built-in defaults), and
// unreadable Claude settings. Both are handled by other doctor checks, so
// "refresh" must stay quiet to avoid double reporting.
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
		t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
		t.Setenv("STATUSLOOM_CACHE_DIR", filepath.Join(dir, "cache"))
		writeDocument(t, "claude-code",
			`<statusloom version="1" tool="claude-code"><layout name="d" active="true"><line><field name="weekly-reset"/></line></layout></statusloom>`)
		out, _, _ := runCLI(t, []string{"doctor", "--settings", filepath.Join(dir, "missing-settings.json")}, nil, nil)
		if strings.Contains(out, "WARN refresh") {
			t.Errorf("did not expect a refresh warning: %q", out)
		}
	})
}
