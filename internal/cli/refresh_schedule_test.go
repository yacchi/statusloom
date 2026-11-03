package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
	"github.com/yacchi/statusloom/internal/sources"
)

// Scenario tests for the render -> refresh scheduler -> worker seams
// (plans/external-sources.md, 2026-11-03 refresh scheduling). Each test drives
// the real render path through runCLI and the real worker through runRefresh,
// substituting only the spawn and the external-source fetch.

const (
	testSessionID  = "3f9a2b1c-4d5e-6f70-8a9b-0c1d2e3f4a5b"
	testTranscript = "/Users/dev/.claude/projects/-Users-dev-myapp/3f9a2b1c.jsonl"
	testVersion    = "2.1.200"
)

// captureSpawns replaces startRefreshProcess with a recorder that copies each
// spawn's arguments and reports success without starting anything. The saved
// value (the TestMain stub) is restored by t.Cleanup.
func captureSpawns(t *testing.T) *[][]string {
	t.Helper()
	var mu sync.Mutex
	var calls [][]string
	prev := startRefreshProcess
	startRefreshProcess = func(args ...string) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	t.Cleanup(func() { startRefreshProcess = prev })
	return &calls
}

// stubFetch replaces the external-source fetcher for the test's duration.
func stubFetch(t *testing.T, f sources.Fetcher) {
	t.Helper()
	prev := fetchSource
	fetchSource = f
	t.Cleanup(func() { fetchSource = prev })
}

// payload is the full.json stdin fixture with its rate_limits removed (so no
// five-hour/seven-day window rolls over and forces a usage spawn), pointed at
// the test session and transcript, and stamped with the Claude Code version.
func payload(t *testing.T) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(fixture(t, "full.json"), &m); err != nil {
		t.Fatalf("decode full.json: %v", err)
	}
	delete(m, "rate_limits")
	m["session_id"] = testSessionID
	m["transcript_path"] = testTranscript
	m["version"] = testVersion
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	return data
}

// saveSources writes the given definitions to sources.json (keyed by ID).
func saveSources(t *testing.T, defs ...sources.Definition) {
	t.Helper()
	f := sources.File{Sources: map[string]sources.Definition{}}
	for _, d := range defs {
		f.Sources[d.ID] = d
	}
	if err := sources.Save(f); err != nil {
		t.Fatalf("sources.Save: %v", err)
	}
}

