package procinfo

import (
	"errors"
	"testing"
)

// setHook replaces platformLookup with f for the duration of the test and
// restores the previous value via t.Cleanup.
func setHook(t *testing.T, f func(int) (Info, error)) {
	t.Helper()
	saved := platformLookup
	platformLookup = f
	t.Cleanup(func() { platformLookup = saved })
}

func TestLookupNonPositivePID(t *testing.T) {
	called := false
	setHook(t, func(pid int) (Info, error) {
		called = true
		return Info{PID: pid}, nil
	})

	for _, pid := range []int{0, -1, -12345} {
		info, err := Lookup(pid)
		if info != (Info{}) {
			t.Errorf("Lookup(%d) info = %+v, want zero Info", pid, info)
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("Lookup(%d) err = %v, want ErrNotFound", pid, err)
		}
	}
	if called {
		t.Error("hook was called for pid <= 0")
	}
}

func TestLookupNilHook(t *testing.T) {
	setHook(t, nil)

	for _, pid := range []int{1, 42, 1 << 30} {
		info, err := Lookup(pid)
		if info != (Info{}) {
			t.Errorf("Lookup(%d) info = %+v, want zero Info", pid, info)
		}
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("Lookup(%d) err = %v, want ErrUnsupported", pid, err)
		}
		if errors.Is(err, ErrNotFound) {
			t.Errorf("Lookup(%d) err = %v, must not be ErrNotFound", pid, err)
		}
	}
}

func TestLookupReturnsHookResultUnchanged(t *testing.T) {
	want := Info{PID: 7, PPID: 3, StartedAt: 1700000000, Comm: "claude"}
	var gotPID int
	setHook(t, func(pid int) (Info, error) {
		gotPID = pid
		return want, nil
	})

	info, err := Lookup(7)
	if err != nil {
		t.Fatalf("Lookup(7) err = %v, want nil", err)
	}
	if info != want {
		t.Errorf("Lookup(7) info = %+v, want %+v", info, want)
	}
	if gotPID != 7 {
		t.Errorf("hook received pid %d, want 7", gotPID)
	}
}

func TestLookupReturnsHookErrorUnchanged(t *testing.T) {
	boom := errors.New("boom")
	partial := Info{PID: 5, PPID: 1, StartedAt: 42, Comm: "partial"}
	setHook(t, func(pid int) (Info, error) {
		return partial, boom
	})

	info, err := Lookup(5)
	if info != partial {
		t.Errorf("Lookup(5) info = %+v, want %+v", info, partial)
	}
	if err != boom {
		t.Errorf("Lookup(5) err = %v, want identical boom error", err)
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnsupported) {
		t.Errorf("Lookup(5) err = %v, must be neither ErrNotFound nor ErrUnsupported", err)
	}

	setHook(t, func(pid int) (Info, error) {
		return Info{}, ErrNotFound
	})
	_, err = Lookup(9)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Lookup(9) err = %v, want ErrNotFound", err)
	}
}
