import { t, type Lang } from "../i18n.ts";

interface Props {
    toolName: string;
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
    toolName,
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
            <span className="hint">{toolName}</span>
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
