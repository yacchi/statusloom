package gitstatus

import (
	"strconv"
	"strings"

	"github.com/yacchi/statusloom/internal/schema"
)

const detachedBranchLabel = "(detached)"

// splitCompleteLines splits data on "\n" and drops the final element.
// When data ends in "\n" that final element is the empty string
// produced by the trailing separator, so nothing is lost. When data
// does not end in "\n" (e.g. output truncated by the capture-size
// cap) the final element is a partial line, and dropping it is
// exactly the "tolerate a truncated last line by ignoring it"
// behavior callers need.
func splitCompleteLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	return lines[:len(lines)-1]
}

// parseStatusPorcelainV2 parses the output of
// `git status --porcelain=v2 --branch [--untracked-files=...]` into a
// RepositorySnapshot. The Root field is left zero-valued; the caller
// fills it in separately.
func parseStatusPorcelainV2(data []byte) schema.RepositorySnapshot {
	var snap schema.RepositorySnapshot
	var oid string

	for _, line := range splitCompleteLines(data) {
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "# branch.head "):
			snap.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.oid "):
			oid = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.ab "):
			snap.Ahead, snap.Behind = parseAheadBehind(strings.TrimPrefix(line, "# branch.ab "))
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			if xy, ok := entryXY(line); ok {
				if xy[0] != '.' {
					snap.Staged++
				}
				if xy[1] != '.' {
					snap.Unstaged++
				}
			}
		case strings.HasPrefix(line, "u "):
			snap.Unstaged++
		case strings.HasPrefix(line, "? "):
			snap.Untracked++
		}
		// "! " (ignored) lines are not requested by --untracked-files and
		// carry no counts; nothing to do for them.
	}

	if snap.Branch == detachedBranchLabel {
		if oid != "" && oid != "(initial)" {
			if len(oid) > 7 {
				snap.Branch = oid[:7]
			} else {
				snap.Branch = oid
			}
		}
		// oid missing/"(initial)": keep the "(detached)" label as-is.
	}

	snap.Dirty = snap.Staged+snap.Unstaged+snap.Untracked > 0

	return snap
}

// entryXY extracts the two-character XY status code from a
// porcelain v2 "1 ..." (ordinary changed) or "2 ..." (renamed/copied)
// entry line.
func entryXY(line string) (string, bool) {
	fields := strings.SplitN(line, " ", 4)
	if len(fields) < 3 {
		return "", false
	}
	xy := fields[1]
	if len(xy) != 2 {
		return "", false
	}
	return xy, true
}

// parseAheadBehind parses the "+A -B" payload of a "# branch.ab"
// line. Fields that fail to parse are ignored so a malformed or
// truncated value degrades to 0 rather than propagating an error.
func parseAheadBehind(s string) (ahead, behind int) {
	for _, field := range strings.Fields(s) {
		if len(field) < 2 {
			continue
		}
		n, err := strconv.Atoi(field[1:])
		if err != nil {
			continue
		}
		switch field[0] {
		case '+':
			ahead = n
		case '-':
			behind = n
		}
	}
	return ahead, behind
}

// parseNumstat parses the tab-separated output of
// `git diff --numstat` (or --cached --numstat), summing added and
// deleted line counts. Binary entries (added/deleted rendered as
// "-") are skipped, as are malformed/truncated lines.
func parseNumstat(data []byte) (added, deleted int) {
	for _, line := range splitCompleteLines(data) {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "-" || fields[1] == "-" {
			continue // binary file, no line counts
		}
		a, errA := strconv.Atoi(fields[0])
		d, errD := strconv.Atoi(fields[1])
		if errA != nil || errD != nil {
			continue
		}
		added += a
		deleted += d
	}
	return added, deleted
}
