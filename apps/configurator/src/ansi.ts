// ANSI SGR (Select Graphic Rendition) -> span converter.
//
// The Go renderer is the source of truth for layout and emits raw ANSI SGR
// escapes. This module only translates those escapes into styled spans for the
// preview <pre>; it never reimplements any rendering/layout logic.
//
// Supported codes: 0 (reset), 1 (bold), foreground/background ANSI16,
// 256-color, and truecolor. Unknown codes are ignored.
//
// OSC 8 hyperlinks are also understood: the Go renderer may wrap a widget's
// styled text in `ESC ] 8 ; ; URL (BEL|ST)` … `ESC ] 8 ; ; (BEL|ST)`. Text
// between an open and its close carries `link`, so the preview can render it
// as an <a>. Any other OSC sequence is consumed and ignored (never leaked
// into the visible text).

export type Theme = "dark" | "light";

export interface Span {
    text: string;
    color?: string;
    background?: string;
    bold: boolean;
    // OSC 8 hyperlink target for this span; absent when the span is not linked.
    link?: string;
}

// The 16 ANSI color names, in SGR order (0-7 normal, 8-15 bright).
export const ANSI_COLOR_NAMES = [
    "black",
    "red",
    "green",
    "yellow",
    "blue",
    "magenta",
    "cyan",
    "white",
    "brightBlack",
    "brightRed",
    "brightGreen",
    "brightYellow",
    "brightBlue",
    "brightMagenta",
    "brightCyan",
    "brightWhite",
] as const;

export type AnsiColorName = (typeof ANSI_COLOR_NAMES)[number];

// Palette for colors displayed on a dark background (VS Code dark theme).
const PALETTE_DARK: Record<AnsiColorName, string> = {
    black: "#000000",
    red: "#cd3131",
    green: "#0dbc79",
    yellow: "#e5e510",
    blue: "#2472c8",
    magenta: "#bc3fbc",
    cyan: "#11a8cd",
    white: "#e5e5e5",
    brightBlack: "#666666",
    brightRed: "#f14c4c",
    brightGreen: "#23d18b",
    brightYellow: "#f5f543",
    brightBlue: "#3b8eea",
    brightMagenta: "#d670d6",
    brightCyan: "#29b8db",
    brightWhite: "#ffffff",
};

// Palette for colors displayed on a light background (VS Code light theme).
const PALETTE_LIGHT: Record<AnsiColorName, string> = {
    black: "#000000",
    red: "#cd3131",
    green: "#00bc00",
    yellow: "#949800",
    blue: "#0451a5",
    magenta: "#bc05bc",
    cyan: "#0598bc",
    white: "#555555",
    brightBlack: "#666666",
    brightRed: "#cd3131",
    brightGreen: "#14ce14",
    brightYellow: "#b5ba00",
    brightBlue: "#0451a5",
    brightMagenta: "#bc05bc",
    brightCyan: "#0598bc",
    brightWhite: "#a5a5a5",
};

export function paletteFor(theme: Theme): Record<AnsiColorName, string> {
    return theme === "light" ? PALETTE_LIGHT : PALETTE_DARK;
}

function toHex(n: number): string {
    const v = Math.max(0, Math.min(255, Math.round(n)));
    return v.toString(16).padStart(2, "0");
}

function rgbHex(r: number, g: number, b: number): string {
    return `#${toHex(r)}${toHex(g)}${toHex(b)}`;
}

// Standard xterm-256 palette entry for index n.
export function xterm256(n: number, palette: Record<AnsiColorName, string>): string | undefined {
    if (n < 0 || n > 255) {
        return undefined;
    }
    if (n < 16) {
        return palette[ANSI_COLOR_NAMES[n]];
    }
    if (n < 232) {
        // 6x6x6 color cube.
        const i = n - 16;
        const r = Math.floor(i / 36);
        const g = Math.floor((i % 36) / 6);
        const b = i % 6;
        const level = (v: number): number => (v === 0 ? 0 : 55 + v * 40);
        return rgbHex(level(r), level(g), level(b));
    }
    // 24-step grayscale ramp.
    const v = 8 + (n - 232) * 10;
    return rgbHex(v, v, v);
}

