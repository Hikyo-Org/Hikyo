package server

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// recoverPanics is the outermost error-contract leg, immediately inside the
// observational middleware. A panic anywhere below it — contract validation
// included — becomes the uniform `internal` refusal instead of a dropped
// connection, and the panic value and stack land in the slog pipeline rather
// than the std logger net/http recovers into.
//
// The service layer panics on state-reachable invariant violations (double
// classification, an unknown fetch-result variant), so this leg is part of the
// error contract, not a programmer-error net. The security-header and CORS
// middlewares are router-level (server.go), one layer up, so a recovered answer
// still carries them — the response tracker is the inner one.
//
// When a handler has already committed the response before panicking — a fault
// mid-advisory-stream, say — the status is gone and a second WriteHeader would
// be the exact std-logger noise this leg exists to remove, so the committed
// writer is left alone and the log alone carries the failure.
func (a *API) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := newResponseWriter(w)
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			if marker, ok := w.(interface{ markRecoveredPanic() }); ok {
				marker.markRecoveredPanic()
			}
			// Mirror a.fault: the wire carries nothing derived from the cause,
			// the process log carries everything. The contract operation is not
			// yet in context this far out, so method and path name the op.
			if a.Log != nil {
				msg := "handler panic"
				if rec.wroteHeader {
					msg = "handler panic after response committed"
				}
				a.Log.ErrorContext(r.Context(), msg,
					"op", r.Method+" "+r.URL.Path,
					"err", fmt.Sprintf("%v", p),
					"stack", string(debug.Stack()))
			}
			if !rec.wroteHeader {
				writeError(rec, wirePolicyForCode(apigen.ErrorCodeInternal), "")
			}
		}()
		next.ServeHTTP(rec, r)
	})
}
