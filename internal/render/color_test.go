package render

import "testing"

func TestParseColor(t *testing.T) {
	cases := []struct {
		in       string
		wantKind colorKind
	}{
		{"", ckNone},
		{"cyan", ckAnsi16},
		{"brightWhite", ckAnsi16},
		{"ansi256:196", ckAnsi256},
		{"ansi256:0", ckAnsi256},
		{"ansi256:255", ckAnsi256},
		{"ansi256:256", ckNone}, // out of range
		{"ansi256:x", ckNone},
		{"#ff0000", ckTruecolor},
		{"#00ff00", ckTruecolor},
		{"#zzzzzz", ckNone},
		{"#fff", ckNone}, // wrong length
		{"bogus", ckNone},
	}
	for _, c := range cases {
		if got := parseColor(c.in); got.kind != c.wantKind {
			t.Errorf("parseColor(%q).kind = %v, want %v", c.in, got.kind, c.wantKind)
		}
	}
}

func TestDowngradeToAnsi16(t *testing.T) {
	// #ff0000 -> nearest ansi256 (196) -> nearest ansi16 (red = 1).
	red := parseColor("#ff0000")
	got := downgrade(red, levelAnsi16)
	if got.kind != ckAnsi16 || got.idx != 1 {
		t.Errorf("#ff0000 -> ansi16 = %+v, want ansi16 idx 1 (red)", got)
	}

	// ansi256:196 -> nearest ansi16 (red = 1).
	c196 := parseColor("ansi256:196")
	got = downgrade(c196, levelAnsi16)
	if got.kind != ckAnsi16 || got.idx != 1 {
		t.Errorf("ansi256:196 -> ansi16 = %+v, want ansi16 idx 1 (red)", got)
	}
}

func TestDowngradePreservesAtLevel(t *testing.T) {
	// Truecolor stays truecolor at truecolor level.
	c := parseColor("#123456")
	if got := downgrade(c, levelTruecolor); got.kind != ckTruecolor {
		t.Errorf("truecolor at truecolor level downgraded to %v", got.kind)
	}
	// Truecolor -> ansi256 at ansi256 level.
	if got := downgrade(c, levelAnsi256); got.kind != ckAnsi256 {
		t.Errorf("truecolor at ansi256 level = %v, want ansi256", got.kind)
	}
	// ansi256 stays at ansi256 level.
	if got := downgrade(parseColor("ansi256:100"), levelAnsi256); got.kind != ckAnsi256 {
		t.Errorf("ansi256 at ansi256 level = %v, want ansi256", got.kind)
	}
}

func TestRgbToAnsi256(t *testing.T) {
	if got := rgbToAnsi256(255, 0, 0); got != 196 {
		t.Errorf("rgbToAnsi256(red) = %d, want 196", got)
	}
	if got := rgbToAnsi256(0, 0, 0); got != 16 {
		t.Errorf("rgbToAnsi256(black) = %d, want 16", got)
	}
	if got := rgbToAnsi256(255, 255, 255); got != 231 {
		t.Errorf("rgbToAnsi256(white) = %d, want 231", got)
	}
}
