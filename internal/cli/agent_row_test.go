package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// errorOnReadReader fails the test if anything reads from it. The --preview
// path must never touch stdin.
type errorOnReadReader struct {
	t *testing.T
}

func (r errorOnReadReader) Read([]byte) (int, error) {
	r.t.Error("stdin was read, but --preview must ignore stdin")
	return 0, io.EOF
}

// runAgentRowCLI runs runAgentRow with stdin as its input and env as its
// environment, returning what it wrote to stdout and stderr and its exit code.
func runAgentRowCLI(t *testing.T, args []string, stdin []byte, env map[string]string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = runAgentRow(args, bytes.NewReader(stdin), &out, &errBuf, envMap(env))
	return out.String(), errBuf.String(), code
}

// agentRowStdin builds an agent-row request: {"v": v, "sessionId", "row", and
// "agents" when any are given}.
func agentRowStdin(t *testing.T, v any, row map[string]any, agents ...map[string]any) []byte {
	t.Helper()
	req := map[string]any{
		"v":         v,
		"sessionId": "test-session",
		"row":       row,
	}
	if len(agents) > 0 {
		req["agents"] = agents
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal agent-row stdin: %v", err)
	}
	return data
}

// agentRowBody decodes stdout as exactly one {"v":1,"body"?} JSON line and
// returns its body, failing the test on any other shape.
func agentRowBody(t *testing.T, stdout string) string {
	t.Helper()
	if strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("stdout must be exactly one line, got %q", stdout)
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("stdout is not a JSON object %q: %v", stdout, err)
	}
	if _, ok := resp["v"]; !ok {
		t.Fatalf("response lacks v: %q", stdout)
	}
	var body string
	if raw, ok := resp["body"]; ok {
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("body is not a string: %q", stdout)
		}
	}
	return body
}

func TestAgentRow_RendersBody(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocBasic)

	stdin := agentRowStdin(t, 1, map[string]any{
		"id":          "t1",
		"description": "My task",
		"model":       "claude-opus-4-8",
		"columns":     120,
	})
	stdout, stderr, code := runAgentRowCLI(t, nil, stdin, nil)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("stdout is not JSON %q: %v", stdout, err)
	}
	if len(resp) != 2 {
		t.Errorf("response keys = %d, want exactly v and body: %q", len(resp), stdout)
	}
	body := agentRowBody(t, stdout)
	if !strings.Contains(body, "My task") || !strings.Contains(body, "Opus 4.8") {
		t.Errorf("body = %q, want task description and model", body)
	}
}

func TestAgentRow_VNot1EmitsNothing(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocBasic)
	row := map[string]any{"id": "t1", "description": "My task"}

	cases := map[string]any{
		"v=2":     2,
		"v=0":     0,
		"v=\"1\"": "1",
	}
	for name, v := range cases {
		stdout, stderr, code := runAgentRowCLI(t, nil, agentRowStdin(t, v, row), nil)
		if code != 0 || stdout != "" {
			t.Errorf("%s: exit = %d, stdout = %q, stderr = %q; want exit 0 and empty stdout", name, code, stdout, stderr)
		}
	}

	noV := map[string]any{"sessionId": "s", "row": row}
	data, err := json.Marshal(noV)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	stdout, _, code := runAgentRowCLI(t, nil, data, nil)
	if code != 0 || stdout != "" {
		t.Errorf("no v key: exit = %d, stdout = %q; want exit 0 and empty stdout", code, stdout)
	}
}

func TestAgentRow_HasBodyAnswersWithoutRendering(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocBasic)

	for _, body := range []string{"", "custom"} {
		row := map[string]any{"id": "t1", "description": "My task", "body": body}
		stdout, stderr, code := runAgentRowCLI(t, nil, agentRowStdin(t, 1, row), nil)
		if code != 0 || stdout != "{\"v\":1}\n" {
			t.Errorf("body %q: exit = %d, stdout = %q, stderr = %q; want {\"v\":1}", body, code, stdout, stderr)
		}
	}
}

func TestAgentRow_NoSubagentRegionAnswersEmptyBody(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, `<statusloom version="1" tool="claude-code" color-level="none"><layout name="default" active="true"><line><field name="model"/></line></layout></statusloom>`)

	row := map[string]any{"id": "t1", "description": "My task"}
	stdout, _, code := runAgentRowCLI(t, nil, agentRowStdin(t, 1, row), nil)
	if code != 0 || stdout != "{\"v\":1}\n" {
		t.Errorf("exit = %d, stdout = %q; want exit 0 and {\"v\":1}", code, stdout)
	}
}

