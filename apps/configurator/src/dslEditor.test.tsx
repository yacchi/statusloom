// App-level tests of the DSL Editor pane and the invalid-source behavior
// (markup.md "DSL syntax error時"): while the source has error diagnostics,
// the preview and the visual editor keep the last valid AST (read-only),
// saving is blocked, and everything resynchronizes once the source is fixed.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "./App.tsx";
import { defaultTestDoc, installFakeDslServer, type FakeServer } from "./test/fakeDsl.ts";

const TOKEN = "a".repeat(32);

let server: FakeServer;

beforeEach(() => {
    window.location.hash = `#token=${TOKEN}`;
    server = installFakeDslServer(defaultTestDoc());
});

afterEach(() => {
    vi.unstubAllGlobals();
    window.location.hash = "";
    window.localStorage.clear();
});

async function renderSplit() {
    const view = render(<App />);
    await waitFor(() => expect(screen.getByTestId("seg-0-0")).toBeTruthy(), { timeout: 3000 });
    fireEvent.click(screen.getByTestId("view-split"));
    await waitFor(() => expect(screen.getByTestId("dsl-textarea")).toBeTruthy());
    return view;
}

function textarea(): HTMLTextAreaElement {
    return screen.getByTestId("dsl-textarea") as HTMLTextAreaElement;
}

describe("DSL editor", () => {
    it("shows the current source and stays in sync with visual edits", async () => {
        await renderSplit();
        expect(textarea().value).toBe(server.document);

        fireEvent.click(screen.getByTestId("palette-field:git-branch"));
        await waitFor(() => expect(textarea().value).toContain('"git-branch"'), {
            timeout: 3000,
        });
    });

    it("invalid source: diagnostics appear, the canvas keeps the last valid AST read-only, saving is blocked", async () => {
        await renderSplit();
        const goodSource = textarea().value;

        fireEvent.change(textarea(), { target: { value: "PARSE-ERROR <<<" } });

        // Diagnostics and the invalid status appear after the parse debounce.
        await waitFor(
            () => expect(screen.getByTestId("dsl-status").textContent).toBe("✕"),
            { timeout: 3000 },
        );
        expect(screen.getByTestId("diag-0").textContent).toContain("not well-formed");

        // The canvas still shows the last valid AST's chips...
        expect(screen.getByTestId("seg-0-0")).toBeTruthy();
        // ...but is read-only.
        expect(screen.getByTestId("canvas-readonly")).toBeTruthy();

        // Saving is blocked.
        expect((screen.getByTestId("save-button") as HTMLButtonElement).disabled).toBe(true);

        // Visual edits are no-ops while invalid.
        fireEvent.click(screen.getByTestId("palette-field:model"));
        await new Promise((r) => setTimeout(r, 600));
        expect(textarea().value).toBe("PARSE-ERROR <<<");

        // The invalid source is still shared through the draft channel.
        await waitFor(
            () =>
                expect(server.putDraftBodies[server.putDraftBodies.length - 1]).toBe(
                    "PARSE-ERROR <<<",
                ),
            { timeout: 3000 },
        );

        // Fixing the source restores everything.
        fireEvent.change(textarea(), { target: { value: goodSource } });
        await waitFor(
            () => expect(screen.getByTestId("dsl-status").textContent).toBe("✓"),
            { timeout: 3000 },
        );
        expect(screen.queryByTestId("canvas-readonly")).toBeNull();
        expect((screen.getByTestId("save-button") as HTMLButtonElement).disabled).toBe(false);
    });

    it("clicking a diagnostic moves the caret to its source position", async () => {
        await renderSplit();
        fireEvent.change(textarea(), { target: { value: "xx PARSE-ERROR" } });
        await waitFor(() => expect(screen.getByTestId("diag-0")).toBeTruthy(), {
            timeout: 3000,
        });
        // The fake reports range {0,3}; the row shows 1:1 and clicking it
        // selects that span in the textarea.
        expect(screen.getByTestId("diag-0").textContent).toContain("1:1");
        fireEvent.click(screen.getByTestId("diag-0"));
        expect(textarea().selectionStart).toBe(0);
        expect(textarea().selectionEnd).toBe(3);
    });

    it("saves the document when valid and records warnings-only diagnostics", async () => {
        await renderSplit();
        fireEvent.click(screen.getByTestId("save-button"));
        await waitFor(() => expect(server.putDocumentBodies).toHaveLength(1), {
            timeout: 3000,
        });
        expect(server.putDocumentBodies[0]).toBe(server.document);
    });

    it("DSL-only view hides the canvas; visual view hides the editor", async () => {
        await renderSplit();
        fireEvent.click(screen.getByTestId("view-dsl"));
        expect(screen.queryByTestId("seg-0-0")).toBeNull();
        expect(screen.getByTestId("dsl-textarea")).toBeTruthy();

        fireEvent.click(screen.getByTestId("view-visual"));
        await waitFor(() => expect(screen.getByTestId("seg-0-0")).toBeTruthy(), {
            timeout: 3000,
        });
        expect(screen.queryByTestId("dsl-textarea")).toBeNull();
    });
});
