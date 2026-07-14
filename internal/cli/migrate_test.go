package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_Claude_AutoMigratesLegacyConfig verifies the render-path auto
// migration: when claude-code.xml is absent but a legacy config.json exists,
// `statusloom claude` migrates it to claude-code.xml (leaving config.json in
// place), reports it on stderr, and renders the migrated layout.
func TestRun_Claude_AutoMigratesLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	xmlPath := filepath.Join(dir, "claude-code.xml")
	t.Setenv("STATUSLOOM_CONFIG", configPath)
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	// A distinctive single-line layout so the output is unmistakably the
	// migrated config, not the built-in default.
	legacy := `{"schemaVersion":1,"shared":{},"tools":{"claude-code":{"colorLevel":"none","layouts":[{"name":"Default","lines":[[{"type":"model"},{"type":"separator","text":" | "},{"type":"tool-version"}]]}],"activeLayout":0}}}`
	if err := os.WriteFile(configPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, []string{"claude"}, fixture(t, "full.json"), nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "migrated config.json to") {
		t.Errorf("stderr missing migration notice: %q", stderr)
	}
	if _, err := os.Stat(xmlPath); err != nil {
		t.Fatalf("claude-code.xml was not written: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config.json should be left in place: %v", err)
	}

	got := lines(stdout)
	if len(got) != 1 {
		t.Fatalf("want single migrated line, got %d: %q", len(got), stdout)
	}
	// colorLevel none -> plain text, so we can compare directly.
	if want := "Opus 4.8 | v2.1.200"; got[0] != want {
		t.Errorf("migrated output = %q, want %q", got[0], want)
	}

	// A second render must not migrate again (xml now exists): no notice.
	_, stderr2, code2 := runCLI(t, []string{"claude"}, fixture(t, "full.json"), nil)
	if code2 != 0 || strings.Contains(stderr2, "migrated config.json") {
		t.Errorf("second render re-migrated or failed: code=%d stderr=%q", code2, stderr2)
	}
}

// TestRun_Claude_DefaultDocumentWhenNoConfig confirms that with neither
// claude-code.xml nor config.json present, the built-in default document
// renders (two lines) without any migration notice.
func TestRun_Claude_DefaultDocumentWhenNoConfig(t *testing.T) {
	setupEnv(t)
	stdout, stderr, code := runCLI(t, []string{"claude"}, fixture(t, "full.json"), nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if strings.Contains(stderr, "migrated") {
		t.Errorf("unexpected migration notice: %q", stderr)
	}
	if len(lines(stdout)) != 2 {
		t.Errorf("want 2 default lines, got %q", stdout)
	}
}
