package cli

import (
	"errors"
	"net/http"
	"strings"
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
		http.StatusTooManyRequests:       ExitThrottled,
		http.StatusServiceUnavailable:    ExitUnavailable,
		http.StatusTeapot:                ExitInternal,
	} {
		if got := exitForStatus(status); got != want {
			t.Errorf("exitForStatus(%d) = %d, want %d", status, got, want)
		}
	}
}

// A throttled call exits with its own code and names the advertised wait, so a
// script can back off without matching message text (#806).
func TestThrottledResponseExitsDistinctlyWithTheWait(t *testing.T) {
	header := http.Header{"Retry-After": []string{"40"}}
	for name, payload := range map[string][]byte{
		"json":  []byte(`{"error":{"code":"too_many_requests","message":"too many requests"}}`),
		"plain": []byte("slow down"),
	} {
		err := errorFromResponse(http.StatusTooManyRequests, header, payload)
		var ce *Error
		if !errors.As(err, &ce) || ce.Code != ExitThrottled {
			t.Fatalf("%s: err = %v, want exit %d", name, err, ExitThrottled)
		}
		if !strings.Contains(err.Error(), "retry after 40s") {
			t.Fatalf("%s: message %q does not name the advertised wait", name, err)
		}
	}
	if err := errorFromResponse(http.StatusServiceUnavailable, header, nil); strings.Contains(err.Error(), "retry after") {
		t.Fatalf("a non-429 named a wait: %q", err)
	}
}
