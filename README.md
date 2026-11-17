# Statusloom

Statusloom is a fast, portable status-line toolkit for coding agents. Build, preview, install, and share status lines for Claude Code, Codex, GitHub Copilot, and other coding tools. Statusloom ships as a single Go binary, keeps the render path network-free, and includes a visual local configurator.

*日本語版: [README.ja.md](README.ja.md)*

![The Statusloom configurator: a field palette, the live status-line preview being edited directly, and the properties of the selected field](docs/media/properties.png)

The preview *is* the editor: click a field in it to edit that field, drag
to rearrange, and watch the real rendered output change as you go.

![Adding a field, restyling it, and narrowing the terminal to watch the line adapt](docs/media/editor.gif)

## Status

Statusloom is in early development. v0.1 targets Claude Code.

**Requirements**: Claude Code v2.1.132+ (for correct `context_window`
token semantics). Flex-separator width resolution and
`compactThreshold` additionally require v2.1.153+ — Claude Code only
started passing `COLUMNS`/`LINES` to the status line command in that
release. On v2.1.132–v2.1.152, flex-separators fall back to a single
space and compact mode never triggers.

**Upgrading and downgrading**: from v0.0.8 on, a version that cannot read
the current saved layout shows the newest saved layout it can read. v0.0.7
and earlier show the built-in default layout instead. Do not run v0.0.7
side by side with v0.0.8 or later on the same config: downgrading from
v0.0.8 to v0.0.7 shows the default layout until you upgrade again.

## Installation

### Homebrew (macOS / Linux, recommended)

```sh
brew install yacchi/tap/statusloom
```

### Scoop (Windows, recommended)

```powershell
scoop bucket add yacchi https://github.com/yacchi/scoop-bucket
scoop install statusloom
```

On Windows, Claude Code runs the status line command through Git Bash, so
`statusloom` only needs to be on `PATH` (Scoop takes care of that). Two
features are not available on Windows: the terminal embedded in the
configurator, and the session state file (`sessions.json`).

### GitHub Releases (manual)

