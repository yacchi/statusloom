// Immutable operations on the AST JSON document (types.ts). Node addressing
// uses the deterministic, position-derived IDs from DSL_API.md, so an
// operation navigates by parsing the ID path rather than by object identity.
//
// IMPORTANT: IDs are position-derived, so any structural change (insert /
// remove / move) makes the modified tree's IDs stale. Callers must treat the
// AST returned by the serialize -> parse round trip as the truth afterwards
// (App.tsx does exactly that); these helpers never recompute IDs themselves.
// For selection UX only, `predictChildId` names the ID a freshly inserted
// child will receive from that round trip.

import type {
    AstNode,
    CommentNode,
    LayoutNode,
    LineChild,
    LineNode,
    StatusloomNode,
} from "./types.ts";

// ---- ID paths ----

export type IdStep =
    | { t: "git" }
    | { t: "rootComment"; k: number }
    | { t: "layout"; i: number }
    | { t: "layoutComment"; k: number }
    | { t: "line"; j: number }
    | { t: "child"; k: number }
    | { t: "colorRule"; c: number };

// Parse a node ID into path steps ([] = the root). Returns null for a
// malformed ID.
export function parseNodeId(id: string): IdStep[] | null {
    if (id === "root") {
        return [];
    }
    if (id === "git") {
        return [{ t: "git" }];
    }
    const parts = id.split(".");
    const steps: IdStep[] = [];
    let idx = 0;

    if (parts[0] === "root") {
        const m = /^c(\d+)$/.exec(parts[1] ?? "");
        if (!m || parts.length !== 2) {
            return null;
        }
        return [{ t: "rootComment", k: Number(m[1]) }];
    }

    const lm = /^L(\d+)$/.exec(parts[0]);
    if (!lm) {
        return null;
    }
    steps.push({ t: "layout", i: Number(lm[1]) });
    idx = 1;

    if (idx < parts.length) {
        const cm = /^c(\d+)$/.exec(parts[idx]);
        if (cm) {
            if (parts.length !== idx + 1) {
                return null;
            }
            steps.push({ t: "layoutComment", k: Number(cm[1]) });
            return steps;
        }
        if (!/^\d+$/.test(parts[idx])) {
            return null;
        }
        steps.push({ t: "line", j: Number(parts[idx]) });
        idx += 1;
    }

    for (; idx < parts.length; idx += 1) {
        const cr = /^cr(\d+)$/.exec(parts[idx]);
        if (cr) {
            if (parts.length !== idx + 1) {
                return null; // color-rules are always leaves
            }
            steps.push({ t: "colorRule", c: Number(cr[1]) });
            return steps;
        }
        if (!/^\d+$/.test(parts[idx])) {
            return null;
        }
        steps.push({ t: "child", k: Number(parts[idx]) });
    }
    return steps;
}

// The parent ID of any node ID, or null for the root (which has no parent).
// Used by the minimal-diff dirty stamping: a structural edit (insert/remove/
// move) marks the affected *container* dirty so the serializer reconstructs
// it, while unchanged siblings are reused verbatim from the base source.
export function parentIdOf(id: string): string | null {
    if (id === "root") {
        return null;
    }
    if (id === "git" || /^root\.c\d+$/.test(id)) {
        return "root";
    }
    const cr = /^(.*)\.cr\d+$/.exec(id);
    if (cr) {
        return cr[1];
    }
    const layoutComment = /^(L\d+)\.c\d+$/.exec(id);
    if (layoutComment) {
        return layoutComment[1];
    }
    if (/^L\d+$/.test(id)) {
        return "root";
    }
    const dot = id.lastIndexOf(".");
    if (dot < 0) {
        return null;
    }
    return id.slice(0, dot);
}

// markDirty returns a shallow copy of `node` with the wire-only `dirty` flag
// set, so the minimal-diff serializer regenerates it rather than reusing its
// base-source slice.
function markDirty<T extends AstNode>(node: T): T {
    return { ...node, dirty: true };
}

// markDirtyById stamps `dirty` on the node addressed by `id` (a no-op when the
// id is null or does not resolve).
function markDirtyById(root: StatusloomNode, id: string | null): StatusloomNode {
    if (id === null) {
        return root;
    }
    return transformNode(root, id, (node) => markDirty(node));
}

// The parent ID of a line/span child ID ("L0.1.2" -> "L0.1"), or null when
// the ID does not address a mixed-content child.
export function parentChildId(id: string): { parentId: string; index: number } | null {
    const steps = parseNodeId(id);
    if (!steps || steps.length === 0) {
        return null;
    }
    const last = steps[steps.length - 1];
    if (last.t !== "child") {
        return null;
    }
    const dot = id.lastIndexOf(".");
    return { parentId: id.slice(0, dot), index: last.k };
}

