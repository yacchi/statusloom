package sources

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

var providerNow = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

// isolateProvider points both the config and cache dirs at fresh temp dirs so no
// test reads or writes the user's statusloom state.
func isolateProvider(t *testing.T) {
	t.Helper()
	isolateConfig(t)
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
}

func writeSourceFile(t *testing.T, path, content string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
}

func fileFixture(t *testing.T, id, path string, staleAfter int) File {
	t.Helper()
	return File{SchemaVersion: SchemaVersion, Sources: map[string]Definition{
		id: {ID: id, Kind: KindFile, Path: path, StaleAfterSeconds: staleAfter},
	}}
}

func TestLookupFileDecodesAndReportsAvailable(t *testing.T) {
	isolateProvider(t)
	path := filepath.Join(t.TempDir(), "ccfile.json")
	writeSourceFile(t, path, `{"a":{"n":3}}`, providerNow)

	p := NewProviderFromFile(fileFixture(t, "ccfile", path, 0), providerNow, 0)
	doc, defined, available := p.Lookup("ccfile")
	want := map[string]any{"a": map[string]any{"n": float64(3)}}
	if !defined || !available || !reflect.DeepEqual(doc, want) {
		t.Fatalf("Lookup = (%#v, %v, %v), want (%#v, true, true)", doc, defined, available, want)
	}
}

func TestLookupUndefinedID(t *testing.T) {
	isolateProvider(t)
	p := NewProviderFromFile(File{Sources: map[string]Definition{}}, providerNow, 0)
	doc, defined, available := p.Lookup("nope")
	if doc != nil || defined || available {
		t.Fatalf("Lookup(undefined) = (%#v, %v, %v), want (nil, false, false)", doc, defined, available)
	}
}

