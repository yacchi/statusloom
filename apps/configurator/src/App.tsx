// The configurator app. The single source of truth is the DSL SOURCE TEXT
// (markup.md "Visual Editor"):
//
//   sourceText (history) --parse--> parsedAst + diagnostics --> preview / canvas
//   visual edit --> AST update --serialize--> new sourceText --parse--> ast
//
// While the source is invalid (error diagnostics), the preview and the visual
// editor keep the LAST VALID ast (read-only) and saving is disabled; the DSL
// editor shows the diagnostics. Node IDs are position-derived, so after every
// structural edit the AST re-fetched via serialize -> parse is the truth
// (this file never recomputes IDs itself).

import { Suspense, lazy, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
    DndContext,
    DragOverlay,
    PointerSensor,
    closestCorners,
    useSensor,
    useSensors,
} from "@dnd-kit/core";
import { ApiError, createApi, readToken } from "./api.ts";
import {
    canRedo,
    canUndo,
    initHistory,
    pushHistory,
    redo,
    undo,
    type History,
} from "./history.ts";
import {
    activeLayoutIndex,
    addLayout,
    addLine,
    applyDropEdit,
    appendLayouts,
    deleteLayout,
    deleteLine,
    duplicateLayout,
    getNode,
    insertChild,
    lineIndexOfContainerId,
    parentChildId,
    predictChildId,
    removeNode,
    renameLayout,
    setActiveLayout,
    updateAttrs,
    updateGitAttrs,
    updateRootAttrs,
    type AttrPatch,
} from "./ast.ts";
import {
    spanContainersOf,
    useDragEditing,
    type DragPayload,
    type DropTarget,
} from "./useDragEditing.ts";
import { I18nContext, loadLang, saveLang, t, type Lang } from "./i18n.ts";
import { STRUCTURAL_PRESETS, makeFieldNode, nodeLabel } from "./presets.ts";
import {
    hasErrors,
    type Diagnostic,
    type FieldCatalogEntry,
    type LineChild,
    type Metric,
    type PreviewLine,
    type PreviewResponse,
    type PreviewSource,
    type SessionSummary,
    type StatusloomNode,
} from "./types.ts";
import type { Theme } from "./ansi.ts";
import { Header } from "./components/Header.tsx";
import { Canvas } from "./components/Canvas.tsx";
import { DslEditor } from "./components/DslEditor.tsx";
import { Palette } from "./components/Palette.tsx";
import { PropertiesPanel } from "./components/PropertiesPanel.tsx";
import { DisplaySettings } from "./components/DisplaySettings.tsx";
import { SettingsModal } from "./components/SettingsModal.tsx";
import { ImportModal, type ImportMode } from "./components/ImportModal.tsx";
import { LayoutTabs } from "./components/LayoutTabs.tsx";
import { LiveMonitor } from "./components/LiveMonitor.tsx";

// xterm.js is heavy and only needed for the embedded-terminal drawer, so it is
// split into its own chunk and loaded on demand.
const TerminalDrawer = lazy(() =>
    import("./components/TerminalDrawer.tsx").then((m) => ({ default: m.TerminalDrawer })),
);
import type { TerminalStatus } from "./components/TerminalDrawer.tsx";

const TOOL_ID = "claude-code";
const TOOL_NAME = "Claude Code";

type ViewMode = "visual" | "split" | "dsl";

interface PreviewState {
    lines: PreviewLine[] | null;
    fallback: PreviewResponse["fallback"] | null;
    error: string | null;
    loading: boolean;
}

// Expand a palette key ("field:<name>" / "preset:<id>") into the AST node it
// inserts plus a drag-overlay label.
function expandPaletteKey(
    key: string,
    fields: readonly FieldCatalogEntry[],
): { node: LineChild; label: string } | null {
    if (key.startsWith("field:")) {
        const name = key.slice("field:".length);
        const entry = fields.find((f) => f.name === name);
        return { node: makeFieldNode({ name }), label: entry?.displayName ?? name };
    }
    if (key.startsWith("preset:")) {
        const preset = STRUCTURAL_PRESETS.find((p) => p.id === key.slice("preset:".length));
        if (!preset) {
            return null;
        }
        return { node: preset.make(), label: preset.label };
    }
    return null;
}

