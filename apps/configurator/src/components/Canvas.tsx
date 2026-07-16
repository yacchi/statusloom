// Direct-manipulation editing canvas: the preview itself is the editor.
// Each AST line renders as one horizontal row on a terminal-styled
// background; each top-level child of the line is a selectable/draggable
// chip, painted from the node-ID segments of POST /api/dsl/preview. A node
// with no output in the current sample renders as a dimmed ghost chip so it
// stays editable. A <span> renders as a chip group: a bordered container
// whose own prefix/suffix/padding segments select the span, wrapping nested
// chips for its children. Span children live in their own SortableContext
// (one per container), so chips can be dragged into, out of, and within
// spans; the span group itself is a drop container (hovering its area
// appends to the span's end).
//
// A layout's children mix plain <line> rows with <responsive> containers
// (markup.md / plans/responsive-container-design.md): a responsive renders
// as a row of <variant> cards (widest first), each holding its own stack of
// lines rendered exactly like a plain row. Preview segments are matched back
// onto lines by AST id (previewMatch.ts) rather than by position, since the
// width-selected variant determines how many PreviewLines a responsive
// contributes; the selected variant is highlighted so changing the width
// slider visibly shows which candidate is currently in effect.
//
// IMPORTANT: nothing in this component may restructure or resize during a
// drag based on drag state — drop indicators are paint-only (::before
// pseudo-elements / box-shadows), and every SortableContext items array is
// derived from the AST, which is frozen during a drag. See the regression
// note in useDragEditing.

