// App-level integration test for the Git settings modal: it is hidden by
// default and toggled by the header ⚙ button. The display settings (compact
// threshold, output style, …) now live near the Canvas and are always shown;
// only the Git settings live in the modal.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "./App.tsx";
import { defaultTestDoc, installFakeDslServer } from "./test/fakeDsl.ts";

const TOKEN = "a".repeat(32);

beforeEach(() => {
    window.location.hash = `#token=${TOKEN}`;
    installFakeDslServer(defaultTestDoc());
});

afterEach(() => {
    vi.unstubAllGlobals();
    window.location.hash = "";
    window.localStorage.clear();
});

async function renderApp() {
    const view = render(<App />);
    await waitFor(() => expect(screen.getByTestId("settings-button")).toBeTruthy(), {
        timeout: 3000,
    });
    return view;
}

describe("settings modal", () => {
    it("shows the display settings inline and gates the git settings behind the ⚙ button", async () => {
        await renderApp();

        // Display settings render near the Canvas without opening anything
        // (they appear once the document has parsed).
        await waitFor(() =>
            expect(screen.getByTestId("setting-output-style")).toBeTruthy(),
        );

        // The git settings form is not mounted anywhere by default.
        expect(screen.queryByTestId("setting-git-cache-ttl")).toBeNull();

        // Opening via the gear button reveals the git form.
        fireEvent.click(screen.getByTestId("settings-button"));
        await waitFor(() =>
            expect(screen.getByTestId("setting-git-cache-ttl")).toBeTruthy(),
        );

        // Closing removes it again.
        fireEvent.click(screen.getByText("Close"));
        await waitFor(() =>
            expect(screen.queryByTestId("setting-git-cache-ttl")).toBeNull(),
        );
    });
});
