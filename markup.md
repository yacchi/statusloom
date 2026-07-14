Statusloomの設定DSLを、現在のWidget列ベースの構文から、XML/JSX風の宣言的マークアップへ改訂してください。

## 目的

現在の設定UIは、Model、Separator、Effortなどを個別Widgetとして並べる必要があり、完成形を直接編集するUXになっていません。

新しいDSLでは、最終的なステータスラインに近い見た目で構造を記述できるようにします。

例:

```xml
<statusloom version="1" tool="claude-code">
  <layout name="default" active="true">
    <line>
      <text>Model: </text>
      <field name="model" color="cyan" bold="true"/>

      <span
        optional="thinking-effort"
        prefix=" ("
        suffix=")"
        color="bright-black"
      >
        <field name="thinking-effort" color="yellow"/>
      </span>

      <text role="separator" padding="1">|</text>

      <text>Context: </text>
      <field
        name="context-percentage-usable"
        format="percent"
        precision="0"
      />
    </line>
  </layout>
</statusloom>
```

想定される出力:

```text
Model: Opus-4.8 (high) | Context: 42%
```

## 基本方針

* XML互換の構文とする
* JSXに似た見た目を持つが、JavaScriptやJSX parserには依存しない
* Go側では、パースは標準ライブラリの`encoding/xml`を基本に実装する（再生成は自前serializer。後述）
* DSLを永続化形式とする
* レンダリング時にDSLをASTへ変換する
* Web UIとDSL Editorは同じASTを介して相互反映する
* Visual Editorによる変更時も、元のDSL表現を可能な限り維持する
* 完全な汎用テンプレート言語やプログラミング言語にはしない
* 任意コード実行や任意関数呼び出しは提供しない
* プレリリースにつき後方互換性は不要。既存`config.json`からの破壊的移行も許容する（現行仕様との並存はしない）

## 必須ノード

初期実装では、少なくとも次のノードを提供してください。

### `statusloom`

ドキュメントルート。tool-level設定はroot属性として持たせる。

```xml
<statusloom
  version="1"
  tool="claude-code"
  color-level="ansi16"
  output-style="standard"
  compact-threshold="60"
  context-percentage-mode="usable"
  context-reserve-tokens="0"
>
  <layout name="default" active="true">
    ...
  </layout>
</statusloom>
```

属性（tool-level設定。現行`ToolConfig`相当）:

* `version` — DSLバージョン（現状`1`）
* `tool` — 対象ツール識別子（例: `claude-code`）。1ファイル1ツール（→「設定ファイルの配置」参照）
* `color-level` — `none` | `ansi16` | `ansi256` | `truecolor`
* `output-style` — `standard` | `powerline`。行全体の描画方式
* `compact-threshold` — compact表現へ切り替える端末幅閾値。`0`で無効
* `context-percentage-mode` — context使用率の計算方式。`raw` | `usable` | `both`
* `context-reserve-tokens` — usable計算時に予約するトークン数

rootの直下に置けるのは、optionalな`git`（0または1個）と、1つ以上の`layout`のみとする。`line`を直接置くことはできない。

### 設定ファイルの配置

配置は`<configDir>/<tool>.xml`（例: `~/.config/statusloom/claude-code.xml`）とし、1ファイル1ツールとする。root要素が単一の`tool`属性を持つ本設計と整合する。

shared git設定は別ファイルにせず、各ツールファイルのroot直下のoptionalな`<git/>`要素として置く（→「git」参照）。現行の`<configDir>/config.json`（tools map + shared git設定）は廃止し、移行する。

### `git`

git情報収集の設定。rootの直下に0または1個だけ置ける。省略時は既定値を使う。

```xml
<git cache-ttl-ms="3000" timeout-ms="200" include-untracked="true" collect-numstat="true"/>
```

属性（現行shared git設定相当）:

* `cache-ttl-ms` — gitステータスキャッシュのTTL（ミリ秒）
* `timeout-ms` — gitコマンド実行のタイムアウト（ミリ秒）
* `include-untracked` — untrackedファイルを変更数に含めるか
* `collect-numstat` — numstat（行数増減）を収集するか

`git`要素が複数ある場合、および数値・boolean属性の不正値はvalidation error。

