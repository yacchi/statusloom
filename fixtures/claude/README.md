# Claude Code Statusline Fixtures

These are Claude Code statusLine stdin fixtures based on https://code.claude.com/docs/en/statusline.md.

Notes:
- `resets_at` is Unix epoch seconds
- `rate_limits` and `effort` fields may be absent depending on the session state
- These fixtures represent various states a Claude Code session can report during execution
- `session_name` is only present when the session was named via `--name` or `/rename`
- `agent` is only present when running with `--agent`; `agent.name` identifies the subagent
- `vim` is only present when vim mode is enabled; `vim.mode` is one of `NORMAL`/`INSERT`/`VISUAL`/`VISUAL LINE`
- `pr` is only present when the current branch has an open pull request; `pr.review_state` may independently be absent (treated as `""`) even when `pr` itself is present
- `workspace.git_worktree` names any linked worktree the session is running from; `worktree.name` is only present for sessions started with `--worktree`. When both could apply, `workspace.git_worktree` takes priority
- `workspace.repo` (`host`/`owner`/`name`) is the repository identity parsed from the `origin` remote; absent outside a git repo or without an `origin` remote
- `cost.total_duration_ms`/`total_api_duration_ms` are milliseconds since session start / spent waiting on API responses; `cost.total_lines_added`/`total_lines_removed` are cumulative line counts
- `context_window.current_usage` is the token breakdown for the last API call (`input_tokens`/`output_tokens`/`cache_creation_input_tokens`/`cache_read_input_tokens`); it is `null` before the first API call and right after `/compact`

## subagentStatusLine Fixtures

`subagent-running.json` / `subagent-completed.json` are `subagentStatusLine` stdin fixtures, captured live from Claude Code v2.1.210 (see `subagentStatusLine` in settings.json) and scrubbed of PII. This is a **separate protocol** from `statusLine`.

Schema notes (verified against real capture, not just the public docs):

- The top-level object is **slimmer than `statusLine`**: only `session_id`, `transcript_path`, `cwd`, `prompt_id`, `columns`, and `tasks`. It does **not** carry `model`, `version`, `output_style`, `workspace`, `cost`, `context_window`, `rate_limits`, etc. Per-agent identity lives entirely inside each `tasks[]` entry.
- `columns` is the available row width in characters (the analog of the `COLUMNS` env var for the main statusline).
- `prompt_id` is undocumented; it is the id of the prompt turn that owns these subagents.
- Each `tasks[]` entry: `id`, `type` (observed `"local_agent"`), `status` (observed `"running"`/`"completed"`), `description`, `label`, `startTime`, `model`, `contextWindowSize`, `tokenCount`, `tokenSamples`, `cwd`.
  - `startTime` is **epoch milliseconds** (13 digits), not seconds.
  - `model` is the resolved full model id (e.g. `claude-opus-4-8`); each task carries its own, so different subagents can report different models. Per docs, `model`/`contextWindowSize` require v2.1.205+ and are **omitted** (key absent) for a task whose model isn't resolved yet — this transient state was not captured (models resolved immediately) so it is not represented in these fixtures.
  - `tokenSamples` is the history of `tokenCount` values across refreshes; it grows by one per refresh and repeats the last value while the task is idle/completed.
  - The docs list a `name` field, but it did not appear for `local_agent` tasks (only `description`/`label`).
- Output protocol (not exercised by these input fixtures): one JSON line per row, `{"id":"<task-id>","content":"<row-body>"}`. Tasks not returned keep default rendering; an empty `content` hides the row. `content` supports ANSI colors and OSC 8 hyperlinks.
- Update triggers match `statusLine` (post-assistant-message, `/compact`, permission/vim mode change, `refreshInterval`); the command is invoked once per refresh with **all** visible subagent rows in `tasks`, not once per row.
