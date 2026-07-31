package dsl

import (
	"reflect"
	"strings"
	"testing"
)

// responsiveDoc is a broad document exercising a fixed line, a <responsive>
// with two variants, comments directly under the layout / responsive / variant,
// and a symbolic when — everything a round trip must preserve.
const responsiveDoc = `<statusloom version="1" tool="claude-code">
  <layout name="default" active="true">
    <line>
      <field name="model"/>
    </line>
    <!-- adaptive block -->
    <responsive>
      <!-- widest first -->
      <variant>
        <line>
          <field name="context-percentage" prefix="ctx "/>
          <text role="separator" padding="1">|</text>
          <field name="session-cost" prefix="$"/>
        </line>
      </variant>
      <variant>
        <!-- two-row fallback -->
        <line>
          <field name="context-percentage" prefix="ctx "/>
        </line>
        <line>
          <field name="session-cost" prefix="$"/>
        </line>
      </variant>
    </responsive>
  </layout>
</statusloom>
`

func TestParseResponsive_Structure(t *testing.T) {
	doc := parseClean(t, responsiveDoc)
	layout := doc.Root.Layouts[0]
	if len(layout.Children) != 2 {
		t.Fatalf("layout children = %d, want 2", len(layout.Children))
	}
	if _, ok := layout.Children[0].(*LineNode); !ok {
		t.Fatalf("child 0 is %T, want *LineNode", layout.Children[0])
	}
	rn, ok := layout.Children[1].(*ResponsiveNode)
	if !ok {
		t.Fatalf("child 1 is %T, want *ResponsiveNode", layout.Children[1])
	}
	if len(rn.Variants) != 2 {
		t.Fatalf("variants = %d, want 2", len(rn.Variants))
	}
	if len(rn.Comments) != 1 || rn.Comments[0].Text != " widest first " {
		t.Fatalf("responsive comments = %#v", rn.Comments)
	}
	if len(rn.Variants[0].Lines) != 1 {
		t.Fatalf("variant 0 lines = %d, want 1", len(rn.Variants[0].Lines))
	}
	if len(rn.Variants[1].Lines) != 2 {
		t.Fatalf("variant 1 lines = %d, want 2", len(rn.Variants[1].Lines))
	}
	if len(rn.Variants[1].Comments) != 1 || rn.Variants[1].Comments[0].Text != " two-row fallback " {
		t.Fatalf("variant 1 comments = %#v", rn.Variants[1].Comments)
	}
	// Layout-level comment is still collected on the layout, not the responsive.
	if len(layout.Comments) != 1 || layout.Comments[0].Text != " adaptive block " {
		t.Fatalf("layout comments = %#v", layout.Comments)
	}
}

func TestValidateResponsive_HappyPath(t *testing.T) {
	expectNoErrors(t, validate(t, responsiveDoc))
}

func TestValidateResponsive_EmptyContainerAndVariant(t *testing.T) {
	// <responsive> with no variant.
	diags := validate(t, `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><line><field name="model"/></line><responsive></responsive></layout></statusloom>`)
	expectError(t, diags, "<responsive> requires at least one <variant>")

	// <variant> with no line.
	diags = validate(t, `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><responsive><variant></variant></responsive></layout></statusloom>`)
	expectError(t, diags, "<variant> requires at least one <line>")
}

func TestValidateResponsive_LinesInVariantAreValidated(t *testing.T) {
	// An unknown field inside a variant's line must still be reported.
	diags := validate(t, `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><responsive><variant><line><field name="nope"/></line></variant></responsive></layout></statusloom>`)
	expectError(t, diags, `unknown field "nope"`)
}

func TestValidateResponsive_ActiveRuleUnchanged(t *testing.T) {
	// Two layouts, none active: still an error even when one has a responsive.
	diags := validate(t, `<statusloom version="1" tool="claude-code"><layout name="a"><responsive><variant><line><field name="model"/></line></variant></responsive></layout><layout name="b"><line><field name="model"/></line></layout></statusloom>`)
	expectError(t, diags, "no active layout")
}

