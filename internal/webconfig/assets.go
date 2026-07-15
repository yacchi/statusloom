package webconfig

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
)

// assets holds the configurator frontend's static files. In this repo it
// is a placeholder index.html; a build script (scripts/build-web.sh, per
// statusloom-local-development-plan.md section 14) copies the real Vite
// build from apps/configurator/dist over internal/webconfig/dist before
// this is embedded.
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
