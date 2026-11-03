package dsl

import (
	"strings"
	"testing"
)

func TestValidSourceID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"single letter", "a", true},
		{"plain word", "ccprofile", true},
		{"hyphenated", "a-b-1", true},
		{"trailing hyphen", "a-", true},
		{"max length 64", strings.Repeat("a", 64), true},
		{"over max length 65", strings.Repeat("a", 65), false},
		{"empty", "", false},
		{"leading digit", "1abc", false},
		{"leading hyphen", "-abc", false},
		{"uppercase first", "Abc", false},
		{"uppercase later", "aBc", false},
		{"underscore", "a_b", false},
		{"dot", "a.b", false},
		{"space", "a b", false},
		{"colon", "a:b", false},
		{"non-ascii word", "café", false},
		{"non-ascii letter", "é", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidSourceID(tt.id); got != tt.want {
				t.Fatalf("ValidSourceID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestParseMetricRef(t *testing.T) {
	tests := []struct {
		name      string
		ref       string
		wantName  string
		wantParam string
	}{
		{"with param", "source-stale:ccprofile", "source-stale", "ccprofile"},
		{"no colon", "width", "width", ""},
		{"empty param", "source-stale:", "source-stale", ""},
		{"splits at first colon only", "a:b:c", "a", "b:c"},
		{"empty name", ":x", "", "x"},
		{"empty ref", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotParam := ParseMetricRef(tt.ref)
			if gotName != tt.wantName || gotParam != tt.wantParam {
				t.Fatalf("ParseMetricRef(%q) = (%q, %q), want (%q, %q)",
					tt.ref, gotName, gotParam, tt.wantName, tt.wantParam)
			}
		})
	}
}
