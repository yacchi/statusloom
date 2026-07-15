# Statusloom

Statusloom is a fast, portable status-line toolkit for coding agents. Build, preview, install, and share status lines for Claude Code, Codex, GitHub Copilot, and other coding tools. Statusloom ships as a single Go binary, keeps the render path network-free, and includes a visual local configurator.

## Status

Statusloom is in early development. v0.1 targets Claude Code.

**Requirements**: Claude Code v2.1.132+ (for correct `context_window`
token semantics). Flex-separator width resolution and
`compactThreshold` additionally require v2.1.153+ — Claude Code only
started passing `COLUMNS`/`LINES` to the status line command in that
release. On v2.1.132–v2.1.152, flex-separators fall back to a single
space and compact mode never triggers.

## Installation

### Homebrew (macOS / Linux, recommended)

```sh
brew install yacchi/tap/statusloom
```

Windows users should use the GitHub Releases archives or `go install`
below.

### GitHub Releases (manual)

Prebuilt archives are published for macOS, Linux, and Windows (amd64 and
arm64). Download the archive for your platform from the
[Releases page](https://github.com/yacchi/statusloom/releases), extract
it, and place the `statusloom` binary somewhere on your `PATH`.

### go install

```sh
go install github.com/yacchi/statusloom/cmd/statusloom@latest
```

> **Note:** binaries built with `go install` do not embed the web
> configurator UI, so `statusloom config` will only serve a placeholder
> page. Status line rendering (`statusloom claude`) works fine. If you
> want to use the visual configurator, install via Homebrew or a
> GitHub Releases binary instead.

### After installing

Register statusloom as Claude Code's status line command:

```sh
statusloom setup claude-code
```

This writes `statusLine.command` into `~/.claude/settings.json` for
you (see [Usage](#usage) below for the equivalent manual JSON).

## Usage

### Claude Code status line

Add statusloom as Claude Code's status line command in `settings.json`:

```json
{
  "statusLine": {
    "type": "command",
    "command": "statusloom claude"
  }
}
```

Claude Code invokes this command on every prompt, piping session JSON to
its stdin; `statusloom claude` renders the configured status line to
stdout.

### Subagent status line

Claude Code's `subagentStatusLine` setting renders one line per running
subagent in the agent panel. Statusloom implements it with a separate
command:

```json
{
  "subagentStatusLine": {
    "type": "command",
    "command": "statusloom claude-subagent"
  }
}
```

Claude Code pipes a JSON array of subagent tasks to stdin; `statusloom
claude-subagent` writes one `{"id", "content"}` JSON line per task to
stdout. Subagent rows are configured as their own document,
`claude-code-subagent`, independent of the session document (see
[Configuration](#configuration)), with its own field catalog — `task-description`,
`task-model`, `task-model-id`, `task-tokens`, `task-context-size`,
`task-context-percent`, `task-status`, and `task-duration` (see
[Fields](#fields)). Pass `--draft` to render the shared draft document
instead of the saved one, mirroring `statusloom claude`'s `--draft`
handling; monitor workspaces use this to preview unsaved subagent-row
edits. Pass `--preview` to render a built-in representative payload
(a few sample tasks) instead of reading stdin, so you can see the row
without hand-crafting a `tasks[]` JSON; it composes with `--draft`.

**Protocol limitation:** the `subagentStatusLine` stdin payload carries
no per-subagent reasoning effort or agent type name (e.g.
`general-purpose`), so statusloom's rows can't reproduce the agent type
label Claude Code's own default row leads with — only model, tokens,
context, duration, and status are available. A `task-effort` field
exists in the catalog for forward compatibility but is currently always
unavailable.

### Configurator

```
statusloom config [--port N] [--no-browser]
```

Starts a local web UI (bound to `127.0.0.1` only) for viewing, editing,
and previewing your statusloom configuration. It offers two editing
modes over the same document: a **Visual Editor** (drag fields from a
palette, edit them directly on a live preview) and a **DSL Editor** (edit
the XML markup as text with live diagnostics and preview). By default it
picks a random free port and opens your browser to it; pass `--port` to
bind a specific port, and `--no-browser` to skip opening a browser
automatically. The server prints its URL (including a one-time auth
token) to stdout and shuts down on `Ctrl-C`, after an idle period, or
when you close it from the UI.

A tab switches the whole UI between the session status-line document
and the `subagentStatusLine` document, each with its own field
catalog; switching tabs preserves editing state (undo history, unsaved
edits, selection). The subagent tab's preview can toggle between a
running and a completed sample task.

### Setup

```
statusloom setup claude-code
statusloom setup claude-code --refresh-interval 60
```

Configures Claude Code to run `statusloom claude` (`statusLine`) and
`statusloom claude-subagent` (`subagentStatusLine`). Existing settings
are backed up, and replacing a different status-line command requires
confirmation.

`--refresh-interval <seconds>` sets Claude Code's own `refreshInterval`
setting (minimum 1 second), applied to both `statusLine` and
`subagentStatusLine`. Claude Code doesn't emit new statusline events
while idle, so countdown widgets like `five-hour-reset` and
`weekly-reset` otherwise freeze until the next event; use this flag if
you configure those widgets. `statusloom doctor` warns when
`refreshInterval` is unset while a countdown widget is configured.

### Format

```
statusloom fmt [file] [--check]
```

Rewrites a DSL document in canonical form — normalizing attribute order,
self-closing tags, indentation, and every `when` expression to the word
form (`and`, `or`, `lt`, `ge`, ...). With no argument it formats your
`claude-code.xml`; `-` reads stdin and writes stdout; `--check` reports
whether formatting would change the document (non-zero exit) without
writing. Normal saves through the configurator preserve your original
formatting for untouched nodes; `fmt` is the opt-in whole-document
canonicalizer.

### Diagnostics

Run `statusloom doctor` to check the binary, the status-line document,
the cache, Git, and Claude Code setup — including whether
`refreshInterval` is configured when countdown fields (`five-hour-reset`,
`weekly-reset`) are in use.

## Fields

Statusloom ships a catalog of built-in fields for Claude Code covering
model, context usage, cost, Git status, and rate limits. A `<field>`
hides itself automatically when its underlying data isn't available.

Recently added:

- `session-name` — session name set via `--name` or `/rename`
- `agent-name` — name of the running agent, when started with `--agent`
- `vim-mode` — current vim mode, when vim mode is enabled. If you use
  this widget, set `hideVimModeIndicator: true` in Claude Code's
  settings to avoid showing the mode twice
- `pr-number` — open PR for the current branch (e.g. `#1234`); hidden
  once the PR is merged or closed
- `pr-review-state` — `approved` / `pending` / `changes_requested` / `draft`
- `repo-name` — `owner/name` derived from the `origin` remote
- `worktree` — name of the current linked Git worktree
- `session-duration` / `api-duration` — elapsed wall-clock time / API
  time, formatted like `1h 15m`
- `lines-changed` — lines added/removed in the session, formatted like
  `(+156,-23)`
- `cache-hit-rate` — prompt cache hit rate over recent API calls
- session/model state: `session-id`, `model-id`, `output-style`,
  `thinking-enabled`
- context detail: `context-window-size`, `context-remaining`,
  `context-output-tokens`, `current-input-tokens`, `current-output-tokens`,
  `cache-creation-tokens`, `cache-read-tokens`, `exceeds-200k`
- repository detail: `project-directory`, `git-root`, `git-staged`,
  `git-unstaged`, `git-untracked`, `git-ahead`, `git-behind`, `git-clean`
- `lines-added` / `lines-removed` — separate session line counts

The `claude-code-subagent` document (see [Subagent status
line](#subagent-status-line)) has its own catalog, scoped to a single
subagent task: `task-description`, `task-model`, `task-model-id`,
`task-tokens`, `task-context-size`, `task-context-percent`,
`task-status`, and `task-duration`. `task-effort` is also registered
but currently always unavailable, since Claude Code's
`subagentStatusLine` protocol doesn't expose per-subagent reasoning
effort.

### Conditional display and color

Any node can be shown conditionally with a `when="..."` expression (and a
field can require its own data with `optional="<field>"`), and colored by
threshold with `<color-rule when="..." color="..."/>` children. Conditions
use word operators (`lt le gt ge eq ne`) over `self` (the field's own
metric) or a named metric (context usage, rate-limit percentages and reset
countdowns, session cost, durations, lines changed, cache hit rate,
`git-dirty`, token breakdowns, detailed Git counts, and terminal `width`).
Boolean metrics include `thinking-enabled`, `exceeds-200k`, and
`git-clean`. `width` is the terminal width in columns, usable in
breakpoints like `when="width ge 80"`; on a host that doesn't report a
width, `width` resolves as unbounded, so a width breakpoint never hides
content when the width is unknown. See `markup.md` for the full syntax.

### Layout

Claude Code overlays system notices (MCP errors, update prompts) and,
in verbose mode, a token counter on the right side of the status line.
Prefer `<flex size="full-minus-N"/>` over `full` so your content doesn't
collide with them.

### Hyperlinks

Set `hyperlink="true"` on `pr-number`, `pr-review-state`, or
`repo-name` fields to render them as OSC 8 hyperlinks (PR fields link to
the PR URL, repo links to `https://<host>/<owner>/<name>`). Hyperlinks are
skipped when `colorLevel` is `none`. Supported in terminals like
iTerm2, Kitty, and WezTerm; if your terminal doesn't advertise support,
try `FORCE_HYPERLINK=1`. Links may be stripped inside tmux or over SSH.

### Extra usage and per-model weekly limits

If your Claude subscription plan has pay-as-you-go overage ("extra
usage") enabled, statusloom can show that spend, plus per-model weekly
rate-limit usage, in the status line:

| field | shows | format |
|---|---|---|
| `extra-usage-cost` | metered pay-as-you-go cost for the current billing period (USD) | `currency` |
| `extra-usage-limit` | configured monthly extra-usage spending cap (USD) | `currency` |
| `extra-usage-percent` | % of the extra-usage monthly limit consumed | `percent` |
| `weekly-usage-opus` | % of the 7-day rate limit used by Opus models | `percent` |
| `weekly-usage-sonnet` | % of the 7-day rate limit used by Sonnet models | `percent` |
| `weekly-reset-opus` | countdown to the Opus 7-day window reset | `countdown` |
| `weekly-reset-sonnet` | countdown to the Sonnet 7-day window reset | `countdown` |

```xml
<span prefix="overage: " optional="extra-usage-cost">
    <field name="extra-usage-cost" format="currency"/>
</span>
```

**`extra-usage-cost` only shows a value once you've enabled usage
credits *and* exceeded your subscription limits** — for subscription
users within their limits, it stays empty. It's the metered
pay-as-you-go charge for the current billing period, not a session
estimate; `session-cost` remains the separate per-session cost
estimate.

These fields come from Claude Code's own OAuth usage endpoint — the
same (currently undocumented) API behind Claude Code's `/usage`
command — rather than from the status-line stdin payload. To keep the
render path network-free, that call happens only in a short-lived
background `statusloom refresh --once` subprocess that statusloom
spawns opportunistically (a one-shot process, not a daemon); it fetches
on its own schedule (every 5 minutes by default, with exponential
backoff up to 60 minutes on failure) and writes the result to the local
cache, which `statusloom claude` then reads. `statusloom claude` itself
never makes network calls.

The refresh subprocess reads your Claude Code OAuth token **read-only**
(never refreshed, never logged), checked in this order: the
`CLAUDE_CODE_OAUTH_TOKEN` environment variable, then
`~/.claude/.credentials.json`, then, on macOS, the login Keychain entry
named `Claude Code-credentials` (read via the Apple-signed
`/usr/bin/security` binary, so this doesn't trigger a Keychain access
prompt or require statusloom to be code-signed). Set
`STATUSLOOM_NO_USAGE_API=1` to disable usage-API fetching entirely —
these fields will simply stay empty, as if the underlying data weren't
available.

`statusloom config` probes the usage API on startup (`GET
/api/usage/probe`) and only offers these fields in the field palette
when the probe succeeds.

Like other countdown fields, `weekly-reset-opus` and
`weekly-reset-sonnet` only update when the status line re-renders; set
`--refresh-interval` (see [Setup](#setup)) so they keep counting down
during idle sessions.

## Configuration

Statusloom is configured with an XML markup document per tool. For Claude
Code it lives at `~/.config/statusloom/claude-code.xml` (on macOS too;
Statusloom uses XDG-style paths, and respects `XDG_CONFIG_HOME`). If the
file is absent, a built-in default document is used, so Statusloom works
out of the box. The subagent status line uses a second document in the
same directory, `claude-code-subagent.xml`, with the same built-in-default
fallback when it is absent.

A minimal document:

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

`<field>` shows a dynamic value (and hides its surrounding decoration when
it has no data), `<text role="separator">` is a collapsing separator,
`<span>` groups children to share styling / apply prefix-suffix / gate
visibility, and `<flex/>` is a flexible separator that fills the line.
Edit documents by hand or through `statusloom config`; the full markup
reference (elements, attributes, styling, formatters, conditions, colors)
is in [`markup.md`](markup.md).

An empty separator is a segment boundary. To render the whole line as
Powerline, set `output-style` on `<statusloom>`:

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

Powerline output filters manual separator nodes and generates transitions
between each visible top-level field/text/span. A span and all of its nested
children form one merged segment. Standard output continues to render manual
separator text and its collapsing behavior unchanged.
Powerline output assigns foreground/background colors from its built-in theme,
so uncolored content still forms complete segments. An explicitly configured
background takes precedence. A `<flex/>` closes the left run with ``, fills
the available default-background space, and opens the right-aligned run with
``.
The terminal font must include the Powerline glyphs.

**Migrating from another status-line tool.** Statusloom has no built-in
importer. To port a setup from another tool (for example ccstatusline),
open the configurator, launch a monitor workspace, and let the coding agent
do the translation: it reads the other tool's config, expresses the same
layout in Statusloom markup, shares it as an unsaved draft, and previews the
render so you can compare and iterate before saving. The workspace's
generated `CLAUDE.md` documents this playbook.

## Development

Requires [mise](https://mise.jdx.dev/) for toolchain management (Go,
Node, pnpm versions are pinned in `mise.toml`).

```
mise install                                    # install pinned Go/Node/pnpm
go test ./...                                   # run the Go test suite (or: mise run test)
scripts/build-web.sh                            # build the configurator frontend into internal/webconfig/dist (or: mise run build-web)
pnpm --filter @statusloom/configurator test      # run the configurator frontend's test suite (or: mise run test)
```

mise tasks bundle these steps; run `mise tasks ls` for the full list
(`mise run check` runs the pre-commit gate: lint + frontend build + tests).

`scripts/build-web.sh` produces built assets that must not be committed;
run `scripts/clean-web.sh` to restore the placeholder
`internal/webconfig/dist/index.html` before committing.

## License

[Apache-2.0](LICENSE)
