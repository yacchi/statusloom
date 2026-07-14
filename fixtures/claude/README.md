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
