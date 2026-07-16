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

// The vertical counterpart of Rect, used ONLY by variant-card reordering,
// which is a Y-axis drag (cards stack vertically and never move horizontally).
// Kept separate so the chip resolvers (computeDropTarget / resolveGeometricTarget
// / getRects) stay purely horizontal and are not forced to carry top/height.
interface VRect {
    top: number;
    height: number;
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

// Resolve the LINE ("row") container that owns `overId`: a row droppable id
// maps to its line directly; a chip or span id climbs its owners (spans nest,
// so a span-nested chip walks span -> ... -> line, because a span id appears
// in its parent's childIds). Returns null when the id addresses no line in
// `containers` (the caller treats that as "fall back to legacy").
function lineContainerOf(
    containers: readonly ContainerView[],
    overId: string,
): ContainerView | null {
    if (overId.startsWith(LINE_ID_PREFIX)) {
        const n = Number(overId.slice(LINE_ID_PREFIX.length));
        return containers.find((c) => c.kind === "line" && c.lineIndex === n) ?? null;
    }
    if (overId.startsWith(VARIANT_LINE_ID_PREFIX)) {
        const lineId = overId.slice(VARIANT_LINE_ID_PREFIX.length);
        return containers.find((c) => c.kind === "line" && c.id === lineId) ?? null;
    }
    // overId is a chip or span id: climb owners until a line is reached.
    let cur = overId;
    // Bound the climb by the container count so a malformed tree can't loop.
    for (let guard = 0; guard <= containers.length; guard += 1) {
        const owner = containers.find((c) => c.childIds.includes(cur));
        if (!owner) {
            return null;
        }
        if (owner.kind === "line") {
            return owner;
        }
        cur = owner.id;
    }
    return null;
}

function rectContains(r: Rect, x: number): boolean {
    return x >= r.left && x <= r.left + r.width;
}

// The insertion index within `childIds` nearest `pointerX`: the count of
// children whose horizontal center sits left of the pointer, yielding 0
// (before all) .. n (after all) with no snap-to-end for an interior gap.
// Children render left-to-right in document order, so counting centers is a
// stable ordering. An empty container is index 0. Returns null when any child
// rect is missing (incomplete geometry) so the caller can fall back to the
// legacy resolver rather than guess a caret.
function nearestGap(
    childIds: readonly string[],
    rects: ReadonlyMap<string, Rect>,
    pointerX: number,
): number | null {
    let index = 0;
    for (const id of childIds) {
        const r = rects.get(id);
        if (!r) {
            return null;
        }
        if (r.left + r.width / 2 < pointerX) {
            index += 1;
        }
    }
    return index;
}

// Re-validate a candidate target with the SAME rules as computeDropTarget:
// <flex> is line-only (never a span), and a node never drops into itself or
// its own subtree. Returns the target unchanged when valid, else null.
function validateGeometric(
    containers: readonly ContainerView[],
    target: DropTarget,
    payload: ChipDragPayload,
    kind: string,
): DropTarget | null {
    const container = containers.find((c) => c.id === target.containerId);
    if (container && kind === "flex" && container.kind === "span") {
        return null;
    }
    if (
        payload.kind === "node" &&
        (target.containerId === payload.id ||
            target.containerId.startsWith(payload.id + "."))
    ) {
        return null;
    }
    return target;
}

// Pure, geometry-based nearest-gap resolver. Unlike computeDropTarget (which
// keys off the single hovered droppable and its left/right half), this looks
// at EVERY chip's measured rectangle in the hovered row at once, so a chip can
// land in any gap between chips — including BESIDE a span — instead of always
// snapping to the row's end or INTO a hovered span.
//
// Return semantics let the caller stay backwards-compatible:
//   DropTarget -> resolved caret;
//   null       -> resolved but INVALID for this payload (paint nothing);
//   undefined  -> geometry unavailable/incomplete -> fall back to legacy.
//
// `rects` are viewport/client coords (dnd-kit droppableRects), consistent with
// `pointerX` (activator clientX + delta). Reading them is pure: droppables are
// stationary and the AST is frozen for the whole drag (see the #185 note).
export function resolveGeometricTarget(
    containers: readonly ContainerView[],
    overId: string,
    payload: ChipDragPayload,
    pointerX: number,
    rects: ReadonlyMap<string, { left: number; width: number }>,
): DropTarget | null | undefined {
    const line = lineContainerOf(containers, overId);
    if (!line) {
        return undefined; // addresses no line -> legacy fallback
    }
    const kind = draggedNodeKind(payload);
    // Find the DEEPEST span within the row whose rect contains the pointer and
    // that is a legal drop target for this payload. flex is line-only, so it
    // considers no spans at all; a node ignores its own subtree so it can be
    // dragged out of the span it lives in. "Deepest" = the id with the most
    // dot-separated segments (a nested span's id startsWith its outer span's).
    let span: ContainerView | null = null;
    if (kind !== "flex") {
        const prefix = line.id + ".";
        let bestDepth = -1;
        for (const c of containers) {
            if (c.kind !== "span" || !c.id.startsWith(prefix)) {
                continue;
            }
            const r = rects.get(c.id);
            if (!r || !rectContains(r, pointerX)) {
                continue;
            }
            if (
                payload.kind === "node" &&
                (c.id === payload.id || c.id.startsWith(payload.id + "."))
            ) {
                continue; // never resolve into the dragged node's own subtree
            }
            const depth = c.id.split(".").length;
            if (depth > bestDepth) {
                bestDepth = depth;
                span = c;
            }
        }
    }

    let target: DropTarget;
    if (span) {
        const S = span;
        const r = rects.get(S.id);
        if (!r) {
            return undefined;
        }
        // A margin on each edge of the span lets the user drop BESIDE it
        // (before/after within the parent line) rather than always into it.
        const margin = Math.min(14, r.width * 0.3);
        if (pointerX <= r.left + margin || pointerX >= r.left + r.width - margin) {
            const parent = containers.find((c) => c.childIds.includes(S.id));
            if (!parent) {
                return undefined;
            }
            const idx = parent.childIds.indexOf(S.id);
            const after = pointerX >= r.left + r.width - margin;
            target = { containerId: parent.id, index: after ? idx + 1 : idx };
        } else {
            const gap = nearestGap(S.childIds, rects, pointerX);
            if (gap === null) {
                return undefined;
            }
            target = { containerId: S.id, index: gap };
        }
    } else {
        const gap = nearestGap(line.childIds, rects, pointerX);
        if (gap === null) {
            return undefined;
        }
        target = { containerId: line.id, index: gap };
    }

    const validated = validateGeometric(containers, target, payload, kind);
    if (validated) {
        return validated;
    }
    // Retry once at the line level (valid for flex and any non-self node), then
    // give up. This is the graceful degradation for e.g. dropping onto a span
    // that turned out to be the dragged node's own subtree.
    const fallbackGap = nearestGap(line.childIds, rects, pointerX);
    if (fallbackGap === null) {
        return undefined;
    }
    return validateGeometric(containers, { containerId: line.id, index: fallbackGap }, payload, kind);
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
// caret math but along the Y axis (before/after the hovered card by pointer
// height) — variant cards stack VERTICALLY and are only ever reordered up or
// down — over a flat variant-id list rather than a container tree; a <variant>
// never nests into anything else, so there is no span-like "append into" case.
function resolveVariantDropTarget(
    lists: readonly VariantListView[],
    overId: string,
    pointerY: number | null,
    vRect: VRect | null,
): DropTarget | null {
    for (const list of lists) {
        const containerId = VARIANT_CONTAINER_PREFIX + list.responsiveId;
        const j = list.variantIds.indexOf(overId);
        if (j >= 0) {
            const after =
                pointerY !== null &&
                vRect !== null &&
                pointerY > vRect.top + vRect.height / 2;
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

// Y-axis counterparts of the pointer/rect helpers above, used only for
// variant-card reordering (a vertical drag). Kept separate from the X helpers
// so chip/span drops remain horizontal.
function activeCenterYOf(e: DragOverEvent | DragEndEvent): number | null {
    const r = e.active.rect.current.translated;
    return r ? r.top + r.height / 2 : null;
}

function pointerYOf(e: DragOverEvent | DragEndEvent): number | null {
    const clientY = (e.activatorEvent as { clientY?: unknown } | null)?.clientY;
    if (typeof clientY === "number") {
        return clientY + (e.delta?.y ?? 0);
    }
    return activeCenterYOf(e);
}

function overVRectOf(e: DragOverEvent | DragEndEvent): VRect | null {
    const r = e.over?.rect;
    return r ? { top: r.top, height: r.height } : null;
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
    // Snapshot of every droppable's measured rect (viewport px), captured by
    // the collision detector during a drag, or null when unavailable (e.g. the
    // hook's own tests, or before the first collision pass). Drives the
    // geometry-based nearest-gap resolver; when absent the legacy hovered-chip
    // resolver is used, so existing behavior is unchanged.
    getRects?: () => Map<string, { left: number; width: number }> | null;
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
    getRects = () => null,
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
        (
            payload: DragPayload,
            overId: string,
            pointerX: number | null,
            overRect: Rect | null,
            pointerY: number | null,
            overVRect: VRect | null,
        ) => {
            if (payload.kind === "variant") {
                // Variant cards are a vertical stack: resolve on the Y axis.
                const lists = getVariantLists();
                return lists ? resolveVariantDropTarget(lists, overId, pointerY, overVRect) : null;
            }
            const containers = getContainers();
            if (!containers) {
                return null;
            }
            // Prefer the geometry resolver (nearest-gap over all chips) when
            // rects and a real pointer are available; it returns undefined to
            // defer to the legacy hovered-chip resolver when geometry is
            // incomplete, and null/DropTarget when it resolved on its own.
            const rects = getRects();
            if (rects && pointerX !== null) {
                const geo = resolveGeometricTarget(containers, overId, payload, pointerX, rects);
                if (geo !== undefined) {
                    return geo;
                }
            }
            return computeDropTarget(containers, overId, payload, pointerX, overRect);
        },
        [getContainers, getRects, getVariantLists],
    );

    const onDragOver = useCallback(
        (e: DragOverEvent) => {
            const payload = payloadRef.current;
            if (!payload) {
                return;
            }
            const next = e.over
                ? resolveTarget(
                      payload,
                      String(e.over.id),
                      pointerXOf(e),
                      overRectOf(e),
                      pointerYOf(e),
                      overVRectOf(e),
                  )
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
                const target = resolveTarget(
                    payload,
                    String(e.over.id),
                    pointerXOf(e),
                    overRectOf(e),
                    pointerYOf(e),
                    overVRectOf(e),
                );
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
