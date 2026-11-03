import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { PropertiesPanel } from "./PropertiesPanel.tsx";
import { WhenBuilder } from "./WhenBuilder.tsx";
import { t } from "../i18n.ts";
import type { SourceEntry, SourcesApi, SourceTestResult } from "../sourcesApi.ts";
import type { AstNode, FieldCatalogEntry, FieldNode, Metric } from "../types.ts";

const DESC = { en: "description", ja: "説明" };

const SOURCE_FIELD: FieldCatalogEntry = {
    name: "source",
    displayName: "Source",
    descriptions: DESC,
    category: "common",
    requiresSource: true,
    preview: { text: "", ansi: "" },
};

const MODEL_FIELD: FieldCatalogEntry = {
    name: "model",
    displayName: "Model",
    descriptions: DESC,
    category: "common",
    preview: { text: "Opus", ansi: "Opus" },
};

const ENV_FIELD: FieldCatalogEntry = {
    name: "env",
    displayName: "Environment",
    descriptions: DESC,
    category: "common",
    requiresVar: true,
    preview: { text: "", ansi: "" },
};

const TASK_FIELD: FieldCatalogEntry = {
    name: "task-model",
    displayName: "Task model",
    descriptions: DESC,
    category: "subagent",
    preview: { text: "", ansi: "" },
};

const OUTPUT_FIELD: FieldCatalogEntry = {
    name: "output-style",
    displayName: "Output Style",
    descriptions: DESC,
    category: "claude",
    preview: { text: "default", ansi: "default" },
};

const FIELDS: FieldCatalogEntry[] = [SOURCE_FIELD, MODEL_FIELD, ENV_FIELD, TASK_FIELD, OUTPUT_FIELD];

const SOURCE_A: SourceEntry = {
    id: "a",
    kind: "file",
    path: "/tmp/a.json",
    argv: [],
    url: "",
    allowRemote: false,
    ttlSeconds: 0,
    staleAfterSeconds: 0,
    timeoutSeconds: 0,
    warnings: [],
};

function fieldNode(extra: Partial<FieldNode> = {}): FieldNode {
    return { id: "n1", kind: "field", name: "source", ...extra };
}

function fakeApi(): SourcesApi {
    return {
        listSources: vi.fn(),
        putSource: vi.fn(),
        deleteSource: vi.fn(),
        testSource: vi.fn(),
    };
}

interface PanelOpts {
    node?: AstNode;
    sources?: SourceEntry[];
    sourcesApi?: SourcesApi;
    previewTarget?: { sessionId?: string; sample?: string };
}

function renderPanel(opts: PanelOpts = {}) {
    const onPatch = vi.fn();
    render(
        <PropertiesPanel
            node={opts.node ?? fieldNode({ source: "a" })}
            fields={FIELDS}
            metrics={[]}
            theme="dark"
            readOnly={false}
            onPatch={onPatch}
            onDuplicate={vi.fn()}
            onRemove={vi.fn()}
            sources={opts.sources}
            sourcesApi={opts.sourcesApi}
            previewTarget={opts.previewTarget}
        />,
    );
    return { onPatch };
}

function optionValues(testid: string): string[] {
    const el = screen.getByTestId(testid) as HTMLSelectElement;
    return Array.from(el.options).map((o) => o.value);
}

