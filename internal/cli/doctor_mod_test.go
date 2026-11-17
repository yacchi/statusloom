package cli

import (
	"os"
	"strings"
	"testing"
)

// modLine returns the doctor line for the mod check, or "" when none is printed.
func modLine(out string) string {
	for _, l := range lines(out) {
		if strings.Contains(l, " mod - ") {
			return l
		}
	}
	return ""
}

func TestDoctorModRows(t *testing.T) {
	const statusloomSubagent = `{"type":"command","command":"/usr/local/bin/statusloom claude-subagent"}`
	const otherSubagent = `{"type":"command","command":"other-tool rows"}`
	const lookalikeSubagent = `{"type":"command","command":"statusloom-extra claude-subagent"}`
	const enabled = `"enabledPlugins":{"statusloom@statusloom":true}`

	tests := []struct {
		name     string
		settings string
		prefix   string
		want     string
	}{
		{
			name:     "not enabled",
			settings: `{}`,
			prefix:   "WARN mod - ",
			want:     "statusloom setup claude-code",
		},
		{
			name:     "enabled",
			settings: `{` + enabled + `}`,
			prefix:   "PASS mod - ",
			want:     "statusloom@statusloom enabled",
		},
		{
			name:     "enabled with statusloom subagentStatusLine still set",
			settings: `{` + enabled + `,"subagentStatusLine":` + statusloomSubagent + `}`,
			prefix:   "WARN mod - ",
			want:     "to migrate",
		},
		{
			name:     "enabled with third-party subagentStatusLine",
			settings: `{` + enabled + `,"subagentStatusLine":` + otherSubagent + `}`,
			prefix:   "PASS mod - ",
			want:     "a subagentStatusLine from another tool draws the agent rows",
		},
		{
			name:     "not enabled with lookalike subagentStatusLine",
			settings: `{"subagentStatusLine":` + lookalikeSubagent + `}`,
			prefix:   "WARN mod - ",
			want:     "statusloom setup claude-code",
		},
		{
			name:     "enabled false",
			settings: `{"enabledPlugins":{"statusloom@statusloom":false}}`,
			prefix:   "WARN mod - ",
			want:     "statusloom setup claude-code",
		},
		{
			name:     "enabled as string",
			settings: `{"enabledPlugins":{"statusloom@statusloom":"true"}}`,
			prefix:   "WARN mod - ",
			want:     "statusloom setup claude-code",
		},
		{
			name:     "enabledPlugins not an object",
			settings: `{"enabledPlugins":"x"}`,
			prefix:   "WARN mod - ",
			want:     "statusloom setup claude-code",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := setupDoctorSources(t)
			if err := os.WriteFile(settings, []byte(tt.settings), 0o600); err != nil {
				t.Fatal(err)
			}
			out, code := runDoctorOut(t, settings)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0; out=%q", code, out)
			}
			line := modLine(out)
			if !strings.HasPrefix(line, tt.prefix) {
				t.Fatalf("mod line = %q, want prefix %q", line, tt.prefix)
			}
			if !strings.Contains(line, tt.want) {
				t.Errorf("mod line = %q, want it to contain %q", line, tt.want)
			}
		})
	}
}

func TestDoctorModNoLineForInvalidSettings(t *testing.T) {
	settings := setupDoctorSources(t)
	if err := os.WriteFile(settings, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := runDoctorOut(t, settings)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; out=%q", code, out)
	}
	if line := modLine(out); line != "" {
		t.Errorf("unexpected mod line for invalid JSON: %q", line)
	}
}
