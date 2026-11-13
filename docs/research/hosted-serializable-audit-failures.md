# Task: cross-org SERIALIZABLE audit failures on the real schema

Date: 2026-11-13
Ticket: https://github.com/Hikyo-Org/Hikyo/issues/1311
Map: https://github.com/Hikyo-Org/Hikyo/issues/1301
Code: `origin/dev` 748930f1f, migrations through `00087`. PostgreSQL 18.6.
Sources: [hosted-service ADR](https://github.com/Hikyo-Org/Hikyo/blob/dev/docs/adr/hosted-service.md) (decision 10), [handoff](https://github.com/Hikyo-Org/Hikyo/blob/dev/docs/handoff/hosted-service-adr.md) (pgbench method), [PostgreSQL research](https://github.com/Hikyo-Org/Hikyo/blob/research/hosted-postgres-contention/docs/research/hosted-postgres-contention.md).

## Result

**The cause is the plan of one statement in the commit-order trigger, not SERIALIZABLE audit inserts as such.**

`assign_audit_tenant_commit_seq()` (migration `00011_audit_export_order.sql`) runs, at commit, for every audit row:

```sql
UPDATE audit_tenant_events SET commit_seq = nextval('audit_tenant_commit_seq')
WHERE seq = NEW.seq AND commit_seq IS NULL;
```

Once the table has statistics, the planner serves this with an index scan on `audit_tenant_events_commit_seq_unique` (`Index Cond: commit_seq IS NULL`, `Filter: seq = …`), not with the `seq` primary key. Every audit row is inserted with `commit_seq` NULL, and NULLs sort last. So the scan walks every in-flight audit row of every org, plus dead NULL versions that earlier `UPDATE`s left in the index and that have not yet been removed. Reading those rows makes each committing transaction an SSI reader of other orgs' concurrent writes. The evidence, kept in `observations.txt`:

- **Where it fails.** The failing statement is always the trigger's `UPDATE` during `COMMIT`, with `Reason code: Canceled on conflict out to pivot N, during read`. That held for all 5,759 serialization failures logged in one 15-second pgbench run at 16 clients and 400/s, and for all 60,533 logged across the runs before it.
- **Locks under load, original plan.** Sampled `pg_locks` show SIRead locks on `audit_tenant_events_commit_seq_unique` pages, on `audit_tenant_events` heap pages, and relation-level SIRead locks on `audit_tenant_events`. A relation-level SIRead lock conflicts with every insert from every org. These come from PostgreSQL promoting many page locks to one relation lock past `max_pred_locks_per_relation` (default −2, which is 32 pages here with `max_pred_locks_per_transaction = 64`), so how often it happens depends on that setting.
- **Locks under load, primary-key plan.** With the same `UPDATE` forced onto the primary key, the only sampled table locks are `audit_tenant_events_pkey` pages, and the failures go to zero (tables below).

A freshly migrated, never-analyzed table gets the primary-key plan (`observations.txt` §1). This likely explains why the handoff's runs on an empty database, and this task's first short runs, began with few failures and degraded later. That was not measured directly. The instance trail's `assign_audit_instance_commit_seq()` has the same predicate, but it was not measured.

**Ruled out.** Each candidate was removed on its own, all on the original `commit_seq IS NULL` plan, at 64 clients, 400/s and native commit. None reduced failures (2.5–3.7%, against 2.71% for baseline):

- the `upgrade_control FOR SHARE` read
- shared lock 85
- global lock 84: a per-org lock with the original `UPDATE` was 3.74%

The stronger argument is the primary-key plan: it reaches 0 failures with `FOR SHARE`, lock 85, lock 84 and `nextval` all still in place. A trigger that only calls `nextval` also gives 0, because sequences sit outside SSI. REPEATABLE READ and READ COMMITTED give 0 with the original trigger, and so does removing the deferred trigger.

**Second fact: per-org commit order (decision 10).** With the primary-key plan, global lock 84 is what keeps the remaining conflicts away. Replacing lock 84 with a per-org lock on top of the primary-key plan brings failures back: 0.10% at native commit and 4.78% at a 1 ms commit (64 clients, 400/s). The likely mechanism, inferred and not lock-sampled: concurrent deferred `UPDATE`s read the rightmost `seq` primary-key leaf, and a non-HOT update also writes a new entry there. A per-org commit order therefore needs a design that does not read a shared index tail inside SERIALIZABLE. A different lock key alone is not enough.

**Third fact: the lock 84 ceiling.** With the primary-key plan and a 1 ms commit, audited commits topped out at:

- about 540–570 per second in pgbench, with seconds of queueing when offered 800 per second;
- 674 per second on the real path.

That implies lock 84 is held for about 1.5–1.85 ms per commit: the 1 ms commit delay plus the rest of the commit work done under the lock. The ceiling is set by that hold time, as the PostgreSQL research predicted, not by the commit delay alone. At native (sub-millisecond) commit, both carried about 800 per second.

**Real write path.** Three nodes driving the real revealing read reproduce the failures: 15–17% at 400 per second and 34–45% at 800 per second. The primary-key plan removes them, with 0 at every rate. No other cross-org conflict source showed up on that path at these rates.

## Method

**Host.** Apple M2 Pro with 8 performance and 4 efficiency cores, 16 GB, macOS. PostgreSQL 18.6 (Homebrew) ran natively, so it could use all cores. Client and server shared the host, connecting over `127.0.0.1`.

**Server settings.** `shared_buffers=1GB`, `max_connections=300`, `max_pred_locks_per_transaction=64`, otherwise defaults (`synchronous_commit=on`).

**Commit latency.** macOS `fsync` is fast, so "native commit" is sub-millisecond. "1 ms commit" adds `commit_delay=1000` and `commit_siblings=0` per database, as the handoff did.

**Schema.** Hikyo's own admitted migration path (`store.Open` through the upgrade gate fixture; `migrate_test.go.txt`) built a `tmpl` database. `template.sql` then built `tmpl_big` from it.

### 1. pgbench, the handoff's method, on a production-like table

Each transaction runs at SERIALIZABLE, with up to 4 tries (`--max-tries=4`):

```sql
\set org random(1, 1000)
BEGIN ISOLATION LEVEL SERIALIZABLE;
SELECT 1 FROM upgrade_control c FOR SHARE OF c;
INSERT INTO audit_tenant_events (id, type, schema_version, occurred_at, occurred_asserted, actor_id, actor_class, scope_class, org_id, outcome, origin, payload)
VALUES (gen_random_uuid()::text, 'secret.read', 1, now(), true, 'usr_x', 'human', 'org', 'org_' || :org, 'success', 'api', '{}');
END;
```

`internal/store/tx` makes the same 4 attempts, but it waits 10, 50 and 250 ms between them, while pgbench retries at once. Unlike the handoff, the table starts with 500,000 rows over 1,000 orgs, then `VACUUM ANALYZE`, because the plan changes with statistics. Run settings:

- every case on a fresh copy of that template;
- 30 seconds, open-loop at a fixed rate (`pgbench -R`);
- `-M prepared`, 4 pgbench threads.

"failed" means transactions that still failed after 4 tries.

Variants (all in `hosted-serializable-audit-failures/`):

- `baseline`: the schema as shipped.
- `pk-plan` (`v_pk.sql`): the same `UPDATE`, with the NULL test written `(commit_seq + 0) IS NULL`, so only the primary key can serve it. The plan was checked with `auto_explain`. `commit_seq IS NOT DISTINCT FROM NULL` does **not** work for this: PostgreSQL 18 plans it as `IS NULL` on the `commit_seq` index (`observations.txt` §1).
- `pk-plan+per-org-lock` (`v_pk_orglock.sql`): `pk-plan` with `pg_advisory_xact_lock(hashtext(org_id))` instead of lock 84.
- Controls: `no-deferred-trigger` (`v_notrig.sql`) and `REPEATABLE READ` (`audit_rr.sql`).

Native commit (`pgbench-results.txt`):

| clients | rate/s | baseline failed | pk-plan failed | baseline tps | pk-plan tps |
|---|---|---|---|---|---|
| 16 | 200 | 0.94% | 0 | 200 | 204 |
| 16 | 400 | 11.6% | 0 | 353 | 391 |
| 16 | 800 | 11.6% | 0 | 702 | 795 |
| 64 | 200 | 1.69% | 0 | 195 | 200 |
| 64 | 400 | 7.16% | 0 | 374 | 406 |
| 64 | 800 | 15.8% | 0 | 678 | 798 |

1 ms commit:

| clients | rate/s | baseline failed | pk-plan failed | baseline tps | pk-plan tps |
|---|---|---|---|---|---|
| 16 | 200 | 3.97% | 0 | 190 | 205 |
| 16 | 400 | 21.4% | 0 | 315 | 401 |
| 16 | 800 | 65.8% | 0 | 274 | 571 (avg latency 4.4 s, saturated) |
| 64 | 200 | 3.29% | 0 | 192 | 202 |
| 64 | 400 | 24.8% | 0 | 299 | 402 |
| 64 | 800 | 61.9% | 0 | 305 | 539 (avg latency 4.9 s, saturated) |

Controls at 64 clients, 400/s:

| variant | native failed | 1 ms failed |
|---|---|---|
| pk-plan+per-org-lock | 0.10% (3,012 transactions retried) | 4.78% |
| no-deferred-trigger | 0 | 0 |
| REPEATABLE READ (original trigger) | 0 | 0 |

**Earlier runs** (`pgbench-results-early.txt`, same method, 30 seconds unless noted). These ran at 64 clients, 400/s and native commit, all on the original `commit_seq IS NULL` plan:

| variant | failed |
|---|---|
| baseline | 2.71% |
| no `FOR SHARE` | 2.62% |
| no lock 85 | 2.54% |
| per-org lock instead of 84 | 3.74% |
| READ COMMITTED | 0 |

**Trigger-body ablations** (16 clients, 400/s, 15 seconds):

| trigger body | plan | failed |
|---|---|---|
| reads its row only (`v_select.sql`), no write | `commit_seq` NULL plan | 11.2% |
| `nextval` only | none | 0 |
| `UPDATE` without the sequence (`v_noseq.sql`) | NULL plan | 5.49% |
| `UPDATE` with the four `commit_seq` indexes dropped (`v_pk_noidx.sql`) | primary key only | 0 |

Absolute rates vary between runs with how long the table has run since its last vacuum. The direction never changed.

### 2. Real write path, several nodes

A Go test (`zz_audit_stress_test.go.txt`) drives the real revealing read, `(*service.Values).Get(…, reveal=true)`. That is a SERIALIZABLE `tx.WriteResult` with session and grant resolution, authorization, the value read and one disclosure audit row. `internal/store/tx` retries it up to 4 attempts. Setup:

- **Orgs.** 200, each with its own project, environment, one `secret` key and a value, all created through the services. One human principal holds `read`, `edit`, `publish`, `reveal` and `definitions-edit` in every org.
- **Nodes.** Three `store.Open` pools in one test process, on the same admitted database, each with its own keyring load and services. Each pool has the default 10 connections, so at most 30 transactions run at once. 48 client goroutines are spread over the nodes.
- **Table.** 200,000 pre-loaded audit rows, then `VACUUM ANALYZE`.
- **Load.** Open loop for 30 seconds: one op per tick at the target rate, random org per op. An op that finds no idle client within one tick is counted `late` and dropped. Go's ticker also silently drops ticks while the loop is blocked, so `attempted + late` can fall short of `rate × 30` when clients are saturated. Baseline at native commit and 800/s attempted 9,017 with 13,610 late, against 24,000 offered.
- **Failure.** "failed" means the final error after `tx`'s retries carries SQLSTATE 40001 or 40P01. No other errors occurred.

`real-path-results.txt`:

| commit | rate/s | baseline failed | baseline ok/s | baseline late | baseline p99 | pk-plan failed | pk-plan ok/s | pk-plan late | pk-plan p99 |
|---|---|---|---|---|---|---|---|---|---|
| native | 200 | 0.63% | 198.7 | 0 | 240 ms | 0 | 199.9 | 0 | 11 ms |
| native | 400 | 16.8% | 325.7 | 228 | 313 ms | 0 | 399.8 | 0 | 7 ms |
| native | 800 | 45.2% | 164.6 | 13,610 | 345 ms | 0 | 781.3 | 360 | 82 ms |
| 1 ms | 200 | 1.87% | 196.2 | 0 | 263 ms | 0 | 199.9 | 0 | 13 ms |
| 1 ms | 400 | 15.1% | 339.0 | 6 | 304 ms | 0 | 399.9 | 0 | 13 ms |
| 1 ms | 800 | 34.4% | 299.6 | 10,113 | 318 ms | 0 | 673.8 | 3,722 | 95 ms |

This reproduces the ADR's figure (8% failed at 400 per second) with the real write path from three pools: 15–17% at 400 per second here. Baseline p99 sits near 300 ms because failing operations spend their retry backoff. With the primary-key plan, what remains at 1 ms commit and 800 per second is a throughput limit (lock 84 hold time and 30 connections), not failures.

## What this does not cover

- **Core count.** The ticket asks for production-like core counts, but production's core count is not decided. This host gave PostgreSQL 8 performance cores, a common size for a mid-size managed primary (8 vCPU). It also shared those cores with the clients. Results on a larger or smaller primary were not measured.
- **One host.** Client, Go pools and PostgreSQL share CPUs, and there is no network round trip. Network latency inside each transaction widens overlap, which would likely raise the baseline failure rate, not lower it.
- **Nodes.** The three nodes are pools in one process, not separate server processes or machines.
- **No synchronous standby.** The 1 ms commit stands in for one. A real standby's round trip is part of the lock 84 hold time.
- **No pooler.** All runs used direct connections.
- **One operation.** The real path covered only the revealing read. Value writes, grant changes, denial flushes and a mix of operations were not driven.
- **One trail.** Only the tenant trail was measured. The instance trail has the same trigger predicate.
- **Other open items.** The PostgreSQL research's other open items were not measured: MultiXact growth on `upgrade_control`, starvation of a maintenance `UPDATE`, and pooler behaviour.
- **Lost raw logs.** The server log and `pg_locks` samples were not kept as files. `observations.txt` is a transcription of what the session printed.

## Files

`docs/research/hosted-serializable-audit-failures/`:

- **Setup.** `migrate_test.go.txt` builds the migrated `tmpl` database, and `template.sql` builds `tmpl_big` from it. `explain.sql` shows the trigger's plan and the SIRead locks it takes in one session. `sample-locks.sh` samples SIRead locks under load.
- **pgbench.** `audit_*.sql` are the pgbench scripts and `v_*.sql` the trigger variants. `run.sh`, `q.sh` and `matrix2.sh` are the runners; `PGBIN`, `PGHOST`, `PGPORT` and `PGUSER` select the server. `matrix2.sh` force-drops a database named `audit_bench_scratch`.
- **Go harness.** `zz_audit_stress_test.go.txt`. Copy it into `internal/isolation/` as `zz_audit_stress_test.go`, then run `HIKYO_AUDIT_STRESS=1 HIKYO_TEST_POSTGRES_DSN=postgres://…/stress go test -p 2 -count=1 -run TestZZAuditStress -v ./internal/isolation/`. Its settings are `HIKYO_AUDIT_STRESS_{ORGS,NODES,CLIENTS,RATE,SECS,PRELOAD_ROWS,VARIANT_SQL}`.
- **Results.** `pgbench-results.txt`, `pgbench-results-early.txt` and `real-path-results.txt` hold the raw output lines behind the tables. `observations.txt` holds the plans, lock samples and log tallies.
