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
    resp,
    spn,
    txt,
    variant,
    type FakeServer,
} from "./test/fakeDsl.ts";
import type { ResponsiveNode, StatusloomNode } from "./types.ts";

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

    it("selects a decoration-less span via its always-on handle", async () => {
        // A span with no prefix/suffix/padding exposes no own clickable area,
        // so the static grip handle is the only reliable way to select it.
        server = installFakeDslServer(
            doc([
                lay(
                    "Default",
                    [ln([fld("model"), spn([fld("git-branch")]), fld("session-cost")])],
                    true,
                ),
            ]),
        );
        await renderApp();
        const handle = await screen.findByTestId("span-handle-L0.0.1");
        fireEvent.click(handle);
        await waitFor(() =>
            expect(screen.getByTestId("props-title").textContent).toBe("Span"),
        );
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
        const children = ast.layouts[0].children;
        const noop = () => {};
        const renderWith = (dropTarget: { containerId: string; index: number } | null) =>
            render(
                <DndContext>
                    <Canvas
                        children={children}
                        layoutId="L0"
                        layoutSubagent={ast.layouts[0].subagent}
                        previewLines={null}
                        selectedVariants={null}
                        subagentPreview={null}
                        fallback={null}
                        selection={null}
                        activeLine={0}
                        dropTarget={dropTarget}
                        dragCategory={null}
                        theme="dark"
                        width={120}
                        previewSource={{ kind: "sample", sample: "full" }}
                        subagentSample="subagent-running"
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
                        onAddResponsive={noop}
                        onDeleteLine={noop}
                        onDuplicateLine={noop}
                        onAddLineToVariant={noop}
                        onAddVariant={noop}
                        onDeleteVariant={noop}
                        onDuplicateVariant={noop}
                        onPatchVariantWhen={noop}
                        onAddSubagent={noop}
                        onDeleteSubagent={noop}
                        onFillSubagentDefault={noop}
                        onWidth={noop}
                        onPreviewSourceChange={noop}
                        onSubagentSampleChange={noop}
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

// L0.0: model (plain line)
// L0.1: responsive — variant 0 (wide, 60 chars, fits only at generous
// widths) / variant 1 (narrow fallback, 1 char, always fits).
function responsiveTestDoc(): StatusloomNode {
    return doc([
        lay(
            "Default",
            [
                ln([fld("model")]),
                resp([variant([ln([txt("X".repeat(60))])]), variant([ln([txt("Y")])])]),
            ],
            true,
        ),
    ]);
}

describe("canvas responsive / variant editing", () => {
    it("highlights the width-selected variant and flips it when the width changes", async () => {
        server = installFakeDslServer(responsiveTestDoc());
        await renderApp();
        await waitFor(() => expect(screen.getByTestId("variant-L0.1.v0")).toBeTruthy(), {
            timeout: 3000,
        });

        // Default width (120) fits the wide variant (60 chars): it is the
        // one selected, and only it carries the "selected" badge/class.
        await waitFor(() => {
            expect(screen.getByTestId("variant-L0.1.v0").className).toContain(
                "variant-selected",
            );
            expect(screen.getByTestId("variant-L0.1.v1").className).not.toContain(
                "variant-selected",
            );
        });

        // Narrowing the width past the wide variant's natural width (60)
        // flips the selection to the fallback — this is the feature's whole
        // point made visible: the slider shows which candidate would
        // actually render at that width.
        fireEvent.change(screen.getByTestId("width-slider"), { target: { value: "45" } });
        await waitFor(() => {
            expect(screen.getByTestId("variant-L0.1.v1").className).toContain(
                "variant-selected",
            );
            expect(screen.getByTestId("variant-L0.1.v0").className).not.toContain(
                "variant-selected",
            );
        });
    });

    it("adds and deletes a variant via the canvas buttons, round-tripping through serialize", async () => {
        server = installFakeDslServer(responsiveTestDoc());
        await renderApp();
        await waitFor(() => expect(screen.getByTestId("variant-L0.1.v0")).toBeTruthy());

        fireEvent.click(screen.getByTestId("responsive-add-variant-L0.1"));
        await waitFor(() => expect(screen.getByTestId("variant-L0.1.v2")).toBeTruthy(), {
            timeout: 3000,
        });
        await waitFor(() => {
            const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
            expect(JSON.parse(last).layouts[0].children[1].variants).toHaveLength(3);
        });

        fireEvent.click(screen.getByTestId("variant-delete-L0.1.v2"));
        await waitFor(() => expect(screen.queryByTestId("variant-L0.1.v2")).toBeNull(), {
            timeout: 3000,
        });
    });

    it("edits a variant's when gate and clears it again, round-tripping through serialize", async () => {
        server = installFakeDslServer(responsiveTestDoc());
        await renderApp();
        const box = (await waitFor(() => screen.getByTestId("variant-when-L0.1.v0"), {
            timeout: 3000,
        })) as HTMLInputElement;
        // An unconditional variant starts empty (no `when` attribute at all).
        expect(box.value).toBe("");

        fireEvent.change(box, { target: { value: 'account-type eq "claude_team"' } });
        await waitFor(() => {
            const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
            expect(JSON.parse(last).layouts[0].children[1].variants[0].when).toBe(
                'account-type eq "claude_team"',
            );
        });

        // Emptying the box clears the attribute rather than storing "".
        fireEvent.change(screen.getByTestId("variant-when-L0.1.v0"), { target: { value: "" } });
        await waitFor(() => {
            const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
            expect(JSON.parse(last).layouts[0].children[1].variants[0].when).toBeUndefined();
        });
    });

    it("disables deleting a responsive's last remaining variant", async () => {
        server = installFakeDslServer(
            doc([lay("Default", [resp([variant([ln([fld("model")])])])], true)]),
        );
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("variant-delete-L0.0.v0")).toBeTruthy(), {
            timeout: 3000,
        });
        expect((screen.getByTestId("variant-delete-L0.0.v0") as HTMLButtonElement).disabled).toBe(
            true,
        );
    });

    it("adds a line to a variant and a new responsive from the canvas footer", async () => {
        server = installFakeDslServer(responsiveTestDoc());
        await renderApp();
        await waitFor(() => expect(screen.getByTestId("variant-add-line-L0.1.v0")).toBeTruthy());

        fireEvent.click(screen.getByTestId("variant-add-line-L0.1.v0"));
        await waitFor(() => {
            const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
            expect(JSON.parse(last).layouts[0].children[1].variants[0].lines).toHaveLength(2);
        });

        fireEvent.click(screen.getByText("+ Add responsive"));
        await waitFor(() => expect(document.querySelectorAll(".responsive-row")).toHaveLength(2), {
            timeout: 3000,
        });
    });
});

// L0.0: model (plain line); L0.s: subagent line [task-description, task-model]
function subagentLayoutDoc(): StatusloomNode {
    const d = doc([lay("Default", [ln([fld("model")])], true)]);
    d.layouts[0].subagent = {
        id: "",
        kind: "subagent",
        line: ln([fld("task-description"), fld("task-model")]),
    };
    return d;
}

// L0.0: model (plain line); L0.s: subagent region with a line that has lost
// all its fields (a hand-authored/legacy empty <line/>, or one emptied out by
// deleting every chip) — exercises the "Use default fields" affordance.
function subagentEmptyLayoutDoc(): StatusloomNode {
    const d = doc([lay("Default", [ln([fld("model")])], true)]);
    d.layouts[0].subagent = { id: "", kind: "subagent", line: ln([]) };
    return d;
}

// L0.0: model; L0.1 responsive — v0 (wide) subagent [task-description],
// v1 (fallback) subagent [task-model].
function subagentVariantDoc(): StatusloomNode {
    const d = doc([
        lay(
            "Default",
            [
                ln([fld("model")]),
                resp([variant([ln([txt("X".repeat(60))])]), variant([ln([txt("Y")])])]),
            ],
            true,
        ),
    ]);
    const r = d.layouts[0].children[1] as ResponsiveNode;
    r.variants[0].subagent = { id: "", kind: "subagent", line: ln([fld("task-description")]) };
    r.variants[1].subagent = { id: "", kind: "subagent", line: ln([fld("task-model")]) };
    return d;
}

// L0.0: model; L0.s: subagent line [task-description, <flex/>, task-model] —
// the flex should push task-model to the right in the preview.
function subagentFlexLayoutDoc(): StatusloomNode {
    const d = doc([lay("Default", [ln([fld("model")])], true)]);
    d.layouts[0].subagent = {
        id: "",
        kind: "subagent",
        line: ln([fld("task-description"), { id: "", kind: "flex" }, fld("task-model")]),
    };
    return d;
}

describe("canvas subagent band", () => {
    it("shows an Add subagent row button for a region-less layout", async () => {
        // The default testDoc() (installed in beforeEach) has no subagent.
        await renderApp();
        await waitFor(() => expect(screen.getByTestId("subagent-add-L0")).toBeTruthy(), {
            timeout: 3000,
        });
        expect(screen.queryByTestId("subagent-band-L0")).toBeNull();
    });

    it("renders the editable subagent line and per-task preview rows", async () => {
        server = installFakeDslServer(subagentLayoutDoc());
        await renderApp();
        await waitFor(() => expect(screen.getByTestId("subagent-band-L0")).toBeTruthy(), {
            timeout: 3000,
        });
        // While a region exists, a "Reset to default" affordance is always shown
        // (even for a populated row — resetting is undoable via history).
        expect(screen.getByTestId("subagent-fill-default-L0").textContent).toContain(
            "Reset to default",
        );
        // Once the subagent preview resolves: three running tasks -> three
        // preview rows carrying task values, and the editable line renders its
        // chips against the first sample task's output (like the main line), so
        // the task-description chip shows that task's value rather than a
        // structural "Task Description" label.
        await waitFor(
            () => {
                const preview = screen.getByTestId("subagent-preview-L0");
                expect(preview.querySelectorAll(".subagent-preview-line").length).toBe(3);
                expect(preview.textContent).toContain("Review render pipeline");
                expect(screen.getByTestId("seg-L0.s-0").textContent).toContain(
                    "Review render pipeline",
                );
            },
            { timeout: 3000 },
        );
    });

    it("selecting a subagent field highlights it in every task preview row", async () => {
        server = installFakeDslServer(subagentLayoutDoc());
        await renderApp();
        await waitFor(() => expect(screen.getByTestId("subagent-preview-L0")).toBeTruthy(), {
            timeout: 3000,
        });
        // The subagent line's first chip (task-description, id L0.s.0).
        fireEvent.click(screen.getByTestId("seg-L0.s-0"));
        await waitFor(
            () =>
                expect(
                    document.querySelectorAll(
                        ".subagent-preview-line .subagent-seg.selected",
                    ).length,
                ).toBe(3),
            { timeout: 3000 },
        );
    });

    it("adds an EMPTY region, seeds it via Reset to default, and deletes it, round-tripping through serialize", async () => {
        await renderApp();
        fireEvent.click(screen.getByTestId("subagent-add-L0"));
        await waitFor(() => expect(screen.getByTestId("subagent-band-L0")).toBeTruthy(), {
            timeout: 3000,
        });
        // The freshly added region is EMPTY (no task-* fields): it exposes a
        // persistent "Reset to default" affordance, and round-trips as an empty
        // <subagent> line through serialize.
        const resetBtn = await screen.findByTestId("subagent-fill-default-L0");
        expect(resetBtn.textContent).toContain("Reset to default");
        await waitFor(
            () => {
                const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
                expect(last).toContain('"kind":"subagent"');
                expect(last).not.toContain('"name":"task-description"');
            },
            { timeout: 3000 },
        );

        // Reset to default seeds the built-in default fields (presets.ts's
        // makeDefaultSubagentLine, mirroring document.go).
        fireEvent.click(screen.getByTestId("subagent-fill-default-L0"));
        await waitFor(
            () => {
                const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
                expect(last).toContain('"name":"task-description"');
                expect(last).toContain('"name":"task-model"');
                expect(last).toContain('"name":"task-duration"');
                expect(last).toContain('"name":"task-tokens"');
                expect(last).toContain('"name":"task-context-percent"');
            },
            { timeout: 3000 },
        );

        fireEvent.click(screen.getByTestId("subagent-delete-L0"));
        await waitFor(() => expect(screen.getByTestId("subagent-add-L0")).toBeTruthy(), {
            timeout: 3000,
        });
    });

    it("seeds an empty subagent line with the default fields via Reset to default", async () => {
        server = installFakeDslServer(subagentEmptyLayoutDoc());
        await renderApp();
        await waitFor(() => expect(screen.getByTestId("subagent-band-L0")).toBeTruthy(), {
            timeout: 3000,
        });
        const fillButton = await screen.findByTestId("subagent-fill-default-L0");
        expect(fillButton.textContent).toContain("Reset to default");
        fireEvent.click(fillButton);

        await waitFor(
            () => {
                const last = server.putDraftBodies[server.putDraftBodies.length - 1] ?? "";
                expect(last).toContain('"name":"task-description"');
                expect(last).toContain('"name":"task-context-percent"');
            },
            { timeout: 3000 },
        );
        // The affordance persists (it can re-seed an already-populated row too).
        expect(screen.getByTestId("subagent-fill-default-L0")).toBeTruthy();
    });

    it("toggles the subagent sample and re-requests the subagent preview", async () => {
        server = installFakeDslServer(subagentLayoutDoc());
        await renderApp();
        const select = (await screen.findByTestId(
            "subagent-sample-select",
        )) as HTMLSelectElement;
        expect(select.value).toBe("subagent-running");

        fireEvent.change(select, { target: { value: "subagent-completed" } });
        await waitFor(() => expect(select.value).toBe("subagent-completed"));
        await waitFor(
            () => {
                const bodies = server.fetchMock.mock.calls
                    .filter((c: unknown[]) => String(c[0]).endsWith("/api/dsl/preview"))
                    .map((c: unknown[]) =>
                        JSON.parse(String((c[1] as RequestInit).body)),
                    );
                expect(
                    bodies.some(
                        (b) => b.section === "subagent" && b.sample === "subagent-completed",
                    ),
                ).toBe(true);
            },
            { timeout: 3000 },
        );
    });

    it("renders per-variant subagent bands in a responsive layout (no layout-level band)", async () => {
        server = installFakeDslServer(subagentVariantDoc());
        await renderApp();
        await waitFor(() => expect(screen.getByTestId("subagent-band-L0.1.v0")).toBeTruthy(), {
            timeout: 3000,
        });
        expect(screen.getByTestId("subagent-band-L0.1.v1")).toBeTruthy();
        // A responsive layout carries its subagent inside each variant, so no
        // layout-level band / add button appears.
        expect(screen.queryByTestId("subagent-band-L0")).toBeNull();
        expect(screen.queryByTestId("subagent-add-L0")).toBeNull();
    });

    it("preserves <flex/> whitespace in the subagent preview (right-alignment)", async () => {
        server = installFakeDslServer(subagentFlexLayoutDoc());
        await renderApp();
        const preview = await screen.findByTestId("subagent-preview-L0");
        // The preview rows render inside a ${width}ch terminal-width wrapper,
        // like the main preview surface, so the flex filler renders at literal
        // width instead of being shrunk by a flex row.
        expect(preview.querySelector(".terminal-width")).toBeTruthy();
        await waitFor(
            () => {
                const rows = preview.querySelectorAll(".subagent-preview-line");
                expect(rows.length).toBeGreaterThan(0);
                const first = rows[0];
                // The <flex/> filler renders as its own whitespace segment
                // between the fields — a flex layout would have collapsed it,
                // killing the right-alignment. Its whitespace must survive.
                const segs = Array.from(first.querySelectorAll(".subagent-seg"));
                expect(segs.some((s) => /^\s+$/.test(s.textContent ?? ""))).toBe(true);
                // Order: description, then (flex space), then model.
                expect(first.textContent).toMatch(/Review render pipeline\s+Opus 4\.8/);
            },
            { timeout: 3000 },
        );
    });

    it("renders subagent rows below the main lines in pure output", async () => {
        server = installFakeDslServer(subagentLayoutDoc());
        await renderApp();
        // Wait until the subagent preview data has arrived.
        await waitFor(() => expect(screen.getByTestId("subagent-preview-L0")).toBeTruthy(), {
            timeout: 3000,
        });
        fireEvent.click(screen.getByLabelText(/Pure output/));
        await waitFor(
            () => {
                const pre = document.querySelector(".pure-pre");
                expect(pre).toBeTruthy();
                // The subagent task values (only produced by the subagent pass)
                // now appear in the pure output, stacked below the main lines.
                expect(pre?.textContent).toContain("Review render pipeline");
            },
            { timeout: 3000 },
        );
    });
});
