# Statusloom ローカル開発プラン

## 1. プロジェクト概要

**Statusloom** は、Claude Code、Codex、GitHub Copilot CLIなどのコーディングエージェント向けに、軽量でカスタマイズ可能なステータスラインを提供するツールです。

主な特徴:

- Go製の単一バイナリ
- 通常描画時はネットワークアクセスなし
- デーモン常駐を前提としない
- ツールごとの個別設定
- Git情報など共通データの再利用
- 5時間・週間制限などアカウント共有情報のキャッシュ
- ReactベースのローカルWeb設定UI
- 将来的な共有サイト「Statusloom Room」
- プリセットの共有・導入
- Claude Code以外への拡張を前提としたアダプター構成

---

## 2. 初期ゴール

最初の開発対象はClaude Codeとする。

### v0.1で実現すること

- Claude CodeのstatusLine入力JSONを読み取る
- 以下を表示する
  - モデル
  - thinking effort
  - コンテキスト使用量
  - usable context percentage
  - セッションコスト
  - Gitブランチ
  - Git変更状況
  - 5時間利用率
  - 5時間リセット時刻
  - 週間利用率
  - 週間リセット時刻
  - Claude Codeバージョン
- JSON設定ファイル
- ANSIカラー
- 複数行レイアウト
- ccstatusline設定のimport
- React製ローカル設定UI
- GoバイナリへのWebアセット埋め込み
- 単一バイナリ配布

### v0.1ではやらないこと

- 常駐デーモン
- 公開共有サイト
- ユーザー認証
- 任意コマンドWidget
- 自動アップデート
- リモートテレメトリ
- 複雑なプラグイン機構
- Codex/Copilotの完全対応

---

## 3. 設計原則

### 3.1 描画経路を最小化する

`statusloom render` は以下のみを行う。

1. stdinのJSONを読み取る
2. ツールを判定する
3. ツール固有状態を正規化する
4. ローカルキャッシュを読む
5. Git情報を取得する
6. 設定に従って描画する
7. stdoutへ出力する

通常描画中にネットワークアクセスしない。

### 3.2 セッション固有情報はその場で処理する

以下は各呼び出しでstdinから取得する。

- model
- effort / reasoning
- context usage
- session cost
- session ID
- permission / approval mode
- task progress

これらは別プロセスへ渡さない。

### 3.3 アカウント共有情報はキャッシュする

以下はセッション間で共有する。

- 5時間制限
- 週間制限
- reset時刻
- 将来追加される利用制限情報
- 更新確認結果

優先順位:

1. stdinに最新値がある場合はそれを使用
2. 同時に共有キャッシュを更新
3. stdinに値がない場合は共有キャッシュのstale値を表示

rate_limitsの取得手段はstdinのみ（Pro/Max購読者のみ、セッション初回API応答後に出現。`five_hour`/`seven_day`それぞれ独立に欠落しうる）。render pathはnetwork-freeのため、キャッシュを能動的に更新する手段は存在しない。キャッシュの役割は「rate_limits欠落時に他セッションで観測した値を表示する」ことに限定する。

### 3.4 デーモンレスを既定とする

初期構成:

```text
statusloom render
  ├─ stdin処理
  ├─ cache読取
  ├─ Git取得
  └─ 描画
```

`statusloom refresh`（短命バックグラウンドワーカー)は設計から保留する。アカウント情報の取得手段がstdin以外に存在しないため、能動的なキャッシュ更新は実装不能であり不要。将来stdin以外の公式データソースが出現した場合にのみ再検討する。

常駐サービス、launchd、systemd、Windows Serviceは対象外。

---

## 4. モノレポ構成

```text
statusloom/
├── cmd/
│   └── statusloom/
│       └── main.go
│
├── internal/
│   ├── adapters/
│   │   ├── claude/
│   │   ├── codex/
│   │   └── copilot/
│   ├── cache/
│   ├── cli/
│   ├── config/
│   ├── detect/
│   ├── gitstatus/
│   ├── render/
│   ├── schema/
│   └── webconfig/
│
├── packages/
│   ├── editor/
│   ├── schema/
│   ├── catalog/
│   └── ui/
│
├── apps/
│   ├── configurator/
│   └── room/
│
├── presets/
│   ├── built-in/
│   └── examples/
│
├── fixtures/
│   ├── claude/
│   ├── codex/
│   └── copilot/
│
├── docs/
├── scripts/
│
├── go.mod
├── package.json
├── pnpm-workspace.yaml
├── mise.toml
├── README.md
└── LICENSE
```

