package webconfig

import (
	"os"
	"os/exec"
)

// gitInitWorkspace initializes a git repo in dir and creates one commit
// containing whatever files were already written. It is strictly
// best-effort: git may be absent or sandboxed, in which case the workspace
// still functions and only the statusline's git widgets stay empty. All
// errors are intentionally ignored.
//
// user.name/user.email are set per-command (-c) so the commit succeeds even
// when the machine has no global git identity, without mutating any global
// git config.
func gitInitWorkspace(dir string) {
	run := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		// Keep git from prompting or reading the user's environment config.
		cmd.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_CONFIG_NOSYSTEM=1",
		)
		return cmd.Run()
	}

	if err := run("init"); err != nil {
		return
	}
	if err := run("add", "-A"); err != nil {
		return
	}
	_ = run(
		"-c", "user.email=statusloom@localhost",
		"-c", "user.name=statusloom",
		"commit", "-m", "statusloom monitor workspace",
	)
}

// workspaceClaudeMD is the CLAUDE.md written into a monitor workspace. It
// teaches the draft edit loop, summarizes the status-line markup DSL, and
// shows how to preview a render.
const workspaceClaudeMD = "# statusloom statusline workspace\n" + `
This is a throwaway workspace for customizing your statusloom **status line**.
Editing here does not touch any project of yours; it only changes the shared
**draft** the web configurator is showing. Saving to your real config
(` + "`<tool>.xml`" + `) is done from the web UI (the Save action), never
automatically.

## Editing the draft

1. Pull the current draft to a local file:
   ` + "`statusloom draft pull`" + `  (writes ` + "`./statusloom-draft.xml`" + `)
2. Edit that XML markup (see the DSL summary below).
3. Push it back:
   ` + "`statusloom draft push`" + `  (writes the shared draft; diagnostics are
   printed but do not block the push, so in-progress edits still share)

Your changes appear in the web configurator as **unsaved edits**, and this
session's status line re-renders against them (it runs
` + "`statusloom monitor --draft`" + `). If the draft is invalid, the session
falls back to the saved document. The user saves them from the web UI.

## Preview a render

Render the status line for a representative payload without any live session:

    statusloom claude < sample.json

## Markup DSL (summary)

The status line is described in an XML/JSX-style markup, one file per tool
(` + "`claude-code.xml`" + `). See the project ` + "`markup.md`" + ` for the full spec.

    <statusloom version="1" tool="claude-code" color-level="ansi16" output-style="standard"
                compact-threshold="60" context-percentage-mode="usable">
      <!-- optional shared git settings -->
      <git cache-ttl-ms="3000" timeout-ms="200"
           include-untracked="true" collect-numstat="true"/>
      <layout name="Default" active="true">
        <line>
          <text>Model: </text>
          <field name="model" color="cyan" bold="true"/>
          <span optional="thinking-effort" prefix=" (" suffix=")">
            <field name="thinking-effort" color="yellow"/>
          </span>
          <text role="separator" padding="1">|</text>
          <field name="context-percentage-usable" format="percent" precision="0"/>
          <flex/>
          <field name="git-branch" color="magenta"/>
        </line>
      </layout>
    </statusloom>

### Nodes

- ` + "`<field name=\"...\"/>`" + `  a dynamic value (field catalog below).
      Attributes: ` + "`format`" + ` / ` + "`precision`" + ` / ` + "`currency`" + `,
      ` + "`raw=\"true\"`" + ` (unlabeled value), ` + "`hyperlink=\"true\"`" + `
      (OSC 8; linkable fields only), ` + "`prefix`" + ` / ` + "`suffix`" + `.
- ` + "`<text>...</text>`" + `      literal text (whitespace preserved).
      ` + "`role=\"separator\"`" + ` opts into auto-collapsing separators.
- ` + "`<span>...</span>`" + `      groups children; inherits decoration, applies
      ` + "`prefix`" + ` / ` + "`suffix`" + ` / ` + "`padding`" + ` and conditions.
- ` + "`<flex/>`" + `             fills remaining terminal width
      (` + "`size=\"full\"`" + ` or ` + "`\"full-minus-N\"`" + `).
      In Powerline output it closes the left run and starts a right-aligned run.

Set ` + "`output-style=\"powerline\"`" + ` on the root to render empty separators
as themed segment transitions. Standard is the default.

### Common attributes

- Decoration (inherited): ` + "`color`" + `, ` + "`background`" + `, ` + "`bold`" + `,
      ` + "`dim`" + `, ` + "`italic`" + `, ` + "`underline`" + `, ` + "`strikethrough`" + `
- Layout: ` + "`padding`" + ` / ` + "`padding-left`" + ` / ` + "`padding-right`" + `,
      ` + "`prefix`" + `, ` + "`suffix`" + `
- Visibility: ` + "`optional=\"<field>\"`" + ` (hide when the field is empty),
      ` + "`when=\"<metric> ge 80\"`" + ` (word ops: lt le gt ge eq ne / and or not)
- ` + "`<color-rule when=\"self ge 90\" color=\"red\"/>`" + ` child elements switch
      color by condition (first match wins).

### Content fields

    model  git-branch  git-changes  tool-version  current-directory
    thinking-effort  context-length  context-percentage
    context-percentage-usable  session-cost  five-hour-usage  five-hour-reset
    weekly-usage  weekly-reset  session-name  agent-name  vim-mode
    pr-number  pr-review-state  repo-name  worktree  session-duration
    api-duration  lines-changed  cache-hit-rate

Labels like "5h:" / "7d:" are NOT baked into fields; add them with a
` + "`prefix`" + ` (often on an ` + "`optional`" + ` span so the label hides with the value).
`

// workspaceSampleJSON is a representative Claude Code statusLine stdin
// payload, written into the workspace so `statusloom claude < sample.json`
// works out of the box. It mirrors the shape statusloom parses (model,
// context_window, rate_limits, cost, version). Reset timestamps are set far
// in the future so the rate-limit widgets render a live countdown.
const workspaceSampleJSON = `{
  "session_id": "3f9a2b1c-4d5e-6f70-8a9b-0c1d2e3f4a5b",
  "session_name": "customize-statusline",
  "cwd": "/tmp/statusloom-monitor-workspace",
  "version": "2.1.200",
  "output_style": { "name": "default" },
  "model": { "id": "claude-opus-4-8", "display_name": "Opus 4.8" },
  "workspace": {
    "current_dir": "/tmp/statusloom-monitor-workspace",
    "project_dir": "/tmp/statusloom-monitor-workspace",
    "repo": { "host": "github.com", "owner": "yacchi", "name": "statusloom" }
  },
  "effort": { "level": "high" },
  "thinking": { "enabled": true },
  "cost": {
    "total_cost_usd": 1.2345,
    "total_duration_ms": 4523000,
    "total_api_duration_ms": 123000,
    "total_lines_added": 156,
    "total_lines_removed": 23
  },
  "exceeds_200k_tokens": false,
  "context_window": {
    "total_input_tokens": 64000,
    "total_output_tokens": 8000,
    "context_window_size": 200000,
    "used_percentage": 32,
    "remaining_percentage": 68,
    "current_usage": {
      "input_tokens": 4000,
      "output_tokens": 1200,
      "cache_creation_input_tokens": 12000,
      "cache_read_input_tokens": 48000
    }
  },
  "rate_limits": {
    "five_hour": { "used_percentage": 27, "resets_at": 2000000000 },
    "seven_day": { "used_percentage": 79, "resets_at": 2000500000 }
  }
}
`
