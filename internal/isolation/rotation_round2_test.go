package isolation

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// A restore clears a run-now queued before the backup, so releasing the hold
// starts no run and revokes no role through a skip-overlap request (#148, S2b-2
// review round 1).
func TestRotationRestoreHoldClearsQueuedRunNow(t *testing.T) {
	forEngines(t, runRotationRestoreHoldClearsQueuedRunNow)
}

func runRotationRestoreHoldClearsQueuedRunNow(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.cycle(rot.ID)
	role1 := x.lastRun(rot.ID, "candidate_handle")
	mints, revokes := x.effects("mint", "success"), x.effects("revoke", "success")
	if _, err := x.runNow(x.session(alice, true), rot.ID, true); err != nil || x.manualRequest(rot.ID) == "" {
		t.Fatalf("queue run-now: %v (request %q)", err, x.manualRequest(rot.ID))
	}
	holdRestored(t, db)
	if got := x.manualRequest(rot.ID); got != "" {
		t.Fatalf("the restore left the queued run-now %q", got)
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM rotations WHERE id='`+rot.ID+`' AND manual_skip_overlap`); n != 0 {
		t.Fatal("the restore left a skip-overlap request")
	}
	if _, err := x.svc.ReleaseHold(tctx(t), x.session(alice, true), x.scope, service.RotationStateRequest{RotationID: rot.ID}); err != nil {
		t.Fatalf("release: %v", err)
	}
	x.drain()
	// The request made the rotation due, so the released rotation starts an
	// ordinary scheduled run, like any rotation that came due while held; the
	// skip-overlap request is gone: the run keeps the full revocation delay and the
	// superseded role is still there.
	run := x.openRunID(rot.ID)
	if run == "" || x.effects("mint", "success") != mints+1 {
		t.Fatalf("release: open=%q mints=%d (was %d)", run, x.effects("mint", "success"), mints)
	}
	if got := queryString(t, db, `SELECT run_trigger FROM rotation_runs WHERE id='`+run+`'`); got != "schedule" {
		t.Fatalf("run trigger after release = %s, want schedule", got)
	}
	if got := queryInt(t, db, `SELECT revocation_delay_seconds FROM rotation_runs WHERE id='`+run+`'`); got != 3600 {
		t.Fatalf("run revocation delay = %d, want the full 3600", got)
	}
	if x.effects("revoke", "success") != revokes || !x.ctl.has(role1) {
		t.Fatal("release revoked the superseded role at once")
	}
}

// A held rotation refuses delete until it is released (round 6), and the delete
// that follows leaves no hold behind, so every reader (show, list
// --include-disabled, the gauge) sees a deleted rotation, never a held one, and
// release-hold refuses it as deleted.
func TestRotationDeleteClearsRestoreHold(t *testing.T) {
	forEngines(t, runRotationDeleteClearsRestoreHold)
}

func runRotationDeleteClearsRestoreHold(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	holdRestored(t, db)
	human := func() service.Actor { return x.session(alice, true) }
	if got := x.state(rot.ID); !got.RestoreHold {
		t.Fatal("setup: the rotation is not held")
	}
	if _, err := x.svc.Delete(tctx(t), human(), x.scope, service.RotationStateRequest{RotationID: rot.ID}); !errors.Is(err, service.ErrRotationRestoreHeld) {
		t.Fatalf("delete while held = %v, want ErrRotationRestoreHeld", err)
	}
	if _, err := x.svc.ReleaseHold(tctx(t), human(), x.scope, service.RotationStateRequest{RotationID: rot.ID}); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := x.svc.Delete(tctx(t), human(), x.scope, service.RotationStateRequest{RotationID: rot.ID}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := x.state(rot.ID); got.State != store.RotationStateDisabled || got.RestoreHold {
		t.Fatalf("show after delete: state=%s hold=%v", got.State, got.RestoreHold)
	}
	listed, err := x.list(x.actor, x.scope, true)
	if err != nil || len(listed) != 1 || listed[0].RestoreHold {
		t.Fatalf("list --include-disabled = %+v, %v", listed, err)
	}
	if _, err := x.svc.Runs(tctx(t), x.actor, x.scope, service.RotationRunsRequest{RotationID: rot.ID}); err != nil {
		t.Fatalf("runs of a deleted rotation: %v", err)
	}
	g, err := store.NewRotationRuntime(db).Gauges(tctx(t), x.now())
	if err != nil || g.RestoreHeld != 0 {
		t.Fatalf("gauges after delete = %+v, %v", g, err)
	}
	if _, err := x.svc.ReleaseHold(tctx(t), human(), x.scope, service.RotationStateRequest{RotationID: rot.ID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("release of a deleted rotation = %v, want the deleted conflict", err)
	}
}

// The approaching-expiry gauge counts only credentials that still have time:
// one past its expiry is not "expiring", and one beyond the horizon is not yet.
func TestRotationExpiringGaugeBounds(t *testing.T) { forEngines(t, runRotationExpiringGaugeBounds) }

func runRotationExpiringGaugeBounds(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.cycle(rot.ID)
	until := mustParseStamp(t, x.state(rot.ID).CredentialExpiresAt)
	rt := store.NewRotationRuntime(db)
	expiring := func(at time.Time) int64 {
		g, err := rt.Gauges(tctx(t), at)
		if err != nil {
			t.Fatal(err)
		}
		return g.Expiring
	}
	if got := expiring(until.Add(-store.RotationExpiringWithin - time.Hour)); got != 0 {
		t.Fatalf("beyond the horizon counted %d", got)
	}
	if got := expiring(until.Add(-3 * time.Hour)); got != 1 {
		t.Fatalf("inside the horizon counted %d, want 1", got)
	}
	if got := expiring(until.Add(time.Minute)); got != 0 {
		t.Fatalf("a lapsed credential counted %d, want 0", got)
	}
}

// A rotation whose start keeps being deferred by datastore faults stays overdue:
// the gauge reads the first due time, which DeferRotation does not move.
func TestRotationOverdueSurvivesDeferral(t *testing.T) {
	forEngines(t, runRotationOverdueSurvivesDeferral)
}

func runRotationOverdueSurvivesDeferral(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	due := mustParseStamp(t, x.state(rot.ID).NextRunAt)
	rt := store.NewRotationRuntime(db)
	overdue := func(at time.Time) int64 {
		g, err := rt.Gauges(tctx(t), at)
		if err != nil {
			t.Fatal(err)
		}
		return g.Overdue
	}
	if got := overdue(due.Add(10 * time.Minute)); got != 0 {
		t.Fatalf("inside the grace counted %d", got)
	}
	d := store.DueRotation{ID: rot.ID, OrgID: string(orgA), ProjectID: string(prjA1), EnvironmentID: string(x.env)}
	for i := 1; i <= 3; i++ {
		at := due.Add(time.Duration(i) * 20 * time.Minute)
		if err := rt.DeferRotation(tctx(t), d, at.Add(time.Hour), at); err != nil {
			t.Fatalf("defer: %v", err)
		}
	}
	if got := overdue(due.Add(70 * time.Minute)); got != 1 {
		t.Fatalf("a repeatedly deferred rotation counted %d overdue, want 1", got)
	}
	// A real schedule write (a pause) clears the anchor.
	if _, err := x.svc.Pause(tctx(t), x.session(alice, true), x.scope, service.RotationStateRequest{RotationID: rot.ID}); err != nil {
		t.Fatal(err)
	}
	if got := overdue(due.Add(70 * time.Minute)); got != 0 {
		t.Fatalf("a paused rotation counted %d overdue", got)
	}
}

// release-hold preflights what resume does: an authority that no longer holds the
// formula refuses with the named error, the hold stays, and the ceremony is not
// spent.
func TestRotationReleaseHoldPreflight(t *testing.T) { forEngines(t, runRotationReleaseHoldPreflight) }

func runRotationReleaseHoldPreflight(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	const releaser = domain.PrincipalID("usr_rot_releaser")
	rotationHuman(t, db, releaser, "read", "edit", "publish", "reveal")
	holdRestored(t, db)
	req := service.RotationStateRequest{RotationID: rot.ID}
	execRaw(t, db, `UPDATE principals SET containment_state='contained' WHERE id='`+string(alice)+`'`)
	session := x.session(releaser, true)
	if _, err := x.svc.ReleaseHold(tctx(t), session, x.scope, req); !errors.Is(err, service.ErrRotationAuthorityLost) {
		t.Fatalf("release with a contained authority = %v, want authority lost", err)
	}
	if queryInt(t, db, `SELECT COUNT(*) FROM rotations WHERE id='`+rot.ID+`' AND restore_hold`) != 1 {
		t.Fatal("a refused release cleared the hold")
	}
	execRaw(t, db, `UPDATE principals SET containment_state='none' WHERE id='`+string(alice)+`'`)
	// The refused attempt did not consume the window: the same session releases.
	if _, err := x.svc.ReleaseHold(tctx(t), session, x.scope, req); err != nil {
		t.Fatalf("release once the authority holds again = %v", err)
	}
}

// The lookup behind pause and the other transitions does not need read, and every
// refusal, an absent rotation included, answers the same not-found.
func TestRotationTransitionLookupNeedsNoRead(t *testing.T) {
	forEngines(t, runRotationTransitionLookupNeedsNoRead)
}

func runRotationTransitionLookupNeedsNoRead(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	const publishOnly, nobody = domain.PrincipalID("usr_rot_pubonly"), domain.PrincipalID("usr_rot_nobody")
	rotationHuman(t, db, publishOnly, "publish")
	rotationHuman(t, db, nobody)

	absent := service.RotationStateRequest{RotationID: "rot_does_not_exist"}
	_, absentErr := x.svc.Pause(tctx(t), x.session(nobody, true), x.scope, absent)
	_, deniedErr := x.svc.Pause(tctx(t), x.session(nobody, true), x.scope, service.RotationStateRequest{RotationID: rot.ID})
	if !errors.Is(absentErr, domain.ErrNotFound) || !errors.Is(deniedErr, domain.ErrNotFound) || absentErr.Error() != deniedErr.Error() {
		t.Fatalf("absent = %v, denied = %v: want the identical not-found", absentErr, deniedErr)
	}
	denied := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE outcome='denied' AND actor_id='`+string(nobody)+`' AND payload LIKE '%rotation.pause%'`)
	if denied == 0 {
		t.Fatal("the denied pause was not recorded under rotation.pause")
	}
	if inspect := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE outcome='denied' AND actor_id='`+string(nobody)+`' AND payload LIKE '%rotation.inspect%'`); inspect != 0 {
		t.Fatalf("the denied pause was recorded under rotation.inspect %d times", inspect)
	}
	paused, err := x.svc.Pause(tctx(t), x.session(publishOnly, true), x.scope, service.RotationStateRequest{RotationID: rot.ID})
	if err != nil || paused.State != store.RotationStatePaused {
		t.Fatalf("pause by a publisher without read = %+v, %v", paused, err)
	}
}

// release-hold follows the same key-aware formula as resume: a holder narrowed to
// both keys releases, one narrowed to a single key is refused as not-found, and a
// machine, an API token or a session-less principal is refused as non-human.
func TestRotationReleaseHoldCallers(t *testing.T) { forEngines(t, runRotationReleaseHoldCallers) }

func runRotationReleaseHoldCallers(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	execRaw(t, db, `UPDATE projects SET machine_reveal=TRUE WHERE id='prj_a1'`)
	rules := &service.Rules{DB: db}
	envMode, envs := onlyEnvs(x.env)
	narrow := func(p domain.PrincipalID, keys ...string) {
		t.Helper()
		rotationHuman(t, db, p, "read")
		items := make([]domain.RuleKeyItem, len(keys))
		for i, k := range keys {
			items[i] = domain.RuleKeyItem{KeyID: k}
		}
		for _, c := range []domain.Capability{domain.CapEdit, domain.CapPublish, domain.CapReveal} {
			if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{
				Target: p, Capability: c, Org: orgA, Where: whereIn(envMode, envs, domain.AxisOnly, items...),
			}); err != nil {
				t.Fatalf("rule for %s: %v", p, err)
			}
		}
	}
	const oneKey, bothKeys = domain.PrincipalID("usr_rel_onekey"), domain.PrincipalID("usr_rel_bothkeys")
	narrow(oneKey, rotPasswordKey)
	narrow(bothKeys, rotPasswordKey, rotUsernameKey)
	ident := identitySvc(db)
	sa, err := ident.CreateServiceAccount(ctx, service.LocalPrincipal(alice), prjScope(), "rel-machine", domain.ClassWorkload)
	if err != nil {
		t.Fatal(err)
	}
	minted, err := ident.MintCredential(ctx, service.LocalPrincipal(alice), prjScope(), sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for i, capability := range []string{"edit", "publish", "reveal", "read"} {
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_rel_sa_`+string(rune('a'+i))+`','`+string(sa.Principal)+`','`+capability+`','org_a','prj_a1','`+string(x.env)+`',`+ts+`)`)
	}
	holdRestored(t, db)
	req := service.RotationStateRequest{RotationID: rot.ID}
	release := func(actor service.Actor) error {
		_, err := x.svc.ReleaseHold(ctx, actor, x.scope, req)
		return err
	}
	if err := release(x.session(oneKey, true)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("release by the one-key holder = %v, want not-found", err)
	}
	if queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE outcome='denied' AND actor_id='`+string(oneKey)+`' AND payload LIKE '%rotation.release_hold%'`) == 0 {
		t.Fatal("no denial under rotation.release_hold for the one-key holder")
	}
	for name, actor := range map[string]service.Actor{
		"machine bearer": service.Bearer(minted.Value),
		"local host":     service.LocalPrincipal(alice),
	} {
		if err := release(actor); !errors.Is(err, service.ErrRotationHumanOnly) {
			t.Fatalf("release by %s = %v, want human-only", name, err)
		}
	}
	if !x.state(rot.ID).RestoreHold {
		t.Fatal("a refused release cleared the hold")
	}
	if err := release(x.session(bothKeys, true)); err != nil {
		t.Fatalf("release by the holder of both keys = %v", err)
	}
	if x.state(rot.ID).RestoreHold {
		t.Fatal("the hold survived a permitted release")
	}
}

// A run recovered after its worker died records why: a paused rotation is
// "rotation_paused". A rotation a restore holds recovers nothing at all (see
// TestRotationHeldRotationDoesNotRecoverInFlightRuns).
func TestRotationRecoveryRecordsPaused(t *testing.T) {
	for _, tc := range []struct{ name, set, want string }{
		{"paused", "state='paused', next_run_at=NULL", "rotation_paused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				x := newRotExec(t, db)
				rot := x.create(rotPasswordKey, rotUsernameKey)
				x.advance(24*time.Hour + time.Minute)
				x.crashOnce("create:after")
				if !x.crashSweep("wrk_a") {
					t.Fatal("the worker did not reach the crash point")
				}
				run := x.openRunID(rot.ID)
				execRaw(t, db, `UPDATE rotations SET `+tc.set+` WHERE id='`+rot.ID+`'`)
				x.advance(rotationLeaseTermForTest + time.Second)
				x.requireSweep("wrk_b", true)
				if got := queryString(t, db, `SELECT last_error FROM rotation_runs WHERE id='`+run+`'`); got != tc.want {
					t.Fatalf("recovered run last_error = %q, want %q", got, tc.want)
				}
			})
		})
	}
}

// The release-hold route is wired through the real router and reaches the
// service: addressed with well-formed ids of an org that does not exist, a human
// session gets the uniform not-found from the handler (a missing route answers
// differently), and no bearer is refused as unauthenticated. The service-level
// behaviour is covered by the tests above; the fixture's short ids cannot pass the
// wire's ID pattern, so a held rotation cannot be addressed here.
func TestRotationReleaseHoldRoute(t *testing.T) { forEngines(t, runRotationReleaseHoldRoute) }

func runRotationReleaseHoldRoute(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	srv := httptest.NewServer(server.New(&server.API{Auth: x.svc.Auth, CredentialRotations: x.svc}, nil))
	t.Cleanup(srv.Close)
	const id = "00000000-0000-7000-8000-000000000001"
	url := srv.URL + "/api/v1/orgs/org_" + id + "/projects/prj_" + id + "/environments/env_" + id + "/rotations/rot_" + id + "/release-hold"
	post := func(token string) (int, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(body)
	}
	if status, body := post(""); status != http.StatusUnauthorized {
		t.Fatalf("release-hold without a bearer = %d %s, want 401", status, body)
	}
	if status, body := post(x.sessionToken(alice, true)); status != http.StatusNotFound || !strings.Contains(body, "not_found") {
		t.Fatalf("release-hold on an absent org = %d %s, want the handler's uniform not-found", status, body)
	}
}

// The restore closure itself, not just its rotation step, brings every live
// rotation back held and clears a queued run-now; a deleted rotation is left alone.
func TestRotationCompleteRestoreHoldsRotations(t *testing.T) {
	forEngines(t, runRotationCompleteRestoreHoldsRotations)
}

func runRotationCompleteRestoreHoldsRotations(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	live := x.create(rotPasswordKey, rotUsernameKey)
	deleted := x.create(rotSecondKey, rotSecondUser)
	if _, err := x.runNow(x.session(alice, true), live.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := x.svc.Delete(tctx(t), x.session(alice, true), x.scope, service.RotationStateRequest{RotationID: deleted.ID}); err != nil {
		t.Fatal(err)
	}
	complete := service.CompleteRestore(time.Now().UTC(), store.Manifest{Engine: db.Engine(), SchemaVersion: 94})
	if err := tx.Write(tctx(t), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error { return complete(ctx, az) }); err != nil {
		t.Fatalf("complete restore: %v", err)
	}
	held := func(id string) int64 {
		return queryInt(t, db, `SELECT COUNT(*) FROM rotations WHERE id='`+id+`' AND restore_hold`)
	}
	if held(live.ID) != 1 || held(deleted.ID) != 0 {
		t.Fatalf("after the restore closure: live held=%d, deleted held=%d", held(live.ID), held(deleted.ID))
	}
	if got := x.manualRequest(live.ID); got != "" {
		t.Fatalf("the restore closure left the queued run-now %q", got)
	}
}