`apps/room`、`packages/catalog`、`packages/ui`はMilestone 8まで作成しない。`apps/configurator`、`packages/editor`、`packages/schema`はMilestone 5まで作成しない（空ディレクトリを先行して作らない）。

### 各ディレクトリの役割

#### `cmd/statusloom`

エントリーポイントのみを置く。

#### `internal/adapters`

各ツール固有の入力処理。

- Claude Code stdin JSON
- Codex設定export
- Copilot hooks/session情報

#### `internal/render`

ANSI描画、幅制御、複数行レイアウト、compact表示。

#### `internal/gitstatus`

Gitブランチ、変更状況、ahead/behind、numstat取得。

#### `internal/cache`

共有JSONキャッシュ、atomic write、lease管理。

#### `apps/configurator`

ローカル設定用Reactアプリ。

#### `apps/room`

将来の共有サイト。

#### `packages/editor`

ConfiguratorとRoomで共有するReactコンポーネント。

#### `packages/schema`

JSON Schema、TypeScript型生成、fixtures。

---

## 5. CLI設計

```text
statusloom render
statusloom render --tool claude-code

statusloom claude
statusloom codex
statusloom copilot

statusloom config
statusloom config --tool claude-code

statusloom setup claude-code
statusloom setup claude-code --refresh-interval 60
statusloom setup codex
statusloom setup github-copilot

statusloom import ccstatusline
statusloom doctor

statusloom cache inspect
statusloom cache clear

statusloom preset use <name>
statusloom preset export
statusloom preset validate

statusloom version
```

### 初期版で必須のコマンド

```text
statusloom claude
statusloom config
statusloom setup claude-code
statusloom import ccstatusline
statusloom doctor
statusloom version
```

---

## 6. ツール判定

優先順位:

1. `--tool`
2. 専用サブコマンド
3. stdinの構造
4. 環境変数
5. 実行ファイル名
6. 判定不能ならエラー

```go
type Detection struct {
    Tool       ToolID
    Confidence int
    Reasons    []string
}
```

Claude Codeの判定例:

```go
func DetectClaudeCode(raw map[string]json.RawMessage) bool {
    _, hasSessionID := raw["session_id"]
    _, hasTranscript := raw["transcript_path"]
    _, hasContext := raw["context_window"]

    return hasSessionID && hasTranscript && hasContext
}
```

単一フィールドだけでは判定しない。

---

## 7. 共通状態モデル

```go
type StatusSnapshot struct {
    Tool       ToolSnapshot
    Session    SessionSnapshot
    Repository RepositorySnapshot
    Account    AccountSnapshot
    System     SystemSnapshot
    PullRequest *PullRequest
}
```

```go
type SessionSnapshot struct {
    ID string

    Model     *string
    Reasoning *string
    State     *string

    Name      *string // --name / /rename
    AgentName *string // --agent
    VimMode   *string // vim mode有効時のみ

    Context *ContextUsage
    Cost    *CostUsage
    Tokens  *TokenUsage

    PermissionMode *string
    TaskProgress   *Progress
}
```

```go
type ContextUsage struct {
    TotalInputTokens  int
    ContextWindowSize int
    UsedPercentage    float64
    UsableUsedPercentage float64
    Current           *TokenBreakdown // 初回API応答前・/compact直後はnil
}

type TokenBreakdown struct {
    Input         int
    CacheRead     int
    CacheCreation int
    Output        int
}

type CostUsage struct {
    USD          float64
    Duration     time.Duration
    APIDuration  time.Duration
    LinesAdded   int
    LinesRemoved int
}
```

```go
type SystemSnapshot struct {
    Repo     *string // originから得たowner/name
    Worktree *string // linked git worktree名
}

type PullRequest struct {
    Number      int
    URL         string
    ReviewState string // approved | pending | changes_requested | draft
}
```

