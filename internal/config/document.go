package config

// This file implements the DSL-document side of the configuration layer
// (markup.md "設定ファイルの配置"): loading/saving <tool>.xml documents and
// migrating the legacy config.json into a dsl.Document. It bridges the
// legacy Config/WidgetSpec schema (types.go) and the new DSL AST
// (internal/dsl) while the two coexist during Phase 2.
//
// Dependency direction: config -> dsl only (dsl imports no statusloom
// package, so there is no cycle). config does NOT depend on render.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/yacchi/statusloom/internal/dsl"
)

// configDir resolves the directory that holds statusloom's configuration
// files (the legacy config.json and the new <tool>.xml documents). It reuses
// Path()'s STATUSLOOM_CONFIG / XDG / home resolution:
//
//   - When STATUSLOOM_CONFIG (or the resolved path) names an existing
//     directory, that directory is used verbatim. This lets verification
//     harnesses set STATUSLOOM_CONFIG=$(mktemp -d) and keep every statusloom
//     file inside the isolated directory.
//   - Otherwise the parent directory of the resolved config-file path is
//     used, matching the legacy "STATUSLOOM_CONFIG is a config.json file
//     path" convention (Path() and all existing tests).
func configDir() (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		return p, nil
	}
	return filepath.Dir(p), nil
}

// legacyConfigPath returns the config.json path inside the config directory.
// It is where MigrateFromLegacy reads the document to migrate.
func legacyConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// DocumentPath returns the DSL document location for a tool:
// <configDir>/<tool>.xml. The file need not exist. On an unresolvable config
// directory (e.g. no home directory) it degrades to a bare "<tool>.xml"
// relative path rather than returning an error, keeping the signature simple
// for the common case.
func DocumentPath(tool string) string {
	dir, err := configDir()
	if err != nil {
		return tool + ".xml"
	}
	return filepath.Join(dir, tool+".xml")
}

// DocumentExists reports whether the <tool>.xml document is present on disk.
func DocumentExists(tool string) bool {
	_, err := os.Stat(DocumentPath(tool))
	return err == nil
}

// LoadDocument reads, parses, and validates the <tool>.xml document. A
// missing file is not an error: it falls back to DefaultDocument(tool), which
// is parsed and validated the same way (callers can distinguish absence with
// DocumentExists). The returned diagnostics are Parse's structural findings
// plus Validate's semantic findings. A read error other than "not found" is
// returned as err (with a nil document).
func LoadDocument(tool string) (*dsl.Document, []dsl.Diagnostic, error) {
	path := DocumentPath(tool)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			doc, diags := parseAndValidate(DefaultDocument(tool))
			return doc, diags, nil
		}
		return nil, nil, err
	}
	doc, diags := parseAndValidate(string(data))
	return doc, diags, nil
}

// parseAndValidate parses src and, when a root was produced, appends the
// semantic validation diagnostics.
func parseAndValidate(src string) (*dsl.Document, []dsl.Diagnostic) {
	doc, diags := dsl.Parse(src)
	if doc != nil && doc.Root != nil {
		diags = append(diags, dsl.Validate(doc)...)
	}
	return doc, diags
}

var xmlTempSequence uint64

// SaveDocumentSource atomically writes src to <tool>.xml (sibling temp file
// + fsync + rename), mirroring the config.json write discipline so concurrent
// readers never observe a partial file.
func SaveDocumentSource(tool, src string) error {
	return writeFileAtomic(DocumentPath(tool), []byte(src))
}

