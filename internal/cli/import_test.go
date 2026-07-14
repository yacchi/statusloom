package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/dsl"
)

func ccFixture(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "fixtures", "ccstatusline", "settings-v3.json")
}

// loadImportedDoc parses the claude-code.xml document written into dir by an
// import, failing the test if it is missing or invalid.
func loadImportedDoc(t *testing.T, dir string) *dsl.Document {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "claude-code.xml"))
	if err != nil {
		t.Fatalf("read claude-code.xml: %v", err)
	}
	doc, diags := dsl.Parse(string(data))
	if doc == nil || doc.Root == nil {
		t.Fatalf("parse claude-code.xml: nil document; diags=%v", diags)
	}
	if diags := append(diags, dsl.Validate(doc)...); dsl.HasErrors(diags) {
		t.Fatalf("claude-code.xml has errors: %v", diags)
	}
	return doc
}

// activeLayout returns the layout with active="true" (the sole active layout
// after any import).
func activeLayout(t *testing.T, doc *dsl.Document) *dsl.LayoutNode {
	t.Helper()
	for _, l := range doc.Root.Layouts {
		if l.Active != nil && *l.Active {
			return l
		}
	}
	t.Fatalf("no active layout in %v", doc.Root.Layouts)
	return nil
}

// fieldNames collects the <field> names of the first line of a layout, in
// order.
func fieldNames(l *dsl.LayoutNode) []string {
	var out []string
	if len(l.Lines) == 0 {
		return out
	}
	for _, ch := range l.Lines[0].Children {
		if f, ok := ch.(*dsl.FieldNode); ok {
			out = append(out, f.Name)
		}
	}
	return out
}

func TestImportCCStatuslineFixture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("STATUSLOOM_CONFIG", path)
	out, stderr, code := runCLI(t, []string{"import", "ccstatusline", ccFixture(t)}, nil, nil)
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	// The fixture sets flexMode "full-minus-40" but has no flex-separator
	// widgets, so the setting is reported as having had no effect. colorLevel
	// and compactThreshold are always reported as not applied.
	wantOut := "Imported 3 widgets. Added layout \"ccstatusline\" to claude-code and made it active.\n" +
		"Unsupported settings:\n" +
		"- flexMode \"full-minus-40\" (no flex-separator widgets; no effect)\n" +
		"- colorLevel \"ansi256\" (not applied; existing tool colorLevel preserved)\n" +
		"- compactThreshold 60 (not applied; existing tool compactThreshold preserved)\n"
	if out != wantOut {
		t.Fatalf("stdout=%q want=%q", out, wantOut)
	}

	doc := loadImportedDoc(t, dir)
	// The default document's layout is preserved and the imported layout is
	// appended after it, then made active.
	if len(doc.Root.Layouts) != 2 {
		t.Fatalf("len(layouts)=%d want=2", len(doc.Root.Layouts))
	}
	imported := activeLayout(t, doc)
	if imported.Name != "ccstatusline" {
		t.Errorf("active layout name=%q want=%q", imported.Name, "ccstatusline")
	}
	if imported != doc.Root.Layouts[1] {
		t.Errorf("imported layout is not the last (appended) layout")
	}
	// model + five-hour-usage fields (separator becomes a <text>, not a field).
	if got := fieldNames(imported); len(got) != 2 || got[0] != "model" || got[1] != "five-hour-usage" {
		t.Errorf("imported fields=%v want=[model five-hour-usage]", got)
	}
	// The session-usage widget's "5h: " label became a prefix, gated with
	// optional so the label disappears when the usage value is empty
	// (matching the legacy template semantics).
	var five *dsl.FieldNode
	for _, ch := range imported.Lines[0].Children {
		if f, ok := ch.(*dsl.FieldNode); ok && f.Name == "five-hour-usage" {
			five = f
		}
	}
	if five == nil || five.Common.Prefix != "5h: " {
		t.Errorf("five-hour-usage prefix=%q want %q", five.Common.Prefix, "5h: ")
	}
	if five != nil && five.Common.Optional != "five-hour-usage" {
		t.Errorf("five-hour-usage optional=%q want %q", five.Common.Optional, "five-hour-usage")
	}
	// model retained its cyan color.
	if fieldNames(imported)[0] == "model" {
		if m, _ := imported.Lines[0].Children[0].(*dsl.FieldNode); m == nil || m.Common.Style.Color != "cyan" {
			t.Errorf("model color not preserved: %+v", imported.Lines[0].Children[0])
		}
	}
}

