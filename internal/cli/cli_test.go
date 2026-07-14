package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/cache"
)

// fixture reads a Claude Code stdin fixture from fixtures/claude/.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "claude", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

// envMap adapts a map to the getenv func(string) string signature Run
// expects, so tests can control COLUMNS without touching the real process
// environment.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// setupEnv points STATUSLOOM_CONFIG at a nonexistent path (so the render path
// falls back to the built-in default DSL document) and STATUSLOOM_CACHE_DIR at
// a fresh temp directory, isolating each test's cache from every other test's
// and from the developer's real ~/.cache/statusloom.
func setupEnv(t *testing.T) {
	t.Helper()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(t.TempDir(), "does-not-exist.json"))
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
}

func runCLI(t *testing.T, args []string, stdin []byte, env map[string]string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = Run(args, bytes.NewReader(stdin), &out, &errBuf, "test-version", envMap(env))
	return out.String(), errBuf.String(), code
}

// lines splits rendered stdout into its (trailing-newline-stripped) lines.
func lines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func TestRun_Claude_Full(t *testing.T) {
	setupEnv(t)
	data := fixture(t, "full.json")

	stdout, stderr, code := runCLI(t, []string{"claude"}, data, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	got := lines(stdout)
	if len(got) != 2 {
		t.Fatalf("stdout has %d lines, want 2:\n%q", len(got), stdout)
	}

	for _, want := range []string{"Opus 4.8", "high", "64,000", "$1.23"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("line 1 missing %q: %q", want, got[0])
		}
	}
	for _, want := range []string{"5h: 27%", "7d: 79%", "v2.1.200"} {
		if !strings.Contains(got[1], want) {
			t.Errorf("line 2 missing %q: %q", want, got[1])
		}
	}
	if !strings.Contains(stdout, "\x1b[") {
		t.Errorf("expected ANSI escapes in output (default colorLevel is ansi16), got %q", stdout)
	}
}

// rewriteResetsAt decodes a fixture and overwrites rate_limits.<window>.resets_at
// (epoch seconds). The fixtures carry absolute timestamps that eventually go
// stale in real time, so tests set them relative to the test run instead.
func rewriteResetsAt(t *testing.T, raw []byte, window string, resetsAt int64) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	rl, ok := doc["rate_limits"].(map[string]any)
	if !ok {
		t.Fatal("fixture has no rate_limits object")
	}
	w, ok := rl[window].(map[string]any)
	if !ok {
		t.Fatalf("fixture has no rate_limits.%s object", window)
	}
	w["resets_at"] = resetsAt
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal rewritten fixture: %v", err)
	}
	return out
}