### `layout`

名前付きの複数レイアウト定義。rootの子として複数記述でき、内部に`line`を並べる。

```xml
<layout name="wide" active="true">
  <line>...</line>
  <line>...</line>
</layout>
<layout name="compact">
  <line>...</line>
</layout>
```

属性:

* `name` — レイアウト名（layout内で一意）
* `active` — 描画対象。`true`のlayoutは常にちょうど1つ

`active`のルール:

* activeなlayoutは常にちょうど1つ。重複・欠如はvalidation error
* layoutが1つだけの場合は`active`を省略可（暗黙にactive扱い）

### `line`

ステータスラインの1行を表す。`layout`の子として記述する。

```xml
<line>
  ...
</line>
```

複数の`line`を記述した場合は複数行として出力する。

### `span`

子ノードをまとめるコンテナ。

主な用途:

* 色や太字などの装飾を子へ継承する
* prefix、suffixを子の前後へ追加する
* paddingをコンテナ全体へ追加する
* 条件表示をまとめて制御する

```xml
<span
  color="cyan"
  bold="true"
  prefix="("
  suffix=")"
>
  <field name="thinking-effort"/>
</span>
```

### `text`

固定文字列を厳密に表現するリーフノード。

```xml
<text>Model: </text>
<text role="separator" padding="1">|</text>
```

`text`内部では、先頭・末尾を含む空白を維持する。

`text`内部の改行は許可せず、validation errorとする。

`role`属性:

* `role="separator"` を指定すると自動折りたたみ（collapsing）セマンティクスを持つ（→「separator」参照）
* 省略時は通常の固定テキスト
* `separator`以外の値はvalidation error

### `field`

動的値を表示するリーフノード。

```xml
<field name="model"/>
```

主な属性:

* `name` — fieldカタログ上の実在するfield名（→「fieldカタログ」参照）
* `format` — formatter名
* `precision` — 小数桁など
* `raw` — `true`でラベル・装飾を剥がした生値を出す（後述）
* `hyperlink` — `true`でOSC 8ハイパーリンクを出力（linkableなfieldのみ）
* `prefix`
* `suffix`
* 文字装飾属性
* 共通表示属性

fieldが存在しない、または値が空の場合、field本体は空文字を返す。

ただし、prefixやsuffixを暗黙に非表示にはしない。条件表示が必要な場合は`optional`または`when`属性を使用する。

#### `raw`属性

現行`WidgetSpec.RawValue`相当。`raw="true"`のとき、そのfieldはラベルや装飾を伴わない生値を出力する。`raw`はcompact変換より優先される（compact-threshold発動時もraw値をそのまま出す）。

```xml
<field name="context-length" raw="true"/>
```

#### `hyperlink`属性

`hyperlink="true"`でOSC 8ハイパーリンクを出力する。linkableなfield（現行: `pr-number`、`pr-review-state`、`repo-name`）でのみ有効。非linkableなfieldに指定した場合はvalidation error。

```xml
<field name="pr-number" hyperlink="true"/>
```

### `flex`

flex-separator相当のリーフノード。端末幅（環境変数`COLUMNS`）を目標幅とし、行内の残余スペースを埋める。

```xml
<flex/>
<flex size="full-minus-2"/>
```

属性:

* `size` — 目標幅の書式。`full`（省略時のデフォルト）または `full-minus-<N>`

セマンティクス（現行flex-separator踏襲）:

* `full` は端末幅いっぱいまで、`full-minus-N` は端末幅から`N`引いた幅を目標とする
* 同一行に複数の`flex`がある場合、各targetの`min`を採用し、残余スペースを均等分配する
* 端末幅が不明（`COLUMNS`未設定など）の場合は空白1個にフォールバックする
* `size`の書式が不正な場合はvalidation error

## 生のTextNode

XMLのmixed contentとして、生の文字列も許可する。

```xml
<line>
  Model:
  <field name="model"/>
</line>
```

生のTextNodeは、読みやすさのための簡易表現として扱う。

### 生TextNodeの空白ルール

