package dsl

// This file validates `format`/`precision`/`currency` attribute
// combinations on <field> nodes (markup.md "formatter"). It does not
// execute formatting (turning a value into display text) - that is
// deferred to the renderer once the AST/evaluation layer exists.

// FormatterConfig is the formatter-related attributes of a single
// <field> node.
type FormatterConfig struct {
	// Name is the `format` attribute value, or "" if not specified.
	Name string
	// Precision is the raw `precision` attribute value (e.g. "0", "2",
	// "adaptive"), or "" if not specified.
	Precision string
	// Currency is the raw `currency` attribute value (e.g. "USD"), or ""
	// if not specified.
	Currency string
}

// knownFormatters is the master set of formatter names the DSL
// recognizes (markup.md "formatter" -> "初期候補"). A field additionally
// restricts which of these it accepts via FieldDef.Formats.
var knownFormatters = map[string]bool{
	"percent":        true,
	"number":         true,
	"compact-number": true,
	"currency":       true,
	"duration":       true,
	"countdown":      true,
	"datetime":       true,
	"boolean":        true,
	"enum":           true,
}

// ValidateFormatter checks a <field>'s formatter configuration against
// its field definition. It returns an empty slice when cfg is valid for
// field. r is attached to every returned Diagnostic as its source range
// (the caller supplies the range of the relevant attribute(s) on the
// node, since this package does not itself have access to the AST).
func ValidateFormatter(field FieldDef, cfg FormatterConfig, r SourceRange) []Diagnostic {
	var diags []Diagnostic

	if cfg.Name == "" {
		if cfg.Precision != "" {
			diags = append(diags, Errorf(r, "precision attribute requires a format attribute"))
		}
		if cfg.Currency != "" {
			diags = append(diags, Errorf(r, "currency attribute requires format=\"currency\""))
		}
		return diags
	}

	if !knownFormatters[cfg.Name] {
		diags = append(diags, Errorf(r, "unknown formatter %q", cfg.Name))
		return diags
	}

	if !containsString(field.Formats, cfg.Name) {
		diags = append(diags, Errorf(r, "formatter %q is not applicable to field %q", cfg.Name, field.Name))
	}

	if cfg.Precision != "" {
		adaptiveOK := cfg.Name == "currency" && cfg.Precision == "adaptive"
		if !adaptiveOK && !isNonNegativeInt(cfg.Precision) {
			diags = append(diags, Errorf(r, "invalid precision %q: must be a non-negative integer, or \"adaptive\" for format=\"currency\"", cfg.Precision))
		}
	}

	if cfg.Currency != "" {
		if cfg.Name != "currency" {
			diags = append(diags, Errorf(r, "currency attribute is only valid with format=\"currency\""))
		} else if cfg.Currency != "USD" {
			diags = append(diags, Errorf(r, "unsupported currency %q: only \"USD\" is supported", cfg.Currency))
		}
	}

	return diags
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func isNonNegativeInt(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
