// Package server is the HTTP layer: chi router, health surfaces, and the
// generated API. It never imports internal/store — enforced by the
// import-boundary test — so a handler cannot reach data except through the
// service layer, which authorizes inside a transaction.
package server

import (
	"context"
	"io/fs"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

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
	// MCP is nil while the independently gated MCP surface is disabled.
	MCP http.Handler
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
	r.Use(boundPublicRequests)
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

	if publicOptions.MCP != nil {
		r.Handle("/mcp", publicOptions.MCP)
	}

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
		unix := func(t time.Time) float64 {
			if t.IsZero() {
				return 0
			}
			return float64(t.Unix())
		}
		flag := func(b bool) float64 {
			if b {
				return 1
			}
			return 0
		}
		backup := health.Backup
		snapshot := prometheus.NewPedanticRegistry()
		snapshot.MustRegister(
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricLastBackupExportSuccess, Help: "Unix timestamp of the last successful backup export; 0 when never."}, func() float64 { return unix(backup.LastSuccessAt) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricBackupRPOExceeded, Help: "Whether the newest successful backup export is older than the configured RPO."}, func() float64 { return flag(backup.RPOExceeded) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricLastBackupPruneSuccess, Help: "Unix timestamp of the last successful backup retention prune; 0 when never."}, func() float64 { return unix(backup.LastPruneAt) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricLastRestoreDrill, Help: "Unix timestamp of the last recorded restore drill; 0 when never."}, func() float64 { return unix(backup.LastDrillAt) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricRestoreDrillOK, Help: "Whether the last recorded restore drill passed every step within the RTO target."}, func() float64 { return flag(backup.LastDrillOK) }),
		)
		snapshot.MustRegister(
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricLastPruneSuccess, Help: "Unix timestamp of the last successful retention prune."}, func() float64 { return float64(last) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricPruneStale, Help: "Whether the retention prune state is stale."}, func() float64 { return float64(stale) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricProjectStoragePeak, Help: "Largest observed project storage usage in bytes."}, func() float64 { return float64(health.PeakProjectBytes) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricProjectStorageWarn, Help: "Whether project storage is above its warning threshold."}, func() float64 { return float64(storageWarn) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricTLSCertNotAfter, Help: "Unix timestamp when the active TLS certificate expires."}, func() float64 { return float64(tlsNotAfter) }),
			prometheus.NewCounterFunc(prometheus.CounterOpts{Name: MetricTLSReloadFailures, Help: "Total failed TLS certificate reloads."}, func() float64 { return float64(tlsReloadFailures) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricAdapterTargetsFailed, Help: "Active deployment-adapter targets whose last attempt failed and that are not paused."}, func() float64 { return float64(health.Adapters.TargetsFailed) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricAdapterTargetsPaused, Help: "Active deployment-adapter targets an operator has paused."}, func() float64 { return float64(health.Adapters.TargetsPaused) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricAdapterTargetsAttention, Help: "Active deployment-adapter targets whose destination drifted from the ownership ledger and need an operator."}, func() float64 { return float64(health.Adapters.TargetsAttention) }),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: MetricAdapterJobsQueued, Help: "Deployment-adapter outbox jobs waiting to be claimed."}, func() float64 { return float64(health.Adapters.JobsQueued) }),
		)
		diagnostics := health.Diagnostics
		volumePercent := math.NaN()
		if diagnostics.Volume.Known {
			volumePercent = diagnostics.Volume.UsedPercent
		}
		for _, gauge := range []struct {
			name, help string
			value      float64
		}{
			{MetricDataVolumeKnown, "Whether datastore volume capacity was authoritatively measured.", flag(diagnostics.Volume.Known)},
			{MetricDataVolumeUsedPercent, "Measured datastore volume utilization; NaN when unknown.", volumePercent},
			{MetricDataVolumeWarn, "Measured datastore volume at or above 80 percent; check capacity_known.", flag(diagnostics.Volume.Known && volumePercent >= 80)},
			{MetricDataVolumeCritical, "Measured datastore volume at or above 90 percent; check capacity_known.", flag(diagnostics.Volume.Known && volumePercent >= 90)},
			{MetricRootEscrowVerified, "Whether escrow verification matches the current root and recovery incarnation.", flag(diagnostics.EscrowCurrent)},
			{MetricRootEscrowVerifiedAt, "Last recorded root escrow verification timestamp; validity is reported separately.", unix(diagnostics.Metadata.EscrowVerifiedAt)},
			{MetricRootRotationPending, "Whether multiple active root wrappers remain.", flag(diagnostics.Metadata.RootWrappers > 1)},
			{MetricReencryptPendingScopes, "Number of key scopes with retiring versions.", float64(diagnostics.Metadata.RetiringScopes)},
			{MetricLastReencryptSuccess, "Last successful scope reencrypt completion; zero when never recorded.", unix(diagnostics.Metadata.LastReencryptSuccess)},
			{MetricPinsExpired, "Number of expired pins, which no longer protect retention.", float64(diagnostics.Metadata.PinsExpired)},
			{MetricPinsExpiringDay, "Pins expiring within one day.", float64(diagnostics.Metadata.PinsDay)},
			{MetricPinsExpiringWeek, "Pins expiring after one day and within seven days.", float64(diagnostics.Metadata.PinsWeek)},
			{MetricPinsExpiringMonth, "Pins expiring after seven days and within thirty days.", float64(diagnostics.Metadata.PinsMonth)},
		} {
			snapshot.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: gauge.name, Help: gauge.help}, func() float64 { return gauge.value }))
		}
		gatherers := prometheus.Gatherers{snapshot}
		if metrics != nil {
			gatherers = append(gatherers, metrics.registry)
		}
		promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{
			ErrorHandling:     promhttp.HTTPErrorOnError,
			EnableOpenMetrics: false,
		}).ServeHTTP(w, req)
	})
	return r
}

// ContractPrefix re-exports the version prefix so callers building URLs read
// it from the contract rather than restating it.
const ContractPrefix = api.PathPrefix
