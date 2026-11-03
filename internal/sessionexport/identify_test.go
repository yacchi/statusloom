package sessionexport

import (
	"errors"
	"testing"

	"github.com/yacchi/statusloom/internal/procinfo"
)

// identifyLookup returns a fake process lookup over infos and errs, and a pointer to
// the list of pids it was asked for.
func identifyLookup(infos map[int]procinfo.Info, errs map[int]error) (func(int) (procinfo.Info, error), *[]int) {
	var calls []int
	lookup := func(pid int) (procinfo.Info, error) {
		calls = append(calls, pid)
		if err, ok := errs[pid]; ok {
			return procinfo.Info{}, err
		}
		if info, ok := infos[pid]; ok {
			return info, nil
		}
		return procinfo.Info{}, procinfo.ErrNotFound
	}
	return lookup, &calls
}

// identifyGetenv returns a getenv over env; absent keys read as "".
func identifyGetenv(env map[string]string) func(string) string {
	return func(key string) string {
		return env[key]
	}
}

func TestIdentify(t *testing.T) {
	shellChain := map[int]procinfo.Info{
		100: {PID: 100, PPID: 50, Comm: "-zsh", StartedAt: 5},
		50:  {PID: 50, PPID: 1, Comm: "x", StartedAt: 7},
	}
	tests := []struct {
		name        string
		env         map[string]string
		ppid        int
		infos       map[int]procinfo.Info
		errs        map[int]error
		wantPID     int
		wantStarted int64
		wantOK      bool
		wantNoCalls bool
	}{
		{
			name:        "direct child with CLAUDE_PID",
			env:         map[string]string{"CLAUDE_PID": "100"},
			ppid:        100,
			infos:       map[int]procinfo.Info{100: {PID: 100, PPID: 1, Comm: "2.1.317", StartedAt: 1700000000}},
			wantPID:     100,
			wantStarted: 1700000000,
			wantOK:      true,
		},
		{
			name:        "CLAUDE_PID with surrounding whitespace",
			env:         map[string]string{"CLAUDE_PID": " 100\n"},
			ppid:        100,
			infos:       map[int]procinfo.Info{100: {PID: 100, PPID: 1, Comm: "node", StartedAt: 1700000000}},
			wantPID:     100,
			wantStarted: 1700000000,
			wantOK:      true,
		},
		{
			name:        "shell between uses StartedAt of CLAUDE_PID process",
			env:         map[string]string{"CLAUDE_PID": "50"},
			ppid:        100,
			infos:       shellChain,
			wantPID:     50,
			wantStarted: 7,
			wantOK:      true,
		},
		{
			name:   "non-shell between is never guessed",
			env:    map[string]string{"CLAUDE_PID": "50"},
			ppid:   100,
			infos:  map[int]procinfo.Info{100: {PID: 100, PPID: 50, Comm: "node", StartedAt: 5}},
			wantOK: false,
		},
		{
			name:   "CLAUDE_PID missing",
			env:    map[string]string{},
			ppid:   100,
			infos:  map[int]procinfo.Info{100: {PID: 100, PPID: 1, Comm: "2.1.317", StartedAt: 1}},
			wantOK: false, wantNoCalls: true,
		},
		{
			name:   "CLAUDE_PID empty",
			env:    map[string]string{"CLAUDE_PID": ""},
			ppid:   100,
			infos:  map[int]procinfo.Info{100: {PID: 100, PPID: 1, Comm: "2.1.317", StartedAt: 1}},
			wantOK: false, wantNoCalls: true,
		},
		{
			name:   "CLAUDE_PID non-numeric",
			env:    map[string]string{"CLAUDE_PID": "abc"},
			ppid:   100,
			infos:  map[int]procinfo.Info{100: {PID: 100, PPID: 1, Comm: "2.1.317", StartedAt: 1}},
			wantOK: false, wantNoCalls: true,
		},
		{
			name:   "CLAUDE_PID with trailing garbage",
			env:    map[string]string{"CLAUDE_PID": "12x"},
			ppid:   100,
			infos:  map[int]procinfo.Info{100: {PID: 100, PPID: 1, Comm: "2.1.317", StartedAt: 1}},
			wantOK: false, wantNoCalls: true,
		},
		{
			name:   "CLAUDE_PID zero",
			env:    map[string]string{"CLAUDE_PID": "0"},
			ppid:   100,
			infos:  map[int]procinfo.Info{100: {PID: 100, PPID: 1, Comm: "2.1.317", StartedAt: 1}},
			wantOK: false, wantNoCalls: true,
		},
		{
			name:   "CLAUDE_PID one",
			env:    map[string]string{"CLAUDE_PID": "1"},
			ppid:   100,
			infos:  map[int]procinfo.Info{100: {PID: 100, PPID: 1, Comm: "2.1.317", StartedAt: 1}},
			wantOK: false, wantNoCalls: true,
		},
		{
			name:   "CLAUDE_PID negative",
			env:    map[string]string{"CLAUDE_PID": "-5"},
			ppid:   100,
			infos:  map[int]procinfo.Info{100: {PID: 100, PPID: 1, Comm: "2.1.317", StartedAt: 1}},
			wantOK: false, wantNoCalls: true,
		},
		{
			name:   "four shell hops reach the limit before CLAUDE_PID",
			env:    map[string]string{"CLAUDE_PID": "104"},
			ppid:   100,
			infos:  identifyShellChain(),
			wantOK: false,
		},
		{
			name:        "CLAUDE_PID found on the fourth hop",
			env:         map[string]string{"CLAUDE_PID": "103"},
			ppid:        100,
			infos:       identifyShellChain(),
			wantPID:     103,
			wantStarted: 9,
			wantOK:      true,
		},
		{
			name:   "ppid 1 stops without lookup",
			env:    map[string]string{"CLAUDE_PID": "100"},
			ppid:   1,
			infos:  map[int]procinfo.Info{100: {PID: 100, PPID: 1, Comm: "2.1.317", StartedAt: 1}},
			wantOK: false, wantNoCalls: true,
		},
		{
			name:   "ppid 0 stops without lookup",
			env:    map[string]string{"CLAUDE_PID": "100"},
			ppid:   0,
			infos:  map[int]procinfo.Info{100: {PID: 100, PPID: 1, Comm: "2.1.317", StartedAt: 1}},
			wantOK: false, wantNoCalls: true,
		},
		{
			name: "shell chain reaches pid 1 before CLAUDE_PID",
			env:  map[string]string{"CLAUDE_PID": "50"},
			ppid: 100,
			infos: map[int]procinfo.Info{
				100: {PID: 100, PPID: 1, Comm: "sh", StartedAt: 5},
			},
			wantOK: false,
		},
		{
			name:   "lookup error at first hop",
			env:    map[string]string{"CLAUDE_PID": "50"},
			ppid:   100,
			errs:   map[int]error{100: procinfo.ErrNotFound},
			wantOK: false,
		},
		{
			name: "lookup error at second hop",
			env:  map[string]string{"CLAUDE_PID": "50"},
			ppid: 100,
			infos: map[int]procinfo.Info{
				100: {PID: 100, PPID: 101, Comm: "zsh", StartedAt: 5},
			},
			errs:   map[int]error{101: errors.New("x")},
			wantOK: false,
		},
		{
			name:   "unsupported platform lookup",
			env:    map[string]string{"CLAUDE_PID": "100"},
			ppid:   100,
			errs:   map[int]error{100: procinfo.ErrUnsupported},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup, calls := identifyLookup(tt.infos, tt.errs)
			pid, started, ok := identify(identifyGetenv(tt.env), tt.ppid, lookup)
			if pid != tt.wantPID || started != tt.wantStarted || ok != tt.wantOK {
				t.Errorf("identify() = (%d, %d, %v), want (%d, %d, %v)",
					pid, started, ok, tt.wantPID, tt.wantStarted, tt.wantOK)
			}
			if tt.wantNoCalls && len(*calls) != 0 {
				t.Errorf("lookup called for %v, want no calls", *calls)
			}
		})
	}
}

// identifyShellChain is 100(sh) -> 101(bash) -> 102(fish) -> 103(zsh) -> 104.
func identifyShellChain() map[int]procinfo.Info {
	return map[int]procinfo.Info{
		100: {PID: 100, PPID: 101, Comm: "sh", StartedAt: 1},
		101: {PID: 101, PPID: 102, Comm: "bash", StartedAt: 2},
		102: {PID: 102, PPID: 103, Comm: "fish", StartedAt: 3},
		103: {PID: 103, PPID: 104, Comm: "zsh", StartedAt: 9},
	}
}

func TestIdentifyIsShell(t *testing.T) {
	tests := map[string]bool{
		"-zsh":    true,
		"ZSH":     true,
		"-Bash":   true,
		"bash":    true,
		"pwsh":    true,
		"nu":      true,
		"busybox": true,
		"--zsh":   false,
		"-":       false,
		"":        false,
		"zshx":    false,
		"node":    false,
		"2.1.317": false,
		"python":  false,
	}
	for comm, want := range tests {
		if got := identifyIsShell(comm); got != want {
			t.Errorf("identifyIsShell(%q) = %v, want %v", comm, got, want)
		}
	}
}
