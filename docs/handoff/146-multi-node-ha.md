# Handoff: #146 Multi-node HA (coordinated serving and automatic failover)

Issue: https://github.com/Hikyo-Org/Hikyo/issues/146 (milestone v1.1). ADR
amendment: [`ops-spec.md`](../adr/ops-spec.md) § 13,
[`system-architecture.md`](../adr/system-architecture.md) § Component set,
[`mvp-boundary.md`](../adr/mvp-boundary.md) § 4.2. Operator doc:
`docs/site/.../high-availability.mdx`.

Application-tier HA: one logical instance across several server nodes behind one
endpoint, coordinated through Postgres, with automatic failover of singleton
work. Not cross-region DR (backups stay #145).

## ADR gate

HA was decided out of v1 in three locked places. This PR opens with the
declared amendment (commit 1): operative banners promote single-region
application-tier HA to an opt-in mode, gated on the scale-out trigger the ops
spec recorded. The banners state what no prior ADR did: sessions are
datastore-backed so no load-balancer stickiness; singleton work under a fenced
lease; shared admission counters; RTO = lease TTL + probe period;
`instance_identity` stays one row shared by all nodes. The GitHub issue
reopen/re-close ceremony is governance the human performs; the banners are the
operative artifact and live in this PR.

## What was built

- **Migration 00036** (both engines): `singleton_leases`, `ha_nodes`,
  `admission_counters`, all `class=instance`. Tables exist on sqlite for schema
  parity; HA itself is refused on sqlite.
- **`store.Coordination`** (`internal/store/coordination.go`): proof-free,
  infra-level (like the outbox worker). Fenced lease (Claim/Renew/Release with a
  monotonic `fence_token`), node registry (upsert, live count, foreign-
  fingerprint check, prune), shared admission counters (windowed bump, account
  backoff). Tested both engines against real Postgres.
- **Scheduler HA** (`internal/app/scheduler.go`): with a lease, runs singleton
  jobs only while holding the `scheduler` lease; renews on a heartbeat; a lost
  or blocked renew cancels the in-flight term (fail closed). Single-node (no
  lease) is unchanged and always leader.
- **HA config** (`internal/config`): `HIKYO_HA` + `HIKYO_NODE_ID`, validated at
  boot: HA requires Postgres, a node id, and an explicit shared root-key
  source; every misconfiguration is a boot refusal.
- **HA boot** (`internal/app/ha.go`): root-key fingerprint check (refuse mixed
  keys by name), node registration, per-tick heartbeat + admission/node sweep +
  gauge refresh + instance-DEK reload, `/readyz` lease probe, HA metric source.
- **Admission sharing** (`internal/admission`): optional `SharedStore` backend;
  under HA the per-IP/meta/issuer/account counters are installation-wide, fail
  closed on backend error. Semaphore stays per node.
- **DEK freshness** (`internal/crypto/keyring.go`): under HA the project DEK
  cache revalidates a hit against the store's active version; the instance DEK
  reloads per tick. Off by default (single-node path byte-identical).
- **Metrics/health** (`internal/server`): `/readyz` fails closed on lease-
  datastore loss; three label-free gauges (`hikyo_ha_is_leader`,
  `hikyo_ha_nodes_seen`, `hikyo_ha_lease_age_seconds`), registered and pinned in
  the metrics conformance test.
- **Chart** (`chart/hikyo`): `ha.enabled` → 3 replicas, `HIKYO_HA`,
  `HIKYO_NODE_ID` from the pod name, PDB (minAvailable 2), topology spread,
  `terminationGracePeriodSeconds`. `check-chart.sh` asserts the HA render and
  refuses invalid HA configs.

## Scope decisions (made here; not spelled out by the handoff comment)

- **Fencing is lease-gated execution + term cancellation, not per-job
  `fence_token` columns.** Only the leader runs singleton jobs, and losing the
  lease cancels the running term. Per-write fence columns on job state arrive
  with #145's backup state, which has genuinely non-idempotent shared writes.
- **Admission windows are fixed one-minute buckets** (`INSERT ... ON CONFLICT`),
  not a true sliding window (which is a write per attempt). Boundary burst is up
  to 2x for one window; the semaphore still bounds real work.
- **Mixed root keys** on a shared database are refused by `LoadKeyring` in the
  common case (a wrong root cannot unwrap the master). The fingerprint check is
  load-bearing only mid-root-rotation (dual-wrapped master), so root rotation
  under HA is stop-all → rotate → start-all. Documented in the operator page.
- **Instance DEK** is a single held set, not an LRU, and `ForInstance` takes no
  context, so it cannot be revalidated per fetch; it reloads per heartbeat
  instead (bounded staleness).
- **`admission.Snapshot().ActiveBackoffs` is 0 under HA** (the in-memory map is
  empty); the shared backoff count is not worth a per-scrape query.
- **Advisory/SSE fan-out stays per node**; cross-node change visibility falls
  back to revision polling. pg `LISTEN/NOTIFY` is a follow-up, not this ticket.
- **NTP assumption**: lease acquisition compares the acquirer's clock to the
  recorded expiry; skew beyond the heartbeat is a split-brain vector.
- HA wiring (`SetHASource`, `UseShared`, `setHAProbe`, `SetHAFreshness`) runs
  during boot after the `Server` literal but before `ServeWithReady`, so the
  limiter/metrics/keyring are mutated before anything serves concurrently.

## Tests and gates

- `go build ./... && go vet ./...` clean; `go vet -tags k8se2e ./...` clean
  (the arity changes to `configureHA` / `ForeignRootKeyFingerprints` were swept
  against build-tagged files). `go vet -tags ui` needs a built `web/dist`.
- Both-engine Postgres tests: coordination lease/fence/counters/registry,
  keyring HA freshness, admission sharing (two limiters, one backend),
  scheduler HA (fake lease: non-leader idle, leader runs, drop on lost/blocked
  renew), and a **3-node integration test against real Postgres** (one leader,
  ownership matches the datastore lease, failover on leader termination,
  singleton runs exactly once and exactly once more after takeover).
- Chart: `helm lint` + `check-chart.sh` (HA render + refusals) green.
- **Failover-drill evidence** is the in-process 3-node integration test against
  real Postgres. A pod-level k8s e2e (`//go:build k8se2e`, kill the leader pod)
  is deferred: that suite runs only on the scheduled race-isolation workflow,
  never on the pull-request wall-clock (`ci.yml`), so a test added here could
  not be verified in this change and would only surface later. The in-process
  test exercises the same invariants (one leader, failover on termination,
  exactly-once singleton, stale-fence write affects zero rows) end to end
  against the real datastore.

## Cross-model review (Codex R1) fixes

- **Fence preserved on release**: `ReleaseLease` marks the row expired instead
  of deleting it, so `fence_token` never resets to 1 (a delete/reinsert would
  let a delayed same-owner process match an old token).
- **Datastore clock**: all lease-time comparisons read `Coordination.Now`
  (Postgres `now()`), so per-node clock skew is not a takeover/split-brain
  vector. `RenewLease` now requires `expires_at > now` (a paused holder cannot
  revive an expired term), and `runHA` drops locally expired leadership before
  attempting anything.
- **Atomic account backoff**: one upsert increments the failure count and
  stamps the failure instant; the delay deadline is derived from
  (failures, last_failure), never separately stored, so a coordination hiccup
  cannot leave the row admitting again or overwrite a stronger deadline. Idle
  account rows are pruned (`PruneAccountBackoff`), so unknown usernames cannot
  accumulate permanent rows.
- **Mixed-root check is atomic**: `RegisterNodeChecked` does the foreign-
  fingerprint check and the node upsert under one Postgres advisory lock
  (classid 87), so two nodes starting at once with different roots cannot both
  slip past.
- **Job fencing (finding 2, partial)**: on graceful shutdown the loop cancels
  the leadership term and JOINS it before releasing the lease, so it never
  hands the lease to a standby while its own singleton job is committing. The
  current singletons (retention sweep, reencrypt sweep) are idempotent and
  context-aware; genuinely non-idempotent shared job writes still need per-job
  `fence_token` columns, which arrive with #145's backup state (documented, not
  regressed).

## Gotchas for the next context

- Advisory-lock classids 84-86 were taken; `RegisterNodeChecked` now uses 87
  (namespace 1464159830). The lease and counters are ordinary tables (no
  classid); only the mixed-root check serializes under the advisory lock.
- The Postgres DSN env var is `HIKYO_TEST_POSTGRES_DSN`; a sqlite-only run is
  blind to the pg-only lease/counter/fence paths.
- `internal/conformance/metrics_test.go` pins the metric family registry; a new
  gauge must be added there and in `RegisteredMetricFamilies`.
