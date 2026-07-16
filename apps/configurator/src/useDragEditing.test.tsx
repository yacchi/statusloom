import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { DragEndEvent, DragOverEvent, DragStartEvent } from "@dnd-kit/core";
import {
    LINE_ID_PREFIX,
    PALETTE_ID_PREFIX,
    SUBAGENT_LINE_ID_PREFIX,
    VARIANT_CONTAINER_PREFIX,
    VARIANT_ID_PREFIX,
    computeDropTarget,
    resolveGeometricTarget,
    useDragEditing,
    type ContainerView,
    type DragPayload,
    type DropTarget,
    type VariantListView,
} from "./useDragEditing.ts";
import type { LineChild } from "./types.ts";

// Two rows: L0.0 with two chips, L0.1 with one.
function lines(): ContainerView[] {
    return [
        { id: "L0.0", kind: "line", lineIndex: 0, childIds: ["L0.0.0", "L0.0.1"] },
        { id: "L0.1", kind: "line", lineIndex: 1, childIds: ["L0.1.0"] },
    ];
}

// ---- fabricated dnd-kit events (only the fields the hook reads) ----

function activeObj(id: string, centerLeft: number | null, centerTop: number | null = null) {
    const hasRect = centerLeft !== null || centerTop !== null;
    return {
        id,
        rect: {
            current: {
                initial: null,
                translated: hasRect
                    ? {
                          left: (centerLeft ?? 0) - 5,
                          width: 10,
                          right: (centerLeft ?? 0) + 5,
                          // Vertical center parameterized independently so
                          // variant tests can drive the Y axis; defaults keep
                          // the original top:0/height:10 for X-only chip tests.
                          top: centerTop === null ? 0 : centerTop - 5,
                          bottom: centerTop === null ? 10 : centerTop + 5,
                          height: 10,
                      }
                    : null,
            },
        },
    };
}

function overObj(id: string, left: number, width: number, top = 0, height = 10) {
    return {
        id,
        rect: { left, width, top, right: left + width, bottom: top + height, height },
    };
}

function startEvent(activeId: string): DragStartEvent {
    return { active: activeObj(activeId, null) } as unknown as DragStartEvent;
}

interface EventOpts {
    activeCenter?: number;
    overLeft?: number;
    overWidth?: number;
    // Simulates a real pointer: the activator PointerEvent's clientX plus the
    // accumulated drag delta (defaults to 0). When set, the hook must use this
    // pointer X — not the dragged rect center — for before/after resolution.
    pointerClientX?: number;
    delta?: number;
    // Y-axis counterparts, used by variant-card reorder tests (a vertical
    // drag). Chip tests pass only the X opts, so their geometry is unchanged.
    activeCenterY?: number;
    overTop?: number;
    overHeight?: number;
    pointerClientY?: number;
    deltaY?: number;
}

function overEvent(
    activeId: string,
    overId: string | null,
    opts: EventOpts = {},
): DragOverEvent {
    const ev: Record<string, unknown> = {
        active: activeObj(activeId, opts.activeCenter ?? null, opts.activeCenterY ?? null),
        over:
            overId === null
                ? null
                : overObj(
                      overId,
                      opts.overLeft ?? 0,
                      opts.overWidth ?? 10,
                      opts.overTop ?? 0,
                      opts.overHeight ?? 10,
                  ),
    };
    if (opts.pointerClientX !== undefined || opts.pointerClientY !== undefined) {
        ev.activatorEvent = { clientX: opts.pointerClientX, clientY: opts.pointerClientY };
        ev.delta = { x: opts.delta ?? 0, y: opts.deltaY ?? 0 };
    }
    return ev as unknown as DragOverEvent;
}

function endEvent(
    activeId: string,
    overId: string | null,
    opts: EventOpts = {},
): DragEndEvent {
    return overEvent(activeId, overId, opts) as unknown as DragEndEvent;
}

// ---- hook harness ----