// The ID the k-th mixed-content child of `parentId` (a line or span) will
// have after the next serialize -> parse round trip.
export function predictChildId(parentId: string, index: number): string {
    return `${parentId}.${index}`;
}

// ---- navigation ----

function childOf(node: AstNode, step: IdStep): AstNode | null {
    switch (step.t) {
        case "git":
            return node.kind === "statusloom" ? (node.git ?? null) : null;
        case "rootComment":
            return node.kind === "statusloom" ? (node.comments?.[step.k] ?? null) : null;
        case "layout":
            return node.kind === "statusloom" ? (node.layouts[step.i] ?? null) : null;
        case "layoutComment":
            return node.kind === "layout" ? (node.comments?.[step.k] ?? null) : null;
        case "line":
            return node.kind === "layout" ? (node.lines[step.j] ?? null) : null;
        case "child":
            return node.kind === "line" || node.kind === "span"
                ? (node.children[step.k] ?? null)
                : null;
        case "colorRule":
            return node.kind === "field" || node.kind === "span" || node.kind === "text"
                ? (node.colorRules?.[step.c] ?? null)
                : null;
    }
}

// Find a node by ID. Returns null when the ID is malformed or dangling.
export function getNode(root: StatusloomNode, id: string): AstNode | null {
    const steps = parseNodeId(id);
    if (!steps) {
        return null;
    }
    let node: AstNode = root;
    for (const step of steps) {
        const next = childOf(node, step);
        if (!next) {
            return null;
        }
        node = next;
    }
    return node;
}

// ---- generic immutable rewrite ----

// Rebuild the path down to `id`, applying `fn` to the addressed node.
// `fn` returning null removes the node (only supported where removal makes
// sense: layouts, lines, children, color rules, git, comments). Returns the
// original root when the ID is malformed/dangling or nothing changed.
export function transformNode(
    root: StatusloomNode,
    id: string,
    fn: (node: AstNode) => AstNode | null,
): StatusloomNode {
    const steps = parseNodeId(id);
    if (!steps) {
        return root;
    }
    const result = rewrite(root, steps, fn);
    if (result === undefined) {
        return root;
    }
    return (result ?? root) as StatusloomNode;
}

// rewrite returns the replacement node, null for "remove me", or undefined
// for "path did not resolve — leave the tree unchanged".
function rewrite(
    node: AstNode,
    steps: IdStep[],
    fn: (node: AstNode) => AstNode | null,
): AstNode | null | undefined {
    if (steps.length === 0) {
        return fn(node);
    }
    const [step, ...rest] = steps;
    const cur = childOf(node, step);
    if (!cur) {
        return undefined;
    }
    const next = rewrite(cur, rest, fn);
    if (next === undefined) {
        return undefined;
    }
    return replaceChild(node, step, next);
}

function replaceChild(node: AstNode, step: IdStep, next: AstNode | null): AstNode {
    switch (step.t) {
        case "git": {
            const r = node as StatusloomNode;
            const out = { ...r };
            if (next === null) {
                delete out.git;
            } else {
                out.git = next as StatusloomNode["git"];
            }
            return out;
        }
        case "rootComment": {
            const r = node as StatusloomNode;
            return { ...r, comments: spliceArr(r.comments ?? [], step.k, next as CommentNode | null) };
        }
        case "layout": {
            const r = node as StatusloomNode;
            return { ...r, layouts: spliceArr(r.layouts, step.i, next as LayoutNode | null) };
        }
        case "layoutComment": {
            const l = node as LayoutNode;
            return { ...l, comments: spliceArr(l.comments ?? [], step.k, next as CommentNode | null) };
        }
        case "line": {
            const l = node as LayoutNode;
            return { ...l, lines: spliceArr(l.lines, step.j, next as LineNode | null) };
        }
        case "child": {
            const p = node as LineNode; // line or span; both have `children`
            return { ...p, children: spliceArr(p.children, step.k, next as LineChild | null) };
        }
        case "colorRule": {
            const o = node as Extract<AstNode, { kind: "field" | "span" | "text" }>;
            const rules = spliceArr(o.colorRules ?? [], step.c, next as never);
            const out = { ...o };
            if (rules.length === 0) {
                delete out.colorRules;
            } else {
                out.colorRules = rules;
            }
            return out;
        }
    }
}

