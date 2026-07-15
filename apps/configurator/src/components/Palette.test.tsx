import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
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

// task-effort's capability ("subagent-effort") currently has no environment
// that satisfies it (no probe exists for it, unlike oauth-usage), so it must
// never appear in the palette regardless of `oauthUsageAvailable`.
const SUBAGENT_EFFORT_FIELD: FieldCatalogEntry = {
    name: "task-effort",
    displayName: "Task Effort",
    descriptions: { en: "Task effort", ja: "タスクの努力レベル" },
    category: "subagent",
    capability: "subagent-effort",
    preview: { text: "", ansi: "" },
};

describe("Palette", () => {
    it("hides oauth-usage-capability fields and shows the note when the probe is unavailable", () => {
        render(
            <Palette
                fields={[PLAIN_FIELD, OAUTH_FIELD]}
                onAdd={vi.fn()}
                oauthUsageAvailable={false}
            />,
        );

        expect(screen.getByTestId("palette-field:model")).toBeTruthy();
        expect(screen.queryByTestId("palette-field:extra-usage-percent")).toBeNull();
        expect(
            screen.getByText(/Extra-usage fields are unavailable/i),
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
        expect(screen.queryByText(/Extra-usage fields are unavailable/i)).toBeNull();
    });

    it("shows no note when there are no oauth-usage-capability fields at all", () => {
        render(
            <Palette fields={[PLAIN_FIELD]} onAdd={vi.fn()} oauthUsageAvailable={false} />,
        );

        expect(screen.queryByText(/Extra-usage fields are unavailable/i)).toBeNull();
    });

    it("hides task-effort (subagent-effort capability) unconditionally", () => {
        render(
            <Palette
                fields={[PLAIN_FIELD, SUBAGENT_EFFORT_FIELD]}
                onAdd={vi.fn()}
                oauthUsageAvailable={true}
            />,
        );

        expect(screen.getByTestId("palette-field:model")).toBeTruthy();
        expect(screen.queryByTestId("palette-field:task-effort")).toBeNull();
    });
});