* whitespace-only TextNodeは無視する
* 前後の空白・改行はトリムする（インデント用の改行・空白はこれにより無視される）
* トリム後の内部にある空白連続のうち、改行を含む連続は1個のスペースへ折りたたむ（HTMLと同様）
* 改行を含まない内部の空白連続（スペース・タブのみ）はそのまま維持する
* 厳密な先頭・末尾空白が必要な場合は`text`要素を使用する

例:

```xml
<line>
  Session Cost:
  <field name="session-cost"/>
</line>
```

生TextNodeは`Session Cost:`として扱う。

ソース上で`Session`と`Cost:`が別行に分かれている場合も、内部改行の折りたたみにより同じく`Session Cost:`として扱う。

```xml
<line>
  Session
  Cost:
  <field name="session-cost"/>
</line>
```

次の末尾空白には意味を持たせない。

```xml
<line>
  Model: 
  <field name="model"/>
</line>
```

表示は次のようになる。

```text
Model:Opus-4.8
```

空白が必要な場合は次のように書く。

```xml
<line>
  <text>Model: </text>
  <field name="model"/>
</line>
```

## 共通属性

描画可能なノードに、可能な範囲で次の共通属性を提供する。

### 文字装飾

* `color`
* `background`
* `bold`
* `dim`
* `italic`
* `underline`
* `strikethrough`

文字装飾は親から子へ継承する。

子ノードで同じ属性が指定された場合、最も近いノードの指定を優先する。

例:

```xml
<span color="cyan" prefix="(" suffix=")">
  <field name="thinking-effort" color="yellow"/>
</span>
```

表示:

* `(`と`)`はcyan
* thinking-effort値はyellow

### レイアウト

* `padding`
* `padding-left`
* `padding-right`
* `prefix`
* `suffix`

`padding`は左右共通値。

個別指定がある場合は`padding-left`、`padding-right`を優先する。

レンダリング順序は次とする。

```text
padding-left
prefix
content
suffix
padding-right
```

padding、prefix、suffixは、その属性を持つノード自身の文字装飾を使用する。

これらの属性は子へ継承しない。

### 条件表示

* `optional`
* `when`

専用の`optional`要素、`when`要素は作らない。

#### `optional`

単一fieldの存在確認に使用する。値はfield名。

```xml
<span optional="thinking-effort">
  ...
</span>
```

次の場合は非表示:

* fieldが存在しない
* null
* 空文字
* 空のcollection

次の場合は存在扱い:

* 数値`0`
* boolean `false`

#### `when`

条件式による表示制御。

```xml
<text when="git-dirty eq true">●</text>
```

```xml
<span when="context-percent ge 80">
  ...
</span>
```

##### when式の演算子

ワード形式を第一級構文とする。`statusloom fmt`はワード形式へ正規化する。

* 比較: `lt` `le` `gt` `ge` `eq` `ne`
* 論理: `and` `or` `not`

加えて、XML属性値内で合法な記号演算子も受理する。

* `>` `>=` `==` `!=` `!`

ただし次の記号は、XML属性値内に生では書けない（well-formed違反になる）。

* `<` `<=` `&&` `||`

これらはエスケープ（`&lt;`、`&amp;`）した場合のみ動作するが、可読性・移植性の観点から推奨しない。ワード形式（`lt`、`le`、`and`、`or`）を使うこと。

なお、`&lt;`や`&amp;&amp;`とエスケープされた記号は、XMLデコード後のcondition parserには生の`<`・`&&`として届く。したがってcondition parser自体は全記号形式（`<` `<=` `>` `>=` `==` `!=` `&&` `||` `!`）を受理する実装とし、`statusloom fmt`がワード形式へ正規化する。DSL上の記述としてはワード形式を推奨する。

初期実装では、少なくとも次をサポートする。

* メトリクス参照（→「fieldカタログ」のメトリクス名）
* 上記の比較・論理演算子（ワード形式および合法な記号形式）
* 括弧
* string literal
* number literal
* boolean literal

`optional`と`when`が両方指定された場合はAND条件とする。

メトリクスが解決不能な場合（対応する値がスナップショットに存在しないなど）は、そのノードを非表示扱いにする。

CELなどの外部式言語は、初期実装では必須としない。将来差し替え可能なinterfaceへ分離する。

## prefix / suffix

新DSLでは`prefix`/`suffix`を第一級属性として採用する。旧WidgetSpecでの「prefix/suffix廃止」判断はtemplate（`{value}`置換）方式の文脈での判断であり、本DSLには適用しない。

