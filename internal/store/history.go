package store

// This file is the Phase B1 surface on top of the Phase A1 store (store.go):
// the history DAG's read-side (Revisions/Revision), restore (Restore), and
// rotation (rotateTool, wired into Save) — plans/config-store-and-format.md
// §4 and §8.11.

import (
	"fmt"
	"sort"
)

// maxRevisionsPerTool is the rotation retention count N (§4.4). It is a
// constant (not yet configurable) so this is the single place to change it.
const maxRevisionsPerTool = 50

// RevisionWithID pairs a Revision with the id that is otherwise only
// implicit as the revisions map's key: Revision itself deliberately never
// carries its own id on disk (§3.2), but a revision handed out on its own —
// a history listing entry, a single lookup result — needs one attached.
type RevisionWithID struct {
	ID string `json:"id"`
	Revision
}

// Revisions returns every revision belonging to tool, ordered oldest-first by
// SavedAt (ties broken by id ascending) — the display order for `statusloom
// history list` and GET /api/history (§3.3, §8.11, §8.12). The listing is
// flat: it reflects every revision the DAG's parent pointers currently
// connect (including branches left behind by a past restore, §4.2), not a
// single linear chain.
func (s *Store) Revisions(tool string) ([]RevisionWithID, error) {
	sf, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]RevisionWithID, 0, len(sf.Revisions))
	for id, rev := range sf.Revisions {
		if rev.Tool != tool {
			continue
		}
		out = append(out, RevisionWithID{ID: id, Revision: rev})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].SavedAt.Equal(out[j].SavedAt) {
			return out[i].SavedAt.Before(out[j].SavedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Revision looks up a single revision by id, tool-agnostically: ids are
// UUIDv7 and therefore globally unique across every tool sharing the store,
// so a bare id resolves without knowing its tool up front (§8.3, §8.11 —
// "{id}はUUIDv7でtool横断に一意"). ok is false (no error) when id is unknown.
func (s *Store) Revision(id string) (rev Revision, ok bool, err error) {
	sf, err := s.load()
	if err != nil {
		return Revision{}, false, err
	}
	rev, ok = sf.Revisions[id]
	return rev, ok, nil
}

// Restore repoints tool's current ref at the existing revision id and
// discards tool's draft working node (§4.2): it never creates a new
// revision. A subsequent edit therefore becomes a new child of id — an
// implicit branch whenever id already had a different child. It is an error
// if id is unknown or belongs to a different tool.
func (s *Store) Restore(tool, id string) error {
	sf, err := s.load()
	if err != nil {
		return err
	}
	rev, ok := sf.Revisions[id]
	if !ok {
		return fmt.Errorf("store: unknown revision %q", id)
	}
	if rev.Tool != tool {
		return fmt.Errorf("store: revision %q belongs to tool %q, not %q", id, rev.Tool, tool)
	}

	r := sf.Refs[tool]
	if r == nil {
		r = &ref{}
		sf.Refs[tool] = r
	}
	r.Current = id
	r.Draft = nil
	return s.save(sf)
}

// rotateTool enforces §4.4's retention rule for tool in place on sf (the
// caller — Save — persists afterward): keep the newest maxRevisionsPerTool
// revisions by id (UUIDv7 sorts ≈ chronologically), plus whatever
// refs.<tool>.current points at regardless of age, so current can never go
// dangling. Everything else belonging to tool is deleted. A surviving
// revision whose parent was deleted is reparented to its nearest surviving
// ancestor (found by walking the pre-deletion parent chain), or to nil if no
// ancestor survived — the DAG never keeps a dangling parent reference.
func rotateTool(sf *storeFile, tool string) {
	r := sf.Refs[tool]
	var currentID string
	if r != nil {
		currentID = r.Current
	}

	ids := make([]string, 0)
	for id, rev := range sf.Revisions {
		if rev.Tool == tool {
			ids = append(ids, id)
		}
	}
	if len(ids) <= maxRevisionsPerTool {
		return
	}
	// UUIDv7 ids sort lexicographically ≈ chronologically (§3.2); descending
	// order puts the newest ids first.
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))

	keep := make(map[string]bool, maxRevisionsPerTool+1)
	for i, id := range ids {
		if i < maxRevisionsPerTool {
			keep[id] = true
		}
	}
	if currentID != "" {
		keep[currentID] = true
	}

	// Snapshot the pre-deletion parent chain so reparenting can walk past
	// revisions that are themselves about to be deleted.
	origParent := make(map[string]*string, len(ids))
	for _, id := range ids {
		origParent[id] = sf.Revisions[id].Parent
	}

	for _, id := range ids {
		if !keep[id] {
			delete(sf.Revisions, id)
		}
	}

	for id := range keep {
		rev, ok := sf.Revisions[id]
		if !ok || rev.Parent == nil {
			continue
		}
		if _, survived := sf.Revisions[*rev.Parent]; survived {
			continue
		}
		var newParent *string
		ancestor := origParent[*rev.Parent]
		for ancestor != nil {
			if _, survived := sf.Revisions[*ancestor]; survived {
				newParent = ancestor
				break
			}
			ancestor = origParent[*ancestor]
		}
		rev.Parent = newParent
		sf.Revisions[id] = rev
	}
}