```go
type RepositorySnapshot struct {
    Root      string
    Branch    string
    Dirty     bool
    Staged    int
    Unstaged  int
    Untracked int
    Added     int
    Deleted   int
    Ahead     int
    Behind    int
}
```

ツール固有値は別mapへ逃がしてもよい。

```go
type ToolState struct {
    Common StatusSnapshot
    Fields map[string]any
}
```

---

## 8. Adapter設計

```go
type Adapter interface {
    ID() ToolID
    Detect(DetectionInput) Detection
    Decode(context.Context, Input) (StatusSnapshot, error)
    WidgetCatalog() []WidgetDefinition
}
```

初期実装:

- `ClaudeAdapter`
- `CodexAdapter` はstub
- `CopilotAdapter` はstub

### ClaudeAdapter

- stdin JSON decode
- rate_limits取込
- context計算
- cost計算
- version取込
- session metadata取込
- transcript解析は行わない（context・rate_limitsはstdinで完結する）

---

## 9. Widget設計

初期Widget:

### 共通

- model
- separator
- git-branch
- git-changes
- tool-version
- current-directory

### Claude Code

- thinking-effort
- context-length
- context-percentage
- context-percentage-usable
- session-cost
- five-hour-usage
- five-hour-reset
- weekly-usage
- weekly-reset
- session-name（`--name` / `/rename` で付けたセッション名）
- agent-name（`--agent` 実行時のエージェント名）
- vim-mode（vim mode有効時の現在モード。使用する場合はClaude Code側`hideVimModeIndicator: true`を推奨し二重表示を防ぐ）
- pr-number（現ブランチのopen PR番号。例 `#1234`。merge/closeで非表示になる）
- pr-review-state（`approved` / `pending` / `changes_requested` / `draft`）
- repo-name（`origin` remote由来の `owner/name`）
- worktree（linked git worktree名）
- session-duration / api-duration（経過時間。`1h 15m`形式）
- lines-changed（`(+156,-23)`形式）
- cache-hit-rate（直近API呼び出しのキャッシュ読取率%）

上記の新規11 widgetも含め、全widgetは対応データが欠落している場合に自動非表示となる（既存のhide-when-empty仕様に従う）。

### レイアウト

- separator
- flex-separator
- line break
- hide-when-empty（値が空のWidgetは非表示にし、隣接するseparatorも畳む）
- compact mode
- conditional display（`showWhen`）

flexターゲット幅はツール設定ではなく各flex-separator Widgetの`flex`プロパティ（`""`=`"full"`、`"full-minus-<N>"`）で指定する。同一行に複数ある場合は最小の解決値が行全体のターゲットになる。Claude Codeはstatusline行の右側にシステム通知（MCPエラー・auto-update）やverbose時のトークンカウンタを表示するため、`full`よりも`full-minus-N`を推奨する。

全コンテンツWidgetは任意の`template`を受け付ける（`{value}`プレースホルダーをWidgetの描画値で置換し、置換結果全体をWidgetのスタイル内側に描画。非表示Widgetは何も出力しない（templateだけが単独で描画されることはない）。separator/flex-separatorでは無視される）。five-hour-usage / weekly-usageの「5h: 」「7d: 」ラベルはレンダラーではなくデフォルトプリセットのtemplate（`"5h: {value}"` / `"7d: {value}"`）が持つ。

端末幅はClaude Codeが設定する`COLUMNS`/`LINES`環境変数（v2.1.153+）から取得する。statusLineコマンドのstdoutはターミナルに接続されていないため`tput cols`は使えない。**v2.1.132〜2.1.152ではこれらの環境変数が渡されないため、flex-separatorは1スペース固定、`compactThreshold`は発動しない（劣化動作）。**

### 表示条件・色・リンク（WidgetSpec拡張）

