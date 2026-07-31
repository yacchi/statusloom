package claudecfg

import (
	"path/filepath"
	"testing"
)

// env builds a getenv from a map, so a test never reads the real
// environment.
func env(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestDir(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "CLAUDE_CONFIG_DIR wins over HOME",
			env:  map[string]string{"CLAUDE_CONFIG_DIR": "/profiles/max", "HOME": "/home/u"},
			want: "/profiles/max",
		},
		{
			name: "falls back to HOME/.claude",
			env:  map[string]string{"HOME": "/home/u"},
			want: filepath.Join("/home/u", ".claude"),
		},
		{
			name: "empty CLAUDE_CONFIG_DIR is treated as unset",
			env:  map[string]string{"CLAUDE_CONFIG_DIR": "", "HOME": "/home/u"},
			want: filepath.Join("/home/u", ".claude"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Dir(env(tt.env)); got != tt.want {
				t.Errorf("Dir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSettingsPath(t *testing.T) {
	got := SettingsPath(env(map[string]string{"CLAUDE_CONFIG_DIR": "/profiles/max"}))
	if want := filepath.Join("/profiles/max", "settings.json"); got != want {
		t.Errorf("SettingsPath() = %q, want %q", got, want)
	}
	got = SettingsPath(env(map[string]string{"HOME": "/home/u"}))
	if want := filepath.Join("/home/u", ".claude", "settings.json"); got != want {
		t.Errorf("SettingsPath() = %q, want %q", got, want)
	}
}

func TestCredentialsPath(t *testing.T) {
	got := CredentialsPath(env(map[string]string{"CLAUDE_CONFIG_DIR": "/profiles/max"}))
	if want := filepath.Join("/profiles/max", ".credentials.json"); got != want {
		t.Errorf("CredentialsPath() = %q, want %q", got, want)
	}
	got = CredentialsPath(env(map[string]string{"HOME": "/home/u"}))
	if want := filepath.Join("/home/u", ".claude", ".credentials.json"); got != want {
		t.Errorf("CredentialsPath() = %q, want %q", got, want)
	}
}

// TestGlobalConfigPath pins the asymmetry: inside the config dir when
// CLAUDE_CONFIG_DIR is set, but a sibling of ~/.claude by default.
func TestGlobalConfigPath(t *testing.T) {
	got := GlobalConfigPath(env(map[string]string{"CLAUDE_CONFIG_DIR": "/profiles/max"}))
	if want := filepath.Join("/profiles/max", ".claude.json"); got != want {
		t.Errorf("GlobalConfigPath() = %q, want %q", got, want)
	}
	got = GlobalConfigPath(env(map[string]string{"HOME": "/home/u"}))
	if want := filepath.Join("/home/u", ".claude.json"); got != want {
		t.Errorf("GlobalConfigPath() = %q, want %q", got, want)
	}
}
