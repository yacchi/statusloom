package procinfo

// Alive reports whether a process with pid may still be running. It is the
// one process-liveness probe in the module (the refresh lease and the
// configurator's monitor-workspace lock both use it) and, like Lookup, runs no
// command.
//
// "Alive" is the safe default: Alive returns false only when the platform
// proves the process is gone (Unix: kill(pid, 0) fails with ESRCH; Windows:
// OpenProcess reports no such pid, or the process has an exit code). Permission
// denied, an unexpected error and an unsupported platform all report true, so
// a caller that reclaims a dead holder's lock falls back to its own timeout
// instead of stealing a live one. pid <= 0 names no process and reports false.
//
// A recycled pid can only make a dead process look alive (the new process
// answers), never the reverse, so pid reuse costs a caller its fast path, not
// its correctness.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return alive(pid)
}