interface SgrState {
    color?: string;
    background?: string;
    bold: boolean;
}

function applySgr(codes: number[], state: SgrState, palette: Record<AnsiColorName, string>): void {
    // An empty parameter list (ESC[m) is equivalent to a reset.
    const list = codes.length === 0 ? [0] : codes;
    let i = 0;
    while (i < list.length) {
        const c = list[i];
        if (c === 0) {
            state.color = undefined;
            state.background = undefined;
            state.bold = false;
            i += 1;
        } else if (c === 1) {
            state.bold = true;
            i += 1;
        } else if (c >= 30 && c <= 37) {
            state.color = palette[ANSI_COLOR_NAMES[c - 30]];
            i += 1;
        } else if (c >= 90 && c <= 97) {
            state.color = palette[ANSI_COLOR_NAMES[8 + (c - 90)]];
            i += 1;
        } else if (c >= 40 && c <= 47) {
            state.background = palette[ANSI_COLOR_NAMES[c - 40]];
            i += 1;
        } else if (c >= 100 && c <= 107) {
            state.background = palette[ANSI_COLOR_NAMES[8 + (c - 100)]];
            i += 1;
        } else if (c === 38 || c === 48) {
            const target = c === 38 ? "color" : "background";
            const mode = list[i + 1];
            if (mode === 5) {
                state[target] = xterm256(list[i + 2], palette);
                i += 3;
            } else if (mode === 2) {
                state[target] = rgbHex(list[i + 2], list[i + 3], list[i + 4]);
                i += 5;
            } else {
                // Malformed 38 sequence; skip just this code.
                i += 1;
            }
        } else {
            // Unknown / unsupported code: ignore it.
            i += 1;
        }
    }
}

// Matches either an SGR escape (ESC [ <params> m) or any OSC sequence
// (ESC ] <body> terminated by BEL or ST = ESC \). The OSC body is captured
// non-greedily so back-to-back sequences don't merge.
const ANSI_RE = /\x1b\[([0-9;]*)m|\x1b\]([\s\S]*?)(?:\x07|\x1b\\)/g;

// applyOsc updates the current hyperlink from an OSC body (the bytes between
// `ESC ]` and the terminator). Only OSC 8 (`8;params;URI`) is meaningful:
// a non-empty URI opens a link, an empty URI closes it. Every other OSC
// sequence is ignored.
function applyOsc(body: string, state: LinkState): void {
    if (!body.startsWith("8;")) {
        return;
    }
    // Skip the "8" and its params field to reach the URI (everything after
    // the second ';'). The URI itself may contain ';'.
    const secondSemi = body.indexOf(";", 2);
    const uri = secondSemi >= 0 ? body.slice(secondSemi + 1) : "";
    state.link = uri === "" ? undefined : uri;
}

interface LinkState {
    link?: string;
}

// Parse a single line (no newline) containing ANSI SGR escapes and OSC 8
// hyperlinks into spans.
export function parseAnsiLine(line: string, theme: Theme): Span[] {
    const palette = paletteFor(theme);
    const state: SgrState = { bold: false };
    const linkState: LinkState = {};
    const spans: Span[] = [];

    let lastIndex = 0;
    ANSI_RE.lastIndex = 0;
    let match: RegExpExecArray | null;

    const pushText = (text: string): void => {
        if (text.length === 0) {
            return;
        }
        spans.push({
            text,
            color: state.color,
            background: state.background,
            bold: state.bold,
            link: linkState.link,
        });
    };

    while ((match = ANSI_RE.exec(line)) !== null) {
        pushText(line.slice(lastIndex, match.index));
        if (match[2] !== undefined) {
            // OSC sequence (link open/close or an ignored sequence).
            applyOsc(match[2], linkState);
        } else {
            // SGR sequence.
            const params =
                match[1] === "" ? [] : match[1].split(";").map((p) => (p === "" ? 0 : Number(p)));
            applySgr(params, state, palette);
        }
        lastIndex = ANSI_RE.lastIndex;
    }
    pushText(line.slice(lastIndex));

    return spans;
}

// Convenience: parse an array of lines.
export function parseAnsiLines(lines: string[], theme: Theme): Span[][] {
    return lines.map((line) => parseAnsiLine(line, theme));
}
