# DSL API (`/api/dsl/*`)

The DSL-native configurator API — the sole configuration API (the legacy
widget-index endpoints have been removed). Alongside these `/api/dsl/*` paths
the server also serves `GET /api/tools`, `GET /api/sessions`,
`GET /api/usage/probe`, the history endpoints (`GET /api/history`,
`GET /api/history/{id}`, `POST /api/history/{id}/restore` — see "History API"
below), the Markdown exchange-format endpoints (`POST /api/exchange/import`,
`GET /api/exchange/export` — see "Exchange API" below), the live-preview and
embedded-terminal channels, and `POST /api/shutdown`. Every path is under
`/api/`, so the same security applies: 127.0.0.1-only bind, `Authorization:
Bearer <token>`, Host + Origin validation, no cookies.

Exactly one tool is supported: `claude-code` (the regular session
`statusLine`). `GET /api/tools` returns this single entry. Every endpoint
below that takes a `tool` parameter accepts only this value; any other tool
name is rejected with `400`. The document is `claude-code`'s current
committed revision in the internal store (`<configDir>/statusloom.json`; see
the root `CLAUDE.md`'s "設定ファイル配置"), with its own draft working node
(`refs.claude-code.draft`).

Claude Code's `subagentStatusLine` (one row per agent-panel task) is a
`<subagent>` region (markup.md "subagent") nested inside the `claude-code`
document itself — directly under `<layout>` when the layout has no
`<responsive>`, or inside each `<variant>` when it does — so the same
document, the same width breakpoints, and the same visual editor canvas cover
both the main status line and its subagent row (see
`plans/subagent-region-dsl.md`). Its `task-*` fields (`Category: "subagent"`)
are merged into the single `claude-code` field/metric catalog (`GET
/api/dsl/fields`/`/metrics`); the field-scope validator enforces that
`task-*` fields only appear inside `<subagent>` and non-`task-*` fields never
appear inside one. `POST /api/dsl/preview`'s `section` field selects which
region a request previews — see that endpoint below.

## AST JSON

A parsed document is represented as a tree of node objects. Every node has:

```jsonc
{
  "id": "L0.0.1",          // deterministic, position-derived (see Node IDs)
  "kind": "field",         // node kind (see below)
  "range": { "start": 42, "end": 78 },  // byte offsets into the source
  // ...attributes, keyed by their DSL attribute name (kebab-case)...
  "children": [ /* nodes */ ],   // line/span (mixed content); also layout (ordered line|responsive)
  "colorRules": [ /* color-rule nodes */ ]  // field/span/text only
}
```

A `layout` node's `children` is an ordered mix of `line` and `responsive`
nodes (the layout's rendered rows and its width-adaptive containers). A
`responsive` node has a `variants` array (widest first); each `variant` has a
`lines` array and an optional `when` (its candidacy gate). The renderer first
drops the variants whose `when` is false, then picks the first remaining one
all of whose lines fit the terminal width (unknown width → first/widest
candidate, none fit → last candidate). When EVERY variant is gated out the
responsive renders no lines at all, and `selectedVariants` reports `-1` for it.
By default `/api/dsl/preview` emits segments only for this width-selected
variant's lines, matching what the real statusline renders. With
`allVariants: true` it instead emits every variant's lines (each rendered as
if it were the selected one) and reports which index was actually selected
per responsive in the response's `selectedVariants` map — see
`POST /api/dsl/preview` below.

- Attribute keys are the DSL attribute names verbatim (`color`, `bold`,
  `padding`, `padding-left`, `prefix`, `role`, `name`, `format`, `precision`,
  `currency`, `raw`, `hyperlink`, `min-width`, `align`, `optional`, `when`,
  `size`, `color-level`, `compact-threshold`, ...).
- **Omitted attributes are absent from the object** (not `null`/`""`). A bool
  attribute present as `false` means an explicit `false` (e.g. `bold="false"`
  overriding an inherited `true`); its absence means "inherit/unspecified".
- `padding="N"` is emitted when left and right padding are equal; otherwise
  `padding-left` / `padding-right` are emitted individually.
- `raw` and `hyperlink` are emitted only when `true`.
- `dirty` (bool, absent = `false`) marks a node the visual editor changed. The
  server never emits it (parsed nodes are always clean); the client sets it on
  edited nodes so `POST /api/dsl/serialize` with a `baseSource` can regenerate
  only those nodes and reuse the rest verbatim (see that endpoint).

### Node kinds and their shape

| kind | attributes | container fields |
|------|-----------|------------------|
| `statusloom` | `version`, `tool`, `color-level`, `compact-threshold`, `context-percentage-mode`, `context-reserve-tokens` | `git?`, `layouts[]`, `comments[]?` |
| `git` | `cache-ttl-ms`, `timeout-ms`, `include-untracked`, `collect-numstat` | — |
| `layout` | `name`, `active` | `children[]` (ordered `line` \| `responsive`), `subagent?`, `comments[]?` |
| `responsive` | — | `variants[]`, `comments[]?` |
| `variant` | `when?` | `lines[]`, `subagent?`, `comments[]?` |
| `subagent` | — | `line?` (its single `<line>`), `comments[]?` |
| `line` | *common* | `children[]` |
| `span` | *common* | `children[]`, `colorRules[]?` |
| `text` | `role`, `value`, *common* | `colorRules[]?` |
| `field` | `name`, `format`, `precision`, `currency`, `raw`, `hyperlink`, `min-width`, `align`, *common* | `colorRules[]?` |
| `flex` | `size` | — |
| `raw-text` | `value` | — |
| `comment` | `value` | — |
| `color-rule` | `when`, `color` | — |

*common* = the drawable-node display attributes: `color`, `background`, `bold`,
`dim`, `italic`, `underline`, `strikethrough`, `padding` / `padding-left` /
`padding-right`, `prefix`, `suffix`, `optional`, `when`.

- `text` / `raw-text` / `comment` carry their text in `value`.
- Color-rules are **not** in `children`; they live in a separate `colorRules`
  array so `children` indices stay aligned with the renderer's leaf nodes.
- `subagent` is a `layout`'s or `variant`'s **optional**, at-most-one region
  (markup.md "subagent"): the row(s) `statusloom claude-subagent` renders
  below the main status line, one row per running/completed task. It carries
  no attributes of its own and holds a single `line` (never `responsive` /
  `variant` / a second `line` — the parser/validator reject those). A layout
  with a `<responsive>` child instead nests its `subagent` inside each
  `variant` it wants a subagent design for (a `variant` with none falls back
  to the layout's own at render time, see `POST /api/dsl/preview`'s
  `section="subagent"` below).

### Node IDs

IDs are deterministic paths derived from a node's position — no random
component, stable across parses of the same source, and identical to the IDs a
`/api/dsl/preview` segment reports for the same node.

```
root                 the <statusloom> root
git                  the optional <git/> element
root.c{k}            k-th XML comment directly under the root
L{i}                 i-th <layout>
L{i}.c{k}            k-th comment directly under layout i
L{i}.{p}             p-th child of layout i — a line or a responsive (index into layout.children)
L{i}.{p}.v{v}        v-th <variant> of responsive L{i}.{p}
L{i}.{p}.v{v}.{j}    j-th <line> of that variant
L{i}.s               the layout-direct <subagent> region's <line>
L{i}.{p}.v{v}.s      the <subagent> region's <line> inside variant L{i}.{p}.v{v}
{parent}.{k}         k-th mixed-content child of a line/span (index into children)
{owner}.cr{c}        c-th <color-rule> of a field/span/text owner
```

`children` nest, so a field inside a span inside a line reads e.g.
`L0.2.1.0` (layout 0, child 2 = line, child 1 = span, child 0 = field). A field
inside a responsive's variant reads e.g. `L0.1.v0.0.1` (layout 0, child 1 =
responsive, variant 0, line 0, child 1). A **responsive-free** layout's line at
position `p` keeps the historical `L{i}.{p}` form (child index equals line
index), so IDs are unchanged for such documents. Comments that appear inside a
line/span are ordinary `children` entries (kind `comment`) and take a numeric
child index; root-level, layout-level, responsive-level, and variant-level
comments use the `.c{k}` form.

