package isolation

import (
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// S2b-2 review round 4.

// A recorded authority that holds edit, publish and reveal only through member
// rules narrowed to the rotation's two keys is a valid authority: create accepts
// it and resume and release-hold treat it as holding the formula. The executor
// must therefore start, publish and settle its runs as that authority too, rather
// than halting the rotation at the Gate because a keyless publish check can never
// be satisfied by a key-narrowed rule.
func TestRotationExecutorRunsAKeyNarrowedAuthority(t *testing.T) {
	forEngines(t, runRotationExecutorRunsAKeyNarrowedAuthority)
}

func runRotationExecutorRunsAKeyNarrowedAuthority(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	rotationHuman(t, db, rotNarrow, "read")
	rules := &service.Rules{DB: db}
	envMode, envs := onlyEnvs(x.env)
	for _, c := range []domain.Capability{domain.CapRead, domain.CapEdit, domain.CapPublish, domain.CapReveal} {
		if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{
			Target: rotNarrow, Capability: c, Org: orgA,
			Where: whereIn(envMode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: rotPasswordKey}, domain.RuleKeyItem{KeyID: rotUsernameKey}),
		}); err != nil {
			t.Fatalf("create %s rule: %v", c, err)
		}
	}
	rot, err := x.svc.Create(ctx, x.session(rotNarrow, true), x.scope, x.request(rotPasswordKey, rotUsernameKey))
	if err != nil {
		t.Fatalf("create through key-narrowed rules: %v", err)
	}
	if rot.AuthorityPrincipalID != string(rotNarrow) {
		t.Fatalf("authority = %q, want the narrowed principal", rot.AuthorityPrincipalID)
	}

	x.advance(24*time.Hour + time.Minute)
	x.drain()
	if got := x.state(rot.ID); got.State != store.RotationStateActive {
		t.Fatalf("rotation is %s (halt reason %q) after its first due time, want active", got.State, queryString(t, db, "SELECT COALESCE(halt_reason, '') FROM rotations WHERE id='"+rot.ID+"'"))
	}
	if n := x.publishedRuns(rot.ID); n != 1 {
		t.Fatalf("published runs = %d (phases %q), want 1: a key-narrowed authority must be able to publish", n, x.runPhases(rot.ID))
	}
	x.advance(time.Hour + time.Minute)
	x.drain()
	if got := x.runPhases(rot.ID); got != "done," {
		t.Fatalf("run phases = %q, want the run settled", got)
	}
}

// A restore hold that lands after a run was claimed but before its publish
// refuses the publish (errRotationHeld) and fails the run, but must leave the
// held rotation exactly as it was: not halted, not rescheduled. The run was
// loaded before the hold, so the failure planning still sees an active rotation;
// the settle statements themselves must refuse a held row.
func TestRotationHoldDuringAClaimedRunLeavesTheRotationAlone(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		name := "retry"
		if exhausted {
			name = "would halt"
		}
		t.Run(name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) { runRotationHoldDuringAClaimedRun(t, db, exhausted) })
		})
	}
}

func runRotationHoldDuringAClaimedRun(t *testing.T, db *store.DB, exhausted bool) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	if exhausted {
		// One attempt short of the cap: the run the hold interrupts is the last one,
		// so its failure plan is a halt.
		for i := range 11 {
			id := fmt.Sprintf("run_r4_failed_%d", i)
			execRaw(t, db, `INSERT INTO rotation_runs (id, org_id, project_id, environment_id, rotation_id, generation, run_trigger, phase, candidate_handle, revocation_delay_seconds, started_at, phase_at, next_attempt_at, last_error) VALUES ('`+id+`','org_a','prj_a1','`+string(x.env)+`','`+rot.ID+`',1,'schedule','failed','role_`+id+`',3600,`+ts+`,`+ts+`,`+ts+`,'provider_unreachable')`)
		}
	}
	var parked string
	generation := queryInt(t, db, `SELECT generation FROM rotations WHERE id='`+rot.ID+`'`)
	held := false
	x.ctl.set(func() {
		x.ctl.at = func(point, _ string) {
			if point == "create:after" && !held {
				held = true
				// The start parked the schedule while the run is in flight; that is the
				// value the hold must leave alone.
				parked = x.state(rot.ID).NextRunAt
				holdRestored(t, db)
			}
		}
	})
	x.advance(24*time.Hour + time.Minute)
	x.drain()
	if !held {
		t.Fatal("the hold never landed between the claim and the publish")
	}
	if n := x.publishedRuns(rot.ID); n != 0 {
		t.Fatalf("%d runs published under a restore hold", n)
	}
	after := x.state(rot.ID)
	if after.State != store.RotationStateActive || !after.RestoreHold {
		t.Fatalf("rotation is %s (held %v) after the hold, want active and held", after.State, after.RestoreHold)
	}
	if got := queryInt(t, db, `SELECT generation FROM rotations WHERE id='`+rot.ID+`'`); got != generation {
		t.Fatalf("generation moved %d -> %d under a hold", generation, got)
	}
	if after.NextRunAt != parked {
		t.Fatalf("a held rotation's schedule moved %s -> %s", parked, after.NextRunAt)
	}
	if n := x.halts("retries_exhausted") + x.halts("gate_failed") + x.halts("authority_lost"); n != 0 {
		t.Fatalf("%d halt audits were written for a held rotation", n)
	}
	if got := queryString(t, db, `SELECT COALESCE(halt_reason, '') FROM rotations WHERE id='`+rot.ID+`'`); got != "" {
		t.Fatalf("a held rotation carries halt reason %q", got)
	}
}