func TestAgentRow_WidthFromColumnsAndDepth(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocResponsiveCLI)

	twoLevels := []map[string]any{
		{"id": "p1", "parentId": "root"},
		{"id": "root"},
	}
	threeLevels := []map[string]any{
		{"id": "p2", "parentId": "p1"},
		{"id": "p1", "parentId": "root"},
		{"id": "root"},
	}

	cases := []struct {
		name   string
		row    map[string]any
		agents []map[string]any
		want   string
	}{
		{
			name: "columns 22 no depth picks wide",
			row:  map[string]any{"id": "t1", "description": "My task", "columns": 22},
			want: "wide: My task",
		},
		{
			name:   "columns 22 depth 2 narrows to 18",
			row:    map[string]any{"id": "t1", "description": "My task", "columns": 22, "parentId": "p1"},
			agents: twoLevels,
			want:   "narrow: My task",
		},
		{
			name:   "no columns depth 2 stays at width 0",
			row:    map[string]any{"id": "t1", "description": "My task", "parentId": "p1"},
			agents: twoLevels,
			want:   "wide: My task",
		},
		{
			name:   "columns 4 depth 3 clamps to width 1",
			row:    map[string]any{"id": "t1", "description": "My task", "columns": 4, "parentId": "p2"},
			agents: threeLevels,
			want:   "narrow: My task",
		},
	}
	for _, tc := range cases {
		stdin := agentRowStdin(t, 1, tc.row, tc.agents...)
		stdout, stderr, code := runAgentRowCLI(t, nil, stdin, nil)
		if code != 0 {
			t.Errorf("%s: exit = %d, stderr = %q", tc.name, code, stderr)
			continue
		}
		if body := agentRowBody(t, stdout); body != tc.want {
			t.Errorf("%s: body = %q, want %q", tc.name, body, tc.want)
		}
	}
}

func TestAgentRow_DraftSelection(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocBasic)
	writeDraft(t, "claude-code", subagentDocBasicDraftMarker)
	row := map[string]any{"id": "t1", "description": "My task"}

	cases := []struct {
		name      string
		args      []string
		env       map[string]string
		wantDraft bool
	}{
		{name: "no flag no env uses saved", wantDraft: false},
		{name: "env 1 uses draft", env: map[string]string{"STATUSLOOM_AGENT_ROW_DRAFT": "1"}, wantDraft: true},
		{name: "--draft uses draft", args: []string{"--draft"}, wantDraft: true},
		{name: "env true does not use draft", env: map[string]string{"STATUSLOOM_AGENT_ROW_DRAFT": "true"}, wantDraft: false},
	}
	for _, tc := range cases {
		stdout, stderr, code := runAgentRowCLI(t, tc.args, agentRowStdin(t, 1, row), tc.env)
		if code != 0 {
			t.Errorf("%s: exit = %d, stderr = %q", tc.name, code, stderr)
			continue
		}
		body := agentRowBody(t, stdout)
		if got := strings.Contains(body, "[draft]"); got != tc.wantDraft {
			t.Errorf("%s: draft marker present = %v, want %v (body %q)", tc.name, got, tc.wantDraft, body)
		}
	}
}

func TestAgentRow_DecodeErrorExitsOneWithStderr(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocBasic)

	for _, stdin := range []string{`not json`, `[]`, `{"v":1}`} {
		stdout, stderr, code := runAgentRowCLI(t, nil, []byte(stdin), nil)
		if code != 1 || stdout != "" {
			t.Errorf("stdin %q: exit = %d, stdout = %q; want exit 1 and empty stdout", stdin, code, stdout)
		}
		if n := strings.Count(stderr, "\n"); n != 1 || !strings.HasPrefix(stderr, "statusloom: claude:") {
			t.Errorf("stdin %q: stderr = %q; want exactly one line starting %q", stdin, stderr, "statusloom: claude:")
		}
	}
}

func TestAgentRow_PreviewIgnoresStdin(t *testing.T) {
	setupEnv(t)
	writeSubagentDoc(t, subagentDocBasic)

	var out, errBuf bytes.Buffer
	code := runAgentRow([]string{"--preview"}, errorOnReadReader{t: t}, &out, &errBuf, envMap(nil))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	body := agentRowBody(t, out.String())
	if !strings.Contains(body, "Investigate flaky render test") || !strings.Contains(body, "Opus 4.8") {
		t.Errorf("preview body = %q, want the built-in task and model", body)
	}
}
