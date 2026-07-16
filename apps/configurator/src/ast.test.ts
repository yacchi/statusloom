import { describe, expect, it } from "vitest";
import {
    activeLayoutIndex,
    addLayout,
    addLine,
    addResponsive,
    addSubagent,
    addVariant,
    adjustIdAfterRemoval,
    appendLayouts,
    applyDropEdit,
    deleteLayout,
    deleteLine,
    deleteSubagent,
    deleteVariant,
    duplicateChild,
    duplicateLayout,
    duplicateLineNode,
    duplicateVariant,
    fillSubagentDefault,
    getNode,
    insertChild,
    insertVariant,
    lineIndexOfContainerId,
    moveChild,
    moveVariant,
    parentChildId,
    parentIdOf,
    parseNodeId,
    predictChildId,
    removeNode,
    renameLayout,
    setActiveLayout,
    transformNode,
    updateAttrs,
    updateGitAttrs,
    updateRootAttrs,
} from "./ast.ts";
import { makeDefaultSubagentLine } from "./presets.ts";
import { assignIds, doc, fld, lay, ln, resp, spn, txt, variant } from "./test/fakeDsl.ts";
import type {
    FieldNode,
    LineChild,
    LineNode,
    ResponsiveNode,
    SpanNode,
    StatusloomNode,
    SubagentNode,
    VariantNode,
} from "./types.ts";

// ---- subagent-region test fixtures ----
//
// The fakeDsl assignIds helper does not descend into <subagent> regions (they
// live in a dedicated field, not Children), so these stamp the backend-scheme
// ids ("L{i}.s" / "L{i}.{p}.v{v}.s", children "...s.{k}") the region and its
// single shared-id <line> would receive from a real parse.
function saRegion(children: LineChild[]): SubagentNode {
    return { id: "", kind: "subagent", line: { id: "", kind: "line", children } };
}

function stampSubagent(sub: SubagentNode, id: string): void {
    sub.id = id;
    sub.line.id = id;
    sub.line.children.forEach((c, k) => {
        c.id = `${id}.${k}`;
    });
}

function assignSubagentIds(root: StatusloomNode): StatusloomNode {
    root.layouts.forEach((l, i) => {
        if (l.subagent) {
            stampSubagent(l.subagent, `L${i}.s`);
        }
        l.children.forEach((child, p) => {
            if (child.kind === "responsive") {
                child.variants.forEach((v, vi) => {
                    if (v.subagent) {
                        stampSubagent(v.subagent, `L${i}.${p}.v${vi}.s`);
                    }
                });
            }
        });
    });
    return root;
}

// A responsive-free layout carrying a layout-direct <subagent> region:
// L0.0: [model] (plain line); L0.s: subagent line [task-description, task-model]
function subagentLayoutFixture(): StatusloomNode {
    const root = doc([lay("Default", [ln([fld("model")])], true)]);
    root.layouts[0].subagent = saRegion([fld("task-description"), fld("task-model")]);
    return assignSubagentIds(assignIds(root));
}

// A responsive layout whose two variants each carry their own <subagent>:
// L0.0: [model]; L0.1 responsive: v0 (line [git-branch], subagent [task-description]),
// v1 (line [session-cost], subagent [task-model]).
function subagentVariantFixture(): StatusloomNode {
    const root = doc([
        lay(
            "Default",
            [
                ln([fld("model")]),
                resp([variant([ln([fld("git-branch")])]), variant([ln([fld("session-cost")])])]),
            ],
            true,
        ),
    ]);
    const responsive = root.layouts[0].children[1] as ResponsiveNode;
    responsive.variants[0].subagent = saRegion([fld("task-description")]);
    responsive.variants[1].subagent = saRegion([fld("task-model")]);
    return assignSubagentIds(assignIds(root));
}

// A document with ids assigned per the backend scheme:
// L0.0: [model, span[thinking-effort], text"|"], L0.1: [git-branch]
// L1.0: [model]
function fixture(): StatusloomNode {
    return assignIds(
        doc([
            lay(
                "Default",
                [ln([fld("model"), spn([fld("thinking-effort")]), txt("|")]), ln([fld("git-branch")])],
                true,
            ),
            lay("Compact", [ln([fld("model")])]),
        ]),
    );
}

// A document whose layout mixes a plain line with a <responsive> (two
// variants: a one-line wide candidate and a one-line fallback), per
// plans/responsive-container-design.md §10:
// L0.0: [model] (plain line)
// L0.1: responsive, variant 0 (L0.1.v0.0: [git-branch]), variant 1 (L0.1.v1.0: [session-cost])
function responsiveFixture(): StatusloomNode {
    return assignIds(
        doc([
            lay(
                "Default",
                [
                    ln([fld("model")]),
                    resp([variant([ln([fld("git-branch")])]), variant([ln([fld("session-cost")])])]),
                ],
                true,
            ),
        ]),
    );
}

function childNames(root: StatusloomNode, layout: number, line: number): string[] {
    const l = root.layouts[layout].children[line] as LineNode;
    return l.children.map((c) => {
        if (c.kind === "field") {
            return c.name ?? "";
        }
        if (c.kind === "span") {
            return "span";
        }
        if (c.kind === "text") {
            return c.value;
        }
        return c.kind;
    });
}