A `<subagent>` region is a dedicated field on its `layout`/`variant` (never a
`children` entry), so it does not consume a position index the way a
`responsive` does; its id is its container's id with a `.s` suffix appended
directly: `L{i}.s` for a layout-direct region, `L{i}.{p}.v{v}.s` for one
nested in variant `L{i}.{p}.v{v}`. A `<subagent>` holds at most one `<line>`,
and — mirroring the responsive-free-layout rule above — that line reuses the
region's own id rather than gaining an extra nesting level, so its children
read `L{i}.s.{k}` / `L{i}.{p}.v{v}.s.{k}`. For example, in a responsive-free
layout 0 whose `<subagent><line><field .../><field .../></line></subagent>`
holds two fields, their ids are `L0.s.0` and `L0.s.1`.

## Endpoints

### `GET /api/dsl/document?tool=claude-code`
→ `200 { "source": string, "version": string, "exists": bool }`

`source` is tool's current committed revision's source (or the built-in
default when `exists` is `false`). `version` is `sha256(tool + "\0" +
source)` hex (the store's `SourceVersion`). The raw source is returned even
if it is invalid, so the editor can display and repair it — though in
practice a committed revision is always valid, since `PUT` (below) is the
store's validation boundary.

### `PUT /api/dsl/document` `{ "tool", "source" }`
→ `200 { "version", "diagnostics": [] }` on save
→ `409 { "version", "diagnostics": [...] }` when `source` has error diagnostics

Parses and validates `source` (the store's `Save`, origin `"ui"`).
**Error-severity diagnostics block the save** (409, nothing written; no new
revision). Warning-only source is saved as a new revision and the warnings
are returned (a save identical to the current tip is a no-op: no new
revision, `current` unchanged).

### `POST /api/dsl/parse` `{ "source" }`
→ `200 { "ast"?: node, "diagnostics": [...], "version": string }`

Never writes. `ast` is present only when a root element was parsed (absent for a
fatal XML well-formedness error). Use this for the DSL Editor's live analysis.

### `POST /api/dsl/serialize` `{ "ast": node, "baseSource"?: string }`
→ `200 { "source": string, "diagnostics": [...] }`

Turns an AST (the visual editor's working tree) back into DSL source via the
self-serializer, then reports diagnostics from re-parsing it. The AST must have
a `statusloom` root (else `400`).

- Without `baseSource`: output is the whole-document **canonical** form.
- With `baseSource` (the client's last valid source, into which the AST's node
  `range`s index): **minimal-diff** serialization. Nodes that are not `dirty`
  and whose subtree is unchanged are emitted verbatim from `baseSource`; only
  `dirty` nodes and client-inserted nodes (no/zero `range`) are regenerated.
  This preserves comments, raw text, symbolic-operator `when` expressions, and
  custom indentation across visual edits. When no node is dirty and every range
  is valid, the output equals `baseSource` byte-for-byte.

### `GET /api/dsl/draft?tool=claude-code`
→ `200 { "source", "version", "exists" }`

The tool's draft working node (the store's `refs.<tool>.draft`). `exists`
reflects the draft's presence; when absent, `source` falls back to the
current committed document, then the built-in default.

### `PUT /api/dsl/draft` `{ "tool", "source" }`
→ `200 { "version", "diagnostics": [...] }`

Writes `source` to the tool's draft working node **unconditionally**
(last-writer-wins; no new revision is ever created). The draft is a
text-sharing channel that tolerates in-progress, invalid input; diagnostics
are returned for the editor but never block the write.

### `POST /api/dsl/preview` `{ "tool", "source", "width", "sample", "sessionId"?, "layoutIndex"?, "allVariants"?, "section"? }`
→ `200 { "lines": [...], "diagnostics": [...], "fallback": { "ansi", "active" }, "selectedVariants"?, "subagentPreview"? }`

```jsonc
"lines": [
  {
    "omitted": false,
    "ansi": "…",                       // concatenated visible-segment ANSI
    "segments": [
      { "nodeId": "L0.0.1", "text": "Opus 4.8", "ansi": "[36m…", "visible": true }
    ]
  }
]
```

- Parses `source`; **unparseable source yields `lines: []` and diagnostics
  only** (the last good AST is the client's responsibility to keep).
- `segments[].nodeId` matches the AST node ID (empty for decoration segments
  with no owning node, e.g. a `<line>`'s own prefix/suffix or the fallback
  line). Span prefix/suffix/padding segments carry the span's node ID.
- `layoutIndex` (default 0, clamped) previews a layout other than the
  document's active one.
- `allVariants` (default `false`) changes how `<responsive>` containers are
  previewed. By default `lines` carries only the width-selected variant's
  lines — what the real statusline renders — so a non-selected variant's
  chips have no preview data. With `allVariants: true`, `lines` instead
  carries **every** variant's lines, each rendered with its real values at
  `width` as if it were selected, and the response additionally includes
  `selectedVariants`: a map from each `<responsive>`'s AST node ID
  (`"L{i}.{p}"`) to the variant index that width would actually select
  (e.g. `{ "L0.1": 0 }`), or `-1` when every variant is gated out by its
  `when` and the responsive renders nothing. This is what the config editor's canvas uses so
  every variant card shows real values with the active one marked, instead
  of falling back to placeholder text for the others; the field is present
  (possibly `{}`) whenever `allVariants` was requested, and absent otherwise.
- `sessionId` renders against a real cached session (see `GET /api/sessions`);
  otherwise `sample` selects a synthetic snapshot, defaulting (when omitted)
  to `"full"` for the main section (`"early-session"` is the other main
  sample) or `"subagent-running"` for the subagent section
  (`"subagent-completed"` is the other subagent sample — see `section`
  below). `width` is clamped to [20, 400] (default 120).
- `fallback.active` is `true` when every line is omitted, in which case
  `fallback.ansi` is the fallback line (model + tool-version).
- `section` (default `""`/`"main"`) selects which region of the document this
  request previews (markup.md "subagent"):
  - `""` / `"main"`: unchanged from before — `lines` carries the main status
    line only; the `<subagent>` region is never rendered here (matching the
    real `claude` render pass exactly). `subagentPreview` is absent.
  - `"subagent"`: previews the `<subagent>` region(s) instead. `lines` is
    always `[]` and `fallback.active` is always `false` (they are meaningless
    for this section). The response instead carries **`subagentPreview`**: a
    map from a **container's** AST node ID to an array of rendered rows, one
    row per task in the sample's task list — `dslPreviewLine` (the same shape
    as an entry of `lines`) each. A container is the layout itself
    (`"L{i}"`) when it has no `<responsive>`, or each of its variants
    (`"L{i}.{p}.v{v}"`) when it does; a variant with no `<subagent>` of its
    own falls back to the layout's (same underlying node, so its rows'
    segment `nodeId`s are the layout-level `"L{i}.s.*"`, not that variant's
    own `"L{i}.{p}.v{v}.s.*"`) — mirroring exactly how the real
    `claude-subagent` render path resolves which region to use, so the
    preview never disagrees with the real output. A container with no
    `<subagent>` anywhere is omitted from `subagentPreview`. `sample` for
    this section is one of `"subagent-running"` / `"subagent-completed"`:
    each names an ordered task list (currently three tasks), rendered
    independently per container. `selectedVariants` is still included under
    the same "whenever `allVariants` was requested" rule as the main section,
    computed against the main section's own default sample/sessionId (so it
    reports the same width-selected variant the main line would show — the
    same-width, same-variant guarantee plans/subagent-region-dsl.md
    requires).

