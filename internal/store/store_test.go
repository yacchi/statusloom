package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yacchi/statusloom/internal/dsl"
)

const (
	tool      = "claude-code"
	validDSL  = `<statusloom version="1" tool="claude-code"><layout name="d" active="true"><line><field name="model"/></line></layout></statusloom>`
	validDSL2 = `<statusloom version="1" tool="claude-code"><layout name="d" active="true"><line><field name="model"/><field name="git-branch"/></line></layout></statusloom>`
	// Well-formed XML, but references a field that does not exist, so
	// dsl.Validate reports an error and Save must refuse it.
	invalidDSL = `<statusloom version="1" tool="claude-code"><layout name="d" active="true"><line><field name="not-a-field"/></line></layout></statusloom>`
)

// isolatedStore points STATUSLOOM_CONFIG at a fresh temp directory and returns
// a Store handle to the statusloom.json inside it, so no test ever touches the
// real config.
func isolatedStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", dir)
	s, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got, want := s.Path(), filepath.Join(dir, "statusloom.json"); got != want {
		t.Fatalf("store path = %q, want %q", got, want)
	}
	return s
}

func TestSourceVersion(t *testing.T) {
	a := SourceVersion(tool, validDSL)
	if len(a) != 64 {
		t.Fatalf("SourceVersion should be 64 hex chars, got %d: %q", len(a), a)
	}
	if SourceVersion(tool, validDSL) != a {
		t.Fatal("SourceVersion is not deterministic")
	}
	if SourceVersion(tool, validDSL2) == a {
		t.Fatal("different sources must hash differently")
	}
	// The tool is mixed into the hash, so the same source under a different
	// tool must differ.
	if SourceVersion("other", validDSL) == a {
		t.Fatal("same source under a different tool must hash differently")
	}
}

