package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yacchi/statusloom/internal/config"
	"github.com/yacchi/statusloom/internal/sources"
)

// setupDoctorSources isolates statusloom's files, HOME and CLAUDE_CONFIG_DIR in
// temp dirs and returns a minimal Claude Code settings path for --settings.
func setupDoctorSources(t *testing.T) string {
	t.Helper()
	t.Setenv("STATUSLOOM_CONFIG", t.TempDir())
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return settingsPath
}

// docWithLines returns the built-in claude-code document with extra inserted
// at the start of its first line, so tests can add references without
// hand-writing a whole document.
func docWithLines(extra string) string {
	return strings.Replace(config.DefaultDocument("claude-code"), "<line>", "<line>"+extra, 1)
}

// putSources writes the given definitions to sources.json through the
// package's own Save, so the file has the exact on-disk shape.
func putSources(t *testing.T, defs ...sources.Definition) {
	t.Helper()
	f := sources.File{SchemaVersion: sources.SchemaVersion, Sources: map[string]sources.Definition{}}
	for _, d := range defs {
		if err := f.Put(d); err != nil {
			t.Fatalf("Put %s: %v", d.ID, err)
		}
	}
	if err := sources.Save(f); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func runDoctorOut(t *testing.T, settingsPath string) (string, int) {
	t.Helper()
	out, stderr, code := runCLI(t, []string{"doctor", "--settings", settingsPath}, nil, nil)
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	return out, code
}

func sourceLines(out string) []string {
	var got []string
	for _, l := range lines(out) {
		if strings.Contains(l, " sources - ") {
			got = append(got, l)
		}
	}
	return got
}

func TestDoctorSourcesMissingFileIsZeroDefined(t *testing.T) {
	settings := setupDoctorSources(t)
	out, code := runDoctorOut(t, settings)
	if code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "PASS sources - 0 defined") {
		t.Errorf("missing PASS line: %q", out)
	}
	if strings.Contains(out, "WARN sources") {
		t.Errorf("unexpected WARN sources line: %q", out)
	}
}

func TestDoctorSourcesAllowRemoteWarnsOnlyForThatSource(t *testing.T) {
	settings := setupDoctorSources(t)
	putSources(t,
		sources.Definition{ID: "a", Kind: sources.KindFile, Path: "/tmp/x.json"},
		sources.Definition{ID: "b", Kind: sources.KindHTTP, URL: "http://127.0.0.1:1/x", AllowRemote: true},
	)
	out, code := runDoctorOut(t, settings)
	if code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "PASS sources - 2 defined") {
		t.Errorf("missing PASS line: %q", out)
	}
	want := "WARN sources - b: allowRemote lets this http source reach non-loopback hosts"
	if got := sourceLines(out); len(got) != 2 || got[1] != want {
		t.Errorf("source lines = %q, want PASS line then %q", got, want)
	}
}

func TestDoctorSourcesUndefinedReferencesListedOnceSorted(t *testing.T) {
	settings := setupDoctorSources(t)
	writeDocument(t, "claude-code", docWithLines(
		`<field name="source" source="zz"/>`+
			`<field name="source" source="aa"/>`+
			`<field name="source" source="aa"/>`))
	out, code := runDoctorOut(t, settings)
	if code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	want := "WARN sources - document references undefined source(s): aa, zz"
	if n := strings.Count(out, "WARN sources"); n != 1 || !strings.Contains(out, want) {
		t.Errorf("want exactly one %q, got %q", want, out)
	}

	putSources(t, sources.Definition{ID: "aa", Kind: sources.KindFile, Path: "/tmp/x.json"})
	out, _ = runDoctorOut(t, settings)
	if want := "WARN sources - document references undefined source(s): zz"; !strings.Contains(out, want) {
		t.Errorf("after defining aa, want %q: %q", want, out)
	}

	putSources(t,
		sources.Definition{ID: "aa", Kind: sources.KindFile, Path: "/tmp/x.json"},
		sources.Definition{ID: "zz", Kind: sources.KindFile, Path: "/tmp/z.json"},
	)
	out, _ = runDoctorOut(t, settings)
	if strings.Contains(out, "undefined source") {
		t.Errorf("all defined but still warned: %q", out)
	}
}

func TestDoctorSourcesUnreadableFileFailsAndSkipsUndefinedCheck(t *testing.T) {
	settings := setupDoctorSources(t)
	writeDocument(t, "claude-code", docWithLines(`<field name="source" source="zz"/>`))
	path, err := sources.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := runDoctorOut(t, settings)
	if code != 1 {
		t.Errorf("code=%d, want 1; out=%q", code, out)
	}
	got := sourceLines(out)
	if len(got) != 1 || !strings.HasPrefix(got[0], "FAIL sources - cannot read ") || !strings.Contains(got[0], path) {
		t.Errorf("source lines = %q, want one FAIL naming %s", got, path)
	}
	if strings.Contains(out, "undefined source") {
		t.Errorf("undefined check ran against unreadable sources.json: %q", out)
	}
}

func TestDoctorSourcesNeverFetches(t *testing.T) {
	settings := setupDoctorSources(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	putSources(t, sources.Definition{ID: "h", Kind: sources.KindHTTP, URL: srv.URL + "/x"})
	writeDocument(t, "claude-code", docWithLines(`<field name="source" source="h"/>`))
	if _, code := runDoctorOut(t, settings); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("doctor fetched the http source %d time(s)", n)
	}
}

func TestDoctorRefreshWarnsForSourceAndCacheExpiryFields(t *testing.T) {
	for _, field := range []string{
		`<field name="cache-expires-at"/>`,
		`<field name="source" source="aa"/>`,
	} {
		t.Run(field, func(t *testing.T) {
			settings := setupDoctorSources(t)
			writeDocument(t, "claude-code", docWithLines(field))
			out, _ := runDoctorOut(t, settings)
			if !strings.Contains(out, "WARN refresh - ") {
				t.Errorf("missing WARN refresh line: %q", out)
			}
		})
	}
}