// A restore hold that commits after the publish transaction's begin check but
// before its commit must not let the publish stand: on PostgreSQL the
// transaction runs SERIALIZABLE and the hold lands while it is open, so the
// publish must roll back whole (no revision), the run must settle as held
// (rotation_held, a failed attempt) and the rotation must not halt. SQLite
// writes one transaction at a time (BEGIN IMMEDIATE), so no hold can land
// inside a publish there; the begin check already refuses a hold that was
// committed before it.
func TestRotationHoldDuringThePublishTransactionRollsItBack(t *testing.T) {
	db := seededDB(t, openPostgres)
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	held := false
	x.exec.Revisions.PublishProbe = &rotPublishProbe{onBaseline: func() {
		if !held {
			held = true
			holdRestored(t, db)
		}
	}}
	x.advance(24*time.Hour + time.Minute)
	x.drain()
	if !held {
		t.Fatal("the hold never landed inside the publish transaction")
	}
	if n := x.publishedRuns(rot.ID); n != 0 {
		t.Fatalf("%d runs published for a rotation held during the publish (phases %q)", n, x.runPhases(rot.ID))
	}
	if x.snapshots() != 0 {
		t.Fatalf("%d revisions were committed under a restore hold", x.snapshots())
	}
	after := x.state(rot.ID)
	if after.State != store.RotationStateActive || !after.RestoreHold {
		t.Fatalf("rotation is %s (held %v), want active and held", after.State, after.RestoreHold)
	}
	if got := x.runPhases(rot.ID); got != "failed," {
		t.Fatalf("run phases = %q, want the run failed", got)
	}
	if got := x.lastRun(rot.ID, "last_error"); got != "rotation_held" {
		t.Fatalf("the run failed with %q, want rotation_held", got)
	}
	if n := x.halts("retries_exhausted") + x.halts("gate_failed") + x.halts("authority_lost"); n != 0 {
		t.Fatalf("%d halts for a held rotation", n)
	}
}

// An incident halt that commits after the publish transaction's begin check but
// before its commit must not let the publish stand (#1039 S3). The halt is a
// write of the same rotation row the publish's schedule write targets, so under
// PostgreSQL SERIALIZABLE the publish aborts whole and its rerun finds the rotation
// halted. SQLite writes one transaction at a time, so no halt lands inside a
// publish there.
func TestRotationIncidentHaltDuringThePublishTransactionRollsItBack(t *testing.T) {
	db := seededDB(t, openPostgres)
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	generation := queryInt(t, db, `SELECT generation FROM rotations WHERE id='`+rot.ID+`'`)
	halted := false
	x.exec.Revisions.PublishProbe = &rotPublishProbe{onBaseline: func() {
		if !halted {
			halted = true
			execRaw(t, db, `UPDATE rotations SET state='halted', halt_reason='incident_contained', next_run_at=NULL, generation=generation+1 WHERE id='`+rot.ID+`'`)
		}
	}}
	x.advance(24*time.Hour + time.Minute)
	x.drain()
	if !halted {
		t.Fatal("the halt never landed inside the publish transaction")
	}
	if n := x.publishedRuns(rot.ID); n != 0 {
		t.Fatalf("%d runs published for a rotation halted during the publish (phases %q)", n, x.runPhases(rot.ID))
	}
	if x.snapshots() != 0 {
		t.Fatalf("%d revisions were committed after the incident halt", x.snapshots())
	}
	after := x.state(rot.ID)
	if after.State != store.RotationStateHalted {
		t.Fatalf("rotation is %s, want halted", after.State)
	}
	if got := queryInt(t, db, `SELECT generation FROM rotations WHERE id='`+rot.ID+`'`); got != generation+1 {
		t.Fatalf("generation = %d, want %d: the halt must stand and nothing else may move it", got, generation+1)
	}
}