import type { CSSProperties, PointerEvent as ReactPointerEvent, ReactNode } from "react";
import { useDroppable } from "@dnd-kit/core";
import {
    SortableContext,
    horizontalListSortingStrategy,
    useSortable,
    verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { parseAnsiLine, type Theme } from "../ansi.ts";
import { t, useLang } from "../i18n.ts";
import { nodeLabel } from "../presets.ts";
import { effectiveLines, matchPreview, type PreviewMatch } from "../previewMatch.ts";
import {
    LINE_ID_PREFIX,
    SUBAGENT_LINE_ID_PREFIX,
    VARIANT_CONTAINER_PREFIX,
    VARIANT_ID_PREFIX,
    VARIANT_LINE_ID_PREFIX,
    type DropCategory,
    type DropTarget,
} from "../useDragEditing.ts";
import type {
    LayoutChild,
    LineChild,
    LineNode,
    PreviewLine,
    PreviewResponse,
    PreviewSegment,
    PreviewSource,
    ResponsiveNode,
    SampleKind,
    SessionSummary,
    SpanNode,
    SubagentNode,
    VariantNode,
} from "../types.ts";
import { HelpTip } from "./HelpTip.tsx";

// The subagent-region samples the subagent preview toggle picks between.
type SubagentSample = "subagent-running" | "subagent-completed";

// Paint-only drop eligibility of a band for the in-flight drag: "valid" when
// the dragged content family matches the band's, "invalid" when it doesn't,
// "idle" when there is no family-restricted drag (nothing dragged, a variant
// reorder, or a structural preset that drops anywhere). Fixed for the whole
// drag, so it never feeds the #185 restructure loop.
type Eligibility = "idle" | "valid" | "invalid";

function eligibilityFor(
    dragCategory: DropCategory | null,
    accepts: "subagent" | "main",
): Eligibility {
    if (dragCategory === null || dragCategory === "any") {
        return "idle";
    }
    return dragCategory === accepts ? "valid" : "invalid";
}

function eligibilityClass(e: Eligibility): string {
    if (e === "valid") {
        return " drop-eligible";
    }
    if (e === "invalid") {
        return " drop-ineligible";
    }
    return "";
}

// Encodes/decodes the sample-selector's <option value>: "sample:<kind>" for a
// synthetic sample, "session:<id>" for a captured real session.
function sourceValue(source: PreviewSource): string {
    return source.kind === "session" ? `session:${source.id}` : `sample:${source.sample}`;
}

function parseSourceValue(value: string): PreviewSource {
    if (value.startsWith("session:")) {
        return { kind: "session", id: value.slice("session:".length) };
    }
    // The <option value>s rendered below are always one of SampleKind, so
    // this cast is a safe narrowing (unlike the free-text session id above).
    return { kind: "sample", sample: value.slice("sample:".length) as SampleKind };
}

function basename(path: string): string {
    const parts = path.split(/[\\/]/).filter((p) => p.length > 0);
    return parts.length > 0 ? parts[parts.length - 1] : path;
}

// Compact, readable label for a captured session option, e.g.
// "myrepo · claude-opus-4-6 · 42s ago [RL,git]".
function sessionLabel(s: SessionSummary): string {
    const badges: string[] = [];
    if (s.hasRateLimits) {
        badges.push("RL");
    }
    if (s.hasRepo) {
        badges.push("git");
    }
    const badgeSuffix = badges.length > 0 ? ` [${badges.join(",")}]` : "";
    return `${basename(s.cwd)} · ${s.model} · ${s.ageSeconds}s ago${badgeSuffix}`;
}

function ansiSpans(ansi: string, theme: Theme): ReactNode {
    const spans = parseAnsiLine(ansi, theme);
    if (spans.length === 0) {
        return " ";
    }
    return spans.map((s, i) => {
        if (s.text === "\ue0b0" || s.text === "\ue0b2") {
            const pointsRight = s.text === "\ue0b0";
            return (
                <span
                    key={i}
                    className="powerline-preview"
                    style={{ color: s.color, backgroundColor: s.background }}
                    role="img"
                    aria-label={pointsRight ? "Powerline separator" : "Powerline start cap"}
                >
                    <span
                        className={
                            "powerline-preview-arrow" + (pointsRight ? "" : " left")
                        }
                        style={{ backgroundColor: s.color }}
                    />
                </span>
            );
        }
        const style = {
            color: s.color,
            backgroundColor: s.background,
            fontWeight: s.bold ? 700 : 400,
        };
        // OSC 8 spans render as real links — a WebUI-only affordance the
        // terminal expresses as a clickable hyperlink.
        if (s.link) {
            return (
                <a
                    key={i}
                    className="ansi-run"
                    href={s.link}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={style}
                    onClick={(e) => e.stopPropagation()}
                >
                    {s.text}
                </a>
            );
        }
        return (
            <span key={i} className="ansi-run" style={style}>
                {s.text}
            </span>
        );
    });
}

// The segments belonging to node `id`'s subtree (its own plus descendants),
// in document order.
function segsFor(segs: readonly PreviewSegment[], id: string): PreviewSegment[] {
    return segs.filter((s) => s.nodeId === id || s.nodeId.startsWith(id + "."));
}

function visibleAnsi(segs: readonly PreviewSegment[]): string {
    return segs
        .filter((s) => s.visible)
        .map((s) => s.ansi)
        .join("");
}

// Splits a span subtree's ordered segments into the span's own leading
// decoration (padding-left/prefix), and its own trailing decoration
// (suffix/padding-right). Own segments carry exactly the span's id.
function splitOwnDeco(
    segs: readonly PreviewSegment[],
    id: string,
): { leading: PreviewSegment[]; trailing: PreviewSegment[] } {
    let firstDesc = segs.length;
    let lastDesc = -1;
    segs.forEach((s, i) => {
        if (s.nodeId !== id) {
            if (i < firstDesc) {
                firstDesc = i;
            }
            lastDesc = i;
        }
    });
    const leading = segs.filter((s, i) => s.nodeId === id && i < firstDesc);
    const trailing = segs.filter((s, i) => s.nodeId === id && i > lastDesc);
    return { leading, trailing };
}

// Paint-only drop indicator flags for the chip at `childIndex` of container
// `containerId` (whose children count is `containerLen`).
function dropFlags(
    dropTarget: DropTarget | null,
    containerId: string,
    childIndex: number,
    containerLen: number,
): { before: boolean; after: boolean } {
    if (!dropTarget || dropTarget.containerId !== containerId) {
        return { before: false, after: false };
    }
    return {
        before: dropTarget.index === childIndex,
        after: dropTarget.index >= containerLen && childIndex === containerLen - 1,
    };
}

interface ChipBodyProps {
    node: LineChild;
    // Subtree segments in document order, or null when no fresh preview data
    // exists for this line (e.g. while a request is in flight).
    segs: PreviewSegment[] | null;
    theme: Theme;
    selection: string | null;
    dropTarget: DropTarget | null;
    displayName: (field: string) => string;
    onSelect: (id: string) => void;
}

// The inside of a chip: rendered ANSI when the node has visible output, a
// ghost label when it renders nothing, a plain label without preview data.
// Spans recurse into a chip group.
function ChipBody({
    node,
    segs,
    theme,
    selection,
    dropTarget,
    displayName,
    onSelect,
}: ChipBodyProps) {
    const lang = useLang();
    if (node.kind === "span") {
        return (
            <SpanGroup
                span={node}
                segs={segs}
                theme={theme}
                selection={selection}
                dropTarget={dropTarget}
                displayName={displayName}
                onSelect={onSelect}
            />
        );
    }
    const label = nodeLabel(node, displayName);
    if (segs === null) {
        return <span className="chip-plain">{label}</span>;
    }
    const ansi = visibleAnsi(segs);
    if (ansi === "" && segsToText(segs) === "") {
        const separatorGhost = node.kind === "text" && node.role === "separator";
        return (
            <span
                className={"chip-ghost" + (separatorGhost ? " manual-separator-ghost" : "")}
                data-tip={`${label} — ${t(lang, "ghostHiddenReason")}`}
            >
                {label}
            </span>
        );
    }
    return <>{ansiSpans(ansi, theme)}</>;
}

function segsToText(segs: readonly PreviewSegment[]): string {
    return segs
        .filter((s) => s.visible)
        .map((s) => s.text)
        .join("");
}

interface InnerChipProps {
    id: string;
    node: LineChild;
    segs: PreviewSegment[] | null;
    theme: Theme;
    selection: string | null;
    dropTarget: DropTarget | null;
    parentId: string; // the owning span's AST id
    childIndex: number;
    parentLen: number;
    displayName: (field: string) => string;
    onSelect: (id: string) => void;
}

// A chip nested inside a span group: sortable within the span's own
// SortableContext, selectable, and recursing for nested spans. Pointer-down
// stops propagating so a drag grabs this chip, not the enclosing span chip.
function InnerChip({
    id,
    node,
    segs,
    theme,
    selection,
    dropTarget,
    parentId,
    childIndex,
    parentLen,
    displayName,
    onSelect,
}: InnerChipProps) {
    const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
        useSortable({ id });
    const style: CSSProperties = {
        transform: CSS.Transform.toString(transform),
        transition,
    };
    const { before, after } = dropFlags(dropTarget, parentId, childIndex, parentLen);

    let cls = "seg-chip inner";
    if (node.kind === "span") {
        cls += " has-group";
    }
    if (selection === id && node.kind !== "span") {
        cls += " selected";
    }
    if (isDragging) {
        cls += " dragging";
    }
    if (before) {
        cls += " drop-before";
    }
    if (after) {
        cls += " drop-after";
    }

    const onPointerDown = (e: ReactPointerEvent<HTMLSpanElement>) => {
        // Without this, the enclosing (line-level) sortable chip would also
        // arm its drag sensor and win the drag.
        e.stopPropagation();
        (
            listeners?.onPointerDown as
                | ((ev: ReactPointerEvent<HTMLSpanElement>) => void)
                | undefined
        )?.(e);
    };

    return (
        <span
            ref={setNodeRef}
            style={style}
            className={cls}
            data-testid={`node-${id}`}
            data-nodeid={id}
            {...attributes}
            {...listeners}
            onPointerDown={onPointerDown}
            onClick={(e) => {
                e.stopPropagation();
                onSelect(node.kind === "span" ? node.id : id);
            }}
        >
            <ChipBody
                node={node}
                segs={segs}
                theme={theme}
                selection={selection}
                dropTarget={dropTarget}
                displayName={displayName}
                onSelect={onSelect}
            />
        </span>
    );
}

interface SpanGroupProps {
    span: SpanNode;
    segs: PreviewSegment[] | null;
    theme: Theme;
    selection: string | null;
    dropTarget: DropTarget | null;
    displayName: (field: string) => string;
    onSelect: (id: string) => void;
}

// A <span> chip group: a bordered container and a drop container. The span's
// own decoration segments (prefix/suffix/padding) select the span itself;
// children render as nested sortable chips in the span's own SortableContext
// (its items are derived from the drag-frozen AST, so they never change
// mid-drag).
function SpanGroup({
    span,
    segs,
    theme,
    selection,
    dropTarget,
    displayName,
    onSelect,
}: SpanGroupProps) {
    const lang = useLang();
    const hidden = segs !== null && segs.length === 0;
    const isDropInto = dropTarget?.containerId === span.id;
    const ids = span.children.map((c, i) => (c.id !== "" ? c.id : `pending-span-${i}`));
    let cls = "span-group";
    if (selection === span.id) {
        cls += " selected";
    }
    if (hidden) {
        cls += " ghost-group";
    }
    if (isDropInto) {
        cls += " drop-into";
        if (span.children.length === 0) {
            cls += " drop-empty";
        }
    }
    const selectSpan = (e: { stopPropagation(): void }) => {
        e.stopPropagation();
        onSelect(span.id);
    };
    const deco = segs !== null ? splitOwnDeco(segs, span.id) : { leading: [], trailing: [] };
    return (
        <span
            className={cls}
            data-testid={`span-${span.id}`}
            data-tip={hidden ? `span — ${t(lang, "ghostHiddenReason")}` : "span"}
            onClick={selectSpan}
        >
            <SortableContext items={ids} strategy={horizontalListSortingStrategy}>
                {/* Always-on selection handle: a span with no decoration
                    (prefix/suffix/padding) exposes no own clickable area, so
                    this static grip guarantees the span itself is selectable.
                    It is a plain span (not a sortable item / not in `ids`), so
                    it never becomes a drag target and never mutates mid-drag. */}
                <span
                    className="span-handle"
                    data-testid={`span-handle-${span.id}`}
                    role="button"
                    aria-label="Select span"
                    onClick={selectSpan}
                >
                    ⋮
                </span>
                {deco.leading.length > 0 ? (
                    <span className="span-deco">
                        {ansiSpans(visibleAnsi(deco.leading), theme)}
                    </span>
                ) : null}
                {span.children.map((child, i) => (
                    <InnerChip
                        key={ids[i]}
                        id={ids[i]}
                        node={child}
                        segs={segs !== null ? segsFor(segs, child.id) : null}
                        theme={theme}
                        selection={selection}
                        dropTarget={dropTarget}
                        parentId={span.id}
                        childIndex={i}
                        parentLen={span.children.length}
                        displayName={displayName}
                        onSelect={onSelect}
                    />
                ))}
                {deco.trailing.length > 0 ? (
                    <span className="span-deco">
                        {ansiSpans(visibleAnsi(deco.trailing), theme)}
                    </span>
                ) : null}
                {hidden && span.children.length === 0 ? (
                    <span className="chip-ghost">span</span>
                ) : null}
            </SortableContext>
        </span>
    );
}

interface SortableChipProps {
    id: string;
    lineIndex: number;
    childIndex: number;
    // Disambiguates the `data-testid` when it would otherwise collide: a
    // variant-nested row's chips share their owning responsive's topIndex
    // with every other variant's (and any plain row's) chips at the same
    // position, so those rows pass their own line id here instead. Absent
    // for top-level rows, whose "seg-{topIndex}-{childIndex}" testid is
    // unchanged from before <responsive> existed.
    testRowKey?: string;
    node: LineChild;
    segs: PreviewSegment[] | null;
    theme: Theme;
    selection: string | null;
    dropTarget: DropTarget | null;
    containerId: string; // the owning line's AST id
    containerLen: number;
    displayName: (field: string) => string;
    onSelect: (id: string, lineIndex: number) => void;
}

// A top-level (line-child) chip: sortable, selectable.
function SortableChip({
    id,
    lineIndex,
    childIndex,
    testRowKey,
    node,
    segs,
    theme,
    selection,
    dropTarget,
    containerId,
    containerLen,
    displayName,
    onSelect,
}: SortableChipProps) {
    const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
        useSortable({ id });
    const style: CSSProperties = {
        transform: CSS.Transform.toString(transform),
        transition,
    };
    const { before, after } = dropFlags(dropTarget, containerId, childIndex, containerLen);

    let cls = "seg-chip";
    if (node.kind === "span") {
        cls += " has-group";
    }
    if (selection === id && node.kind !== "span") {
        cls += " selected";
    }
    if (isDragging) {
        cls += " dragging";
    }
    if (before) {
        cls += " drop-before";
    }
    if (after) {
        cls += " drop-after";
    }

    return (
        <span
            ref={setNodeRef}
            style={style}
            className={cls}
            data-testid={`seg-${testRowKey ?? lineIndex}-${childIndex}`}
            data-nodeid={id}
            {...attributes}
            {...listeners}
            onClick={(e) => {
                e.stopPropagation();
                // A span top-chip delegates selection to the group inside it;
                // clicking its padding still selects the span.
                onSelect(node.kind === "span" ? node.id : id, lineIndex);
            }}
        >
            <ChipBody
                node={node}
                segs={segs}
                theme={theme}
                selection={selection}
                dropTarget={dropTarget}
                displayName={displayName}
                onSelect={(nodeId) => onSelect(nodeId, lineIndex)}
            />
        </span>
    );
}

interface CanvasRowProps {
    // The useDroppable id for this row's track: LINE_ID_PREFIX + the
    // top-level position for a plain line, VARIANT_LINE_ID_PREFIX + the
    // line's own AST id for a variant-nested line (see useDragEditing.ts).
    dropId: string;
    // The numeric badge text ("1", "2", …), or null to omit it (variant-
    // nested lines aren't part of the top-level row count).
    rowLabel: string | null;
    // The layout-child position this row (or its owning responsive) lives
    // at — what onSelect/onActivateLine report as the active row, so
    // selecting or activating anything inside a responsive's variant marks
    // the whole responsive block active, exactly like a plain line.
    topIndex: number;
    line: LineNode;
    previewLine: PreviewLine | null;
    theme: Theme;
    selection: string | null;
    active: boolean;
    dropTarget: DropTarget | null;
    readOnly: boolean;
    // False disables the delete button without disabling the row (used to
    // keep a variant's last line, which validation requires).
    canDelete: boolean;
    // Hides the per-row duplicate/delete-line buttons (used by the subagent
    // band, whose single line is added/removed via the band header, not here).
    hideRowActions?: boolean;
    // Paint-only drop eligibility of this row for the current drag.
    eligibility?: Eligibility;
    displayName: (field: string) => string;
    onSelect: (id: string, topIndex: number) => void;
    onActivateLine: (topIndex: number) => void;
    onDeleteLine: (lineId: string) => void;
    onDuplicateLine: (lineId: string) => void;
}

function CanvasRow({
    dropId,
    rowLabel,
    topIndex,
    line,
    previewLine,
    theme,
    selection,
    active,
    dropTarget,
    readOnly,
    canDelete,
    hideRowActions = false,
    eligibility = "idle",
    displayName,
    onSelect,
    onActivateLine,
    onDeleteLine,
    onDuplicateLine,
}: CanvasRowProps) {
    const lang = useLang();
    const { setNodeRef, isOver } = useDroppable({ id: dropId });
    const children = line.children;
    const segments = previewLine ? previewLine.segments : null;
    const ids = children.map((c, i) => (c.id !== "" ? c.id : `pending-${line.id || topIndex}-${i}`));
    const isDropLine = dropTarget?.containerId === line.id;
    const isPowerline = previewLine?.ansi.includes("\ue0b0") === true;

    return (
        <div
            className={
                "canvas-row" +
                (active ? " active" : "") +
                (previewLine?.omitted ? " omitted" : "")
            }
            onClick={() => onActivateLine(topIndex)}
        >
            {rowLabel !== null ? <span className="row-label">{rowLabel}</span> : null}
            <SortableContext items={ids} strategy={horizontalListSortingStrategy}>
                <div
                    ref={setNodeRef}
                    className={
                        "row-track" +
                        (isPowerline ? " powerline-row" : "") +
                        (isOver ? " over" : "") +
                        (isDropLine && children.length === 0 ? " drop-empty" : "") +
                        eligibilityClass(eligibility)
                    }
                >
                    {children.length === 0 ? (
                        <span className="row-empty">{t(lang, "dropHere")}</span>
                    ) : (
                        children.map((child, j) => (
                            <SortableChip
                                key={ids[j]}
                                id={ids[j]}
                                lineIndex={topIndex}
                                childIndex={j}
                                testRowKey={rowLabel === null ? line.id : undefined}
                                node={child}
                                segs={segments ? segsFor(segments, child.id) : null}
                                theme={theme}
                                selection={selection}
                                dropTarget={dropTarget}
                                containerId={line.id}
                                containerLen={children.length}
                                displayName={displayName}
                                onSelect={onSelect}
                            />
                        ))
                    )}
                </div>
            </SortableContext>
            {previewLine?.omitted ? (
                <span className="omit-badge">{t(lang, "omittedBadge")}</span>
            ) : null}
            {hideRowActions ? null : (
                <>
                    <button
                        className="row-duplicate"
                        title="Duplicate line"
                        disabled={readOnly}
                        onClick={(e) => {
                            e.stopPropagation();
                            onDuplicateLine(line.id);
                        }}
                    >
                        ⧉
                    </button>
                    <button
                        className="row-delete"
                        title="Delete line"
                        disabled={readOnly || !canDelete}
                        onClick={(e) => {
                            e.stopPropagation();
                            onDeleteLine(line.id);
                        }}
                    >
                        ✕
                    </button>
                </>
            )}
        </div>
    );
}

interface SubagentBandProps {
    // The width-adaptive container the region belongs to: a layout id ("L{i}")
    // for a responsive-free layout, or a variant id ("L{i}.{p}.v{v}"). Used as
    // the add/delete target and the subagentPreview map key.
    containerId: string;
    // The layout-child position this band's owning row lives at, reported to
    // onSelect so selecting a subagent chip keeps that row active.
    topIndex: number;
    subagent: SubagentNode | undefined;
    // The subagent line rendered once per sample task (subagentPreview for this
    // container), or null while no preview has arrived.
    previewLines: PreviewLine[] | null;
    theme: Theme;
    // Simulated terminal width (COLUMNS): the preview rows render inside a
    // ${width}ch container so <flex/>-driven spacing (right-alignment) shows
    // at literal width, exactly like the main preview surface.
    width: number;
    selection: string | null;
    dropTarget: DropTarget | null;
    readOnly: boolean;
    // Paint-only drop eligibility of the subagent line for the current drag.
    eligibility: Eligibility;
    displayName: (field: string) => string;
    onSelect: (id: string, topIndex: number) => void;
    onAddSubagent: (containerId: string) => void;
    onDeleteSubagent: (containerId: string) => void;
    onFillSubagentDefault: (subagentLineId: string) => void;
}

// One subagent region: a labelled band holding the editable subagent <line>
// (task-* only) and, below it, that line rendered once per sample task
// (subagentPreview). When the container has no region yet, an "add" button.
// The band mirrors the real subagent statusline: it always sits BELOW its
// container's lines (Canvas renders it after the layout's lines / a variant's
// lines).
function SubagentBand({
    containerId,
    topIndex,
    subagent,
    previewLines,
    theme,
    width,
    selection,
    dropTarget,
    readOnly,
    eligibility,
    displayName,
    onSelect,
    onAddSubagent,
    onDeleteSubagent,
    onFillSubagentDefault,
}: SubagentBandProps) {
    if (!subagent) {
        return (
            <div className="subagent-band subagent-band-empty">
                <button
                    className="subagent-add"
                    data-testid={`subagent-add-${containerId}`}
                    title="Add subagent row"
                    disabled={readOnly}
                    onClick={(e) => {
                        e.stopPropagation();
                        onAddSubagent(containerId);
                    }}
                >
                    + Add subagent row
                </button>
            </div>
        );
    }
    const line = subagent.line;
    return (
        <div className="subagent-band" data-testid={`subagent-band-${containerId}`}>
            <div className="subagent-band-header">
                <span className="subagent-band-title">Subagent</span>
                {/* Always available while a region exists: (re)seed the row with
                    the built-in default fields (presets.ts's
                    makeDefaultSubagentLine). Overwriting a populated row is
                    undoable via history, so no confirmation is needed. */}
                <button
                    className="subagent-fill-default"
                    data-testid={`subagent-fill-default-${containerId}`}
                    title="Reset to default"
                    disabled={readOnly}
                    onClick={(e) => {
                        e.stopPropagation();
                        onFillSubagentDefault(line.id);
                    }}
                >
                    Reset to default
                </button>
                <button
                    className="subagent-delete"
                    data-testid={`subagent-delete-${containerId}`}
                    title="Delete subagent row"
                    disabled={readOnly}
                    onClick={(e) => {
                        e.stopPropagation();
                        onDeleteSubagent(containerId);
                    }}
                >
                    ✕
                </button>
            </div>
            <CanvasRow
                dropId={SUBAGENT_LINE_ID_PREFIX + line.id}
                rowLabel={null}
                topIndex={topIndex}
                line={line}
                // Render the editing chips against the first sample task's
                // output (all tasks share the same line, so their segments key
                // off the same child ids) so the strip matches the main line's
                // rendered chips instead of falling back to structural labels
                // (a separator would otherwise show as the JSON.stringify'd
                // "|" label rather than its actual glyph).
                previewLine={previewLines?.[0] ?? null}
                theme={theme}
                selection={selection}
                active={false}
                dropTarget={dropTarget}
                readOnly={readOnly}
                canDelete={false}
                hideRowActions
                eligibility={eligibility}
                displayName={displayName}
                onSelect={onSelect}
                onActivateLine={() => {}}
                onDeleteLine={() => {}}
                onDuplicateLine={() => {}}
            />
            {previewLines && previewLines.length > 0 ? (
                <div className="subagent-preview" data-testid={`subagent-preview-${containerId}`}>
                    {/* Render inside a ${width}ch terminal-width container, like
                        the main preview surface: the per-field segments are inline
                        (never flex items) inside a white-space:pre line, so the
                        <flex/> filler span keeps its literal width and trailing
                        fields stay right-aligned at the simulated width. */}
                    <div className="terminal-width" style={{ width: `${width}ch` }}>
                        {previewLines.map((taskLine, ti) => (
                            <div className={"subagent-preview-line " + theme} key={ti}>
                                {line.children.map((child) => {
                                    const segs = segsFor(taskLine.segments, child.id);
                                    const ansi = visibleAnsi(segs);
                                    if (ansi === "") {
                                        return null;
                                    }
                                    const selected = selection === child.id;
                                    return (
                                        <span
                                            key={child.id}
                                            className={
                                                "subagent-seg" + (selected ? " selected" : "")
                                            }
                                        >
                                            {ansiSpans(ansi, theme)}
                                        </span>
                                    );
                                })}
                            </div>
                        ))}
                    </div>
                </div>
            ) : null}
        </div>
    );
}

interface VariantCardProps {
    // The draggable id (VARIANT_ID_PREFIX + variant.id).
    dragId: string;
    variant: VariantNode;
    variantIndex: number;
    variantCount: number;
    responsiveId: string;
    // Whether the preview response currently selected this variant at the
    // simulated width — the feature's whole point made visible.
    selected: boolean;
    topIndex: number;
    match: PreviewMatch;
    theme: Theme;
    // Simulated terminal width, forwarded to the variant's subagent band.
    width: number;
    selection: string | null;
    dropTarget: DropTarget | null;
    readOnly: boolean;
    canDeleteVariant: boolean;
    // The in-flight drag's content family, for per-band drop eligibility.
    dragCategory: DropCategory | null;
    // Each container's subagent line rendered per sample task, keyed by
    // container node id (this variant's id).
    subagentPreview: Record<string, PreviewLine[]> | null;
    displayName: (field: string) => string;
    onSelect: (id: string, topIndex: number) => void;
    onActivateLine: (topIndex: number) => void;
    onDeleteLine: (lineId: string) => void;
    onDuplicateLine: (lineId: string) => void;
    onAddLine: () => void;
    onDeleteVariant: () => void;
    onDuplicate: () => void;
    onAddSubagent: (containerId: string) => void;
    onDeleteSubagent: (containerId: string) => void;
    onFillSubagentDefault: (subagentLineId: string) => void;
}

// One <variant> candidate: a draggable, sortable card (reordering variants
// changes their width priority — widest first) holding its own stack of
// lines. `selected` mirrors the preview response's width-selected variant
// for the enclosing <responsive>.
function VariantCard({
    dragId,
    variant,
    variantIndex,
    variantCount,
    responsiveId,
    selected,
    topIndex,
    match,
    theme,
    width,
    selection,
    dropTarget,
    readOnly,
    canDeleteVariant,
    dragCategory,
    subagentPreview,
    displayName,
    onSelect,
    onActivateLine,
    onDeleteLine,
    onDuplicateLine,
    onAddLine,
    onDeleteVariant,
    onDuplicate,
    onAddSubagent,
    onDeleteSubagent,
    onFillSubagentDefault,
}: VariantCardProps) {
    const lang = useLang();
    const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
        id: dragId,
    });
    const style: CSSProperties = { transform: CSS.Transform.toString(transform), transition };
    const { before, after } = dropFlags(
        dropTarget,
        VARIANT_CONTAINER_PREFIX + responsiveId,
        variantIndex,
        variantCount,
    );
    let cls = "variant-card";
    if (selected) {
        cls += " variant-selected";
    }
    if (isDragging) {
        cls += " dragging";
    }
    if (before) {
        cls += " drop-before";
    }
    if (after) {
        cls += " drop-after";
    }
    return (
        <div ref={setNodeRef} style={style} className={cls} data-testid={`variant-${variant.id}`}>
            <div className="variant-header">
                <span
                    className="variant-drag"
                    data-testid={`variant-handle-${variant.id}`}
                    {...attributes}
                    {...listeners}
                >
                    ⋮
                </span>
                <span className="variant-title">
                    {t(lang, "variantLabel")} {variantIndex + 1}
                    {selected ? (
                        <span className="variant-selected-badge">
                            {t(lang, "variantSelectedBadge")}
                        </span>
                    ) : null}
                </span>
                <button
                    className="variant-duplicate"
                    data-testid={`variant-duplicate-${variant.id}`}
                    title="Duplicate variant"
                    disabled={readOnly}
                    onClick={onDuplicate}
                >
                    ⧉
                </button>
                <button
                    className="variant-delete"
                    data-testid={`variant-delete-${variant.id}`}
                    title="Delete variant"
                    disabled={readOnly || !canDeleteVariant}
                    onClick={onDeleteVariant}
                >
                    ✕
                </button>
            </div>
            <div className="variant-lines">
                {variant.lines.map((line, j) => (
                    <CanvasRow
                        key={line.id !== "" ? line.id : `vline-${variant.id}-${j}`}
                        dropId={VARIANT_LINE_ID_PREFIX + line.id}
                        rowLabel={null}
                        topIndex={topIndex}
                        line={line}
                        previewLine={match.byLineId.get(line.id) ?? null}
                        theme={theme}
                        selection={selection}
                        active={false}
                        dropTarget={dropTarget}
                        readOnly={readOnly}
                        canDelete={variant.lines.length > 1}
                        eligibility={eligibilityFor(dragCategory, "main")}
                        displayName={displayName}
                        onSelect={onSelect}
                        onActivateLine={onActivateLine}
                        onDeleteLine={onDeleteLine}
                        onDuplicateLine={onDuplicateLine}
                    />
                ))}
            </div>
            <button
                className="variant-add-line"
                data-testid={`variant-add-line-${variant.id}`}
                title="Add line to this variant"
                disabled={readOnly}
                onClick={onAddLine}
            >
                + Line
            </button>
            <SubagentBand
                containerId={variant.id}
                topIndex={topIndex}
                subagent={variant.subagent}
                previewLines={subagentPreview?.[variant.id] ?? null}
                theme={theme}
                width={width}
                selection={selection}
                dropTarget={dropTarget}
                readOnly={readOnly}
                eligibility={eligibilityFor(dragCategory, "subagent")}
                displayName={displayName}
                onSelect={onSelect}
                onAddSubagent={onAddSubagent}
                onDeleteSubagent={onDeleteSubagent}
                onFillSubagentDefault={onFillSubagentDefault}
            />
        </div>
    );
}

