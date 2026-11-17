package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubModInstall replaces runModInstall for one test and records the calls.
func stubModInstall(t *testing.T, err error) *[][]string {
	t.Helper()
	var calls [][]string
	orig := runModInstall
	runModInstall = func(ctx context.Context, env []string) (string, error) {
		calls = append(calls, env)
		return "", err
	}
	t.Cleanup(func() { runModInstall = orig })
	return &calls
}

func hasConfigDirEnv(env []string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], "CLAUDE_CONFIG_DIR="); ok {
			return v, true
		}
	}
	return "", false
}

// TestSetupInstallsModInDefaultProfile: when the settings file is the default
// one (here via --settings pointing at the same cleaned path), the child gets
// the inherited environment: CLAUDE_CONFIG_DIR is not added.
func TestSetupInstallsModInDefaultProfile(t *testing.T) {
	setupEnv(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	calls := stubModInstall(t, nil)
	env := map[string]string{"HOME": home}
	// A non-clean spelling of the default path must still count as default.
	settings := filepath.Join(dir, ".", "settings.json")
	out, _, code := runCLI(t, []string{"setup", "claude-code", "--settings", settings}, nil, env)
	if code != 0 || len(*calls) != 1 {
		t.Fatalf("code=%d calls=%d out=%q", code, len(*calls), out)
	}
	// The inherited environment (here CLAUDE_CONFIG_DIR is set to "") is
	// passed unchanged: setup adds nothing.
	if got, _ := hasConfigDirEnv((*calls)[0]); got != "" {
		t.Errorf("CLAUDE_CONFIG_DIR=%q must not be added for the default path", got)
	}
	if !strings.Contains(out, "Installed or updated the statusloom mod") || !strings.Contains(out, "/reload-plugins") {
		t.Errorf("out=%q", out)
	}
}

// TestSetupInstallsModInOtherProfile: --settings elsewhere installs into that
// directory's profile.
func TestSetupInstallsModInOtherProfile(t *testing.T) {
	setupEnv(t)
	calls := stubModInstall(t, nil)
	dir := t.TempDir()
	out, _, code := runCLI(t, []string{"setup", "claude-code", "--settings", filepath.Join(dir, "settings.json")}, nil, map[string]string{"HOME": t.TempDir()})
	if code != 0 || len(*calls) != 1 {
		t.Fatalf("code=%d calls=%d out=%q", code, len(*calls), out)
	}
	if got, ok := hasConfigDirEnv((*calls)[0]); !ok || got != dir {
		t.Errorf("CLAUDE_CONFIG_DIR=%q ok=%v, want %q", got, ok, dir)
	}
}

func TestSetupDryRunDoesNotInstallMod(t *testing.T) {
	setupEnv(t)
	calls := stubModInstall(t, nil)
	path := filepath.Join(t.TempDir(), "settings.json")
	_, _, code := runCLI(t, []string{"setup", "claude-code", "--dry-run", "--settings", path}, nil, nil)
	if code != 0 || len(*calls) != 0 {
		t.Fatalf("code=%d calls=%d", code, len(*calls))
	}
}

// TestSetupModInstallFailureKeepsSetupSuccessful: a failing runner prints the
// fallback guidance and setup still exits 0 with the settings written.
func TestSetupModInstallFailureKeepsSetupSuccessful(t *testing.T) {
	setupEnv(t)
	stubModInstall(t, errors.New("boom"))
	path := filepath.Join(t.TempDir(), "settings.json")
	out, _, code := runCLI(t, []string{"setup", "claude-code", "--settings", path}, nil, nil)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("settings not written: %v", err)
	}
	for _, want := range []string{"Could not run `claude plugin install`", "after the next session start", "claude plugin install statusloom --marketplace yacchi/statusloom", "/reload-plugins"} {
		if !strings.Contains(out, want) {
			t.Errorf("out missing %q: %q", want, out)
		}
	}
}

// TestSetupNoopStillInstallsMod: settings already configured -> nothing is
// written, but the (idempotent) install still runs with the inherited env.
func TestSetupNoopStillInstallsMod(t *testing.T) {
	setupEnv(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "settings.json")
	env := map[string]string{"HOME": home}
	stubModInstall(t, nil)
	if _, _, code := runCLI(t, []string{"setup", "claude-code", "--settings", settings}, nil, env); code != 0 {
		t.Fatal("first setup failed")
	}
	calls := stubModInstall(t, nil)
	out, _, code := runCLI(t, []string{"setup", "claude-code", "--settings", settings}, nil, env)
	if code != 0 || !strings.HasPrefix(out, "already configured\n") || len(*calls) != 1 {
		t.Fatalf("code=%d calls=%d out=%q", code, len(*calls), out)
	}
	if got, _ := hasConfigDirEnv((*calls)[0]); got != "" {
		t.Errorf("CLAUDE_CONFIG_DIR=%q must not be added for the default path", got)
	}
	// --dry-run on the no-op path must not install.
	runCLI(t, []string{"setup", "claude-code", "--dry-run", "--settings", settings}, nil, env)
	if len(*calls) != 1 {
		t.Errorf("dry-run installed: calls=%d", len(*calls))
	}
}

// stubModInstallDroppingAutoUpdate mimics `claude plugin install
// --marketplace`, which rewrites the statusloom marketplace entry without its
// autoUpdate key.
func stubModInstallDroppingAutoUpdate(t *testing.T, settings string) {
	t.Helper()
	orig := runModInstall
	runModInstall = func(ctx context.Context, env []string) (string, error) {
		raw, err := os.ReadFile(settings)
		if err != nil {
			return "", err
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return "", err
		}
		doc["extraKnownMarketplaces"].(map[string]any)["statusloom"] = map[string]any{
			"source": map[string]any{"source": "github", "repo": "yacchi/statusloom"},
		}
		return "", writeJSONAtomic(settings, doc)
	}
	t.Cleanup(func() { runModInstall = orig })
}

func marketplaceEntryOf(t *testing.T, settings string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc["extraKnownMarketplaces"].(map[string]any)["statusloom"].(map[string]any)
}

// TestSetupRestoresAutoUpdateAfterClaudeRewrite: the claude CLI drops
// autoUpdate when it rewrites the marketplace entry; setup puts back the value
// it settled on, including a user's explicit false.
func TestSetupRestoresAutoUpdateAfterClaudeRewrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		initial string
		want    any
	}{
		{"fresh", `{}`, true},
		{"explicit false", `{"extraKnownMarketplaces":{"statusloom":{"source":{"source":"github","repo":"yacchi/statusloom"},"autoUpdate":false}},"enabledPlugins":{"statusloom@statusloom":true},"statusLine":{"type":"command","command":"statusloom claude","refreshInterval":30}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupEnv(t)
			settings := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(settings, []byte(tc.initial), 0o600); err != nil {
				t.Fatal(err)
			}
			stubModInstallDroppingAutoUpdate(t, settings)
			out, _, code := runCLI(t, []string{"setup", "claude-code", "--settings", settings, "--yes"}, nil, map[string]string{"HOME": t.TempDir()})
			if code != 0 {
				t.Fatalf("code=%d out=%q", code, out)
			}
			if got := marketplaceEntryOf(t, settings)["autoUpdate"]; got != tc.want {
				t.Errorf("autoUpdate = %v, want %v (out=%q)", got, tc.want, out)
			}
		})
	}
}
