package cli

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/yacchi/statusloom/internal/modrec"
)

// runModIngest implements `statusloom mod-ingest`: the statusloom Claude Code mod
// pipes its recorded events and queries as one JSON object on stdin. It writes
// nothing to stdout.
func runModIngest(args []string, stdin io.Reader, _ io.Writer, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "statusloom mod-ingest: unexpected argument %q\n", args[0])
		fmt.Fprintln(stderr, "usage: statusloom mod-ingest < request.json")
		return 2
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes))
	if err != nil {
		return fail(stderr, err)
	}
	err = modrec.Ingest(raw, time.Now())
	switch {
	case errors.Is(err, modrec.ErrUnsupportedVersion):
		return 0
	case err != nil:
		return fail(stderr, err)
	}
	return 0
}
