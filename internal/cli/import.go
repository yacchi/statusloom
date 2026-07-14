package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/dsl"
)

var timeNow = time.Now

// importedLayoutName is the name given to the layout built from an
// imported ccstatusline document. When appending (the default mode), a
// numeric suffix is added if a layout with this name already exists.
const importedLayoutName = "ccstatusline"

type ccSettings struct {
	Version                int          `json:"version"`
	Lines                  [][]ccWidget `json:"lines"`
	FlexMode               string       `json:"flexMode"`
	CompactThreshold       int          `json:"compactThreshold"`
	ColorLevel             int          `json:"colorLevel"`
	InheritSeparatorColors bool         `json:"inheritSeparatorColors"`
	GlobalBold             bool         `json:"globalBold"`
	MinimalistMode         bool         `json:"minimalistMode"`
	Powerline              struct {
		Enabled bool `json:"enabled"`
	} `json:"powerline"`
}

type ccWidget struct {
	Type     string            `json:"type"`
	Text     string            `json:"text"`
	Color    string            `json:"color"`
	Bold     bool              `json:"bold"`
	RawValue bool              `json:"rawValue"`
	Metadata map[string]string `json:"metadata"`
}

var ccWidgetTypes = map[string]string{
	"model": "model", "separator": "separator", "thinking-effort": "thinking-effort",
	"context-length": "context-length", "context-percentage": "context-percentage",
	"context-percentage-usable": "context-percentage-usable", "session-cost": "session-cost",
	"git-branch": "git-branch", "git-changes": "git-changes", "session-usage": "five-hour-usage",
	"reset-timer": "five-hour-reset", "weekly-usage": "weekly-usage",
	"weekly-reset-timer": "weekly-reset", "version": "tool-version",
	"current-directory": "current-directory", "flex-separator": "flex-separator",
}

