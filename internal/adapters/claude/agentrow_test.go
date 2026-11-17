package claude

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// decodeAgentRowDoc decodes {"row": row} plus an optional agents array (""
// omits the key), failing the test on error.
func decodeAgentRowDoc(t *testing.T, row, agents string) AgentRowRequest {
	t.Helper()
	doc := fmt.Sprintf(`{"row":%s`, row)
	if agents != "" {
		doc += fmt.Sprintf(`,"agents":%s`, agents)
	}
	doc += "}"
	req, err := DecodeAgentRow([]byte(doc))
	if err != nil {
		t.Fatalf("DecodeAgentRow(%s): %v", doc, err)
	}
	return req
}

func requireInt(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %d", name, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %d, want %d", name, *got, want)
	}
}

func TestDecodeAgentRow_Fixture(t *testing.T) {
	req, err := DecodeAgentRow(loadFixture(t, "agent-row.json"))
	if err != nil {
		t.Fatalf("DecodeAgentRow: %v", err)
	}

	if req.V != 1 {
		t.Errorf("V = %d, want 1", req.V)
	}
	if req.SessionID != "3f9a2b1c-4d5e-6f70-8a9b-0c1d2e3f4a5b" {
		t.Errorf("SessionID = %q", req.SessionID)
	}
	if req.Columns != 120 {
		t.Errorf("Columns = %d, want 120", req.Columns)
	}
	if req.HasBody {
		t.Errorf("HasBody = true, want false (fixture has no body)")
	}
	if req.Depth != 2 {
		t.Errorf("Depth = %d, want 2", req.Depth)
	}

	task := req.Task
	if task.Depth == nil || *task.Depth != 2 {
		t.Errorf("*Task.Depth = %v, want 2", task.Depth)
	}
	checks := []struct {
		name, got, want string
	}{
		{"ID", task.ID, "a7c3e9f1b2d40586"},
		{"Type", task.Type, "local_agent"},
		{"Status", task.Status, "running"},
		{"Description", task.Description, "Audit DSL validation paths"},
		{"Label", task.Label, "Reading internal/dsl/validate.go"},
		{"ModelID", task.ModelID, "claude-sonnet-5"},
		{"ModelDisplay", task.ModelDisplay, "Sonnet 5"},
		{"Cwd", task.Cwd, "/Users/dev/myapp"},
		{"AgentType", task.AgentType, "Explore"},
		{"Name", task.Name, "dsl-audit"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if !task.StartedAt.Equal(time.UnixMilli(1784104399052)) {
		t.Errorf("StartedAt = %v, want %v", task.StartedAt, time.UnixMilli(1784104399052))
	}
	if task.ContextWindowSize != 200000 {
		t.Errorf("ContextWindowSize = %d, want 200000", task.ContextWindowSize)
	}
	if task.TokenCount != 31250 {
		t.Errorf("TokenCount = %d, want 31250", task.TokenCount)
	}
	if task.Effort == nil || *task.Effort != "0.75" {
		t.Errorf("Effort = %v, want \"0.75\"", task.Effort)
	}
	if task.CostUSD == nil || *task.CostUSD != 0.42 {
		t.Errorf("CostUSD = %v, want 0.42", task.CostUSD)
	}
	requireInt(t, "InputTokens", task.InputTokens, 12800)
	requireInt(t, "OutputTokens", task.OutputTokens, 3450)
	requireInt(t, "CacheReadTokens", task.CacheReadTokens, 42600)
	requireInt(t, "CacheCreationTokens", task.CacheCreationTokens, 5000)
}

func TestDecodeAgentRow_Errors(t *testing.T) {
	tests := []struct {
		name, raw, want string
	}{
		{"invalid JSON", `not json`, "claude: invalid agent-row JSON: "},
		{"empty input", ``, "claude: invalid agent-row JSON: "},
		{"array top level", `[]`, "claude: invalid agent-row JSON: "},
		{"string top level", `""`, "claude: invalid agent-row JSON: "},
		{"number top level", `5`, "claude: invalid agent-row JSON: "},
		{"null top level", `null`, "claude: invalid agent-row JSON: not an object"},
		{"no row", `{}`, "claude: agent-row request has no row object"},
		{"null row", `{"row":null}`, "claude: agent-row request has no row object"},
		{"array row", `{"row":[]}`, "claude: agent-row request has no row object"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeAgentRow([]byte(tc.raw))
			if err == nil {
				t.Fatalf("DecodeAgentRow(%q) returned no error", tc.raw)
			}
			if !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("error = %q, want prefix %q", err.Error(), tc.want)
			}
		})
	}
}

func TestDecodeAgentRow_WrongTypedKeysDoNotFail(t *testing.T) {
	req, err := DecodeAgentRow([]byte(`{"v":"1","row":{"tokenCount":"many","model":5}}`))
	if err != nil {
		t.Fatalf("DecodeAgentRow: %v", err)
	}
	if req.V != 0 {
		t.Errorf("V = %d, want 0", req.V)
	}
	if req.Task.TokenCount != 0 {
		t.Errorf("TokenCount = %d, want 0", req.Task.TokenCount)
	}
	if req.Task.ModelID != "" {
		t.Errorf("ModelID = %q, want empty", req.Task.ModelID)
	}
}

