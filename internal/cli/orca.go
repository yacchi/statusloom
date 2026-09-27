package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	home := getenv("HOME")
	if home == "" {
		return
	}
	script := filepath.Join(home, ".orca", "agent-hooks", "claude-statusline.sh")
	info, err := os.Stat(script)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), orcaStatusLineHookTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, script)
	cmd.Stdin = bytes.NewReader(raw)
	_ = cmd.Run()
}