// A restore hold that lands between an extend's claim and its Gate, while the
// recorded authority has also lost its access, must still leave the settled run
// due: the authorization refusal is a definite halt reason, but the hold blocks the
// halt itself, so parking the run at "never" would leave the delivered credential to
// lapse with nothing to extend it after the hold is released.
func TestRotationExtendHeldAndAuthorityRefusedStaysDueAfterRelease(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		run := x.cycle(rot.ID)
		role := x.lastRun(rot.ID, "candidate_handle")
		x.ctl.set(func() { x.ctl.createErr = dynamic.ErrUnreachable })
		due := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run+"'"))
		if d := due.Sub(x.now()); d > 0 {
			x.advance(d + time.Second)
		}
		var restore func()
		x.exec.AfterExtendSupersessionCheck = func() {
			x.exec.AfterExtendSupersessionCheck = nil
			execRaw(t, db, `UPDATE rotations SET restore_hold=TRUE WHERE id='`+rot.ID+`'`)
			restore = x.parkAuthority()
		}
		if _, err := x.sweep(x.worker); err != nil {
			t.Fatalf("sweep: %v", err)
		}
		if restore == nil {
			t.Fatal("the extend never reached its supersession check")
		}
		if got := x.state(rot.ID); got.State != store.RotationStateActive || !got.RestoreHold {
			t.Fatalf("rotation = %s hold=%v, want active and held", got.State, got.RestoreHold)
		}
		if n := x.ctl.count("extend:" + role); n != 0 {
			t.Fatalf("a held rotation's credential was extended %d times", n)
		}

		restore()
		execRaw(t, db, `UPDATE rotations SET restore_hold=FALSE WHERE id='`+rot.ID+`'`)
		for i := 0; i < 3 && x.ctl.count("extend:"+role) == 0; i++ {
			x.advance(5 * time.Minute)
			if _, err := x.sweep(x.worker); err != nil {
				t.Fatalf("sweep after the release: %v", err)
			}
		}
		if n := x.ctl.count("extend:" + role); n != 1 {
			t.Fatalf("after the release the delivered credential was extended %d times, want 1 (run %s next attempt %s)", n, x.phase(run), queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run+"'"))
		}
	})
}

// The hold read that classifies a refused extend and the settle that follows are
// separate steps: a hold landing between them must still leave the run due. The
// settle decides on the hold at write time, so the done run is not parked at
// "never" for a rotation the halt then refuses to halt.
func TestRotationExtendHoldLandingAfterTheHoldReadStaysDueAfterRelease(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		run := x.cycle(rot.ID)
		role := x.lastRun(rot.ID, "candidate_handle")
		x.ctl.set(func() { x.ctl.createErr = dynamic.ErrUnreachable })
		due := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run+"'"))
		if d := due.Sub(x.now()); d > 0 {
			x.advance(d + time.Second)
		}
		restore := x.parkAuthority()
		landed := false
		x.exec.AfterExtendHoldRead = func() {
			x.exec.AfterExtendHoldRead = nil
			landed = true
			execRaw(t, db, `UPDATE rotations SET restore_hold=TRUE WHERE id='`+rot.ID+`'`)
		}
		if _, err := x.sweep(x.worker); err != nil {
			t.Fatalf("sweep: %v", err)
		}
		if !landed {
			t.Fatal("the extend never reached its hold read")
		}
		if got := x.state(rot.ID); got.State != store.RotationStateActive || !got.RestoreHold {
			t.Fatalf("rotation = %s hold=%v, want active and held", got.State, got.RestoreHold)
		}

		restore()
		execRaw(t, db, `UPDATE rotations SET restore_hold=FALSE WHERE id='`+rot.ID+`'`)
		for i := 0; i < 3 && x.ctl.count("extend:"+role) == 0; i++ {
			x.advance(5 * time.Minute)
			if _, err := x.sweep(x.worker); err != nil {
				t.Fatalf("sweep after the release: %v", err)
			}
		}
		if n := x.ctl.count("extend:" + role); n != 1 {
			t.Fatalf("after the release the delivered credential was extended %d times, want 1 (run %s next attempt %s)", n, x.phase(run), queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run+"'"))
		}
	})
}
