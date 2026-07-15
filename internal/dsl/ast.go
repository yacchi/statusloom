package dsl

// This file defines the DSL abstract syntax tree (markup.md "AST"). The
// parser (parser.go) builds it from XML source; validation.go performs
// semantic checks over it; later phases add a serializer and an
// evaluation layer on top. Node source ranges are byte offsets into
// Document.Source (see source.go); the parser records them from
// xml.Decoder.InputOffset().

// Document is a fully parsed DSL file.
type Document struct {
	// Source is the original, unmodified source text. Node ranges index
	// into it, and the minimal-diff serializer (Phase 3) reuses unchanged
	// slices of it.
	Source string
	// Root is the parsed <statusloom> element, or nil when the source has
	// no root element (a recoverable structural error reported via
	// diagnostics; a well-formedness violation instead yields doc=nil from
	// Parse).
	Root *StatusloomNode
}

// NodeMeta is embedded by every AST node. It carries the node's source
// location and the Phase-3 minimal-diff dirty flag.
type NodeMeta struct {
	// SourceRange spans from the node's opening '<' to just past its
	// closing '>' (for self-closing elements, just past "/>").
	SourceRange SourceRange
	// Dirty marks a node whose serialized form must be regenerated rather
	// than reused from Source. The parser always leaves it false; only
	// Visual-Editor edits set it (Phase 3).
	Dirty bool
}

// Node is the common interface for the children of <line>, <span>, and
// <text> mixed content. Implemented by *SpanNode, *RawTextNode,
// *TextNode, *FieldNode, *FlexNode, and *CommentNode. LayoutNode,
// LineNode, StatusloomNode, and ColorRule are deliberately not Nodes:
// they live in typed slices / dedicated fields, not in a Children list.
type Node interface {
	node()
}

// StatusloomNode is the document root (<statusloom>). Tool-level settings
// are its attributes (markup.md "statusloom").
type StatusloomNode struct {
	Meta     NodeMeta
	Version  string
	Tool     string
	Settings ToolSettings
	// Git is the optional <git/> element (nil = omitted, use defaults).
	Git *GitSettings
	// Layouts are the <layout> children in document order.
	Layouts []*LayoutNode
	// Comments are the XML comments that appear directly under the root
	// (between <git>/<layout> children). The listed AST has no Children
	// list at the root, so they are collected here to preserve them for
	// round-tripping (markup.md "コメント": comments must not be dropped).
	Comments []*CommentNode
}

// ToolSettings holds the tool-level configuration attributes on the root.
// Unset attributes are represented by "" (string) or nil (*int) so the
// serializer can distinguish "absent" from "explicit zero".
type ToolSettings struct {
	ColorLevel            string // "" = unspecified
	OutputStyle           string // "" | "standard" | "powerline"
	CompactThreshold      *int   // nil = unspecified
	ContextPercentageMode string // "" = unspecified
	ContextReserveTokens  *int   // nil = unspecified
}

// GitSettings is the parsed <git/> element (markup.md "git"). Each
// attribute is a pointer so an omitted attribute is distinguishable from
// an explicit zero/false.
type GitSettings struct {
	Meta             NodeMeta
	CacheTTLMS       *int
	TimeoutMS        *int
	IncludeUntracked *bool
	CollectNumstat   *bool
}

// LayoutNode is one named <layout> (markup.md "layout").
type LayoutNode struct {
	Meta   NodeMeta
	Name   string
	Active *bool // nil = no active attribute present
	Lines  []*LineNode
	// Comments are XML comments directly under this layout (between
	// <line> children); preserved for round-tripping.
	Comments []*CommentNode
}

// LineNode is one status-line row (<line>).
type LineNode struct {
	Meta     NodeMeta
	Children []Node
	Common   CommonAttributes
}

// SpanNode groups children to inherit decoration / apply prefix-suffix /
// gate visibility (<span>).
type SpanNode struct {
	Meta     NodeMeta
	Children []Node
	Common   CommonAttributes
}

func (*SpanNode) node() {}

// RawTextNode is mixed-content text written directly (not inside a
// <text> element). RawValue is the untrimmed source text; RenderText is
// the whitespace-rule-applied display text (markup.md "生TextNodeの空白
// ルール").
type RawTextNode struct {
	Meta       NodeMeta
	RawValue   string
	RenderText string
}

func (*RawTextNode) node() {}

// TextNode is an explicit <text> element preserving leading/trailing
// whitespace. Role is "" or "separator".
type TextNode struct {
	Meta   NodeMeta
	Value  string
	Role   string // "" | "separator"
	Common CommonAttributes
}

func (*TextNode) node() {}

// FieldNode is a <field> displaying a dynamic value.
type FieldNode struct {
	Meta      NodeMeta
	Name      string
	Formatter FormatterConfig // raw format/precision/currency attribute values
	Raw       bool
	Hyperlink bool
	// MinWidth is the raw `min-width` attribute (nil = unspecified). When
	// set, the field's formatted value (outside prefix/suffix) is padded
	// with spaces to at least this many display columns; a longer value is
	// never truncated (markup.md "min-width").
	MinWidth *int
	// Align is the raw `align` attribute: "" (defaults to left) | "left" |
	// "right". It selects which side MinWidth padding is added to.
	Align  string
	Common CommonAttributes
}

func (*FieldNode) node() {}

// FlexNode is a <flex/> flexible separator. Size is the raw `size`
// attribute value; "" is treated as "full".
type FlexNode struct {
	Meta NodeMeta
	Size string
}

func (*FlexNode) node() {}

// CommentNode is an XML comment (<!-- ... -->); Text is its inner content.
type CommentNode struct {
	Meta NodeMeta
	Text string
}

func (*CommentNode) node() {}

// ColorRule is a <color-rule> child of a field/span/text node. It lives
// in the owner's CommonAttributes.ColorRules, not in a Children list, so
// it is not a Node. When/Color are the raw (XML-decoded) attribute
// values.
type ColorRule struct {
	Meta  NodeMeta
	When  string
	Color string
}

// Style holds the character-decoration common attributes. Bool fields are
// pointers so nil means "inherit / unspecified" versus an explicit
// true/false.
type Style struct {
	Color         string
	Background    string
	Bold          *bool
	Dim           *bool
	Italic        *bool
	Underline     *bool
	Strikethrough *bool
}

// Box holds the padding layout common attributes. `padding` expands to
// both sides; an explicit `padding-left`/`padding-right` overrides it.
type Box struct {
	PaddingLeft  *int
	PaddingRight *int
}

// CommonAttributes are the shared display attributes available on
// drawable nodes (line/span/text/field). Prefix/Suffix/Optional/When are
// raw attribute values ("" = unspecified).
type CommonAttributes struct {
	Style      Style
	Box        Box
	Prefix     string
	Suffix     string
	Optional   string
	When       string
	ColorRules []ColorRule
}
