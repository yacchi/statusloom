package dsl

import (
	"reflect"
	"testing"
)

// refsDoc parses src and returns the document, ignoring diagnostics: refs are
// read from the AST, so documents with validation errors must still walk.
func refsDoc(t *testing.T, src string) *Document {
	t.Helper()
	doc, _ := Parse(src)
	if doc == nil {
		t.Fatalf("Parse returned nil document for %s", src)
	}
	return doc
}

// refsLayout wraps body (layout children) in one active claude-code layout.
func refsLayout(body string) string {
	return `<statusloom version="1" tool="claude-code"><layout name="default" active="true">` +
		body + `</layout></statusloom>`
}

func TestReferencedSources(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "all locations across layouts variants spans and subagents",
			src: `<statusloom version="1" tool="claude-code">
  <layout name="a" active="true">
    <line><field name="source" source="zeta"/></line>
  </layout>
  <layout name="b">
    <line><span optional="source:alpha"><field name="model"/></span></line>
    <responsive>
      <variant when="source-stale:mid"><line><field name="model"/></line></variant>
      <variant>
        <line><field name="model"/></line>
        <subagent><line><field name="task-model"><color-rule when="source-stale:zeta" color="red"/></field></line></subagent>
      </variant>
    </responsive>
  </layout>
</statusloom>`,
			want: []string{"alpha", "mid", "zeta"},
		},
		{
			name: "empty source attribute is not an id",
			src:  refsLayout(`<line><field name="source" source=""/></line>`),
			want: nil,
		},
		{
			name: "source attribute on a non-source field is ignored",
			src:  refsLayout(`<line><field name="model" source="x"/></line>`),
			want: nil,
		},
		{
			name: "optional naming env or an empty source id is ignored",
			src: refsLayout(`<line><span optional="env:AWS_PROFILE"><field name="model"/></span>` +
				`<span optional="source:"><field name="model"/></span></line>`),
			want: nil,
		},
		{
			name: "two source-stale references in one when",
			src:  refsLayout(`<line when="source-stale:a and source-stale:b"><field name="model"/></line>`),
			want: []string{"a", "b"},
		},
		{
			name: "unparsable when is skipped and other references survive",
			src: refsLayout(`<line><field name="model" when="source-stale:a and ("/>` +
				`<field name="source" source="b"/></line>`),
			want: []string{"b"},
		},
		{
			name: "sorted and deduplicated",
			src: refsLayout(`<line><field name="source" source="b"/><field name="source" source="a"/>` +
				`<span optional="source:b"><field name="model"/></span>` +
				`<text when="source-stale:a">x</text></line>`),
			want: []string{"a", "b"},
		},
		{
			name: "self metric is not a source reference",
			src:  refsLayout(`<line><field name="model" when="self gt 0"/></line>`),
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ReferencedSources(refsDoc(t, c.src))
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("ReferencedSources = %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestReferencedSourcesNilDocument(t *testing.T) {
	if got := ReferencedSources(nil); got != nil {
		t.Fatalf("nil doc: got %#v", got)
	}
	if got := ReferencedSources(&Document{}); got != nil {
		t.Fatalf("doc without root: got %#v", got)
	}
}

func TestUsesTranscript(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "transcript field",
			src:  refsLayout(`<line><field name="compaction-count"/></line>`),
			want: true,
		},
		{
			name: "source field whose fallback is a transcript field",
			src:  refsLayout(`<line><field name="source" source="s" fallback="compaction-count"/></line>`),
			want: true,
		},
		{
			name: "optional naming a transcript field",
			src:  refsLayout(`<line><span optional="session-total-tokens"><field name="model"/></span></line>`),
			want: true,
		},
		{
			name: "line when naming a transcript metric",
			src:  refsLayout(`<line when="input-token-speed gt 0"><field name="model"/></line>`),
			want: true,
		},
		{
			name: "color-rule on a text naming a transcript metric",
			src:  refsLayout(`<line><text>x<color-rule when="compaction-auto gt 1" color="red"/></text></line>`),
			want: true,
		},
		{
			name: "transcript field inside a subagent region of a non-active layout",
			src: `<statusloom version="1" tool="claude-code">` +
				`<layout name="a" active="true"><line><field name="model"/></line></layout>` +
				`<layout name="b"><line><field name="model"/></line>` +
				`<subagent><line><field name="compaction-count"/></line></subagent></layout>` +
				`</statusloom>`,
			want: true,
		},
		{
			name: "transcript field in a second responsive variant",
			src: `<statusloom version="1" tool="claude-code"><layout name="default" active="true">` +
				`<responsive>` +
				`<variant><line><field name="model"/></line></variant>` +
				`<variant><line><field name="compaction-count"/></line></variant>` +
				`</responsive></layout></statusloom>`,
			want: true,
		},
		{
			name: "cache-hit-rate is not transcript-derived",
			src:  refsLayout(`<line><field name="cache-hit-rate"/></line>`),
			want: false,
		},
		{
			name: "source field whose fallback is not a transcript field",
			src:  refsLayout(`<line><field name="source" source="s" fallback="model"/></line>`),
			want: false,
		},
		{
			name: "self metric on a non-transcript field",
			src:  refsLayout(`<line><field name="model" when="self gt 0"/></line>`),
			want: false,
		},
		{
			name: "only non-transcript fields and metrics",
			src:  refsLayout(`<line><field name="model"/><field name="context-percentage"/></line>`),
			want: false,
		},
		{
			name: "transcript name under another tool",
			src: `<statusloom version="1" tool="other"><layout name="default" active="true">` +
				`<line><field name="compaction-count"/></line></layout></statusloom>`,
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := UsesTranscript(refsDoc(t, c.src)); got != c.want {
				t.Fatalf("UsesTranscript = %v, want %v", got, c.want)
			}
		})
	}
}

func TestUsesTranscriptNilDocument(t *testing.T) {
	if UsesTranscript(nil) {
		t.Fatal("nil doc reported transcript use")
	}
	if UsesTranscript(&Document{}) {
		t.Fatal("doc without root reported transcript use")
	}
}
