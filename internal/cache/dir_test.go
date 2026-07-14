package cache

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestDir_EnvOverrideWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "should-not-be-used"))

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if got != dir {
		t.Fatalf("Dir() = %q, want %q", got, dir)
	}
}

func TestDir_XDGCacheHomeRespected(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", "")
	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	want := filepath.Join(xdg, "statusloom")
	if got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}

func TestDir_PlatformDefault(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}

	if runtime.GOOS == "windows" {
		if got == "" {
			t.Fatalf("Dir() = empty string on windows")
		}
		return
	}

	if filepath.Base(got) != "statusloom" {
		t.Fatalf("Dir() = %q, want basename %q", got, "statusloom")
	}
}
