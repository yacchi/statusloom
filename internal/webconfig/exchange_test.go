package webconfig

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yacchi/statusloom/internal/exchange"
	"github.com/yacchi/statusloom/internal/store"
)

// exchangeSource returns a valid claude-code document whose layout name
// embeds name, so successive Saves produce distinct revisions (identical
// source would dedup to a no-op) — mirroring historySource in history_test.go.
func exchangeSource(name string) string {
	return `<statusloom version="1" tool="claude-code"><layout name="` + name + `" active="true"><line><field name="model"/></line></layout></statusloom>`
}

// rawPOST issues an authenticated POST with a raw (non-JSON) body, for
// /api/exchange/import whose body is a *.sloom.md Markdown document rather
// than a JSON envelope.
func rawPOST(t *testing.T, ts *testServer, path string, body []byte) *http.Response {
	t.Helper()
	return authedRequest(t, ts, "POST", path, body)
}

func TestExchange_Import_CreatesRevision(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	ts := startTestServer(t, time.Hour)

	md := exchange.Encode(exchange.Meta{Name: "Imported", Description: "d", Author: "a"}, exchangeSource("x"))

	resp := rawPOST(t, ts, "/api/exchange/import", md)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	var out struct {
		Tool        string `json:"tool"`
		Revision    string `json:"revision"`
		Diagnostics []any  `json:"diagnostics"`
	}
	if err := decodeJSON(resp.Body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Tool != "claude-code" || out.Revision == "" {
		t.Errorf("response = %+v, want tool=claude-code and a non-empty revision", out)
	}

	st := store.OpenAt(filepath.Join(dir, "statusloom.json"))
	rev, _, ok, err := st.Current("claude-code")
	if err != nil || !ok {
		t.Fatalf("Current: ok=%v err=%v", ok, err)
	}
	if rev.Source != exchangeSource("x") {
		t.Errorf("current source = %q, want %q", rev.Source, exchangeSource("x"))
	}
	if rev.Meta.Name != "Imported" || rev.Meta.Description != "d" || rev.Meta.Author != "a" {
		t.Errorf("current meta = %+v", rev.Meta)
	}
	if rev.Origin != "import" {
		t.Errorf("current origin = %q, want %q", rev.Origin, "import")
	}
}

func TestExchange_Import_InvalidDocumentRejectedWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	ts := startTestServer(t, time.Hour)

	bad := `<statusloom version="1" tool="claude-code"><layout name="D" active="true"><line><field name="not-a-field"/></line></layout></statusloom>`
	md := exchange.Encode(exchange.Meta{}, bad)

	resp := rawPOST(t, ts, "/api/exchange/import", md)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	var out struct {
		Diagnostics []struct {
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := decodeJSON(resp.Body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	foundError := false
	for _, d := range out.Diagnostics {
		if d.Severity == "error" {
			foundError = true
		}
	}
	if !foundError {
		t.Errorf("diagnostics = %+v, want at least one error", out.Diagnostics)
	}

	st := store.OpenAt(filepath.Join(dir, "statusloom.json"))
	if _, _, ok, _ := st.Current("claude-code"); ok {
		t.Error("store should have no current revision after a rejected import")
	}
}

func TestExchange_Import_MalformedMarkdownIs400(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	resp := rawPOST(t, ts, "/api/exchange/import", []byte("not a sloom.md document"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestExchange_Export_ReturnsCurrentAsMarkdown(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATUSLOOM_CONFIG", filepath.Join(dir, "config.json"))
	ts := startTestServer(t, time.Hour)
	st := store.OpenAt(filepath.Join(dir, "statusloom.json"))

	src := exchangeSource("a")
	if _, _, err := st.Save("claude-code", src, "test", store.Meta{Name: "n", Description: "d", Author: "a"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	resp := authedRequest(t, ts, "GET", "/api/exchange/export?tool=claude-code", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("Content-Type = %q, want a text/markdown prefix", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	meta, xml, err := exchange.Decode(body)
	if err != nil {
		t.Fatalf("exported body did not decode as a sloom.md document: %v\n%s", err, body)
	}
	if xml != src {
		t.Errorf("xml = %q, want %q", xml, src)
	}
	if meta.Name != "n" || meta.Description != "d" || meta.Author != "a" {
		t.Errorf("meta = %+v", meta)
	}
}

func TestExchange_Export_NoCurrentIs404(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	resp := authedRequest(t, ts, "GET", "/api/exchange/export?tool=claude-code", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestExchange_Export_UnknownToolIs400(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	resp := authedRequest(t, ts, "GET", "/api/exchange/export?tool=nope", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestExchange_RequiresAuth(t *testing.T) {
	t.Setenv("STATUSLOOM_CACHE_DIR", t.TempDir())
	ts := startTestServer(t, time.Hour)

	for _, req := range []struct {
		method, path string
	}{
		{"POST", "/api/exchange/import"},
		{"GET", "/api/exchange/export"},
	} {
		httpReq, err := http.NewRequest(req.method, ts.baseURL+req.path, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			t.Fatalf("request %s %s: %v", req.method, req.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s unauthenticated status = %d, want 401", req.method, req.path, resp.StatusCode)
		}
	}
}
