// Unit coverage for previewMatch.ts's three exports against a fabricated
// preview response shaped like the backend's allVariants:true response
// (DSL_API.md "allVariants"): a top-level line L0.0 plus a responsive L0.1
// whose BOTH variants (v0, v1) contributed lines, node IDs assigned exactly
// as the backend does (buildAST / astjson.go "Node IDs").

import { describe, expect, it } from "vitest";
import { effectiveLines, matchPreview } from "./previewMatch.ts";
import type { PreviewLine } from "./types.ts";

function line(nodeId: string, text = "x"): PreviewLine {
    return {
        omitted: false,
        ansi: text,
        segments: [{ nodeId, text, ansi: text, visible: true }],
    };
}

// L0.0: top-level line. L0.1: responsive with variant 0 (line L0.1.v0.0) and
// variant 1 (line L0.1.v1.0) — both present, as an allVariants response
// carries every variant's lines regardless of which one width-selects.
const topLevel = line("L0.0.0", "model");
const variant0Line = line("L0.1.v0.0.0", "wide");
const variant1Line = line("L0.1.v1.0.0", "narrow");
const allLines = [topLevel, variant0Line, variant1Line];

describe("matchPreview", () => {
    it("returns an empty match for a null response", () => {
        const match = matchPreview(null, null);
        expect(match.byLineId.size).toBe(0);
        expect(match.selectedVariant.size).toBe(0);
    });

    it("byLineId covers every variant's line, not just the selected one", () => {
        const match = matchPreview(allLines, { "L0.1": 0 });
        expect(match.byLineId.get("L0.0")).toBe(topLevel);
        expect(match.byLineId.get("L0.1.v0.0")).toBe(variant0Line);
        expect(match.byLineId.get("L0.1.v1.0")).toBe(variant1Line);
    });

    it("selectedVariant reflects the explicit selectedVariants argument verbatim", () => {
        const withV0 = matchPreview(allLines, { "L0.1": 0 });
        expect(withV0.selectedVariant.get("L0.1")).toBe(0);

        const withV1 = matchPreview(allLines, { "L0.1": 1 });
        expect(withV1.selectedVariant.get("L0.1")).toBe(1);
    });

    it("selectedVariant is empty when the response carries no selection yet", () => {
        const match = matchPreview(allLines, null);
        expect(match.selectedVariant.size).toBe(0);
        // byLineId is unaffected — it never depended on selection.
        expect(match.byLineId.get("L0.1.v0.0")).toBe(variant0Line);
    });
});

describe("effectiveLines", () => {
    it("keeps top-level lines and only the selected variant's lines", () => {
        expect(effectiveLines(allLines, { "L0.1": 0 })).toEqual([topLevel, variant0Line]);
        expect(effectiveLines(allLines, { "L0.1": 1 })).toEqual([topLevel, variant1Line]);
    });

    it("falls back to every line when selectedVariants is null or undefined", () => {
        expect(effectiveLines(allLines, null)).toEqual(allLines);
        expect(effectiveLines(allLines, undefined)).toEqual(allLines);
    });

    it("returns an empty array for a null preview response", () => {
        expect(effectiveLines(null, { "L0.1": 0 })).toEqual([]);
    });
});
