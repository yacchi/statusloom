package procinfo

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const procfsSys = "cpu  1 2 3 4\nintr 5\nbtime 1762128000\nprocesses 9\n"

// procfsLine builds a /proc/<pid>/stat line whose starttime lands on fields[19].
func procfsLine(comm, state, ppid, start string) string {
	return "4242 (" + comm + ") " + state + " " + ppid + " 4242 4242 34816 4250 4194304 1234 0 0 0 10 5 0 0 20 0 1 0 " + start + " 9437184 1200\n"
}

// procfsRoot writes each relative path with its content under a fresh temp dir and returns it.
func procfsRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// procfsIsOther reports a non-nil error that is neither ErrNotFound nor ErrUnsupported.
func procfsIsOther(err error) bool {
	return err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrUnsupported)
}

func TestProcfsParse(t *testing.T) {
	want := Info{PID: 4242, PPID: 4200, StartedAt: 1762128123, Comm: "zsh"}
	tests := []struct {
		name     string
		pidStat  string
		sysStat  string
		want     Info
		notFound bool
		wantErr  bool
	}{
		{name: "normal", pidStat: procfsLine("zsh", "S", "4200", "12345"), sysStat: procfsSys, want: want},
		{name: "starttime truncated not rounded", pidStat: procfsLine("zsh", "S", "4200", "12399"), sysStat: procfsSys, want: want},
		{name: "comm with parentheses", pidStat: procfsLine("a) (b", "S", "4200", "12345"), sysStat: procfsSys, want: Info{PID: 4242, PPID: 4200, StartedAt: 1762128123, Comm: "a) (b"}},
		{name: "zombie", pidStat: procfsLine("zsh", "Z", "4200", "12345"), sysStat: procfsSys, notFound: true},
		{name: "dead", pidStat: procfsLine("zsh", "X", "4200", "12345"), sysStat: procfsSys, notFound: true},
		{name: "empty stat", pidStat: "", sysStat: procfsSys, wantErr: true},
		{name: "no parentheses", pidStat: ")x(", sysStat: procfsSys, wantErr: true},
		{name: "short stat line", pidStat: "4242 (zsh) S 4200 1 2", sysStat: procfsSys, wantErr: true},
		{name: "bad ppid", pidStat: procfsLine("zsh", "S", "x", "12345"), sysStat: procfsSys, wantErr: true},
		{name: "bad starttime", pidStat: procfsLine("zsh", "S", "4200", "x"), sysStat: procfsSys, wantErr: true},
		{name: "negative starttime", pidStat: procfsLine("zsh", "S", "4200", "-1"), sysStat: procfsSys, wantErr: true},
		{name: "no btime line", pidStat: procfsLine("zsh", "S", "4200", "12345"), sysStat: "cpu  1 2 3 4\nintr 5\n", wantErr: true},
		{name: "bad btime", pidStat: procfsLine("zsh", "S", "4200", "12345"), sysStat: "btime x\n", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := procfsParse(4242, tc.pidStat, tc.sysStat)
			switch {
			case tc.notFound:
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				if got != (Info{}) {
					t.Fatalf("got %+v, want zero Info", got)
				}
			case tc.wantErr:
				if !procfsIsOther(err) {
					t.Fatalf("err = %v, want non-sentinel error", err)
				}
				if got != (Info{}) {
					t.Fatalf("got %+v, want zero Info", got)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tc.want {
					t.Fatalf("got %+v, want %+v", got, tc.want)
				}
			}
		})
	}
}

func TestProcfsRead(t *testing.T) {
	want := Info{PID: 4242, PPID: 4200, StartedAt: 1762128123, Comm: "zsh"}

	t.Run("normal", func(t *testing.T) {
		root := procfsRoot(t, map[string]string{
			"4242/stat": procfsLine("zsh", "S", "4200", "12345"),
			"stat":      procfsSys,
		})
		got, err := procfsRead(root, 4242)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})

	t.Run("no pid directory", func(t *testing.T) {
		root := procfsRoot(t, map[string]string{"stat": procfsSys})
		if _, err := procfsRead(root, 4242); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("both stat files missing", func(t *testing.T) {
		root := t.TempDir()
		if _, err := procfsRead(root, 4242); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("root stat missing", func(t *testing.T) {
		root := procfsRoot(t, map[string]string{"4242/stat": procfsLine("zsh", "S", "4200", "12345")})
		if _, err := procfsRead(root, 4242); !procfsIsOther(err) {
			t.Fatalf("err = %v, want non-sentinel error", err)
		}
	})

	t.Run("pid stat is a directory", func(t *testing.T) {
		root := procfsRoot(t, map[string]string{"stat": procfsSys})
		if err := os.MkdirAll(filepath.Join(root, "4242", "stat"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := procfsRead(root, 4242); !procfsIsOther(err) {
			t.Fatalf("err = %v, want non-sentinel error", err)
		}
	})
}

func TestProcfsSelfCheck(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
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
	now := time.Now().Unix()
	if self.StartedAt > now || self.StartedAt < now-600 {
		t.Errorf("StartedAt = %d, want within the last 10 minutes (now %d)", self.StartedAt, now)
	}
	if _, err := Lookup(os.Getppid()); err != nil {
		t.Errorf("Lookup(parent): %v", err)
	}
	if _, err := Lookup(1 << 30); !errors.Is(err, ErrNotFound) {
		t.Errorf("Lookup(1<<30) err = %v, want ErrNotFound", err)
	}
}
