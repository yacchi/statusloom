package dsl

import (
	"strings"
	"testing"
)

// validate parses (requiring no fatal) and returns the validation diags.
func validate(t *testing.T, src string) []Diagnostic {
	t.Helper()
	doc, pdiags := Parse(src)
	if doc == nil {
		t.Fatalf("Parse fatal: %v", pdiags)
	}
	return Validate(doc)
}

// validateWrapped validates a document whose single line contains inner.
func validateWrapped(t *testing.T, inner string) []Diagnostic {
	t.Helper()
	return validate(t, wrap(inner))
}

func expectNoErrors(t *testing.T, diags []Diagnostic) {
	t.Helper()
	if HasErrors(diags) {
		t.Fatalf("expected no errors, got %v", diags)
	}
}

func expectError(t *testing.T, diags []Diagnostic, substr string) {
	t.Helper()
	if !hasErrorContaining(diags, substr) {
		t.Fatalf("expected error containing %q, got %v", substr, diags)
	}
}

func TestValidateHappyPath(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="ansi16" context-percentage-mode="usable">
  <layout name="default" active="true">
    <line>
      <text>Model: </text>
      <field name="model" color="cyan" bold="true"/>
      <span optional="thinking-effort" prefix=" (" suffix=")" color="bright-black">
        <field name="thinking-effort" color="yellow"/>
      </span>
      <text role="separator" padding="1">|</text>
      <field name="context-percentage-usable" format="percent" precision="0">
        <color-rule when="self ge 90" color="red"/>
        <color-rule when="self ge 70" color="yellow"/>
      </field>
    </line>
  </layout>
</statusloom>`
	expectNoErrors(t, validate(t, src))
}

func TestValidateVersionMustBeOne(t *testing.T) {
	src := `<statusloom version="2" tool="claude-code"><layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`
	expectError(t, validate(t, src), `version must be "1"`)
}

func TestValidateUnknownTool(t *testing.T) {
	src := `<statusloom version="1" tool="nope"><layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`
	expectError(t, validate(t, src), `unknown tool "nope"`)
}

func TestValidateUnknownField(t *testing.T) {
	expectError(t, validateWrapped(t, `<field name="not-a-field"/>`), `unknown field "not-a-field"`)
}

func TestValidateFormatterMismatch(t *testing.T) {
	// model accepts no formatter.
	expectError(t, validateWrapped(t, `<field name="model" format="percent"/>`), "not applicable to field")
}

func TestValidateInvalidPrecision(t *testing.T) {
	expectError(t, validateWrapped(t, `<field name="context-percentage" format="percent" precision="-1"/>`), "precision")
}

func TestValidateHyperlinkOnlyOnLinkable(t *testing.T) {
	expectError(t, validateWrapped(t, `<field name="model" hyperlink="true"/>`), "does not support hyperlink")
	expectNoErrors(t, validateWrapped(t, `<field name="pr-number" hyperlink="true"/>`))
}

func TestValidateOptionalReferencesField(t *testing.T) {
	expectError(t, validateWrapped(t, `<span optional="nope"><field name="model"/></span>`), `optional references unknown field "nope"`)
	expectNoErrors(t, validateWrapped(t, `<span optional="thinking-effort"><field name="model"/></span>`))
}

func TestValidateColorFormats(t *testing.T) {
	expectNoErrors(t, validateWrapped(t, `<field name="model" color="bright-cyan"/>`))
	expectNoErrors(t, validateWrapped(t, `<field name="model" color="ansi256:200"/>`))
	expectNoErrors(t, validateWrapped(t, `<field name="model" color="#ff8800"/>`))
	expectError(t, validateWrapped(t, `<field name="model" color="chartreuse"/>`), `invalid color "chartreuse"`)
	expectError(t, validateWrapped(t, `<field name="model" color="ansi256:300"/>`), "invalid color")
	expectError(t, validateWrapped(t, `<field name="model" background="#ffff"/>`), "invalid background color")
}

func TestValidateFlexSize(t *testing.T) {
	expectNoErrors(t, validateWrapped(t, `<flex/>`))
	expectNoErrors(t, validateWrapped(t, `<flex size="full"/>`))
	expectNoErrors(t, validateWrapped(t, `<flex size="full-minus-3"/>`))
	expectError(t, validateWrapped(t, `<flex size="full-minus-0"/>`), "invalid flex size")
	expectError(t, validateWrapped(t, `<flex size="wide"/>`), "invalid flex size")
}

func TestValidateColorRuleRequiresWhenAndColor(t *testing.T) {
	expectError(t, validateWrapped(t, `<field name="context-percentage"><color-rule color="red"/></field>`), "requires a when attribute")
	expectError(t, validateWrapped(t, `<field name="context-percentage"><color-rule when="self ge 90"/></field>`), "requires a color attribute")
}

func TestValidateSelfOnlyOnFieldWithSelfMetric(t *testing.T) {
	// context-percentage has a self metric: ok.
	expectNoErrors(t, validateWrapped(t, `<field name="context-percentage" when="self ge 50"/>`))
	// model has no self metric: error.
	expectError(t, validateWrapped(t, `<field name="model" when="self ge 50"/>`), "self is not available")
	// text/span cannot use self.
	expectError(t, validateWrapped(t, `<text when="self ge 1">x</text>`), "self is not available")
	// color-rule self on a self-less field.
	expectError(t, validateWrapped(t, `<field name="model"><color-rule when="self ge 1" color="red"/></field>`), "self is not available")
}

func TestValidateNamedMetricReference(t *testing.T) {
	expectNoErrors(t, validateWrapped(t, `<text when="git-dirty eq true">x</text>`))
	expectError(t, validateWrapped(t, `<text when="no-such-metric gt 1">x</text>`), `unknown metric "no-such-metric"`)
}

func TestValidateInvalidWhenExpression(t *testing.T) {
	expectError(t, validateWrapped(t, `<text when="git-dirty eq">x</text>`), "invalid when expression")
}

func TestValidateToolLevelEnums(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code" color-level="rainbow" output-style="ornate" context-percentage-mode="sideways"><layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`
	diags := validate(t, src)
	expectError(t, diags, "invalid color-level")
	expectError(t, diags, "invalid output-style")
	expectError(t, diags, "invalid context-percentage-mode")
}