function setup(rows: ContainerView[] | null = lines()) {
    let renders = 0;
    const onDrop = vi.fn<(payload: DragPayload, target: DropTarget) => void>();
    const makePaletteNode = vi.fn((key: string) => {
        if (key === "preset:separator") {
            return {
                node: {
                    id: "",
                    kind: "text",
                    role: "separator",
                    value: "",
                } as LineChild,
                label: "Separator",
            };
        }
        if (key.startsWith("field:")) {
            const name = key.slice("field:".length);
            return {
                node: { id: "", kind: "field", name } as LineChild,
                label: name.toUpperCase(),
            };
        }
        return null;
    });
    const view = renderHook(() => {
        renders += 1;
        return useDragEditing({
            getContainers: () => rows,
            makePaletteNode,
            labelForNodeId: (id) => `node ${id}`,
            kindForNodeId: () => "text",
            onDrop,
        });
    });
    return { view, onDrop, makePaletteNode, renders: () => renders };
}

describe("computeDropTarget", () => {
    it("maps a row id to the end of that line", () => {
        expect(computeDropTarget(lines(), `${LINE_ID_PREFIX}0`, { kind: "palette", node: { id: "", kind: "text", value: "x" } }, null, null)).toEqual({
            containerId: "L0.0",
            index: 2,
        });
        expect(computeDropTarget(lines(), `${LINE_ID_PREFIX}1`, { kind: "palette", node: { id: "", kind: "text", value: "x" } }, null, null)).toEqual({
            containerId: "L0.1",
            index: 1,
        });
    });

    it("maps a chip id to before/after depending on the dragged center", () => {
        // over "L0.0.1", over rect [10, 20): center 15
        const payload = { kind: "palette", node: { id: "", kind: "text", value: "x" } } as const;
        expect(computeDropTarget(lines(), "L0.0.1", payload, 12, { left: 10, width: 10 })).toEqual({
            containerId: "L0.0",
            index: 1,
        });
        expect(computeDropTarget(lines(), "L0.0.1", payload, 18, { left: 10, width: 10 })).toEqual({
            containerId: "L0.0",
            index: 2,
        });
    });

    it("defaults to before when geometry is unknown", () => {
        expect(computeDropTarget(lines(), "L0.1.0", { kind: "node", id: "L0.0.0", nodeKind: "text" }, null, null)).toEqual({ containerId: "L0.1", index: 0 });
    });

    it("returns null for unknown or out-of-range ids", () => {
        const payload = { kind: "node", id: "L0.0.0", nodeKind: "text" } as const;
        expect(computeDropTarget(lines(), "nope", payload, null, null)).toBeNull();
        expect(computeDropTarget(lines(), `${LINE_ID_PREFIX}9`, payload, null, null)).toBeNull();
    });
});

// The <subagent> mutual block: a subagent line (accepts "subagent") takes
// only task-* payloads (category "subagent"), a main line (accepts "main")
// only non-task ones, and a structural preset (no category = "any") drops
// into either. A blocked container never resolves to a drop target.
describe("computeDropTarget: subagent mutual block", () => {
    // A main line L0.0 and a layout-direct subagent line L0.s.
    function containers(): ContainerView[] {
        return [
            { id: "L0.0", kind: "line", lineIndex: 0, accepts: "main", childIds: ["L0.0.0"] },
            { id: "L0.s", kind: "line", accepts: "subagent", childIds: ["L0.s.0"] },
        ];
    }
    const taskField = {
        kind: "palette",
        node: { id: "", kind: "field", name: "task-model" },
        category: "subagent",
    } as const;
    const mainField = {
        kind: "palette",
        node: { id: "", kind: "field", name: "model" },
        category: "main",
    } as const;
    // A structural preset carries no category -> "any" (drops anywhere).
    const preset = {
        kind: "palette",
        node: { id: "", kind: "text", value: "x" },
    } as const;

    it("a task-* field lands only in the subagent line", () => {
        expect(
            computeDropTarget(containers(), `${SUBAGENT_LINE_ID_PREFIX}L0.s`, taskField, null, null),
        ).toEqual({ containerId: "L0.s", index: 1 });
        expect(
            computeDropTarget(containers(), `${LINE_ID_PREFIX}0`, taskField, null, null),
        ).toBeNull();
    });

    it("a non-task field lands only in a main line", () => {
        expect(
            computeDropTarget(containers(), `${LINE_ID_PREFIX}0`, mainField, null, null),
        ).toEqual({ containerId: "L0.0", index: 1 });
        expect(
            computeDropTarget(containers(), `${SUBAGENT_LINE_ID_PREFIX}L0.s`, mainField, null, null),
        ).toBeNull();
    });

    it("a structural preset drops into either family", () => {
        expect(
            computeDropTarget(containers(), `${LINE_ID_PREFIX}0`, preset, null, null),
        ).toEqual({ containerId: "L0.0", index: 1 });
        expect(
            computeDropTarget(containers(), `${SUBAGENT_LINE_ID_PREFIX}L0.s`, preset, null, null),
        ).toEqual({ containerId: "L0.s", index: 1 });
    });

    it("a blocked container never becomes the drop target (chip move too)", () => {
        // Moving a task chip out of the subagent line onto the main line is
        // blocked, and hovering the main line's own chip is blocked as well.
        const taskChip = {
            kind: "node",
            id: "L0.s.0",
            nodeKind: "field",
            category: "subagent",
        } as const;
        expect(
            computeDropTarget(containers(), `${LINE_ID_PREFIX}0`, taskChip, null, null),
        ).toBeNull();
        expect(computeDropTarget(containers(), "L0.0.0", taskChip, null, null)).toBeNull();
    });
});

