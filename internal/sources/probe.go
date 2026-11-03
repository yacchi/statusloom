package sources

// This file implements explicit one-shot source tests. A command or http
// source is never fetched in the calling process: the caller re-runs the
// statusloom executable as `statusloom refresh --probe <id>`, and that child
// (ServeProbe) fetches once and writes one JSON line. Network access and
// external commands therefore stay inside the refresh subprocess.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ProbeOutput is the single JSON object (one line, then "\n") the probe child writes to stdout.
type ProbeOutput struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"` // set when !OK
	Body  json.RawMessage `json:"body,omitempty"`  // the fetched JSON document when OK
}

// ProbeFunc fetches the SAVED source id once inside a refresh subprocess and returns
// the raw JSON body (or the error text the child reported).
type ProbeFunc func(ctx context.Context, id string) ([]byte, error)

// ErrProbeUnavailable is returned by RunProbe when the running executable is a go test
// binary (name ends in ".test" or ".test.exe"): re-running it would re-run the test suite.
var ErrProbeUnavailable = errors.New("source probe is unavailable in tests")

// ServeProbe implements the child side of `statusloom refresh --probe <id>`. It
// loads sources.json, looks up id, and fetches once with no ETag. It never reads
// or writes the cache envelopes, the refresh schedule, or the refresh lease. It
// writes exactly one ProbeOutput line to w; the returned error is only a
// write/encode error, and every other failure is reported inside the line.
func ServeProbe(ctx context.Context, id string, w io.Writer, fetch Fetcher) error {
	out := ProbeOutput{}
	f, err := Load()
	if err != nil {
		out.Error = err.Error()
		return json.NewEncoder(w).Encode(out)
	}
	def, ok := f.Get(id)
	if !ok {
		out.Error = "unknown source"
		return json.NewEncoder(w).Encode(out)
	}
	res, err := fetch(ctx, def, "")
	if err != nil {
		out.Error = err.Error()
		return json.NewEncoder(w).Encode(out)
	}
	// A nil Body (NotModified) is not valid JSON, so it lands here too.
	if !json.Valid(res.Body) {
		out.Error = "probe failed"
		return json.NewEncoder(w).Encode(out)
	}
	out.OK = true
	out.Body = json.RawMessage(res.Body)
	// encoding/json compacts RawMessage, so a pretty-printed body stays on one line.
	return json.NewEncoder(w).Encode(out)
}

// RunProbe is the production ProbeFunc: it runs `statusloom refresh --probe <id>`
// as a child process and decodes its ProbeOutput. stdin and stderr are discarded,
// the environment is inherited, and stdout is capped at MaxBodyBytes+64 KiB.
func RunProbe(ctx context.Context, id string) ([]byte, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// Never re-exec a `go test` binary: the child would re-run the whole suite.
	if strings.HasSuffix(exe, ".test") || strings.HasSuffix(exe, ".test.exe") {
		return nil, ErrProbeUnavailable
	}
	cmd := exec.CommandContext(ctx, exe, "refresh", "--probe", id)
	// Stdin and Stderr stay nil, so the child gets the null device.
	cmd.WaitDelay = 2 * time.Second
	capped := &cappedBuffer{max: MaxBodyBytes + 64<<10}
	cmd.Stdout = capped
	if err := cmd.Run(); cmd.Process == nil {
		// The process never started.
		return nil, err
	}
	// The exit status is ignored: the child reports failures in its JSON line.
	return decodeProbeOutput(capped.buf.Bytes())
}

// cappedBuffer keeps at most max bytes and drops the rest. Write always reports
// the full length, so the child never blocks on a full pipe.
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		c.buf.Write(p)
	}
	return len(p), nil
}

// decodeProbeOutput turns the child's stdout into the body, or the error the child
// reported. The child's error text is returned unchanged.
func decodeProbeOutput(stdout []byte) ([]byte, error) {
	var out ProbeOutput
	if err := json.NewDecoder(bytes.NewReader(stdout)).Decode(&out); err != nil {
		return nil, errors.New("probe failed")
	}
	if !out.OK {
		if out.Error == "" {
			return nil, errors.New("probe failed")
		}
		return nil, errors.New(out.Error)
	}
	if len(out.Body) == 0 {
		return nil, errors.New("probe failed")
	}
	return []byte(out.Body), nil
}

// FetchOnce fetches def once for an explicit test and decodes the body with
// encoding/json into `any` (numbers float64). A file source is read in-process.
// A command or http source goes through probe. The body is never written to the cache.
func FetchOnce(ctx context.Context, def Definition, probe ProbeFunc) (any, error) {
	var body []byte
	switch def.Kind {
	case KindFile:
		path, err := def.ExpandedPath()
		if err != nil {
			return nil, err
		}
		b, _, err := ReadFileSource(path)
		if err != nil {
			return nil, err
		}
		body = b
	case KindCommand, KindHTTP:
		b, err := probe(ctx, def.ID)
		if err != nil {
			return nil, err
		}
		body = b
	default:
		return nil, fmt.Errorf("source %q: unknown kind %q (want file, command or http)", def.ID, def.Kind)
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, err
	}
	return v, nil
}
