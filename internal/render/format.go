package render

import (
	"math"
	"strconv"
	"strings"
	"time"
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

// visibleWidth returns the number of printable runes in s, ignoring ANSI
// escape sequences (CSI sequences introduced by ESC "[").
//
// East Asian double-width characters are out of scope: every printable
// rune is counted as width 1.
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
		_, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		w++
		i += size
	}
	return w
}
