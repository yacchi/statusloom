import { t, useLang, type MessageKey } from "../i18n.ts";

// Small (?) affordance whose hover shows a localized help text via the shared
// data-tip tooltip mechanism (see styles.css).
export function HelpTip({ k }: { k: MessageKey }) {
    const lang = useLang();
    return (
        <span className="help-icon" data-tip={t(lang, k)}>
            ?
        </span>
    );
}
