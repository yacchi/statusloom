package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/store"
)

func TestDefaultDocument_ParsesAndValidates(t *testing.T) {
	for _, tool := range []string{"claude-code"} {
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

// TestDefaultDocument_ClaudeCodeHasSubagentRegion verifies the phase-2b
// integration of a <subagent> region (plans/subagent-region-dsl.md) into the
// single claude-code default document: the layout has no
// <responsive>/<variant> breakpoints, so the region is expected directly
// under <layout>, below its two <line> elements, containing exactly one
// <line> built from task-* fields only.
func TestDefaultDocument_ClaudeCodeHasSubagentRegion(t *testing.T) {
	src := DefaultDocument("claude-code")
	doc, diags := dsl.Parse(src)
	if doc == nil || doc.Root == nil {
		t.Fatalf("claude-code default document did not parse: %v", diags)
	}
	if diags := append(diags, dsl.Validate(doc)...); dsl.HasErrors(diags) {
		t.Fatalf("claude-code default document has validation errors: %v", diags)
	}

	if len(doc.Root.Layouts) != 1 {
		t.Fatalf("expected exactly one layout, got %d", len(doc.Root.Layouts))
	}
	layout := doc.Root.Layouts[0]
	for _, child := range layout.Children {
		if _, ok := child.(*dsl.ResponsiveNode); ok {
			t.Fatal("claude-code default layout unexpectedly uses <responsive>; test assumes layout-direct placement")
		}
	}
	if layout.Subagent == nil {
		t.Fatal("claude-code default layout has no <subagent> region")
	}
	if layout.Subagent.Line == nil {
		t.Fatal("<subagent> region has no <line>")
	}

	var fieldNames []string
	for _, child := range layout.Subagent.Line.Children {
		if f, ok := child.(*dsl.FieldNode); ok {
			fieldNames = append(fieldNames, f.Name)
		}
	}
	want := []string{"task-description", "task-model", "task-duration", "task-tokens", "task-context-percent"}
	if len(fieldNames) != len(want) {
		t.Fatalf("subagent line fields = %v, want %v", fieldNames, want)
	}
	for i, name := range want {
		if fieldNames[i] != name {
			t.Errorf("subagent line field[%d] = %q, want %q", i, fieldNames[i], name)
		}
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

	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open error = %v", err)
	}
	if _, diags, err := st.Save("claude-code", src, "test", store.Meta{}); err != nil || dsl.HasErrors(diags) {
		t.Fatalf("Save error = %v diags=%v", err, diags)
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

// layoutSource returns a minimal valid claude-code document whose layout is
// named name, so tests can tell which revision a LoadDocument call returned.
func layoutSource(name string) string {
	return `<statusloom version="1" tool="claude-code"><layout name="` + name + `" active="true"><line><field name="model"/></line></layout></statusloom>`
}

// unreadableSource uses an attribute this binary's parser rejects, standing
// in for syntax saved by a newer version.
func unreadableSource(name string) string {
	return `<statusloom version="1" tool="claude-code"><layout name="` + name + `" active="true"><line><field name="model" future-attr="on"/></line></layout></statusloom>`
}

// patchStoredRevision edits the store file in place, bypassing Save's
// validation, to simulate revisions a newer binary or a hand edit left behind.
func patchStoredRevision(t *testing.T, id string, patch map[string]any) {
	t.Helper()
	path, err := store.StorePath()
	if err != nil {
		t.Fatalf("StorePath: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	var sf map[string]any
	if err := json.Unmarshal(raw, &sf); err != nil {
		t.Fatalf("decode store: %v", err)
	}
	revs, ok := sf["revisions"].(map[string]any)
	if !ok {
		t.Fatal("store has no revisions map")
	}
	rev, ok := revs[id].(map[string]any)
	if !ok {
		t.Fatalf("revision %s not found", id)
	}
	for k, v := range patch {
		rev[k] = v
	}
	out, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		t.Fatalf("encode store: %v", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write store: %v", err)
	}
}

// isolateStore points the store at a fresh temp dir and saves each source in
// order, returning the revision ids (oldest first). Each save becomes the
// child of the previous one, so ids[i]'s parent is ids[i-1].
func isolateStore(t *testing.T, sources ...string) []string {
	t.Helper()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	ids := make([]string, 0, len(sources))
	for _, src := range sources {
		id, diags, err := st.Save("claude-code", src, "test", store.Meta{})
		if err != nil || dsl.HasErrors(diags) {
			t.Fatalf("Save error = %v diags=%v", err, diags)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestLoadDocumentUsesNewestReadableAncestor(t *testing.T) {
	ids := isolateStore(t, layoutSource("Parent"), layoutSource("Child"))
	patchStoredRevision(t, ids[1], map[string]any{"source": unreadableSource("Child")})

	doc, diags, err := LoadDocument("claude-code")
	if err != nil {
		t.Fatalf("LoadDocument error = %v", err)
	}
	if doc == nil || doc.Source != layoutSource("Parent") {
		t.Fatalf("LoadDocument did not return the readable parent")
	}
	if dsl.HasErrors(diags) {
		t.Fatalf("fallback to an ancestor must not report errors: %v", diags)
	}
	if len(diags) != 1 || diags[0].Severity != dsl.SeverityWarning {
		t.Fatalf("want exactly one warning diagnostic, got %v", diags)
	}
	if !strings.Contains(diags[0].Message, ids[0]) {
		t.Errorf("warning %q does not name the revision used (%s)", diags[0].Message, ids[0])
	}
}

func TestLoadDocumentFallsBackToDefaultWhenNoAncestorParses(t *testing.T) {
	ids := isolateStore(t, layoutSource("Root"), layoutSource("Child"))
	patchStoredRevision(t, ids[0], map[string]any{"source": unreadableSource("Root")})
	patchStoredRevision(t, ids[1], map[string]any{"source": unreadableSource("Child")})

	doc, diags, err := LoadDocument("claude-code")
	if err != nil {
		t.Fatalf("LoadDocument error = %v", err)
	}
	if doc == nil || doc.Source != DefaultDocument("claude-code") {
		t.Fatal("want the built-in default document when no revision parses")
	}
	if !dsl.HasErrors(diags) {
		t.Fatalf("the current revision's parse errors should still be reported: %v", diags)
	}
}

func TestLoadDocumentReadsNoAncestorWhenCurrentParses(t *testing.T) {
	ids := isolateStore(t, layoutSource("Parent"), layoutSource("Child"))
	// The parent is unreadable. A readable current revision must be returned
	// directly, without a warning, so the history is never consulted.
	patchStoredRevision(t, ids[0], map[string]any{"source": unreadableSource("Parent")})

	doc, diags, err := LoadDocument("claude-code")
	if err != nil {
		t.Fatalf("LoadDocument error = %v", err)
	}
	if doc == nil || doc.Source != layoutSource("Child") {
		t.Fatal("LoadDocument did not return the current revision")
	}
	if len(diags) != 0 {
		t.Fatalf("readable current revision must produce no diagnostics: %v", diags)
	}
}

func TestLoadDocumentTerminatesOnCycleAndMissingParent(t *testing.T) {
	t.Run("cycle", func(t *testing.T) {
		ids := isolateStore(t, layoutSource("A"), layoutSource("B"))
		// B -> A -> B, both unreadable: the walk must stop on the revisit.
		patchStoredRevision(t, ids[0], map[string]any{"source": unreadableSource("A"), "parent": ids[1]})
		patchStoredRevision(t, ids[1], map[string]any{"source": unreadableSource("B")})

		doc, _, err := LoadDocument("claude-code")
		if err != nil {
			t.Fatalf("LoadDocument error = %v", err)
		}
		if doc == nil || doc.Source != DefaultDocument("claude-code") {
			t.Fatal("cyclic chain must fall back to the default document")
		}
	})
	t.Run("missing parent", func(t *testing.T) {
		ids := isolateStore(t, layoutSource("A"), layoutSource("B"))
		patchStoredRevision(t, ids[1], map[string]any{"source": unreadableSource("B"), "parent": "00000000-missing"})

		doc, _, err := LoadDocument("claude-code")
		if err != nil {
			t.Fatalf("LoadDocument error = %v", err)
		}
		if doc == nil || doc.Source != DefaultDocument("claude-code") {
			t.Fatal("missing parent must fall back to the default document")
		}
	})
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
