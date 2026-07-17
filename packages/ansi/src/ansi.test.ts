import { describe, expect, it } from "vitest";
import { parseAnsiLine, paletteFor, xterm256, type Span } from "./ansi.ts";

const ESC = "\x1b[";

const dark = paletteFor("dark");

interface Case {
    name: string;
    input: string;
    expected: Span[];
}

const cases: Case[] = [
    {
        name: "plain text (no escapes)",
        input: "hello",
        expected: [{ text: "hello", color: undefined, bold: false }],
    },
    {
        name: "named foreground color (cyan / 36)",
        input: `${ESC}36mcyan${ESC}0m`,
        expected: [{ text: "cyan", color: dark.cyan, bold: false }],
    },
    {
        name: "Powerline foreground and background colors",
        input: `${ESC}34;42m\ue0b0${ESC}0m`,
        expected: [
            {
                text: "\ue0b0",
                color: dark.blue,
                background: dark.green,
                bold: false,
            },
        ],
    },
    {
        name: "bright color (91 = brightRed)",
        input: `${ESC}91mred`,
        expected: [{ text: "red", color: dark.brightRed, bold: false }],
    },
    {
        name: "bold then reset",
        input: `${ESC}1mB${ESC}0mN`,
        expected: [
            { text: "B", color: undefined, bold: true },
            { text: "N", color: undefined, bold: false },
        ],
    },
    {
        name: "combined bold + color in one SGR",
        input: `${ESC}1;32mgo`,
        expected: [{ text: "go", color: dark.green, bold: true }],
    },
    {
        name: "256-color cube",
        input: `${ESC}38;5;196mX`,
        expected: [{ text: "X", color: xterm256(196, dark), bold: false }],
    },
    {
        name: "256-color mapping to base 16",
        input: `${ESC}38;5;1mX`,
        expected: [{ text: "X", color: dark.red, bold: false }],
    },
    {
        name: "truecolor",
        input: `${ESC}38;2;10;20;30mT`,
        expected: [{ text: "T", color: "#0a141e", bold: false }],
    },
    {
        name: "unknown code is ignored (7 = reverse)",
        input: `${ESC}7mplain`,
        expected: [{ text: "plain", color: undefined, bold: false }],
    },
    {
        name: "unknown code mixed with known (7;33)",
        input: `${ESC}7;33mY`,
        expected: [{ text: "Y", color: dark.yellow, bold: false }],
    },
    {
        name: "empty SGR params (ESC[m) is a reset",
        input: `${ESC}33mY${ESC}mZ`,
        expected: [
            { text: "Y", color: dark.yellow, bold: false },
            { text: "Z", color: undefined, bold: false },
        ],
    },
    {
        name: "mixed segments",
        input: `${ESC}36mmodel${ESC}0m | ${ESC}35mmain`,
        expected: [
            { text: "model", color: dark.cyan, bold: false },
            { text: " | ", color: undefined, bold: false },
            { text: "main", color: dark.magenta, bold: false },
        ],
    },
    {
        name: "color persists across text until reset",
        input: `${ESC}31ma${ESC}1mb`,
        expected: [
            { text: "a", color: dark.red, bold: false },
            { text: "b", color: dark.red, bold: true },
        ],
    },
];

describe("parseAnsiLine", () => {
    for (const c of cases) {
        it(c.name, () => {
            expect(parseAnsiLine(c.input, "dark")).toEqual(c.expected);
        });
    }

    it("uses the light palette when theme is light", () => {
        const light = paletteFor("light");
        expect(parseAnsiLine(`${ESC}32mg`, "light")).toEqual([
            { text: "g", color: light.green, bold: false },
        ]);
    });

    it("skips empty text segments between adjacent escapes", () => {
        const spans = parseAnsiLine(`${ESC}31m${ESC}32mg`, "dark");
        expect(spans).toEqual([{ text: "g", color: dark.green, bold: false }]);
    });
});

const OSC = "\x1b]"; // OSC introducer (ESC ])
const BEL = "\x07";
const ST = "\x1b\\"; // string terminator (ESC \)

describe("parseAnsiLine OSC 8 hyperlinks", () => {
    it("attaches the link to text inside an OSC 8 open/close pair (BEL terminated)", () => {
        const url = "https://github.com/yacchi/statusloom/pull/1234";
        const input = `${OSC}8;;${url}${BEL}PR #1234${OSC}8;;${BEL}`;
        expect(parseAnsiLine(input, "dark")).toEqual([
            { text: "PR #1234", color: undefined, bold: false, link: url },
        ]);
    });

    it("accepts the ST (ESC \\) terminator as well as BEL", () => {
        const url = "https://example.com";
        const input = `${OSC}8;;${url}${ST}link${OSC}8;;${ST}`;
        expect(parseAnsiLine(input, "dark")).toEqual([
            { text: "link", color: undefined, bold: false, link: url },
        ]);
    });

    it("combines a link with SGR color inside it", () => {
        const url = "https://github.com/yacchi/statusloom";
        const input = `${OSC}8;;${url}${BEL}${ESC}34myacchi/statusloom${ESC}0m${OSC}8;;${BEL}`;
        expect(parseAnsiLine(input, "dark")).toEqual([
            { text: "yacchi/statusloom", color: dark.blue, bold: false, link: url },
        ]);
    });

    it("only links text between open and close; surrounding text is unlinked", () => {
        const url = "https://example.com";
        const input = `a${OSC}8;;${url}${BEL}b${OSC}8;;${BEL}c`;
        expect(parseAnsiLine(input, "dark")).toEqual([
            { text: "a", color: undefined, bold: false },
            { text: "b", color: undefined, bold: false, link: url },
            { text: "c", color: undefined, bold: false },
        ]);
    });

    it("keeps the link open to end of line when the close is missing", () => {
        const url = "https://example.com";
        const input = `${OSC}8;;${url}${BEL}dangling`;
        expect(parseAnsiLine(input, "dark")).toEqual([
            { text: "dangling", color: undefined, bold: false, link: url },
        ]);
    });

    it("ignores non-8 OSC sequences without leaking them into the text", () => {
        // OSC 0 (window title) must be consumed but produce no visible text.
        const input = `${OSC}0;window title${BEL}visible`;
        expect(parseAnsiLine(input, "dark")).toEqual([
            { text: "visible", color: undefined, bold: false },
        ]);
    });

    it("preserves ';' characters within the linked URL", () => {
        const url = "https://example.com/a;b;c";
        const input = `${OSC}8;;${url}${BEL}x${OSC}8;;${BEL}`;
        expect(parseAnsiLine(input, "dark")).toEqual([
            { text: "x", color: undefined, bold: false, link: url },
        ]);
    });
});

describe("xterm256", () => {
    it("returns base palette entries for 0-15", () => {
        expect(xterm256(0, dark)).toBe(dark.black);
        expect(xterm256(15, dark)).toBe(dark.brightWhite);
    });

    it("computes the grayscale ramp", () => {
        expect(xterm256(232, dark)).toBe("#080808");
        expect(xterm256(255, dark)).toBe("#eeeeee");
    });

    it("computes a cube corner (231 = white)", () => {
        expect(xterm256(231, dark)).toBe("#ffffff");
    });

    it("returns undefined for out-of-range indices", () => {
        expect(xterm256(-1, dark)).toBeUndefined();
        expect(xterm256(256, dark)).toBeUndefined();
    });
});