- `showWhen: {"source": "...", "op": "gte", "value": 70}` — `source`省略または`"self"`はWidget自身のメトリック（context-percentageなら使用率%、five-hour-resetならリセットまでの分数など）、それ以外は名前付きメトリック（`context-percent` / `context-usable-percent` / `context-tokens` / `five-hour-percent` / `seven-day-percent` / `five-hour-reset-minutes` / `seven-day-reset-minutes` / `session-cost-usd` / `session-duration-minutes` / `api-duration-minutes` / `lines-added` / `lines-removed` / `lines-changed-total` / `cache-hit-percent`）。`op`は`lt|lte|gt|gte|eq|neq`。メトリックが解決できない場合はWidget非表示。例: 「context使用率70%以上のときだけ表示」「5hリセット60分前からカウントダウンを出す」
- `colorRules: [{"op":"gte","value":90,"color":"red"},{"op":"gte","value":70,"color":"yellow"}]` — selfメトリックで評価し先勝ち、どれにも該当しなければ`color`にフォールバック
- `hyperlink: true` — pr-number / pr-review-state / repo-nameをOSC 8ハイパーリンク化（PRは`pr.url`、repoは`https://<host>/<owner>/<name>`）。`colorLevel: "none"`では出力しない。iTerm2/Kitty/WezTermなどが対応、非対応端末では`FORCE_HYPERLINK=1`が必要な場合がある。tmux/SSH越しでは剥がれる場合がある
- `GET /api/tools/{tool}/widgets`のentryは`linkable?: bool`でhyperlink対応Widgetを示す。`GET /api/tools/{tool}/metrics`が`showWhen`用メトリック一覧（`[{name, displayName, descriptions:{en,ja}}]`）を返す（15章参照）

### 将来候補（未実装）

- output-style（stdinに既に存在するデータ）
- subagentStatusLine対応（サブエージェントパネル行の差し替え。通常のstatusLineとは別プロトコルで、stdinの構造も異なる。`tasks`配列を受け`{"id","content"}`のJSON行を出力する。per-taskの`model`/`contextWindowSize`はv2.1.205+）

```go
type WidgetDefinition struct {
    Type        string
    DisplayName string
    Description string
    Category    string
    Properties  []PropertyDefinition
    Linkable    bool // hyperlink対応（`linkable?`としてAPIへ公開）
}
```

---

## 10. 設定ファイル

保存先はXDG Base Directory準拠（macOSでも`~/.config`を使用、`XDG_CONFIG_HOME`尊重）。Windowsは`%AppData%`。

```text
macOS/Linux: ~/.config/statusloom/config.json
Windows:     %AppData%\statusloom\config.json
```

例:

```json
{
  "schemaVersion": 1,
  "shared": {
    "git": {
      "cacheTtlMs": 3000,
      "includeUntracked": true,
      "collectNumstat": true
    }
  },
  "tools": {
    "claude-code": {
      "compactThreshold": 60,
      "colorLevel": "ansi16",
      "lines": [
        [
          {
            "type": "model",
            "color": "cyan"
          },
          {
            "type": "separator",
            "text": " | "
          },
          {
            "type": "thinking-effort"
          },
          {
            "type": "separator",
            "text": " | "
          },
          {
            "type": "context-length"
          },
          {
            "type": "separator",
            "text": " | "
          },
          {
            "type": "context-percentage-usable"
          },
          {
            "type": "separator",
            "text": " | "
          },
          {
            "type": "session-cost"
          },
          {
            "type": "separator",
            "text": " | "
          },
          {
            "type": "git-branch",
            "color": "magenta"
          },
          {
            "type": "separator",
            "text": " | "
          },
          {
            "type": "git-changes",
            "color": "yellow"
          }
        ],
        [
          {
            "type": "five-hour-usage"
          },
          {
            "type": "separator",
            "text": " | "
          },
          {
            "type": "five-hour-reset"
          },
          {
            "type": "separator",
            "text": " | "
          },
          {
            "type": "weekly-usage"
          },
          {
            "type": "separator",
            "text": " | "
          },
          {
            "type": "weekly-reset"
          },
          {
            "type": "separator",
            "text": " | "
          },
          {
            "type": "tool-version"
          }
        ]
      ]
    }
  }
}
```

WidgetSpecは`showWhen`・`colorRules`・`hyperlink`も受け付ける（詳細は9章「表示条件・色・リンク」参照）。例:

```json
{
  "type": "five-hour-reset",
  "showWhen": { "source": "self", "op": "lte", "value": 60 },
  "colorRules": [
    { "op": "gte", "value": 90, "color": "red" },
    { "op": "gte", "value": 70, "color": "yellow" }
  ],
  "color": "green"
}
```

