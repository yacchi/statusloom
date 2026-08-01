package webconfig

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
)

// assets holds the configurator frontend's static files: the Vite build that
// scripts/build-web.sh copies from apps/configurator/dist into
// internal/webconfig/dist (statusloom-local-development-plan.md section 14).
//
// NOTHING in dist/ is tracked by git — dist/.gitignore (the sole tracked entry,
// which only exists so the directory survives a clone, since this embed
// directive will not compile against a missing directory) excludes all of it.
// So a checkout that has not run the build script embeds an EMPTY frontend, and
// notBuiltHandler below answers every request with the instructions to build it.
// That is the whole point: there is no committable build artifact and no
// pre-commit clean-up step that can be forgotten in either direction.
//
// The "all:" prefix ensures files starting with "_" or "." are not
// silently dropped from the embed.
//
//go:embed all:dist
var assets embed.FS

// staticHandler serves the embedded frontend, falling back to
// index.html for any path that isn't a real file (single-page-app
// routing).
func staticHandler() http.Handler {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		// The embed directive guarantees dist/ exists at build time.
		panic(err)
	}
	return staticHandlerFS(sub)
}

// staticHandlerFS is staticHandler over an arbitrary file system, so the
// not-built branch is testable (the embedded one is fixed at compile time).
func staticHandlerFS(sub fs.FS) http.Handler {
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return notBuiltHandler()
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(r.URL.Path)
		rel := clean[1:] // drop leading "/"
		if rel == "" {
			rel = "index.html"
		}

		if _, err := fs.Stat(sub, rel); err != nil {
			// Not a real file: serve index.html instead (client-side
			// routing), without redirecting the browser's address bar.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}

		fileServer.ServeHTTP(w, r)
	})
}

// notBuiltPage is what a binary built without the frontend serves. It replaces
// the placeholder index.html that used to be committed into dist/.
const notBuiltPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Statusloom configurator — not built</title>
</head>
<body>
<h1>The configurator UI is not in this binary</h1>
<p>Build the frontend, then start <code>statusloom config</code> again:</p>
<pre>scripts/build-web.sh   # or: mise run build-web</pre>
<p>The build output (<code>internal/webconfig/dist</code>) is intentionally not
tracked by git, so a fresh checkout always starts out in this state.</p>
</body>
</html>
`

// notBuiltHandler answers every request with notBuiltPage. It is installed in
// place of the file server when the embedded frontend has no index.html, so the
// missing build reports itself instead of surfacing as a bare 404.
func notBuiltHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(notBuiltPage))
	})
}