// WriteFileAtomic atomically writes data to an arbitrary path using the same
// sibling-temp-file + fsync + rename discipline as the document/draft writers,
// so concurrent readers never observe a partial file. Exposed for `statusloom
// fmt`, which formats a document file in place.
func WriteFileAtomic(path string, data []byte) error {
	return writeFileAtomic(path, data)
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), atomic.AddUint64(&xmlTempSequence, 1))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadLegacyConfig loads the legacy config.json for migration. ok is false
// (with a nil error) when the file is simply absent; a decode/schema error is
// returned as err.
func LoadLegacyConfig() (cfg *Config, ok bool, err error) {
	p, perr := legacyConfigPath()
	if perr != nil {
		return nil, false, perr
	}
	data, rerr := os.ReadFile(p)
	if rerr != nil {
		if errors.Is(rerr, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, rerr
	}
	parsed, perr := parse(data)
	if perr != nil {
		return nil, false, perr
	}
	return parsed, true, nil
}

// defaultDocuments holds the built-in DSL source for each tool. The
// claude-code document is authored to render visually identically to the
// legacy Default() preset (defaults.go): the same fields in the same order,
// the same colors, " | " separators (as role="separator" text that compacts
// to "|"), and the "5h:"/"7d:" usage labels expressed as optional spans so an
// empty usage value hides the label exactly like the legacy template did.
var defaultDocuments = map[string]string{
	"claude-code": claudeCodeDefaultDocument,
}

const claudeCodeDefaultDocument = `<statusloom version="1" tool="claude-code" color-level="ansi16" compact-threshold="60" context-percentage-mode="usable">
  <layout name="Default" active="true">
    <line>
      <field name="model" color="cyan"/>
      <text role="separator" padding="1">|</text>
      <field name="thinking-effort"/>
      <text role="separator" padding="1">|</text>
      <field name="context-length"/>
      <text role="separator" padding="1">|</text>
      <field name="context-percentage-usable"/>
      <text role="separator" padding="1">|</text>
      <field name="session-cost"/>
      <text role="separator" padding="1">|</text>
      <field name="git-branch" color="magenta"/>
      <text role="separator" padding="1">|</text>
      <field name="git-changes" color="yellow"/>
    </line>
    <line>
      <span optional="five-hour-usage" prefix="5h: "><field name="five-hour-usage"/></span>
      <text role="separator" padding="1">|</text>
      <field name="five-hour-reset"/>
      <text role="separator" padding="1">|</text>
      <span optional="weekly-usage" prefix="7d: "><field name="weekly-usage"/></span>
      <text role="separator" padding="1">|</text>
      <field name="weekly-reset"/>
      <text role="separator" padding="1">|</text>
      <field name="tool-version"/>
    </line>
  </layout>
</statusloom>
`

// DefaultDocument returns the built-in DSL source for tool. Unknown tools
// yield "".
func DefaultDocument(tool string) string {
	return defaultDocuments[tool]
}

// DocumentGitConfig derives a GitConfig from a document's optional <git/>
// element, applying the built-in defaults (3000ms / 200ms / include-untracked
// / collect-numstat) for a missing element or missing attributes.
func DocumentGitConfig(doc *dsl.Document) GitConfig {
	gc := GitConfig{CacheTTLMs: 3000, TimeoutMs: 200, IncludeUntracked: true, CollectNumstat: true}
	if doc == nil || doc.Root == nil || doc.Root.Git == nil {
		return gc
	}
	g := doc.Root.Git
	if g.CacheTTLMS != nil {
		gc.CacheTTLMs = *g.CacheTTLMS
	}
	if g.TimeoutMS != nil {
		gc.TimeoutMs = *g.TimeoutMS
	}
	if g.IncludeUntracked != nil {
		gc.IncludeUntracked = *g.IncludeUntracked
	}
	if g.CollectNumstat != nil {
		gc.CollectNumstat = *g.CollectNumstat
	}
	return gc
}

// --- migration -----------------------------------------------------------

// migrateOps maps legacy Condition/ColorRule operators (Condition.Op /
// ColorRule.Op, which use the lt/lte/gt/gte/eq/neq spelling) to the DSL
// when-expression word operators (lt/le/gt/ge/eq/ne).
var migrateOps = map[string]string{
	"lt":  "lt",
	"lte": "le",
	"gt":  "gt",
	"gte": "ge",
	"eq":  "eq",
	"neq": "ne",
}

// MigrateFromLegacy converts one tool's slice of the legacy config.json into
// a dsl.Document. The second return value lists items that could not be
// carried over faithfully (unconvertible templates, unknown operators,
// dropped metadata) plus any validation findings on the produced document.
//
// The returned document is the result of serializing the built AST and
// re-parsing it, so it carries a real Source and node ranges and is exactly
// what SaveDocumentSource would persist.
func MigrateFromLegacy(cfg Config, tool string) (*dsl.Document, []string) {
	var warnings []string

	root := &dsl.StatusloomNode{Version: "1", Tool: tool}

	toolCfg, ok := cfg.Tools[tool]
	if !ok {
		warnings = append(warnings, fmt.Sprintf("tool %q not present in config.json; migrating shared/tool defaults only", tool))
	}

	// Tool-level settings -> root attributes.
	if toolCfg.ColorLevel != "" {
		root.Settings.ColorLevel = toolCfg.ColorLevel
	}
	// compact-threshold is always emitted (including 0 = disabled): omitting
	// it would let the DSL default of 60 take over, silently re-enabling
	// compaction for a config that had it off.
	ct := toolCfg.CompactThreshold
	root.Settings.CompactThreshold = &ct
	if toolCfg.Context.PercentageMode != "" {
		root.Settings.ContextPercentageMode = toolCfg.Context.PercentageMode
	}
	if toolCfg.Context.ReserveTokens != 0 {
		rt := toolCfg.Context.ReserveTokens
		root.Settings.ContextReserveTokens = &rt
	}

	// Shared git settings -> <git/> element (all four attributes emitted).
	g := cfg.Shared.Git
	root.Git = &dsl.GitSettings{
		CacheTTLMS:       intPtr(g.CacheTTLMs),
		TimeoutMS:        intPtr(g.TimeoutMs),
		IncludeUntracked: boolPtr(g.IncludeUntracked),
		CollectNumstat:   boolPtr(g.CollectNumstat),
	}

	// Layouts -> <layout> elements; ActiveLayout selects active="true".
	active := toolCfg.ActiveLayout
	if active < 0 || active >= len(toolCfg.Layouts) {
		active = 0
	}
	for i, lay := range toolCfg.Layouts {
		name := lay.Name
		if name == "" {
			name = fmt.Sprintf("Layout %d", i+1)
		}
		ln, w := WidgetLinesToLayout(name, i == active, lay.Lines)
		warnings = append(warnings, w...)
		root.Layouts = append(root.Layouts, ln)
	}
	if len(root.Layouts) == 0 {
		warnings = append(warnings, "config has no layouts to migrate")
	}

	// Serialize + re-parse + validate so the returned document is round-trip
	// consistent with what will be written to disk.
	tmp := &dsl.Document{Root: root}
	src := dsl.Serialize(tmp)
	doc, diags := dsl.Parse(src)
	if doc == nil {
		for _, d := range diags {
			warnings = append(warnings, fmt.Sprintf("%s: %s", d.Severity, d.Message))
		}
		return tmp, warnings
	}
	diags = append(diags, dsl.Validate(doc)...)
	for _, d := range diags {
		warnings = append(warnings, fmt.Sprintf("%s: %s", d.Severity, d.Message))
	}
	return doc, warnings
}

// WidgetLinesToLayout converts legacy widget lines into a dsl.LayoutNode.
// Shared by MigrateFromLegacy and `statusloom import ccstatusline`. active
// sets the layout's active="true" attribute.
func WidgetLinesToLayout(name string, active bool, lines [][]WidgetSpec) (*dsl.LayoutNode, []string) {
	var warnings []string
	layout := &dsl.LayoutNode{Name: name}
	if active {
		t := true
		layout.Active = &t
	}
	for _, line := range lines {
		lineNode := &dsl.LineNode{}
		for _, spec := range line {
			node, w := widgetSpecToNode(spec)
			warnings = append(warnings, w...)
			if node != nil {
				lineNode.Children = append(lineNode.Children, node)
			}
		}
		layout.Lines = append(layout.Lines, lineNode)
	}
	return layout, warnings
}

// widgetSpecToNode converts a single legacy WidgetSpec into a DSL node:
// "separator" -> role="separator" <text>, "flex-separator" -> <flex/>, and
// every content type -> <field>. It returns any information that could not be
// carried over (unconvertible template, unknown operator, dropped metadata).
func widgetSpecToNode(spec WidgetSpec) (dsl.Node, []string) {
	var warnings []string

	style := dsl.Style{Color: migrateColor(spec.Color)}
	if spec.Bold {
		style.Bold = boolPtr(true)
	}

	switch spec.Type {
	case "separator":
		// An empty-Text separator is the legacy default " | " (compacting to
		// "|"); reproduce it as the canonical padded "|" so compaction still
		// works. A non-empty Text is embedded verbatim, preserving its exact
		// surrounding spaces.
		if spec.Text == "" {
			return &dsl.TextNode{
				Role:  "separator",
				Value: "|",
				Common: dsl.CommonAttributes{
					Style: style,
					Box:   dsl.Box{PaddingLeft: intPtr(1), PaddingRight: intPtr(1)},
				},
			}, warnings
		}
		return &dsl.TextNode{
			Role:   "separator",
			Value:  spec.Text,
			Common: dsl.CommonAttributes{Style: style},
		}, warnings

	case "flex-separator":
		// Flex "" means full; carry it through unchanged (validation accepts
		// "" / "full" / "full-minus-N").
		return &dsl.FlexNode{Size: spec.Flex}, warnings

	default:
		field := &dsl.FieldNode{Name: spec.Type, Raw: spec.RawValue, Hyperlink: spec.Hyperlink}
		common := dsl.CommonAttributes{Style: style}

		if spec.Template != "" {
			if before, after, found := strings.Cut(spec.Template, "{value}"); found {
				common.Prefix = before
				common.Suffix = after
				// The legacy template rendered nothing at all when the widget
				// was hidden (empty value included); a DSL prefix/suffix,
				// however, always renders once the field is visible. Gate the
				// field on its own presence so an empty value hides the
				// prefix/suffix too, matching the legacy semantics. (When a
				// showWhen -> when condition is also present the two AND,
				// which likewise matches: legacy emitted no template output
				// when either the condition failed or the value was empty.)
				common.Optional = spec.Type
			} else {
				warnings = append(warnings, fmt.Sprintf("field %q: template %q has no {value} placeholder; dropped", spec.Type, spec.Template))
			}
		}

		if spec.ShowWhen != nil {
			if when, ok := migrateCondition(*spec.ShowWhen); ok {
				common.When = when
			} else {
				warnings = append(warnings, fmt.Sprintf("field %q: showWhen has unknown operator %q; dropped", spec.Type, spec.ShowWhen.Op))
			}
		}

		for _, cr := range spec.ColorRules {
			op, ok := migrateOps[cr.Op]
			if !ok {
				warnings = append(warnings, fmt.Sprintf("field %q: colorRule has unknown operator %q; dropped", spec.Type, cr.Op))
				continue
			}
			common.ColorRules = append(common.ColorRules, dsl.ColorRule{
				When:  fmt.Sprintf("self %s %s", op, formatMigrateNumber(cr.Value)),
				Color: migrateColor(cr.Color),
			})
		}

		if len(spec.Metadata) > 0 {
			warnings = append(warnings, fmt.Sprintf("field %q: metadata dropped (%s)", spec.Type, strings.Join(sortedKeys(spec.Metadata), ", ")))
		}

		field.Common = common
		return field, warnings
	}
}

// migrateCondition builds a DSL when expression ("<source> <op> <value>")
// from a legacy Condition. ok is false when the operator is unknown.
func migrateCondition(c Condition) (string, bool) {
	op, ok := migrateOps[c.Op]
	if !ok {
		return "", false
	}
	source := c.Source
	if source == "" || source == "self" {
		source = "self"
	}
	return fmt.Sprintf("%s %s %s", source, op, formatMigrateNumber(c.Value)), true
}

// migrateColor converts a legacy color name to the DSL's kebab-case form
// (e.g. "brightBlack" -> "bright-black"). Hex ("#rrggbb") and "ansi256:N"
// values pass through unchanged, as do already-lowercase/kebab names.
func migrateColor(c string) string {
	if c == "" || strings.HasPrefix(c, "#") || strings.HasPrefix(c, "ansi256:") {
		return c
	}
	var b strings.Builder
	for i, r := range c {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// formatMigrateNumber renders a float64 metric threshold as the shortest
// exact decimal (e.g. 80 -> "80", 0.5 -> "0.5").
func formatMigrateNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }
