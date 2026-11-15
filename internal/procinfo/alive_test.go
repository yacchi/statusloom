package procinfo

import (
	"os"
	"testing"

	"github.com/yacchi/statusloom/internal/testenv"
)

func TestAlive(t *testing.T) {
	if !Alive(os.Getpid()) {
		t.Errorf("Alive(self) = false")
	}
	if Alive(0) || Alive(-1) {
		t.Errorf("Alive(<= 0) = true, want false")
	}
	if pid := testenv.ExitedPID(t); Alive(pid) {
		t.Errorf("Alive(exited child %d) = true, want false", pid)
	}
}