describe("computeDropTarget: span containers", () => {
    // L0.0: [field, span[field, span[field]], flex]; L0.1: [field].
    function spanContainers(): ContainerView[] {
        return [
            {
                id: "L0.0",
                kind: "line",
                lineIndex: 0,
                childIds: ["L0.0.0", "L0.0.1", "L0.0.2"],
            },
            { id: "L0.0.1", kind: "span", childIds: ["L0.0.1.0", "L0.0.1.1"] },
            { id: "L0.0.1.1", kind: "span", childIds: ["L0.0.1.1.0"] },
            { id: "L0.1", kind: "line", lineIndex: 1, childIds: ["L0.1.0"] },
        ];
    }
    const field = { kind: "palette", node: { id: "", kind: "field", name: "model" } } as const;
    const flexPreset = { kind: "palette", node: { id: "", kind: "flex" } } as const;

    it("hovering a span group's own area appends to the span's end", () => {
        expect(computeDropTarget(spanContainers(), "L0.0.1", field, null, null)).toEqual({
            containerId: "L0.0.1",
            index: 2,
        });
        // Nested spans resolve the same way.
        expect(computeDropTarget(spanContainers(), "L0.0.1.1", field, null, null)).toEqual({
            containerId: "L0.0.1.1",
            index: 1,
        });
    });

    it("hovering a nested chip yields a caret inside its span", () => {
        // over "L0.0.1.0", rect [10, 20): center 15
        expect(
            computeDropTarget(spanContainers(), "L0.0.1.0", field, 12, { left: 10, width: 10 }),
        ).toEqual({ containerId: "L0.0.1", index: 0 });
        expect(
            computeDropTarget(spanContainers(), "L0.0.1.0", field, 18, { left: 10, width: 10 }),
        ).toEqual({ containerId: "L0.0.1", index: 1 });
    });

    it("rejects flex inside spans (line-only node) but allows it on lines", () => {
        // Palette Flex preset.
        expect(computeDropTarget(spanContainers(), "L0.0.1", flexPreset, null, null)).toBeNull();
        expect(
            computeDropTarget(spanContainers(), "L0.0.1.0", flexPreset, null, null),
        ).toBeNull();
        expect(
            computeDropTarget(spanContainers(), `${LINE_ID_PREFIX}1`, flexPreset, null, null),
        ).toEqual({ containerId: "L0.1", index: 1 });
        // An existing flex chip being moved.
        const flexNode = { kind: "node", id: "L0.0.2", nodeKind: "flex" } as const;
        expect(computeDropTarget(spanContainers(), "L0.0.1", flexNode, null, null)).toBeNull();
        expect(
            computeDropTarget(spanContainers(), `${LINE_ID_PREFIX}1`, flexNode, null, null),
        ).toEqual({ containerId: "L0.1", index: 1 });
    });

    it("rejects a node dropping into itself or its own subtree", () => {
        const spanNode = { kind: "node", id: "L0.0.1", nodeKind: "span" } as const;
        // Onto itself (its own group area).
        expect(computeDropTarget(spanContainers(), "L0.0.1", spanNode, null, null)).toBeNull();
        // Onto a chip inside its own subtree (resolves to a descendant span).
        expect(
            computeDropTarget(spanContainers(), "L0.0.1.0", spanNode, null, null),
        ).toBeNull();
        expect(
            computeDropTarget(spanContainers(), "L0.0.1.1", spanNode, null, null),
        ).toBeNull();
        // A sibling line is still fine.
        expect(
            computeDropTarget(spanContainers(), `${LINE_ID_PREFIX}1`, spanNode, null, null),
        ).toEqual({ containerId: "L0.1", index: 1 });
    });

    it("moving a span's child out to its line works (not a self-subtree case)", () => {
        const inner = { kind: "node", id: "L0.0.1.0", nodeKind: "field" } as const;
        expect(
            computeDropTarget(spanContainers(), `${LINE_ID_PREFIX}0`, inner, null, null),
        ).toEqual({ containerId: "L0.0", index: 3 });
    });
});

