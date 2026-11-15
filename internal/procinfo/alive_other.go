//go:build !unix && !windows

package procinfo

// alive cannot probe a process on this platform, so it reports every pid as
// alive (the safe default: callers then fall back to their own timeouts).
func alive(int) bool { return true }
