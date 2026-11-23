# Lint vet driver PR 1: deferred in-package test sites

Work: PR 1 of `docs/adr/lint-analysis-driver.md` (branch `dunky13/lint-vettool-driver`).

## Why this exists

The vet driver analyzes the in-package test variant of every package. The old go/packages harness kept one variant per package path (the production one), so it never saw in-package `_test.go` files. Two analyzers, `handles` and `redactfmt`, report many sites there. Per the owner's scope decision (option A), PR 1 ships these two analyzers in the vet driver for every context but does not gate on those sites yet.

## Exact gate

`TestVetDriverRepo` ignores a diagnostic only when all of these hold (`strictDeferred` in `internal/lint/analyzer.go`, `deferTestSites` in the same file):

1. The analyzer is `handles` or `redactfmt` (the entries in `strictDeferred`).
2. The package is an in-package test variant: its path does not end in `_test` (an external test package stays strict).
3. The diagnostic's file name ends in `_test.go`, read from the file on disk without `//line` adjustment, so a `//line` directive cannot move a site into a test file.

Any production-file finding, any external test package finding, and every other analyzer's finding still fail the test.

## What gates these sites today

The deferred sites are gated by a ratchet, not by the old harness. The old go/packages harness never loads in-package `_test.go` files, so it does not enforce them, and `TestVetDriverParity` proves nothing about them (its leg runs the same analyzer core through the old loader and detects only loader and variant differences).

`TestVetDriverRepo` compares the deferred diagnostics of every context with `internal/lint/testdata/deferred_sites.json`, a checked-in ceiling of analyzer -> file -> count per context, decoded strictly into typed structures. It fails when a file absent from the pin gains a deferred diagnostic, when a file's count exceeds its ceiling, and, with an instruction to lower the pin, when a count drops, so the pin only ratchets down. After fixing sites, lower it with `go test ./internal/lint -run 'TestVetDriverRepo$' -update-deferred-sites` and review the diff. The pin is not a `driverFiles` inventory entry: it grants nothing, it only caps the backlog. No `driverFiles` entries were added.

## Counts

Distinct diagnostics (union over the default, ui, darwin and windows contexts, one per file:line for `handles`, which reports each line once, and one per file:line:column for `redactfmt`):

| Analyzer | Diagnostics | Distinct file:line | Files |
|---|---|---|---|
| handles | 1141 | not recounted after the `dev` integration (784 before it) | 103 |
| redactfmt | 84 | not recounted after the `dev` integration (56 before it) | 25 |

forgeguard and fence: 0 deferred; their 36 newly visible findings (34 forgeguard, 2 fence) were fixed in this PR (34 forgeguard includes `unattended_operator_test.go`, added by the `dev` integration). Every other analyzer: 0 findings in all four contexts.

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

## Per-file breakdown: redactfmt

- internal/isolation/automation_preview_test.go: 16
- internal/isolation/adapter_lifecycle_retirement_test.go: 9
- internal/isolation/dynamic_ttl_test.go: 7
- internal/isolation/profile_e2e_test.go: 6
- internal/isolation/audit_export_receipt_wire_test.go: 5
- internal/isolation/policy_member_footprint_test.go: 5
- internal/isolation/audit_collector_rehearsal_test.go: 3
- internal/app/self_config_topology_test.go: 2
- internal/isolation/access_e2e_test.go: 2
- internal/isolation/instance_audit_wire_test.go: 2
- internal/isolation/rules_wire_test.go: 2
- internal/isolation/scanning_sweep_e2e_test.go: 2
- internal/app/candidate_next_root_test.go: 1
- internal/app/self_config_deployment_test.go: 1
- internal/isolation/aws_real_credential_fence_test.go: 1
- internal/isolation/cli_tls_consumer_test.go: 1
- internal/isolation/client_skew_e2e_test.go: 1
- internal/isolation/dynamic_renewal_test.go: 1
- internal/isolation/federated_signup_e2e_test.go: 1
- internal/isolation/fixture_cli_login_e2e_test.go: 1
- internal/isolation/incident_containment_cli_test.go: 1
- internal/isolation/rotation_cascade_e2e_test.go: 6
- internal/isolation/rotation_executor_round4_test.go: 4
- internal/isolation/rotation_e2e_test.go: 2
- internal/isolation/rotation_runnow_e2e_test.go: 2

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