func TestDecodeAgentRow_Status(t *testing.T) {
	tests := []struct {
		name, row, want string
	}{
		{"empty status falls back", `{"status":"","taskStatus":"completed"}`, "completed"},
		{"taskStatus only", `{"taskStatus":"failed"}`, "failed"},
		{"status wins", `{"status":"idle","taskStatus":"running"}`, "idle"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodeAgentRowDoc(t, tc.row, "").Task.Status; got != tc.want {
				t.Errorf("Status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecodeAgentRow_Effort(t *testing.T) {
	tests := []struct {
		name, row string
		want      *string
	}{
		{"string", `{"effort":"high"}`, strPtr("high")},
		{"integer literal", `{"effort":4}`, strPtr("4")},
		{"fraction literal kept", `{"effort":0.75}`, strPtr("0.75")},
		{"empty string", `{"effort":""}`, nil},
		{"null", `{"effort":null}`, nil},
		{"bool", `{"effort":true}`, nil},
		{"object", `{"effort":{}}`, nil},
		{"absent", `{}`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeAgentRowDoc(t, tc.row, "").Task.Effort
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("Effort = %q, want nil", *got)
			case tc.want != nil && (got == nil || *got != *tc.want):
				t.Errorf("Effort = %v, want %q", got, *tc.want)
			}
		})
	}
}

func TestDecodeAgentRow_Depth(t *testing.T) {
	cycle := `[{"id":"A","parentId":"B"},{"id":"B","parentId":"A"}]`
	tests := []struct {
		name, row, agents string
		want              int
	}{
		{"parentId absent", `{}`, "", 0},
		{"parentId empty", `{"parentId":""}`, "", 0},
		{"parent missing, agents absent", `{"parentId":"X"}`, "", 1},
		{"parent missing from agents", `{"parentId":"X"}`, `[{"id":"A"}]`, 1},
		{"chain of three", `{"parentId":"C"}`, `[{"id":"C","parentId":"B"},{"id":"B","parentId":"A"},{"id":"A"}]`, 3},
		{"cycle stops at 16", `{"parentId":"A"}`, cycle, 16},
		{"self-loop stops at 16", `{"parentId":"A"}`, `[{"id":"A","parentId":"A"}]`, 16},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodeAgentRowDoc(t, tc.row, tc.agents).Depth; got != tc.want {
				t.Errorf("Depth = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDecodeAgentRow_UsageNilVersusZero(t *testing.T) {
	t.Run("absent leaves nil", func(t *testing.T) {
		task := decodeAgentRowDoc(t, `{}`, "").Task
		if task.InputTokens != nil || task.OutputTokens != nil || task.CacheReadTokens != nil || task.CacheCreationTokens != nil {
			t.Errorf("token pointers = non-nil for absent usage, want all nil")
		}
	})
	t.Run("null leaves nil", func(t *testing.T) {
		task := decodeAgentRowDoc(t, `{"usage":null}`, "").Task
		if task.InputTokens != nil || task.OutputTokens != nil {
			t.Errorf("token pointers = non-nil for null usage, want all nil")
		}
	})
	t.Run("empty array gives zeros", func(t *testing.T) {
		task := decodeAgentRowDoc(t, `{"usage":[]}`, "").Task
		requireInt(t, "InputTokens", task.InputTokens, 0)
		requireInt(t, "OutputTokens", task.OutputTokens, 0)
		requireInt(t, "CacheReadTokens", task.CacheReadTokens, 0)
		requireInt(t, "CacheCreationTokens", task.CacheCreationTokens, 0)
	})
	t.Run("non-object elements skipped", func(t *testing.T) {
		task := decodeAgentRowDoc(t, `{"usage":[{"input_tokens":5},"x"]}`, "").Task
		requireInt(t, "InputTokens", task.InputTokens, 5)
		requireInt(t, "OutputTokens", task.OutputTokens, 0)
		requireInt(t, "CacheReadTokens", task.CacheReadTokens, 0)
		requireInt(t, "CacheCreationTokens", task.CacheCreationTokens, 0)
	})
}

func TestDecodeAgentRow_HasBody(t *testing.T) {
	tests := []struct {
		name, row string
		want      bool
	}{
		{"empty string body", `{"body":""}`, true},
		{"string body", `{"body":"x"}`, true},
		{"null body", `{"body":null}`, false},
		{"number body", `{"body":3}`, false},
		{"absent body", `{}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := decodeAgentRowDoc(t, tc.row, "").HasBody; got != tc.want {
				t.Errorf("HasBody = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDecodeAgentRow_CostUSD(t *testing.T) {
	tests := []struct {
		name, row string
		want      *float64
	}{
		{"zero kept", `{"cost":{"usd":0}}`, floatPtr(0)},
		{"empty object", `{"cost":{}}`, nil},
		{"string usd", `{"cost":{"usd":"1"}}`, nil},
		{"null cost", `{"cost":null}`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeAgentRowDoc(t, tc.row, "").Task.CostUSD
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("CostUSD = %v, want nil", *got)
			case tc.want != nil && (got == nil || *got != *tc.want):
				t.Errorf("CostUSD = %v, want %v", got, *tc.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func floatPtr(f float64) *float64 { return &f }
