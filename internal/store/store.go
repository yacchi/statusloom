// Package store is the single source of truth for statusloom's configuration:
// a cross-tool JSON store (<configDir>/statusloom.json) holding a normalized
// pool of committed revisions plus per-tool refs (current pointer + inline
// mutable draft working node), modeled on git's objects/refs
// (plans/config-store-and-format.md §3).
//
// Dependency direction is store -> dsl only. store does NOT import
// internal/config; config is the thin layer above store (config -> store), so
// a store -> config edge would be a cycle (§8.11). store therefore owns its
// own path resolution (path.go), atomic writer (atomic.go), and UUIDv7
// generator (uuid.go).
//
// This file holds the Phase A1 surface: Current / ReadDraft / WriteDraft /
// Save / SourceVersion plus WriteFileAtomic. The Phase B1 history DAG surface
// (Revisions / Revision / Restore, plus the rotation Save now applies) lives
// in history.go.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/yacchi/statusloom/internal/dsl"
)

// formatVersion is the on-disk schema version written to every store file.
const formatVersion = 1

// Meta is the human-authored revision metadata carried alongside the DSL
// source (never embedded in the DSL itself; §5). Notes is free-form prose
// (e.g. the exchange format's pre-fence Markdown, internal/exchange.Meta)
// rather than a single-line label like the other three fields.
type Meta struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

// Revision is one committed version of a tool's whole document. Its id is the
// key in the store's revisions map, so it is not repeated in the value.
type Revision struct {
	// Hash is sha256(tool + "\0" + source) as hex, used for dedup against the
	// tip and (via SourceVersion) as the echo-guard version. It is stored
	// explicitly rather than recomputed so a reader need not re-hash.
	Hash string `json:"hash"`
	// Tool is the tool this revision configures (e.g. "claude-code"). Because
	// ids are globally unique across tools, this field resolves a bare id back
	// to its tool.
	Tool string `json:"tool"`
	// Source is the raw DSL text (the exchange format), stored verbatim.
	Source string `json:"source"`
	// SavedAt is the commit time (RFC3339 on disk, UTC).
	SavedAt time.Time `json:"savedAt"`
	// Parent is the id this revision was derived from (the DAG edge), or nil
	// for a root revision.
	Parent *string `json:"parent"`
	// Origin records how the revision entered the store: "ui" | "cli" |
	// "import".
	Origin string `json:"origin"`
	// Meta is the carry-forward human metadata.
	Meta Meta `json:"meta"`
}

// Draft is the per-tool mutable working node: the in-progress edit that
// autosave overwrites (last-writer-wins), not yet frozen into a revision. A
// nil draft ref means "no unsaved edit (synced with current)".
type Draft struct {
	Source  string    `json:"source"`
	SavedAt time.Time `json:"savedAt"`
}

// ref is a tool's pair of pointers: the committed current revision id and the
// optional inline draft working node.
type ref struct {
	Current string `json:"current"`
	Draft   *Draft `json:"draft,omitempty"`
}

// storeFile is the serialized shape of statusloom.json.
type storeFile struct {
	FormatVersion int                 `json:"formatVersion"`
	Revisions     map[string]Revision `json:"revisions"`
	Refs          map[string]*ref     `json:"refs"`
}

// Store is a handle to a single statusloom.json file. It holds no cached
// state: every operation re-reads the file, mutates, and atomically rewrites
// it, so concurrent processes interleave with last-writer-wins semantics (the
// accepted v0.1 tradeoff; §3.1).
type Store struct {
	path string
}

// Open resolves the store path from the environment (STATUSLOOM_CONFIG / XDG /
// platform defaults, matching internal/config) and returns a handle. The file
// need not exist yet.
func Open() (*Store, error) {
	p, err := StorePath()
	if err != nil {
		return nil, err
	}
	return &Store{path: p}, nil
}

// OpenAt returns a handle to an explicit store-file path, bypassing
// environment resolution. Useful for tests and for callers that already hold a
// resolved path.
func OpenAt(path string) *Store {
	return &Store{path: path}
}

// Path returns the store file path this handle operates on.
func (s *Store) Path() string {
	return s.path
}

// SourceVersion is the single spelling of statusloom's source version /
// content hash: sha256(tool + "\0" + source) as hex. The "\0" separator keeps
// identical sources under different tools distinct. It is used both as a
// revision's Hash and as the GET/PUT echo-guard version that replaces
// webconfig's former sourceVersion (§3.3).
func SourceVersion(tool, src string) string {
	h := sha256.Sum256([]byte(tool + "\x00" + src))
	return hex.EncodeToString(h[:])
}

// load reads and unmarshals the store file, returning an empty (but non-nil)
// store when the file does not exist.
func (s *Store) load() (*storeFile, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &storeFile{
				FormatVersion: formatVersion,
				Revisions:     map[string]Revision{},
				Refs:          map[string]*ref{},
			}, nil
		}
		return nil, err
	}
	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, err
	}
	if sf.Revisions == nil {
		sf.Revisions = map[string]Revision{}
	}
	if sf.Refs == nil {
		sf.Refs = map[string]*ref{}
	}
	if sf.FormatVersion == 0 {
		sf.FormatVersion = formatVersion
	}
	return &sf, nil
}

