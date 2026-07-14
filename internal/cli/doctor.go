package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
)

func runDoctor(args []string, stdout, stderr io.Writer, version string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	settingsFlag := fs.String("settings", "", "Claude Code settings file")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	failed := false
	report := func(status, name, detail string) {
		fmt.Fprintf(stdout, "%s %s - %s\n", status, name, detail)
		if status == "FAIL" {
			failed = true
		}
	}
	report("PASS", "binary", version)
	doctorDocument(report)
	doctorLegacyConfig(report)
	doctorCache(report)
	doctorGit(report)
	settings, err := claudeSettingsPath(*settingsFlag)
	if err != nil {
		report("WARN", "claude-code", "cannot locate settings; run statusloom setup claude-code")
	} else {
		doctorClaudeCode(report, settings)
		doctorRefreshInterval(report, settings)
	}
	if failed {
		return 1
	}
	return 0
}

// doctorDocument checks the DSL status-line document (<tool>.xml), which is
// the source of truth the render path now uses. Absent: the built-in default
// document is used (PASS). Present: it is parsed and validated, and any
// diagnostics are surfaced human-readably (errors -> FAIL, warnings -> WARN).
func doctorDocument(report func(string, string, string)) {
	const tool = "claude-code"
	path := config.DocumentPath(tool)
	if !config.DocumentExists(tool) {
		report("PASS", "document", fmt.Sprintf("using built-in defaults (%s)", path))
		return
	}
	_, diags, err := config.LoadDocument(tool)
	if err != nil {
		report("FAIL", "document", err.Error())
		return
	}
	var errs, warns []string
	for _, d := range diags {
		if d.Severity == dsl.SeverityError {
			errs = append(errs, d.Message)
		} else {
			warns = append(warns, d.Message)
		}
	}
	switch {
	case len(errs) > 0:
		report("FAIL", "document", fmt.Sprintf("%s: %s", path, strings.Join(errs, "; ")))
	case len(warns) > 0:
		report("WARN", "document", fmt.Sprintf("%s: %s", path, strings.Join(warns, "; ")))
	default:
		report("PASS", "document", path)
	}
}

// doctorLegacyConfig notices a leftover config.json. It is no longer used for
// rendering; the render path auto-migrates it to <tool>.xml on first use.
// A config.json that fails to parse is left to doctorConfig to report.
func doctorLegacyConfig(report func(string, string, string)) {
	const tool = "claude-code"
	_, ok, err := config.LoadLegacyConfig()
	if err != nil || !ok {
		return
	}
	if config.DocumentExists(tool) {
		report("WARN", "migration", "legacy config.json is still present but no longer used for rendering; it can be removed")
	} else {
		report("WARN", "migration", fmt.Sprintf("legacy config.json found; it will be auto-migrated to %s on the next render", config.DocumentPath(tool)))
	}
}

func doctorCache(report func(string, string, string)) {
	dir, err := cache.Dir()
	if err != nil {
		report("FAIL", "cache", err.Error())
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		report("FAIL", "cache", err.Error())
		return
	}
	probe := filepath.Join(dir, fmt.Sprintf(".doctor-probe-%d", os.Getpid()))
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		report("FAIL", "cache", err.Error())
		return
	}
	if err := os.Remove(probe); err != nil {
		report("FAIL", "cache", err.Error())
		return
	}
	report("PASS", "cache", dir)
}

func doctorGit(report func(string, string, string)) {
	out, err := exec.Command("git", "version").CombinedOutput()
	if err != nil {
		report("WARN", "git", "git widgets will be hidden")
		return
	}
	report("PASS", "git", strings.TrimSpace(string(out)))
}

func doctorClaudeCode(report func(string, string, string), path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		report("WARN", "claude-code", "not configured; run statusloom setup claude-code")
		return
	}
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		report("WARN", "claude-code", "not configured; run statusloom setup claude-code")
		return
	}
	if disable, ok := doc["disableAllHooks"].(bool); ok && disable {
		report("WARN", "claude-code", "disableAllHooks is true; Claude Code disables the status line too — remove it or set false")
	}
	if statusLine, ok := doc["statusLine"].(map[string]any); ok {
		if command, ok := statusLine["command"].(string); ok && strings.Contains(command, "statusloom") {
			report("PASS", "claude-code", command)
			return
		}
	}
	report("WARN", "claude-code", "not configured; run statusloom setup claude-code")
}

// doctorRefreshInterval warns when the active claude-code layout has a
// countdown field (five-hour-reset / weekly-reset) but Claude Code's
// statusLine.refreshInterval is not set to configure a re-render timer —
// without it, those fields can visibly stall while idle (no events are
// firing to trigger a re-render). See statusloom setup claude-code
// --refresh-interval.
//
// It reads the saved <tool>.xml document (not config.json): the DSL document
// is what the render path uses. When no document file exists yet (built-in
// defaults, or a legacy config.json awaiting migration) the check stays
// silent, mirroring the previous "only warn once configured" behavior. It is
// also silent whenever the document or the Claude Code settings can't be
// read/parsed, since those problems are already surfaced by the document /
// claude-code checks.
func doctorRefreshInterval(report func(string, string, string), settingsPath string) {
	const tool = "claude-code"
	if !config.DocumentExists(tool) {
		return
	}
	doc, _, err := config.LoadDocument(tool)
	if err != nil || doc == nil || doc.Root == nil || !documentHasResetField(doc) {
		return
	}
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		return
	}
	var settings map[string]any
	if json.Unmarshal(b, &settings) != nil {
		return
	}
	if statusLine, ok := settings["statusLine"].(map[string]any); ok {
		if v, ok := statusLine["refreshInterval"]; ok {
			if n, ok := v.(float64); ok && n >= 1 {
				return
			}
		}
	}
	report("WARN", "refresh", "countdown widgets (five-hour-reset/weekly-reset) can stall while idle; run statusloom setup claude-code --refresh-interval 60")
}

// documentHasResetField reports whether the document's active layout (the one
// with active="true", or the first layout otherwise, mirroring the renderer)
// contains a five-hour-reset or weekly-reset field on any line, including
// inside spans.
func documentHasResetField(doc *dsl.Document) bool {
	root := doc.Root
	if root == nil || len(root.Layouts) == 0 {
		return false
	}
	layout := root.Layouts[0]
	for _, l := range root.Layouts {
		if l.Active != nil && *l.Active {
			layout = l
			break
		}
	}
	for _, line := range layout.Lines {
		if nodesHaveResetField(line.Children) {
			return true
		}
	}
	return false
}

func nodesHaveResetField(nodes []dsl.Node) bool {
	for _, n := range nodes {
		switch v := n.(type) {
		case *dsl.FieldNode:
			if v.Name == "five-hour-reset" || v.Name == "weekly-reset" {
				return true
			}
		case *dsl.SpanNode:
			if nodesHaveResetField(v.Children) {
				return true
			}
		}
	}
	return false
}
