package sources

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAvailableBoundary(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	def := Definition{ID: "a", Kind: KindCommand, Argv: []string{"x"}, StaleAfterSeconds: 60}
	now := time.Now()
	ok := Envelope{
		ID:        "a",
		DefHash:   def.Hash(),
		OK:        true,
		FetchedAt: now.Add(-60 * time.Second),
		Body:      json.RawMessage(`{"a":1}`),
	}

	if !Available(def, ok, now, 0) {
		t.Fatal("age exactly StaleAfter must still be available")
	}
	if Available(def, ok, now.Add(time.Nanosecond), 0) {
		t.Fatal("age one nanosecond past StaleAfter must not be available")
	}
}

func TestAvailableRejectsInvalidEnvelopes(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())

	def := Definition{ID: "a", Kind: KindCommand, Argv: []string{"x"}, StaleAfterSeconds: 60}
	now := time.Now()
	base := Envelope{
		ID:        "a",
		DefHash:   def.Hash(),
		OK:        true,
		FetchedAt: now,
		Body:      json.RawMessage(`{"a":1}`),
	}

	cases := map[string]Envelope{
		"hash mismatch": func() Envelope { e := base; e.DefHash = "other"; return e }(),
		"failed fetch":  func() Envelope { e := base; e.OK = false; e.Error = "boom"; return e }(),
		"empty body":    func() Envelope { e := base; e.Body = nil; return e }(),
	}
	for name, env := range cases {
		if Available(def, env, now, 0) {
			t.Errorf("%s: Available = true, want false", name)
		}
	}
}

func TestStoreAndLoadEnvelope(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", cacheDir)

	def := Definition{ID: "a", Kind: KindCommand, Argv: []string{"x"}}
	env := Envelope{
		ID:        "a",
		DefHash:   def.Hash(),
		OK:        true,
		FetchedAt: time.Now(),
		Body:      json.RawMessage(`{"a":1}`),
	}

	if err := StoreEnvelope(env); err != nil {
		t.Fatalf("StoreEnvelope: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "sources", "external", "a.json")); err != nil {
		t.Fatalf("envelope not written under sources/external: %v", err)
	}

	got, ok := LoadEnvelope("a")
	if !ok {
		t.Fatal("LoadEnvelope(a) = false, want true")
	}
	if got.ID != env.ID || got.DefHash != env.DefHash || got.OK != env.OK {
		t.Errorf("round trip mismatch: got %+v", got)
	}
	// The atomic writer re-indents raw JSON, so compare the decoded value, not the bytes.
	var gotBody, wantBody any
	if err := json.Unmarshal(got.Body, &gotBody); err != nil {
		t.Fatalf("stored body is not JSON: %v", err)
	}
	if err := json.Unmarshal(env.Body, &wantBody); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotBody, wantBody) {
		t.Errorf("body = %s, want %s", got.Body, env.Body)
	}
	if got.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", got.SchemaVersion)
	}
}

func TestLoadEnvelopeMissingAndCorrupt(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", cacheDir)

	if _, ok := LoadEnvelope("missing"); ok {
		t.Fatal("LoadEnvelope(missing) = true, want false")
	}

	dir := filepath.Join(cacheDir, "sources", "external")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadEnvelope("bad"); ok {
		t.Fatal("LoadEnvelope(bad) = true for corrupt JSON, want false")
	}
}

func TestEnvelopeDoesNotCollideWithTranscriptCache(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("STATUSLOOM_CACHE_DIR", cacheDir)

	transcriptDir := filepath.Join(cacheDir, "sources")
	if err := os.MkdirAll(transcriptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(transcriptDir, "transcript-x.json")
	const siblingBody = `{"kept":true}`
	if err := os.WriteFile(sibling, []byte(siblingBody), 0o600); err != nil {
		t.Fatal(err)
	}

	env := Envelope{ID: "transcript-x", OK: true, FetchedAt: time.Now(), Body: json.RawMessage(`{"a":1}`)}
	if err := StoreEnvelope(env); err != nil {
		t.Fatalf("StoreEnvelope: %v", err)
	}

	raw, err := os.ReadFile(sibling)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != siblingBody {
		t.Errorf("transcript cache file was modified: %s", raw)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "sources", "external", "transcript-x.json")); err != nil {
		t.Errorf("envelope not stored under sources/external: %v", err)
	}
	if _, ok := LoadEnvelope("transcript-x"); !ok {
		t.Error("LoadEnvelope(transcript-x) = false, want true")
	}
}