func runImport(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "ccstatusline" {
		fmt.Fprintln(stderr, "statusloom: usage: statusloom import ccstatusline [path] [--dry-run] [--replace]")
		return 2
	}
	fs := flag.NewFlagSet("import ccstatusline", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "print the imported config without writing")
	replace := fs.Bool("replace", false, "replace claude-code's layouts with the imported one instead of adding it")
	// The documented syntax places the optional path before flags. Move these
	// boolean flags forward so both conventional flag orders are accepted.
	parseArgs := make([]string, 0, len(args)-1)
	for _, arg := range args[1:] {
		if arg == "--dry-run" || arg == "--replace" {
			parseArgs = append([]string{arg}, parseArgs...)
		} else {
			parseArgs = append(parseArgs, arg)
		}
	}
	if err := fs.Parse(parseArgs); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "statusloom: import ccstatusline accepts at most one path")
		return 2
	}
	var input string
	var err error
	if fs.NArg() == 1 {
		input = fs.Arg(0)
	} else {
		input, err = ccstatuslinePath()
	}
	if err != nil {
		fmt.Fprintf(stderr, "statusloom: %v\n", err)
		return 1
	}
	b, err := os.ReadFile(input)
	if err != nil {
		fmt.Fprintf(stderr, "statusloom: read %s: %v\n", input, err)
		return 1
	}
	var source ccSettings
	if err := json.Unmarshal(b, &source); err != nil {
		fmt.Fprintf(stderr, "statusloom: invalid ccstatusline JSON: %v\n", err)
		return 1
	}

	lines, widgetCount, unsupportedWidgets, unsupportedSettings := importCCSettings(source)

	const tool = "claude-code"

	// The destination is the tool's DSL document: parse the existing
	// <tool>.xml when present, else start from the built-in default document.
	// A present-but-invalid document falls back to the default so an import
	// can still recover the file.
	var doc *dsl.Document
	if data, rerr := os.ReadFile(config.DocumentPath(tool)); rerr == nil {
		parsed, diags := dsl.Parse(string(data))
		if parsed == nil || parsed.Root == nil || dsl.HasErrors(diags) {
			fmt.Fprintf(stderr, "statusloom: existing %s is invalid; starting from the default document\n", config.DocumentPath(tool))
			parsed, _ = dsl.Parse(config.DefaultDocument(tool))
		}
		doc = parsed
	} else if errors.Is(rerr, os.ErrNotExist) {
		doc, _ = dsl.Parse(config.DefaultDocument(tool))
	} else {
		fmt.Fprintf(stderr, "statusloom: read %s: %v\n", config.DocumentPath(tool), rerr)
		return 1
	}

	// Build the imported layout and merge it in as the new active layout.
	layoutName := importedLayoutName
	if !*replace {
		layoutName = uniqueDocLayoutName(doc.Root.Layouts, importedLayoutName)
	}
	newLayout, layoutWarnings := config.WidgetLinesToLayout(layoutName, true, lines)
	if *replace {
		doc.Root.Layouts = []*dsl.LayoutNode{newLayout}
	} else {
		// Exactly one layout may be active: clear active on every existing
		// layout, then append the imported one (already active="true").
		for _, l := range doc.Root.Layouts {
			l.Active = nil
		}
		// The serializer orders sibling elements by source-range start; a
		// hand-built layout has a zero range and would otherwise sort before
		// the parsed layouts. Stamp it past the end of the source so it
		// serializes last, preserving the intended append order.
		end := len(doc.Source)
		newLayout.Meta.SourceRange = dsl.SourceRange{Start: end, End: end}
		doc.Root.Layouts = append(doc.Root.Layouts, newLayout)
	}

	// Serialize, then re-parse/validate so the on-disk document is checked.
	src := dsl.Serialize(doc)
	if reparsed, diags := dsl.Parse(src); reparsed == nil || dsl.HasErrors(append(diags, dsl.Validate(reparsed)...)) {
		fmt.Fprintln(stderr, "statusloom: internal error: imported document failed validation")
		for _, d := range append(diags, dsl.Validate(reparsed)...) {
			fmt.Fprintf(stderr, "statusloom: %s: %s\n", d.Severity, d.Message)
		}
		return 1
	}

	if *dryRun {
		io.WriteString(stdout, src)
		return 0
	}

	docPath := config.DocumentPath(tool)
	if _, err := os.Stat(docPath); err == nil {
		if _, err := backupFile(docPath); err != nil {
			fmt.Fprintf(stderr, "statusloom: backup document: %v\n", err)
			return 1
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "statusloom: inspect document: %v\n", err)
		return 1
	}
	if err := config.SaveDocumentSource(tool, src); err != nil {
		fmt.Fprintf(stderr, "statusloom: write document: %v\n", err)
		return 1
	}
	for _, w := range layoutWarnings {
		fmt.Fprintf(stderr, "statusloom: import: %s\n", w)
	}
	printImportSummary(stdout, layoutName, *replace, widgetCount, unsupportedWidgets, unsupportedSettings)
	return 0
}

// uniqueDocLayoutName returns base if no layout in existing is named base, or
// "base 2", "base 3", ... otherwise.
func uniqueDocLayoutName(existing []*dsl.LayoutNode, base string) string {
	used := make(map[string]bool, len(existing))
	for _, l := range existing {
		used[l.Name] = true
	}
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s %d", base, i)
		if !used[candidate] {
			return candidate
		}
	}
}

func ccstatuslinePath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ccstatusline", "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ccstatusline", "settings.json"), nil
}

