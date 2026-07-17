package store

import (
	"fmt"
	"testing"
	"time"
)

// docFor returns a valid claude-code DSL document whose layout name embeds n,
// so successive calls hash differently (Save's dedup would otherwise treat
// them as no-ops).
func docFor(n int) string {
	return fmt.Sprintf(`<statusloom version="1" tool="claude-code"><layout name="d%d" active="true"><line><field name="model"/></line></layout></statusloom>`, n)
}

func TestRevisions_OrderedBySavedAtThenID(t *testing.T) {
	s := isolatedStore(t)

	id1, _, err := s.Save(tool, docFor(1), "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	id2, _, err := s.Save(tool, docFor(2), "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	id3, _, err := s.Save(tool, docFor(3), "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 3: %v", err)
	}

	revs, err := s.Revisions(tool)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	if len(revs) != 3 {
		t.Fatalf("Revisions len = %d, want 3", len(revs))
	}
	got := []string{revs[0].ID, revs[1].ID, revs[2].ID}
	want := []string{id1, id2, id3}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Revisions order = %v, want %v", got, want)
		}
	}
}

func TestRevisions_TiesBrokenByID(t *testing.T) {
	s := isolatedStore(t)

	// White-box: inject two revisions with an identical SavedAt directly,
	// bypassing Save's time.Now() timestamping, to make the tie-break
	// deterministic instead of racing the clock's resolution.
	sf, err := s.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sf.Revisions["id-b"] = Revision{Hash: "h1", Tool: tool, Source: docFor(1), SavedAt: when}
	sf.Revisions["id-a"] = Revision{Hash: "h2", Tool: tool, Source: docFor(2), SavedAt: when}
	if err := s.save(sf); err != nil {
		t.Fatalf("save: %v", err)
	}

	revs, err := s.Revisions(tool)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("Revisions len = %d, want 2", len(revs))
	}
	if revs[0].ID != "id-a" || revs[1].ID != "id-b" {
		t.Fatalf("tie-break order = [%s, %s], want [id-a, id-b]", revs[0].ID, revs[1].ID)
	}
}

