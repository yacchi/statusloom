# DSL API (`/api/dsl/*`)

The DSL-native configurator API — the sole configuration API (the legacy
widget-index endpoints have been removed). Alongside these `/api/dsl/*` paths
the server also serves `GET /api/tools`, `GET /api/sessions`,
`GET /api/usage/probe`, the live-preview and embedded-terminal channels, and
`POST /api/shutdown`. Every path is under `/api/`, so the same security applies:
127.0.0.1-only bind, `Authorization: Bearer <token>`, Host + Origin validation,
no cookies.

Two tools are supported: `claude-code` (the regular session `statusLine`)
and `claude-code-subagent` (Claude Code's `subagentStatusLine`, one row per
agent-panel task). `GET /api/tools` lists both, `claude-code` first. Every
endpoint below that takes a `tool` parameter accepts either value; an
unrecognized tool is rejected with `400`. The two tools have disjoint field
catalogs (`claude-code`'s session/account/git fields vs.
`claude-code-subagent`'s `task-*` fields, see `GET /api/dsl/fields`) and
separate documents (`<configDir>/claude-code.xml` vs.
`<configDir>/claude-code-subagent.xml`, and their own drafts).

## AST JSON

A parsed document is represented as a tree of node objects. Every node has:

```jsonc
{
  "id": "L0.0.1",          // deterministic, position-derived (see Node IDs)
  "kind": "field",         // node kind (see below)
  "range": { "start": 42, "end": 78 },  // byte offsets into the source
  // ...attributes, keyed by their DSL attribute name (kebab-case)...
  "children": [ /* nodes */ ],   // line/span only
  "colorRules": [ /* color-rule nodes */ ]  // field/span/text only
}
```

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
| `layout` | `name`, `active` | `lines[]`, `comments[]?` |
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
L{i}.{j}             j-th <line> of layout i (index into layout.lines)
{parent}.{k}         k-th mixed-content child of a line/span (index into children)
{owner}.cr{c}        c-th <color-rule> of a field/span/text owner
```

`children` nest, so a field inside a span inside a line reads e.g.
`L0.2.1.0` (layout 0, line 2, child 1 = span, child 0 = field). Comments that
appear inside a line/span are ordinary `children` entries (kind `comment`) and
take a numeric child index; only root-level and layout-level comments use the
`.c{k}` form.

## Endpoints

### `GET /api/dsl/document?tool=claude-code`
→ `200 { "source": string, "version": string, "exists": bool }`

`source` is the saved `<tool>.xml` (or the built-in default when `exists` is
`false`). `version` is the sha256 hex of `source`. The raw source is returned
even if it is invalid, so the editor can display and repair it.

### `PUT /api/dsl/document` `{ "tool", "source" }`
→ `200 { "version", "diagnostics": [] }` on save
→ `409 { "version", "diagnostics": [...] }` when `source` has error diagnostics

Parses and validates `source`. **Error-severity diagnostics block the save**
(409, nothing written). Warning-only source is saved and the warnings are
returned.

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

The shared draft source (`<tool>.draft.xml`). `exists` reflects the draft
file's presence; when absent, `source` falls back to the saved document, then
the built-in default.

### `PUT /api/dsl/draft` `{ "tool", "source" }`
→ `200 { "version", "diagnostics": [...] }`

Saves `source` to the shared draft **unconditionally** (last-writer-wins). The
draft is a text-sharing channel that tolerates in-progress, invalid input;
diagnostics are returned for the editor but never block the write.

### `POST /api/dsl/preview` `{ "tool", "source", "width", "sample", "sessionId"?, "layoutIndex"? }`
→ `200 { "lines": [...], "diagnostics": [...], "fallback": { "ansi", "active" } }`

```jsonc
"lines": [
  {
    "omitted": false,
    "ansi": "…",                       // concatenated visible-segment ANSI
    "segments": [
      { "nodeId": "L0.0.1", "text": "Opus 4.8", "ansi": "[36m…", "visible": true }
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
- `sessionId` renders against a real cached session (see `GET /api/sessions`);
  otherwise `sample` selects a synthetic snapshot. Sample names are
  independent of `tool` — the same synthetic snapshot names are accepted no
  matter which tool's source is being previewed — but when `sample` is
  omitted the default depends on `tool`: `"full"` for `tool=claude-code`
  (`"early-session"` is the other `claude-code` sample), `"subagent-running"`
  for `tool=claude-code-subagent` (`"subagent-completed"` is the other
  subagent sample, same task further along: same id/model, `status` flipped
  to `"completed"`, higher `tokenCount`). `width` is clamped to [20, 400]
  (default 120).
- `fallback.active` is `true` when every line is omitted, in which case
  `fallback.ansi` is the fallback line (model + tool-version).

### `GET /api/dsl/fields?tool=claude-code`
→ `200 { "fields": [ { "name", "displayName", "descriptions": {"en","ja"}, "category", "linkable"?, "selfMetric"?, "formats"?, "capability"?, "preview": {"text","ansi"} } ] }`

The field catalog for the palette, sourced entirely from the Go DSL registry
(single source of truth) — `tool=claude-code` returns the session/account/git
field set, `tool=claude-code-subagent` returns the disjoint `task-*` set (one
entry per subagentStatusLine task attribute: `task-description`,
`task-model`, `task-model-id`, `task-status`, `task-tokens`,
`task-context-size`, `task-context-percent`, `task-duration`, `task-effort`;
all `category: "subagent"`). `preview` is a single-field rendering against
that tool's default sample snapshot (the `full` session sample for
`claude-code`, the `subagent-running` task sample for
`claude-code-subagent` — see `POST /api/dsl/preview`'s default-sample rule
above). `capability` (optional; absent = always available) names a runtime
capability the field depends on: `"oauth-usage"` (the authenticated OAuth
usage API) for the `claude-code` extra-usage / weekly-usage / weekly-reset
fields, or `"subagent-effort"` for `claude-code-subagent`'s `task-effort`
(Claude Code does not yet report per-task reasoning effort, so this
capability is unavailable in every environment today). Capability gating is
entirely client-side: the server only tags the field, it never filters the
response by availability. The configurator probes `"oauth-usage"` via
`GET /api/usage/probe` and hides those fields from the palette when
unreachable; there is no analogous probe for `"subagent-effort"` yet, so a
`capability`-tagged field with no matching probe should be hidden
unconditionally until one exists.

### `GET /api/dsl/metrics?tool=claude-code`
→ `200 { "metrics": [ { "name", "displayName", "descriptions": {"en","ja"} } ] }`

The named-metric catalog for `when` / `color-rule` editing, from the DSL
registry (`tool=claude-code-subagent` returns the `task-*` self-metrics
backing `task-tokens`/`task-context-size`/`task-context-percent`/
`task-duration`). Both tools additionally return the tool-agnostic `width`
metric (terminal width in columns), which backs width breakpoints such as
`when="width ge 80"`; an unknown width counts as unbounded so a width
condition never hides content.

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

## Diagnostics shape

Every diagnostics array element is:

```jsonc
{ "severity": "error" | "warning", "message": string, "range": { "start", "end" } }
```

`range` is a byte-offset span into the submitted source (`{0,0}` when a finding
has no specific location).
