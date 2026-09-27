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

// orcaStatusLineHookTimeout bounds the best-effort delegation to Orca's own
// claude-statusline.sh hook. Orca's script itself caps its outbound request
// at 1.5s (see its own --max-time), so this leaves it room to complete
// normally while still guaranteeing statusloom's own process exits promptly
// if the hook hangs.
const orcaStatusLineHookTimeout = 2 * time.Second

// runClaude implements `statusloom claude`. It renders exactly like
// runRenderPipeline, and additionally best-effort forwards the raw stdin
// payload to Orca's own claude-statusline.sh hook, if present. This lets
// statusloom be Claude Code's sole statusLine command on a machine where
// Orca (a third-party coding-agent IDE) previously installed its own
// forwarder there: without this, configuring statusloom as statusLine
// silently drops Orca's pane integration, and configuring Orca's script
// instead silently drops statusloom's own status line - only one command
// can occupy the slot.
func runClaude(stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	raw, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes))
	if err != nil {
		return fail(stderr, fmt.Errorf("reading stdin: %w", err))
	}

	lines, rerr := renderDocFromRaw(raw, getenv, "claude-code", stderr, false)
	code := writeRenderResult(stdout, stderr, lines, nil, rerr)

	forwardToOrcaStatusLine(raw, getenv)

	return code
}

// forwardToOrcaStatusLine best-effort hands raw to
// ~/.orca/agent-hooks/claude-statusline.sh, if it exists and is executable.
//
// statusloom deliberately does not reimplement Orca's forwarding protocol
// (which pane it's running in, which local port and token to post to, its
// own dedup window). That protocol is undocumented and Orca's to change;
// the script already gates all of it internally and exits immediately
// when not running inside an Orca-managed pane, so handing it the same
// payload Orca's own installer would have wired as statusLine is enough -
// with zero cost on any machine where Orca (or the hook) isn't present.
func forwardToOrcaStatusLine(raw []byte, getenv func(string) string) {
	script := orcaStatusLineHookPath(getenv)
	if script == "" || !hasOrcaStatusLineHook(getenv) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), orcaStatusLineHookTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, script)
	cmd.Stdin = bytes.NewReader(raw)
	_ = cmd.Run()
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

// hasOrcaStatusLineHook reports whether Orca's claude-statusline.sh hook is
// present and executable, i.e. whether forwardToOrcaStatusLine (and, by
// extension, `statusloom claude`) will actually hand it the rendered
// payload.
func hasOrcaStatusLineHook(getenv func(string) string) bool {
	script := orcaStatusLineHookPath(getenv)
	if script == "" {
		return false
	}
	info, err := os.Stat(script)
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

// statusLineReferencesOrca reports whether value - an existing
// statusLine/subagentStatusLine object as decoded from Claude Code settings
// JSON (map[string]any) - has a "command" string that references Orca's own
// claude-statusline hook. It is false for anything else (a differently
// shaped value, a missing or non-string command, or a command that simply
// doesn't mention Orca's hook).
func statusLineReferencesOrca(value any) bool {
	m, ok := value.(map[string]any)
	if !ok {
		return false
	}
	command, ok := m["command"].(string)
	if !ok {
		return false
	}
	return strings.Contains(command, orcaStatusLineHookMarker)
}