func TestParseResponsive_PlacementViolations(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "inside line",
			src:  wrap(`<responsive><variant><line><field name="model"/></line></variant></responsive>`),
			want: "unknown element <responsive>",
		},
		{
			name: "at root",
			src:  `<statusloom version="1" tool="claude-code"><responsive><variant><line><field name="model"/></line></variant></responsive></statusloom>`,
			want: "unknown element <responsive> inside <statusloom>",
		},
		{
			name: "nested inside variant",
			src:  `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><responsive><variant><responsive><variant><line><field name="model"/></line></variant></responsive></variant></responsive></layout></statusloom>`,
			want: "unknown element <responsive> inside <variant>",
		},
		{
			name: "nested directly inside responsive",
			src:  `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><responsive><responsive><variant><line><field name="model"/></line></variant></responsive></responsive></layout></statusloom>`,
			want: "unknown element <responsive> inside <responsive>",
		},
		{
			name: "variant outside responsive",
			src:  `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><variant><line><field name="model"/></line></variant></layout></statusloom>`,
			want: "unknown element <variant> inside <layout>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := mustParse(t, tc.src)
			if !hasErrorContaining(diags, tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, diags)
			}
		})
	}
}

func TestSerializeResponsive_RoundTrip(t *testing.T) {
	doc1 := parseClean(t, responsiveDoc)
	out1 := Serialize(doc1)

	doc2 := parseClean(t, out1)
	out2 := Serialize(doc2)
	if out1 != out2 {
		t.Errorf("serializer not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out1, out2)
	}

	// Comments must survive canonical serialization.
	for _, want := range []string{"<!-- adaptive block -->", "<!-- widest first -->", "<!-- two-row fallback -->", "<responsive>", "<variant>", "</variant>", "</responsive>"} {
		if !strings.Contains(out1, want) {
			t.Errorf("serialized output missing %q:\n%s", want, out1)
		}
	}

	n1 := normalizeForCompare(parseClean(t, responsiveDoc))
	n2 := normalizeForCompare(parseClean(t, out1))
	if !reflect.DeepEqual(n1, n2) {
		t.Errorf("round-trip AST mismatch:\n%#v\n\n%#v", n1.Root, n2.Root)
	}
}

func TestSerializeMinimalResponsive_CleanIsByteIdentical(t *testing.T) {
	doc := parseClean(t, responsiveDoc)
	out := SerializeMinimal(doc)
	if out != responsiveDoc {
		t.Errorf("clean responsive document not byte-identical.\n--- got ---\n%q\n--- want ---\n%q", out, responsiveDoc)
	}
}

func TestSerializeMinimalResponsive_DirtyLineReconstructs(t *testing.T) {
	doc := parseClean(t, responsiveDoc)
	rn := doc.Root.Layouts[0].Children[1].(*ResponsiveNode)
	// Edit a field inside the first variant's line and mark it dirty.
	fld := rn.Variants[0].Lines[0].Children[0].(*FieldNode)
	fld.Common.Style.Color = "green"
	fld.Meta.Dirty = true

	out := SerializeMinimal(doc)
	if !strings.Contains(out, `color="green"`) {
		t.Errorf("dirty field's new color missing:\n%s", out)
	}
	// The untouched second variant is reused verbatim (comment included).
	if !strings.Contains(out, "<!-- two-row fallback -->") {
		t.Errorf("untouched variant comment not preserved:\n%s", out)
	}
	if _, diags := Parse(out); HasErrors(diags) {
		t.Errorf("minimal output not well-formed: %v\n%s", diags, out)
	}
}

func TestCanonicalizeResponsive_NormalizesVariantWhen(t *testing.T) {
	// A symbolic when inside a variant line must be normalized to word form.
	src := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><responsive><variant><line><field name="context-percentage" when="context-percent &gt;= 80"/></line></variant></responsive></layout></statusloom>`
	out := Canonicalize(parseClean(t, src))
	if !strings.Contains(out, `when="context-percent ge 80"`) {
		t.Errorf("variant-line when not normalized to word form:\n%s", out)
	}
}

// A <variant>'s own `when` (its candidacy gate) must survive parse, validate,
// and a full round trip, and must be normalized to word form by Canonicalize
// exactly like a node's when.
func TestVariantWhen_ParseValidateRoundTrip(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code">
  <layout name="a" active="true">
    <responsive>
      <variant when="account-type eq &#34;claude_team&#34;">
        <line>
          <field name="account-seat"/>
        </line>
      </variant>
      <variant>
        <line>
          <field name="model"/>
        </line>
      </variant>
    </responsive>
  </layout>
</statusloom>
`
	doc := parseClean(t, src)
	r := doc.Root.Layouts[0].Children[0].(*ResponsiveNode)
	if got := r.Variants[0].When; got != `account-type eq "claude_team"` {
		t.Errorf("variant[0].When = %q", got)
	}
	if got := r.Variants[1].When; got != "" {
		t.Errorf("variant[1].When = %q, want empty (unconditional)", got)
	}
	if diags := Validate(doc); HasErrors(diags) {
		t.Errorf("unexpected validation errors: %v", diags)
	}
	out := Serialize(doc)
	if !strings.Contains(out, `<variant when="account-type eq &quot;claude_team&quot;">`) {
		t.Errorf("serialized output lost the variant when:\n%s", out)
	}
	// Re-parsing the serialized form yields the same gate (idempotent).
	again := parseClean(t, out)
	r2 := again.Root.Layouts[0].Children[0].(*ResponsiveNode)
	if r2.Variants[0].When != r.Variants[0].When {
		t.Errorf("round trip changed the gate: %q -> %q", r.Variants[0].When, r2.Variants[0].When)
	}
}

func TestVariantWhen_CanonicalizeNormalizesToWordForm(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><responsive><variant when="width &gt;= 80"><line><field name="model"/></line></variant></responsive></layout></statusloom>`
	out := Canonicalize(parseClean(t, src))
	if !strings.Contains(out, `<variant when="width ge 80">`) {
		t.Errorf("variant when not normalized to word form:\n%s", out)
	}
}

// A <variant> has no field of its own, so `self` cannot resolve in its gate,
// and an unknown metric / a syntax error are errors just as elsewhere.
func TestVariantWhen_InvalidExpressions(t *testing.T) {
	cases := []struct {
		name string
		when string
		want string
	}{
		{"self is unavailable", "self ge 80", "self is not available here"},
		{"unknown metric", "no-such-metric eq 1", `unknown metric "no-such-metric"`},
		{"syntax error", "account-type eq", "invalid when expression"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><responsive><variant when="` + tc.when + `"><line><field name="model"/></line></variant></responsive></layout></statusloom>`
			doc, diags := Parse(src)
			if HasErrors(diags) {
				t.Fatalf("parse errors: %v", diags)
			}
			diags = Validate(doc)
			if !HasErrors(diags) {
				t.Fatalf("expected a validation error for when=%q", tc.when)
			}
			var joined strings.Builder
			for _, d := range diags {
				joined.WriteString(d.Message)
				joined.WriteString("\n")
			}
			if !strings.Contains(joined.String(), tc.want) {
				t.Errorf("diagnostics %q do not mention %q", joined.String(), tc.want)
			}
		})
	}
}

func TestVariantWhen_UnknownAttributeStillRejected(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><responsive><variant nope="1"><line><field name="model"/></line></variant></responsive></layout></statusloom>`
	_, diags := Parse(src)
	if !HasErrors(diags) {
		t.Fatal("expected an unknown-attribute error on <variant>")
	}
}
