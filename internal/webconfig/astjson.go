package webconfig

// This file implements the JSON wire representation of a dsl.Document AST for
// the /api/dsl/* endpoints, and the node-ID scheme the visual editor uses to
// address AST nodes and map preview segments back to them. The full contract
// is documented in DSL_API.md.
//
// Node IDs are deterministic, position-derived paths (no random component):
//
//	root                 the <statusloom> root
//	git                  the optional <git/> element
//	root.c{k}            the k-th XML comment directly under the root
//	L{i}                 the i-th <layout>
//	L{i}.c{k}            the k-th comment directly under layout i
//	L{i}.{j}             the j-th <line> of layout i (index into layout.Lines)
//	{parent}.{k}         the k-th mixed-content child of a line/span
//	                     (index into Children; spans nest, so paths grow)
//	{owner}.cr{c}        the c-th <color-rule> of a field/span/text owner
//
// The same traversal produces the AST JSON (buildAST) and the dsl.Node ->
// node-ID map used to label preview segments, so an ID in the AST always
// matches the ID a preview segment reports for the same node.

import (
	"fmt"

	"github.com/yacchi/statusloom/internal/dsl"
)

// astRangeJSON is a byte-offset source range on the wire.
type astRangeJSON struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func rangeJSON(r dsl.SourceRange) astRangeJSON {
	return astRangeJSON{Start: r.Start, End: r.End}
}

// diagJSON is one parse/validation diagnostic on the wire.
type diagJSON struct {
	Severity string       `json:"severity"` // "error" | "warning"
	Message  string       `json:"message"`
	Range    astRangeJSON `json:"range"`
}

func toDiagsJSON(diags []dsl.Diagnostic) []diagJSON {
	out := make([]diagJSON, 0, len(diags))
	for _, d := range diags {
		out = append(out, diagJSON{
			Severity: d.Severity.String(),
			Message:  d.Message,
			Range:    rangeJSON(d.Range),
		})
	}
	return out
}

// astBuilder walks a document once, emitting the AST JSON (as ordered
// map[string]any per node) and recording a dsl.Node -> node-ID map so preview
// segments can be labeled with the same IDs.
type astBuilder struct {
	ids map[dsl.Node]string
}

// buildAST converts a parsed document to its JSON representation (a root
// map[string]any) and the dsl.Node -> node-ID map. doc.Root must be non-nil.
func buildAST(doc *dsl.Document) (map[string]any, map[dsl.Node]string) {
	b := &astBuilder{ids: map[dsl.Node]string{}}
	return b.statusloom(doc.Root), b.ids
}

func (b *astBuilder) statusloom(n *dsl.StatusloomNode) map[string]any {
	m := map[string]any{"id": "root", "kind": "statusloom", "range": rangeJSON(n.Meta.SourceRange)}
	if n.Version != "" {
		m["version"] = n.Version
	}
	if n.Tool != "" {
		m["tool"] = n.Tool
	}
	if n.Settings.ColorLevel != "" {
		m["color-level"] = n.Settings.ColorLevel
	}
	if n.Settings.OutputStyle != "" {
		m["output-style"] = n.Settings.OutputStyle
	}
	if n.Settings.CompactThreshold != nil {
		m["compact-threshold"] = *n.Settings.CompactThreshold
	}
	if n.Settings.ContextPercentageMode != "" {
		m["context-percentage-mode"] = n.Settings.ContextPercentageMode
	}
	if n.Settings.ContextReserveTokens != nil {
		m["context-reserve-tokens"] = *n.Settings.ContextReserveTokens
	}
	if n.Git != nil {
		m["git"] = b.git(n.Git)
	}
	layouts := make([]any, 0, len(n.Layouts))
	for i, l := range n.Layouts {
		layouts = append(layouts, b.layout(l, i))
	}
	m["layouts"] = layouts
	if len(n.Comments) > 0 {
		cs := make([]any, 0, len(n.Comments))
		for k, c := range n.Comments {
			cs = append(cs, b.comment(c, fmt.Sprintf("root.c%d", k)))
		}
		m["comments"] = cs
	}
	return m
}

func (b *astBuilder) git(g *dsl.GitSettings) map[string]any {
	m := map[string]any{"id": "git", "kind": "git", "range": rangeJSON(g.Meta.SourceRange)}
	if g.CacheTTLMS != nil {
		m["cache-ttl-ms"] = *g.CacheTTLMS
	}
	if g.TimeoutMS != nil {
		m["timeout-ms"] = *g.TimeoutMS
	}
	if g.IncludeUntracked != nil {
		m["include-untracked"] = *g.IncludeUntracked
	}
	if g.CollectNumstat != nil {
		m["collect-numstat"] = *g.CollectNumstat
	}
	return m
}

