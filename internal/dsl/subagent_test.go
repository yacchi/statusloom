package dsl

import (
	"reflect"
	"strings"
	"testing"
)

// subagentVariantDoc places a <subagent> region inside each <variant> of a
// responsive layout, so every width breakpoint carries its own subagent
// design. It is the primary round-trip fixture for the variant-scoped case.
const subagentVariantDoc = `<statusloom version="1" tool="claude-code">
  <layout name="Default" active="true">
    <responsive>
      <variant>
        <line>
          <field name="model"/>
          <flex/>
          <field name="context-percentage"/>
        </line>
        <subagent>
          <line>
            <field name="task-description"/>
            <field name="task-model"/>
            <flex/>
            <field name="task-tokens"/>
          </line>
        </subagent>
      </variant>
      <variant>
        <line>
          <field name="model"/>
          <flex/>
        </line>
        <subagent>
          <line>
            <field name="task-model"/>
            <flex/>
            <field name="task-context-percent"/>
          </line>
        </subagent>
      </variant>
    </responsive>
  </layout>
</statusloom>
`

// subagentLayoutDoc places a <subagent> region directly under a
// (responsive-free) <layout>, below its lines.
const subagentLayoutDoc = `<statusloom version="1" tool="claude-code">
  <layout name="Default" active="true">
    <line>
      <field name="model"/>
      <flex/>
      <field name="context-percentage"/>
    </line>
    <subagent>
      <!-- one row per running task -->
      <line>
        <field name="task-description"/>
        <flex/>
        <field name="task-tokens"/>
      </line>
    </subagent>
  </layout>
</statusloom>
`

func TestParseSubagent_LayoutDirect(t *testing.T) {
	doc := parseClean(t, subagentLayoutDoc)
	layout := doc.Root.Layouts[0]
	if len(layout.Children) != 1 {
		t.Fatalf("layout children = %d, want 1 (line only; subagent is a dedicated field)", len(layout.Children))
	}
	if _, ok := layout.Children[0].(*LineNode); !ok {
		t.Fatalf("child 0 is %T, want *LineNode", layout.Children[0])
	}
	sa := layout.Subagent
	if sa == nil {
		t.Fatal("layout.Subagent is nil, want a <subagent> region")
	}
	if sa.Line == nil {
		t.Fatal("subagent.Line is nil, want one <line>")
	}
	if len(sa.Comments) != 1 || sa.Comments[0].Text != " one row per running task " {
		t.Fatalf("subagent comments = %#v", sa.Comments)
	}
	// The subagent line holds the task-* fields.
	if _, ok := sa.Line.Children[0].(*FieldNode); !ok {
		t.Fatalf("subagent line child 0 is %T, want *FieldNode", sa.Line.Children[0])
	}
}

func TestParseSubagent_InsideVariant(t *testing.T) {
	doc := parseClean(t, subagentVariantDoc)
	layout := doc.Root.Layouts[0]
	rn, ok := layout.Children[0].(*ResponsiveNode)
	if !ok {
		t.Fatalf("child 0 is %T, want *ResponsiveNode", layout.Children[0])
	}
	if len(rn.Variants) != 2 {
		t.Fatalf("variants = %d, want 2", len(rn.Variants))
	}
	for i, vr := range rn.Variants {
		if vr.Subagent == nil {
			t.Fatalf("variant %d has no subagent", i)
		}
		if vr.Subagent.Line == nil {
			t.Fatalf("variant %d subagent has no line", i)
		}
	}
	// The layout itself has no direct subagent (only the variants do).
	if layout.Subagent != nil {
		t.Fatalf("layout.Subagent = %#v, want nil", layout.Subagent)
	}
}

