package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

type atomicTestPayload struct {
	Value int `json:"value"`
}

// TestWriteJSONAtomic_ConcurrentReadersAlwaysValid runs many concurrent
// writers against a single path while a reader loop reads it, verifying
// the reader never observes a partially written / corrupt file: either
// the file does not exist yet, or it unmarshals cleanly.
func TestWriteJSONAtomic_ConcurrentReadersAlwaysValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.json")

	stop := make(chan struct{})
	var mu sync.Mutex
	var readErr error

	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}

			data, err := os.ReadFile(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				mu.Lock()
				readErr = err
				mu.Unlock()
				return
			}

			var p atomicTestPayload
			if err := json.Unmarshal(data, &p); err != nil {
				mu.Lock()
				readErr = err
				mu.Unlock()
				return
			}
		}
	}()

	var writers sync.WaitGroup
	for g := 0; g < 10; g++ {
		writers.Add(1)
		go func(g int) {
			defer writers.Done()
			for i := 0; i < 20; i++ {
				if err := WriteJSONAtomic(path, atomicTestPayload{Value: g*100 + i}); err != nil {
					mu.Lock()
					readErr = err
					mu.Unlock()
					return
				}
			}
		}(g)
	}

	writers.Wait()
	close(stop)
	readers.Wait()

	mu.Lock()
	defer mu.Unlock()
	if readErr != nil {
		t.Fatalf("reader observed invalid state: %v", readErr)
	}

	// No stray temp files should remain.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "payload.json" {
		t.Fatalf("directory contains unexpected entries: %v", entries)
	}
}

func TestReadJSON_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")

	var out atomicTestPayload
	ok, err := ReadJSON(path, &out)
	if err != nil {
		t.Fatalf("ReadJSON() error = %v", err)
	}
	if ok {
		t.Fatalf("ReadJSON() ok = true, want false")
	}
}

func TestWriteJSONAtomic_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "payload.json")

	want := atomicTestPayload{Value: 42}
	if err := WriteJSONAtomic(path, want); err != nil {
		t.Fatalf("WriteJSONAtomic() error = %v", err)
	}

	var got atomicTestPayload
	ok, err := ReadJSON(path, &got)
	if err != nil {
		t.Fatalf("ReadJSON() error = %v", err)
	}
	if !ok {
		t.Fatalf("ReadJSON() ok = false, want true")
	}
	if got != want {
		t.Fatalf("ReadJSON() = %+v, want %+v", got, want)
	}
}

func TestWriteJSONAtomicNoSync_CreatesNestedDirAndModes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "x.json")

	if err := WriteJSONAtomicNoSync(path, atomicTestPayload{Value: 7}); err != nil {
		t.Fatalf("WriteJSONAtomicNoSync() error = %v", err)
	}

	var got atomicTestPayload
	ok, err := ReadJSON(path, &got)
	if err != nil {
		t.Fatalf("ReadJSON() error = %v", err)
	}
	if !ok || got.Value != 7 {
		t.Fatalf("ReadJSON() = (%v, %+v), want (true, Value 7)", ok, got)
	}

	if runtime.GOOS == "windows" {
		return
	}
	for _, d := range []string{filepath.Join(dir, "a"), filepath.Join(dir, "a", "b")} {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatalf("Stat(%s) error = %v", d, err)
		}
		if mode := info.Mode().Perm(); mode != 0o700 {
			t.Fatalf("dir %s mode = %o, want 700", d, mode)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode = %o, want 600", mode)
	}
}

func TestWriteJSONAtomicNoSync_ReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.json")

	if err := WriteJSONAtomicNoSync(path, atomicTestPayload{Value: 1}); err != nil {
		t.Fatalf("first WriteJSONAtomicNoSync() error = %v", err)
	}
	want := atomicTestPayload{Value: 2}
	if err := WriteJSONAtomicNoSync(path, want); err != nil {
		t.Fatalf("second WriteJSONAtomicNoSync() error = %v", err)
	}

	var got atomicTestPayload
	if _, err := ReadJSON(path, &got); err != nil {
		t.Fatalf("ReadJSON() error = %v", err)
	}
	if got != want {
		t.Fatalf("ReadJSON() = %+v, want %+v", got, want)
	}
}

func TestWriteJSONAtomicNoSync_MarshalFailureLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.json")

	if err := WriteJSONAtomicNoSync(path, make(chan int)); err == nil {
		t.Fatalf("WriteJSONAtomicNoSync() error = nil, want marshal error")
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target file stat error = %v, want not exist", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory contains unexpected entries: %v", entries)
	}
}

func TestWriteJSONAtomicNoSync_SuccessLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.json")

	if err := WriteJSONAtomicNoSync(path, atomicTestPayload{Value: 3}); err != nil {
		t.Fatalf("WriteJSONAtomicNoSync() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "payload.json" {
		t.Fatalf("directory contains unexpected entries: %v", entries)
	}
}
