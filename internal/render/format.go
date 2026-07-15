package render

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// formatThousands renders a non-negative integer with comma thousands
// separators, e.g. 64000 -> "64,000".
func formatThousands(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre == 0 {
		pre = 3
	}
	b.WriteString(s[:pre])
	for i := pre; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// formatCompactK renders an integer in compact thousands form with one
// decimal place, e.g. 64000 -> "64.0k".
func formatCompactK(n int) string {
	return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k"
}

// formatPercentValue renders a percentage value rounded to one decimal
// place with trailing zeros trimmed, without the trailing "%" sign,
// e.g. 32.0 -> "32", 41.2 -> "41.2", 33.3333 -> "33.3".
func formatPercentValue(f float64) string {
	rounded := math.Round(f*10) / 10
	return strconv.FormatFloat(rounded, 'f', -1, 64)
}

// formatPercent is formatPercentValue with a trailing "%".
func formatPercent(f float64) string {
	return formatPercentValue(f) + "%"
}

// roundInt rounds a float to the nearest integer (round half away from
// zero).
func roundInt(f float64) int {
	return int(math.Round(f))
}

// countdown renders the duration between now and resetsAt as a compact
// human string:
//
//	> 1 day   -> "2d3h"  (days + hours)
//	> 1 hour  -> "1h23m" (hours + minutes)
//	otherwise -> "45m"   (minutes)
//	past/zero -> "0m"
//
// Sub-unit remainders are truncated (floored), not rounded.
func countdown(now, resetsAt time.Time) string {
	d := resetsAt.Sub(now)
	if d <= 0 {
		return "0m"
	}
	days := int(d / (24 * time.Hour))
	hours := int((d % (24 * time.Hour)) / time.Hour)
	mins := int((d % time.Hour) / time.Minute)
	switch {
	case days > 0:
		return strconv.Itoa(days) + "d" + strconv.Itoa(hours) + "h"
	case hours > 0:
		return strconv.Itoa(hours) + "h" + strconv.Itoa(mins) + "m"
	default:
		return strconv.Itoa(mins) + "m"
	}
}

// formatDuration renders a duration as a compact human string:
//
//	< 1 min   -> "42s"
//	< 1 hour  -> "5m 12s"
//	>= 1 hour -> "1h 15m"  (seconds omitted at the hour scale)
//
// Sub-unit remainders are truncated (floored), not rounded. Negative
// durations are treated as zero ("0s").
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		m := int(d / time.Minute)
		s := int((d % time.Minute) / time.Second)
		return strconv.Itoa(m) + "m " + strconv.Itoa(s) + "s"
	default:
		h := int(d / time.Hour)
		m := int((d % time.Hour) / time.Minute)
		return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
	}
}

// visibleWidth returns the display width (in terminal columns) of s,
// ignoring ANSI escape sequences (CSI sequences introduced by ESC "[") and
// counting East Asian wide/fullwidth runes as 2 columns (see runeWidth).
// This is the single width-calculation source for flex sizing, compact-mode
// thresholding, padding, and min-width alignment.
func visibleWidth(s string) int {
	w := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b { // ESC
			i++
			if i < len(s) && s[i] == '[' {
				i++
				// Consume parameter/intermediate bytes until the final
				// byte in the range '@'..'~'.
				for i < len(s) && !(s[i] >= '@' && s[i] <= '~') {
					i++
				}
				if i < len(s) {
					i++ // consume final byte
				}
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		w += runeWidth(r)
		i += size
	}
	return w
}

// runeWidth returns the display width of a single rune: 2 columns for East
// Asian wide/fullwidth characters, 1 otherwise. Combining marks (width 0)
// are out of scope; ASCII and general symbols are width 1. Go's standard
// library has no East Asian Width table, so this combines unicode.Is
// range checks (Han/Hiragana/Katakana/Hangul) with explicit block ranges
// for the wide punctuation/symbol blocks those categories do not cover
// (CJK symbols and punctuation, fullwidth forms, CJK compatibility, etc.),
// per Unicode's EastAsianWidth.txt W/F ranges (markup.md "East Asian Width
// 対応").
func runeWidth(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115F: // Hangul Jamo
		return 2
	case r >= 0x2E80 && r <= 0x2FDF: // Kangxi radicals / CJK radicals supplement
		return 2
	case r >= 0x3000 && r <= 0x303F: // CJK symbols and punctuation
		return 2
	case r >= 0x3040 && r <= 0x30FF: // Hiragana / Katakana
		return 2
	case r >= 0x3300 && r <= 0x33FF: // CJK compatibility
		return 2
	case r >= 0x3400 && r <= 0x4DBF: // CJK unified ideographs extension A
		return 2
	case r >= 0x4E00 && r <= 0x9FFF: // CJK unified ideographs
		return 2
	case r >= 0xAC00 && r <= 0xD7A3: // Hangul syllables
		return 2
	case r >= 0xF900 && r <= 0xFAFF: // CJK compatibility ideographs
		return 2
	case r >= 0xFE30 && r <= 0xFE4F: // CJK compatibility forms (vertical forms)
		return 2
	case r >= 0xFF00 && r <= 0xFF60: // Fullwidth forms
		return 2
	case r >= 0xFFE0 && r <= 0xFFE6: // Fullwidth signs
		return 2
	case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r),
		unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
		return 2
	default:
		return 1
	}
}

// padMinWidth pads s with spaces to at least minWidth display columns
// (measured via visibleWidth so East Asian wide characters count as 2),
// adding the padding on the side opposite align: align="right" pads on the
// left, everything else (default "left") pads on the right. A value already
// at or beyond minWidth is returned unchanged (min-width never truncates).
// minWidth == nil means "no min-width set"; s is returned unchanged. An
// empty s is also returned unchanged: a field whose value is empty (but
// whose optional/when gate still passed) keeps the "field本体は空文字を
// 返す" invariant (markup.md "field") rather than rendering bare padding
// spaces as if it had content.
func padMinWidth(s string, minWidth *int, align string) string {
	if minWidth == nil || s == "" {
		return s
	}
	deficit := *minWidth - visibleWidth(s)
	if deficit <= 0 {
		return s
	}
	pad := strings.Repeat(" ", deficit)
	if align == "right" {
		return pad + s
	}
	return s + pad
}
