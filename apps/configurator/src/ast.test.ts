import { describe, expect, it } from "vitest";
import {
    activeLayoutIndex,
    addLayout,
    addLine,
    adjustIdAfterRemoval,
    appendLayouts,
    applyDropEdit,
    deleteLayout,
    deleteLine,
    duplicateLayout,
    getNode,
    insertChild,
    lineIndexOfContainerId,
    moveChild,
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
import { assignIds, doc, fld, lay, ln, spn, txt } from "./test/fakeDsl.ts";
import type { FieldNode, LineNode, SpanNode, StatusloomNode } from "./types.ts";

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

function childNames(root: StatusloomNode, layout: number, line: number): string[] {
    const l = root.layouts[layout].lines[line];
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
        expect(parseNodeId("L0.1")).toEqual([
            { t: "layout", i: 0 },
            { t: "line", j: 1 },
        ]);
        expect(parseNodeId("L0.1.2.0")).toEqual([
            { t: "layout", i: 0 },
            { t: "line", j: 1 },
            { t: "child", k: 2 },
            { t: "child", k: 0 },
        ]);
        expect(parseNodeId("L0.0.1.cr2")).toEqual([
            { t: "layout", i: 0 },
            { t: "line", j: 0 },
            { t: "child", k: 1 },
            { t: "colorRule", c: 2 },
        ]);
    });

    it("rejects malformed ids", () => {
        expect(parseNodeId("")).toBeNull();
        expect(parseNodeId("X0")).toBeNull();
        expect(parseNodeId("L0.x")).toBeNull();
        expect(parseNodeId("L0.1.cr0.0")).toBeNull();
        expect(parseNodeId("root.c1.0")).toBeNull();
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
        const next = deleteLine(root, 0, 0);
        expect(next.layouts[0].lines).toHaveLength(1);
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
        const next = addLine(root, 1);
        expect(next.layouts[1].lines).toHaveLength(2);
        expect((next.layouts[1].lines[1] as LineNode).children).toEqual([]);
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
        expect(dup.layouts[1].lines[0]).not.toBe(dup.layouts[0].lines[0]);
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
