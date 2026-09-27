package usage

import "testing"

// Real-world verified vectors: these keychain service names were confirmed
// against actual macOS Keychain entries on a machine using multiple
// CLAUDE_CONFIG_DIR profiles (ccprofile). All three inputs are pure ASCII,
// so they cannot distinguish an NFC-normalizing implementation from a
// non-normalizing one (NFC is a no-op on ASCII); see the deviation noted
// in keychain_service.go's doc comment.
func TestKeychainServiceName(t *testing.T) {
	cases := []struct {
		name string
		dir  string
		want string
	}{
		{
			name: "unset CLAUDE_CONFIG_DIR uses base service name",
			dir:  "",
			want: "Claude Code-credentials",
		},
		{
			name: "max profile",
			dir:  "/Users/yasu/.config/ccprofile/profiles/max",
			want: "Claude Code-credentials-f540d886",
		},
		{
			name: "team profile",
			dir:  "/Users/yasu/.config/ccprofile/profiles/team",
			want: "Claude Code-credentials-5264dce6",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := fakeGetenv(map[string]string{"CLAUDE_CONFIG_DIR": tc.dir})
			got := keychainServiceName(getenv)
			if got != tc.want {
				t.Errorf("keychainServiceName(%q) = %q, want %q", tc.dir, got, tc.want)
			}
		})
	}
}
