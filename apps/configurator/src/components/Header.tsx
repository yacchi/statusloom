import { t, type Lang } from "../i18n.ts";
import type { ToolInfo } from "../types.ts";

interface Props {
    // The documents (tools) the backend supports and which one is active;
    // both come from GET /api/tools, so the tab list is never hardcoded here.
    tools: ToolInfo[];
    activeTool: string | null;
    // The tool currently loading on its first open (null when idle); its tab
    // shows a busy affordance. Subsequent switches are instant (no pending).
    pendingTool: string | null;
    onSwitchTool: (id: string) => void;
    dirty: boolean;
    saving: boolean;
    // False while the DSL has error diagnostics (saving is blocked).
    canSave: boolean;
    canUndo: boolean;
    canRedo: boolean;
    lang: Lang;
    onUndo: () => void;
    onRedo: () => void;
    onSave: () => void;
    onSaveClose: () => void;
    onDiscardClose: () => void;
    onExportMarkdown: () => void;
    onImport: () => void;
    onOpenSettings: () => void;
    onOpenHistory: () => void;
}

export function Header({
    tools,
    activeTool,
    pendingTool,
    onSwitchTool,
    dirty,
    saving,
    canSave,
    canUndo,
    canRedo,
    lang,
    onUndo,
    onRedo,
    onSave,
    onSaveClose,
    onDiscardClose,
    onExportMarkdown,
    onImport,
    onOpenSettings,
    onOpenHistory,
}: Props) {
    return (
        <header className="header">
            <h1>Statusloom</h1>
            <div className="tool-tabs" role="tablist" aria-label="Document">
                {tools.map((tool) => (
                    <button
                        key={tool.id}
                        type="button"
                        role="tab"
                        aria-selected={activeTool === tool.id}
                        aria-busy={pendingTool === tool.id}
                        className={activeTool === tool.id ? "on" : ""}
                        data-testid={`tool-${tool.id}`}
                        disabled={pendingTool !== null}
                        onClick={() => onSwitchTool(tool.id)}
                    >
                        {tool.displayName}
                    </button>
                ))}
            </div>
            {dirty ? (
                <span className="dirty-dot" title="Unsaved changes">
                    ● unsaved
                </span>
            ) : null}
            <div className="spacer" />
            <div className="toolbar">
                {/* Undo and Redo are one unit: grouped so they sit tighter to
                    each other than to the neighboring tools (.toolbar-group). */}
                <span className="toolbar-group">
                    <button onClick={onUndo} disabled={!canUndo} title="Undo (Cmd/Ctrl+Z)">
                        Undo
                    </button>
                    <button onClick={onRedo} disabled={!canRedo} title="Redo (Shift+Cmd/Ctrl+Z)">
                        Redo
                    </button>
                </span>
                <button
                    className="settings-button"
                    data-testid="settings-button"
                    title={t(lang, "globalSettingsTitle")}
                    aria-label={t(lang, "globalSettingsTitle")}
                    onClick={onOpenSettings}
                >
                    ⚙
                </button>
                <button onClick={onImport}>Import</button>
                <button
                    data-testid="export-button"
                    onClick={onExportMarkdown}
                    title="Export as a .sloom.md Markdown exchange file"
                >
                    Export
                </button>
                <button data-testid="history-button" onClick={onOpenHistory}>
                    History
                </button>
                <button
                    className="primary"
                    data-testid="save-button"
                    onClick={onSave}
                    disabled={saving || !canSave}
                >
                    {saving ? "Saving…" : "Save"}
                </button>
                <button
                    className="primary"
                    onClick={onSaveClose}
                    disabled={saving || !canSave}
                >
                    Save &amp; Close
                </button>
                {/* Unlike Save/Save & Close, this is never gated on canSave:
                    it's the escape hatch when the DSL has errors and the
                    other two buttons are disabled. */}
                <button
                    data-testid="discard-close-button"
                    onClick={onDiscardClose}
                    disabled={saving}
                    title="Discard unsaved changes and close"
                >
                    Close without saving
                </button>
            </div>
        </header>
    );
}