func TestImportCCStatuslineAppendDedupesName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("STATUSLOOM_CONFIG", path)

	// First import creates layout "ccstatusline".
	if _, stderr, code := runCLI(t, []string{"import", "ccstatusline", ccFixture(t)}, nil, nil); code != 0 || stderr != "" {
		t.Fatalf("first import: code=%d stderr=%q", code, stderr)
	}
	// Second import must not collide with the first layout's name.
	out, stderr, code := runCLI(t, []string{"import", "ccstatusline", ccFixture(t)}, nil, nil)
	if code != 0 || stderr != "" {
		t.Fatalf("second import: code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(out, `Added layout "ccstatusline 2"`) {
		t.Errorf("stdout=%q, want mention of deduped layout name", out)
	}

	doc := loadImportedDoc(t, dir)
	names := make([]string, len(doc.Root.Layouts))
	seen := make(map[string]bool)
	for i, l := range doc.Root.Layouts {
		names[i] = l.Name
		if seen[l.Name] {
			t.Fatalf("duplicate layout name %q in %v", l.Name, names)
		}
		seen[l.Name] = true
	}
	if !seen["ccstatusline"] || !seen["ccstatusline 2"] {
		t.Errorf("layouts=%v, want both %q and %q", names, "ccstatusline", "ccstatusline 2")
	}
	if active := activeLayout(t, doc); active.Name != "ccstatusline 2" {
		t.Errorf("active layout=%q, want the last-appended %q", active.Name, "ccstatusline 2")
	}
}

func TestImportCCStatuslineReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("STATUSLOOM_CONFIG", path)

	if _, stderr, code := runCLI(t, []string{"import", "ccstatusline", ccFixture(t)}, nil, nil); code != 0 || stderr != "" {
		t.Fatalf("first import: code=%d stderr=%q", code, stderr)
	}
	out, stderr, code := runCLI(t, []string{"import", "ccstatusline", "--replace", ccFixture(t)}, nil, nil)
	if code != 0 || stderr != "" {
		t.Fatalf("replace import: code=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(out, `Replaced claude-code's layouts with "ccstatusline"`) {
		t.Errorf("stdout=%q, want replace summary", out)
	}

	doc := loadImportedDoc(t, dir)
	if len(doc.Root.Layouts) != 1 {
		t.Fatalf("len(layouts)=%d, want 1 after --replace", len(doc.Root.Layouts))
	}
	if doc.Root.Layouts[0].Name != "ccstatusline" {
		t.Errorf("layouts[0].name=%q, want %q", doc.Root.Layouts[0].Name, "ccstatusline")
	}
	if a := doc.Root.Layouts[0].Active; a == nil || !*a {
		t.Errorf("layouts[0] not active after --replace")
	}
}

func TestImportCCSettingsFlexMapping(t *testing.T) {
	source := ccSettings{
		Version:  3,
		FlexMode: "full-minus-40",
		Lines: [][]ccWidget{{
			{Type: "model"},
			{Type: "flex-separator"},
			{Type: "git-branch"},
			{Type: "flex-separator"},
		}},
	}
	lines, count, unsupported, settings := importCCSettings(source)
	if count != 4 || len(unsupported) != 0 {
		t.Fatalf("count=%d unsupported=%v", count, unsupported)
	}
	// flexMode maps onto each flex-separator; no "no effect" warning.
	for _, item := range settings {
		if strings.Contains(item, "flexMode") {
			t.Errorf("unexpected flexMode warning: %q", item)
		}
	}
	for _, idx := range []int{1, 3} {
		if got := lines[0][idx].Flex; got != "full-minus-40" {
			t.Errorf("widget %d Flex = %q, want full-minus-40", idx, got)
		}
	}
	// "full" and "" match the per-widget default and are not written.
	for _, mode := range []string{"", "full"} {
		source.FlexMode = mode
		lines, _, _, _ := importCCSettings(source)
		if got := lines[0][1].Flex; got != "" {
			t.Errorf("flexMode %q: widget Flex = %q, want empty", mode, got)
		}
	}
	// "off" has no per-widget equivalent -> unsupported warning, no Flex.
	source.FlexMode = "off"
	lines, _, _, settings = importCCSettings(source)
	if got := lines[0][1].Flex; got != "" {
		t.Errorf("flexMode off: widget Flex = %q, want empty", got)
	}
	found := false
	for _, item := range settings {
		if item == `flexMode "off" (no per-widget equivalent)` {
			found = true
		}
	}
	if !found {
		t.Errorf("flexMode off: settings = %v, want unsupported warning", settings)
	}
}

