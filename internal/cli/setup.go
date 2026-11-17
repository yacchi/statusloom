package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/claudecfg"
)

// defaultRefreshInterval is the re-render period (seconds) that setup writes
// when --refresh-interval is not given and the existing settings don't
// already carry a usable refreshInterval for that object.
const defaultRefreshInterval = 30

// modMarketplaceName is the key setup merges into Claude Code's
// extraKnownMarketplaces: the name the statusloom marketplace is known by.
const modMarketplaceName = "statusloom"

// modMarketplaceRepo is the GitHub repository that serves the statusloom
// marketplace (its .claude-plugin/marketplace.json).
const modMarketplaceRepo = "yacchi/statusloom"

// modPluginID is the enabledPlugins key of the statusloom mod: plugin name
// "statusloom" from the marketplace named modMarketplaceName.
const modPluginID = "statusloom@statusloom"

// modPluginName is the plugin's name inside the statusloom marketplace.
const modPluginName = "statusloom"

// modInstallFallback is printed when setup could not run `claude plugin
// install` itself (claude missing, timeout, non-zero exit). It states the
// install truth: Claude Code installs the mod itself, after the next session
// start, and a running session needs /reload-plugins to load it.
const modInstallFallback = "The statusloom mod (%[1]s) is enabled. Claude Code clones the %[2]s marketplace and installs the mod in the background after the next session start (network needed).\n" +
	"To install right away: claude plugin install %[3]s --marketplace %[2]s; a session that is already running then loads it with /reload-plugins.\n"

// modInstallSuccess is printed when setup ran `claude plugin install`
// successfully.
const modInstallSuccess = "Installed the statusloom mod (%[1]s) with `claude plugin install`. A session that is already running loads it with /reload-plugins.\n"

// modRequirementNotice is always printed after the install line.
const modRequirementNotice = "The mod runs with your permissions and needs a Claude Code version with mods (verified on 2.1.326).\n"

// modInstallTimeout bounds the marketplace add and `claude plugin install`
// together; the add clones the marketplace over the network.
const modInstallTimeout = 120 * time.Second

// runModInstall runs `claude plugin install statusloom --marketplace
// yacchi/statusloom` in the given environment and returns the combined output
// (for the one-line summary only). `--marketplace` adds the marketplace first:
// the CLI does not fetch one that is only declared in extraKnownMarketplaces
// (an interactive session clones it in the background at start). Re-running it
// is harmless ("already added" / "already installed", exit 0). It is a var so
// tests do not spawn the real claude.
var runModInstall = func(ctx context.Context, env []string) (string, error) {
	claude, err := exec.LookPath("claude")
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, claude, "plugin", "install", modPluginName, "--marketplace", modMarketplaceRepo)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// modInstallEnv returns the child environment for installing into the profile
// whose settings file is settings. The default settings path
// (claudecfg.SettingsPath) keeps the inherited environment untouched: setting
// CLAUDE_CONFIG_DIR even to ~/.claude would change where Claude Code reads
// .claude.json and its Keychain service name. Any other path adds
// CLAUDE_CONFIG_DIR=<dir of settings>.
func modInstallEnv(settings string, getenv func(string) string, base []string) []string {
	env := append([]string(nil), base...)
	if def := claudecfg.SettingsPath(getenv); def != "" && filepath.Clean(def) == filepath.Clean(settings) {
		return env
	}
	return append(env, "CLAUDE_CONFIG_DIR="+filepath.Dir(settings))
}

// installMod runs `claude plugin install` for the profile of settings and
// prints one summary line. It never fails setup: any error prints the
// fallback guidance.
func installMod(stdout io.Writer, settings string, getenv func(string) string) {
	ctx, cancel := context.WithTimeout(context.Background(), modInstallTimeout)
	defer cancel()
	if _, err := runModInstall(ctx, modInstallEnv(settings, getenv, os.Environ())); err != nil {
		fmt.Fprintf(stdout, "Could not run `claude plugin install` (%s).\n", summarizeInstallError(ctx, err))
		fmt.Fprintf(stdout, modInstallFallback, modPluginID, modMarketplaceRepo, modPluginName)
	} else {
		fmt.Fprintf(stdout, modInstallSuccess, modPluginID)
	}
	fmt.Fprint(stdout, modRequirementNotice)
}

func summarizeInstallError(ctx context.Context, err error) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, exec.ErrNotFound):
		return "claude not found in PATH"
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("exit status %d", ee.ExitCode())
	}
	return err.Error()
}

// settingsChange is one settings value that setup adds, replaces or removes.
// label names the value as printed in the diff: statusLine,
// subagentStatusLine, extraKnownMarketplaces.statusloom or
// enabledPlugins.statusloom@statusloom. had reports whether the value existed
// before; before holds it when it did. remove marks a deletion, in which case
// after is not printed.
type settingsChange struct {
	label  string
	before any
	had    bool
	after  any
	remove bool
}

