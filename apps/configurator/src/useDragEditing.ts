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
// Droppable id prefix for a top-level (layout-child) line row; the layout
// child's position index follows the dash. A responsive-free layout's lines
// keep this exact scheme, unchanged from before <responsive> existed.
export const LINE_ID_PREFIX = "line-";
// Droppable id prefix for a line row nested inside a <responsive>'s variant;
// the line's own AST id follows the colon (variant-nested lines have no
// single flat index to key on the way top-level lines do).
export const VARIANT_LINE_ID_PREFIX = "vline:";
// Draggable id prefix for a <variant> card being reordered within its
// <responsive>; the variant's own AST id follows the colon.
export const VARIANT_ID_PREFIX = "variant:";
// Synthetic (non-AST) droppable/container id for a responsive's variant
// list, so the caret math and paint-only drop indicators shared with chip
// drops (DropTarget / dropFlags) also work for variant reordering. The
// responsive's own AST id follows the colon.
export const VARIANT_CONTAINER_PREFIX = "variants-of:";

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
// its kind (needed to reject line-only nodes as span content); a variant
// drag reorders a whole <variant> card within its <responsive> and never
// touches line/span content at all.
export type DragPayload =
    | { kind: "palette"; node: LineChild }
    | { kind: "node"; id: string; nodeKind: string }
    | { kind: "variant"; id: string };

// A <responsive>'s variant list, as the drag logic sees it: the responsive's
// own AST id plus its variants' AST ids in order (widest first).
export interface VariantListView {
    responsiveId: string;
    variantIds: string[];
}

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

// The chip-drag payloads computeDropTarget/resolveRawTarget understand —
// everything but a variant reorder, which never touches line/span content
// and is resolved separately (resolveVariantDropTarget).
export type ChipDragPayload = Exclude<DragPayload, { kind: "variant" }>;

function draggedNodeKind(payload: ChipDragPayload): string {
    return payload.kind === "palette" ? payload.node.kind : payload.nodeKind;
}

// Pure: resolve a droppable/sortable id (plus pointer geometry, when known)
// to an insertion position, or null when the id resolves nowhere or the
// target is invalid for the dragged payload. `pointerX` is the real pointer X
// (viewport px), used to pick before/after within the hovered chip.
export function computeDropTarget(
    containers: readonly ContainerView[],
    overId: string,
    payload: ChipDragPayload,
    pointerX: number | null,
    overRect: Rect | null,
): DropTarget | null {
    const raw = resolveRawTarget(containers, overId, pointerX, overRect);
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
    pointerX: number | null,
    overRect: Rect | null,
): DropTarget | null {
    if (overId.startsWith(LINE_ID_PREFIX)) {
        const n = Number(overId.slice(LINE_ID_PREFIX.length));
        const line = containers.find((c) => c.kind === "line" && c.lineIndex === n);
        return line ? { containerId: line.id, index: line.childIds.length } : null;
    }
    if (overId.startsWith(VARIANT_LINE_ID_PREFIX)) {
        const lineId = overId.slice(VARIANT_LINE_ID_PREFIX.length);
        const line = containers.find((c) => c.kind === "line" && c.id === lineId);
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
                pointerX !== null &&
                overRect !== null &&
                pointerX > overRect.left + overRect.width / 2;
            return { containerId: c.id, index: after ? j + 1 : j };
        }
    }
    return null;
}

// Pure: resolve a droppable/sortable id to a variant-reorder caret within
// `lists`, or null when it addresses no variant list. Mirrors the chip
// caret math (before/after the hovered card by pointer position) but over a
// flat variant-id list rather than a container tree — a <variant> never
// nests into anything else, so there is no span-like "append into" case.
function resolveVariantDropTarget(
    lists: readonly VariantListView[],
    overId: string,
    pointerX: number | null,
    overRect: Rect | null,
): DropTarget | null {
    for (const list of lists) {
        const containerId = VARIANT_CONTAINER_PREFIX + list.responsiveId;
        const j = list.variantIds.indexOf(overId);
        if (j >= 0) {
            const after =
                pointerX !== null &&
                overRect !== null &&
                pointerX > overRect.left + overRect.width / 2;
            return { containerId, index: after ? j + 1 : j };
        }
    }
    return null;
}

