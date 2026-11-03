package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// isolateProbe points both the config and cache directories at fresh temp dirs.
func isolateProbe(t *testing.T) string {
	t.Helper()
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	return isolateConfig(t)
}

func saveProbeSources(t *testing.T, defs map[string]Definition) {
	t.Helper()
	if err := Save(File{Sources: defs}); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// probeFetch returns a Fetcher that records its calls and answers with res/err.
type probeFetch struct {
	calls []Definition
	etags []string
	res   FetchResult
	err   error
}

func (p *probeFetch) fetch(_ context.Context, def Definition, etag string) (FetchResult, error) {
	p.calls = append(p.calls, def)
	p.etags = append(p.etags, etag)
	return p.res, p.err
}

func TestRunProbeRefusesTestBinary(t *testing.T) {
	_, err := RunProbe(context.Background(), "web")
	if !errors.Is(err, ErrProbeUnavailable) {
		t.Fatalf("RunProbe under go test = %v, want ErrProbeUnavailable", err)
	}
}

func TestServeProbeSuccessIsOneCompactLine(t *testing.T) {
	isolateProbe(t)
	saveProbeSources(t, map[string]Definition{
		"web": {Kind: KindHTTP, URL: "http://127.0.0.1:1/x"},
	})
	pf := &probeFetch{res: FetchResult{Body: []byte("{\n  \"a\": 1\n}")}}

	var buf bytes.Buffer
	if err := ServeProbe(context.Background(), "web", &buf, pf.fetch); err != nil {
		t.Fatalf("ServeProbe: %v", err)
	}
	if got, want := buf.String(), "{\"ok\":true,\"body\":{\"a\":1}}\n"; got != want {
		t.Fatalf("ServeProbe output = %q, want %q", got, want)
	}
}

func TestServeProbeFailuresAreOneLine(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T)
		id        string
		fetch     *probeFetch
		want      string
		wantCalls int
	}{
		{
			name: "unknown id",
			setup: func(t *testing.T) {
				saveProbeSources(t, map[string]Definition{"web": {Kind: KindHTTP, URL: "http://127.0.0.1:1/x"}})
			},
			id:    "missing",
			fetch: &probeFetch{},
			want:  "{\"ok\":false,\"error\":\"unknown source\"}\n",
		},
		{
			name: "invalid sources.json",
			setup: func(t *testing.T) {
				p, err := Path()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("not json"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			id:    "web",
			fetch: &probeFetch{},
			want:  "", // filled in below from Load's own error text
		},
		{
			name: "fetch error with quotes and newline",
			setup: func(t *testing.T) {
				saveProbeSources(t, map[string]Definition{"web": {Kind: KindHTTP, URL: "http://127.0.0.1:1/x"}})
			},
			id:        "web",
			fetch:     &probeFetch{err: errors.New("source \"web\": connection refused\nretry")},
			want:      "{\"ok\":false,\"error\":\"source \\\"web\\\": connection refused\\nretry\"}\n",
			wantCalls: 1,
		},
		{
			name: "not modified counts as failure",
			setup: func(t *testing.T) {
				saveProbeSources(t, map[string]Definition{"web": {Kind: KindHTTP, URL: "http://127.0.0.1:1/x"}})
			},
			id:        "web",
			fetch:     &probeFetch{res: FetchResult{NotModified: true}},
			want:      "{\"ok\":false,\"error\":\"probe failed\"}\n",
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateProbe(t)
			tt.setup(t)
			var buf bytes.Buffer
			if err := ServeProbe(context.Background(), tt.id, &buf, tt.fetch.fetch); err != nil {
				t.Fatalf("ServeProbe: %v", err)
			}
			got := buf.String()
			if strings.Count(got, "\n") != 1 || !strings.HasSuffix(got, "\n") {
				t.Fatalf("output %q is not exactly one line", got)
			}
			if tt.name == "invalid sources.json" {
				_, loadErr := Load()
				if loadErr == nil {
					t.Fatal("Load() succeeded on invalid sources.json")
				}
				var out ProbeOutput
				if err := json.Unmarshal([]byte(got), &out); err != nil {
					t.Fatalf("decode %q: %v", got, err)
				}
				if out.OK || out.Error != loadErr.Error() {
					t.Fatalf("output = %+v, want error %q", out, loadErr.Error())
				}
				if len(tt.fetch.calls) != 0 {
					t.Fatalf("fetch called %d times for invalid sources.json", len(tt.fetch.calls))
				}
				return
			}
			if got != tt.want {
				t.Fatalf("output = %q, want %q", got, tt.want)
			}
			if len(tt.fetch.calls) != tt.wantCalls {
				t.Fatalf("fetch calls = %d, want %d", len(tt.fetch.calls), tt.wantCalls)
			}
		})
	}
}