describe("parseNodeId", () => {
    it("parses the documented forms", () => {
        expect(parseNodeId("root")).toEqual([]);
        expect(parseNodeId("git")).toEqual([{ t: "git" }]);
        expect(parseNodeId("root.c1")).toEqual([{ t: "rootComment", k: 1 }]);
        expect(parseNodeId("L2")).toEqual([{ t: "layout", i: 2 }]);
        expect(parseNodeId("L0.c0")).toEqual([
            { t: "layout", i: 0 },
            { t: "layoutComment", k: 0 },
        ]);
        // A responsive-free layout's line at position p keeps the historical
        // "L{i}.{p}" form (DSL_API.md "Node IDs").
        expect(parseNodeId("L0.1")).toEqual([
            { t: "layout", i: 0 },
            { t: "layoutChild", p: 1 },
        ]);
        expect(parseNodeId("L0.1.2.0")).toEqual([
            { t: "layout", i: 0 },
            { t: "layoutChild", p: 1 },
            { t: "child", k: 2 },
            { t: "child", k: 0 },
        ]);
        expect(parseNodeId("L0.0.1.cr2")).toEqual([
            { t: "layout", i: 0 },
            { t: "layoutChild", p: 0 },
            { t: "child", k: 1 },
            { t: "colorRule", c: 2 },
        ]);
    });

    it("parses responsive/variant forms", () => {
        expect(parseNodeId("L0.1.c0")).toEqual([
            { t: "layout", i: 0 },
            { t: "layoutChild", p: 1 },
            { t: "responsiveComment", k: 0 },
        ]);
        expect(parseNodeId("L0.1.v0")).toEqual([
            { t: "layout", i: 0 },
            { t: "layoutChild", p: 1 },
            { t: "variant", v: 0 },
        ]);
        expect(parseNodeId("L0.1.v0.c1")).toEqual([
            { t: "layout", i: 0 },
            { t: "layoutChild", p: 1 },
            { t: "variant", v: 0 },
            { t: "variantComment", k: 1 },
        ]);
        expect(parseNodeId("L0.1.v0.2")).toEqual([
            { t: "layout", i: 0 },
            { t: "layoutChild", p: 1 },
            { t: "variant", v: 0 },
            { t: "variantLine", j: 2 },
        ]);
        expect(parseNodeId("L0.1.v0.2.3")).toEqual([
            { t: "layout", i: 0 },
            { t: "layoutChild", p: 1 },
            { t: "variant", v: 0 },
            { t: "variantLine", j: 2 },
            { t: "child", k: 3 },
        ]);
    });

    it("parses subagent forms", () => {
        // A layout-direct region's line shares the region's "L{i}.s" id; its
        // children continue with the plain "child" tail.
        expect(parseNodeId("L0.s")).toEqual([
            { t: "layout", i: 0 },
            { t: "subagent" },
        ]);
        expect(parseNodeId("L0.s.1")).toEqual([
            { t: "layout", i: 0 },
            { t: "subagent" },
            { t: "child", k: 1 },
        ]);
        expect(parseNodeId("L0.s.0.cr1")).toEqual([
            { t: "layout", i: 0 },
            { t: "subagent" },
            { t: "child", k: 0 },
            { t: "colorRule", c: 1 },
        ]);
        // A variant-nested region: "...v{v}.s...".
        expect(parseNodeId("L0.1.v0.s")).toEqual([
            { t: "layout", i: 0 },
            { t: "layoutChild", p: 1 },
            { t: "variant", v: 0 },
            { t: "subagent" },
        ]);
        expect(parseNodeId("L0.1.v0.s.2")).toEqual([
            { t: "layout", i: 0 },
            { t: "layoutChild", p: 1 },
            { t: "variant", v: 0 },
            { t: "subagent" },
            { t: "child", k: 2 },
        ]);
    });

    it("rejects malformed ids", () => {
        expect(parseNodeId("")).toBeNull();
        expect(parseNodeId("X0")).toBeNull();
        expect(parseNodeId("L0.x")).toBeNull();
        expect(parseNodeId("L0.1.cr0.0")).toBeNull();
        expect(parseNodeId("root.c1.0")).toBeNull();
        expect(parseNodeId("L0.1.v0.x")).toBeNull();
        expect(parseNodeId("L0.1.vX")).toBeNull();
        expect(parseNodeId("L0.s.x")).toBeNull();
        expect(parseNodeId("L0.s.0.cr0.0")).toBeNull(); // color-rules are leaves
    });
});

describe("getNode", () => {
    const root = fixture();

    it("resolves nested paths", () => {
        expect(getNode(root, "root")).toBe(root);
        expect((getNode(root, "L1") as { name?: string }).name).toBe("Compact");
        expect((getNode(root, "L0.0.0") as FieldNode).name).toBe("model");
        expect((getNode(root, "L0.0.1.0") as FieldNode).name).toBe("thinking-effort");
        expect((getNode(root, "L0.0.2") as { value?: string }).value).toBe("|");
    });

    it("returns null for dangling or malformed ids", () => {
        expect(getNode(root, "L9")).toBeNull();
        expect(getNode(root, "L0.0.9")).toBeNull();
        expect(getNode(root, "bogus")).toBeNull();
        expect(getNode(root, "git")).toBeNull(); // no <git/> in the fixture
    });
});

describe("parentChildId / predictChildId / adjustIdAfterRemoval", () => {
    it("splits a child id into parent + index", () => {
        expect(parentChildId("L0.0.2")).toEqual({ parentId: "L0.0", index: 2 });
        expect(parentChildId("L0.0.1.0")).toEqual({ parentId: "L0.0.1", index: 0 });
        expect(parentChildId("L0.0")).toBeNull(); // a line, not a child
        expect(parentChildId("root")).toBeNull();
    });

    it("predicts the id an inserted child receives", () => {
        expect(predictChildId("L0.1", 3)).toBe("L0.1.3");
    });

    it("shifts sibling ids after a removal", () => {
        expect(adjustIdAfterRemoval("L0.0.2", "L0.0.1")).toBe("L0.0.1");
        expect(adjustIdAfterRemoval("L0.0.1", "L0.0.1")).toBe("L0.0.1");
        expect(adjustIdAfterRemoval("L0.0.0", "L0.0.1")).toBe("L0.0.0");
        // Descendant paths shift with their ancestor.
        expect(adjustIdAfterRemoval("L0.0.2.0", "L0.0.1")).toBe("L0.0.1.0");
        // Other parents are untouched.
        expect(adjustIdAfterRemoval("L0.1.0", "L0.0.1")).toBe("L0.1.0");
    });
});

