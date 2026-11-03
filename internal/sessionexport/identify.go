package sessionexport

import (
	"os"
	"strconv"
	"strings"

	"github.com/yacchi/statusloom/internal/procinfo"
)

// identifyMaxHops bounds how many parent processes identify walks up from the
// statusline command before giving up.
const identifyMaxHops = 4

// IdentifyClaudeProcess reports the Claude Code process that runs this statusline
// command. getenv is the caller's injected environment; the real os.Getenv is never
// read here.
func IdentifyClaudeProcess(getenv func(string) string) (pid int, startedAt int64, ok bool) {
	return identify(getenv, os.Getppid(), procinfo.Lookup)
}

// identify walks from ppid upward (at most identifyMaxHops processes) looking for the
// pid named by CLAUDE_PID. Only login/interactive shells may sit in between; any other
// process in the chain means the answer is unknown, so identify returns false.
func identify(getenv func(string) string, ppid int, lookup func(int) (procinfo.Info, error)) (pid int, startedAt int64, ok bool) {
	claude, err := strconv.Atoi(strings.TrimSpace(getenv("CLAUDE_PID")))
	if err != nil || claude <= 1 {
		return 0, 0, false
	}

	p := ppid
	for hop := 0; hop < identifyMaxHops; hop++ {
		if p <= 1 {
			return 0, 0, false
		}
		info, err := lookup(p)
		if err != nil {
			return 0, 0, false
		}
		if p == claude {
			return p, info.StartedAt, true
		}
		if !identifyIsShell(info.Comm) {
			return 0, 0, false
		}
		p = info.PPID
	}
	return 0, 0, false
}

// identifyShellNames lists executable names treated as shells that may sit between the
// statusline command and Claude Code.
var identifyShellNames = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "mksh": true,
	"oksh": true, "yash": true, "ash": true, "busybox": true, "fish": true, "tcsh": true,
	"csh": true, "nu": true, "pwsh": true,
}

// identifyIsShell reports whether comm names a shell. One leading "-" (login shell
// marker) is removed and the name is compared case-insensitively.
func identifyIsShell(comm string) bool {
	name := strings.ToLower(strings.TrimPrefix(comm, "-"))
	return identifyShellNames[name]
}
