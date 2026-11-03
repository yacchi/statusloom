package sources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fetchTestJSON returns a JSON string literal of exactly n bytes.
func fetchTestJSON(n int) string {
	return `"` + strings.Repeat("a", n-2) + `"`
}

func fetchTestNeed(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available: %v", tool, err)
		}
	}
}

// fetchTestCounting starts a loopback server that counts requests and answers 200 {}.
func fetchTestCounting(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func fetchTestPort(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Port()
}

func fetchTestHTTP(rawURL string) Definition {
	return Definition{ID: "t", Kind: KindHTTP, URL: rawURL}
}

func fetchTestCmd(argv ...string) Definition {
	return Definition{ID: "t", Kind: KindCommand, Argv: argv}
}

func fetchTestSh(script string) Definition {
	return fetchTestCmd("sh", "-c", script)
}

func fetchTestWantErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// fetchTestNoSecret fails when an error text carries a body or command output.
func fetchTestNoSecret(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error leaks output: %v", err)
	}
}

// fetchTestGuarded runs Fetch and fails if it has not returned after 10 s
// (descendants in these tests sleep 20 s; this is a hang guard, not a timing assertion).
func fetchTestGuarded(t *testing.T, def Definition) (FetchResult, error) {
	t.Helper()
	type outcome struct {
		res FetchResult
		err error
	}
	ch := make(chan outcome, 1)
	go func() {
		res, err := Fetch(context.Background(), def, "")
		ch <- outcome{res, err}
	}()
	select {
	case o := <-ch:
		return o.res, o.err
	case <-time.After(10 * time.Second):
		t.Fatal("Fetch hung")
		return FetchResult{}, nil
	}
}

func TestFetch_LoopbackHostError(t *testing.T) {
	ok := []string{
		"http://localhost:1/",
		"http://LOCALHOST/",
		"http://127.0.0.1:1/",
		"http://127.9.9.9/",
		"http://[::1]:1/",
		"http://[::ffff:127.0.0.1]/",
	}
	for _, raw := range ok {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := loopbackHostError(u); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
	bad := map[string]string{
		"http://0.0.0.0/":               "0.0.0.0",
		"http://[::]/":                  "::",
		"http://10.0.0.1/":              "10.0.0.1",
		"http://127.1/":                 "127.1",
		"http://example.com/":           "example.com",
		"http://localhost.example.com/": "localhost.example.com",
		"http://127.0.0.1@example.com/": "example.com",
		"http://[fe80::1%25en0]/":       "fe80::1%en0",
	}
	for raw, h := range bad {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("host %q is not loopback; set allowRemote to allow it", h)
		if err := loopbackHostError(u); err == nil || err.Error() != want {
			t.Errorf("%s: err = %v, want %q", raw, err, want)
		}
	}
}

func TestFetch_LoopbackDialControl(t *testing.T) {
	for _, a := range []string{"127.0.0.1:80", "[::1]:80"} {
		if err := loopbackDialControl("tcp", a, nil); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
	for _, a := range []string{"10.0.0.1:80", "0.0.0.0:80", "[::]:80"} {
		if err := loopbackDialControl("tcp", a, nil); err == nil || !strings.Contains(err.Error(), "is not loopback") {
			t.Errorf("%s: err = %v, want not loopback", a, err)
		}
	}
}

func TestFetch_NonLoopbackHostRejectedBeforeDial(t *testing.T) {
	srv, hits := fetchTestCounting(t)
	_, err := Fetch(context.Background(), fetchTestHTTP("http://0.0.0.0:"+fetchTestPort(t, srv)+"/"), "")
	fetchTestWantErr(t, err, `host "0.0.0.0" is not loopback; set allowRemote to allow it`)
	if n := hits.Load(); n != 0 {
		t.Fatalf("server hit %d times", n)
	}
}

func TestFetch_DialerRejectsNonLoopback(t *testing.T) {
	srv, hits := fetchTestCounting(t)
	resp, err := newFetchClient(false).Get("http://0.0.0.0:" + fetchTestPort(t, srv) + "/")
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected dial error")
	}
	if !strings.Contains(err.Error(), "is not loopback") {
		t.Fatalf("err = %v", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("server hit %d times", n)
	}
}

func TestFetch_RedirectToNonLoopbackRejected(t *testing.T) {
	target, hits := fetchTestCounting(t)
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://0.0.0.0:"+fetchTestPort(t, target)+"/", http.StatusFound)
	}))
	t.Cleanup(src.Close)
	_, err := Fetch(context.Background(), fetchTestHTTP(src.URL+"/"), "")
	if err == nil || !strings.Contains(err.Error(), `host "0.0.0.0" is not loopback`) {
		t.Fatalf("err = %v", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("redirect target hit %d times", n)
	}
}

