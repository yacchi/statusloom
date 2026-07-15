//go:build !darwin

package usage

// readKeychain is the seam used by Token to fetch Claude Code's stored
// credentials JSON from the platform keychain. There is no supported
// keychain integration outside darwin, so this always reports no token
// available.
var readKeychain = func() ([]byte, error) {
	return nil, ErrNoToken
}