// save atomically writes the store file with stable, indented JSON (map keys
// are sorted by encoding/json, so revisions land in id order = roughly
// chronological; §3.2).
func (s *Store) save(sf *storeFile) error {
	sf.FormatVersion = formatVersion
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteFileAtomic(s.path, data)
}

// Current returns the tool's committed current revision. ok is false (with no
// error) when the store, the tool's refs, or the pointed-to revision is
// absent — there is deliberately no DefaultDocument fallback here; that is the
// config layer's responsibility (§8.11). A read/parse failure is returned as
// err.
func (s *Store) Current(tool string) (rev Revision, id string, ok bool, err error) {
	sf, err := s.load()
	if err != nil {
		return Revision{}, "", false, err
	}
	r := sf.Refs[tool]
	if r == nil || r.Current == "" {
		return Revision{}, "", false, nil
	}
	rev, exists := sf.Revisions[r.Current]
	if !exists {
		return Revision{}, "", false, nil
	}
	return rev, r.Current, true, nil
}

// ReadDraft returns the tool's draft working-node source. ok is false (no
// error) when there is no draft; it never falls back to current (§8.11). A
// read failure is returned as err.
func (s *Store) ReadDraft(tool string) (src string, ok bool, err error) {
	sf, err := s.load()
	if err != nil {
		return "", false, err
	}
	r := sf.Refs[tool]
	if r == nil || r.Draft == nil {
		return "", false, nil
	}
	return r.Draft.Source, true, nil
}

// WriteDraft overwrites the tool's draft working node with src
// (last-writer-wins, no parse/validate; the draft tolerates in-progress,
// invalid input). It creates the tool's ref entry if needed.
func (s *Store) WriteDraft(tool, src string) error {
	sf, err := s.load()
	if err != nil {
		return err
	}
	r := sf.Refs[tool]
	if r == nil {
		r = &ref{}
		sf.Refs[tool] = r
	}
	r.Draft = &Draft{Source: src, SavedAt: time.Now().UTC()}
	return s.save(sf)
}

// Save is the validation boundary (§5): it parses and validates src via
// dsl.ParseAndValidate and, only when there are no error-severity diagnostics,
// freezes src into a new committed revision.
//
//   - On error diagnostics the save is rejected: revID is "", diags carries the
//     findings, err is nil, and the store file is left untouched (nothing was
//     written).
//   - When src's hash equals the current tip's hash the save is a dedup no-op:
//     no new revision is created and current does not move; any draft working
//     node is cleared (it is now synced with current). revID is the unchanged
//     current id.
//   - Otherwise a new revision is created with a fresh UUIDv7 id, hash,
//     parent = the previous current, the given origin, and meta; current is
//     advanced to it and the draft is cleared (§3.5). meta carry-forward: a
//     zero-value meta (Name/Description/Author/Notes all empty — what a plain
//     UI save or `fmt --write` passes) inherits the parent revision's meta
//     instead of blanking it; a non-zero meta (import, an explicit rename)
//     replaces it outright (§3, §8.11).
//
// diags is returned in the success and dedup cases too (it may hold warnings).
func (s *Store) Save(tool, src, origin string, meta Meta) (revID string, diags []dsl.Diagnostic, err error) {
	_, diags = dsl.ParseAndValidate(src)
	if dsl.HasErrors(diags) {
		return "", diags, nil
	}

	sf, err := s.load()
	if err != nil {
		return "", diags, err
	}

	hash := SourceVersion(tool, src)
	r := sf.Refs[tool]

	// Dedup: identical to the current tip AND no real meta change -> no new
	// revision. hash alone cannot detect a meta-only change (it is computed
	// from tool+source only), so a zero-value meta (carry-forward) or a meta
	// equal to the tip's own is required too; an explicit, different meta
	// (e.g. a re-import that only edited notes, or a rename) must still
	// create a new revision even when the source is byte-identical (§3.3).
	if r != nil && r.Current != "" {
		if tip, ok := sf.Revisions[r.Current]; ok && tip.Hash == hash {
			if meta == (Meta{}) || meta == tip.Meta {
				if r.Draft != nil {
					r.Draft = nil
					if err := s.save(sf); err != nil {
						return "", diags, err
					}
				}
				return r.Current, diags, nil
			}
		}
	}

	id, err := newUUIDv7()
	if err != nil {
		return "", diags, err
	}

	var parent *string
	var parentMeta Meta
	if r != nil && r.Current != "" {
		p := r.Current
		parent = &p
		if tip, ok := sf.Revisions[p]; ok {
			parentMeta = tip.Meta
		}
	}

	// Zero-value meta carries forward the parent's meta rather than blanking
	// it; a caller that wants to actually clear meta on a non-root revision
	// has no way to express that today, which matches every current call
	// site (webconfig's plain UI save, `fmt --write`) always passing the
	// zero value and never meaning "erase the name" (§3, §8.11).
	revMeta := meta
	if revMeta == (Meta{}) {
		revMeta = parentMeta
	}

	sf.Revisions[id] = Revision{
		Hash:    hash,
		Tool:    tool,
		Source:  src,
		SavedAt: time.Now().UTC(),
		Parent:  parent,
		Origin:  origin,
		Meta:    revMeta,
	}
	if r == nil {
		r = &ref{}
		sf.Refs[tool] = r
	}
	r.Current = id
	r.Draft = nil

	rotateTool(sf, tool)

	if err := s.save(sf); err != nil {
		return "", diags, err
	}
	return id, diags, nil
}
