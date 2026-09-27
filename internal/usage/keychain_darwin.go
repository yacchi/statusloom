//go:build darwin

package usage

import "os/exec"

// readKeychain is the seam used by Token to fetch Claude Code's stored
// credentials JSON from the macOS keychain. Tests override this var
// directly rather than exec'ing the real `security` binary. The service
// name depends on CLAUDE_CONFIG_DIR (see keychainServiceName): Claude Code
// stores credentials under a different keychain entry per config
// directory, so getenv must be threaded through to find the right one.
var readKeychain = func(getenv func(string) string) ([]byte, error) {
	service := keychainServiceName(getenv)
	cmd := exec.Command("/usr/bin/security", "find-generic-password", "-s", service, "-w")
	return cmd.Output()
}
