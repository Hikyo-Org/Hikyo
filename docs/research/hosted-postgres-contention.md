# Research: PostgreSQL behaviour for a shared multi-tenant primary (poolers, advisory locks, FOR SHARE, SERIALIZABLE)

Date: 2026-11-12
Ticket: https://github.com/Hikyo-Org/Hikyo/issues/1308
Target: PostgreSQL 17 and 18. Hikyo code read at `origin/dev` 4ea1ff4a2 (2026-11-12).

## How to read this

- **Fact** lines are stated by the cited primary source.
- **Inference** lines combine cited facts with Hikyo code. No source states them directly. The benchmark task should confirm or refute them.
- **Unverified** means no primary source was found.
- PostgreSQL C source was fetched on 2026-11-12 from `REL_17_STABLE` (head 179f22f256) and `REL_18_STABLE` (head b9a7d9b8f2) on github.com/postgres/postgres. Line numbers refer to those files. If a line differs between the branches, both are given as 17/18.
- Docs are cited with version-pinned URLs. The 17 and 18 texts quoted here are identical unless the doc says otherwise.
- Path correction: the ticket names `queries/postgres/recovery_runtime.sql`. The file is actually `internal/store/queries/postgres/recovery_runtime.sql`.

This document makes no decisions. It records facts and what they imply for each Hikyo code path.

---

## 0. Summary

1. **Session advisory locks do not work under transaction pooling.** PgBouncer documents "Session-level advisory locks: Never" for transaction pooling. pgcat and Supabase's Supavisor-based shared pooler say the same. `tx.WriteSerialized` takes a session lock (`pg_advisory_lock`) as a separate autocommit statement and then opens its transaction. Behind any of these poolers, that sequence can leave the lock on a backend that other clients reuse. The documented pooler-safe replacement, `pg_advisory_xact_lock`, takes the snapshot before it waits when it runs as the first statement of a SERIALIZABLE transaction (inference from `postgres.c` and `snapmgr.c`). It is therefore not a drop-in replacement.
2. **Every Hikyo Postgres transaction share-locks one row.** This includes read-only reads. `beginPostgresOn` runs `GuardPostgres`, which does `SELECT … FOR SHARE OF c` on `upgrade_control` singleton 1.
   - Whenever two or more of these transactions overlap, PostgreSQL creates a new MultiXact for each one that joins.
   - Each lock also dirties the heap page, writes a WAL record and assigns an XID.
   - Because of that, every read commits as a synchronous commit: it flushes WAL and waits for any synchronous standby (inference from `xact.c`). These flushes can be group-committed, so the cost is latency and fsync load, not a global serialization point.
   - Row locks are not allowed on a hot standby, so Hikyo's read path cannot run on a read replica as written.
3. **Audit commits across all tenants are serialized.** Audit lock 84 is taken in a deferred trigger and released only after the commit record has been flushed and any synchronous-replica wait has finished. Every transaction that wrote an audit row therefore passes through one global lock for the whole commit-flush latency (inference from `xact.c`).
4. **SERIALIZABLE false conflicts are mostly page-level.** Btree inserts check for conflicts against the leaf page they write to, and searches predicate-lock leaf pages. Monotonic keys (`commit_seq`, `seq`) concentrate inserts on the rightmost leaf. Sequences sit outside SSI entirely. Under SSI, a UNIQUE violation is reported as 40001 instead of 23505 only when the transaction has read the key range first.
5. **Per-tenant ordering options differ under SERIALIZABLE.**
   - A per-org counter row turns concurrent writers of the same org into 40001 retries, not waits.
   - A per-org advisory xact lock combined with the existing global sequence keeps commit order within each org. Gaps remain, which 00011 already accepts.
   - Partitioning by org splits indexes and predicate-lock pages per partition. It requires the org key in every UNIQUE constraint.

---

## 1. Advisory locks and transaction-mode poolers

### 1.1 PostgreSQL facts

