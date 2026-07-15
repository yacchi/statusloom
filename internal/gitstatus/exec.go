package gitstatus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// maxCapturedOutput caps how much stdout we buffer from a single git
// invocation. Exceeding the cap is not treated as an error: the extra
// bytes are simply discarded and downstream parsers tolerate a
// truncated final line.
const maxCapturedOutput = 1 << 20 // 1MB

// maxCapturedStderr caps how much stderr we buffer, defensively, since
// stderr is only ever used to build an error message.
const maxCapturedStderr = 64 << 10 // 64KB

// errNotARepository is a sentinel returned by repoRoot when dir is not
// inside a git working tree.
var errNotARepository = errors.New("gitstatus: not a git repository")

// gitError wraps a failed git invocation with enough context (exit
// code, stderr) to classify the failure without re-parsing error
// strings at every call site.
type gitError struct {
	args     []string
	exitCode int
	stderr   string
	err      error
}

func (e *gitError) Error() string {
	msg := strings.TrimSpace(e.stderr)
	if msg == "" {
		msg = e.err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.args, " "), msg)
}

func (e *gitError) Unwrap() error { return e.err }

// limitedBuffer is an io.Writer that stops accumulating bytes past
// limit but never reports an error to the writer (matching how
// exec.Cmd expects Stdout/Stderr to behave).
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.limit <= 0 || l.buf.Len() >= l.limit {
		return len(p), nil
	}
	remaining := l.limit - l.buf.Len()
	if len(p) <= remaining {
		l.buf.Write(p)
	} else {
		l.buf.Write(p[:remaining])
	}
	return len(p), nil
}

// runGit executes `git --no-optional-locks -C dir <args...>` with the
// given timeout (falling back to defaultTimeout when timeout <= 0),
// no stdin, GIT_OPTIONAL_LOCKS=0 in the environment, and a capped
// stdout buffer.
func runGit(ctx context.Context, dir string, timeout time.Duration, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fullArgs := make([]string, 0, len(args)+3)
	fullArgs = append(fullArgs, "--no-optional-locks", "-C", dir)
	fullArgs = append(fullArgs, args...)

	cmd := exec.CommandContext(runCtx, "git", fullArgs...)
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")

	stdout := &limitedBuffer{limit: maxCapturedOutput}
	stderr := &limitedBuffer{limit: maxCapturedStderr}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	if err != nil {
		if ctxErr := runCtx.Err(); ctxErr != nil {
			return stdout.buf.Bytes(), ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.buf.Bytes(), &gitError{
				args:     fullArgs,
				exitCode: exitErr.ExitCode(),
				stderr:   stderr.buf.String(),
				err:      err,
			}
		}
		return stdout.buf.Bytes(), err
	}

	return stdout.buf.Bytes(), nil
}

// repoRoot resolves the top-level directory of the git repository
// containing dir. It returns errNotARepository (wrapped, checkable
// via errors.Is) when dir is not inside a git working tree.
func repoRoot(ctx context.Context, dir string, timeout time.Duration) (string, error) {
	out, err := runGit(ctx, dir, timeout, "rev-parse", "--show-toplevel")
	if err != nil {
		if isNotARepoError(err) {
			return "", errNotARepository
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isNotARepoError reports whether err represents git rev-parse
// failing because dir is not inside a git working tree.
func isNotARepoError(err error) bool {
	var ge *gitError
	if !errors.As(err, &ge) {
		return false
	}
	if ge.exitCode == 128 {
		return true
	}
	return strings.Contains(ge.stderr, "not a git repository")
}