describe("updateAttrs", () => {
    it("sets and removes attributes immutably", () => {
        const root = fixture();
        const next = updateAttrs(root, "L0.0.0", { color: "cyan", bold: true });
        expect((getNode(next, "L0.0.0") as FieldNode).color).toBe("cyan");
        expect((getNode(root, "L0.0.0") as FieldNode).color).toBeUndefined();

        const cleared = updateAttrs(next, "L0.0.0", { color: undefined });
        const node = getNode(cleared, "L0.0.0") as FieldNode;
        expect("color" in node).toBe(false);
        expect(node.bold).toBe(true);
    });

    it("returns the same root for a dangling id", () => {
        const root = fixture();
        expect(updateAttrs(root, "L9.0.0", { color: "red" })).toBe(root);
    });
});

describe("insertChild / removeNode / moveChild", () => {
    it("inserts into a line at a clamped index", () => {
        const root = fixture();
        const next = insertChild(root, "L0.1", 99, txt("x"));
        expect(childNames(next, 0, 1)).toEqual(["git-branch", "x"]);
    });

    it("inserts into a span", () => {
        const root = fixture();
        const next = insertChild(root, "L0.0.1", 0, fld("model"));
        const span = getNode(next, "L0.0.1") as SpanNode;
        expect(span.children.map((c) => (c as FieldNode).name)).toEqual([
            "model",
            "thinking-effort",
        ]);
    });

    it("removes a child (and a nested child)", () => {
        const root = fixture();
        expect(childNames(removeNode(root, "L0.0.1"), 0, 0)).toEqual(["model", "|"]);
        const inner = removeNode(root, "L0.0.1.0");
        expect((getNode(inner, "L0.0.1") as SpanNode).children).toEqual([]);
    });

    it("removes a whole line", () => {
        const root = fixture();
        const next = deleteLine(root, "L0.0");
        expect(next.layouts[0].children).toHaveLength(1);
        expect(childNames(next, 0, 0)).toEqual(["git-branch"]);
    });

    it("moves forward within a line with caret semantics", () => {
        const root = fixture();
        // caret index 2 (before "|"): model lands between span and "|"
        const next = moveChild(root, "L0.0.0", "L0.0", 2);
        expect(childNames(next, 0, 0)).toEqual(["span", "model", "|"]);
        // caret at the very end
        const end = moveChild(root, "L0.0.0", "L0.0", 3);
        expect(childNames(end, 0, 0)).toEqual(["span", "|", "model"]);
    });

    it("moves backward within a line", () => {
        const root = fixture();
        const next = moveChild(root, "L0.0.2", "L0.0", 0);
        expect(childNames(next, 0, 0)).toEqual(["|", "model", "span"]);
    });

    it("returns the same root for a no-op move", () => {
        const root = fixture();
        expect(moveChild(root, "L0.0.0", "L0.0", 0)).toBe(root);
        expect(moveChild(root, "L0.0.0", "L0.0", 1)).toBe(root);
    });

    it("moves across lines", () => {
        const root = fixture();
        const next = moveChild(root, "L0.0.0", "L0.1", 1);
        expect(childNames(next, 0, 0)).toEqual(["span", "|"]);
        expect(childNames(next, 0, 1)).toEqual(["git-branch", "model"]);
    });

    it("refuses to move a span into its own subtree", () => {
        const root = fixture();
        expect(moveChild(root, "L0.0.1", "L0.0.1", 0)).toBe(root);
    });
});

describe("layout operations", () => {
    it("addLine appends an empty line", () => {
        const root = fixture();
        const next = addLine(root, "L1");
        expect(next.layouts[1].children).toHaveLength(2);
        expect((next.layouts[1].children[1] as LineNode).children).toEqual([]);
    });

    it("addLayout appends an inactive layout with a unique name", () => {
        const root = fixture();
        const next = addLayout(addLayout(root));
        expect(next.layouts.map((l) => l.name)).toEqual([
            "Default",
            "Compact",
            "Layout 3",
            "Layout 4",
        ]);
        expect(activeLayoutIndex(next)).toBe(0);
        expect(next.layouts[2].active).toBeUndefined();
    });

    it("setActiveLayout keeps exactly one layout active", () => {
        const root = fixture();
        const next = setActiveLayout(root, 1);
        expect(next.layouts[0].active).toBeUndefined();
        expect(next.layouts[1].active).toBe(true);
        expect(activeLayoutIndex(next)).toBe(1);
    });

    it("deleteLayout keeps at least one and re-points the active flag", () => {
        const root = fixture();
        expect(deleteLayout(deleteLayout(root, 1), 0)).toEqual(deleteLayout(root, 1));
        // Deleting the active layout activates the survivor...
        const next = deleteLayout(root, 0);
        expect(next.layouts).toHaveLength(1);
        expect(activeLayoutIndex(next)).toBe(0);
        // ...and a single layout omits the flag entirely (implicitly active).
        expect(next.layouts[0].active).toBeUndefined();
    });

    it("renameLayout / duplicateLayout", () => {
        const root = fixture();
        expect(renameLayout(root, 1, "Wide").layouts[1].name).toBe("Wide");
        const dup = duplicateLayout(root, 0);
        expect(dup.layouts.map((l) => l.name)).toEqual(["Default", "Default copy", "Compact"]);
        expect(dup.layouts[1].active).toBeUndefined();
        expect(activeLayoutIndex(dup)).toBe(0);
        // The copy shares no node identity with the source.
        expect(dup.layouts[1].children[0]).not.toBe(dup.layouts[0].children[0]);
    });

    it("appendLayouts disambiguates names and never imports an active flag", () => {
        const root = fixture();
        const incoming = [lay("Default", [ln([fld("model")])], true)];
        const next = appendLayouts(root, incoming);
        expect(next.layouts.map((l) => l.name)).toEqual(["Default", "Compact", "Default 2"]);
        expect(next.layouts[2].active).toBeUndefined();
        expect(activeLayoutIndex(next)).toBe(0);
    });
});

