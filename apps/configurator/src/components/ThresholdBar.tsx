// Direct-manipulation editor for a percent field's color-by-threshold rules.
// The 0..100 bar is split into color bands by draggable breakpoints:
//   - click the bar        -> add a breakpoint (splits the band, opens picker)
//   - drag a handle sideways-> move that breakpoint
//   - drag a handle out of  -> remove that breakpoint
//     the bar (up/down)
//   - click a band's swatch -> recolor that band (base band = the node's color)
//
// React #185 discipline (see useDragEditing.ts): dragging NEVER mutates the
// document. The live handle position lives in local `drag` state during the
// gesture and is committed to the AST (via onChange) exactly once, on pointer
// up. Selection is shown with an outline only — nothing changes width when
// selected, so the layout never shifts.

import { useRef, useState } from "react";
import { paletteFor, type Theme } from "../ansi.ts";
import { t, type Lang } from "../i18n.ts";
import {
    addBreakpoint,
    type Band,
    MIN_GAP,
    moveBreakpoint,
    removeBreakpoint,
    setBandColor,
} from "../thresholds.ts";
import { ColorPicker } from "./ColorPicker.tsx";

// Vertical pointer travel (px) past the bar edges beyond which releasing a
// handle deletes its breakpoint instead of moving it.
const REMOVE_DISTANCE = 40;

interface Props {
    bands: Band[];
    theme: Theme;
    lang: Lang;
    onChange: (bands: Band[]) => void;
}

interface DragState {
    index: number; // band index of the dragged breakpoint (>= 1)
    value: number; // live position on the 0..100 scale (unrounded)
    remove: boolean; // pointer has left the bar -> release will delete
}

// Resolve a DSL color (kebab-case ANSI name or #hex) to a CSS color for the
// swatch/segment fill; unknown or unset -> null (rendered as an "inherit" cell).
function cssColor(color: string | undefined, theme: Theme): string | null {
    if (!color) {
        return null;
    }
    if (color.startsWith("#")) {
        return color;
    }
    const camel = color.replace(/-([a-z])/g, (_, c: string) => c.toUpperCase());
    const palette = paletteFor(theme) as Record<string, string>;
    return palette[camel] ?? null;
}

function bandLabel(bands: Band[], i: number): string {
    const from = Math.round(bands[i].from);
    const to = bands[i + 1] ? Math.round(bands[i + 1].from) : 100;
    return `${from}–${to}%`;
}

export function ThresholdBar({ bands, theme, lang, onChange }: Props) {
    const trackRef = useRef<HTMLDivElement>(null);
    const [drag, setDrag] = useState<DragState | null>(null);
    // Which band's color picker is open (index into bands), or null.
    const [picker, setPicker] = useState<number | null>(null);

    // Bands as currently displayed: during a drag the dragged breakpoint
    // follows the pointer, but the AST (the `bands` prop) is untouched.
    const view = drag
        ? bands.map((b, i) => (i === drag.index ? { ...b, from: drag.value } : b))
        : bands;

    const pctFromClientX = (clientX: number): number => {
        const rect = trackRef.current?.getBoundingClientRect();
        if (!rect || rect.width === 0) {
            return 0;
        }
        return ((clientX - rect.left) / rect.width) * 100;
    };

    const onHandleDown = (e: React.PointerEvent, index: number) => {
        e.preventDefault();
        e.stopPropagation();
        (e.target as Element).setPointerCapture?.(e.pointerId);
        setPicker(null);
        setDrag({ index, value: bands[index].from, remove: false });
    };

    const onHandleMove = (e: React.PointerEvent) => {
        if (!drag) {
            return;
        }
        const rect = trackRef.current?.getBoundingClientRect();
        if (!rect) {
            return;
        }
        const lo = bands[drag.index - 1].from + MIN_GAP;
        const hi = (bands[drag.index + 1]?.from ?? 100) - MIN_GAP;
        const value = Math.min(Math.max(pctFromClientX(e.clientX), lo), hi);
        const remove =
            e.clientY < rect.top - REMOVE_DISTANCE || e.clientY > rect.bottom + REMOVE_DISTANCE;
        setDrag((d) => (d ? { ...d, value, remove } : d));
    };

    const onHandleUp = (e: React.PointerEvent) => {
        if (!drag) {
            return;
        }
        (e.target as Element).releasePointerCapture?.(e.pointerId);
        const committed = drag;
        setDrag(null);
        onChange(
            committed.remove
                ? removeBreakpoint(bands, committed.index)
                : moveBreakpoint(bands, committed.index, committed.value),
        );
    };

    // Click on the bar background (not a handle / swatch): add a breakpoint.
    const onTrackClick = (e: React.MouseEvent) => {
        if (drag) {
            return;
        }
        const { bands: next, index } = addBreakpoint(bands, pctFromClientX(e.clientX));
        if (index >= 0) {
            onChange(next);
            setPicker(index);
        }
    };

    return (
        <div className="threshold-editor" data-testid="threshold-bar">
            <div
                className="threshold-track"
                ref={trackRef}
                onClick={onTrackClick}
                role="presentation"
            >
                {view.map((b, i) => {
                    const left = b.from;
                    const right = view[i + 1]?.from ?? 100;
                    const fill = cssColor(b.color, theme);
                    return (
                        <div
                            key={i}
                            className={"threshold-band" + (fill ? "" : " inherit")}
                            style={{
                                left: `${left}%`,
                                width: `${right - left}%`,
                                background: fill ?? undefined,
                            }}
                            data-testid={`threshold-band-${i}`}
                        >
                            <button
                                type="button"
                                className={
                                    "threshold-swatch" + (picker === i ? " selected" : "")
                                }
                                style={{ background: fill ?? undefined }}
                                title={bandLabel(view, i)}
                                data-testid={`threshold-swatch-${i}`}
                                onClick={(e) => {
                                    e.stopPropagation();
                                    setPicker(picker === i ? null : i);
                                }}
                            />
                        </div>
                    );
                })}
                {view.map((b, i) =>
                    i === 0 ? null : (
                        <div
                            key={`h${i}`}
                            className={
                                "threshold-handle" +
                                (drag?.index === i && drag.remove ? " removing" : "")
                            }
                            style={{ left: `${b.from}%` }}
                            data-testid={`threshold-handle-${i}`}
                            onPointerDown={(e) => onHandleDown(e, i)}
                            onPointerMove={onHandleMove}
                            onPointerUp={onHandleUp}
                            onClick={(e) => e.stopPropagation()}
                        >
                            <span className="threshold-handle-label">{Math.round(b.from)}</span>
                        </div>
                    ),
                )}
            </div>

            {picker !== null && view[picker] ? (
                <div className="threshold-picker" data-testid="threshold-picker">
                    <div className="threshold-picker-head">
                        <span className="threshold-picker-title">
                            {picker === 0
                                ? t(lang, "thresholdBaseBand")
                                : bandLabel(view, picker)}
                        </span>
                        <button
                            type="button"
                            className="link-button"
                            data-testid="threshold-picker-done"
                            onClick={() => setPicker(null)}
                        >
                            Done
                        </button>
                    </div>
                    <ColorPicker
                        color={view[picker].color}
                        previewTheme={theme}
                        onChange={(color) =>
                            onChange(setBandColor(bands, picker, color === "" ? undefined : color))
                        }
                    />
                </div>
            ) : null}

            <p className="hint">{t(lang, "thresholdBarHint")}</p>
        </div>
    );
}
