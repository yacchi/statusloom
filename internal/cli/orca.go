package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// orcaStatusLineHookTimeout bounds the best-effort delegation to a known
// third-party tool's own statusline hook (e.g. Orca's claude-statusline.sh).
// Orca's script itself caps its outbound request at 1.5s (see its own
// --max-time), so this leaves it room to complete normally while still
// guaranteeing statusloom's own process exits promptly if a hook hangs.
const orcaStatusLineHookTimeout = 2 * time.Second

// knownStatusLineHook describes a third-party tool that may have installed
// its own statusLine command, which forwardToKnownStatusLineHooks can
// delegate to, and setup/doctor can recognize in an existing statusLine.
type knownStatusLineHook struct {
	name   string // human-readable, for messages, e.g. "Orca"
	path   func(getenv func(string) string) string
	marker string // substring identifying this tool's command
}

// knownStatusLineHooks is the small internal registry of third-party tools
// statusloom knows how to coexist with. It is intentionally not a plugin
// system - Orca is the only known instance today, and this slice is meant
// to stay tiny (see plans/... for the YAGNI rationale). Add an entry here
// (not a parallel code path) if another tool starts doing the same thing.
var knownStatusLineHooks = []knownStatusLineHook{
	{name: "Orca", path: orcaStatusLineHookPath, marker: orcaStatusLineHookMarker},
}

// runClaude implements `statusloom claude`. It renders exactly like
// runRenderPipeline, and additionally best-effort forwards the raw stdin
// payload to any known third-party tool's own statusline hook (see
// knownStatusLineHooks), if present. This lets statusloom be Claude Code's
// sole statusLine command on a machine where such a tool (e.g. Orca, a
// third-party coding-agent IDE) previously installed its own forwarder
// there: without this, configuring statusloom as statusLine silently drops
// that tool's own integration, and configuring the tool's script instead
// silently drops statusloom's own status line - only one command can
// occupy the slot.
func runClaude(stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	raw, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes))
	if err != nil {
		return fail(stderr, fmt.Errorf("reading stdin: %w", err))
	}

	lines, rerr := renderDocFromRaw(raw, getenv, "claude-code", stderr, false)
	code := writeRenderResult(stdout, stderr, lines, nil, rerr)

	forwardToKnownStatusLineHooks(raw, getenv)

	return code
}

// forwardToKnownStatusLineHooks best-effort hands raw to every hook in
// knownStatusLineHooks whose path exists and is executable. A failing or
// hanging hook never affects another: each gets its own timeout and error
// is ignored independently.
//
// statusloom deliberately does not reimplement any of these tools' own
// forwarding protocols (which pane it's running in, which local port and
// token to post to, its own dedup window). Those protocols are undocumented
// and each tool's to change; the scripts already gate all of that
// internally and exit immediately when not applicable, so handing them the
// same payload their own installer would have wired as statusLine is
// enough - with zero cost on any machine where the tool (or its hook)
// isn't present.
func forwardToKnownStatusLineHooks(raw []byte, getenv func(string) string) {
	for _, hook := range knownStatusLineHooks {
		script := hook.path(getenv)
		if script == "" || !isExecutableFile(script) {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), orcaStatusLineHookTimeout)
		cmd := exec.CommandContext(ctx, script)
		cmd.Stdin = bytes.NewReader(raw)
		_ = cmd.Run()
		cancel()
	}
}

// orcaStatusLineHookPath returns the path to Orca's own claude-statusline.sh
// hook, or "" if HOME cannot be determined.
func orcaStatusLineHookPath(getenv func(string) string) string {
	home := getenv("HOME")
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".orca", "agent-hooks", "claude-statusline.sh")
}

// isExecutableFile reports whether path exists, is not a directory, and has
// at least one executable bit set - i.e. whether it's safe to exec it as a
// forwarding hook.
func isExecutableFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// orcaStatusLineHookMarker is the substring Orca's own installer embeds in
// the statusLine.command it writes (observed as, e.g.,
// "${HOME-}/.orca/agent-hooks/claude-statusline.sh" inside a larger
// OS-dispatching shell case statement, or claude-statusline.cmd on
// Windows). Matching on this substring - rather than checking whether the
// hook file exists on disk - is what actually answers "is the statusLine
// setup is about to overwrite Orca's own", since the two can disagree: a
// machine can have the hook file present yet already have statusloom
// configured as statusLine (nothing to warn about), or have an
// Orca-authored statusLine command while the hook file itself is missing.
const orcaStatusLineHookMarker = ".orca/agent-hooks/claude-statusline"

// detectKnownStatusLineHook reports which known third-party tool (if any)
// the existing statusLine/subagentStatusLine value - as decoded from
// Claude Code settings JSON (map[string]any) - has a "command" string that
// references. It is (zero value, false) for anything else (a differently
// shaped value, a missing or non-string command, or a command that
// doesn't mention any known hook's marker).
func detectKnownStatusLineHook(value any) (knownStatusLineHook, bool) {
	m, ok := value.(map[string]any)
	if !ok {
		return knownStatusLineHook{}, false
	}
	command, ok := m["command"].(string)
	if !ok {
		return knownStatusLineHook{}, false
	}
	for _, hook := range knownStatusLineHooks {
		if strings.Contains(command, hook.marker) {
			return hook, true
		}
	}
	return knownStatusLineHook{}, false
}