// argAfter returns the value following flag in args, or "" when flag is absent.
func argAfter(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

// normalizeSpawn checks the fixed spawn prefix and returns args with the random
// lease id (index 3) replaced by "L".
func normalizeSpawn(t *testing.T, args []string) []string {
	t.Helper()
	if len(args) < 4 || args[0] != "refresh" || args[1] != "--once" || args[2] != "--lease-id" || args[3] == "" {
		t.Fatalf("spawn args = %q, want refresh --once --lease-id <id> ...", args)
	}
	out := slices.Clone(args)
	out[3] = "L"
	return out
}

// runSpawned runs the worker with a captured spawn's arguments, which also
// releases the spawn's lease.
func runSpawned(t *testing.T, args []string) {
	t.Helper()
	var out, errb bytes.Buffer
	if code := runRefresh(args[1:], &out, &errb); code != 0 {
		t.Fatalf("worker %q exit = %d, want 0 (stderr: %s)", args, code, errb.String())
	}
}

// scheduleDoc wraps body in a one-line claude-code document.
func scheduleDoc(body string) string {
	return `<statusloom version="1" tool="claude-code" color-level="none">
  <layout name="default" active="true">
    <line>` + body + `</line>
  </layout>
</statusloom>`
}

// renderClaude runs `statusloom claude` on payload(t) and returns its stdout.
func renderClaude(t *testing.T, env map[string]string) string {
	t.Helper()
	stdout, stderr, code := runCLI(t, []string{"claude"}, payload(t), env)
	if code != 0 {
		t.Fatalf("claude exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	return stdout
}

// TestRefreshSchedule_TranscriptGating: only a document that uses
// transcript-derived values hands the transcript to the worker.
func TestRefreshSchedule_TranscriptGating(t *testing.T) {
	t.Run("no transcript field", func(t *testing.T) {
		setupEnv(t)
		t.Setenv("STATUSLOOM_NO_USAGE_API", "")
		writeDocument(t, "claude-code", scheduleDoc(`<field name="model"/>`))
		spawns := captureSpawns(t)
		renderClaude(t, nil)
		if len(*spawns) != 1 {
			t.Fatalf("spawns = %d, want 1", len(*spawns))
		}
		got := normalizeSpawn(t, (*spawns)[0])
		want := []string{"refresh", "--once", "--lease-id", "L", "--cc-version", testVersion}
		if !slices.Equal(got, want) {
			t.Errorf("args = %q, want %q", got, want)
		}
	})

	t.Run("compaction-count field", func(t *testing.T) {
		setupEnv(t)
		t.Setenv("STATUSLOOM_NO_USAGE_API", "")
		writeDocument(t, "claude-code", scheduleDoc(`<field name="compaction-count"/>`))
		spawns := captureSpawns(t)
		renderClaude(t, nil)
		if len(*spawns) != 1 {
			t.Fatalf("spawns = %d, want 1", len(*spawns))
		}
		got := normalizeSpawn(t, (*spawns)[0])
		want := []string{"refresh", "--once", "--lease-id", "L", "--cc-version", testVersion,
			"--session-id", testSessionID, "--transcript", testTranscript}
		if !slices.Equal(got, want) {
			t.Errorf("args = %q, want %q", got, want)
		}
	})
}

// TestRefreshSchedule_RefreshIntervalSpawnArg: the render passes Claude Code's
// statusLine.refreshInterval to the worker only when it is known and positive.
func TestRefreshSchedule_RefreshIntervalSpawnArg(t *testing.T) {
	cases := []struct {
		name     string
		settings string // settings.json body; "" = no file
		wantArgs []string
	}{
		{name: "15 seconds", settings: `{"statusLine":{"type":"command","command":"statusloom claude","refreshInterval":15}}`,
			wantArgs: []string{"--refresh-interval", "15"}},
		{name: "2.5 seconds", settings: `{"statusLine":{"type":"command","command":"statusloom claude","refreshInterval":2.5}}`,
			wantArgs: []string{"--refresh-interval", "2.5"}},
		{name: "zero", settings: `{"statusLine":{"type":"command","command":"statusloom claude","refreshInterval":0}}`},
		{name: "no settings file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupEnv(t)
			t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
			dir := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", dir)
			if tc.settings != "" {
				if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(tc.settings), 0o600); err != nil {
					t.Fatalf("write settings.json: %v", err)
				}
			}
			writeDocument(t, "claude-code", scheduleDoc(`<field name="compaction-count"/>`))
			spawns := captureSpawns(t)
			renderClaude(t, map[string]string{"CLAUDE_CONFIG_DIR": dir})
			if len(*spawns) != 1 {
				t.Fatalf("spawns = %d, want 1", len(*spawns))
			}
			got := normalizeSpawn(t, (*spawns)[0])
			want := append([]string{"refresh", "--once", "--lease-id", "L", "--cc-version", testVersion,
				"--session-id", testSessionID, "--transcript", testTranscript}, tc.wantArgs...)
			if !slices.Equal(got, want) {
				t.Errorf("args = %q, want %q", got, want)
			}
		})
	}
}

// TestRefreshSchedule_WorkerTranscriptInterval: the worker schedules the next
// transcript refresh from --refresh-interval: floored at 5 s when known, 30 s
// when unknown, and 60 s after a failure (one doubling of the 30 s default).
func TestRefreshSchedule_WorkerTranscriptInterval(t *testing.T) {
	cases := []struct {
		name  string
		value string // --refresh-interval value; "" = flag absent
		want  time.Duration
	}{
		{name: "60 seconds", value: "60", want: 60 * time.Second},
		{name: "2 seconds floored", value: "2", want: 5 * time.Second},
		{name: "2.5 seconds floored", value: "2.5", want: 5 * time.Second},
		{name: "absent", want: 30 * time.Second},
		{name: "zero", value: "0", want: 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupEnv(t)
			t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
			transcript := filepath.Join(t.TempDir(), "t.jsonl")
			if err := os.WriteFile(transcript, nil, 0o600); err != nil {
				t.Fatalf("write transcript: %v", err)
			}
			args := []string{"--once", "--session-id", "s1", "--transcript", transcript}
			if tc.value != "" {
				args = append(args, "--refresh-interval", tc.value)
			}
			if code := runRefresh(args, io.Discard, io.Discard); code != 0 {
				t.Fatalf("runRefresh(%v) = %d, want 0", args, code)
			}
			w := cache.LoadRefreshSchedule().Transcripts["s1"]
			if got := w.NextDueAt.Sub(w.LastAttempt); got != tc.want {
				t.Errorf("NextDueAt - LastAttempt = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("missing transcript counts as a failure", func(t *testing.T) {
		setupEnv(t)
		t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
		missing := filepath.Join(t.TempDir(), "missing.jsonl")
		args := []string{"--once", "--session-id", "s1", "--transcript", missing}
		if code := runRefresh(args, io.Discard, io.Discard); code != 0 {
			t.Fatalf("runRefresh(%v) = %d, want 0", args, code)
		}
		w := cache.LoadRefreshSchedule().Transcripts["s1"]
		if w.Failures != 1 {
			t.Errorf("Failures = %d, want 1", w.Failures)
		}
		if got := w.NextDueAt.Sub(w.LastAttempt); got != 60*time.Second {
			t.Errorf("NextDueAt - LastAttempt = %v, want 60s", got)
		}
	})
}

// TestRefreshSchedule_SourceDueNotDueEdit: a command source is spawned when
// due, not re-spawned while its schedule entry is in the future, and spawned
// again as soon as its definition is edited. The render itself never fetches.
func TestRefreshSchedule_SourceDueNotDueEdit(t *testing.T) {
	setupEnv(t)
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	loc := filepath.Join(t.TempDir(), "loc.json")
	if err := os.WriteFile(loc, []byte(`{"v":7}`), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	saveSources(t,
		sources.Definition{ID: "cmd", Kind: sources.KindCommand, Argv: []string{"never-run"}},
		sources.Definition{ID: "loc", Kind: sources.KindFile, Path: loc},
	)
	writeDocument(t, "claude-code", scheduleDoc(
		`<field name="source" source="cmd" select="v"/><field name="source" source="loc" select="v"/>`))

	var fetched []string
	stubFetch(t, func(_ context.Context, def sources.Definition, etag string) (sources.FetchResult, error) {
		fetched = append(fetched, def.ID+"|"+etag)
		return sources.FetchResult{Body: []byte(`{"v":7}`)}, nil
	})
	spawns := captureSpawns(t)

	// Render 1: cmd is due, so exactly one spawn for it; nothing fetched.
	renderClaude(t, nil)
	if len(*spawns) != 1 {
		t.Fatalf("render 1 spawns = %d, want 1", len(*spawns))
	}
	want := []string{"refresh", "--once", "--lease-id", "L", "--cc-version", testVersion, "--sources", "cmd"}
	if got := normalizeSpawn(t, (*spawns)[0]); !slices.Equal(got, want) {
		t.Errorf("render 1 args = %q, want %q", got, want)
	}
	if len(fetched) != 0 {
		t.Errorf("render fetched %v, want no fetches", fetched)
	}

	// The worker performs the fetch and releases the lease.
	runSpawned(t, (*spawns)[0])
	if !slices.Equal(fetched, []string{"cmd|"}) {
		t.Errorf("worker fetched %v, want [cmd|]", fetched)
	}

	// Render 2: cmd's next refresh is in the future, so no new spawn, and the
	// envelope's value is shown.
	out := renderClaude(t, nil)
	if len(*spawns) != 1 {
		t.Fatalf("render 2 spawns total = %d, want 1 (no new spawn)", len(*spawns))
	}
	if !strings.Contains(out, "7") {
		t.Errorf("render 2 stdout = %q, want the fetched value 7", out)
	}

	// Editing cmd's definition makes it due at once.
	saveSources(t,
		sources.Definition{ID: "cmd", Kind: sources.KindCommand, Argv: []string{"other"}},
		sources.Definition{ID: "loc", Kind: sources.KindFile, Path: loc},
	)
	renderClaude(t, nil)
	if len(*spawns) != 2 {
		t.Fatalf("render 3 spawns total = %d, want 2", len(*spawns))
	}
	if got := argAfter((*spawns)[1], "--sources"); got != "cmd" {
		t.Errorf("render 3 --sources = %q, want cmd", got)
	}
}

// TestRefreshSchedule_SortedSourceIDs: the due ids are handed over sorted.
func TestRefreshSchedule_SortedSourceIDs(t *testing.T) {
	setupEnv(t)
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	saveSources(t,
		sources.Definition{ID: "b-cmd", Kind: sources.KindCommand, Argv: []string{"never-run"}},
		sources.Definition{ID: "a-cmd", Kind: sources.KindCommand, Argv: []string{"never-run"}},
	)
	writeDocument(t, "claude-code", scheduleDoc(
		`<field name="source" source="b-cmd" select="v"/><field name="source" source="a-cmd" select="v"/>`))
	spawns := captureSpawns(t)
	renderClaude(t, nil)
	if len(*spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(*spawns))
	}
	if got := argAfter((*spawns)[0], "--sources"); got != "a-cmd,b-cmd" {
		t.Errorf("--sources = %q, want a-cmd,b-cmd", got)
	}
}

// TestRefreshSchedule_RenderNeverFetches: even with command and http sources
// due, the render path reads no fetcher; only the spawned worker may fetch.
func TestRefreshSchedule_RenderNeverFetches(t *testing.T) {
	setupEnv(t)
	t.Setenv("STATUSLOOM_NO_USAGE_API", "")
	loc := filepath.Join(t.TempDir(), "loc.json")
	if err := os.WriteFile(loc, []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	saveSources(t,
		sources.Definition{ID: "loc", Kind: sources.KindFile, Path: loc},
		sources.Definition{ID: "cmd", Kind: sources.KindCommand, Argv: []string{"never-run"}},
		sources.Definition{ID: "web", Kind: sources.KindHTTP, URL: "http://127.0.0.1:1/x"},
	)
	writeDocument(t, "claude-code", scheduleDoc(
		`<field name="source" source="loc" select="v"/>`+
			`<field name="source" source="cmd" select="v"/>`+
			`<field name="source" source="web" select="v"/>`))
	stubFetch(t, func(context.Context, sources.Definition, string) (sources.FetchResult, error) {
		t.Errorf("render must not fetch an external source")
		return sources.FetchResult{}, io.ErrUnexpectedEOF
	})
	spawns := captureSpawns(t)
	renderClaude(t, nil)
	if len(*spawns) != 1 {
		t.Fatalf("spawns = %d, want 1", len(*spawns))
	}
	if got := argAfter((*spawns)[0], "--sources"); got != "cmd,web" {
		t.Errorf("--sources = %q, want cmd,web", got)
	}
}

// TestRefreshSchedule_FileSourceEndToEnd: a file source renders its value
// without any spawn carrying --sources (file sources are read at render time).
func TestRefreshSchedule_FileSourceEndToEnd(t *testing.T) {
	setupEnv(t)
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	loc := filepath.Join(t.TempDir(), "loc.json")
	if err := os.WriteFile(loc, []byte(`{"v":"hello"}`), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	saveSources(t, sources.Definition{ID: "loc", Kind: sources.KindFile, Path: loc})
	writeDocument(t, "claude-code", scheduleDoc(`<field name="source" source="loc" select="v"/>`))
	spawns := captureSpawns(t)

	out := renderClaude(t, nil)
	if got := strings.TrimRight(out, "\n"); got != "hello" {
		t.Errorf("stdout = %q, want hello", got)
	}
	for _, args := range *spawns {
		if slices.Contains(args, "--sources") {
			t.Errorf("spawn %q carries --sources for a file source", args)
		}
	}
}

// TestRefreshSchedule_Probe: --probe fetches with the configured fetcher and
// prints one JSON line, and never touches the schedule or envelope cache.
func TestRefreshSchedule_Probe(t *testing.T) {
	setupEnv(t)
	t.Setenv("STATUSLOOM_NO_USAGE_API", "1")
	stubFetch(t, sources.Fetch)

	probeLine := func(t *testing.T, id string) sources.ProbeOutput {
		t.Helper()
		var out bytes.Buffer
		if code := runRefresh([]string{"--probe", id}, &out, io.Discard); code != 0 {
			t.Fatalf("runRefresh(--probe %s) = %d, want 0", id, code)
		}
		ls := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
		if len(ls) != 1 {
			t.Fatalf("probe stdout = %q, want exactly one line", out.String())
		}
		var got sources.ProbeOutput
		if err := json.Unmarshal([]byte(ls[0]), &got); err != nil {
			t.Fatalf("decode %q: %v", ls[0], err)
		}
		return got
	}
	sameJSON := func(t *testing.T, got json.RawMessage, want string) {
		t.Helper()
		var g, w any
		if err := json.Unmarshal(got, &g); err != nil {
			t.Fatalf("probe body %q: %v", got, err)
		}
		if err := json.Unmarshal([]byte(want), &w); err != nil {
			t.Fatalf("want body %q: %v", want, err)
		}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("probe body = %s, want %s", got, want)
		}
	}

	loc := filepath.Join(t.TempDir(), "loc.json")
	if err := os.WriteFile(loc, []byte(`{"v":"hello"}`), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"n":1}`))
	}))
	t.Cleanup(srv.Close)
	saveSources(t,
		sources.Definition{ID: "loc", Kind: sources.KindFile, Path: loc},
		sources.Definition{ID: "web", Kind: sources.KindHTTP, URL: srv.URL + "/x"},
	)

	fileOut := probeLine(t, "loc")
	if !fileOut.OK {
		t.Fatalf("file probe = %+v, want OK", fileOut)
	}
	sameJSON(t, fileOut.Body, `{"v":"hello"}`)

	webOut := probeLine(t, "web")
	if !webOut.OK {
		t.Fatalf("http probe = %+v, want OK", webOut)
	}
	sameJSON(t, webOut.Body, `{"n":1}`)
	if _, ok := sources.LoadEnvelope("web"); ok {
		t.Error("probe wrote a source cache envelope; it must not")
	}
	if n := len(cache.LoadRefreshSchedule().Sources); n != 0 {
		t.Errorf("probe wrote %d schedule entries, want 0", n)
	}

	var out bytes.Buffer
	if code := runRefresh([]string{"--probe", "nope"}, &out, io.Discard); code != 0 {
		t.Fatalf("runRefresh(--probe nope) = %d, want 0", code)
	}
	if want := `{"ok":false,"error":"unknown source"}` + "\n"; out.String() != want {
		t.Errorf("probe unknown id stdout = %q, want %q", out.String(), want)
	}
}