// A layout child at position p is either a line or a responsive; these
// operations manage the latter (plans/responsive-container-design.md).
describe("responsive / variant operations", () => {
    const isDirty = (root: StatusloomNode, id: string): boolean =>
        (getNode(root, id) as { dirty?: boolean } | null)?.dirty === true;

    it("resolves responsive/variant node paths", () => {
        const root = responsiveFixture();
        expect((getNode(root, "L0.1") as ResponsiveNode).kind).toBe("responsive");
        expect((getNode(root, "L0.1.v0") as VariantNode).kind).toBe("variant");
        expect((getNode(root, "L0.1.v0.0") as LineNode).kind).toBe("line");
        expect((getNode(root, "L0.1.v0.0.0") as FieldNode).name).toBe("git-branch");
        expect((getNode(root, "L0.1.v1.0.0") as FieldNode).name).toBe("session-cost");
    });

    it("addResponsive appends a two-variant, one-line-each responsive", () => {
        const root = fixture();
        const next = addResponsive(root, 1);
        const responsive = next.layouts[1].children[1] as ResponsiveNode;
        expect(responsive.kind).toBe("responsive");
        expect(responsive.variants).toHaveLength(2);
        expect(responsive.variants[0].lines).toHaveLength(1);
        expect(responsive.variants[0].lines[0].children).toEqual([]);
    });

    it("addVariant appends an empty-line variant", () => {
        const root = responsiveFixture();
        const next = addVariant(root, "L0.1");
        const responsive = next.layouts[0].children[1] as ResponsiveNode;
        expect(responsive.variants).toHaveLength(3);
        expect(responsive.variants[2].lines).toHaveLength(1);
    });

    it("insertVariant clamps the index", () => {
        const root = responsiveFixture();
        const newVariant: VariantNode = { id: "", kind: "variant", lines: [ln([fld("model")])] };
        const next = insertVariant(root, "L0.1", 0, newVariant);
        const responsive = next.layouts[0].children[1] as ResponsiveNode;
        expect(responsive.variants).toHaveLength(3);
        expect((responsive.variants[0].lines[0].children[0] as FieldNode).name).toBe("model");
    });

    it("deleteVariant removes a variant but keeps at least one", () => {
        const root = responsiveFixture();
        const next = deleteVariant(root, "L0.1.v0");
        const responsive = next.layouts[0].children[1] as ResponsiveNode;
        expect(responsive.variants).toHaveLength(1);
        expect((responsive.variants[0].lines[0].children[0] as FieldNode).name).toBe(
            "session-cost",
        );
        // Refuses to drop the last variant.
        expect(deleteVariant(next, "L0.1.v0")).toBe(next);
    });

    it("moveVariant reorders within the same responsive", () => {
        const root = responsiveFixture();
        const next = moveVariant(root, "L0.1.v1", "L0.1", 0);
        const responsive = next.layouts[0].children[1] as ResponsiveNode;
        expect((responsive.variants[0].lines[0].children[0] as FieldNode).name).toBe(
            "session-cost",
        );
        expect((responsive.variants[1].lines[0].children[0] as FieldNode).name).toBe(
            "git-branch",
        );
        // No-op moves return the same root.
        expect(moveVariant(root, "L0.1.v0", "L0.1", 0)).toBe(root);
        expect(moveVariant(root, "L0.1.v0", "L0.1", 1)).toBe(root);
    });

    it("lineIndexOfContainerId resolves the owning responsive's position for a variant-nested line", () => {
        expect(lineIndexOfContainerId("L0.1.v0.0")).toBe(1);
        expect(lineIndexOfContainerId("L0.1.v0.0.0")).toBe(1);
    });

    it("deleteVariant stamps the responsive whose variant list shrank", () => {
        // deleteVariant delegates to removeNode, which stamps the parent
        // dirty so the serializer reconstructs it; addVariant/moveVariant
        // append/reorder without a range, matching addLine's convention of
        // relying on the missing range alone to trigger regeneration.
        const root = responsiveFixture();
        const removed = deleteVariant(root, "L0.1.v0");
        expect(isDirty(removed, "L0.1")).toBe(true);
    });
});

