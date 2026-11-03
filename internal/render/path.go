package render

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/yacchi/statusloom/internal/dsl"
	"github.com/yacchi/statusloom/internal/schema"
)

// pathFieldValue returns the absolute path a directory field names (the
// FieldDef.PathStyles fields). ok is false when the field is not a path field
// or its value is unavailable (no git repository for git-root).
func pathFieldValue(name string, snap schema.StatusSnapshot) (string, bool) {
	switch name {
	case "current-directory":
		return snap.System.Cwd, true
	case "project-directory":
		return snap.System.ProjectDir, true
	case "git-root":
		if snap.Repository == nil {
			return "", false
		}
		return snap.Repository.Root, true
	}
	return "", false
}

// applyPathStyle renders an absolute path in the given path style (the
// `path` attribute, markup.md "path"). An empty path stays empty, so a path
// style never makes a hidden field visible.
func applyPathStyle(abs, style, home string) string {
	if abs == "" {
		return ""
	}
	switch style {
	case dsl.PathBase:
		return filepath.Base(abs)
	case dsl.PathHome:
		return homeRelativePath(abs, home)
	default:
		return abs
	}
}

// homeRelativePath replaces a leading home directory with "~". The match must
// end on a path separator (or be the whole path), so /Users/fujieX is not
// under /Users/fujie. A path outside home, or an unknown home, stays absolute.
func homeRelativePath(abs, home string) string {
	h := strings.TrimRight(home, `/\`)
	if h == "" {
		return abs
	}
	if abs == h {
		return "~"
	}
	if strings.HasPrefix(abs, h+"/") || strings.HasPrefix(abs, h+`\`) {
		return "~" + abs[len(h):]
	}
	return abs
}

// homeDir resolves the user's home directory through the renderer's env
// injection (Options.Env), so tests can fake HOME. Only when no injection is
// given does it fall back to the process's home directory.
func homeDir(opts Options) string {
	if home := opts.env("HOME"); home != "" {
		return home
	}
	if opts.Env != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
