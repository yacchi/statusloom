package exchange

import (
	"fmt"
	"strings"
	"testing"
)

func TestRoundtrip(t *testing.T) {
	// Test 1: Encode→Decode roundtrip
	meta := Meta{
		Name:        "Test Config",
		Description: "A test configuration",
		Author:      "Test Author",
	}
	xmlSource := "<statusloom>\n  <layout>test</layout>\n</statusloom>"

	encoded := Encode(meta, xmlSource)
	decodedMeta, decodedXML, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decodedMeta.Name != meta.Name {
		t.Errorf("Name mismatch: got %q, want %q", decodedMeta.Name, meta.Name)
	}
	if decodedMeta.Description != meta.Description {
		t.Errorf("Description mismatch: got %q, want %q", decodedMeta.Description, meta.Description)
	}
	if decodedMeta.Author != meta.Author {
		t.Errorf("Author mismatch: got %q, want %q", decodedMeta.Author, meta.Author)
	}
	if decodedXML != xmlSource {
		t.Errorf("XML mismatch: got %q, want %q", decodedXML, xmlSource)
	}
}

func TestSpecialCharactersInDescription(t *testing.T) {
	// Test 2: Colon, #, quotes, emoji
	meta := Meta{
		Name:        "Special",
		Description: "使い方: 注意 #1 も書ける 🎉",
		Author:      `Author "with quotes"`,
	}
	xmlSource := "<root/>"

	encoded := Encode(meta, xmlSource)
	decodedMeta, _, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decodedMeta.Description != meta.Description {
		t.Errorf("Description with special chars mismatch: got %q, want %q", decodedMeta.Description, meta.Description)
	}
	if decodedMeta.Author != meta.Author {
		t.Errorf("Author with quotes mismatch: got %q, want %q", decodedMeta.Author, meta.Author)
	}
}

func TestNorwayProblem(t *testing.T) {
	// Test 3: "no" should be decoded as string "no", not boolean false (Norway problem)
	markdown := `---
format: "statusloom/config"
formatVersion: 1
name: "no"
description: "no"
author: "no"
---

` + "```xml\n<root/>\n```\n"

	meta, _, err := Decode([]byte(markdown))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if meta.Name != "no" {
		t.Errorf("Name should be string \"no\", got %v", meta.Name)
	}
	if meta.Description != "no" {
		t.Errorf("Description should be string \"no\", got %v", meta.Description)
	}
	if meta.Author != "no" {
		t.Errorf("Author should be string \"no\", got %v", meta.Author)
	}
}

func TestMissingFormat(t *testing.T) {
	// Test 4a: missing format key
	markdown := `---
formatVersion: 1
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error for missing format key")
	}
}

func TestWrongFormat(t *testing.T) {
	// Test 4b: wrong format value
	markdown := `---
format: "wrong/format"
formatVersion: 1
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error for wrong format value")
	}
}

