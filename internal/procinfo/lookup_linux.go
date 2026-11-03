//go:build linux

package procinfo

func init() {
	platformLookup = func(pid int) (Info, error) {
		return procfsRead("/proc", pid)
	}
}
