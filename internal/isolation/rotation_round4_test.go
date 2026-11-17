package isolation

import (
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
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