func TestRevisionLookup_CrossTool(t *testing.T) {
	s := isolatedStore(t)

	id, _, err := s.Save(tool, docFor(1), "cli", Meta{Name: "n"})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	rev, ok, err := s.Revision(id)
	if err != nil || !ok {
		t.Fatalf("Revision(%q): ok=%v err=%v", id, ok, err)
	}
	if rev.Tool != tool || rev.Meta.Name != "n" {
		t.Fatalf("Revision fields mismatch: %+v", rev)
	}

	if _, ok, err := s.Revision("does-not-exist"); err != nil || ok {
		t.Fatalf("Revision(unknown): ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

func TestRestore_RepointsCurrentAndDiscardsDraft(t *testing.T) {
	s := isolatedStore(t)

	id1, _, err := s.Save(tool, docFor(1), "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if _, _, err := s.Save(tool, docFor(2), "ui", Meta{}); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if err := s.WriteDraft(tool, "wip"); err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}

	if err := s.Restore(tool, id1); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	_, curID, ok, err := s.Current(tool)
	if err != nil || !ok || curID != id1 {
		t.Fatalf("Current after Restore: cur=%q ok=%v err=%v, want %q", curID, ok, err, id1)
	}
	if _, ok, _ := s.ReadDraft(tool); ok {
		t.Fatal("Restore should discard the draft working node")
	}

	// Restore must not create a new revision.
	revs, err := s.Revisions(tool)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("Restore created/removed a revision: len=%d, want 2", len(revs))
	}
}

func TestRestore_UnknownIDErrors(t *testing.T) {
	s := isolatedStore(t)
	if _, _, err := s.Save(tool, docFor(1), "ui", Meta{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before, _, ok, err := s.Current(tool)
	if err != nil || !ok {
		t.Fatalf("Current: ok=%v err=%v", ok, err)
	}

	if err := s.Restore(tool, "not-a-real-id"); err == nil {
		t.Fatal("Restore with an unknown id should error")
	}

	after, _, ok, err := s.Current(tool)
	if err != nil || !ok || after.Hash != before.Hash {
		t.Fatalf("a rejected Restore must not change current: before=%+v after=%+v", before, after)
	}
}

func TestRestore_ToolMismatchErrors(t *testing.T) {
	s := isolatedStore(t)
	id, _, err := s.Save(tool, docFor(1), "ui", Meta{})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Restore("some-other-tool", id); err == nil {
		t.Fatal("Restore should reject an id belonging to a different tool")
	}
}

func TestRestore_ThenEditBranches(t *testing.T) {
	s := isolatedStore(t)

	id1, _, err := s.Save(tool, docFor(1), "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	id2, _, err := s.Save(tool, docFor(2), "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	if err := s.Restore(tool, id1); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	id3, _, err := s.Save(tool, docFor(3), "ui", Meta{})
	if err != nil {
		t.Fatalf("Save 3 (post-restore edit): %v", err)
	}

	// id1 now has two children: id2 (the original) and id3 (the new branch).
	rev2, ok, err := s.Revision(id2)
	if err != nil || !ok || rev2.Parent == nil || *rev2.Parent != id1 {
		t.Fatalf("id2 parent = %v, want %q", rev2.Parent, id1)
	}
	rev3, ok, err := s.Revision(id3)
	if err != nil || !ok || rev3.Parent == nil || *rev3.Parent != id1 {
		t.Fatalf("id3 parent = %v, want %q", rev3.Parent, id1)
	}

	revs, err := s.Revisions(tool)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	if len(revs) != 3 {
		t.Fatalf("Revisions len = %d, want 3 (id1, id2, id3)", len(revs))
	}

	_, curID, ok, err := s.Current(tool)
	if err != nil || !ok || curID != id3 {
		t.Fatalf("current after branching edit = %q, want %q", curID, id3)
	}
}

func TestSaveRotatesOldRevisions(t *testing.T) {
	s := isolatedStore(t)

	const total = maxRevisionsPerTool + 5
	ids := make([]string, total)
	for i := 0; i < total; i++ {
		id, _, err := s.Save(tool, docFor(i), "ui", Meta{})
		if err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
		ids[i] = id
	}

	revs, err := s.Revisions(tool)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	if len(revs) != maxRevisionsPerTool {
		t.Fatalf("Revisions len after rotation = %d, want %d", len(revs), maxRevisionsPerTool)
	}

	// The oldest 5 must be gone; the newest maxRevisionsPerTool must survive.
	for i := 0; i < 5; i++ {
		if _, ok, _ := s.Revision(ids[i]); ok {
			t.Fatalf("revision %d (%s) should have been rotated out", i, ids[i])
		}
	}
	for i := 5; i < total; i++ {
		if _, ok, _ := s.Revision(ids[i]); !ok {
			t.Fatalf("revision %d (%s) should have survived rotation", i, ids[i])
		}
	}

	// current must still be the latest and untouched.
	_, curID, ok, err := s.Current(tool)
	if err != nil || !ok || curID != ids[total-1] {
		t.Fatalf("current after rotation = %q, want %q", curID, ids[total-1])
	}

	// The new oldest survivor's parent (which pointed at a rotated-out
	// revision) must have been reparented to nil (its whole ancestry was
	// deleted).
	oldestSurvivor, ok, err := s.Revision(ids[5])
	if err != nil || !ok {
		t.Fatalf("Revision(ids[5]): ok=%v err=%v", ok, err)
	}
	if oldestSurvivor.Parent != nil {
		t.Fatalf("oldest survivor's parent = %v, want nil (its ancestry was fully rotated out)", oldestSurvivor.Parent)
	}
}

func TestSaveRotationIsPerTool(t *testing.T) {
	s := isolatedStore(t)

	const otherTool = "other-tool"
	const total = maxRevisionsPerTool + 5
	for i := 0; i < total; i++ {
		if _, _, err := s.Save(tool, docFor(i), "ui", Meta{}); err != nil {
			t.Fatalf("Save tool %d: %v", i, err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, _, err := s.Save(otherTool, docFor(1000+i), "ui", Meta{}); err != nil {
			t.Fatalf("Save otherTool %d: %v", i, err)
		}
	}

	revs, err := s.Revisions(tool)
	if err != nil {
		t.Fatalf("Revisions(tool): %v", err)
	}
	if len(revs) != maxRevisionsPerTool {
		t.Fatalf("Revisions(tool) len = %d, want %d", len(revs), maxRevisionsPerTool)
	}
	otherRevs, err := s.Revisions(otherTool)
	if err != nil {
		t.Fatalf("Revisions(otherTool): %v", err)
	}
	if len(otherRevs) != 3 {
		t.Fatalf("Revisions(otherTool) len = %d, want 3 (must not be rotated by tool's activity)", len(otherRevs))
	}
}

// TestRotateTool_KeepsCurrentAndReparentsChain is a white-box test on
// rotateTool itself: it constructs a storeFile with a straight linear chain
// long enough to exceed maxRevisionsPerTool, points current at an id old
// enough that it would otherwise be rotated out, and checks both halves of
// §4.4 in one scenario: current survives regardless of age, and the
// reparenting walk can skip more than one deleted ancestor (a "reparent
// chain") to find the nearest survivor — which, here, is the
// out-of-band-protected current itself.
func TestRotateTool_KeepsCurrentAndReparentsChain(t *testing.T) {
	const n = maxRevisionsPerTool + 5 // 55 ids: natural top 50 = ids[5:55]
	ids := make([]string, n)
	for i := range ids {
		id, err := newUUIDv7()
		if err != nil {
			t.Fatalf("newUUIDv7: %v", err)
		}
		ids[i] = id
	}

	sf := &storeFile{
		FormatVersion: formatVersion,
		Revisions:     map[string]Revision{},
		Refs:          map[string]*ref{},
	}
	for i, id := range ids {
		var parent *string
		if i > 0 {
			p := ids[i-1]
			parent = &p
		}
		sf.Revisions[id] = Revision{Hash: id, Tool: tool, Source: docFor(i), Parent: parent}
	}
	// Pin current at ids[2]: the 3rd-oldest, well outside the natural
	// top-50-newest window (ids[5:55]).
	sf.Refs[tool] = &ref{Current: ids[2]}

	rotateTool(sf, tool)

	if len(sf.Revisions) != maxRevisionsPerTool+1 {
		t.Fatalf("len(Revisions) after rotation = %d, want %d (top %d + protected current)", len(sf.Revisions), maxRevisionsPerTool+1, maxRevisionsPerTool)
	}

	// ids[0], ids[1], ids[3], ids[4] were not protected and are outside the
	// natural window: gone.
	for _, i := range []int{0, 1, 3, 4} {
		if _, ok := sf.Revisions[ids[i]]; ok {
			t.Fatalf("ids[%d] should have been rotated out", i)
		}
	}
	// ids[2] (current) survives despite its age.
	if _, ok := sf.Revisions[ids[2]]; !ok {
		t.Fatal("current revision must survive rotation regardless of age")
	}
	// The natural top 50 (ids[5:55]) all survive.
	for i := 5; i < n; i++ {
		if _, ok := sf.Revisions[ids[i]]; !ok {
			t.Fatalf("ids[%d] should have survived as part of the natural top %d", i, maxRevisionsPerTool)
		}
	}

	// ids[2]'s own parent (ids[1]) was deleted, and ids[1]'s parent (ids[0])
	// was also deleted, and ids[0] had no parent: the reparent walk exhausts
	// the chain and lands on nil.
	protected := sf.Revisions[ids[2]]
	if protected.Parent != nil {
		t.Fatalf("protected current's parent = %v, want nil (its whole ancestry was rotated out)", protected.Parent)
	}

	// ids[5]'s parent (ids[4]) was deleted, whose parent (ids[3]) was also
	// deleted, whose parent (ids[2]) survived (protected) — a two-hop
	// reparent chain landing on the protected current.
	survivor := sf.Revisions[ids[5]]
	if survivor.Parent == nil || *survivor.Parent != ids[2] {
		t.Fatalf("ids[5] reparented to %v, want %q (two-hop chain to the protected survivor)", survivor.Parent, ids[2])
	}
}

func TestRotateTool_NoOpAtOrBelowLimit(t *testing.T) {
	sf := &storeFile{
		FormatVersion: formatVersion,
		Revisions:     map[string]Revision{},
		Refs:          map[string]*ref{},
	}
	var lastID string
	for i := 0; i < maxRevisionsPerTool; i++ {
		id, err := newUUIDv7()
		if err != nil {
			t.Fatalf("newUUIDv7: %v", err)
		}
		sf.Revisions[id] = Revision{Hash: id, Tool: tool, Source: docFor(i)}
		lastID = id
	}
	sf.Refs[tool] = &ref{Current: lastID}

	rotateTool(sf, tool)

	if len(sf.Revisions) != maxRevisionsPerTool {
		t.Fatalf("rotateTool at exactly the limit should be a no-op: len=%d, want %d", len(sf.Revisions), maxRevisionsPerTool)
	}
}