function activeCenterXOf(e: DragOverEvent | DragEndEvent): number | null {
    const r = e.active.rect.current.translated;
    return r ? r.left + r.width / 2 : null;
}

// The real pointer X during a drag: the activator PointerEvent's clientX
// shifted by the accumulated drag delta. Palette chips originate in the
// sidebar, so the dragged-overlay rect center is far from the cursor; using
// the pointer keeps before/after caret resolution aligned with where the user
// actually points. Falls back to the dragged rect center when the activator
// carries no usable clientX (defensive: non-pointer activators, test stubs).
function pointerXOf(e: DragOverEvent | DragEndEvent): number | null {
    const clientX = (e.activatorEvent as { clientX?: unknown } | null)?.clientX;
    if (typeof clientX === "number") {
        return clientX + (e.delta?.x ?? 0);
    }
    return activeCenterXOf(e);
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
    // The current layout's drop containers (lines, incl. every variant's
    // lines, + all spans), or null while nothing is loaded / the DSL is
    // invalid. Read fresh on every event so the logic never captures a stale
    // tree.
    getContainers: () => ContainerView[] | null;
    // Every <responsive>'s variant-id list in the current layout, for
    // variant-reorder drags; null while nothing is loaded / the DSL is
    // invalid. Omit when the caller has no responsive containers to manage.
    getVariantLists?: () => VariantListView[] | null;
    // Build the AST node (and overlay label) for a palette key
    // ("field:<name>" / "preset:<id>"); null cancels the drag.
    makePaletteNode: (key: string) => { node: LineChild; label: string } | null;
    // Overlay label for an existing canvas chip or variant card.
    labelForNodeId: (id: string) => string;
    // AST node kind for an existing canvas chip (null = unknown).
    kindForNodeId: (id: string) => string | null;
    // Called exactly once per successful drop.
    onDrop: (payload: DragPayload, target: DropTarget) => void;
}

export function useDragEditing({
    getContainers,
    getVariantLists = () => null,
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
            } else if (id.startsWith(VARIANT_ID_PREFIX)) {
                const variantId = id.slice(VARIANT_ID_PREFIX.length);
                payloadRef.current = { kind: "variant", id: variantId };
                setDragLabel(labelForNodeId(variantId));
            } else {
                payloadRef.current = { kind: "node", id, nodeKind: kindForNodeId(id) ?? "" };
                setDragLabel(labelForNodeId(id));
            }
        },
        [kindForNodeId, labelForNodeId, makePaletteNode],
    );

    // Resolves the current drop target for `payload` against `overId`,
    // dispatching to the variant-list resolver or the chip/container
    // resolver depending on what is being dragged.
    const resolveTarget = useCallback(
        (payload: DragPayload, overId: string, pointerX: number | null, overRect: Rect | null) => {
            if (payload.kind === "variant") {
                const lists = getVariantLists();
                return lists ? resolveVariantDropTarget(lists, overId, pointerX, overRect) : null;
            }
            const containers = getContainers();
            return containers
                ? computeDropTarget(containers, overId, payload, pointerX, overRect)
                : null;
        },
        [getContainers, getVariantLists],
    );

    const onDragOver = useCallback(
        (e: DragOverEvent) => {
            const payload = payloadRef.current;
            if (!payload) {
                return;
            }
            const next = e.over
                ? resolveTarget(payload, String(e.over.id), pointerXOf(e), overRectOf(e))
                : null;
            // Bail out (same object) when unchanged so repeated identical
            // drag-over events cause zero re-renders.
            setDropTarget((prev) => (sameTarget(prev, next) ? prev : next));
        },
        [resolveTarget],
    );

    const onDragEnd = useCallback(
        (e: DragEndEvent) => {
            const payload = payloadRef.current;
            if (payload && e.over) {
                const target = resolveTarget(payload, String(e.over.id), pointerXOf(e), overRectOf(e));
                if (target) {
                    onDrop(payload, target);
                }
            }
            reset();
        },
        [onDrop, reset, resolveTarget],
    );

    return { dragLabel, dropTarget, onDragStart, onDragOver, onDragEnd, onDragCancel: reset };
}