// Replace (or remove, when `next` is null) index i of `arr`.
function spliceArr<T>(arr: readonly T[], i: number, next: T | null): T[] {
    if (next === null) {
        return arr.filter((_, j) => j !== i);
    }
    return arr.map((v, j) => (j === i ? next : v));
}

// ---- attribute editing ----

// Attribute patch: keys present with `undefined` are REMOVED from the node
// (the wire contract omits unset attributes), other keys are set.
export type AttrPatch = Record<string, unknown>;

export function updateAttrs(root: StatusloomNode, id: string, patch: AttrPatch): StatusloomNode {
    // An attribute change is local to the node: stamp it dirty so only it is
    // regenerated (its parent reconstructs to place it, siblings reused).
    return transformNode(root, id, (node) => markDirty(applyPatch(node, patch)));
}

function applyPatch<T extends object>(node: T, patch: AttrPatch): T {
    const out = { ...node } as Record<string, unknown>;
    for (const [key, value] of Object.entries(patch)) {
        if (value === undefined) {
            delete out[key];
        } else {
            out[key] = value;
        }
    }
    return out as T;
}

// ---- mixed-content child operations (line/span children) ----

// A position within the current layout's lines grid.
export interface ChildAddress {
    line: number; // index into layout.lines
    index: number; // index into line.children
}

// Insert `child` into the mixed-content children of `parentId` (a line or
// span), clamping the index. A no-op for a non-container parent.
export function insertChild(
    root: StatusloomNode,
    parentId: string,
    index: number,
    child: LineChild,
): StatusloomNode {
    return transformNode(root, parentId, (node) => {
        if (node.kind !== "line" && node.kind !== "span") {
            return node;
        }
        const children = node.children;
        const at = Math.max(0, Math.min(index, children.length));
        // Stamp the container dirty: its child list changed, so it must be
        // reconstructed. The inserted child (no source range) is regenerated;
        // existing children are reused verbatim.
        return markDirty({
            ...node,
            children: [...children.slice(0, at), child, ...children.slice(at)],
        });
    });
}

export function removeNode(root: StatusloomNode, id: string): StatusloomNode {
    const parentId = parentIdOf(id);
    const removed = transformNode(root, id, () => null);
    if (removed === root) {
        return root; // id did not resolve; nothing removed
    }
    // The parent's child list changed: mark it dirty so the serializer
    // reconstructs it (dropping the removed node) and reuses the surviving
    // siblings verbatim. The parent's own id is unaffected by the removal.
    return markDirtyById(removed, parentId);
}

// Move the child at `fromId` so it lands at caret position `toIndex` of
// `toParentId` (a line or span). `toIndex` is interpreted against the target
// children as currently rendered (pre-removal); same-parent moves adjust for
// the removal, matching dnd-kit's arrayMove semantics. Returns `root`
// unchanged (same identity) for no-op moves.
export function moveChild(
    root: StatusloomNode,
    fromId: string,
    toParentId: string,
    toIndex: number,
): StatusloomNode {
    const from = parentChildId(fromId);
    const node = getNode(root, fromId);
    if (!from || !node || node.kind === "statusloom") {
        return root;
    }
    let index = toIndex;
    if (from.parentId === toParentId) {
        if (index > from.index) {
            index -= 1;
        }
        if (index === from.index) {
            return root;
        }
    }
    const target = getNode(root, toParentId);
    if (!target || (target.kind !== "line" && target.kind !== "span")) {
        return root;
    }
    // Don't allow dropping a span into its own subtree.
    if (toParentId === fromId || toParentId.startsWith(fromId + ".")) {
        return root;
    }
    const removed = removeNode(root, fromId);
    // Removal shifts the target parent's ID when both share a parent list and
    // the source precedes it; recompute the target ID against the new tree.
    const adjustedParentId = adjustIdAfterRemoval(toParentId, fromId);
    return insertChild(removed, adjustedParentId, index, node as LineChild);
}

// After removing `removedId`, sibling IDs following it in the same parent
// shift down by one. Adjust `id` (and its descendants' path) accordingly.
export function adjustIdAfterRemoval(id: string, removedId: string): string {
    const removed = parentChildId(removedId);
    if (!removed) {
        return id;
    }
    const prefix = removed.parentId + ".";
    if (!id.startsWith(prefix)) {
        return id;
    }
    const rest = id.slice(prefix.length);
    const tok = rest.split(".")[0];
    if (!/^\d+$/.test(tok)) {
        return id;
    }
    const k = Number(tok);
    if (k <= removed.index) {
        return id;
    }
    return prefix + String(k - 1) + rest.slice(tok.length);
}

