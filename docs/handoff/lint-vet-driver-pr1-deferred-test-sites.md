# Lint vet driver PR 1: deferred in-package test sites

Work: PR 1 of `docs/adr/lint-analysis-driver.md` (branch `dunky13/lint-vettool-driver`).

## Why this exists

The vet driver analyzes the in-package test variant of every package. The old go/packages harness kept one variant per package path (the production one), so it never saw in-package `_test.go` files. One analyzer, `handles`, still reports many sites there. `redactfmt` reported 84 more; those were all false positives of the analyzer rule and are resolved by the rule fix (see "redactfmt: resolved"). Per the owner's scope decision (option A), PR 1 ships `handles` in the vet driver for every context but does not gate on those sites yet.

## Exact gate

`TestVetDriverRepo` ignores a diagnostic only when all of these hold (`strictDeferred` in `internal/lint/analyzer.go`, `deferTestSites` in the same file):

1. The analyzer is `handles` (the only entry in `strictDeferred`).
2. The package is an in-package test variant: its path does not end in `_test` (an external test package stays strict).
3. The diagnostic's file name ends in `_test.go`, read from the file on disk without `//line` adjustment, so a `//line` directive cannot move a site into a test file.

Any production-file finding, any external test package finding, and every other analyzer's finding still fail the test.

## What gates these sites today

The deferred sites are gated by a ratchet, not by the old harness. The old go/packages harness never loads in-package `_test.go` files, so it does not enforce them, and `TestVetDriverParity` proves nothing about them (its leg runs the same analyzer core through the old loader and detects only loader and variant differences).

`TestVetDriverRepo` compares the deferred diagnostics of every context with `internal/lint/testdata/deferred_sites.json`, a checked-in ceiling of analyzer -> file -> count per context, decoded strictly into typed structures. It fails when a file absent from the pin gains a deferred diagnostic, when a file's count exceeds its ceiling, and, with an instruction to lower the pin, when a count drops, so the pin only ratchets down. After fixing sites, lower it with `go test ./internal/lint -run 'TestVetDriverRepo$' -update-deferred-sites` and review the diff. The pin is not a `driverFiles` inventory entry: it grants nothing, it only caps the backlog. No `driverFiles` entries were added.

## Counts

Distinct diagnostics (union over the default, ui, darwin and windows contexts, one per file:line:column):

| Analyzer | Diagnostics | Files |
|---|---|---|
| handles | 1141 | 103 |

redactfmt: 0 deferred (resolved). forgeguard and fence: 0 deferred; their 36 newly visible findings (34 forgeguard, 2 fence) were fixed in this PR (34 forgeguard includes `unattended_operator_test.go`, added by the `dev` integration). Every other analyzer: 0 findings in all four contexts.

## Per-file breakdown: handles