```json
{
  "type": "pr-number",
  "hyperlink": true
}
```

補足:

- 色はANSI16色名（`cyan`、`brightBlack`など）、256色番号（`"ansi256:242"`）、hex（`"#8be9fd"`）をサポートする。`colorLevel`（`none` / `ansi16` / `ansi256` / `truecolor`）に応じてダウングレードする
- `refreshInterval`はClaude Code側`settings.json`の設定項目であり、このファイルには持たせない（setupコマンドの責務）。`statusloom setup claude-code --refresh-interval <seconds>`（最小1秒、Claude Code公式仕様）で設定できる

---

## 11. キャッシュ設計

保存先はXDG準拠（`XDG_CACHE_HOME`尊重）。Windowsは`%LocalAppData%`。

```text
~/.cache/statusloom/
├── account/
│   └── <account-key>.json
├── repos/
│   └── <repo-hash>.json
└── previews/
    └── claude-code.json
```

### アカウントキー

stdinにアカウント識別子は含まれないため、v0.1では固定キー`default`を使用する。マルチアカウント対応は将来課題として明記する。

### アカウントキャッシュ

```json
{
  "schemaVersion": 1,
  "source": "claude-code-stdin",
  "observedAt": "2026-07-11T10:00:00+09:00",
  "expiresAt": "2026-07-11T10:05:00+09:00",
  "fiveHour": {
    "usedPercentage": 27,
    "resetsAt": "2026-07-11T13:00:00+09:00"
  },
  "sevenDay": {
    "usedPercentage": 79,
    "resetsAt": "2026-07-15T15:00:00+09:00"
  }
}
```

時刻はすべてISO 8601に統一する（stdinの`resets_at`はepoch秒で届くため正規化して保存）。書き込みは値が変化した場合、または`observedAt`が30秒以上古い場合のみ行う（300msデバウンスの描画ごとに書かない）。

### 書き込み方式

```text
file.tmp.<pid>
  ↓
fsync
  ↓
atomic rename
```

### 更新競合対策

- atomic write + last-writer-winsのみ（lock/leaseは持たない。能動更新ワーカーが存在しないため不要）
- 描画処理では待たない

---

## 12. Git情報取得

基本コマンド:

```bash
git --no-optional-locks \
  -C <repo> \
  status \
  --porcelain=v2 \
  --branch \
  --untracked-files=normal
```

必要に応じて:

```bash
git -C <repo> diff --numstat
git -C <repo> diff --cached --numstat
```

実行条件:

- timeout: 既定200ms（設定可能）
- `GIT_OPTIONAL_LOCKS=0`
- stdinなし
- stdoutサイズ制限
- hooksを発火しない
- repo単位キャッシュ（`repos/<repo-hash>.json`、TTL既定3秒）を最初から組み込む
- timeout・失敗時はキャッシュのstale値を表示し、なければ非表示

Claude Codeは新しい更新イベントが来ると実行中のstatusLineコマンドをキャンセルするため、描画は常に高速である必要がある。

---

## 13. usable context percentage

計算の基礎はstdinの`context_window`（`total_input_tokens`、`context_window_size`、`used_percentage`）とする。自前のトークン集計やtranscript解析は行わない。

```text
usableSize = context_window_size - reserveTokens
usableUsedPercentage = total_input_tokens / usableSize * 100
```

reserveTokensは固定値ではなく、`context_window_size`に応じた既定値を持つ（200k / 1Mの両方に対応）。設定で上書き可能とする。

設定例:

```json
{
  "context": {
    "autoCompactReserve": { "mode": "auto" },
    "percentageMode": "usable"
  }
}
```

選択肢:

- raw（stdinの`used_percentage`をそのまま表示）
- usable
- both

初期検証中は`both`をサポートしてもよい。

サポート最低バージョンはClaude Code v2.1.132とする（それ以前は`total_input_tokens`が累積値で意味が異なる）。`used_percentage`はinput系トークンのみの計算（output tokenは含まない、公式仕様）。`context_window.exceeds_200k_tokens`はcontext window sizeによらず固定200k閾値での判定であり、`usable`計算とは独立。

端末幅取得（`COLUMNS`/`LINES`）に必要なv2.1.153+については9章「レイアウト」を参照。v2.1.132〜2.1.152ではflex-separator・compactThresholdが劣化動作となる。