func TestFetch_RedirectLimit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/hop/{n}", func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		if n == 0 {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		http.Redirect(w, r, "/hop/"+strconv.Itoa(n-1), http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, err := Fetch(context.Background(), fetchTestHTTP(srv.URL+"/hop/3"), "")
	if err != nil || string(res.Body) != `{}` {
		t.Fatalf("3 redirects: res = %+v, err = %v", res, err)
	}
	_, err = Fetch(context.Background(), fetchTestHTTP(srv.URL+"/hop/4"), "")
	if err == nil || !strings.Contains(err.Error(), "stopped after 3 redirects") {
		t.Fatalf("4 redirects: err = %v", err)
	}
}

func TestFetch_ETag(t *testing.T) {
	var mu sync.Mutex
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`{"n":1}`))
	}))
	t.Cleanup(srv.Close)
	headers := func() http.Header {
		mu.Lock()
		defer mu.Unlock()
		return got
	}

	res, err := Fetch(context.Background(), fetchTestHTTP(srv.URL), "")
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Body) != `{"n":1}` || res.ETag != `"v1"` || res.NotModified {
		t.Fatalf("res = %+v", res)
	}
	h := headers()
	if h.Get("Accept") != "application/json" || h.Get("User-Agent") != "statusloom" {
		t.Fatalf("headers = %v", h)
	}
	if _, ok := h["If-None-Match"]; ok {
		t.Fatalf("If-None-Match sent without etag: %v", h)
	}

	res, err = Fetch(context.Background(), fetchTestHTTP(srv.URL), `"v1"`)
	if err != nil {
		t.Fatal(err)
	}
	if !res.NotModified || res.ETag != `"v1"` || res.Body != nil {
		t.Fatalf("res = %+v", res)
	}
	if h := headers(); h.Get("If-None-Match") != `"v1"` {
		t.Fatalf("If-None-Match = %q", h.Get("If-None-Match"))
	}
}

func TestFetch_HTTPErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/500", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("SECRET"))
	})
	mux.HandleFunc("/notjson", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("SECRET"))
	})
	mux.HandleFunc("/max", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(fetchTestJSON(MaxBodyBytes)))
	})
	mux.HandleFunc("/over", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(fetchTestJSON(MaxBodyBytes + 1)))
	})
	mux.HandleFunc("/hang", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := context.Background()

	_, err := Fetch(ctx, fetchTestHTTP(srv.URL+"/500"), "")
	fetchTestWantErr(t, err, "HTTP 500")
	_, err = Fetch(ctx, fetchTestHTTP(srv.URL+"/notjson"), "")
	fetchTestWantErr(t, err, "body is not JSON")

	res, err := Fetch(ctx, fetchTestHTTP(srv.URL+"/max"), "")
	if err != nil || len(res.Body) != MaxBodyBytes {
		t.Fatalf("max body: len = %d, err = %v", len(res.Body), err)
	}
	_, err = Fetch(ctx, fetchTestHTTP(srv.URL+"/over"), "")
	fetchTestWantErr(t, err, "body exceeds 1 MiB")

	def := fetchTestHTTP(srv.URL + "/hang")
	def.TimeoutSeconds = 1
	_, err = Fetch(ctx, def, "")
	fetchTestWantErr(t, err, "request timed out after 1s")
}

