package webconfig

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/yacchi/statusloom/internal/sources"
)

// setupSourcesTest isolates config, cache and Claude Code files for one test
// and starts a configurator server against them.
func setupSourcesTest(t *testing.T) (cfgDir, cacheDir string, ts *testServer) {
	t.Helper()
	isolateClaudeConfig(t)
	cfgDir = t.TempDir()
	cacheDir = t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", cfgDir)
	t.Setenv("STATUSLOOM_CACHE_DIR", cacheDir)
	ts = startTestServer(t, time.Minute)
	return cfgDir, cacheDir, ts
}

// swapSourceProbe installs fake as the probe seam for the rest of the test.
func swapSourceProbe(t *testing.T, fake sources.ProbeFunc) {
	t.Helper()
	old := sourceProbe
	sourceProbe = fake
	t.Cleanup(func() { sourceProbe = old })
}

// sourcesJSON issues an authenticated request and decodes the JSON object
// body. It returns the status code and the decoded map.
func sourcesJSON(t *testing.T, ts *testServer, method, path string, body []byte) (int, map[string]any) {
	t.Helper()
	resp := authedRequest(t, ts, method, path, body)
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: decode body: %v", method, path, err)
	}
	return resp.StatusCode, out
}

func TestSourcesRoutesRequireBearerToken(t *testing.T) {
	_, _, ts := setupSourcesTest(t)
	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/sources"},
		{http.MethodPut, "/api/sources/x"},
		{http.MethodDelete, "/api/sources/x"},
		{http.MethodPost, "/api/sources/x/test"},
	}
	for _, c := range cases {
		req, err := http.NewRequest(c.method, ts.baseURL+c.path, strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without token: status %d, want 401", c.method, c.path, resp.StatusCode)
		}
	}
}

func TestPutSourceReturnsEntryWithAllKeys(t *testing.T) {
	cfgDir, _, ts := setupSourcesTest(t)

	code, out := sourcesJSON(t, ts, http.MethodPut, "/api/sources/myhttp",
		[]byte(`{"kind":"http","url":"http://127.0.0.1:47921/s.json","allowRemote":true,"ttlSeconds":6}`))
	if code != http.StatusOK {
		t.Fatalf("PUT status %d, want 200 (%v)", code, out)
	}
	src, ok := out["source"].(map[string]any)
	if !ok {
		t.Fatalf("response has no source object: %v", out)
	}
	for _, key := range []string{"id", "kind", "path", "argv", "url", "allowRemote", "ttlSeconds", "staleAfterSeconds", "timeoutSeconds", "warnings"} {
		if _, present := src[key]; !present {
			t.Errorf("source entry missing key %q", key)
		}
	}
	if argv, _ := src["argv"].([]any); argv == nil || len(argv) != 0 {
		t.Errorf("argv = %#v, want empty array", src["argv"])
	}
	warnings, _ := src["warnings"].([]any)
	if len(warnings) != 2 || warnings[0] != "allow-remote" || warnings[1] != "short-ttl" {
		t.Errorf("warnings = %#v, want [allow-remote short-ttl]", src["warnings"])
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "sources.json")); err != nil {
		t.Errorf("sources.json not written: %v", err)
	}
}

func TestPutInvalidSourceWritesNothing(t *testing.T) {
	cfgDir, _, ts := setupSourcesTest(t)

	code, out := sourcesJSON(t, ts, http.MethodPut, "/api/sources/bad",
		[]byte(`{"kind":"command","argv":["x"],"ttlSeconds":3}`))
	if code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", code)
	}
	if got, want := out["error"], `source "bad": ttlSeconds must be at least 5`; got != want {
		t.Errorf("error = %v, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "sources.json")); !os.IsNotExist(err) {
		t.Errorf("sources.json exists after rejected PUT (err=%v)", err)
	}

	code, out = sourcesJSON(t, ts, http.MethodPut, "/api/sources/Bad_ID", []byte(`{"kind":"file","path":"/x"}`))
	if code != http.StatusBadRequest {
		t.Fatalf("invalid id: status %d, want 400", code)
	}
	if msg, _ := out["error"].(string); !strings.HasPrefix(msg, `invalid source id "Bad_ID"`) {
		t.Errorf("invalid id error = %q", msg)
	}
}

