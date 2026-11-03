// Package sources defines external JSON sources: named inputs that statusloom
// shows in a field without spelling their data in the DSL.
//
// Definitions live only in <configDir>/sources.json, never in the DSL. The
// render path only reads file sources and cache envelopes. command and http
// sources are fetched only by the refresh subprocess, so rendering never waits
// on a network request or an external command.
package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
)

// Kind is the kind of a source: where its JSON comes from.
type Kind string

const (
	KindFile    Kind = "file"
	KindCommand Kind = "command"
	KindHTTP    Kind = "http"
)

const (
	SchemaVersion   = 1
	MaxBodyBytes    = 1 << 20          // 1 MiB cap on file size / command stdout / HTTP body
	DefaultTTL      = 30 * time.Second // floor of the default fetch interval
	MinTTL          = 5 * time.Second
	ShortTTLWarning = 10 * time.Second // an explicit ttl below this triggers the "short-ttl" warning
	DefaultTimeout  = 2 * time.Second
	MaxTimeout      = 30 * time.Second
	MaxBackoff      = 10 * time.Minute
)

// Definition is one entry of sources.json. ID is the map key in the file and is
// not serialized inside the entry.
type Definition struct {
	ID                string   `json:"-"`
	Kind              Kind     `json:"kind"`
	Path              string   `json:"path,omitempty"`              // file
	Argv              []string `json:"argv,omitempty"`              // command (no shell)
	URL               string   `json:"url,omitempty"`               // http
	AllowRemote       bool     `json:"allowRemote,omitempty"`       // http only
	TTLSeconds        int      `json:"ttlSeconds,omitempty"`        // command/http; 0 = default
	StaleAfterSeconds int      `json:"staleAfterSeconds,omitempty"` // 0 = default (see StaleAfter)
	TimeoutSeconds    int      `json:"timeoutSeconds,omitempty"`    // command/http; 0 = DefaultTimeout
}

// TTL is the fetch interval of a command/http source: TTLSeconds when set, else
// max(DefaultTTL, refreshInterval) (plans/external-sources.md 2026-11-03 item 4).
// refreshInterval is Claude Code's statusLine.refreshInterval; <= 0 means unknown.
func (d Definition) TTL(refreshInterval time.Duration) time.Duration {
	if d.TTLSeconds > 0 {
		return time.Duration(d.TTLSeconds) * time.Second
	}
	if refreshInterval <= 0 {
		return DefaultTTL
	}
	return max(DefaultTTL, refreshInterval)
}

// StaleAfter: command/http -> StaleAfterSeconds when set, else 3*TTL(refreshInterval);
// file -> StaleAfterSeconds (0 = never stale; a file's age is its mtime).
func (d Definition) StaleAfter(refreshInterval time.Duration) time.Duration {
	explicit := time.Duration(d.StaleAfterSeconds) * time.Second
	if d.Kind == KindFile {
		return explicit
	}
	if explicit > 0 {
		return explicit
	}
	return 3 * d.TTL(refreshInterval)
}

// Timeout is the per-fetch deadline of a command/http source.
func (d Definition) Timeout() time.Duration {
	if d.TimeoutSeconds == 0 {
		return DefaultTimeout
	}
	return time.Duration(d.TimeoutSeconds) * time.Second
}

// ExpandedPath expands a leading "~/" (or a bare "~") to the user's home directory.
// Other forms, including "~user/x", are returned unchanged. The home directory is
// looked up only when a "~" form is present.
func (d Definition) ExpandedPath() (string, error) {
	if d.Path != "~" && !strings.HasPrefix(d.Path, "~/") {
		return d.Path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand ~: %w", err)
	}
	if d.Path == "~" {
		return home, nil
	}
	return filepath.Join(home, d.Path[len("~/"):]), nil
}