// duplicateChild/duplicateLineNode/duplicateVariant mirror duplicateLayout:
// an id-reset structuredClone spliced in immediately after the source. Their
// truth is the serialize -> parse round trip (App.tsx), so these unit tests
// only assert structure/order/content, not the copy's post-round-trip id.
describe("duplicate operations", () => {
    it("duplicateChild inserts a copy of a top-level chip right after the source", () => {
        const root = fixture();
        const { next, select } = duplicateChild(root, "L0.0.0");
        expect(childNames(next, 0, 0)).toEqual(["model", "model", "span", "|"]);
        expect(select).toBe(predictChildId("L0.0", 1));
        const src = getNode(root, "L0.0.0") as FieldNode;
        const copy = getNode(next, "L0.0.1") as FieldNode;
        expect(copy.name).toBe(src.name);
        expect(copy.id).toBe("");
    });

    it("duplicateChild duplicates a span (with its children) in place", () => {
        const root = fixture();
        const { next, select } = duplicateChild(root, "L0.0.1");
        const line = next.layouts[0].children[0] as LineNode;
        expect(line.children.map((c) => c.kind)).toEqual(["field", "span", "span", "text"]);
        expect(select).toBe(predictChildId("L0.0", 2));
        const copy = line.children[2] as SpanNode;
        expect(copy.children.map((c) => (c as FieldNode).name)).toEqual(["thinking-effort"]);
        expect(copy.id).toBe("");
    });

    it("duplicateChild is a no-op for a non-child id", () => {
        const root = fixture();
        expect(duplicateChild(root, "L0.0")).toEqual({ next: root, select: null });
        expect(duplicateChild(root, "root")).toEqual({ next: root, select: null });
    });

    it("duplicateLineNode duplicates a top-level line right after the source", () => {
        const root = fixture();
        const next = duplicateLineNode(root, "L0.0");
        expect(next.layouts[0].children).toHaveLength(3);
        expect(childNames(next, 0, 0)).toEqual(["model", "span", "|"]);
        expect(childNames(next, 0, 1)).toEqual(["model", "span", "|"]);
        expect(childNames(next, 0, 2)).toEqual(["git-branch"]);
        const copy = next.layouts[0].children[1] as LineNode;
        expect(copy.id).toBe("");
        expect(copy).not.toBe(next.layouts[0].children[0]);
    });

    it("duplicateLineNode duplicates a variant-nested line right after the source", () => {
        const root = responsiveFixture();
        const next = duplicateLineNode(root, "L0.1.v0.0");
        const responsive = next.layouts[0].children[1] as ResponsiveNode;
        expect(responsive.variants[0].lines).toHaveLength(2);
        expect(responsive.variants[1].lines).toHaveLength(1); // untouched sibling variant
        const [first, second] = responsive.variants[0].lines;
        expect((first.children[0] as FieldNode).name).toBe("git-branch");
        expect((second.children[0] as FieldNode).name).toBe("git-branch");
        expect(second.id).toBe("");
        expect(second).not.toBe(first);
    });

    it("duplicateLineNode is a no-op for a responsive (not a plain line)", () => {
        const root = responsiveFixture();
        expect(duplicateLineNode(root, "L0.1")).toBe(root);
    });

    it("duplicateVariant inserts a copy right after the source variant", () => {
        const root = responsiveFixture();
        const next = duplicateVariant(root, "L0.1.v0");
        const responsive = next.layouts[0].children[1] as ResponsiveNode;
        expect(responsive.variants).toHaveLength(3);
        expect((responsive.variants[0].lines[0].children[0] as FieldNode).name).toBe(
            "git-branch",
        );
        expect((responsive.variants[1].lines[0].children[0] as FieldNode).name).toBe(
            "git-branch",
        );
        expect((responsive.variants[2].lines[0].children[0] as FieldNode).name).toBe(
            "session-cost",
        );
        expect(responsive.variants[1].id).toBe("");
        expect(responsive.variants[1]).not.toBe(responsive.variants[0]);
        // Regression guard: insertVariant must mark the <responsive> dirty so
        // the serializer reconstructs its variant list. Without the flag, the
        // duplicated variant (which retains the source clone's range) is
        // dropped on the serialize -> parse round trip.
        expect(getNode(next, "L0.1")?.dirty).toBe(true);
    });

    it("duplicateVariant is a no-op for a non-variant id", () => {
        const root = responsiveFixture();
        expect(duplicateVariant(root, "L0.1")).toBe(root);
    });
});

describe("updateGitAttrs", () => {
    it("creates the git element on first set and drops it when cleared", () => {
        const root = fixture();
        const withGit = updateGitAttrs(root, { "cache-ttl-ms": 5000 });
        expect(withGit.git?.["cache-ttl-ms"]).toBe(5000);
        const cleared = updateGitAttrs(withGit, { "cache-ttl-ms": undefined });
        expect(cleared.git).toBeUndefined();
    });
});

// Minimal-diff dirty stamping: edit helpers mark the node(s) whose content
// changed so POST /api/dsl/serialize (with baseSource) regenerates only those
// and reuses the rest verbatim. `dirty` is a wire-only flag.
describe("dirty stamping (minimal-diff)", () => {
    const isDirty = (root: StatusloomNode, id: string): boolean =>
        (getNode(root, id) as { dirty?: boolean } | null)?.dirty === true;

    it("parentIdOf resolves the container of every id form", () => {
        expect(parentIdOf("root")).toBeNull();
        expect(parentIdOf("git")).toBe("root");
        expect(parentIdOf("root.c0")).toBe("root");
        expect(parentIdOf("L0")).toBe("root");
        expect(parentIdOf("L0.c1")).toBe("L0");
        expect(parentIdOf("L0.1")).toBe("L0");
        expect(parentIdOf("L0.0.2")).toBe("L0.0");
        expect(parentIdOf("L0.0.1.0")).toBe("L0.0.1");
        expect(parentIdOf("L0.0.0.cr1")).toBe("L0.0.0");
    });

    it("updateAttrs stamps only the edited node", () => {
        const root = fixture();
        const next = updateAttrs(root, "L0.0.0", { color: "cyan" });
        expect(isDirty(next, "L0.0.0")).toBe(true);
        // Siblings and the parent line are not stamped by an attribute edit.
        expect(isDirty(next, "L0.0.2")).toBe(false);
        expect(isDirty(next, "L0.0")).toBe(false);
    });

    it("insertChild stamps the parent container", () => {
        const root = fixture();
        const next = insertChild(root, "L0.1", 0, txt("x"));
        expect(isDirty(next, "L0.1")).toBe(true);
    });

    it("removeNode stamps the parent container (which drops the child)", () => {
        const root = fixture();
        const next = removeNode(root, "L0.0.2");
        expect(isDirty(next, "L0.0")).toBe(true);
        // The surviving siblings are not individually stamped (reused verbatim).
        expect(isDirty(next, "L0.0.0")).toBe(false);
    });

    it("moveChild stamps both containers but not the moved node", () => {
        const root = fixture();
        // Move model (L0.0.0) into the second line.
        const next = moveChild(root, "L0.0.0", "L0.1", 1);
        expect(isDirty(next, "L0.0")).toBe(true); // source line reconstructs
        expect(isDirty(next, "L0.1")).toBe(true); // target line reconstructs
        // The moved node itself stays clean so its source is reused verbatim.
        const moved = getNode(next, "L0.1.1") as { name?: string; dirty?: boolean };
        expect(moved.name).toBe("model");
        expect(moved.dirty).not.toBe(true);
    });

    it("moveChild within a line stamps that line", () => {
        const root = fixture();
        const next = moveChild(root, "L0.0.0", "L0.0", 2);
        expect(isDirty(next, "L0.0")).toBe(true);
    });

    it("updateRootAttrs stamps the root", () => {
        const root = fixture();
        const next = updateRootAttrs(root, { "color-level": "truecolor" });
        expect(next.dirty).toBe(true);
    });

    it("updateGitAttrs stamps the root (git add/change/remove)", () => {
        const root = fixture();
        const next = updateGitAttrs(root, { "cache-ttl-ms": 5000 });
        expect(next.dirty).toBe(true);
        expect(next.git?.dirty).toBe(true);
    });

    it("setActiveLayout stamps the layouts whose active flag changed", () => {
        const root = fixture();
        const next = setActiveLayout(root, 1);
        expect(isDirty(next, "L0")).toBe(true); // was active, now not
        expect(isDirty(next, "L1")).toBe(true); // now active
    });

    it("deleteLayout stamps the root", () => {
        const root = fixture();
        const next = deleteLayout(root, 1);
        expect(next.dirty).toBe(true);
    });

    it("renameLayout stamps the renamed layout", () => {
        const root = fixture();
        const next = renameLayout(root, 1, "Wide");
        expect(isDirty(next, "L1")).toBe(true);
    });
});

