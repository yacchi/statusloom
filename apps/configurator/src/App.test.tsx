import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { App } from "./App.tsx";
import { defaultTestDoc, installFakeDslServer, type FakeServer } from "./test/fakeDsl.ts";

describe("App", () => {
    it("renders the full-page error when the URL has no token", () => {
        // jsdom's default location has an empty hash, so no token is present.
        render(<App />);
        expect(screen.getByText(/statusloom config/i)).toBeTruthy();
        expect(screen.getByText(/Cannot open configurator/i)).toBeTruthy();
    });
});

// Re-fetching the field catalog after a successful usage-API probe: the
// backend's GET /api/usage/probe, when it succeeds (reason "ok"), persists
// the user's real extra-usage / weekly-usage values to the shared
// account-usage cache (usageprobe.go's persistAccountUsage), and GET
// /api/dsl/fields' preview then overlays those real values
// (dsl.go's overlayRealAccountUsage) instead of the synthetic sample. The
// palette only shows this if the frontend re-fetches the field catalog once
// the probe resolves.
describe("App usage-probe field refresh", () => {
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

    function fieldsRequestCount(): number {
        return server.fetchMock.mock.calls.filter((call: unknown[]) =>
            String(call[0]).startsWith("/api/dsl/fields"),
        ).length;
    }

    it("re-fetches fields after a successful probe (reason ok)", async () => {
        server.usageProbe = { available: true, reason: "ok", extraUsageEnabled: true };
        render(<App />);

        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });
        // Initial load fetches fields once; the probe resolving with "ok"
        // should trigger exactly one more fetch.
        await waitFor(() => expect(fieldsRequestCount()).toBe(2), { timeout: 3000 });
    });

    it("does not re-fetch fields when the probe is unavailable", async () => {
        server.usageProbe = { available: false, reason: "no-token" };
        render(<App />);

        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });
        // Give the (fail-closed) probe effect a moment to settle, then make
        // sure it never asked for the fields a second time.
        await waitFor(() => expect(server.fetchMock).toHaveBeenCalled());
        expect(fieldsRequestCount()).toBe(1);
    });

    it("does not re-fetch fields when the probe is available but rate-limited", async () => {
        // "rate-limited" is available=true but the backend did not fetch (and
        // therefore did not persist) a report; a refetch here would be wasted.
        server.usageProbe = { available: true, reason: "rate-limited" };
        render(<App />);

        await waitFor(() => expect(screen.getByTestId("palette-field:model")).toBeTruthy(), {
            timeout: 3000,
        });
        await waitFor(() => expect(server.fetchMock).toHaveBeenCalled());
        expect(fieldsRequestCount()).toBe(1);
    });
});
