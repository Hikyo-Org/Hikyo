package isolation

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The instance-wide rotation counts /metrics and doctor read (#148, S2b-2): each
// condition moves exactly its own count, on both engines.
func TestRotationGaugesCountWhatNeedsAnOperator(t *testing.T) { forEngines(t, runRotationGauges) }

func runRotationGauges(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	runtime := store.NewRotationRuntime(db)
	gauges := func() store.RotationGauges {
		t.Helper()
		g, err := runtime.Gauges(tctx(t), x.now())
		if err != nil {
			t.Fatalf("gauges: %v", err)
		}
		return g
	}
	expect := func(label string, want store.RotationGauges) {
		t.Helper()
		if got := gauges(); got != want {
			t.Fatalf("%s: gauges = %+v, want %+v", label, got, want)
		}
	}
	expect("nothing has run", store.RotationGauges{})

	// A published run overlapping its predecessor needs nobody.
	if _, err := x.runNow(x.actor, rot.ID, false); err != nil {
		t.Fatalf("run-now: %v", err)
	}
	x.drain()
	run := x.openRunID(rot.ID)
	expect("a run in its overlap", store.RotationGauges{})

	execRaw(t, db, "UPDATE rotation_runs SET phase='revoke_refused' WHERE id='"+run+"'")
	expect("revoke refused", store.RotationGauges{RevokeRefused: 1})

	execRaw(t, db, "UPDATE rotation_runs SET phase='quarantine' WHERE id='"+run+"'")
	expect("quarantine", store.RotationGauges{Quarantined: 1})

	// An effect left unknown counts while its run is unsettled, and stops counting
	// once the run is done.
	execRaw(t, db, "UPDATE rotation_effects SET outcome='unknown' WHERE run_id='"+run+"' AND kind='mint'")
	expect("unknown effect on a quarantined run", store.RotationGauges{Quarantined: 1, UnknownOutcome: 1})
	execRaw(t, db, "UPDATE rotation_runs SET phase='done' WHERE id='"+run+"'")
	expect("settled run", store.RotationGauges{})

	// Due for longer than the grace with nothing started: overdue. A due rotation
	// inside the grace is not (the executor polls every few seconds).
	x.advance(24*time.Hour + store.RotationOverdueGrace - time.Minute)
	expect("due inside the grace", store.RotationGauges{})
	x.advance(2 * time.Minute)
	expect("due past the grace", store.RotationGauges{Overdue: 1})

	// A rotation a restore holds starts nothing, so it is held, not overdue.
	execRaw(t, db, "UPDATE rotations SET restore_hold=TRUE WHERE id='"+rot.ID+"'")
	expect("held after a restore", store.RotationGauges{RestoreHeld: 1})
	execRaw(t, db, "UPDATE rotations SET restore_hold=FALSE WHERE id='"+rot.ID+"'")

	// A paused rotation is not overdue, and its delivered credential still expires:
	// inside the horizon it is counted, outside it is not.
	execRaw(t, db, "UPDATE rotations SET state='paused', next_run_at=NULL WHERE id='"+rot.ID+"'")
	expect("paused with a credential far from expiring", store.RotationGauges{})
	until := mustParseStamp(t, x.state(rot.ID).CredentialExpiresAt)
	x.advance(until.Add(-3 * time.Hour).Sub(x.now()))
	expect("paused near the credential's expiry", store.RotationGauges{Expiring: 1})

	execRaw(t, db, "UPDATE rotations SET state='halted', halt_reason='retries_exhausted' WHERE id='"+rot.ID+"'")
	expect("halted", store.RotationGauges{Halted: 1, Expiring: 1})

	// A credential already past its expiry is not "expiring": the gauge counts only
	// what an operator can still extend in time.
	x.advance(4 * time.Hour)
	expect("halted with a lapsed credential", store.RotationGauges{Halted: 1})
	x.advance(-4 * time.Hour)

	// A deleted rotation is history: nothing about it needs an operator.
	execRaw(t, db, "UPDATE rotations SET state='disabled', halt_reason=NULL WHERE id='"+rot.ID+"'")
	expect("deleted", store.RotationGauges{})
}
