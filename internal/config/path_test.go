package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPath_EnvOverride(t *testing.T) {
	t.Setenv("STATUSLOOM_CONFIG", "/tmp/custom-statusloom-config.json")

	p, err := Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	if p != "/tmp/custom-statusloom-config.json" {
		t.Errorf("Path() = %q, want /tmp/custom-statusloom-config.json", p)
	}
}

func TestPath_XDGConfigHome(t *testing.T) {
	t.Setenv("STATUSLOOM_CONFIG", "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	p, err := Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}

	want := filepath.Join(dir, "statusloom", "config.json")
	if runtime.GOOS == "windows" {
		t.Skip("XDG_CONFIG_HOME is not consulted on Windows")
	}
	if p != want {
		t.Errorf("Path() = %q, want %q", p, want)
	}
}

func TestPath_HomeFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("home-directory fallback is not used on Windows")
	}

	t.Setenv("STATUSLOOM_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory available: %v", err)
	}

	p, err := Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}

	want := filepath.Join(home, ".config", "statusloom", "config.json")
	if p != want {
		t.Errorf("Path() = %q, want %q", p, want)
	}
}
