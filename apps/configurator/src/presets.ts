// Palette presets: what a palette chip inserts into the AST. Fields come from
// the server catalog (GET /api/dsl/fields); the structural presets (text,
// separator, flex, span) and the semantic field presets (markup.md "UI上の
// 意味的プリセット") are defined here. Presets expand to plain AST nodes —
// there is no dedicated separator (or preset) node kind.

import type { FieldCatalogEntry, FieldNode, LineChild, LineNode } from "./types.ts";

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
        make: () => ({ id: "", kind: "text", role: "separator", value: "|", padding: 1 }),
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

// ---- default subagent <line> ----
//
// A subagent region's content is small and near-always the same, so "+ Add
// subagent row" / "Use default fields" (Canvas.tsx) fill it with this
// built-in default rather than leaving the user to place task-* fields by
// hand. This MUST mirror internal/config/document.go's claudeCodeDefaultDocument
// <subagent><line> verbatim (that Go source is the single source of truth for
// the default DSL document); update both together.
//
// FieldNode (types.ts) does not yet model the `min-width`/`align` attributes
// (the wire format already round-trips them — internal/webconfig/astjson.go
// — the TS type just hasn't caught up), so `taskField` accepts them via a
// loosely-typed attrs bag and asserts the result's shape at the boundary
// instead of widening FieldNode itself.
interface TaskFieldAttrs {
    prefix?: string;
    suffix?: string;
    format?: string;
    precision?: string;
    optional?: string;
    when?: string;
    "min-width"?: number;
    align?: string;
}

function taskField(name: string, attrs: TaskFieldAttrs = {}): FieldNode {
    return { id: "", kind: "field", name, ...attrs } as FieldNode;
}

// Builds a fresh <line> matching document.go's default subagent row:
//   <field name="task-description"/>
//   <field name="task-model" prefix="  "/>
//   <flex/>
//   <field name="task-duration" format="duration" when="width ge 48" min-width="7" align="right"/>
//   <field name="task-tokens" prefix=" · ↓ " format="compact-number" optional="task-tokens" when="width ge 64" min-width="6" align="right"/>
//   <field name="task-context-percent" prefix=" (" suffix=")" format="percent" precision="0" optional="task-context-percent" when="width ge 80" min-width="4" align="right"/>
// A fresh node every call (id "" — the next serialize -> parse round trip
// assigns real ids, exactly like the other add-* helpers in ast.ts).
export function makeDefaultSubagentLine(): LineNode {
    return {
        id: "",
        kind: "line",
        children: [
            taskField("task-description"),
            taskField("task-model", { prefix: "  " }),
            { id: "", kind: "flex" },
            taskField("task-duration", {
                format: "duration",
                when: "width ge 48",
                "min-width": 7,
                align: "right",
            }),
            taskField("task-tokens", {
                prefix: " · ↓ ",
                format: "compact-number",
                optional: "task-tokens",
                when: "width ge 64",
                "min-width": 6,
                align: "right",
            }),
            taskField("task-context-percent", {
                prefix: " (",
                suffix: ")",
                format: "percent",
                precision: "0",
                optional: "task-context-percent",
                when: "width ge 80",
                "min-width": 4,
                align: "right",
            }),
        ],
    };
}