`prefix`と`suffix`は、field、span、textなどの描画可能ノードで利用可能にする。

例:

```xml
<field name="thinking-effort" prefix=" (" suffix=")"/>
```

この場合、fieldの文字装飾がprefix、値、suffixのすべてへ適用される。

括弧と値で色を分けたい場合はspanを使う。

```xml
<span
  optional="thinking-effort"
  prefix=" ("
  suffix=")"
  color="bright-black"
>
  <field name="thinking-effort" color="yellow"/>
</span>
```

## separator

専用のseparatorノードは作らない。separatorは`text`の属性としてopt-inする。

```xml
<text role="separator" padding="1">|</text>
```

`role="separator"`の`text`は、現行rendererと同じ自動折りたたみ（collapsing）セマンティクスを持つ。

* 直前に可視コンテンツがあり、かつ直後（行末まで）に可視コンテンツまたは`flex`がある場合のみ表示する
* 行頭・行末に位置する場合は自動で落ちる
* 連続するseparatorは自動で落ちる（1つに畳まれる）

UIプリセット「Separator」はこの`role="separator"`付きの`text`を生成する。role属性を持たない通常の`text`はcollapsingの対象にならない。

### Output style

文字を省略した`<text role="separator"/>`は、`statusloom`の
`output-style="standard|powerline"`に従う。デフォルトは`standard`。

```xml
<statusloom version="1" tool="claude-code" output-style="powerline">
  <layout name="Powerline" active="true">
    <line>
      <field name="model"/>
      <text role="separator"/>
      <field name="context-percentage-usable"/>
      <flex/>
      <field name="git-branch"/>
    </line>
  </layout>
</statusloom>
```

Powerlineではグリフの前景色に直前の可視コンテンツの`background`、背景色に直後の
可視コンテンツの`background`を使用する。色未指定のセグメントには組み込みテーマの
前景色・背景色を割り当て、明示した`background`を優先する。`flex`では左runを``で
閉じ、端末デフォルト背景の空白を展開し、右runを``で開始する。

Powerlineでは手動separatorを描画せず、line直下の可視field/text/span間へ遷移を
自動生成する。spanとその全子孫はmergeされた1セグメントとして扱う。standardでは
手動separatorの文字とcollapsing動作を従来どおり維持する。
表示にはPowerlineグリフを収録したフォントが必要。

## color-rule

条件によるcolor切り替えを、`<color-rule>`子要素で記述する。field、span、textの子として複数記述できる。

```xml
<field name="context-percentage" color="green">
  <color-rule when="self ge 90" color="red"/>
  <color-rule when="self ge 70" color="yellow"/>
</field>
```

セマンティクス（現行`colorRules`相当）:

* 上から順に評価し、最初に該当したruleの`color`を採用する（先勝ち）
* どのruleにも該当しなければ、親ノードの`color`属性へフォールバックする
* `when`はwhen式構文を再利用する（`when`属性は必須。省略はvalidation error）
* `self`は、そのノードのselfメトリックを参照する
* selfメトリックを持たないfieldや、`text`/`span`では`self`は使えない。名前付きメトリクスのみ参照可能

## formatter

fieldには型に応じたformatterを適用できるようにする。現行実装（`internal/render/format.go`）との対応を次に示す。

初期候補:

* `percent` — `formatPercent`相当（例: `42%`）
* `number` — 桁区切り（`formatThousands`相当）
* `compact-number` — 64.0k形式（`formatCompactK`相当）
* `currency` — `$`+小数2桁。adaptive precision（有効桁に応じた桁数）を候補とする
* `duration` — `42s` / `5m 12s` / `1h 15m`（`formatDuration`相当）
* `countdown` — `2d3h` / `1h23m` / `45m`（`countdown`相当。現行`five-hour-reset`/`weekly-reset`が使用）
* `datetime`
* `boolean`
* `enum`

例:

```xml
<field
  name="context-percentage-usable"
  format="percent"
  precision="0"
/>
```

```xml
<field
  name="session-cost"
  format="currency"
  currency="USD"
  precision="adaptive"
/>
```

