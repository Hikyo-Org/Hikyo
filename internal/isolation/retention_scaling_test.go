package isolation

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// TestRetentionRankingScaling exercises the real migrated stores and service,
// including payload deletion, audit writes and transaction commits. Timings are
// diagnostics, not a latency SLO: shared CI machines cannot enforce wall time.
// HIKYO_RETENTION_SCALING=1 enables the larger local measurement matrix.
// HIKYO_RETENTION_SCALING_SIZES overrides sizes for bounded engine comparisons.
func TestRetentionRankingScaling(t *testing.T) {
	sizes := []int{240, 480, 960}
	if os.Getenv("HIKYO_RETENTION_SCALING") == "1" {
		sizes = []int{5000, 10000, 20000}
	}
	if custom := os.Getenv("HIKYO_RETENTION_SCALING_SIZES"); custom != "" {
		sizes = nil
		for _, value := range strings.Split(custom, ",") {
			size, err := strconv.Atoi(value)
			if err != nil || size < 240 {
				t.Fatalf("invalid retention measurement size %q: use integers >= 240", value)
			}
			sizes = append(sizes, size)
		}
	}
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			for _, workload := range []string{"catchup", "collected_history", "many_environments", "quiet_environment_stress", "quiet_projects", "protected_roots"} {
				for _, size := range sizes {
					t.Run(fmt.Sprintf("%s/%d", workload, size), func(t *testing.T) {
						var db *store.DB
						if engine == store.EngineSQLite {
							db = openSQLite(t)
						} else {
							db = openPostgres(t)
						}
						now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
						want, retained := seedRetentionScaling(t, db, workload, size)
						maxEnvs := queryInt(t, db, "SELECT COALESCE(MAX(n),0) FROM (SELECT COUNT(*) AS n FROM environments GROUP BY org_id, project_id) AS counts")
						if workload != "quiet_environment_stress" && maxEnvs > service.MaxEnvironmentsPerProject {
							t.Fatalf("fixture exceeds supported environment cap: %d", maxEnvs)
						}
						if maxEnvs > service.MaxEnvironmentsPerProject {
							t.Logf("out-of-contract stress: environments per project=%d exceeds supported cap=%d", maxEnvs, service.MaxEnvironmentsPerProject)
						}
						svc := &service.Retention{DB: db, Now: func() time.Time { return now }}
						// Measure the store's first batch separately from the full audited sweep.
						started := time.Now()
						var first []store.GCEligibleSnapshot
						err := storetx.Write(t.Context(), db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
							p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
							if err != nil {
								return err
							}
							first, err = r.Retention().Eligible(ctx, p, now, service.RetentionBatchSize)
							return err
						})
						if err != nil {
							t.Fatalf("first eligibility transaction: %v", err)
						}
						firstElapsed := time.Since(started)
						firstWant := min(want, service.RetentionBatchSize)
						if len(first) != firstWant {
							t.Fatalf("first batch=%d want=%d", len(first), firstWant)
						}
						started = time.Now()
						got, err := svc.Sweep(t.Context())
						elapsed := time.Since(started)
						if err != nil || got != int64(want) {
							t.Fatalf("sweep=%d, %v; want=%d", got, err, want)
						}
						if got := queryInt(t, db, "SELECT COUNT(*) FROM snapshots"); got != int64(size) {
							t.Fatalf("lineage=%d want=%d", got, size)
						}
						if got := queryInt(t, db, "SELECT COUNT(*) FROM snapshots WHERE payload_present=TRUE"); got != int64(len(retained)) {
							t.Fatalf("live payloads=%d want=%d", got, len(retained))
						}
						if got := queryInt(t, db, "SELECT COUNT(*) FROM snapshots WHERE payload_present=TRUE AND id IN ("+strings.Join(retained, ",")+")"); got != int64(len(retained)) {
							t.Fatalf("expected retained roots=%d want=%d", got, len(retained))
						}
						if got := queryInt(t, db, "SELECT COUNT(*) FROM snapshot_entries"); got != int64(len(retained)) {
							t.Fatalf("entries=%d want=%d", got, len(retained))
						}
						if got := queryInt(t, db, "SELECT COUNT(*) FROM snapshot_entries e JOIN snapshots s ON s.id=e.snapshot_id WHERE s.payload_present=FALSE"); got != 0 {
							t.Fatalf("collected entries=%d", got)
						}
						if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='retention.payload_gc'"); got != int64(want) {
							t.Fatalf("collection audits=%d want=%d", got, want)
						}
						if got, err := svc.Sweep(t.Context()); err != nil || got != 0 {
							t.Fatalf("repeat sweep=%d, %v", got, err)
						}
						// A later policy widening counts collected lineage but cannot
						// resurrect its payload, even when N exceeds the whole history.
						execRaw(t, db, fmt.Sprintf("UPDATE orgs SET retention_revision_count=%d", size+1))
						if got, err := svc.Sweep(t.Context()); err != nil || got != 0 {
							t.Fatalf("widened policy sweep=%d, %v", got, err)
						}
						if got := queryInt(t, db, "SELECT COUNT(*) FROM snapshot_entries"); got != int64(len(retained)) {
							t.Fatalf("policy widening resurrected entries: got=%d want=%d", got, len(retained))
						}
						if got := queryInt(t, db, "SELECT COUNT(*) FROM snapshots WHERE payload_present=TRUE"); got != int64(len(retained)) {
							t.Fatalf("policy widening resurrected headers: got=%d want=%d", got, len(retained))
						}
						t.Logf("engine=%s workload=%s lineage=%d collected=%d retained=%d batch=%d first_batch=%s full_audited_sweep=%s", engine, workload, size, want, len(retained), service.RetentionBatchSize, firstElapsed, elapsed)
					})
				}
			}
		})
	}
}

