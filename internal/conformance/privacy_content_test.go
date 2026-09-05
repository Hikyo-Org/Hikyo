package conformance

import (
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func init() {
	corpus = append(corpus, scenario{"privacy_environment_deletion_erases_pinned_content", scenarioPrivacyEnvironmentDeletion})
}

// Environment deletion is the supported coarse erasure boundary for customer
// content. Clearing a value alone deliberately preserves immutable history.
// Prove erasure against stored rows, including drafts and pinned history, while
// preserving the neighbouring environment, another tenant and shared identity.
func scenarioPrivacyEnvironmentDeletion(t *testing.T, db *store.DB) {
	who, scope, values, envs, keys := valueFixture(t, db, "privacycontent")
	actor := service.LocalPrincipal(who)
	target := mustEnv(t, envs, actor, scope, "erase")
	neighbour := mustEnv(t, envs, actor, scope, "keep")
	mustKey(t, keys, actor, scope, "PRIVATE", string(schema.Secret), schema.DefaultPresenceRules())
	publishValue(t, db, values, actor, target, "PRIVATE", "previous personal content")
	publishValue(t, db, values, actor, target, "PRIVATE", "current personal content")
	publishValue(t, db, values, actor, neighbour, "PRIVATE", "unrelated content")
	grantOrg(t, db, who, scope.Org, "privacycontent", "pin")
	workload := seedWorkload(t, db, scope, who, "privacycontent")
	pins := &service.Pins{DB: db, Keyring: sharedKeyring(t, db)}
	if _, err := pins.Set(t.Context(), actor, target, service.SetPinRequest{
		WorkloadPrincipalID: workload, Revision: latestRevisionOf(t, db, string(target.Env)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := values.Set(t.Context(), actor, target, "PRIVATE", "unpublished personal content", nil); err != nil {
		t.Fatal(err)
	}

	otherWho, otherScope, otherValues, otherEnvs, otherKeys := valueFixture(t, db, "privacyother")
	otherActor := service.LocalPrincipal(otherWho)
	other := mustEnv(t, otherEnvs, otherActor, otherScope, "erase")
	mustKey(t, otherKeys, otherActor, otherScope, "PRIVATE", string(schema.Secret), schema.DefaultPresenceRules())
	publishValue(t, db, otherValues, otherActor, other, "PRIVATE", "other tenant content")

	tables := []string{"value_entries", "pending_changes", "snapshot_entries", "snapshots", "revision_key_changes", "revision_pins", "secret_value_occurrences"}
	before := make(map[string]int64, len(tables))
	for _, table := range tables {
		before[table] = contentRows(t, db, table, target)
		if before[table] == 0 {
			t.Fatalf("fixture missing %s rows", table)
		}
	}
	neighbourRows := contentRows(t, db, "snapshot_entries", neighbour)
	otherRows := contentRows(t, db, "snapshot_entries", other)
	// An unresolved environment dependency must roll back the entire erasure,
	// including the payload deletes that run before the final FK check.
	seed(t, db, []string{fmt.Sprintf(`INSERT INTO grants
		(id,principal_id,capability,org_id,project_id,env_id,created_at)
		VALUES ('grt_privacy_content_dependency','%s','read','%s','%s','%s','2026-01-01T00:00:00Z')`,
		workload, target.Org, target.Project, target.Env)})
	if err := envs.Delete(t.Context(), actor, target); err == nil {
		t.Fatal("deletion accepted unresolved environment grant")
	}
	for _, table := range tables {
		if got := contentRows(t, db, table, target); got != before[table] {
			t.Errorf("refused deletion changed %s rows: %d -> %d", table, before[table], got)
		}
	}
	if got := auditEventCount(t, db, string(target.Env), "settings.environment_deleted"); got != 0 {
		t.Fatalf("refused deletion recorded %d successful erasures", got)
	}
	seed(t, db, []string{"DELETE FROM grants WHERE id='grt_privacy_content_dependency'"})
	if err := envs.Delete(t.Context(), actor, target); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if got := contentRows(t, db, table, target); got != 0 {
			t.Errorf("erasure left %d %s rows", got, table)
		}
	}
	if got := contentRows(t, db, "snapshot_entries", neighbour); got != neighbourRows {
		t.Errorf("neighbour content changed: %d -> %d", neighbourRows, got)
	}
	if got := contentRows(t, db, "snapshot_entries", other); got != otherRows {
		t.Errorf("other tenant content changed: %d -> %d", otherRows, got)
	}
	if _, err := envs.Get(t.Context(), actor, neighbour); err != nil {
		t.Fatalf("shared principal no longer accesses surviving environment: %v", err)
	}
	if got := auditEventCount(t, db, string(target.Env), "settings.environment_deleted"); got != 1 {
		t.Fatalf("erasure audit count = %d, want 1", got)
	}
}

func contentRows(t *testing.T, db *store.DB, table string, scope domain.Scope) int64 {
	t.Helper()
	// Table names are fixed test constants; values remain bound parameters.
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE org_id=$1 AND project_id=$2 AND environment_id=$3", table)
	var n int64
	var err error
	if db.Engine() == store.EnginePostgres {
		err = db.PG().QueryRow(t.Context(), query, string(scope.Org), string(scope.Project), string(scope.Env)).Scan(&n)
	} else {
		err = db.SQLiteRead().QueryRowContext(t.Context(), query, string(scope.Org), string(scope.Project), string(scope.Env)).Scan(&n)
	}
	if err != nil {
		t.Fatal(err)
	}
	return n
}