formatterの属性は、field definitionの型に応じてsemantic validationする。

未知のformatterや不正な属性はエラーにする。

## fieldカタログ

field名・formatter対応・selfMetric・linkableは、宣言的なfield定義レジストリとしてGo側で一元管理する。現行はrenderの`switch`ハードコードと`webconfig/catalog.go`のハンドメンテが重複しているため、これを単一レジストリへ集約する。DSLのvalidation、Web UIのカタログ提供、rendererはこの単一の情報源を参照する。

### コンテンツfield（現行47種）

`model`、`model-id`、`output-style`、`session-id`、`thinking-enabled`、`git-branch`、`git-changes`、`tool-version`、`current-directory`、`project-directory`、`git-root`、`thinking-effort`、`context-length`、`context-window-size`、`context-remaining`、`context-output-tokens`、`current-input-tokens`、`current-output-tokens`、`cache-creation-tokens`、`cache-read-tokens`、`exceeds-200k`、`context-percentage`、`context-percentage-usable`、`session-cost`、`five-hour-usage`、`five-hour-reset`、`weekly-usage`、`weekly-reset`、`session-name`、`agent-name`、`vim-mode`、`pr-number`、`pr-review-state`、`repo-name`、`worktree`、`session-duration`、`api-duration`、`lines-changed`、`lines-added`、`lines-removed`、`cache-hit-rate`、`git-staged`、`git-unstaged`、`git-untracked`、`git-ahead`、`git-behind`、`git-clean`

`separator`・`flex-separator`はノード（`role="separator"`の`text` / `<flex/>`）へ置き換わるため、fieldカタログには含めない。

linkable（`hyperlink`可）: `pr-number`、`pr-review-state`、`repo-name`

### メトリクス名（when / color-ruleで参照可能）

kebab-caseの名前付きメトリクス:

`api-duration-minutes`、`cache-hit-percent`、`cache-creation-tokens`、`cache-read-tokens`、`context-output-tokens`、`context-percent`、`context-remaining-percent`、`context-tokens`、`context-usable-percent`、`context-window-tokens`、`current-input-tokens`、`current-output-tokens`、`exceeds-200k`、`five-hour-percent`、`five-hour-reset-minutes`、`git-ahead`、`git-behind`、`git-clean`、`git-dirty`、`git-staged`、`git-unstaged`、`git-untracked`、`lines-added`、`lines-changed-total`、`lines-removed`、`session-cost-usd`、`session-duration-minutes`、`seven-day-percent`、`seven-day-reset-minutes`、`thinking-enabled`

selfメトリックを持つfield（`self`参照が可能）と対応メトリクス:

* `context-length` → `context-tokens`
* `context-percentage` → `context-percent`
* `context-percentage-usable` → `context-usable-percent`
* `session-cost` → `session-cost-usd`
* `five-hour-usage` → `five-hour-percent`
* `weekly-usage` → `seven-day-percent`
* `five-hour-reset` → `five-hour-reset-minutes`
* `weekly-reset` → `seven-day-reset-minutes`
* `session-duration` → `session-duration-minutes`
* `api-duration` → `api-duration-minutes`
* `lines-changed` → `lines-changed-total`
* `cache-hit-rate` → `cache-hit-percent`

selfメトリックを持たないfield（`model`、`git-branch`など）では、color-rule/whenで`self`は使えない。名前付きメトリクスのみ参照可能。

## compact・行omit・fallback

現行rendererのセマンティクスを踏襲する。

### compact

* `compact-threshold > 0` かつ `0 < 端末幅 < compact-threshold` のとき、一部fieldはcompact表現になる（例: `context-length` → `64.0k`）
* `role="separator"`の`text`はcompact時に`|`相当の短縮表現になる（現行挙動）
* `raw="true"`のfieldはcompact変換の対象外（生値を優先）

### 行omit

* 行内に可視コンテンツ（`field`/`text`）が1つも無い場合、その行はomitされる（複数行の詰め）

### fallback

* 全行がomitされた場合は、既定のfallbackライン（`model` + `tool-version`）を描画する

## AST

DSLから次のようなASTへ変換する。

概念例:

