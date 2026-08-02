# Statusloom

Statusloom はコーディングエージェント向けの高速・ポータブルなステータスライン
ツールキットです。Claude Code、Codex、GitHub Copilot などのステータスラインを
組み立て、プレビューし、インストールし、共有できます。Go の単一バイナリとして
配布され、描画パスはネットワークに一切アクセスせず、ビジュアルな設定 UI を同梱
しています。

*English version: [README.md](README.md)*

![Statusloom の設定 UI: フィールドパレット、直接編集できるライブプレビュー、選択中フィールドのプロパティ](docs/media/properties.png)

プレビュー**そのものがエディタ**です。プレビュー上のフィールドをクリックすれば
そのフィールドを編集でき、ドラッグで並べ替えられ、実際に描画される出力がその場で
変化します。

![フィールドを追加し、装飾を変え、端末幅を狭めて行の変化を見る](docs/media/editor.gif)

## 状況

Statusloom は開発初期です。v0.1 は Claude Code を対象にしています。

**要件**: Claude Code v2.1.132 以降（`context_window` のトークン意味論が正しい
バージョン）。flex セパレータの幅解決と `compactThreshold` はさらに v2.1.153
以降が必要です — Claude Code がステータスラインコマンドへ `COLUMNS`/`LINES` を
渡すようになったのがこのリリースだからです。v2.1.132〜v2.1.152 では flex
セパレータは 1 スペースに退化し、compact モードは発動しません。

## インストール

### Homebrew（macOS / Linux、推奨）

```sh
brew install yacchi/tap/statusloom
```

Windows では下記の GitHub Releases のアーカイブを使ってください。

### GitHub Releases（手動）