export function App() {
    const token = useMemo(() => readToken(window.location.hash), []);

    if (!token) {
        return (
            <div className="fullscreen-error">
                <div>
                    <h1>Cannot open configurator</h1>
                    <p>
                        Open the URL printed by <code>statusloom config</code>.
                    </p>
                </div>
            </div>
        );
    }

    return <Configurator token={token} />;
}

function Configurator({ token }: { token: string }) {
    const api = useMemo(() => createApi(token), [token]);

    const [loadError, setLoadError] = useState<string | null>(null);
    // Undo/redo history over the DSL source text.
    const [history, setHistory] = useState<History<string> | null>(null);
    const [savedSource, setSavedSource] = useState<string | null>(null);
    // Last successfully parsed (error-free) AST and the source it came from.
    // The canvas and the preview keep using these while the source is broken.
    const [ast, setAstState] = useState<StatusloomNode | null>(null);
    const [lastValidSource, setLastValidSource] = useState<string>("");
    const [diagnostics, setDiagnostics] = useState<Diagnostic[]>([]);
    const [valid, setValidState] = useState(true);

    const [fields, setFields] = useState<FieldCatalogEntry[]>([]);
    const [metrics, setMetrics] = useState<Metric[]>([]);
    // Selection is an AST node ID (chips, incl. nested span children).
    const [selection, setSelection] = useState<string | null>(null);
    const [activeLine, setActiveLine] = useState(0);
    // The layout currently shown in the canvas (the edit target). Distinct
    // from the document's active layout, which the real status line renders.
    const [editLayoutIndex, setEditLayoutIndex] = useState(0);
    const [viewMode, setViewMode] = useState<ViewMode>("visual");
    const [notice, setNotice] = useState<string | null>(null);

    const [preview, setPreview] = useState<PreviewState>({
        lines: null,
        fallback: null,
        error: null,
        loading: false,
    });
    const [width, setWidth] = useState(120);
    const [previewSource, setPreviewSource] = useState<PreviewSource>({
        kind: "sample",
        sample: "full",
    });
    const [sessions, setSessions] = useState<SessionSummary[]>([]);
    const [theme, setTheme] = useState<Theme>("dark");
    const [pureOutput, setPureOutput] = useState(false);

    const [lang, setLang] = useState<Lang>(loadLang);
    const [warnings, setWarnings] = useState<string[] | null>(null);
    const [saveError, setSaveError] = useState<string | null>(null);
    const [saving, setSaving] = useState(false);
    const [closed, setClosed] = useState(false);
    const [showImport, setShowImport] = useState(false);
    const [showSettings, setShowSettings] = useState(false);

    // Embedded terminal drawer: `termOpen` toggles the drawer's visibility;
    // bumping `termStartNonce` asks the (mounted) drawer to (re)start a
    // session. The drawer owns the socket/xterm lifecycle.
    const [termOpen, setTermOpen] = useState(false);
    const [termStartNonce, setTermStartNonce] = useState(0);
    // Lifted from the drawer so the footer bar can show the embedded session's
    // status/actions without a second visible bar. Null until the drawer mounts.
    const [termStatus, setTermStatus] = useState<TerminalStatus | null>(null);
    const openEmbeddedTerminal = useCallback(() => {
        setTermStartNonce((n) => n + 1);
        setTermOpen(true);
    }, []);
    const hideEmbeddedTerminal = useCallback(() => {
        setTermOpen(false);
    }, []);

    const sensors = useSensors(
        useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    );

    const present = history?.present ?? null;

    // ---- refs mirroring the latest values for async flows / callbacks ----
    const presentRef = useRef<string | null>(null);
    presentRef.current = present;
    const astRef = useRef<StatusloomNode | null>(null);
    const validRef = useRef(true);
    // The source the current AST was parsed from; passed to serialize as the
    // minimal-diff base so unchanged nodes are reused verbatim.
    const lastValidSourceRef = useRef<string>("");
    lastValidSourceRef.current = lastValidSource;
    const fieldsRef = useRef<FieldCatalogEntry[]>([]);
    fieldsRef.current = fields;
    const selectionRef = useRef<string | null>(null);
    selectionRef.current = selection;
    const layoutIndexRef = useRef(0);
    // Monotonic sequence of visual (AST) edits; a stale serialize/parse chain
    // is dropped when a newer edit superseded it.
    const editSeqRef = useRef(0);
    // Where the next `present` change came from. "ast" means applyAstEdit
    // already parsed the source, so the parse effect must not run again.
    const originRef = useRef<"text" | "ast">("text");

    const setAst = useCallback((next: StatusloomNode | null) => {
        astRef.current = next;
        setAstState(next);
    }, []);
    const setValid = useCallback((next: boolean) => {
        validRef.current = next;
        setValidState(next);
    }, []);

    // ---- shared draft (<tool>.draft.xml) ----
    // The version (sha256 of the source) we last wrote or imported. A poll
    // returning this same version is our own echo and is ignored; a different
    // version is an external edit (e.g. `statusloom draft push`, or Claude in
    // the embedded terminal) to fold into history.
    const lastDraftVersionRef = useRef<string | null>(null);
    const lastDraftContentRef = useRef<string | null>(null);
    // Disabled after the first draft call fails so the feature stays inert.
    const draftEnabledRef = useRef(true);

    // Initial load.
    useEffect(() => {
        let cancelled = false;
        (async () => {
            try {
                const [fieldList, metricList, doc, draft] = await Promise.all([
                    api.getFields(TOOL_ID),
                    api.getMetrics(TOOL_ID),
                    api.getDocument(TOOL_ID),
                    api.getDraft(TOOL_ID).catch(() => null),
                ]);
                if (cancelled) {
                    return;
                }
                setFields(fieldList);
                setMetrics(metricList);
                setSavedSource(doc.source);
                // Start from the shared draft (it falls back to the saved
                // document server-side); without draft support, from the doc.
                if (draft) {
                    lastDraftVersionRef.current = draft.version;
                    lastDraftContentRef.current = draft.source;
                } else {
                    draftEnabledRef.current = false;
                }
                setHistory(initHistory(draft ? draft.source : doc.source));
            } catch (err) {
                if (!cancelled) {
                    setLoadError((err as Error).message);
                }
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [api]);

    // Captured real sessions usable as a preview source. Best-effort: a
    // transient failure must not break the configurator, so failures are
    // swallowed and the sample selector simply omits the "Live sessions" group.
    const refreshSessions = useCallback(async () => {
        try {
            const list = await api.getSessions();
            setSessions(list);
        } catch {
            // Live sessions are optional; keep whatever we had (or none).
        }
    }, [api]);
    useEffect(() => {
        refreshSessions();
    }, [refreshSessions]);

    function pushSource(next: string) {
        setHistory((h) => (h ? pushHistory(h, next) : h));
    }

    // ---- parse pipeline ----
    // Text-originated source changes (typing, undo/redo, draft import, import
    // modal) are parsed after a debounce. AST-originated changes were already
    // parsed inside applyAstEdit and are skipped here.
    useEffect(() => {
        if (present === null) {
            return;
        }
        const origin = originRef.current;
        originRef.current = "text";
        if (origin === "ast") {
            return;
        }
        let cancelled = false;
        const handle = window.setTimeout(async () => {
            try {
                const res = await api.parse(present);
                if (cancelled) {
                    return;
                }
                setDiagnostics(res.diagnostics);
                const ok = res.ast !== undefined && !hasErrors(res.diagnostics);
                setValid(ok);
                if (ok) {
                    setAst(res.ast ?? null);
                    setLastValidSource(present);
                }
            } catch (err) {
                if (!cancelled) {
                    setSaveError((err as Error).message);
                }
            }
        }, 300);
        return () => {
            cancelled = true;
            window.clearTimeout(handle);
        };
    }, [api, present, setAst, setValid]);

    // ---- visual edits (AST -> serialize -> parse -> source history) ----
    // Every visual-editor operation funnels through here. The edit applies to
    // the latest AST synchronously (so rapid edits chain), then the server
    // serializes it to canonical source and re-parses it; that parsed AST is
    // the truth afterwards (position-derived node IDs are recomputed by it).
    const applyAstEdit = useCallback(
        async (
            edit: (root: StatusloomNode) => StatusloomNode,
            opts?: { select?: string | null },
        ): Promise<boolean> => {
            const base = astRef.current;
            if (!base || !validRef.current) {
                return false;
            }
            const next = edit(base);
            if (next === base) {
                return false;
            }
            setAst(next); // optimistic, so consecutive edits chain
            const seq = ++editSeqRef.current;
            try {
                // Serialize against the base the edited AST's ranges index into
                // (minimal-diff), so unchanged nodes — comments, raw text,
                // symbolic-operator when, custom indentation — survive verbatim.
                const base = lastValidSourceRef.current;
                const ser = await api.serialize(next, base !== "" ? base : undefined);
                if (seq !== editSeqRef.current) {
                    return true;
                }
                const par = await api.parse(ser.source);
                if (seq !== editSeqRef.current) {
                    return true;
                }
                originRef.current = "ast";
                setHistory((h) => (h ? pushHistory(h, ser.source) : h));
                setDiagnostics(par.diagnostics);
                const ok = par.ast !== undefined && !hasErrors(par.diagnostics);
                setValid(ok);
                if (ok) {
                    setAst(par.ast ?? null);
                    setLastValidSource(ser.source);
                }
                if (opts && opts.select !== undefined) {
                    setSelection(opts.select);
                }
                return true;
            } catch (err) {
                setSaveError((err as Error).message);
                return false;
            }
        },
        [api, setAst, setValid],
    );

    // Publish source edits to the shared draft (debounced), so the terminal
    // side sees the in-progress, unsaved document — even while it is invalid
    // (the draft channel tolerates in-progress input by design).
    useEffect(() => {
        if (present === null || !draftEnabledRef.current) {
            return;
        }
        if (present === lastDraftContentRef.current) {
            return;
        }
        const handle = window.setTimeout(async () => {
            if (present === lastDraftContentRef.current) {
                return;
            }
            try {
                const { version } = await api.putDraft(TOOL_ID, present);
                lastDraftVersionRef.current = version;
                lastDraftContentRef.current = present;
            } catch {
                draftEnabledRef.current = false;
            }
        }, 250);
        return () => window.clearTimeout(handle);
    }, [api, present]);

    // Poll the shared draft; a version different from the one we last
    // wrote/imported means the other side edited the draft. Fold that
    // external edit into history so it is undoable, and record its
    // version+content so our echo does not re-import it.
    useEffect(() => {
        const id = window.setInterval(async () => {
            if (!draftEnabledRef.current || lastDraftVersionRef.current === null) {
                return;
            }
            try {
                const { source, version } = await api.getDraft(TOOL_ID);
                if (version === lastDraftVersionRef.current) {
                    return; // our own echo
                }
                lastDraftVersionRef.current = version;
                lastDraftContentRef.current = source;
                if (presentRef.current === source) {
                    return;
                }
                setHistory((h) => (h ? pushHistory(h, source) : h));
            } catch {
                draftEnabledRef.current = false;
            }
        }, 1000);
        return () => window.clearInterval(id);
    }, [api]);

    // ---- derived layout state (from the last valid AST) ----
    const layouts = ast?.layouts ?? [];
    const layoutCount = layouts.length;
    const layoutIndex =
        layoutCount > 0 ? Math.max(0, Math.min(editLayoutIndex, layoutCount - 1)) : 0;
    layoutIndexRef.current = layoutIndex;
    const curLayout = layouts[layoutIndex] ?? null;
    const curLines = curLayout?.lines ?? [];
    const activeIndex = ast ? activeLayoutIndex(ast) : 0;

    // Keep the derived (clamped) index in state so callbacks stay consistent.
    useEffect(() => {
        if (layoutCount > 0 && editLayoutIndex !== layoutIndex) {
            setEditLayoutIndex(layoutIndex);
        }
    }, [editLayoutIndex, layoutIndex, layoutCount]);

    const dirty = present !== null && savedSource !== null && present !== savedSource;

    const displayName = useCallback(
        (field: string) => fields.find((f) => f.name === field)?.displayName ?? field,
        [fields],
    );

    const removeSelected = useCallback(() => {
        const sel = selectionRef.current;
        if (!sel) {
            return;
        }
        // Only chips (line/span children) are removable via the keyboard.
        if (!parentChildId(sel)) {
            return;
        }
        applyAstEdit((root) => removeNode(root, sel), { select: null });
    }, [applyAstEdit]);

    const toggleLang = useCallback(() => {
        setLang((cur) => {
            const next: Lang = cur === "en" ? "ja" : "en";
            saveLang(next);
            return next;
        });
    }, []);

    // ---- drag & drop ----
    // Dragging never mutates the document; the hook computes a paint-only
    // drop target and calls onDrop exactly once, on drop.
    const onDrop = useCallback(
        (payload: DragPayload, target: DropTarget) => {
            const root = astRef.current;
            if (!root) {
                return;
            }
            const result = applyDropEdit(root, payload, target);
            void applyAstEdit(() => result.next, { select: result.select });
            const lineIndex = lineIndexOfContainerId(target.containerId);
            if (lineIndex !== null) {
                setActiveLine(lineIndex);
            }
        },
        [applyAstEdit],
    );

    const { dragLabel, dropTarget, onDragStart, onDragOver, onDragEnd, onDragCancel } =
        useDragEditing({
            getContainers: useCallback(() => {
                if (!validRef.current) {
                    return null; // read-only while the source is broken
                }
                const li = layoutIndexRef.current;
                const lines = astRef.current?.layouts[li]?.lines ?? null;
                if (!lines) {
                    return null;
                }
                return lines.flatMap((line, j) => {
                    const id = line.id !== "" ? line.id : `L${li}.${j}`;
                    return [
                        { id, kind: "line" as const, lineIndex: j, childIds: line.children.map((c) => c.id) },
                        ...spanContainersOf(line.children),
                    ];
                });
            }, []),
            makePaletteNode: useCallback(
                (key: string) => expandPaletteKey(key, fieldsRef.current),
                [],
            ),
            labelForNodeId: useCallback((id: string) => {
                const root = astRef.current;
                const node = root ? getNode(root, id) : null;
                if (!node || node.kind === "statusloom" || node.kind === "git") {
                    return "";
                }
                if (
                    node.kind === "layout" ||
                    node.kind === "line" ||
                    node.kind === "color-rule"
                ) {
                    return node.kind;
                }
                return nodeLabel(node, (f) => f);
            }, []),
            kindForNodeId: useCallback((id: string) => {
                const root = astRef.current;
                return root ? getNode(root, id)?.kind ?? null : null;
            }, []),
            onDrop,
        });

    // ---- preview (renders the last valid source) ----
    useEffect(() => {
        if (lastValidSource === "") {
            return;
        }
        let cancelled = false;
        setPreview((p) => ({ ...p, loading: true }));
        const handle = window.setTimeout(async () => {
            try {
                const res = await api.preview(
                    previewSource.kind === "session"
                        ? {
                              tool: TOOL_ID,
                              source: lastValidSource,
                              width,
                              sample: "full",
                              sessionId: previewSource.id,
                              layoutIndex,
                          }
                        : {
                              tool: TOOL_ID,
                              source: lastValidSource,
                              width,
                              sample: previewSource.sample,
                              layoutIndex,
                          },
                );
                if (!cancelled) {
                    setPreview({
                        lines: res.lines,
                        fallback: res.fallback ?? null,
                        error: null,
                        loading: false,
                    });
                }
            } catch (err) {
                if (cancelled) {
                    return;
                }
                // A selected session may have been pruned (TTL) between
                // selection and this request; fall back to the synthetic
                // "full" sample rather than getting stuck on an error.
                if (
                    previewSource.kind === "session" &&
                    err instanceof ApiError &&
                    err.status === 400
                ) {
                    setPreviewSource({ kind: "sample", sample: "full" });
                    return;
                }
                setPreview({
                    lines: null,
                    fallback: null,
                    error: (err as Error).message,
                    loading: false,
                });
            }
        }, 250);
        return () => {
            cancelled = true;
            window.clearTimeout(handle);
        };
    }, [api, lastValidSource, width, previewSource, layoutIndex]);

    // Keyboard: undo/redo, deselect, delete selected node.
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            const target = e.target as HTMLElement | null;
            if (target && /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName)) {
                return;
            }
            const mod = e.metaKey || e.ctrlKey;
            if (mod && e.key.toLowerCase() === "z") {
                e.preventDefault();
                setHistory((h) => (h ? (e.shiftKey ? redo(h) : undo(h)) : h));
                return;
            }
            if (e.key === "Escape") {
                setSelection(null);
                return;
            }
            if (e.key === "Delete" || e.key === "Backspace") {
                if (selectionRef.current) {
                    e.preventDefault();
                    removeSelected();
                }
            }
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [removeSelected]);

    // Warn on unload when there are unsaved changes.
    useEffect(() => {
        const handler = (e: BeforeUnloadEvent) => {
            if (dirty) {
                e.preventDefault();
                e.returnValue = "";
            }
        };
        window.addEventListener("beforeunload", handler);
        return () => window.removeEventListener("beforeunload", handler);
    }, [dirty]);

    // ---- editing actions ----

    function addFromPalette(key: string) {
        if (!ast || !valid || !curLayout) {
            return;
        }
        const made = expandPaletteKey(key, fields);
        if (!made) {
            return;
        }
        const li = layoutIndex;
        const hasLines = curLines.length > 0;
        const line = hasLines ? Math.max(0, Math.min(activeLine, curLines.length - 1)) : 0;
        const lineId = `L${li}.${line}`;
        const endIndex = hasLines ? curLines[line].children.length : 0;
        applyAstEdit(
            (root) => {
                let r = root;
                if (!hasLines) {
                    r = addLine(r, li);
                }
                return insertChild(r, lineId, endIndex, made.node);
            },
            { select: predictChildId(lineId, endIndex) },
        );
        setActiveLine(line);
    }

    async function doSave(): Promise<boolean> {
        const current = presentRef.current;
        if (current === null) {
            return false;
        }
        if (!validRef.current) {
            setSaveError(t(lang, "saveBlocked"));
            return false;
        }
        setSaving(true);
        setSaveError(null);
        try {
            const res = await api.putDocument(TOOL_ID, current);
            if (!res.saved) {
                // 409: error diagnostics; nothing was written.
                setDiagnostics(res.diagnostics);
                setValid(false);
                setSaveError(t(lang, "saveBlocked"));
                return false;
            }
            setSavedSource(current);
            const warn = res.diagnostics
                .filter((d) => d.severity === "warning")
                .map((d) => d.message);
            setWarnings(warn.length > 0 ? warn : null);
            return true;
        } catch (err) {
            setSaveError((err as Error).message);
            return false;
        } finally {
            setSaving(false);
        }
    }

    async function doSaveClose() {
        const ok = await doSave();
        if (!ok) {
            return;
        }
        try {
            await api.shutdown();
        } catch {
            // The server may drop the connection as it exits; ignore.
        }
        setClosed(true);
    }

    function doExport() {
        if (present === null) {
            return;
        }
        const blob = new Blob([present], { type: "application/xml" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `${TOOL_ID}.xml`;
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
    }

    // Import: "append" adds the pasted document's layouts to the current set;
    // "replace" swaps in the whole source (even if invalid — the DSL editor
    // then shows its diagnostics). Returns an error message or null.
    async function onImport(source: string, mode: ImportMode): Promise<string | null> {
        if (mode === "replace") {
            pushSource(source);
            setSelection(null);
            setActiveLine(0);
            setShowImport(false);
            setNotice("Document replaced.");
            return null;
        }
        if (!valid || !ast) {
            return t(lang, "dslInvalid");
        }
        try {
            const res = await api.parse(source);
            if (!res.ast || hasErrors(res.diagnostics)) {
                const msgs = res.diagnostics
                    .filter((d) => d.severity === "error")
                    .map((d) => d.message);
                return msgs.length > 0 ? msgs.join("; ") : "Invalid DSL source.";
            }
            const incoming = res.ast.layouts;
            if (incoming.length === 0) {
                return "No layouts found to import.";
            }
            const before = ast.layouts.length;
            const ok = await applyAstEdit((root) => appendLayouts(root, incoming));
            if (!ok) {
                return "Import failed.";
            }
            setEditLayoutIndex(before);
            setSelection(null);
            setActiveLine(0);
            setShowImport(false);
            setNotice(
                `Imported ${incoming.length} layout${incoming.length === 1 ? "" : "s"}.`,
            );
            return null;
        } catch (err) {
            return (err as Error).message;
        }
    }

    // ---- layout actions ----

    function switchLayout(index: number) {
        setEditLayoutIndex(index);
        setSelection(null);
        setActiveLine(0);
    }

    function onAddLayout() {
        if (!ast) {
            return;
        }
        const next = ast.layouts.length;
        applyAstEdit((root) => addLayout(root)).then((ok) => {
            if (ok) {
                switchLayout(next);
            }
        });
    }

    function onDuplicateLayout(index: number) {
        applyAstEdit((root) => duplicateLayout(root, index)).then((ok) => {
            if (ok) {
                switchLayout(index + 1);
            }
        });
    }

    function onDeleteLayout(index: number) {
        if (!ast || ast.layouts.length <= 1) {
            return;
        }
        const remaining = ast.layouts.length - 1;
        applyAstEdit((root) => deleteLayout(root, index)).then((ok) => {
            if (ok) {
                setSelection(null);
                setActiveLine(0);
                setEditLayoutIndex((cur) => Math.max(0, Math.min(cur, remaining - 1)));
            }
        });
    }

    function onRenameLayout(index: number, name: string) {
        applyAstEdit((root) => renameLayout(root, index, name));
    }

    function onSetActiveLayout(index: number) {
        applyAstEdit((root) => setActiveLayout(root, index));
    }

    // ---- render ----

    if (closed) {
        return (
            <div className="fullscreen-error">
                <div>
                    <h1>Saved</h1>
                    <p>You can close this tab.</p>
                </div>
            </div>
        );
    }

    if (loadError) {
        return (
            <div className="fullscreen-error">
                <div>
                    <h1>Failed to load configuration</h1>
                    <p className="inline-error">{loadError}</p>
                </div>
            </div>
        );
    }

    if (!history || present === null) {
        return (
            <div className="fullscreen-error">
                <div>Loading…</div>
            </div>
        );
    }

    const selectedNode = ast && selection ? getNode(ast, selection) : null;
    const showVisual = viewMode !== "dsl";
    const showDsl = viewMode !== "visual";

    return (
        <I18nContext.Provider value={lang}>
        <div className="app">
            <Header
                toolName={TOOL_NAME}
                dirty={dirty}
                saving={saving}
                canSave={valid}
                canUndo={canUndo(history)}
                canRedo={canRedo(history)}
                lang={lang}
                onToggleLang={toggleLang}
                onUndo={() => setHistory((h) => (h ? undo(h) : h))}
                onRedo={() => setHistory((h) => (h ? redo(h) : h))}
                onSave={doSave}
                onSaveClose={doSaveClose}
                onExport={doExport}
                onImport={() => setShowImport(true)}
                onOpenSettings={() => setShowSettings(true)}
            />

            {warnings ? (
                <div className="banner warn">
                    <ul>
                        {warnings.map((w, i) => (
                            <li key={i}>{w}</li>
                        ))}
                    </ul>
                    <button onClick={() => setWarnings(null)}>Dismiss</button>
                </div>
            ) : null}

            {saveError ? (
                <div className="banner error">
                    <span style={{ flex: 1 }}>{saveError}</span>
                    <button onClick={() => setSaveError(null)}>Dismiss</button>
                </div>
            ) : null}

            {notice ? (
                <div className="banner info">
                    <span style={{ flex: 1 }}>{notice}</span>
                    <button onClick={() => setNotice(null)}>Dismiss</button>
                </div>
            ) : null}

            <DndContext
                sensors={sensors}
                collisionDetection={closestCorners}
                onDragStart={onDragStart}
                onDragOver={onDragOver}
                onDragEnd={onDragEnd}
                onDragCancel={onDragCancel}
            >
                <div className="main">
                    <aside className="side">
                        <Palette fields={fields} onAdd={addFromPalette} />
                    </aside>

                    <section className="content">
                        <div className="view-toggle" role="tablist">
                            {(["visual", "split", "dsl"] as const).map((mode) => (
                                <button
                                    key={mode}
                                    role="tab"
                                    aria-selected={viewMode === mode}
                                    className={viewMode === mode ? "on" : ""}
                                    data-testid={`view-${mode}`}
                                    onClick={() => setViewMode(mode)}
                                >
                                    {mode === "visual"
                                        ? "Visual"
                                        : mode === "split"
                                          ? "Split"
                                          : "DSL"}
                                </button>
                            ))}
                        </div>

                        {showVisual ? (
                            <LayoutTabs
                                layouts={layouts}
                                editIndex={layoutIndex}
                                activeIndex={activeIndex}
                                readOnly={!valid}
                                onSelectEdit={switchLayout}
                                onSetActive={onSetActiveLayout}
                                onAdd={onAddLayout}
                                onDuplicate={onDuplicateLayout}
                                onDelete={onDeleteLayout}
                                onRename={onRenameLayout}
                            />
                        ) : null}

                        {showVisual && ast ? (
                            <DisplaySettings
                                root={ast}
                                readOnly={!valid}
                                onPatchRoot={(patch: AttrPatch) =>
                                    applyAstEdit((root) => updateRootAttrs(root, patch))
                                }
                            />
                        ) : null}

                        {showVisual ? (
                            <Canvas
                                lines={curLines}
                                previewLines={preview.lines}
                                fallback={preview.fallback}
                                selection={selection}
                                activeLine={activeLine}
                                dropTarget={dropTarget}
                                theme={theme}
                                width={width}
                                previewSource={previewSource}
                                sessions={sessions}
                                pureOutput={pureOutput}
                                loading={preview.loading}
                                error={preview.error}
                                readOnly={!valid}
                                displayName={displayName}
                                onSelect={(id, lineIndex) => {
                                    setSelection(id);
                                    setActiveLine(lineIndex);
                                }}
                                onDeselect={() => setSelection(null)}
                                onActivateLine={(lineIndex) => {
                                    setActiveLine(lineIndex);
                                    setSelection(null);
                                }}
                                onAddLine={() => {
                                    const li = layoutIndex;
                                    applyAstEdit((root) => addLine(root, li));
                                    setActiveLine(curLines.length);
                                }}
                                onDeleteLine={(lineIndex) => {
                                    const li = layoutIndex;
                                    applyAstEdit((root) => deleteLine(root, li, lineIndex), {
                                        select: null,
                                    });
                                    setActiveLine((cur) =>
                                        Math.max(0, Math.min(cur, curLines.length - 2)),
                                    );
                                }}
                                onWidth={setWidth}
                                onPreviewSourceChange={setPreviewSource}
                                onRefreshSessions={refreshSessions}
                                onTheme={setTheme}
                                onPureOutput={setPureOutput}
                            />
                        ) : null}

                        {showDsl ? (
                            <DslEditor
                                source={present}
                                diagnostics={diagnostics}
                                valid={valid}
                                onChange={pushSource}
                            />
                        ) : null}

                        {showVisual ? (
                            <PropertiesPanel
                                node={selectedNode}
                                fields={fields}
                                metrics={metrics}
                                theme={theme}
                                readOnly={!valid}
                                onPatch={(patch: AttrPatch) => {
                                    const sel = selectionRef.current;
                                    if (!sel) {
                                        return;
                                    }
                                    applyAstEdit((root) => updateAttrs(root, sel, patch));
                                }}
                                onRemove={removeSelected}
                            />
                        ) : null}
                    </section>
                </div>

                <DragOverlay>
                    {dragLabel ? <div className="drag-overlay-chip">{dragLabel}</div> : null}
                </DragOverlay>
            </DndContext>

            {/* Docked full-width terminal drawer at the bottom of the window.
                Kept outside the DndContext so its height drag never interacts
                with the canvas drag-editing. Only mounted once opened. */}
            {termOpen || termStartNonce > 0 ? (
                <Suspense fallback={null}>
                    <TerminalDrawer
                        api={api}
                        token={token}
                        previewSource={previewSource}
                        onPreviewSourceChange={setPreviewSource}
                        startNonce={termStartNonce}
                        open={termOpen}
                        onSetOpen={setTermOpen}
                        onStatusChange={setTermStatus}
                    />
                </Suspense>
            ) : null}

            <footer className="footer-bar">
                <LiveMonitor
                    api={api}
                    token={token}
                    previewSource={previewSource}
                    onPreviewSourceChange={setPreviewSource}
                    onOpenEmbedded={openEmbeddedTerminal}
                    onHideEmbedded={hideEmbeddedTerminal}
                    embeddedOpen={termOpen}
                    termStatus={termStatus}
                />
            </footer>

            {showImport ? (
                <ImportModal onImport={onImport} onClose={() => setShowImport(false)} />
            ) : null}

            {showSettings && ast ? (
                <SettingsModal
                    root={ast}
                    readOnly={!valid}
                    onPatchGit={(patch: AttrPatch) =>
                        applyAstEdit((root) => updateGitAttrs(root, patch))
                    }
                    onClose={() => setShowSettings(false)}
                />
            ) : null}
        </div>
        </I18nContext.Provider>
    );
}
