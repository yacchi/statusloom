// Minimal dependency-free i18n for the configurator UI.
//
// Scope is deliberately narrow: only descriptions, help, and hint texts are
// translated. Field displayNames, action button labels (Save, Undo, …) and
// technical terms stay English.

import { createContext, useContext } from "react";

export type Lang = "en" | "ja";

export const LANG_STORAGE_KEY = "statusloom.lang";

// navigator.language "ja", "ja-JP", … → ja; everything else → en.
export function detectLang(navLang: string | null | undefined): Lang {
    return navLang && navLang.toLowerCase().startsWith("ja") ? "ja" : "en";
}

export function loadLang(): Lang {
    try {
        const stored = window.localStorage.getItem(LANG_STORAGE_KEY);
        if (stored === "en" || stored === "ja") {
            return stored;
        }
    } catch {
        // Storage unavailable: fall through to detection.
    }
    return detectLang(typeof navigator !== "undefined" ? navigator.language : undefined);
}

export function saveLang(lang: Lang): void {
    try {
        window.localStorage.setItem(LANG_STORAGE_KEY, lang);
    } catch {
        // Best effort only.
    }
}

const EN = {
    // hints
    paletteHint: "Click to add to the active line, or drag into the preview.",
    propertiesEmptyHint: "Click a chip in the preview to edit it.",
    removeHint: "or press Delete / Backspace",
    canvasFooterHint:
        "Click a chip to select it; drag to reorder or move across lines. Esc deselects, Delete removes.",
    ghostHiddenReason: "no output in the current sample",
    zeroDisabledHint: "0 = disabled",
    dropHere: "drop items here",
    omittedBadge: "omitted at this width",
    rendering: "rendering…",
    noPreview: "No preview yet.",
    // layout tabs
    layoutTabHint: "Click to edit this layout; double-click to rename.",
    layoutActive: "● active",
    layoutSetActive: "○ set active",
    layoutActiveTip: "This layout is used by the real status line.",
    layoutSetActiveTip: "Make this the layout the real status line uses.",
    layoutAddTip: "Add a new layout",
    layoutDuplicate: "Duplicate",
    layoutDuplicateTip: "Duplicate the layout being edited",
    layoutDeleteTip: "Delete this layout",
    // preview fallback
    fallbackNote:
        "This layout produces no output when information is scarce (e.g. right after a session starts). The status line then shows:",
    // DSL editor
    dslEditorHint:
        "The DSL source of this document. Edits here and in the visual editor stay in sync.",
    dslInvalid:
        "The DSL has errors. The preview shows the last valid state; visual editing and saving are disabled until the errors are fixed.",
    dslNoProblems: "No problems.",
    saveBlocked: "Cannot save: the document has errors.",
    // settings
    settingsTitle: "Git settings",
    // help texts
    helpGit: "Git data collection settings (the optional <git/> element). Defaults apply when unset.",
    helpFlex:
        "How this flex node expands to fill the terminal width: full = fill the full width, full-minus-N = leave N columns free. When a line has multiple flex nodes with different sizes, the smallest target wins.",
    helpCompactThreshold:
        "Below this COLUMNS width, fields render in their compact form. 0 disables compact mode.",
    helpColorLevel:
        "Color depth of the output. Higher levels are downgraded automatically when the terminal does not support them.",
    helpContextMode:
        "How context percentage fields are computed: raw = of the whole context window, usable = of the usable window (minus reserved tokens), both = show both.",
    helpReserveTokens: "Tokens reserved from the window for the usable-percentage calculation.",
    helpColor: "Foreground color: an ANSI color name (kebab-case) or a custom hex value.",
    helpBackground: "Background color: an ANSI color name (kebab-case) or a custom hex value.",
    helpDecorations:
        "Text decorations. Unspecified inherits from the parent; on/off override the inherited value.",
    helpRawValue: "Output the raw value without label or formatting (also skips compact mode).",
    helpHyperlink:
        "Wrap this field in a terminal hyperlink (OSC 8) so supporting terminals make it clickable.",
    helpPrefixSuffix:
        "Fixed text rendered before/after this node's content, in the node's own style. Not inherited by children.",
    helpPadding:
        "Spaces around this node: padding sets both sides; left/right override individually.",
    helpOptional:
        "Show this node only while the named field has data. Missing, empty values hide it; 0 and false count as present.",
    helpWhen:
        'Condition expression, e.g. "context-percent ge 80". Word operators lt le gt ge eq ne with and/or/not and parentheses. An unresolvable metric hides the node.',
    helpColorRules:
        "Recolor this node by condition (self = its own metric). Rules are checked top to bottom and the first match wins; the Color above is the fallback when none match.",
    helpFormat: "Formatter applied to the field value.",
    helpPrecision: 'Formatter precision (digits, or "adaptive" for currency).',
    helpRole:
        'Marks this text as a collapsing segment boundary. In Powerline output, an empty separator becomes the transition between adjacent segments; explicit text is preserved.',
    colorRulesEmpty: "No color rules yet.",
    addRule: "+ Add rule",
    helpWidth: "Simulated terminal width (COLUMNS) used for the preview.",
    helpSample:
        "Samples are fake data used only for the preview. Full data = every field has data; Session start = a brand-new session where usage and cost data have not arrived yet.",
    sampleFull: "Full data",
    sampleEarly: "Session start (no data yet)",
    // live monitor
    liveMonitorTitle: "Live monitor",
    liveMonitorIntro:
        "Watch a real coding-agent session update the preview live as it runs, instead of using a sample.",
    liveMonitorStart: "Start live monitor",
    liveMonitorStarting: "Starting…",
    liveMonitorRetry: "Retry",
    liveMonitorRunHint: "Run this command in another terminal to launch the monitored session:",
    liveMonitorCopy: "Copy",
    liveMonitorCopied: "Copied!",
    liveMonitorStop: "Stop live monitor",
    liveMonitorStatusWaiting: "waiting for monitor session…",
    liveMonitorStatusConnected: "connected — waiting for monitor session…",
    liveMonitorStatusActive: "monitor active",
    // live monitor mode tabs
    liveModeSelf: "Run it yourself",
    liveModeEmbedded: "Embedded terminal",
    // embedded terminal
    embeddedTerminalWarning:
        "This launches and controls a real Claude Code process inside a temporary directory in your environment, right here in the browser.",
    embeddedTerminalStart: "Start embedded session",
    embeddedTerminalStop: "Stop",
    embeddedTerminalRestart: "Restart",
    embeddedTerminalTitle: "Embedded session",
    embeddedTerminalHide: "Hide terminal (keeps the session running)",
    embeddedTerminalShow: "Show terminal",
    embeddedTerminalConnecting: "connecting…",
    embeddedTerminalConnected: "session running",
    embeddedTerminalClosed: "The session has ended.",
};

