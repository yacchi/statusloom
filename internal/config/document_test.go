package config

import (
	"path/filepath"
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
	for _, tool := range []string{"claude-code", "claude-code-subagent"} {
		t.Run(tool, func(t *testing.T) {
			src := DefaultDocument(tool)
			if src == "" {
				t.Fatalf("DefaultDocument(%s) is empty", tool)
			}
			doc, diags := dsl.Parse(src)
			if doc == nil || doc.Root == nil {
				t.Fatalf("default document did not parse: %v", diags)
			}
			if diags := append(diags, dsl.Validate(doc)...); dsl.HasErrors(diags) {
				t.Fatalf("default document has validation errors: %v", diags)
			}
		})
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