---

## 14. React設定UI

起動:

```bash
statusloom config
```

動作:

1. `127.0.0.1:0`へbind
2. ランダムポート取得
3. 起動用token生成
4. ブラウザ起動
5. 設定編集
6. 保存
7. idle timeoutまたは保存後終了

### UI構成

```text
┌ Tool Selector ────────────────────────────┐
│ Claude Code                              │
└──────────────────────────────────────────┘

┌ Widgets ─────┐ ┌ Preview ────────────────┐
│ Model        │ │ Opus | high | 32%       │
│ Context      │ │ main ● +2 ~1            │
│ Usage        │ └─────────────────────────┘
│ Git Branch   │
│ Git Changes  │ ┌ Properties ─────────────┐
└──────────────┘ │ Color: cyan              │
                 │ Format: compact           │
                 └───────────────────────────┘
```

### 操作モデル（ブラウザUIらしい直接操作）

- Widgetパレット: 全Widgetをカテゴリ別チップとして常時表示。クリックで追加、ドラッグでプレビューへ挿入
- プレビュー直接操作: プレビュー自体が編集キャンバス。セグメント（widget単位の描画結果）をクリックで選択、ドラッグで行内・行間の並べ替え。非表示Widget（データなし）はゴーストチップで表示・選択可能
- ドロップダウンでのWidget追加や移動ボタンは採用しない（TUI的操作の持ち込み禁止）
- 実現のためpreview APIはwidget単位のセグメント構造を返す（Go rendererが正）

### 必須機能

- Widget追加
- 並べ替え
- 行追加・削除
- 色変更
- format変更
- terminal width preview
- compact threshold preview
- dark/light preview
- Nerd Fontあり/なし
- sample/last observed data切替
- undo/redo
- JSON import/export

### 技術

- React
- TypeScript
- Vite
- pnpm
- dnd-kit（@dnd-kit/core + sortable。ドラッグ&ドロップの唯一の外部UI依存）
- Goへ静的アセット埋め込み

`go:embed`はソースファイル相対パスしか参照できないため、ビルド時に`apps/configurator`の成果物を`internal/webconfig/dist/`へコピーしてからembedする。

```go
// internal/webconfig/assets.go
//go:embed all:dist
var assets embed.FS
```

`all:`プレフィックスで`_`・`.`始まりのファイルの取りこぼしを防ぐ。

---

## 15. WebUI API

```text
GET  /api/tools
GET  /api/tools/{tool}/widgets
GET  /api/tools/{tool}/metrics
GET  /api/config
PUT  /api/config
POST /api/preview
POST /api/import/ccstatusline
POST /api/setup/{tool}
```

`GET /api/tools/{tool}/widgets`の各entryは`{type, displayName, descriptions:{en,ja}, category, preview:{text,ansi}, defaultTemplate?, linkable?, selfMetric?}`。`GET /api/tools/{tool}/metrics`は`showWhen`で参照可能なメトリック一覧を`[{name, displayName, descriptions:{en,ja}}]`で返す（9章参照）。

PreviewはGo rendererを正とする。

TypeScript側でANSI描画ロジックを再実装しない。

---

## 16. ローカルWebUIのセキュリティ

- `127.0.0.1`のみbind
- ランダムポート
- 起動時tokenを全APIリクエストの`Authorization`ヘッダで必須化
- Host検証
- Origin検証
- CORS無効
- cookieは使用しない（token認証のみのためCSRF対策は不要）
- 任意ファイル書き込み禁止（書込先は設定ファイルの固定パスのみ）
- 任意コマンド禁止
- custom command Widgetは初期版対象外
- idle timeout
- 保存後終了オプション

---

## 17. ccstatusline import

入力:

```text
~/.config/ccstatusline/settings.json
```

実行:

```bash
statusloom import ccstatusline ~/.config/ccstatusline/settings.json
```

初期対応Widget:

- model
- separator
- thinking-effort
- context-length
- context-percentage-usable
- session-cost
- git-branch
- git-changes
- session-usage
- reset-timer
- weekly-usage
- weekly-reset-timer
- version

