# Responsive container (`<responsive>`/`<variant>`) — 実装契約

コンテナ単位レスポンシブ。幅の数値（マジックナンバー）を書かず、**自動 first-fit** で
「全行が収まる最初のバリアント」を選ぶ。前回までの設計議論で確定した仕様を実装契約として固定する。

## 1. 目的・挙動

- `<responsive>` は複数の表現候補 `<variant>` を **広い順** に持つ。
- レンダラは各 variant の全行の自然幅を実測し、**全行が `COLUMNS`(Options.Width) に収まる最初の variant** を採用する。
- どの variant も収まらなければ **最後の variant**（＝最も詰めた / 複数行の fallback）。
- 幅不明（`Options.Width <= 0`）→ **先頭 variant**（既存 width-unbounded と一貫。widest を採用）。
- 新しいメトリック・新しい数値属性は **追加しない**。

### 「収まる」の定義
- variant が収まる ⟺ その variant の **全 line** が収まる。
- line が収まる ⟺ その line の **自然幅（natural width）≤ Options.Width**。
- 自然幅 = flatten + gate(optional/when) + markSeparators 適用後の、可視な content/separator piece の
  `visibleWidth(plain)` 合算。**flex piece は幅0として扱う**（flex は残余を埋めるだけで、base が幅以内なら必ず満たせる）。
  → 既存 `computeFlexWidths`（render.go）内で計算している `base` と同一定義。これを per-line ヘルパーに切り出して再利用する。
- 計測は現在の `e.compact` 状態で行う（compact は全体で固定なので計測は一貫）。

## 2. AST（internal/dsl/ast.go）

`LayoutNode.Lines []*LineNode` を廃止し、順序保持の子リストへ置換する。

```go
// LayoutChild is a layout's ordered child: *LineNode or *ResponsiveNode.
type LayoutChild interface{ isLayoutChild() }

func (*LineNode) isLayoutChild()       {}
func (*ResponsiveNode) isLayoutChild() {}

type LayoutNode struct {
    Meta     NodeMeta
    Name     string
    Active   *bool
    Children []LayoutChild // ordered mix of *LineNode | *ResponsiveNode
    Comments []*CommentNode
}

// ResponsiveNode is a <responsive> width-adaptive container (layout child only).
type ResponsiveNode struct {
    Meta     NodeMeta
    Variants []*VariantNode
    Comments []*CommentNode // comments directly under <responsive>, between variants
}

// VariantNode is one <variant> rendering candidate inside <responsive>.
type VariantNode struct {
    Meta     NodeMeta
    Lines    []*LineNode
    Comments []*CommentNode // comments directly under <variant>, between lines
}
```

- `ResponsiveNode`/`VariantNode` は `Node` ではない（`LineNode` が Node でないのと同様）。属性なし（`<responsive>`/`<variant>` は装飾属性を持たない。当面 `<variant>` に属性なし）。

## 3. 文法・配置ルール（parser + validation）

- `<responsive>` は **`<layout>` 直下のみ**。`<line>`/`<span>` の中や root 直下に出たら validation error。
- `<responsive>` の子は **`<variant>` のみ**（1個以上必須。0個は error）。他要素・生テキストは error。
- `<variant>` の子は **`<line>` のみ**（1個以上必須。0個は error）。他要素は error。
- **入れ子 responsive は禁止**（`<variant>` 内の line children に responsive は型上出現しないが、`<responsive>` を
  `<variant>` 直下や `<responsive>` 直下に置いた場合も error にする）。
- active layout「常にちょうど1つ」ルールは不変。responsive はその内側。
- variant 内の各 line は既存 `validateLine` 相当（common 属性・子ノード検証）を通す。

## 4. レンダリング（internal/render/doc.go）

- `RenderDocument` は `layout.Lines` の代わりに `layout.Children` を走査：
  - `*LineNode` → 従来どおり `renderLineDoc` で 1 DocLine。
  - `*ResponsiveNode` → §1 のアルゴリズムで variant を選択し、その **各 line を `renderLineDoc` で展開**して DocLine 列に追加。
- 出力 DocLine 数は選択 variant により **動的**。既存コメントの「layout.Lines と 1:1」不変条件は
  「**選択後の出力 line と 1:1**」へ更新する。`RenderDocumentString` / fallback は変更不要（非omitted結合のまま）。
- variant 選択ヘルパー・自然幅ヘルパーを render.go/doc.go に追加。**flex は幅0扱い**で fit 判定。

## 5. シリアライザ

