package lint

import (
	"go/token"
	"path/filepath"
	"strings"
)

// Exact source-owned exceptions. Being inside store, a fixture package or the
// migration package does not grant new files ambient raw-driver authority.
const artifactHarnessRuntimeFile = "scripts/security/artifact-harness/runtime.go"
const alertCollectorSnapshotFile = "scripts/security/alert-route/collector_snapshot.go"
const alertRouteTestFile = "scripts/security/alert-route/main_test.go"
const mcpFixtureDirectory = "scripts/acceptance/mcp-fixture/"

var driverFiles = map[string]bool{
	"internal/store/migrate/authorization_pending_indexes_test.go": true, // exact historical-schema fixture measures complete query plans and unchanged row/uniqueness behavior
	"internal/service/advisory_benchmark_test.go":                  true, // signed-admitted benchmark seeds isolated session/grant fixtures and counts actual driver transactions

	artifactHarnessRuntimeFile:                                   true, // blackbox fixture creates/drops its owned disposable PostgreSQL database only
	alertCollectorSnapshotFile:                                   true, // exact read-only snapshot protocol for the independently administered external receiver; no Hikyo store handles
	mcpFixtureDirectory + "seed.go":                              true, // guarded read-only empty-datastore check; supported admission and domain writers own all tenant creation
	mcpFixtureDirectory + "local.go":                             true, // owned disposable local PostgreSQL pool passed to the pinned lifecycle protocol
	mcpFixtureDirectory + "lifecycle.go":                         true, // pinned cancellation/drain catalogue lock and committed claim/backend observations
	mcpFixtureDirectory + "public.go":                            true, // pinned guarded read-only observations and bounded approved fixture catalogue lock
	"internal/store/self_config_test.go":                         true, // admitted, isolated both-engine protected hierarchy/activation fixtures
	"internal/store/timestamps_postgres_test.go":                 true, // existing exact PostgreSQL connection timestamp regression
	"internal/upgradegate/gate_populated_process_test.go":        true, // actual admitted transactions seed crash-recovery acceptance
	"internal/upgradegate/configuration_test.go":                 true, // admitted old-catalogue fixture verifies read-only upgrade preview
	"internal/isolation/mcp_deployment_fixture_test.go":          true, // isolated exact-build deployment seed, after production admission
	"internal/store/upgrade/operator_rotation.go":                true, // canonical credential invalidation on migration-owned connection
	"internal/store/upgrade/migration_history.go":                true,
	"internal/store/upgrade/migration_history_test.go":           true,
	"internal/store/admission_process_test.go":                   true,
	"internal/store/upgrade/build_schema.go":                     true,
	"internal/store/upgrade/build_schema_test.go":                true,
	"internal/app/admin_test.go":                                 true,
	"internal/app/app_test.go":                                   true,
	"internal/app/premigration_test.go":                          true,
	"internal/app/scheduler_ha_integration_test.go":              true,
	"internal/conformance/conformance_test.go":                   true,
	"internal/conformance/keyring_test.go":                       true,
	"internal/conformance/revisions_test.go":                     true,
	"internal/conformance/scanning_test.go":                      true,
	"internal/conformance/values_test.go":                        true,
	"internal/isolation/approval_acceptance_test.go":             true,
	"internal/isolation/audit_e2e_test.go":                       true,
	"internal/isolation/managed_migration_audit_fixture_test.go": true, // signed gate fixture exercises the actual fenced migration audit emitter
	"internal/isolation/backup_drill_e2e_test.go":                true,
	"internal/isolation/forgejo_e2e_test.go":                     true,
	"internal/isolation/harness_test.go":                         true,
	"internal/isolation/querycount_test.go":                      true,
	"internal/isolation/reencrypt_e2e_test.go":                   true,
	"internal/isolation/remote_e2e_test.go":                      true,
	"internal/isolation/retention_cli_e2e_test.go":               true,
	"internal/isolation/retention_gc_e2e_test.go":                true,
	"internal/isolation/saml_storage_test.go":                    true,
	"internal/isolation/scim_e2e_test.go":                        true,
	"internal/isolation/scim_login_e2e_test.go":                  true,
	"internal/service/adapters_test.go":                          true,
	"internal/service/backup_preparation_test.go":                true,
	"internal/service/backup_prove_test.go":                      true,
	"internal/service/updates_test.go":                           true,
	"internal/store/adapter_bug744_test.go":                      true, // #744 both-engine origin-reuse + conflict-dedup fixtures
	"internal/store/adapter_control_test.go":                     true,
	"internal/store/adapter_runtime.go":                          true,
	"internal/store/adapter_runtime_test.go":                     true,
	"internal/store/admission.go":                                true,
	"internal/store/admission_serialized.go":                     true,
	"internal/store/admission_test.go":                           true,
	"internal/store/backup.go":                                   true,
	"internal/store/backup_durability_test.go":                   true,
	"internal/store/backup_snapshot_test.go":                     true,
	"internal/store/backup_test.go":                              true,
	"internal/store/backup_upgrade.go":                           true,
	"internal/store/coordination_test.go":                        true,
	"internal/store/coordination_transactions.go":                true,
	"internal/store/keys_test.go":                                true,
	"internal/store/migrate/adapter_migration_test.go":           true,
	"internal/store/migrate/adapter_timestamp_migration_test.go": true,
	"internal/store/migrate/migrate.go":                          true,
	"internal/store/migrate/pre_freeze_migration_test.go":        true,
	"internal/store/migrate/saml_migration_test.go":              true,
	"internal/store/migrate/upgrade_test.go":                     true,
	"internal/store/pool_test.go":                                true,
	"internal/store/recovery.go":                                 true,
	"internal/store/restore_destination.go":                      true,
	"internal/store/runtime_read.go":                             true,
	"internal/store/runtime_status.go":                           true,
	"internal/store/store.go":                                    true,
	"internal/store/tx/recovery.go":                              true,
	"internal/store/tx/restore.go":                               true,
	"internal/store/tx/tx.go":                                    true,
	"internal/store/upgrade/acceptance.go":                       true,
	"internal/store/upgrade/acceptance_test.go":                  true,
	"internal/store/upgrade/admission.go":                        true,
	"internal/store/upgrade/admission_test.go":                   true,
	"internal/store/upgrade/archive_fixture_test.go":             true,
	"internal/store/upgrade/candidate_initialize.go":             true,
	"internal/store/upgrade/candidate_initialize_test.go":        true,
	"internal/store/upgrade/candidate_keys.go":                   true,
	"internal/store/upgrade/candidate_keys_test.go":              true,
	"internal/store/upgrade/configuration_migrate.go":            true, // exact schema-applied session owns atomic encrypted managed-data migration
	"internal/store/upgrade/configuration_migrate_test.go":       true, // isolated both-engine migration, rollback and ciphertext preservation fixtures
	"internal/store/upgrade/crash_test.go":                       true,
	"internal/store/upgrade/domain_catalog.go":                   true,
	"internal/store/upgrade/fresh_instance.go":                   true,
	"internal/store/upgrade/genesis.go":                          true,
	"internal/store/upgrade/identity_consistency_test.go":        true,
	"internal/store/upgrade/inspect_control.go":                  true,
	"internal/store/upgrade/inspection.go":                       true,
	"internal/store/upgrade/ledger_test.go":                      true,
	"internal/store/upgrade/lock.go":                             true,
	"internal/store/upgrade/maintenance_leases.go":               true,
	"internal/store/upgrade/migrate.go":                          true,
	"internal/store/upgrade/migrate_session_test.go":             true,
	"internal/store/upgrade/migration_fixture_test.go":           true,
	"internal/store/upgrade/preparation.go":                      true,
	"internal/store/upgrade/recovery_admission.go":               true,
	"internal/store/upgrade/recovery_operation.go":               true,
	"internal/store/upgrade/recovery_operation_test.go":          true,
	"internal/store/upgrade/refusal_test.go":                     true,
	"internal/store/upgrade/restore.go":                          true,
	"internal/store/upgrade/restore_destination.go":              true,
	"internal/store/upgrade/restore_entropy_test.go":             true,
	"internal/store/upgrade/restore_schema.go":                   true,
	"internal/store/upgrade/restore_schema_test.go":              true,
	"internal/store/upgrade/restore_test.go":                     true,
	"internal/store/upgrade/rollback.go":                         true, // rollback lock holds the reserved migration session and replaces the live database under it
	"internal/store/upgrade/schema_postgres.go":                  true, // canonical catalog scope on the reserved migration/admission connection; no pool access
	"internal/store/upgrade/scratch_reset.go":                    true,
	"internal/store/upgrade/storage.go":                          true,
	"internal/store/upgrade_source.go":                           true,
	"internal/upgradegate/gate_test.go":                          true,

	// Harness test files of the vet driver's in-package test variants, reviewed one by one (#1488).
	"internal/app/automatic_apply_test.go":                             true, // second raw SQLite handle alters the schema out-of-band to prove the automatic route's fence refuses unexpected change
	"internal/app/backup_recovery_capability_test.go":                  true, // raw pgx connections occupy the disposable restore destination to prove capability expiry and occupied-target refusals
	"internal/app/backup_recovery_schema65_test.go":                    true, // raw SQLite and pgx transactions drive recovery transactions over schema 65/87 fixtures to assert grant and containment projections
	"internal/app/backup_upgrade_auto_drill_test.go":                   true, // raw SQLite/pgx connections seed and observe disposable upgrade-drill fixtures for automatic credential-authority selection
	"internal/app/backup_upgrade_drill_test.go":                        true, // reads untouched rows back through the store handles and builds legacy-drill fixtures with a pgx admin connection on disposable databases
	"internal/app/catalogue_limits_test.go":                            true, // one read-only SQLiteRead count of keys to assert the project key limit survives owner-runtime reload
	"internal/app/owner_runtime_test.go":                               true, // pings the read pool and bumps self_config_binding generation on the write pool to drive owner-runtime reload and refusal paths
	"internal/app/releasecompat_test.go":                               true, // pgx admin connection creates disposable databases to check both-engine release-compatibility schemas
	"internal/app/runtime_cleanup_settlement_test.go":                  true, // holds raw SQLite pool connections and transactions to prove shutdown joins a canceled settlement and leaves none in use
	"internal/app/security_rollback_store_test.go":                     true, // raw database/sql handle seeds and inspects rows for rollback-to-fenced-snapshot refusal tests
	"internal/app/self_config_deployment_test.go":                      true, // one write-pool UPDATE of self_config_binding simulates a changed binding for bootstrap-deployment proof tests
	"internal/app/self_config_topology_repair_test.go":                 true, // one read on the restored Postgres pool asserts coordination rows survive source repair after HA restore
	"internal/app/unattended_integration_test.go":                      true, // raw SQLite handle runs statements against the live fixture database for packaged unattended enrollment and upgrade acceptance
	"internal/app/unattended_scratch_test.go":                          true, // pgx connection plants unowned data in the scratch database to prove unattended scratch reuse preserves it
	"internal/conformance/privacy_content_test.go":                     true, // per-scope read-only counts through PG()/SQLiteRead assert privacy-masked content is absent at rest
	"internal/conformance/values_schema_test.go":                       true, // read-only schema queries through PG()/SQLiteRead assert masked values are absent from the stored schema
	"internal/isolation/access_counts_read_test.go":                    true, // holds a store write transaction open to prove operational access counts read without queuing on the writer
	"internal/isolation/adapter_adoption_clock_test.go":                true, // holds a raw Postgres FOR UPDATE transaction on adapter_targets to force the lock waits adoption rechecks must survive
	"internal/isolation/adapter_authority_timeout_test.go":             true, // pins the SQLite read pool to one connection and holds Postgres row locks to force authority-timeout and shutdown paths
	"internal/isolation/adapter_configuration_fence_test.go":           true, // holds raw Postgres FOR UPDATE locks on adapters to prove addition rechecks authority after parent lock waits
	"internal/isolation/adapter_core_scope_test.go":                    true, // binds generated adapter-core queries to the raw pools to prove they refuse foreign chains
	"internal/isolation/adapter_durable_progress_test.go":              true, // binds generated queries to the raw pools to seed and observe durable-progress rows
	"internal/isolation/adapter_generated_read_scope_test.go":          true, // binds generated read queries to the raw pools to prove they bind the owning chain
	"internal/isolation/adapter_origin_custody_test.go":                true, // holds LOCK TABLE adapter_ledger in a raw Postgres transaction to prove origin-custody guards under contention
	"internal/isolation/adapter_preparation_timeout_test.go":           true, // holds blocking raw Postgres transactions so preparation deadlines roll back and queue a retry
	"internal/isolation/adapter_reviewed_predicate_scope_test.go":      true, // calls reviewed-predicate generated queries on the raw pools to prove each binds its owning chain
	"internal/isolation/adapter_runtime_cleanup_scope_test.go":         true, // binds generated runtime-cleanup queries to the raw pools to prove they preserve live custody within scope
	"internal/isolation/adapter_runtime_direct_scope_test.go":          true, // binds generated direct-runtime queries to the raw pools to prove they bind the owning chain
	"internal/isolation/audit_refusal_emitter_test.go":                 true, // holds store transactions and reads pool wait counts to prove the refusal emitter waits for the staging writer
	"internal/isolation/audit_retention_e2e_test.go":                   true, // leaves a raw Postgres/SQLite transaction pending with a prune victim to prove retention rejects unproved deletes and rolls back on receipt failure
	"internal/isolation/auth_refusal_acceptance_test.go":               true, // read-only dumps of every table through PG()/SQLiteRead assert refusal capture stores no credential or request data
	"internal/isolation/auth_refusal_commit_test.go":                   true, // pgx connection behind a commit proxy proves snapshots wait for Postgres settlement and lost commit receipts retain evidence
	"internal/isolation/auth_refusal_storage_test.go":                  true, // native Postgres/SQLite transactions prove revoked-replay refusals are retained and correlated in storage
	"internal/isolation/aws_real_durable_test.go":                      true, // read-only counts through PG()/SQLiteRead assert the real AWS smoke leaves no secret in audit payloads
	"internal/isolation/aws_real_process_test.go":                      true, // read-only PRAGMA and count queries through SQLiteRead/PG() observe the real AWS replay process's stored state
	"internal/isolation/backup_sequence_restore_test.go":               true, // pgx connection to the restored database reads principals and sequence values to prove exhausted sequences refuse restore
	"internal/isolation/containment_e2e_test.go":                       true, // reads the SQLite write pool's WaitCount only, to assert containment paths do not queue on the writer
	"internal/isolation/containment_historical_test.go":                true, // raw SQLite/pgx handles build pre-containment historical schemas to prove recovery before containment
	"internal/isolation/federated_signup_e2e_test.go":                  true, // a nil check on PG() selects the engine for the one-query-path assertion; no query is run on the handle
	"internal/isolation/gitlab_e2e_test.go":                            true, // read-only target and job queries through PG()/SQLiteRead observe the real GitLab lifecycle
	"internal/isolation/instance_audit_wire_test.go":                   true, // raw Postgres transactions at differing isolation prove instance-audit export commit order
	"internal/isolation/member_access_rules_e2e_test.go":               true, // read-only transactions through PG()/SQLiteRead compare the generated queries with the service for member access rules
	"internal/isolation/oauth2_e2e_test.go":                            true, // reads the stored client_secret column to prove the provider secret is persisted sealed
	"internal/isolation/offline_receipt_ha_test.go":                    true, // reads the SQLite write pool's WaitCount to prove replica rotation does not queue on the writer
	"internal/isolation/pending_auth_proof_concurrency_test.go":        true, // reads the SQLite write pool's WaitCount to prove approval, redemption and login finish serialize with revocation
	"internal/isolation/personal_owner_e2e_test.go":                    true, // read-only transactions through PG()/SQLiteRead compare the generated queries with the service for personal-owner list and delete
	"internal/isolation/pki_generated_queries_test.go":                 true, // binds generated PKI queries to the raw pools to prove scope, binding membership and state CAS
	"internal/isolation/pki_restore_crl_hold_test.go":                  true, // holds a raw Postgres maintenance transaction to prove a restore hold blocks fresh CRL signing
	"internal/isolation/policy_member_footprint_test.go":               true, // side raw Postgres transactions measure the member-footprint query against concurrent locks
	"internal/isolation/recovery_fixture_test.go":                      true, // VACUUM INTO and pgxpool connections snapshot the datastore into recovery fixtures; no tenant data is written
	"internal/isolation/registration_e2e_test.go":                      true, // a nil check on PG() selects the engine for registration-policy assertions; no query is run on the handle
	"internal/isolation/remote_pairing_e2e_test.go":                    true, // read-only queries through PG()/SQLiteRead and a pgx connection inspect sealed remote credentials and pairing state
	"internal/isolation/remote_pairing_replay_test.go":                 true, // writes replay-admission rows on the SQLite write pool to prove peers are isolated
	"internal/isolation/rotation_cascade_e2e_test.go":                  true, // binds generated rotation queries to the raw pools to read credential expiries after cascades
	"internal/isolation/rotation_executor_real_pg_test.go":             true, // pgx admin connections and read queries drive the executor against a real Postgres role lifecycle
	"internal/isolation/rotation_summary_review_test.go":               true, // binds generated rotation list/summary queries to the raw pools to prove owed-drop counts stay inside their chain
	"internal/isolation/rotation_summary_test.go":                      true, // binds generated rotation summary queries to the raw pools to prove last-run bounds and owed-drop counts
	"internal/isolation/rule_refusal_querywork_test.go":                true, // read-only transactions through PG()/SQLiteRead measure the query work of rule-management refusals
	"internal/isolation/scim_page_bound_test.go":                       true, // pins Postgres pool connections and a nil-checked PG() to measure SCIM page materialization bounds
	"internal/isolation/scim_retirement_generated_scope_test.go":       true, // binds generated SCIM retirement queries to the raw pools to prove they preserve live owners within scope
	"internal/isolation/secret_occurrence_batch_test.go":               true, // read-only transactions through PG()/SQLiteRead measure secret-occurrence batch query behavior
	"internal/isolation/transit_admission_lock_test.go":                true, // Postgres transactions hold foreign-key and admission locks to prove transit admissions serialize
	"internal/service/adapter_authorization_clock_test.go":             true, // SQLite read/write pool statements seed and observe adapter state to prove final writes recheck live authority
	"internal/service/adapter_configuration_fence_test.go":             true, // SQLite pool reads count adapter targets to prove obsolete provider verification is refused
	"internal/service/adapter_retry_security_test.go":                  true, // SQLite pool statements seed reauth windows and count budgets to prove retries recheck the security clock
	"internal/service/adapters_gitlab_test.go":                         true, // SQLite pool statements seed grants and read back persisted GitLab adapter policy
	"internal/service/adapters_vaultkv_test.go":                        true, // one SQLite write-pool statement seeds the Vault KV adapter create test
	"internal/service/developer_credentials_test.go":                   true, // SQLite pool statements seed principals and observe credential rows for developer-credential mint, cap and revocation tests
	"internal/service/email_invitation_dispatch_test.go":               true, // reads dispatch outcome rows through PG()/SQLiteRead
	"internal/service/incident_census_test.go":                         true, // SQLite write-pool statements seed rules of a gated target for the incident census
	"internal/service/incident_unit_test.go":                           true, // one SQLite write-pool statement seeds the incident ownership unit fixture
	"internal/service/rotation_preflight_test.go":                      true, // SQLite write-pool transactions seed rotation state for root-rotation preflight refusals
	"internal/service/schema_fixture_boundary_test.go":                 true, // opens a raw SQLite handle on the prepared fixture copy to prove fixture writes stay in their copy
	"internal/service/self_config_runtime_test.go":                     true, // pgx admin connection prepares the disposable Postgres database for the self-config lifecycle test
	"internal/service/ux_benchmark_test.go":                            true, // one write-pool wal_checkpoint pragma between benchmark runs; no tenant data is written
	"internal/store/adapter_move_error_boundary_test.go":               true, // raw in-memory SQLite handles prove constraint errors keep their boundary through the move accessors
	"internal/store/admission_serialized_test.go":                      true, // holds serialized Postgres admission transactions on pool connections to prove the lock persists until settlement
	"internal/store/admission_settlement_driver_test.go":               true, // wraps a fake SQL driver connector to prove SQLite settlement preserves driver errors and survives automatic discard
	"internal/store/admission_settlement_test.go":                      true, // opens store transactions and inspects the raw pools to prove SQLite settlement, cancellation and in-use accounting
	"internal/store/audit_commit_seq_ssi_test.go":                      true, // raw Postgres transactions probe commit_seq assignment to prove it takes no SI read lock
	"internal/store/backup_diagnostics_test.go":                        true, // pgx connection and transactions seed and compare the diagnostics seed to prove restore replaces only the untouched one
	"internal/store/backup_refusal_test.go":                            true, // pgx connection and transactions seed and compare the refusal seed to prove restore replaces only the untouched one
	"internal/store/migrate/adapter_adoption_indexes_test.go":          true, // raw migration-fixture handle fills tables to prove the adoption indexes at scale
	"internal/store/migrate/adapter_custody_move_repair_test.go":       true, // seeds custody-move rows through the migration fixture handle for the repair migration test
	"internal/store/migrate/adapter_custody_repairs_test.go":           true, // migration-fixture handle seeds and checks rows before and after the custody repair migration, both engines
	"internal/store/migrate/audit_scope_indexes_test.go":               true, // migration-fixture handle checks the audit scope indexes after migration, both engines
	"internal/store/migrate/aws_adapter_migration_test.go":             true, // migration-fixture handle checks the AWS adapter migration, both engines
	"internal/store/migrate/cli_reauth_mint_handoff_migration_test.go": true, // migration-fixture handle seeds and reads handoff rows to prove the 00097 mint constraints, both engines
	"internal/store/migrate/scim_users_account_index_test.go":          true, // migration-fixture handle fills 20000 rows to prove the SCIM users account index migration
	"internal/store/migrate/social_signin_migration_test.go":           true, // migration-fixture handle checks the social sign-in migration and email-validity parity, both engines
	"internal/store/migrate/transit_mldsa65_migration_test.go":         true, // migration-fixture handle seeds transit keys to prove the ML-DSA-65 migration, both engines
	"internal/store/postgres_pool_prepared_test.go":                    true, // holds prepared Postgres pool connections to prove transactions and coordination survive prepared-pool switchover and stale candidates
	"internal/store/postgres_source_proof_test.go":                     true, // holds Postgres pool connections and blocking transactions to prove source-proof alias binding, sanitized failures and recovery refusal
	"internal/store/secret_occurrence_bench_test.go":                   true, // raw transactions and pgx connections load the secret-occurrence benchmark and observe both engines
	"internal/store/upgrade/backup_fence_test.go":                      true, // raw handles drive the backup fence over disposable databases to prove drain, wrong-proof rollback and rotation recovery
	"internal/store/upgrade/candidate_configuration_test.go":           true, // migration-session connection runs queries to prove the candidate configuration projection binds owner scope and session
	"internal/store/upgrade/rollback_postgres_test.go":                 true, // raw Postgres handles on disposable databases prove rollback as an unprivileged role and the admit basis
	"internal/store/upgrade/schema_postgres_scope_test.go":             true, // raw pgx transactions and connections prove the catalog scope, cancellation and cleanup-failure invalidation
	"internal/store/upgrade/schema_postgres_test.go":                   true, // pooled database/sql connections prove canonical catalog rendering and caller-transaction preservation
	"internal/store/upgrade/session_timeouts_test.go":                  true, // queries version() on a session connection to prove a dead client drops its lock session
	"internal/upgradegate/catalog_rendering_test.go":                   true, // pgx connection renders the Postgres signed-gate catalog on a disposable database
	"internal/upgradegate/operator_rotation_test.go":                   true, // raw database/sql handles seed and inspect rows for operator-rotation journal, crash-resume and installation-pin tests
	mcpFixtureDirectory + "audit_seed_test.go":                         true, // pgx connection checks the audit seed cannot reuse the MCP datastore or another installation's, on both engines
	alertRouteTestFile: true, // raw SQLite handles on throwaway fixture databases drive the alert-route collector snapshot tests
}

