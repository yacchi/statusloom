# Statusloom

Claude Code等コーディングエージェント向けステータスラインツール。Go単一バイナリ + React設定UI。
モジュール `github.com/yacchi/statusloom`（GitHubユーザー yacchi）。

**詳細仕様は `statusloom-local-development-plan.md`（改訂済み・正）を参照。** 本ファイルは差分・運用ルールのみを記載し、仕様の重複記載はしない。README.mdはユーザー向け利用説明。**設定DSL（XMLマークアップ）は `markup.md` が正（計画書の設定スキーマ記述より優先）。** 旧Widget列パイプライン（JSON `config.json` / `WidgetSpec` / `/api/config`系）は削除済み。設定の唯一のソースはDSLドキュメント（`<tool>.xml`）で、旧 `config.json` はmigration入力としてのみ残存。

## 開発環境・コマンド

- ツールチェーンは `mise.toml` でpin（Go 1.25.8 / Node 22 / pnpm 10）。`mise install` を先に実行
- Go標準ライブラリのみ使用（例外: webconfigのWebSocketに限り `github.com/coder/websocket`、webconfigの埋め込み端末に限り `github.com/creack/pty` を許可）。フロントの外部UI依存はdnd-kitのみ許可
- モノレポ構成: `cmd/statusloom`, `internal/{adapters,cache,cli,config,detect,gitstatus,render,schema,webconfig}`, `apps/configurator`（React設定UI）。詳細は計画書 4章
- 動作確認:
  - `go run ./cmd/statusloom config`（設定UI起動。`mise run config`）
  - `./statusloom claude < fixtures/claude/full.json`（描画確認。`mise run render`）
  - 検証時は必ず `STATUSLOOM_CONFIG` / `STATUSLOOM_CACHE_DIR` を設定し実configを隔離すること
- webビルド: `./scripts/build-web.sh`（`apps/configurator/dist` → `internal/webconfig/dist` にコピーしembed。`mise run build-web`）。**ビルド成果物はコミット禁止**。コミット前に `./scripts/clean-web.sh` でplaceholderへ復元（`mise run clean-web`）
- 完了条件（PR/コミット前に全て通すこと。まとめて `mise run check`）:
  - `gofmt -l .` が空、`go vet ./...`（`mise run lint`）
  - `go test ./...` 全パス（`mise run test` はこれと下記フロントtestを順に実行）
  - `pnpm --filter @statusloom/configurator build && pnpm --filter @statusloom/configurator test`（`mise run web-build` / `mise run test`）

## Claude Code statusline 仕様（検証済み・再調査不要）

- stdin JSONに `context_window`（used_percentage等）・`rate_limits.five_hour/seven_day`（epoch秒、Pro/Maxプランのみ・初回API応答後に出現、各windowは独立に欠落しうる）・`effort.level` が全て含まれる。**transcriptファイルの解析は不要**
- 端末幅は環境変数 `COLUMNS`/`LINES` から取得（stdoutは端末非接続のため `tput` 不可）。**これらの環境変数はv2.1.153以降でのみ渡される。** v2.1.132〜2.1.152ではflex-separatorの幅展開が効かず1スペース固定になり、`compactThreshold` も発動しない（劣化動作。README・計画書9章に明記済み）。複数行出力に対応する
- 更新はイベント駆動+300msデバウンス。トリガーはassistantメッセージ後・`/compact`完了後・permission mode変更・vim mode切替など。実行中のレンダーコマンドはキャンセルされ得る。アイドル中は更新イベントが来ないため、countdown系widget（five-hour-reset/weekly-reset）を使う場合は`statusloom setup claude-code --refresh-interval`の設定を推奨
- `context_window.exceeds_200k_tokens` はcontext window sizeによらず固定200k閾値での判定。`used_percentage` はinput系トークンのみの計算（output tokenは含まない、公式仕様）
- サポート最低バージョンは **v2.1.132**（それ以前は `total_input_tokens` が累積値でセマンティクスが異なる）

## 確定済み設計判断（覆さない・再議論不要）

