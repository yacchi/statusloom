// Package gitstatus collects a lightweight snapshot of the git
// repository (if any) surrounding a directory, suitable for embedding
// in a status line that must render quickly. All git invocations run
// with a bounded timeout, disable optional locks so they never block
// on or interfere with a concurrently running git command, and never
// read stdin.
package gitstatus

import (
	"context"
	"errors"
	"time"

	"github.com/yacchi/statusloom/internal/schema"
)

// defaultTimeout is used when Options.Timeout is zero.
const defaultTimeout = 200 * time.Millisecond

// Options configures a single Collect invocation.
type Options struct {
	// Timeout bounds every git invocation made during Collect. Zero
	// means defaultTimeout (200ms).
	Timeout time.Duration

	// IncludeUntracked requests untracked file information from
	// `git status` (--untracked-files=normal vs. --untracked-files=no).
	IncludeUntracked bool

	// CollectNumstat additionally runs `git diff --numstat` and
	// `git diff --cached --numstat` to populate Added/Deleted line
	// counts. Failures collecting numstat are non-fatal.
	CollectNumstat bool
}

// Collect gathers git information for dir.
//
// When dir is not inside a git working tree, Collect returns
// (nil, nil) — this is the expected, common case for statusloom
// running outside a repository, not an error.
//
// When a git invocation times out or otherwise fails, Collect returns
// (nil, err); callers that want stale-cache fallback behavior should
// handle that at a layer above this package.
func Collect(ctx context.Context, dir string, opts Options) (*schema.RepositorySnapshot, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	root, err := repoRoot(ctx, dir, timeout)
	if err != nil {
		if errors.Is(err, errNotARepository) {
			return nil, nil
		}
		return nil, err
	}

	statusArgs := []string{"status", "--porcelain=v2", "--branch"}
	if opts.IncludeUntracked {
		statusArgs = append(statusArgs, "--untracked-files=normal")
	} else {
		statusArgs = append(statusArgs, "--untracked-files=no")
	}

	out, err := runGit(ctx, dir, timeout, statusArgs...)
	if err != nil {
		return nil, err
	}

	snap := parseStatusPorcelainV2(out)
	snap.Root = root

	if opts.CollectNumstat {
		snap.Added, snap.Deleted = collectNumstat(ctx, dir, timeout)
	}

	return &snap, nil
}

// collectNumstat sums added/deleted line counts across unstaged and
// staged diffs. Failures are swallowed: a failing `git diff` (e.g. in
// a repo with no commits yet) simply contributes 0, matching the
// "numstat failure is non-fatal" requirement.
func collectNumstat(ctx context.Context, dir string, timeout time.Duration) (added, deleted int) {
	numstatArgs := [][]string{
		{"diff", "--numstat"},
		{"diff", "--cached", "--numstat"},
	}
	for _, args := range numstatArgs {
		out, err := runGit(ctx, dir, timeout, args...)
		if err != nil {
			continue
		}
		a, d := parseNumstat(out)
		added += a
		deleted += d
	}
	return added, deleted
}