Widget単位の`color`・`rawValue`・`metadata`（例: `session-usage`の`display`）もマッピングする。

グローバル設定のマッピング:

- `flexMode` → 各flex-separator Widgetの`flex`プロパティへマッピング（`full`/空はスキップ。flex-separatorが無い場合は効果なしと警告）
- `compactThreshold` → そのまま取込
- `colorLevel` → 色深度設定へ変換
- `powerline.enabled: true` → v0.1未対応のため警告
- `inheritSeparatorColors` / `globalBold` / `minimalistMode` → 対応可否を実装時に判断し、未対応なら警告

未対応Widget・設定は黙って無視せず、警告を出してスキップする。

例:

```text
Imported 13 widgets.
Unsupported widgets:
- custom-command
- voice-status
```

---

## 18. Claude Code setup

```bash
statusloom setup claude-code
statusloom setup claude-code --refresh-interval 60
```

想定設定:

```json
{
  "statusLine": {
    "type": "command",
    "command": "statusloom claude"
  },
  "refreshInterval": 60
}
```

`refreshInterval`は既定では書き込まない（Claude Codeはイベント駆動＝応答ごと・/compact後・permission mode変更時などに更新するため通常不要）。時刻カウントダウン系Widget（`five-hour-reset` / `weekly-reset`）を使う場合、Claude Codeはアイドル中に更新イベントを送らず表示が固まるため、`--refresh-interval <seconds>`（最小1秒、Claude Code公式仕様）での設定を推奨する。`statusloom doctor`はこれらのWidgetが設定済みかつ`refreshInterval`未設定の場合に警告する。

Windowsでは`statusLine.command`のパスをスラッシュ区切りにする（バックスラッシュ区切りはGit Bashがエスケープ文字として消費してしまうため）。

既存設定がある場合:

- backup作成
- diff表示
- 明示確認
- rollback可能にする

---

## 19. Statusloom Room

将来の共有サイト。

### 初期コンセプト

- Preset一覧
- Preview
- Fork
- Install command
- Tool別filter
- Nerd Font有無
- Terminal width
- Tags
- Version履歴

### CLI

```bash
statusloom preset use fujie/compact-two-line
statusloom preset export
statusloom preset publish
```

### 公開Presetに含めないもの

- ローカルパス
- account識別子
- token
- proxy
- custom command
- 任意URL
- 任意ファイル参照

### 初期公開方式

最初はGitHub PRベースでもよい。

```text
presets/
└── <owner>/
    └── <slug>/
        ├── preset.json
        ├── README.md
        └── preview.svg
```

---

## 20. テスト方針

### Go

- unit test
- golden test
- fixture-based decode test
- ANSI snapshot test
- malformed input test
- concurrent cache test
- atomic write test
- Git parser test
- tool detection test

### Frontend

- component test
- schema validation test
- drag/drop state test
- preview API test
- import/export test

### E2E

- Claude Code fixtureをstdinへ渡す
- 実際の設定JSONで描画
- ccstatusline import
- setup dry-run
- local configurator起動
- config保存

---

## 21. Benchmark

最低限計測する。

```text
cold start
warm render
8 parallel renders
32 parallel renders
Gitあり
Gitなし
cache fresh
cache stale
large repository
```

目標:

```text
stdin-only render: p95 < 5ms
cache read render: p95 < 8ms
Git cache hit render: p95 < 10ms
Git cache miss render（git status同期実行込み）: p95 < 50ms
runtime dependencies: 0
network in render path: 0
```

バイナリサイズ目標:

```text
CLIのみ: 10MB前後
WebUI埋込後: 15〜30MB程度を許容
```

---

## 22. 依存方針

### Go

原則標準ライブラリ。

候補:

- `golang.org/x/sys` のみ必要時
- CLI frameworkは使わない
- Git libraryは使わない
- TOML/YAML parserは使わない
- telemetry SDKは使わない
- 自動更新ライブラリは使わない

### Frontend

- React
- TypeScript
- Vite
- 必要最小限のdrag/drop library
- UI frameworkは慎重に選定
- CDN依存なし
- build artifactのみGoへ埋め込む

---

## 23. ビルド

```bash
CGO_ENABLED=0 go build \
  -trimpath \
  -buildvcs=true \
  -ldflags="-s -w -X main.version=${VERSION}" \
  ./cmd/statusloom
```

