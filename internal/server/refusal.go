package server

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/google/uuid"
)

const RefusalRequestIDHeader = "X-Hikyo-Request-ID"

type RefusalRecorder interface {
	Record(context.Context, audit.RefusalSample) error
}

// observeRefusals precedes caps, parsing and the API/MCP split. Signals are
// settled before the first final response header, without buffering bodies.
func (a *API) observeRefusals(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.NewV7()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		source := ""
		if ip, err := netip.ParseAddr(a.sourceIP(r)); err == nil {
			source = ip.WithZone("").Unmap().String()
		}
		ctx := audit.WithRefusalReceipt(r.Context(), source, id.String())
		w.Header().Set(RefusalRequestIDHeader, id.String())
		sw := newResponseWriter(w)
		sw.beforeHeader = func(status int) {
			if status < 400 {
				return
			}
			if sample, ok := audit.PendingRefusal(ctx); ok {
				_ = a.Refusals.Record(ctx, sample)
			}
		}
		next.ServeHTTP(sw, r.WithContext(ctx))
	})
}
