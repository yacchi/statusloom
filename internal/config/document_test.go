package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/dsl"
)

func TestDocumentPath_UsesConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	if got, want := DocumentPath("claude-code"), filepath.Join(dir, "claude-code.xml"); got != want {
		t.Errorf("DocumentPath = %q, want %q", got, want)
	}
}

func TestDocumentPath_ConfigDirIsADirectory(t *testing.T) {
	dir := t.TempDir()
	// STATUSLOOM_CONFIG naming an existing directory is honored verbatim, so a
	// harness can isolate every file inside a single mktemp -d directory.
	t.Setenv("STATUSLOOM_CONFIG", dir)
	if got, want := DocumentPath("claude-code"), filepath.Join(dir, "claude-code.xml"); got != want {
		t.Errorf("DocumentPath = %q, want %q", got, want)
	}
}

func TestDefaultDocument_ParsesAndValidates(t *testing.T) {
	src := DefaultDocument("claude-code")
	if src == "" {
		t.Fatal("DefaultDocument(claude-code) is empty")
	}
	doc, diags := dsl.Parse(src)
	if doc == nil || doc.Root == nil {
		t.Fatalf("default document did not parse: %v", diags)
	}
	if diags := append(diags, dsl.Validate(doc)...); dsl.HasErrors(diags) {
		t.Fatalf("default document has validation errors: %v", diags)
	}
	if DefaultDocument("unknown-tool") != "" {
		t.Error("DefaultDocument(unknown-tool) should be empty")
	}
}

func TestLoadDocument_FallsBackToDefaultWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	if DocumentExists("claude-code") {
		t.Fatal("document should be absent")
	}
	doc, diags, err := LoadDocument("claude-code")
	if err != nil {
		t.Fatalf("LoadDocument error = %v", err)
	}
	if doc == nil || doc.Root == nil {
		t.Fatalf("LoadDocument returned no document; diags=%v", diags)
	}
	if dsl.HasErrors(diags) {
		t.Fatalf("default fallback has errors: %v", diags)
	}
	if doc.Source != DefaultDocument("claude-code") {
		t.Error("fallback document source is not DefaultDocument")
	}
}

func TestSaveAndLoadDocument_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	src := DefaultDocument("claude-code")
	if err := SaveDocumentSource("claude-code", src); err != nil {
		t.Fatalf("SaveDocumentSource error = %v", err)
	}
	if !DocumentExists("claude-code") {
		t.Fatal("document should exist after save")
	}
	doc, diags, err := LoadDocument("claude-code")
	if err != nil || dsl.HasErrors(diags) {
		t.Fatalf("LoadDocument err=%v diags=%v", err, diags)
	}
	if doc.Source != src {
		t.Errorf("round-trip source mismatch:\n got %q\nwant %q", doc.Source, src)
	}
}

func TestMigrateFromLegacy_Default(t *testing.T) {
	doc, warnings := MigrateFromLegacy(*Default(), "claude-code")
	if doc == nil || doc.Root == nil {
		t.Fatalf("migration returned no document; warnings=%v", warnings)
	}
	for _, w := range warnings {
		if strings.HasPrefix(w, "error:") {
			t.Errorf("migration produced an error diagnostic: %s", w)
		}
	}
	root := doc.Root
	if root.Version != "1" || root.Tool != "claude-code" {
		t.Errorf("root version=%q tool=%q", root.Version, root.Tool)
	}
	// Tool-level settings migrated onto the root.
	if root.Settings.ColorLevel != "ansi16" {
		t.Errorf("color-level=%q want ansi16", root.Settings.ColorLevel)
	}
	if root.Settings.CompactThreshold == nil || *root.Settings.CompactThreshold != 60 {
		t.Errorf("compact-threshold=%v want 60", root.Settings.CompactThreshold)
	}
	if root.Settings.ContextPercentageMode != "usable" {
		t.Errorf("context-percentage-mode=%q want usable", root.Settings.ContextPercentageMode)
	}
	// Shared git migrated to a <git> element.
	if root.Git == nil {
		t.Fatal("git element not migrated")
	}
	if root.Git.CacheTTLMS == nil || *root.Git.CacheTTLMS != 3000 {
		t.Errorf("git cache-ttl-ms=%v want 3000", root.Git.CacheTTLMS)
	}
	// Exactly one active layout.
	active := 0
	for _, l := range root.Layouts {
		if l.Active != nil && *l.Active {
			active++
		}
	}
	if active != 1 {
		t.Errorf("active layouts=%d want 1", active)
	}
}

