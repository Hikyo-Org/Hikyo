package isolation

import (
	"database/sql"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestAdapterAdoptionConflictCountBindsConsentAndOwningChain(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		execRealAdoption(t, db, `INSERT INTO adapter_conflicts(id,artifact_id,org_id,project_id,environment_id,target_id,destination_id,repository_id,target_generation,surface,effective_name,observed_provider_version,created_at) VALUES ('conf_review','artifact_review','org_gitlab','prj_gitlab','env_gitlab_e2e','tgt_gitlab_a',42,0,1,'secret','P_TOKEN',3,'2026-08-17T00:00:00Z')`)
		// PostgreSQL params are value-only and map one-for-one to SQLite here.
		own := pggen.AdapterAdoptionConflictCountParams{ArtifactID: "artifact_review", TargetID: "tgt_gitlab_a", ChainOrg: "org_gitlab", ChainProject: "prj_gitlab", EnvironmentID: "env_gitlab_e2e", RepositoryID: 0, DestinationID: 42, Generation: 1, Surface: "secret", EffectiveName: "P_TOKEN", RequireVersion: 1}
		read := func(p pggen.AdapterAdoptionConflictCountParams) (int64, error) {
			if db.Engine() == store.EnginePostgres {
				return pggen.New(db.PG()).AdapterAdoptionConflictCount(t.Context(), p)
			}
			n, err := sqlitegen.New(db.SQLiteWrite()).AdapterAdoptionConflictCount(t.Context(), sqlitegen.AdapterAdoptionConflictCountParams{ArtifactID: p.ArtifactID, TargetID: p.TargetID, ChainOrg: p.ChainOrg, ChainProject: p.ChainProject, EnvironmentID: p.EnvironmentID, RepositoryID: p.RepositoryID, DestinationID: p.DestinationID, Generation: p.Generation, Surface: p.Surface, EffectiveName: p.EffectiveName, RequireVersion: int64(p.RequireVersion)})
			return int64(n), err
		}
		if n, err := read(own); err != nil || n != 1 {
			t.Fatalf("own positive=%d %v", n, err)
		}
		for _, mutate := range []func(*pggen.AdapterAdoptionConflictCountParams){
			func(p *pggen.AdapterAdoptionConflictCountParams) { p.ChainOrg = "org_foreign" },
			func(p *pggen.AdapterAdoptionConflictCountParams) { p.ChainProject = "prj_foreign" },
			func(p *pggen.AdapterAdoptionConflictCountParams) { p.EnvironmentID = "env_gitlab_second" },
			func(p *pggen.AdapterAdoptionConflictCountParams) { p.TargetID = "tgt_gitlab_b" },
			func(p *pggen.AdapterAdoptionConflictCountParams) { p.ArtifactID = "other_artifact" },
			func(p *pggen.AdapterAdoptionConflictCountParams) { p.RepositoryID++ },
			func(p *pggen.AdapterAdoptionConflictCountParams) { p.DestinationID++ },
			func(p *pggen.AdapterAdoptionConflictCountParams) { p.Generation++ },
			func(p *pggen.AdapterAdoptionConflictCountParams) { p.Surface = "config" },
			func(p *pggen.AdapterAdoptionConflictCountParams) { p.EffectiveName = "OTHER" },
		} {
			foreign := own
			mutate(&foreign)
			if n, err := read(foreign); err != nil || n != 0 {
				t.Fatalf("foreign consent=%d %v", n, err)
			}
		}
		execRealAdoption(t, db, `UPDATE adapter_conflicts SET observed_provider_version=NULL WHERE id='conf_review'`)
		if n, err := read(own); err != nil || n != 0 {
			t.Fatalf("missing version=%d %v", n, err)
		}
		own.RequireVersion = 0
		if n, err := read(own); err != nil || n != 1 {
			t.Fatalf("non-versioned provider consent=%d %v", n, err)
		}
		execRealAdoption(t, db, `UPDATE adapter_conflicts SET adopted_at='2026-08-17T00:00:01Z' WHERE id='conf_review'`)
		if n, err := read(own); err != nil || n != 0 {
			t.Fatalf("consumed consent=%d %v", n, err)
		}
	})
}