// desiredStatusLine builds the statusLine object statusloom wants written
// to Claude Code settings. refreshInterval < 1 means "unset": the key is
// omitted entirely (what `--refresh-interval 0` asks for).
//
// refreshInterval is a float64 so this value compares equal, via
// reflect.DeepEqual, to a statusLine round-tripped through json.Unmarshal
// into map[string]any — encoding/json always decodes JSON numbers as
// float64 regardless of how they were written.
func desiredStatusLine(refreshInterval float64) map[string]any {
	m := map[string]any{"type": "command", "command": "statusloom claude"}
	if refreshInterval >= 1 {
		m["refreshInterval"] = refreshInterval
	}
	return m
}

// desiredMarketplace builds the extraKnownMarketplaces entry that points at
// the statusloom GitHub marketplace. Every value is a string, so the entry
// compares equal, via reflect.DeepEqual, to one round-tripped through JSON.
func desiredMarketplace() map[string]any {
	return map[string]any{"source": map[string]any{"source": "github", "repo": modMarketplaceRepo}}
}

// isStatusloomSubagentStatusLine reports whether v, a decoded
// subagentStatusLine value, is one statusloom wrote: its command splits on
// whitespace into a first token whose base name (after the last "/" or "\")
// is statusloom or statusloom.exe, followed by the claude-subagent
// subcommand. Quoted paths and any other command count as third-party.
func isStatusloomSubagentStatusLine(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	command, ok := m["command"].(string)
	if !ok {
		return false
	}
	tok := strings.Fields(command)
	if len(tok) < 2 {
		return false
	}
	base := tok[0][strings.LastIndexAny(tok[0], "/\\")+1:]
	return (base == "statusloom" || base == "statusloom.exe") && tok[1] == "claude-subagent"
}

// mergeObjectEntry sets doc[key][entry] to want, keeping every other entry of
// the object. A missing or null key gets a new object. A key holding a
// non-object value is replaced by an object holding only this entry; that is
// reported as a replacement (replaces is true). ok is false when the entry
// already equals want, in which case nothing changes.
func mergeObjectEntry(doc map[string]any, key, entry string, want any) (c settingsChange, replaces, ok bool) {
	raw, present := doc[key]
	if present && raw != nil {
		if _, isObject := raw.(map[string]any); !isObject {
			doc[key] = map[string]any{entry: want}
			return settingsChange{label: key, before: raw, had: true, after: doc[key]}, true, true
		}
	}
	m, _ := raw.(map[string]any)
	if m == nil {
		m = map[string]any{}
		doc[key] = m
	}
	old, had := m[entry]
	if had && reflect.DeepEqual(old, want) {
		return settingsChange{}, false, false
	}
	m[entry] = want
	return settingsChange{label: key + "." + entry, before: old, had: had, after: want}, had, true
}

// resolveRefreshInterval decides the refreshInterval for one settings object
// (existing is its current value, nil when absent). An explicit flag always
// wins: N >= 1 is written, 0 yields unset. Without the flag, an existing
// numeric refreshInterval >= 1 is kept so a user's own choice is not
// overwritten; otherwise the default applies.
func resolveRefreshInterval(flagSet bool, flagValue int, existing any) float64 {
	if flagSet {
		if flagValue >= 1 {
			return float64(flagValue)
		}
		return 0
	}
	if obj, ok := existing.(map[string]any); ok {
		if n, ok := obj["refreshInterval"].(float64); ok && n >= 1 {
			return n
		}
	}
	return defaultRefreshInterval
}