describe("PropertiesPanel source field", () => {
    it("clearing the source patches undefined, not an empty string", () => {
        const { onPatch } = renderPanel({ sources: [SOURCE_A] });
        fireEvent.change(screen.getByTestId("attr-source"), { target: { value: "" } });
        expect(onPatch).toHaveBeenCalledTimes(1);
        expect(onPatch.mock.calls[0][0]).toStrictEqual({ source: undefined });
    });

    it("keeps a source id missing from sources selectable and shows the undefined note", () => {
        renderPanel({ node: fieldNode({ source: "gone" }), sources: [SOURCE_A] });
        expect(optionValues("attr-source")).toStrictEqual(["", "a", "gone"]);
        expect((screen.getByTestId("attr-source") as HTMLSelectElement).value).toBe("gone");
        expect(screen.getByTestId("source-undefined").textContent).toBe(
            t("en", "sourceUndefined"),
        );
    });

    it("shows no undefined note when the source list is unknown", () => {
        renderPanel({ node: fieldNode({ source: "a" }), sources: undefined });
        expect(screen.queryByTestId("source-undefined")).toBeNull();
    });

    it("clearing select and fallback patches undefined", () => {
        const { onPatch } = renderPanel({
            node: fieldNode({ source: "a", select: ".x", fallback: "model" }),
            sources: [SOURCE_A],
        });
        fireEvent.change(screen.getByTestId("attr-select"), { target: { value: "" } });
        fireEvent.change(screen.getByTestId("attr-fallback"), { target: { value: "" } });
        expect(onPatch.mock.calls[0][0]).toStrictEqual({ select: undefined });
        expect(onPatch.mock.calls[1][0]).toStrictEqual({ fallback: undefined });
    });

    it("offers only plain built-in fields as fallback", () => {
        renderPanel({ sources: [SOURCE_A] });
        expect(optionValues("attr-fallback")).toStrictEqual(["", "model", "output-style"]);
    });

    it("groups fallback options under their category optgroups", () => {
        renderPanel({ sources: [SOURCE_A] });
        const select = screen.getByTestId("attr-fallback") as HTMLSelectElement;
        expect(select.options[0].value).toBe("");
        const groups = Array.from(select.querySelectorAll("optgroup"));
        expect(groups.map((g) => g.label)).toStrictEqual(["Common", "Claude Code"]);
        const valuesOf = (group: Element) =>
            Array.from(group.querySelectorAll("option")).map((o) => o.value);
        expect(valuesOf(groups[0])).toStrictEqual(["model"]);
        expect(valuesOf(groups[1])).toStrictEqual(["output-style"]);
    });

    it("keeps an out-of-list fallback selectable", () => {
        renderPanel({ node: fieldNode({ source: "a", fallback: "task-model" }), sources: [SOURCE_A] });
        expect(optionValues("attr-fallback")).toStrictEqual(["", "model", "output-style", "task-model"]);
    });

    it("disables Test without sourcesApi or without a source", () => {
        renderPanel({ sources: [SOURCE_A] });
        expect((screen.getByTestId("source-test") as HTMLButtonElement).disabled).toBe(true);
    });

    it("disables Test when the node has no source", () => {
        renderPanel({ node: fieldNode(), sources: [SOURCE_A], sourcesApi: fakeApi() });
        expect((screen.getByTestId("source-test") as HTMLButtonElement).disabled).toBe(true);
    });

    it("sends only the session id when the preview target has one", async () => {
        const api = fakeApi();
        vi.mocked(api.testSource).mockResolvedValue({
            ok: true,
            error: "",
            matched: true,
            value: 42,
            display: "42",
            documentPreview: "",
        } satisfies SourceTestResult);
        renderPanel({
            node: fieldNode({ source: "a", select: ".x" }),
            sources: [SOURCE_A],
            sourcesApi: api,
            previewTarget: { sessionId: "s1" },
        });
        fireEvent.click(screen.getByTestId("source-test"));
        await waitFor(() => {
            expect(screen.getByTestId("source-test-result").textContent).toBe("42");
        });
        expect(api.testSource).toHaveBeenCalledWith("a", { select: ".x", sessionId: "s1" });
        expect(Object.keys(vi.mocked(api.testSource).mock.calls[0][1])).not.toContain("sample");
    });

    it("renders the display, then the JSON value when display is empty", async () => {
        const api = fakeApi();
        vi.mocked(api.testSource).mockResolvedValue({
            ok: true,
            error: "",
            matched: true,
            value: { a: 1 },
            display: "",
            documentPreview: "",
        });
        renderPanel({ node: fieldNode({ source: "a" }), sources: [SOURCE_A], sourcesApi: api });
        fireEvent.click(screen.getByTestId("source-test"));
        await waitFor(() => {
            expect(screen.getByTestId("source-test-result").textContent).toBe('{"a":1}');
        });
    });

    it("renders the no-match text when the select matched nothing", async () => {
        const api = fakeApi();
        vi.mocked(api.testSource).mockResolvedValue({
            ok: true,
            error: "",
            matched: false,
            value: null,
            display: "",
            documentPreview: "",
        });
        renderPanel({ node: fieldNode({ source: "a" }), sources: [SOURCE_A], sourcesApi: api });
        fireEvent.click(screen.getByTestId("source-test"));
        await waitFor(() => {
            expect(screen.getByTestId("source-test-result").textContent).toBe(
                t("en", "sourceNoMatch"),
            );
        });
    });

    it("renders the error text inline when the test fails", async () => {
        const api = fakeApi();
        vi.mocked(api.testSource).mockResolvedValue({
            ok: false,
            error: "exit status 1",
            matched: false,
            value: null,
            display: "",
            documentPreview: "",
        });
        renderPanel({ node: fieldNode({ source: "a" }), sources: [SOURCE_A], sourcesApi: api });
        fireEvent.click(screen.getByTestId("source-test"));
        await waitFor(() => {
            expect(screen.getByTestId("source-test-result").textContent).toBe("exit status 1");
        });
    });

    it("renders a rejected request inline instead of throwing", async () => {
        const api = fakeApi();
        vi.mocked(api.testSource).mockRejectedValue(new Error("boom"));
        renderPanel({ node: fieldNode({ source: "a" }), sources: [SOURCE_A], sourcesApi: api });
        fireEvent.click(screen.getByTestId("source-test"));
        await waitFor(() => {
            expect(screen.getByTestId("source-test-result").textContent).toBe("boom");
        });
    });
});

describe("PropertiesPanel optional select", () => {
    it("excludes the source field but keeps a source:<id> optional selectable", () => {
        renderPanel({ node: fieldNode({ source: "a", optional: "source:a" }), sources: [SOURCE_A] });
        const values = optionValues("attr-optional");
        expect(values).not.toContain("source");
        expect(values).toContain("source:a");
        expect((screen.getByTestId("attr-optional") as HTMLSelectElement).value).toBe("source:a");
    });
});

describe("WhenBuilder metric picker", () => {
    const METRICS: Metric[] = [
        { name: "context-percent", displayName: "Context %", descriptions: DESC },
        { name: "source-stale", displayName: "Source stale", descriptions: DESC, param: "source" },
    ];

    it("does not offer parameterized metrics", () => {
        render(
            <WhenBuilder
                value="context-percent ge 80"
                metrics={METRICS}
                readOnly={false}
                testidPrefix="v0"
                onChange={vi.fn()}
            />,
        );
        const values = optionValues("v0-metric");
        expect(values).toContain("context-percent");
        expect(values).not.toContain("source-stale");
    });
});
