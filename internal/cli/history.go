package cli

// runHistory implements `statusloom history list|show <id>|diff <id> [<id2>]|
// restore <id> [--force]` (plans/config-store-and-format.md §8.12): a thin
// CLI wrapper over internal/store's history DAG surface
// (Revisions/Revision/Restore). Diffing uses a small standard-library-only
// line diff (diff.go), never an external `diff` binary. restore refuses to
// run (unless --force) when tool has a draft working node that differs from
// current, since store.Restore discards it unconditionally and
// unrecoverably (§4.2).

import (
	"fmt"
	"io"
	"time"

	"github.com/yacchi/statusloom/internal/store"
)

// historyDefaultTool is the tool `statusloom history` acts on when --tool is
// omitted, matching every other single-tool subcommand (draft, fmt, monitor).
const historyDefaultTool = "claude-code"

func runHistory(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "statusloom history: expected a subcommand (list|show|diff|restore)")
		return 2
	}
	sub := args[0]
	tool, rest, err := extractToolFlag(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "statusloom history: %s\n", err)
		return 2
	}

	switch sub {
	case "list":
		if len(rest) != 0 {
			fmt.Fprintln(stderr, "statusloom history list: unexpected arguments")
			return 2
		}
		return runHistoryList(tool, stdout, stderr)

	case "show":
		if len(rest) != 1 {
			fmt.Fprintln(stderr, "statusloom history show: expected <id>")
			return 2
		}
		return runHistoryShow(rest[0], stdout, stderr)

	case "diff":
		if len(rest) < 1 || len(rest) > 2 {
			fmt.Fprintln(stderr, "statusloom history diff: expected <id> or <id1> <id2>")
			return 2
		}
		return runHistoryDiff(tool, rest, stdout, stderr)

	case "restore":
		force, restArgs := scanForceFlag(rest)
		if len(restArgs) != 1 {
			fmt.Fprintln(stderr, "statusloom history restore: expected <id> [--force]")
			return 2
		}
		return runHistoryRestore(tool, restArgs[0], force, stdout, stderr)

	default:
		fmt.Fprintf(stderr, "statusloom history: unknown subcommand %q (want list|show|diff|restore)\n", sub)
		return 2
	}
}

// extractToolFlag pulls a "--tool VALUE" or "--tool=VALUE" pair out of args
// (in any position, mirroring the manual flag handling `statusloom fmt`
// already uses for its own non-flag.FlagSet parsing), defaulting to
// historyDefaultTool when absent, and returns the remaining positional
// arguments in order. It is scanToolFlag (import.go) with historyDefaultTool
// filled in — the single place both subcommands' "--tool" parsing lives.
func extractToolFlag(args []string) (tool string, rest []string, err error) {
	tool, found, rest, err := scanToolFlag(args)
	if err != nil {
		return "", nil, err
	}
	if !found {
		tool = historyDefaultTool
	}
	return tool, rest, nil
}

// runHistoryList prints tool's revisions oldest-first (Revisions' order),
// marking the current one, one line per revision: id, savedAt, origin, and
// meta.name (or a placeholder when unnamed).
func runHistoryList(tool string, stdout, stderr io.Writer) int {
	st, err := store.Open()
	if err != nil {
		return fail(stderr, err)
	}
	revs, err := st.Revisions(tool)
	if err != nil {
		return fail(stderr, err)
	}
	if len(revs) == 0 {
		fmt.Fprintf(stdout, "no history for %s\n", tool)
		return 0
	}
	_, curID, _, err := st.Current(tool)
	if err != nil {
		return fail(stderr, err)
	}
	for _, rev := range revs {
		marker := "  "
		if rev.ID == curID {
			marker = "* "
		}
		name := rev.Meta.Name
		if name == "" {
			name = "(no name)"
		}
		fmt.Fprintf(stdout, "%s%s  %s  %-8s  %s\n", marker, rev.ID, rev.SavedAt.Format(time.RFC3339), rev.Origin, name)
	}
	return 0
}

// runHistoryShow prints one revision's full metadata and source.
func runHistoryShow(id string, stdout, stderr io.Writer) int {
	st, err := store.Open()
	if err != nil {
		return fail(stderr, err)
	}
	rev, ok, err := st.Revision(id)
	if err != nil {
		return fail(stderr, err)
	}
	if !ok {
		fmt.Fprintf(stderr, "statusloom history show: unknown revision %q\n", id)
		return 1
	}

	parent := "(none)"
	if rev.Parent != nil {
		parent = *rev.Parent
	}
	fmt.Fprintf(stdout, "id:          %s\n", id)
	fmt.Fprintf(stdout, "tool:        %s\n", rev.Tool)
	fmt.Fprintf(stdout, "parent:      %s\n", parent)
	fmt.Fprintf(stdout, "savedAt:     %s\n", rev.SavedAt.Format(time.RFC3339))
	fmt.Fprintf(stdout, "origin:      %s\n", rev.Origin)
	fmt.Fprintf(stdout, "name:        %s\n", rev.Meta.Name)
	fmt.Fprintf(stdout, "description: %s\n", rev.Meta.Description)
	fmt.Fprintf(stdout, "author:      %s\n", rev.Meta.Author)
	if rev.Meta.Notes != "" {
		fmt.Fprintf(stdout, "notes:\n%s\n", rev.Meta.Notes)
	}
	fmt.Fprintln(stdout, "---")
	fmt.Fprintln(stdout, rev.Source)
	return 0
}