func TestValidateActiveUniqueness(t *testing.T) {
	// Duplicate active.
	dup := `<statusloom version="1" tool="claude-code">
		<layout name="a" active="true"><line><field name="model"/></line></layout>
		<layout name="b" active="true"><line><field name="model"/></line></layout>
	</statusloom>`
	expectError(t, validate(t, dup), "multiple active layouts")

	// No active among multiple layouts.
	none := `<statusloom version="1" tool="claude-code">
		<layout name="a"><line><field name="model"/></line></layout>
		<layout name="b"><line><field name="model"/></line></layout>
	</statusloom>`
	expectError(t, validate(t, none), "no active layout")
}

func TestValidateSingleLayoutActiveOmitted(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code"><layout name="only"><line><field name="model"/></line></layout></statusloom>`
	expectNoErrors(t, validate(t, src))
}

func TestValidateDuplicateLayoutName(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code">
		<layout name="same" active="true"><line><field name="model"/></line></layout>
		<layout name="same"><line><field name="model"/></line></layout>
	</statusloom>`
	expectError(t, validate(t, src), `duplicate layout name "same"`)
}

func TestValidateWhenErrorRangePointsIntoAttribute(t *testing.T) {
	// A syntax error mid-expression should carry a range inside the source,
	// not the zero range.
	src := wrap(`<text when="git-dirty eq">x</text>`)
	doc, _ := Parse(src)
	diags := Validate(doc)
	var found bool
	for _, d := range diags {
		if strings.Contains(d.Message, "invalid when expression") {
			found = true
			if d.Range.IsZero() {
				t.Fatalf("when-error range is zero; expected a located range")
			}
			slice := d.Range.Slice(src)
			// The range should fall within the source document.
			if d.Range.End > len(src) {
				t.Fatalf("range out of bounds: %+v", d.Range)
			}
			_ = slice
		}
	}
	if !found {
		t.Fatalf("no when-expression error found: %v", diags)
	}
}
