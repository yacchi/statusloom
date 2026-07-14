package config

import (
	"os"
	"path/filepath"
	"testing"
)

// planExampleJSON mirrors the example in
// statusloom-local-development-plan.md section 10, minus a couple of
// widgets trimmed for brevity — the parts that matter for defaulting are
// kept intact. It uses the legacy top-level "lines" shape, which the
// migration path must still parse.
const planExampleJSON = `{
  "schemaVersion": 1,
  "shared": {
    "git": {
      "cacheTtlMs": 3000,
      "includeUntracked": true,
      "collectNumstat": true
    }
  },
  "tools": {
    "claude-code": {
      "compactThreshold": 60,
      "colorLevel": "ansi16",
      "lines": [
        [
          { "type": "model", "color": "cyan" },
          { "type": "separator", "text": " | " },
          { "type": "thinking-effort" },
          { "type": "separator", "text": " | " },
          { "type": "context-length" },
          { "type": "separator", "text": " | " },
          { "type": "context-percentage-usable" },
          { "type": "separator", "text": " | " },
          { "type": "session-cost" },
          { "type": "separator", "text": " | " },
          { "type": "git-branch", "color": "magenta" },
          { "type": "separator", "text": " | " },
          { "type": "git-changes", "color": "yellow" }
        ],
        [
          { "type": "five-hour-usage" },
          { "type": "separator", "text": " | " },
          { "type": "five-hour-reset" },
          { "type": "separator", "text": " | " },
          { "type": "weekly-usage" },
          { "type": "separator", "text": " | " },
          { "type": "weekly-reset" },
          { "type": "separator", "text": " | " },
          { "type": "tool-version" }
        ]
      ]
    }
  }
}`

// parseConfig is a small helper: parse decodes and defaults a legacy
// config.json document held in memory (the migration path's reader).
func parseConfig(t *testing.T, doc string) *Config {
	t.Helper()
	cfg, err := parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	return cfg
}

func TestParse_PlanExample(t *testing.T) {
	cfg := parseConfig(t, planExampleJSON)

	tool, ok := cfg.Tools["claude-code"]
	if !ok {
		t.Fatal("expected claude-code tool config")
	}
	if tool.CompactThreshold != 60 {
		t.Errorf("CompactThreshold = %d, want 60", tool.CompactThreshold)
	}
	if tool.ColorLevel != "ansi16" {
		t.Errorf("ColorLevel = %q, want ansi16", tool.ColorLevel)
	}
	// Not present in the document -> defaulted.
	if tool.Context.PercentageMode != "usable" {
		t.Errorf("Context.PercentageMode = %q, want usable (default)", tool.Context.PercentageMode)
	}
	// The document uses the legacy top-level "lines" shape; the lenient
	// loader wraps it in a single "Default" layout.
	if len(tool.Layouts) != 1 {
		t.Fatalf("len(Layouts) = %d, want 1 (legacy lines wrapped)", len(tool.Layouts))
	}
	if tool.Layouts[0].Name != "Default" {
		t.Errorf("Layouts[0].Name = %q, want %q", tool.Layouts[0].Name, "Default")
	}
	if tool.ActiveLayout != 0 {
		t.Errorf("ActiveLayout = %d, want 0", tool.ActiveLayout)
	}
	if len(tool.Layouts[0].Lines) != 2 {
		t.Fatalf("len(Layouts[0].Lines) = %d, want 2", len(tool.Layouts[0].Lines))
	}

	if cfg.Shared.Git.CacheTTLMs != 3000 {
		t.Errorf("Git.CacheTTLMs = %d, want 3000", cfg.Shared.Git.CacheTTLMs)
	}
	// Not present in the document -> defaulted.
	if cfg.Shared.Git.TimeoutMs != 200 {
		t.Errorf("Git.TimeoutMs = %d, want 200 (default)", cfg.Shared.Git.TimeoutMs)
	}
	if !cfg.Shared.Git.IncludeUntracked || !cfg.Shared.Git.CollectNumstat {
		t.Errorf("Git booleans = %+v, want both true", cfg.Shared.Git)
	}
}

// TestParse_LegacyLinesWrapped verifies that a pre-release document with a
// top-level "lines" array and no "layouts" is parsed leniently into a single
// "Default" layout, preserving widget content (important for migration).
func TestParse_LegacyLinesWrapped(t *testing.T) {
	doc := `{
	  "schemaVersion": 1,
	  "tools": {
	    "claude-code": {
	      "colorLevel": "ansi16",
	      "lines": [
	        [ { "type": "model", "color": "cyan" }, { "type": "tool-version" } ]
	      ]
	    }
	  }
	}`
	cfg := parseConfig(t, doc)
	tool := cfg.Tools["claude-code"]
	if len(tool.Layouts) != 1 {
		t.Fatalf("len(Layouts) = %d, want 1", len(tool.Layouts))
	}
	if tool.Layouts[0].Name != "Default" {
		t.Errorf("Layouts[0].Name = %q, want %q", tool.Layouts[0].Name, "Default")
	}
	lines := tool.Layouts[0].Lines
	if len(lines) != 1 || len(lines[0]) != 2 {
		t.Fatalf("wrapped lines = %#v, want one line of two widgets", lines)
	}
	if lines[0][0].Type != "model" || lines[0][0].Color != "cyan" || lines[0][1].Type != "tool-version" {
		t.Errorf("wrapped widgets = %#v, want [model(cyan) tool-version]", lines[0])
	}
}