interface ResponsiveBlockProps {
    responsive: ResponsiveNode;
    topIndex: number;
    match: PreviewMatch;
    theme: Theme;
    // Simulated terminal width, forwarded to each variant's subagent band.
    width: number;
    selection: string | null;
    active: boolean;
    dropTarget: DropTarget | null;
    readOnly: boolean;
    dragCategory: DropCategory | null;
    subagentPreview: Record<string, PreviewLine[]> | null;
    displayName: (field: string) => string;
    onSelect: (id: string, topIndex: number) => void;
    onActivateLine: (topIndex: number) => void;
    onDeleteLine: (lineId: string) => void;
    onDuplicateLine: (lineId: string) => void;
    onAddLineToVariant: (variantId: string) => void;
    onAddVariant: (responsiveId: string) => void;
    onDeleteVariant: (variantId: string) => void;
    onDuplicateVariant: (variantId: string) => void;
    onAddSubagent: (containerId: string) => void;
    onDeleteSubagent: (containerId: string) => void;
    onFillSubagentDefault: (subagentLineId: string) => void;
}

// A <responsive> row: a horizontal, reorderable track of <variant> cards
// (widest first). The variant the current width selected (per the preview
// response) is highlighted — that visibility is the feature's whole point.
function ResponsiveBlock({
    responsive,
    topIndex,
    match,
    theme,
    width,
    selection,
    active,
    dropTarget,
    readOnly,
    dragCategory,
    subagentPreview,
    displayName,
    onSelect,
    onActivateLine,
    onDeleteLine,
    onDuplicateLine,
    onAddLineToVariant,
    onAddVariant,
    onDeleteVariant,
    onDuplicateVariant,
    onAddSubagent,
    onDeleteSubagent,
    onFillSubagentDefault,
}: ResponsiveBlockProps) {
    const selectedVariant = match.selectedVariant.get(responsive.id) ?? null;
    const dragIds = responsive.variants.map((v, i) =>
        VARIANT_ID_PREFIX + (v.id !== "" ? v.id : `pending-variant-${topIndex}-${i}`),
    );

    return (
        <div
            className={"canvas-row responsive-row" + (active ? " active" : "")}
            onClick={() => onActivateLine(topIndex)}
        >
            <span className="row-label">{topIndex + 1}</span>
            <div className="responsive-block">
                <SortableContext items={dragIds} strategy={verticalListSortingStrategy}>
                    <div className="variant-track">
                        {responsive.variants.map((variant, v) => (
                            <VariantCard
                                key={dragIds[v]}
                                dragId={dragIds[v]}
                                variant={variant}
                                variantIndex={v}
                                variantCount={responsive.variants.length}
                                responsiveId={responsive.id}
                                selected={selectedVariant === v}
                                topIndex={topIndex}
                                match={match}
                                theme={theme}
                                width={width}
                                selection={selection}
                                dropTarget={dropTarget}
                                readOnly={readOnly}
                                canDeleteVariant={responsive.variants.length > 1}
                                dragCategory={dragCategory}
                                subagentPreview={subagentPreview}
                                displayName={displayName}
                                onSelect={onSelect}
                                onActivateLine={onActivateLine}
                                onDeleteLine={onDeleteLine}
                                onDuplicateLine={onDuplicateLine}
                                onAddLine={() => onAddLineToVariant(variant.id)}
                                onDeleteVariant={() => onDeleteVariant(variant.id)}
                                onDuplicate={() => onDuplicateVariant(variant.id)}
                                onAddSubagent={onAddSubagent}
                                onDeleteSubagent={onDeleteSubagent}
                                onFillSubagentDefault={onFillSubagentDefault}
                            />
                        ))}
                    </div>
                </SortableContext>
                <button
                    className="variant-add"
                    data-testid={`responsive-add-variant-${responsive.id}`}
                    title="Add variant"
                    disabled={readOnly}
                    onClick={(e) => {
                        e.stopPropagation();
                        onAddVariant(responsive.id);
                    }}
                >
                    + Variant
                </button>
            </div>
        </div>
    );
}

