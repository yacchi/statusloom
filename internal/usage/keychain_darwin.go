//go:build darwin

package usage

import "os/exec"

// readKeychain is the seam used by Token to fetch Claude Code's stored
// credentials JSON from the macOS keychain. Tests override this var
// directly rather than exec'ing the real `security` binary.
var readKeychain = func() ([]byte, error) {
	cmd := exec.Command("/usr/bin/security", "find-generic-password", "-s", "Claude Code-credentials", "-w")
	return cmd.Output()
}
