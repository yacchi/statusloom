import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { App } from "./App.tsx";

describe("App", () => {
    it("renders the full-page error when the URL has no token", () => {
        // jsdom's default location has an empty hash, so no token is present.
        render(<App />);
        expect(screen.getByText(/statusloom config/i)).toBeTruthy();
        expect(screen.getByText(/Cannot open configurator/i)).toBeTruthy();
    });
});