func TestWrongFormatVersion(t *testing.T) {
	// Test 4c: wrong formatVersion
	markdown := `---
format: "statusloom/config"
formatVersion: 2
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error for wrong formatVersion")
	}
}

func TestNoFrontmatter(t *testing.T) {
	// Test 5a: no frontmatter
	markdown := "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error when frontmatter is missing")
	}
}

func TestNoXMLFence(t *testing.T) {
	// Test 5b: no ```xml fence
	markdown := `---
format: "statusloom/config"
formatVersion: 1
---

This is just text without XML fence.
`

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error when ```xml fence is missing")
	}
}

func TestCRLFInput(t *testing.T) {
	// Test 6: CRLF input
	markdown := "---\r\nformat: \"statusloom/config\"\r\nformatVersion: 1\r\nname: \"Test\"\r\n---\r\n\r\n```xml\r\n<root/>\r\n```\r\n"

	meta, xml, err := Decode([]byte(markdown))
	if err != nil {
		t.Fatalf("Decode with CRLF failed: %v", err)
	}

	if meta.Name != "Test" {
		t.Errorf("CRLF test: expected name \"Test\", got %q", meta.Name)
	}
	if xml != "<root/>" {
		t.Errorf("CRLF test: expected XML \"<root/>\", got %q", xml)
	}
}

func TestUnknownKeysIgnored(t *testing.T) {
	// Test 7: unknown keys are ignored
	markdown := `---
format: "statusloom/config"
formatVersion: 1
name: "Test"
unknown_key: "should be ignored"
another_unknown: 123
---

` + "```xml\n<root/>\n```\n"

	meta, _, err := Decode([]byte(markdown))
	if err != nil {
		t.Fatalf("Decode with unknown keys failed: %v", err)
	}

	if meta.Name != "Test" {
		t.Errorf("Expected name \"Test\", got %q", meta.Name)
	}
	// Unknown keys should be silently ignored without affecting parsing
}

func TestTextOutsideFenceIgnored(t *testing.T) {
	// Test 8: text before and after fence is ignored
	markdown := `---
format: "statusloom/config"
formatVersion: 1
name: "Test"
---

This is a human-readable explanation that comes before the XML fence.

` + "```xml\n<root/>\n```\n" + `

This is text after the fence. It should also be ignored.

More documentation here.
`

	meta, xml, err := Decode([]byte(markdown))
	if err != nil {
		t.Fatalf("Decode with surrounding text failed: %v", err)
	}

	if meta.Name != "Test" {
		t.Errorf("Expected name \"Test\", got %q", meta.Name)
	}
	if xml != "<root/>" {
		t.Errorf("Expected XML \"<root/>\", got %q", xml)
	}
}

func TestEmptyMetaFieldsOmitted(t *testing.T) {
	// Encode should omit empty fields
	meta := Meta{
		Name:        "Only Name",
		Description: "",
		Author:      "",
	}
	xmlSource := "<root/>"

	encoded := Encode(meta, xmlSource)
	encodedStr := string(encoded)

	if !strings.Contains(encodedStr, "name: \"Only Name\"") {
		t.Error("name field should be present")
	}
	if strings.Contains(encodedStr, "description:") {
		t.Error("empty description field should be omitted")
	}
	if strings.Contains(encodedStr, "author:") {
		t.Error("empty author field should be omitted")
	}
}

func TestXMLWithoutTrailingNewline(t *testing.T) {
	// Encode should add trailing newline to XML if missing
	meta := Meta{Name: "Test"}
	xmlSource := "<root/>"

	encoded := Encode(meta, xmlSource)
	encodedStr := string(encoded)

	if !strings.Contains(encodedStr, "<root/>\n```") {
		t.Error("Encode should add newline before closing fence")
	}
}

func TestXMLWithTrailingNewline(t *testing.T) {
	// Encode should not double-add newline
	meta := Meta{Name: "Test"}
	xmlSource := "<root/>\n"

	encoded := Encode(meta, xmlSource)
	encodedStr := string(encoded)

	// Count newlines before closing fence
	if strings.Contains(encodedStr, "<root/>\n\n```") {
		t.Error("Encode should not add extra newline if XML already has one")
	}
}

func TestBOMRemoval(t *testing.T) {
	// Decode should handle UTF-8 BOM
	bom := []byte{0xEF, 0xBB, 0xBF}
	markdown := `---
format: "statusloom/config"
formatVersion: 1
name: "With BOM"
---

` + "```xml\n<root/>\n```\n"

	input := append(bom, []byte(markdown)...)
	meta, _, err := Decode(input)
	if err != nil {
		t.Fatalf("Decode with BOM failed: %v", err)
	}

	if meta.Name != "With BOM" {
		t.Errorf("BOM test: expected name \"With BOM\", got %q", meta.Name)
	}
}

func TestEmptyLines(t *testing.T) {
	// Frontmatter empty lines should be ignored
	markdown := `---
format: "statusloom/config"

formatVersion: 1
name: "Test"

author: "Author"
---

` + "```xml\n<root/>\n```\n"

	meta, _, err := Decode([]byte(markdown))
	if err != nil {
		t.Fatalf("Decode with empty lines failed: %v", err)
	}

	if meta.Name != "Test" || meta.Author != "Author" {
		t.Errorf("Empty lines test: meta = %+v", meta)
	}
}

func TestInvalidJSONValue(t *testing.T) {
	// Invalid JSON value should cause error
	markdown := `---
format: "statusloom/config"
formatVersion: 1
name: unquoted_string
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error for unquoted JSON string")
	}
}

func TestMultilineXMLContent(t *testing.T) {
	// Roundtrip with multiline XML
	meta := Meta{
		Name:        "Multiline",
		Description: "Test multiline XML",
		Author:      "Test",
	}
	xmlSource := `<statusloom>
  <layout>
    <line>
      <field name="test"/>
    </line>
  </layout>
</statusloom>`

	encoded := Encode(meta, xmlSource)
	decodedMeta, decodedXML, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode multiline XML failed: %v", err)
	}

	if decodedXML != xmlSource {
		t.Errorf("Multiline XML mismatch:\ngot:\n%s\nwant:\n%s", decodedXML, xmlSource)
	}
	if decodedMeta.Name != meta.Name || decodedMeta.Description != meta.Description {
		t.Errorf("Meta mismatch: got %+v, want %+v", decodedMeta, meta)
	}
}

func TestNumberFormatVersion(t *testing.T) {
	// formatVersion should be accepted as number
	markdown := `---
format: "statusloom/config"
formatVersion: 1
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err != nil {
		t.Fatalf("Decode with numeric formatVersion failed: %v", err)
	}
}

