package dsl

import (
	"encoding/xml"
	"strconv"
	"strings"
)

// This file holds attribute-level parsing helpers shared by parser.go:
// non-negative int / boolean attribute parsing (with parse-time
// diagnostics for type violations, per markup.md validation) and the
// common-attribute collector that extracts Style/Box/prefix/suffix/
// optional/when from an element's attributes.

// parseBoolAttr parses a boolean attribute value ("true"/"false" only).
// On an invalid value it records an error diagnostic and returns nil.
func (p *parser) parseBoolAttr(name, val string, r SourceRange) *bool {
	switch val {
	case "true":
		t := true
		return &t
	case "false":
		f := false
		return &f
	default:
		p.errf(r, "attribute %q must be \"true\" or \"false\", got %q", name, val)
		return nil
	}
}

// parseIntAttr parses a non-negative integer attribute value. On an
// invalid value it records an error diagnostic and returns nil.
func (p *parser) parseIntAttr(name, val string, r SourceRange) *int {
	n, ok := parseNonNegInt(val)
	if !ok {
		p.errf(r, "attribute %q must be a non-negative integer, got %q", name, val)
		return nil
	}
	return &n
}

// parsePositiveIntAttr parses a positive (>= 1) integer attribute value. On
// an invalid value (zero, negative, or non-numeric) it records an error
// diagnostic and returns nil.
func (p *parser) parsePositiveIntAttr(name, val string, r SourceRange) *int {
	n, ok := parseNonNegInt(val)
	if !ok || n < 1 {
		p.errf(r, "attribute %q must be a positive integer, got %q", name, val)
		return nil
	}
	return &n
}

// parseNonNegInt reports whether s is a base-10 non-negative integer and,
// if so, its value. It rejects signs, whitespace, and empty strings.
func parseNonNegInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

// isPositiveInt reports whether s is a base-10 integer >= 1.
func isPositiveInt(s string) bool {
	n, ok := parseNonNegInt(s)
	return ok && n >= 1
}

// commonBuilder accumulates the common attributes of a drawable element
// while its attribute list is iterated. padding resolution is deferred to
// build() because an explicit padding-left/right must win over padding
// regardless of attribute order.
type commonBuilder struct {
	c        CommonAttributes
	padding  *int
	padLeft  *int
	padRight *int
	hasLeft  bool
	hasRight bool
}

// consume applies attribute a if it is a recognized common attribute,
// recording diagnostics for type violations. It reports whether a was a
// common attribute (so the caller can flag genuinely unknown ones).
func (b *commonBuilder) consume(p *parser, a xml.Attr, r SourceRange) bool {
	switch a.Name.Local {
	case "color":
		b.c.Style.Color = a.Value
	case "background":
		b.c.Style.Background = a.Value
	case "bold":
		b.c.Style.Bold = p.parseBoolAttr("bold", a.Value, r)
	case "dim":
		b.c.Style.Dim = p.parseBoolAttr("dim", a.Value, r)
	case "italic":
		b.c.Style.Italic = p.parseBoolAttr("italic", a.Value, r)
	case "underline":
		b.c.Style.Underline = p.parseBoolAttr("underline", a.Value, r)
	case "strikethrough":
		b.c.Style.Strikethrough = p.parseBoolAttr("strikethrough", a.Value, r)
	case "padding":
		b.padding = p.parseIntAttr("padding", a.Value, r)
	case "padding-left":
		b.padLeft = p.parseIntAttr("padding-left", a.Value, r)
		b.hasLeft = true
	case "padding-right":
		b.padRight = p.parseIntAttr("padding-right", a.Value, r)
		b.hasRight = true
	case "prefix":
		b.c.Prefix = a.Value
	case "suffix":
		b.c.Suffix = a.Value
	case "optional":
		b.c.Optional = a.Value
	case "when":
		b.c.When = a.Value
	default:
		return false
	}
	return true
}

// build finalizes the accumulated attributes, expanding `padding` to
// whichever side was not explicitly overridden.
func (b *commonBuilder) build() CommonAttributes {
	if b.hasLeft {
		b.c.Box.PaddingLeft = b.padLeft
	} else if b.padding != nil {
		v := *b.padding
		b.c.Box.PaddingLeft = &v
	}
	if b.hasRight {
		b.c.Box.PaddingRight = b.padRight
	} else if b.padding != nil {
		v := *b.padding
		b.c.Box.PaddingRight = &v
	}
	return b.c
}

// attrValueRange locates attr="value" (or attr='value') within the
// opening tag of the element spanned by base, returning the source range
// of the value text (excluding quotes). ok is false when the attribute
// cannot be located; callers then fall back to base. This is best-effort:
// with XML entities in the value the byte offsets are approximate but
// stay within the value.
func attrValueRange(src string, base SourceRange, attr string) (SourceRange, bool) {
	if base.Start < 0 || base.End > len(src) || base.Start >= base.End {
		return SourceRange{}, false
	}
	seg := src[base.Start:base.End]
	if gt := strings.IndexByte(seg, '>'); gt >= 0 {
		seg = seg[:gt]
	}
	// Scan for the attribute name as a whole token followed by '='.
	i := 0
	for {
		idx := strings.Index(seg[i:], attr)
		if idx < 0 {
			return SourceRange{}, false
		}
		at := i + idx
		before := at == 0 || isAttrBoundary(seg[at-1])
		rest := strings.TrimLeft(seg[at+len(attr):], " \t\r\n")
		if before && strings.HasPrefix(rest, "=") {
			q := strings.TrimLeft(rest[1:], " \t\r\n")
			if len(q) > 0 && (q[0] == '"' || q[0] == '\'') {
				quote := q[0]
				valStartInQ := len(seg) - len(q) + 1
				end := strings.IndexByte(seg[valStartInQ:], quote)
				if end >= 0 {
					return SourceRange{
						Start: base.Start + valStartInQ,
						End:   base.Start + valStartInQ + end,
					}, true
				}
			}
		}
		i = at + len(attr)
	}
}

func isAttrBoundary(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '<' || c == '/'
}

// offsetInto returns a one-byte range at value-relative offset off inside
// r, clamped to r's bounds.
func offsetInto(r SourceRange, off int) SourceRange {
	start := r.Start + off
	if start < r.Start {
		start = r.Start
	}
	if start > r.End {
		start = r.End
	}
	end := start + 1
	if end > r.End {
		end = r.End
	}
	if end < start {
		end = start
	}
	return SourceRange{Start: start, End: end}
}
