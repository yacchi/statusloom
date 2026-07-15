package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAccountRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	observed := time.Date(2026, 7, 11, 10, 0, 0, 0, time.FixedZone("JST", 9*3600))
	u := AccountUsage{
		SchemaVersion: 1,
		Source:        "claude-code-stdin",
		ObservedAt:    observed,
		ExpiresAt:     observed.Add(5 * time.Minute),
		FiveHour: &RateWindowState{
			UsedPercentage: 27,
			ResetsAt:       observed.Add(3 * time.Hour),
		},
	}

	if err := StoreAccount("default", u); err != nil {
		t.Fatalf("StoreAccount() error = %v", err)
	}

	got, err := LoadAccount("default")
	if err != nil {
		t.Fatalf("LoadAccount() error = %v", err)
	}
	if got == nil {
		t.Fatalf("LoadAccount() = nil, want value")
	}
	if !got.ObservedAt.Equal(observed) {
		t.Fatalf("ObservedAt = %v, want %v", got.ObservedAt, observed)
	}
	if got.FiveHour == nil || !got.FiveHour.ResetsAt.Equal(u.FiveHour.ResetsAt) {
		t.Fatalf("FiveHour = %+v, want ResetsAt %v", got.FiveHour, u.FiveHour.ResetsAt)
	}
	if got.SevenDay != nil {
		t.Fatalf("SevenDay = %+v, want nil", got.SevenDay)
	}
}

func TestLoadAccount_Missing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	got, err := LoadAccount("default")
	if err != nil {
		t.Fatalf("LoadAccount() error = %v", err)
	}
	if got != nil {
		t.Fatalf("LoadAccount() = %+v, want nil", got)
	}
}

func TestLoadAccount_Corrupt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	path, err := accountPath("default")
	if err != nil {
		t.Fatalf("accountPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := LoadAccount("default")
	if err != nil {
		t.Fatalf("LoadAccount() error = %v", err)
	}
	if got != nil {
		t.Fatalf("LoadAccount() = %+v, want nil", got)
	}
}

func TestStoreAccount_SkipsRedundantWrite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", dir)

	base := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)
	u := AccountUsage{
		SchemaVersion: 1,
		Source:        "claude-code-stdin",
		ObservedAt:    base,
		ExpiresAt:     base.Add(5 * time.Minute),
		FiveHour: &RateWindowState{
			UsedPercentage: 27,
			ResetsAt:       base.Add(3 * time.Hour),
		},
	}
	if err := StoreAccount("default", u); err != nil {
		t.Fatalf("StoreAccount() error = %v", err)
	}

	path, err := accountPath("default")
	if err != nil {
		t.Fatalf("accountPath() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	// Identical values, observed 1s later: must be skipped (no rewrite).
	u2 := u
	u2.ObservedAt = base.Add(1 * time.Second)
	u2.ExpiresAt = u2.ObservedAt.Add(5 * time.Minute)
	if err := StoreAccount("default", u2); err != nil {
		t.Fatalf("StoreAccount() error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("file content changed for identical values within 30s")
	}
	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Fatalf("mtime changed for a skipped write: before=%v after=%v", infoBefore.ModTime(), infoAfter.ModTime())
	}

	// Changed percentage: must be rewritten.
	u3 := u
	u3.ObservedAt = base.Add(2 * time.Second)
	u3.FiveHour = &RateWindowState{UsedPercentage: 42, ResetsAt: u.FiveHour.ResetsAt}
	if err := StoreAccount("default", u3); err != nil {
		t.Fatalf("StoreAccount() error = %v", err)
	}
	got, err := LoadAccount("default")
	if err != nil {
		t.Fatalf("LoadAccount() error = %v", err)
	}
	if got == nil || got.FiveHour == nil || got.FiveHour.UsedPercentage != 42 {
		t.Fatalf("LoadAccount() = %+v, want UsedPercentage 42", got)
	}

	// Identical values again, but observed 31s later than the last store:
	// must be rewritten even though the values didn't change.
	afterChange, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	u4 := u3
	u4.ObservedAt = u3.ObservedAt.Add(31 * time.Second)
	if err := StoreAccount("default", u4); err != nil {
		t.Fatalf("StoreAccount() error = %v", err)
	}
	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(final) == string(afterChange) {
		t.Fatalf("file was not rewritten after 31s even though it was identical")
	}
}

func TestSanitizeAccountKey(t *testing.T) {
	cases := map[string]string{
		"default":      "default",
		"a/b\\c":       "a_b_c",
		"UPPER-case_1": "_____-case_1",
	}
	for in, want := range cases {
		if got := sanitizeAccountKey(in); got != want {
			t.Fatalf("sanitizeAccountKey(%q) = %q, want %q", in, got, want)
		}
	}
}