- `serializer.go`（正規化）と `serializer_minimal.go`（minimal-diff）両方で `<responsive>`/`<variant>` を出力。
- `canonicalize.go` も layout children の順序を保持して処理。
- round-trip（parse→serialize→parse）でバイト等価/構造等価を保つテストを追加。
- コメント（responsive/variant 直下）も round-trip 保存する。

## 6. Web API（internal/webconfig/astjson.go, dsl.go, DSL_API.md）

### node-ID スキーム（DSL_API.md 更新）
既存は `L{i}.{j}` が「layout i の j 番目の line（layout.Lines のindex）」。
child が line/responsive の混在になるため **layout children の位置 index** に変更する
（responsive 無しなら従来と同一 ID になり後方互換）。

```
L{i}                     i 番目の layout
L{i}.{p}                 layout i の p 番目の子（line または responsive）
L{i}.{p}.v{v}            responsive(L{i}.{p}) の v 番目の variant
L{i}.{p}.v{v}.{j}        その variant の j 番目の line
L{i}.{p}.v{v}.{j}.{k}    その line の k 番目の子（以降は既存 children スキーム）
L{i}.{p}.{k}             (line 直下childの場合) 既存どおり
```

- `buildAST`（astjson.go）: layout は `children`（line/responsive 混在配列）を出す。
  responsive ノードの JSON は `{"id","kind":"responsive","range","variants":[...]}`、
  variant は `{"id","kind":"variant","range","lines":[...]}`。comments も従来同様に付す。
- 逆方向（`jsonTo*`。JSON→AST）も responsive/variant を復元する（AST JSON を書き戻す経路がある場合）。
  DSL_API.md の該当節を更新。
- preview: レンダラは選択 variant のみ segment を出す。preview segment の `nodeId` は
  選択 variant の line children ID（`L{i}.{p}.v{v}.{j}.{k}`）に一致させる。非選択 variant は segment を出さない。
  editor が幅を変えて各 variant を確認できるよう、preview は要求幅で選択された結果を返す（既存の width 指定を踏襲）。

## 7. ドキュメント（markup.md）

`<responsive>`/`<variant>` の節を追加：要素定義、first-fit 選択、幅不明時=先頭、fallback=最後、
数値不要、配置・入れ子ルール、compact との関係（当面併存。将来 compact-threshold 非推奨の余地）。

## 8. テスト・完了条件

- 既存 golden（internal/render/testdata）は **byte 等価のまま**（responsive を含まないため不変）。`-update` 禁止。
- 追加テスト：
  - render: 幅別の variant 選択（広い→先頭 / 中→中間 / 狭い→fallback / 幅不明→先頭 / 全滅→最後）。flex を含む variant の fit。
  - validation: responsive の配置違反・variant/line 0個・入れ子禁止・active ルール不変。
  - serializer（両方）+ canonicalize: round-trip、コメント保存。
  - webconfig: buildAST の responsive/variant 形、node-ID、preview の選択 variant segment。
- `gofmt -l .` 空 / `go vet ./...` / `go test ./...` 全緑（backend）。
- フロント：`pnpm --filter @statusloom/configurator build && test` 緑。

## 9. 影響ファイル

backend（Go・1エージェント一括）:
`internal/dsl/{ast,parser,validation,serializer,serializer_minimal,canonicalize}.go`,
`internal/render/{doc,render}.go`, `internal/cli/doctor.go`,
`internal/webconfig/{astjson,dsl}.go`, `markup.md`, `internal/webconfig/DSL_API.md`
（+ 各 `_test.go`）。

frontend（後続・別エージェント）:
`apps/configurator/*` — variant の追加/並べ替え(DnD)/削除、プレビュー幅切替で選択結果を可視化。
onDragOver 中の構造変更禁止（React #185）。TUI 的操作禁止（パレット+DnD+直接操作のみ）。

## 10. 参照実装例

```xml
<layout active="true">
  <line><field name="model"/></line>            <!-- 固定 -->
  <responsive>
    <variant>                                     <!-- 広い: 1行 -->
      <line>
        <field name="context-percentage" prefix="ctx "/>
        <text role="separator" padding="1">|</text>
        <field name="session-cost" prefix="$"/>
      </line>
    </variant>
    <variant>                                     <!-- 狭い: 2行 (常に収まる=fallback) -->
      <line><field name="context-percentage" prefix="ctx "/></line>
      <line><field name="session-cost" prefix="$"/></line>
    </variant>
  </responsive>
</layout>
```