```go
type Document struct {
    Source string
    Root   *StatusloomNode
}

type StatusloomNode struct {
    Meta     NodeMeta
    Version  string
    Tool     string
    Settings ToolSettings
    Git      *GitSettings // nil = 既定値
    Layouts  []*LayoutNode
}

type ToolSettings struct {
    ColorLevel            string // none|ansi16|ansi256|truecolor
    CompactThreshold      int    // 0 = disabled
    ContextPercentageMode string // raw|usable|both
    ContextReserveTokens  int
}

type GitSettings struct {
    Meta             NodeMeta
    CacheTTLMs       int
    TimeoutMs        int
    IncludeUntracked bool
    CollectNumstat   bool
}

type LayoutNode struct {
    Meta   NodeMeta
    Name   string
    Active bool
    Lines  []*LineNode
}

type Node interface {
    node()
}

type LineNode struct {
    Meta     NodeMeta
    Children []Node
    Common   CommonAttributes
}

type SpanNode struct {
    Meta     NodeMeta
    Children []Node
    Common   CommonAttributes
}

type RawTextNode struct {
    Meta       NodeMeta
    RawValue   string
    RenderText string
}

type TextNode struct {
    Meta   NodeMeta
    Value  string
    Role   string // "" | "separator"
    Common CommonAttributes
}

type FieldNode struct {
    Meta      NodeMeta
    Name      string
    Formatter FormatterConfig
    Raw       bool
    Hyperlink bool
    Common    CommonAttributes
}

type FlexNode struct {
    Meta NodeMeta
    Size string // "full" | "full-minus-N"
}

type ColorRule struct {
    When  string
    Color string
}
```

共通属性例:

```go
type CommonAttributes struct {
    Style      Style
    Box        Box
    Prefix     string
    Suffix     string
    Optional   string
    When       string
    ColorRules []ColorRule // field/span/textで有効
}
```

## DSL表現の維持

Visual Editorで変更した際に、未変更部分まで勝手に書き換えないようにする。

特に次を維持する。

* 生TextNodeは、変更が不要なら生TextNodeのまま維持する
* `<text>`で記述されたものは`<text>`のまま維持する
* `<span>`で記述されたものは`<span>`のまま維持する
* `<text role="separator" padding="1">|</text>`のような表現を勝手に別表現へ変換しない
* 意味的に同じでも、元の表現を可能な限り保持する

各ノードに少なくとも次を持たせる。

```go
type NodeMeta struct {
    SourceRange SourceRange
    Dirty       bool
}
```

source rangeは、`xml.Decoder.InputOffset()`によるノード単位のオフセットで取得する。

可能であれば、元source全体もDocumentに保持する（上記`Document.Source`）。

保存時:

* 未変更ノードは元source sliceを可能な限り再利用する
* 変更ノードだけserializerで再生成する
* 全体canonical formatは通常保存とは分離する

### serializerは自前実装

再生成は自前serializerで行う。標準の`encoding/xml`の`Encoder`は次の理由で再生成に使えない。

* self-closingタグ（`<field .../>`、`<flex/>`）を出力できない
* 属性順を保持しない

したがって、パースのみ`encoding/xml`を用い、再生成（serialize）は自前実装とする。

CLIとして次を想定する。

```bash
statusloom fmt
```

通常保存はminimal diffを優先し、`fmt`実行時のみ全体をcanonicalizeする。`fmt`はwhen式のワード形式正規化も行う。

完全なlossless CST実装が過大であれば、まずはノード単位のsource rangeとdirty管理で実現する。

## Web UIプレビューAPI契約

プレビューAPIはノードID参照で結果を返す（移行済み。旧widget index参照の`POST /api/preview`は削除済み）。

* `render.RenderDocument`が返す`DocSegment`は発生元の`dsl.Node`を保持する
* `POST /api/dsl/preview`のレスポンスsegmentは`nodeId`（`POST /api/dsl/parse`のASTノードIDと同一体系）でノードを参照する
* Visual Editorのプレビュー直接操作は、このノードIDでASTノードと対応付ける

## Visual Editor

Visual EditorとDSL Editorは同じASTを編集する。

### DSLからVisual Editor

```text
DSL
→ parse
→ AST
→ Visual Editor
→ preview
```

### Visual EditorからDSL

```text
Visual Editor operation
→ AST update
→ changed node serialization
→ DSL update
```