// Hash identifies WHAT the definition fetches: the first 16 hex characters of
// sha256 over the encoding/json encoding of
// struct{Kind Kind; Path string; Argv []string; URL string; AllowRemote bool}{...}.
// TTL/staleAfter/timeout are not part of it (changing them keeps the cache).
func (d Definition) Hash() string {
	b, err := json.Marshal(struct {
		Kind        Kind
		Path        string
		Argv        []string
		URL         string
		AllowRemote bool
	}{d.Kind, d.Path, d.Argv, d.URL, d.AllowRemote})
	if err != nil {
		// The struct holds only strings, bools and string slices, which always marshal.
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// Warnings returns warning codes, in this order: "allow-remote" (http with AllowRemote),
// "short-ttl" (command/http with 0 < TTLSeconds*time.Second < ShortTTLWarning). nil when none.
func (d Definition) Warnings() []string {
	var out []string
	if d.Kind == KindHTTP && d.AllowRemote {
		out = append(out, "allow-remote")
	}
	if (d.Kind == KindCommand || d.Kind == KindHTTP) &&
		d.TTLSeconds > 0 && time.Duration(d.TTLSeconds)*time.Second < ShortTTLWarning {
		out = append(out, "short-ttl")
	}
	return out
}

// Validate returns the first violation (checked in the order listed below) with its exact text.
func (d Definition) Validate() error {
	if !dsl.ValidSourceID(d.ID) {
		return fmt.Errorf("invalid source id %q: use 1-64 characters of a-z, 0-9 and -, starting with a letter", d.ID)
	}

	switch d.Kind {
	case KindFile:
		if d.Path == "" {
			return fmt.Errorf("source %q: file sources need a path", d.ID)
		}
		if !filepath.IsAbs(d.Path) && d.Path != "~" && !strings.HasPrefix(d.Path, "~/") {
			return fmt.Errorf("source %q: path %q must be absolute (or start with ~/)", d.ID, d.Path)
		}
	case KindCommand:
		if len(d.Argv) == 0 || d.Argv[0] == "" {
			return fmt.Errorf("source %q: command sources need a non-empty argv", d.ID)
		}
	case KindHTTP:
		u, err := url.Parse(d.URL)
		if err != nil || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") || u.Host == "" {
			return fmt.Errorf("source %q: url %q must be an http or https URL with a host", d.ID, d.URL)
		}
	default:
		return fmt.Errorf("source %q: unknown kind %q (want file, command or http)", d.ID, d.Kind)
	}

	inapplicable := []struct {
		name string
		set  bool
	}{
		{"path", d.Path != "" && d.Kind != KindFile},
		{"argv", len(d.Argv) > 0 && d.Kind != KindCommand},
		{"url", d.URL != "" && d.Kind != KindHTTP},
		{"allowRemote", d.AllowRemote && d.Kind != KindHTTP},
		{"ttlSeconds", d.TTLSeconds != 0 && d.Kind == KindFile},
		{"timeoutSeconds", d.TimeoutSeconds != 0 && d.Kind == KindFile},
	}
	for _, a := range inapplicable {
		if a.set {
			return fmt.Errorf("source %q: %s does not apply to %s sources", d.ID, a.name, d.Kind)
		}
	}

	if d.TTLSeconds != 0 && d.TTLSeconds < int(MinTTL/time.Second) {
		return fmt.Errorf("source %q: ttlSeconds must be at least %d", d.ID, int(MinTTL/time.Second))
	}

	if d.StaleAfterSeconds < 0 {
		return fmt.Errorf("source %q: staleAfterSeconds must not be negative", d.ID)
	}
	if d.Kind != KindFile && d.StaleAfterSeconds != 0 {
		floor := int(DefaultTTL / time.Second)
		if d.TTLSeconds != 0 {
			floor = d.TTLSeconds
		}
		if d.StaleAfterSeconds < floor {
			return fmt.Errorf("source %q: staleAfterSeconds must be at least ttlSeconds", d.ID)
		}
	}

	if d.TimeoutSeconds != 0 && (d.TimeoutSeconds < 1 || d.TimeoutSeconds > int(MaxTimeout/time.Second)) {
		return fmt.Errorf("source %q: timeoutSeconds must be between 1 and %d", d.ID, int(MaxTimeout/time.Second))
	}
	return nil
}

// FetchResult is what one fetch produced.
type FetchResult struct {
	Body        []byte // valid JSON, <= MaxBodyBytes; nil when NotModified
	ETag        string // http only; "" otherwise
	NotModified bool   // http 304
}

// Fetcher performs one live fetch of def; etag is the ETag to send ("" for none).
// The production implementation is Fetch (internal/sources/fetch.go).
type Fetcher func(ctx context.Context, def Definition, etag string) (FetchResult, error)

// ReadFileSource reads a local JSON file: stat -> size > MaxBodyBytes is an error ->
// read -> must be valid JSON (json.Valid). Returns the bytes and the file's ModTime.
// Local file access only (allowed on the render path).
func ReadFileSource(path string) (body []byte, modTime time.Time, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("stat source file %s: %w", path, err)
	}
	if info.Size() > MaxBodyBytes {
		return nil, time.Time{}, fmt.Errorf("source file %s is %d bytes; the limit is %d", path, info.Size(), MaxBodyBytes)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("open source file %s: %w", path, err)
	}
	defer f.Close()

	// Read one byte past the cap so a file that grew after the stat is still caught.
	body, err = io.ReadAll(io.LimitReader(f, MaxBodyBytes+1))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read source file %s: %w", path, err)
	}
	if len(body) > MaxBodyBytes {
		return nil, time.Time{}, fmt.Errorf("source file %s is %d bytes; the limit is %d", path, len(body), MaxBodyBytes)
	}
	if !json.Valid(body) {
		return nil, time.Time{}, fmt.Errorf("source file %s is not valid JSON", path)
	}
	return body, info.ModTime(), nil
}
