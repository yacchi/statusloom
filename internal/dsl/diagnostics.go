package dsl

import "fmt"

// Severity classifies a Diagnostic. Errors block save/validation; warnings
// do not.
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
)

// String renders the severity as "error" or "warning".
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	default:
		return "unknown"
	}
}

// Diagnostic is a single validation finding, optionally located within the
// source document via Range. See markup.md "validation".
type Diagnostic struct {
	Severity Severity
	Message  string
	Range    SourceRange
}

// Error implements the error interface so a Diagnostic can be used
// anywhere a plain error is expected (e.g. wrapped, compared with
// errors.As).
func (d Diagnostic) Error() string {
	return d.Message
}

// Errorf builds an error-severity Diagnostic with a formatted message.
func Errorf(r SourceRange, format string, args ...any) Diagnostic {
	return Diagnostic{Severity: SeverityError, Message: fmt.Sprintf(format, args...), Range: r}
}

// Warnf builds a warning-severity Diagnostic with a formatted message.
func Warnf(r SourceRange, format string, args ...any) Diagnostic {
	return Diagnostic{Severity: SeverityWarning, Message: fmt.Sprintf(format, args...), Range: r}
}

// HasErrors reports whether diags contains at least one SeverityError
// entry. Validation callers use this to decide whether a document (or a
// single formatter/condition configuration) is save-eligible.
func HasErrors(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}