interface CanvasProps {
    children: LayoutChild[];
    // The edited layout's own id ("L{i}") and its layout-level <subagent>
    // region (used only for a responsive-free layout: its band renders below
    // all the layout's lines, keyed by the layout id).
    layoutId: string;
    layoutSubagent: SubagentNode | undefined;
    previewLines: PreviewLine[] | null;
    // Responsive AST id -> the variant index the preview response selected
    // at the current width (see DSL_API.md "allVariants"). Null while no
    // preview response with this data has arrived yet.
    selectedVariants: Record<string, number> | null;
    // Each width-adaptive container's <subagent> line rendered once per sample
    // task, keyed by container node id (a responsive-free layout's id, or each
    // variant's id). Null while no subagent preview has arrived.
    subagentPreview: Record<string, PreviewLine[]> | null;
    fallback: PreviewResponse["fallback"] | null;
    selection: string | null;
    activeLine: number;
    dropTarget: DropTarget | null;
    // The in-flight chip drag's content family, for drop-eligibility painting.
    dragCategory: DropCategory | null;
    theme: Theme;
    width: number;
    previewSource: PreviewSource;
    // Which subagent-region sample the subagent preview rows use.
    subagentSample: SubagentSample;
    sessions: SessionSummary[];
    pureOutput: boolean;
    loading: boolean;
    error: string | null;
    // True while the DSL source is invalid: the canvas shows the last valid
    // AST but must not edit it.
    readOnly: boolean;
    displayName: (field: string) => string;
    onSelect: (id: string, lineIndex: number) => void;
    onDeselect: () => void;
    onActivateLine: (lineIndex: number) => void;
    onAddLine: () => void;
    onAddResponsive: () => void;
    onDeleteLine: (lineId: string) => void;
    onDuplicateLine: (lineId: string) => void;
    onAddLineToVariant: (variantId: string) => void;
    onAddVariant: (responsiveId: string) => void;
    onDeleteVariant: (variantId: string) => void;
    onDuplicateVariant: (variantId: string) => void;
    onAddSubagent: (containerId: string) => void;
    onDeleteSubagent: (containerId: string) => void;
    onFillSubagentDefault: (subagentLineId: string) => void;
    onWidth: (w: number) => void;
    onPreviewSourceChange: (source: PreviewSource) => void;
    onSubagentSampleChange: (sample: SubagentSample) => void;
    onRefreshSessions: () => void;
    onTheme: (t: Theme) => void;
    onPureOutput: (v: boolean) => void;
}