describe("transformNode", () => {
    it("drops an empty colorRules array after removing the last rule", () => {
        let root = fixture();
        root = updateAttrs(root, "L0.0.0", {
            colorRules: [{ id: "", kind: "color-rule", when: "self ge 90", color: "red" }],
        });
        const removed = transformNode(root, "L0.0.0.cr0", () => null);
        const field = getNode(removed, "L0.0.0") as FieldNode;
        expect("colorRules" in field).toBe(false);
    });
});

describe("lineIndexOfContainerId", () => {
    it("extracts the line index from line and span container ids", () => {
        expect(lineIndexOfContainerId("L0.2")).toBe(2);
        expect(lineIndexOfContainerId("L1.0.3")).toBe(0);
        expect(lineIndexOfContainerId("L0.1.2.0")).toBe(1);
        expect(lineIndexOfContainerId("root")).toBeNull();
        expect(lineIndexOfContainerId("L0")).toBeNull();
    });
});

// applyDropEdit turns a drag & drop result into an AST edit. The container
// may be a line OR a span (nested included).
describe("applyDropEdit", () => {
    const isDirty = (root: StatusloomNode, id: string): boolean =>
        (getNode(root, id) as { dirty?: boolean } | null)?.dirty === true;

    it("palette drop into a span inserts there and stamps the span dirty", () => {
        const root = fixture();
        const { next, select } = applyDropEdit(
            root,
            { kind: "palette", node: fld("model") },
            { containerId: "L0.0.1", index: 1 },
        );
        const span = getNode(next, "L0.0.1") as SpanNode;
        expect(span.children.map((c) => (c as FieldNode).name)).toEqual([
            "thinking-effort",
            "model",
        ]);
        expect(select).toBe("L0.0.1.1");
        // Dirty is stamped on the span container, not on untouched siblings.
        expect(isDirty(next, "L0.0.1")).toBe(true);
        expect(isDirty(next, "L0.0.0")).toBe(false);
        expect(isDirty(next, "L0.1")).toBe(false);
        // Untouched sibling subtrees keep their identity (minimal rebuild).
        expect(getNode(next, "L0.0.0")).toBe(getNode(root, "L0.0.0"));
        expect(next.layouts[1]).toBe(root.layouts[1]);
    });

    it("palette drop clamps the caret index into the span", () => {
        const root = fixture();
        const { next, select } = applyDropEdit(
            root,
            { kind: "palette", node: txt("!") },
            { containerId: "L0.0.1", index: 99 },
        );
        const span = getNode(next, "L0.0.1") as SpanNode;
        expect(span.children).toHaveLength(2);
        expect(select).toBe("L0.0.1.1");
    });

    it("moves an existing chip from the line into a span (both containers stamped)", () => {
        const root = fixture();
        const { next, select } = applyDropEdit(
            root,
            { kind: "node", id: "L0.0.0" },
            { containerId: "L0.0.1", index: 0 },
        );
        // The line lost its first child; the span gained it.
        expect((getNode(next, "L0.0.0") as SpanNode).kind).toBe("span");
        const span = getNode(next, "L0.0.0") as SpanNode;
        expect(span.children.map((c) => (c as FieldNode).name)).toEqual([
            "model",
            "thinking-effort",
        ]);
        // The span was at index 1; after the removal it is child 0, and the
        // moved node is its first child.
        expect(select).toBe("L0.0.0.0");
        expect(isDirty(next, "L0.0")).toBe(true); // source container
        expect(isDirty(next, "L0.0.0")).toBe(true); // target span
    });

    it("moves a span child out to its line", () => {
        const root = fixture();
        const { next, select } = applyDropEdit(
            root,
            { kind: "node", id: "L0.0.1.0" },
            { containerId: "L0.0", index: 3 },
        );
        const line = getNode(next, "L0.0") as LineNode;
        expect(line.children.map((c) => c.kind)).toEqual(["field", "span", "text", "field"]);
        expect((getNode(next, "L0.0.1") as SpanNode).children).toEqual([]);
        expect(select).toBe("L0.0.3");
    });

    it("no-op moves and invalid containers return the same root", () => {
        const root = fixture();
        expect(
            applyDropEdit(root, { kind: "node", id: "L0.0.0" }, { containerId: "L0.0", index: 0 }),
        ).toEqual({ next: root, select: null });
        expect(
            applyDropEdit(
                root,
                { kind: "node", id: "L0.0.1" },
                { containerId: "L0.0.1", index: 0 },
            ),
        ).toEqual({ next: root, select: null });
        expect(
            applyDropEdit(
                root,
                { kind: "palette", node: fld("model") },
                { containerId: "L0.0.0", index: 0 }, // a field, not a container
            ),
        ).toEqual({ next: root, select: null });
    });
});

