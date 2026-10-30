package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func runMoveReplacementUnsentReservations(t *testing.T, engine store.Engine) {
	for _, kind := range []string{"target", "origin"} {
		for _, state := range []string{"reserved", "owned", "dispatched", "prepared"} {
			t.Run(kind+"/"+state, func(t *testing.T) {
				db := repositoryRecoveryDB(t, engine)
				seedMovePolicy(t, db, false)
				execAdapter(t, db, `UPDATE adapter_outbox SET state='superseded',finished_at=created_at WHERE target_id='tgt_pause'`)
				execAdapter(t, db, `UPDATE adapter_targets SET active_job_id=NULL WHERE id='tgt_pause'`)
				initial := state
				if state == "prepared" {
					initial = "reserved"
				}
				execAdapter(t, db, fmt.Sprintf(`UPDATE adapter_ledger SET state='%s' WHERE target_id='tgt_pause'`, initial))
				now := time.Now().UTC()
				if err := movePolicyWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
					if kind == "origin" {
						_, err := r.Adapters().MoveOrigin(ctx, p, store.AdapterOriginMoveMutation{MoveID: "move_unsent", AdapterID: "adp_move", Origin: "https://next.example", PendingCredentialCiphertext: []byte("next-sealed"), AuthorityPrincipalID: "usr_744", At: now})
						return err
					}
					_, err := r.Adapters().MoveTarget(ctx, p, store.AdapterRouteMoveMutation{MoveID: "move_unsent", Target: movePolicyTarget("tgt_pause", "next"), ExpectedGeneration: 1, AuthorityPrincipalID: "usr_744", At: now})
					return err
				}); err != nil {
					t.Fatal(err)
				}
				runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
				claim := func(want adapter.JobKind) adapter.Job {
					t.Helper()
					job, ok, err := runtime.ClaimDue(t.Context(), "unsent-worker", now.Add(time.Second), now.Add(adapter.LeaseTime))
					if err != nil || !ok || job.Kind != want {
						t.Fatalf("claim %s: job=%+v ok=%t err=%v", want, job, ok, err)
					}
					return job
				}
				scrub := claim(adapter.Scrub)
				effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "DB_URL", Disposition: adapter.Delete}
				scrubJournal := runtime.Journal(scrub)
				if state == "prepared" {
					if err := scrubJournal.Prepare(t.Context(), effect, adapter.Reserved); err != nil {
						t.Fatal(err)
					}
					if err := scrubJournal.Finish(t.Context(), effect, adapter.Completion{Outcome: adapter.OutcomeUnknown, State: adapter.Dispatched, Finding: "crash_window"}); err != nil {
						t.Fatal(err)
					}
				}
				if err := runtime.Fail(t.Context(), scrub, 0, now, adapter.ErrOperatorReview); err != nil {
					t.Fatal(err)
				}
				err := movePolicyWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
					if kind == "origin" {
						_, err := r.Adapters().ReplaceMoveOrigin(ctx, p, "move_unsent", "https://replacement.example", []byte("replacement-sealed"), "usr_744", now)
						return err
					}
					_, err := r.Adapters().ReplaceMoveTarget(ctx, p, "move_unsent", movePolicyTarget("tgt_pause", "replacement"), "usr_744", now)
					return err
				})
				if state != "reserved" {
					if !errors.Is(err, adapter.ErrOperatorReview) || recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE route_move_id='move_unsent' AND kind='activate'`) != 0 {
						t.Fatalf("replacement escaped held custody: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("replace route with unsent reservation: %v", err)
				}
				if err := scrubJournal.ReleaseReservation(t.Context(), effect); !errors.Is(err, adapter.ErrSuperseded) {
					t.Fatalf("superseded scrub released reservation: %v", err)
				}
				activation := claim(adapter.Activate)
				stale := activation
				stale.LeaseOwner = "stale-worker"
				connection := adapter.Connection{Version: "fixture-contract", DestinationID: 84}
				if err := runtime.Activate(t.Context(), stale, connection, now); !errors.Is(err, adapter.ErrSuperseded) || recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_pause' AND state='reserved'`) != 1 {
					t.Fatalf("stale activation changed unsent custody: %v", err)
				}
				if err := runtime.Activate(t.Context(), activation, connection, now); err != nil {
					t.Fatal(err)
				}
				if recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_pause'`) != 0 {
					t.Fatal("activation retained old-route reservation")
				}
				converge := claim(adapter.Converge)
				effect.Disposition = adapter.Create
				if got, err := runtime.Journal(converge).Reserve(t.Context(), effect); err != nil || got != adapter.Reserved {
					t.Fatalf("new-route reserve: state=%s err=%v", got, err)
				}
				origin := "https://old.example"
				if kind == "origin" {
					origin = "https://replacement.example"
				}
				if recoveryCount(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_pause' AND normalized_name='DB_URL' AND state='reserved' AND provider_origin='%s' AND destination_id=84`, origin)) != 1 {
					t.Fatal("new reservation was bound to the old route")
				}
			})
		}
	}
}

func TestMoveReplacementUnsentReservationsSQLite(t *testing.T) {
	runMoveReplacementUnsentReservations(t, store.EngineSQLite)
}

func TestMoveReplacementUnsentReservationsPostgres(t *testing.T) {
	runMoveReplacementUnsentReservations(t, store.EnginePostgres)
}
