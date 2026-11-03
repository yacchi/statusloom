package sessionexport

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// rowSetState points XDG_STATE_HOME at a fresh temp dir and returns the expected detail path
// for pid 12345 / startedAt 1793672985.
func rowSetState(t *testing.T) string {
	t.Helper()
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	return filepath.Join(stateDir, "statusloom", "sessions", "12345-1793672985.json")
}

func rowInt64(v int64) *int64 { return &v }

func TestRowFromPayloadFixture(t *testing.T) {
	dp := rowSetState(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "claude", "full.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := RowFromPayload(raw, 12345, 1793672985, "/c")
	if err != nil {
		t.Fatalf("RowFromPayload: %v", err)
	}
	want := Row{
		PID:            12345,
		StartedAt:      1793672985,
		SessionID:      "3f9a2b1c-4d5e-6f70-8a9b-0c1d2e3f4a5b",
		TranscriptPath: "/Users/dev/.claude/projects/-Users-dev-myapp/3f9a2b1c.jsonl",
		ConfigDir:      "/c",
		Cwd:            "/Users/dev/myapp",
		Model:          "claude-opus-4-8",
		PromptCache:    &PromptCache{TTL: "1h", ExpiresAt: rowInt64(1783735200)},
		DetailPath:     dp,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("row = %+v, want %+v", got, want)
	}
}

func TestRowFromPayloadFields(t *testing.T) {
	cases := []struct {
		name      string
		payload   string
		configDir string
		want      Row
	}{
		{
			name:    "cwd is top-level not workspace.current_dir",
			payload: `{"cwd":"/a","workspace":{"current_dir":"/b"}}`,
			want:    Row{Cwd: "/a"},
		},
		{
			name:    "wrong-typed and null strings become empty",
			payload: `{"session_id":5,"cwd":null,"model":"x","transcript_path":["p"]}`,
			want:    Row{},
		},
		{
			name:    "model id wrong type",
			payload: `{"model":{"id":7}}`,
			want:    Row{},
		},
		{
			name:    "model null",
			payload: `{"model":null}`,
			want:    Row{},
		},
		{
			name:    "prompt_cache absent",
			payload: `{}`,
			want:    Row{},
		},
		{
			name:    "prompt_cache null",
			payload: `{"prompt_cache":null}`,
			want:    Row{},
		},
		{
			name:    "prompt_cache string",
			payload: `{"prompt_cache":"1h"}`,
			want:    Row{},
		},
		{
			name:    "prompt_cache array",
			payload: `{"prompt_cache":[]}`,
			want:    Row{},
		},
		{
			name:    "prompt_cache number",
			payload: `{"prompt_cache":5}`,
			want:    Row{},
		},
		{
			name:    "empty prompt_cache object is non-nil",
			payload: `{"prompt_cache":{}}`,
			want:    Row{PromptCache: &PromptCache{}},
		},
		{
			name:    "expires_at null leaves ExpiresAt nil and keeps ttl",
			payload: `{"prompt_cache":{"ttl":"1h","expires_at":null}}`,
			want:    Row{PromptCache: &PromptCache{TTL: "1h"}},
		},
		{
			name:    "expires_at absent",
			payload: `{"prompt_cache":{"ttl":"1h"}}`,
			want:    Row{PromptCache: &PromptCache{TTL: "1h"}},
		},
		{
			name:    "wrong types inside prompt_cache",
			payload: `{"prompt_cache":{"ttl":5,"expires_at":"1"}}`,
			want:    Row{PromptCache: &PromptCache{}},
		},
		{
			name:    "fractional expires_at truncated toward zero",
			payload: `{"prompt_cache":{"ttl":"5m","expires_at":1783735200.9}}`,
			want:    Row{PromptCache: &PromptCache{TTL: "5m", ExpiresAt: rowInt64(1783735200)}},
		},
		{
			name:    "negative fractional expires_at truncated toward zero",
			payload: `{"prompt_cache":{"expires_at":-1.5}}`,
			want:    Row{PromptCache: &PromptCache{ExpiresAt: rowInt64(-1)}},
		},
		{
			name:    "zero expires_at is non-nil",
			payload: `{"prompt_cache":{"expires_at":0}}`,
			want:    Row{PromptCache: &PromptCache{ExpiresAt: rowInt64(0)}},
		},
		{
			name:    "2^63 is out of range",
			payload: `{"prompt_cache":{"expires_at":9223372036854775808}}`,
			want:    Row{PromptCache: &PromptCache{}},
		},
		{
			name:    "1e19 is out of range",
			payload: `{"prompt_cache":{"expires_at":1e19}}`,
			want:    Row{PromptCache: &PromptCache{}},
		},
		{
			name:    "-2^63 is in range",
			payload: `{"prompt_cache":{"expires_at":-9223372036854775808}}`,
			want:    Row{PromptCache: &PromptCache{ExpiresAt: rowInt64(math.MinInt64)}},
		},
		{
			name:      "configDir empty stays empty",
			payload:   `{}`,
			configDir: "",
			want:      Row{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dp := rowSetState(t)
			tc.want.PID = 12345
			tc.want.StartedAt = 1793672985
			tc.want.ConfigDir = tc.configDir
			tc.want.DetailPath = dp
			got, err := RowFromPayload([]byte(tc.payload), 12345, 1793672985, tc.configDir)
			if err != nil {
				t.Fatalf("RowFromPayload: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("row = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRowFromPayloadRejects(t *testing.T) {
	cases := []string{
		`null`,
		` null `,
		`[]`,
		`[{}]`,
		`"x"`,
		`5`,
		`true`,
		``,
		`{`,
	}
	for _, payload := range cases {
		t.Run(payload, func(t *testing.T) {
			rowSetState(t)
			got, err := RowFromPayload([]byte(payload), 12345, 1793672985, "")
			if err == nil {
				t.Errorf("RowFromPayload(%q) error = nil, want error", payload)
			}
			if !reflect.DeepEqual(got, Row{}) {
				t.Errorf("row = %+v, want zero Row", got)
			}
		})
	}
}

func TestRowFromPayloadDetailPathError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME and XDG_STATE_HOME resolution differs on windows")
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	got, err := RowFromPayload([]byte(`{}`), 12345, 1793672985, "")
	if err == nil {
		t.Error("RowFromPayload error = nil, want DetailPath error")
	}
	if !reflect.DeepEqual(got, Row{}) {
		t.Errorf("row = %+v, want zero Row", got)
	}
}
