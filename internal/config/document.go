package config

// This file implements the DSL-document side of the configuration layer
// (markup.md "設定ファイルの配置"): loading, saving, and defaulting the
// <tool>.xml documents that are the sole configuration source.
//
// Dependency direction: config -> dsl only (dsl imports no statusloom
// package, so there is no cycle). config does NOT depend on render.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/yacchi/statusloom/internal/dsl"
)

// configDir resolves the directory that holds statusloom's <tool>.xml
// documents. It reuses Path()'s STATUSLOOM_CONFIG / XDG / home resolution:
//
//   - When STATUSLOOM_CONFIG (or the resolved path) names an existing
//     directory, that directory is used verbatim. This lets verification
//     harnesses set STATUSLOOM_CONFIG=$(mktemp -d) and keep every statusloom
//     file inside the isolated directory.
//   - Otherwise the parent directory of the resolved config-file path is
//     used, matching the "STATUSLOOM_CONFIG is a config-file path"
//     convention (Path() and all existing tests).
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

// defaultDocuments holds the built-in DSL source for each tool. The
// claude-code document uses the same fields in the same order, the same
// colors, " | " separators (as role="separator" text that compacts to "|"),
// and the "5h:"/"7d:" usage labels expressed as optional spans so an empty
// usage value hides the label.
var defaultDocuments = map[string]string{
	"claude-code":          claudeCodeDefaultDocument,
	"claude-code-subagent": claudeCodeSubagentDefaultDocument,
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

// claudeCodeSubagentDefaultDocument is the built-in DSL source rendered per
// task by `statusloom claude-subagent` (Claude Code's subagentStatusLine
// feature, one row per agent-panel task). A subagent row is
// information-poor, so the default leads with the task description and
// model (always shown), then right-aligns (via <flex/>) progressively more
// usage stats as the terminal widens, using the tool-agnostic "width"
// metric as breakpoints:
//
//	width >= 48: + task-duration
//	width >= 64: + task-tokens        (↓, compact)
//	width >= 80: + task-context-percent
//
// A subagent row can only occupy a single line, so these width breakpoints
// keep the essentials visible on a narrow agent panel and reveal detail
// only when there is room. task-tokens and task-context-percent also carry
// optional="..." so they stay hidden before the model resolves / while the
// context size is unknown, rather than rendering a bare "0"/"0%". Because an
// unknown width resolves the "width" metric to an unbounded value, a
// width-unaware host (Options.Width == 0) shows every stat.
//
// Each row renders independently (there is no shared column state across
// rows/tasks), so the only way to keep the right-hand stats lined up down
// the agent panel is to right-align each one to a fixed min-width
// (markup.md "min-width"/"align"): task-duration to 7 columns (covers up to
// "12h 34m"), task-tokens to 6 (covers up to "199.9k"), and
// task-context-percent to 4 (covers up to "100%"). Padding lands on the
// formatted value itself, outside each field's prefix, so " · ↓ "/" ("/")"
// stay put and only the digits shift.
const claudeCodeSubagentDefaultDocument = `<statusloom version="1" tool="claude-code-subagent" color-level="ansi16" compact-threshold="60">
  <layout name="Default" active="true">
    <line>
      <field name="task-description"/>
      <field name="task-model" prefix="  "/>
      <flex/>
      <field name="task-duration" format="duration" when="width ge 48" min-width="7" align="right"/>
      <field name="task-tokens" prefix=" · ↓ " format="compact-number" optional="task-tokens" when="width ge 64" min-width="6" align="right"/>
      <field name="task-context-percent" prefix=" (" suffix=")" format="percent" precision="0" optional="task-context-percent" when="width ge 80" min-width="4" align="right"/>
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