// TestParse_LayoutsFormat verifies that a document already using the
// "layouts" shape is loaded as-is (with activeLayout honored).
func TestParse_LayoutsFormat(t *testing.T) {
	doc := `{
	  "schemaVersion": 1,
	  "tools": {
	    "claude-code": {
	      "activeLayout": 1,
	      "layouts": [
	        { "name": "A", "lines": [ [ { "type": "model" } ] ] },
	        { "name": "B", "lines": [ [ { "type": "tool-version" } ] ] }
	      ]
	    }
	  }
	}`
	cfg := parseConfig(t, doc)
	tool := cfg.Tools["claude-code"]
	if len(tool.Layouts) != 2 {
		t.Fatalf("len(Layouts) = %d, want 2", len(tool.Layouts))
	}
	if tool.Layouts[0].Name != "A" || tool.Layouts[1].Name != "B" {
		t.Errorf("layout names = %q/%q, want A/B", tool.Layouts[0].Name, tool.Layouts[1].Name)
	}
	if tool.ActiveLayout != 1 {
		t.Errorf("ActiveLayout = %d, want 1", tool.ActiveLayout)
	}
}

// TestParse_LayoutsWinOverLegacyLines verifies that when both "layouts" and a
// stray legacy "lines" are present, layouts win and lines is ignored.
func TestParse_LayoutsWinOverLegacyLines(t *testing.T) {
	doc := `{
	  "schemaVersion": 1,
	  "tools": {
	    "claude-code": {
	      "lines": [ [ { "type": "model" } ] ],
	      "layouts": [ { "name": "Kept", "lines": [ [ { "type": "tool-version" } ] ] } ]
	    }
	  }
	}`
	cfg := parseConfig(t, doc)
	tool := cfg.Tools["claude-code"]
	if len(tool.Layouts) != 1 || tool.Layouts[0].Name != "Kept" {
		t.Fatalf("Layouts = %#v, want single %q layout", tool.Layouts, "Kept")
	}
	if got := tool.Layouts[0].Lines[0][0].Type; got != "tool-version" {
		t.Errorf("layout widget = %q, want tool-version (legacy lines ignored)", got)
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	if _, err := parse([]byte("{ this is not valid json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParse_UnsupportedSchemaVersion(t *testing.T) {
	if _, err := parse([]byte(`{"schemaVersion":2,"shared":{},"tools":{}}`)); err == nil {
		t.Fatal("expected error for schemaVersion 2")
	}
}

func TestParse_GitBooleansDefaultTrueWhenSharedGitAbsent(t *testing.T) {
	cfg := parseConfig(t, `{"schemaVersion":1,"shared":{},"tools":{}}`)
	if !cfg.Shared.Git.IncludeUntracked || !cfg.Shared.Git.CollectNumstat {
		t.Errorf("expected git booleans to default to true when shared.git is absent, got %+v", cfg.Shared.Git)
	}
}

func TestParse_GitBooleansRespectExplicitFalse(t *testing.T) {
	cfg := parseConfig(t, `{"schemaVersion":1,"shared":{"git":{"includeUntracked":false,"collectNumstat":false}},"tools":{}}`)
	if cfg.Shared.Git.IncludeUntracked || cfg.Shared.Git.CollectNumstat {
		t.Errorf("expected explicit false to be respected, got %+v", cfg.Shared.Git)
	}
}

// TestLoadLegacyConfig_MissingReturnsNotOK verifies the migration reader
// treats an absent config.json as "nothing to migrate" (ok=false, no error).
func TestLoadLegacyConfig_MissingReturnsNotOK(t *testing.T) {
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	cfg, ok, err := LoadLegacyConfig()
	if err != nil {
		t.Fatalf("LoadLegacyConfig() error = %v", err)
	}
	if ok || cfg != nil {
		t.Errorf("LoadLegacyConfig() = (%v, %v), want (nil, false) for a missing file", cfg, ok)
	}
}

// TestLoadLegacyConfig_ReadsAndDefaults verifies the migration reader loads a
// present config.json and applies defaults (the input the migration converts).
func TestLoadLegacyConfig_ReadsAndDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(planExampleJSON), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
	t.Setenv("STATUSLOOM_CONFIG", path)

	cfg, ok, err := LoadLegacyConfig()
	if err != nil || !ok {
		t.Fatalf("LoadLegacyConfig() = (ok=%v, err=%v), want a loaded config", ok, err)
	}
	tool := cfg.Tools["claude-code"]
	if tool.Context.PercentageMode != "usable" {
		t.Errorf("PercentageMode = %q, want usable (defaulted)", tool.Context.PercentageMode)
	}
	if cfg.Shared.Git.TimeoutMs != 200 {
		t.Errorf("Git.TimeoutMs = %d, want 200 (defaulted)", cfg.Shared.Git.TimeoutMs)
	}
}
