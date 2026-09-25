package cli

import (
	"net/http"
	"testing"
)

// A non-JSON refusal falls back to its status. Every status the wire error
// table maps to a refusal code must exit the same way here, or a plain-text
// 413 or 422 from a proxy would read as an internal failure.
func TestExitForStatusMatchesTheRefusalCodes(t *testing.T) {
	for status, want := range map[int]int{
		http.StatusBadRequest:            ExitRefused,
		http.StatusForbidden:             ExitRefused,
		http.StatusConflict:              ExitRefused,
		http.StatusRequestEntityTooLarge: ExitRefused,
		http.StatusUnprocessableEntity:   ExitRefused,
		http.StatusUnauthorized:          ExitAuth,
		http.StatusNotFound:              ExitNotFound,
		http.StatusTooManyRequests:       ExitUnavailable,
		http.StatusServiceUnavailable:    ExitUnavailable,
		http.StatusTeapot:                ExitInternal,
	} {
		if got := exitForStatus(status); got != want {
			t.Errorf("exitForStatus(%d) = %d, want %d", status, got, want)
		}
	}
}
