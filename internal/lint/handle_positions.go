package lint

import (
	"go/token"
	"golang.org/x/tools/go/packages"
	"path/filepath"
	"strings"
)

// Exact source-owned exceptions. Being inside store, a fixture package or the
// migration package does not grant new files ambient raw-driver authority.
var driverFiles = map[string]bool{
	"internal/isolation/floor_bench_test.go":                     true, // signed-admitted bulk fixture and independent committed-chunk observation only
	"internal/isolation/ops_floor_test.go":                       true, // isolated admitted SQLite health accounting for native O2 acceptance
	"internal/store/timestamps_postgres_test.go":                 true, // existing exact PostgreSQL connection timestamp regression
	"internal/upgradegate/gate_populated_process_test.go":        true, // actual admitted transactions seed crash-recovery acceptance
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
	"internal/store/preparation.go":                              true,
	"internal/store/recovery.go":                                 true,
	"internal/store/restore_destination.go":                      true,
	"internal/store/runtime_read.go":                             true,
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
	"internal/store/upgrade/control_schema.go":                   true,
	"internal/store/upgrade/crash_test.go":                       true,
	"internal/store/upgrade/domain_catalog.go":                   true,
	"internal/store/upgrade/fresh_instance.go":                   true,
	"internal/store/upgrade/genesis.go":                          true,
	"internal/store/upgrade/identity_consistency_test.go":        true,
	"internal/store/upgrade/identity_unix.go":                    true,
	"internal/store/upgrade/identity_windows.go":                 true,
	"internal/store/upgrade/inspect_control.go":                  true,
	"internal/store/upgrade/inspect_control_test.go":             true,
	"internal/store/upgrade/inspection.go":                       true,
	"internal/store/upgrade/inspection_test.go":                  true,
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
	"internal/store/upgrade/route_test.go":                       true,
	"internal/store/upgrade/schema.go":                           true,
	"internal/store/upgrade/snapshot.go":                         true,
	"internal/store/upgrade/state.go":                            true,
	"internal/store/upgrade/storage.go":                          true,
	"internal/store/upgrade_source.go":                           true,
	"internal/upgradegate/gate_test.go":                          true,
}

func permittedHandlePosition(p *packages.Package, base string, pos token.Pos) bool {
	// Generated DBTX definitions and outbound provider connections are distinct
	// owned boundaries; neither is a Hikyo runtime repository caller.
	if base == Module+"/internal/store/pggen" || base == Module+"/internal/store/sqlitegen" || base == Module+"/internal/dynamic/postgres" {
		return true
	}
	filename := filepath.ToSlash(p.Fset.Position(pos).Filename)
	index := strings.LastIndex(filename, "/internal/")
	if index < 0 {
		return false
	}
	relative := filename[index+1:]
	expected := Module + "/" + filepath.ToSlash(filepath.Dir(relative))
	if base != expected && base != expected+"_test" {
		return false
	}
	return driverFiles[relative]
}

// Coordination SQL receives already admitted transaction wrappers, never a
// pool or native connection. This exception does not grant raw driver access.
func permittedGuardedTransaction(p *packages.Package, base string, pos token.Pos, named string) bool {
	if named != Module+"/internal/store.SQLiteTransaction" && named != Module+"/internal/store.PostgresTransaction" {
		return false
	}
	return base == Module+"/internal/store" && strings.HasSuffix(filepath.ToSlash(p.Fset.Position(pos).Filename), "/internal/store/coordination.go")
}

func permittedRawConstructor(p *packages.Package, base string, pos token.Pos) bool {
	if base != Module+"/internal/store" {
		return false
	}
	filename := filepath.ToSlash(p.Fset.Position(pos).Filename)
	for _, name := range []string{"store.go", "preparation.go", "recovery.go", "restore_destination.go", "pool_test.go"} {
		if strings.HasSuffix(filename, "/internal/store/"+name) {
			return true
		}
	}
	return false
}
