package dsl

import (
	"strings"
	"testing"
)

// wantOnlyError asserts that diags holds exactly one diagnostic, an error with
// the given message.
func wantOnlyError(t *testing.T, diags []Diagnostic, msg string) {
	t.Helper()
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %v, want exactly one error %q", len(diags), diags, msg)
	}
	if diags[0].Severity != SeverityError || diags[0].Message != msg {
		t.Fatalf("got %s %q, want error %q", diags[0].Severity, diags[0].Message, msg)
	}
}

// wantNoErrors asserts that diags holds no error. Warnings are tolerated here
// because the cases below are about errors only.
func wantNoErrors(t *testing.T, diags []Diagnostic) {
	t.Helper()
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Fatalf("unexpected error: %q", d.Message)
		}
	}
}

func TestSourceAttrsRejectedOnNonSourceField(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"source", `<field name="model" source="x"/>`, `field "model" does not take a source attribute`},
		{"select", `<field name="model" select=".a"/>`, `field "model" does not take a select attribute`},
		{"fallback", `<field name="model" fallback="model"/>`, `field "model" does not take a fallback attribute`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantOnlyError(t, validateSrc(t, tt.body), tt.want)
		})
	}
}

func TestSourceEmptyIdProducesNoDiagnostic(t *testing.T) {
	for _, body := range []string{`<field name="source"/>`, `<field name="source" source=""/>`} {
		if diags := validateSrc(t, body); len(diags) != 0 {
			t.Fatalf("%s: got diagnostics %v, want none", body, diags)
		}
	}
}

func TestSourceValidAttributesAccepted(t *testing.T) {
	diags := validateSrc(t, `<field name="source" source="ccprofile" select=".profile" fallback="model"/>`)
	wantNoErrors(t, diags)
}

func TestSourceInvalidID(t *testing.T) {
	const bad = `invalid source id "Bad": use 1-64 characters of a-z, 0-9 and -, starting with a letter`
	wantOnlyError(t, validateSrc(t, `<field name="source" source="Bad"/>`), bad)

	long64 := strings.Repeat("a", 64)
	long65 := strings.Repeat("a", 65)
	wantNoErrors(t, validateSrc(t, `<field name="source" source="`+long64+`"/>`))
	if _, ok := findDiag(validateSrc(t, `<field name="source" source="`+long65+`"/>`), "invalid source id"); !ok {
		t.Fatal("65-character source id: want invalid-source-id error")
	}
}

func TestSourceInvalidSelect(t *testing.T) {
	diags := validateSrc(t, `<field name="source" source="cc" select=".a["/>`)
	if len(diags) != 1 || diags[0].Severity != SeverityError {
		t.Fatalf("got %v, want one error", diags)
	}
	// The message must be the jsonselect SyntaxError's Message, not its
	// Error() text, which appends an offset.
	if !strings.HasPrefix(diags[0].Message, `invalid select ".a[": `) {
		t.Fatalf("message %q, want prefix %q", diags[0].Message, `invalid select ".a[": `)
	}
	if strings.Contains(diags[0].Message, "at offset") {
		t.Fatalf("message %q carries the Error() offset text", diags[0].Message)
	}
}

func TestSourceFallbackErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"unknown", `<field name="source" source="cc" fallback="nope"/>`,
			`fallback references unknown field "nope"`},
		{"env", `<field name="source" source="cc" fallback="env"/>`,
			`fallback "env" must be a built-in field; source and env cannot be a fallback`},
		{"source", `<field name="source" source="cc" fallback="source"/>`,
			`fallback "source" must be a built-in field; source and env cannot be a fallback`},
		{"subagent", `<field name="source" source="cc" fallback="task-model"/>`,
			`fallback "task-model" cannot be used outside <subagent>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := validateSrc(t, tt.body)
			if _, ok := findDiag(diags, tt.want); !ok {
				t.Fatalf("got %v, want diagnostic %q", diags, tt.want)
			}
		})
	}
}

// The formatter must be checked against the FALLBACK field's formats: the
// source field's own Formats do not decide whether the fallback can format.
func TestSourceFallbackFormatterUsesFallbackFormats(t *testing.T) {
	wantOnlyError(t,
		validateSrc(t, `<field name="source" source="cc" fallback="model" format="percent"/>`),
		`formatter "percent" is not applicable to fallback field "model"`)

	wantOnlyError(t,
		validateSrc(t, `<field name="source" source="cc" fallback="model" format="enum"/>`),
		`formatter "enum" is not applicable to fallback field "model"`)

	wantNoErrors(t, validateSrc(t, `<field name="source" source="cc" fallback="session-cost" format="currency"/>`))
}

func TestOptionalSourceRequiresID(t *testing.T) {
	wantNoErrors(t, validateSrc(t, `<field name="model" optional="source:cc"/>`))

	wantOnlyError(t, validateSrc(t, `<field name="model" optional="source"/>`),
		`optional "source" requires a source id, as in optional="source:SOURCE_ID"`)

	wantOnlyError(t, validateSrc(t, `<field name="model" optional="source:"/>`),
		`optional "source:" requires a source id, as in optional="source:SOURCE_ID"`)

	wantOnlyError(t, validateSrc(t, `<field name="model" optional="source:Bad"/>`),
		`invalid source id "Bad": use 1-64 characters of a-z, 0-9 and -, starting with a letter`)
}

func TestConditionSourceMetrics(t *testing.T) {
	wantNoErrors(t, validateSrc(t, `<text when="source-stale:cc">x</text>`))
	wantNoErrors(t, validateSrc(t, `<text when="not source-stale:cc">x</text>`))

	wantOnlyError(t, validateSrc(t, `<text when="width:x gt 3">x</text>`),
		`metric "width:x" takes no parameter`)

	wantOnlyError(t, validateSrc(t, `<text when="source-stale">x</text>`),
		`metric "source-stale" requires a source id, as in "source-stale:SOURCE_ID"`)

	long65 := strings.Repeat("a", 65)
	if _, ok := findDiag(validateSrc(t, `<text when="source-stale:`+long65+`">x</text>`), "invalid source id"); !ok {
		t.Fatal("65-character metric id: want invalid-source-id error")
	}

	wantOnlyError(t, validateSrc(t, `<text when="nope:x">x</text>`), `unknown metric "nope:x"`)
}

func TestColorRuleSourceMetrics(t *testing.T) {
	wantOnlyError(t,
		validateSrc(t, `<field name="model"><color-rule when="source-stale" color="red"/></field>`),
		`metric "source-stale" requires a source id, as in "source-stale:SOURCE_ID"`)
}