// Geometry-based nearest-gap resolver. All numbers are hand-built: rects are
// {left,width} in viewport px, pointerX is a viewport px too, and the layout
// is a single row L0.0 = [chipA, span, chipB] with the span nesting two chips:
//
//   px:   0    20        30            70   90        110
//         [ A  ]          [ S: S0 S1     ]  [   B   ]
//   centers: A=10             S=50            B=100
//   span S rect [30,70] (w=40 -> edge margin = min(14, 12) = 12):
//     left margin [30,42], right margin [58,70], mid-body (42,58)
//     S0 rect [33,41] center 37, S1 rect [48,64] center 56
describe("resolveGeometricTarget", () => {
    const text = { kind: "palette", node: { id: "", kind: "text", value: "x" } } as const;
    const flex = { kind: "palette", node: { id: "", kind: "flex" } } as const;

    function geo(): ContainerView[] {
        return [
            { id: "L0.0", kind: "line", lineIndex: 0, childIds: ["L0.0.0", "L0.0.1", "L0.0.2"] },
            { id: "L0.0.1", kind: "span", childIds: ["L0.0.1.0", "L0.0.1.1"] },
        ];
    }
    function rects(): Map<string, { left: number; width: number }> {
        return new Map([
            ["L0.0.0", { left: 0, width: 20 }],
            ["L0.0.1", { left: 30, width: 40 }],
            ["L0.0.2", { left: 90, width: 20 }],
            ["L0.0.1.0", { left: 33, width: 8 }],
            ["L0.0.1.1", { left: 48, width: 16 }],
        ]);
    }

    it("line nearest-gap: no snap-to-end for an interior gap", () => {
        // pointer 5: left of A's center -> index 0.
        expect(resolveGeometricTarget(geo(), `${LINE_ID_PREFIX}0`, text, 5, rects())).toEqual({
            containerId: "L0.0",
            index: 0,
        });
        // pointer 25: in the A|S gap (outside the span rect [30,70]) -> index 1,
        // NOT the row's end (which would be 3). This is the finicky-drop fix.
        expect(resolveGeometricTarget(geo(), `${LINE_ID_PREFIX}0`, text, 25, rects())).toEqual({
            containerId: "L0.0",
            index: 1,
        });
        // pointer 150: right of every center -> append (index 3).
        expect(resolveGeometricTarget(geo(), `${LINE_ID_PREFIX}0`, text, 150, rects())).toEqual({
            containerId: "L0.0",
            index: 3,
        });
    });

    it("beside a span via line-level nearest-gap", () => {
        // pointer 75: right of the span's center (50) and OUTSIDE its rect
        // ([30,70]) -> after the span within the LINE (index 2, before B).
        expect(resolveGeometricTarget(geo(), `${LINE_ID_PREFIX}0`, text, 75, rects())).toEqual({
            containerId: "L0.0",
            index: 2,
        });
    });

    it("into a span at the nearest child gap", () => {
        // pointer 50: inside the span, mid-body -> into S; only S0's center (37)
        // is left of 50, so the caret is the S0|S1 gap (index 1).
        expect(resolveGeometricTarget(geo(), `${LINE_ID_PREFIX}0`, text, 50, rects())).toEqual({
            containerId: "L0.0.1",
            index: 1,
        });
    });

    it("into an empty span resolves to index 0", () => {
        const containers: ContainerView[] = [
            { id: "E0", kind: "line", lineIndex: 0, childIds: ["E0.0"] },
            { id: "E0.0", kind: "span", childIds: [] },
        ];
        const map = new Map([["E0.0", { left: 0, width: 100 }]]);
        // pointer 50: inside the empty span, mid-body -> {span, 0}.
        expect(resolveGeometricTarget(containers, `${LINE_ID_PREFIX}0`, text, 50, map)).toEqual({
            containerId: "E0.0",
            index: 0,
        });
    });

    it("span edge margin drops beside the span in the parent line", () => {
        // pointer 35: within the span's left margin [30,42] -> before S (index 1).
        expect(resolveGeometricTarget(geo(), `${LINE_ID_PREFIX}0`, text, 35, rects())).toEqual({
            containerId: "L0.0",
            index: 1,
        });
        // pointer 65: within the span's right margin [58,70] -> after S (index 2).
        expect(resolveGeometricTarget(geo(), `${LINE_ID_PREFIX}0`, text, 65, rects())).toEqual({
            containerId: "L0.0",
            index: 2,
        });
    });

    it("flex never lands in a span (neither into nor via its edge)", () => {
        // pointer 50 (span mid-body) and 35 (span left margin) both resolve at
        // the LINE level for a flex payload -> containerId is the row.
        expect(resolveGeometricTarget(geo(), `${LINE_ID_PREFIX}0`, flex, 50, rects())).toEqual({
            containerId: "L0.0",
            index: 1,
        });
        expect(resolveGeometricTarget(geo(), `${LINE_ID_PREFIX}0`, flex, 35, rects())?.containerId).toBe(
            "L0.0",
        );
    });

    it("node dragged into its own subtree resolves to null", () => {
        // Node "A" owns the whole row "A.0" (its subtree), so every candidate
        // caret — including the line-level fallback — is inside itself: null.
        const containers: ContainerView[] = [
            { id: "A.0", kind: "line", lineIndex: 0, childIds: ["A.0.0"] },
        ];
        const map = new Map([["A.0.0", { left: 0, width: 20 }]]);
        const payload = { kind: "node", id: "A", nodeKind: "span" } as const;
        expect(resolveGeometricTarget(containers, `${LINE_ID_PREFIX}0`, payload, 10, map)).toBeNull();
    });

    it("returns undefined on incomplete rects so the caller falls back to legacy", () => {
        const map = rects();
        map.delete("L0.0.2"); // a line child's rect is missing
        // pointer 25 -> line-level nearest-gap, which walks into the missing rect.
        expect(resolveGeometricTarget(geo(), `${LINE_ID_PREFIX}0`, text, 25, map)).toBeUndefined();
    });

    it("returns undefined when the id addresses no line (legacy fallback)", () => {
        expect(resolveGeometricTarget(geo(), "nope", text, 25, rects())).toBeUndefined();
    });
});

