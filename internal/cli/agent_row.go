package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/yacchi/statusloom/internal/adapters/claude"
	"github.com/yacchi/statusloom/internal/schema"
)

// agentRowResponse is the one JSON object `statusloom claude-agent-row` writes
// to stdout (agent-row-protocol). body is omitted when empty; v1 never emits
// isHidden.
type agentRowResponse struct {
	V    int    `json:"v"`
	Body string `json:"body,omitempty"`
}

// agentRowPreviewPayload is the built-in representative agent-row request
// `statusloom claude-agent-row --preview` renders instead of reading stdin. It
// is one running Opus task nested one level below a root agent, with columns
// set so the row renders at depth 1 and width 118 (120 − 2·1). Its row carries
// no body key, so the document's <subagent> region is what draws it.
const agentRowPreviewPayload = `{
  "v": 1,
  "sessionId": "preview-session",
  "row": {
    "id": "9c8b7a6f5e4d3c2b",
    "description": "Investigate flaky render test",
    "type": "Explore",
    "status": "running",
    "parentId": "0a1b2c3d4e5f6071",
    "name": "render-audit",
    "kind": "local_agent",
    "taskStatus": "running",
    "label": "Investigate flaky render test",
    "startTime": 1784130382000,
    "cwd": "/Users/dev/statusloom",
    "model": "claude-opus-4-8",
    "effort": "high",
    "contextWindowSize": 200000,
    "tokenCount": 42800,
    "usage": [
      {
        "input_tokens": 12000,
        "output_tokens": 3000,
        "cache_read_input_tokens": 26000,
        "cache_creation_input_tokens": 1800
      }
    ],
    "cost": {
      "usd": 0.42
    },
    "rowIds": [
      "0a1b2c3d4e5f6071",
      "9c8b7a6f5e4d3c2b"
    ],
    "columns": 120,
    "isHidden": false
  },
  "agents": [
    {
      "id": "0a1b2c3d4e5f6071"
    },
    {
      "id": "9c8b7a6f5e4d3c2b",
      "parentId": "0a1b2c3d4e5f6071"
    }
  ]
}
`

// runAgentRow implements `statusloom claude-agent-row`, the statusloom mod's
// AgentRow relay (agent-row-protocol). It reads one request object on stdin
// ({"v", "sessionId", "row", "agents"}) and writes at most one response object
// on stdout ({"v":1,"body"?}). Each step returns as follows:
//
//   - A stdin that is not a JSON object, or whose "row" is missing or not an
//     object, fails with exit 1, one stderr line and nothing on stdout.
//   - A "v" other than 1 exits 0 with empty stdout, so the mod falls back to
//     its default row.
//   - A row that already has a "body" key (the person's settings draw the row)
//     answers {"v":1} without rendering.
//   - Otherwise the claude-code document's <subagent> region is rendered for the
//     row's task exactly as claude-subagent renders it. The width is the row's
//     columns less two per nesting level (minimum 1), or 0 when columns is
//     absent. The draft working node is used with --draft or
//     STATUSLOOM_AGENT_ROW_DRAFT=1. A non-empty result answers {"v":1,"body"}
//     and an empty one answers {"v":1}.
//
// The command is network-free: it spawns nothing, writes no file, starts no
// refresh and exports no session. With --preview, stdin is not read at all;
// the built-in agentRowPreviewPayload is rendered instead.
func runAgentRow(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("claude-agent-row", flag.ContinueOnError)
	fs.SetOutput(stderr)
	draft := fs.Bool("draft", false, "render against the claude-code document's draft working node instead of the saved document")
	preview := fs.Bool("preview", false, "render a built-in representative request instead of reading stdin (no JSON needed)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var raw []byte
	if *preview {
		raw = []byte(agentRowPreviewPayload)
	} else {
		r, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes))
		if err != nil {
			return fail(stderr, fmt.Errorf("reading stdin: %w", err))
		}
		raw = r
	}

	req, err := claude.DecodeAgentRow(raw)
	if err != nil {
		return fail(stderr, err)
	}
	if req.V != 1 {
		return 0
	}
	if req.HasBody {
		writeAgentRowResponse(stdout, agentRowResponse{V: 1})
		return 0
	}

	useDraft := *draft || getenv("STATUSLOOM_AGENT_ROW_DRAFT") == "1"
	doc := resolveRenderDocument(string(schema.ToolClaudeCode), useDraft, stderr)

	width := 0
	if req.Columns > 0 {
		width = max(req.Columns-2*req.Depth, 1)
	}
	body := renderSubagentTask(req.Task, doc, subagentRenderOptions(width))
	writeAgentRowResponse(stdout, agentRowResponse{V: 1, Body: body})
	return 0
}

// writeAgentRowResponse writes resp as one JSON line. Marshalling a struct of
// an int and a string cannot fail, so an error here is dropped rather than
// reported on stdout.
func writeAgentRowResponse(stdout io.Writer, resp agentRowResponse) {
	encoded, err := json.Marshal(resp)
	if err != nil {
		return
	}
	fmt.Fprintln(stdout, string(encoded))
}
