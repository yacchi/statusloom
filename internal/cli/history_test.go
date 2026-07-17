package cli

import (
	"strings"
	"testing"

	"github.com/yacchi/statusloom/internal/store"
)

// historyDoc returns a valid claude-code document whose layout name embeds
// name, so successive calls Save distinct content (identical source would
// dedup to a no-op revision).
func historyDoc(name string) string {
	return `<statusloom version="1" tool="claude-code"><layout name="` + name + `" active="true"><line><field name="model"/></line></layout></statusloom>`
}

func TestHistory_ListShowsRevisionsNewestOrder(t *testing.T) {
	setupEnv(t)
	writeDocument(t, "claude-code", historyDoc("a"))
	writeDocument(t, "claude-code", historyDoc("b"))

	stdout, stderr, code := runCLI(t, []string{"history", "list"}, nil, nil)
	if code != 0 {
		t.Fatalf("history list exit = %d, stderr=%s", code, stderr)
	}
	ls := lines(stdout)
	if len(ls) != 2 {
		t.Fatalf("history list lines = %v, want 2", ls)
	}
	// Revisions() orders oldest-first; the current (last-saved) revision is
	// marked with "*" and must be the last line.
	if !strings.HasPrefix(ls[1], "*") {
		t.Errorf("last line should be marked current: %q", ls[1])
	}
	if strings.HasPrefix(ls[0], "*") {
		t.Errorf("first (older) line should not be marked current: %q", ls[0])
	}
}