### DSL syntax error時

DSL Editorは入力途中で一時的にinvalidになり得る。

状態は分離する。

```go
type EditorState struct {
    SourceText string
    ParsedAST  *Document
    ParseError *Diagnostic
}
```

parse失敗時:

* DSL Editorへdiagnosticを表示する
* Previewは最後に成功したASTを使用する
* 保存を禁止する
* Visual Editorは一時的にread-onlyにする
* DSLが再びvalidになったらVisual Editorを同期する

## UI上の意味的プリセット

ASTやDSLには専用のseparatorノードを作らない。

Visual Editorでは「Separator」を追加可能にしてよいが、実際には次のような`text`を生成する。

```xml
<text role="separator" padding="1">|</text>
```

同様に、Model with EffortなどもUIプリセットとして提供し、AST上は基本ノードへ展開する。

例:

```xml
<text>Model: </text>
<field name="model"/>
<span
  optional="thinking-effort"
  prefix=" ("
  suffix=")"
>
  <field name="thinking-effort"/>
</span>
```

## コメント

XMLコメントをサポートする。

```xml
<!-- Model and effort -->
```

コメントはAST上でCommentNodeとして保持し、レンダリング時には無視する。

通常保存やVisual Editor操作で、関係のないコメントを削除・移動しない。

## validation

次を検証する。

* XMLとしてwell-formedであること
* ルート要素が`statusloom`であること
* rootの子が`git`（0または1個）と`layout`のみであること（`line`直下禁止）
* `git`要素が2個以上ないこと
* `git`要素の属性が正しいこと（未知の属性、数値・boolean属性の不正値）
* activeなlayoutがちょうど1つであること（重複・欠如はエラー。単一layout時のみ`active`省略可）
* 未知の要素
* 未知の属性
* 必須属性不足
* toolで利用できないfield
* field型とformatterの不一致
* 不正なprecision
* 不正なcolor
* 不正なboolean属性
* 不正な`role`値（`separator`以外）
* 非linkableなfieldへの`hyperlink`指定
* `color-rule`の`when`属性欠如
* `self`を参照できないノード（selfメトリック無しfield / text / span）での`self`使用
* `flex`の不正な`size`書式（`full` / `full-minus-<N>`以外）
* 不正なtool-level設定値（`color-level`、`context-percentage-mode`の列挙外など）
* `text`要素内の改行
* 不正なwhen式
* 未知のfield参照 / 未知のメトリクス参照
* 不正なpadding値
* 不正なネスト

diagnosticには可能な限りsource rangeを含める。

```go
type Diagnostic struct {
    Severity Severity
    Message  string
    Range    SourceRange
}
```

## パッケージ構成案

```text
internal/dsl/
├── ast.go
├── parser.go
├── serializer.go
├── formatter.go
├── registry.go      // fieldカタログ / selfMetric / linkable の単一レジストリ
├── diagnostics.go
├── source.go
├── attributes.go
├── condition.go
└── validation.go
```

既存rendererとは次の境界を持たせる。

```text
DSL
→ AST
→ evaluation
→ styled spans
→ ANSI renderer
```

DSL parser内でANSI文字列を直接生成しない。

## テスト

少なくとも次を追加する。

### Parser

* mixed content
* raw text
* text element
* span nesting
* field
* flex
* comments
* prefix/suffix
* padding
* optional
* when（ワード形式・記号形式）
* layout / active
* git要素（省略時の既定値・属性parse）
* invalid XML
* unknown node
* unknown attribute

### Whitespace

* whitespace-only raw nodeは無視
* raw textの前後空白は無視
* raw text内部の改行を含む空白連続は1個のスペースへ折りたたみ
* raw text内部の空白（改行を含まない連続）は維持
* text内部の前後空白は維持
* text内部の改行はエラー
* indentationが描画へ影響しない

### Style inheritance

* 親styleの継承
* 子styleの上書き
* prefix/suffixは所有ノードのstyle
* paddingは所有ノードのstyle
* layout属性は継承しない

### Visibility

* optional missing
* optional empty string
* optional number zero
* optional boolean false
* when true/false（ワード・記号両形式）
* optionalとwhenのAND
* メトリクス解決不能時は非表示