### `GET /api/dsl/fields?tool=claude-code`
→ `200 { "fields": [ { "name", "displayName", "descriptions": {"en","ja"}, "category", "linkable"?, "requiresVar"?, "selfMetric"?, "formats"?, "capability"?, "preview": {"text","ansi"} } ] }`

`requiresVar` marks a field parameterized by a `var` attribute (`env` is the
only one today): the properties panel must render a variable-name input for it,
and a `<field>` AST node for it carries `"var"` (string) and `"unmask"`
(boolean, emitted only when true). A missing `var` produces no diagnostic at
all - neither error nor warning - so a freshly dropped palette item stays
savable and does not pollute the agent's stderr on every render; the field just
renders empty.

The field catalog for the palette, sourced entirely from the Go DSL registry
(single source of truth): the session/account/git field set **plus** the
`task-*` set merged in from the subagent task field catalog (one entry per
subagentStatusLine task attribute: `task-description`,
`task-model`, `task-model-id`, `task-status`, `task-tokens`,
`task-context-size`, `task-context-percent`, `task-duration`, `task-effort`;
all `category: "subagent"`, valid only inside a `<subagent>` region). `preview`
is a single-field rendering against the `"full"` sample snapshot, which also
carries a sample task so `task-*` fields render real content rather than
falling back. `capability` (optional; absent = always available) names a
runtime capability the field depends on: `"oauth-usage"` (the authenticated
OAuth usage API) for the extra-usage / weekly-usage / weekly-reset fields, or
`"subagent-effort"` for `task-effort` (Claude Code does not yet report
per-task reasoning effort, so this capability is unavailable in every
environment today). Capability gating is entirely client-side: the server
only tags the field, it never filters the response by availability. The
configurator probes `"oauth-usage"` via `GET /api/usage/probe` and hides
those fields from the palette when unreachable; there is no analogous probe
for `"subagent-effort"` yet, so a `capability`-tagged field with no matching
probe should be hidden unconditionally until one exists.

