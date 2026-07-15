// App-level tests of the session/subagent document switch. GET /api/tools
// drives the tab list (never hardcoded client-side). Switching is a pure
// in-memory view swap, NOT a reload: each tool's editing state (undo
// history, unsaved edits, selection, preview, sample toggle) is kept alive
// and restored on return — no reset, no "Loading…" flash, and a tool is
// fetched from the server exactly once (its first open). Switching still
// flushes the outgoing tool's pending draft (data-loss safety), and every
// preview/save/draft call stays bound to the tool its state belongs to.

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

function calledUrls(): string[] {
    return server.fetchMock.mock.calls.map((call: unknown[]) => String(call[0]));
}

function calledBodies(urlSuffix: string): Record<string, unknown>[] {
    return server.fetchMock.mock.calls
        .filter((call: unknown[]) => String(call[0]).endsWith(urlSuffix))
        .map((call: unknown[]) => {
            const init = call[1] as RequestInit | undefined;
            return init?.body ? JSON.parse(String(init.body)) : {};
        });
}

function docGetCountFor(tool: string): number {
    // getDocument's URL is `/api/dsl/document?tool=<tool>` with the tool as
    // the final token, so an exact endsWith avoids "claude-code" also
    // matching "claude-code-subagent".
    return calledUrls().filter(
        (u) => u.startsWith("/api/dsl/document") && u.endsWith(`tool=${tool}`),
    ).length;
}

function rowCount(): number {
    return document.querySelectorAll(".canvas-row").length;
}

describe("tool switching", () => {
    it("renders a tab per GET /api/tools entry and starts on the first", async () => {
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        expect(screen.getByTestId("tool-claude-code").getAttribute("aria-selected")).toBe("true");
        expect(
            screen.getByTestId("tool-claude-code-subagent").getAttribute("aria-selected"),
        ).toBe("false");
    });

    it("switches the palette, document, and preview to the subagent tool", async () => {
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        fireEvent.click(screen.getByTestId("tool-claude-code-subagent"));

        // The subagent bundle loads on first open: the palette now shows the
        // task-* fields, not the session ones.
        await waitFor(
            () => expect(screen.getByTestId("palette-field:task-description")).toBeTruthy(),
            { timeout: 3000 },
        );
        expect(screen.queryByTestId("palette-field:model")).toBeNull();
        expect(
            screen.getByTestId("tool-claude-code-subagent").getAttribute("aria-selected"),
        ).toBe("true");

        // Every fields/document request after the switch names the subagent
        // tool; the preview dispatch carries it too.
        const fieldsCalls = calledUrls().filter((u) => u.startsWith("/api/dsl/fields"));
        expect(fieldsCalls.some((u) => u.includes("tool=claude-code-subagent"))).toBe(true);
        const documentCalls = calledUrls().filter((u) => u.startsWith("/api/dsl/document"));
        expect(documentCalls.some((u) => u.includes("tool=claude-code-subagent"))).toBe(true);
        await waitFor(() => {
            const previewBodies = calledBodies("/api/dsl/preview");
            expect(previewBodies.some((b) => b.tool === "claude-code-subagent")).toBe(true);
        });
    });

    it("keeps each tool's edits and undo history across switches (no reload, no reset)", async () => {
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });
        await waitFor(() => expect(rowCount()).toBe(2), { timeout: 3000 });

        // Edit the session document: add a field so line 0 gains a 2nd chip
        // and the undo history has an entry.
        fireEvent.click(screen.getByTestId("palette-field:model"));
        await waitFor(() => expect(screen.getByTestId("seg-0-1")).toBeTruthy(), { timeout: 3000 });
        const undo = screen.getByTitle(/Undo/i) as HTMLButtonElement;
        expect(undo.disabled).toBe(false);

        // The session document was fetched exactly once so far (its first and
        // only load).
        expect(docGetCountFor("claude-code")).toBe(1);

        // Go to the subagent tool (first open loads it once).
        fireEvent.click(screen.getByTestId("tool-claude-code-subagent"));
        await waitFor(
            () => expect(screen.getByTestId("palette-field:task-description")).toBeTruthy(),
            { timeout: 3000 },
        );

        // Return to the session tool: it must be restored from memory, NOT
        // refetched, and it must still carry the edit and the undo history.
        fireEvent.click(screen.getByTestId("tool-claude-code"));
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        // The edit survived the round trip: line 0 still has the added chip.
        expect(screen.getByTestId("seg-0-1")).toBeTruthy();
        // Undo history survived and still reverts the edit.
        const undoAfter = screen.getByTitle(/Undo/i) as HTMLButtonElement;
        expect(undoAfter.disabled).toBe(false);
        fireEvent.click(undoAfter);
        await waitFor(() => expect(screen.queryByTestId("seg-0-1")).toBeNull(), { timeout: 3000 });

        // Restoring from memory issued no second document GET for the session
        // tool (still the single initial load), and exactly one for the
        // subagent tool (its first open only).
        expect(docGetCountFor("claude-code")).toBe(1);
        expect(docGetCountFor("claude-code-subagent")).toBe(1);
    });

    it("flushes the outgoing document's pending edit to its own draft before switching", async () => {
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });
        await waitFor(() => expect(rowCount()).toBe(2), { timeout: 3000 });

        // Land a pending edit (line 0 gains a 2nd chip = 3 total "model").
        fireEvent.click(screen.getByTestId("palette-field:model"));
        await waitFor(() => expect(screen.getByTestId("seg-0-1")).toBeTruthy(), { timeout: 3000 });

        // Switch — the switch's own immediate flush writes the edit to the
        // session tool's draft, tagged with the session tool id.
        fireEvent.click(screen.getByTestId("tool-claude-code-subagent"));

        await waitFor(() => {
            const draftBodies = calledBodies("/api/dsl/draft");
            const sessionWrite = draftBodies.find(
                (b) => b.tool === "claude-code" && String(b.source).match(/"model"/g)?.length === 3,
            );
            expect(sessionWrite).toBeTruthy();
        });
    });

    it("keeps each tool's own preview sample toggle across switches", async () => {
        render(<App />);
        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });

        fireEvent.click(screen.getByTestId("tool-claude-code-subagent"));
        const select = (await screen.findByTestId(
            "preview-source-select",
        )) as HTMLSelectElement;
        // Subagent samples, defaulting to running.
        await waitFor(() => expect(select.value).toBe("sample:subagent-running"));

        // Flip to the completed sample.
        fireEvent.change(select, { target: { value: "sample:subagent-completed" } });
        await waitFor(() => expect(select.value).toBe("sample:subagent-completed"));

        // Away and back: the subagent tool remembers its toggle (not reset to
        // the running default), while the session tool keeps its own "full".
        fireEvent.click(screen.getByTestId("tool-claude-code"));
        await waitFor(() =>
            expect(
                (screen.getByTestId("preview-source-select") as HTMLSelectElement).value,
            ).toBe("sample:full"),
        );
        fireEvent.click(screen.getByTestId("tool-claude-code-subagent"));
        await waitFor(() =>
            expect(
                (screen.getByTestId("preview-source-select") as HTMLSelectElement).value,
            ).toBe("sample:subagent-completed"),
        );
    });
});