func (b *astBuilder) layout(l *dsl.LayoutNode, i int) map[string]any {
	id := fmt.Sprintf("L%d", i)
	m := map[string]any{"id": id, "kind": "layout", "range": rangeJSON(l.Meta.SourceRange)}
	if l.Name != "" {
		m["name"] = l.Name
	}
	if l.Active != nil {
		m["active"] = *l.Active
	}
	lines := make([]any, 0, len(l.Lines))
	for j, ln := range l.Lines {
		lines = append(lines, b.line(ln, fmt.Sprintf("%s.%d", id, j)))
	}
	m["lines"] = lines
	if len(l.Comments) > 0 {
		cs := make([]any, 0, len(l.Comments))
		for k, c := range l.Comments {
			cs = append(cs, b.comment(c, fmt.Sprintf("%s.c%d", id, k)))
		}
		m["comments"] = cs
	}
	return m
}

func (b *astBuilder) line(ln *dsl.LineNode, id string) map[string]any {
	m := map[string]any{"id": id, "kind": "line", "range": rangeJSON(ln.Meta.SourceRange)}
	b.common(m, ln.Common, id)
	m["children"] = b.children(ln.Children, id)
	return m
}

// children emits a mixed-content child list, registering each node's pointer
// against its position-derived ID.
func (b *astBuilder) children(nodes []dsl.Node, parentID string) []any {
	out := make([]any, 0, len(nodes))
	for k, n := range nodes {
		out = append(out, b.node(n, fmt.Sprintf("%s.%d", parentID, k)))
	}
	return out
}

func (b *astBuilder) node(n dsl.Node, id string) map[string]any {
	b.ids[n] = id
	switch t := n.(type) {
	case *dsl.SpanNode:
		m := map[string]any{"id": id, "kind": "span", "range": rangeJSON(t.Meta.SourceRange)}
		b.common(m, t.Common, id)
		m["children"] = b.children(t.Children, id)
		return m
	case *dsl.TextNode:
		m := map[string]any{"id": id, "kind": "text", "range": rangeJSON(t.Meta.SourceRange)}
		if t.Role != "" {
			m["role"] = t.Role
		}
		m["value"] = t.Value
		b.common(m, t.Common, id)
		return m
	case *dsl.FieldNode:
		m := map[string]any{"id": id, "kind": "field", "range": rangeJSON(t.Meta.SourceRange)}
		if t.Name != "" {
			m["name"] = t.Name
		}
		if t.Formatter.Name != "" {
			m["format"] = t.Formatter.Name
		}
		if t.Formatter.Precision != "" {
			m["precision"] = t.Formatter.Precision
		}
		if t.Formatter.Currency != "" {
			m["currency"] = t.Formatter.Currency
		}
		if t.Raw {
			m["raw"] = true
		}
		if t.Hyperlink {
			m["hyperlink"] = true
		}
		b.common(m, t.Common, id)
		return m
	case *dsl.FlexNode:
		m := map[string]any{"id": id, "kind": "flex", "range": rangeJSON(t.Meta.SourceRange)}
		if t.Size != "" {
			m["size"] = t.Size
		}
		return m
	case *dsl.RawTextNode:
		return map[string]any{"id": id, "kind": "raw-text", "range": rangeJSON(t.Meta.SourceRange), "value": t.RenderText}
	case *dsl.CommentNode:
		return b.comment(t, id)
	default:
		return map[string]any{"id": id, "kind": "unknown"}
	}
}

func (b *astBuilder) comment(c *dsl.CommentNode, id string) map[string]any {
	return map[string]any{"id": id, "kind": "comment", "range": rangeJSON(c.Meta.SourceRange), "value": c.Text}
}

