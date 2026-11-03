package procinfo

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// kinfoSize is sizeof(struct kinfo_proc) on darwin/arm64 and darwin/amd64 (LP64).
const kinfoSize = 648

// kinfoParse decodes the kinfo_proc that sysctl {CTL_KERN, KERN_PROC, KERN_PROC_PID, pid}
// returned for pid (b is the returned bytes, length = the length sysctl reported).
//
//	len(b) == 0             -> ErrNotFound (sysctl reports length 0 for a missing pid)
//	0 < len(b) < kinfoSize  -> a non-sentinel error (short read)
//	p_stat == 5 (SZOMB)     -> ErrNotFound
//	otherwise               -> Info{PID: pid, PPID: e_ppid, StartedAt: p_starttime.tv_sec,
//	                           Comm: p_comm cut at the first NUL}
func kinfoParse(pid int, b []byte) (Info, error) {
	if len(b) == 0 {
		return Info{}, ErrNotFound
	}
	if len(b) < kinfoSize {
		return Info{}, fmt.Errorf("procinfo: short kinfo_proc for pid %d: %d bytes", pid, len(b))
	}
	const (
		offStarttime = 0   // p_starttime.tv_sec, int64
		offStat      = 36  // p_stat, byte
		offComm      = 243 // p_comm, [17]byte
		offPPID      = 560 // e_ppid, int32
		commLen      = 17
	)
	if b[offStat] == 5 { // SZOMB
		return Info{}, ErrNotFound
	}
	comm := b[offComm : offComm+commLen]
	if i := bytes.IndexByte(comm, 0); i >= 0 {
		comm = comm[:i]
	}
	return Info{
		PID:       pid,
		PPID:      int(int32(binary.LittleEndian.Uint32(b[offPPID : offPPID+4]))),
		StartedAt: int64(binary.LittleEndian.Uint64(b[offStarttime : offStarttime+8])),
		Comm:      string(comm),
	}, nil
}