func TestPutSourceRejectsMalformedAndOversizedBodies(t *testing.T) {
	_, _, ts := setupSourcesTest(t)

	code, _ := sourcesJSON(t, ts, http.MethodPut, "/api/sources/x", []byte(`{"kind":`))
	if code != http.StatusBadRequest {
		t.Errorf("malformed body: status %d, want 400", code)
	}
	big := []byte(`{"kind":"file","path":"` + strings.Repeat("a", 70<<10) + `"}`)
	code, _ = sourcesJSON(t, ts, http.MethodPut, "/api/sources/x", big)
	if code != http.StatusBadRequest {
		t.Errorf("70 KiB body: status %d, want 400", code)
	}
}

func TestListAndDeleteSources(t *testing.T) {
	_, _, ts := setupSourcesTest(t)

	code, out := sourcesJSON(t, ts, http.MethodGet, "/api/sources", nil)
	if code != http.StatusOK {
		t.Fatalf("empty list status %d", code)
	}
	if list, ok := out["sources"].([]any); !ok || len(list) != 0 {
		t.Errorf("empty list sources = %#v, want []", out["sources"])
	}

	sourcesJSON(t, ts, http.MethodPut, "/api/sources/myhttp",
		[]byte(`{"kind":"http","url":"http://127.0.0.1:47921/s.json","allowRemote":true,"ttlSeconds":6}`))
	sourcesJSON(t, ts, http.MethodPut, "/api/sources/aaa", []byte(`{"kind":"file","path":"/tmp/x.json"}`))

	_, out = sourcesJSON(t, ts, http.MethodGet, "/api/sources", nil)
	list, _ := out["sources"].([]any)
	if len(list) != 2 {
		t.Fatalf("list has %d entries, want 2", len(list))
	}
	first, _ := list[0].(map[string]any)
	if first["id"] != "aaa" {
		t.Errorf("list not sorted by id: first = %v", first["id"])
	}

	code, out = sourcesJSON(t, ts, http.MethodDelete, "/api/sources/nope", nil)
	if code != http.StatusNotFound || out["error"] != "unknown source" {
		t.Errorf("DELETE unknown: status %d body %v, want 404 unknown source", code, out)
	}
	code, out = sourcesJSON(t, ts, http.MethodDelete, "/api/sources/myhttp", nil)
	if code != http.StatusOK || out["removed"] != true {
		t.Errorf("DELETE existing: status %d body %v", code, out)
	}
	_, out = sourcesJSON(t, ts, http.MethodGet, "/api/sources", nil)
	list, _ = out["sources"].([]any)
	if len(list) != 1 {
		t.Errorf("after delete list has %d entries, want 1", len(list))
	}
}

func TestTestSourceUnknownIDAndUnknownSample(t *testing.T) {
	_, _, ts := setupSourcesTest(t)

	code, out := sourcesJSON(t, ts, http.MethodPost, "/api/sources/nope/test", []byte(`{"select":""}`))
	if code != http.StatusNotFound || out["error"] != "unknown source" {
		t.Errorf("unknown id: status %d body %v, want 404", code, out)
	}

	sourcesJSON(t, ts, http.MethodPut, "/api/sources/aaa", []byte(`{"kind":"file","path":"/tmp/x.json"}`))
	code, out = sourcesJSON(t, ts, http.MethodPost, "/api/sources/aaa/test",
		[]byte(`{"select":"","sample":"no-such-sample"}`))
	if code != http.StatusBadRequest || out["error"] != "unknown sample" {
		t.Errorf("unknown sample: status %d body %v, want 400 unknown sample", code, out)
	}
}

