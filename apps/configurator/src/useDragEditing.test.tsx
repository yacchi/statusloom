import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { DragEndEvent, DragOverEvent, DragStartEvent } from "@dnd-kit/core";
import {
    LINE_ID_PREFIX,
    PALETTE_ID_PREFIX,
    computeDropTarget,
    useDragEditing,
    type ContainerView,
    type DragPayload,
    type DropTarget,
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

function activeObj(id: string, centerLeft: number | null) {
    return {
        id,
        rect: {
            current: {
                initial: null,
                translated:
                    centerLeft === null
                        ? null
                        : {
                              left: centerLeft - 5,
                              width: 10,
                              top: 0,
                              right: centerLeft + 5,
                              bottom: 10,
                              height: 10,
                          },
            },
        },
    };
}

function overObj(id: string, left: number, width: number) {
    return {
        id,
        rect: { left, width, top: 0, right: left + width, bottom: 10, height: 10 },
    };
}

function startEvent(activeId: string): DragStartEvent {
    return { active: activeObj(activeId, null) } as unknown as DragStartEvent;
}

function overEvent(
    activeId: string,
    overId: string | null,
    opts: { activeCenter?: number; overLeft?: number; overWidth?: number } = {},
): DragOverEvent {
    return {
        active: activeObj(activeId, opts.activeCenter ?? null),
        over: overId === null ? null : overObj(overId, opts.overLeft ?? 0, opts.overWidth ?? 10),
    } as unknown as DragOverEvent;
}

function endEvent(
    activeId: string,
    overId: string | null,
    opts: { activeCenter?: number; overLeft?: number; overWidth?: number } = {},
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
});
