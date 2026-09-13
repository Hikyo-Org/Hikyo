# Gemini audit validation

Validated the seven supplied findings against this checkout on 2026-09-13.
Two warranted changes: scoped audit indexes and browser URL hardening.
The other five describe documented trust boundaries, capacity limits or engine
behavior rather than demonstrated defects.

| Finding | Verdict | Evidence and disposition |
| --- | --- | --- |
| 1. Kubeconfig exec plugins | Expected, explicitly trusted functionality | [Import ADR](../adr/import-paths.md#trust-ambient-credentials-read-only-never-persisted) explicitly includes ambient kubeconfig exec plugins as third-party code. [Importer](../../internal/importer/k8s.go) validates the original policy before substituting its bounded runner; deny-all and allowlists are preserved. The later allow-all applies to Hikyo's wrapper. Default ambient kubeconfig can execute commands, so it must be trusted like kubectl configuration. This is not a server-side kubeconfig upload/execution path. Existing policy and bounded subprocess tests retained. |
| 2. Missing scoped audit indexes | Confirmed performance gap | PostgreSQL had org/seq and scoped **commit_seq** export indexes, but interactive pages and ceilings use **seq**. SQLite had only org/seq. Migration 53 adds `(org_id, project_id, seq)` and `(org_id, project_id, env_id, seq)` on both engines. A full-org sequential scan is not inevitable: the old planner could use other indexes, filter or sort. The confirmed issue is the missing matching access path, not a measured universal scan behavior. |
| 3. Argon2id burst refusals | Intentional admission control | [Limiter](../../internal/admission/admission.go) implements the operations budget and uniform overload refusal. Concurrency is derived from configured memory and capped at eight, not fixed at eight. The default 272 MiB budget minus 16 MiB headroom permits four 64 MiB verifications. Sixteen waiters are deliberate; removing these bounds would weaken pre-auth resource protection. |
| 4. Plaintext Go strings | Documented residual | [Encryption ADR](../adr/encryption-model.md#key-material-in-memory) explicitly makes no memory-secrecy claim or guaranteed-zeroization promise. Replacing transport strings with byte slices would not eliminate JSON/runtime copies. No new disclosure path was established. |
| 5. SQLite single writer | Intentional engine configuration | [Store](../../internal/store/store.go) uses one writer plus four WAL readers. SQLite serializes writers at engine level; increasing writer connections does not provide concurrent write throughput. PostgreSQL is the existing alternative for that workload. |
| 6. Whole-environment value loading | Bounded delivery contract | [Query](../../internal/store/queries/postgres/values.sql) intentionally returns a complete environment set. [Schema limits](../../internal/schema/schema.go) cap projects at 1,000 keys and individual values at 65,536 bytes; [key creation](../../internal/service/keys.go), [definitions apply](../../internal/service/definitions_apply.go), and [value writes](../../internal/service/values.go) enforce these. The raw value ceiling is 62.5 MiB before overhead, not an unbounded set or a measured heap ceiling. Adding LIMIT without a complete pagination contract could silently omit delivered secrets. No capacity failure was demonstrated. |
| 7. Browser URL schemes | Confirmed defense-in-depth gap | [OpenBrowser](../../internal/cli/cli_reauth.go) passed arbitrary strings to OS handlers. Current reauth constructs URLs from an HTTP(S) trusted origin, so this is not evidence of an existing remote exploit. Validation now rejects non-HTTP(S), relative, hostless, malformed and credential-bearing targets before command creation; errors omit URL contents. Tests cover all three platform command shapes and preserve complete legitimate handoff URLs. |

## Changes and operational impact

Migration 53 only adds indexes. Historical migration bytes and release trust pins
remain intact. The development compatibility declaration is regenerated from
fresh SQLite and PostgreSQL catalogs, adding migration 53 and new target schema
digests while preserving source edges. Index creation runs through the existing
maintenance migration path; it consumes disk and blocks writes during creation,
and the two indexes add ongoing audit-write overhead. No query/API pagination
or authorization semantics change.

[Migration regression](../../internal/store/migrate/audit_scope_indexes_test.go)
upgrades a schema-52 database containing 10,000 audit events, verifies all rows
survive, and checks that project/environment pages and ceilings use the matching
ordered index without a separate sort on SQLite and PostgreSQL.
[Browser regressions](../../internal/cli/browser_test.go) construct commands
without launching a browser.

## Validation boundaries

The source report points to another worktree. Its HTTP server, PID, LAN reachability
and numerical security/performance scores were not verified by this code review.
No listener was started or changed. These seven findings cannot establish
"zero critical/high flaws" across the production core. XChaCha20-Poly1305 uses a
192-bit nonce and a 256-bit key; the nonce size is not its encryption strength.

Cross-provider review: skipped because the user confirmed Claude is out of tokens.
This is not a CLEAN cross-provider verdict. Commit, push and PR creation were
subsequently authorized; merge and deployment are outside this delivery request.

Validation passed: full CLI, store (including migrations and upgrade), importer,
admission, release identity, build compatibility, upgrade gate and development
upgrade package tests with the scratch PostgreSQL DSN; audit integration tests (`TestAuditCore`, PostgreSQL export
ordering/cutoff tests, audit invariants and export pagination); focused browser
and both-engine index tests under the race detector; `go vet` for CLI, store,
importer, admission and build compatibility; SQLC regeneration (no generated
query changes); fresh-catalog development declaration check; `git diff --check`.

The local shell initially selected PostgreSQL 14 and the scratch cluster inherited
the host timezone. Historical catalog checks refused that setup. PostgreSQL 18
with UTC matches CI and passes the historical migration check. No pinned genesis
or production schema-inspection logic was changed to make the tests pass.