// runSetup implements `statusloom setup claude-code`. It merges the statusLine
// and the statusloom mod entries into the Claude Code settings file, keeping
// every other key. It removes a subagentStatusLine that statusloom wrote and
// leaves any other subagentStatusLine untouched.
func runSetup(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 || args[0] != "claude-code" {
		fmt.Fprintln(stderr, "statusloom: usage: statusloom setup claude-code [--settings PATH] [--yes] [--dry-run] [--refresh-interval N]")
		return 2
	}
	fs := flag.NewFlagSet("setup claude-code", flag.ContinueOnError)
	fs.SetOutput(stderr)
	settingsFlag := fs.String("settings", "", "Claude Code settings file")
	yes := fs.Bool("yes", false, "replace existing settings without prompting")
	dryRun := fs.Bool("dry-run", false, "show changes without writing")
	refreshInterval := fs.Int("refresh-interval", 0, "seconds between forced statusline re-renders while idle (default 30; 0 leaves it unset)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return 2
	}
	refreshIntervalSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "refresh-interval" {
			refreshIntervalSet = true
		}
	})
	if refreshIntervalSet && *refreshInterval < 0 {
		fmt.Fprintln(stderr, "statusloom: --refresh-interval must be >= 0 (0 leaves it unset)")
		return 2
	}
	settings, err := claudeSettingsPath(*settingsFlag, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "statusloom: %v\n", err)
		return 1
	}
	doc := make(map[string]any)
	original, err := os.ReadFile(settings)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "statusloom: read %s: %v\n", settings, err)
		return 1
	}
	if exists {
		if err := json.Unmarshal(original, &doc); err != nil {
			fmt.Fprintf(stderr, "statusloom: invalid Claude Code settings JSON: %v\n", err)
			return 1
		}
	}

	// Every change is applied to doc in memory; only writeJSONAtomic touches
	// the file, after the user has confirmed.
	var changes []settingsChange
	needConfirm := false

	before, hadStatusLine := doc["statusLine"]
	desired := desiredStatusLine(resolveRefreshInterval(refreshIntervalSet, *refreshInterval, before))
	statusLineChanged := !hadStatusLine || !reflect.DeepEqual(before, desired)
	if statusLineChanged {
		changes = append(changes, settingsChange{label: "statusLine", before: before, had: hadStatusLine, after: desired})
		needConfirm = needConfirm || hadStatusLine
		doc["statusLine"] = desired
	}

	// subagentStatusLine: a line statusloom wrote is removed without asking.
	// Any other line is kept, and the note below says so.
	thirdPartySub := false
	if sub, hadSub := doc["subagentStatusLine"]; hadSub {
		if isStatusloomSubagentStatusLine(sub) {
			delete(doc, "subagentStatusLine")
			changes = append(changes, settingsChange{label: "subagentStatusLine", before: sub, had: true, remove: true})
		} else {
			thirdPartySub = true
		}
	}

	if c, replaces, ok := mergeObjectEntry(doc, "extraKnownMarketplaces", modMarketplaceName, desiredMarketplace()); ok {
		changes = append(changes, c)
		needConfirm = needConfirm || replaces
	}
	if c, replaces, ok := mergeObjectEntry(doc, "enabledPlugins", modPluginID, true); ok {
		changes = append(changes, c)
		needConfirm = needConfirm || replaces
	}

	if thirdPartySub {
		sub, _ := json.Marshal(doc["subagentStatusLine"])
		fmt.Fprintf(stdout, "Note: kept subagentStatusLine %s: it is not statusloom's, so it keeps drawing the agent rows instead of the statusloom mod.\n", sub)
	}
	if len(changes) == 0 {
		fmt.Fprintln(stdout, "already configured")
		if !*dryRun {
			// Idempotent: `claude plugin install` on an installed plugin exits 0.
			installMod(stdout, settings, getenv)
		}
		return 0
	}
	for _, c := range changes {
		printSettingsChange(stdout, c)
	}
	if hadStatusLine && statusLineChanged {
		if hook, ok := detectKnownStatusLineHook(before); ok {
			hookDesc := "own hook"
			if path := hook.path(getenv); path != "" {
				hookDesc = "own " + path + " hook"
			}
			fmt.Fprintf(stdout, "Note: existing statusLine looks like %s's %s. `statusloom claude` forwards rendered payloads to it automatically, so %s's integration will keep working after this change.\n", hook.name, hookDesc, hook.name)
		}
	}
	if *dryRun {
		if exists {
			fmt.Fprintf(stdout, "Would update %s.\n", settings)
		} else {
			fmt.Fprintf(stdout, "Would configure %s with statusloom claude.\n", settings)
		}
		return 0
	}
	if needConfirm && !*yes {
		fmt.Fprint(stdout, "Replace existing settings? [y/N] ")
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(stderr, "aborted")
			return 1
		}
	}
	if exists {
		backup, err := backupFile(settings)
		if err != nil {
			fmt.Fprintf(stderr, "statusloom: backup settings: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Backup: %s\nRollback: mv %s %s\n", backup, backup, settings)
	}
	if err := writeJSONAtomic(settings, doc); err != nil {
		fmt.Fprintf(stderr, "statusloom: write settings: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Configured %s.\n", settings)
	installMod(stdout, settings, getenv)
	return 0
}

func claudeSettingsPath(override string, getenv func(string) string) (string, error) {
	if override != "" {
		return override, nil
	}
	p := claudecfg.SettingsPath(getenv)
	if p == "" {
		return "", errors.New("cannot determine Claude Code config dir: set $CLAUDE_CONFIG_DIR or $HOME")
	}
	return p, nil
}

// printSettingsChange prints one change as a "- label: before" line when the
// value existed, then a "+ label: after" line unless the value is removed.
func printSettingsChange(w io.Writer, c settingsChange) {
	if c.had {
		oldJSON, _ := json.Marshal(c.before)
		fmt.Fprintf(w, "- %s: %s\n", c.label, oldJSON)
	}
	if !c.remove {
		newJSON, _ := json.Marshal(c.after)
		fmt.Fprintf(w, "+ %s: %s\n", c.label, newJSON)
	}
}
