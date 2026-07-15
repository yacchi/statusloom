package dsl

import "testing"

func mustField(t *testing.T, name string) FieldDef {
	t.Helper()
	f, ok := FieldByName("claude-code", name)
	if !ok {
		t.Fatalf("field %q not found in registry", name)
	}
	return f
}

func TestValidateFormatter_NoFormatter_NoAttributes_IsValid(t *testing.T) {
	field := mustField(t, "model")
	diags := ValidateFormatter(field, FormatterConfig{}, SourceRange{})
	if HasErrors(diags) {
		t.Errorf("unexpected errors: %v", diags)
	}
}

func TestValidateFormatter_PrecisionWithoutFormatIsError(t *testing.T) {
	field := mustField(t, "model")
	diags := ValidateFormatter(field, FormatterConfig{Precision: "2"}, SourceRange{})
	if !HasErrors(diags) {
		t.Error("expected error: precision without a format attribute")
	}
}

func TestValidateFormatter_CurrencyWithoutFormatIsError(t *testing.T) {
	field := mustField(t, "model")
	diags := ValidateFormatter(field, FormatterConfig{Currency: "USD"}, SourceRange{})
	if !HasErrors(diags) {
		t.Error("expected error: currency without a format attribute")
	}
}

func TestValidateFormatter_UnknownFormatterIsError(t *testing.T) {
	field := mustField(t, "context-length")
	diags := ValidateFormatter(field, FormatterConfig{Name: "hexadecimal"}, SourceRange{})
	if !HasErrors(diags) {
		t.Error("expected error for unknown formatter name")
	}
}

func TestValidateFormatter_NotApplicableToFieldIsError(t *testing.T) {
	field := mustField(t, "model") // Formats is empty
	diags := ValidateFormatter(field, FormatterConfig{Name: "percent"}, SourceRange{})
	if !HasErrors(diags) {
		t.Error("expected error: percent is not applicable to a field with no Formats")
	}
}

func TestValidateFormatter_FormatterNotInFieldsAllowList(t *testing.T) {
	field := mustField(t, "context-length") // Formats: number, compact-number
	diags := ValidateFormatter(field, FormatterConfig{Name: "percent"}, SourceRange{})
	if !HasErrors(diags) {
		t.Error("expected error: percent is not in context-length's Formats")
	}
}

func TestValidateFormatter_ValidPercentWithPrecision(t *testing.T) {
	field := mustField(t, "context-percentage")
	diags := ValidateFormatter(field, FormatterConfig{Name: "percent", Precision: "0"}, SourceRange{})
	if HasErrors(diags) {
		t.Errorf("unexpected errors: %v", diags)
	}
}

func TestValidateFormatter_NegativePrecisionIsError(t *testing.T) {
	field := mustField(t, "context-percentage")
	diags := ValidateFormatter(field, FormatterConfig{Name: "percent", Precision: "-1"}, SourceRange{})
	if !HasErrors(diags) {
		t.Error("expected error for negative precision")
	}
}

func TestValidateFormatter_NonNumericPrecisionIsError(t *testing.T) {
	field := mustField(t, "context-percentage")
	diags := ValidateFormatter(field, FormatterConfig{Name: "percent", Precision: "high"}, SourceRange{})
	if !HasErrors(diags) {
		t.Error("expected error for non-numeric precision")
	}
}

func TestValidateFormatter_AdaptivePrecision_OnlyValidForCurrency(t *testing.T) {
	currency := mustField(t, "session-cost")
	diags := ValidateFormatter(currency, FormatterConfig{Name: "currency", Precision: "adaptive"}, SourceRange{})
	if HasErrors(diags) {
		t.Errorf("adaptive precision should be valid for currency: %v", diags)
	}

	percent := mustField(t, "context-percentage")
	diags2 := ValidateFormatter(percent, FormatterConfig{Name: "percent", Precision: "adaptive"}, SourceRange{})
	if !HasErrors(diags2) {
		t.Error("adaptive precision should be invalid for a non-currency formatter")
	}
}

func TestValidateFormatter_CurrencyAttributeRequiresCurrencyFormat(t *testing.T) {
	field := mustField(t, "context-percentage")
	diags := ValidateFormatter(field, FormatterConfig{Name: "percent", Currency: "USD"}, SourceRange{})
	if !HasErrors(diags) {
		t.Error("expected error: currency attribute set with format=\"percent\"")
	}
}

func TestValidateFormatter_UnsupportedCurrencyIsError(t *testing.T) {
	field := mustField(t, "session-cost")
	diags := ValidateFormatter(field, FormatterConfig{Name: "currency", Currency: "EUR"}, SourceRange{})
	if !HasErrors(diags) {
		t.Error("expected error for unsupported currency EUR")
	}
}

func TestValidateFormatter_ValidCurrencyUSD(t *testing.T) {
	field := mustField(t, "session-cost")
	diags := ValidateFormatter(field, FormatterConfig{Name: "currency", Currency: "USD", Precision: "2"}, SourceRange{})
	if HasErrors(diags) {
		t.Errorf("unexpected errors: %v", diags)
	}
}

func TestValidateFormatter_EnumFormatterOnEnumField(t *testing.T) {
	field := mustField(t, "thinking-effort")
	diags := ValidateFormatter(field, FormatterConfig{Name: "enum"}, SourceRange{})
	if HasErrors(diags) {
		t.Errorf("unexpected errors: %v", diags)
	}
}

func TestValidateFormatter_DiagnosticCarriesGivenRange(t *testing.T) {
	field := mustField(t, "model")
	r := SourceRange{Start: 10, End: 20}
	diags := ValidateFormatter(field, FormatterConfig{Name: "percent"}, r)
	if len(diags) == 0 {
		t.Fatal("expected at least one diagnostic")
	}
	for _, d := range diags {
		if d.Range != r {
			t.Errorf("diagnostic Range = %v, want %v", d.Range, r)
		}
	}
}
