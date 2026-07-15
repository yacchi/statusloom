package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// desiredStatusLine builds the statusLine object statusloom wants written
// to Claude Code settings. refreshInterval < 1 means "unspecified": the
// key is omitted entirely so existing configs without it stay untouched.
//
// refreshInterval is stored as float64 (not int) so this value compares
// equal, via reflect.DeepEqual, to a statusLine round-tripped through
// json.Unmarshal into map[string]any — encoding/json always decodes JSON
// numbers as float64 regardless of how they were written.
func desiredStatusLine(refreshInterval int) map[string]any {
	m := map[string]any{"type": "command", "command": "statusloom claude"}
	if refreshInterval >= 1 {
		m["refreshInterval"] = float64(refreshInterval)
	}
	return m
}

// desiredSubagentStatusLine builds the subagentStatusLine object statusloom
// wants written to Claude Code settings, mirroring desiredStatusLine's
// shape and refreshInterval handling for the `statusloom claude-subagent`
// command (Claude Code's subagentStatusLine feature).
func desiredSubagentStatusLine(refreshInterval int) map[string]any {
	m := map[string]any{"type": "command", "command": "statusloom claude-subagent"}
	if refreshInterval >= 1 {
		m["refreshInterval"] = float64(refreshInterval)
	}
	return m
}

func runSetup(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "claude-code" {
		fmt.Fprintln(stderr, "statusloom: usage: statusloom setup claude-code [--settings PATH] [--yes] [--dry-run] [--refresh-interval N]")
		return 2
	}
	fs := flag.NewFlagSet("setup claude-code", flag.ContinueOnError)
	fs.SetOutput(stderr)
	settingsFlag := fs.String("settings", "", "Claude Code settings file")
	yes := fs.Bool("yes", false, "replace an existing statusLine without prompting")
	dryRun := fs.Bool("dry-run", false, "show changes without writing")
	refreshInterval := fs.Int("refresh-interval", 0, "seconds between forced statusline re-renders while idle (min 1); omit to leave unset")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return 2
	}
	refreshIntervalSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "refresh-interval" {
			refreshIntervalSet = true
		}
	})
	if refreshIntervalSet && *refreshInterval < 1 {
		fmt.Fprintln(stderr, "statusloom: --refresh-interval must be >= 1")
		return 2
	}
	desired := desiredStatusLine(*refreshInterval)
	desiredSubagent := desiredSubagentStatusLine(*refreshInterval)
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
	statusLineChanged := !hadStatusLine || !reflect.DeepEqual(before, desired)
	beforeSubagent, hadSubagentStatusLine := doc["subagentStatusLine"]
	subagentChanged := !hadSubagentStatusLine || !reflect.DeepEqual(beforeSubagent, desiredSubagent)

	if !statusLineChanged && !subagentChanged {
		fmt.Fprintln(stdout, "already configured")
		return 0
	}
	if hadStatusLine && statusLineChanged {
		printStatusLineDiff(stdout, before, desired)
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
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func printStatusLineDiff(w io.Writer, before, desired any) {
	oldJSON, _ := json.Marshal(before)
	newJSON, _ := json.Marshal(desired)
	fmt.Fprintf(w, "- %s\n+ %s\n", oldJSON, newJSON)
}
