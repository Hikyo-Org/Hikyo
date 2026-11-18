package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// holdRestored runs the restore closure's rotation step on its own transaction.
func holdRestored(t *testing.T, db *store.DB) {
	t.Helper()
	if err := tx.Write(tctx(t), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return az.HoldRestoredRotations(ctx, time.Now().UTC())
	}); err != nil {
		t.Fatalf("hold restored rotations: %v", err)
	}
}

// A restore holds every rotation it brings back, whatever its state; a held
// rotation starts nothing, is refused by resume and run-now, lets a run already
// past its publish finish its revoke, and a human release with the publish
// ceremony starts the schedule again, audited (#148, S2b-2).
func TestRotationRestoreHold(t *testing.T) { forEngines(t, runRotationRestoreHold) }

func runRotationRestoreHold(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.cycle(rot.ID)
	role1 := x.lastRun(rot.ID, "candidate_handle")
	x.advance(24 * time.Hour)
	x.drain()
	run2 := x.openRunID(rot.ID)
	if got := x.phase(run2); got != "overlap" {
		t.Fatalf("run 2 is %s, want overlap", got)
	}
	other := x.create(rotSecondKey, rotSecondUser)
	execRaw(t, db, `UPDATE rotations SET state='halted', next_run_at=NULL, halt_reason='authority lost' WHERE id='`+other.ID+`'`)

	holdRestored(t, db)
	held := func(id string) bool {
		return queryInt(t, db, `SELECT COUNT(*) FROM rotations WHERE id='`+id+`' AND restore_hold`) == 1
	}
	if !held(rot.ID) || !held(other.ID) {
		t.Fatal("a restored active or halted rotation was not held")
	}

	// Refused with the named error, and nothing is recorded as requested.
	human := func() service.Actor { return x.session(alice, true) }
	if _, err := x.runNow(human(), rot.ID, false); !errors.Is(err, service.ErrRotationRestoreHeld) {
		t.Fatalf("run-now while held = %v", err)
	}
	if got := x.manualRequest(rot.ID); got != "" {
		t.Fatalf("a refused run-now left request %q", got)
	}
	if _, err := x.svc.Resume(ctx, human(), x.scope, service.RotationStateRequest{RotationID: other.ID}); !errors.Is(err, service.ErrRotationRestoreHeld) {
		t.Fatalf("resume while held = %v", err)
	}

	// The in-flight overlap finishes: its revoke drops the superseded role. No
	// further run starts, nothing is extended, though the schedule is past due.
	mints := x.effects("mint", "success")
	x.advance(time.Hour + time.Minute)
	x.drain()
	if got := x.phase(run2); got != "done" || x.ctl.has(role1) {
		t.Fatalf("held rotation's overlap run = %s (superseded role present: %v)", got, x.ctl.has(role1))
	}
	x.advance(25 * time.Hour)
	x.drain()
	if x.openRunID(rot.ID) != run2 || x.effects("mint", "success") != mints || x.ctl.count("extend:") != 0 {
		t.Fatalf("a held rotation acted: open=%q mints=%d extends=%d", x.openRunID(rot.ID), x.effects("mint", "success"), x.ctl.count("extend:"))
	}

	// Release: a person with the ceremony, one generation, one audit row.
	req := service.RotationStateRequest{RotationID: rot.ID}
	if _, err := x.svc.ReleaseHold(ctx, x.session(alice, false), x.scope, req); !errors.Is(err, service.ErrNoReauthWindow) || !held(rot.ID) {
		t.Fatalf("release without the publish ceremony = %v (held: %v)", err, held(rot.ID))
	}
	req.ExpectedGeneration = 99
	if _, err := x.svc.ReleaseHold(ctx, human(), x.scope, req); !errors.Is(err, service.ErrRotationStale) {
		t.Fatalf("release at the wrong generation = %v", err)
	}
	req.ExpectedGeneration = 0
	before := x.state(rot.ID)
	view, err := x.svc.ReleaseHold(ctx, human(), x.scope, req)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if view.RestoreHold || view.Generation != before.Generation || held(rot.ID) {
		t.Fatalf("after release: hold=%v generation %d (was %d)", view.RestoreHold, view.Generation, before.Generation)
	}
	if _, err := x.svc.ReleaseHold(ctx, human(), x.scope, req); !errors.Is(err, service.ErrRotationNotHeld) {
		t.Fatalf("second release = %v", err)
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE payload LIKE '%hold_released%'`); n != 1 {
		t.Fatalf("hold_released audit rows = %d, want 1", n)
	}
	// The halted rotation stays halted: release never resumes.
	if _, err := x.svc.ReleaseHold(ctx, human(), x.scope, service.RotationStateRequest{RotationID: other.ID}); err != nil {
		t.Fatalf("release of the halted rotation: %v", err)
	}
	if got := queryString(t, db, `SELECT state FROM rotations WHERE id='`+other.ID+`'`); got != store.RotationStateHalted {
		t.Fatalf("released halted rotation is now %s", got)
	}
	// The schedule runs again.
	x.advance(time.Hour)
	x.drain()
	if x.openRunID(rot.ID) == run2 {
		t.Fatal("the released rotation never started its overdue run")
	}
}

// A restored archive written before migration 00094 has the rotations table but no
// hold column: its active rotations are paused instead, and nothing else changes.
func TestRotationRestoreHoldFallbackPausesWithoutTheColumn(t *testing.T) {
	forEngines(t, runRotationRestoreHoldFallback)
}

func runRotationRestoreHoldFallback(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	other := x.create(rotSecondKey, rotSecondUser)
	execRaw(t, db, `UPDATE rotations SET state='halted', next_run_at=NULL, halt_reason='authority lost' WHERE id='`+other.ID+`'`)
	execRaw(t, db, `ALTER TABLE rotations DROP COLUMN restore_hold`)
	holdRestored(t, db)
	if got := queryString(t, db, `SELECT state FROM rotations WHERE id='`+rot.ID+`'`); got != store.RotationStatePaused {
		t.Fatalf("active rotation of a pre-00094 archive is %s, want paused", got)
	}
	if got := queryString(t, db, `SELECT state FROM rotations WHERE id='`+other.ID+`'`); got != store.RotationStateHalted {
		t.Fatalf("halted rotation of a pre-00094 archive is %s, want halted", got)
	}
}

// Releasing a restore hold lifts only the rotation-level hold. A run held at
// revoke_refused because the keys serve other values stays exactly as it was: no
// drop, no revoke effect, no new run.
func TestRotationReleaseLeavesARefusedRunAlone(t *testing.T) {
	forEngines(t, runRotationReleaseLeavesRefusedRun)
}

func runRotationReleaseLeavesRefusedRun(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.cycle(rot.ID)
	role1 := x.lastRun(rot.ID, "candidate_handle")
	x.advance(24 * time.Hour)
	x.drain()
	run2 := x.openRunID(rot.ID)
	local := service.LocalPrincipal(alice)
	values := &service.Values{DB: db, Keyring: probeKeyring(t, db)}
	var staged []string
	for name, value := range map[string]string{"ROT_DB_USER": "manual_user", "ROT_DB_PASSWORD": "manual-pw-value"} {
		change, err := values.Set(tctx(t), local, x.scope, name, value, nil)
		if err != nil {
			t.Fatalf("stage %s: %v", name, err)
		}
		staged = append(staged, change.VersionID)
	}
	if _, err := x.revisions().PublishPlanned(tctx(t), local, x.scope, service.PublishRequest{VersionIDs: staged}); err != nil {
		t.Fatalf("manual publish: %v", err)
	}
	x.advance(time.Hour + time.Minute)
	x.drain()
	if got := x.phase(run2); got != "revoke_refused" {
		t.Fatalf("phase = %s, want revoke_refused", got)
	}
	runs := x.runPhases(rot.ID)
	lastErr := x.lastRun(rot.ID, "last_error")

	holdRestored(t, db)
	if _, err := x.svc.ReleaseHold(tctx(t), x.session(alice, true), x.scope, service.RotationStateRequest{RotationID: rot.ID}); err != nil {
		t.Fatalf("release: %v", err)
	}
	x.advance(10 * time.Hour)
	x.drain()
	if got := x.phase(run2); got != "revoke_refused" || x.lastRun(rot.ID, "last_error") != lastErr || x.runPhases(rot.ID) != runs {
		t.Fatalf("release changed the refused run: phase %s, runs %q (was %q)", got, x.runPhases(rot.ID), runs)
	}
	if !x.ctl.has(role1) || x.ctl.count("drop:"+role1) != 0 || x.effects("revoke", "success") != 0 {
		t.Fatal("release dropped or revoked the superseded role")
	}
}

// A contained authority principal loads no grants, so the rotation's Gate refuses
// at its next start and halts it; releasing the principal never resumes it
// (#148, S2b-2: containment goes through the existing authority seam, and the
// ledger enumeration of rotations awaits an owner decision).
func TestRotationHaltsWhenItsAuthorityIsContained(t *testing.T) {
	forEngines(t, runRotationHaltsWhenAuthorityContained)
}

func runRotationHaltsWhenAuthorityContained(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.cycle(rot.ID)
	mints := x.effects("mint", "success")
	runs := x.runPhases(rot.ID)

	execRaw(t, db, `UPDATE principals SET containment_state='contained' WHERE id='`+string(alice)+`'`)
	x.advance(25 * time.Hour)
	x.drain()
	state := func() string { return queryString(t, db, `SELECT state FROM rotations WHERE id='`+rot.ID+`'`) }
	if got := state(); got != store.RotationStateHalted || x.effects("mint", "success") != mints || x.runPhases(rot.ID) != runs {
		t.Fatalf("contained authority: state=%s mints=%d runs=%q (was %d, %q)", got, x.effects("mint", "success"), x.runPhases(rot.ID), mints, runs)
	}

	execRaw(t, db, `UPDATE principals SET containment_state='none' WHERE id='`+string(alice)+`'`)
	x.advance(25 * time.Hour)
	x.drain()
	if got := state(); got != store.RotationStateHalted || x.effects("mint", "success") != mints {
		t.Fatalf("release resumed the rotation: state=%s mints=%d", got, x.effects("mint", "success"))
	}
}
