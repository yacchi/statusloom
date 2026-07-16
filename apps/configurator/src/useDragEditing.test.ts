// Unit coverage for resolveGeometricTarget's span into/beside resolution
// (useDragEditing.ts). This is the regression guard for the bug where an
// empty span's tiny ghost-label rect left almost no "into" zone (a fixed
// beside margin ate most/all of the width), and a populated span's beside
// margin (30% per edge) left only a 40%-wide into zone. The fix: an empty
// span always resolves into (no beside at all), and a populated span's
// margin shrinks to 20% per edge so into keeps >= 60% of the span.

import { describe, expect, it } from "vitest";
import { resolveGeometricTarget } from "./useDragEditing.ts";
import type { ChipDragPayload, ContainerView } from "./useDragEditing.ts";

// A non-family-restricted "node" payload (category omitted -> "any"), so
// acceptsCategory/self-subtree checks never interfere with the geometry this
// suite is exercising. `id` never matches any container id used below.
const PAYLOAD: ChipDragPayload = { kind: "node", id: "dragged-x", nodeKind: "field" };

function rects(entries: Record<string, { left: number; width: number }>): Map<string, { left: number; width: number }> {
    return new Map(Object.entries(entries));
}

describe("resolveGeometricTarget", () => {
    it("resolves an empty span to INTO even when the pointer sits at its left edge (old margin zone)", () => {
        const containers: ContainerView[] = [
            { id: "L0", kind: "line", lineIndex: 0, childIds: ["L0.0"] },
            { id: "L0.0", kind: "span", childIds: [] },
        ];
        const r = rects({ "L0.0": { left: 100, width: 40 } });
        // Old margin: min(14, 40*0.3=12) = 12 -> beside zone was [100,112]; 102
        // sits inside it. The fix must ignore beside entirely for an empty span.
        const target = resolveGeometricTarget(containers, "L0.0", PAYLOAD, 102, r);
        expect(target).toEqual({ containerId: "L0.0", index: 0 });
    });

    it("resolves an empty span to INTO even when the pointer sits at its right edge (old margin zone)", () => {
        const containers: ContainerView[] = [
            { id: "L0", kind: "line", lineIndex: 0, childIds: ["L0.0"] },
            { id: "L0.0", kind: "span", childIds: [] },
        ];
        const r = rects({ "L0.0": { left: 100, width: 40 } });
        // Old margin: [128,140]; 138 sits inside it.
        const target = resolveGeometricTarget(containers, "L0.0", PAYLOAD, 138, r);
        expect(target).toEqual({ containerId: "L0.0", index: 0 });
    });

    it("resolves a populated span's CENTER to INTO, at the nearest child gap", () => {
        const containers: ContainerView[] = [
            { id: "L0", kind: "line", lineIndex: 0, childIds: ["L0.0"] },
            { id: "L0.0", kind: "span", childIds: ["L0.0.0", "L0.0.1"] },
        ];
        const r = rects({
            "L0.0": { left: 100, width: 100 },
            "L0.0.0": { left: 100, width: 50 }, // center 125
            "L0.0.1": { left: 150, width: 50 }, // center 175
        });
        const target = resolveGeometricTarget(containers, "L0.0", PAYLOAD, 150, r);
        expect(target).toEqual({ containerId: "L0.0", index: 1 });
    });

    it("resolves a populated span's point just inside the new (narrower) margin to INTO, though it was BESIDE under the old 30% margin", () => {
        const containers: ContainerView[] = [
            { id: "L0", kind: "line", lineIndex: 0, childIds: ["L0.0"] },
            { id: "L0.0", kind: "span", childIds: ["L0.0.0", "L0.0.1"] },
        ];
        const r = rects({
            "L0.0": { left: 100, width: 100 },
            "L0.0.0": { left: 100, width: 50 },
            "L0.0.1": { left: 150, width: 50 },
        });
        // New margin: min(14, 100*0.2=20) = 14 -> into zone is [114,186]. 120 is
        // inside it, but the old margin (30 -> beside zone [100,130]) would
        // have resolved this same point to BESIDE.
        const target = resolveGeometricTarget(containers, "L0.0", PAYLOAD, 120, r);
        expect(target).toEqual({ containerId: "L0.0", index: 0 });
    });

    it("resolves a populated span's left-edge point (inside the new margin) to BESIDE, before the span in the parent line", () => {
        const containers: ContainerView[] = [
            { id: "L0", kind: "line", lineIndex: 0, childIds: ["L0.0"] },
            { id: "L0.0", kind: "span", childIds: ["L0.0.0", "L0.0.1"] },
        ];
        const r = rects({
            "L0.0": { left: 100, width: 100 },
            "L0.0.0": { left: 100, width: 50 },
            "L0.0.1": { left: 150, width: 50 },
        });
        // margin = 14 -> beside zone is pointerX <= 114.
        const target = resolveGeometricTarget(containers, "L0.0", PAYLOAD, 105, r);
        expect(target).toEqual({ containerId: "L0", index: 0 });
    });

    it("resolves a populated span's right-edge point (inside the new margin) to BESIDE, after the span in the parent line", () => {
        const containers: ContainerView[] = [
            { id: "L0", kind: "line", lineIndex: 0, childIds: ["L0.0"] },
            { id: "L0.0", kind: "span", childIds: ["L0.0.0", "L0.0.1"] },
        ];
        const r = rects({
            "L0.0": { left: 100, width: 100 },
            "L0.0.0": { left: 100, width: 50 },
            "L0.0.1": { left: 150, width: 50 },
        });
        // margin = 14 -> beside zone is pointerX >= 186.
        const target = resolveGeometricTarget(containers, "L0.0", PAYLOAD, 190, r);
        expect(target).toEqual({ containerId: "L0", index: 1 });
    });
});
