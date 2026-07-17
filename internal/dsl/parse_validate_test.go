package dsl

import "testing"

func TestParseAndValidate(t *testing.T) {
	const valid = `<statusloom version="1" tool="claude-code"><layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`

	doc, diags := ParseAndValidate(valid)
	if doc == nil || doc.Root == nil {
		t.Fatalf("valid source produced no root: doc=%v", doc)
	}
	if HasErrors(diags) {
		t.Fatalf("valid source produced error diagnostics: %v", diags)
	}
}

func TestParseAndValidateSemanticError(t *testing.T) {
	// Well-formed XML but references an unknown field: Parse succeeds (root
	// present) and Validate appends an error diagnostic.
	const semanticallyInvalid = `<statusloom version="1" tool="claude-code"><layout name="d" active="true"><line><field name="not-a-field"/></line></layout></statusloom>`

	doc, diags := ParseAndValidate(semanticallyInvalid)
	if doc == nil || doc.Root == nil {
		t.Fatalf("well-formed source should still yield a root: doc=%v", doc)
	}
	if !HasErrors(diags) {
		t.Fatalf("expected a validation error for an unknown field, got: %v", diags)
	}
}

func TestParseAndValidateMalformed(t *testing.T) {
	// Not well-formed: Parse yields doc=nil, and ParseAndValidate must not
	// panic dereferencing it.
	doc, diags := ParseAndValidate(`<statusloom><layout></statusloom>`)
	_ = doc
	if !HasErrors(diags) {
		t.Fatalf("expected diagnostics for malformed source, got: %v", diags)
	}
}
