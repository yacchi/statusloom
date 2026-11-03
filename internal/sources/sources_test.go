package sources

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTTL(t *testing.T) {
	tests := []struct {
		name string
		def  Definition
		ri   time.Duration
		want time.Duration
	}{
		{"default with unknown refresh", Definition{}, 0, 30 * time.Second},
		{"default follows longer refresh", Definition{}, 60 * time.Second, 60 * time.Second},
		{"default floored at 30s", Definition{}, 10 * time.Second, 30 * time.Second},
		{"explicit ttl wins over refresh", Definition{TTLSeconds: 7}, 60 * time.Second, 7 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.def.TTL(tt.ri); got != tt.want {
				t.Fatalf("TTL(%v) = %v, want %v", tt.ri, got, tt.want)
			}
		})
	}
}

func TestStaleAfter(t *testing.T) {
	tests := []struct {
		name string
		def  Definition
		want time.Duration
	}{
		{"command default is 3*TTL", Definition{Kind: KindCommand, Argv: []string{"a"}}, 180 * time.Second},
		{"command explicit", Definition{Kind: KindCommand, Argv: []string{"a"}, StaleAfterSeconds: 100}, 100 * time.Second},
		{"http default is 3*TTL", Definition{Kind: KindHTTP, URL: "https://h/x"}, 180 * time.Second},
		{"file default never stale", Definition{Kind: KindFile, Path: "/x"}, 0},
		{"file explicit", Definition{Kind: KindFile, Path: "/x", StaleAfterSeconds: 90}, 90 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.def.StaleAfter(60 * time.Second); got != tt.want {
				t.Fatalf("StaleAfter = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	if got := (Definition{}).Timeout(); got != DefaultTimeout {
		t.Fatalf("default Timeout = %v, want %v", got, DefaultTimeout)
	}
	if got := (Definition{TimeoutSeconds: 5}).Timeout(); got != 5*time.Second {
		t.Fatalf("Timeout(5) = %v, want 5s", got)
	}
}

func TestExpandedPath(t *testing.T) {
	t.Setenv("HOME", "/h")
	tests := map[string]string{
		"~":      "/h",
		"~/a/b":  "/h/a/b",
		"/x":     "/x",
		"~bob/x": "~bob/x",
		"rel/x":  "rel/x",
	}
	for in, want := range tests {
		got, err := (Definition{Path: in}).ExpandedPath()
		if err != nil {
			t.Fatalf("ExpandedPath(%q) error: %v", in, err)
		}
		if got != want {
			t.Errorf("ExpandedPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHashIdentity(t *testing.T) {
	base := Definition{ID: "a", Kind: KindCommand, Argv: []string{"x", "y"}, TTLSeconds: 10, StaleAfterSeconds: 40, TimeoutSeconds: 3}
	h := base.Hash()
	if len(h) != 16 || strings.ToLower(h) != h {
		t.Fatalf("Hash() = %q, want 16 lowercase hex characters", h)
	}

	sameFetch := base
	sameFetch.ID = "other"
	sameFetch.TTLSeconds = 0
	sameFetch.StaleAfterSeconds = 0
	sameFetch.TimeoutSeconds = 0
	if sameFetch.Hash() != h {
		t.Errorf("Hash changed with ID/ttl/staleAfter/timeout, which must not be part of it")
	}

	changed := []Definition{
		{Kind: KindFile, Argv: base.Argv},
		{Kind: KindCommand, Argv: []string{"x", "z"}},
		{Kind: KindHTTP, URL: "https://h/x"},
		{Kind: KindHTTP, URL: "https://h/x", AllowRemote: true},
		{Kind: KindFile, Path: "/p"},
	}
	for i, d := range changed {
		if d.Hash() == h {
			t.Errorf("case %d: Hash did not change when the fetched target changed", i)
		}
	}
}

func TestWarnings(t *testing.T) {
	tests := []struct {
		name string
		def  Definition
		want []string
	}{
		{"http remote and short ttl", Definition{Kind: KindHTTP, URL: "https://h/x", AllowRemote: true, TTLSeconds: 5}, []string{"allow-remote", "short-ttl"}},
		{"command ttl at threshold", Definition{Kind: KindCommand, Argv: []string{"a"}, TTLSeconds: 10}, nil},
		{"command ttl below threshold", Definition{Kind: KindCommand, Argv: []string{"a"}, TTLSeconds: 9}, []string{"short-ttl"}},
		{"file", Definition{Kind: KindFile, Path: "/x"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.def.Warnings()
			if (got == nil) != (tt.want == nil) || strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("Warnings() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		def  Definition
		want string // empty = valid
	}{
		{"id checked before kind", Definition{ID: "Bad", Kind: "x"}, `invalid source id "Bad": use 1-64 characters of a-z, 0-9 and -, starting with a letter`},
		{"unknown kind", Definition{ID: "s", Kind: "ftp"}, `source "s": unknown kind "ftp" (want file, command or http)`},
		{"file without path", Definition{ID: "s", Kind: KindFile}, `source "s": file sources need a path`},
		{"file relative path", Definition{ID: "s", Kind: KindFile, Path: "rel/x"}, `source "s": path "rel/x" must be absolute (or start with ~/)`},
		{"file ~user is not home-relative", Definition{ID: "s", Kind: KindFile, Path: "~bob/x"}, `source "s": path "~bob/x" must be absolute (or start with ~/)`},
		{"file ~/ path passes", Definition{ID: "s", Kind: KindFile, Path: "~/x"}, ""},
		{"file bare ~ passes", Definition{ID: "s", Kind: KindFile, Path: "~"}, ""},
		{"file absolute passes", Definition{ID: "s", Kind: KindFile, Path: "/x"}, ""},
		{"command empty argv[0]", Definition{ID: "s", Kind: KindCommand, Argv: []string{""}}, `source "s": command sources need a non-empty argv`},
		{"http scheme", Definition{ID: "s", Kind: KindHTTP, URL: "ftp://h/x"}, `source "s": url "ftp://h/x" must be an http or https URL with a host`},
		{"http empty host", Definition{ID: "s", Kind: KindHTTP, URL: "http:///x"}, `source "s": url "http:///x" must be an http or https URL with a host`},
		{"http unparsable", Definition{ID: "s", Kind: KindHTTP, URL: "::"}, `source "s": url "::" must be an http or https URL with a host`},
		{"http passes", Definition{ID: "s", Kind: KindHTTP, URL: "https://h/x"}, ""},
		{"ttl on file", Definition{ID: "s", Kind: KindFile, Path: "/x", TTLSeconds: 60}, `source "s": ttlSeconds does not apply to file sources`},
		{"allowRemote on command", Definition{ID: "s", Kind: KindCommand, Argv: []string{"a"}, AllowRemote: true}, `source "s": allowRemote does not apply to command sources`},
		{"ttl below minimum", Definition{ID: "s", Kind: KindCommand, Argv: []string{"a"}, TTLSeconds: 4}, `source "s": ttlSeconds must be at least 5`},
		{"negative ttl", Definition{ID: "s", Kind: KindCommand, Argv: []string{"a"}, TTLSeconds: -1}, `source "s": ttlSeconds must be at least 5`},
		{"negative staleAfter", Definition{ID: "s", Kind: KindCommand, Argv: []string{"a"}, StaleAfterSeconds: -1}, `source "s": staleAfterSeconds must not be negative`},
		{"staleAfter below default floor", Definition{ID: "s", Kind: KindCommand, Argv: []string{"a"}, StaleAfterSeconds: 20}, `source "s": staleAfterSeconds must be at least ttlSeconds`},
		{"staleAfter equal to ttl passes", Definition{ID: "s", Kind: KindCommand, Argv: []string{"a"}, TTLSeconds: 10, StaleAfterSeconds: 10}, ""},
		{"file staleAfter any positive passes", Definition{ID: "s", Kind: KindFile, Path: "/x", StaleAfterSeconds: 1}, ""},
		{"timeout above max", Definition{ID: "s", Kind: KindCommand, Argv: []string{"a"}, TimeoutSeconds: 31}, `source "s": timeoutSeconds must be between 1 and 30`},
		{"timeout negative", Definition{ID: "s", Kind: KindCommand, Argv: []string{"a"}, TimeoutSeconds: -1}, `source "s": timeoutSeconds must be between 1 and 30`},
		{"timeout max passes", Definition{ID: "s", Kind: KindCommand, Argv: []string{"a"}, TimeoutSeconds: 30}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.def.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Validate() = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestReadFileSource(t *testing.T) {
	dir := t.TempDir()

	valid := filepath.Join(dir, "valid.json")
	if err := os.WriteFile(valid, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(valid)
	if err != nil {
		t.Fatal(err)
	}
	body, mod, err := ReadFileSource(valid)
	if err != nil {
		t.Fatalf("ReadFileSource(valid) error: %v", err)
	}
	if string(body) != `{"a":1}` || !mod.Equal(info.ModTime()) {
		t.Fatalf("ReadFileSource(valid) = %q, %v; want %q, %v", body, mod, `{"a":1}`, info.ModTime())
	}

	exact := filepath.Join(dir, "exact.json")
	payload := `"` + strings.Repeat("a", MaxBodyBytes-2) + `"`
	if err := os.WriteFile(exact, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadFileSource(exact); err != nil {
		t.Fatalf("ReadFileSource(exactly MaxBodyBytes) error: %v", err)
	}

	tooBig := filepath.Join(dir, "big.json")
	if err := os.WriteFile(tooBig, []byte(`"`+strings.Repeat("a", MaxBodyBytes)+`"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadFileSource(tooBig); err == nil || !strings.Contains(err.Error(), "the limit is") {
		t.Fatalf("ReadFileSource(MaxBodyBytes+1) = %v, want a size-limit error", err)
	}

	invalid := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(invalid, []byte(`{bad`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadFileSource(invalid); err == nil || err.Error() != "source file "+invalid+" is not valid JSON" {
		t.Fatalf("ReadFileSource(invalid) = %v, want not-valid-JSON error", err)
	}

	if _, _, err := ReadFileSource(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("ReadFileSource(missing) = nil error, want error")
	}
}

// Guards the encoding contract that sources.json entries rely on: the ID is never
// serialized into the entry, and kind is the JSON "kind" key.
func TestDefinitionJSONOmitsID(t *testing.T) {
	b, err := json.Marshal(Definition{ID: "s", Kind: KindFile, Path: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"kind":"file"`)) || bytes.Contains(b, []byte(`"s"`)) {
		t.Fatalf("Marshal = %s, want kind key and no ID", b)
	}
}
