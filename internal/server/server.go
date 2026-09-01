// Package server is the HTTP layer: chi router, health surfaces, and the
// generated API. It never imports internal/store — enforced by the
// import-boundary test — so a handler cannot reach data except through the
// service layer, which authorizes inside a transaction.
package server

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// ReadyChecker reports whether a request would actually work.
type ReadyChecker interface {
	Ready(ctx context.Context) error
}

// PublicOptions carries transport facts that affect public response policy.
type PublicOptions struct {
	HSTS bool
	// ExternalOrigin is the config-validated canonical public origin. CORS uses
	// it to identify same-origin requests without trusting the Host header.
	ExternalOrigin string
}

// TLSMetrics reports the label-free native TLS gauges served operationally.
type TLSMetrics interface {
	TLSMetrics() (notAfterUnix int64, reloadFailures uint64)
}

// New builds the public API/UI router.
//
// Route partitioning, and why it is a partition rather than one stack:
//
//   - /healthz, /readyz, and /metrics live only on NewOperational. Keeping them
//     off this listener means public middleware and traffic cannot affect
//     probes or expose process metrics.
//   - /api/v1/* is the contract. Every request there is validated against
//     api/openapi.yaml before a handler sees it, carries wire metadata for the
//     audit trail, and renders refusals through one uniform writer.
//
// Anything not matching the API is a 404 in the contract's own error shape,
// unless `ui` carries an embedded single-page application, in which case the
// rules in spa.go decide, and only for an HTML navigation to a non-reserved
// path. A nil `ui` is an API-only binary, which is what a plain `go build`
// produces.
// remoteOriginSource adapts the directory service to the SPA document writer.
// It swallows the read error deliberately and answers the BASELINE: a database
// hiccup must tighten the policy, never loosen it, and a document served with
// `connect-src 'self'` is a workspace that cannot connect — visible and safe —
// where a document served with no CSP at all would be neither.
//
// It is handed to serveSPA rather than to the header middleware (#211): only a
// served document consumes the extension, so only a served document pays for
// the read.
func remoteOriginSource(a *API) func(context.Context) []string {
	if a == nil || a.Remotes == nil {
		return nil
	}
	return func(ctx context.Context) []string {
		origins, err := a.Remotes.RemoteOrigins(ctx)
		if err != nil {
			return nil
		}
		return origins
	}
}

func New(ready ReadyChecker, a *API, ui fs.FS) http.Handler {
	return NewPublic(ready, a, ui, PublicOptions{})
}

// NewPublic builds the public router with explicit transport policy.
func NewPublic(ready ReadyChecker, a *API, ui fs.FS, publicOptions PublicOptions) http.Handler {
	r := chi.NewRouter()
	// The static security baseline, on every response including refusals. The
	// dynamic part — `connect-src` extended with the configured remotes'
	// origins (#71), a closed list read per DOCUMENT so an added or removed
	// remote takes effect without a restart — belongs to the SPA writer below,
	// which is the only response that can use it.
	r.Use(securityHeaders(publicOptions.HSTS))
	// Observe at router scope so unmatched API paths, unsupported methods, and
	// CORS preflights contribute to RED totals as class=other. Route-group
	// middleware never sees those requests because chi has not selected a route.
	if a != nil {
		r.Use(a.observe)
	}
	// Cross-origin readability for allowlisted workspace origins (#71), at the
	// TOP of the chain rather than inside the API group, and that placement is
	// load-bearing rather than tidy.
	//
	// A CORS PREFLIGHT MUST BE ANSWERED WHETHER OR NOT IT MATCHES A ROUTE. The
	// contract declares no OPTIONS operations — correctly, it describes the API
	// and not the browser's transport protocol — so an `OPTIONS
	// /api/v1/auth/workspace/start` matches nothing and, from inside a route
	// group, never reaches that group's middleware at all: it falls through to
	// the router's not-found handler, which knows nothing about CORS and
	// answers with no headers. The browser reports that as a missing
	// `Access-Control-Allow-Origin`, which reads like an allowlist problem and
	// is not — and it made every cross-origin POST of the workspace tier
	// unreachable while the GETs worked.
	//
	// Router-level middleware runs on every request, matched or not. It is also
	// OUTSIDE the artifact middleware, which is what a preflight needs: a
	// preflight carries no credential by definition, so it must be answered
	// before anything tries to resolve one. Requests without an `Origin` header
	// pass through untouched, so nothing else on the router changes.
	r.Use(workspaceCORS(workspaceOriginCheck(a, publicOptions.ExternalOrigin)))

	if a != nil {
		r.Group(func(g chi.Router) {
			for _, mw := range a.Middleware() {
				g.Use(mw)
			}
			g.Use(a.SlideSessionClocks)
			// The strict server's own error legs go through the SAME uniform
			// writer as every handler. Left at their defaults they emit
			// `http.Error` plain text, which is neither the contract's error
			// shape nor uniform — and it is the leg a handler takes when it
			// returns a bare domain error rather than building one of twenty
			// near-identical per-operation refusal objects.
			apigen.HandlerFromMux(apigen.NewStrictHandlerWithOptions(a, nil, apigen.StrictHTTPServerOptions{
				RequestErrorHandlerFunc:  a.writeRequestError,
				ResponseErrorHandlerFunc: a.writeHandlerError,
			}), g)
		})
	}

	if ui != nil {
		r.Handle(assetPrefix+"*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			serveAsset(ui, w, req)
		}))
		origins := remoteOriginSource(a)
		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			serveSPA(ui, origins, w, req)
		})
	} else {
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			markUnmatched(w)
			writeError(w, wirePolicyForCode(apigen.ErrorCodeNotFound), "")
		})
	}
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		// A method the contract does not describe on a path it does is,
		// from outside, the same fact as a path that is not there.
		markUnmatched(w)
		writeError(w, wirePolicyForCode(apigen.ErrorCodeNotFound), "")
	})
	return r
}

