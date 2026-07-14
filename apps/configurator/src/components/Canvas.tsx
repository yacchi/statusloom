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
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { parseAnsiLine, type Theme } from "../ansi.ts";
import { t, useLang } from "../i18n.ts";
import { nodeLabel } from "../presets.ts";
import { LINE_ID_PREFIX, type DropTarget } from "../useDragEditing.ts";
import type {
    LineChild,
    LineNode,
    PreviewLine,
    PreviewResponse,
    PreviewSegment,
    PreviewSource,
    SessionSummary,
    SpanNode,
} from "../types.ts";
import { HelpTip } from "./HelpTip.tsx";

// Encodes/decodes the sample-selector's <option value>: "sample:<kind>" for a
// synthetic sample, "session:<id>" for a captured real session.
function sourceValue(source: PreviewSource): string {
    return source.kind === "session" ? `session:${source.id}` : `sample:${source.sample}`;
}

function parseSourceValue(value: string): PreviewSource {
    if (value.startsWith("session:")) {
        return { kind: "session", id: value.slice("session:".length) };
    }
    const sample = value.slice("sample:".length);
    return { kind: "sample", sample: sample === "early-session" ? "early-session" : "full" };
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
            data-testid={`seg-${lineIndex}-${childIndex}`}
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
    lineIndex: number;
    line: LineNode;
    previewLine: PreviewLine | null;
    theme: Theme;
    selection: string | null;
    active: boolean;
    dropTarget: DropTarget | null;
    readOnly: boolean;
    displayName: (field: string) => string;
    onSelect: (id: string, lineIndex: number) => void;
    onActivateLine: (lineIndex: number) => void;
    onDeleteLine: (lineIndex: number) => void;
}

function CanvasRow({
    lineIndex,
    line,
    previewLine,
    theme,
    selection,
    active,
    dropTarget,
    readOnly,
    displayName,
    onSelect,
    onActivateLine,
    onDeleteLine,
}: CanvasRowProps) {
    const lang = useLang();
    const { setNodeRef, isOver } = useDroppable({ id: LINE_ID_PREFIX + lineIndex });
    const children = line.children;
    const segments = previewLine ? previewLine.segments : null;
    const ids = children.map((c, i) => (c.id !== "" ? c.id : `pending-${lineIndex}-${i}`));
    const isDropLine = dropTarget?.containerId === line.id;
    const isPowerline = previewLine?.ansi.includes("\ue0b0") === true;

    return (
        <div
            className={
                "canvas-row" +
                (active ? " active" : "") +
                (previewLine?.omitted ? " omitted" : "")
            }
            onClick={() => onActivateLine(lineIndex)}
        >
            <span className="row-label">{lineIndex + 1}</span>
            <SortableContext items={ids} strategy={horizontalListSortingStrategy}>
                <div
                    ref={setNodeRef}
                    className={
                        "row-track" +
                        (isPowerline ? " powerline-row" : "") +
                        (isOver ? " over" : "") +
                        (isDropLine && children.length === 0 ? " drop-empty" : "")
                    }
                >
                    {children.length === 0 ? (
                        <span className="row-empty">{t(lang, "dropHere")}</span>
                    ) : (
                        children.map((child, j) => (
                            <SortableChip
                                key={ids[j]}
                                id={ids[j]}
                                lineIndex={lineIndex}
                                childIndex={j}
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
            <button
                className="row-delete"
                title="Delete line"
                disabled={readOnly}
                onClick={(e) => {
                    e.stopPropagation();
                    onDeleteLine(lineIndex);
                }}
            >
                ✕
            </button>
        </div>
    );
}

interface CanvasProps {
    lines: LineNode[];
    previewLines: PreviewLine[] | null;
    fallback: PreviewResponse["fallback"] | null;
    selection: string | null;
    activeLine: number;
    dropTarget: DropTarget | null;
    theme: Theme;
    width: number;
    previewSource: PreviewSource;
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
    onDeleteLine: (lineIndex: number) => void;
    onWidth: (w: number) => void;
    onPreviewSourceChange: (source: PreviewSource) => void;
    onRefreshSessions: () => void;
    onTheme: (t: Theme) => void;
    onPureOutput: (v: boolean) => void;
}

export function Canvas({
    lines,
    previewLines,
    fallback,
    selection,
    activeLine,
    dropTarget,
    theme,
    width,
    previewSource,
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
    onDeleteLine,
    onWidth,
    onPreviewSourceChange,
    onRefreshSessions,
    onTheme,
    onPureOutput,
}: CanvasProps) {
    const lang = useLang();
    // Segments are only trustworthy while the preview still matches the AST's
    // line structure.
    const previewUsable = previewLines !== null && previewLines.length === lines.length;
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
                                {previewLines
                                    .filter((l) => !l.omitted)
                                    .map((l, i) => (
                                        <div key={i}>{ansiSpans(l.ansi, theme)}</div>
                                    ))}
                            </pre>
                        ) : (
                            <span className="hint">{t(lang, "noPreview")}</span>
                        )
                    ) : (
                        lines.map((line, i) => (
                            <CanvasRow
                                key={line.id !== "" ? line.id : `line-${i}`}
                                lineIndex={i}
                                line={line}
                                previewLine={previewUsable ? previewLines[i] : null}
                                theme={theme}
                                selection={selection}
                                active={i === activeLine}
                                dropTarget={dropTarget}
                                readOnly={readOnly}
                                displayName={displayName}
                                onSelect={onSelect}
                                onActivateLine={onActivateLine}
                                onDeleteLine={onDeleteLine}
                            />
                        ))
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
                    <span className="hint">{t(lang, "canvasFooterHint")}</span>
                </div>
            )}
        </div>
    );
}