func TestRun_AccountCache_FillsFromPreviousRun(t *testing.T) {
	setupEnv(t)

	// Make five_hour already expired and seven_day still in the future,
	// independent of the fixture's absolute timestamps: only the future
	// window may be filled from the cache on the second run.
	full := fixture(t, "full.json")
	full = rewriteResetsAt(t, full, "five_hour", 1000000000) // 2001, long past
	full = rewriteResetsAt(t, full, "seven_day", time.Now().Add(24*time.Hour).Unix())

	if _, stderr, code := runCLI(t, []string{"claude"}, full, nil); code != 0 {
		t.Fatalf("first run exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	cacheDir := os.Getenv("STATUSLOOM_CACHE_DIR")
	accountPath := filepath.Join(cacheDir, "account", "default.json")
	if _, err := os.Stat(accountPath); err != nil {
		t.Fatalf("expected account cache file at %s: %v", accountPath, err)
	}

	noRateLimits := fixture(t, "no-rate-limits.json")
	stdout, stderr, code := runCLI(t, []string{"claude"}, noRateLimits, nil)
	if code != 0 {
		t.Fatalf("second run exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	got := lines(stdout)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 lines, got:\n%q", stdout)
	}
	if !strings.Contains(got[1], "7d: 79%") {
		t.Errorf("line 2 = %q, want it to contain 7d: 79%% (future window filled from account cache)", got[1])
	}
	if strings.Contains(got[1], "5h:") {
		t.Errorf("line 2 = %q, want no 5h widget (expired cached window must not be filled)", got[1])
	}
}

func TestRun_AccountCache_SkipsExpiredWindow(t *testing.T) {
	setupEnv(t)

	// Pre-populate the account cache directly with one expired and one
	// future window; only the future one may render.
	now := time.Now()
	err := cache.StoreAccount("default", cache.AccountUsage{
		Source:     "test",
		ObservedAt: now,
		ExpiresAt:  now.Add(5 * time.Minute),
		FiveHour:   &cache.RateWindowState{UsedPercentage: 42, ResetsAt: now.Add(-time.Hour)},
		SevenDay:   &cache.RateWindowState{UsedPercentage: 63, ResetsAt: now.Add(48 * time.Hour)},
	})
	if err != nil {
		t.Fatalf("StoreAccount: %v", err)
	}

	stdout, stderr, code := runCLI(t, []string{"claude"}, fixture(t, "no-rate-limits.json"), nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	got := lines(stdout)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 lines, got:\n%q", stdout)
	}
	if !strings.Contains(got[1], "7d: 63%") {
		t.Errorf("line 2 = %q, want 7d: 63%% (future cached window)", got[1])
	}
	if strings.Contains(got[1], "5h:") {
		t.Errorf("line 2 = %q, want no 5h widget (expired cached window)", got[1])
	}
}

func TestRun_NoRateLimits_EmptyCache(t *testing.T) {
	setupEnv(t)
	data := fixture(t, "no-rate-limits.json")

	stdout, stderr, code := runCLI(t, []string{"claude"}, data, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}

	got := lines(stdout)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 lines, got:\n%q", stdout)
	}
	line2 := got[1]

	if strings.Contains(line2, "5h") || strings.Contains(line2, "7d") {
		t.Errorf("line 2 should hide usage widgets with no rate limits in stdin or cache: %q", line2)
	}
	if strings.Contains(stdout, " |  | ") {
		t.Errorf("output has a dangling doubled separator: %q", stdout)
	}
	if strings.HasPrefix(line2, "|") || strings.HasSuffix(line2, "|") {
		t.Errorf("line 2 has a leading/trailing separator: %q", line2)
	}
}

func TestRun_EarlySession_NoCrash(t *testing.T) {
	setupEnv(t)
	data := fixture(t, "early-session.json")

	_, stderr, code := runCLI(t, []string{"claude"}, data, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
}

func TestRun_MalformedStdin(t *testing.T) {
	setupEnv(t)

	stdout, stderr, code := runCLI(t, []string{"claude"}, []byte("not json"), nil)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.HasPrefix(stderr, "statusloom: ") {
		t.Errorf("stderr = %q, want prefix %q", stderr, "statusloom: ")
	}
}

func TestRun_Version(t *testing.T) {
	stdout, stderr, code := runCLI(t, []string{"version"}, nil, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	const want = "statusloom test-version\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	stdout, stderr, code := runCLI(t, []string{"bogus"}, nil, nil)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if stderr == "" {
		t.Errorf("expected usage text on stderr")
	}
}

func TestRun_NoArgs(t *testing.T) {
	_, stderr, code := runCLI(t, nil, nil, nil)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stderr == "" {
		t.Errorf("expected usage text on stderr")
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
}

// rewriteCwd decodes a fixture, overwrites its cwd (and workspace.current_dir,
// if present) to dir, and re-encodes it. The fixtures otherwise hard-code
// paths like /Users/dev/myapp that don't exist on the test machine.
func rewriteCwd(t *testing.T, raw []byte, dir string) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	doc["cwd"] = dir
	if ws, ok := doc["workspace"].(map[string]any); ok {
		ws["current_dir"] = dir
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal rewritten fixture: %v", err)
	}
	return out
}

func TestRun_GitStatus_NonRepoCwd(t *testing.T) {
	requireGit(t)
	setupEnv(t)

	nonRepoDir := t.TempDir()
	rewritten := rewriteCwd(t, fixture(t, "full.json"), nonRepoDir)

	stdout, stderr, code := runCLI(t, []string{"claude"}, rewritten, nil)
	if code != 0 {
		t.Fatalf("first run exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.Contains(stdout, "(+") {
		t.Errorf("expected no git-changes widget for a non-repo cwd, got: %q", stdout)
	}

	// A second render within the repo cache TTL must not crash - this
	// exercises the cache-read path (cache.LoadRepo), even though there is
	// no on-disk entry to hit since Collect never stored one for a non-repo
	// directory.
	stdout2, stderr2, code2 := runCLI(t, []string{"claude"}, rewritten, nil)
	if code2 != 0 {
		t.Fatalf("second run exit = %d, want 0 (stderr: %s)", code2, stderr2)
	}
	if strings.Contains(stdout2, "(+") {
		t.Errorf("expected no git-changes widget for a non-repo cwd on second run, got: %q", stdout2)
	}
}

// syncBuffer wraps bytes.Buffer with a mutex so it can be safely written
// from Run's goroutine (via webconfig's Stdout) while the test goroutine
// polls its contents concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRun_Config_E2E(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("sandbox does not permit loopback listeners: %v", err)
	}
	probe.Close()
	setupEnv(t)

	stdout := &syncBuffer{}
	var stderr bytes.Buffer

	codeCh := make(chan int, 1)
	go func() {
		codeCh <- Run([]string{"config", "--no-browser"}, nil, stdout, &stderr, "test-version", envMap(nil))
	}()

	const prefix = "Statusloom configurator: "
	var line string
	deadline := time.Now().Add(5 * time.Second)
	for {
		if s := stdout.String(); strings.Contains(s, prefix) {
			idx := strings.Index(s, prefix)
			rest := s[idx+len(prefix):]
			if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
				line = rest[:nl]
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for configurator URL line; stdout so far: %q", stdout.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	u, err := url.Parse(line)
	if err != nil {
		t.Fatalf("parsing configurator URL %q: %v", line, err)
	}
	port := u.Port()
	token := strings.TrimPrefix(u.Fragment, "token=")
	if port == "" || token == "" {
		t.Fatalf("could not parse port/token from URL %q", line)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%s/api/shutdown", port), nil)
	if err != nil {
		t.Fatalf("building shutdown request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/shutdown: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/shutdown status = %d, want 200", resp.StatusCode)
	}

	select {
	case code := <-codeCh:
		if code != 0 {
			t.Fatalf("Run(config) returned %d, want 0 (stderr: %s)", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Run(config) to return after shutdown")
	}
}

func TestRun_Config_BadPort(t *testing.T) {
	setupEnv(t)

	_, stderr, code := runCLI(t, []string{"config", "--port", "-1", "--no-browser"}, nil, nil)
	if code == 0 {
		t.Fatalf("exit code = 0, want nonzero (stderr: %s)", stderr)
	}
}

func TestRun_Config_JunkFlag(t *testing.T) {
	setupEnv(t)

	_, stderr, code := runCLI(t, []string{"config", "--this-flag-does-not-exist"}, nil, nil)
	if code == 0 {
		t.Fatalf("exit code = 0, want nonzero (stderr: %s)", stderr)
	}
}
