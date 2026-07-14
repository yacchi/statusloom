package webconfig

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// routes builds the full handler chain: security middleware (Host/Origin
// checks for every route, bearer-token auth for /api/*, idle-timer resets
// for /api/*) wrapping the mux of API endpoints and the static asset
// handler.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/tools", s.handleTools)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)

	// DSL-native endpoints. Contract: DSL_API.md / astjson.go.
	mux.HandleFunc("GET /api/dsl/document", s.handleGetDSLDocument)
	mux.HandleFunc("PUT /api/dsl/document", s.handlePutDSLDocument)
	mux.HandleFunc("POST /api/dsl/parse", s.handleParseDSL)
	mux.HandleFunc("POST /api/dsl/serialize", s.handleSerializeDSL)
	mux.HandleFunc("GET /api/dsl/draft", s.handleGetDSLDraft)
	mux.HandleFunc("PUT /api/dsl/draft", s.handlePutDSLDraft)
	mux.HandleFunc("POST /api/dsl/preview", s.handlePreviewDSL)
	mux.HandleFunc("GET /api/dsl/fields", s.handleDSLFields)
	mux.HandleFunc("GET /api/dsl/metrics", s.handleDSLMetrics)

	mux.HandleFunc("POST /api/shutdown", s.handleShutdown)
	mux.HandleFunc("POST /api/live", s.handleLive)
	mux.HandleFunc("POST /api/live/session", s.handleLiveSession)
	mux.HandleFunc("POST /api/terminal/session", s.handleTerminalSession)

	// WebSocket channels. Under /ws/ (not /api/), so withSecurity applies
	// Host+Origin validation but not bearer auth; the handlers authenticate
	// via the ?token= query parameter instead.
	mux.HandleFunc("GET /ws/live", s.handleLiveWS)
	mux.HandleFunc("GET /ws/terminal", s.handleTerminalWS)

	// Catch-all for any other /api/* path, registered before "/" so more
	// specific patterns above still win on exact/longest match.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	mux.Handle("/", staticHandler())

	return s.withSecurity(mux)
}

// withSecurity enforces, in order: Host validation, Origin validation
// (when present), and - for /api/* only - bearer-token auth. It also
// resets the idle timer on every /api/* request, regardless of auth
// outcome, per statusloom-local-development-plan.md section 16.
func (s *server) withSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validHost(r.Host) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !s.validOrigin(origin) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}

		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.touch()
			if !s.checkAuth(r) {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// validHost reports whether host (an http.Request.Host value, which may
// include a port) resolves to 127.0.0.1 or localhost.
func validHost(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	return h == "127.0.0.1" || h == "localhost"
}

// validOrigin reports whether origin is exactly this server's own origin:
// http://127.0.0.1:<port> or http://localhost:<port>.
func (s *server) validOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" {
		return false
	}
	return u.Port() == strconv.Itoa(s.port)
}

// checkAuth reports whether r carries a valid "Authorization: Bearer
// <token>" header, using a constant-time comparison.
func (s *server) checkAuth(r *http.Request) bool {
	const prefix = "Bearer "
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, prefix) {
		return false
	}
	token := strings.TrimPrefix(authz, prefix)
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}
