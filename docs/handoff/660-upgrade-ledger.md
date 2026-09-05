# Issue 660: atomic upgrade ledger foundation

Base: `373910cca63eb8a2e2e6b5a079cc285b64a2ae95` (merged governance #645). Worktree: `/tmp/hikyo-upgrade-ledger`, branch `feat/660-upgrade-ledger`. No commit or publication by the implementation worker.

## Delivered boundary

`internal/store/upgrade` owns instance-level control storage and migration exclusion. It does **not** authenticate release claims, attest a backup, drain existing processes, or grant runtime admission. F1 verifies release/route trust, F4 verifies operator evidence, F3 fences runtime transactions and F5 invokes the mandatory gate. Existing pre-gate binaries cannot enforce these records.

`WithLock` reserves one physical PostgreSQL connection with the real existing goose session advisory lock, or holds the canonical SQLite migration file lock. SQLite rejects hard links and changed file identity. No raw connection is exposed. The existing `migrate.Run` production callers are untouched here; replacing their authority belongs to F5.

## Public seams

- `Inspect` and `InspectInstalled` are read-only, including missing SQLite files. Fresh and exact `legacy/v1` (governance commit, schema 44, 43 migration files) are explicit source identities. No released version is fabricated. `InspectSQLiteSource` / `InspectPostgresSource` inspect the caller-owned snapshot.
- `DomainCatalogSQLite`, `DomainCatalogPostgres` and `Session.DomainCatalog` are the shared release-build, backup and boot fingerprint algorithm. They omit only the exact independently validated control/pending/nonce schema. Extra or changed gate-owned objects refuse. `PinnedLegacySchemaDigest` and `PinnedLegacyManifest` expose the owned approved declaration to release tooling without implying live-state authority. `Catalog.Digest()` covers versioned canonical JSON plus the complete ordered applied goose set.
- `Bootstrap(ctx, manifest, operation, domain)` atomically creates all control tables, complete pending state, trust floors and nonce consumption after source inspection. Fresh genesis mints a CSPRNG incarnation; legacy requires an explicit nonzero operator-generated proposal, independently verified by F4/F5. The proposal is not represented as already-live state.
- `Prepare`, `PrepareAfterRestore`, `Advance`, `Resume` and `RefreshTrust` compare complete persisted expected state under a transaction lock. Phases are exactly prepared, schema-write-started, schema-applied, healthy and restore-required. Intermediate healthy hops retain maintenance and generation across the whole pinned route. New routes advance generation. Freshly authenticated trust floors can advance on an exact healthy restart without a new backup or generation.
- `ApplyMigrations` consumes immutable embedded SQL on the same locked physical connection only after durable write-started, complete expected-state equality and target migration-digest comparison. It neither verifies release claims nor advances the phase. PostgreSQL uses the actual reentrant goose locker and a one-use driver lease; reconnect is impossible. `CandidateKeys` exposes only the existing encrypted wrapper inventory at matching schema-applied state, rechecked on every read; no runtime store or key creation is exposed.

## Exact identity and evidence

State stores explicit production/local-development trust domain, pinned release-root digest, authenticated metadata/catalog/highest-release floors, instance identity, Source union, schema/migration digests, credential invalidation epoch, 32-byte recovery incarnation, generation, maintenance and pending operation.

Operation binds route origin/current source/target, source/target schema and migration digests, complete route digest/length/hop, generation/incarnation, backup identity and typed public acceptance claims. F5 must map only actual F1/F4 verified results. F2's structural claim validation is not signature authority. Nonce uniqueness is `(trust domain, instance, incarnation, epoch, nonce)`; consumption and trust floors share pending creation's transaction. Attestation time validity is rechecked against the database clock.

F1 owns the copied `internal/releaseidentity` leaf. Keep it byte-identical when combining branches. No release/attestation signing implementation is added here.

## Restore integration contract

`PreparePostgresRestoreControlSchema(ctx, pgx.Tx, authenticated source manifest, source schema digest)` and the SQLite `*sql.Tx` counterpart validate the complete existing source schema and applied set, then create only exact empty control tables. Existing tables must be exact and all empty. F5 must call this **inside** the import transaction before archive inventory/row loading, not commit a separate preparation step.

After the existing resolver advances one beyond the largest credential stamp and invalidates restored credentials/grants, call `ReconcilePostgresRestore` / `ReconcileSQLiteRestore` inside that same transaction. SQLite must finish this before file sync/publication. The primitive requires the already-advanced credential/restore epoch, generates a fresh CSPRNG incarnation, advances generation, preserves trust domain/root/floors and retains archived pending state as invalidated/restore-required history. Ordinary resume refuses it. Current operator public-key custody remains outside the archive.

Fresh bootstrap's instance ID predates migration 20's generated singleton. `Session.ReconcileFreshInstance` performs explicit first-fresh/schema-applied reconciliation under a transaction, verifies target catalog and canonical auth seed, and refuses populated organizations/principals, legacy sources and later phases. F5 owns the invocation. The stored restore fence initially captures the strongest auth epoch. Actual identity must match; credential-only revocation may advance normally after adoption, while an unacknowledged restore above the stored fence refuses. Operator-key rotation must explicitly advance its proof fence and is separate from ordinary credential revocation.

## Validation and limitations

Real SQLite and local PostgreSQL 18.6 integration cover fresh/legacy genesis, schema/migration/control drift, stale identities, every phase path, route maintenance, nonce/floor refusal, lock contention/cancellation, process restart and abrupt child exits before/after bootstrap commit, real archive restoration twice and failure before SQLite publication. Migration seam tests also exercise committed nontransactional SQL cancellation and PostgreSQL backend termination without reconnect or phase advancement. Candidate wrapper tests cover confined read-only lifecycle.

Evidence is recorded in `docs/reports/1.0/upgrade-ledger/index.html` and `/tmp/hikyo-ledger-evidence/`. This does not claim runtime F3 fencing, F5 application integration, production rollout, external physical-floor acceptance, or a released 1.0 tag.

Control DDL lives outside ordinary goose migrations because its own atomic genesis gate must precede goose. The exact upgrade package is added to the driver/generator boundary trusted set for that storage boundary and the closed candidate wrapper reader. No authn writer exemption or wildcard import exception is added.

Internal upgrade tests construct the documented source from unchanged SQL with test-only `os.DirFS` and maintained goose. Actual archive integration tests use external `upgrade_test`, avoiding the cycle introduced when runtime store/export imports upgrade. Its exact raw-driver test entry permits the existing real restore mutation callbacks; no production permission or authn writer exemption is widened. All archive regressions remain exercised.

`InspectControl` reads exact control schema, state and floors without demanding old applied-domain schema or goose history. It is strictly read-only; missing files/control return ErrAbsent, partial control refuses. `HealthyKeys` supplies the same bounded existing-wrapper inventory as `CandidateKeys`, but only for healthy nonmaintenance restarts; each retained capability remains session/phase bound.

## Explicit same-release recovery integration

`Operation.Kind` is a closed persisted upgrade/recovery field. Typed upgrade constructors normalize an omitted caller claim to upgrade; persisted JSON requires the explicit recognized field. A recovery operation binds exactly the restored source release to itself, with equal source/target schema and migration digests. It has one recovery operation and zero migration edges, so its RouteLength=1 is not a claimed upgrade edge. `PrepareRecovery` consumes NEW backup evidence bound to the restored incarnation and next generation in the same transaction as complete pending replacement. Old backup identities and nonce replays refuse. `ValidateRecoverySchema` verifies actual catalog and instance/epoch under exclusion, then records schema-applied without schema-write-started or goose. Candidate health remains F5's responsibility before Healthy. `ApplyMigrations` explicitly refuses recovery operations.

Parent actual SQLite/PostgreSQL storage race test `TestSameReleaseRecoveryRequiresNewProofAndNeverRunsMigrations` passed6.571s, including precommit rollback, old backup rejection, exact same-release requirement and replay refusal. Independent F3 owner review CLEAN. Existing full ledger normal replay after the kind addition passed21.658s. This does not yet claim full F5 recovery boot acceptance.
