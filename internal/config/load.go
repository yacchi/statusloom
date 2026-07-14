package config

import (
	"encoding/json"
	"fmt"
)

// parse decodes and defaults a legacy config.json document already read into
// memory. It is used by the migration path (LoadLegacyConfig) to read the
// legacy config.json that `statusloom` auto-migrates into a <tool>.xml DSL
// document; the DSL document is the live configuration source.
func parse(data []byte) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: invalid JSON: %w", err)
	}

	if cfg.SchemaVersion != 1 {
		// TODO(schema): when a schemaVersion 2 is introduced, this is the
		// hook point for a migration step (upgrade cfg in place, or parse
		// with a versioned struct and convert) instead of rejecting the
		// document outright.
		return nil, fmt.Errorf("config: unsupported schemaVersion %d (want 1)", cfg.SchemaVersion)
	}

	// GitConfig's IncludeUntracked/CollectNumstat default to true, but a
	// decoded `false` can't be told apart from "absent" once cfg has been
	// unmarshaled (see ApplyDefaults for the full explanation). Pre-scan
	// the raw document here, before that information is lost, to decide
	// whether shared.git was present at all.
	gitPresent, err := gitObjectPresent(data)
	if err != nil {
		return nil, fmt.Errorf("config: invalid JSON: %w", err)
	}
	if !gitPresent {
		cfg.Shared.Git.IncludeUntracked = true
		cfg.Shared.Git.CollectNumstat = true
	}

	ApplyDefaults(&cfg)
	return &cfg, nil
}

// UnmarshalJSON decodes a ToolConfig while tolerating the older on-disk
// shape. Pre-release configs stored a single top-level "lines" array with
// no "layouts"; such a document is parsed leniently by wrapping those
// lines in one implicit "Default" layout. This is not a schema migration
// (the version is not bumped) — it only keeps existing local configs from
// hard-erroring. When "layouts" is present it wins and any stray "lines"
// is ignored.
func (tc *ToolConfig) UnmarshalJSON(data []byte) error {
	// alias avoids recursing into this method; the extra Lines field
	// captures the legacy top-level "lines" key that ToolConfig no longer
	// has.
	type alias ToolConfig
	var raw struct {
		alias
		Lines [][]WidgetSpec `json:"lines"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*tc = ToolConfig(raw.alias)
	if len(tc.Layouts) == 0 && len(raw.Lines) > 0 {
		tc.Layouts = []Layout{{Name: "Default", Lines: raw.Lines}}
	}
	return nil
}

// gitObjectPresent reports whether the document has a shared.git object at
// all (regardless of its contents), by re-decoding just that path as a raw
// message.
func gitObjectPresent(data []byte) (bool, error) {
	var doc struct {
		Shared struct {
			Git json.RawMessage `json:"git"`
		} `json:"shared"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false, err
	}
	return len(doc.Shared.Git) > 0, nil
}
