package dsl

import (
	"strings"
	"testing"
)

// validateSrc parses and validates src, returning all diagnostics.
func validateSrc(t *testing.T, body string) []Diagnostic {
	t.Helper()
	src := `<statusloom version="1" tool="claude-code"><layout name="l" active="true"><line>` +
		body + `</line></layout></statusloom>`
	_, diags := ParseAndValidate(src)
	return diags
}

// findDiag returns the first diagnostic whose message contains sub.
func findDiag(diags []Diagnostic, sub string) (Diagnostic, bool) {
	for _, d := range diags {
		if strings.Contains(d.Message, sub) {
			return d, true
		}
	}
	return Diagnostic{}, false
}

func TestParseOptional(t *testing.T) {
	tests := []struct {
		in      string
		field   string
		varName string
	}{
		{"env:AWS_PROFILE", "env", "AWS_PROFILE"},
		{"five-hour-usage", "five-hour-usage", ""},
		{"env:", "env", ""},
		{"env:A:B", "env", "A:B"}, // only the first colon splits
		{"", "", ""},
	}
	for _, tt := range tests {
		field, varName := ParseOptional(tt.in)
		if field != tt.field || varName != tt.varName {
			t.Errorf("ParseOptional(%q) = (%q, %q), want (%q, %q)", tt.in, field, varName, tt.field, tt.varName)
		}
	}
}

func TestValidateVar_Accepted(t *testing.T) {
	diags := validateSrc(t, `<field name="env" var="AWS_PROFILE"/>`)
	if HasErrors(diags) {
		t.Errorf("unexpected errors: %v", diags)
	}
	if len(diags) != 0 {
		t.Errorf("unexpected diagnostics for a plain env field: %v", diags)
	}
}

// TestValidateVar_MissingVarIsWarning pins the deliberate choice that a
// freshly dropped palette item stays SAVABLE: no var is a warning, not an
// error.
func TestValidateVar_MissingVarIsWarning(t *testing.T) {
	diags := validateSrc(t, `<field name="env"/>`)
	if HasErrors(diags) {
		t.Errorf("missing var produced an error, want warning only: %v", diags)
	}
	d, ok := findDiag(diags, "no var attribute")
	if !ok {
		t.Fatalf("no warning about the missing var: %v", diags)
	}
	if d.Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning", d.Severity)
	}
}

func TestValidateVar_Errors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			"var on a field that does not take one",
			`<field name="model" var="AWS_PROFILE"/>`,
			"does not take a var attribute",
		},
		{
			"unmask on a field that does not take one",
			`<field name="model" unmask="true"/>`,
			"does not take an unmask attribute",
		},
		{
			"unmask without a var",
			`<field name="env" unmask="true"/>`,
			"unmask requires a var attribute",
		},
		{
			"var containing the environ separator",
			`<field name="env" var="A=B"/>`,
			"invalid var",
		},
		{
			"optional naming a var field without a variable",
			`<field name="env" var="X" optional="env"/>`,
			"requires a variable name",
		},
		{
			"optional passing a variable to a field that takes none",
			`<field name="model" optional="model:X"/>`,
			"does not take a var",
		},
		{
			"optional naming an unknown field",
			`<field name="model" optional="nope"/>`,
			"unknown field",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := validateSrc(t, tt.body)
			if !HasErrors(diags) {
				t.Fatalf("no error diagnostic: %v", diags)
			}
			if _, ok := findDiag(diags, tt.want); !ok {
				t.Errorf("diagnostics %v do not mention %q", diags, tt.want)
			}
		})
	}
}

func TestValidateVar_SecretWarnings(t *testing.T) {
	// Masked by default: the author is told the value will not show.
	diags := validateSrc(t, `<field name="env" var="AWS_SECRET_ACCESS_KEY"/>`)
	if HasErrors(diags) {
		t.Errorf("unexpected errors: %v", diags)
	}
	if _, ok := findDiag(diags, "looks like a credential, so its value renders as"); !ok {
		t.Errorf("no masking warning: %v", diags)
	}

	// unmask on a secret name: the author is told it will be exposed.
	diags = validateSrc(t, `<field name="env" var="AWS_SECRET_ACCESS_KEY" unmask="true"/>`)
	if HasErrors(diags) {
		t.Errorf("unexpected errors: %v", diags)
	}
	d, ok := findDiag(diags, "renders it in clear text")
	if !ok {
		t.Fatalf("no exposure warning: %v", diags)
	}
	if d.Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning", d.Severity)
	}

	// A non-secret name warns about nothing.
	if diags := validateSrc(t, `<field name="env" var="AWS_PROFILE" unmask="true"/>`); len(diags) != 0 {
		t.Errorf("unexpected diagnostics: %v", diags)
	}
}

func TestValidateVar_OptionalWithVariable(t *testing.T) {
	body := `<span optional="env:AWS_PROFILE"><field name="env" var="AWS_PROFILE"/></span>`
	if diags := validateSrc(t, body); len(diags) != 0 {
		t.Errorf("unexpected diagnostics: %v", diags)
	}
}

// TestSerializeVar pins that var/unmask survive a parse -> serialize round
// trip (they are part of the exchange format).
func TestSerializeVar(t *testing.T) {
	src := `<statusloom version="1" tool="claude-code"><layout name="l" active="true"><line>` +
		`<field name="env" var="AWS_PROFILE" unmask="true"/>` +
		`</line></layout></statusloom>`
	doc, diags := Parse(src)
	if doc == nil || HasErrors(diags) {
		t.Fatalf("parse failed: %v", diags)
	}
	out := Serialize(doc)
	if !strings.Contains(out, `var="AWS_PROFILE"`) {
		t.Errorf("serialized output lost var:\n%s", out)
	}
	if !strings.Contains(out, `unmask="true"`) {
		t.Errorf("serialized output lost unmask:\n%s", out)
	}
}

// TestAccountFieldsRegistered pins the account-* catalog entries the UI and
// renderer both key off.
func TestAccountFieldsRegistered(t *testing.T) {
	for _, name := range []string{
		"account-email", "account-name", "account-org",
		"account-role", "account-plan", "account-seat",
	} {
		def, ok := FieldByName("claude-code", name)
		if !ok {
			t.Errorf("field %q not registered", name)
			continue
		}
		if def.RequiresVar {
			t.Errorf("field %q unexpectedly requires a var", name)
		}
		if def.Capability != "" {
			t.Errorf("field %q has capability %q, want none (it reads a local file)", name, def.Capability)
		}
		if def.DisplayName == "" || def.Descriptions.EN == "" || def.Descriptions.JA == "" {
			t.Errorf("field %q is missing display metadata", name)
		}
	}

	def, ok := FieldByName("claude-code", "env")
	if !ok {
		t.Fatal("field env not registered")
	}
	if !def.RequiresVar {
		t.Error("env field does not set RequiresVar")
	}
}