func markUnmatched(w http.ResponseWriter) {
	if marker, ok := w.(interface{ markUnmatched() }); ok {
		marker.markUnmatched()
	}
}

// NewOperational builds the separate plaintext operational router. It carries
// no CORS or admission middleware and registers only local process surfaces.
//
// metrics is the shared RED collector (#513). It is the SAME instance handed to
// the API middleware, so the scrape here reads the counters that middleware
// writes. A nil collector leaves /metrics as the retention/TLS gauges alone —
// the pre-#513 shape.
func NewOperational(ready ReadyChecker, healthService OperationalRetentionHealthService, metrics *Metrics) http.Handler {
	r := chi.NewRouter()
	r.Use(securityHeaders(false))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		if ready == nil || ready.Ready(req.Context()) != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	r.Get("/metrics", func(w http.ResponseWriter, req *http.Request) {
		if healthService == nil {
			http.Error(w, "retention health unavailable", http.StatusServiceUnavailable)
			return
		}
		health, err := healthService.OperationalHealth(req.Context())
		if err != nil {
			http.Error(w, "retention health unavailable", http.StatusServiceUnavailable)
			return
		}
		last := int64(0)
		if health.Recorded {
			last = health.LastSuccess.Unix()
		}
		stale := 0
		if health.Stale {
			stale = 1
		}
		storageWarn := 0
		if health.StorageWarn {
			storageWarn = 1
		}
		var tlsNotAfter int64
		var tlsReloadFailures uint64
		if metrics, ok := healthService.(TLSMetrics); ok {
			tlsNotAfter, tlsReloadFailures = metrics.TLSMetrics()
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "# TYPE "+MetricLastPruneSuccess+" gauge\n"+
			MetricLastPruneSuccess+" %d\n"+
			"# TYPE "+MetricPruneStale+" gauge\n"+
			MetricPruneStale+" %d\n"+
			"# TYPE "+MetricProjectStoragePeak+" gauge\n"+
			MetricProjectStoragePeak+" %d\n"+
			"# TYPE "+MetricProjectStorageWarn+" gauge\n"+
			MetricProjectStorageWarn+" %d\n"+
			"# TYPE "+MetricTLSCertNotAfter+" gauge\n"+
			MetricTLSCertNotAfter+" %d\n"+
			"# TYPE "+MetricTLSReloadFailures+" counter\n"+
			MetricTLSReloadFailures+" %d\n", last, stale, health.PeakProjectBytes, storageWarn, tlsNotAfter, tlsReloadFailures)
		if metrics != nil {
			metrics.writeInto(w)
		}
	})
	return r
}

// ContractPrefix re-exports the version prefix so callers building URLs read
// it from the contract rather than restating it.
const ContractPrefix = api.PathPrefix
