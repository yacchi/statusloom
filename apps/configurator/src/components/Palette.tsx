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

export function Palette({ fields, onAdd }: Props) {
    const lang = useLang();
    const categories = [
        ...CATEGORY_ORDER,
        ...[...new Set(fields.map((f) => f.category))].filter(
            (c) => !CATEGORY_ORDER.includes(c),
        ),
    ];
    return (
        <div className="panel">
            <h2>Fields</h2>
            <p className="hint">{t(lang, "paletteHint")}</p>
            {categories.map((cat) => {
                const entries = fields.filter((f) => f.category === cat);
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