func TestLookupFileUnusableIsDefinedButUnavailable(t *testing.T) {
	isolateProvider(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	bad := filepath.Join(dir, "bad.json")
	writeSourceFile(t, bad, `{bad`, providerNow)
	big := filepath.Join(dir, "big.json")
	// A valid JSON prefix padded to MaxBodyBytes+1 so only the size check fails.
	pad := make([]byte, MaxBodyBytes+1)
	for i := range pad {
		pad[i] = ' '
	}
	copy(pad, `{}`)
	writeSourceFile(t, big, string(pad), providerNow)

	for name, path := range map[string]string{"missing": missing, "bad": bad, "big": big} {
		p := NewProviderFromFile(fileFixture(t, "f", path, 0), providerNow, 0)
		doc, defined, available := p.Lookup("f")
		if doc != nil || !defined || available {
			t.Errorf("%s: Lookup = (%#v, %v, %v), want (nil, true, false)", name, doc, defined, available)
		}
	}
}

func TestLookupFileStaleness(t *testing.T) {
	isolateProvider(t)
	dir := t.TempDir()
	fresh := filepath.Join(dir, "fresh.json")
	stale := filepath.Join(dir, "stale.json")
	old := filepath.Join(dir, "old.json")
	writeSourceFile(t, fresh, `{"v":1}`, providerNow.Add(-59*time.Second))
	writeSourceFile(t, stale, `{"v":1}`, providerNow.Add(-61*time.Second))
	writeSourceFile(t, old, `{"v":1}`, providerNow.AddDate(-1, 0, 0))

	cases := []struct {
		name      string
		path      string
		staleSecs int
		available bool
	}{
		{"within StaleAfter", fresh, 60, true},
		{"past StaleAfter", stale, 60, false},
		{"StaleAfter zero never stale", old, 0, true},
	}
	for _, c := range cases {
		p := NewProviderFromFile(fileFixture(t, "f", c.path, c.staleSecs), providerNow, 0)
		_, _, available := p.Lookup("f")
		if available != c.available {
			t.Errorf("%s: available = %v, want %v", c.name, available, c.available)
		}
	}
}

// The process-wide file memo is keyed by path and validated by (modTime, size).
// A rewrite that keeps both must be served from the memo; any change to either must
// be re-read. Each step uses a new Provider so the per-id memo cannot hide the result.
func TestFileMemoValidatedByModTimeAndSize(t *testing.T) {
	isolateProvider(t)
	path := filepath.Join(t.TempDir(), "memo.json")
	mtime := providerNow.Add(-time.Minute)
	writeSourceFile(t, path, `{"v":"aaa"}`, mtime)
	f := fileFixture(t, "f", path, 0)

	lookup := func() any {
		t.Helper()
		doc, _, available := NewProviderFromFile(f, providerNow, 0).Lookup("f")
		if !available {
			t.Fatalf("Lookup unavailable for %s", path)
		}
		return doc
	}
	if got := lookup(); !reflect.DeepEqual(got, map[string]any{"v": "aaa"}) {
		t.Fatalf("first lookup = %#v", got)
	}

	// Same size, same mtime: the memo must win, so the old document is returned.
	writeSourceFile(t, path, `{"v":"bbb"}`, mtime)
	if got := lookup(); !reflect.DeepEqual(got, map[string]any{"v": "aaa"}) {
		t.Fatalf("same size+mtime: got %#v, want memoized aaa", got)
	}

	// Same size, new mtime: must re-read.
	writeSourceFile(t, path, `{"v":"ccc"}`, mtime.Add(time.Second))
	if got := lookup(); !reflect.DeepEqual(got, map[string]any{"v": "ccc"}) {
		t.Fatalf("new mtime: got %#v, want ccc", got)
	}

	// Different size, original-to-new mtime restored: must re-read.
	writeSourceFile(t, path, `{"v":"longer-value"}`, mtime.Add(time.Second))
	if got := lookup(); !reflect.DeepEqual(got, map[string]any{"v": "longer-value"}) {
		t.Fatalf("new size: got %#v, want longer-value", got)
	}
}

func TestLookupMalformedSourcesJSONDefinesNothing(t *testing.T) {
	isolateProvider(t)
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	writeSourceFile(t, path, "not json", time.Time{})

	p := NewProvider(providerNow, 0)
	doc, defined, available := p.Lookup("x")
	if doc != nil || defined || available {
		t.Fatalf("Lookup = (%#v, %v, %v), want (nil, false, false)", doc, defined, available)
	}
	if f := p.File(); f.Sources == nil || len(f.Sources) != 0 {
		t.Fatalf("File().Sources = %#v, want empty non-nil map", f.Sources)
	}
}

func TestNewProviderOpensSourcesLazily(t *testing.T) {
	isolateProvider(t)
	var calls atomic.Int32
	orig := loadSources
	loadSources = func() (File, error) {
		calls.Add(1)
		return File{SchemaVersion: SchemaVersion, Sources: map[string]Definition{}}, nil
	}
	t.Cleanup(func() { loadSources = orig })

	p := NewProvider(providerNow, 0)
	if n := calls.Load(); n != 0 {
		t.Fatalf("NewProvider opened sources.json %d times, want 0", n)
	}
	p.Lookup("a")
	p.Lookup("b")
	p.File()
	if n := calls.Load(); n != 1 {
		t.Fatalf("Lookup x2 + File opened sources.json %d times, want 1", n)
	}
}

// Render-path lookups must never run a command or contact the network, even when a
// source has no envelope yet. The command writes a marker; the http server counts hits.
func TestLookupNeverRunsCommandOrNetwork(t *testing.T) {
	isolateProvider(t)
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"x":1}`))
	}))
	t.Cleanup(srv.Close)

	f := File{SchemaVersion: SchemaVersion, Sources: map[string]Definition{
		"cmd":  {ID: "cmd", Kind: KindCommand, Argv: []string{"sh", "-c", "touch " + marker}},
		"http": {ID: "http", Kind: KindHTTP, URL: srv.URL},
	}}
	p := NewProviderFromFile(f, providerNow, 0)
	for _, id := range []string{"cmd", "http"} {
		doc, defined, available := p.Lookup(id)
		if doc != nil || !defined || available {
			t.Errorf("Lookup(%s) = (%#v, %v, %v), want (nil, true, false)", id, doc, defined, available)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("command ran during Lookup (marker stat err = %v)", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("http source contacted the network %d times during Lookup", n)
	}
}

func httpFixture(t *testing.T, staleSecs int) Definition {
	t.Helper()
	return Definition{ID: "web", Kind: KindHTTP, URL: "http://127.0.0.1:9/feed", StaleAfterSeconds: staleSecs}
}

func storeHTTPEnvelope(t *testing.T, d Definition, fetchedAt time.Time, mutate func(*Envelope)) {
	t.Helper()
	env := Envelope{
		ID:        d.ID,
		DefHash:   d.Hash(),
		FetchedAt: fetchedAt,
		OK:        true,
		Body:      json.RawMessage(`{"x":1}`),
	}
	if mutate != nil {
		mutate(&env)
	}
	if err := StoreEnvelope(env); err != nil {
		t.Fatal(err)
	}
}

func TestLookupEnvelopeAvailability(t *testing.T) {
	isolateProvider(t)
	d := httpFixture(t, 60)
	f := File{SchemaVersion: SchemaVersion, Sources: map[string]Definition{"web": d}}
	want := map[string]any{"x": float64(1)}

	storeHTTPEnvelope(t, d, providerNow.Add(-5*time.Second), nil)
	if doc, defined, available := NewProviderFromFile(f, providerNow, 0).Lookup("web"); !defined || !available || !reflect.DeepEqual(doc, want) {
		t.Fatalf("fresh envelope: Lookup = (%#v, %v, %v)", doc, defined, available)
	}

	// Exactly StaleAfter old is still available (inclusive boundary).
	storeHTTPEnvelope(t, d, providerNow.Add(-60*time.Second), nil)
	if _, _, available := NewProviderFromFile(f, providerNow, 0).Lookup("web"); !available {
		t.Fatal("envelope at exactly StaleAfter reported unavailable, want available")
	}

	// One second past StaleAfter is unavailable.
	storeHTTPEnvelope(t, d, providerNow.Add(-61*time.Second), nil)
	if doc, defined, available := NewProviderFromFile(f, providerNow, 0).Lookup("web"); doc != nil || !defined || available {
		t.Fatalf("stale envelope: Lookup = (%#v, %v, %v), want (nil, true, false)", doc, defined, available)
	}

	// Envelope of an older definition (DefHash mismatch) is unavailable.
	storeHTTPEnvelope(t, d, providerNow.Add(-5*time.Second), func(e *Envelope) { e.DefHash = "0000000000000000" })
	if doc, defined, available := NewProviderFromFile(f, providerNow, 0).Lookup("web"); doc != nil || !defined || available {
		t.Fatalf("DefHash mismatch: Lookup = (%#v, %v, %v), want (nil, true, false)", doc, defined, available)
	}
}

// Once a command/http lookup has resolved, the same Provider keeps answering from its
// per-id memo even if the envelope file is gone.
func TestLookupPerIDMemoSurvivesEnvelopeRemoval(t *testing.T) {
	isolateProvider(t)
	d := httpFixture(t, 60)
	f := File{SchemaVersion: SchemaVersion, Sources: map[string]Definition{"web": d}}
	storeHTTPEnvelope(t, d, providerNow.Add(-5*time.Second), nil)

	p := NewProviderFromFile(f, providerNow, 0)
	if _, _, available := p.Lookup("web"); !available {
		t.Fatal("first lookup unavailable")
	}
	path, err := envelopePath("web")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	doc, defined, available := p.Lookup("web")
	if !defined || !available || !reflect.DeepEqual(doc, map[string]any{"x": float64(1)}) {
		t.Fatalf("after envelope removal: Lookup = (%#v, %v, %v), want memoized doc", doc, defined, available)
	}
}
