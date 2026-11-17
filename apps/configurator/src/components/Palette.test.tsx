import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { Palette } from "./Palette.tsx";
import type { FieldCatalogEntry } from "../types.ts";

const PLAIN_FIELD: FieldCatalogEntry = {
    name: "model",
    displayName: "Model",
    descriptions: { en: "The model name", ja: "モデル名" },
    category: "common",
    preview: { text: "Opus", ansi: "Opus" },
};

const OAUTH_FIELD: FieldCatalogEntry = {
    name: "extra-usage-percent",
    displayName: "Extra usage %",
    descriptions: { en: "Extra usage percentage", ja: "従量課金の使用率" },
    category: "claude",
    capability: "oauth-usage",
    preview: { text: "12%", ansi: "12%" },
};

const MOD_FIELD: FieldCatalogEntry = {
    name: "subagents-running",
    displayName: "Running Subagents",
    descriptions: { en: "Subagents running now", ja: "実行中のサブエージェント数" },
    category: "mod",
    preview: { text: "3", ansi: "3" },
};

const SUBAGENT_FIELD: FieldCatalogEntry = {
    name: "task-name",
    displayName: "Task Name",
    descriptions: { en: "Name of the subagent task", ja: "サブエージェントのタスク名" },
    category: "subagent",
    preview: { text: "review", ansi: "review" },
};

describe("Palette", () => {
    it("groups mod fields under their own heading, between Claude Code and Subagent", () => {
        render(
            <Palette
                fields={[SUBAGENT_FIELD, MOD_FIELD, OAUTH_FIELD]}
                onAdd={vi.fn()}
                oauthUsageAvailable={true}
            />,
        );

        const modHeading = screen.getByRole("heading", { name: "Claude Code mod" });
        const modGroup = modHeading.closest(".palette-group") as HTMLElement;
        expect(
            within(modGroup).getByTestId("palette-field:subagents-running"),
        ).toBeTruthy();

        const headings = screen
            .getAllByRole("heading", { level: 3 })
            .map((h) => h.textContent);
        expect(headings).toEqual(["Claude Code", "Claude Code mod", "Subagent", "Layout"]);
    });

    it("still shows oauth-usage-capability fields, and shows the note, when the probe is unavailable", () => {
        render(
            <Palette
                fields={[PLAIN_FIELD, OAUTH_FIELD]}
                onAdd={vi.fn()}
                oauthUsageAvailable={false}
            />,
        );

        expect(screen.getByTestId("palette-field:model")).toBeTruthy();
        expect(screen.getByTestId("palette-field:extra-usage-percent")).toBeTruthy();
        expect(
            screen.getByText(/usage API isn't reachable on this device/i),
        ).toBeTruthy();
    });

    it("shows oauth-usage-capability fields and hides the note when the probe is available", () => {
        render(
            <Palette
                fields={[PLAIN_FIELD, OAUTH_FIELD]}
                onAdd={vi.fn()}
                oauthUsageAvailable={true}
            />,
        );

        expect(screen.getByTestId("palette-field:model")).toBeTruthy();
        expect(screen.getByTestId("palette-field:extra-usage-percent")).toBeTruthy();
        expect(
            screen.queryByText(/usage API isn't reachable on this device/i),
        ).toBeNull();
    });

    it("shows no note when there are no oauth-usage-capability fields at all", () => {
        render(
            <Palette fields={[PLAIN_FIELD]} onAdd={vi.fn()} oauthUsageAvailable={false} />,
        );

        expect(
            screen.queryByText(/usage API isn't reachable on this device/i),
        ).toBeNull();
    });
});
