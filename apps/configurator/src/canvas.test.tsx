// App-level canvas tests against the fake /api/dsl/* server (test/fakeDsl.ts):
// chips are painted from node-ID preview segments, spans render as chip
// groups, nodes without output render as ghosts, and edits round-trip through
// AST -> serialize -> parse.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { App } from "./App.tsx";
import {
    doc,
    fld,
    installFakeDslServer,
    lay,
    ln,
    spn,
    txt,
    type FakeServer,
} from "./test/fakeDsl.ts";
import type { StatusloomNode } from "./types.ts";

const TOKEN = "a".repeat(32);

// L0.0: model + span(prefix "5h: ", optional five-hour-usage)[five-hour-usage] + git-branch
// L0.1: session-cost (absent from the fake sample -> ghost, line omitted)
function testDoc(): StatusloomNode {
    return doc([
        lay(
            "Default",
            [
                ln([
                    fld("model"),
                    spn([fld("five-hour-usage")], {
                        prefix: "5h: ",
                        optional: "five-hour-usage",
                    }),
                    fld("git-branch"),
                ]),
                ln([fld("session-cost")]),
            ],
            true,
        ),
    ]);
}

let server: FakeServer;

beforeEach(() => {
    window.location.hash = `#token=${TOKEN}`;
    server = installFakeDslServer(testDoc());
});

afterEach(() => {
    vi.unstubAllGlobals();
    window.location.hash = "";
    window.localStorage.clear();
});

async function renderApp() {
    const view = render(<App />);
    await waitFor(() => expect(screen.getByTestId("seg-0-0")).toBeTruthy(), { timeout: 3000 });
    return view;
}

describe("canvas chips", () => {
    it("paints chips from node-ID preview segments", async () => {
        await renderApp();
        await waitFor(
            () => expect(screen.getByTestId("seg-0-0").textContent).toContain("Opus 4.8"),
            { timeout: 3000 },
        );
        expect(screen.getByTestId("seg-0-2").textContent).toContain("main");
    });

    it("renders a span as a chip group with its own decoration and nested chips", async () => {
        await renderApp();
        await waitFor(() => expect(screen.getByTestId("span-L0.0.1")).toBeTruthy(), {
            timeout: 3000,
        });
        const group = screen.getByTestId("span-L0.0.1");
        await waitFor(() => expect(group.textContent).toContain("5h: "), { timeout: 3000 });
        expect(group.querySelector(".span-deco .ansi-run")).toBeTruthy();
        // The span's child is a nested chip carrying the field's own segment.
        const inner = within(group).getByTestId("node-L0.0.1.0");
        expect(inner.textContent).toContain("32%");
    });

    it("renders a node without output as a ghost chip and marks the line omitted", async () => {
        await renderApp();
        await waitFor(
            () => {
                const chip = screen.getByTestId("seg-1-0");
                expect(chip.querySelector(".chip-ghost")).toBeTruthy();
            },
            { timeout: 3000 },
        );
        expect(document.querySelectorAll(".canvas-row.omitted").length).toBe(1);
    });

    it("selecting a chip opens its properties; selecting a span child selects the child", async () => {
        await renderApp();
        fireEvent.click(screen.getByTestId("seg-0-0"));
        await waitFor(() =>
            expect(screen.getByTestId("props-title").textContent).toBe("Model"),
        );

        fireEvent.click(screen.getByTestId("node-L0.0.1.0"));
        await waitFor(() =>
            expect(screen.getByTestId("props-title").textContent).toBe("5-Hour Usage"),
        );

        // Clicking the group's decoration selects the span itself.
        fireEvent.click(screen.getByTestId("span-L0.0.1"));
        await waitFor(() => expect(screen.getByTestId("props-title").textContent).toBe("Span"));
    });

    it("adds a palette field to the active line via serialize -> parse", async () => {
        await renderApp();
        fireEvent.click(screen.getByTestId("palette-field:git-branch"));
        // The new chip lands at the end of line 0 and gets selected.
        await waitFor(() => expect(screen.getByTestId("seg-0-3")).toBeTruthy(), {
            timeout: 3000,
        });
        await waitFor(
            () =>
                expect(screen.getByTestId("props-title").textContent).toBe("Git Branch"),
            { timeout: 3000 },
        );
        // The shared draft eventually carries a second git-branch field.
        await waitFor(
            () => {
                const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
                expect(last.match(/"git-branch"/g)?.length).toBe(2);
            },
            { timeout: 3000 },
        );
    });

    it("removes the selected chip with the Delete key", async () => {
        await renderApp();
        fireEvent.click(screen.getByTestId("seg-0-2"));
        await waitFor(() =>
            expect(screen.getByTestId("props-title").textContent).toBe("Git Branch"),
        );
        fireEvent.keyDown(window, { key: "Delete" });
        await waitFor(() => expect(screen.queryByTestId("seg-0-2")).toBeNull(), {
            timeout: 3000,
        });
        // Line 0 now has two children: model + span.
        expect(screen.getByTestId("seg-0-0")).toBeTruthy();
        expect(screen.getByTestId("seg-0-1")).toBeTruthy();
    });

    it("attribute edits round-trip into the shared draft source", async () => {
        await renderApp();
        fireEvent.click(screen.getByTestId("span-L0.0.1"));
        await waitFor(() => expect(screen.getByTestId("attr-prefix")).toBeTruthy());
        fireEvent.change(screen.getByTestId("attr-prefix"), { target: { value: "5H " } });
        await waitFor(
            () => {
                const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
                expect(last).toContain('"prefix":"5H "');
            },
            { timeout: 3000 },
        );
    });

    it("changes text into a style-controlled separator", async () => {
        server = installFakeDslServer(
            doc([
                lay(
                    "Default",
                    [
                        ln([
                            fld("model"),
                            txt("|", { role: "separator", padding: 1 }),
                            fld("git-branch"),
                        ]),
                    ],
                    true,
                ),
            ]),
        );
        await renderApp();
        fireEvent.click(screen.getByTestId("seg-0-1"));
        await waitFor(() => expect(screen.getByTestId("attr-role")).toBeTruthy());
        fireEvent.change(screen.getByTestId("attr-role"), { target: { value: "separator" } });
        await waitFor(
            () => {
                const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
                expect(last).toContain('"role":"separator"');
                expect(last).toContain('"value":""');
                expect(last).not.toContain('"padding":1');
            },
            { timeout: 3000 },
        );
        expect(document.querySelector(".manual-separator-ghost")).toBeTruthy();
    });

    it("shows the fallback line when every line is omitted", async () => {
        // A document whose only field has no data in the sample.
        server = installFakeDslServer(
            doc([lay("Default", [ln([fld("session-cost")])], true)]),
        );
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("seg-0-0")).toBeTruthy(), {
            timeout: 3000,
        });
        await waitFor(
            () => expect(document.querySelector(".fallback-note")).toBeTruthy(),
            { timeout: 3000 },
        );
    });
});

