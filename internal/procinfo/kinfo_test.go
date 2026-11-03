package procinfo

import (
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// kinfoBuf returns a zeroed kinfo_proc-sized buffer with the fields written at the frozen
// offsets (little-endian, darwin layout).
func kinfoBuf(startSec int64, stat byte, comm []byte, ppid int32) []byte {
	b := make([]byte, kinfoSize)
	binary.LittleEndian.PutUint64(b[0:8], uint64(startSec))
	b[36] = stat
	copy(b[243:260], comm)
	binary.LittleEndian.PutUint32(b[560:564], uint32(ppid))
	return b
}

func TestKinfoParseFields(t *testing.T) {
	got, err := kinfoParse(4321, kinfoBuf(1762150000, 2, []byte("zsh\x00317abc"), 777))
	if err != nil {
		t.Fatalf("kinfoParse: %v", err)
	}
	want := Info{PID: 4321, PPID: 777, StartedAt: 1762150000, Comm: "zsh"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestKinfoParseCommWithoutNUL(t *testing.T) {
	got, err := kinfoParse(4321, kinfoBuf(1, 2, []byte("abcdefghijklmnopq"), 1))
	if err != nil {
		t.Fatalf("kinfoParse: %v", err)
	}
	if got.Comm != "abcdefghijklmnopq" {
		t.Fatalf("Comm = %q, want the full 17-byte name", got.Comm)
	}
}

func TestKinfoParseCommStartsWithNUL(t *testing.T) {
	got, err := kinfoParse(4321, kinfoBuf(1, 2, []byte("\x00garbage"), 1))
	if err != nil {
		t.Fatalf("kinfoParse: %v", err)
	}
	if got.Comm != "" {
		t.Fatalf("Comm = %q, want empty", got.Comm)
	}
}

func TestKinfoParseStatesDecode(t *testing.T) {
	for _, stat := range []byte{1, 2, 3, 4} {
		if _, err := kinfoParse(4321, kinfoBuf(1, stat, []byte("x"), 1)); err != nil {
			t.Fatalf("stat %d: unexpected error %v", stat, err)
		}
	}
}

func TestKinfoParseIgnoresTvUsec(t *testing.T) {
	b := kinfoBuf(1762150000, 2, []byte("zsh"), 1)
	binary.LittleEndian.PutUint64(b[8:16], 999999) // tv_usec must not leak into StartedAt
	got, err := kinfoParse(4321, b)
	if err != nil {
		t.Fatalf("kinfoParse: %v", err)
	}
	if got.StartedAt != 1762150000 {
		t.Fatalf("StartedAt = %d, want 1762150000", got.StartedAt)
	}
}

func TestKinfoParseNegativePPIDSignExtends(t *testing.T) {
	got, err := kinfoParse(4321, kinfoBuf(1, 2, []byte("x"), -1))
	if err != nil {
		t.Fatalf("kinfoParse: %v", err)
	}
	if got.PPID != -1 {
		t.Fatalf("PPID = %d, want -1", got.PPID)
	}
}

func TestKinfoParseNotFound(t *testing.T) {
	zombie := kinfoBuf(1, 5, []byte("zsh"), 1)
	for name, b := range map[string][]byte{
		"nil":    nil,
		"empty":  {},
		"zombie": zombie,
	} {
		if _, err := kinfoParse(4321, b); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
}

func TestKinfoParseShortRead(t *testing.T) {
	for _, n := range []int{1, kinfoSize - 1} {
		_, err := kinfoParse(4321, make([]byte, n))
		if err == nil {
			t.Fatalf("len %d: want error", n)
		}
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnsupported) {
			t.Fatalf("len %d: err = %v, want a non-sentinel error", n, err)
		}
		want := "procinfo: short kinfo_proc for pid 4321: " + strconv.Itoa(n) + " bytes"
		if err.Error() != want {
			t.Fatalf("len %d: message = %q, want %q", n, err.Error(), want)
		}
	}
}

func TestKinfoSelfCheck(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	now := time.Now().Unix()

	self, err := Lookup(os.Getpid())
	if err != nil {
		t.Fatalf("Lookup(self): %v", err)
	}
	if self.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", self.PID, os.Getpid())
	}
	if self.PPID != os.Getppid() {
		t.Errorf("PPID = %d, want %d", self.PPID, os.Getppid())
	}
	if self.Comm == "" {
		t.Error("Comm is empty")
	}
	if self.StartedAt < now-600 || self.StartedAt > now {
		t.Errorf("StartedAt = %d, want within the last 10 minutes (now %d)", self.StartedAt, now)
	}

	if _, err := Lookup(os.Getppid()); err != nil {
		t.Errorf("Lookup(parent): %v", err)
	}
	if _, err := Lookup(1 << 30); !errors.Is(err, ErrNotFound) {
		t.Errorf("Lookup(1<<30): err = %v, want ErrNotFound", err)
	}
}
