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
    onToggleLang: () => void;
    onUndo: () => void;
    onRedo: () => void;
    onSave: () => void;
    onSaveClose: () => void;
    onExport: () => void;
    onImport: () => void;
    onOpenSettings: () => void;
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
    onToggleLang,
    onUndo,
    onRedo,
    onSave,
    onSaveClose,
    onExport,
    onImport,
    onOpenSettings,
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
                <button onClick={onToggleLang} title="Language / 言語">
                    {lang === "en" ? "日本語" : "EN"}
                </button>
                <button onClick={onUndo} disabled={!canUndo} title="Undo (Cmd/Ctrl+Z)">
                    Undo
                </button>
                <button onClick={onRedo} disabled={!canRedo} title="Redo (Shift+Cmd/Ctrl+Z)">
                    Redo
                </button>
                <button
                    className="settings-button"
                    data-testid="settings-button"
                    title={t(lang, "settingsTitle")}
                    aria-label={t(lang, "settingsTitle")}
                    onClick={onOpenSettings}
                >
                    ⚙
                </button>
                <button onClick={onImport}>Import</button>
                <button onClick={onExport}>Export</button>
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
            </div>
        </header>
    );
}