- `statusloom refresh` ワーカーは保留。rate_limitsの取得元はstdinのみで、レンダーパスはnetwork-free
- accountキャッシュキーは固定文字列 `default`。キャッシュ補完時、`ResetsAt` が過去のwindowは使わない。lock/leaseなし（atomic write + last-writer-wins）
- パスはmacOSでもXDG流（`~/.config/statusloom`, `~/.cache/statusloom`）。テスト/検証は env `STATUSLOOM_CONFIG` / `STATUSLOOM_CACHE_DIR` で必ず隔離する
- **旧方式（削除済み・復活禁止）**: JSON `WidgetSpec`（`template`/`prefix`/`suffix`/`showWhen`/`colorRules`）による1次元Widget列レンダリング、`render.RenderSegments`/`Render`、`config.Validate`/`Load`、`config.draft.json`。設定は全てDSLドキュメント（`<tool>.xml`）へ移行済み。`Config`/`ToolConfig`/`WidgetSpec`/`Layout` 型と `config.Load*Legacy`/`MigrateFromLegacy` はmigration経路が必要とする範囲でのみ残す（migrationから到達不能な関数・validate・defaults項目は削除済み）
- **DSL設計（markup.mdが正）**: XMLマークアップ（`<statusloom>`/`<layout>`/`<line>`/`<span>`/`<field>`/`<text>`/`<flex/>`）。区切りは `<text role="separator">`（leading/trailing/連続は自動collapse、compact時はpaddingを落として `|` に短縮）。装飾は `prefix`/`suffix`/`padding` 属性＋文字装飾（color/background/bold/dim/italic/underline/strikethrough、nearest-wins継承）を第一級で採用。`<flex size="">`（`""`=full / `full-minus-N`、同一行複数はmin採用）。条件表示は `optional="<field>"`（値存在ゲート）＋ `when="<expr>"`（ワード演算子 `lt|le|gt|ge|eq|ne`、`self`＝自fieldのメトリック、`git-dirty`等の名前付きメトリック）。色分岐は `<color-rule when=".." color=".."/>`（selfメトリックで先勝ち評価、非該当は基底色にフォールバック）。色名はkebab-case（`bright-black` 等）。`hyperlink`（linkable fieldのみ）。formatter＝`format="percent|number|compact-number|currency|duration|countdown|enum"`＋`precision`
- `5h:`/`7d:` 等のラベルはレンダラーに焼き込まない。DSLでは `<span prefix="5h: " optional="five-hour-usage">` のように明示する（migration/importが自動付与）
- UIはTUI的操作禁止（ドロップダウン追加・移動ボタン等は不可）。パレット + dnd-kit DnD + プレビュー直接操作のみ。選択状態でチップ幅が変わる装飾は禁止
- dnd-kitの `onDragOver` 中にドキュメント構造を変更すると React #185 無限ループを起こす。`apps/configurator/src/useDragEditing.ts` 参照。ドラッグ中は不変にし、drop時のみ変更すること
- i18n: 説明・ヘルプのみ日英翻訳（カタログの `descriptions` は `{en, ja}`）。Widget名・ボタンラベルは英語のまま

## 主要契約（フロント・バック境界。破壊的変更時は両側を同一変更内で更新）

- レンダラ: `render.RenderDocument(snap, *dsl.Document, opts) []DocLine` — `DocSegment{Node(dsl.Node), Text, ANSI, Visible}`, `DocLine{Omitted, Segments}`。leafノードと1:1（decorationは所有ノードに帰属、fallback行はNode=nil）。`render.RenderDocumentString` は非omitted行を結合し、全omitted時は `RenderFallback`（model + tool-version）を返す。共有プラミング（`renderContent`/`metricValue`/`markSeparators`/`computeFlexWidths`/`RenderFallback`/color.go/format.go/`piece`）はDSL経路から利用
- 設定API: `/api/dsl/*` のみ（旧 `/api/config`・`/api/preview`・`/api/tools/{tool}/widgets|metrics`・`/api/draft`（config.draft.json版）は削除済み）。契約は `internal/webconfig/DSL_API.md`。document/draft/parse/serialize/preview/fields/metrics を提供。preview segmentは `nodeId`（`/api/dsl/parse` のAST node IDと対応）で参照。加えて `GET /api/tools`・`GET /api/sessions`・live/terminal channels・`POST /api/shutdown`
- fieldカタログ: `internal/dsl` レジストリ（`FieldByName`/`Fields`/`MetricByName`/`Metrics`＝`FieldDef`/`MetricDef`、表示メタ`{DisplayName, Descriptions{EN,JA}, Category, SelfMetric, Linkable, Formats}`）が **単一情報源**。DSL validation・`GET /api/dsl/fields|metrics`・レンダラのfield metadata解決が全てここを参照（旧 `config/metrics.go`・`webconfig/catalog.go` のwidget表は削除済み）
- schema: `StatusSnapshot.PullRequest`、`SystemSnapshot.Worktree`/`Repo`、`SessionSnapshot.Name`/`AgentName`/`VimMode`、`CostUsage`（USD + Duration + APIDuration + LinesAdded/Removed）、`ContextUsage.Current`（TokenBreakdown、初回API応答前と`/compact`直後はnil）。詳細は計画書7章
- 設定ファイル配置: 保存済み設定＝`<configDir>/<tool>.xml`（`config.DocumentPath`）。git設定はドキュメント内 `<git/>` 要素（`config.DocumentGitConfig`）。旧 `config.json` はmigration入力のみ（`autoMigrate` が初回レンダー時に `MigrateFromLegacy` で `<tool>.xml` を生成、原本は残置）
- draft共有: 未保存編集は `<configDir>/<tool>.draft.xml`（`config.DraftDocumentPath`）にLWW共有（DSL生テキスト）。`GET /api/dsl/draft`→`{source, version, exists}`、`PUT /api/dsl/draft`→`{version, diagnostics}`（`version`＝ソーステキストのsha256 hex `sourceVersion()`、GET/PUT共通・エコーガード用。draftはparse失敗も許容し無条件保存）。保存（`<tool>.xml`書込）は `PUT /api/dsl/document` のみ（error診断があれば409で拒否）。`statusloom draft pull|push [file]`（ローカルのみ・token不要）と `statusloom monitor --draft`（draft優先で描画、無効時は保存済みへフォールバック）が同ファイルを読み書き。monitorワークスペース（`provisionMonitorDir`）はstatusLineに`--draft`付与＋CLAUDE.md/sample.json生成＋best-effort `git init`初期コミット
- webconfigセキュリティ: バインドは127.0.0.1のみ、`/api/*` にBearer token必須、Host/Origin検証、cookie不使用

