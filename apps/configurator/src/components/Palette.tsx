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
    // catalog entry carries `capability: "oauth-usage"` are always shown in
    // the palette (whether they can be configured is independent of whether
    // this particular probe happened to succeed just now); when this is
    // false we only show a note that live preview data isn't available yet.
    oauthUsageAvailable: boolean;
}

const CATEGORY_ORDER = ["common", "claude", "subagent"];
const CATEGORY_LABEL: Record<string, string> = {
    common: "Common",
    claude: "Claude Code",
    subagent: "Subagent",
};

// Chips sit in a two-column grid, so a long name or a long preview value
// (paths, emails, session ids) would be clipped to an ellipsis in one column.
// Those chips opt out of the grid and span the full width instead, laying the
// name and the sample out side by side (`.palette-chip.wide` in styles.css).
// The thresholds are the number of characters that fit in one column at the
// palette's 340px width: names render at 13px, samples at 11px monospace.
const WIDE_NAME_CHARS = 18;
const WIDE_SAMPLE_CHARS = 14;

function isWideChip(label: string, sample: string): boolean {
    return label.length > WIDE_NAME_CHARS || sample.length > WIDE_SAMPLE_CHARS;
}

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
            className={
                "palette-chip" +
                (isWideChip(label, sample) ? " wide" : "") +
                (isDragging ? " dragging" : "")
            }
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
    // oauth-usage-capability fields are always shown: whether the field can
    // be *configured* is independent of whether this session's probe of the
    // authenticated usage API happened to succeed (the actual render path
    // runs on its own schedule/cache and may well succeed even when the
    // configurator's probe just failed). When the probe is unavailable we
    // only show a note below (see showOAuthUsageUnavailableNote) that live
    // preview data isn't available right now — we never hide the fields
    // themselves. task-effort (capability "subagent-effort") has no probe at
    // all — no environment currently supports it (see markup.md /
    // DSL_API.md) — so it is hidden unconditionally, the same way any future
    // permanently-unavailable capability should be handled here.
    const visibleFields = fields.filter((f) => f.capability !== "subagent-effort");
    const showOAuthUsageUnavailableNote =
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
            {showOAuthUsageUnavailableNote ? (
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