// DOM-level checks of the span drop-container UI: nested chips are sortable
// (span-level SortableContext) and drop indicators map container targets onto
// the right elements. The Canvas is rendered standalone with a fabricated
// dropTarget — exactly what useDragEditing paints during a drag (the AST is
// never mutated mid-drag; indicators are paint-only).
describe("canvas span drop containers", () => {
    it("renders span children as sortable chips", async () => {
        await renderApp();
        const inner = screen.getByTestId("node-L0.0.1.0");
        // useSortable marks its draggables with an aria role description.
        expect(inner.getAttribute("aria-roledescription")).toBe("sortable");
    });
});

describe("canvas drop indicators (standalone)", () => {
    it("marks the span group and its chips for container-based drop targets", async () => {
        const { Canvas } = await import("./components/Canvas.tsx");
        const { DndContext } = await import("@dnd-kit/core");
        const { assignIds } = await import("./test/fakeDsl.ts");

        const ast = assignIds(testDoc());
        const lines = ast.layouts[0].lines;
        const noop = () => {};
        const renderWith = (dropTarget: { containerId: string; index: number } | null) =>
            render(
                <DndContext>
                    <Canvas
                        lines={lines}
                        previewLines={null}
                        fallback={null}
                        selection={null}
                        activeLine={0}
                        dropTarget={dropTarget}
                        theme="dark"
                        width={120}
                        previewSource={{ kind: "sample", sample: "full" }}
                        sessions={[]}
                        pureOutput={false}
                        loading={false}
                        error={null}
                        readOnly={false}
                        displayName={(f) => f}
                        onSelect={noop}
                        onDeselect={noop}
                        onActivateLine={noop}
                        onAddLine={noop}
                        onDeleteLine={noop}
                        onWidth={noop}
                        onPreviewSourceChange={noop}
                        onRefreshSessions={noop}
                        onTheme={noop}
                        onPureOutput={noop}
                    />
                </DndContext>,
            );

        // Target: append into the span (L0.0.1, index = len). The group gets
        // the drop-into highlight and its last inner chip the after-caret.
        const intoSpan = renderWith({ containerId: "L0.0.1", index: 1 });
        const group = intoSpan.getByTestId("span-L0.0.1");
        expect(group.className).toContain("drop-into");
        expect(intoSpan.getByTestId("node-L0.0.1.0").className).toContain("drop-after");
        // Line-level chips show no indicator for a span-container target.
        expect(intoSpan.getByTestId("seg-0-0").className).not.toContain("drop-before");
        intoSpan.unmount();

        // Target: caret before the span's first child.
        const beforeInner = renderWith({ containerId: "L0.0.1", index: 0 });
        expect(beforeInner.getByTestId("node-L0.0.1.0").className).toContain("drop-before");
        expect(beforeInner.getByTestId("span-L0.0.1").className).toContain("drop-into");
        beforeInner.unmount();

        // Target: a line-level caret leaves the span group unmarked.
        const lineCaret = renderWith({ containerId: "L0.0", index: 0 });
        expect(lineCaret.getByTestId("seg-0-0").className).toContain("drop-before");
        expect(lineCaret.getByTestId("span-L0.0.1").className).not.toContain("drop-into");
        lineCaret.unmount();
    });
});
