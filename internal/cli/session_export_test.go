package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yacchi/statusloom/internal/sessionexport"
)

const (
	sessionExportPID       = 4242
	sessionExportStartedAt = int64(1762149600)
)

// sessionExportStub installs the process-identify and stdin seams for one test.
// The identify stub counts its calls and succeeds with the fixed pid/startedAt
// when ok is true, or reports failure otherwise. stdin reports a directory, so the
// stdin check never mistakes the test runner's own os.Stdin for a manual run. The
// state dir is a fresh temp dir, and both seams are restored by t.Cleanup.
func sessionExportStub(t *testing.T, ok bool) *int {
	t.Helper()
	setupEnv(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	prevIdentify, prevStat := identifyClaudeProcess, stdinStat
	t.Cleanup(func() {
		identifyClaudeProcess = prevIdentify
		stdinStat = prevStat
	})

	dirInfo, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	stdinStat = func() (os.FileInfo, error) { return dirInfo, nil }

	calls := new(int)
	identifyClaudeProcess = func(func(string) string) (int, int64, bool) {
		*calls++
		if !ok {
			return 0, 0, false
		}
		return sessionExportPID, sessionExportStartedAt, true
	}
	return calls
}

func sessionExportDetailPath(t *testing.T) string {
	t.Helper()
	p, err := sessionexport.DetailPath(sessionExportPID, sessionExportStartedAt)
	if err != nil {
		t.Fatalf("DetailPath: %v", err)
	}
	return p
}

func sessionExportReadDetail(t *testing.T) sessionexport.Detail {
	t.Helper()
	data, err := os.ReadFile(sessionExportDetailPath(t))
	if err != nil {
		t.Fatalf("reading detail file: %v", err)
	}
	var d sessionexport.Detail
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	return d
}

func sessionExportAssertAbsent(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(sessionExportDetailPath(t)); !os.IsNotExist(err) {
		t.Errorf("detail file present or unreadable (err=%v), want absent", err)
	}
	if sessionexport.MergePending() {
		t.Error("merge marker present, want absent")
	}
}

// sessionExportCaptureServer answers the monitor emit with 204, as monitor_test.go does.
func sessionExportCaptureServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestSessionExportInvocations(t *testing.T) {
	cases := []struct {
		name      string
		args      func(url string) []string
		fixture   string
		wantCalls int
	}{
		{"claude", func(string) []string { return []string{"claude"} }, "full.json", 1},
		{"render", func(string) []string { return []string{"render"} }, "full.json", 1},
		{"monitor", func(u string) []string {
			return []string{"monitor", "--emit-url", u, "--token", "tok"}
		}, "full.json", 1},
		{"monitor --draft", func(u string) []string {
			return []string{"monitor", "--draft", "--emit-url", u, "--token", "tok"}
		}, "full.json", 1},
		{"claude --draft", func(string) []string { return []string{"claude", "--draft"} }, "full.json", 0},
		{"claude --set", func(string) []string {
			return []string{"claude", "--set", "extra-usage-limit-usd=50"}
		}, "full.json", 0},
		{"render --draft", func(string) []string { return []string{"render", "--draft"} }, "full.json", 0},
		{"render --set", func(string) []string {
			return []string{"render", "--set", "extra-usage-limit-usd=50"}
		}, "full.json", 0},
		{"claude-subagent", func(string) []string { return []string{"claude-subagent"} }, "subagent-running.json", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := sessionExportStub(t, true)
			url := sessionExportCaptureServer(t)
			_, stderr, code := runCLI(t, tc.args(url), fixture(t, tc.fixture), nil)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
			}
			if *calls != tc.wantCalls {
				t.Fatalf("identify calls = %d, want %d", *calls, tc.wantCalls)
			}
			if tc.wantCalls == 0 {
				sessionExportAssertAbsent(t)
				return
			}
			d := sessionExportReadDetail(t)
			if d.Version != 1 {
				t.Errorf("detail version = %d, want 1", d.Version)
			}
			if d.Row.PID != sessionExportPID || d.Row.StartedAt != sessionExportStartedAt {
				t.Errorf("row pid/startedAt = %d/%d, want %d/%d",
					d.Row.PID, d.Row.StartedAt, sessionExportPID, sessionExportStartedAt)
			}
			if d.Row.SessionID != "3f9a2b1c-4d5e-6f70-8a9b-0c1d2e3f4a5b" {
				t.Errorf("row sessionId = %q", d.Row.SessionID)
			}
		})
	}
}

