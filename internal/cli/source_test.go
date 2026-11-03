package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/sources"
)

// fakeProbe replaces the probeSource seam for one test and restores it on cleanup.
func fakeProbe(t *testing.T, fn func(ctx context.Context, id string) ([]byte, error)) {
	t.Helper()
	prev := probeSource
	probeSource = fn
	t.Cleanup(func() { probeSource = prev })
}

func sourcesJSONPath(t *testing.T) string {
	t.Helper()
	p, err := sources.Path()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func readSourcesJSON(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(sourcesJSONPath(t))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// writeAbsFile writes body to a file under t.TempDir and returns its absolute path.
func writeAbsFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSourceAddAndListFormats(t *testing.T) {
	setupEnv(t)

	out, _, code := runCLI(t, []string{"source", "add", "ccfile", "--file", "/tmp/s.json"}, nil, nil)
	if code != 0 || out != "added source ccfile (file)\n" {
		t.Fatalf("add file: code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, []string{"source", "add", "mycmd", "--command", "--ttl", "7", "--", "ccprofile", "sessions", "--json"}, nil, nil)
	if code != 0 || out != "added source mycmd (command)\nwarning: mycmd: ttl below 10s spawns a fetch very often\n" {
		t.Fatalf("add command: code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, []string{"source", "add", "api", "--url", "https://example.com/a.json", "--allow-remote"}, nil, nil)
	if code != 0 || !strings.Contains(out, "warning: api: allowRemote lets this source reach non-loopback hosts") {
		t.Fatalf("add url: code=%d out=%q", code, out)
	}

	out, _, code = runCLI(t, []string{"source", "list"}, nil, nil)
	want := "api\thttp\thttps://example.com/a.json\tttl=default\tWARN:allow-remote\n" +
		"ccfile\tfile\t/tmp/s.json\t-\n" +
		"mycmd\tcommand\tccprofile sessions --json\tttl=7s\tWARN:short-ttl\n"
	if code != 0 || out != want {
		t.Fatalf("list: code=%d out=%q, want %q", code, out, want)
	}
}

// Argv after -- is passed through verbatim, so a flag-like element such as
// --json is not parsed as an add flag.
func TestSourceAddArgvAfterDashDashIsNotParsed(t *testing.T) {
	setupEnv(t)

	_, _, code := runCLI(t, []string{"source", "add", "c", "--command", "--", "prog", "--json", "--force"}, nil, nil)
	if code != 0 {
		t.Fatalf("add: code=%d", code)
	}
	f, err := sources.Load()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := f.Get("c")
	if !ok || strings.Join(d.Argv, "|") != "prog|--json|--force" || d.TTLSeconds != 0 {
		t.Fatalf("argv = %v (ok=%v), want [prog --json --force]", d.Argv, ok)
	}
}

func TestSourceAddUsageErrorsWriteNothing(t *testing.T) {
	setupEnv(t)

	cases := [][]string{
		{"source", "add", "x", "--command"},
		{"source", "add", "x", "--file", "/a", "--", "y"},
		{"source", "add", "x", "--file", "/a", "--url", "http://h/"},
		{"source", "add", "x"},
		{"source", "add", "--file", "/a"},
		{"source", "add", "x", "--file", "/a", "--bogus"},
	}
	for _, args := range cases {
		_, stderr, code := runCLI(t, args, nil, nil)
		if code != 2 {
			t.Errorf("%v: code=%d stderr=%q, want 2", args, code, stderr)
		}
	}
	if _, err := os.Stat(sourcesJSONPath(t)); !os.IsNotExist(err) {
		t.Fatalf("sources.json written by usage error (stat err=%v)", err)
	}
}

func TestSourceAddRuntimeErrors(t *testing.T) {
	setupEnv(t)

	_, stderr, code := runCLI(t, []string{"source", "add", "ccfile", "--file", "rel/p.json"}, nil, nil)
	if code != 1 || !strings.Contains(stderr, `statusloom: source add: source "ccfile": path "rel/p.json" must be absolute (or start with ~/)`) {
		t.Fatalf("relative path: code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = runCLI(t, []string{"source", "add", "Bad", "--file", "/a"}, nil, nil)
	if code != 1 || !strings.Contains(stderr, `invalid source id "Bad"`) {
		t.Fatalf("bad id: code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(sourcesJSONPath(t)); !os.IsNotExist(err) {
		t.Fatalf("sources.json written by validation error (stat err=%v)", err)
	}
}

func TestSourceAddExistingNeedsForce(t *testing.T) {
	setupEnv(t)

	if _, _, code := runCLI(t, []string{"source", "add", "ccfile", "--file", "/a/s.json"}, nil, nil); code != 0 {
		t.Fatalf("first add: code=%d", code)
	}
	before := readSourcesJSON(t)

	_, stderr, code := runCLI(t, []string{"source", "add", "ccfile", "--file", "/b/s.json"}, nil, nil)
	if code != 1 || !strings.Contains(stderr, `already exists (use --force to replace)`) {
		t.Fatalf("add without force: code=%d stderr=%q", code, stderr)
	}
	if string(readSourcesJSON(t)) != string(before) {
		t.Fatal("sources.json changed by a rejected add")
	}

	out, _, code := runCLI(t, []string{"source", "add", "ccfile", "--file", "/b/s.json", "--force"}, nil, nil)
	if code != 0 || out != "replaced source ccfile (file)\n" {
		t.Fatalf("add --force: code=%d out=%q", code, out)
	}
	out, _, _ = runCLI(t, []string{"source", "list"}, nil, nil)
	if out != "ccfile\tfile\t/b/s.json\t-\n" {
		t.Fatalf("list after replace = %q", out)
	}
}

func TestSourceListEmpty(t *testing.T) {
	setupEnv(t)

	out, _, code := runCLI(t, []string{"source", "list"}, nil, nil)
	if code != 0 || out != "no sources defined ("+sourcesJSONPath(t)+")\n" {
		t.Fatalf("list: code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, []string{"source", "list", "--json"}, nil, nil)
	if code != 0 || out != "{\n  \"schemaVersion\": 1,\n  \"sources\": {}\n}\n" {
		t.Fatalf("list --json: code=%d out=%q", code, out)
	}
}

func TestSourceRemove(t *testing.T) {
	setupEnv(t)

	runCLI(t, []string{"source", "add", "ccfile", "--file", "/a/s.json"}, nil, nil)
	out, _, code := runCLI(t, []string{"source", "rm", "ccfile"}, nil, nil)
	if code != 0 || out != "removed source ccfile\n" {
		t.Fatalf("rm: code=%d out=%q", code, out)
	}
	_, stderr, code := runCLI(t, []string{"source", "rm", "nope"}, nil, nil)
	if code != 1 || !strings.Contains(stderr, `statusloom: source rm: unknown source "nope"`) {
		t.Fatalf("rm unknown: code=%d stderr=%q", code, stderr)
	}
}

func TestSourceTestSelectsFromFileWithPayload(t *testing.T) {
	setupEnv(t)

	body := writeAbsFile(t, "s.json", `{"sessions":[{"id":"3f9a2b1c-4d5e-6f70-8a9b-0c1d2e3f4a5b","name":"<a&b>"}]}`)
	runCLI(t, []string{"source", "add", "s", "--file", body}, nil, nil)

	sel := "sessions[id=${session_id}].name"
	out, stderr, code := runCLI(t, []string{"source", "test", "s", "--select", sel, "--payload", "../../fixtures/claude/full.json"}, nil, nil)
	if code != 0 || out != "\"<a&b>\"\n" {
		t.Fatalf("test with payload: code=%d out=%q stderr=%q", code, out, stderr)
	}

	_, stderr, code = runCLI(t, []string{"source", "test", "s", "--select", sel}, nil, nil)
	if code != 1 || !strings.Contains(stderr, "statusloom: source test: no match for select") {
		t.Fatalf("test without payload: code=%d stderr=%q", code, stderr)
	}
}

func TestSourceTestFileSourceNeverProbes(t *testing.T) {
	setupEnv(t)
	fakeProbe(t, func(context.Context, string) ([]byte, error) {
		t.Error("probeSource called for a file source")
		return nil, errors.New("unexpected probe")
	})

	body := writeAbsFile(t, "s.json", `{"a":1}`)
	runCLI(t, []string{"source", "add", "s", "--file", body}, nil, nil)
	out, stderr, code := runCLI(t, []string{"source", "test", "s"}, nil, nil)
	if code != 0 || out != "{\n  \"a\": 1\n}\n" {
		t.Fatalf("file test: code=%d out=%q stderr=%q", code, out, stderr)
	}
}

func TestSourceTestCommandSourceUsesProbe(t *testing.T) {
	setupEnv(t)

	var gotID string
	fakeProbe(t, func(_ context.Context, id string) ([]byte, error) {
		gotID = id
		return []byte(`{"a":1}`), nil
	})
	runCLI(t, []string{"source", "add", "mycmd", "--command", "--", "prog"}, nil, nil)

	out, stderr, code := runCLI(t, []string{"source", "test", "mycmd"}, nil, nil)
	if code != 0 || out != "{\n  \"a\": 1\n}\n" || gotID != "mycmd" {
		t.Fatalf("command test: code=%d out=%q id=%q stderr=%q", code, out, gotID, stderr)
	}

	fakeProbe(t, func(context.Context, string) ([]byte, error) {
		return nil, errors.New("boom")
	})
	_, stderr, code = runCLI(t, []string{"source", "test", "mycmd"}, nil, nil)
	if code != 1 || !strings.Contains(stderr, "boom") {
		t.Fatalf("probe error: code=%d stderr=%q", code, stderr)
	}
}

// test is a read-only check: it must not write the cache.
func TestSourceTestDoesNotTouchCache(t *testing.T) {
	setupEnv(t)
	fakeProbe(t, func(context.Context, string) ([]byte, error) {
		return []byte(`{"a":1}`), nil
	})

	body := writeAbsFile(t, "s.json", `{"a":1}`)
	runCLI(t, []string{"source", "add", "f", "--file", body}, nil, nil)
	runCLI(t, []string{"source", "add", "c", "--command", "--", "prog"}, nil, nil)
	runCLI(t, []string{"source", "test", "f"}, nil, nil)
	runCLI(t, []string{"source", "test", "c"}, nil, nil)

	entries, err := os.ReadDir(os.Getenv("STATUSLOOM_CACHE_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache dir has %d entries after source test, want 0", len(entries))
	}
}

func TestSourceTestErrorClasses(t *testing.T) {
	setupEnv(t)

	body := writeAbsFile(t, "s.json", `{"a":1}`)
	runCLI(t, []string{"source", "add", "s", "--file", body}, nil, nil)

	_, stderr, code := runCLI(t, []string{"source", "test", "s", "--select", "a]"}, nil, nil)
	if code != 1 || !strings.Contains(stderr, "select:") {
		t.Fatalf("bad select: code=%d stderr=%q", code, stderr)
	}

	notPayload := writeAbsFile(t, "p.json", `{}`)
	_, stderr, code = runCLI(t, []string{"source", "test", "s", "--payload", notPayload}, nil, nil)
	if code != 1 || !strings.Contains(stderr, "claude: not a Claude Code payload") {
		t.Fatalf("bad payload: code=%d stderr=%q", code, stderr)
	}

	_, _, code = runCLI(t, []string{"source", "test", "nope"}, nil, nil)
	if code != 1 {
		t.Fatalf("unknown id: code=%d, want 1", code)
	}
}

func TestSourceSubcommandUsageErrors(t *testing.T) {
	setupEnv(t)

	cases := [][]string{
		{"source"},
		{"source", "bogus"},
		{"source", "test"},
		{"source", "test", "--select", "a"},
		{"source", "rm"},
		{"source", "rm", "x", "--force"},
		{"source", "list", "extra"},
	}
	for _, args := range cases {
		_, _, code := runCLI(t, args, nil, nil)
		if code != 2 {
			t.Errorf("%v: code=%d, want 2", args, code)
		}
	}
}