// Every revision number is sparse. Fixtures contain one opaque payload entry
// per live snapshot; this tests retention and deletion, not cryptographic reads.
func seedRetentionScaling(t *testing.T, db *store.DB, workload string, size int) (collect int, retained []string) {
	t.Helper()
	exec := func(q string) { t.Helper(); execRaw(t, db, q) }
	exec("INSERT INTO orgs(id,name,active,metadata,created_at) VALUES('org_rank','rank',TRUE,'{}','2020-01-01T00:00:00Z')")
	exec("INSERT INTO projects(id,org_id,name,created_at) VALUES('prj_rank','org_rank','rank','2020-01-01T00:00:00Z')")
	envs := 1
	if workload == "many_environments" {
		envs = 20
	}
	if workload == "quiet_environment_stress" || workload == "quiet_projects" {
		envs = size / 5
	}
	// The stress case deliberately exceeds the environment cap in one project.
	// quiet_projects distributes the same quiet histories over supported projects.
	projectForEnv := func(env int) string {
		if workload == "quiet_projects" && env >= service.MaxEnvironmentsPerProject {
			return fmt.Sprintf("prj_rank_%d", env/service.MaxEnvironmentsPerProject)
		}
		return "prj_rank"
	}
	for env := 0; env < envs; env++ {
		project := projectForEnv(env)
		if workload == "quiet_projects" && env > 0 && env%service.MaxEnvironmentsPerProject == 0 {
			exec(fmt.Sprintf("INSERT INTO projects(id,org_id,name,created_at) VALUES('%s','org_rank','rank-%d','2020-01-01T00:00:00Z')", project, env))
		}
		exec(fmt.Sprintf("INSERT INTO environments(id,org_id,project_id,name,note,display_order,created_at) VALUES('env_rank_%d','org_rank','%s','rank-%d','',%d,'2020-01-01T00:00:00Z')", env, project, env, env))
	}
	snapshots, entries := []string{}, []string{}
	flush := func() {
		if len(snapshots) > 0 {
			exec("INSERT INTO snapshots(id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at,payload_present,collected_at,collected_policy) VALUES " + strings.Join(snapshots, ","))
			snapshots = nil
		}
		if len(entries) > 0 {
			exec("INSERT INTO snapshot_entries(id,org_id,project_id,environment_id,snapshot_id,key_id,key_name,classification,ciphertext,value_entry_id) VALUES " + strings.Join(entries, ","))
			entries = nil
		}
	}
	for i := 0; i < size; i++ {
		env, ordinal := i%envs, i/envs
		envSize := (size + envs - 1 - env) / envs
		live := workload != "collected_history" || ordinal < 100 || ordinal >= envSize-10 && ordinal < envSize-5
		keep := ordinal >= envSize-10
		published := "2020-01-01T00:00:00Z"
		if workload == "protected_roots" && ordinal < 5 {
			keep = true
			if ordinal == 4 {
				published = "2026-10-16T12:00:00Z"
			}
		}
		present, at, policy := "TRUE", "NULL", ""
		if !live {
			present, at, policy = "FALSE", "'2020-02-01T00:00:00Z'", "fixture-collected"
		} else if keep {
			retained = append(retained, fmt.Sprintf("'snp_rank_%d'", i))
		} else {
			collect++
		}
		snapshots = append(snapshots, fmt.Sprintf("('snp_rank_%d','org_rank','%s','env_rank_%d',%d,1,'usr_rank','%s',%s,%s,'%s')", i, projectForEnv(env), env, ordinal*11+1, published, present, at, policy))
		if live {
			entries = append(entries, fmt.Sprintf("('sen_rank_%d','org_rank','%s','env_rank_%d','snp_rank_%d','key_rank','RANK','config','opaque-payload','val_rank')", i, projectForEnv(env), env, i))
		}
		if len(snapshots) == 100 {
			flush()
		}
	}
	flush()
	if workload == "protected_roots" {
		for i, slot := range []string{"desired", "previous", "candidate"} {
			exec(fmt.Sprintf("INSERT INTO self_config_retention(slot,snapshot_id) VALUES('%s','snp_rank_%d')", slot, i))
		}
		exec("INSERT INTO principals(id,kind,class,created_at) VALUES('mch_rank','machine','workload','2020-01-01T00:00:00Z'),('usr_rank','human','human','2020-01-01T00:00:00Z')")
		exec("INSERT INTO revision_pins(id,org_id,project_id,environment_id,workload_principal_id,snapshot_id,revision,authority_principal_id,expires_at,created_at,authorized_at,history_authorized,schema_override) VALUES('pin_rank','org_rank','prj_rank','env_rank_0','mch_rank','snp_rank_3',34,'usr_rank','2027-01-01T00:00:00Z','2020-01-01T00:00:00Z','2020-01-01T00:00:00Z',TRUE,FALSE)")
	}
	return collect, retained
}
