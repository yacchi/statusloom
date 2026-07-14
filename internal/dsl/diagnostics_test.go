package dsl

import "testing"

func TestSeverity_String(t *testing.T) {
	if got := SeverityError.String(); got != "error" {
		t.Errorf("SeverityError.String() = %q, want %q", got, "error")
	}
	if got := SeverityWarning.String(); got != "warning" {
		t.Errorf("SeverityWarning.String() = %q, want %q", got, "warning")
	}
}

func TestErrorf(t *testing.T) {
	r := SourceRange{Start: 3, End: 8}
	d := Errorf(r, "bad %s at %d", "thing", 42)
	if d.Severity != SeverityError {
		t.Errorf("Severity = %v, want SeverityError", d.Severity)
	}
	if d.Message != "bad thing at 42" {
		t.Errorf("Message = %q, want %q", d.Message, "bad thing at 42")
	}
	if d.Range != r {
		t.Errorf("Range = %v, want %v", d.Range, r)
	}
	if d.Error() != d.Message {
		t.Errorf("Error() = %q, want %q", d.Error(), d.Message)
	}
}

func TestWarnf(t *testing.T) {
	d := Warnf(SourceRange{}, "heads up")
	if d.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want SeverityWarning", d.Severity)
	}
	if d.Message != "heads up" {
		t.Errorf("Message = %q, want %q", d.Message, "heads up")
	}
}

func TestHasErrors(t *testing.T) {
	if HasErrors(nil) {
		t.Error("HasErrors(nil) = true, want false")
	}
	if HasErrors([]Diagnostic{Warnf(SourceRange{}, "just a warning")}) {
		t.Error("HasErrors with only warnings = true, want false")
	}
	if !HasErrors([]Diagnostic{
		Warnf(SourceRange{}, "warning"),
		Errorf(SourceRange{}, "error"),
	}) {
		t.Error("HasErrors with an error present = false, want true")
	}
}
