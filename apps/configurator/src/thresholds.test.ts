import { describe, expect, it } from "vitest";
import {
    addBreakpoint,
    type Band,
    buildColorRules,
    moveBreakpoint,
    parseThresholdBands,
    removeBreakpoint,
    setBandColor,
} from "./thresholds.ts";
import type { ColorRuleNode, FieldNode } from "./types.ts";

function rule(when: string, color?: string): ColorRuleNode {
    return { id: "", kind: "color-rule", when, color };
}

function field(color: string | undefined, colorRules?: ColorRuleNode[]): FieldNode {
    return { id: "F", kind: "field", name: "context-percentage", color, colorRules };
}

describe("parseThresholdBands / buildColorRules round trip", () => {
    it("parses a descending ge-threshold pattern into ascending bands", () => {
        const node = field("green", [rule("self ge 80", "red"), rule("self ge 60", "yellow")]);
        expect(parseThresholdBands(node)).toEqual([
            { from: 0, color: "green" },
            { from: 60, color: "yellow" },
            { from: 80, color: "red" },
        ]);
    });

    it("treats a field with no color-rules as a single base band", () => {
        expect(parseThresholdBands(field("blue"))).toEqual([{ from: 0, color: "blue" }]);
        expect(parseThresholdBands(field(undefined))).toEqual([{ from: 0, color: undefined }]);
    });

    it("builds descending `self ge N` rules from ascending bands", () => {
        const bands: Band[] = [
            { from: 0, color: "green" },
            { from: 60, color: "yellow" },
            { from: 80, color: "red" },
        ];
        expect(buildColorRules(bands)).toEqual({
            color: "green",
            colorRules: [
                { id: "", kind: "color-rule", when: "self ge 80", color: "red" },
                { id: "", kind: "color-rule", when: "self ge 60", color: "yellow" },
            ],
        });
    });

    it("round-trips bands -> rules -> bands", () => {
        const bands: Band[] = [
            { from: 0, color: undefined },
            { from: 50, color: "yellow" },
            { from: 90, color: "red" },
        ];
        const built = buildColorRules(bands);
        const back = parseThresholdBands(field(built.color, built.colorRules));
        expect(back).toEqual(bands);
    });

    it("accepts `gt` on the way in but normalizes to `ge` on the way out", () => {
        const node = field("green", [rule("self gt 80", "red")]);
        const bands = parseThresholdBands(node);
        expect(bands).toEqual([
            { from: 0, color: "green" },
            { from: 80, color: "red" },
        ]);
        expect(buildColorRules(bands!).colorRules[0].when).toBe("self ge 80");
    });
});

describe("parseThresholdBands fallback (returns null -> Advanced editor)", () => {
    it("rejects compound conditions", () => {
        expect(parseThresholdBands(field("green", [rule("self ge 80 and git-dirty", "red")]))).toBeNull();
    });

    it("rejects named metrics", () => {
        expect(parseThresholdBands(field("green", [rule("context-percent ge 80", "red")]))).toBeNull();
    });

    it("rejects operators other than ge / gt", () => {
        expect(parseThresholdBands(field("green", [rule("self lt 20", "red")]))).toBeNull();
        expect(parseThresholdBands(field("green", [rule("self eq 50", "red")]))).toBeNull();
    });

    it("rejects non-descending thresholds", () => {
        expect(
            parseThresholdBands(field("green", [rule("self ge 60", "yellow"), rule("self ge 80", "red")])),
        ).toBeNull();
    });

    it("rejects duplicate thresholds", () => {
        expect(
            parseThresholdBands(field("green", [rule("self ge 60", "yellow"), rule("self ge 60", "red")])),
        ).toBeNull();
    });

    it("rejects thresholds outside (0, 100)", () => {
        expect(parseThresholdBands(field("green", [rule("self ge 0", "red")]))).toBeNull();
        expect(parseThresholdBands(field("green", [rule("self ge 100", "red")]))).toBeNull();
    });

    it("rejects negation / parentheses", () => {
        expect(parseThresholdBands(field("green", [rule("not (self ge 80)", "red")]))).toBeNull();
    });
});

describe("band editing operations", () => {
    const base: Band[] = [
        { from: 0, color: "green" },
        { from: 80, color: "red" },
    ];

    it("adds a breakpoint that inherits the split band's color and opens its index", () => {
        const { bands, index } = addBreakpoint(base, 60);
        expect(index).toBe(1);
        expect(bands).toEqual([
            { from: 0, color: "green" },
            { from: 60, color: "green" },
            { from: 80, color: "red" },
        ]);
    });

    it("rejects a breakpoint that collides with an existing one or an edge", () => {
        expect(addBreakpoint(base, 80).index).toBe(-1);
        expect(addBreakpoint(base, 0).index).toBe(-1);
        expect(addBreakpoint(base, 100).index).toBe(-1);
    });

    it("clamps a moved breakpoint strictly between its neighbors", () => {
        const bands: Band[] = [
            { from: 0, color: "green" },
            { from: 50, color: "yellow" },
            { from: 80, color: "red" },
        ];
        // Try to drag the middle handle past the top one.
        expect(moveBreakpoint(bands, 1, 95)[1].from).toBe(79);
        // ...and past the base edge.
        expect(moveBreakpoint(bands, 1, -10)[1].from).toBe(1);
    });

    it("removes a breakpoint but never the base band", () => {
        expect(removeBreakpoint(base, 1)).toEqual([{ from: 0, color: "green" }]);
        expect(removeBreakpoint(base, 0)).toEqual(base);
    });

    it("recolors any band including the base", () => {
        expect(setBandColor(base, 0, "cyan")[0].color).toBe("cyan");
        expect(setBandColor(base, 1, undefined)[1].color).toBeUndefined();
    });
});
