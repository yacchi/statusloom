package sources

// This file is the render-time resolver for external sources. It only reads
// sources.json, file sources and the cache envelopes the refresh worker wrote.
// It never runs a command and never touches the network, so the render path
// cannot wait on either.

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// loadSources is a seam so tests can count how often sources.json is opened.
var loadSources = Load

// Provider resolves source ids for one render (or one preview). Results are
// memoized per id for the Provider's lifetime.
type Provider struct {
	now  time.Time
	ri   time.Duration
	file func() File

	mu   sync.Mutex
	memo map[string]lookupResult
}

// lookupResult is the memoized answer for one id.
type lookupResult struct {
	doc       any
	defined   bool
	available bool
}

// fileMemoEntry caches one decoded file source across renders. It is keyed by
// path and validated by the stat (modTime, size) taken on each lookup.
type fileMemoEntry struct {
	modTime time.Time
	size    int64
	doc     any
}

// fileMemo is process-wide: the render path runs once per statusline update,
// and re-decoding an unchanged file on every update would be wasted work.
var (
	fileMemoMu sync.Mutex
	fileMemo   = map[string]fileMemoEntry{}
)

// NewProvider defers Load() to the first Lookup or File call, so a document that
// references no source never opens sources.json. A Load error behaves as "no sources".
// refreshInterval is Claude Code's statusLine.refreshInterval (0 = unknown); it feeds
// Definition.StaleAfter.
func NewProvider(now time.Time, refreshInterval time.Duration) *Provider {
	file := sync.OnceValue(func() File {
		f, err := loadSources()
		if err != nil {
			return File{SchemaVersion: SchemaVersion, Sources: map[string]Definition{}}
		}
		return f
	})
	return &Provider{now: now, ri: refreshInterval, file: file, memo: map[string]lookupResult{}}
}

// NewProviderFromFile uses f instead of Load() (tests).
func NewProviderFromFile(f File, now time.Time, refreshInterval time.Duration) *Provider {
	return &Provider{
		now:  now,
		ri:   refreshInterval,
		file: func() File { return f },
		memo: map[string]lookupResult{},
	}
}

// File returns the definitions the provider uses (loading them on first use).
func (p *Provider) File() File {
	return p.file()
}

// Lookup has exactly the signature of render.Options.Sources. Memoized per id per
// Provider. It NEVER runs commands or network I/O:
//
//	undefined id -> (nil, false, false)
//	file         -> decoded file source, or (nil, true, false) when unusable or stale
//	command/http -> the cached envelope's body, or (nil, true, false) when unavailable
func (p *Provider) Lookup(id string) (doc any, defined bool, available bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.memo[id]; ok {
		return r.doc, r.defined, r.available
	}
	r := p.resolve(id)
	p.memo[id] = r
	return r.doc, r.defined, r.available
}

func (p *Provider) resolve(id string) lookupResult {
	def, ok := p.file().Get(id)
	if !ok {
		return lookupResult{}
	}
	unavailable := lookupResult{defined: true}
	switch def.Kind {
	case KindFile:
		return lookupFile(def, p.now, p.ri)
	case KindCommand, KindHTTP:
		env, ok := LoadEnvelope(id)
		if !ok || !Available(def, env, p.now, p.ri) {
			return unavailable
		}
		var doc any
		if err := json.Unmarshal(env.Body, &doc); err != nil {
			return unavailable
		}
		return lookupResult{doc: doc, defined: true, available: true}
	default:
		return unavailable
	}
}

// lookupFile resolves a file source. Every failure is "defined but unavailable".
// The decoded document is memoized process-wide, keyed by path and validated by the
// stat (modTime, size), so an unchanged file is not decoded again.
func lookupFile(def Definition, now time.Time, ri time.Duration) lookupResult {
	unavailable := lookupResult{defined: true}
	path, err := def.ExpandedPath()
	if err != nil {
		return unavailable
	}
	fi, err := os.Stat(path)
	if err != nil {
		return unavailable
	}
	if fi.Size() > MaxBodyBytes {
		return unavailable
	}
	if sa := def.StaleAfter(ri); sa > 0 && now.Sub(fi.ModTime()) > sa {
		return unavailable
	}

	fileMemoMu.Lock()
	entry, hit := fileMemo[path]
	fileMemoMu.Unlock()
	if hit && entry.modTime.Equal(fi.ModTime()) && entry.size == fi.Size() {
		return lookupResult{doc: entry.doc, defined: true, available: true}
	}

	body, _, err := ReadFileSource(path)
	if err != nil {
		return unavailable
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return unavailable
	}
	fileMemoMu.Lock()
	fileMemo[path] = fileMemoEntry{modTime: fi.ModTime(), size: fi.Size(), doc: doc}
	fileMemoMu.Unlock()
	return lookupResult{doc: doc, defined: true, available: true}
}
