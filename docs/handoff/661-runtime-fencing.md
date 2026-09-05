# Runtime fencing handoff: issue 661

Worktree: `/tmp/hikyo-runtime-fence`. Base: governance commit373910cca63eb8a2e2e6b5a079cc285b64a2ae95 plus uncommitted F1/F2/F4/F5 dependency slices. No commit or push performed. Parent owns delivery and final integration.

## Implemented boundaries

- `store.Open(ctx,cfg,upgrade.Admission)` has a mandatory third argument. Zero admission refuses before pool creation. `Session.Admit` requires actual verified release node, exact source/schema/migration inventory, healthy non-maintenance state and owned exclusion. The gate still owns build identity and pinned trust routing.
- Every normal SQLite transaction holds a shared host lock before BEGIN through settlement, including WAL readers. Every PostgreSQL transaction takes `FOR SHARE OF c` on the control row before domain queries; read paths then become SQL read-only. No KEY SHARE or separate guardian connection. Runtime authority never refreshes itself after generation, incarnation, source, epoch or trust-domain changes.
- `tx.Read`, Write/WriteResult/serialized writes and denial flushes use opaque guarded transactions. Panic paths roll back and release ownership. Adapter and dynamic runtime helpers use the same boundaries. Coordination methods receive only a privately held already-admitted transaction; PostgreSQL coordination retains READ COMMITTED semantics after the existing advisory lock.
- Maintenance preparation invalidates durable singleton lease tokens and expiry in the same pending/control transaction. Overflow aborts. Fresh bootstrap has no domain tables yet and omits this domain update only for explicit fresh provenance.
- Runtime backup export retains SQLite shared exclusion through VACUUM/framing. PostgreSQL uses one SERIALIZABLE transaction: control row share lock, SET TRANSACTION READ ONLY, exact version/table/sequence/COPY snapshot. The old DEFERRABLE claim is removed because this transaction starts read-write to establish the row fence.
- PreparationDB is inspection/export only and expires with Session. RecoveryDB is a different type from runtime DB. Authenticated scratch recovery requires full opaque archive proof plus independently inspected restored source and rotated epoch/incarnation. Ordinary data recovery uses an explicit closed private kind, actual signed source Plan and restored state, making no archive-attestation claim. Every recovery commit checks retained operator ownership. F4 owns narrow service/keyring adapters and separate PostgreSQL restore destination types.

## Writer inventory

| Family | Concrete boundary |
| --- | --- |
| Tenant/authentication/keyring/retention/audit/re-encryption services | internal/store/tx/tx.go guarded Read/Write/WriteResult/denial flush |
| Named serialized authorization writes | admission_serialized.go, session advisory lock before admitted serializable transaction |
| HA registration, counters, singleton leases, MCP inflight/rate, dynamic failure coordination | coordination_transactions.go, no direct pool-backed autocommit SQL |
| Adapter outbox/claim/journal/activation and reads | adapter_runtime.go plus runtime_read.go |
| Dynamic claim/intents/outcomes/retry/material/gauges | dynamic_runtime.go plus runtime_read.go |
| Readiness | DB.CheckAdmission, no goose table creation or migration |
| Backup consistency/audit barrier | backup.go and store.go admitted transactions; actual COPY/VACUUM retained ownership |
| Upgrade/bootstrap/migration/candidate health | leaf upgrade Session, independently typed/verified gate contracts |
| Preparation and restore/drill | distinct PreparationDB, RecoveryDB and destination capabilities; never ordinary runtime constructor |
| Release schema generation | BuildScratchSchema, source-file-restricted generator only |

## Static guard

`internal/lint/handle_positions.go` enumerates exact native-handle files and exact fixture paths. No package-wide store/tx/test exemption remains for native access. Opaque transaction types and Begin methods also count as bypass-capable handles outside named boundaries. Coordination's exception permits only guarded wrappers. Private openConfigured calls are independently confined to explicit constructors and the existing pool-validation unit fixture. New-file negative tests cover a hypothetical writer inside the otherwise trusted store package.

## Validation so far

- Both-engine admission/maintenance/drain/old-generation/singleton-token race:3 cases passed9.158s, zero skips.
- Root store plus tx normal suites:143 cases passed, both engines, zero skips before adding direct-family probes. Later probes independently passed SQLite; full both-engine replay running.
- Real separate-process SQLite WAL reader exclusion, hardlink/replaced-inode refusal and transaction-panic release: race passed12.203s.
- `go vet ./internal/store/...` passed after mandatory Open integration.
- Historical F2 archive fixtures now use genuine signed development admission and retained public bundle. Historical migration43-to44 archive compatibility uses actual signed legacy preparation, age encryption and receipt authentication.
- Full store/... race, final lint/boundary and final combined app/isolation checks remain pending. Evidence runner outputs live in `/tmp/hikyo-runtime-fence-evidence`.

## Decisions and discovered fixture defects

1. Optional runtime admission was temporary compilation scaffolding only and has been removed. A zero-value runtime claim is an explicit negative test, never a fallback.
2. Preserve legacy manual backup/restore with separate operator capabilities, not runtime permission. New serving after supported restore still requires current-incarnation evidence at the gate.
3. Existing MCP capacity test assumed20 database transactions finished before one refill interval. Under concurrent build load that assumption failed. It now seeds an explicitly exhausted bucket for the overflow check and separately verifies exact unchanged bucket timestamp after a rejected concurrency claim. Production rate behavior is unchanged.
4. Keep genuine initialized keys in normal fixtures. The repository-only key-rotation invariant tests explicitly replace their own key rows after admission because those cases intentionally construct incomplete/corrupt hierarchies; hierarchy generation row remains.

Review and final exact-head validation are still required before delivery.
