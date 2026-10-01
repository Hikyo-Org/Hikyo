package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type blockedPreparationModule struct {
	fanoutModule
	ready, proceed chan struct{}
	returned       chan error
}

func (m *blockedPreparationModule) Sync(ctx context.Context, _ adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_TIMEOUT", Disposition: adapter.Create}
	prior, err := journal.Reserve(ctx, effect)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	close(m.ready)
	select {
	case <-m.proceed:
	case <-ctx.Done():
		return adapter.SyncResult{}, ctx.Err()
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	err = journal.Prepare(deadlineCtx, effect, prior)
	m.returned <- err
	return adapter.SyncResult{}, err
}

func TestAdapterPostgresPreparationDeadlineRollsBackThenQueuesRetry(t *testing.T) {
	db := seededDB(t, openPostgres)
	seedGitLabMoves(t, db, "staging")
	runtime := generatedAdapterRuntime(db)
	module := blockedPreparationModule{ready: make(chan struct{}), proceed: make(chan struct{}), returned: make(chan error, 1)}
	worker := adapter.Worker{Store: runtime, Loader: deadlineRuntimeLoader{module: &module}, ID: "deadline_preparation"}
	finished := make(chan error, 1)
	go func() {
		_, err := worker.RunOnce(t.Context())
		finished <- err
	}()
	select {
	case <-module.ready:
	case <-time.After(10 * time.Second):
		t.Fatal("preparation worker never reserved")
	}
	blocked, err := db.PG().Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Rollback(context.Background())
	if _, err := blocked.Exec(t.Context(), `SELECT id FROM adapter_outbox WHERE id='job_gitlab' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	close(module.proceed)
	select {
	case err := <-module.returned:
		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, adapter.ErrSuperseded) {
			t.Fatalf("actual blocked Prepare SQL error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual blocked Prepare ignored its deadline")
	}
	if err := blocked.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("durable retry after Prepare timeout = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not yield after blocked Prepare")
	}
	if queryInt(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE id='job_gitlab' AND state='queued' AND attempt_count=1 AND lease_owner IS NULL`) != 1 || queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_gitlab_a' AND provider_lease_job_id IS NULL`) != 1 || queryInt(t, db, `SELECT COUNT(*) FROM adapter_effects WHERE target_id='tgt_gitlab_a'`) != 0 || queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_gitlab_a' AND state='reserved'`) != 1 {
		t.Fatal("Prepare deadline discarded its retry or leaked uncommitted custody")
	}
}

func TestAdapterPostgresReserveAndKeepRemoteRemovalShareTargetFirstFence(t *testing.T) {
	db := seededDB(t, openPostgres)
	seedGitLabMoves(t, db, "staging")
	runtime := generatedAdapterRuntime(db)
	job := claimReservationJob(t, runtime)
	held, err := db.PG().Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(context.Background())
	if _, err := held.Exec(t.Context(), `SELECT id FROM adapter_targets WHERE id='tgt_gitlab_a' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	reserved, removed := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := runtime.Journal(job).Reserve(ctx, adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_RACE", Disposition: adapter.Create})
		reserved <- err
	}()
	go func() {
		_, err := (&service.Adapters{DB: db}).RemoveTarget(ctx, service.LocalPrincipal("usr_gitlab"), domain.Scope{Org: "org_gitlab", Project: "prj_gitlab"}, job.TargetID, true)
		removed <- err
	}()
	// Prove both canonical transactions reached the held target, rather than
	// merely scheduling two goroutines that could run sequentially.
	blockedDeadline := time.Now().Add(3 * time.Second)
	for queryInt(t, db, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%adapter_targets%'`) < 2 {
		if time.Now().After(blockedDeadline) {
			t.Fatal("Reserve/removal did not both wait on the held target row")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := held.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-reserved; err != nil && !errors.Is(err, adapter.ErrSuperseded) {
		t.Fatalf("concurrent old Reserve failed outside its fence: %v", err)
	}
	if err := <-removed; err != nil {
		t.Fatalf("concurrent canonical removal = %v", err)
	}
	if queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_gitlab_a' AND state<>'released'`) != 0 {
		t.Fatal("target-first interleaving resurrected removed custody")
	}
}
