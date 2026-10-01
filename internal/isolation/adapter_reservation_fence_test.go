package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/forgejo"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type reservationListWaitAPI struct {
	forgejo.API
	listed, resume chan struct{}
}

func (api reservationListWaitAPI) ResolveDestination(context.Context, adapter.Destination) (int64, error) {
	return 42, nil
}

func (api reservationListWaitAPI) ListSecretNames(ctx context.Context, _ adapter.Destination) ([]string, error) {
	close(api.listed)
	select {
	case <-api.resume:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func claimReservationJob(t *testing.T, runtime *store.AdapterRuntime) adapter.Job {
	t.Helper()
	now := time.Now().UTC()
	job, claimed, err := runtime.ClaimDue(t.Context(), "reservation_worker", now, now.Add(adapter.LeaseTime))
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	return job
}

func TestAdapterProviderListWaitCannotResurrectKeepRemoteRemovedCustody(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		runtime := generatedAdapterRuntime(db)
		job := claimReservationJob(t, runtime)
		api := reservationListWaitAPI{listed: make(chan struct{}), resume: make(chan struct{})}
		module := forgejo.Module{API: api}
		finished := make(chan error, 1)
		go func() {
			_, err := module.Sync(t.Context(), adapter.SyncRequest{Target: adapter.Target{ID: job.TargetID, NamePrefix: "P_", Destination: adapter.Destination{Kind: adapter.Repository, Owner: "team", Name: "api-a", NumericID: 42}}, Manifest: []adapter.ManifestEntry{{KeyID: "key_gitlab_move", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: "test-value"}}}, runtime.Journal(job))
			finished <- err
		}()
		select {
		case <-api.listed:
		case <-time.After(10 * time.Second):
			t.Fatal("provider list never entered")
		}
		svc := service.Adapters{DB: db}
		if _, err := svc.RemoveTarget(t.Context(), service.LocalPrincipal("usr_gitlab"), domain.Scope{Org: "org_gitlab", Project: "prj_gitlab"}, job.TargetID, true); err != nil {
			close(api.resume)
			<-finished
			t.Fatal(err)
		}
		close(api.resume)
		if err := <-finished; !errors.Is(err, adapter.ErrSuperseded) {
			t.Fatalf("old provider list completion = %v", err)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_gitlab_a' AND state<>'released'`); got != 0 {
			t.Fatalf("removed target resurrected %d custody rows", got)
		}
		if err := gitLabMoveWrite(t, db, func(ctx context.Context, repos store.Repos, proof authz.Proof) error {
			_, err := repos.Adapters().AddTarget(ctx, proof, store.AdapterTargetUpdate{Target: store.AdapterTargetMutation{ID: "tgt_replacement", AdapterID: "adp_gitlab", EnvironmentID: "env_gitlab_second", DestinationKind: "repository", DestinationOwner: "team", DestinationName: "api-a", DestinationID: 42, DestinationScope: "production", NamePrefix: "P_", KeyIDs: []string{"key_gitlab_move"}}, AuthorityPrincipalID: "usr_gitlab", At: time.Now().UTC()})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Enqueue(t.Context(), adapter.Job{OrgID: job.OrgID, ProjectID: job.ProjectID, EnvironmentID: "env_gitlab_second", TargetID: "tgt_replacement", Kind: adapter.Converge, AuthorityPrincipal: job.AuthorityPrincipal}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		replacement := claimReservationJob(t, runtime)
		if state, err := runtime.Journal(replacement).Reserve(t.Context(), adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_TOKEN", Disposition: adapter.Create}); err != nil || state != adapter.Reserved {
			t.Fatalf("replacement same-path reservation = %s, %v", state, err)
		}
	})
}

func TestAdapterStaleRefuseCannotDeleteNewGenerationReservation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		runtime := generatedAdapterRuntime(db)
		old := claimReservationJob(t, runtime)
		effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_TOKEN", Disposition: adapter.Create}
		if _, err := runtime.Journal(old).Reserve(t.Context(), effect); err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.Enqueue(t.Context(), adapter.Job{OrgID: old.OrgID, ProjectID: old.ProjectID, EnvironmentID: old.EnvironmentID, TargetID: old.TargetID, Kind: adapter.Converge, AuthorityPrincipal: old.AuthorityPrincipal}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		current := claimReservationJob(t, runtime)
		if state, err := runtime.Journal(current).Reserve(t.Context(), effect); err != nil || state != adapter.Reserved {
			t.Fatalf("current reservation = %s, %v", state, err)
		}
		if err := runtime.Journal(old).Refuse(t.Context(), effect); !errors.Is(err, adapter.ErrSuperseded) {
			t.Fatalf("stale refusal = %v", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_gitlab_a' AND state='reserved'`) != 1 || queryInt(t, db, `SELECT COUNT(*) FROM adapter_conflicts WHERE target_id='tgt_gitlab_a'`) != 0 {
			t.Fatal("stale refusal changed newer reservation or conflict history")
		}
		if err := runtime.Journal(current).Refuse(t.Context(), effect); err != nil {
			t.Fatalf("current refusal = %v", err)
		}
	})
}

type preparationFailureModule struct {
	fanoutModule
	explicit bool
	prepared int
}

func (m *preparationFailureModule) Sync(ctx context.Context, _ adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_FAIL", Disposition: adapter.Create, RequireExplicitRecovery: m.explicit}
	prior, err := journal.Reserve(ctx, effect)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	if err := journal.Prepare(ctx, effect, prior); err != nil {
		return adapter.SyncResult{}, err
	}
	m.prepared++
	return adapter.SyncResult{}, errors.New("unexpected successful preparation")
}

func TestAdapterTransientPreparationSQLFailureRollsBackAndRetries(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		if db.Engine() == store.EngineSQLite {
			execRaw(t, db, `CREATE TRIGGER preparation_unavailable BEFORE UPDATE ON adapter_ledger WHEN NEW.normalized_name='P_FAIL' AND NEW.state='dispatched' BEGIN SELECT RAISE(ABORT,'preparation datastore unavailable'); END`)
		} else {
			execRaw(t, db, `CREATE FUNCTION preparation_unavailable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.normalized_name='P_FAIL' AND NEW.state='dispatched' THEN RAISE EXCEPTION 'preparation datastore unavailable'; END IF; RETURN NEW; END $$`)
			execRaw(t, db, `CREATE TRIGGER preparation_unavailable BEFORE UPDATE ON adapter_ledger FOR EACH ROW EXECUTE FUNCTION preparation_unavailable()`)
		}
		runtime := generatedAdapterRuntime(db)
		clock := time.Now().UTC()
		module := &preparationFailureModule{}
		worker := adapter.Worker{Store: runtime, Loader: deadlineRuntimeLoader{module: module}, ID: "preparation_worker", Now: func() time.Time { return clock }}
		for attempt, explicit := range []bool{false, true} {
			module.explicit = explicit
			if worked, err := worker.RunOnce(t.Context()); !worked || err != nil {
				t.Fatalf("SQL failure retry explicit=%v: %v, %v", explicit, worked, err)
			}
			if module.prepared != 0 || queryInt(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE id='job_gitlab' AND state='queued' AND lease_owner IS NULL`) != 1 || queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_gitlab_a' AND effective_name='P_FAIL' AND state='reserved'`) != 1 || queryInt(t, db, `SELECT COUNT(*) FROM adapter_effects WHERE target_id='tgt_gitlab_a'`) != 0 || queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_gitlab_a' AND provider_lease_job_id IS NULL`) != 1 {
				t.Fatalf("SQL failure attempt %d became terminal or leaked provider custody", attempt+1)
			}
			clock = clock.Add(2 * time.Minute)
		}
	})
}
