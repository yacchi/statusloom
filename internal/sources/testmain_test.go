package sources

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/yacchi/statusloom/internal/testenv"
)

// fetchTestHelperEnv makes the test binary act as a command source instead of
// running tests: it prints its arguments (os.Args[1:]) as a JSON array and
// exits. Tests re-exec os.Args[0] with it set to observe exactly the argv a
// command source receives, on every OS, without depending on Unix tools.
const fetchTestHelperEnv = "STATUSLOOM_SOURCES_TEST_ARGV_HELPER"

// TestMain isolates HOME, XDG dirs, STATUSLOOM_CONFIG/STATUSLOOM_CACHE_DIR and
// CLAUDE_CONFIG_DIR for the whole test binary, so a test that forgets its own
// isolation cannot write the developer's real config or cache.
func TestMain(m *testing.M) {
	if os.Getenv(fetchTestHelperEnv) == "1" {
		if err := json.NewEncoder(os.Stdout).Encode(os.Args[1:]); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	testenv.Main(m)
}