- **Fact.** Advisory locks exist at session level and at transaction level. A session lock "is held until explicitly released or the session ends". Session lock requests "do not honor transaction semantics: a lock acquired during a transaction that is later rolled back will still be held following the rollback, and likewise an unlock is effective even if the calling transaction fails later". Transaction-level locks "are automatically released at the end of the transaction, and there is no explicit unlock operation". Source: https://www.postgresql.org/docs/18/explicit-locking.html §13.3.5 (same text at /docs/17/).
- **Fact.** Locks are reentrant across scopes: "If a session already holds a given advisory lock, additional requests by it will always succeed, even if other sessions are awaiting the lock; this statement is true regardless of whether the existing lock hold and new request are at session level or transaction level." Session requests stack, so three locks need three unlocks. Sources: same section, and https://www.postgresql.org/docs/18/functions-admin.html §9.28.10.
- **Fact.** The single-`bigint` key space and the two-`int4` key space "do not overlap". Source: functions-admin §9.28.10.
- **Fact.** `pg_advisory_unlock_all()` "is implicitly invoked at session end, even if the client disconnects ungracefully". Source: functions-admin §9.28.10.
- **Fact.** Advisory locks and regular locks share one memory pool sized by `max_locks_per_transaction` and `max_connections`. Exhausting it means "the server will be unable to grant any locks at all". Source: explicit-locking §13.3.5.
- **Fact.** Advisory locks always go through the shared lock table and never use the fast path. Fast-path eligibility is limited to `DEFAULT_LOCKMETHOD` relation locks weaker than ShareUpdateExclusive (`lock.c` `EligibleForRelationFastPath`, 17:213 / 18:267). Every acquire and release of one hot advisory key therefore takes the same lock-partition LWLock, which shows up as the `LockManager` wait event (https://www.postgresql.org/docs/18/monitoring-stats.html, wait-event tables).
- **Fact.** Queueing: a new request that conflicts with a mode already *waited for* must join the wait queue, even if it is compatible with every current holder: `if (lockMethodTable->conflictTab[lockmode] & lock->waitMask) found_conflict = true;` (`lock.c` 17:1036 / 18:1093). A request for a lock the backend already holds skips this check and just increments its local count (`lock.c` 17:877–883 / 18:934–940).
- **Fact.** Advisory locks are not WAL-logged and "relate only to the server on which they are acquired". Source: https://www.postgresql.org/docs/18/hot-standby.html §26.4.

### 1.2 Pooler facts

| Pooler | Session advisory locks in transaction mode | Session `SET` | Named / protocol prepared statements in transaction mode | Reset between clients in transaction mode |
|---|---|---|---|---|
| PgBouncer 1.26 | **Never** (feature table) | Never | Yes, when `max_prepared_statements` > 0. Default is 200 since 1.24.0. Added in 1.21.0. | `server_reset_query` "is not used" in transaction pooling. `server_reset_query_always` defaults to 0. |
| pgcat (main, last commit 2025-02-27) | **Not supported**. The docs recommend `pg_advisory_xact_lock` instead. | Not supported | The README says not supported. CONFIG lists `prepared_statements_cache_size` (default 0, "TODO: update documentation"). The two docs conflict. | Unverified |
| Supavisor (supabase/supavisor main) | Not stated in Supavisor's own docs, which say transaction-mode behaviour "is the same as PgBouncer". Supabase's hosted-docs page states session-level advisory locks are lost between transactions. | Lost (Supabase docs) | Supported when the `named_prepared_statements` feature flag is on. Supabase's hosted shared pooler: "Prepared statements aren't supported". | Unverified |

Sources:
- PgBouncer feature table: https://www.pgbouncer.org/features.html
- PgBouncer config: https://www.pgbouncer.org/config.html (`server_reset_query`, `server_reset_query_always`, `max_prepared_statements`, `pool_mode`; the default `pool_mode` is `session`)
- PgBouncer changelog: https://www.pgbouncer.org/changelog.html (1.21.0 prepared statements; 1.24.0 default 200)
- pgcat: https://github.com/postgresml/pgcat/blob/main/README.md ("Transaction mode" section), https://github.com/postgresml/pgcat/blob/main/CONFIG.md (`pool_mode` default `"transaction"`)
- Supavisor: https://github.com/supabase/supavisor/blob/main/docs/migrating/pgbouncer.md, https://github.com/supabase/supavisor/blob/main/docs/faq.md, https://github.com/supabase/supavisor/blob/main/docs/configuration/pool_modes.md
- Supabase hosted: https://supabase.com/docs/guides/database/connecting-to-postgres ("Transaction mode limitations": "Session-level state is lost between transactions. This covers set and reset, session-level advisory locks, listen and notify, and temporary tables." It also says query pipelining is not supported in transaction mode.)

pgx facts:
- **Fact.** pgx v5.12.0, the version in Hikyo's `go.mod`, defaults to `QueryExecModeCacheStatement`, which automatically prepares and caches named statements (`github.com/jackc/pgx/v5@v5.12.0/conn.go:192, 657–661`). Hikyo does not override this in production code. It sets `default_query_exec_mode=simple_protocol` only in tests (`internal/isolation/scim_page_bound_test.go:152`).

### 1.3 Hikyo code paths

**A. `tx.WriteSerialized` → `store.BeginPostgresSerialized`** (`internal/store/tx/tx.go:80`, `internal/store/admission_serialized.go:17–56`, SQL `internal/store/queries/postgres/recovery_runtime.sql` `AdmissionSerializedLock`/`AdmissionSerializedUnlock`).

The sequence is:
1. Acquire one pgxpool conn.
2. Run `SELECT pg_advisory_lock(ns,key)` as an autocommit statement.
3. Run `BEGIN ISOLATION LEVEL SERIALIZABLE` on the same pgx conn.
4. Run `GuardPostgres` and the domain SQL.
5. Run COMMIT.
6. Run `SELECT pg_advisory_unlock(ns,key)`.
7. If the unlock is not confirmed, Hijack and Close the conn.

The call sites are org creation (`hierarchy.go:212`), local signup (`local_signup.go:105, 317`), OAuth2 and OIDC first-login org creation (`oauth2_flow.go:536`, `oidc_flow.go:663`), backups (`backup.go`, `backup_dr.go`), and self-config (`bootstrap.go`, `self_config_*.go`). On a hosted multi-tenant primary the signup and login paths are tenant-driven, so this lock is not only a rare control-plane operation.

- **Direct connection or session pooling.** This matches the documented semantics. The lock lives on one backend for the life of the client conn, and the discard path ends the session, which runs `pg_advisory_unlock_all`.
- **Transaction pooling (PgBouncer, pgcat, Supavisor transaction mode). Inference from §1.1 and §1.2:**
  1. Step 2 is its own implicit transaction. When it completes, the pooler may return the server backend to the pool.
  2. Steps 3–5 can run on a different backend from step 2, and step 6 on yet another one. `pg_advisory_unlock` then returns false because that backend holds nothing, and Hikyo discards its *client* connection.
  3. Closing the client connection does not end the server session, because the pooler keeps the server backend. In transaction mode PgBouncer does not run `server_reset_query`, so the session lock stays on a pooled backend indefinitely.
  4. Any later Hikyo transaction routed to that backend can take the same lock again, because session locks are reentrant ("additional requests by it will always succeed"). Mutual exclusion between those callers is then lost. Every other backend blocks on the key forever.
  5. Separately, `PostgresTransaction.Commit` joins the unlock error into its return value (`admission.go:166–180`). A caller can therefore see an error even though the transaction committed.
  6. Under low pooler load the backends may happen to coincide, so the failure is intermittent.
- **Documented alternative, and its limit.** pgcat's docs name `pg_advisory_xact_lock` as the transaction-mode replacement. Hikyo's comment explains why its lock is taken *before* BEGIN: waiters must not hold stale snapshots. A transaction-level lock can only be taken inside a transaction.
  - **Fact (docs).** Under REPEATABLE READ and SERIALIZABLE, the transaction sees a snapshot "as of the start of the first non-transaction-control statement" (https://www.postgresql.org/docs/18/transaction-iso.html §13.2.2).
  - **Fact (source).** For a statement that needs parse analysis, such as any `SELECT`, PostgreSQL calls `GetTransactionSnapshot()` *before* planning and execution. This happens in:
    - `exec_simple_query` (`postgres.c` 17:1166–1168 / 18:1161–1163)
    - `exec_parse_message` (17:1515–1517 / 18:1509–1511)
    - `exec_bind_message` (17:1794–1796 / 18:1788–1790)

    `analyze_requires_snapshot` is true for `SelectStmt` (`parser/analyze.c`, REL_18_STABLE:447–506). In transaction-snapshot mode, the first `GetTransactionSnapshot()` call fixes `FirstXactSnapshot` for the rest of the transaction (`snapmgr.c`, `GetTransactionSnapshot` at 17:216 / 18:271; the copy-and-register block is at 18:308–331).
  - **Inference.** In a SERIALIZABLE or REPEATABLE READ transaction, `SELECT pg_advisory_xact_lock(...)` as the first statement takes the transaction snapshot *before* it waits for the lock. A waiter would therefore resume with a snapshot from before the previous holder committed. That is the stale-snapshot case the `WriteSerialized` comment says it avoids. A transaction-level lock at the head of the SERIALIZABLE transaction is not a drop-in equivalent of the current session lock.

**B. Transaction-scoped advisory locks** (classes 84, 85, 87, 88): `00011_audit_export_order.sql` triggers; `recovery_runtime.sql:7` (`AuditExportWriterBarrier`); `coordination_runtime.sql:60, 166`; `self_config.sql:74`. These are pooler-safe by the documentation of all three poolers, because they live and die inside one transaction.

**C. `upgrade.SetPostgresSessionTimeouts`**, run from the main pool's AfterConnect hook (`internal/store/store.go:1353`, `internal/store/upgrade/lock.go:241`). It sets `tcp_user_timeout` and the keepalive GUCs with `set_config(…, false)`, which is session scope.
- **Fact.** Session `SET` is "Never" under PgBouncer transaction pooling, and pgcat and Supabase say the same.
- **Inference.** Behind a transaction pooler, the settings land on whichever backend served the AfterConnect query, leak to other clients, and apply to the pooler↔server socket, not the Hikyo↔pooler socket. The stated purpose, dropping a vanished Pod's session, does not hold through a pooler.

**D. pgx statement cache.** Under PgBouncer this works only with `max_prepared_statements` > 0 (the default since 1.24.0). Under Supabase's shared pooler, prepared statements must be disabled, which in pgx means `default_query_exec_mode` exec or simple protocol. Under pgcat the docs conflict. **Unverified for pgcat.**

---

## 2. Row locks, `FOR SHARE` and MultiXact growth

### 2.1 Facts

- **Fact.** "locking a row might cause a disk write, e.g., SELECT FOR UPDATE modifies selected rows to mark them locked". Source: https://www.postgresql.org/docs/18/explicit-locking.html §13.3.2.
- **Fact (source).** `heap_lock_tuple` does three things:
  - It assigns the transaction an XID through `GetCurrentTransactionId()`, which assigns one "if it's not yet set" (`heapam.c` 17:5406 / 18:5382; `xact.c` 17:452 / 18:455).
  - It dirties the buffer (`MarkBufferDirty`, 17:5455 / 18:5431).
  - It emits an `XLOG_HEAP_LOCK` WAL record (17:5489 / 18:5465). The first touch of a page after a checkpoint can also write a full-page image (standard WAL behaviour; not separately verified here).
- **Fact.** A single locker is stored as its XID in `xmax`. "Whenever we need to store more than one lock, we replace the first locker's Xid with a new MultiXactId." Source: `src/backend/access/heap/README.tuplock` (MultiXacts section). Multixacts are created "whenever there is more than one transaction concurrently locking a row" (https://www.postgresql.org/docs/18/routine-vacuuming.html §24.1.5.1).
- **Fact (source).** Joining an existing MultiXact never modifies it. `MultiXactIdExpand` creates a **new** MultiXact: "we do NOT actually modify the membership of a pre-existing MultiXactId; instead we create a new one" (`multixact.c` 17:518 / 18:519). The new MultiXact copies only members that are still running, or updaters that committed, and then adds the new locker (`multixact.c` 17:593– / 18:594–). If no member of the old MultiXact is still running, `compute_new_xmax_infomask` discards it and stores the new locker's XID alone (`heapam.c` 17:5688 / 18:5664).
- **Inference.** On a single hot row, each new share-locker that arrives while others still hold the lock consumes one MultiXact ID plus as many member slots as there are concurrent lockers at that moment. Member-space consumption therefore scales roughly as (lock acquisitions per second) × (average concurrent lockers). Only true overlap produces MultiXacts; serial lockers do not.
- **Fact.** MultiXact IDs and the member space are each 32-bit counters needing wraparound management. An aggressive vacuum is forced when `relminmxid` age exceeds `vacuum_multixact_freeze_table_age` or `autovacuum_multixact_freeze_max_age`. "If the storage occupied by multixacts members exceeds about 10GB, aggressive vacuum scans will occur more often for all tables … The members storage area can grow up to about 20GB before reaching wraparound." Source: routine-vacuuming §24.1.5.1 (17 and 18). In source: `MaxMultiXactOffset` is `0xFFFFFFFF`, `MULTIXACT_MEMBER_SAFE_THRESHOLD` is half of it (`multixact.h:30`; `multixact.c` 17:215 / 18:216).
- **Fact.** PostgreSQL `master` widened `MultiXactOffset` to 64 bits (commit bd8d9c9bdf, 2025-12-09; `src/include/c.h` on master: `typedef uint64 MultiXactOffset`). This is not in 17 or 18; both have `typedef uint32 MultiXactOffset` (`c.h` 17:681 / 18:650). Whether it ships in a released major version was not checked.
- **Fact.** PostgreSQL 17 added sizing GUCs for the MultiXact SLRU caches: `multixact_offset_buffers` (default 16) and `multixact_member_buffers` (default 32), both server-start only. It also added `serializable_buffers` (default 32). Source: https://www.postgresql.org/docs/17/runtime-config-resource.html (same in /docs/18/). Contention is observable as the wait events `MultiXactOffsetSLRU`, `MultiXactMemberSLRU`, `MultiXactOffsetBuffer`, `MultiXactMemberBuffer`, `MultiXactGen`, and `MultixactCreation` (https://www.postgresql.org/docs/18/monitoring-stats.html), and through the `pg_stat_slru` view (§27.2.25).
- **Fact.** Conflict table: FOR SHARE conflicts with UPDATE and NO KEY UPDATE. FOR KEY SHARE conflicts only with UPDATE, meaning key changes or deletes (README.tuplock; explicit-locking §13.3.2). Both are shared modes, and any overlapping pair creates a MultiXact. The KEY SHARE variant is no cheaper in MultiXact terms.
- **Facts (source) plus one inference.** Share-lockers can bypass a waiting exclusive locker:
  - README.tuplock says incoming share-lockers must queue through `LockTuple` "if there is any conflict, to ensure that they don't starve out waiting exclusive-lockers. However, if there is not any active conflict for a tuple, we don't incur any extra overhead" (README.tuplock:34).
  - In `heap_lock_tuple`, a `LockTupleShare` request against a tuple held only by non-exclusive lockers skips sleeping (`heapam.c` 17:5077 / 18:5053–5060).
  - **Inference.** A maintenance `UPDATE upgrade_control` waits for the current share-lockers. New FOR SHARE requests that arrive while it waits can still be granted, so with continuously overlapping transactions the update can wait indefinitely. The benchmark should measure this.
- **Fact.** Row locks cannot be taken in a READ ONLY transaction.
  - "The use of FOR NO KEY UPDATE, FOR UPDATE, FOR SHARE or FOR KEY SHARE requires UPDATE privilege as well" (https://www.postgresql.org/docs/18/sql-select.html).
  - In source, the parser adds `ACL_SELECT_FOR_UPDATE` (defined as `ACL_UPDATE`) to the locked relation's required permissions (`parser/analyze.c` 18:3519, 3608; `nodes/parsenodes.h` 18:94).
  - `ExecCheckXactReadOnly` calls `PreventCommandIfReadOnly` for any non-temp relation that requires more than `ACL_SELECT` (`executor/execMain.c` 17:792 / 18:802–820).

  This is why `beginPostgresOn` opens read-write, locks, then sets READ ONLY.
- **Fact.** Row locks are disallowed on a hot standby: "SELECT … FOR SHARE | UPDATE, because row locks cannot be taken without updating the underlying data files". The SERIALIZABLE isolation level is also not available there. Source: https://www.postgresql.org/docs/18/hot-standby.html §26.4.
- **Fact.** In REPEATABLE READ and SERIALIZABLE, FOR SHARE on a row that a concurrent committed transaction "actually updated or deleted … not just locked" raises `could not serialize access due to concurrent update`. A row that was only locked does not. Source: https://www.postgresql.org/docs/18/transaction-iso.html §13.2.2; explicit-locking §13.3.2.

### 2.2 Hikyo code paths

**A. `GuardPostgres` on every transaction.** `internal/store/admission.go:195–214` (`beginPostgresOn`) calls `d.admission.GuardPostgres` (`internal/store/upgrade/admission.go:169–183`), which runs `snapshotSQL + " FOR SHARE OF c"`. `snapshotSQL` reads `upgrade_control c … WHERE c.singleton=1` (`internal/store/upgrade/storage.go:38`).

- This covers all writes (`writePostgresOnce`), `WriteSerialized`, denial flushes (`flushOnce`), reads (`readOnce`, REPEATABLE READ), `CheckAdmission`, and the audit-export transactions. A read-only transaction is opened read-write, locks the row, then runs `SET TRANSACTION READ ONLY` (`admission.go:200, 210`).
- **Inference, combining §2.1 with the code.** On a shared primary with N concurrent Hikyo transactions across all tenants:
  1. One installation-wide row's `xmax` becomes a MultiXact. It is replaced by a fresh MultiXact on every overlapping transaction start.
  2. MultiXact ID and member consumption scales with total transaction rate × concurrency, regardless of tenant.
  3. Every transaction, including reads, gets an XID, dirties the `upgrade_control` heap page, and writes an `XLOG_HEAP_LOCK` record. Reads therefore consume XID space and produce WAL.
  4. Every read pays a synchronous commit. `RecordTransactionCommit` flushes WAL when `wrote_xlog && markXidCommitted && synchronous_commit > off` (`xact.c` 17:1488 / 18:1502). It waits for synchronous standbys when `wrote_xlog && markXidCommitted` (17:1546–1547 / 18:1566–1567). A transaction that wrote no WAL and assigned no XID would skip both. Because `GuardPostgres` assigns an XID and writes WAL, every `readOnce` and `CheckAdmission` commit waits for fsync, plus the sync-standby round trip where one is configured. These flushes can be group-committed; this is a latency and fsync-load cost, not a global serialization point like lock 84.
  5. All transactions contend on that one buffer's content lock and on the MultiXact SLRUs.
  6. Hikyo's read path cannot be pointed at a hot-standby replica as written, because FOR SHARE is refused there.
  7. Maintenance or upgrade `UPDATE`s of `upgrade_control` can starve under continuous load.

**B. Other FOR SHARE rows.**
- `keyring.sql` `GetActiveTokenKeyForReceipt` (`tier3_keys` with `purpose='token' AND org_id='' AND project_id=''`) is a **second installation-wide** shared row. The same MultiXact growth applies, scaled by how often token receipts are issued.
- These are per-scope and contend only within a tenant scope:
  - `keyring.sql` `AssertActiveTier3Version` (one row per purpose/org/project/version, the writer fence)
  - `transit_repository.sql:8` (`transit_keys` per org/project/env/name)
  - `adapter_repository.sql:98` (`adapter_targets` per target)

**C. FOR UPDATE on singleton rows** (`auth_refusal_window id=1`, `developer_credential_policy id=1`, `credential_policy id=1`, `instance_identity id=1`, `self_config_binding id=1`). These serialize their callers installation-wide. Under SERIALIZABLE, a second updater fails with 40001 once the first commits (§2.1, last fact). Which of these sit on tenant-driven hot paths was not checked.

---

## 3. Commit-time lock hold: audit locks 84 and 85

### 3.1 Facts

- **Fact (source).** `CommitTransaction` runs in this order:
  1. Deferred triggers (`AfterTriggerFireDeferred`, `xact.c` 17:2229 / 18:2278)
  2. `PreCommit_CheckForSerializationFailure` (17:2306 / 18:2355)
  3. `RecordTransactionCommit` (17:2332 / 18:2381)
  4. `ProcArrayEndTransaction` (17:2356 / 18:2405)
  5. `ResourceOwnerRelease(…RESOURCE_RELEASE_LOCKS…)` (17:2399 / 18:2453), which calls `ProcReleaseLocks` and so `LockReleaseAll(USER_LOCKMETHOD, false)`, releasing transaction-level advisory locks (`proc.c` 18:894–903)

  Inside `RecordTransactionCommit`, a synchronous commit calls `XLogFlush(XactLastRecEnd)` (17:1492 / 18:1506) and then `SyncRepWaitForLSN` (17:1547 / 18:1567). The comment there reads: "at this stage we have marked clog, but still show as running in the procarray and continue to hold locks" (17:1544 / 18:1564).
- **Fact.** Sequence values are never rolled back, so "PostgreSQL sequence objects cannot be used to obtain 'gapless' sequences". Source: https://www.postgresql.org/docs/18/functions-sequence.html.
- **Fact.** HOT applies only if "the update does not modify any columns referenced by the table's indexes, not including summarizing indexes" (BRIN). Otherwise new index entries are needed. Source: https://www.postgresql.org/docs/18/storage-hot.html (and /docs/17/storage-hot.html).

### 3.2 Hikyo code paths

**`00011_audit_export_order.sql`** (unchanged by later migrations on `origin/dev`):
- Lock 85 is taken **shared** by a BEFORE INSERT row trigger on every audit row.
- Lock 84 is taken **exclusive** by the DEFERRABLE INITIALLY DEFERRED constraint trigger on every audit row. That trigger then runs `UPDATE … SET commit_seq = nextval(…)`.
- `audit_tenant_events` and `audit_instance_events` share the one key 84.

Inferences from §3.1:
- **Lock 84 is held from the deferred trigger through the WAL flush and any synchronous-replica wait, until lock release.** Transactions that wrote any audit row, for any tenant on either trail, therefore commit one at a time. Each waits for the previous one's full commit latency (fsync, plus the sync-standby round trip where configured). Group commit cannot batch them, because the next transaction cannot reach its own commit record until the previous one releases 84. The ceiling on audit-writing commits per second is about 1 / (commit latency), and it applies to the whole installation, not per tenant. The benchmark task should measure this.
- **The design claim holds.** 00011 says visible `commit_seq` order equals commit order. This is consistent with the source order: `ProcArrayEndTransaction`, which makes the transaction visible, runs before the locks are released.
- **Serialization failures happen after `commit_seq` is assigned.** For SERIALIZABLE transactions, `PreCommit_CheckForSerializationFailure` runs *after* deferred triggers. A transaction can take 84, consume `nextval`, and then abort with 40001. That produces a gap, which 00011 already accepts, and holds 84 during the abort.
- **Double writes per audit row.** `commit_seq` appears in four indexes: `UNIQUE(commit_seq)` and the three `(org_id, …, commit_seq)` indexes. The deferred `UPDATE` can never be HOT, so each audit row writes two heap tuples and inserts index entries in every index on the table twice.
- **Lock 85 queueing.** `AuditExportSnapshotTime` and `AwaitAuditExportWriters` (`internal/store/store.go:1128–1192`) take 85 **exclusive** in a READ COMMITTED transaction. Under the waitMask rule (§1.1), once an export is waiting for 85, *every new* audit writer's first shared request queues behind it. So an export stalls all audit-writing transactions installation-wide until every earlier writer commits and the export's short transaction ends. A writer that already holds 85 shared and inserts another row does not queue, because a re-request of a held lock skips the waitMask check (`lock.c` 17:877 / 18:934).

**Coordination classes 87 and 88** (`coordination_runtime.sql:60, 166`; `self_config.sql:74`). These are exclusive transaction-level locks. Class 88 encloses MCP admission for the whole installation, as its `hikyo:reason` annotation states. The same commit-latency hold from §3.1 applies to any transaction that takes them.

---

## 4. SERIALIZABLE (SSI): predicate-lock granularity and false conflicts

### 4.1 Facts

- **Fact.** Predicate (SIRead) locks never block. They are "based on data actually accessed". "multiple finer-grained locks (e.g., tuple locks) may be combined into fewer coarser-grained locks (e.g., page locks)". SIRead locks "often need to be kept past transaction commit, until overlapping read write transactions complete". Source: https://www.postgresql.org/docs/18/transaction-iso.html §13.2.3.
- **Fact.** Promotion is controlled by three settings (https://www.postgresql.org/docs/18/runtime-config-locks.html):
  - `max_pred_locks_per_transaction` (default 64) sizes the shared table.
  - `max_pred_locks_per_relation` (default −2, meaning `max_pred_locks_per_transaction`/2, so 32) promotes page and tuple locks in one relation to a relation lock.
  - `max_pred_locks_per_page` (default 2) promotes tuple locks on one page to a page lock.

  "When the system is forced to combine multiple page-level predicate locks into a single relation-level predicate lock … an increase in the rate of serialization failures may occur." "A sequential scan will always necessitate a relation-level predicate lock." Source: transaction-iso §13.2.3.
- **Fact (README-SSI, identical in 17 and 18):**
  - "B-tree index searches acquire predicate locks only on the index *leaf* pages needed to lock the appropriate index range" (README-SSI:371).
  - For an index AM without predicate-lock support, the whole index is locked for any search (README-SSI:368).
  - "Inserting a new tuple creates a rw-conflict with any transaction holding a SIREAD lock on the entire relation" only. Heap page locks do not lock gaps (README-SSI:319).
  - Btree inserts call `CheckForSerializableConflictIn` on the target leaf page (`nbtinsert.c:251`).
  - A search of an empty btree takes a relation-level lock (`nbtsearch.c` 18:1529–1540).
  - `ampredlocks` is true for btree, hash, GiST and GIN, and false for SP-GiST and BRIN (`nbtree.c` 17:118 / 18:141; `hash.c`; `gist.c`; `ginutil.c`; `spgutils.c` 17:61 / 18:64; `brin.c` 17:264 / 18:270).
  - The optimization of skipping the index predicate lock for an equality search on a full UNIQUE key is listed under "R&D Issues", i.e. not implemented (README-SSI:613–618).
- **Inference.** False conflicts come from leaf-page sharing. A serializable transaction that range-scans or `MAX()`-scans the tail of a monotonic index, such as `seq` or `commit_seq`, predicate-locks the rightmost leaf page. Every concurrent insert into that page, for any tenant, is then a rw-conflict. In composite `(org_id, …)` indexes, an org's tail page is shared only with keys of neighbouring orgs on the same page.
- **Fact.** SSI does not cover "system tables, temporary tables, sequences, hint bit rewrites" (README-SSI:204). `sequence.c` contains no predicate-lock calls in 17 or 18 (grep for `Predicate|Serializable`: 0 matches). `nextval` never creates an SSI conflict.
- **Fact.** "Any transaction which is run at a transaction isolation level other than SERIALIZABLE will not be affected by SSI. If you want to enforce business rules through SSI, all transactions should be run at that [level]" (README-SSI:209). The user docs say the same: consistency is automatic only "If the Serializable transaction isolation level is used for all writes and for all reads which need a consistent view of the data" (https://www.postgresql.org/docs/18/applevel-consistency.html §13.4.1). SSI protection "does not yet extend to hot standby mode … or logical replicas" (same page).
- **Fact.** A concurrent update of the same row aborts the second transaction with 40001 instead of waiting it through. In REPEATABLE READ, and therefore SERIALIZABLE, if "the first updater commits (and actually updated or deleted the row, not just locked it) then the repeatable read transaction will be rolled back" with `could not serialize access due to concurrent update` (transaction-iso §13.2.2).
- **Fact.** UNIQUE behaves differently under SSI depending on whether the key was read first.
  - Commit fcff8a5 (Kevin Grittner, author Thomas Munro, 2016-04-07, first released in 9.6) "adds a check for serialization failure ahead of duplicate key checking". A transaction that explicitly checked for the pre-existing value gets a serialization failure when the cause is a concurrent serializable transaction, "otherwise they will get a duplicate key error" (https://git.postgresql.org/gitweb/?p=postgresql.git;a=commit;h=fcff8a575198478023ada8a48e13b50f70054766).
  - The check is still present at `nbtinsert.c:639` (17 and 18).
  - The docs add that unique violations "might be reported even in cases where they could not occur in a serial execution" if transactions insert without first reading (transaction-iso §13.2.3).
- **Fact.** `SERIALIZABLE READ ONLY DEFERRABLE` "may block when first acquiring its snapshot, after which it is able to run without the normal overhead of a SERIALIZABLE transaction and without any risk of contributing to or being canceled by a serialization failure". `DEFERRABLE` has no effect unless the transaction is also SERIALIZABLE and READ ONLY. Sources: https://www.postgresql.org/docs/18/sql-set-transaction.html; transaction-iso §13.2.3. The isolation level "cannot be changed after the first query". Access mode can be set later, as Hikyo does.

### 4.2 Hikyo code paths

- **`writePostgresOnce` / `flushOnce` / `WriteSerialized`, all SERIALIZABLE** (`tx.go:199, 345`; `admission_serialized.go:52`). Every write reads `upgrade_control` under FOR SHARE first. That gives one SIRead tuple lock on a row that is written only by maintenance, so it conflicts only during upgrades. False conflicts across tenants arise where a serializable write reads a shared index range: tail scans, `MAX(seq)`-style queries (`audit.sql` `MaxTenantAudit*`, `MaxInstanceAudit`), sequential scans, or relations with more than 32 locked pages per transaction. Which of these queries run inside serializable writes was **not checked**.
- **Audit inserts.** `commit_seq` and `seq` are monotonic, so every audit insert from every tenant targets the rightmost leaf of `UNIQUE(commit_seq)`, of the `seq` PK, and of each org's tail page in the composite indexes. Any serializable transaction that has predicate-locked those pages gets a rw-conflict. `nextval` itself never conflicts (§4.1).
- **`readOnce` at REPEATABLE READ** (`tx.go:242–270`). It takes no SIRead locks and is not protected by SSI. The docs require "all reads which need a consistent view" to be SERIALIZABLE for SSI guarantees to cover them. The code comment explains that RR was chosen for a single-snapshot proof, not for SSI. `SERIALIZABLE READ ONLY DEFERRABLE` is the documented SSI-safe read mode. It can block at snapshot acquisition, cannot run on a hot standby, and still conflicts with the FOR SHARE in `GuardPostgres`, which requires read-write at that point.
- **`retryable()` comment** (`tx.go:380–386`: "postgres answers both 23505"). This holds when the racing SERIALIZABLE transactions insert without first reading the key. If both racing transactions first SELECT the identity key under SERIALIZABLE and see it absent, PostgreSQL ≥ 9.6 returns 40001 to the loser, and 23505 only for a duplicate that already existed (fcff8a5). Whether the SCIM path reads first was **not checked**.

---

## 5. Per-tenant ordering on append-only tables: facts per option

Context: today, `seq` is a global identity/sequence and `commit_seq` is a global sequence assigned under global lock 84. Gaps are accepted (00011 header).

| Option | Relevant facts | Source |
|---|---|---|
| **Per-org counter row** (`UPDATE org_counters SET n=n+1 WHERE org_id=$1 RETURNING n`) | Concurrent writers for the same org: under SERIALIZABLE the second fails with 40001 once the first commits, rather than waiting through. Under READ COMMITTED it would wait and proceed. Every increment creates a dead tuple (one row churned per event) and a row lock held to commit, including the commit flush (§3.1). The result is gapless if the row update rolls back with the transaction. | transaction-iso §13.2.2; explicit-locking §13.3.2; xact.c ordering |
| **Per-org advisory xact lock + existing global sequence** (`pg_advisory_xact_lock(ns, hash(org_id))` in the deferred trigger, then `nextval`) | Transaction-level, so pooler-safe. Held through commit flush, which keeps per-org `commit_seq` order equal to per-org commit order, the same argument as 00011. Different orgs proceed in parallel except on hash collision. Values are unique and monotonic per org with gaps; sequences are gapful anyway. Uses one shared-lock-table entry per active org per transaction (`max_locks_per_transaction` pool). The int4-pair key space is separate from the bigint space. A transaction carrying events for two orgs must lock in a fixed order to avoid deadlock; deadlocks surface as 40P01. Cross-org global commit order is lost (no single serialization point). | explicit-locking §13.3.5; functions-admin §9.28.10; functions-sequence; xact.c |
| **Per-org sequence objects** | `nextval` is non-transactional and outside SSI. Values are gapful. One sequence relation per org (catalog growth; DDL to create). On its own it does not give commit order; it still needs a lock held to commit for that. | functions-sequence; README-SSI:204 |
| **Partition by org** (LIST or HASH on `org_id`) | "the constraint's columns must include all of the partition key columns", so `UNIQUE(commit_seq)` would become `UNIQUE(org_id, commit_seq)`. Indexes are per partition, so btree leaf pages and SSI page locks are no longer shared across partitions. The planner handles "up to a few thousand partitions fairly well" when pruning applies, and memory grows when sessions touch many partitions. The docs suggest HASH with a fixed count when tenant counts may grow. Sequences remain global unless created per partition. Detach and attach lock levels apply on lifecycle operations. | https://www.postgresql.org/docs/18/ddl-partitioning.html §5.12.2 (Limitations subsection), §5.12.6 Best Practices |

None of these options removes the `GuardPostgres` singleton FOR SHARE (§2) or the session-lock pooler problem (§1). Those are independent.

---

## 6. Unverified and open items, for the benchmark task

1. An isolation test confirming the §1.3A inference: a waiter on `pg_advisory_xact_lock` as the first SERIALIZABLE statement resumes with a snapshot taken before the lock was granted.
2. The measured ceiling on audit-writing commits per second under lock 84, with `synchronous_commit=on` and with a sync standby (§3.2).
3. MultiXact ID and member consumption rates, `pg_stat_slru` hit/read counts, and wait events on `upgrade_control` at realistic concurrency (§2.2A).
4. Whether a maintenance `UPDATE upgrade_control` can starve under continuous FOR SHARE traffic (§2.1, README.tuplock).
5. pgcat prepared-statement behaviour in transaction mode (its README and CONFIG conflict). Supavisor's own docs on session advisory locks (only Supabase's hosted docs state it).
6. Which serializable write paths read shared index tails or sequentially scan shared tables (§4.2).
7. Whether the SCIM create reads the identity key before inserting (§4.2, `retryable()` comment).
8. Other transaction-mode breakers. A grep of `origin/dev` non-test `*.go` and `*.sql` for `LISTEN`, `pg_notify`, `WaitForNotification`, `SendBatch`, `CREATE TEMP`, `WITH HOLD` and `SET SESSION` found no uses. Whether pgx pipelines any other request path was not checked.
9. Hosted providers often restrict server-start GUCs (`multixact_*_buffers`, `max_pred_locks_*`). Per-provider limits were not researched.

## Sources

PostgreSQL docs (17 and 18):
- https://www.postgresql.org/docs/18/explicit-locking.html, https://www.postgresql.org/docs/17/explicit-locking.html
- https://www.postgresql.org/docs/18/functions-admin.html (§9.28.10 advisory lock functions)
- https://www.postgresql.org/docs/18/transaction-iso.html, https://www.postgresql.org/docs/17/transaction-iso.html
- https://www.postgresql.org/docs/18/applevel-consistency.html
- https://www.postgresql.org/docs/18/sql-set-transaction.html
- https://www.postgresql.org/docs/18/runtime-config-locks.html
- https://www.postgresql.org/docs/17/runtime-config-resource.html, https://www.postgresql.org/docs/18/runtime-config-resource.html
- https://www.postgresql.org/docs/18/routine-vacuuming.html, https://www.postgresql.org/docs/17/routine-vacuuming.html
- https://www.postgresql.org/docs/18/hot-standby.html
- https://www.postgresql.org/docs/18/monitoring-stats.html
- https://www.postgresql.org/docs/18/storage-hot.html, https://www.postgresql.org/docs/17/storage-hot.html
- https://www.postgresql.org/docs/18/functions-sequence.html
- https://www.postgresql.org/docs/18/ddl-partitioning.html

PostgreSQL source (`REL_17_STABLE`, `REL_18_STABLE`, fetched 2026-11-12):
- `src/backend/access/transam/xact.c`, `src/backend/access/transam/multixact.c`, `src/include/access/multixact.h`
- `src/backend/access/heap/heapam.c`, `src/backend/access/heap/README.tuplock`
- `src/backend/storage/lmgr/lock.c`, `src/backend/storage/lmgr/proc.c`, `src/backend/storage/lmgr/README-SSI`
- `src/backend/access/nbtree/nbtinsert.c`, `src/backend/access/nbtree/nbtsearch.c`, `src/backend/access/nbtree/nbtree.c`, `src/backend/access/{hash/hash.c,gist/gist.c,gin/ginutil.c,spgist/spgutils.c,brin/brin.c}`
- `src/backend/commands/sequence.c`
- `src/backend/tcop/postgres.c`, `src/backend/utils/time/snapmgr.c`, `src/backend/parser/analyze.c` (18), `src/backend/executor/execMain.c`, `src/include/nodes/parsenodes.h` (18), `src/include/c.h`
- https://www.postgresql.org/docs/18/sql-select.html (privileges for locking clauses)
- Commit fcff8a5: https://git.postgresql.org/gitweb/?p=postgresql.git;a=commit;h=fcff8a575198478023ada8a48e13b50f70054766
- Commit bd8d9c9bdf (master, "Widen MultiXactOffset to 64 bits"): https://github.com/postgres/postgres/commit/bd8d9c9bdf

Poolers and drivers:
- https://www.pgbouncer.org/features.html, https://www.pgbouncer.org/config.html, https://www.pgbouncer.org/changelog.html
- https://github.com/postgresml/pgcat/blob/main/README.md, https://github.com/postgresml/pgcat/blob/main/CONFIG.md
- https://github.com/supabase/supavisor/tree/main/docs (pool_modes.md, faq.md, migrating/pgbouncer.md)
- https://supabase.com/docs/guides/database/connecting-to-postgres
- `github.com/jackc/pgx/v5@v5.12.0/conn.go`

Hikyo (`origin/dev` 4ea1ff4a2):
- `internal/store/tx/tx.go`
- `internal/store/admission_serialized.go`, `internal/store/admission.go`
- `internal/store/upgrade/admission.go`, `internal/store/upgrade/storage.go`, `internal/store/upgrade/lock.go`
- `internal/store/store.go`
- `internal/store/migrations/postgres/00011_audit_export_order.sql`
- `internal/store/queries/postgres/{recovery_runtime,coordination_runtime,self_config,keyring,transit_repository,adapter_repository,audit}.sql`