macOS・Linux・Windows（amd64 / arm64）向けのビルド済みアーカイブを配布して
います。[Releases ページ](https://github.com/yacchi/statusloom/releases) から
自分のプラットフォーム向けアーカイブをダウンロードし、展開して `statusloom`
バイナリを `PATH` 上に置いてください。

### ソースから（開発者向け）

クローンからビルドすると Web 設定 UI が埋め込まれます（`go install ...@latest`
では埋め込まれません）。ツールチェーンは `mise.toml` で固定されている（Go /
Node / pnpm）ので、まず [mise](https://mise.jdx.dev/) を入れてから 1 コマンド
です:

```sh
git clone https://github.com/yacchi/statusloom.git
cd statusloom
mise run install   # Go/Node/pnpm の導入・UI のビルド・statusloom のインストール
```

`mise run install` は固定バージョンのツールチェーンを自動導入し、設定 UI を
ビルドして埋め込み、`go install ./cmd/statusloom` を実行します。ローカルの
作業ツリーからビルドするため、`internal/webconfig/dist` にビルドされた UI が
バイナリに埋め込まれます。（クローンなら動くのに `go install ...@latest` では
動かない理由がこれです: 埋め込みアセットは git 管理外でコミットされません —
`CLAUDE.md` 参照。）Windows でも動作しますが、Claude Code 自体の Windows
サポートはまだ十分ではありません。

インストールせずチェックアウトから直接動かす場合、`mise run build` が
リポジトリルートに `./statusloom` を生成し、`mise run config` が設定 UI を
起動します。

### インストール後

statusloom を Claude Code のステータスラインコマンドとして登録します:

```sh
statusloom setup claude-code
```

これは `~/.claude/settings.json` に `statusLine.command` を書き込みます
（同等の手書き JSON は下記[使い方](#使い方)を参照）。`CLAUDE_CONFIG_DIR` で
プロファイルを切り替えている場合はそれに追随し、
`$CLAUDE_CONFIG_DIR/settings.json` に書き込みます。`--settings <path>` は
常に両者を上書きします。

## 使い方

### Claude Code のステータスライン

`settings.json` に statusloom をステータスラインコマンドとして追加します:

```json
{
  "statusLine": {
    "type": "command",
    "command": "statusloom claude"
  }
}
```

Claude Code はプロンプトごとにこのコマンドを起動し、セッション JSON を stdin に
流します。`statusloom claude` は設定されたステータスラインを stdout に描画します。

### サブエージェントのステータスライン

Claude Code の `subagentStatusLine` 設定は、エージェントパネル内で実行中の
サブエージェント 1 件につき 1 行を描画します。Statusloom は専用コマンドで
これを実装しています:

```json
{
  "subagentStatusLine": {
    "type": "command",
    "command": "statusloom claude-subagent"
  }
}
```

Claude Code はサブエージェントのタスク配列を JSON で stdin に流し、
`statusloom claude-subagent` はタスクごとに `{"id", "content"}` の JSON 行を
stdout に書きます。サブエージェント行は**同じ `claude-code` ドキュメントの中の
`<subagent>` 要素**として設定します（別ドキュメントではありません。
[設定](#設定)参照）。使えるのは専用のフィールド群 — `task-description`、
`task-model`、`task-model-id`、`task-tokens`、`task-context-size`、
`task-context-percent`、`task-status`、`task-duration`（[フィールド](#フィールド)
参照）。`--draft` を付けると保存済みではなく共有 draft を描画します
（`statusloom claude` の `--draft` と同じ扱い）。monitor ワークスペースは
これを使って未保存のサブエージェント行の編集をプレビューします。`--preview` を
付けると stdin を読まずに組み込みの代表的ペイロード（サンプルタスク数件）を
描画するので、`tasks[]` の JSON を手で書かずに行を確認できます。`--draft` と
併用できます。

**プロトコル上の制限:** `subagentStatusLine` の stdin ペイロードには
サブエージェントごとの reasoning effort やエージェント種別名
（`general-purpose` など）が含まれていないため、Claude Code 自身の既定行が
先頭に出すエージェント種別ラベルは再現できません。利用できるのはモデル・
トークン・コンテキスト・所要時間・状態のみです。前方互換のために
`task-effort` フィールドはカタログに登録されていますが、現状では常に
利用不能です。

### 設定 UI

```
statusloom config [--port N] [--no-browser]
```

ローカル Web UI（`127.0.0.1` のみにバインド）を起動し、設定の閲覧・編集・
プレビューができます。同一ドキュメントに対して 2 つの編集モードを提供します:
**Visual Editor**（パレットからフィールドをドラッグし、ライブプレビュー上で
直接編集）と **DSL Editor**（XML マークアップをテキストとして編集、診断と
プレビューはライブ）。既定では空きポートをランダムに選んでブラウザを開きます。
`--port` で特定ポートを指定、`--no-browser` でブラウザを開かないようにできます。
サーバは URL（ワンタイム認証トークンを含む）を stdout に出力し、`Ctrl-C`・
アイドル経過・UI からの終了操作でシャットダウンします。

どちらのエディタも 1 つのドキュメントを扱うので、ビジュアルに組み立てて
生成されたマークアップを読むこともできますし、その逆もできます:

![ビジュアルエディタと DSL エディタを並べ、同じドキュメントを表示している様子](docs/media/dsl-editor.png)

`<responsive>` コンテナは複数のレイアウト候補を保持します。現在の幅で選ばれて
いる候補にはエディタが印を付け、さらに各 variant には条件を持たせられます
（例: Team アカウントのときだけ）:

![2 つの variant カードを持つ responsive コンテナ。一方は account-type で条件付け](docs/media/responsive.png)

#### 変更履歴

保存するたびにリビジョンが作られます。History パネルは新しい順に一覧し、
任意のリビジョンと現在のドキュメントとの diff を表示し、復元できます —
レイアウトを試すコストがゼロになります。

![リビジョン一覧と、選択したリビジョンの DSL diff を表示する History パネル](docs/media/history.png)

#### 自分の実セッションでのライブプレビュー

上記のサンプルは合成データです。ライブモニタは**実際の**セッションスナップ
ショットが届くたびにステータスラインを描画するので、作られた数値ではなく
自分のリポジトリ・使用量・コストでレイアウトの挙動を確認できます。監視したい
端末で実行するコマンドを表示します:

![セッションを待っているライブモニタと、実行するコマンド](docs/media/live-monitor.png)

#### Claude Code 自身によるカスタマイズ

「Start embedded session」は設定 UI の中に端末を開き、statusloom が用意した
ワークスペースで Claude Code を起動します。そのワークスペースには DSL と
現在のドキュメントを説明する専用の `CLAUDE.md`、サンプルの stdin ペイロード、
`--draft` を付けた statusLine が揃っています。つまり欲しいものを言葉で頼めます
（「git ブランチを右端に置いて、端末が狭いときはコストを落として」）。その編集は
共有 draft に入るので、ビジュアルエディタとプレビューが即座に映します。
ステータスラインは組み立てるより説明する方が速いことがあり、同じドキュメントに
対して両方の経路を保てます。

### セットアップ

```
statusloom setup claude-code
statusloom setup claude-code --refresh-interval 60
```

Claude Code が `statusloom claude`（`statusLine`）と
`statusloom claude-subagent`（`subagentStatusLine`）を実行するよう設定します。
既存の設定はバックアップされ、別のステータスラインコマンドを置き換える場合は
確認を求めます。

`--refresh-interval <秒>` は Claude Code 自身の `refreshInterval` 設定
（最小 1 秒）を、`statusLine` と `subagentStatusLine` の両方に対して設定します。
Claude Code はアイドル中は新しいステータスラインイベントを発行しないため、
`five-hour-reset` や `weekly-reset` のようなカウントダウン系ウィジェットは
次のイベントまで止まります。そうしたウィジェットを設定する場合はこのフラグを
使ってください。カウントダウン系ウィジェットが設定されているのに
`refreshInterval` が未設定なら `statusloom doctor` が警告します。

### フォーマット

```
statusloom fmt [file] [--check]
```

DSL ドキュメントを正規形に書き換えます — 属性順・自己終了タグ・インデント、
そして `when` 式をすべてワード形式（`and`・`or`・`lt`・`ge` …）に正規化します。
引数なしなら保存済みの `claude-code` ドキュメントを、`-` なら stdin を読んで
stdout に書きます。`--check` は書き込まずに、整形でドキュメントが変わるか
どうかを報告します（変わる場合は非ゼロ終了）。設定 UI からの通常の保存は
触っていないノードの元の整形を保つので、`fmt` は明示的に呼ぶ全文正規化です。

### 診断

`statusloom doctor` はバイナリ・ステータスラインドキュメント・キャッシュ・
Git・Claude Code のセットアップを検査します — カウントダウン系フィールド
（`five-hour-reset`・`weekly-reset`）が設定されているのに `refreshInterval`
が未設定かどうかも含みます。

## フィールド

Statusloom は Claude Code 向けに、モデル・コンテキスト使用量・コスト・Git の
状態・レート制限などを網羅した組み込みフィールドのカタログを備えています。
`<field>` は元データが利用できないとき自動的に自身を隠します。

最近追加されたもの:

- `session-name` — `--name` や `/rename` で設定したセッション名
- `agent-name` — `--agent` で起動した場合の実行中エージェント名
- `vim-mode` — vim モードが有効なときの現在のモード。このウィジェットを使う
  場合は Claude Code の設定で `hideVimModeIndicator: true` にしてモードの
  二重表示を避けてください
- `pr-number` — 現在のブランチのオープンな PR（例: `#1234`）。マージ or
  クローズされると隠れます
- `pr-review-state` — `approved` / `pending` / `changes_requested` / `draft`
- `repo-name` — `origin` リモートから導出した `owner/name`
- `worktree` — 現在の Git worktree 名
- `session-duration` / `api-duration` — 経過実時間 / API 時間。`1h 15m` の形式
- `lines-changed` — セッション内の追加/削除行数。`(+156,-23)` の形式
- `cache-hit-rate` — 直近の API 呼び出しにおけるプロンプトキャッシュのヒット率
- セッション/モデルの状態: `session-id`、`model-id`、`output-style`、
  `thinking-enabled`
- コンテキスト詳細: `context-window-size`、`context-remaining`、
  `context-output-tokens`、`current-input-tokens`、`current-output-tokens`、
  `cache-creation-tokens`、`cache-read-tokens`、`exceeds-200k`
- リポジトリ詳細: `project-directory`、`git-root`、`git-staged`、
  `git-unstaged`、`git-untracked`、`git-ahead`、`git-behind`、`git-clean`
- `lines-added` / `lines-removed` — セッションの追加・削除行数を個別に

`<subagent>` 領域（[サブエージェントのステータスライン](#サブエージェントのステータスライン)
参照）は、サブエージェント 1 タスクにスコープされた専用フィールドのみを
受け付けます: `task-description`、`task-model`、`task-model-id`、
`task-tokens`、`task-context-size`、`task-context-percent`、`task-status`、
`task-duration`。`task-effort` も登録されていますが、Claude Code の
`subagentStatusLine` プロトコルがサブエージェントごとの reasoning effort を
公開していないため、現状は常に利用不能です。

### 条件付き表示と色

どのノードも `when="..."` 式で条件付き表示にできます（フィールドは
`optional="<field>"` で自身のデータ有無をゲートにできます）。また
`<color-rule when="..." color="..."/>` の子要素でしきい値による色分けが
できます。条件はワード演算子（`lt le gt ge eq ne`）を使い、`self`
（そのフィールド自身のメトリクス）または名前付きメトリクス（コンテキスト
使用量、レート制限の割合とリセットまでのカウントダウン、セッションコスト、
所要時間、変更行数、キャッシュヒット率、`git-dirty`、トークンの内訳、
Git の詳細カウント、端末の `width`）を対象にできます。真偽値メトリクスには
`thinking-enabled`、`exceeds-200k`、`git-clean` があります。`width` は
端末幅（桁数）で、`when="width ge 80"` のようなブレークポイントに使えます。
幅を報告しないホストでは `width` は無制限として解決されるので、幅が不明な
ときに幅ブレークポイントが内容を隠すことはありません。完全な構文は
`markup.md` を参照してください。

### レイアウト

Claude Code はシステム通知（MCP エラー、更新の案内）を、verbose モードでは
さらにトークンカウンタをステータスラインの右側に重ねて表示します。自分の
内容が衝突しないよう、`full` より `<flex size="full-minus-N"/>` を優先して
ください。

### ハイパーリンク

`pr-number`・`pr-review-state`・`repo-name` フィールドに
`hyperlink="true"` を設定すると OSC 8 ハイパーリンクとして描画されます
（PR 系は PR の URL、repo は `https://<host>/<owner>/<name>` へリンク）。
`colorLevel` が `none` のときはリンクを付けません。iTerm2・Kitty・WezTerm
などの端末で機能します。端末が対応を通知しない場合は `FORCE_HYPERLINK=1` を
試してください。tmux 内や SSH 経由ではリンクが除去されることがあります。

### 従量課金（extra usage）とモデル別の週次上限

Claude のサブスクリプションプランで従量課金の超過分（extra usage）が
有効になっている場合、statusloom はその支出額とモデル別の週次レート制限
使用量をステータスラインに表示できます:

| フィールド | 表示内容 | フォーマット |
|---|---|---|
| `extra-usage-cost` | 現在の請求期間の従量課金額（USD） | `currency` |
| `extra-usage-limit` | 設定されている月次の従量課金上限（USD） | `currency` |
| `extra-usage-percent` | 従量課金の月次上限に対する消費率 | `percent` |
| `weekly-usage-opus` | 7 日間レート制限のうち Opus 系が使った割合 | `percent` |
| `weekly-usage-sonnet` | 7 日間レート制限のうち Sonnet 系が使った割合 | `percent` |
| `weekly-reset-opus` | Opus の 7 日間ウィンドウのリセットまで | `countdown` |
| `weekly-reset-sonnet` | Sonnet の 7 日間ウィンドウのリセットまで | `countdown` |

```xml
<span prefix="overage: " optional="extra-usage-cost">
    <field name="extra-usage-cost" format="currency"/>
</span>
```

**`extra-usage-cost` に値が出るのは、使用クレジットを有効化し*かつ*
サブスクリプションの上限を超えた場合のみ**です。上限内のサブスクリプション
利用者では空のままです。これは現在の請求期間における従量課金の実費であり、
セッションの見積りではありません。セッション単位のコスト見積りは
`session-cost` が別途担います。

これらのフィールドは Claude Code 自身の OAuth usage エンドポイント
（Claude Code の `/usage` コマンドが使う、現時点で非公開の API）から取得され、
ステータスラインの stdin ペイロード由来ではありません。描画パスを
ネットワークフリーに保つため、この呼び出しは statusloom が機会的に spawn する
短命なバックグラウンドの `statusloom refresh --once` サブプロセスの中だけで
行われます（デーモンではなく単発プロセス）。独自のスケジュール（既定 5 分間隔、
失敗時は最大 60 分まで指数バックオフ）で取得し、結果をローカルキャッシュに
書き、`statusloom claude` はそれを読みます。`statusloom claude` 自体は
ネットワークアクセスを一切しません。

refresh サブプロセスは Claude Code の OAuth トークンを**読み取り専用**で
参照します（リフレッシュもログ出力もしません）。探索順は
`CLAUDE_CODE_OAUTH_TOKEN` 環境変数 → `~/.claude/.credentials.json`
（`CLAUDE_CONFIG_DIR` が設定されている場合は
`$CLAUDE_CONFIG_DIR/.credentials.json`）→ macOS ではログインキーチェーンの
`Claude Code-credentials` エントリ（Apple 署名済みの `/usr/bin/security`
経由なので、キーチェーンのアクセス許可プロンプトも statusloom への
コード署名も不要）です。`STATUSLOOM_NO_USAGE_API=1` を設定すると usage API の
取得を完全に無効化できます — 該当フィールドは、元データが無いときと同じく
単に空になります。

`statusloom config` は起動時に usage API を検査し（`GET /api/usage/probe`）、
成功した場合のみこれらのフィールドをパレットに出します。

他のカウントダウン系フィールドと同様、`weekly-reset-opus` と
`weekly-reset-sonnet` はステータスラインが再描画されたときにしか更新され
ません。アイドル中もカウントダウンを進めたい場合は `--refresh-interval`
（[セットアップ](#セットアップ)参照）を設定してください。

### 今どのアカウントでログインしている？

`CLAUDE_CONFIG_DIR` でログインを切り替えている場合、現在のセッションが
どのアカウントを使っているかをこれらのフィールドが示します:

| フィールド | 表示内容 | 例 |
|---|---|---|
| `account-email` | ログイン中アカウントのメールアドレス | `dev@example.com` |
| `account-name` | 表示名 | `Dev User` |
| `account-org` | 組織名 | `Example Inc` |
| `account-role` | 組織内での役割 | `primary_owner` |
| `account-type` | Team シートか個人サブスクリプションか | `claude_team` / `claude_max` |
| `account-plan` | レート制限ティア | `default_claude_max_5x` |
| `account-seat` | シートティア（Team のみ） | `team_tier_1` |

```xml
<span prefix="as " optional="account-email">
    <field name="account-email" color="bright-black"/>
</span>
```

これらは Claude Code 自身のローカル `.claude.json` を読みます
（`CLAUDE_CONFIG_DIR` を尊重）。ネットワークアクセスは発生せず、値は
アクティブなプロファイルに追随します。値は Claude Code が保存した生の
文字列なので、`account-plan` は「Max 5x」ではなく `default_claude_max_5x`
になります。ログアウト時やファイルが無いときは全フィールドが空になり、
ドキュメントが実際にこれらのフィールドを使っている場合にのみファイルを
読みます。

**Team シートと個人サブスクリプションを併用している場合**、両者を判別できる
のは `account-type` です — 両方で必ず値が入る唯一のフィールドです
（`claude_team` / `claude_max`）。個人サブスクリプションにはシートが無いため
`account-seat` は空になります。`account-plan` は解決されます（statusloom が
個人アカウントで使われる組織スコープのレート制限ティアにフォールバック
するため）。

### 環境変数

`<field name="env" var="...">` は環境変数を表示します。ステータスラインが
他の手段では知り得ないもの — このシェルがどのクラウドプロファイル・
クラスタ・デプロイ先を向いているか — を出すのに便利です:

```xml
<span prefix="aws: " optional="env:AWS_PROFILE">
    <field name="env" var="AWS_PROFILE" color="yellow"/>
</span>
```

`optional="env:<NAME>"` のように変数名まで書いてください（変数名が値の一部
です）。そうすれば変数が未設定のときラベルもフィールドと一緒に消えます。
`var` が未指定のフィールドは単に空を描画し、何も報告しません。

**認証情報らしい変数はマスクされます。** ステータスラインはスクリーンショット
や画面共有に写り、共有されるプリセットは第三者が書きます。
そのため変数**名**に `TOKEN`・`SECRET`・`KEY`・`PASSWORD`・`PASSWD`・
`CREDENTIAL`・`AUTH`・`SESSION`・`COOKIE`・`PRIVATE`・`SIGNATURE` が含まれる
場合、statusloom は値ではなく `***` を描画します。この判定は名前だけを見て
値の中身は決して見ません。また意図的に過剰マッチします —
`KEYBOARD_LAYOUT` や `SSH_AUTH_SOCK` もマスクされます。実際に表示したい
場合は `unmask="true"` を付けてください:

```xml
<field name="env" var="KEYBOARD_LAYOUT" unmask="true"/>
```

## 設定

Statusloom の設定は単一の内部ストア `~/.config/statusloom/statusloom.json`
に保持されます（macOS でも同様に XDG 流のパスを使い、`XDG_CONFIG_HOME` を
尊重します）。このストアは git に似た構造を持ち、保存のたびにリビジョンが
積まれ、tool ごとに「現在」と「draft」の参照を持ちます。各リビジョンは
XML マークアップドキュメントを交換フォーマットとして保持します。ストアが
無い場合は組み込みの既定ドキュメントが使われるので、そのままでも動作します。

最小のドキュメント:

```xml
<statusloom version="1" tool="claude-code" color-level="ansi16">
  <layout name="Default" active="true">
    <line>
      <field name="model" color="cyan"/>
      <text role="separator" padding="1">|</text>
      <field name="git-branch" color="magenta"/>
      <text role="separator" padding="1">|</text>
      <span optional="context-percentage-usable" suffix=" ctx">
        <field name="context-percentage-usable"/>
      </span>
    </line>
  </layout>
</statusloom>
```

`<field>` は動的な値を表示し（データが無いときは周囲の装飾も隠します）、
`<text role="separator">` は自動的に畳まれるセパレータ、`<span>` は
子要素をまとめてスタイルを共有・prefix/suffix を適用・表示をゲートし、
`<flex/>` は行を埋める伸縮セパレータです。ドキュメントは手で編集しても
`statusloom config` から編集してもかまいません。マークアップの完全な
リファレンス（要素・属性・装飾・フォーマッタ・条件・色）は
[`markup.md`](markup.md) にあります。

空のセパレータはセグメント境界になります。行全体を Powerline として描画
するには `<statusloom>` に `output-style` を設定します:

```xml
<statusloom version="1" tool="claude-code" output-style="powerline">
  <layout name="Powerline" active="true">
    <line>
      <span background="blue" padding="1"><field name="model"/></span>
      <text role="separator"/>
      <span background="green" padding="1"><field name="git-branch"/></span>
    </line>
  </layout>
</statusloom>
```

Powerline 出力では手書きのセパレータノードは除去され、可視のトップレベル
field/text/span の間に遷移が生成されます。span とその入れ子の子要素すべては
1 つのセグメントにマージされます。標準出力では手書きセパレータのテキストと
その畳み込み挙動は従来どおりです。Powerline 出力は組み込みテーマから
前景色・背景色を割り当てるので、色を指定していない内容でも完全な
セグメントになります。明示的に設定した背景色が優先されます。`<flex/>` は
左側の連なりを `` で閉じ、既定背景で空きを埋め、右寄せの連なりを ``
で開きます。端末のフォントに Powerline のグリフが含まれている必要が
あります。

**他のステータスラインツールからの移行。** Statusloom に組み込みの
インポータはありません。他のツール（例: ccstatusline）の設定を移すには、
設定 UI を開いて monitor ワークスペースを起動し、コーディングエージェントに
翻訳させてください。エージェントが相手ツールの設定を読み、同じレイアウトを
Statusloom のマークアップで表現し、未保存の draft として共有し、描画を
プレビューするので、保存前に見比べて調整できます。ワークスペースに生成
される `CLAUDE.md` にこの手順が書かれています。

## 開発

ツールチェーン管理に [mise](https://mise.jdx.dev/) が必要です（Go・Node・
pnpm のバージョンは `mise.toml` で固定）。

```
mise install                                    # 固定バージョンの Go/Node/pnpm を導入
go test ./...                                   # Go のテストを実行（または: mise run test）
scripts/build-web.sh                            # 設定 UI を internal/webconfig/dist にビルド（または: mise run build-web）
pnpm --filter @statusloom/configurator test      # 設定 UI のテストを実行（または: mise run test）
```

mise のタスクがこれらの手順をまとめています。一覧は `mise tasks ls` で
確認してください（`mise run check` がコミット前のゲート: lint ＋ フロント
ビルド ＋ テスト）。

ブラウザレベルの検証には `mise run ui-check` があります。実際の UI を
Chromium で操作し、jsdom では書けない検証 — 実測ジオメトリ、行のソフト
ラップ、レイアウトの安定性 — を確認します。README の素材は
`mise run capture-media` で再生成できます。どちらも使い捨ての設定ディレクトリ
に対して動くので、自分の設定には触れません。

`scripts/build-web.sh` は `internal/webconfig/dist` に書き込みます。この
ディレクトリは全体が git 管理外なので（`internal/webconfig/dist/.gitignore`
だけが tracked）、コミット前の後始末は不要です。ビルドせずに作った
バイナリは UI ではなく「scripts/build-web.sh を実行してください」という
案内ページを返します。

## ライセンス

[Apache-2.0](LICENSE)
