package sessionexport

import (
	"testing"

	"github.com/yacchi/statusloom/internal/testenv"
)

// TestMain isolates HOME, XDG dirs, STATUSLOOM_CONFIG/STATUSLOOM_CACHE_DIR and
// CLAUDE_CONFIG_DIR for the whole test binary, so a test that forgets its own
// isolation cannot write the developer's real config or cache.
func TestMain(m *testing.M) { testenv.Main(m) }