// importCCSettings converts a parsed ccstatusline document into the
// widget lines for a new layout, plus reporting of what could not be
// carried over.
//
// ccstatusline's tool-level settings (colorLevel, compactThreshold, and
// the flags reported below) are never applied to the destination tool's
// configuration - only the widget lines become a new layout, and the
// existing tool-level settings are always preserved. Any tool-level
// setting the source document customized is reported in
// unsupportedSettings purely for the user's information.
func importCCSettings(source ccSettings) (lines [][]config.WidgetSpec, count int, unsupportedWidgets, unsupportedSettings []string) {
	// ccstatusline's tool-level flexMode maps onto each imported
	// flex-separator widget's "flex" field. ""/"full" match the
	// per-widget default and are not written; other values without a
	// per-widget equivalent ("off", unknown strings) are reported as
	// unsupported instead of producing an invalid config.
	mappedFlex := ""
	flexUnsupported := false
	switch {
	case source.FlexMode == "" || source.FlexMode == "full":
	case strings.HasPrefix(source.FlexMode, "full-minus-"):
		mappedFlex = source.FlexMode
	default:
		flexUnsupported = true
	}

	unsupportedSet := make(map[string]struct{})
	flexCount := 0
	for _, sourceLine := range source.Lines {
		var line []config.WidgetSpec
		for _, widget := range sourceLine {
			targetType, ok := ccWidgetTypes[widget.Type]
			if !ok {
				unsupportedSet[widget.Type] = struct{}{}
				continue
			}
			// ccstatusline widget metadata (e.g. {"display":"time"}) has no
			// representation in the DSL, so it is intentionally dropped rather
			// than carried in spec.Metadata (which would then be reported as a
			// dropped-metadata warning on every import).
			spec := config.WidgetSpec{Type: targetType, Text: widget.Text, Color: widget.Color, Bold: widget.Bold, RawValue: widget.RawValue}
			// The usage widgets render bare values ("27%"); ccstatusline
			// baked the labels in, so give imports the default templates
			// to keep their labels. The global flexMode becomes a
			// per-flex-separator flex value.
			switch targetType {
			case "five-hour-usage":
				spec.Template = "5h: {value}"
			case "weekly-usage":
				spec.Template = "7d: {value}"
			case "flex-separator":
				spec.Flex = mappedFlex
				flexCount++
			}
			line = append(line, spec)
			count++
		}
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}
	unsupportedWidgets = make([]string, 0, len(unsupportedSet))
	for name := range unsupportedSet {
		unsupportedWidgets = append(unsupportedWidgets, name)
	}
	sort.Strings(unsupportedWidgets)
	settings := make([]string, 0, 6)
	if source.Version != 3 {
		settings = append(settings, fmt.Sprintf("version %d (expected 3)", source.Version))
	}
	if flexUnsupported {
		settings = append(settings, fmt.Sprintf("flexMode %q (no per-widget equivalent)", source.FlexMode))
	} else if mappedFlex != "" && flexCount == 0 {
		settings = append(settings, fmt.Sprintf("flexMode %q (no flex-separator widgets; no effect)", source.FlexMode))
	}
	if source.Powerline.Enabled {
		settings = append(settings, "powerline.enabled")
	}
	if source.InheritSeparatorColors {
		settings = append(settings, "inheritSeparatorColors")
	}
	if source.GlobalBold {
		settings = append(settings, "globalBold")
	}
	if source.MinimalistMode {
		settings = append(settings, "minimalistMode")
	}
	// colorLevel and compactThreshold are tool-level settings. Imports only
	// ever add a layout of widget lines - the destination tool's existing
	// tool-level settings are always kept, so report what was not applied
	// instead of overwriting them.
	levels := []string{"none", "ansi16", "ansi256", "truecolor"}
	if source.ColorLevel < 0 || source.ColorLevel >= len(levels) {
		settings = append(settings, fmt.Sprintf("colorLevel %d (invalid; not applied)", source.ColorLevel))
	} else {
		settings = append(settings, fmt.Sprintf("colorLevel %q (not applied; existing tool colorLevel preserved)", levels[source.ColorLevel]))
	}
	if source.CompactThreshold != 0 {
		settings = append(settings, fmt.Sprintf("compactThreshold %d (not applied; existing tool compactThreshold preserved)", source.CompactThreshold))
	}
	return lines, count, unsupportedWidgets, settings
}

func printImportSummary(w io.Writer, layoutName string, replaced bool, count int, widgets, settings []string) {
	if replaced {
		fmt.Fprintf(w, "Imported %d widgets. Replaced claude-code's layouts with %q and made it active.\n", count, layoutName)
	} else {
		fmt.Fprintf(w, "Imported %d widgets. Added layout %q to claude-code and made it active.\n", count, layoutName)
	}
	if len(widgets) > 0 {
		fmt.Fprintln(w, "Unsupported widgets:")
		for _, item := range widgets {
			fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(settings) > 0 {
		fmt.Fprintln(w, "Unsupported settings:")
		for _, item := range settings {
			fmt.Fprintf(w, "- %s\n", item)
		}
	}
}
