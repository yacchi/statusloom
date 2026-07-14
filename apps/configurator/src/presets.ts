// Palette presets: what a palette chip inserts into the AST. Fields come from
// the server catalog (GET /api/dsl/fields); the structural presets (text,
// separator, flex, span) and the semantic field presets (markup.md "UI上の
// 意味的プリセット") are defined here. Presets expand to plain AST nodes —
// there is no dedicated separator (or preset) node kind.

import type { FieldCatalogEntry, LineChild } from "./types.ts";

// Structural presets shown in the palette's "Layout" group.
export type StructuralPresetId = "text" | "separator" | "flex" | "span";

export interface StructuralPreset {
    id: StructuralPresetId;
    label: string;
    // Sample text shown on the palette chip.
    sample: string;
    make(): LineChild;
}

export const STRUCTURAL_PRESETS: StructuralPreset[] = [
    {
        id: "text",
        label: "Text",
        sample: "text",
        make: () => ({ id: "", kind: "text", value: "text" }),
    },
    {
        id: "separator",
        label: "Separator",
        sample: "|",
        // <text role="separator" padding="1">|</text> — the collapsing
        // separator per markup.md "separator".
        make: () => ({ id: "", kind: "text", role: "separator", value: "" }),
    },
    {
        id: "flex",
        label: "Flex",
        sample: "⇔",
        make: () => ({ id: "", kind: "flex" }),
    },
    {
        id: "span",
        label: "Span",
        sample: "( )",
        make: () => ({ id: "", kind: "span", children: [] }),
    },
];

// Semantic prefixes for fields whose legacy defaultTemplate wrapped the value
// (e.g. five-hour-usage -> "5h: {value}"). Placing such a field creates
// <span optional="<name>" prefix="<prefix>"><field name="<name>"/></span> so
// the label disappears together with the value.
const FIELD_PRESET_PREFIX: Record<string, string> = {
    "five-hour-usage": "5h: ",
    "weekly-usage": "7d: ",
    "api-duration": "api: ",
    "cache-hit-rate": "cache: ",
};

// Build the AST node a palette field chip inserts.
export function makeFieldNode(entry: Pick<FieldCatalogEntry, "name">): LineChild {
    const prefix = FIELD_PRESET_PREFIX[entry.name];
    if (prefix) {
        return {
            id: "",
            kind: "span",
            optional: entry.name,
            prefix,
            children: [{ id: "", kind: "field", name: entry.name }],
        };
    }
    return { id: "", kind: "field", name: entry.name };
}

// Short label for a chip that has no rendered output (ghost / plain chips).
export function nodeLabel(node: LineChild, displayName: (field: string) => string): string {
    switch (node.kind) {
        case "field":
            return node.name ? displayName(node.name) : "field";
        case "text":
            return node.role === "separator"
                ? JSON.stringify(node.value)
                : node.value || "text";
        case "raw-text":
            return node.value || "text";
        case "flex":
            return node.size ? `flex (${node.size})` : "flex";
        case "span":
            return "span";
        case "comment":
            return "<!-- -->";
    }
}