// common writes the shared display attributes into m and appends color-rule
// children (addressed as {ownerID}.cr{c}).
func (b *astBuilder) common(m map[string]any, c dsl.CommonAttributes, ownerID string) {
	s := c.Style
	if s.Color != "" {
		m["color"] = s.Color
	}
	if s.Background != "" {
		m["background"] = s.Background
	}
	putBool(m, "bold", s.Bold)
	putBool(m, "dim", s.Dim)
	putBool(m, "italic", s.Italic)
	putBool(m, "underline", s.Underline)
	putBool(m, "strikethrough", s.Strikethrough)

	pl, pr := c.Box.PaddingLeft, c.Box.PaddingRight
	if pl != nil && pr != nil && *pl == *pr {
		m["padding"] = *pl
	} else {
		if pl != nil {
			m["padding-left"] = *pl
		}
		if pr != nil {
			m["padding-right"] = *pr
		}
	}
	if c.Prefix != "" {
		m["prefix"] = c.Prefix
	}
	if c.Suffix != "" {
		m["suffix"] = c.Suffix
	}
	if c.Optional != "" {
		m["optional"] = c.Optional
	}
	if c.When != "" {
		m["when"] = c.When
	}
	if len(c.ColorRules) > 0 {
		rules := make([]any, 0, len(c.ColorRules))
		for cIdx, r := range c.ColorRules {
			rm := map[string]any{"id": fmt.Sprintf("%s.cr%d", ownerID, cIdx), "kind": "color-rule", "range": rangeJSON(r.Meta.SourceRange)}
			if r.When != "" {
				rm["when"] = r.When
			}
			if r.Color != "" {
				rm["color"] = r.Color
			}
			rules = append(rules, rm)
		}
		m["colorRules"] = rules
	}
}

func putBool(m map[string]any, key string, v *bool) {
	if v != nil {
		m[key] = *v
	}
}

// --- JSON -> dsl AST (POST /api/dsl/serialize) ----------------------------

// astNodeJSON decodes any AST node from the wire. Every possible attribute is
// present; pointers distinguish "absent" from an explicit zero/false. The Kind
// field selects how the node is converted back to a dsl node.
type astNodeJSON struct {
	ID    string       `json:"id"`
	Kind  string       `json:"kind"`
	Range astRangeJSON `json:"range"`
	// Dirty marks a node the visual editor changed; the minimal-diff
	// serializer regenerates it (and forces its ancestors to reconstruct)
	// while reusing every unchanged node's source slice verbatim. Absent =
	// false. Only consulted when the serialize request carries a baseSource.
	Dirty bool `json:"dirty"`

	// statusloom / tool settings
	Version               string        `json:"version"`
	Tool                  string        `json:"tool"`
	ColorLevel            string        `json:"color-level"`
	OutputStyle           string        `json:"output-style"`
	CompactThreshold      *int          `json:"compact-threshold"`
	ContextPercentageMode string        `json:"context-percentage-mode"`
	ContextReserveTokens  *int          `json:"context-reserve-tokens"`
	Git                   *astNodeJSON  `json:"git"`
	Layouts               []astNodeJSON `json:"layouts"`
	Comments              []astNodeJSON `json:"comments"`

	// git
	CacheTTLMs       *int  `json:"cache-ttl-ms"`
	TimeoutMs        *int  `json:"timeout-ms"`
	IncludeUntracked *bool `json:"include-untracked"`
	CollectNumstat   *bool `json:"collect-numstat"`

	// layout
	Name   string        `json:"name"` // also field name
	Active *bool         `json:"active"`
	Lines  []astNodeJSON `json:"lines"`

	// common display attributes
	Color         string        `json:"color"`
	Background    string        `json:"background"`
	Bold          *bool         `json:"bold"`
	Dim           *bool         `json:"dim"`
	Italic        *bool         `json:"italic"`
	Underline     *bool         `json:"underline"`
	Strikethrough *bool         `json:"strikethrough"`
	Padding       *int          `json:"padding"`
	PaddingLeft   *int          `json:"padding-left"`
	PaddingRight  *int          `json:"padding-right"`
	Prefix        string        `json:"prefix"`
	Suffix        string        `json:"suffix"`
	Optional      string        `json:"optional"`
	When          string        `json:"when"` // also color-rule when
	ColorRules    []astNodeJSON `json:"colorRules"`

	// text / raw-text / comment
	Role  string `json:"role"`
	Value string `json:"value"`

	// field
	Format    string `json:"format"`
	Precision string `json:"precision"`
	Currency  string `json:"currency"`
	Raw       *bool  `json:"raw"`
	Hyperlink *bool  `json:"hyperlink"`

	// flex
	Size string `json:"size"`

	// color-rule reuses the Color ("color") and When ("when") fields above.

	// children (line/span)
	Children []astNodeJSON `json:"children"`
}

// metaJSON builds a NodeMeta from a decoded node, carrying both its source
// range and its dirty flag (for the minimal-diff serializer).
func metaJSON(j astNodeJSON) dsl.NodeMeta {
	return dsl.NodeMeta{
		SourceRange: dsl.SourceRange{Start: j.Range.Start, End: j.Range.End},
		Dirty:       j.Dirty,
	}
}