func TestHistory_ListEmpty(t *testing.T) {
	setupEnv(t)
	stdout, stderr, code := runCLI(t, []string{"history", "list"}, nil, nil)
	if code != 0 {
		t.Fatalf("history list exit = %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "no history") {
		t.Errorf("expected an empty-history message, got %q", stdout)
	}
}

func TestHistory_Show(t *testing.T) {
	setupEnv(t)
	writeDocument(t, "claude-code", historyDoc("a"))

	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	_, curID, ok, err := st.Current("claude-code")
	if err != nil || !ok {
		t.Fatalf("Current: ok=%v err=%v", ok, err)
	}

	stdout, stderr, code := runCLI(t, []string{"history", "show", curID}, nil, nil)
	if code != 0 {
		t.Fatalf("history show exit = %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, curID) {
		t.Errorf("show output missing id %q:\n%s", curID, stdout)
	}
	if !strings.Contains(stdout, `name="a"`) {
		t.Errorf("show output missing source content:\n%s", stdout)
	}
}

func TestHistory_ShowUnknownID(t *testing.T) {
	setupEnv(t)
	_, stderr, code := runCLI(t, []string{"history", "show", "no-such-id"}, nil, nil)
	if code == 0 {
		t.Fatal("history show with an unknown id should fail")
	}
	if !strings.Contains(stderr, "unknown revision") {
		t.Errorf("stderr = %q, want an unknown-revision message", stderr)
	}
}

func TestHistory_DiffAgainstCurrent(t *testing.T) {
	setupEnv(t)
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	id1, _, err := st.Save("claude-code", historyDoc("a"), "test", store.Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if _, _, err := st.Save("claude-code", historyDoc("b"), "test", store.Meta{}); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	stdout, stderr, code := runCLI(t, []string{"history", "diff", id1}, nil, nil)
	if code != 0 {
		t.Fatalf("history diff exit = %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "-"+`<layout name="a"`) && !strings.Contains(stdout, `-<statusloom version="1" tool="claude-code"><layout name="a"`) {
		t.Errorf("diff output missing a removed line for the old content:\n%s", stdout)
	}
	if !strings.Contains(stdout, `name="b"`) {
		t.Errorf("diff output missing the new content:\n%s", stdout)
	}
	if !strings.HasPrefix(stdout, "--- "+id1+"\n") {
		t.Errorf("diff output should start with a --- header naming %s:\n%s", id1, stdout)
	}
}

func TestHistory_DiffTwoIDs(t *testing.T) {
	setupEnv(t)
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	id1, _, err := st.Save("claude-code", historyDoc("a"), "test", store.Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	id2, _, err := st.Save("claude-code", historyDoc("b"), "test", store.Meta{})
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	stdout, stderr, code := runCLI(t, []string{"history", "diff", id1, id2}, nil, nil)
	if code != 0 {
		t.Fatalf("history diff exit = %d, stderr=%s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "--- "+id1+"\n+++ "+id2+"\n") {
		t.Errorf("diff output header = %q, want --- %s / +++ %s", stdout, id1, id2)
	}
}

func TestHistory_DiffIdentical(t *testing.T) {
	setupEnv(t)
	writeDocument(t, "claude-code", historyDoc("a"))
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	_, curID, ok, err := st.Current("claude-code")
	if err != nil || !ok {
		t.Fatalf("Current: ok=%v err=%v", ok, err)
	}

	stdout, stderr, code := runCLI(t, []string{"history", "diff", curID, curID}, nil, nil)
	if code != 0 {
		t.Fatalf("history diff exit = %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "no differences") {
		t.Errorf("diffing identical ids should say so: %q", stdout)
	}
}

func TestHistory_RestoreDiscardsDraftAndRepointsCurrent(t *testing.T) {
	setupEnv(t)
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	id1, _, err := st.Save("claude-code", historyDoc("a"), "test", store.Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if _, _, err := st.Save("claude-code", historyDoc("b"), "test", store.Meta{}); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if err := st.WriteDraft("claude-code", "in progress"); err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}

	// The draft differs from current, so a plain restore must be refused;
	// --force is required to proceed (TestHistory_RestoreRefusesDirtyDraft
	// covers the refusal itself).
	stdout, stderr, code := runCLI(t, []string{"history", "restore", id1, "--force"}, nil, nil)
	if code != 0 {
		t.Fatalf("history restore exit = %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, id1) {
		t.Errorf("restore output missing id %q: %q", id1, stdout)
	}

	_, curID, ok, err := st.Current("claude-code")
	if err != nil || !ok || curID != id1 {
		t.Fatalf("current after restore = %q, want %q", curID, id1)
	}
	if _, ok, _ := st.ReadDraft("claude-code"); ok {
		t.Error("restore should have discarded the draft working node")
	}
}

func TestHistory_RestoreRefusesDirtyDraftWithoutForce(t *testing.T) {
	setupEnv(t)
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	id1, _, err := st.Save("claude-code", historyDoc("a"), "test", store.Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if _, _, err := st.Save("claude-code", historyDoc("b"), "test", store.Meta{}); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if err := st.WriteDraft("claude-code", "in progress"); err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}

	_, stderr, code := runCLI(t, []string{"history", "restore", id1}, nil, nil)
	if code == 0 {
		t.Fatal("restoring over a dirty draft without --force should fail")
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("stderr = %q, want a mention of --force", stderr)
	}

	// The store must be left completely unchanged: current still the
	// pre-restore tip, draft still present.
	_, curID, ok, err := st.Current("claude-code")
	if err != nil || !ok || curID == id1 {
		t.Fatalf("current after refused restore = %q, must not have moved to %q", curID, id1)
	}
	draftSrc, ok, err := st.ReadDraft("claude-code")
	if err != nil || !ok || draftSrc != "in progress" {
		t.Fatalf("draft after refused restore: src=%q ok=%v err=%v, want the draft untouched", draftSrc, ok, err)
	}
}

func TestHistory_RestoreNoForceNeededWhenDraftClean(t *testing.T) {
	setupEnv(t)
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	id1, _, err := st.Save("claude-code", historyDoc("a"), "test", store.Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	cur, _, err := st.Save("claude-code", historyDoc("b"), "test", store.Meta{})
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	rev, ok, err := st.Revision(cur)
	if err != nil || !ok {
		t.Fatalf("Revision(cur): ok=%v err=%v", ok, err)
	}
	// Draft identical to current is not dirty: restore should not require
	// --force.
	if err := st.WriteDraft("claude-code", rev.Source); err != nil {
		t.Fatalf("WriteDraft: %v", err)
	}

	stdout, stderr, code := runCLI(t, []string{"history", "restore", id1}, nil, nil)
	if code != 0 {
		t.Fatalf("history restore exit = %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, id1) {
		t.Errorf("restore output missing id %q: %q", id1, stdout)
	}
}

// TestHistory_RestoreNoForceNeededWhenNoDraft covers the plain no-draft case
// (no §4.2 confirmation needed, and no regression from requiring --force
// unconditionally).
func TestHistory_RestoreNoForceNeededWhenNoDraft(t *testing.T) {
	setupEnv(t)
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	id1, _, err := st.Save("claude-code", historyDoc("a"), "test", store.Meta{})
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if _, _, err := st.Save("claude-code", historyDoc("b"), "test", store.Meta{}); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	stdout, stderr, code := runCLI(t, []string{"history", "restore", id1}, nil, nil)
	if code != 0 {
		t.Fatalf("history restore exit = %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, id1) {
		t.Errorf("restore output missing id %q: %q", id1, stdout)
	}
}

func TestHistory_RestoreUnknownID(t *testing.T) {
	setupEnv(t)
	_, stderr, code := runCLI(t, []string{"history", "restore", "no-such-id", "--force"}, nil, nil)
	if code == 0 {
		t.Fatal("restoring an unknown id should fail")
	}
	if stderr == "" {
		t.Error("expected an error message on stderr")
	}
}

func TestHistory_ToolFlag(t *testing.T) {
	setupEnv(t)
	st, err := store.Open()
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if _, _, err := st.Save("other-tool", historyDoc("x"), "test", store.Meta{}); err != nil {
		t.Fatalf("Save other-tool: %v", err)
	}

	// Without --tool, history acts on claude-code (empty here).
	stdout, _, code := runCLI(t, []string{"history", "list"}, nil, nil)
	if code != 0 || !strings.Contains(stdout, "no history") {
		t.Fatalf("default-tool list = %q (code %d), want an empty-history message", stdout, code)
	}

	// With --tool other-tool, it lists that tool's revision instead.
	stdout, stderr, code := runCLI(t, []string{"history", "list", "--tool", "other-tool"}, nil, nil)
	if code != 0 {
		t.Fatalf("--tool list exit = %d, stderr=%s", code, stderr)
	}
	if strings.Contains(stdout, "no history") {
		t.Errorf("--tool other-tool list should find the other-tool revision: %q", stdout)
	}
}

func TestHistory_UnknownSubcommand(t *testing.T) {
	setupEnv(t)
	_, stderr, code := runCLI(t, []string{"history", "bogus"}, nil, nil)
	if code != 2 {
		t.Fatalf("unknown subcommand exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "unknown subcommand") {
		t.Errorf("stderr = %q, want an unknown-subcommand message", stderr)
	}
}

func TestHistory_NoSubcommand(t *testing.T) {
	setupEnv(t)
	_, stderr, code := runCLI(t, []string{"history"}, nil, nil)
	if code != 2 {
		t.Fatalf("no subcommand exit = %d, want 2", code)
	}
	if stderr == "" {
		t.Error("expected a usage message on stderr")
	}
}
