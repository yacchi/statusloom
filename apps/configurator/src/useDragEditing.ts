// Drag & drop editing logic for the configurator canvas.
//
// Drop targets are CONTAINER-based: a caret position inside any line or span
// (`{containerId, index}`), so chips can be dropped into span groups, not
// just at a line's top level. Resolution priority (see computeDropTarget):
//   1. a row droppable ("line-N")            -> end of that line
//   2. a span container id (the group area)  -> end of that span
//   3. a chip id (top-level or span-nested)  -> before/after in its container
// Invalid targets resolve to null already at drag-over time, so no indicator
// is painted for them: a node never drops into its own subtree, and <flex>
// is line-only (the DSL forbids it inside <span>).
//
// Regression note (React error #185, "Maximum update depth exceeded"): an
// earlier implementation restructured the document inside onDragOver —
// live-inserting palette placeholders and moving chips between lines. Every
// restructure changed the SortableContext items and the droppable registry
// mid-drag, which made dnd-kit re-measure and recompute collisions, which
// changed `over`, which fired onDragOver again and restructured back: an
// unbounded setState/measure feedback loop that crashed on placing a chip.
//
// The fix is architectural: dragging NEVER mutates the document (the AST).
// onDragOver only computes a pure drop target, and the state update bails out
// (returns the previous object) when the target is unchanged, so repeated
// identical events cause zero re-renders. The drop indicator is paint-only
// (no layout impact), the per-span SortableContext item arrays are derived
// from the frozen AST (static during a drag), and the single AST restructure
// happens exactly once, on drop (App.tsx turns it into applyDropEdit +
// serialize).

import { useCallback, useRef, useState } from "react";
import type { DragEndEvent, DragOverEvent, DragStartEvent } from "@dnd-kit/core";
import type { LineChild } from "./types.ts";

// Draggable id prefix for palette chips; the palette key follows the colon
// ("field:<name>" or "preset:<id>").
export const PALETTE_ID_PREFIX = "palette:";
// Droppable id prefix for whole line rows; the line index follows the dash.
export const LINE_ID_PREFIX = "line-";

// A drop container as the drag logic sees it: a line or a span (nested spans
// included), with the AST node IDs of its direct children (the chips).
export interface ContainerView {
    id: string; // the LineNode's / SpanNode's AST id
    kind: "line" | "span";
    // For kind "line": the index of the line within the edited layout
    // (matches the row droppable's "line-N" id).
    lineIndex?: number;
    childIds: string[];
}

// An insertion caret position: insert before the child currently at `index`
// of container `containerId` (index === children length means "append").
export interface DropTarget {
    containerId: string;
    index: number;
}

// What was dragged, resolved at drag start. A palette drag carries the
// freshly built AST node; a canvas drag carries the existing node's ID plus
// its kind (needed to reject line-only nodes as span content).
export type DragPayload =
    | { kind: "palette"; node: LineChild }
    | { kind: "node"; id: string; nodeKind: string };

interface Rect {
    left: number;
    width: number;
}

// Flatten the span containers (nested included, in document order) out of a
// mixed-content child list. Used by App.tsx to build the container list.
export function spanContainersOf(children: readonly LineChild[]): ContainerView[] {
    const out: ContainerView[] = [];
    const walk = (nodes: readonly LineChild[]) => {
        for (const n of nodes) {
            if (n.kind === "span") {
                out.push({ id: n.id, kind: "span", childIds: n.children.map((c) => c.id) });
                walk(n.children);
            }
        }
    };
    walk(children);
    return out;
}

function draggedNodeKind(payload: DragPayload): string {
    return payload.kind === "palette" ? payload.node.kind : payload.nodeKind;
}

// Pure: resolve a droppable/sortable id (plus pointer geometry, when known)
// to an insertion position, or null when the id resolves nowhere or the
// target is invalid for the dragged payload.
export function computeDropTarget(
    containers: readonly ContainerView[],
    overId: string,
    payload: DragPayload,
    activeCenterX: number | null,
    overRect: Rect | null,
): DropTarget | null {
    const raw = resolveRawTarget(containers, overId, activeCenterX, overRect);
    if (!raw) {
        return null;
    }
    const container = containers.find((c) => c.id === raw.containerId);
    if (!container) {
        return null;
    }
    // <flex> is line-only: the DSL rejects it inside <span>.
    if (draggedNodeKind(payload) === "flex" && container.kind === "span") {
        return null;
    }
    // A node can never move into itself or its own subtree.
    if (
        payload.kind === "node" &&
        (raw.containerId === payload.id || raw.containerId.startsWith(payload.id + "."))
    ) {
        return null;
    }
    return raw;
}

