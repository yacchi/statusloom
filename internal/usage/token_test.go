package usage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestToken_EnvVarWins(t *testing.T) {
	getenv := fakeGetenv(map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": "env-token-value",
	})

	token, err := Token(getenv)
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if token != "env-token-value" {
		t.Errorf("token = %q, want env-token-value", token)
	}
}

func withCredentialsPath(t *testing.T, path string) {
	t.Helper()
	original := credentialsPath
	credentialsPath = func(getenv func(string) string) string { return path }
	t.Cleanup(func() { credentialsPath = original })
}

func withReadKeychain(t *testing.T, fn func() ([]byte, error)) {
	t.Helper()
	original := readKeychain
	readKeychain = fn
	t.Cleanup(func() { readKeychain = original })
}

func TestToken_CredentialsFileValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")
	const body = `{"claudeAiOauth":{"accessToken":"file-token-value","refreshToken":"r","expiresAt":123,"scopes":["a"]}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write credentials file: %v", err)
	}
	withCredentialsPath(t, path)
	withReadKeychain(t, func() ([]byte, error) { return nil, ErrNoToken })

	getenv := fakeGetenv(nil)
	token, err := Token(getenv)
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if token != "file-token-value" {
		t.Errorf("token = %q, want file-token-value", token)
	}
}

func TestToken_CredentialsFileInvalidFallsThroughToKeychain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write credentials file: %v", err)
	}
	withCredentialsPath(t, path)
	withReadKeychain(t, func() ([]byte, error) {
		return []byte(`{"claudeAiOauth":{"accessToken":"keychain-token-value"}}`), nil
	})

	getenv := fakeGetenv(nil)
	token, err := Token(getenv)
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if token != "keychain-token-value" {
		t.Errorf("token = %q, want keychain-token-value", token)
	}
}

func TestToken_CredentialsFileMissingFallsThroughToKeychain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")
	withCredentialsPath(t, path)
	withReadKeychain(t, func() ([]byte, error) {
		return []byte(`{"claudeAiOauth":{"accessToken":"keychain-token-value"}}`), nil
	})

	getenv := fakeGetenv(nil)
	token, err := Token(getenv)
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if token != "keychain-token-value" {
		t.Errorf("token = %q, want keychain-token-value", token)
	}
}

func TestToken_KeychainEmptyAccessTokenFallsThrough(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")
	withCredentialsPath(t, path)
	withReadKeychain(t, func() ([]byte, error) {
		return []byte(`{"claudeAiOauth":{"accessToken":""}}`), nil
	})

	getenv := fakeGetenv(nil)
	_, err := Token(getenv)
	if !errors.Is(err, ErrNoToken) {
		t.Errorf("err = %v, want ErrNoToken", err)
	}
}

func TestToken_NoSourcesReturnsErrNoToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")
	withCredentialsPath(t, path)
	withReadKeychain(t, func() ([]byte, error) { return nil, ErrNoToken })

	getenv := fakeGetenv(nil)
	token, err := Token(getenv)
	if token != "" {
		t.Errorf("token = %q, want empty", token)
	}
	if !errors.Is(err, ErrNoToken) {
		t.Errorf("err = %v, want ErrNoToken", err)
	}
}

func TestToken_ErrorNeverEmbedsTokenValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")
	withCredentialsPath(t, path)

	const secret = "sk-ant-super-secret-value-should-not-leak"
	withReadKeychain(t, func() ([]byte, error) {
		return nil, errors.New("keychain lookup failed for " + secret)
	})

	getenv := fakeGetenv(nil)
	_, err := Token(getenv)
	if err == nil {
		t.Fatal("err is nil, want ErrNoToken")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("err = %v, leaks secret value", err)
	}
	if !errors.Is(err, ErrNoToken) {
		t.Errorf("err = %v, want ErrNoToken", err)
	}
}

func TestCredentialsPathDefault_UsesHomeEnv(t *testing.T) {
	getenv := fakeGetenv(map[string]string{"HOME": "/home/testuser"})
	got := credentialsPath(getenv)
	want := filepath.Join("/home/testuser", ".claude", ".credentials.json")
	if got != want {
		t.Errorf("credentialsPath = %q, want %q", got, want)
	}
}
