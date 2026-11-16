package isolation

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Round 2 review fixes for the scheduled rotation executor (#148, S2a): the
// authority-draft refusal at create and resume, the provider-delete guard over a
// superseded role, the abandon audit, and the infra-fault counters.

// A rotation is refused at create while its authority holds a draft on either of
// its keys, by name, before any ceremony or row.
func TestRotationCreateRefusesAuthorityDraft(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newRotationFixture(t, db)
		for _, name := range []string{"ROT_DB_PASSWORD", "ROT_DB_USER"} {
			_, err := (&service.Values{DB: db, Keyring: probeKeyring(t, db)}).Set(tctx(t), service.LocalPrincipal(alice), f.scope, name, "draft-"+name, nil)
			if err != nil {
				t.Fatalf("stage %s: %v", name, err)
			}
			if _, err := f.svc.Create(tctx(t), f.actor, f.scope, f.request(rotPasswordKey, rotUsernameKey)); !errors.Is(err, service.ErrRotationDraftPending) {
				t.Fatalf("create over a draft on %s = %v, want ErrRotationDraftPending", name, err)
			}
			if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 0 {
				t.Fatalf("a refused create left %d rows", got)
			}
			execRaw(t, db, "DELETE FROM pending_changes WHERE owner_id='"+string(alice)+"'")
		}
		f.create(rotPasswordKey, rotUsernameKey)
	})
}

// A resume is refused for the same reason and leaves the rotation paused.
func TestRotationResumeRefusesAuthorityDraft(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		if _, err := x.transition(x.actor, "pause", rot.ID); err != nil {
			t.Fatal(err)
		}
		x.stageDraft(alice, "ROT_DB_USER", "alice-draft-while-paused")
		if _, err := x.transition(x.actor, "resume", rot.ID); !errors.Is(err, service.ErrRotationDraftPending) {
			t.Fatalf("resume over a draft = %v, want ErrRotationDraftPending", err)
		}
		if got := x.state(rot.ID).State; got != store.RotationStatePaused {
			t.Fatalf("a refused resume left the state %s", got)
		}
		if x.drafts(alice, rotUsernameKey) != 1 {
			t.Fatal("the refused resume touched the draft")
		}
	})
}

// A run of a rotation on another provider that still owes the revoke of a role
// this provider minted blocks this provider's delete, until that run settles.
func TestRotationProviderDeleteGuardCountsSupersededRole(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newRotationFixture(t, db)
		second, err := f.dyn.Configure(tctx(t), service.LocalPrincipal(alice), prjScope(), service.CreateDynamicProviderRequest{
			Kind: string(dynamic.KindPostgres), Origin: "postgres://admin@db.second.example:5432/app",
			GrantRole: "app_rw", Credential: []byte(rotAdminSecret),
		})
		if err != nil {
			t.Fatalf("configure second provider: %v", err)
		}
		// rot_old minted role_run_old on the first provider and is done; rot_new
		// moved to the second provider and still owes the first provider's role.
		rawRotation(t, db, f, "rot_guard_old", "rk_guard_old")
		rawRun(t, db, f, "run_old", "rot_guard_old", "done", "2", "", "2026-01-01T00:00:00.000000Z")
		rawRotation(t, db, f, "rot_guard_new", "rk_guard_new")
		execRaw(t, db, `UPDATE rotations SET provider_id='`+second.ID+`' WHERE id='rot_guard_new'`)
		rawRun(t, db, f, "run_new", "rot_guard_new", "overlap", "3", "2030-01-01T00:00:00.000000Z", "2026-01-02T00:00:00.000000Z")
		execRaw(t, db, `UPDATE rotation_runs SET superseded_handle='role_run_old' WHERE id='run_new'`)

		me := service.LocalPrincipal(alice)
		if _, err := f.dyn.Delete(tctx(t), me, prjScope(), f.provider, false); !errors.Is(err, service.ErrProviderHasLiveRotationRuns) {
			t.Fatalf("first provider delete over a run owing its role = %v", err)
		}
		execRaw(t, db, `UPDATE rotation_runs SET phase='done' WHERE id='run_new'`)
		if _, err := f.dyn.Delete(tctx(t), me, prjScope(), f.provider, false); err != nil {
			t.Fatalf("first provider delete once the run settled: %v", err)
		}
	})
}

// A failed run that never published will never revoke the superseded role it
// names, so it does not hold the minting provider's delete.
func TestRotationProviderDeleteGuardIgnoresUnpublishedFailedRun(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newRotationFixture(t, db)
		second, err := f.dyn.Configure(tctx(t), service.LocalPrincipal(alice), prjScope(), service.CreateDynamicProviderRequest{
			Kind: string(dynamic.KindPostgres), Origin: "postgres://admin@db.second.example:5432/app",
			GrantRole: "app_rw", Credential: []byte(rotAdminSecret),
		})
		if err != nil {
			t.Fatalf("configure second provider: %v", err)
		}
		rawRotation(t, db, f, "rot_guard_old", "rk_guard_old")
		rawRun(t, db, f, "run_old", "rot_guard_old", "done", "2", "", "2026-01-01T00:00:00.000000Z")
		rawRotation(t, db, f, "rot_guard_new", "rk_guard_new")
		execRaw(t, db, `UPDATE rotations SET provider_id='`+second.ID+`' WHERE id='rot_guard_new'`)
		rawRun(t, db, f, "run_new", "rot_guard_new", "failed", "NULL", "", "2026-01-02T00:00:00.000000Z")
		execRaw(t, db, `UPDATE rotation_runs SET superseded_handle='role_run_old' WHERE id='run_new'`)
		if _, err := f.dyn.Delete(tctx(t), service.LocalPrincipal(alice), prjScope(), f.provider, false); err != nil {
			t.Fatalf("first provider delete over a failed run that never published: %v", err)
		}
	})
}

