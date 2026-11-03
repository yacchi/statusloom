package procinfo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procfsRead reads a procfs mounted at root and returns Info for pid.
//
//	<root>/<pid>/stat missing (fs.ErrNotExist) -> ErrNotFound; other read errors pass through.
//	<root>/stat unreadable                    -> a non-sentinel error (never ErrNotFound).
func procfsRead(root string, pid int) (Info, error) {
	pidPath := filepath.Join(root, strconv.Itoa(pid), "stat")
	pidStat, err := os.ReadFile(pidPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Info{}, ErrNotFound
		}
		return Info{}, err
	}
	sysPath := filepath.Join(root, "stat")
	sysStat, err := os.ReadFile(sysPath)
	if err != nil {
		return Info{}, fmt.Errorf("procinfo: read %s: %w", sysPath, err)
	}
	return procfsParse(pid, string(pidStat), string(sysStat))
}

// procfsParse decodes /proc/<pid>/stat (pidStat) and the "btime" line of /proc/stat (sysStat).
// It is pure. comm is the text between the first '(' and the last ')'. The remaining fields
// are read after the last ')': fields[0] is the state, fields[1] the ppid and fields[19] the
// start time in clock ticks. StartedAt = btime + starttime/100 (USER_HZ is 100 on mainstream
// Linux; integer division truncates).
func procfsParse(pid int, pidStat, sysStat string) (Info, error) {
	i := strings.IndexByte(pidStat, '(')
	j := strings.LastIndexByte(pidStat, ')')
	if i < 0 || j < i {
		return Info{}, fmt.Errorf("procinfo: malformed stat for pid %d", pid)
	}
	comm := pidStat[i+1 : j]
	fields := strings.Fields(pidStat[j+1:])
	if len(fields) < 20 {
		return Info{}, fmt.Errorf("procinfo: short stat for pid %d: %d fields", pid, len(fields))
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return Info{}, ErrNotFound
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return Info{}, fmt.Errorf("procinfo: bad ppid for pid %d: %w", pid, err)
	}
	starttime, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil || starttime < 0 {
		return Info{}, fmt.Errorf("procinfo: bad starttime for pid %d: %q", pid, fields[19])
	}
	btime, err := procfsBtime(sysStat)
	if err != nil {
		return Info{}, err
	}
	return Info{PID: pid, PPID: ppid, StartedAt: btime + starttime/100, Comm: comm}, nil
}

// procfsBtime returns the integer of the first line of /proc/stat that is exactly "btime <n>".
func procfsBtime(sysStat string) (int64, error) {
	for _, line := range strings.Split(sysStat, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || f[0] != "btime" {
			continue
		}
		btime, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("procinfo: bad btime %q", f[1])
		}
		return btime, nil
	}
	return 0, errors.New("procinfo: btime not found")
}
