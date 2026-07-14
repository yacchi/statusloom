package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/config"
)

// unformatted has a symbolic when (&gt;=), non-canonical attribute order, and
// idiosyncratic indentation — all of which `fmt` rewrites into canonical form.
const unformatted = `<statusloom version="1" tool="claude-code">
  <layout name="default" active="true">
    <line>
        <field color="cyan" name="model"/>
        <field name="context-percentage" when="context-percent &gt;= 80"/>
    </line>
  </layout>
</statusloom>
`

func TestFmt_File_RewritesCanonical(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.xml")
	if err := os.WriteFile(path, []byte(unformatted), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, []string{"fmt", path}, nil, nil)
	if code != 0 {
		t.Fatalf("fmt exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "Formatted") {
		t.Errorf("stdout missing confirmation: %q", stdout)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	// Canonical attribute order (name before color) and word-form when.
	if !strings.Contains(got, `<field name="model" color="cyan"/>`) {
		t.Errorf("attribute order not canonicalized:\n%s", got)
	}
	if !strings.Contains(got, `when="context-percent ge 80"`) {
		t.Errorf("when not normalized to word form:\n%s", got)
	}
	if strings.Contains(got, "&gt;") {
		t.Errorf("symbolic operator survived:\n%s", got)
	}

	// Running fmt again is a no-op (idempotent).
	stdout2, _, code2 := runCLI(t, []string{"fmt", path}, nil, nil)
	if code2 != 0 {
		t.Fatalf("second fmt exit = %d, want 0", code2)
	}
	if !strings.Contains(stdout2, "already formatted") {
		t.Errorf("second fmt should be a no-op: %q", stdout2)
	}
}

func TestFmt_Check_ReportsDifference(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.xml")
	if err := os.WriteFile(path, []byte(unformatted), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCLI(t, []string{"fmt", "--check", path}, nil, nil)
	if code != 1 {
		t.Fatalf("fmt --check exit = %d, want 1 (unformatted)", code)
	}
	if !strings.Contains(stderr, "not formatted") {
		t.Errorf("stderr missing 'not formatted': %q", stderr)
	}
	// --check must not write.
	out, _ := os.ReadFile(path)
	if string(out) != unformatted {
		t.Errorf("--check modified the file:\n%s", out)
	}

	// After formatting, --check passes.
	if _, _, c := runCLI(t, []string{"fmt", path}, nil, nil); c != 0 {
		t.Fatalf("fmt failed")
	}
	if _, _, c := runCLI(t, []string{"fmt", "--check", path}, nil, nil); c != 0 {
		t.Errorf("fmt --check on a formatted file should exit 0, got %d", c)
	}
}

func TestFmt_Stdin_ToStdout(t *testing.T) {
	stdout, stderr, code := runCLI(t, []string{"fmt", "-"}, []byte(unformatted), nil)
	if code != 0 {
		t.Fatalf("fmt - exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, `when="context-percent ge 80"`) {
		t.Errorf("stdin->stdout not canonicalized:\n%s", stdout)
	}
}

func TestFmt_ErrorsAbort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.xml")
	// Unknown field is a validation error.
	bad := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><line><field name="does-not-exist"/></line></layout></statusloom>`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, []string{"fmt", path}, nil, nil)
	if code == 0 {
		t.Fatal("fmt should fail on a document with errors")
	}
	if !strings.Contains(stderr, "error") {
		t.Errorf("stderr should report the error: %q", stderr)
	}
	// The file is left untouched.
	out, _ := os.ReadFile(path)
	if string(out) != bad {
		t.Errorf("errored fmt must not rewrite the file")
	}
}

func TestFmt_DefaultToolFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	if err := config.SaveDocumentSource("claude-code", unformatted); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLI(t, []string{"fmt"}, nil, nil)
	if code != 0 {
		t.Fatalf("fmt (default file) exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	data, err := os.ReadFile(config.DocumentPath("claude-code"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `when="context-percent ge 80"`) {
		t.Errorf("default <tool>.xml not formatted:\n%s", data)
	}
}