// jsonToDocument rebuilds a dsl.Document from a decoded AST root so it can be
// serialized. It does not require valid source ranges; missing ones default to
// zero, which the canonical serializer handles via a stable sort.
func jsonToDocument(root astNodeJSON) (*dsl.Document, error) {
	if root.Kind != "statusloom" {
		return nil, fmt.Errorf("root AST node kind must be %q, got %q", "statusloom", root.Kind)
	}
	sn := &dsl.StatusloomNode{
		Meta:    metaJSON(root),
		Version: root.Version,
		Tool:    root.Tool,
	}
	sn.Settings.ColorLevel = root.ColorLevel
	sn.Settings.OutputStyle = root.OutputStyle
	sn.Settings.CompactThreshold = root.CompactThreshold
	sn.Settings.ContextPercentageMode = root.ContextPercentageMode
	sn.Settings.ContextReserveTokens = root.ContextReserveTokens
	if root.Git != nil {
		g := root.Git
		sn.Git = &dsl.GitSettings{
			Meta:             metaJSON(*g),
			CacheTTLMS:       g.CacheTTLMs,
			TimeoutMS:        g.TimeoutMs,
			IncludeUntracked: g.IncludeUntracked,
			CollectNumstat:   g.CollectNumstat,
		}
	}
	for _, lj := range root.Layouts {
		sn.Layouts = append(sn.Layouts, jsonToLayout(lj))
	}
	for i := range root.Comments {
		sn.Comments = append(sn.Comments, jsonToComment(root.Comments[i]))
	}
	return &dsl.Document{Root: sn}, nil
}

func jsonToLayout(lj astNodeJSON) *dsl.LayoutNode {
	ln := &dsl.LayoutNode{Meta: metaJSON(lj), Name: lj.Name, Active: lj.Active}
	for _, linej := range lj.Lines {
		ln.Lines = append(ln.Lines, jsonToLine(linej))
	}
	for i := range lj.Comments {
		ln.Comments = append(ln.Comments, jsonToComment(lj.Comments[i]))
	}
	return ln
}

func jsonToLine(j astNodeJSON) *dsl.LineNode {
	return &dsl.LineNode{
		Meta:     metaJSON(j),
		Common:   jsonToCommon(j),
		Children: jsonToChildren(j.Children),
	}
}

func jsonToChildren(js []astNodeJSON) []dsl.Node {
	var out []dsl.Node
	for i := range js {
		if n := jsonToNode(js[i]); n != nil {
			out = append(out, n)
		}
	}
	return out
}

func jsonToNode(j astNodeJSON) dsl.Node {
	switch j.Kind {
	case "span":
		return &dsl.SpanNode{Meta: metaJSON(j), Common: jsonToCommon(j), Children: jsonToChildren(j.Children)}
	case "text":
		return &dsl.TextNode{Meta: metaJSON(j), Value: j.Value, Role: j.Role, Common: jsonToCommon(j)}
	case "field":
		n := &dsl.FieldNode{
			Meta:      metaJSON(j),
			Name:      j.Name,
			Formatter: dsl.FormatterConfig{Name: j.Format, Precision: j.Precision, Currency: j.Currency},
			Common:    jsonToCommon(j),
		}
		if j.Raw != nil {
			n.Raw = *j.Raw
		}
		if j.Hyperlink != nil {
			n.Hyperlink = *j.Hyperlink
		}
		return n
	case "flex":
		return &dsl.FlexNode{Meta: metaJSON(j), Size: j.Size}
	case "raw-text":
		return &dsl.RawTextNode{Meta: metaJSON(j), RawValue: j.Value, RenderText: j.Value}
	case "comment":
		return jsonToComment(j)
	default:
		return nil
	}
}

func jsonToComment(j astNodeJSON) *dsl.CommentNode {
	return &dsl.CommentNode{Meta: metaJSON(j), Text: j.Value}
}

func jsonToCommon(j astNodeJSON) dsl.CommonAttributes {
	c := dsl.CommonAttributes{
		Style: dsl.Style{
			Color:         j.Color,
			Background:    j.Background,
			Bold:          j.Bold,
			Dim:           j.Dim,
			Italic:        j.Italic,
			Underline:     j.Underline,
			Strikethrough: j.Strikethrough,
		},
		Prefix:   j.Prefix,
		Suffix:   j.Suffix,
		Optional: j.Optional,
		When:     j.When,
	}
	if j.Padding != nil {
		c.Box.PaddingLeft = j.Padding
		c.Box.PaddingRight = j.Padding
	} else {
		c.Box.PaddingLeft = j.PaddingLeft
		c.Box.PaddingRight = j.PaddingRight
	}
	for i := range j.ColorRules {
		cr := j.ColorRules[i]
		c.ColorRules = append(c.ColorRules, dsl.ColorRule{Meta: metaJSON(cr), When: cr.When, Color: cr.Color})
	}
	return c
}