func TestNameAsNumber(t *testing.T) {
	// Test: name field must be string, not number
	markdown := `---
format: "statusloom/config"
formatVersion: 1
name: 12345
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error when name is a number, but got nil")
	}
}

func TestDescriptionAsNumber(t *testing.T) {
	// Test: description field must be string, not number
	markdown := `---
format: "statusloom/config"
formatVersion: 1
description: 99999
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error when description is a number, but got nil")
	}
}

func TestAuthorAsNumber(t *testing.T) {
	// Test: author field must be string, not number
	markdown := `---
format: "statusloom/config"
formatVersion: 1
author: 42
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error when author is a number, but got nil")
	}
}

func TestFormatAsNumber(t *testing.T) {
	// Test: format field must be string, not number
	markdown := `---
format: 123
formatVersion: 1
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error when format is a number, but got nil")
	}
}

func TestFormatVersionAsString(t *testing.T) {
	// Test: formatVersion must be number, not string
	markdown := `---
format: "statusloom/config"
formatVersion: "1"
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error when formatVersion is a string, but got nil")
	}
}

func TestNameAsArray(t *testing.T) {
	// Test: name field must be string, not array
	markdown := `---
format: "statusloom/config"
formatVersion: 1
name: ["a", "b"]
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error when name is an array, but got nil")
	}
}

func TestNameAsObject(t *testing.T) {
	// Test: name field must be string, not object
	markdown := `---
format: "statusloom/config"
formatVersion: 1
name: {"key": "value"}
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error when name is an object, but got nil")
	}
}

func TestNameAsBoolean(t *testing.T) {
	// Test: name field must be string, not boolean
	markdown := `---
format: "statusloom/config"
formatVersion: 1
name: true
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error when name is a boolean, but got nil")
	}
}

func TestNameAsNull(t *testing.T) {
	// Test: name field must be string, not null
	markdown := `---
format: "statusloom/config"
formatVersion: 1
name: null
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error when name is null, but got nil")
	}
}

func TestFormatVersionFractional(t *testing.T) {
	// Test: formatVersion must be exactly 1, not fractional values
	testCases := []float64{1.9, 1.0001, 1.5, 1.1, 1.9999}

	for _, fv := range testCases {
		markdown := `---
format: "statusloom/config"
formatVersion: ` + fmt.Sprint(fv) + `
---

` + "```xml\n<root/>\n```\n"

		_, _, err := Decode([]byte(markdown))
		if err == nil {
			t.Errorf("Expected error for formatVersion: %v, but got nil", fv)
		}
	}
}

func TestFormatVersionZero(t *testing.T) {
	// Test: formatVersion must be 1, not 0
	markdown := `---
format: "statusloom/config"
formatVersion: 0
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error for formatVersion: 0, but got nil")
	}
}

func TestNotesRoundtrip(t *testing.T) {
	// Free-form Markdown prose between frontmatter and the ```xml fence
	// survives an Encode -> Decode roundtrip verbatim, including internal
	// structure (heading, blank line, paragraph).
	meta := Meta{
		Name:  "With notes",
		Notes: "# Usage\n\nRemember to run `statusloom doctor` after editing this.",
	}
	xmlSource := "<root/>"

	encoded := Encode(meta, xmlSource)
	decodedMeta, decodedXML, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if decodedMeta.Notes != meta.Notes {
		t.Errorf("Notes mismatch:\ngot:\n%q\nwant:\n%q", decodedMeta.Notes, meta.Notes)
	}
	if decodedXML != xmlSource {
		t.Errorf("XML mismatch: got %q, want %q", decodedXML, xmlSource)
	}
}

func TestNotesAbsentWhenNoProse(t *testing.T) {
	// No prose between frontmatter and fence -> Notes stays empty (Encode
	// with a zero-value Notes must not manufacture one on Decode).
	meta := Meta{Name: "No notes"}
	xmlSource := "<root/>"

	encoded := Encode(meta, xmlSource)
	decodedMeta, _, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if decodedMeta.Notes != "" {
		t.Errorf("expected empty Notes, got %q", decodedMeta.Notes)
	}
}

