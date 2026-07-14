package render

import (
	"math"
	"strconv"
	"strings"
)

// colorLevel is the maximum color depth the terminal is expected to
// support. Higher values are supersets of lower ones.
type colorLevel int

const (
	levelNone colorLevel = iota
	levelAnsi16
	levelAnsi256
	levelTruecolor
)

// parseColorLevel maps a config ColorLevel string to a colorLevel.
// Empty or unrecognized values are treated as levelNone (no escapes),
// so an unset ColorLevel never accidentally emits color.
func parseColorLevel(s string) colorLevel {
	switch s {
	case "ansi16":
		return levelAnsi16
	case "ansi256":
		return levelAnsi256
	case "truecolor":
		return levelTruecolor
	default:
		return levelNone
	}
}

// colorKind identifies how a color is represented.
type colorKind int

const (
	ckNone colorKind = iota
	ckAnsi16
	ckAnsi256
	ckTruecolor
)

// color is a parsed, level-agnostic color value.
type color struct {
	kind    colorKind
	idx     int // ansi16: 0-15; ansi256: 0-255
	r, g, b uint8
}

// ansi16Names maps configuration color names to ANSI 16-color indices.
//
// The camelCase keys are the legacy config.json names; the kebab-case
// "bright-*" aliases are the DSL color names (markup.md uses e.g.
// "bright-black"). Both resolve to the same index during the transition.
// TODO(phase-2): drop the camelCase keys once the legacy config.json /
// WidgetSpec path is removed and the DSL is the sole configuration source.
var ansi16Names = map[string]int{
	"black":          0,
	"red":            1,
	"green":          2,
	"yellow":         3,
	"blue":           4,
	"magenta":        5,
	"cyan":           6,
	"white":          7,
	"brightBlack":    8,
	"brightRed":      9,
	"brightGreen":    10,
	"brightYellow":   11,
	"brightBlue":     12,
	"brightMagenta":  13,
	"brightCyan":     14,
	"brightWhite":    15,
	"bright-black":   8,
	"bright-red":     9,
	"bright-green":   10,
	"bright-yellow":  11,
	"bright-blue":    12,
	"bright-magenta": 13,
	"bright-cyan":    14,
	"bright-white":   15,
}

// ansi16RGB is the RGB value of each ANSI 16-color index (standard
// VGA/xterm palette), used for nearest-color downgrade math.
var ansi16RGB = [16][3]uint8{
	{0, 0, 0},       // black
	{170, 0, 0},     // red
	{0, 170, 0},     // green
	{170, 85, 0},    // yellow
	{0, 0, 170},     // blue
	{170, 0, 170},   // magenta
	{0, 170, 170},   // cyan
	{170, 170, 170}, // white
	{85, 85, 85},    // brightBlack
	{255, 85, 85},   // brightRed
	{85, 255, 85},   // brightGreen
	{255, 255, 85},  // brightYellow
	{85, 85, 255},   // brightBlue
	{255, 85, 255},  // brightMagenta
	{85, 255, 255},  // brightCyan
	{255, 255, 255}, // brightWhite
}

// parseColor parses a configuration color value. Unknown / malformed
// values yield ckNone so rendering stays uncolored rather than erroring.
func parseColor(s string) color {
	if s == "" {
		return color{kind: ckNone}
	}
	if idx, ok := ansi16Names[s]; ok {
		return color{kind: ckAnsi16, idx: idx}
	}
	if strings.HasPrefix(s, "ansi256:") {
		n, err := strconv.Atoi(s[len("ansi256:"):])
		if err == nil && n >= 0 && n <= 255 {
			return color{kind: ckAnsi256, idx: n}
		}
		return color{kind: ckNone}
	}
	if strings.HasPrefix(s, "#") && len(s) == 7 {
		if r, g, b, ok := parseHex(s); ok {
			return color{kind: ckTruecolor, r: r, g: g, b: b}
		}
	}
	return color{kind: ckNone}
}

// parseHex parses a "#rrggbb" string into RGB components.
func parseHex(s string) (r, g, b uint8, ok bool) {
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), true
}

// downgrade reduces a color to the highest representation the given
// level supports: truecolor -> nearest ansi256 -> nearest ansi16.
func downgrade(c color, level colorLevel) color {
	switch c.kind {
	case ckAnsi16:
		return c // representable at every non-none level
	case ckAnsi256:
		if level >= levelAnsi256 {
			return c
		}
		return color{kind: ckAnsi16, idx: ansi256ToAnsi16(c.idx)}
	case ckTruecolor:
		if level >= levelTruecolor {
			return c
		}
		idx256 := rgbToAnsi256(c.r, c.g, c.b)
		if level >= levelAnsi256 {
			return color{kind: ckAnsi256, idx: idx256}
		}
		return color{kind: ckAnsi16, idx: ansi256ToAnsi16(idx256)}
	default:
		return color{kind: ckNone}
	}
}

// rgbToAnsi256 maps an RGB triple to the nearest xterm 256-color index
// using the 6x6x6 color cube plus the grayscale ramp.
func rgbToAnsi256(r, g, b uint8) int {
	if r == g && g == b {
		switch {
		case r < 8:
			return 16
		case r > 248:
			return 231
		default:
			return int(math.Round((float64(r)-8)/247*24)) + 232
		}
	}
	ri := int(math.Round(float64(r) / 255 * 5))
	gi := int(math.Round(float64(g) / 255 * 5))
	bi := int(math.Round(float64(b) / 255 * 5))
	return 16 + 36*ri + 6*gi + bi
}

// ansi256ToRGB returns the RGB value of an xterm 256-color index.
func ansi256ToRGB(idx int) (uint8, uint8, uint8) {
	switch {
	case idx < 16:
		c := ansi16RGB[idx]
		return c[0], c[1], c[2]
	case idx >= 232:
		gray := uint8(8 + (idx-232)*10)
		return gray, gray, gray
	default:
		i := idx - 16
		r := i / 36
		g := (i / 6) % 6
		b := i % 6
		return cubeComponent(r), cubeComponent(g), cubeComponent(b)
	}
}

// cubeComponent maps a 0-5 cube coordinate to its 8-bit channel value.
func cubeComponent(c int) uint8 {
	if c == 0 {
		return 0
	}
	return uint8(55 + 40*c)
}

// ansi256ToAnsi16 maps a 256-color index to the nearest ANSI 16 color.
func ansi256ToAnsi16(idx int) int {
	if idx < 16 {
		return idx
	}
	r, g, b := ansi256ToRGB(idx)
	best := 0
	bestDist := math.MaxFloat64
	for i, c := range ansi16RGB {
		dr := float64(r) - float64(c[0])
		dg := float64(g) - float64(c[1])
		db := float64(b) - float64(c[2])
		dist := dr*dr + dg*dg + db*db
		if dist < bestDist {
			bestDist = dist
			best = i
		}
	}
	return best
}

// sgrColorParam returns the SGR foreground parameter(s) for a color, or
// "" when the color carries no visible attribute.
func sgrColorParam(c color) string {
	switch c.kind {
	case ckAnsi16:
		if c.idx < 8 {
			return strconv.Itoa(30 + c.idx)
		}
		return strconv.Itoa(90 + (c.idx - 8))
	case ckAnsi256:
		return "38;5;" + strconv.Itoa(c.idx)
	case ckTruecolor:
		return "38;2;" + strconv.Itoa(int(c.r)) + ";" +
			strconv.Itoa(int(c.g)) + ";" + strconv.Itoa(int(c.b))
	default:
		return ""
	}
}