// ---- line operations (within a layout) ----

export function addLine(root: StatusloomNode, layoutIndex: number): StatusloomNode {
    return transformNode(root, `L${layoutIndex}`, (node) => {
        if (node.kind !== "layout") {
            return node;
        }
        const line: LineNode = { id: "", kind: "line", children: [] };
        return { ...node, lines: [...node.lines, line] };
    });
}

export function deleteLine(
    root: StatusloomNode,
    layoutIndex: number,
    lineIndex: number,
): StatusloomNode {
    return removeNode(root, `L${layoutIndex}.${lineIndex}`);
}

// ---- drop application ----

// The line index (within the edited layout) a drop container belongs to, or
// null when the id addresses no line (e.g. "root").
export function lineIndexOfContainerId(id: string): number | null {
    const steps = parseNodeId(id);
    const line = steps?.find((s) => s.t === "line");
    return line && line.t === "line" ? line.j : null;
}

// Apply a drag & drop result to the tree: a palette payload inserts its node
// at the caret; a node payload moves the existing node there (with the same
// caret semantics as moveChild). The container may be a line or a span —
// structural changes always rebuild the target container node (and its path
// to the root), leaving every untouched sibling subtree identical, which is
// what marks the container as the changed ("dirty") node for serialization.
// Returns the new root (same identity for a no-op) plus the ID the affected
// child will have after the serialize -> parse round trip, for selection.
export function applyDropEdit(
    root: StatusloomNode,
    payload: { kind: "palette"; node: LineChild } | { kind: "node"; id: string },
    target: { containerId: string; index: number },
): { next: StatusloomNode; select: string | null } {
    const container = getNode(root, target.containerId);
    if (!container || (container.kind !== "line" && container.kind !== "span")) {
        return { next: root, select: null };
    }
    if (payload.kind === "palette") {
        const index = Math.max(0, Math.min(target.index, container.children.length));
        const next = insertChild(root, target.containerId, index, payload.node);
        if (next === root) {
            return { next: root, select: null };
        }
        return { next, select: predictChildId(target.containerId, index) };
    }
    const from = parentChildId(payload.id);
    const next = moveChild(root, payload.id, target.containerId, target.index);
    if (next === root) {
        return { next: root, select: null };
    }
    let index = target.index;
    if (from && from.parentId === target.containerId && index > from.index) {
        index -= 1;
    }
    // Removal may shift the target container's own ID (a span after the
    // source in the same child list); predict against the post-removal tree.
    const containerId = adjustIdAfterRemoval(target.containerId, payload.id);
    return { next, select: predictChildId(containerId, index) };
}

// ---- layout operations ----

function clamp(value: number, min: number, max: number): number {
    return Math.max(min, Math.min(value, max));
}

// Pick a name not already present in `existing`; disambiguates by appending
// " 2", " 3", ... so imported/duplicated layouts stay uniquely named.
function uniqueLayoutName(existing: readonly string[], base: string): string {
    const name = base.length > 0 ? base : "Layout";
    if (!existing.includes(name)) {
        return name;
    }
    let n = 2;
    while (existing.includes(`${name} ${n}`)) {
        n += 1;
    }
    return name + ` ${n}`;
}

// The index of the active layout (exactly one layout is active; a single
// layout may omit the flag and is implicitly active).
export function activeLayoutIndex(root: StatusloomNode): number {
    const i = root.layouts.findIndex((l) => l.active === true);
    return i >= 0 ? i : 0;
}

// Rewrite the `active` flags so exactly layout `index` is active. With a
// single layout the flag is omitted entirely (implicitly active).
function withActive(layouts: LayoutNode[], index: number): LayoutNode[] {
    return layouts.map((l, i) => {
        const out = { ...l };
        const before = out.active;
        if (layouts.length === 1) {
            delete out.active;
        } else if (i === index) {
            out.active = true;
        } else {
            delete out.active;
        }
        // A layout whose `active` flag actually changed is regenerated so the
        // minimal-diff serializer does not reuse its stale attribute.
        if (out.active !== before) {
            out.dirty = true;
        }
        return out;
    });
}

export function setActiveLayout(root: StatusloomNode, index: number): StatusloomNode {
    if (index < 0 || index >= root.layouts.length) {
        return root;
    }
    return { ...root, layouts: withActive(root.layouts, index) };
}