### `GET /api/dsl/metrics?tool=claude-code`
→ `200 { "metrics": [ { "name", "displayName", "descriptions": {"en","ja"}, "percent", "text", "values" } ] }`

The named-metric catalog for `when` / `color-rule` editing, from the DSL
registry: the session/account metrics plus the `task-*` self-metrics backing
`task-tokens`/`task-context-size`/`task-context-percent`/`task-duration`, and
the tool-agnostic `width` metric (terminal width in columns), which backs
width breakpoints such as `when="width ge 80"`; an unknown width counts as
unbounded so a width condition never hides content. `percent` (boolean,
optional, omitted when `false`) marks a 0..100-scale percentage metric (e.g.
`five-hour-percent`, `context-percent`); the configurator uses it to surface
percentage metrics as threshold-bar-driven color-rule candidates.

`text` (boolean, optional, omitted when `false`) marks a STRING-valued metric —
the `account-*` identity metrics (`account-type`, `account-plan`,
`account-seat`, `account-role`, `account-org`, `account-email`). They compare
with `eq` / `ne` against a quoted string literal only (an ordering operator is a
type error), and `values` (optional) lists the known values when the metric is a
closed enumeration (`account-type`: `claude_max` / `claude_team`); a value
outside that list is still valid, since the strings come from Claude Code. Their
main use is a conditional `<variant when="account-type eq &quot;claude_team&quot;">`.