func TestFetch_CommandNoShell(t *testing.T) {
	fetchTestNeed(t, "printf")
	res, err := Fetch(context.Background(), fetchTestCmd("printf", `{"x":"%s","y":"%s"}`, "a b", "$HOME"), "")
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Body) != `{"x":"a b","y":"$HOME"}` {
		t.Fatalf("body = %s", res.Body)
	}
}

func TestFetch_CommandStderrDiscarded(t *testing.T) {
	fetchTestNeed(t, "sh")
	res, err := Fetch(context.Background(), fetchTestSh(`echo SECRET >&2; printf '{}'`), "")
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Body) != `{}` {
		t.Fatalf("body = %q", res.Body)
	}
}

func TestFetch_CommandErrors(t *testing.T) {
	fetchTestNeed(t, "sh")
	ctx := context.Background()

	_, err := Fetch(ctx, fetchTestSh(`echo SECRET; echo SECRET >&2; exit 3`), "")
	fetchTestWantErr(t, err, "command exited: exit status 3")
	fetchTestNoSecret(t, err)

	_, err = Fetch(ctx, fetchTestSh(`echo SECRET`), "")
	fetchTestWantErr(t, err, "output is not JSON")

	def := fetchTestSh(`sleep 5`)
	def.TimeoutSeconds = 1
	_, err = fetchTestGuarded(t, def)
	fetchTestWantErr(t, err, "command timed out after 1s")

	_, err = Fetch(ctx, fetchTestCmd("statusloom-no-such-command"), "")
	if err == nil || !strings.HasPrefix(err.Error(), "command failed to start: ") {
		t.Fatalf("err = %v", err)
	}

	_, err = Fetch(ctx, Definition{ID: "t", Kind: KindCommand}, "")
	fetchTestWantErr(t, err, `source "t": command sources need a non-empty argv`)
}

func TestFetch_CommandOutputLimit(t *testing.T) {
	fetchTestNeed(t, "cat")
	dir := t.TempDir()
	maxPath := filepath.Join(dir, "max.json")
	overPath := filepath.Join(dir, "over.json")
	if err := os.WriteFile(maxPath, []byte(fetchTestJSON(MaxBodyBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overPath, []byte(fetchTestJSON(MaxBodyBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := fetchTestGuarded(t, fetchTestCmd("cat", maxPath))
	if err != nil || len(res.Body) != MaxBodyBytes {
		t.Fatalf("max output: len = %d, err = %v", len(res.Body), err)
	}
	_, err = fetchTestGuarded(t, fetchTestCmd("cat", overPath))
	fetchTestWantErr(t, err, "output exceeds 1 MiB")
}

func TestFetch_CommandDescendantHoldsStdout(t *testing.T) {
	fetchTestNeed(t, "sh")

	def := fetchTestSh(`sleep 20 & printf '{"a":1}'`)
	def.TimeoutSeconds = 1
	res, err := fetchTestGuarded(t, def)
	if err != nil {
		t.Fatalf("exit 0 with lingering descendant: err = %v", err)
	}
	if string(res.Body) != `{"a":1}` {
		t.Fatalf("body = %q", res.Body)
	}

	def = fetchTestSh(`sleep 20 & sleep 20`)
	def.TimeoutSeconds = 1
	_, err = fetchTestGuarded(t, def)
	fetchTestWantErr(t, err, "command timed out after 1s")
}

func TestFetch_File(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("~/ expansion test uses HOME")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "s.json"), []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Fetch(context.Background(), Definition{ID: "t", Kind: KindFile, Path: "~/s.json"}, "x")
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Body) != `{"a":1}` || res.ETag != "" || res.NotModified {
		t.Fatalf("res = %+v", res)
	}
}
