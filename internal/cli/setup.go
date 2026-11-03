package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/yacchi/statusloom/internal/claudecfg"
)

// defaultRefreshInterval is the re-render period (seconds) that setup writes
// when --refresh-interval is not given and the existing settings don't
// already carry a usable refreshInterval for that object.
const defaultRefreshInterval = 30

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

// desiredSubagentStatusLine builds the subagentStatusLine object statusloom
// wants written to Claude Code settings, mirroring desiredStatusLine's
// shape and refreshInterval handling for the `statusloom claude-subagent`
// command (Claude Code's subagentStatusLine feature).
func desiredSubagentStatusLine(refreshInterval float64) map[string]any {
	m := map[string]any{"type": "command", "command": "statusloom claude-subagent"}
	if refreshInterval >= 1 {
		m["refreshInterval"] = refreshInterval
	}
	return m
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

func runSetup(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 || args[0] != "claude-code" {
		fmt.Fprintln(stderr, "statusloom: usage: statusloom setup claude-code [--settings PATH] [--yes] [--dry-run] [--refresh-interval N]")
		return 2
	}
	fs := flag.NewFlagSet("setup claude-code", flag.ContinueOnError)
	fs.SetOutput(stderr)
	settingsFlag := fs.String("settings", "", "Claude Code settings file")
	yes := fs.Bool("yes", false, "replace an existing statusLine without prompting")
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
	settings, err := claudeSettingsPath(*settingsFlag)
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
	before, hadStatusLine := doc["statusLine"]
	desired := desiredStatusLine(resolveRefreshInterval(refreshIntervalSet, *refreshInterval, before))
	statusLineChanged := !hadStatusLine || !reflect.DeepEqual(before, desired)
	beforeSubagent, hadSubagentStatusLine := doc["subagentStatusLine"]
	desiredSubagent := desiredSubagentStatusLine(resolveRefreshInterval(refreshIntervalSet, *refreshInterval, beforeSubagent))
	subagentChanged := !hadSubagentStatusLine || !reflect.DeepEqual(beforeSubagent, desiredSubagent)

	if !statusLineChanged && !subagentChanged {
		fmt.Fprintln(stdout, "already configured")
		return 0
	}
	if hadStatusLine && statusLineChanged {
		printStatusLineDiff(stdout, before, desired)
		if hook, ok := detectKnownStatusLineHook(before); ok {
			hookDesc := "own hook"
			if path := hook.path(getenv); path != "" {
				hookDesc = "own " + path + " hook"
			}
			fmt.Fprintf(stdout, "Note: existing statusLine looks like %s's %s. `statusloom claude` forwards rendered payloads to it automatically, so %s's integration will keep working after this change.\n", hook.name, hookDesc, hook.name)
		}
	}
	if hadSubagentStatusLine && subagentChanged {
		printStatusLineDiff(stdout, beforeSubagent, desiredSubagent)
	}
	if *dryRun {
		if !hadStatusLine && !hadSubagentStatusLine {
			fmt.Fprintf(stdout, "Would configure %s with statusloom claude.\n", settings)
		} else {
			fmt.Fprintf(stdout, "Would update %s.\n", settings)
		}
		return 0
	}
	needConfirm := (hadStatusLine && statusLineChanged) || (hadSubagentStatusLine && subagentChanged)
	if needConfirm && !*yes {
		fmt.Fprint(stdout, "Replace existing statusLine? [y/N] ")
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
	doc["statusLine"] = desired
	doc["subagentStatusLine"] = desiredSubagent
	if err := writeJSONAtomic(settings, doc); err != nil {
		fmt.Fprintf(stderr, "statusloom: write settings: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Configured %s.\n", settings)
	return 0
}

func claudeSettingsPath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	p := claudecfg.SettingsPath(os.Getenv)
	if p == "" {
		return "", errors.New("cannot determine Claude Code config dir: set $CLAUDE_CONFIG_DIR or $HOME")
	}
	return p, nil
}

func printStatusLineDiff(w io.Writer, before, desired any) {
	oldJSON, _ := json.Marshal(before)
	newJSON, _ := json.Marshal(desired)
	fmt.Fprintf(w, "- %s\n+ %s\n", oldJSON, newJSON)
}
