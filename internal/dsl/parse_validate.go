package dsl

// ParseAndValidate parses src and, when a root element was produced, appends
// the semantic validation diagnostics. It is the single public spelling of the
// "parse then validate" composition that callers outside the dsl package need
// as a validation boundary (e.g. internal/store's Save gate and the webconfig
// document handlers). The returned diagnostics are Parse's structural findings
// followed by Validate's semantic findings; doc is nil only when the source is
// not well-formed (mirroring Parse).
func ParseAndValidate(src string) (*Document, []Diagnostic) {
	doc, diags := Parse(src)
	if doc != nil && doc.Root != nil {
		diags = append(diags, Validate(doc)...)
	}
	return doc, diags
}