func TestParseSubagent_Errors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "two subagents in layout",
			src:  `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><line><field name="model"/></line><subagent><line><field name="task-model"/></line></subagent><subagent><line><field name="task-model"/></line></subagent></layout></statusloom>`,
			want: "at most one <subagent> is allowed in a <layout>",
		},
		{
			name: "two subagents in variant",
			src:  `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><responsive><variant><line><field name="model"/></line><subagent><line><field name="task-model"/></line></subagent><subagent><line><field name="task-model"/></line></subagent></variant></responsive></layout></statusloom>`,
			want: "at most one <subagent> is allowed in a <variant>",
		},
		{
			name: "two lines in subagent",
			src:  `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><subagent><line><field name="task-model"/></line><line><field name="task-tokens"/></line></subagent></layout></statusloom>`,
			want: "<subagent> allows at most one <line>",
		},
		{
			name: "responsive inside subagent",
			src:  `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><subagent><responsive><variant><line><field name="task-model"/></line></variant></responsive></subagent></layout></statusloom>`,
			want: "unknown element <responsive> inside <subagent>",
		},
		{
			name: "variant inside subagent",
			src:  `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><subagent><variant><line><field name="task-model"/></line></variant></subagent></layout></statusloom>`,
			want: "unknown element <variant> inside <subagent>",
		},
		{
			name: "attribute on subagent",
			src:  `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><subagent color="red"><line><field name="task-model"/></line></subagent></layout></statusloom>`,
			want: `unknown attribute "color" on <subagent>`,
		},
		{
			name: "subagent inside line",
			src:  `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><line><subagent><line><field name="task-model"/></line></subagent></line></layout></statusloom>`,
			want: "unknown element <subagent>",
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

func TestValidateSubagent_HappyPathLayoutDirect(t *testing.T) {
	expectNoErrors(t, validate(t, subagentLayoutDoc))
}

func TestValidateSubagent_HappyPathVariant(t *testing.T) {
	expectNoErrors(t, validate(t, subagentVariantDoc))
}

func TestValidateSubagent_EmptyRegionRequiresLine(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><line><field name="model"/></line><subagent></subagent></layout></statusloom>`
	expectError(t, validate(t, src), "<subagent> requires exactly one <line>")
}

func TestValidateSubagent_FieldScope(t *testing.T) {
	// A task-* field in a main line is rejected.
	mainTask := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><line><field name="task-model"/></line></layout></statusloom>`
	expectError(t, validate(t, mainTask), `field "task-model" cannot be used outside <subagent>`)

	// A non-subagent field inside a <subagent> is rejected.
	subMain := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><subagent><line><field name="model"/></line></subagent></layout></statusloom>`
	expectError(t, validate(t, subMain), `field "model" can only be used inside <subagent>`)

	// A task-* field inside a variant main line is rejected (scope applies at
	// every depth, including responsive variants).
	variantTask := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><responsive><variant><line><field name="task-model"/></line></variant></responsive></layout></statusloom>`
	expectError(t, validate(t, variantTask), `field "task-model" cannot be used outside <subagent>`)

	// A task-* field nested inside a <span> in a subagent line is accepted.
	nested := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><subagent><line><span><field name="task-model"/></span></line></subagent></layout></statusloom>`
	expectNoErrors(t, validate(t, nested))
}

func TestValidateSubagent_SelfAndNamedMetrics(t *testing.T) {
	// A task field's own self metric resolves inside a subagent line.
	self := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><subagent><line>` +
		`<field name="task-context-percent" when="self ge 50"><color-rule when="self ge 80" color="red"/></field>` +
		`</line></subagent></layout></statusloom>`
	expectNoErrors(t, validate(t, self))

	// A named subagent metric (merged into the claude-code catalog) resolves
	// in a when=, and optional= to a task field resolves too.
	named := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><subagent><line>` +
		`<field name="task-description" optional="task-tokens" when="task-token-count gt 0"/>` +
		`</line></subagent></layout></statusloom>`
	expectNoErrors(t, validate(t, named))

	// The tool-agnostic width metric still resolves inside a subagent line.
	width := `<statusloom version="1" tool="claude-code"><layout name="a" active="true"><subagent><line>` +
		`<field name="task-model" when="width ge 64"/>` +
		`</line></subagent></layout></statusloom>`
	expectNoErrors(t, validate(t, width))
}

// TestValidateSubagent_LegacyToolUnaffected confirms the field-scope rule is
// NOT enforced for the legacy subagent-only tool (all its fields are
// Category="subagent" with no <subagent> wrapper), so task-* fields remain
// valid at top level there.
func TestValidateSubagent_LegacyToolUnaffected(t *testing.T) {
	legacy := `<statusloom version="1" tool="claude-code-subagent"><layout name="a" active="true"><line>` +
		`<field name="task-description"/><field name="task-tokens"/>` +
		`</line></layout></statusloom>`
	expectNoErrors(t, validate(t, legacy))
}

func TestSerializeSubagent_RoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"layout-direct", subagentLayoutDoc},
		{"variant", subagentVariantDoc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc1 := parseClean(t, tc.src)
			out1 := Serialize(doc1)
			if !strings.Contains(out1, "<subagent>") || !strings.Contains(out1, "</subagent>") {
				t.Fatalf("serialized output missing <subagent> tags:\n%s", out1)
			}

			doc2 := parseClean(t, out1)
			out2 := Serialize(doc2)
			if out1 != out2 {
				t.Errorf("serializer not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out1, out2)
			}

			n1 := normalizeForCompare(parseClean(t, tc.src))
			n2 := normalizeForCompare(parseClean(t, out1))
			if !reflect.DeepEqual(n1, n2) {
				t.Errorf("round-trip AST mismatch:\n%#v\n\n%#v", n1.Root, n2.Root)
			}
		})
	}
}

func TestSerializeMinimalSubagent_CleanIsByteIdentical(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"layout-direct", subagentLayoutDoc},
		{"variant", subagentVariantDoc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := parseClean(t, tc.src)
			out := SerializeMinimal(doc)
			if out != tc.src {
				t.Errorf("clean subagent document not byte-identical.\n--- got ---\n%q\n--- want ---\n%q", out, tc.src)
			}
		})
	}
}

func TestSerializeMinimalSubagent_DirtyFieldReconstructs(t *testing.T) {
	doc := parseClean(t, subagentLayoutDoc)
	sa := doc.Root.Layouts[0].Subagent
	// Edit a field inside the subagent line and mark it dirty.
	fld := sa.Line.Children[0].(*FieldNode)
	fld.Common.Style.Color = "green"
	fld.Meta.Dirty = true

	out := SerializeMinimal(doc)
	if !strings.Contains(out, `color="green"`) {
		t.Errorf("dirty subagent field's new color missing:\n%s", out)
	}
	// The untouched main line and the subagent comment are still preserved.
	if !strings.Contains(out, "<!-- one row per running task -->") {
		t.Errorf("untouched subagent comment not preserved:\n%s", out)
	}
	if !strings.Contains(out, `<field name="context-percentage"/>`) {
		t.Errorf("untouched main line not preserved:\n%s", out)
	}
	if _, diags := Parse(out); HasErrors(diags) {
		t.Errorf("minimal output not well-formed: %v\n%s", diags, out)
	}
}

// TestSerializeMinimalSubagent_CleanSubtreeReused verifies that when a sibling
// (the main line) is edited, the untouched <subagent> subtree is reused
// verbatim rather than reformatted.
func TestSerializeMinimalSubagent_CleanSubtreeReused(t *testing.T) {
	doc := parseClean(t, subagentLayoutDoc)
	// Dirty a field in the main line, leaving the subagent region clean.
	line := doc.Root.Layouts[0].Children[0].(*LineNode)
	fld := line.Children[0].(*FieldNode)
	fld.Common.Style.Color = "cyan"
	fld.Meta.Dirty = true

	out := SerializeMinimal(doc)
	if !strings.Contains(out, `color="cyan"`) {
		t.Errorf("dirty main field's new color missing:\n%s", out)
	}
	// The subagent subtree (its verbatim slice, comment included) survives.
	if !strings.Contains(out, "<!-- one row per running task -->") {
		t.Errorf("clean subagent subtree not reused verbatim:\n%s", out)
	}
	if _, diags := Parse(out); HasErrors(diags) {
		t.Errorf("minimal output not well-formed: %v\n%s", diags, out)
	}
}

// TestFieldByName_MergedSubagentFields confirms the merged claude-code catalog
// resolves task-* fields and their self metrics (the field-scope check and the
// renderer both rely on this).
func TestFieldByName_MergedSubagentFields(t *testing.T) {
	f, ok := FieldByName("claude-code", "task-model")
	if !ok {
		t.Fatal("task-model not resolvable on claude-code")
	}
	if f.Category != "subagent" {
		t.Errorf("task-model Category = %q, want subagent", f.Category)
	}
	if _, ok := MetricByName("claude-code", "task-token-count"); !ok {
		t.Error("task-token-count metric not resolvable on claude-code")
	}
	// The merge must not duplicate the shared width metric.
	widthCount := 0
	for _, m := range Metrics("claude-code") {
		if m.Name == "width" {
			widthCount++
		}
	}
	if widthCount != 1 {
		t.Errorf("width metric appears %d times in merged catalog, want 1", widthCount)
	}
	// task-* fields must also appear in the palette listing.
	seen := false
	for _, ff := range Fields("claude-code") {
		if ff.Name == "task-description" {
			seen = true
			break
		}
	}
	if !seen {
		t.Error("Fields(claude-code) does not include task-description")
	}
}
