package render

import (
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/schema"
)

func TestFormatThousands(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{5, "5"},
		{999, "999"},
		{1000, "1,000"},
		{64000, "64,000"},
		{200000, "200,000"},
		{1000000, "1,000,000"},
		{1234567, "1,234,567"},
	}
	for _, c := range cases {
		if got := formatThousands(c.in); got != c.want {
			t.Errorf("formatThousands(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatCompactK(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{64000, "64.0k"},
		{64500, "64.5k"},
		{200000, "200.0k"},
	}
	for _, c := range cases {
		if got := formatCompactK(c.in); got != c.want {
			t.Errorf("formatCompactK(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatPercent(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{32, "32%"},
		{32.0, "32%"},
		{41.2, "41.2%"},
		{41.20, "41.2%"},
		{33.3333, "33.3%"},
		{0, "0%"},
		{100, "100%"},
	}
	for _, c := range cases {
		if got := formatPercent(c.in); got != c.want {
			t.Errorf("formatPercent(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCountdown(t *testing.T) {
	base := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"minutes", 45 * time.Minute, "45m"},
		{"minutes truncate seconds", 45*time.Minute + 30*time.Second, "45m"},
		{"hours and minutes", 1*time.Hour + 23*time.Minute, "1h23m"},
		{"hours truncate", 1*time.Hour + 23*time.Minute + 59*time.Second, "1h23m"},
		{"days and hours", 2*24*time.Hour + 3*time.Hour, "2d3h"},
		{"exact zero", 0, "0m"},
		{"past", -5 * time.Minute, "0m"},
		{"less than a minute", 30 * time.Second, "0m"},
		{"exactly one hour", time.Hour, "1h0m"},
		{"exactly one day", 24 * time.Hour, "1d0h"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := countdown(base, base.Add(c.d)); got != c.want {
				t.Errorf("countdown(+%v) = %q, want %q", c.d, got, c.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"seconds", 42 * time.Second, "42s"},
		{"zero", 0, "0s"},
		{"negative", -5 * time.Second, "0s"},
		{"just under a minute", 59 * time.Second, "59s"},
		{"minutes and seconds", 5*time.Minute + 12*time.Second, "5m 12s"},
		{"exactly one minute", time.Minute, "1m 0s"},
		{"minutes truncate sub-second", 5*time.Minute + 12*time.Second + 900*time.Millisecond, "5m 12s"},
		{"just under an hour", 59*time.Minute + 59*time.Second, "59m 59s"},
		{"hours and minutes", 1*time.Hour + 15*time.Minute, "1h 15m"},
		{"exactly one hour", time.Hour, "1h 0m"},
		{"hours drop seconds", 1*time.Hour + 15*time.Minute + 59*time.Second, "1h 15m"},
		{"many hours", 26*time.Hour + 3*time.Minute, "26h 3m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatDuration(c.d); got != c.want {
				t.Errorf("formatDuration(%v) = %q, want %q", c.d, got, c.want)
			}
		})
	}
}

func TestVisibleWidth(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"plain", "hello", 5},
		{"empty", "", 0},
		{"ansi16 wrapped", "\x1b[31mhello\x1b[0m", 5},
		{"ansi256 wrapped", "\x1b[38;5;196mABCD\x1b[0m", 4},
		{"truecolor wrapped", "\x1b[38;2;255;0;0mX\x1b[0m", 1},
		{"bold and color", "\x1b[1;36mOpus\x1b[0m", 4},
		{"multibyte", "café", 4},
		{"spaces", "a   b", 5},
		// East Asian Width (markup.md "表示幅計算"): wide/fullwidth runes
		// count as 2 columns each.
		{"hiragana/katakana", "レビュー中", 10},        // 5 runes x2
		{"kanji", "確認中", 6},                       // 3 runes x2
		{"hangul syllables", "확인중", 6},            // 3 runes x2
		{"cjk symbols and punctuation", "。", 2},   // U+3002 (ideographic full stop)
		{"fullwidth forms", "ＡＢＣ", 6},             // U+FF21-23, fullwidth Latin
		{"mixed ascii and wide", "task レビュー", 13}, // "task " (5 cols) + 4 wide runes (8 cols)
		{"ansi + wide", "\x1b[31mレビュー\x1b[0m", 8}, // 4 wide runes, escapes width 0
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := visibleWidth(c.in); got != c.want {
				t.Errorf("visibleWidth(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestPadMinWidth(t *testing.T) {
	six := 6
	cases := []struct {
		name     string
		in       string
		minWidth *int
		align    string
		want     string
	}{
		{"nil min-width is a no-op", "abc", nil, "", "abc"},
		{"empty string is a no-op even with min-width", "", &six, "right", ""},
		{"left align (default) pads on the right", "ab", &six, "", "ab    "},
		{"explicit align=left pads on the right", "ab", &six, "left", "ab    "},
		{"align=right pads on the left", "ab", &six, "right", "    ab"},
		{"value at width is unchanged", "abcdef", &six, "right", "abcdef"},
		{"value over width is not truncated", "abcdefgh", &six, "right", "abcdefgh"},
		{"EAW-aware: 3 wide runes (6 cols) need no padding at min-width 6", "確認中", &six, "right", "確認中"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := padMinWidth(c.in, c.minWidth, c.align); got != c.want {
				t.Errorf("padMinWidth(%q, %v, %q) = %q, want %q", c.in, c.minWidth, c.align, got, c.want)
			}
		})
	}
}

func TestUsablePercent(t *testing.T) {
	cases := []struct {
		name       string
		ctx        *schema.ContextUsage
		cc         config.ContextConfig
		wantOK     bool
		wantString string // formatted usable percentage
	}{
		{
			name:       "200k auto reserve",
			ctx:        &schema.ContextUsage{TotalInputTokens: 64000, WindowSize: 200000},
			cc:         config.ContextConfig{},
			wantOK:     true,
			wantString: "38.3%", // 64000/(200000-33000)*100 = 38.323...
		},
		{
			name:       "200k explicit reserve",
			ctx:        &schema.ContextUsage{TotalInputTokens: 64000, WindowSize: 200000},
			cc:         config.ContextConfig{ReserveTokens: 40000},
			wantOK:     true,
			wantString: "40%", // 64000/(200000-40000)*100 = 40
		},
		{
			name:       "1M auto reserve",
			ctx:        &schema.ContextUsage{TotalInputTokens: 250000, WindowSize: 1000000},
			cc:         config.ContextConfig{},
			wantOK:     true,
			wantString: "29.9%", // 250000/(1000000-165000)*100 = 29.94...
		},
		{
			name:       "1M explicit reserve",
			ctx:        &schema.ContextUsage{TotalInputTokens: 250000, WindowSize: 1000000},
			cc:         config.ContextConfig{ReserveTokens: 200000},
			wantOK:     true,
			wantString: "31.3%", // 250000/(1000000-200000)*100 = 31.25 -> 31.3
		},
		{
			name:       "clamp to 100",
			ctx:        &schema.ContextUsage{TotalInputTokens: 300000, WindowSize: 200000},
			cc:         config.ContextConfig{},
			wantOK:     true,
			wantString: "100%",
		},
		{
			name:   "nil context",
			ctx:    nil,
			cc:     config.ContextConfig{},
			wantOK: false,
		},
		{
			name:   "zero window",
			ctx:    &schema.ContextUsage{TotalInputTokens: 1000, WindowSize: 0},
			cc:     config.ContextConfig{},
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := usablePercent(c.ctx, c.cc)
			if ok != c.wantOK {
				t.Fatalf("usablePercent ok = %v, want %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if s := formatPercent(got); s != c.wantString {
				t.Errorf("usablePercent = %v (%q), want %q", got, s, c.wantString)
			}
		})
	}
}