describe("useDragEditing", () => {
    it("#185 regression: drag-over never calls onDrop and repeated identical events cause zero re-renders", () => {
        const { view, onDrop, renders } = setup();

        act(() =>
            view.result.current.onDragStart(startEvent(`${PALETTE_ID_PREFIX}preset:separator`)),
        );
        act(() =>
            view.result.current.onDragOver(
                overEvent(`${PALETTE_ID_PREFIX}preset:separator`, `${LINE_ID_PREFIX}0`),
            ),
        );
        expect(view.result.current.dropTarget).toEqual({ containerId: "L0.0", index: 2 });

        // A storm of identical drag-over events (dnd-kit re-fires on
        // re-measure) must not produce unbounded renders — this is the
        // invariant whose violation caused the setState/measure loop. React
        // may render once more before bailing out on identical state, so
        // allow at most one extra render for 100 events.
        const stableCount = renders();
        act(() => {
            for (let i = 0; i < 100; i += 1) {
                view.result.current.onDragOver(
                    overEvent(`${PALETTE_ID_PREFIX}preset:separator`, `${LINE_ID_PREFIX}0`),
                );
            }
        });
        expect(renders()).toBeLessThanOrEqual(stableCount + 1);

        // Oscillating between two containers (the geometry feedback case)
        // must never commit anything mid-drag.
        act(() => {
            for (let i = 0; i < 50; i += 1) {
                view.result.current.onDragOver(
                    overEvent(`${PALETTE_ID_PREFIX}preset:separator`, `${LINE_ID_PREFIX}1`),
                );
                view.result.current.onDragOver(
                    overEvent(`${PALETTE_ID_PREFIX}preset:separator`, `${LINE_ID_PREFIX}0`),
                );
            }
        });
        expect(onDrop).not.toHaveBeenCalled();
    });

    it("palette drop reports the built node and the drop target exactly once", () => {
        const { view, onDrop } = setup();

        act(() =>
            view.result.current.onDragStart(startEvent(`${PALETTE_ID_PREFIX}preset:separator`)),
        );
        expect(view.result.current.dragLabel).toBe("Separator");

        act(() =>
            view.result.current.onDragEnd(
                endEvent(`${PALETTE_ID_PREFIX}preset:separator`, `${LINE_ID_PREFIX}0`),
            ),
        );

        expect(onDrop).toHaveBeenCalledTimes(1);
        const [payload, target] = onDrop.mock.calls[0];
        expect(target).toEqual({ containerId: "L0.0", index: 2 });
        expect(payload.kind).toBe("palette");
        if (payload.kind === "palette") {
            // The structural preset expands to a collapsing-separator text.
            expect(payload.node).toMatchObject({
                kind: "text",
                role: "separator",
                value: "",
            });
        }
        expect(view.result.current.dropTarget).toBeNull();
        expect(view.result.current.dragLabel).toBeNull();
    });

    it("field palette keys build field nodes", () => {
        const { view, onDrop } = setup();
        act(() =>
            view.result.current.onDragStart(startEvent(`${PALETTE_ID_PREFIX}field:git-branch`)),
        );
        expect(view.result.current.dragLabel).toBe("GIT-BRANCH");
        act(() =>
            view.result.current.onDragEnd(
                endEvent(`${PALETTE_ID_PREFIX}field:git-branch`, `${LINE_ID_PREFIX}1`),
            ),
        );
        const [payload] = onDrop.mock.calls[0];
        expect(payload).toMatchObject({
            kind: "palette",
            node: { kind: "field", name: "git-branch" },
        });
    });

    it("palette drops follow the pointer, not the dragged-overlay rect", () => {
        // A palette chip's overlay rect sits far left (activeCenter 5, like the
        // sidebar it came from), but the pointer is on the right half of the
        // hovered chip L0.0.1 (over rect [100,120), center 110; pointer 115).
        // Rect-based logic would read 5 < 110 -> "before" (index 1); pointer
        // logic reads 115 > 110 -> "after" (index 2). Asserting index 2 proves
        // the pointer path is used.
        const { view } = setup();
        act(() =>
            view.result.current.onDragStart(startEvent(`${PALETTE_ID_PREFIX}field:git-branch`)),
        );
        act(() =>
            view.result.current.onDragOver(
                overEvent(`${PALETTE_ID_PREFIX}field:git-branch`, "L0.0.1", {
                    activeCenter: 5,
                    overLeft: 100,
                    overWidth: 20,
                    pointerClientX: 115,
                }),
            ),
        );
        expect(view.result.current.dropTarget).toEqual({ containerId: "L0.0", index: 2 });

        // Moving the pointer to the left half (95) flips it to "before".
        act(() =>
            view.result.current.onDragOver(
                overEvent(`${PALETTE_ID_PREFIX}field:git-branch`, "L0.0.1", {
                    activeCenter: 5,
                    overLeft: 100,
                    overWidth: 20,
                    pointerClientX: 95,
                }),
            ),
        );
        expect(view.result.current.dropTarget).toEqual({ containerId: "L0.0", index: 1 });
    });

    it("canvas drags carry the existing node id", () => {
        const { view, onDrop } = setup();

        act(() => view.result.current.onDragStart(startEvent("L0.0.0")));
        expect(view.result.current.dragLabel).toBe("node L0.0.0");
        // Dragged center (18) is right of L0.1.0's center (15) → after it.
        act(() =>
            view.result.current.onDragEnd(
                endEvent("L0.0.0", "L0.1.0", { activeCenter: 18, overLeft: 10, overWidth: 10 }),
            ),
        );

        expect(onDrop).toHaveBeenCalledTimes(1);
        expect(onDrop.mock.calls[0][0]).toEqual({ kind: "node", id: "L0.0.0", nodeKind: "text" });
        expect(onDrop.mock.calls[0][1]).toEqual({ containerId: "L0.1", index: 1 });
    });

    it("dropping outside all rows cancels without a commit", () => {
        const { view, onDrop } = setup();

        act(() =>
            view.result.current.onDragStart(startEvent(`${PALETTE_ID_PREFIX}preset:separator`)),
        );
        act(() =>
            view.result.current.onDragOver(
                overEvent(`${PALETTE_ID_PREFIX}preset:separator`, `${LINE_ID_PREFIX}0`),
            ),
        );
        act(() =>
            view.result.current.onDragEnd(endEvent(`${PALETTE_ID_PREFIX}preset:separator`, null)),
        );

        expect(onDrop).not.toHaveBeenCalled();
        expect(view.result.current.dropTarget).toBeNull();
    });

    it("stays inert when getContainers returns null (read-only while the DSL is invalid)", () => {
        const { view, onDrop } = setup(null);

        act(() =>
            view.result.current.onDragStart(startEvent(`${PALETTE_ID_PREFIX}preset:separator`)),
        );
        act(() =>
            view.result.current.onDragOver(
                overEvent(`${PALETTE_ID_PREFIX}preset:separator`, `${LINE_ID_PREFIX}0`),
            ),
        );
        expect(view.result.current.dropTarget).toBeNull();
        act(() =>
            view.result.current.onDragEnd(
                endEvent(`${PALETTE_ID_PREFIX}preset:separator`, `${LINE_ID_PREFIX}0`),
            ),
        );
        expect(onDrop).not.toHaveBeenCalled();
    });

    it("unknown palette keys cancel the drag", () => {
        const { view, onDrop } = setup();
        act(() => view.result.current.onDragStart(startEvent(`${PALETTE_ID_PREFIX}preset:nope`)));
        expect(view.result.current.dragLabel).toBeNull();
        act(() =>
            view.result.current.onDragEnd(
                endEvent(`${PALETTE_ID_PREFIX}preset:nope`, `${LINE_ID_PREFIX}0`),
            ),
        );
        expect(onDrop).not.toHaveBeenCalled();
    });

    it("exposes dragCategory and blocks a cross-family drop over the whole drag", () => {
        const rows: ContainerView[] = [
            { id: "L0.0", kind: "line", lineIndex: 0, accepts: "main", childIds: [] },
            { id: "L0.s", kind: "line", accepts: "subagent", childIds: [] },
        ];
        const onDrop = vi.fn<(payload: DragPayload, target: DropTarget) => void>();
        const view = renderHook(() =>
            useDragEditing({
                getContainers: () => rows,
                makePaletteNode: (key: string) => ({
                    node: { id: "", kind: "field", name: key.slice("field:".length) } as LineChild,
                    label: key,
                }),
                categoryForPalette: (key: string) =>
                    key === "field:task-model" ? "subagent" : "main",
                labelForNodeId: (id) => id,
                kindForNodeId: () => "field",
                onDrop,
            }),
        );

        act(() =>
            view.result.current.onDragStart(startEvent(`${PALETTE_ID_PREFIX}field:task-model`)),
        );
        // The canvas reads this to highlight/dim the eligible bands.
        expect(view.result.current.dragCategory).toBe("subagent");

        // Over a main line: blocked -> no drop target painted.
        act(() =>
            view.result.current.onDragOver(
                overEvent(`${PALETTE_ID_PREFIX}field:task-model`, `${LINE_ID_PREFIX}0`),
            ),
        );
        expect(view.result.current.dropTarget).toBeNull();

        // Over the subagent line: allowed.
        act(() =>
            view.result.current.onDragOver(
                overEvent(`${PALETTE_ID_PREFIX}field:task-model`, `${SUBAGENT_LINE_ID_PREFIX}L0.s`),
            ),
        );
        expect(view.result.current.dropTarget).toEqual({ containerId: "L0.s", index: 0 });

        // Dropping on the blocked main line commits nothing; dragCategory resets.
        act(() =>
            view.result.current.onDragEnd(
                endEvent(`${PALETTE_ID_PREFIX}field:task-model`, `${LINE_ID_PREFIX}0`),
            ),
        );
        expect(onDrop).not.toHaveBeenCalled();
        expect(view.result.current.dragCategory).toBeNull();
    });
});

