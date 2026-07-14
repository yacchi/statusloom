// Layout selector strip over the AST's <layout> elements. Two orthogonal
// concepts are surfaced here:
//   * the EDIT target — the layout currently shown in the canvas (highlighted
//     tab border/background). Click a tab body to switch.
//   * the ACTIVE layout — the one the real status line renders (marked with a
//     "● active" badge; exactly one layout is active). Click a non-active
//     tab's badge to make it active.
// Rename is inline via double-click. No dropdowns, no move buttons, and the
// tab geometry never changes with selection (per the UI constraints).

import { useEffect, useRef, useState } from "react";
import { t, useLang } from "../i18n.ts";
import type { LayoutNode } from "../types.ts";

interface Props {
    layouts: LayoutNode[];
    editIndex: number;
    activeIndex: number;
    readOnly: boolean;
    onSelectEdit: (index: number) => void;
    onSetActive: (index: number) => void;
    onAdd: () => void;
    onDuplicate: (index: number) => void;
    onDelete: (index: number) => void;
    onRename: (index: number, name: string) => void;
}

export function LayoutTabs({
    layouts,
    editIndex,
    activeIndex,
    readOnly,
    onSelectEdit,
    onSetActive,
    onAdd,
    onDuplicate,
    onDelete,
    onRename,
}: Props) {
    const lang = useLang();
    const [renaming, setRenaming] = useState<number | null>(null);
    const [draft, setDraft] = useState("");
    const inputRef = useRef<HTMLInputElement>(null);

    useEffect(() => {
        if (renaming !== null) {
            inputRef.current?.focus();
            inputRef.current?.select();
        }
    }, [renaming]);

    const startRename = (index: number) => {
        if (readOnly) {
            return;
        }
        setRenaming(index);
        setDraft(layouts[index]?.name ?? "");
    };

    const commitRename = () => {
        if (renaming === null) {
            return;
        }
        const name = draft.trim();
        if (name.length > 0 && name !== layouts[renaming]?.name) {
            onRename(renaming, name);
        }
        setRenaming(null);
    };

    return (
        <div className="layout-tabs" role="tablist">
            {layouts.map((layout, i) => {
                const isEditing = i === editIndex;
                const isActive = i === activeIndex;
                return (
                    <div
                        key={i}
                        className={"layout-tab" + (isEditing ? " editing" : "")}
                        role="tab"
                        aria-selected={isEditing}
                    >
                        {renaming === i ? (
                            <input
                                ref={inputRef}
                                className="layout-tab-input"
                                value={draft}
                                onChange={(e) => setDraft(e.target.value)}
                                onBlur={commitRename}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") {
                                        e.preventDefault();
                                        commitRename();
                                    } else if (e.key === "Escape") {
                                        e.preventDefault();
                                        setRenaming(null);
                                    }
                                }}
                            />
                        ) : (
                            <button
                                className="layout-tab-name"
                                title={t(lang, "layoutTabHint")}
                                onClick={() => onSelectEdit(i)}
                                onDoubleClick={() => startRename(i)}
                            >
                                {layout.name ?? `Layout ${i + 1}`}
                            </button>
                        )}
                        <button
                            className={"layout-active-badge" + (isActive ? " on" : "")}
                            title={t(lang, isActive ? "layoutActiveTip" : "layoutSetActiveTip")}
                            disabled={isActive || readOnly}
                            onClick={() => onSetActive(i)}
                        >
                            {isActive ? t(lang, "layoutActive") : t(lang, "layoutSetActive")}
                        </button>
                        <button
                            className="layout-tab-delete"
                            title={t(lang, "layoutDeleteTip")}
                            disabled={layouts.length <= 1 || readOnly}
                            onClick={() => onDelete(i)}
                        >
                            ✕
                        </button>
                    </div>
                );
            })}
            <button
                className="layout-tab-add"
                title={t(lang, "layoutAddTip")}
                disabled={readOnly}
                onClick={onAdd}
            >
                +
            </button>
            <button
                className="layout-tab-dup"
                title={t(lang, "layoutDuplicateTip")}
                disabled={readOnly}
                onClick={() => onDuplicate(editIndex)}
            >
                {t(lang, "layoutDuplicate")}
            </button>
        </div>
    );
}