func TestMigrateFromLegacy_WidgetRules(t *testing.T) {
	cfg := Config{
		SchemaVersion: 1,
		Shared:        SharedConfig{Git: GitConfig{CacheTTLMs: 3000, TimeoutMs: 200, IncludeUntracked: true, CollectNumstat: true}},
		Tools: map[string]ToolConfig{
			"claude-code": {
				ColorLevel:   "ansi16",
				ActiveLayout: 0,
				Layouts: []Layout{{
					Name: "Default",
					Lines: [][]WidgetSpec{{
						// color-name conversion + bold
						{Type: "git-branch", Color: "brightBlack", Bold: true},
						// explicit separator text preserved verbatim
						{Type: "separator", Text: " :: "},
						// template split into prefix/suffix
						{Type: "five-hour-usage", Template: "5h: {value}!"},
						// showWhen -> when, op conversion, self source
						{Type: "context-percentage", ShowWhen: &Condition{Op: "gte", Value: 80}},
						// colorRules -> <color-rule> children, op + color conversion
						{Type: "context-percentage-usable", Color: "green", ColorRules: []ColorRule{
							{Op: "gte", Value: 90, Color: "brightRed"},
							{Op: "lte", Value: 10, Color: "cyan"},
						}},
						// raw value
						{Type: "context-length", RawValue: true},
						// metadata dropped -> warning
						{Type: "model", Metadata: map[string]string{"foo": "bar"}},
						// flex-separator
						{Type: "flex-separator", Flex: "full-minus-20"},
						// template without {value} -> warning + dropped
						{Type: "session-cost", Template: "cost"},
					}},
				}},
			},
		},
	}

	doc, warnings := MigrateFromLegacy(cfg, "claude-code")
	for _, w := range warnings {
		if strings.HasPrefix(w, "error:") {
			t.Fatalf("unexpected error diagnostic: %s", w)
		}
	}
	// Warnings for dropped metadata and template-without-{value}.
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "metadata dropped") {
		t.Errorf("missing metadata warning in %v", warnings)
	}
	if !strings.Contains(joined, "no {value}") {
		t.Errorf("missing template warning in %v", warnings)
	}

	children := doc.Root.Layouts[0].Lines[0].Children
	byField := map[string]*dsl.FieldNode{}
	var sep *dsl.TextNode
	var flex *dsl.FlexNode
	for _, ch := range children {
		switch n := ch.(type) {
		case *dsl.FieldNode:
			byField[n.Name] = n
		case *dsl.TextNode:
			sep = n
		case *dsl.FlexNode:
			flex = n
		}
	}

	if b := byField["git-branch"]; b == nil || b.Common.Style.Color != "bright-black" || b.Common.Style.Bold == nil || !*b.Common.Style.Bold {
		t.Errorf("git-branch color/bold not migrated: %+v", byField["git-branch"])
	}
	if sep == nil || sep.Role != "separator" || sep.Value != " :: " {
		t.Errorf("separator not migrated verbatim: %+v", sep)
	}
	if f := byField["five-hour-usage"]; f == nil || f.Common.Prefix != "5h: " || f.Common.Suffix != "!" {
		t.Errorf("template split wrong: %+v", byField["five-hour-usage"])
	} else if f.Common.Optional != "five-hour-usage" {
		// Template-derived prefix/suffix are gated on the field's own
		// presence so an empty value hides the label, matching the legacy
		// template's "no output when hidden" semantics.
		t.Errorf("template-derived field optional=%q want %q", f.Common.Optional, "five-hour-usage")
	}
	if f := byField["context-percentage"]; f == nil || f.Common.When != "self ge 80" {
		t.Errorf("showWhen -> when wrong: %+v", byField["context-percentage"])
	} else if f.Common.Optional != "" {
		// optional is only added for template-derived prefix/suffix; a bare
		// showWhen widget must not gain one.
		t.Errorf("non-template field gained optional=%q", f.Common.Optional)
	}
	if f := byField["context-percentage-usable"]; f == nil || len(f.Common.ColorRules) != 2 {
		t.Fatalf("color-rules not migrated: %+v", byField["context-percentage-usable"])
	} else {
		if f.Common.ColorRules[0].When != "self ge 90" || f.Common.ColorRules[0].Color != "bright-red" {
			t.Errorf("color-rule[0]=%+v", f.Common.ColorRules[0])
		}
		if f.Common.ColorRules[1].When != "self le 10" || f.Common.ColorRules[1].Color != "cyan" {
			t.Errorf("color-rule[1]=%+v", f.Common.ColorRules[1])
		}
	}
	if f := byField["context-length"]; f == nil || !f.Raw {
		t.Errorf("raw not migrated: %+v", byField["context-length"])
	}
	if f := byField["session-cost"]; f == nil || f.Common.Prefix != "" || f.Common.Suffix != "" || f.Common.Optional != "" {
		t.Errorf("template-without-{value} should be dropped entirely (no prefix/suffix/optional): %+v", byField["session-cost"])
	}
	if flex == nil || flex.Size != "full-minus-20" {
		t.Errorf("flex not migrated: %+v", flex)
	}
}

func TestLoadLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))

	if _, ok, err := LoadLegacyConfig(); ok || err != nil {
		t.Fatalf("expected absent legacy config; ok=%v err=%v", ok, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"schemaVersion":1,"shared":{},"tools":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err := LoadLegacyConfig()
	if !ok || err != nil || cfg == nil {
		t.Fatalf("expected present legacy config; ok=%v err=%v cfg=%v", ok, err, cfg)
	}
}

func TestDocumentGitConfig(t *testing.T) {
	// Missing <git> -> defaults.
	doc, _ := dsl.Parse(`<statusloom version="1" tool="claude-code"><layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`)
	gc := DocumentGitConfig(doc)
	if gc.CacheTTLMs != 3000 || gc.TimeoutMs != 200 || !gc.IncludeUntracked || !gc.CollectNumstat {
		t.Errorf("defaults wrong: %+v", gc)
	}

	// Explicit <git> attributes are honored, including an explicit false.
	doc2, _ := dsl.Parse(`<statusloom version="1" tool="claude-code"><git cache-ttl-ms="1000" timeout-ms="50" include-untracked="false" collect-numstat="false"/><layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`)
	gc2 := DocumentGitConfig(doc2)
	if gc2.CacheTTLMs != 1000 || gc2.TimeoutMs != 50 || gc2.IncludeUntracked || gc2.CollectNumstat {
		t.Errorf("explicit git not honored: %+v", gc2)
	}
}