func TestTestFileSourceReadsInProcess(t *testing.T) {
	_, _, ts := setupSourcesTest(t)
	swapSourceProbe(t, func(context.Context, string) ([]byte, error) {
		t.Errorf("file source must not go through the probe")
		return nil, errors.New("unreachable")
	})

	docPath := filepath.Join(t.TempDir(), "doc.json")
	if err := os.WriteFile(docPath, []byte(`{"items":[{"name":"héllo"}]}`), 0o600); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"kind": "file", "path": docPath})
	sourcesJSON(t, ts, http.MethodPut, "/api/sources/doc", body)

	code, out := sourcesJSON(t, ts, http.MethodPost, "/api/sources/doc/test", []byte(`{"select":"items[0].name"}`))
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if out["ok"] != true || out["error"] != "" || out["matched"] != true || out["value"] != "héllo" || out["display"] != "héllo" {
		t.Errorf("test result = %v", out)
	}
	if preview, _ := out["documentPreview"].(string); !strings.Contains(preview, `"name": "héllo"`) {
		t.Errorf("documentPreview = %q, want two-space indented document", preview)
	}
}

func TestTestCommandSourceUsesProbeOnceAndWritesNoCache(t *testing.T) {
	_, _, ts := setupSourcesTest(t)
	var calls []string
	swapSourceProbe(t, func(_ context.Context, id string) ([]byte, error) {
		calls = append(calls, id)
		return []byte(`{"n":3}`), nil
	})

	sourcesJSON(t, ts, http.MethodPut, "/api/sources/cmd", []byte(`{"kind":"command","argv":["true"]}`))
	code, out := sourcesJSON(t, ts, http.MethodPost, "/api/sources/cmd/test", []byte(`{"select":"n"}`))
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if len(calls) != 1 || calls[0] != "cmd" {
		t.Errorf("probe calls = %v, want exactly [cmd]", calls)
	}
	if out["ok"] != true || out["matched"] != true || out["value"] != float64(3) || out["display"] != "3" {
		t.Errorf("test result = %v", out)
	}
	if _, ok := sources.LoadEnvelope("cmd"); ok {
		t.Error("test endpoint wrote the source cache")
	}
}

func TestTestSourceFetchErrorReportedInBody(t *testing.T) {
	_, _, ts := setupSourcesTest(t)
	swapSourceProbe(t, func(context.Context, string) ([]byte, error) {
		return nil, errors.New("boom")
	})
	sourcesJSON(t, ts, http.MethodPut, "/api/sources/cmd", []byte(`{"kind":"command","argv":["true"]}`))

	code, out := sourcesJSON(t, ts, http.MethodPost, "/api/sources/cmd/test", []byte(`{"select":""}`))
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if out["ok"] != false || out["error"] != "boom" || out["matched"] != false || out["value"] != nil || out["documentPreview"] != "" {
		t.Errorf("fetch error result = %v", out)
	}
}

func TestTestSourceSelectErrorAndMiss(t *testing.T) {
	_, _, ts := setupSourcesTest(t)
	swapSourceProbe(t, func(context.Context, string) ([]byte, error) {
		return []byte(`{"n":3}`), nil
	})
	sourcesJSON(t, ts, http.MethodPut, "/api/sources/cmd", []byte(`{"kind":"command","argv":["true"]}`))

	_, out := sourcesJSON(t, ts, http.MethodPost, "/api/sources/cmd/test", []byte(`{"select":"a..b"}`))
	if out["ok"] != false || out["error"] == "" || out["documentPreview"] == "" {
		t.Errorf("select syntax error result = %v", out)
	}

	_, out = sourcesJSON(t, ts, http.MethodPost, "/api/sources/cmd/test", []byte(`{"select":"missing"}`))
	if out["ok"] != true || out["matched"] != false || out["value"] != nil || out["display"] != "" {
		t.Errorf("miss result = %v", out)
	}
}

func TestSourceDocumentPreview(t *testing.T) {
	long := strings.Repeat("é", 3000)
	got := sourceDocumentPreview(long)
	if len(got) > maxDocumentPreviewBytes {
		t.Errorf("preview is %d bytes, want <= %d", len(got), maxDocumentPreviewBytes)
	}
	if !utf8.ValidString(got) {
		t.Error("preview cut a rune")
	}
	if !strings.HasPrefix(got, `"é`) {
		t.Errorf("preview prefix = %q", got[:min(len(got), 8)])
	}

	if got := sourceDocumentPreview(map[string]any{"a": 1.0}); got != "{\n  \"a\": 1\n}" {
		t.Errorf("indented preview = %q", got)
	}
	if got := sourceDocumentPreview("<b>"); got != `"<b>"` {
		t.Errorf("preview escaped HTML: %q", got)
	}
}
