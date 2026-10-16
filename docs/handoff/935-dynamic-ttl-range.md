# #935: Checked dynamic lease lifetimes

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/935>

## Behavior

Dynamic lease requests accept whole-second lifetimes from 1 through
9,223,372,036, the largest whole-second value representable by Go's duration.
This is a representation boundary, not a new product default. Mint uses
`dynamic.TTLDuration` before budget acquisition, persistence or provider access.
Out-of-range explicit mint and renewal requests return the existing invalid-input
refusal. A representable renewal that widens the stored ceiling still conflicts,
as established by #934.

An omitted or null HTTP renewal retains the stored ceiling. The service's zero
sentinel retains that behavior for valid leases, but now checks the effective
stored value inside the authorized transaction before enqueueing or auditing a
renewal. An explicit valid tightening can repair a historical overflowing
ceiling. Explicit zero remains invalid on the HTTP contract.

The worker validates the ceiling of derived renewal transitions before loading
provider credentials or invoking the factory, including unknown renewal effects.
It records a fenced failure and preserves the current expiry for invalid values.
Revoke, expiry and crashed-mint cleanup remain available for historical invalid
rows. Response metadata intentionally keeps its existing int64 range so old
leases remain inspectable and revocable by generated clients.

PostgreSQL's positive-ceiling predicate explicitly types the parameter as BIGINT.
Without that cast, PostgreSQL inferred int4 from the zero literal and refused a
valid explicit renewal of 9,223,372,036 seconds. The active-state, positive-value
and non-increasing-ceiling predicates remain intact. The SQL scope checker now
recognizes only a BIGINT-cast bind parameter compared with an integer as a
row-independent narrowing condition. This condition cannot satisfy a tenant
chain equality; negative controls cover missing chains, column casts, functions,
subqueries, unsupported casts and top-level OR.

## Ownership and generated contracts

- `internal/dynamic/ttl.go`: shared conversion and mechanical maximum, from #934.
- `internal/service/dynamic.go` and `dynamic_worker.go`: request and worker guards.
- `internal/store/queries/postgres/dynamic_repository.sql`: typed renewal binding.
- `internal/lint/sqlpredicate_narrow.go`: bounded SQL proof grammar.
- `api/openapi.yaml`: request range and omitted-renewal semantics.
- Generated Go, sqlc and TypeScript outputs come from their pinned generators.

## Evidence

The original SQLite service flow at the #934 dependency head accepted
9,223,372,037 seconds, returned an active credential and password, and stored an
expiry in 1734. MaxInt64 returned an active credential expiring one second before
the fixture clock. Zero and negative requests already refused. These original
failures were retained.

The first post-fix run exercised both SQLite and isolated PostgreSQL 18 storage,
plus a separate real TLS PostgreSQL provider. Mint refusal, corrupt queued and
unknown renewal handling, omitted-ceiling refusal and repair, all four cleanup
states, concurrency, authorization, retry, ambiguity, claim fencing and the audit
lifecycle passed. The explicit-largest-renewal case exposed the int4 issue above;
that failed run remains recorded rather than being presented as a full pass.

After the BIGINT repair, both-engine largest/normal lifetime tests and the expanded
real-provider lifecycle passed in 5.348 seconds. The provider's actual VALID UNTIL
round-tripped dates in 2319 for maximum mint, explicit maximum renewal and omitted
maximum renewal. Tightening and widening refusal were checked separately; service
revocation removed the real role. Storage fixtures used UTC, independently of the
separate timestamp canonicalization issue.

API, dynamic and real PostgreSQL provider package tests passed. SQL predicate,
raw-SQL protocol and grammar controls passed in 39.725 seconds without refreshing
or bypassing review pins. Canonical client generation, client typechecking and all
40 client tests passed. Web consumer typechecking, lint and 202 files containing
1,763 unit tests passed, using a source-matched prebuilt OIDC helper. Three Sol
source-review rounds were independently assessed; the final round found no
additional actionable source defect. Cross-provider review was unavailable.

## Repeating focused checks

Configure isolated storage and the separate TLS provider before these commands.
The existing `scripts/ci/start-dynamic-pg.sh` owns the provider fixture setup;
keep its required flag enabled and clean up only the fixture it created.

```sh
: "${HIKYO_TEST_POSTGRES_DSN:?isolated PostgreSQL storage is required}"
: "${HIKYO_TEST_DYNAMIC_PG_DSN:?real TLS PostgreSQL provider is required}"
: "${HIKYO_TEST_DYNAMIC_PG_PASSWORD:?provider test credential is required}"
: "${HIKYO_TEST_DYNAMIC_PG_CA:?provider test CA is required}"
: "${HIKYO_TEST_DYNAMIC_PG_GRANT_ROLE:?provider grant role is required}"
export HIKYO_DYNAMIC_PG_REQUIRED=1
GOMAXPROCS=2 go test -p 1 ./internal/isolation -run '^TestDynamic(MintCheckedTTL|TTL|Renewal)' -count=1
GOMAXPROCS=2 go test -p 1 ./api ./internal/dynamic/... -count=1
GOMAXPROCS=2 go test -p 1 ./internal/lint -run 'TestTypedParameterNarrowingRetainsChainProof|TestSQLPredicatesRepo|TestRawSQLProtocolsRepo' -count=1
pnpm --dir clients/ts run verify
```

Hosted CI, review state and verified main integration are separate delivery
checks. This handoff records local evidence; it does not claim a release,
production deployment or merge.