### `GET /api/usage/probe`
→ `200 { "available": bool, "reason": "ok" | "no-token" | "unauthorized" | "rate-limited" | "error", "extraUsageEnabled": bool }`

Capability detection for the authenticated OAuth usage API. Requires the same
`Authorization: Bearer <token>` as every other `/api/*` route. **Always returns
`200`** — the probe result is data describing availability, never an HTTP error.

The configurator calls this once on load and uses `available` to decide whether
to show `capability:"oauth-usage"` fields in the palette at all (hidden when
unavailable). `reason` explains the outcome:

- `ok` — the usage API responded successfully (`available: true`).
- `no-token` — no OAuth credential could be resolved (`available: false`).
- `unauthorized` — the API rejected the credential, `401` (`available: false`).
- `rate-limited` — the API is throttling, `429`; the capability exists, so
  `available: true`.
- `error` — any other failure (network, unexpected status) (`available: false`).

`extraUsageEnabled` is meaningful only when `reason` is `ok`: it reports whether
the account has extra (metered) usage enabled at all.

### History API

The internal store (`<configDir>/statusloom.json`) keeps every committed
revision of a tool's whole document in a single pool plus per-tool refs
(`current` + an inline `draft` working node), modeled on git's objects/refs
(plans/config-store-and-format.md §3–§4). These three endpoints expose that
history: listing, single-revision lookup, and restore. Like every `/api/*`
route they require the same `Authorization: Bearer <token>` and Host/Origin
validation.

Because a revision id is a UUIDv7 minted once at commit time, it is globally
unique across every tool sharing the store — the two by-id endpoints below
take no `tool` parameter; the id alone resolves it (via the revision's own
`tool` field).

#### `GET /api/history?tool=claude-code`
→ `200 { "revisions": [...], "refs": { "current", "draft" } }`

```jsonc
"revisions": [
  {
    "id": "01958f2a-...",
    "parent": "01958f29-..." | null,
    "savedAt": "2026-07-17T09:00:00Z",   // RFC3339
    "origin": "ui" | "cli" | "import",
    "meta": { "name"?, "description"?, "author"?, "notes"? }
  }
]
```