func TestServeProbeFetchesOnceWithoutETag(t *testing.T) {
	isolateProbe(t)
	saveProbeSources(t, map[string]Definition{"web": {Kind: KindHTTP, URL: "http://127.0.0.1:1/x"}})
	pf := &probeFetch{res: FetchResult{Body: []byte(`{"a":1}`), ETag: "\"v1\""}}

	var buf bytes.Buffer
	if err := ServeProbe(context.Background(), "web", &buf, pf.fetch); err != nil {
		t.Fatalf("ServeProbe: %v", err)
	}
	if len(pf.calls) != 1 {
		t.Fatalf("fetch calls = %d, want 1", len(pf.calls))
	}
	if pf.etags[0] != "" {
		t.Fatalf("etag sent = %q, want empty", pf.etags[0])
	}
	if pf.calls[0].ID != "web" {
		t.Fatalf("def.ID = %q, want %q", pf.calls[0].ID, "web")
	}
}

func TestServeProbeLeavesCacheUntouched(t *testing.T) {
	isolateProbe(t)
	saveProbeSources(t, map[string]Definition{"web": {Kind: KindHTTP, URL: "http://127.0.0.1:1/x"}})
	pf := &probeFetch{res: FetchResult{Body: []byte(`{"a":1}`)}}

	var buf bytes.Buffer
	if err := ServeProbe(context.Background(), "web", &buf, pf.fetch); err != nil {
		t.Fatalf("ServeProbe: %v", err)
	}
	entries, err := os.ReadDir(os.Getenv("STATUSLOOM_CACHE_DIR"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache dir has %d entries after a probe, want 0", len(entries))
	}
}

func TestDecodeProbeOutputPreservesChildError(t *testing.T) {
	isolateProbe(t)
	const msg = "source \"web\": connection refused\nretry \"now\""
	saveProbeSources(t, map[string]Definition{"web": {Kind: KindHTTP, URL: "http://127.0.0.1:1/x"}})
	pf := &probeFetch{err: errors.New(msg)}

	var buf bytes.Buffer
	if err := ServeProbe(context.Background(), "web", &buf, pf.fetch); err != nil {
		t.Fatalf("ServeProbe: %v", err)
	}
	_, err := decodeProbeOutput(buf.Bytes())
	if err == nil || err.Error() != msg {
		t.Fatalf("decodeProbeOutput error = %v, want %q", err, msg)
	}
}

func TestDecodeProbeOutputRoundTripsBody(t *testing.T) {
	isolateProbe(t)
	saveProbeSources(t, map[string]Definition{"web": {Kind: KindHTTP, URL: "http://127.0.0.1:1/x"}})
	pf := &probeFetch{res: FetchResult{Body: []byte("{\n  \"a\": 1\n}")}}

	var buf bytes.Buffer
	if err := ServeProbe(context.Background(), "web", &buf, pf.fetch); err != nil {
		t.Fatalf("ServeProbe: %v", err)
	}
	body, err := decodeProbeOutput(buf.Bytes())
	if err != nil {
		t.Fatalf("decodeProbeOutput: %v", err)
	}
	var got any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body %q is not JSON: %v", body, err)
	}
	if !reflect.DeepEqual(got, map[string]any{"a": float64(1)}) {
		t.Fatalf("body = %v, want {a:1}", got)
	}
}

func TestFetchOnceFileReadsInProcess(t *testing.T) {
	isolateProbe(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(`{"n":2,"s":["x"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	def := Definition{ID: "f", Kind: KindFile, Path: path}
	probe := func(context.Context, string) ([]byte, error) {
		t.Fatal("probe must not be called for a file source")
		return nil, nil
	}

	got, err := FetchOnce(context.Background(), def, probe)
	if err != nil {
		t.Fatalf("FetchOnce: %v", err)
	}
	want := map[string]any{"n": float64(2), "s": []any{"x"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FetchOnce = %#v, want %#v", got, want)
	}
}

func TestFetchOnceCommandUsesProbeAndKeepsError(t *testing.T) {
	var calls []string
	probe := func(_ context.Context, id string) ([]byte, error) {
		calls = append(calls, id)
		return nil, errors.New("boom")
	}
	def := Definition{ID: "c", Kind: KindCommand, Argv: []string{"true"}}

	_, err := FetchOnce(context.Background(), def, probe)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("FetchOnce error = %v, want boom", err)
	}
	if !reflect.DeepEqual(calls, []string{"c"}) {
		t.Fatalf("probe calls = %v, want [c]", calls)
	}
}