function resolveRawTarget(
    containers: readonly ContainerView[],
    overId: string,
    activeCenterX: number | null,
    overRect: Rect | null,
): DropTarget | null {
    if (overId.startsWith(LINE_ID_PREFIX)) {
        const n = Number(overId.slice(LINE_ID_PREFIX.length));
        const line = containers.find((c) => c.kind === "line" && c.lineIndex === n);
        return line ? { containerId: line.id, index: line.childIds.length } : null;
    }
    // Hovering a span group's own area (its chip): append INTO the span.
    // This takes priority over the span's role as a child chip of its line;
    // placing before/after a span is done via its neighbor chips or the row.
    const span = containers.find((c) => c.kind === "span" && c.id === overId);
    if (span) {
        return { containerId: span.id, index: span.childIds.length };
    }
    // A chip (top-level or span-nested): a caret in its own container.
    for (const c of containers) {
        const j = c.childIds.indexOf(overId);
        if (j >= 0) {
            const after =
                activeCenterX !== null &&
                overRect !== null &&
                activeCenterX > overRect.left + overRect.width / 2;
            return { containerId: c.id, index: after ? j + 1 : j };
        }
    }
    return null;
}

function activeCenterXOf(e: DragOverEvent | DragEndEvent): number | null {
    const r = e.active.rect.current.translated;
    return r ? r.left + r.width / 2 : null;
}

function overRectOf(e: DragOverEvent | DragEndEvent): Rect | null {
    const r = e.over?.rect;
    return r ? { left: r.left, width: r.width } : null;
}

function sameTarget(a: DropTarget | null, b: DropTarget | null): boolean {
    if (a === null || b === null) {
        return a === b;
    }
    return a.containerId === b.containerId && a.index === b.index;
}

export interface DragEditing {
    dragLabel: string | null;
    dropTarget: DropTarget | null;
    onDragStart: (e: DragStartEvent) => void;
    onDragOver: (e: DragOverEvent) => void;
    onDragEnd: (e: DragEndEvent) => void;
    onDragCancel: () => void;
}

interface Args {
    // The current layout's drop containers (lines + all spans), or null while
    // nothing is loaded / the DSL is invalid. Read fresh on every event so
    // the logic never captures a stale tree.
    getContainers: () => ContainerView[] | null;
    // Build the AST node (and overlay label) for a palette key
    // ("field:<name>" / "preset:<id>"); null cancels the drag.
    makePaletteNode: (key: string) => { node: LineChild; label: string } | null;
    // Overlay label for an existing canvas chip.
    labelForNodeId: (id: string) => string;
    // AST node kind for an existing canvas chip (null = unknown).
    kindForNodeId: (id: string) => string | null;
    // Called exactly once per successful drop.
    onDrop: (payload: DragPayload, target: DropTarget) => void;
}

export function useDragEditing({
    getContainers,
    makePaletteNode,
    labelForNodeId,
    kindForNodeId,
    onDrop,
}: Args): DragEditing {
    const [dragLabel, setDragLabel] = useState<string | null>(null);
    const [dropTarget, setDropTarget] = useState<DropTarget | null>(null);
    // The payload resolved at drag start; identity is stable all drag.
    const payloadRef = useRef<DragPayload | null>(null);

    const reset = useCallback(() => {
        payloadRef.current = null;
        setDragLabel(null);
        setDropTarget(null);
    }, []);

    const onDragStart = useCallback(
        (e: DragStartEvent) => {
            const id = String(e.active.id);
            if (id.startsWith(PALETTE_ID_PREFIX)) {
                const made = makePaletteNode(id.slice(PALETTE_ID_PREFIX.length));
                if (!made) {
                    return;
                }
                payloadRef.current = { kind: "palette", node: made.node };
                setDragLabel(made.label);
            } else {
                payloadRef.current = { kind: "node", id, nodeKind: kindForNodeId(id) ?? "" };
                setDragLabel(labelForNodeId(id));
            }
        },
        [kindForNodeId, labelForNodeId, makePaletteNode],
    );

    const onDragOver = useCallback(
        (e: DragOverEvent) => {
            const payload = payloadRef.current;
            const containers = getContainers();
            if (!payload || !containers) {
                return;
            }
            const next = e.over
                ? computeDropTarget(
                      containers,
                      String(e.over.id),
                      payload,
                      activeCenterXOf(e),
                      overRectOf(e),
                  )
                : null;
            // Bail out (same object) when unchanged so repeated identical
            // drag-over events cause zero re-renders.
            setDropTarget((prev) => (sameTarget(prev, next) ? prev : next));
        },
        [getContainers],
    );

    const onDragEnd = useCallback(
        (e: DragEndEvent) => {
            const payload = payloadRef.current;
            const containers = getContainers();
            if (payload && containers && e.over) {
                const target = computeDropTarget(
                    containers,
                    String(e.over.id),
                    payload,
                    activeCenterXOf(e),
                    overRectOf(e),
                );
                if (target) {
                    onDrop(payload, target);
                }
            }
            reset();
        },
        [getContainers, onDrop, reset],
    );

    return { dragLabel, dropTarget, onDragStart, onDragOver, onDragEnd, onDragCancel: reset };
}