export function Canvas({
    children,
    layoutId,
    layoutSubagent,
    previewLines,
    selectedVariants,
    subagentPreview,
    fallback,
    selection,
    activeLine,
    dropTarget,
    dragCategory,
    theme,
    width,
    previewSource,
    subagentSample,
    sessions,
    pureOutput,
    loading,
    error,
    readOnly,
    displayName,
    onSelect,
    onDeselect,
    onActivateLine,
    onAddLine,
    onAddResponsive,
    onDeleteLine,
    onDuplicateLine,
    onAddLineToVariant,
    onAddVariant,
    onDeleteVariant,
    onDuplicateVariant,
    onAddSubagent,
    onDeleteSubagent,
    onFillSubagentDefault,
    onWidth,
    onPreviewSourceChange,
    onSubagentSampleChange,
    onRefreshSessions,
    onTheme,
    onPureOutput,
}: CanvasProps) {
    const lang = useLang();
    // Segments are matched onto lines by AST id (not position): a
    // responsive-free child's PreviewLine always matches by its own id, and
    // a responsive's variant-nested lines match only the ones its
    // width-selected variant actually rendered.
    const match = matchPreview(previewLines, selectedVariants);
    // In pure output the real terminal stacks the subagent statusline BELOW
    // the main lines. Resolve the width-selected container's subagent rows the
    // same way the backend does: the width-selected variant of a responsive,
    // else the layout-level region.
    const pureSubagentLines: PreviewLine[] | null = (() => {
        if (!subagentPreview) {
            return null;
        }
        for (const child of children) {
            if (child.kind === "responsive") {
                const v = selectedVariants?.[child.id];
                if (v != null) {
                    const variant = child.variants[v];
                    if (variant && subagentPreview[variant.id]) {
                        return subagentPreview[variant.id];
                    }
                }
            }
        }
        return subagentPreview[layoutId] ?? null;
    })();
    return (
        <div className="panel canvas-panel">
            <h2>
                Preview
                {loading ? <span className="canvas-render-hint">{t(lang, "rendering")}</span> : null}
            </h2>

            <div className="canvas-controls">
                <div className="slider-row">
                    <label>
                        COLUMNS <HelpTip k="helpWidth" />
                    </label>
                    <input
                        type="range"
                        data-testid="width-slider"
                        min={40}
                        max={200}
                        value={width}
                        onChange={(e) => onWidth(Number(e.target.value))}
                    />
                    <span className="cols-value">{width}</span>
                </div>
                <label>
                    Sample <HelpTip k="helpSample" />{" "}
                    <select
                        data-testid="preview-source-select"
                        value={sourceValue(previewSource)}
                        onChange={(e) => onPreviewSourceChange(parseSourceValue(e.target.value))}
                    >
                        <option value="sample:full">{t(lang, "sampleFull")}</option>
                        <option value="sample:early-session">{t(lang, "sampleEarly")}</option>
                        {sessions.length > 0 ? (
                            <optgroup label="Live sessions">
                                {sessions.map((s) => (
                                    <option key={s.id} value={`session:${s.id}`}>
                                        {sessionLabel(s)}
                                    </option>
                                ))}
                            </optgroup>
                        ) : null}
                    </select>
                    <button
                        type="button"
                        className="refresh-sessions"
                        title="Refresh live sessions"
                        onClick={onRefreshSessions}
                    >
                        ⟳
                    </button>
                </label>
                <label>
                    Subagent{" "}
                    <select
                        data-testid="subagent-sample-select"
                        value={subagentSample}
                        onChange={(e) =>
                            onSubagentSampleChange(e.target.value as SubagentSample)
                        }
                    >
                        <option value="subagent-running">
                            {t(lang, "sampleSubagentRunning")}
                        </option>
                        <option value="subagent-completed">
                            {t(lang, "sampleSubagentCompleted")}
                        </option>
                    </select>
                </label>
                <label>
                    Background{" "}
                    <select
                        value={theme}
                        onChange={(e) => onTheme(e.target.value as Theme)}
                    >
                        <option value="dark">dark</option>
                        <option value="light">light</option>
                    </select>
                </label>
                <label className="check-label">
                    <input
                        type="checkbox"
                        checked={pureOutput}
                        onChange={(e) => onPureOutput(e.target.checked)}
                    />{" "}
                    Pure output
                </label>
            </div>

            {readOnly ? (
                <div className="banner warn" data-testid="canvas-readonly">
                    {t(lang, "dslInvalid")}
                </div>
            ) : null}

            <div
                className={"preview-surface " + theme + (readOnly ? " readonly" : "")}
                onClick={(e) => {
                    if (e.target === e.currentTarget) {
                        onDeselect();
                    }
                }}
            >
                <div className="terminal-width" style={{ width: `${width}ch` }}>
                    {pureOutput ? (
                        previewLines ? (
                            <pre className="pure-pre">
                                {effectiveLines(previewLines, selectedVariants)
                                    .filter((l) => !l.omitted)
                                    .map((l, i) => (
                                        <div key={i}>{ansiSpans(l.ansi, theme)}</div>
                                    ))}
                                {/* The subagent statusline stacks below the main
                                    lines, one row per running/completed task. */}
                                {pureSubagentLines
                                    ? pureSubagentLines
                                          .filter((l) => !l.omitted)
                                          .map((l, i) => (
                                              <div key={`sa-${i}`}>
                                                  {ansiSpans(l.ansi, theme)}
                                              </div>
                                          ))
                                    : null}
                            </pre>
                        ) : (
                            <span className="hint">{t(lang, "noPreview")}</span>
                        )
                    ) : (
                        <>
                            {children.map((child, p) =>
                                child.kind === "line" ? (
                                    <CanvasRow
                                        key={child.id !== "" ? child.id : `line-${p}`}
                                        dropId={LINE_ID_PREFIX + p}
                                        rowLabel={String(p + 1)}
                                        topIndex={p}
                                        line={child}
                                        previewLine={match.byLineId.get(child.id) ?? null}
                                        theme={theme}
                                        selection={selection}
                                        active={p === activeLine}
                                        dropTarget={dropTarget}
                                        readOnly={readOnly}
                                        canDelete
                                        eligibility={eligibilityFor(dragCategory, "main")}
                                        displayName={displayName}
                                        onSelect={onSelect}
                                        onActivateLine={onActivateLine}
                                        onDeleteLine={onDeleteLine}
                                        onDuplicateLine={onDuplicateLine}
                                    />
                                ) : (
                                    <ResponsiveBlock
                                        key={child.id !== "" ? child.id : `responsive-${p}`}
                                        responsive={child}
                                        topIndex={p}
                                        match={match}
                                        theme={theme}
                                        width={width}
                                        selection={selection}
                                        active={p === activeLine}
                                        dropTarget={dropTarget}
                                        readOnly={readOnly}
                                        dragCategory={dragCategory}
                                        subagentPreview={subagentPreview}
                                        displayName={displayName}
                                        onSelect={onSelect}
                                        onActivateLine={onActivateLine}
                                        onDeleteLine={onDeleteLine}
                                        onDuplicateLine={onDuplicateLine}
                                        onAddLineToVariant={onAddLineToVariant}
                                        onAddVariant={onAddVariant}
                                        onDeleteVariant={onDeleteVariant}
                                        onDuplicateVariant={onDuplicateVariant}
                                        onAddSubagent={onAddSubagent}
                                        onDeleteSubagent={onDeleteSubagent}
                                        onFillSubagentDefault={onFillSubagentDefault}
                                    />
                                ),
                            )}
                            {/* A responsive-free layout carries its <subagent>
                                region at the layout level, rendered below all
                                its lines. A layout that has any <responsive>
                                puts subagent bands inside each variant instead
                                (matching the width-selected container). */}
                            {children.some((c) => c.kind === "responsive") ? null : (
                                <SubagentBand
                                    containerId={layoutId}
                                    topIndex={activeLine}
                                    subagent={layoutSubagent}
                                    previewLines={subagentPreview?.[layoutId] ?? null}
                                    theme={theme}
                                    width={width}
                                    selection={selection}
                                    dropTarget={dropTarget}
                                    readOnly={readOnly}
                                    eligibility={eligibilityFor(dragCategory, "subagent")}
                                    displayName={displayName}
                                    onSelect={onSelect}
                                    onAddSubagent={onAddSubagent}
                                    onDeleteSubagent={onDeleteSubagent}
                                    onFillSubagentDefault={onFillSubagentDefault}
                                />
                            )}
                        </>
                    )}
                </div>
            </div>

            {fallback?.active ? (
                <div className="fallback-note">
                    <span className="hint">{t(lang, "fallbackNote")}</span>
                    <div className={"preview-surface " + theme}>
                        <div className="terminal-width" style={{ width: `${width}ch` }}>
                            <pre className="pure-pre">
                                <div>{ansiSpans(fallback.ansi, theme)}</div>
                            </pre>
                        </div>
                    </div>
                </div>
            ) : null}

            {error ? <div className="inline-error">{error}</div> : null}

            {pureOutput ? null : (
                <div className="canvas-footer">
                    <button onClick={onAddLine} disabled={readOnly}>
                        + Add line
                    </button>
                    <button onClick={onAddResponsive} disabled={readOnly}>
                        + Add responsive
                    </button>
                    <HelpTip k="helpResponsive" />
                    <span className="hint">{t(lang, "canvasFooterHint")}</span>
                </div>
            )}
        </div>
    );
}