// Append a new, empty layout (one empty line). The active layout is
// unchanged; `active` flags are normalized so the invariant holds.
export function addLayout(root: StatusloomNode, name?: string): StatusloomNode {
    const existing = root.layouts.map((l) => l.name ?? "");
    const layoutName = uniqueLayoutName(existing, name ?? `Layout ${root.layouts.length + 1}`);
    const layout: LayoutNode = {
        id: "",
        kind: "layout",
        name: layoutName,
        lines: [{ id: "", kind: "line", children: [] }],
    };
    const active = activeLayoutIndex(root);
    // The layouts array changed: stamp the root so the serializer reconstructs
    // it (the new, range-less layout is regenerated; the rest reused verbatim).
    return markDirty({ ...root, layouts: withActive([...root.layouts, layout], active) });
}

// Delete a layout, keeping at least one. The active flag is re-pointed so it
// keeps marking the same logical layout where possible.
export function deleteLayout(root: StatusloomNode, index: number): StatusloomNode {
    if (root.layouts.length <= 1 || index < 0 || index >= root.layouts.length) {
        return root;
    }
    let active = activeLayoutIndex(root);
    const layouts = root.layouts.filter((_, i) => i !== index);
    if (active > index) {
        active -= 1;
    } else if (active === index) {
        active = clamp(index, 0, layouts.length - 1);
    }
    // The layouts array shrank: stamp the root so the serializer reconstructs
    // it (dropping the deleted layout) rather than reusing the base verbatim.
    return markDirty({
        ...root,
        layouts: withActive(layouts, clamp(active, 0, layouts.length - 1)),
    });
}

export function renameLayout(root: StatusloomNode, index: number, name: string): StatusloomNode {
    return transformNode(root, `L${index}`, (node) =>
        node.kind === "layout" ? markDirty({ ...node, name }) : node,
    );
}

// Duplicate a layout, inserting the copy (never active) immediately after the
// source, named "<name> copy" (disambiguated).
export function duplicateLayout(root: StatusloomNode, index: number): StatusloomNode {
    const src = root.layouts[index];
    if (!src) {
        return root;
    }
    const copy: LayoutNode = {
        ...structuredClone(src),
        id: "",
        name: uniqueLayoutName(
            root.layouts.map((l) => l.name ?? ""),
            `${src.name ?? "Layout"} copy`,
        ),
    };
    delete copy.active;
    const active = activeLayoutIndex(root);
    const layouts = [...root.layouts.slice(0, index + 1), copy, ...root.layouts.slice(index + 1)];
    return markDirty({ ...root, layouts: withActive(layouts, active <= index ? active : active + 1) });
}

// Append imported layouts (never active, names disambiguated). Returns the
// root unchanged when there is nothing to add.
export function appendLayouts(root: StatusloomNode, layouts: LayoutNode[]): StatusloomNode {
    if (layouts.length === 0) {
        return root;
    }
    const names = root.layouts.map((l) => l.name ?? "");
    const added: LayoutNode[] = [];
    for (const l of layouts) {
        const copy: LayoutNode = { ...structuredClone(l), id: "" };
        delete copy.active;
        copy.name = uniqueLayoutName(names, l.name ?? "Layout");
        names.push(copy.name);
        added.push(copy);
    }
    const active = activeLayoutIndex(root);
    return markDirty({ ...root, layouts: withActive([...root.layouts, ...added], active) });
}

// ---- root / git settings ----

// Patch the root's tool-level attributes (color-level, compact-threshold,
// context-percentage-mode, context-reserve-tokens). `undefined` removes.
export function updateRootAttrs(root: StatusloomNode, patch: AttrPatch): StatusloomNode {
    // Root tool-level attributes changed: stamp the root so the serializer
    // reconstructs its open tag (layouts/git/comments reused verbatim).
    return markDirty(applyPatch(root, patch));
}

// Patch the optional <git/> element's attributes. Creates the element when
// needed; removes it entirely when the patch leaves no attributes set.
export function updateGitAttrs(root: StatusloomNode, patch: AttrPatch): StatusloomNode {
    const cur = root.git ?? { id: "git", kind: "git" as const };
    const next = applyPatch(cur, patch);
    const hasAny = Object.keys(next).some(
        (k) => k !== "id" && k !== "kind" && k !== "range" && k !== "dirty",
    );
    // Stamp the root dirty so the serializer reconstructs it: this reflects a
    // git element that was added, changed, or (when no attributes remain)
    // removed — a case root-verbatim reuse would otherwise miss.
    const out = markDirty({ ...root });
    if (hasAny) {
        out.git = markDirty(next);
    } else {
        delete out.git;
    }
    return out;
}