### Separator / flex

* `role="separator"`の行頭・行末collapsing
* 連続separatorのcollapsing
* 直後がflexの場合のseparator表示
* flex（full / full-minus-N）の残余埋め
* 同一行複数flexのmin採用+均等分配
* 端末幅不明時のflexフォールバック（空白1個）

### color-rule / hyperlink / raw

* color-ruleの先勝ち評価と親colorフォールバック
* selfメトリック参照
* linkable fieldのhyperlink出力（OSC 8）
* raw値出力（ラベル・装飾なし）

### compact / omit / fallback

* compact-threshold発動時のfield短縮（例: `64.0k`）
* compact時のseparator短縮
* rawはcompact対象外
* 可視コンテンツ無しの行omit
* 全行omit時のfallbackライン（model + tool-version）

### Layout / root validation

* active一意性（重複・欠如エラー）
* 単一layout時のactive省略
* git要素の重複エラー
* git要素の不正な数値・boolean属性値エラー

### Serialization

* 未変更raw textの維持
* 未変更text elementの維持
* 未変更spanの維持
* 変更ノードのみ再生成
* self-closingタグ・属性順の維持
* コメント維持
* canonical formatter（when式ワード形式正規化を含む）

### Rendering

入力:

```xml
<statusloom version="1" tool="claude-code">
  <layout name="default" active="true">
    <line>
      <text>Model: </text>
      <field name="model" color="cyan" bold="true"/>

      <span
        optional="thinking-effort"
        prefix=" ("
        suffix=")"
        color="bright-black"
      >
        <field name="thinking-effort" color="yellow"/>
      </span>

      <text role="separator" padding="1">|</text>

      <text>Context: </text>
      <field
        name="context-percentage-usable"
        format="percent"
        precision="0"
      />
    </line>
  </layout>
</statusloom>
```

期待出力:

```text
Model: Opus-4.8 (high) | Context: 42%
```

thinking-effortが空の場合（separatorは前後関係により維持される）:

```text
Model: Opus-4.8 | Context: 42%
```

## 実装フェーズ

### Phase 1: Go側DSLコア

1. ASTと共通属性を定義する（tool-level設定 / layout / role / color-rule / flex / hyperlink / raw を含む）
2. XML parserを実装する
3. raw textとtextの空白ルールを実装する
4. 自前serializerを実装する（self-closing・属性順対応）
5. style継承を実装する
6. prefix、suffix、paddingを実装する
7. fieldレジストリとformatterを接続する
8. optionalを実装する
9. when parser/evaluatorを実装する（ワード形式を第一級、記号形式も受理）
10. ASTからstyled spansへのevaluationを実装する（separator collapsing / flex / color-rule / compact / 行omit / fallback を含む）
11. 既存ANSI rendererへ接続する

### Phase 2: Web UI切替 + DSL Editor

12. migrationを実装する（`import ccstatusline`の生成先を新DSLへ、既存config.jsonからの移行）
13. Web UIを新ASTへ切り替える（プレビューAPI契約をノードID参照へ更新）
14. DSL Editorとの同期を実装する

### Phase 3: minimal diff保存 + canonical fmt

15. minimal diff保存を実装する
16. canonical formatter（`statusloom fmt`）を実装する

波及範囲（各フェーズで同一変更内に含める）:

* draft共有: DSLテキストベースへ移行済み（`<tool>.draft.xml`、versionはソーステキストのsha256 hex。旧`config.draft.json`は削除済み）
* `import ccstatusline`: 生成先を新DSLへ変更する
* golden test（`internal/render/testdata`）: fixtureを新DSLへ移行する
* Web UI: 全面改修 + DSL Editor新設

## 付録: 新設が必要なメトリクス候補

`when` / `color-rule`で参照したいが、現行メトリクス一覧に存在しないもの。実装時に新設を検討する。

* `git-dirty`（boolean） — 作業ツリーに変更があるか。本書の`<text when="git-dirty eq true">●</text>`例で使用。現行の`git.dirty`のようなドット記法は廃止し、kebab-caseの名前付きメトリクスとして新設する

（新設メトリクスもfieldレジストリと同じ単一の情報源で定義し、when/color-ruleのvalidationが参照できるようにする。）
