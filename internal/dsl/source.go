// Package dsl implements the core of Statusloom's XML/JSX-style
// configuration DSL (see markup.md at the repository root for the full
// specification). This file provides the byte-offset source range type
// shared by diagnostics, the AST (added by a later change), and the
// serializer's minimal-diff bookkeeping.
package dsl

// SourceRange identifies a byte-offset span within a DSL source document,
// or (for condition.go's parse errors) within a single attribute value
// string such as a `when` expression.
//
// Start is inclusive, End is exclusive, following Go slicing conventions.
// A zero-value SourceRange (Start == End == 0) means "no range available"
// for diagnostics that cannot be attributed to a specific location.
//
// Node-level ranges are expected to be derived from
// xml.Decoder.InputOffset() by the AST/parser package (markup.md "AST" /
// "DSL表現の維持").
type SourceRange struct {
	Start int // byte offset (inclusive)
	End   int // byte offset (exclusive)
}

// Len returns the number of bytes the range spans. It returns 0 for an
// empty or malformed (End < Start) range.
func (r SourceRange) Len() int {
	if r.End < r.Start {
		return 0
	}
	return r.End - r.Start
}

// IsZero reports whether r is the zero value (Start == 0 && End == 0),
// i.e. it carries no location information.
func (r SourceRange) IsZero() bool {
	return r.Start == 0 && r.End == 0
}

// Contains reports whether the byte offset falls within [Start, End).
func (r SourceRange) Contains(offset int) bool {
	return offset >= r.Start && offset < r.End
}

// Slice returns the substring of src covered by the range. It returns ""
// if the range is out of bounds for src or malformed.
func (r SourceRange) Slice(src string) string {
	if r.Start < 0 || r.End > len(src) || r.Start > r.End {
		return ""
	}
	return src[r.Start:r.End]
}