`revisions` lists every revision belonging to `tool`, **oldest-first** by
`savedAt` (ties broken by id ascending) — a flat chronological listing, not
necessarily a single linear chain: a past `restore` (below) can leave more
than one revision with the same parent (an implicit branch), and every branch
still shows up here. The source text is deliberately omitted (kept out of the
listing payload so it stays light with many revisions) — fetch it via
`GET /api/history/{id}`.

`refs.current` is `tool`'s current committed revision id (the one the render
path and `GET /api/dsl/document` read); `refs.draft` reports only whether a
draft working node exists (never its source — that stays behind
`GET /api/dsl/draft`).

An unrecognized `tool` is `400` (same `knownTool` gate as the `/api/dsl/*`
endpoints; only `claude-code` is served today).

#### `GET /api/history/{id}`
→ `200 { "id", "tool", "source", "parent", "savedAt", "origin", "meta" }`
→ `404` when `id` is unknown

One revision's full record, including its `source` (the raw DSL text) —
everything the listing above omits, for e.g. rendering a diff against the
current document or another revision.

#### `POST /api/history/{id}/restore`
→ `200 { "ok": true, "tool", "current": id }`
→ `404` when `id` is unknown

Repoints `id`'s tool's `current` ref at `id` and **discards that tool's draft
working node** — it never creates a new revision (plans/config-store-and-format.md
§4.2). A subsequent edit therefore becomes a new child of `id`, an implicit
branch whenever `id` already had a different child (visible on the next
`GET /api/history` as two revisions sharing the same `parent`). Because
restoring discards unsaved work, a caller with a dirty draft should confirm
with the user before calling this.

### Exchange API (`*.sloom.md` Markdown)

The Markdown exchange format (plans/config-store-and-format.md §6: a
frontmatter block — `format`/`formatVersion`/optional `name`/`description`/
`author` — plus free-form Markdown prose (kept as `meta.notes`, not a
frontmatter key) and a fenced ` ```xml ` block holding the DSL source
verbatim).
Frontmatter/fence parsing is `internal/exchange`'s sole responsibility (a
self-contained package with no store/config/dsl dependency); these two
endpoints wire it to the store's validation boundary. Like every `/api/*`
route they require the same `Authorization: Bearer <token>` and Host/Origin
validation. Unlike every other endpoint above, their request/response bodies
are not both JSON — see each one below.

#### `POST /api/exchange/import`
Request body: **raw `*.sloom.md` text** (the file's exact bytes — `Content-Type`
is not inspected), not JSON.
→ `200 { "tool", "revision", "diagnostics": [...] }` on save
→ `400 { "error" }` when the body fails to decode as a Markdown exchange
  document (bad/missing frontmatter, no ` ```xml ` fence) or names an
  unrecognized tool
→ `409 { "diagnostics": [...] }` when the extracted DSL has error-severity
  diagnostics (store untouched)

Decodes the body via `exchange.Decode`, learns the target tool from the
extracted DSL's own `<statusloom tool="...">` attribute (the request carries
no separate `tool` field), and saves it through `store.Save` (origin
`"import"`) using the decoded `meta` (`name`/`description`/`author`/`notes`,
the last carrying whatever free-form prose sat between the frontmatter and
the fence).
Warning-only or dedup-no-op saves still return `200`; only error-severity
diagnostics (surfaced by either the Markdown decode or the DSL validation)
block the write.

#### `GET /api/exchange/export?tool=claude-code`
→ `200`, body: raw `*.sloom.md` text (`Content-Type: text/markdown`), not JSON
→ `404 { "error" }` when `tool` has no current committed revision
→ `400 { "error" }` for an unrecognized `tool`

Encodes `tool`'s current committed revision (`source` + `meta`) via
`exchange.Encode` and returns it verbatim. There is deliberately no
`DefaultDocument` fallback here — exporting the built-in default would
misrepresent it as a saved configuration.

## Diagnostics shape

Every diagnostics array element is:

```jsonc
{ "severity": "error" | "warning", "message": string, "range": { "start", "end" } }
```

`range` is a byte-offset span into the submitted source (`{0,0}` when a finding
has no specific location).