- internal/store/postgres_source_proof_test.go: 54
- internal/store/upgrade/schema_postgres_test.go: 53
- internal/store/admission_settlement_test.go: 50
- internal/service/adapter_authorization_clock_test.go: 44
- internal/store/upgrade/schema_postgres_scope_test.go: 43
- internal/store/postgres_pool_prepared_test.go: 41
- internal/app/backup_recovery_schema65_test.go: 29
- internal/app/backup_recovery_capability_test.go: 26
- internal/isolation/transit_admission_lock_test.go: 25
- internal/store/admission_settlement_driver_test.go: 25
- internal/isolation/scim_page_bound_test.go: 23
- internal/store/upgrade/rollback_postgres_test.go: 21
- internal/app/backup_upgrade_drill_test.go: 20
- internal/isolation/rule_refusal_querywork_test.go: 20
- internal/service/developer_credentials_test.go: 20
- internal/store/secret_occurrence_bench_test.go: 20
- internal/app/runtime_cleanup_settlement_test.go: 18
- internal/service/adapter_retry_security_test.go: 18
- internal/isolation/adapter_authority_timeout_test.go: 17
- internal/isolation/audit_refusal_emitter_test.go: 16
- internal/isolation/auth_refusal_storage_test.go: 15
- internal/isolation/recovery_fixture_test.go: 15
- internal/isolation/secret_occurrence_batch_test.go: 14
- internal/upgradegate/operator_rotation_test.go: 13
- internal/isolation/adapter_configuration_fence_test.go: 12
- internal/isolation/adapter_preparation_timeout_test.go: 12
- internal/isolation/adapter_reviewed_predicate_scope_test.go: 12
- internal/isolation/adapter_runtime_cleanup_scope_test.go: 12
- internal/store/upgrade/backup_fence_test.go: 12
- internal/isolation/audit_retention_e2e_test.go: 11
- internal/isolation/remote_pairing_e2e_test.go: 11
- internal/store/admission_serialized_test.go: 11
- internal/store/migrate/transit_mldsa65_migration_test.go: 11
- scripts/security/alert-route/main_test.go: 11
- internal/isolation/auth_refusal_commit_test.go: 10
- internal/isolation/member_access_rules_e2e_test.go: 10
- internal/isolation/personal_owner_e2e_test.go: 10
- internal/isolation/policy_member_footprint_test.go: 10
- internal/store/adapter_move_error_boundary_test.go: 10
- internal/app/releasecompat_test.go: 9
- internal/app/security_rollback_store_test.go: 9
- internal/service/rotation_preflight_test.go: 9
- internal/store/migrate/social_signin_migration_test.go: 9
- internal/isolation/adapter_core_scope_test.go: 8
- internal/isolation/adapter_generated_read_scope_test.go: 8
- internal/isolation/instance_audit_wire_test.go: 8
- internal/store/backup_diagnostics_test.go: 8
- internal/store/backup_refusal_test.go: 8
- internal/service/schema_fixture_boundary_test.go: 7
- internal/store/migrate/adapter_adoption_indexes_test.go: 7
- internal/store/migrate/aws_adapter_migration_test.go: 7
- internal/app/backup_upgrade_auto_drill_test.go: 6
- internal/isolation/adapter_adoption_clock_test.go: 6
- internal/isolation/adapter_origin_custody_test.go: 6
- internal/isolation/backup_sequence_restore_test.go: 6
- internal/service/adapter_configuration_fence_test.go: 6
- internal/service/adapters_gitlab_test.go: 6
- internal/store/upgrade/candidate_configuration_test.go: 6
- internal/isolation/pki_restore_crl_hold_test.go: 5
- internal/service/self_config_runtime_test.go: 5
- internal/store/migrate/scim_users_account_index_test.go: 5
- internal/app/automatic_apply_test.go: 4
- internal/app/owner_runtime_test.go: 4
- internal/app/unattended_scratch_test.go: 4
- internal/conformance/privacy_content_test.go: 4
- internal/conformance/values_schema_test.go: 4
- internal/isolation/adapter_durable_progress_test.go: 4
- internal/isolation/adapter_runtime_direct_scope_test.go: 4
- internal/isolation/aws_real_durable_test.go: 4
- internal/isolation/aws_real_process_test.go: 4
- internal/isolation/gitlab_e2e_test.go: 4
- internal/isolation/oauth2_e2e_test.go: 4
- internal/isolation/offline_receipt_ha_test.go: 4
- internal/isolation/pending_auth_proof_concurrency_test.go: 4
- internal/isolation/pki_generated_queries_test.go: 4
- internal/isolation/remote_pairing_replay_test.go: 4
- internal/isolation/scim_retirement_generated_scope_test.go: 4
- internal/service/email_invitation_dispatch_test.go: 4
- internal/store/migrate/audit_scope_indexes_test.go: 4
- internal/upgradegate/catalog_rendering_test.go: 4
- internal/app/unattended_integration_test.go: 3
- internal/isolation/access_counts_read_test.go: 3
- internal/store/migrate/adapter_custody_repairs_test.go: 3
- scripts/acceptance/mcp-fixture/audit_seed_test.go: 3
- internal/app/catalogue_limits_test.go: 2
- internal/app/self_config_deployment_test.go: 2
- internal/app/self_config_topology_repair_test.go: 2
- internal/isolation/federated_signup_e2e_test.go: 2
- internal/isolation/registration_e2e_test.go: 2
- internal/service/adapters_vaultkv_test.go: 2
- internal/service/ux_benchmark_test.go: 2
- internal/store/migrate/adapter_custody_move_repair_test.go: 2
- internal/store/upgrade/session_timeouts_test.go: 2
- internal/isolation/auth_refusal_acceptance_test.go: 8
- internal/isolation/containment_historical_test.go: 14
- internal/isolation/rotation_executor_real_pg_test.go: 14
- internal/store/audit_commit_seq_ssi_test.go: 12
- internal/isolation/rotation_cascade_e2e_test.go: 8
- internal/isolation/rotation_summary_review_test.go: 8
- internal/isolation/rotation_summary_test.go: 4
- internal/service/incident_census_test.go: 4
- internal/isolation/containment_e2e_test.go: 2
- internal/service/incident_unit_test.go: 2

## Classification sample

Sampled 10 sites per analyzer (seeded random draw), read at the flagged line. The classification is from reading the line and its immediate context, not from running the code.

### handles (10 of 784 lines, sampled before the `dev` integration)

All 10 are legitimate test-harness use under the current rule: tests open an engine handle to seed rows, hold a lock, or assert on raw state. No production exposure was observed. None is a violation a production path could reach. The follow-up decision is policy, not defect: inventory the harness files, or give tests a reviewed helper.