func permittedHandlePosition(fset *token.FileSet, base string, pos token.Pos) bool {
	// Generated DBTX definitions and outbound provider connections are distinct
	// owned boundaries; neither is a Hikyo runtime repository caller.
	if base == Module+"/internal/store/pggen" || base == Module+"/internal/store/sqlitegen" || base == Module+"/internal/dynamic/postgres" {
		return true
	}
	filename := filepath.ToSlash(fset.PositionFor(pos, false).Filename)
	if base == Module+"/scripts/security/artifact-harness" && strings.HasSuffix(filename, "/"+artifactHarnessRuntimeFile) {
		return driverFiles[artifactHarnessRuntimeFile]
	}
	if base == Module+"/scripts/security/alert-route" && strings.HasSuffix(filename, "/"+alertCollectorSnapshotFile) {
		return driverFiles[alertCollectorSnapshotFile]
	}
	if base == Module+"/scripts/security/alert-route" && strings.HasSuffix(filename, "/"+alertRouteTestFile) {
		return driverFiles[alertRouteTestFile]
	}
	if base == Module+"/scripts/acceptance/mcp-fixture" {
		for _, file := range []string{"seed.go", "local.go", "lifecycle.go", "public.go", "audit_seed_test.go"} {
			relative := mcpFixtureDirectory + file
			if strings.HasSuffix(filename, "/"+relative) {
				return driverFiles[relative]
			}
		}
	}
	index := strings.LastIndex(filename, "/internal/")
	if index < 0 {
		return false
	}
	relative := filename[index+1:]
	expected := Module + "/" + filepath.ToSlash(filepath.Dir(relative))
	if base != expected && (base != expected+"_test" || !externalTestFiles[relative]) {
		return false
	}
	return driverFiles[relative]
}