func TestSaveCreatesRevisionAndAdvancesCurrent(t *testing.T) {
	s := isolatedStore(t)

	if _, _, ok, err := s.Current(tool); err != nil || ok {
		t.Fatalf("empty store Current: ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	id, diags, err := s.Save(tool, validDSL, "cli", Meta{Name: "first"})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if dsl.HasErrors(diags) {
		t.Fatalf("valid Save produced errors: %v", diags)
	}
	if id == "" {
		t.Fatal("Save returned empty revID")
	}

	rev, curID, ok, err := s.Current(tool)
	if err != nil || !ok {
		t.Fatalf("Current after Save: ok=%v err=%v", ok, err)
	}
	if curID != id {
		t.Fatalf("current id = %q, want %q", curID, id)
	}
	if rev.Source != validDSL {
		t.Fatalf("revision source mismatch")
	}
	if rev.Tool != tool || rev.Origin != "cli" || rev.Meta.Name != "first" {
		t.Fatalf("revision fields not persisted: %+v", rev)
	}
	if rev.Hash != SourceVersion(tool, validDSL) {
		t.Fatalf("revision hash = %q, want %q", rev.Hash, SourceVersion(tool, validDSL))
	}
	if rev.Parent != nil {
		t.Fatalf("first revision parent = %v, want nil", rev.Parent)
	}
}

func TestSaveParentChain(t *testing.T) {
	s := isolatedStore(t)

	id1, _, err := s.Save(tool, validDSL, "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	id2, _, err := s.Save(tool, validDSL2, "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if id1 == id2 {
		t.Fatal("second Save reused the first id")
	}

	rev2, curID, ok, _ := s.Current(tool)
	if !ok || curID != id2 {
		t.Fatalf("current not advanced to id2: cur=%q ok=%v", curID, ok)
	}
	if rev2.Parent == nil || *rev2.Parent != id1 {
		t.Fatalf("id2 parent = %v, want %q", rev2.Parent, id1)
	}
}

// TestSaveMetaCarryForward covers the carry-forward regression: an import
// (or explicit rename) sets meta, and a subsequent plain save (webconfig's
// PUT /api/dsl/document, `fmt --write`) passes the zero-value Meta{} — that
// must inherit the parent's meta rather than blank it. A save that passes a
// non-zero meta of its own still replaces it outright.
func TestSaveMetaCarryForward(t *testing.T) {
	s := isolatedStore(t)

	id1, _, err := s.Save(tool, validDSL, "import", Meta{Name: "imported", Description: "d", Author: "a"})
	if err != nil {
		t.Fatalf("Save 1 (import, named meta): %v", err)
	}
	rev1, _, ok, err := s.Current(tool)
	if err != nil || !ok || rev1.Meta.Name != "imported" {
		t.Fatalf("Current after named Save: rev=%+v ok=%v err=%v", rev1, ok, err)
	}

	// A plain save (zero-value meta) must carry the parent's meta forward,
	// not blank it.
	id2, _, err := s.Save(tool, validDSL2, "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 2 (zero meta): %v", err)
	}
	if id2 == id1 {
		t.Fatal("second Save should have created a new revision")
	}
	rev2, curID, ok, err := s.Current(tool)
	if err != nil || !ok || curID != id2 {
		t.Fatalf("Current after zero-meta Save: cur=%q ok=%v err=%v", curID, ok, err)
	}
	if rev2.Meta != (Meta{Name: "imported", Description: "d", Author: "a"}) {
		t.Fatalf("zero-value meta did not carry forward: got %+v", rev2.Meta)
	}

	// An explicit (non-zero) meta on the next save replaces it.
	id3, _, err := s.Save(tool, validDSL, "ui", Meta{Name: "renamed"})
	if err != nil {
		t.Fatalf("Save 3 (explicit meta): %v", err)
	}
	if id3 == id2 {
		t.Fatal("third Save should have created a new revision")
	}
	rev3, curID, ok, err := s.Current(tool)
	if err != nil || !ok || curID != id3 {
		t.Fatalf("Current after explicit-meta Save: cur=%q ok=%v err=%v", curID, ok, err)
	}
	if rev3.Meta != (Meta{Name: "renamed"}) {
		t.Fatalf("explicit meta should have replaced the carried-forward one: got %+v", rev3.Meta)
	}
}

func TestSaveDedupNoOp(t *testing.T) {
	s := isolatedStore(t)

	id1, _, err := s.Save(tool, validDSL, "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}

	// Re-saving identical source must not create a new revision.
	id2, _, err := s.Save(tool, validDSL, "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("dedup should return the existing id: got %q, want %q", id2, id1)
	}

	sf, err := s.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(sf.Revisions) != 1 {
		t.Fatalf("dedup created extra revisions: %d", len(sf.Revisions))
	}
}

// TestSaveMetaOnlyChangeCreatesRevision pins §3.3: hash matching the tip is
// not sufficient for dedup when the caller's meta is a real, different value
// (e.g. re-importing the same .sloom.md after only editing its notes, or an
// explicit rename) — that must still create a new revision and update the
// tip's meta, not be silently dropped.
func TestSaveMetaOnlyChangeCreatesRevision(t *testing.T) {
	s := isolatedStore(t)

	id1, _, err := s.Save(tool, validDSL, "import", Meta{Notes: "first notes"})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}

	id2, _, err := s.Save(tool, validDSL, "import", Meta{Notes: "second notes"})
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if id2 == id1 {
		t.Fatal("a meta-only change (same source hash, different meta) must create a new revision, not dedup")
	}

	sf, err := s.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(sf.Revisions) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(sf.Revisions))
	}
	rev2 := sf.Revisions[id2]
	if rev2.Meta.Notes != "second notes" {
		t.Fatalf("current revision's meta.Notes = %q, want %q", rev2.Meta.Notes, "second notes")
	}
	if sf.Refs[tool].Current != id2 {
		t.Fatalf("current ref should advance to the new revision: got %q, want %q", sf.Refs[tool].Current, id2)
	}
}

