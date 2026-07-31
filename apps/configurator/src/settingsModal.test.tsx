// App-level integration test for the global settings modal: it is hidden by
// default and toggled by the header ⚙ button, and holds the editor language
// plus the document's git settings. The display settings (compact threshold,
// output style, …) live near the Canvas instead and are always shown.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { App } from "./App.tsx";
import { defaultTestDoc, installFakeDslServer } from "./test/fakeDsl.ts";
import { LANG_STORAGE_KEY } from "./i18n.ts";

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

        // Closing removes it again (via the modal's × button).
        fireEvent.click(screen.getByTestId("modal-close"));
        await waitFor(() =>
            expect(screen.queryByTestId("setting-git-cache-ttl")).toBeNull(),
        );
    });

    it("hosts the editor language in the modal and persists the choice", async () => {
        await renderApp();

        // The language is a global setting, not a header toolbar toggle.
        expect(screen.queryByTestId("setting-lang")).toBeNull();

        fireEvent.click(screen.getByTestId("settings-button"));
        const select = (await waitFor(() =>
            screen.getByTestId("setting-lang"),
        )) as HTMLSelectElement;
        expect(select.value).toBe("en");

        fireEvent.change(select, { target: { value: "ja" } });
        await waitFor(() =>
            expect(
                (screen.getByTestId("setting-lang") as HTMLSelectElement).value,
            ).toBe("ja"),
        );
        expect(window.localStorage.getItem(LANG_STORAGE_KEY)).toBe("ja");
    });
});