// externalTestFiles are the inventoried files whose package clause is the
// external "<pkg>_test" package. Only these may use the inventory under that
// package path; an in-package-only entry is refused there, so the _test
// package cannot borrow authority reviewed for a different compilation unit.
var externalTestFiles = map[string]bool{
	"internal/store/self_config_test.go":                  true,
	"internal/store/timestamps_postgres_test.go":          true,
	"internal/store/adapter_bug744_test.go":               true,
	"internal/store/adapter_control_test.go":              true,
	"internal/store/adapter_runtime_test.go":              true,
	"internal/store/coordination_test.go":                 true,
	"internal/store/keys_test.go":                         true,
	"internal/store/upgrade/archive_fixture_test.go":      true,
	"internal/store/upgrade/restore_test.go":              true,
	"internal/upgradegate/gate_populated_process_test.go": true,
}

// Coordination SQL receives already admitted transaction wrappers, never a
// pool or native connection. This exception does not grant raw driver access.
func permittedGuardedTransaction(fset *token.FileSet, base string, pos token.Pos, named string) bool {
	if named != Module+"/internal/store.SQLiteTransaction" && named != Module+"/internal/store.PostgresTransaction" {
		return false
	}
	filename := filepath.ToSlash(fset.PositionFor(pos, false).Filename)
	return base == Module+"/internal/store" && (strings.HasSuffix(filename, "/internal/store/coordination.go") || strings.HasSuffix(filename, "/internal/store/coordination_self_config.go"))
}

func permittedRawConstructor(fset *token.FileSet, base string, pos token.Pos) bool {
	if base != Module+"/internal/store" {
		return false
	}
	filename := filepath.ToSlash(fset.PositionFor(pos, false).Filename)
	for _, name := range []string{"store.go", "preparation.go", "recovery.go", "restore_destination.go", "pool_test.go"} {
		if strings.HasSuffix(filename, "/internal/store/"+name) {
			return true
		}
	}
	return false
}
