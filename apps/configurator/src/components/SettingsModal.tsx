// Global settings hosted in a modal (opened from the header ⚙ button). Two
// kinds of settings live here side by side:
//
//   - Editor settings (language): a local UI preference persisted in
//     localStorage, NOT part of the document.
//   - Git settings: the document's optional <git/> element, whose form body
//     lives in GitSettings.
//
// Every change is applied live (an attribute patch for git, a state update for
// the language) — there is no separate save step, so this modal has no
// footer/primary action; the × button and Escape are its only ways to close.

import type { AttrPatch } from "../ast.ts";
import type { StatusloomNode } from "../types.ts";
import { t, useLang, type Lang } from "../i18n.ts";
import { HelpTip } from "./HelpTip.tsx";
import { GitSettings } from "./GitSettings.tsx";
import { Modal } from "./Modal.tsx";

interface Props {
    root: StatusloomNode;
    readOnly: boolean;
    onPatchGit: (patch: AttrPatch) => void;
    onChangeLang: (lang: Lang) => void;
    onClose: () => void;
}

export function SettingsModal({ root, readOnly, onPatchGit, onChangeLang, onClose }: Props) {
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
        </Modal>
    );
}
