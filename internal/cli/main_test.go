package cli

import (
	"context"
	"errors"
	"os"
	"testing"

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
	startRefreshProcess = func(args ...string) error { return nil }
	acquireUsageToken = func(getenv func(string) string) (string, error) {
		return "", usage.ErrNoToken
	}
	fetchUsage = func(ctx context.Context, token, version string) (*usage.Report, int, error) {
		return nil, 0, errors.New("fetchUsage stubbed in tests")
	}
	os.Exit(m.Run())
}