func TestSessionExportStdinCheck(t *testing.T) {
	t.Run("regular file stdin skips export", func(t *testing.T) {
		calls := sessionExportStub(t, true)
		regular := filepath.Join(t.TempDir(), "stdin.json")
		if err := os.WriteFile(regular, fixture(t, "full.json"), 0o600); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(regular)
		if err != nil {
			t.Fatal(err)
		}
		stdinStat = func() (os.FileInfo, error) { return fi, nil }

		if _, _, code := runCLI(t, []string{"claude"}, fixture(t, "full.json"), nil); code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		if *calls != 0 {
			t.Errorf("identify calls = %d, want 0 for regular-file stdin", *calls)
		}
		sessionExportAssertAbsent(t)
	})

	t.Run("stdin stat error does not block export", func(t *testing.T) {
		calls := sessionExportStub(t, true)
		stdinStat = func() (os.FileInfo, error) { return nil, errors.New("stat failed") }

		if _, _, code := runCLI(t, []string{"claude"}, fixture(t, "full.json"), nil); code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		if *calls != 1 {
			t.Errorf("identify calls = %d, want 1", *calls)
		}
		sessionExportReadDetail(t)
	})
}

func TestSessionExportConfigDir(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		sessionExportStub(t, true)
		cfg := t.TempDir()
		runCLI(t, []string{"claude"}, fixture(t, "full.json"), map[string]string{"CLAUDE_CONFIG_DIR": cfg})
		if got := sessionExportReadDetail(t).Row.ConfigDir; got != cfg {
			t.Errorf("row configDir = %q, want %q", got, cfg)
		}
	})
	t.Run("unset", func(t *testing.T) {
		sessionExportStub(t, true)
		runCLI(t, []string{"claude"}, fixture(t, "full.json"), nil)
		if got := sessionExportReadDetail(t).Row.ConfigDir; got != "" {
			t.Errorf("row configDir = %q, want empty", got)
		}
	})
}

func TestSessionExportUnidentifiedIsSilent(t *testing.T) {
	sessionExportStub(t, false)
	runCLI(t, []string{"claude"}, fixture(t, "full.json"), nil)
	sessionExportAssertAbsent(t)
}

func TestSessionExportMarkerBeforeRenderReturns(t *testing.T) {
	sessionExportStub(t, true)
	if _, _, code := runCLI(t, []string{"claude"}, fixture(t, "full.json"), nil); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !sessionexport.MergePending() {
		t.Error("MergePending() = false after a live claude render, want true")
	}
}

// TestSessionExportNeverChangesOutput compares a render that exports with one whose
// identify fails. The output must match whether Observe succeeds or fails, because
// the export is silent.
func TestSessionExportNeverChangesOutput(t *testing.T) {
	data := fixture(t, "full.json")

	sessionExportStub(t, false)
	wantOut, wantErr, wantCode := runCLI(t, []string{"claude"}, data, nil)

	sessionExportStub(t, true)
	gotOut, gotErr, gotCode := runCLI(t, []string{"claude"}, data, nil)
	if gotOut != wantOut || gotErr != wantErr || gotCode != wantCode {
		t.Errorf("export changed render:\nstdout got=%q want=%q\nstderr got=%q want=%q\ncode got=%d want=%d",
			gotOut, wantOut, gotErr, wantErr, gotCode, wantCode)
	}
}

func TestSessionExportObserveFailureIsSilent(t *testing.T) {
	data := fixture(t, "full.json")

	sessionExportStub(t, false)
	wantOut, wantErr, wantCode := runCLI(t, []string{"claude"}, data, nil)

	sessionExportStub(t, true)
	// Make the state dir unusable: XDG_STATE_HOME points at a regular file, so
	// Observe's MkdirAll fails.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", blocker)

	gotOut, gotErr, gotCode := runCLI(t, []string{"claude"}, data, nil)
	if gotOut != wantOut || gotErr != wantErr || gotCode != wantCode {
		t.Errorf("failed export changed render:\nstdout got=%q want=%q\nstderr got=%q want=%q\ncode got=%d want=%d",
			gotOut, wantOut, gotErr, wantErr, gotCode, wantCode)
	}
}