func TestImportCCStatuslineWarnings(t *testing.T) {
	tests := []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"unknown widget once", func(d map[string]any) {
			d["lines"] = []any{[]any{map[string]any{"type": "mystery"}, map[string]any{"type": "mystery"}}}
		}, "Unsupported widgets:\n- mystery\n"},
		{"powerline", func(d map[string]any) { d["powerline"] = map[string]any{"enabled": true} }, "Unsupported settings:\n- powerline.enabled\n"},
		{"version two", func(d map[string]any) { d["version"] = 2 }, "Unsupported settings:\n- version 2 (expected 3)\n"},
	}
	fixture, err := os.ReadFile(ccFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal(fixture, &doc); err != nil {
				t.Fatal(err)
			}
			// The fixture's flexMode would add its own "no effect"
			// warning; neutralize it so each case sees only the warning
			// it is about.
			doc["flexMode"] = "full"
			tt.edit(doc)
			input := filepath.Join(t.TempDir(), "input.json")
			b, _ := json.Marshal(doc)
			if err := os.WriteFile(input, b, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
			out, stderr, code := runCLI(t, []string{"import", "ccstatusline", input}, nil, nil)
			if code != 0 || stderr != "" {
				t.Fatalf("code=%d stderr=%q", code, stderr)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("stdout=%q missing %q", out, tt.want)
			}
		})
	}
}

func TestImportCCStatuslineFiles(t *testing.T) {
	t.Run("dry run writes nothing", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		t.Setenv("STATUSLOOM_CONFIG", path)
		out, stderr, code := runCLI(t, []string{"import", "ccstatusline", "--dry-run", ccFixture(t)}, nil, nil)
		if code != 0 || stderr != "" {
			t.Fatalf("code=%d out=%q err=%q", code, out, stderr)
		}
		// The dry-run output is the serialized DSL document, not JSON.
		doc, diags := dsl.Parse(out)
		if doc == nil || doc.Root == nil {
			t.Fatalf("dry-run output not a valid DSL document: %v", diags)
		}
		if diags := append(diags, dsl.Validate(doc)...); dsl.HasErrors(diags) {
			t.Fatalf("dry-run document has errors: %v", diags)
		}
		if a := activeLayout(t, doc); a.Name != "ccstatusline" {
			t.Errorf("dry-run active layout=%q, want %q", a.Name, "ccstatusline")
		}
		// Nothing was written to disk.
		if _, err := os.Stat(filepath.Join(dir, "claude-code.xml")); !os.IsNotExist(err) {
			t.Fatalf("claude-code.xml unexpectedly exists: %v", err)
		}
	})
	t.Run("backup existing", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")
		xmlPath := filepath.Join(dir, "claude-code.xml")
		original := []byte(`<statusloom version="1" tool="claude-code"><layout name="old" active="true"><line><field name="model"/></line></layout></statusloom>`)
		if err := os.WriteFile(xmlPath, original, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("STATUSLOOM_CONFIG", path)
		_, stderr, code := runCLI(t, []string{"import", "ccstatusline", ccFixture(t)}, nil, nil)
		if code != 0 {
			t.Fatalf("import failed: %q", stderr)
		}
		matches, _ := filepath.Glob(xmlPath + ".bak.*")
		if len(matches) != 1 {
			t.Fatalf("backups=%v", matches)
		}
		got, _ := os.ReadFile(matches[0])
		if string(got) != string(original) {
			t.Errorf("backup=%q", got)
		}
		// The pre-existing "old" layout is preserved alongside the imported one.
		doc := loadImportedDoc(t, dir)
		names := make(map[string]bool)
		for _, l := range doc.Root.Layouts {
			names[l.Name] = true
		}
		if !names["old"] || !names["ccstatusline"] {
			t.Errorf("layouts=%v, want both old and ccstatusline", names)
		}
	})
	t.Run("missing input", func(t *testing.T) {
		t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "config.json"))
		_, stderr, code := runCLI(t, []string{"import", "ccstatusline", filepath.Join(t.TempDir(), "missing")}, nil, nil)
		if code != 1 || !strings.HasPrefix(stderr, "statusloom: ") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
	})
}
