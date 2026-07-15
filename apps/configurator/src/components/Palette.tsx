// The palette: dynamic fields from GET /api/dsl/fields (grouped by catalog
// category) plus the structural presets (Text / Separator / Flex / Span).
// A chip click appends to the active line; a chip drag places precisely.
// Palette keys (after the drag-id prefix) are "field:<name>" or
// "preset:<id>"; App.tsx expands them to AST nodes via presets.ts.

import { useDraggable } from "@dnd-kit/core";
import { PALETTE_ID_PREFIX } from "../useDragEditing.ts";
import { pickDescription, t, useLang } from "../i18n.ts";
import { STRUCTURAL_PRESETS } from "../presets.ts";
import type { FieldCatalogEntry } from "../types.ts";

interface Props {
    fields: FieldCatalogEntry[];
    onAdd: (key: string) => void;
    // Whether the authenticated OAuth usage API is reachable. Fields whose
    // catalog entry carries `capability: "oauth-usage"` are hidden from the
    // palette unless this is true (probe-gated: "駄目ならパレットに出さない").
    oauthUsageAvailable: boolean;
}

const CATEGORY_ORDER = ["common", "claude"];
const CATEGORY_LABEL: Record<string, string> = {
    common: "Common",
    claude: "Claude Code",
};

function PaletteChip({
    paletteKey,
    label,
    sample,
    tip,
    onAdd,
}: {
    paletteKey: string;
    label: string;
    sample: string;
    tip: string;
    onAdd: (key: string) => void;
}) {
    const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
        id: PALETTE_ID_PREFIX + paletteKey,
    });
    return (
        <button
            ref={setNodeRef}
            {...attributes}
            {...listeners}
            className={"palette-chip" + (isDragging ? " dragging" : "")}
            title={tip}
            data-testid={`palette-${paletteKey}`}
            onClick={() => onAdd(paletteKey)}
        >
            <span className="palette-name">{label}</span>
            {sample !== "" ? <span className="palette-sample">{sample}</span> : null}
        </button>
    );
}

export function Palette({ fields, onAdd, oauthUsageAvailable }: Props) {
    const lang = useLang();
    // Hide oauth-usage-capability fields until the probe confirms the
    // authenticated usage API is reachable; never flash them in only to
    // remove them a moment later. task-effort (capability "subagent-effort")
    // has no probe at all — no environment currently supports it (see
    // markup.md / DSL_API.md) — so it is hidden unconditionally, the same
    // way any future never-available capability should be handled here.
    const visibleFields = fields.filter((f) => {
        if (f.capability === "oauth-usage") {
            return oauthUsageAvailable;
        }
        if (f.capability === "subagent-effort") {
            return false;
        }
        return true;
    });
    const hasHiddenOAuthUsage =
        !oauthUsageAvailable && fields.some((f) => f.capability === "oauth-usage");
    const categories = [
        ...CATEGORY_ORDER,
        ...[...new Set(visibleFields.map((f) => f.category))].filter(
            (c) => !CATEGORY_ORDER.includes(c),
        ),
    ];
    return (
        <div className="panel">
            <h2>Fields</h2>
            <p className="hint">{t(lang, "paletteHint")}</p>
            {hasHiddenOAuthUsage ? (
                <p className="hint palette-oauth-usage-note">
                    {t(lang, "oauthUsageUnavailableNote")}
                </p>
            ) : null}
            {categories.map((cat) => {
                const entries = visibleFields.filter((f) => f.category === cat);
                if (entries.length === 0) {
                    return null;
                }
                return (
                    <div className="palette-group" key={cat}>
                        <h3>{CATEGORY_LABEL[cat] ?? cat}</h3>
                        <div className="palette-chips">
                            {entries.map((entry) => (
                                <PaletteChip
                                    key={entry.name}
                                    paletteKey={`field:${entry.name}`}
                                    label={entry.displayName}
                                    sample={entry.preview?.text ?? ""}
                                    tip={pickDescription(entry, lang)}
                                    onAdd={onAdd}
                                />
                            ))}
                        </div>
                    </div>
                );
            })}
            <div className="palette-group">
                <h3>Layout</h3>
                <div className="palette-chips">
                    {STRUCTURAL_PRESETS.map((preset) => (
                        <PaletteChip
                            key={preset.id}
                            paletteKey={`preset:${preset.id}`}
                            label={preset.label}
                            sample={preset.sample}
                            tip=""
                            onAdd={onAdd}
                        />
                    ))}
                </div>
            </div>
        </div>
    );
}
