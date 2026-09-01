# Handoff: #514 OpenAPI warmup and request allocations

Issue: https://github.com/Hikyo-Org/Hikyo/issues/514. Base:
`09f26413714c4001ce120c3c7800217b8c25cf43`.

## Contract

- Server boot parses and validates the embedded OpenAPI document before either
  listener opens; a load failure refuses startup.
- Runtime OpenAPI request validation remains enabled for every API request.
- Matched request accessors share the immutable operation-registry row.
  `Operation.Formula()` and `Operation.Artifacts()` return inspection copies.
- Operation slice fields are private and inspection accessors return copies, so
  the validated request can attach the immutable row to context with no clone.

## Coverage

- Boot test injects the warmup dependency and refuses any listener acquisition
  before warmup completes.
- HTTP benchmark covers authenticated `GET /api/v1/orgs` and
  `POST /api/v1/orgs` through the public middleware stack with OpenAPI request
  validation enabled.
- Apple M2 Pro baseline to optimized allocation counts: GET 89 to 81 allocs/op
  and 11,613 to 11,485 B/op; POST 1056 to 1048 allocs/op and 88,609 to
  88,411 B/op. Five-run post-change counts were stable, excluding GET's first
  warmup-affected sample. Comparison was captured on original base
  `a84fd620cedb068988b4d996a5610d12638172b1` before the final base refresh.
- CI exposed a pre-existing importer test race: its dead Kubernetes origin
  could refuse TCP before the hung exec-plugin deadline won. The fixture now
  uses a reachable TLS server and fails if the API is reached, leaving the
  plugin deadline as the sole expected outcome.
- Generated outputs: none.