| Site | Use | Class |
|---|---|---|
| internal/store/admission_settlement_driver_test.go:70 | builds `SQLiteTransaction` around a raw `sql.Tx` | harness (in-package white-box) |
| internal/isolation/backup_sequence_restore_test.go:57 | `pgx.Connect` to the restore target | harness |
| internal/store/postgres_pool_prepared_test.go:198 | `Ping` on a candidate pool | harness |
| internal/app/runtime_cleanup_settlement_test.go:25 | `SQLiteWrite().Conn` to hold the writer | harness |
| internal/isolation/adapter_adoption_clock_test.go:103 | `db.PG().Begin` to hold a transaction open | harness |
| internal/isolation/adapter_durable_progress_test.go:283 | `pggen.New(db.PG())` to read generated queries | harness |
| internal/store/migrate/aws_adapter_migration_test.go:28 | migration fixture `*sql.DB` | harness |
| internal/app/security_rollback_store_test.go:184 | raw `ExecContext` helper | harness |
| internal/upgradegate/operator_rotation_test.go:237 | `BeginTx` to simulate a concurrent operator | harness |
| internal/isolation/scim_page_bound_test.go:165 | `PG().Acquire` to pin a connection | harness |

### redactfmt: resolved

All 84 pinned redactfmt sites, in 25 files in `internal/isolation` and `internal/app`, were false positives. `subexpressionTypes` (`internal/lint/redaction.go`) walked every sub-expression of a format argument, so in `fmt.Sprintf("%d", h.seq)` it also walked the receiver `h` and found a `crypto.Keyring` field in its struct, though only the int is formatted. The rule now walks a selection's receiver only when the receiver, or an embedded field on the selection path, is itself a pinned sensitive type; the selected value's own type is still fully walked. Field walks of `audit.Event`, promoted `Keyring` methods and `h.inner` holding a Keyring stay flagged. Fixture cases in `internal/lint/testdata/badredact/badredact.go` pin both sides (clean: the holder's int field and a holder method; flagged: the Keyring field, `any(h.keyring)`, `h.keyring.String()`, an audit event field walk, an embedded promoted method).

Measured by the reviewer: the unpatched tool over `./internal/isolation ./internal/app` reported exactly the 84 pinned findings; with the fix, 0. The two `json.Marshal(d.enrollment)` sites formerly marked unverified are also no longer reported. `redactfmt` was removed from `strictDeferred` and its entries from `testdata/deferred_sites.json` (`-update-deferred-sites`). The rule is shared with the old harness adapter; its tests pass.

## Entries added when the branch was integrated with `dev`

`dev` added test files and extended one, all in-package `_test.go` files that the old harness never loaded. The ceilings were raised by hand (the regenerator refuses raises by design). None is a production finding: every file below is a test, and `TestVetDriverRepo` still fails any production file or external test package finding.

| File | Analyzer | Count | Why it is a legitimate deferred test site |
|---|---|---|---|
| internal/app/backup_recovery_schema65_test.go (27 to 29) | handles | 29 | existing file grew: schema fixtures open `database/sql` and `pgx` handles to restore and inspect a backup |
| internal/isolation/auth_refusal_acceptance_test.go | handles | 8 | `db.PG()`/`SQLiteRead()` queries over `information_schema` and tables for the datastore canary scan |
| internal/isolation/containment_e2e_test.go | handles | 2 | `db.SQLiteWrite().Stats()` reads writer-pool wait counts |
| internal/isolation/containment_historical_test.go | handles | 14 | opens raw `sql`/`pgx` handles to build a historical-schema fixture |
| internal/isolation/rotation_cascade_e2e_test.go | handles | 8 | raw `PG()`/`SQLiteWrite()` seeding of rotation rows |
| internal/isolation/rotation_executor_real_pg_test.go | handles | 14 | `SQLiteRead()`/`PG()` assertions on rotation run state |
| internal/isolation/rotation_summary_review_test.go | handles | 8 | `pggen.New(db.PG())` reads generated rotation queries |
| internal/isolation/rotation_summary_test.go | handles | 4 | same generated-query reads |
| internal/service/incident_census_test.go | handles | 4 | `SQLiteWrite().ExecContext` seeds incident rows |
| internal/service/incident_unit_test.go | handles | 2 | same seeding |
| internal/store/audit_commit_seq_ssi_test.go | handles | 12 | `pgx.Connect` to drive concurrent serializable transactions |

Totals added: handles +78 diagnostics in 10 new files (plus 2 in a grown file). The sets are identical in the default, ui, darwin and windows contexts. The first reported site of every handles file and of every redactfmt file was read at the flagged line; the classification is from that reading, not from running the code.

## Follow-up scope

1. `handles`: decide between a `driverFiles` inventory for harness test files and a reviewed test helper; then drop `strictDeferred`. About 1141 diagnostics in 103 files; the largest are `internal/store` and `internal/store/upgrade` postgres tests.
2. Remove `strictDeferred`, the `Deferred` bookkeeping and `deferred_sites.json` once `handles` is clean.
