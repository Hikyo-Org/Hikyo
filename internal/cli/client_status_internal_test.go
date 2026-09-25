package cli

import (
	"errors"
	"net/http"
	"testing"
	"time"
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
		http.StatusTooManyRequests:       ExitRateLimited,
		http.StatusServiceUnavailable:    ExitUnavailable,
		http.StatusTeapot:                ExitInternal,
	} {
		if got := exitForStatus(status); got != want {
			t.Errorf("exitForStatus(%d) = %d, want %d", status, got, want)
		}
	}
}

// A 429 whose JSON body carries some other code (a proxy's own error
// document) is still throttling: exit 7 with its Retry-After kept.
func TestJSON429WithForeignCodeIsRateLimited(t *testing.T) {
	header := http.Header{"Retry-After": []string{"9"}}
	err := errorFromResponse(http.StatusTooManyRequests, header, []byte(`{"error":{"code":"internal","message":"slow down"}}`))
	var ce *Error
	var limited *RateLimitedError
	if !errors.As(err, &ce) || ce.Code != ExitRateLimited || !errors.As(err, &limited) || limited.RetryAfter != 9*time.Second {
		t.Fatalf("err=%v, want exit %d with Retry-After 9s", err, ExitRateLimited)
	}
}
