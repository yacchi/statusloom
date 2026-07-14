package webconfig

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes v as an application/json response with the given
// status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorBody is the shape of every JSON error response.
type errorBody struct {
	Error string `json:"error"`
}

// writeError writes {"error": msg} as an application/json response with
// the given status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}
