# #977 and #989: Local key redaction guardrails

Issues: [#977](https://github.com/Hikyo-Org/Hikyo/issues/977) and
[#989](https://github.com/Hikyo-Org/Hikyo/issues/989).
Starting commit: `cb78b6be111029335e744aa3e4dcc956d5194ba1`.
Both issues were open without comments and no matching PR existed when checked
on 2026-10-18. Authorized endpoint: signed and DCO signed-off commit, push, PR.
Merge, auto-merge, enqueueing, release and deployment remain prohibited.

## Change

`LocalKeys` embeds the existing value-receiver `redactor`. Value and pointer
forms implement String, GoString, LogValue, MarshalText and MarshalJSON. Both
forms are compile-time pinned and the lint sensitive set includes LocalKeys.
The planted-secret test covers fmt, actual slog text/JSON handlers, text
marshaling and JSON encoding, rejecting raw, decimal, lower/upper hex and
Go-syntax hex-byte canaries. External-formatting fixtures cover value fmt,
pointer erasure through `any`, JSON and slog, including exact-line assertions.

`TestRedactionPinsMatchSensitiveTypes` extracts actual typed compile-time
assertions and compares them with `lint.SensitiveTypes`. `keyHandle` and
`dekEntry` are explicit crypto-only private exceptions; `swapHandle` retains its
existing membership in both sets. Stale or broadened exceptions fail. Six
mismatch fixtures per build context exercise omissions and additions in either
direction and invalid exceptions. This checks the declared sets; it does not
discover every future holder of secret material.

The file-sync digest and success-output calls explicitly project configuration
metadata before formatting. This keeps the key-holding session out of those
expressions while preserving digest fields/order and output text. Analyzer rules
and sensitive-type traversal stay intact. Key derivation, stamps, snapshot
cryptography, authorization and refusal ordering are unchanged.

## Acceptance evidence

Before the fix, the planted-secret regression printed raw, decimal and hex key
bytes and LocalKeys lacked LogValue. This reproduces a latent formatting hazard,
not an established production disclosure. An initial complete default-context
repository scan found two file-sync expressions traversing the LocalKeys-bearing
session; it exited 1. Those projections are fixed and the final scans below pass.

Final validation used Go 1.27.2 with ephemeral `GOMAXPROCS=2`,
`GOMEMLIMIT=512MiB`, `GOGC=50`, and `-p 2` where supported. Commands ran
sequentially while owning the task's shared validation slot:

- `go test -p 2 ./internal/cli -count=1 -timeout=15m`
- `go test -p 2 ./internal/crypto -count=1`
- `go vet -p 2 ./internal/crypto ./internal/cli ./internal/lint`
- `scripts/ci/check-go-imports.sh`

All four redaction checks passed in each of `default`, `ui`, `darwin` and
`windows`, with each context run in a separate process to release type data.
The synchronization test's six deliberately mismatched fixtures also passed in
each context by requiring the expected findings:

```sh
checks='^(TestRedactionPinsMatchSensitiveTypes|TestRedactionSurfacesRepo|TestSensitiveFormattingRepo|TestSensitiveFormattingCatchesViolations)$'
for context in default ui darwin windows; do
  GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go test -p 2 ./internal/lint \
    -run "$checks/^$context$" -count=1 -v -timeout=20m
done
```

The final current-source CLI isolation run passed all four cases without skips:

```sh
: "${HIKYO_TEST_POSTGRES_DSN:?set a disposable test-owned PostgreSQL 18 DSN}"
GOMAXPROCS=2 GOMEMLIMIT=512MiB GOGC=50 go test -p 2 ./internal/isolation \
  -run '^(TestComposeCLIDeliverySQLite|TestComposeCLIDeliveryPostgres|TestFileSyncCLISQLite|TestFileSyncCLIPostgres)$' \
  -count=1 -v -timeout=15m
```

`HIKYO_TEST_POSTGRES_DSN` pointed at a fresh test-owned PostgreSQL 18 cluster
through a private Unix socket. Its task-specific port was verified unused before
startup; ownership and PID were recorded, and cleanup stopped only that cluster
before releasing the validation slot. Reproduction requires a disposable
test-owned server with database-creation permission, not a production DSN.
Earlier both-engine test evidence is retained separately from this final rerun.

Full crypto race tests and the full compose/file-sync package suites also passed.
Canonical generation passed with no generated diff or untracked generated
output: `go tool sqlc generate`, `go tool oapi-codegen --config
api/oapi-codegen.yaml api/openapi.yaml`, `go run ./internal/scanning/gen` and
`go run ./internal/authz/gen`, using bounded Go parallelism. Their inputs were
unchanged by the subsequent CLI projections; final generated diffs remain empty.
`git diff --check` passed. No tests, analyzers or CI policy were weakened.

## Limits and remaining gates

The first broad lint attempt was stopped with exit 143 before its package load
completed. Both Graphify attempts were also stopped with exit 143 during resource
coordination; their caches remain, but no graph freshness proof exists. The
final validation batch above passed after serialization. Repository CI and
security review remain separate requirements before any later merge decision.

No UI code changed; web checks and browser flows were not run. Full repository
Go tests, broad race/fuzz suites and CRD generation were not run locally. No
production, external-provider, performance or full CI claim is made. No schema,
OpenAPI, CRD or generator input changed. No independent reviewer or paid review
provider was launched. Preserve the explicit no-merge hold.
