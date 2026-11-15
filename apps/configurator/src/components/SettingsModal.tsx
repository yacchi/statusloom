// Global settings hosted in a modal (opened from the header ⚙ button). Two
// kinds of settings live here side by side:
//
//   - Editor settings (language): a local UI preference persisted in
//     localStorage, NOT part of the document.
//   - Git settings: the document's optional <git/> element, whose form body
//     lives in GitSettings.
//   - Refresh wait: the document's optional <refresh/> element
//     (RefreshSettings).
//   - External sources (when sourcesApi is provided): the local
//     <configDir>/sources.json definitions, managed through SourcesSettings.
//     These are machine-local, never part of the document or an export.
//
// Every change is applied live (an attribute patch for git / refresh, a state
// update for the language, a direct API write for sources) — there is no
// separate save step, so this modal has no footer/primary action; the ×
// button and Escape are its only ways to close.

import type { AttrPatch } from "../ast.ts";
import type { RefreshItems, StatusloomNode } from "../types.ts";
import type { SourceEntry, SourcesApi } from "../sourcesApi.ts";
import { t, useLang, type Lang } from "../i18n.ts";
import { HelpTip } from "./HelpTip.tsx";
import { GitSettings } from "./GitSettings.tsx";
import { RefreshSettings } from "./RefreshSettings.tsx";
import { Modal } from "./Modal.tsx";
import { SourcesSettings } from "./SourcesSettings.tsx";

interface Props {
    root: StatusloomNode;
    readOnly: boolean;
    onPatchGit: (patch: AttrPatch) => void;
    // Work items the document uses (parse response) and the defined sources,
    // for the Refresh panel's toggles.
    refreshItems: RefreshItems | null;
    sources: readonly SourceEntry[];
    onPatchRefresh: (patch: AttrPatch) => void;
    onChangeLang: (lang: Lang) => void;
    onClose: () => void;
    sourcesApi?: SourcesApi;
    onSourcesChanged?: (sources: SourceEntry[]) => void;
}

export function SettingsModal({
    root,
    readOnly,
    onPatchGit,
    refreshItems,
    sources,
    onPatchRefresh,
    onChangeLang,
    onClose,
    sourcesApi,
    onSourcesChanged,
}: Props) {
    const lang = useLang();
    return (
        <Modal
            title={t(lang, "globalSettingsTitle")}
            onClose={onClose}
            className="settings-modal"
        >
            <h3 className="settings-section">{t(lang, "settingsAppSection")}</h3>
            <fieldset className="props-body">
                <div className="field">
                    <label htmlFor="setting-lang">{t(lang, "settingsLanguage")}</label>
                    <select
                        id="setting-lang"
                        data-testid="setting-lang"
                        value={lang}
                        onChange={(e) => onChangeLang(e.target.value as Lang)}
                    >
                        <option value="en">English</option>
                        <option value="ja">日本語</option>
                    </select>
                </div>
            </fieldset>
            <h3 className="settings-section">
                {t(lang, "settingsTitle")} <HelpTip k="helpGit" />
            </h3>
            <GitSettings root={root} readOnly={readOnly} onPatchGit={onPatchGit} />
            <h3 className="settings-section">
                {t(lang, "refreshSection")} <HelpTip k="helpRefresh" />
            </h3>
            <RefreshSettings
                root={root}
                readOnly={readOnly}
                refreshItems={refreshItems}
                sources={sources}
                onPatchRefresh={onPatchRefresh}
            />
            {sourcesApi && (
                <>
                    <h3 className="settings-section">
                        {t(lang, "sourcesSection")} <HelpTip k="helpSources" />
                    </h3>
                    <SourcesSettings sourcesApi={sourcesApi} onChanged={onSourcesChanged} />
                </>
            )}
        </Modal>
    );
}
