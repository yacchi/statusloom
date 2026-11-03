package cli

import (
	"os"
	"time"

	"github.com/yacchi/statusloom/internal/sessionexport"
)

// identifyClaudeProcess finds the Claude Code process that spawned this statusline
// command. It is a seam so tests can stub the process lookup.
var identifyClaudeProcess = sessionexport.IdentifyClaudeProcess

// stdinStat reports on the standard input of this process. It is a seam so tests
// can stub the stdin check.
var stdinStat = func() (os.FileInfo, error) { return os.Stdin.Stat() }

// exportSession records the session state of a live statusline render for the
// session export. It is best effort: it is silent, has no result, and never
// changes what the render prints or its exit code. Only live renders export;
// a regular-file stdin (a manual run such as `statusloom claude < fixture`) is
// skipped, and so is any render whose Claude Code process cannot be identified.
func exportSession(raw []byte, getenv func(string) string, now time.Time) {
	if fi, err := stdinStat(); err == nil && fi.Mode().IsRegular() {
		return
	}
	pid, startedAt, ok := identifyClaudeProcess(getenv)
	if !ok {
		return
	}
	_ = sessionexport.Observe(raw, pid, startedAt, getenv("CLAUDE_CONFIG_DIR"), now)
}
