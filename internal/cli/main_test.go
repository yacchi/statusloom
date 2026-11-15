package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/yacchi/statusloom/internal/testenv"
	"github.com/yacchi/statusloom/internal/usage"
)

// TestMain neutralizes the background refresh spawn for the whole cli test
// binary. In production startRefreshProcess re-execs os.Executable() (the
// statusloom binary); under `go test` os.Executable() is cli.test, and the
// child is launched as `cli.test refresh --once ...`. flag.Parse() stops at
// the "refresh" positional before it can reject the unknown flags, so the
// child re-runs the ENTIRE suite with no -test.run filter — every claude-path
// test then spawns another cli.test, fork-bombing the machine.
//
// Every render of the claude command (render.go -> maybeStartRefresh) reaches
// this seam, and the isolated per-test STATUSLOOM_CACHE_DIR always reports the
// session as due, so the spawn would fire on essentially every test. Stub it
// to a no-op; no test asserts real spawning behavior.
// The account-usage seams are stubbed to credential-free / network-free
// defaults so no cli test can accidentally read a real OAuth token or make a
// real HTTP request. acquireUsageToken reports "no token" and fetchUsage
// fails immediately; tests that exercise the fetch path override these vars
// locally and restore them.
func TestMain(m *testing.M) {
	// Hook-capture mode: Windows hook scripts are .cmd files that re-exec this
	// binary to copy stdin into a file (see writeHookScript in orca_test.go).
	if len(os.Args) == 3 && os.Args[1] == hookCaptureArg {
		f, err := os.Create(os.Args[2])
		if err == nil {
			_, err = io.Copy(f, os.Stdin)
			f.Close()
		}
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	startRefreshProcess = func(args ...string) (int, error) { return 0, nil }
	acquireUsageToken = func(getenv func(string) string) (string, error) {
		return "", usage.ErrNoToken
	}
	fetchUsage = func(ctx context.Context, token, version string) (*usage.Report, int, error) {
		return nil, 0, errors.New("fetchUsage stubbed in tests")
	}
	// testenv also points HOME/XDG/STATUSLOOM_*/CLAUDE_CONFIG_DIR at a temp
	// dir for the whole binary, so a test that forgets setupEnv cannot write
	// the developer's real config or cache.
	os.Exit(testenv.Run(m))
}