func TestAdapterRetireCredentialJobsBindsAdapterAndOwningChain(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		for _, state := range []string{"running", "succeeded"} {
			execRealAdoption(t, db, `INSERT INTO adapter_outbox(id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,next_attempt_at,state,created_at) VALUES ($1,'org_gitlab','prj_gitlab','env_gitlab_e2e','tgt_gitlab_a','converge','usr_gitlab',1,$1,'2026-08-17T00:00:00Z',$2,'2026-08-17T00:00:00Z')`, "job_review_"+state, state)
		}
		at := time.Now().UTC()
		retire := func(scope adapterQueryScope, id string) (int64, error) {
			if db.Engine() == store.EnginePostgres {
				return pggen.New(db.PG()).AdapterRetireCredentialJobs(t.Context(), pggen.AdapterRetireCredentialJobsParams{At: pgtype.Timestamptz{Time: at, Valid: true}, ChainOrg: scope.org, ChainProject: scope.project, AdapterID: id})
			}
			return sqlitegen.New(db.SQLiteWrite()).AdapterRetireCredentialJobs(t.Context(), sqlitegen.AdapterRetireCredentialJobsParams{At: sql.NullString{String: at.Format(time.RFC3339Nano), Valid: true}, ChainOrg: scope.org, ChainProject: scope.project, AdapterID: id})
		}
		own := adapterQueryScope{org: "org_gitlab", project: "prj_gitlab"}
		for _, foreign := range []adapterQueryScope{{org: "org_foreign", project: own.project}, {org: own.org, project: "prj_foreign"}} {
			if n, err := retire(foreign, "adp_gitlab"); err != nil || n != 0 {
				t.Fatalf("foreign retirement=%d %v", n, err)
			}
		}
		if n, err := retire(own, "other_adapter"); err != nil || n != 0 {
			t.Fatalf("foreign adapter retirement=%d %v", n, err)
		}
		if n, err := retire(own, "adp_gitlab"); err != nil || n != 2 {
			t.Fatalf("own pending retirement=%d %v", n, err)
		}
		if queryString(t, db, `SELECT state FROM adapter_outbox WHERE id='job_review_succeeded'`) != "succeeded" || queryInt(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE state='superseded'`) != 2 {
			t.Fatal("retirement rewrote terminal outcome or missed pending jobs")
		}
	})
}

func TestAdapterWorkerLockCustodyTargetBindsLiveLeaseAndOwningChain(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		// More precision than storage supports reproduces Linux clocks on every
		// platform, rather than relying on time.Now's platform-specific precision.
		at := time.Date(2026, 10, 1, 12, 0, 0, 123456789, time.UTC)
		runtime := generatedAdapterRuntime(db)
		job, ok, err := runtime.ClaimDue(t.Context(), "review_lock", at, at.Add(adapter.LeaseTime))
		if err != nil || !ok {
			t.Fatalf("claim=%v %v", ok, err)
		}
		leaseExpires := store.CanonTime(at.Add(adapter.LeaseTime))
		if db.Engine() == store.EngineSQLite {
			if got := queryString(t, db, `SELECT lease_expires_at FROM adapter_outbox WHERE id='job_gitlab'`); got != leaseExpires.Format("2006-01-02T15:04:05.000000Z") {
				t.Fatalf("stored lease timestamp=%q; want canonical fixed-width deadline", got)
			}
		}
		lock := func(job adapter.Job, at time.Time) (int64, error) {
			// Match the production custody wrapper: both engines truncate to
			// microseconds; SQLite additionally requires fixed-width text order.
			at = store.CanonTime(at)
			if db.Engine() == store.EnginePostgres {
				return pggen.New(db.PG()).AdapterWorkerLockCustodyTarget(t.Context(), pggen.AdapterWorkerLockCustodyTargetParams{TargetID: job.TargetID, ChainOrg: job.OrgID, ChainProject: job.ProjectID, ChainEnv: job.EnvironmentID, Generation: job.Generation, JobKind: string(job.Kind), JobID: job.ID, LeaseOwner: pgtype.Text{String: job.LeaseOwner, Valid: true}, Now: pgtype.Timestamptz{Time: at, Valid: true}})
			}
			return sqlitegen.New(db.SQLiteWrite()).AdapterWorkerLockCustodyTarget(t.Context(), sqlitegen.AdapterWorkerLockCustodyTargetParams{TargetID: job.TargetID, ChainOrg: job.OrgID, ChainProject: job.ProjectID, ChainEnv: job.EnvironmentID, Generation: job.Generation, JobKind: string(job.Kind), JobID: job.ID, LeaseOwner: sql.NullString{String: job.LeaseOwner, Valid: true}, Now: sql.NullString{String: at.Format("2006-01-02T15:04:05.000000Z"), Valid: true}})
		}
		if n, err := lock(job, at); err != nil || n != 1 {
			t.Fatalf("own lock=%d %v", n, err)
		}
		for _, mutate := range []func(*adapter.Job){
			func(j *adapter.Job) { j.OrgID = "org_foreign" },
			func(j *adapter.Job) { j.ProjectID = "prj_foreign" },
			func(j *adapter.Job) { j.EnvironmentID = "env_gitlab_second" },
			func(j *adapter.Job) { j.TargetID = "tgt_gitlab_b" },
			func(j *adapter.Job) { j.ID = "other_job" },
			func(j *adapter.Job) { j.LeaseOwner = "other_owner" },
			func(j *adapter.Job) { j.Generation++ },
			func(j *adapter.Job) { j.Kind = adapter.Scrub },
		} {
			foreign := job
			mutate(&foreign)
			if n, err := lock(foreign, at); err != nil || n != 0 {
				t.Fatalf("foreign lock=%d %v", n, err)
			}
		}
		for _, boundary := range []struct {
			name string
			at   time.Time
			want int64
		}{
			{"before_expiry", leaseExpires.Add(-time.Microsecond), 1},
			{"exact_expiry", leaseExpires, 0},
			{"after_expiry", leaseExpires.Add(time.Microsecond), 0},
			{"untruncated_expiry", at.Add(adapter.LeaseTime), 0},
		} {
			if n, err := lock(job, boundary.at); err != nil || n != boundary.want {
				t.Fatalf("%s lock=%d %v; want %d", boundary.name, n, err, boundary.want)
			}
		}
		execRealAdoption(t, db, `UPDATE adapter_targets SET paused_at=$1 WHERE id='tgt_gitlab_a'`, at.Format(time.RFC3339Nano))
		if n, err := lock(job, at); err != nil || n != 0 {
			t.Fatalf("paused lock=%d %v", n, err)
		}
		execRealAdoption(t, db, `UPDATE adapter_targets SET paused_at=NULL WHERE id='tgt_gitlab_a'`)
		execRealAdoption(t, db, `UPDATE adapters SET state='tombstoned' WHERE id='adp_gitlab'`)
		if n, err := lock(job, at); err != nil || n != 0 {
			t.Fatalf("tombstoned parent lock=%d %v", n, err)
		}
	})
}
