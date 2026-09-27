package usage

import (
	"crypto/sha256"
	"fmt"
)

// keychainServiceBase is the macOS keychain service name Claude Code uses
// for its stored OAuth credentials when CLAUDE_CONFIG_DIR is unset.
const keychainServiceBase = "Claude Code-credentials"

// keychainServiceName returns the macOS keychain service name Claude Code
// stores this CLAUDE_CONFIG_DIR's OAuth credentials under: "Claude
// Code-credentials" when CLAUDE_CONFIG_DIR is unset, or that suffixed with
// "-" + the first 8 hex digits of sha256(CLAUDE_CONFIG_DIR) when set. This
// is Claude Code's own naming rule (not something statusloom invented) -
// verified against real keychain entries on a machine using multiple
// CLAUDE_CONFIG_DIR profiles. The raw string is hashed as-is: no
// realpath/symlink resolution, matching Claude Code's own behavior.
//
// NOTE on Unicode normalization: Claude Code's own implementation
// normalizes CLAUDE_CONFIG_DIR to NFC (Unicode Normalization Form C) before
// hashing (confirmed by reading Claude Code's own CLI bundle). This
// function deliberately hashes the raw string as returned by getenv,
// WITHOUT NFC normalization: adding golang.org/x/text/unicode/norm just for
// this would violate this repo's "Go標準ライブラリのみ使用" policy, and the
// deviation only matters for CLAUDE_CONFIG_DIR values containing
// non-precomposed Unicode (e.g. decomposed combining characters) - all
// verified real-world test vectors are pure ASCII, where NFC normalization
// is a no-op, so this deviation is unobserved in practice.
func keychainServiceName(getenv func(string) string) string {
	dir := getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		return keychainServiceBase
	}
	// TODO(nfc): normalize dir to NFC before hashing once the dependency
	// decision is made. See doc comment above.
	sum := sha256.Sum256([]byte(dir))
	return fmt.Sprintf("%s-%x", keychainServiceBase, sum[:4])
}
