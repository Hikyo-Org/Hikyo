# Issues 975, 976, 978: server response hardening

## Scope and ownership

Baseline: `cb78b6be111029335e744aa3e4dcc956d5194ba1`. The three live issues were
open without comments and no matching PR existed when work began on 2026-10-18.
Endpoint: implement, test, signed+DCO commit, push, PR. Merge, auto-merge, queue,
release and deployment remain explicitly unauthorized.

`securityHeaders` in `internal/server/spa.go` owns non-storage for the entire
contract namespace: exact `/api/v1` and its slash-delimited descendants. It
sets `Cache-Control: no-store` and `Pragma: no-cache` before routing, admission,
CORS, body parsing, authentication and CSRF refusal. It appends `Cookie` and
`Authorization` variation without replacing other fields. This deliberately
covers all API responses rather than maintaining a sensitive-route allowlist.
Static assets and SPA documents retain their existing cache policies.

The SSE response writer in `internal/server/revisions.go` explicitly uses
`no-store` too, preserving `Vary: Accept`, retry hints, flushing, write deadlines,
no-buffering and keep-alive. The service still owns recipient projection,
per-event authorization, revocation and advisory-only reconnect semantics.

`oauth2StartError` in `internal/server/oauth2.go` renders the fixed
`ErrOAuth2Reauth` remedy for both bare and wrapped errors. Matching still uses
`errors.Is`; status 409 and the existing 400/401/429/500 classifications remain.
No response schemas, authorization formulas, CSRF rules or refusal order change.

## Sensitive contract inventory (#975)

The affected operation set is every operation in `api/openapi.yaml`. Reviewed
operation IDs and their owning handlers; the sensitive families below inherit
the same policy on success and refusal:

- Delivery and plaintext: `fetchDelivery`; `getValue`, `listValues`,
  `diffValues`; `revealValue`, `revealValues`, `revealValueDiff`;
  `exportValues`, `revealRevisionDiff`, `getRevision`, `diffRevisions`,
  `listPendingDrafts` and pending previews.
  Existing masking and current/history authorization stay in their services.
- Cryptographic output: `transitDecrypt`, `transitDataKey`, and other Transit
  operations; `mintLease` and `renewLease`; `issueCertificate` and
  `renewCertificate`; `issueSshCertificate`. No additional key material is
  exported and no custody policy changes.
- Credential mints: `mintMachineCredential`, `mintDeveloperCredential`,
  `mintScimCredential`, `mintInstanceConnection`; login and account-security
  responses that mint or rotate sessions, CSRF artifacts or recovery codes,
  including `whoami`, `enrolTotpStart` and `regenerateRecoveryCodes`.
- Authentication ceremonies: local/password and factor challenge flows;
  WebAuthn/TOTP enrollment, step-up and reauthentication; recovery, credential
  reset, identity link/unlink and sign-up verification; OIDC/OAuth2/SAML starts
  and callbacks; CLI login/device/reauth starts, approval, polling and redemption.
  TOTP enrollment URI, provider redirect state and refusal material are covered.
- Exports and metadata: `exportDefinitions`, `exportOrgAudit`, `exportProjectAudit`,
  `exportEnvAudit`, discovery, signals,
  project/environment/key metadata and authenticated SSE. These need not all
  contain plaintext to receive the conservative namespace policy.

Other current and future contract operations inherit the same policy. Static
hashed assets remain public/immutable, root files retain no-cache, and SPA
HTML retains private/no-cache plus its existing variation. Operational and MCP
listeners are outside this contract namespace change.

## Validation evidence

Go 1.27.2 on macOS arm64. Final database-backed checks use native PostgreSQL
18.6 with `fsync=on` and `synchronous_commit=on`, plus the repository SQLite
engine. Jobs run sequentially with `GOMEMLIMIT=512MiB`, `GOGC=50`, and at most
`GOMAXPROCS=2` / `-p 2`. The server suite used one worker during contention.

- Original-source HTTP regressions reproduced missing cache headers on
  successful delivery/reveal/recovery-code mint responses and on early
  refusals, with both cookie and header transport. JSON-rendered nested OAuth2
  errors reproduced synthetic internal wrapper text in status-409 detail.
- Full `internal/server` suite: PASS, 80.519s. This includes the new response
  tests and existing static immutable caching, compressed representations,
  CSRF, authentication, refusal, metadata schema and SSE deadline tests.
- Selected `internal/conformance` advisory and historical-export scenarios:
  PASS on SQLite and PostgreSQL, 4.503s. The new HTTP scenario covers CLI and
  browser sessions on both engines: non-storage and all four Vary fields,
  positive metadata receipt, cancellation of reader and server handler,
  reconnect without replay, next-event credential revocation and refused
  reconnect. Existing recipient projection and metadata-only tests remain.
- Thirteen selected `internal/isolation` tests: PASS on both engines, 50.055s:
  delivery cursor/auth movement/uniform refusal/plaintext/expiry, machine
  identity and mint disclosure, all five OAuth2 tests, and the full CLI demo.
- Scoped vet and pinned Go import formatting: PASS (2,036 handwritten files).
- `sqlc`, OpenAPI, scanning and authz generation: PASS. Generated tracked and
  untracked freshness checks: PASS, with no generated changes.
- Supported analyzer matrix preparation: PASS (default, UI, macOS, Windows).
  Boundary checks: PASS, 6.462s. Complete repository lint: PASS, 446.666s.

Reproduce against a disposable durable PostgreSQL test database:

```sh
export GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50
: "${HIKYO_TEST_POSTGRES_DSN:?set a disposable durable PostgreSQL test database}"
go test -p 2 ./internal/server -count=1
go test -p 2 ./internal/conformance -run '^TestConformance(SQLite|Postgres)$/^(advisory_|historical_export_)' -count=1 -v
go test -p 2 ./internal/isolation -run '^(TestDemoFlow|TestDeliveryCursorRoundTrip|TestDeliveryAuthorizationMovementInvalidatesCursor|TestDeliveryUnauthorizedIsIndistinguishable|TestDeliveryDeliversValuesUnderAuthority|TestDeliveryCredentialExpiryIsFiniteOrAbsent|TestMachineIdentityLifecycle|TestMintDisclosureGate|TestOAuth2.*)$' -count=1 -v
go vet -p 2 ./internal/server ./internal/conformance ./internal/isolation
scripts/ci/check-go-imports.sh
go tool sqlc generate
go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml
go run ./internal/scanning/gen
go run ./internal/authz/gen
go run ./scripts/ci/prepare-lint-cache
go test -p 2 ./internal/boundary -count=1
go test -p 2 ./internal/lint -count=1
```

The first compilation and Graphify run were interrupted under host contention;
those attempts are not completed validation. The original regression run also
caught an incomplete mint principal in the new fixture. The first conformance
run failed on a fixture principal-ID collision and the non-durable initial
PostgreSQL configuration. The fixtures were corrected and a separate durable
cluster used for the passing rerun. Original failed/interrupted attempts remain
failed/interrupted evidence; their full logs are ephemeral local artifacts.

## Limits and remaining gates

No deployed proxy cache experiment, production leak, production latency,
performance benchmark, browser UI flow, release or deployment is claimed.
The full repository runtime/race/fuzz/release suites were not run locally.
Graphify produced no completed graph and remains paused under resource
coordination. Final source/spec/security inspection is local self-review.
Independent maintainer security review and exact-head hosted CI remain distinct
from local source inspection and tests. No cross-provider reviewer was invoked.
