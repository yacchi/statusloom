package webconfig

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// internal/webconfig/dist is git-ignored in full, so a fresh checkout embeds an
// EMPTY frontend (only dist/.gitignore). That case must report itself instead of
// 404-ing, and it must not be mistaken for the single-page-app fallback — which
// would otherwise serve a missing index.html for every path.
func TestStaticHandler_NotBuilt(t *testing.T) {
	h := staticHandlerFS(fstest.MapFS{
		".gitignore": &fstest.MapFile{Data: []byte("*\n!.gitignore\n")},
	})

	for _, path := range []string{"/", "/index.html", "/assets/index-abc.js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s status = %d, want 503", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "scripts/build-web.sh") {
			t.Errorf("GET %s body does not tell the reader how to build: %q", path, rec.Body.String())
		}
	}
}

// The built case is unchanged: real files are served, and any other path falls
// back to index.html for client-side routing.
func TestStaticHandler_Built(t *testing.T) {
	h := staticHandlerFS(fstest.MapFS{
		"index.html":          &fstest.MapFile{Data: []byte("<!doctype html><title>ui</title>")},
		"assets/index-abc.js": &fstest.MapFile{Data: []byte("console.log(1)")},
		".gitignore":          &fstest.MapFile{Data: []byte("*\n")},
	})

	// "/index.html" is deliberately absent: http.FileServer redirects it to
	// "/" (301), which is its long-standing behavior and not what this test is
	// about.
	cases := []struct{ path, want string }{
		{"/", "<title>ui</title>"},
		{"/assets/index-abc.js", "console.log(1)"},
		// Unknown path: the SPA fallback, not a 404.
		{"/some/client/route", "<title>ui</title>"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", c.path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("GET %s body = %q, want it to contain %q", c.path, rec.Body.String(), c.want)
		}
	}
}
