// Package statusloom exposes module-level embedded assets shared across the
// binary. Its only member is the canonical markup DSL spec (markup.md), which
// the monitor workspace ships as a local file so the editing LLM can read the
// full spec inside its isolated, throwaway directory (see
// internal/webconfig/live.go provisionMonitorDir).
//
// markup.md stays the single source of truth at the module root; embedding it
// here (rather than copying it into internal/webconfig) avoids a build-time
// copy step and keeps the file uncommitted-artifact-free.
package statusloom

import _ "embed"

// MarkupSpec is the full contents of markup.md, the authoritative DSL spec.
//
//go:embed markup.md
var MarkupSpec string