Prebuilt archives are published for macOS, Linux, and Windows (amd64 and
arm64). Download the archive for your platform from the
[Releases page](https://github.com/yacchi/statusloom/releases), extract
it, and place the `statusloom` binary somewhere on your `PATH`.

### From source (developers)

Building from a clone embeds the web configurator UI (unlike
`go install ...@latest`, which would only ship the placeholder page).
The toolchain is pinned in `mise.toml` (Go, Node, pnpm), so install
[mise](https://mise.jdx.dev/) first, then run a single command:

```sh
git clone https://github.com/yacchi/statusloom.git
cd statusloom
mise run install   # installs Go/Node/pnpm, builds the UI, and installs statusloom
```

`mise run install` auto-installs the pinned toolchain, builds and embeds
the configurator UI, then runs `go install ./cmd/statusloom`. Because it
builds from the local working tree, the freshly built UI in
`internal/webconfig/dist` is embedded into the binary. (This is why
cloning works but `go install ...@latest` does not: the embedded assets
are git-ignored and never committed — see `CLAUDE.md`.) This
works on Windows too.

To run directly from the checkout without installing, `mise run build`
produces `./statusloom` in the repo root, and `mise run config` launches
the configurator.

### After installing

Register statusloom as Claude Code's status line command:

```sh
statusloom setup claude-code
```

This writes `statusLine.command` into `~/.claude/settings.json` for
you (see [Usage](#usage) below for the equivalent manual JSON). It also
enables the statusloom Claude Code mod (marketplace and plugin, see
[Setup](#setup)). If you use profiles via `CLAUDE_CONFIG_DIR`, statusloom
follows it and writes
`$CLAUDE_CONFIG_DIR/settings.json` instead; `--settings <path>` always
overrides both.

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

Agent rows (one row per running subagent in the agent panel) are drawn by
the **statusloom mod**. Its AgentRow hook runs `statusloom
claude-agent-row`, which renders the `<subagent>` element of the SAME
`claude-code` document (not a separate document — see
[Configuration](#configuration)) and hands the text back. The mod runs
`statusloom` from your `PATH`, and it needs a Claude Code version with
mods (verified on 2.1.326).

`statusloom setup claude-code` enables and installs the mod (see
[Setup](#setup)). To install it by hand, run this in Claude Code:

```
/plugin install statusloom --marketplace yacchi/statusloom
```

or this in your shell:

```sh
claude plugin install statusloom --marketplace yacchi/statusloom
```

Subagent rows are configured as a `<subagent>` element, scoped to its own
field catalog: `task-description`, `task-model`, `task-model-id`,
`task-tokens`, `task-context-size`, `task-context-percent`, `task-status`,
and `task-duration`, plus the mod-only fields `task-effort`,
`task-agent-type`, `task-name`, `task-cost`, `task-depth`,
`task-input-tokens`, `task-output-tokens`, `task-cache-read-tokens`, and
`task-cache-creation-tokens` (see [Fields](#fields)). Row width is the
panel width minus two columns per nesting level, so a `<responsive>`
inside `<subagent>` picks its variant per row.

`statusloom claude-agent-row --preview` renders a built-in sample row
without stdin. Add `--draft` to render the shared draft instead of the
saved document. The monitor workspace sets `STATUSLOOM_AGENT_ROW_DRAFT=1`
in its `.claude/settings.local.json`, so its agent rows show the draft.

**A configured `subagentStatusLine` wins.** When one is set, Claude Code
passes its output to the row, and `claude-agent-row` leaves the row alone.

**Without mods:** `statusloom claude-subagent` stays as the renderer for a
`subagentStatusLine` you write by hand in Claude Code without mods:

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
stdout. It reads only the official `subagentStatusLine` stdin keys, so the
mod-only fields stay empty and the row omits them rather than showing a
fabricated value. `--draft` and `--preview` work as before.

`statusloom setup claude-code` removes such a line again, because it counts
as statusloom's own. Use it only where setup is not run, or re-add it after
running setup.

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

Subagent rows are edited inside the same document: each width-adaptive
container gets a Subagent region in the editor, filled from the task-scoped
field catalog. Its preview can toggle between a running and a completed
sample task.

Both editors work on one document, so you can build a layout visually and
read the markup it produced — or the other way round:

![The visual editor and the DSL editor side by side, showing the same document](docs/media/dsl-editor.png)

A `<responsive>` container holds several candidate layouts; the editor
marks the one the current width selects, and a variant can additionally
carry a condition (for example, only on a Team account):

![A responsive container with two variant cards, one gated on account-type](docs/media/responsive.png)

#### Version history

Every save is a revision. The History panel lists them newest first, shows
the diff of any revision against the current document, and restores it —
so trying a layout out costs nothing.

![The history panel listing revisions with the DSL diff of the selected one](docs/media/history.png)

#### Live preview against your own sessions

The samples above are synthetic. The live monitor renders your status line
from REAL session snapshots as they arrive, so you can see how a layout
behaves on your actual repositories, usage, and costs rather than on made-up
numbers. It prints a command to run in whichever terminal you want to
watch:

![The live monitor waiting for a session, with the command to run](docs/media/live-monitor.png)

#### Customizing with Claude Code itself

"Start embedded session" opens a terminal inside the configurator running
Claude Code in a workspace statusloom provisions for it: its own
`CLAUDE.md` explaining the DSL and the current document, a sample stdin
payload, and a status line wired to `--draft`. So you can ask the agent for
what you want in words ("put the git branch on the right, and drop the cost
when the terminal is narrow"), and its edits land in the shared draft that
the visual editor and the preview are already showing. Describing a status
line is often faster than assembling one, and you keep both routes over the
same document.

### Setup

```
statusloom setup claude-code
statusloom setup claude-code --refresh-interval 60
```

Setup points `statusLine` at `statusloom claude` and enables the statusloom
mod by merging into Claude Code's user settings. Other entries of both
objects are left untouched. The merged entries look like this:

```json
{
  "extraKnownMarketplaces": {
    "statusloom": {"source": {"source": "github", "repo": "yacchi/statusloom"}}
  },
  "enabledPlugins": {"statusloom@statusloom": true}
}
```

Setup never writes `subagentStatusLine`. Every run removes a
`subagentStatusLine` that runs `statusloom claude-subagent` (a line from
earlier versions) without asking, and leaves another tool's
`subagentStatusLine` untouched, and prints a note that this other line
keeps drawing the agent rows instead of the mod.

Existing settings are backed up, and the change is shown as a diff.
Confirmation is asked only when an existing value would be replaced: a
different status-line command, a different `statusloom` marketplace entry,
or `statusloom@statusloom` set to `false`.

After writing the settings (or finding them already configured), setup also runs
`claude plugin install statusloom@statusloom` itself (not with `--dry-run`),
which clones the marketplace over the network, and prints one summary line.
It installs into the profile whose `settings.json` setup just wrote: for the
default path the child inherits your environment unchanged, and for
`--settings PATH` elsewhere it runs with `CLAUDE_CONFIG_DIR` set to that
file's directory. A session that is already running loads the mod with
`/reload-plugins`. If `claude` is missing, the install times out (120 s) or
fails, setup still succeeds and prints the fallback: Claude Code installs the
mod in the background after the next session start, or you run
`claude plugin install statusloom@statusloom` yourself. The mod runs with your
user permissions.

Setup also writes Claude Code's `refreshInterval` to `statusLine`, so the
status line re-renders every 30 seconds while the session is idle. One render costs roughly 5–10 ms, so this is cheap.
Claude Code doesn't emit new statusline events while idle, so countdown
widgets like `five-hour-reset` and `weekly-reset` would otherwise freeze
until the next event.

- `--refresh-interval <seconds>` (minimum 1) sets a different interval.
- `--refresh-interval 0` leaves `refreshInterval` unset (opt out).
- Without the flag, an existing `refreshInterval` of 1 or more in your
  settings is kept, so your own choice is not overwritten.

`statusloom doctor` warns when `refreshInterval` is unset while a countdown
widget is configured.

The same value also sets how often statusloom refreshes what the status
line derives from the transcript. When `refreshInterval` is unset that
interval is 30 seconds, and transcript parsing runs only when the document
uses a transcript-derived value. Each external source's default ttl is
`max(30 s, refreshInterval)`, never below 5 seconds, unless you set
`--ttl` on that source.

Git status (branch, changes, counts) is collected by the same background
subprocess, never while the status line renders. The status line shows the
last collected value, so it can lag by one render (up to `refreshInterval`
while idle), and the first render in a new directory shows no Git info. The
`<git/>` element's `cache-ttl-ms` is the minimum interval between collections
for a directory and `timeout-ms` bounds each git command (default 5 s).

To show Git info, sources or usage from the very first render, add a
`<refresh>` element next to `<git/>`:

```xml
<refresh wait="git sources" timeout-ms="2000"/>
```

When a named item (`git`, `transcript`, `usage`, `source:<id>`, `sources`,
`all`) has never been fetched yet - a new directory, session or account - the
render starts the background refresh first and waits for it, up to
`timeout-ms` (default 2000, at most 3000). It costs at most that much once per
new item; later renders never wait. Previews never wait, and
`STATUSLOOM_NO_WAIT=1` turns waiting off. See `markup.md` ("refresh").

### Format

```
statusloom fmt [file] [--check]
```

Rewrites a DSL document in canonical form — normalizing attribute order,
self-closing tags, indentation, and every `when` expression to the word
form (`and`, `or`, `lt`, `ge`, ...). With no argument it formats the saved
`claude-code` document; `-` reads stdin and writes stdout; `--check` reports
whether formatting would change the document (non-zero exit) without
writing. Normal saves through the configurator preserve your original
formatting for untouched nodes; `fmt` is the opt-in whole-document
canonicalizer.

### Diagnostics

Run `statusloom doctor` to check the binary, the status-line document,
the cache, Git, and Claude Code setup — including whether
`refreshInterval` is configured when countdown fields (`five-hour-reset`,
`weekly-reset`) are in use. It also lists source references in the
document that point at undefined ids, and warns about any source defined
with `allowRemote`. It also checks the mod: it warns while statusloom's own
`subagentStatusLine` is still set (run `statusloom setup claude-code` to
migrate), and warns when `statusloom@statusloom` is not enabled, since
agent rows and mod fields stay empty until it is.

### Sources from the CLI

```
statusloom source list [--json]
statusloom source add <id> (--file PATH | --url URL | --command) [--allow-remote] [--ttl N] [--stale-after N] [--timeout N] [--force] [-- ARGV...]
statusloom source rm <id>
statusloom source test <id> [--select EXPR] [--payload FILE]
```

```sh
statusloom source add ccprofile --file ~/.local/state/ccprofile/sessions.json
# added source ccprofile (file)
statusloom source add keepalive --command --ttl 60 -- ccprofile sessions --json
statusloom source add x --url http://127.0.0.1:17921/sessions.json
statusloom source test ccprofile --payload sample.json
```

- `add` refuses to overwrite an existing id unless you pass `--force`.
  Exactly one of `--file`, `--url`, or `--command` is required, and the
  argv after `--` is required only with `--command`.
- `--ttl`, `--stale-after`, and `--timeout` are in seconds.
- `--allow-remote` lets an `http` source reach non-loopback hosts.
- `test` fetches the saved definition once and prints the value that
  `--select` selects, as JSON. With `--payload`,
  `${...}` placeholders are filled from that sample stdin file. `test`
  never reads or writes the cache, so it is safe to run while iterating.
  If nothing matches, it prints `statusloom: source test: no match for select`.
- Exit codes: `0` success, `1` runtime failure (unknown id, validation
  error, unreadable `sources.json`, fetch error, no match), `2` usage error.
  Errors go to stderr.
- `list` shows each source's kind, target, and ttl; `--json` prints the raw
  `sources.json` document.

### Editing the draft from your editor or an agent

```
statusloom draft pull [file]     # write the configurator's unsaved draft to a file
statusloom draft push [file]     # push an edited file back as the draft
```

The configurator's unsaved edits live in a shared draft, and these two
commands are the file-shaped end of it. Pull it, edit it in your own editor
(or hand it to a coding agent), push it back, and the open configurator
picks the change up — preview included — without anyone having to save
first. This is what the embedded terminal's workspace uses, and it needs no
token, since it goes through the local store rather than the HTTP API.

`statusloom claude --draft` renders the draft instead of the saved document,
so you can also point a real Claude Code session at work in progress.

### Version history from the CLI

```
statusloom history list
statusloom history show <id>
statusloom history diff <id> [<id2>]
statusloom history restore <id> [--force]
```

The same revisions the History panel shows. `restore` refuses to discard an
unsaved draft that differs from the current document unless you pass
`--force`.

### Sharing a document: the Markdown exchange format

```
statusloom export [-o file]      # a *.sloom.md document (stdout by default)
statusloom import <file>         # import one as a new revision ("-" reads stdin)
```

A `*.sloom.md` is frontmatter plus a fenced `xml` block — readable as a
document in its own right, and importable verbatim. It is how presets travel
between machines and people:

```sh
statusloom export -o my-layout.sloom.md
# … on another machine
statusloom import my-layout.sloom.md
```

Import goes through exactly the same Parse+Validate boundary as saving from
the configurator, so a document that imports is a document that renders.

### Live preview and rendering

```
statusloom monitor --emit-url URL --token TOK
statusloom render [--tool ID]
statusloom version
```

`monitor` renders like `statusloom claude` and additionally forwards the
payload to a running configurator, which is what drives the live monitor
described above — the configurator prints the exact command to paste.
`render` is the tool-agnostic entry point: it detects the tool from the
stdin payload when `--tool` is omitted.

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
- directory fields (`current-directory`, `project-directory`, `git-root`)
  take a `path` attribute: `base` (last element), `home` (`~/...` under your
  home directory), or `full` (absolute). For example,
  `<field name="current-directory" path="home"/>`; see markup.md `path`.
- `lines-added` / `lines-removed` — separate session line counts

The `<subagent>` region (see [Subagent status
line](#subagent-status-line)) only accepts fields scoped to a single
subagent task: `task-description`, `task-model`, `task-model-id`,
`task-tokens`, `task-context-size`, `task-context-percent`,
`task-status`, and `task-duration`, plus the mod-only fields `task-effort`,
`task-agent-type`, `task-name`, `task-cost`, `task-depth`,
`task-input-tokens`, `task-output-tokens`, `task-cache-read-tokens`, and
`task-cache-creation-tokens`. The mod-only fields are filled only by the
statusloom mod (`statusloom claude-agent-row`) and stay empty under a
hand-written `subagentStatusLine`.

### Session activity from the statusloom mod

The mod records session events: subagent starts and stops, model switches, turn ends, and background work. It
runs `statusloom mod-ingest`, which keeps a per-session record in the
cache. The status line only reads that record, so values appear after the
mod's next send (about a second later). Every field below is empty without
the mod. These are main-line fields, not allowed inside `<subagent>`.

Where the values come from matters when another plugin (for example an
organisation security plugin) is installed. Model switches
and fallbacks come from the session model the mod observes at each turn end, so
they work even then. `permission-mode`, `background-tasks`, and
`session-crons` rely on Claude Code's classic hook events, which another
plugin can intercept before the mod sees them; those fields stay empty
(not zero) until such an event arrives.

- `subagents-running` — running subagents (metric `subagents-running`)
- `subagents-by-type` — running subagents by type, like `Explore×2
  general-purpose`. A count appears only when it is above 1, and the
  types are joined by spaces.
- `subagents-cost` — subagents' cost in USD (metric `subagents-cost-usd`)
- `subagents-tokens` — subagents' tokens (metric `subagents-tokens`)
- `teammates-running` / `teammates-idle` — agent-team teammates (metrics
  of the same names)
- `last-turn-duration` — duration of the last main turn (metric
  `last-turn-seconds`)
- `last-turn-reason` — how it ended: `answer`, `aborted`, `refusal`, or
  `error` (metric `last-turn-reason`)
- `permission-mode` — `default`, `acceptEdits`, `plan`, `auto`, `dontAsk`,
  or `bypassPermissions` (metric `permission-mode`)
- `autocompact-remaining` — tokens left until auto-compact (metric
  `autocompact-remaining-tokens`)
- `context-breakdown` — what fills the context window, largest first, each
  as `<category> <tokens>` joined by ` · `
- `model-switches` — how many times the session model changed between
  turns (metric `model-switches`)
- `model-fallback` — `<session model>→<answering model>`, shown only
  while the model that answered the latest turn differs from the session
  model (bool metric `model-fallback`)
- `background-tasks` / `session-crons` — background tasks and session cron
  jobs (metrics of the same names)

Compaction counts stay the transcript-derived `compaction-count`,
`compaction-auto`, and `compaction-manual`. They are not mod fields. No
field reports a pending permission prompt, because Claude Code hides the
status line while one is shown.

### Grouping with `<span>`

A `<span>` wraps several children and treats them as one thing. That single
idea covers most of what a status line needs beyond "print this value":

```xml
<span prefix="5h: " suffix=" left" padding="1"
      color="cyan" optional="five-hour-usage">
    <field name="five-hour-usage" format="percent"/>
    <text> / </text>
    <field name="five-hour-reset" format="countdown"/>
</span>
```

- **One style for the group.** `color`, `background`, `bold`, `dim`,
  `italic`, `underline`, and `strikethrough` are inherited by everything
  inside (nearest wins), so you colour a group once instead of every field
  in it.
- **Labels that belong to the group.** `prefix`, `suffix`, and `padding`
  render in the group's own style and are not inherited by children.
- **One visibility decision for the group.** `optional="<field>"` and
  `when="..."` gate the whole span — the label disappears together with the
  data, which is the reason `5h: ` is written as a span's prefix rather than
  as a separate `<text>`. Without grouping you would have to repeat the same
  condition on the label and on every field.
- **Nesting.** Spans nest, so a group can carry a shared colour while an
  inner group flips one part of it.

The editor mirrors this: a span renders as a bordered chip group you can
drag chips into and out of, and selecting the group (its `⋮` grip) edits the
group's own style, condition, and label. Below: selecting the group, giving
the whole group a colour at once, editing the group's label, then overriding
one child — the group's colour stays on everything else.

![Selecting a span group, colouring the whole group, editing its label, then overriding one child's colour](docs/media/span-grouping.gif)

### Adaptive width: separators, flex, compact, and variants

Four mechanisms, from smallest to largest, keep a line readable as the
terminal narrows:

- **Collapsing separators.** `<text role="separator">` is dropped when it
  would end up leading, trailing, or next to another separator — so hiding a
  field never leaves a dangling ` | `. You write separators between every
  field and stop thinking about it.
- **Flex.** `<flex/>` expands to fill the line, which is how you
  right-align a run of content. `size="full-minus-N"` leaves N columns free
  for Claude Code's own overlays (see [Layout](#layout)).
- **Compact forms.** Below `compact-threshold` columns, fields switch to
  their compact rendering and separators drop their padding — one attribute,
  no second layout to maintain.
- **Variants.** When compaction is not enough, a `<responsive>` container
  holds several candidate layouts and the first one whose lines all fit is
  used (widest first, last one as the fallback). A `<variant>` can also
  carry a `when` condition, which is evaluated *before* the width fit — that
  is how one document serves both a Team seat and a personal subscription.

```xml
<responsive>
  <variant>                          <!-- wide: everything -->
    <line><field name="model"/><flex/><field name="session-cost"/></line>
  </variant>
  <variant>                          <!-- narrow: fallback -->
    <line><field name="model"/></line>
  </variant>
</responsive>
```

### Several layouts in one document

A document can hold more than one `<layout>`; the one with `active="true"`
is what the status line renders. The others stay in the document, so you can
keep a verbose layout and a minimal one side by side and switch between them
(the configurator's layout tabs do exactly that) instead of rewriting one
layout back and forth.

### Formatters

`format` decides how a value is written, and `precision` how precisely:
`percent`, `number`, `compact-number` (`28.5k`), `currency` (`$9.50`),
`duration` (`1h 15m`), `countdown` (time remaining), and `enum`. The same
field can therefore appear as `38.3%` in one layout and `38%` in a narrower
one without touching the data.

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
`~/.claude/.credentials.json` (or
`$CLAUDE_CONFIG_DIR/.credentials.json` when that variable is set), then,
on macOS, a login Keychain entry read via the Apple-signed
`/usr/bin/security` binary (so this doesn't trigger a Keychain access
prompt or require statusloom to be code-signed). The entry's service
name is `Claude Code-credentials` when `CLAUDE_CONFIG_DIR` is unset, or
that suffixed with `-` plus the first 8 hex digits of
`sha256(CLAUDE_CONFIG_DIR)` when it's set — this is Claude Code's own
naming rule (not something statusloom invented; verified by reading
Claude Code's own CLI bundle), used so that tools which run Claude Code
under multiple `CLAUDE_CONFIG_DIR` profiles (e.g. ccprofile) each get
their own account's credentials instead of statusloom always reading
whichever profile happens to own the unsuffixed entry. Unlike Claude
Code itself, statusloom does **not** apply Unicode NFC normalization to
`CLAUDE_CONFIG_DIR` before hashing it (it hashes the raw string as-is,
to avoid a new external dependency); this only matters if
`CLAUDE_CONFIG_DIR` contains non-precomposed Unicode, which is rare in
practice. Set
`STATUSLOOM_NO_USAGE_API=1` to disable usage-API fetching entirely —
these fields will simply stay empty, as if the underlying data weren't
available.

`statusloom config` probes the usage API on startup (`GET
/api/usage/probe`) and only offers these fields in the field palette
when the probe succeeds.

Like other countdown fields, `weekly-reset-opus` and
`weekly-reset-sonnet` only update when the status line re-renders; set
the default re-render interval from [Setup](#setup) (30 seconds unless
changed) so they keep counting down during idle sessions.

### Which account am I logged in as?

If you switch between logins with `CLAUDE_CONFIG_DIR`, these fields show
which account the current session is using:

| field | shows | example |
|---|---|---|
| `account-email` | email address of the logged-in account | `dev@example.com` |
| `account-name` | display name | `Dev User` |
| `account-org` | organization name | `Example Inc` |
| `account-role` | role within the organization | `primary_owner` |
| `account-type` | Team seat vs. individual subscription | `claude_team` / `claude_max` |
| `account-plan` | rate-limit tier | `default_claude_max_5x` |
| `account-seat` | seat tier (Team only) | `team_tier_1` |

```xml
<span prefix="as " optional="account-email">
    <field name="account-email" color="bright-black"/>
</span>
```

These read Claude Code's own local `.claude.json` (honoring
`CLAUDE_CONFIG_DIR`), so no network call is involved and the values track
whichever profile is active. Values are the raw strings Claude Code
stored, so `account-plan` reads `default_claude_max_5x` rather than
"Max 5x". Every field is empty when you're logged out or the file isn't
there, and the file is only read when a document actually uses one of
these fields.

**If you use both a Team seat and an individual subscription**,
`account-type` is the field that tells them apart — it's the only one
populated on both (`claude_team` vs. `claude_max`). Note that an
individual subscription has no seat, so `account-seat` is empty for it;
`account-plan` still resolves, because statusloom falls back to the
organization-scoped rate-limit tier that individual accounts use.

### Environment variables

`<field name="env" var="...">` shows an environment variable, which is
handy for things the status line can't otherwise know — which cloud
profile, cluster, or deployment target this shell is pointed at:

```xml
<span prefix="aws: " optional="env:AWS_PROFILE">
    <field name="env" var="AWS_PROFILE" color="yellow"/>
</span>
```

Use `optional="env:<NAME>"` (the variable name is part of the value) so
the label disappears along with the field when the variable isn't set. A
field with no `var` yet simply renders empty and reports nothing.

**Credential-looking variables are masked.** A status line ends up in
screenshots and screen shares, and shared presets are written by
strangers, so if the variable *name* contains `TOKEN`,
`SECRET`, `KEY`, `PASSWORD`, `PASSWD`, `CREDENTIAL`, `AUTH`, `SESSION`,
`COOKIE`, `PRIVATE`, or `SIGNATURE`, statusloom renders `***` instead of
the value. The check looks only at the name, never the value, and it
deliberately over-matches — `KEYBOARD_LAYOUT` and `SSH_AUTH_SOCK` are
masked too. Add `unmask="true"` when you actually want such a variable
shown:

```xml
<field name="env" var="KEYBOARD_LAYOUT" unmask="true"/>
```

### Prompt cache

Claude Code (v2.1.251+) passes the state of the prompt cache on stdin as
`prompt_cache`, and statusloom exposes it as the `cache-*` fields. Two
things to know before you use them:

- The data is absent until the main conversation's first API response, so
  these fields render empty until then.
- Claude Code re-renders the status line when the previous `expires_at` is
  reached, so a "went cold" state shows up on its own. A countdown that
  ticks down between events relies on the `refreshInterval` that
  [Setup](#setup) configures by default.

`expires_at` is epoch seconds; the fields take care of the conversion.

| Field | Shows |
|---|---|
| `cache-warm` | whether the cached prefix is still warm |
| `cache-ttl` | the cache lifetime in use, `5m` or `1h` |
| `cache-expires-at` | when the cached prefix goes cold (counts down with `format="countdown"`) |
| `cache-hit-ratio` | the session-wide cache hit ratio reported by Claude Code |
| `cache-requests` | the number of cache-eligible requests |
| `cache-misses` | the number of requests that missed the cache |
| `cache-recache-tokens` | the tokens that would be re-cached if the cache went cold |
| `cache-last-miss-cause` | why the most recent miss happened, as reported by Claude Code |

`cache-hit-ratio` is the ratio over the whole session as Claude Code
computes it. It is not the same as `cache-hit-rate`, which is the hit rate
over recent individual API calls.

The metrics `cache-warm`, `cache-ttl` (`5m` / `1h`, with `eq` / `ne`),
`cache-expires-minutes`, `cache-hit-ratio-percent`, `cache-requests`,
`cache-misses`, and `cache-recache-tokens` are available to `when` and
`<color-rule>`:

```xml
<span prefix="cache: " optional="cache-expires-at">
    <field name="cache-expires-at" format="countdown" color="green">
        <color-rule when="self lt 5" color="yellow"/>
    </field>
</span>
```

### External sources

Sometimes something other than Claude Code keeps a session's cache warm.
For example, a tool such as ccprofile can send its own keep-alive requests
from another process. Then the `expires_at` on stdin goes stale, and the
value you want to show lives somewhere else. A *source* is a JSON value
that statusloom reads from outside the status line:

```sh
statusloom source add ccprofile --file ~/.local/state/ccprofile/sessions.json
```

Then reference the source in the document. The value comes from the source
and falls back to the built-in `cache-expires-at` when the source has no
value:

```xml
<span prefix="cache: " optional="cache-expires-at">
    <field name="source" source="ccprofile"
           select="[id=${session_id},keepalive.enabled=true].keepalive.expiresAt"
           fallback="cache-expires-at" format="countdown"/>
</span>
```

The `keepalive.enabled=true` condition matters. ccprofile keeps the last
`keepalive.expiresAt` in the file after keepalive is turned off, and that
stale value would otherwise keep matching. With the extra condition, a
session whose keepalive is off has no match, so the fallback shows the
stdin `cache-expires-at` instead.

Gate this span on `cache-expires-at` (or no gate at all), not on
`source:ccprofile`. `optional="source:ccprofile"` would hide the span
whenever the source has no value, and that is exactly the case where the
fallback should show.

**Definitions live only in `<configDir>/sources.json`, never in the
document.** A source's path, command, or URL is never written in the DSL,
so importing a shared document can't run a command or read a file on your
machine. The document refers only to the source id. A reference to an
undefined id renders empty and reports nothing; `statusloom doctor` lists
such references, and it also warns about any source that sets
`allowRemote`.

`select` picks one value out of the JSON. It supports:

- `.key` for an object member and `[n]` for an array index
- `[key=value]` for the first array element whose `key` matches (scalars
  are compared by their canonical string, so types are not distinguished).
  Several conditions can be joined with commas (`[id=x,keepalive.enabled=true]`),
  and all must match. A condition key may be a dotted path such as
  `keepalive.enabled`. A value cannot contain `,` or `]`.
- placeholders that are filled in at render time: `${session_id}`,
  `${cwd}`, `${workspace.project_dir}`, and `${CLAUDE_CONFIG_DIR}`

It is not full JSONPath. `fallback` names one built-in field; a source
can't fall back to another source. A source has "no value" when its id is
undefined, its last fetch failed, its last value is stale beyond
`staleAfter` (default 3× its ttl), or the `select` matches nothing. The
metric `source-stale:<id>` tells you when the source's value is stale.

There are three kinds of source:

| Kind | Defined with | How it is read |
|---|---|---|
| `file` | `--file PATH` | Read at render time. Re-read only when the file's mtime or size changes; capped at 1 MB. |
| `command` | `--command -- ARGV...` | An argv array run without a shell. Timeout defaults to 2 s; stdout is capped at 1 MB. Fetched only by the refresh subprocess. |
| `http` | `--url URL` | Loopback addresses only, unless the source is added with `--allow-remote`. ETag aware. Fetched only by the refresh subprocess. |

Rendering never touches the network or runs a command. Command and HTTP
sources are fetched once per ttl for the whole machine, not once per
session, by the background `statusloom refresh` subprocess. There is no
resident process. A failed fetch backs off exponentially, up to 10 minutes.

### Session state export

statusloom publishes which Claude Code process shows which session. That
lets other tools, for example ccprofile, map a process to its session id,
transcript, and config dir without guessing.

#### When something is written

- Export happens only on live renders: `statusloom claude` and
  `statusloom render` without `--draft` or `--set`, and `statusloom monitor`
  (with or without `--draft`, because it is a live Claude Code session).
- Nothing is written for previews (`--draft` / `--set`), for
  `statusloom claude-subagent` or `statusloom claude-agent-row` (their stdin
  is a different payload), or when stdin is a regular file.
- The Claude Code process is identified from the `CLAUDE_PID` variable that
  Claude Code sets for the commands it runs. That pid is accepted only when
  it is the status line command's parent, or when it is reached from the
  parent through shells only (at most 4 hops). Otherwise nothing is written,
  because statusloom does not guess.
- macOS and Linux only. Process facts come from sysctl (macOS) or procfs
  (Linux), not from `ps`. On other platforms nothing is written.

#### Where it is written

`<base>` is `$XDG_STATE_HOME/statusloom` when `XDG_STATE_HOME` is set to an
absolute path, otherwise `~/.local/state/statusloom` (on macOS too). A
relative `XDG_STATE_HOME`, for example `relative/dir`, is ignored and the
default is used. There is no other override.

- Public file: `<base>/sessions.json`
- Detail files: `<base>/sessions/<pid>-<startedAt>.json`

#### Who writes what

The status line command writes only its own detail file. When a summary
field changed, it also leaves a marker. The existing background
`statusloom refresh --once` subprocess rebuilds `sessions.json` and deletes
the detail files of ended processes. It runs one at a time under the
existing refresh lease, and there is no resident process. So
`sessions.json` can lag the screen, and it can still list a process that
has ended until the next refresh run.

#### Schema (version 1)

```json
{
  "version": 1,
  "mergedAt": 1793673034,
  "sessions": [
    {
      "pid": 12345,
      "startedAt": 1793672985,
      "sessionId": "af886782-68bc-4068-9dbd-dde79c7823fb",
      "transcriptPath": "/Users/u/.config/ccprofile/profiles/max/projects/-x/af886782-....jsonl",
      "configDir": "/Users/u/.config/ccprofile/profiles/max",
      "cwd": "/Users/u/projects/x",
      "model": "claude-opus-5-5",
      "promptCache": { "ttl": "1h", "expiresAt": 1793676625 },
      "seenAt": 1793673030,
      "detailPath": "/Users/u/.local/state/statusloom/sessions/12345-1793672985.json"
    }
  ]
}
```

- All times are integer epoch seconds.
- `pid` is the pid of the Claude Code process itself, not the shell that
  runs the status line command and not statusloom. If a shell sits between
  them, the writer walks up the parent chain to the Claude Code process. If
  the Claude Code process cannot be identified, nothing is written.
- `startedAt` is that process's start time, truncated to whole seconds
  (the same resolution as `ps -o lstart`). Readers match it with a tolerance
  of ±1 s.
- `configDir` is the Claude Code process's `CLAUDE_CONFIG_DIR` as seen in the
  status line command's environment. It is an empty string when unset, which
  means `~/.claude`.
- `model` is stdin `model.id` (an empty string if absent).
- `promptCache` comes from stdin `prompt_cache` (`ttl`, `expires_at`). The
  key is omitted when stdin has no `prompt_cache`.
- `seenAt` is when the summary for this row last changed. It is not a
  heartbeat, so do not use it for liveness.
- `sessions` is sorted by `pid` ascending.
- Readers must ignore unknown keys. A reader that sees `version` other than
  1 ignores the file.
- `mergedAt` is when the file was last written (epoch seconds).
- `detailPath` points at the process's detail file.

#### Reading the file

This is the reader contract for any tool that reads `sessions.json`:

1. Read only `sessions.json`. Never read the detail files.
2. If the file is missing, unreadable, unparsable, or has `version` other
   than 1, behave exactly as you would without the file.
3. Accept a row only if `pid` is alive and the process's start time matches
   `startedAt` within ±1 s.
4. An accepted row is authoritative for that process's session id,
   transcript path, and config dir. Processes without an accepted row keep
   your own discovery.

`seenAt` is not a heartbeat. Never use it, or `mergedAt`, for liveness.

Detail files are internal. They hold the raw status line stdin payload plus
the row, their shape may change in any release, and other tools must not
read them.

This file is statusloom's own. It is unrelated to
`~/.local/state/ccprofile/sessions.json` from the External sources examples.

## Configuration

Statusloom keeps its configuration in a single internal store,
`~/.config/statusloom/statusloom.json` (on macOS too; Statusloom uses
XDG-style paths, and respects `XDG_CONFIG_HOME`). The store is git-shaped:
every save appends a revision, and each tool has a `current` and a `draft`
reference. A revision holds the XML markup document as its exchange format.
When the store is absent a built-in default document is used, so Statusloom
works out of the box.

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

`scripts/build-web.sh` writes into `internal/webconfig/dist`, which is
git-ignored in full (`internal/webconfig/dist/.gitignore` is its only
tracked entry), so there is no clean-up step before committing. A binary
built without running it serves a "run scripts/build-web.sh" page instead
of the UI.

## License

[Apache-2.0](LICENSE)