// runHistoryDiff prints a unified diff. One id diffs against tool's current
// revision; two ids diff against each other.
func runHistoryDiff(tool string, ids []string, stdout, stderr io.Writer) int {
	st, err := store.Open()
	if err != nil {
		return fail(stderr, err)
	}

	var fromID, toID, fromSrc, toSrc string

	if len(ids) == 1 {
		rev, ok, err := st.Revision(ids[0])
		if err != nil {
			return fail(stderr, err)
		}
		if !ok {
			fmt.Fprintf(stderr, "statusloom history diff: unknown revision %q\n", ids[0])
			return 1
		}
		cur, curID, ok, err := st.Current(tool)
		if err != nil {
			return fail(stderr, err)
		}
		if !ok {
			fmt.Fprintf(stderr, "statusloom history diff: %s has no current revision to diff against\n", tool)
			return 1
		}
		fromID, fromSrc = ids[0], rev.Source
		toID, toSrc = curID, cur.Source
	} else {
		fr, ok, err := st.Revision(ids[0])
		if err != nil {
			return fail(stderr, err)
		}
		if !ok {
			fmt.Fprintf(stderr, "statusloom history diff: unknown revision %q\n", ids[0])
			return 1
		}
		tr, ok, err := st.Revision(ids[1])
		if err != nil {
			return fail(stderr, err)
		}
		if !ok {
			fmt.Fprintf(stderr, "statusloom history diff: unknown revision %q\n", ids[1])
			return 1
		}
		fromID, fromSrc = ids[0], fr.Source
		toID, toSrc = ids[1], tr.Source
	}

	if fromSrc == toSrc {
		fmt.Fprintf(stdout, "no differences between %s and %s\n", fromID, toID)
		return 0
	}
	fmt.Fprint(stdout, unifiedDiff(fromID, toID, fromSrc, toSrc))
	return 0
}

// scanForceFlag extracts a bare "--force" boolean flag from args (in any
// position), mirroring scanToolFlag's shape for the other manually-parsed
// flags this package uses. The remaining positional arguments are returned
// in original order.
func scanForceFlag(args []string) (force bool, rest []string) {
	rest = make([]string, 0, len(args))
	for _, a := range args {
		if a == "--force" {
			force = true
			continue
		}
		rest = append(rest, a)
	}
	return force, rest
}

// runHistoryRestore repoints tool's current ref at id, discarding any
// unsaved draft (store.Restore; §4.2). Because that discard is silent and
// unrecoverable, restore first checks whether tool has a draft working node
// that actually differs from the current revision's source ("dirty"): if so
// and force is false, nothing is written — the command fails with a message
// telling the user to pass --force to proceed and discard it. A draft that
// is absent, or present but identical to current, restores exactly as
// before (§4.2, §8.12).
func runHistoryRestore(tool, id string, force bool, stdout, stderr io.Writer) int {
	st, err := store.Open()
	if err != nil {
		return fail(stderr, err)
	}

	if !force {
		dirty, err := hasDirtyDraft(st, tool)
		if err != nil {
			return fail(stderr, err)
		}
		if dirty {
			fmt.Fprintf(stderr, "statusloom history restore: %s has an unsaved draft that would be discarded; rerun with --force to discard it and restore\n", tool)
			return 1
		}
	}

	if err := st.Restore(tool, id); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "Restored %s to revision %s (any unsaved draft was discarded)\n", tool, id)
	return 0
}

// hasDirtyDraft reports whether tool has a draft working node whose source
// differs from the current revision's source. No draft, or a draft that
// happens to match current byte-for-byte, is not dirty. A tool with no
// current revision yet but a draft present counts as dirty (there is
// nothing for the draft to match).
func hasDirtyDraft(st *store.Store, tool string) (bool, error) {
	draftSrc, hasDraft, err := st.ReadDraft(tool)
	if err != nil {
		return false, err
	}
	if !hasDraft {
		return false, nil
	}
	cur, _, hasCurrent, err := st.Current(tool)
	if err != nil {
		return false, err
	}
	if !hasCurrent {
		return true, nil
	}
	return draftSrc != cur.Source, nil
}
