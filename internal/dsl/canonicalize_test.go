package dsl

import (
	"strings"
	"testing"
)

// TestCanonicalize_NormalizesWhen verifies fmt's canonical form rewrites every
// when / color-rule when to word form while otherwise matching Serialize.
func TestCanonicalize_NormalizesWhen(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code">
  <layout name="default" active="true">
    <line>
      <field name="context-percentage" when="context-percent &gt;= 80">
        <color-rule when="self &gt;= 90" color="red"/>
      </field>
      <text when="git-dirty == true">*</text>
    </line>
  </layout>
</statusloom>
`
	doc := parseClean(t, src)
	out := Canonicalize(doc)

	if !strings.Contains(out, `when="context-percent ge 80"`) {
		t.Errorf("field when not normalized to word form:\n%s", out)
	}
	if !strings.Contains(out, `when="self ge 90"`) {
		t.Errorf("color-rule when not normalized to word form:\n%s", out)
	}
	if !strings.Contains(out, `when="git-dirty eq true"`) {
		t.Errorf("text when not normalized to word form:\n%s", out)
	}
	if strings.Contains(out, "&gt;") || strings.Contains(out, "==") {
		t.Errorf("symbolic operators still present after canonicalize:\n%s", out)
	}
	if _, diags := Parse(out); HasErrors(diags) {
		t.Errorf("canonical output not well-formed: %v\n%s", diags, out)
	}
}

// TestCanonicalize_KeepsUnparseableWhen leaves a when that does not parse
// untouched (fmt reports the diagnostic separately and refuses to write).
func TestCanonicalize_KeepsUnparseableWhen(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><line><text when="a and">x</text></line></layout></statusloom>`
	doc := parseClean(t, src)
	out := Canonicalize(doc)
	if !strings.Contains(out, `when="a and"`) {
		t.Errorf("unparseable when should be left verbatim:\n%s", out)
	}
}

// TestCanonicalize_MatchesSerializeWithoutWhen is a sanity check that
// Canonicalize equals Serialize when there are no when expressions.
func TestCanonicalize_MatchesSerializeWithoutWhen(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><line><field name="model"/></line></layout></statusloom>`
	doc := parseClean(t, src)
	if Canonicalize(doc) != Serialize(parseClean(t, src)) {
		t.Error("Canonicalize should equal Serialize when there are no when expressions")
	}
}

func TestCanonicalize_Nil(t *testing.T) {
	if Canonicalize(nil) != "" {
		t.Error("Canonicalize(nil) should be empty")
	}
}