// Reordering <variant> cards is a structurally different drag from a chip
// drop (it never touches line/span content), resolved against
// getVariantLists rather than getContainers, but sharing the same
// DropTarget/dropFlags painting contract via the synthetic
// VARIANT_CONTAINER_PREFIX container id.
describe("useDragEditing: variant reorder", () => {
    function variantSetup(lists: VariantListView[] | null = [{
        responsiveId: "L0.1",
        variantIds: ["L0.1.v0", "L0.1.v1", "L0.1.v2"],
    }]) {
        const onDrop = vi.fn<(payload: DragPayload, target: DropTarget) => void>();
        const view = renderHook(() =>
            useDragEditing({
                getContainers: () => null,
                getVariantLists: () => lists,
                makePaletteNode: () => null,
                labelForNodeId: (id) => `node ${id}`,
                kindForNodeId: () => null,
                onDrop,
            }),
        );
        return { view, onDrop };
    }

    it("drags a variant card and reports a reorder target on drop (Y axis)", () => {
        const { view, onDrop } = variantSetup();

        act(() =>
            view.result.current.onDragStart(startEvent(`${VARIANT_ID_PREFIX}L0.1.v0`)),
        );
        expect(view.result.current.dragLabel).toBe("node L0.1.v0");

        // Cards stack vertically: the hovered card sits at top 10 / height 10
        // (vertical midpoint 15). A dragged center BELOW the midpoint (18) is
        // an after-caret, landing past the last variant (index 3). The X
        // geometry is deliberately reversed (dragged far LEFT of the card) to
        // prove resolution is driven by Y, not X.
        act(() =>
            view.result.current.onDragOver(
                overEvent(`${VARIANT_ID_PREFIX}L0.1.v0`, "L0.1.v2", {
                    activeCenter: 0,
                    activeCenterY: 18,
                    overLeft: 100,
                    overWidth: 10,
                    overTop: 10,
                    overHeight: 10,
                }),
            ),
        );
        expect(view.result.current.dropTarget).toEqual({
            containerId: `${VARIANT_CONTAINER_PREFIX}L0.1`,
            index: 3,
        });

        // A dragged center ABOVE the midpoint (12) flips it to a before-caret.
        act(() =>
            view.result.current.onDragOver(
                overEvent(`${VARIANT_ID_PREFIX}L0.1.v0`, "L0.1.v2", {
                    activeCenterY: 12,
                    overTop: 10,
                    overHeight: 10,
                }),
            ),
        );
        expect(view.result.current.dropTarget).toEqual({
            containerId: `${VARIANT_CONTAINER_PREFIX}L0.1`,
            index: 2,
        });

        act(() =>
            view.result.current.onDragEnd(
                endEvent(`${VARIANT_ID_PREFIX}L0.1.v0`, "L0.1.v2", {
                    activeCenterY: 18,
                    overTop: 10,
                    overHeight: 10,
                }),
            ),
        );
        expect(onDrop).toHaveBeenCalledTimes(1);
        expect(onDrop.mock.calls[0][0]).toEqual({ kind: "variant", id: "L0.1.v0" });
        expect(onDrop.mock.calls[0][1]).toEqual({
            containerId: `${VARIANT_CONTAINER_PREFIX}L0.1`,
            index: 3,
        });
    });

    it("stays inert when getVariantLists returns null (read-only while the DSL is invalid)", () => {
        const { view, onDrop } = variantSetup(null);

        act(() =>
            view.result.current.onDragStart(startEvent(`${VARIANT_ID_PREFIX}L0.1.v0`)),
        );
        act(() =>
            view.result.current.onDragOver(
                overEvent(`${VARIANT_ID_PREFIX}L0.1.v0`, "L0.1.v2"),
            ),
        );
        expect(view.result.current.dropTarget).toBeNull();
        act(() =>
            view.result.current.onDragEnd(
                endEvent(`${VARIANT_ID_PREFIX}L0.1.v0`, "L0.1.v2"),
            ),
        );
        expect(onDrop).not.toHaveBeenCalled();
    });

    it("resolves to null (and cancels) when dropped outside any variant list", () => {
        const { view, onDrop } = variantSetup();
        act(() =>
            view.result.current.onDragStart(startEvent(`${VARIANT_ID_PREFIX}L0.1.v0`)),
        );
        act(() =>
            view.result.current.onDragEnd(endEvent(`${VARIANT_ID_PREFIX}L0.1.v0`, "L0.1.v0")),
        );
        // Hovering the dragged card itself resolves to its own index (0) —
        // dnd-kit's sortable semantics tolerate this as a no-op reorder; the
        // hook still calls onDrop (App.tsx's moveVariant is itself a no-op
        // for this case).
        expect(onDrop).toHaveBeenCalledTimes(1);
        expect(onDrop.mock.calls[0][1]).toEqual({
            containerId: `${VARIANT_CONTAINER_PREFIX}L0.1`,
            index: 0,
        });
    });
});