const JA: Record<MessageKey, string> = {
    paletteHint: "クリックでアクティブな行に追加、ドラッグでプレビュー内に配置できます。",
    propertiesEmptyHint: "プレビュー内のチップをクリックすると編集できます。",
    removeHint: "Delete / Backspace でも削除できます",
    canvasFooterHint:
        "チップをクリックで選択、ドラッグで並べ替え・行間の移動ができます。Esc で選択解除、Delete で削除。",
    ghostHiddenReason: "現在のサンプルでは出力がありません",
    zeroDisabledHint: "0 = 無効",
    dropHere: "ここにドロップ",
    omittedBadge: "この幅では省略されます",
    rendering: "描画中…",
    noPreview: "プレビューはまだありません。",
    layoutTabHint: "クリックでこのレイアウトを編集、ダブルクリックで名前変更。",
    layoutActive: "● active",
    layoutSetActive: "○ set active",
    layoutActiveTip: "このレイアウトが実際のステータスラインで使われます。",
    layoutSetActiveTip: "このレイアウトを実際のステータスラインで使うよう設定します。",
    layoutAddTip: "新しいレイアウトを追加",
    layoutDuplicate: "複製",
    layoutDuplicateTip: "編集中のレイアウトを複製",
    layoutDeleteTip: "このレイアウトを削除",
    fallbackNote:
        "このレイアウトは情報が乏しいとき（セッション開始直後など）に出力が空になり、実際には次が表示されます:",
    dslEditorHint:
        "このドキュメントの DSL ソースです。ここでの編集とビジュアルエディタは相互に同期します。",
    dslInvalid:
        "DSL にエラーがあります。プレビューは最後に正常だった状態を表示しています。エラーを修正するまでビジュアル編集と保存はできません。",
    dslNoProblems: "問題はありません。",
    saveBlocked: "保存できません: ドキュメントにエラーがあります。",
    settingsTitle: "Git設定",
    helpGit: "git 情報収集の設定です（省略可能な <git/> 要素）。未設定の項目は既定値が使われます。",
    helpFlex:
        "このフレックスノードが端末幅を埋める方法。full = 全幅まで埋める、full-minus-N = N 桁分を残して埋める。1 行に複数のフレックスノードがありサイズが異なる場合は、最小のターゲットが適用されます。",
    helpCompactThreshold:
        "COLUMNS がこの幅を下回るとフィールドがコンパクト表示になります。0 で無効。",
    helpColorLevel:
        "出力の色深度。端末が対応していない場合は自動的にダウングレードされます。",
    helpContextMode:
        "コンテキスト使用率の計算方式。raw = ウィンドウ全体に対する割合、usable = 予約トークンを除いた実効ウィンドウに対する割合、both = 両方を表示。",
    helpReserveTokens: "usable 計算時にウィンドウから予約しておくトークン数です。",
    helpColor: "前景色。ANSI 色名（ケバブケース）またはカスタム HEX 値を指定します。",
    helpBackground: "背景色。ANSI 色名（ケバブケース）またはカスタム HEX 値を指定します。",
    helpDecorations:
        "文字装飾。未指定は親から継承し、on/off を指定すると継承値を上書きします。",
    helpRawValue: "ラベルや整形なしの生値を出力します（コンパクト表示もスキップされます）。",
    helpHyperlink:
        "このフィールドを端末ハイパーリンク（OSC 8）で囲み、対応端末でクリック可能にします。",
    helpPrefixSuffix:
        "このノードの内容の前後に付く固定テキストです。ノード自身のスタイルで描画され、子には継承されません。",
    helpPadding:
        "ノードの左右に入るスペース。padding は両側共通、left/right は個別に上書きします。",
    helpOptional:
        "指定したフィールドにデータがある間だけこのノードを表示します。欠落・空値では非表示になり、0 や false は「あり」として扱われます。",
    helpWhen:
        '条件式です。例: "context-percent ge 80"。ワード演算子 lt le gt ge eq ne と and/or/not、括弧が使えます。メトリクスが解決できない場合、ノードは非表示になります。',
    helpColorRules:
        "条件に応じて色を変えます（self = このノード自身のメトリック）。ルールは上から順に評価され、最初に一致したものが優先されます。どれも一致しない場合は上の Color がフォールバックになります。",
    helpFormat: "フィールド値に適用するフォーマッタです。",
    helpPrecision: '桁数などのフォーマッタ精度です（currency では "adaptive" も指定可）。',
    helpRole:
        '自動折りたたみされるセグメント境界として扱います。Powerline出力では空のseparatorが隣接セグメント間の遷移になり、文字を指定した場合はその文字を維持します。',
    colorRulesEmpty: "カラールールはまだありません。",
    addRule: "+ ルールを追加",
    helpWidth: "プレビューで再現する端末幅（COLUMNS）です。",
    helpSample:
        "サンプルはプレビュー専用の擬似データです。全データあり = すべてのフィールドにデータがある状態、セッション開始直後 = 使用量やコストのデータがまだ届いていない状態。",
    sampleFull: "全データあり",
    sampleEarly: "セッション開始直後（データ未取得）",
    liveMonitorTitle: "ライブ監視",
    liveMonitorIntro:
        "サンプルの代わりに、実際のコーディングエージェントのセッションが動いている様子をリアルタイムでプレビューに反映します。",
    liveMonitorStart: "ライブ監視を開始",
    liveMonitorStarting: "起動中…",
    liveMonitorRetry: "再試行",
    liveMonitorRunHint: "別のターミナルで次のコマンドを実行すると、そのセッションが監視されます:",
    liveMonitorCopy: "コピー",
    liveMonitorCopied: "コピーしました！",
    liveMonitorStop: "ライブ監視を停止",
    liveMonitorStatusWaiting: "監視セッションの開始を待っています…",
    liveMonitorStatusConnected: "接続済み — 監視セッションの開始を待っています…",
    liveMonitorStatusActive: "監視中",
    liveModeSelf: "自分で起動",
    liveModeEmbedded: "埋め込み端末",
    embeddedTerminalWarning:
        "この機能は、あなたの環境の一時ディレクトリ内で実際の Claude Code プロセスをブラウザ内から起動・操作します。",
    embeddedTerminalStart: "埋め込みセッションを開始",
    embeddedTerminalStop: "停止",
    embeddedTerminalRestart: "再起動",
    embeddedTerminalTitle: "埋め込みセッション",
    embeddedTerminalHide: "端末を隠す（セッションは維持されます）",
    embeddedTerminalShow: "端末を表示",
    embeddedTerminalConnecting: "接続中…",
    embeddedTerminalConnected: "セッション実行中",
    embeddedTerminalClosed: "セッションが終了しました。",
};

export type MessageKey = keyof typeof EN;

export const MESSAGES: Record<Lang, Record<MessageKey, string>> = { en: EN, ja: JA };

export function t(lang: Lang, key: MessageKey): string {
    return MESSAGES[lang][key] ?? MESSAGES.en[key];
}

// Field/metric descriptions come from the catalog's per-language map; fall
// back to English, then to empty.
export function pickDescription(
    entry: { descriptions?: Record<string, string> } | undefined,
    lang: Lang,
): string {
    if (!entry?.descriptions) {
        return "";
    }
    return entry.descriptions[lang] ?? entry.descriptions.en ?? "";
}

export const I18nContext = createContext<Lang>("en");

export function useLang(): Lang {
    return useContext(I18nContext);
}
