# encoding/json/v2 migration handoff

Completed 2026-11-14 as stack #1385, PRs #1374 to #1384 against `dev`.

## Delivered

Hand-written Go moved from `encoding/json` (v1) to `encoding/json/v2` and
`encoding/json/jsontext`, which are GA in Go 1.27. The stack has 11 layers,
each reviewed against its parent:

| PR | Branch | Scope |
|---|---|---|
| #1374 | `dunky13/jsonv2-go-1-27-usage` | `internal/jsonwire.Stable`, v1 import allowlist, redaction sink |
| #1375 | `dunky13/jsonv2-02-tags` | `omitempty` to `omitzero` on bool, number, pointer and interface fields |
| #1376 | `dunky13/jsonv2-03-adapters-auth` | adapters, OIDC, OAuth2, WebAuthn, JWKS |
| #1377 | `dunky13/jsonv2-04-trust` | crypto, release trust, schema, audit, upgrade |
| #1378 | `dunky13/jsonv2-05-store-service` | store, service, domain, authz |
| #1379 | `dunky13/jsonv2-06-core-misc` | config, configrollout, definitions and supporting packages |
| #1380 | `dunky13/jsonv2-07-server` | server, MCP, SCIM, app |
| #1381 | `dunky13/jsonv2-08-cli` | CLI and commands |
| #1382 | `dunky13/jsonv2-09-isolation` | isolation suite |
| #1383 | `dunky13/jsonv2-10-scripts` | scripts and CI tooling |
| #1384 | `dunky13/jsonv2-11-finalize` | final ban, `jsonwire.Encode`/`Decode`, apigen `omitzero` overlay |

Owner decisions:

- Use v2 semantics, but keep v1 bytes wherever output is observable: signed,
  hashed or persisted output, goldens, and the HTTP, CLI, MCP and webhook
  contracts. `jsonwire.Stable` gives those bytes.
- Stricter decoding is accepted, except for SCIM (case-insensitive, RFC 7643).
- Only the generated `api/apigen` keeps v1. `internal/lint` enforces this.

## How it stays correct

- `internal/lint` bans `encoding/json` outside `api/apigen`. A drift check keeps
  the two local `Stable` copies (`scripts/ci/pr-gate`,
  `scripts/ci/analysis-shards-go`) equal to `internal/jsonwire`.
- apigen tags come from the overlay `api/omitzero-overlay.yaml`. After any
  change to `api/openapi.yaml`:
  1. Run `go run ./api/omitzero`.
  2. Run the usual `go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml`.
  3. Run `go test ./api` to check overlay drift and the `omitzero` guard. CI's
     `web-go` job runs these tests for spec changes.
- Third-party types whose v1 `omitempty` tags change bytes under v2 encode
  through their library (Kubernetes: `k8s.io/apimachinery/pkg/util/json`) or a
  `MarshalToFunc` mirror (go-webauthn `SessionExtensions`). Apply the same rule
  to any new third-party type with observable bytes.

## Deviation from the agreed approach

The agreed apigen fix was to rewrite tags after codegen. That needs a change
to `.github/workflows/ci.yml`, which `dev` admission refuses. So the generator
produces `omitzero` itself through an OpenAPI Overlay, and the CI step stays
unchanged.

## Remaining work

1. Merge the stack bottom-up once hosted CI and maintainer review pass. Merge
   needs owner approval.
2. File an issue for the pre-existing `GOOS=windows go vet` failure on `dev`.
   It is unrelated to this stack. In both places a test file uses a helper that
   is defined only in a `//go:build unix` file:
   - `internal/app/backup_stray_args_test.go` uses `bootstrapConfig`.
   - `internal/upgradecustody/local_test.go` uses `testDirectory`.

## Not run locally

- Postgres-backed variants.
- The `k8se2e`, `floorbench`, `flooracceptance` and `opsfloor` tag suites, which
  were compiled and vetted only.
- Windows vet.