// A role minted on a provider that was deleted since (a tombstone, which keeps its
// sealed material) is still revoked through it when a later rotation on another
// provider supersedes it.
func TestRotationSupersededRoleRevokedThroughTombstonedProvider(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		second := &rotProviderCtl{roles: map[string]rotRole{}}
		const secondOrigin = "postgres://admin@db2.rotation.example:5432/app"
		x.exec.ProviderFactory = func(_ dynamic.Kind, origin, _, _ string) (dynamic.Provider, error) {
			if origin == secondOrigin {
				return rotProvider{ctl: second}, nil
			}
			return rotProvider{ctl: x.ctl}, nil
		}
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		if _, err := x.transition(x.actor, "delete", rot.ID); err != nil {
			t.Fatal(err)
		}
		me := service.LocalPrincipal(alice)
		// No run owes role1 yet, so the guard lets the minting provider go.
		if _, err := x.dyn.Delete(tctx(t), me, prjScope(), x.provider, false); err != nil {
			t.Fatalf("delete the minting provider: %v", err)
		}
		p2, err := x.dyn.Configure(tctx(t), me, prjScope(), service.CreateDynamicProviderRequest{
			Kind: string(dynamic.KindPostgres), Origin: secondOrigin, GrantRole: "app_rw", Credential: []byte("second-admin-credential"),
		})
		if err != nil {
			t.Fatalf("configure the second provider: %v", err)
		}
		req := x.request(rotPasswordKey, rotUsernameKey)
		req.ProviderID = p2.ID
		again, err := x.svc.Create(tctx(t), x.actor, x.scope, req)
		if err != nil {
			t.Fatalf("recreate on the second provider: %v", err)
		}
		x.advance(25 * time.Hour)
		x.drain()
		run := x.openRunID(again.ID)
		x.advance(time.Hour + time.Minute)
		x.drain()
		if got := x.phase(run); got != "done" {
			t.Fatalf("run = %s, want done", got)
		}
		if x.ctl.has(role1) || x.ctl.count("drop:"+role1) != 1 {
			t.Fatalf("role1 was not dropped through the tombstoned provider (live %v, drops %d)", x.ctl.has(role1), x.ctl.count("drop:"+role1))
		}
	})
}

// An abandon that settles a published run with nothing to revoke to done writes
// that run's own audit row naming the abandon.
func TestRotationAbandonWritesRunOutcomeForDoneRun(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.drain()
		run := x.openRunID(rot.ID)
		if got := x.phase(run); got != "overlap" {
			t.Fatalf("run = %s, want overlap", got)
		}
		if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID, Abandon: true}); err != nil {
			t.Fatal(err)
		}
		if got := x.phase(run); got != "done" {
			t.Fatalf("run = %s, want done", got)
		}
		n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.run_outcome' AND correlation_id='`+run+`' AND payload LIKE '%"phase":"abandon"%' AND payload LIKE '%"error_code":"abandoned"%'`)
		if n != 1 {
			t.Fatalf("%d abandon outcome rows for the settled run, want 1", n)
		}
	})
}

// Runs that failed on an infrastructure fault count toward the backoff, never
// toward the attempt cap.
func TestRotationInfraFaultCounterIsSeparateFromAttempts(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newRotationFixture(t, db)
		rawRotation(t, db, f, "rot_infra", "rk_infra")
		since := "2026-01-01T00:00:00.000000Z"
		rawRun(t, db, f, "run_infra_1", "rot_infra", "failed", "NULL", "", "2026-02-01T00:00:00.000000Z")
		rawRun(t, db, f, "run_infra_2", "rot_infra", "failed", "NULL", "", "2026-02-02T00:00:00.000000Z")
		rawRun(t, db, f, "run_def_1", "rot_infra", "failed", "NULL", "", "2026-02-03T00:00:00.000000Z")
		rawRun(t, db, f, "run_infra_old", "rot_infra", "failed", "NULL", "", "2025-12-01T00:00:00.000000Z")
		execRaw(t, db, `UPDATE rotation_runs SET last_error='infrastructure_unavailable' WHERE id IN ('run_infra_1','run_infra_2','run_infra_old')`)
		execRaw(t, db, `UPDATE rotation_runs SET last_error='provider_unreachable' WHERE id='run_def_1'`)
		start, err := time.Parse(time.RFC3339Nano, since)
		if err != nil {
			t.Fatal(err)
		}
		chain := domain.Scope{Org: orgA, Project: prjA1, Env: f.env}
		rt := store.NewRotationRuntime(db)
		infra, err := rt.InfraFaults(tctx(t), chain, "rot_infra", 1, start)
		if err != nil {
			t.Fatal(err)
		}
		spent, err := rt.FailedAttempts(tctx(t), chain, "rot_infra", 1, start)
		if err != nil {
			t.Fatal(err)
		}
		if infra != 2 || spent != 1 {
			t.Fatalf("infra faults = %d (want 2), attempts = %d (want 1)", infra, spent)
		}
	})
}