// A <subagent> region is a dedicated LayoutNode.subagent / VariantNode.subagent
// field (never a Children entry) whose single <line> shares the region's
// "L{i}.s" / "L{i}.{p}.v{v}.s" id. getNode resolves that id to the line so the
// normal child ops edit it; the region wrapper is added/removed with dedicated
// helpers.
describe("subagent regions", () => {
    const isDirty = (root: StatusloomNode, id: string): boolean =>
        (getNode(root, id) as { dirty?: boolean } | null)?.dirty === true;
    const saNames = (root: StatusloomNode, id: string): string[] =>
        (getNode(root, id) as LineNode).children.map((c) =>
            c.kind === "field" ? (c.name ?? "") : c.kind === "text" ? c.value : c.kind,
        );

    it("getNode resolves a layout-direct region's line and fields", () => {
        const root = subagentLayoutFixture();
        // "L{i}.s" resolves to the region's <line> — the drop/edit container.
        expect(getNode(root, "L0.s")?.kind).toBe("line");
        expect(saNames(root, "L0.s")).toEqual(["task-description", "task-model"]);
        expect((getNode(root, "L0.s.0") as FieldNode).name).toBe("task-description");
        expect((getNode(root, "L0.s.1") as FieldNode).name).toBe("task-model");
        // No region on a bare layout.
        expect(getNode(fixture(), "L0.s")).toBeNull();
    });

    it("getNode resolves per-variant regions' lines and fields", () => {
        const root = subagentVariantFixture();
        expect(getNode(root, "L0.1.v0.s")?.kind).toBe("line");
        expect((getNode(root, "L0.1.v0.s.0") as FieldNode).name).toBe("task-description");
        expect((getNode(root, "L0.1.v1.s.0") as FieldNode).name).toBe("task-model");
    });

    it("parentIdOf / parentChildId / predictChildId address subagent ids", () => {
        expect(parentIdOf("L0.s")).toBe("L0");
        expect(parentIdOf("L0.s.1")).toBe("L0.s");
        expect(parentIdOf("L0.1.v0.s")).toBe("L0.1.v0");
        expect(parentIdOf("L0.1.v0.s.2")).toBe("L0.1.v0.s");
        expect(parentChildId("L0.s.1")).toEqual({ parentId: "L0.s", index: 1 });
        expect(parentChildId("L0.1.v0.s.0")).toEqual({ parentId: "L0.1.v0.s", index: 0 });
        expect(predictChildId("L0.s", 2)).toBe("L0.s.2");
    });

    it("addSubagent attaches an empty region to a layout and is idempotent", () => {
        const root = fixture(); // Compact layout (L1) has no region
        const added = addSubagent(root, "L1");
        const sa = added.layouts[1].subagent;
        expect(sa?.kind).toBe("subagent");
        expect(sa?.line.kind).toBe("line");
        // The region starts empty: the user drops task-* fields, or clicks
        // "Reset to default" to seed the built-in default (fillSubagentDefault).
        expect(sa?.line.children).toEqual([]);
        expect(isDirty(added, "L1")).toBe(true);
        // Adding a second time is a no-op (same root identity).
        expect(addSubagent(added, "L1")).toBe(added);
    });

    it("addSubagent attaches an empty region to a variant", () => {
        const root = responsiveFixture();
        const added = addSubagent(root, "L0.1.v0");
        const responsive = added.layouts[0].children[1] as ResponsiveNode;
        expect(responsive.variants[0].subagent?.line.kind).toBe("line");
        expect(responsive.variants[0].subagent?.line.children).toEqual([]);
        expect(responsive.variants[1].subagent).toBeUndefined(); // sibling untouched
        expect(addSubagent(added, "L0.1.v0")).toBe(added);
    });

    it("addSubagent is a no-op for a non-container id", () => {
        const root = fixture();
        expect(addSubagent(root, "L0.0")).toBe(root); // a line
        expect(addSubagent(root, "root")).toBe(root);
        expect(addSubagent(root, "L9")).toBe(root); // dangling
    });

    it("makeDefaultSubagentLine mirrors document.go's claudeCodeDefaultDocument subagent line", () => {
        // internal/config/document.go is the single source of truth for this
        // shape; this locks the frontend's mirror of it (fields, order, and
        // the width-adaptive attrs: prefix/suffix/format/precision/optional/
        // when/min-width/align).
        const line = makeDefaultSubagentLine();
        expect(line.children).toEqual([
            { id: "", kind: "field", name: "task-description" },
            { id: "", kind: "field", name: "task-model", prefix: "  " },
            { id: "", kind: "flex" },
            {
                id: "",
                kind: "field",
                name: "task-duration",
                format: "duration",
                when: "width ge 48",
                "min-width": 7,
                align: "right",
            },
            {
                id: "",
                kind: "field",
                name: "task-tokens",
                prefix: " · ↓ ",
                format: "compact-number",
                optional: "task-tokens",
                when: "width ge 64",
                "min-width": 6,
                align: "right",
            },
            {
                id: "",
                kind: "field",
                name: "task-context-percent",
                prefix: " (",
                suffix: ")",
                format: "percent",
                precision: "0",
                optional: "task-context-percent",
                when: "width ge 80",
                "min-width": 4,
                align: "right",
            },
        ]);
    });

    it("fillSubagentDefault replaces an emptied-out subagent line's children", () => {
        const root = subagentLayoutFixture(); // L0.s already has task-description/task-model
        const emptied: StatusloomNode = {
            ...root,
            layouts: root.layouts.map((l, i) =>
                i === 0 && l.subagent ? { ...l, subagent: { ...l.subagent, line: { ...l.subagent.line, children: [] } } } : l,
            ),
        };
        const filled = fillSubagentDefault(emptied, "L0.s", makeDefaultSubagentLine());
        const sa = filled.layouts[0].subagent;
        expect(sa?.line.children.map((c) => (c.kind === "field" ? c.name : c.kind))).toEqual([
            "task-description",
            "task-model",
            "flex",
            "task-duration",
            "task-tokens",
            "task-context-percent",
        ]);
        // The line's own resolved id is preserved (only its content changed).
        expect(sa?.line.id).toBe("L0.s");
        expect(isDirty(filled, "L0.s")).toBe(true);
    });

    it("fillSubagentDefault resets an already-populated subagent line to the default", () => {
        // "Reset to default" also overwrites a non-empty row (undoable via
        // history), not just an emptied-out one.
        const root = subagentLayoutFixture(); // L0.s has task-description/task-model
        const reset = fillSubagentDefault(root, "L0.s", makeDefaultSubagentLine());
        expect(reset).not.toBe(root);
        const sa = reset.layouts[0].subagent;
        expect(sa?.line.children.map((c) => (c.kind === "field" ? c.name : c.kind))).toEqual([
            "task-description",
            "task-model",
            "flex",
            "task-duration",
            "task-tokens",
            "task-context-percent",
        ]);
        expect(isDirty(reset, "L0.s")).toBe(true);
    });

    it("fillSubagentDefault is a no-op for a dangling or non-line id", () => {
        const root = subagentLayoutFixture();
        const line = makeDefaultSubagentLine();
        expect(fillSubagentDefault(root, "L0.s.9.9", line)).toBe(root); // dangling
        expect(fillSubagentDefault(root, "L0", line)).toBe(root); // a layout, not a line
    });

    it("deleteSubagent removes the region (layout and variant)", () => {
        const layoutRoot = subagentLayoutFixture();
        const removed = deleteSubagent(layoutRoot, "L0");
        expect(removed.layouts[0].subagent).toBeUndefined();
        expect(isDirty(removed, "L0")).toBe(true);
        // No-op when absent (same root identity).
        const bare = fixture();
        expect(deleteSubagent(bare, "L0")).toBe(bare);

        const variantRoot = subagentVariantFixture();
        const vRemoved = deleteSubagent(variantRoot, "L0.1.v0");
        const responsive = vRemoved.layouts[0].children[1] as ResponsiveNode;
        expect(responsive.variants[0].subagent).toBeUndefined();
        expect(responsive.variants[1].subagent).toBeDefined(); // sibling kept
    });

    it("insertChild / removeNode edit a layout-direct region's line", () => {
        const root = subagentLayoutFixture();
        const inserted = insertChild(root, "L0.s", 1, txt("|"));
        expect(saNames(inserted, "L0.s")).toEqual(["task-description", "|", "task-model"]);
        expect(isDirty(inserted, "L0.s")).toBe(true); // the line container is stamped

        const removed = removeNode(root, "L0.s.0");
        expect(saNames(removed, "L0.s")).toEqual(["task-model"]);
        expect(isDirty(removed, "L0.s")).toBe(true);
    });

    it("insertChild edits a variant-nested region's line", () => {
        const root = subagentVariantFixture();
        const inserted = insertChild(root, "L0.1.v0.s", 1, fld("task-status"));
        expect(saNames(inserted, "L0.1.v0.s")).toEqual(["task-description", "task-status"]);
        // The sibling variant's region is untouched.
        expect(saNames(inserted, "L0.1.v1.s")).toEqual(["task-model"]);
    });

    it("updateAttrs edits a field inside a region's line", () => {
        const root = subagentLayoutFixture();
        const next = updateAttrs(root, "L0.s.1", { color: "cyan" });
        expect((getNode(next, "L0.s.1") as FieldNode).color).toBe("cyan");
        expect(isDirty(next, "L0.s.1")).toBe(true);
    });

    it("duplicateChild duplicates a chip inside a region's line", () => {
        const root = subagentLayoutFixture();
        const { next, select } = duplicateChild(root, "L0.s.0");
        expect(saNames(next, "L0.s")).toEqual([
            "task-description",
            "task-description",
            "task-model",
        ]);
        expect(select).toBe("L0.s.1");
    });

    it("applyDropEdit drops a palette field into a region's line", () => {
        const root = subagentLayoutFixture();
        const { next, select } = applyDropEdit(
            root,
            { kind: "palette", node: fld("task-status") },
            { containerId: "L0.s", index: 2 },
        );
        expect(saNames(next, "L0.s")).toEqual([
            "task-description",
            "task-model",
            "task-status",
        ]);
        expect(select).toBe("L0.s.2");
    });

    it("moveChild moves a chip out of a region's line into a main line", () => {
        const root = subagentLayoutFixture();
        // Move task-model (L0.s.1) into the main line L0.0 at its end.
        const next = moveChild(root, "L0.s.1", "L0.0", 1);
        expect(saNames(next, "L0.s")).toEqual(["task-description"]);
        expect((getNode(next, "L0.0.1") as FieldNode).name).toBe("task-model");
    });
});