## コード変更ルール

- 削除は完全に行う: 削除したシンボルをgrepし参照ゼロを確認、未使用import/型/定数とそのテストも除去。旧実装と新実装を並存させない
- golden test（`internal/render/testdata`）はリグレッションガード。挙動を変えないリファクタで `-update` は禁止（byte一致で証明する）。意図的な出力変更はテスト設定側で吸収できないか先に検討する
- プレリリースにつき後方互換性は不要。破壊的スキーマ変更もOK
- 単一の情報源を保つ: 型/utilを追加する前に既存定義を検索。共有定義を変更する場合は全利用箇所を同一変更内で更新

## Git運用

- コミット・push等のgit状態変更はユーザーの明示指示があるまで禁止
- 新規ファイルは `git add` のみ即実行してよい
- 現状: リポジトリはまだ初回コミット前（`git log` に履歴なし）。ほぼ全ファイルがステージ済み

## 体制・進め方（ユーザー指示）

- コーディネータ（Fable）は指示・レビュー・読み取り検証のみを行う。実装・ドキュメント編集は必ずOpus/Sonnet/Haikuのサブエージェントに委譲する
- Codex利用時: `codex exec --full-auto` の直接実行は権限拒否されるため、codexプラグインのrescueエージェント経由で呼び出す（モデル指定はユーザー指示に従う。前回はgpt-5.6-sol）。進捗は `codex-companion.mjs` の `status`/`result` で監視する。Codexサンドボックスは`.git`に書き込めないため、`git add` は本体（コーディネータ）側で行う
- 複数エージェントが並行編集する場合、担当ディレクトリ外のファイルには触れない

## 現況と残作業

- v0.1必須コマンド（`claude` / `config` / `setup claude-code` / `import ccstatusline` / `doctor` / `version`）は実装済み。`draft` / `monitor` も実装済み
- 設定UIはユーザーフィードバックを3ラウンド反映済み。Visual Editor＋DSL Editorの両モード
- **設定DSL移行完了**: 旧Widget列パイプライン（render/config/webconfigの旧経路・旧テスト・旧golden）を完全削除し、DSL（`markup.md`）が唯一の設定モデル。migration経路のみ旧 `config.json`/`WidgetSpec` を読む
- **全変更が未コミット**（初回コミット待ち）
- DSL関連の残作業: Phase 3（AST→ソースの minimal-diff シリアライズ＝コメント・整形の保存、`dsl fmt` 相当）。現状 `/api/dsl/serialize` は全文正規化形を返す
- 残マイルストーン（計画書25章）: Milestone 6（クロスビルド・GitHub Release。Windows向け`statusLine.command`パスはスラッシュ区切り必須 — Git Bashがバックスラッシュをエスケープ文字として消費するため）、Milestone 7（Codex/Copilotアダプター）、Milestone 8（Statusloom Room）
- 将来候補: `subagentStatusLine`対応（サブエージェントパネル行の差し替え。stdinで`tasks`配列を受け`{"id","content"}`のJSON行を出力する別プロトコル。per-taskの`model`/`contextWindowSize`はv2.1.205+）
- 進行中の作業がある場合は `git status` と直近のタスク状況から状態を判断すること