### redactfmt (10 of 56 lines, sampled before the `dev` integration)

8 of 10 look like false positives of the analyzer rule: the formatted value is a string or int (`principal`, `h.seq`, `ts`, `wire.org`), and the finding comes from the sub-expression walk reaching a harness struct (`h`, `wire`, `db`) whose fields include a `crypto.Keyring`. Narrowing the rule (skip the receiver of a field selection whose own type is not sensitive) would remove them; a narrowing was tried in PR 1 and reverted because it also lost real detections (see "Follow-up scope"). The other 2 (`json.Marshal(d.enrollment)`) are unverified: whether `enrollment` reaches a Keyring was not checked, so they are not classified as safe.

| Site | Formatted value | Class |
|---|---|---|
| internal/isolation/access_e2e_test.go:82 | `h.seq` int, `principal` string in `Sprintf` ID | rule false positive (receiver `h` holds a Keyring) |
| internal/isolation/access_e2e_test.go:101 | `Sprintf` ID from strings | rule false positive |
| internal/isolation/automation_preview_test.go:123 | `Sprintf` INSERT from strings and `ts` | rule false positive |
| internal/isolation/automation_preview_test.go:140 | `Sprintf` INSERT from `wire.org`, `ts` | rule false positive |
| internal/isolation/audit_collector_rehearsal_test.go:44 | `Sprintf` INSERT | rule false positive |
| internal/isolation/scanning_sweep_e2e_test.go:361 | `Sprintf` args from `org.ID`, `ts` | rule false positive |
| internal/isolation/dynamic_ttl_test.go:133 | `Sprintf` UPDATE with an int and an ID | rule false positive |
| internal/isolation/rules_wire_test.go:35 | `Sprintf` INSERT with env IDs | rule false positive |
| internal/app/self_config_topology_test.go:107 | `json.Marshal(d.enrollment)` | unverified |
| internal/app/self_config_topology_test.go:125 | `json.Marshal(d.enrollment)` | unverified |

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
| internal/isolation/rotation_cascade_e2e_test.go | redactfmt | 6 | rule false positive: `fmt.Sprintf` INSERTs over strings and ints, flagged because the receiver `f` holds a `crypto.Keyring` |
| internal/isolation/rotation_e2e_test.go | redactfmt | 2 | same false positive (`principal`, `f.seq` in IDs) |
| internal/isolation/rotation_executor_round4_test.go | redactfmt | 4 | same false positive (`runID`, `stamp(...)` in UPDATEs) |
| internal/isolation/rotation_runnow_e2e_test.go | redactfmt | 2 | same false positive (`id`, `ts` in INSERTs) |

Totals added: handles +78 diagnostics in 10 new files (plus 2 in a grown file), redactfmt +14 in 4 new files. The sets are identical in the default, ui, darwin and windows contexts. The first reported site of every handles file and of every redactfmt file was read at the flagged line; the classification is from that reading, not from running the code.

## Follow-up scope

1. `redactfmt` ([#1468](https://github.com/Hikyo-Org/Hikyo/issues/1468)): narrow the sub-expression rule for selections from a non-sensitive holder, in its own PR. A narrowing was tried in this PR and reverted after three review rounds: each patch removed the false positives but lost real detections (a method returning `any`, a call argument or literal inside the receiver, audit field walks two or more fields deep such as `ev.Actor.ID`). The follow-up's acceptance gate is a differential test: run the old and new rules over a shared adversarial corpus (start from the `Narrowing`/`NarrowingKeepsLeaks` cases in `internal/lint/testdata/badredact/badredact.go`) and require every case the old rule flags and the new rule does not to be on an explicit false-positive list. Then remove the 84 pinned sites.
2. `handles`: decide between a `driverFiles` inventory for harness test files and a reviewed test helper; then drop `strictDeferred`. About 1141 diagnostics in 103 files; the largest are `internal/store` and `internal/store/upgrade` postgres tests.
3. Remove `strictDeferred`, the `Deferred` bookkeeping and `deferred_sites.json` once both are clean.
