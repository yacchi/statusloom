import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { PropertiesPanel } from "./PropertiesPanel.tsx";
import type { AstNode, FieldCatalogEntry, FieldNode, Metric } from "../types.ts";

const DESC = { en: "description", ja: "説明" };

const CURRENT_DIR: FieldCatalogEntry = {
    name: "current-directory",
    displayName: "Current Directory",
    descriptions: DESC,
    category: "common",
    pathStyles: ["base", "home", "full"],
    pathDefault: "base",
    preview: { text: "statusloom", ansi: "statusloom" },
};

const PROJECT_DIR: FieldCatalogEntry = {
    name: "project-directory",
    displayName: "Project Directory",
    descriptions: DESC,
    category: "common",
    pathStyles: ["base", "home", "full"],
    pathDefault: "full",
    preview: { text: "/Users/fujie/x", ansi: "/Users/fujie/x" },
};

const MODEL: FieldCatalogEntry = {
    name: "model",
    displayName: "Model",
    descriptions: DESC,
    category: "common",
    preview: { text: "Opus", ansi: "Opus" },
};

const FIELDS: FieldCatalogEntry[] = [CURRENT_DIR, PROJECT_DIR, MODEL];

function fieldNode(name: string, extra: Partial<FieldNode> = {}): FieldNode {
    return { id: "n1", kind: "field", name, ...extra };
}

function renderPanel(node: AstNode) {
    const onPatch = vi.fn();
    const metrics: Metric[] = [];
    render(
        <PropertiesPanel
            node={node}
            fields={FIELDS}
            metrics={metrics}
            theme="dark"
            readOnly={false}
            onPatch={onPatch}
            onDuplicate={vi.fn()}
            onRemove={vi.fn()}
        />,
    );
    return { onPatch };
}

function pathSelect(): HTMLSelectElement {
    return screen.getByTestId("attr-path") as HTMLSelectElement;
}

describe("PropertiesPanel path attribute", () => {
    it("is shown only for a field that lists path styles", () => {
        renderPanel(fieldNode("model"));
        expect(screen.queryByTestId("attr-path")).toBeNull();
    });

    it("shows the unset state as the field default", () => {
        renderPanel(fieldNode("current-directory"));
        const sel = pathSelect();
        expect(sel.value).toBe("");
        expect(sel.options[0].textContent).toBe("(default: base)");
        expect(Array.from(sel.options).map((o) => o.value)).toEqual(["", "base", "home", "full"]);
    });

    it("writes an explicit non-default path into the AST", () => {
        const { onPatch } = renderPanel(fieldNode("current-directory"));
        fireEvent.change(pathSelect(), { target: { value: "home" } });
        expect(onPatch).toHaveBeenCalledTimes(1);
        expect(onPatch.mock.calls[0][0]).toStrictEqual({ path: "home" });
    });

    it("removes the attribute when set back to the default", () => {
        const { onPatch } = renderPanel(fieldNode("current-directory", { path: "home" }));
        fireEvent.change(pathSelect(), { target: { value: "base" } });
        expect(onPatch.mock.calls[0][0]).toStrictEqual({ path: undefined });
    });

    it("removes the attribute when the unset entry is chosen", () => {
        const { onPatch } = renderPanel(fieldNode("current-directory", { path: "home" }));
        fireEvent.change(pathSelect(), { target: { value: "" } });
        expect(onPatch.mock.calls[0][0]).toStrictEqual({ path: undefined });
    });

    it("treats full as the non-default value for current-directory", () => {
        const { onPatch } = renderPanel(fieldNode("current-directory"));
        fireEvent.change(pathSelect(), { target: { value: "full" } });
        expect(onPatch.mock.calls[0][0]).toStrictEqual({ path: "full" });
    });

    it("removes the attribute for a project-directory set to its default full", () => {
        const { onPatch } = renderPanel(fieldNode("project-directory", { path: "home" }));
        expect(pathSelect().options[0].textContent).toBe("(default: full)");
        fireEvent.change(pathSelect(), { target: { value: "full" } });
        expect(onPatch.mock.calls[0][0]).toStrictEqual({ path: undefined });
    });

    it("keeps an out-of-list path from the DSL editor selectable", () => {
        renderPanel(fieldNode("current-directory", { path: "tilde" }));
        expect(pathSelect().value).toBe("tilde");
    });
});