func TestSaveClearsDraft(t *testing.T) {
	s := isolatedStore(t)

	if err := s.WriteDraft(tool, "in progress"); err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}
	if _, ok, _ := s.ReadDraft(tool); !ok {
		t.Fatal("draft should exist after WriteDraft")
	}

	if _, _, err := s.Save(tool, validDSL, "ui", Meta{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if src, ok, err := s.ReadDraft(tool); err != nil || ok {
		t.Fatalf("draft should be cleared after Save: ok=%v src=%q err=%v", ok, src, err)
	}
}

func TestDedupClearsDraft(t *testing.T) {
	s := isolatedStore(t)

	if _, _, err := s.Save(tool, validDSL, "ui", Meta{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.WriteDraft(tool, validDSL); err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}
	// Saving source identical to the tip: dedup no-op, but the now-synced
	// draft should be cleared.
	if _, _, err := s.Save(tool, validDSL, "ui", Meta{}); err != nil {
		t.Fatalf("Save dedup: %v", err)
	}
	if _, ok, _ := s.ReadDraft(tool); ok {
		t.Fatal("dedup Save should clear a synced draft")
	}
}

func TestDraftLifecycle(t *testing.T) {
	s := isolatedStore(t)

	if _, ok, err := s.ReadDraft(tool); err != nil || ok {
		t.Fatalf("no draft yet: ok=%v err=%v", ok, err)
	}

	if err := s.WriteDraft(tool, "draft-a"); err != nil {
		t.Fatalf("WriteDraft a: %v", err)
	}
	if src, ok, _ := s.ReadDraft(tool); !ok || src != "draft-a" {
		t.Fatalf("ReadDraft after write: ok=%v src=%q", ok, src)
	}

	// LWW overwrite.
	if err := s.WriteDraft(tool, "draft-b"); err != nil {
		t.Fatalf("WriteDraft b: %v", err)
	}
	if src, ok, _ := s.ReadDraft(tool); !ok || src != "draft-b" {
		t.Fatalf("ReadDraft after overwrite: ok=%v src=%q", ok, src)
	}

	// ReadDraft must not fall back to current.
	if _, _, err := s.Save(tool, validDSL, "ui", Meta{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, ok, _ := s.ReadDraft(tool); ok {
		t.Fatal("ReadDraft returned ok after Save cleared the draft (must not fall back to current)")
	}
}

func TestSaveRejectsInvalidDSL(t *testing.T) {
	s := isolatedStore(t)

	// Establish a valid current first.
	goodID, _, err := s.Save(tool, validDSL, "ui", Meta{})
	if err != nil {
		t.Fatalf("Save good: %v", err)
	}
	beforeInfo, _ := os.Stat(s.Path())

	id, diags, err := s.Save(tool, invalidDSL, "ui", Meta{})
	if err != nil {
		t.Fatalf("rejected Save should return err=nil, got %v", err)
	}
	if id != "" {
		t.Fatalf("rejected Save should return empty id, got %q", id)
	}
	if !dsl.HasErrors(diags) {
		t.Fatalf("rejected Save should return error diagnostics, got %v", diags)
	}

	// current unchanged and file untouched (same size/modtime).
	_, curID, ok, _ := s.Current(tool)
	if !ok || curID != goodID {
		t.Fatalf("current changed after rejected Save: cur=%q ok=%v", curID, ok)
	}
	afterInfo, _ := os.Stat(s.Path())
	if beforeInfo.Size() != afterInfo.Size() || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatal("rejected Save modified the store file")
	}
}

func TestSaveRejectsInvalidDSLOnEmptyStore(t *testing.T) {
	s := isolatedStore(t)

	id, diags, err := s.Save(tool, invalidDSL, "ui", Meta{})
	if err != nil || id != "" || !dsl.HasErrors(diags) {
		t.Fatalf("rejected Save: id=%q err=%v diags=%v", id, err, diags)
	}
	// No file should have been created.
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Fatalf("rejected Save created a store file: %v", err)
	}
}

func TestPersistenceRoundtrip(t *testing.T) {
	s := isolatedStore(t)

	id1, _, err := s.Save(tool, validDSL, "cli", Meta{Name: "n", Description: "d", Author: "a"})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	id2, _, err := s.Save(tool, validDSL2, "import", Meta{Name: "n2"})
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if err := s.WriteDraft(tool, "wip"); err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}

	// Re-open the same file with a fresh handle.
	reopened := OpenAt(s.Path())

	rev1, err := reopened.revisionByID(id1)
	if err != nil {
		t.Fatalf("reopen load: %v", err)
	}
	if rev1.Meta.Name != "n" || rev1.Meta.Description != "d" || rev1.Meta.Author != "a" || rev1.Origin != "cli" {
		t.Fatalf("meta not roundtripped: %+v", rev1)
	}

	rev2, curID, ok, err := reopened.Current(tool)
	if err != nil || !ok || curID != id2 {
		t.Fatalf("reopen Current: cur=%q ok=%v err=%v", curID, ok, err)
	}
	if rev2.Parent == nil || *rev2.Parent != id1 {
		t.Fatalf("reopen parent = %v, want %q", rev2.Parent, id1)
	}
	if src, ok, _ := reopened.ReadDraft(tool); !ok || src != "wip" {
		t.Fatalf("reopen draft: ok=%v src=%q", ok, src)
	}
}

// revisionByID is a test-only helper reaching into the persisted file.
func (s *Store) revisionByID(id string) (Revision, error) {
	sf, err := s.load()
	if err != nil {
		return Revision{}, err
	}
	return sf.Revisions[id], nil
}