func TestNotesIsNotAFrontmatterKey(t *testing.T) {
	// Encode must never emit Notes as a "notes:" frontmatter key — it is
	// body prose, not a scalar frontmatter value (§6.5).
	meta := Meta{Notes: "Some prose."}
	encoded := Encode(meta, "<root/>")
	if strings.Contains(string(encoded), "notes:") {
		t.Error("Encode must not write Notes as a frontmatter key")
	}
}

func TestNotesIgnoresTextAfterFence(t *testing.T) {
	// Prose after the closing ```xml fence is not part of Notes (it was
	// already ignored entirely before Notes existed; this pins that Notes
	// doesn't accidentally pick it up).
	markdown := `---
format: "statusloom/config"
formatVersion: 1
---

Prose that belongs in Notes.

` + "```xml\n<root/>\n```\n" + `

Prose after the fence — must be ignored, not folded into Notes.
`

	meta, _, err := Decode([]byte(markdown))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if meta.Notes != "Prose that belongs in Notes." {
		t.Errorf("Notes = %q, want only the pre-fence prose", meta.Notes)
	}
}

func TestNotesWithEmbeddedNonXMLExampleFence(t *testing.T) {
	// The author's notes prose may itself contain an example fenced code
	// block that is NOT ```xml (e.g. a ```bash snippet). Decode must walk
	// past it as an ordinary paired fence (not mistake its closing ``` line
	// for a boundary) and still find the real config in the first ```xml
	// fence that follows.
	markdown := `---
format: "statusloom/config"
formatVersion: 1
---

# About this file

Run it like:

` + "```bash\nstatusloom claude < input.json\n```" + `

Don't forget to run the doctor command after editing.

` + "```xml\n<root/>\n```\n"

	meta, xml, err := Decode([]byte(markdown))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if xml != "<root/>" {
		t.Errorf("real config XML = %q, want %q", xml, "<root/>")
	}
	wantNotes := "# About this file\n\n" +
		"Run it like:\n\n" +
		"```bash\nstatusloom claude < input.json\n```\n\n" +
		"Don't forget to run the doctor command after editing."
	if meta.Notes != wantNotes {
		t.Errorf("Notes mismatch:\ngot:\n%q\nwant:\n%q", meta.Notes, wantNotes)
	}
}

func TestFirstXMLFenceWinsOverTrailingXMLBlock(t *testing.T) {
	// Per plan §6.6, the real config is the FIRST ```xml fence. A second
	// ```xml block appearing later in the file (e.g. a trailing reference
	// or legacy example, still before EOF) must be ignored exactly like any
	// other text after the real fence's closing ``` — it must NOT be picked
	// up as the real config, and it must NOT be folded into Notes either
	// (Notes is only the prose between frontmatter and the first fence).
	markdown := `---
format: "statusloom/config"
formatVersion: 1
---

Here is the real, active configuration:

` + "```xml\n<statusloom><layout name=\"main\"><text>REAL-CONFIG</text></layout></statusloom>\n```" + `

For reference, here is what an alternate/legacy config looked like (not the active config):

` + "```xml\n<statusloom><layout name=\"legacy\"><text>OLD-REFERENCE-CONFIG</text></layout></statusloom>\n```\n"

	meta, xml, err := Decode([]byte(markdown))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	wantXML := `<statusloom><layout name="main"><text>REAL-CONFIG</text></layout></statusloom>`
	if xml != wantXML {
		t.Errorf("real config XML = %q, want the first fence's content %q (not the second/legacy fence)", xml, wantXML)
	}
	wantNotes := "Here is the real, active configuration:"
	if meta.Notes != wantNotes {
		t.Errorf("Notes mismatch:\ngot:\n%q\nwant:\n%q", meta.Notes, wantNotes)
	}
	if strings.Contains(meta.Notes, "OLD-REFERENCE-CONFIG") || strings.Contains(meta.Notes, "legacy") {
		t.Errorf("Notes must not contain the trailing/ignored second xml block, got: %q", meta.Notes)
	}
}

func TestFormatVersionTwo(t *testing.T) {
	// Test: formatVersion must be 1, not 2
	markdown := `---
format: "statusloom/config"
formatVersion: 2
---

` + "```xml\n<root/>\n```\n"

	_, _, err := Decode([]byte(markdown))
	if err == nil {
		t.Error("Expected error for formatVersion: 2, but got nil")
	}
}