### Release

- macOS arm64
- macOS amd64
- Linux arm64
- Linux amd64
- Windows amd64
- Windows arm64

### 配布候補

- GitHub Releases
- mise
- aqua
- Homebrew
- Scoop
- Winget
- Nix

---

## 24. サプライチェーン対策

- Go依存最小化
- `go.sum`固定
- `pnpm-lock.yaml`固定
- GitHub Actionsはcommit SHA固定
- `govulncheck`
- DependabotまたはRenovate
- SBOM
- SHA-256
- release署名
- provenance
- reproducible build検証
- custom command非対応
- render path network-free

---

## 25. 初期マイルストーン

### Milestone 0: Repository Bootstrap

- モノレポ作成
- Go module
- pnpm workspace
- mise設定
- CI
- lint/test
- license
- README

### Milestone 1: Claude Render Core

- stdin decode
- tool detection
- snapshot model
- widget renderer
- ANSI
- multiple lines
- golden tests

### Milestone 2: Git Integration

- branch
- dirty
- staged/unstaged/untracked
- numstat
- timeout
- repo単位キャッシュ
- parser tests

### Milestone 3: Cache

- usage snapshot
- atomic write
- stale read
- preview snapshot

### Milestone 4: Config

- config schema
- migration
- validation
- built-in preset
- ccstatusline import

### Milestone 5: Local Configurator

- React app
- embedded assets
- local HTTP server
- preview API
- editor
- save
- setup

### Milestone 6: Release

- cross build
- Windows statusLine command pathのスラッシュ区切り検証（18章参照）
- GitHub Release
- checksums
- install docs
- benchmark
- sample screenshots

### Milestone 7: Multi-tool

- Codex capability調査
- Codex config export
- Copilot capability調査
- adapter stubs
- tool-specific configurator tabs

### Milestone 8: Room

- preset schema
- local export/import
- static gallery
- install command
- GitHub PR publishing

---

## 26. 最初のIssue候補

1. Initialize monorepo
2. Define Claude Code fixture format
3. Implement stdin decoder
4. Define normalized snapshot model
5. Implement tool detection
6. Implement basic widget renderer
7. Add ANSI color support
8. Add multi-line layout
9. Implement Git branch detection
10. Implement Git porcelain v2 parser
11. Implement context percentage
12. Implement usable context percentage
13. Implement usage cache
14. Implement atomic JSON writer
15. Implement config schema v1
16. Implement ccstatusline importer
17. Implement `statusloom claude`
18. Implement `statusloom setup claude-code`
19. Bootstrap React configurator
20. Add Go preview API
21. Embed frontend assets
22. Add terminal width preview
23. Add release workflow
24. Add benchmark suite
25. Add built-in presets

---

## 27. 最初の一週間の進め方

### Day 1

- repository作成
- Go module
- pnpm workspace
- mise
- CI
- README skeleton

### Day 2

- Claude Code fixture
- stdin decode
- snapshot model
- basic render

### Day 3

- Widget model
- ANSI
- multiple lines
- current ccstatusline相当の表示

### Day 4

- Git parser
- branch/changes
- timeout
- tests

### Day 5

- config schema
- config load
- ccstatusline import

### Day 6

- React configurator skeleton
- Go local HTTP server
- preview API

### Day 7

- setup command
- embed
- local end-to-end test
- benchmark baseline

---

## 28. README冒頭案

> Statusloom is a fast, portable status-line toolkit for coding agents.
>
> Build, preview, install, and share status lines for Claude Code, Codex, GitHub Copilot, and other coding tools.
>
> Statusloom ships as a single Go binary, keeps the render path network-free, and includes a visual local configurator.

---

## 29. 最終的な初期判断

- 名前: **Statusloom**
- 共有サイト: **Statusloom Room**
- リポジトリ: モノレポ
- CLI: Go
- 設定UI: React
- 配布: 単一バイナリ
- 描画: デーモンレス
- ネットワーク: render pathでは禁止
- キャッシュ: アカウント共有・repo共有
- 設定: ツール別
- 初期対応: Claude Code
- 次期対応: Codex、GitHub Copilot CLI
