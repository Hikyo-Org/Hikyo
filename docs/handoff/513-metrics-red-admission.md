# Handoff: #513 RED /metrics + admission gauges + dev access log

Issue: https://github.com/Hikyo-Org/Hikyo/issues/513. Base:
`ddca9c954377e2fce9403d9507ab7445fc5aba2a`.

## What shipped

Extends the operational `/metrics` surface with closed-cardinality RED metrics,
admission-pressure gauges, and a dev-only access log. The official Prometheus Go
client owns metric primitives, validation, and exposition. A private pedantic
registry excludes the default Go/process collectors and preserves Hikyo's fixed
series budget.

## Contract

- One shared `server.Metrics` collector (`internal/server/metrics.go`) is created
  in `internal/app/app.go`, set on `server.API.Metrics` (the writer, via
  middleware) and passed to `server.NewOperational` (the reader). A nil collector
  leaves `/metrics` at its pre-#513 family set.
- `NewOperational` gained a third parameter `metrics *Metrics`. All non-app
  callers explicitly choose whether the RED collector participates.
- New middleware `API.observe` leads the public router, outside CORS, matching,
  and `recoverPanics`, so unmatched API traffic is counted as `other` and a
  recovered panic's 500 is counted as `5xx`. It reads the chi route pattern
  after `next.ServeHTTP`, classifies it, records, and logs.
- `admission.Limiter.Snapshot()` (`internal/admission/admission.go`) exposes
  instance-wide pressure only (no attacker-chosen keys): concurrency limit,
  in-flight, queue-depth limit, waiting, active backoffs.
- `prometheus.NewPedanticRegistry()` owns the RED families. Every allowed label
  child is created at construction so zero-valued series are deterministic, and
  the operational handler combines it with request-scoped retention/TLS
  collectors before handing exposition to `promhttp`.

## Metrics (all names/labels pinned)

- `hikyo_http_requests_total{class,status}` counter — class ∈ {auth, hierarchy,
  values, revisions, delivery, scim, admin, other(fail-closed)}; status ∈
  {2xx,3xx,4xx,5xx,other}. `3xx` is first-class because the OIDC browser legs
  redirect.
- `hikyo_http_request_errors_total{class,status}` counter — the `4xx` and `5xx`
  subsets as a dedicated RED error family.
- `hikyo_http_requests_in_flight` gauge.
- `hikyo_http_request_duration_seconds` histogram{class}, fixed buckets
  `0.005,0.025,0.1,0.5,1,5` s + `+Inf`, with `_sum`/`_count`.
- `hikyo_admission_{concurrency_limit,in_flight,queue_depth_limit,queue_waiting,active_backoffs}`
  gauges (rendered as zeros when no limiter is wired).

Class is derived from the templated chi route pattern, never a raw path or ID, so
cardinality is bounded. The value/revision/delivery/scim families live under the
`orgs` hierarchy and are matched before the `orgs` catch-all — order is
load-bearing and covered by `TestClassifyIsAClosedSurfaceMap`.

Names, class/status label sets, histogram buckets, forbidden labels, and the
≤1,000 total-series budget are pinned in `internal/conformance/metrics_test.go`.
The authoritative bound registry carries the `metrics-cardinality-budget` row,
and the scrape test proves the registry exactly matches emitted families.

## Access log

`API.observe` logs `request` (method, class, status, duration_ms) at
`slog.LevelDebug`. `app.Logger(dev)` uses a LevelDebug text handler under `--dev`
and a default (Info) JSON handler in production, so the line is present in dev and
absent by default. It emits the class, never the raw path — no path echo.

## Coverage

- `internal/server/metrics_internal_test.go` (white-box): pre-commit and
  post-commit recovered panic → 5xx cell while preserving committed wire bytes;
  classifier table; status buckets; access log present at Debug / absent at Info
  and never echoing a path; shared response writer forwards Flush and preserves
  the final response after informational 1xx responses.
- `internal/server/metrics_scrape_test.go` (black-box): drives traffic through the
  public API stack, scrapes the operational `/metrics`, asserts family TYPE lines,
  exact matched and unmatched counters (including a CORS preflight), a zeroed
  cell, histogram +Inf/count, admission gauge values, retention block still
  present, and that only closed-enum labels appear (no `/api/`, no `org_`).
- `internal/conformance/metrics_test.go`: drift pins for names/labels/buckets.

Final verification: `go test -count=1 ./...` passed 4,055 tests in 69 packages;
`go vet ./...`, the focused race suite, Astro diagnostics/build, OSS policy,
docs PWA checks, and the offline browser test all passed. Standards, spec, and
native Codex adversarial reviews finished clean.

## Scope notes / not done

- Counters cover all `/api/v1/*` traffic. Unmatched paths, unsupported methods,
  and CORS preflights are assigned to the closed `other` class.
- Latency is a fixed-bucket histogram (Prometheus-idiomatic, closed cardinality)
  rather than min/p50/p99 — the issue offered either; the official client owns
  histogram accounting while the pinned bucket grid keeps output deterministic.
- Docs updated: `docs/site/src/content/docs/docs/configuration.mdx` (new
  Operational metrics section) and `self-hosting.mdx` (scrape note).

## Follow-ups

None required. A future multi-node build must revisit admission gauges (the
limiter is process-local by design; see the admission package doc comment).
