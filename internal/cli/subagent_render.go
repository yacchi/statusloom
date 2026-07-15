package cli

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/adapters/claude"
	"github.com/yacchi/statusloom/internal/render"
	"github.com/yacchi/statusloom/internal/schema"
)

// subagentLine is one line of `statusloom claude-subagent`'s stdout: the
// JSON shape Claude Code's subagentStatusLine feature expects per task.
type subagentLine struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// subagentPreviewPayload is the built-in representative subagentStatusLine
// payload `statusloom claude-subagent --preview` renders, so the agent-panel
// row can be previewed without hand-crafting a tasks[] JSON payload (mirroring
// what a sample.json gives `statusloom claude`). It carries three tasks that
// differ in status, model, and token usage so each row of the default
// claude-code-subagent document renders visibly differently:
//
//   - a running Opus task partway through its context window (~21%)
//   - a running Sonnet task just getting started (~8%)
//   - a completed Haiku task with the lightest usage (~4%)
//
// columns is wide enough (120) to clear every width breakpoint the default
// document's task-duration/task-tokens/task-context-percent fields gate on
// (width ge 48/64/80), so every stat renders. startTime values are fixed
// past epoch milliseconds (not relative to time.Now()): task-duration is
// elapsed wall-clock and grows with real time, exactly like the captured
// fixtures/claude/subagent-*.json payloads.
const subagentPreviewPayload = `{
  "session_id": "preview-session",
  "transcript_path": "/Users/dev/.claude/projects/-Users-dev-statusloom/preview-session.jsonl",
  "cwd": "/Users/dev/statusloom",
  "prompt_id": "preview-prompt",
  "columns": 120,
  "tasks": [
    {
      "id": "9c8b7a6f5e4d3c2b",
      "type": "local_agent",
      "status": "running",
      "description": "Investigate flaky render test",
      "label": "Investigate flaky render test",
      "startTime": 1784130382000,
      "model": "claude-opus-4-8",
      "contextWindowSize": 200000,
      "tokenCount": 42800,
      "cwd": "/Users/dev/statusloom"
    },
    {
      "id": "3f2e1d0c9b8a7968",
      "type": "local_agent",
      "status": "running",
      "description": "Draft DSL validation fix",
      "label": "Draft DSL validation fix",
      "startTime": 1784130622000,
      "model": "claude-sonnet-5",
      "contextWindowSize": 200000,
      "tokenCount": 15400,
      "cwd": "/Users/dev/statusloom"
    },
    {
      "id": "7788990011223344",
      "type": "local_agent",
      "status": "completed",
      "description": "Summarize test coverage gaps",
      "label": "Summarize test coverage gaps",
      "startTime": 1784129992000,
      "model": "claude-haiku-4-5-20251001",
      "contextWindowSize": 200000,
      "tokenCount": 8200,
      "cwd": "/Users/dev/statusloom"
    }
  ]
}
`

// runSubagentRender implements `statusloom claude-subagent`: Claude Code's
// subagentStatusLine feature. Its stdin shape is distinct from the regular
// statusLine payload (a slim top-level object plus a tasks[] array, no
// session-level fields), and its stdout shape is one {"id","content"} JSON
// line per task rather than plain text.
//
// This render path is deliberately network- and cache-free: unlike
// runRenderPipeline it applies no account/git/session cache enrichment,
// since a subagent task snapshot carries no rate-limit/account/git identity
// of its own. A task whose rendered document produces no visible content
// emits no line at all (the caller's default DSL rendering never falls
// back to the session fallback line for a subagent render).
//
// With --draft (used by the monitor workspace's subagentStatusLine, mirroring
// `statusloom monitor --draft`) it renders the shared draft
// claude-code-subagent.draft.xml instead, falling back to the saved document
// when the draft is absent or invalid. Without --draft, output is
// byte-identical to before this flag existed.
//
// With --preview, stdin is not read at all (even if piped) - the built-in
// subagentPreviewPayload is decoded instead, so the agent-panel row can be
// previewed without hand-crafting a tasks[] JSON payload. --preview composes
// with --draft exactly like the stdin path: --preview alone renders the
// saved (or built-in default) claude-code-subagent document, and
// --preview --draft renders the shared draft against the same built-in
// payload.
func runSubagentRender(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("claude-subagent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	draft := fs.Bool("draft", false, "render against the shared draft document (claude-code-subagent.draft.xml) instead of the saved document")
	preview := fs.Bool("preview", false, "render a built-in representative payload instead of reading stdin (no JSON needed)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var raw []byte
	if *preview {
		raw = []byte(subagentPreviewPayload)
	} else {
		r, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes))
		if err != nil {
			return fail(stderr, fmt.Errorf("reading stdin: %w", err))
		}
		raw = r
	}

	tasks, err := claude.DecodeSubagent(raw)
	if err != nil {
		return fail(stderr, err)
	}
	columns := claude.DecodeSubagentColumns(raw)

	tool := string(schema.ToolClaudeCodeSubagent)
	doc := resolveRenderDocument(tool, *draft, stderr)
	opts := render.Options{Width: columns, Now: time.Now()}

	w := bufio.NewWriter(stdout)
	defer w.Flush()

	for _, task := range tasks {
		snap := schema.StatusSnapshot{
			Tool:     schema.ToolSnapshot{ID: schema.ToolClaudeCodeSubagent},
			System:   schema.SystemSnapshot{Cwd: task.Cwd},
			Subagent: &task,
		}
		content := joinVisibleLines(render.RenderDocument(snap, doc, opts))
		if content == "" {
			continue
		}
		encoded, err := json.Marshal(subagentLine{ID: task.ID, Content: content})
		if err != nil {
			// Unreachable in practice (content is plain/ANSI text, ID is a
			// plain string), but never let a stray task wedge the loop.
			continue
		}
		fmt.Fprintln(w, string(encoded))
	}
	return 0
}

// joinVisibleLines concatenates a rendered document's non-omitted lines'
// styled text, one per output line, mirroring
// render.RenderDocumentString's non-fallback branch exactly. Unlike
// RenderDocumentString it never substitutes RenderFallback: an all-omitted
// result yields "", which the caller treats as "no line for this task"
// (markup.md's subagent rendering never emits the session fallback).
func joinVisibleLines(lines []render.DocLine) string {
	var out []string
	for _, dl := range lines {
		if dl.Omitted {
			continue
		}
		var b strings.Builder
		for _, s := range dl.Segments {
			b.WriteString(s.ANSI)
		}
		out = append(out, b.String())
	}
	return strings.Join(out, "\n")
}
