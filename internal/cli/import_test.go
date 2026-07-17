package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/exchange"
	"github.com/yacchi/statusloom/internal/store"
)

// setupExchangeEnv isolates STATUSLOOM_CONFIG (so the internal store
// resolves into a temp dir) and STATUSLOOM_CACHE_DIR, mirroring
// setupDraftEnv/setupEnv in the other CLI test files.
func setupExchangeEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
}

// exchangeDoc returns a valid claude-code document whose layout name embeds
// name, so successive Saves produce distinct revisions (identical source
// would dedup to a no-op) — mirroring historyDoc.
func exchangeDoc(name string) string {
	return `<statusloom version="1" tool="claude-code"><layout name="` + name + `" active="true"><line><field name="model"/></line></layout></statusloom>`
}

func TestExport_NoCurrent_Fails(t *testing.T) {
	setupExchangeEnv(t)
	_, stderr, code := runCLI(t, []string{"export"}, nil, nil)
	if code == 0 {
		t.Fatal("export with no saved configuration should fail")
	}
	if !strings.Contains(stderr, "no saved configuration") {
		t.Errorf("stderr = %q, want a no-saved-configuration message", stderr)
	}
}

func TestExport_Stdout_CarriesMetaAndSource(t *testing.T) {
	setupExchangeEnv(t)
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	src := exchangeDoc("a")
	if _, _, err := st.Save("claude-code", src, "test", store.Meta{
		Name: "My Config", Description: "desc", Author: "me",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	stdout, stderr, code := runCLI(t, []string{"export"}, nil, nil)
	if code != 0 {
		t.Fatalf("export exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	meta, xml, err := exchange.Decode([]byte(stdout))
	if err != nil {
		t.Fatalf("exported output did not decode as a sloom.md document: %v\n%s", err, stdout)
	}
	if meta.Name != "My Config" || meta.Description != "desc" || meta.Author != "me" {
		t.Errorf("meta mismatch: %+v", meta)
	}
	if xml != src {
		t.Errorf("xml mismatch:\ngot:  %q\nwant: %q", xml, src)
	}
}

func TestExport_ToFile(t *testing.T) {
	setupExchangeEnv(t)
	writeDocument(t, "claude-code", exchangeDoc("a"))

	outFile := filepath.Join(t.TempDir(), "out.sloom.md")
	stdout, stderr, code := runCLI(t, []string{"export", "-o", outFile}, nil, nil)
	if code != 0 {
		t.Fatalf("export exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, outFile) {
		t.Errorf("export stdout %q missing path %q", stdout, outFile)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	if _, xml, err := exchange.Decode(data); err != nil || xml != exchangeDoc("a") {
		t.Errorf("exported file did not roundtrip: err=%v xml=%q", err, xml)
	}
}

func TestExport_ToolFlag(t *testing.T) {
	setupExchangeEnv(t)
	writeDocument(t, "other-tool", exchangeDoc("x"))

	// Default tool (claude-code) has nothing saved.
	_, _, code := runCLI(t, []string{"export"}, nil, nil)
	if code == 0 {
		t.Fatal("export with no claude-code document should fail")
	}

	stdout, stderr, code := runCLI(t, []string{"export", "--tool", "other-tool"}, nil, nil)
	if code != 0 {
		t.Fatalf("export --tool exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if _, xml, err := exchange.Decode([]byte(stdout)); err != nil || xml != exchangeDoc("x") {
		t.Errorf("export --tool did not export the other-tool document: err=%v xml=%q", err, xml)
	}
}

func TestImport_Stdin_CreatesRevision(t *testing.T) {
	setupExchangeEnv(t)
	md := exchange.Encode(exchange.Meta{Name: "Imported", Author: "them"}, exchangeDoc("a"))

	stdout, stderr, code := runCLI(t, []string{"import", "-"}, md, nil)
	if code != 0 {
		t.Fatalf("import exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "Imported claude-code as revision") {
		t.Errorf("import stdout = %q, want an Imported message", stdout)
	}

	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	rev, _, ok, err := st.Current("claude-code")
	if err != nil || !ok {
		t.Fatalf("Current: ok=%v err=%v", ok, err)
	}
	if rev.Source != exchangeDoc("a") {
		t.Errorf("current source = %q, want %q", rev.Source, exchangeDoc("a"))
	}
	if rev.Meta.Name != "Imported" || rev.Meta.Author != "them" {
		t.Errorf("current meta = %+v, want name=Imported author=them", rev.Meta)
	}
	if rev.Origin != "import" {
		t.Errorf("current origin = %q, want %q", rev.Origin, "import")
	}
}

func TestImport_FromFile(t *testing.T) {
	setupExchangeEnv(t)
	md := exchange.Encode(exchange.Meta{Name: "File Import"}, exchangeDoc("a"))
	file := filepath.Join(t.TempDir(), "in.sloom.md")
	if err := os.WriteFile(file, md, 0o600); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	_, stderr, code := runCLI(t, []string{"import", file}, nil, nil)
	if code != 0 {
		t.Fatalf("import exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	rev, _, ok, err := st.Current("claude-code")
	if err != nil || !ok || rev.Meta.Name != "File Import" {
		t.Fatalf("current after import: ok=%v err=%v meta=%+v", ok, err, rev.Meta)
	}
}

func TestImport_ToolMismatch_Rejected(t *testing.T) {
	setupExchangeEnv(t)
	md := exchange.Encode(exchange.Meta{}, exchangeDoc("a"))

	_, stderr, code := runCLI(t, []string{"import", "--tool", "other-tool", "-"}, md, nil)
	if code != 2 {
		t.Fatalf("import --tool mismatch exit = %d, want 2 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "does not match") {
		t.Errorf("stderr = %q, want a tool-mismatch message", stderr)
	}

	// Nothing should have been written under either tool.
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if _, _, ok, _ := st.Current("claude-code"); ok {
		t.Error("claude-code should have no current revision after a rejected import")
	}
	if _, _, ok, _ := st.Current("other-tool"); ok {
		t.Error("other-tool should have no current revision after a rejected import")
	}
}

func TestImport_ToolMatch_Accepted(t *testing.T) {
	setupExchangeEnv(t)
	md := exchange.Encode(exchange.Meta{}, exchangeDoc("a"))

	_, stderr, code := runCLI(t, []string{"import", "--tool", "claude-code", "-"}, md, nil)
	if code != 0 {
		t.Fatalf("import --tool match exit = %d, want 0 (stderr: %s)", code, stderr)
	}
}

func TestImport_InvalidDocument_StoreUnchanged(t *testing.T) {
	setupExchangeEnv(t)
	// Seed an existing current revision so we can prove it is untouched.
	writeDocument(t, "claude-code", exchangeDoc("a"))
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	_, curBefore, _, err := st.Current("claude-code")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}

	bad := `<statusloom version="1" tool="claude-code"><layout name="D" active="true"><line><field name="not-a-field"/></line></layout></statusloom>`
	md := exchange.Encode(exchange.Meta{}, bad)

	_, stderr, code := runCLI(t, []string{"import", "-"}, md, nil)
	if code == 0 {
		t.Fatal("importing an invalid document should fail")
	}
	if !strings.Contains(stderr, "not-a-field") {
		t.Errorf("stderr = %q, want it to mention the invalid field", stderr)
	}

	_, curAfter, ok, err := st.Current("claude-code")
	if err != nil || !ok || curAfter != curBefore {
		t.Errorf("current after rejected import = %q, want unchanged %q (ok=%v err=%v)", curAfter, curBefore, ok, err)
	}
}

func TestImport_MalformedMarkdown_Rejected(t *testing.T) {
	setupExchangeEnv(t)
	_, stderr, code := runCLI(t, []string{"import", "-"}, []byte("not a sloom.md document"), nil)
	if code == 0 {
		t.Fatal("importing malformed Markdown should fail")
	}
	if stderr == "" {
		t.Error("expected a diagnostic on stderr")
	}
}

func TestImport_DedupNoOp(t *testing.T) {
	setupExchangeEnv(t)
	md := exchange.Encode(exchange.Meta{Name: "n"}, exchangeDoc("a"))

	_, stderr, code := runCLI(t, []string{"import", "-"}, md, nil)
	if code != 0 {
		t.Fatalf("first import exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	_, firstID, ok, err := st.Current("claude-code")
	if err != nil || !ok {
		t.Fatalf("Current after first import: ok=%v err=%v", ok, err)
	}

	// Re-importing byte-identical content is a dedup no-op: no new revision.
	stdout, stderr, code := runCLI(t, []string{"import", "-"}, md, nil)
	if code != 0 {
		t.Fatalf("second import exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "No changes") {
		t.Errorf("second import stdout = %q, want a No changes message", stdout)
	}
	_, secondID, ok, err := st.Current("claude-code")
	if err != nil || !ok || secondID != firstID {
		t.Errorf("current after dedup import = %q, want unchanged %q (ok=%v err=%v)", secondID, firstID, ok, err)
	}
}

func TestImport_UsageErrors(t *testing.T) {
	setupExchangeEnv(t)
	for _, args := range [][]string{
		{"import"},
		{"import", "a", "b"},
	} {
		_, _, code := runCLI(t, args, nil, nil)
		if code != 2 {
			t.Errorf("args %v: exit = %d, want 2", args, code)
		}
	}
}

func TestExport_UsageErrors(t *testing.T) {
	setupExchangeEnv(t)
	_, _, code := runCLI(t, []string{"export", "--unexpected"}, nil, nil)
	if code != 2 {
		t.Errorf("unexpected flag: exit = %d, want 2", code)
	}
}
