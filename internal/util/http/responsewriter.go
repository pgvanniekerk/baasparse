// Package http provides shared HTTP response helpers used by all API handlers
// and middleware in baasparse. It enforces a consistent JSON response envelope
// across the entire API.
//
// All responses are Content-Type: application/json. Success bodies are
// arbitrary structs passed to WriteResponse; error bodies always use the
// standard ErrorResponse shape.
package http

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse is the standard JSON envelope for all error responses.
// The single "error" field contains a human-readable message describing what
// went wrong. Clients should use the HTTP status code for programmatic
// branching and this field for display or logging.
type ErrorResponse struct {
	Error string `json:"error"`
}

// WriteResponse serialises v as JSON, sets Content-Type: application/json,
// and writes the given HTTP status code. It returns any encoding error so
// callers can log it; the status code and headers are already sent by the time
// an error could be returned, so recovery is not possible.
func WriteResponse(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// WriteError writes a standard ErrorResponse JSON body with the given HTTP
// status code. Use this for all error paths in handlers and middleware to
// ensure clients always receive a consistent {"error": "..."} shape.
func WriteError(w http.ResponseWriter, status int, message string) error {
	return WriteResponse(w, status, ErrorResponse{Error: message})
}
