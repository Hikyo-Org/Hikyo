package server

import (
	"net/http"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// Transport bounds belong to the HTTP layer. Streaming handlers replace the
// ordinary response deadline with a fresh deadline for each complete frame.
const (
	MaxInFlightRequests  = 512
	ResponseWriteTimeout = 60 * time.Second
	SSEWriteTimeout      = 30 * time.Second
	SSEHeartbeat         = 30 * time.Second
)

// boundPublicRequests reserves a slot before routing, parsing or authentication.
// The operational listener has its own handler so public saturation cannot
// prevent readiness probes or operational drain checks.
func boundPublicRequests(next http.Handler) http.Handler {
	slots := make(chan struct{}, MaxInFlightRequests)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			next.ServeHTTP(w, r)
		default:
			writeError(w, wirePolicyForCode(apigen.ErrorCodeTooManyRequests), "")
		}
	})
}
