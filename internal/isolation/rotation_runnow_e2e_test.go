package isolation

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Run-now and run inspection (#148, slice S2b-1). The request is a row on the
// rotation; the executor starts the run through its normal path, so these tests
// drive the real service, the real executor and both engines.

func (x *rotExec) runNow(actor service.Actor, id string, skip bool) (service.RunNowResult, error) {
	x.t.Helper()
	return x.svc.RunNow(tctx(x.t), actor, x.scope, service.RunNowRequest{RotationID: id, SkipOverlap: skip, ConfirmDestructive: true})
}

func (x *rotExec) manualRequest(id string) string {
	x.t.Helper()
	return queryString(x.t, x.db, "SELECT COALESCE(manual_run_id, '') FROM rotations WHERE id='"+id+"'")
}

func TestRotationRunNowManualRun(t *testing.T) { forEngines(t, runRotationRunNowManualRun) }

func runRotationRunNowManualRun(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)

	res, err := x.runNow(x.actor, rot.ID, false)
	if err != nil {
		t.Fatalf("run-now: %v", err)
	}
	if !strings.HasPrefix(res.RunID, "rrn_") || res.SkipOverlap || res.RotationID != rot.ID {
		t.Fatalf("result = %+v", res)
	}
	if got := x.manualRequest(rot.ID); got != res.RunID {
		t.Fatalf("pending request = %q, want %q", got, res.RunID)
	}
	// One request at a time.
	if _, err := x.runNow(x.actor, rot.ID, false); !errors.Is(err, service.ErrRotationRunPending) {
		t.Fatalf("second run-now = %v, want the pending refusal", err)
	}
	// The audit row names the run and carries no secret.
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.run_now' AND payload LIKE '%"+res.RunID+"%' AND payload LIKE '%skip_overlap%'"); n != 1 {
		t.Fatalf("%d rotation.run_now audits naming the run", n)
	}

	// Nothing was due on the schedule; the executor starts the manual run at once,
	// through the same path (publish, overlap) a scheduled run takes.
	x.drain()
	if got := x.openRunID(rot.ID); got != res.RunID {
		t.Fatalf("open run = %q, want %q", got, res.RunID)
	}
	if got := x.phase(res.RunID); got != "overlap" {
		t.Fatalf("manual run phase = %s, want overlap", got)
	}
	if got := x.lastRun(rot.ID, "run_trigger"); got != "manual" {
		t.Fatalf("trigger = %q", got)
	}
	if got := x.lastRun(rot.ID, "revocation_delay_seconds"); got != "3600" {
		t.Fatalf("revocation delay = %q, want the rotation's 3600", got)
	}
	if got := x.manualRequest(rot.ID); got != "" {
		t.Fatalf("request not cleared by the run: %q", got)
	}
	next := mustParseStamp(t, x.state(rot.ID).NextRunAt)
	if want := x.now().Add(24 * time.Hour); next.Sub(want) > time.Second || want.Sub(next) > time.Second {
		t.Fatalf("next_run_at = %s, want about %s", next, want)
	}

	// A run in flight refuses another request.
	if _, err := x.runNow(x.actor, rot.ID, false); !errors.Is(err, service.ErrRotationRunInFlight) {
		t.Fatalf("run-now over a run in flight = %v", err)
	}
	x.advance(time.Hour + time.Minute)
	x.drain()
	if got := x.phase(res.RunID); got != "done" {
		t.Fatalf("manual run after the overlap = %s", got)
	}
}

func TestRotationRunNowRefusals(t *testing.T) { forEngines(t, runRotationRunNowRefusals) }

func runRotationRunNowRefusals(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	assertNone := func(label string) {
		t.Helper()
		if got := x.manualRequest(rot.ID); got != "" {
			t.Fatalf("%s left a pending request %q", label, got)
		}
	}

	// A stale generation.
	if _, err := x.svc.RunNow(tctx(t), x.actor, x.scope, service.RunNowRequest{RotationID: rot.ID, ExpectedGeneration: 99, ConfirmDestructive: true}); !errors.Is(err, service.ErrRotationStale) {
		t.Fatalf("stale run-now = %v", err)
	}
	assertNone("stale run-now")

	// Every run-now must confirm itself, on the server: an unconfirmed request is
	// refused by name before anything is read or consumed, skip-overlap or not.
	for _, skip := range []bool{false, true} {
		if _, err := x.svc.RunNow(tctx(t), x.actor, x.scope, service.RunNowRequest{RotationID: rot.ID, SkipOverlap: skip}); !errors.Is(err, service.ErrRotationRunNowUnconfirmed) {
			t.Fatalf("unconfirmed run-now (skip=%v) = %v", skip, err)
		}
	}
	assertNone("unconfirmed run-now")

	// No ceremony window: refused, nothing requested.
	if _, err := x.runNow(x.session(alice, false), rot.ID, false); err == nil {
		t.Fatal("run-now without a ceremony window succeeded")
	}
	assertNone("run-now without ceremony")

	// Not a person: the local principal has no session to consume a ceremony from.
	if _, err := x.runNow(service.LocalPrincipal(alice), rot.ID, false); !errors.Is(err, service.ErrRotationHumanOnly) {
		t.Fatalf("local principal run-now = %v", err)
	}
	assertNone("local principal run-now")

	// Read-only: the uniform not-found. Run-now needs resume's formula, so a
	// publisher without edit and reveal is refused the same way, and so is one
	// that holds edit and publish but not reveal.
	rotationHuman(t, db, "usr_rot_reader", "read")
	if _, err := x.runNow(x.session("usr_rot_reader", true), rot.ID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("read-only run-now = %v", err)
	}
	assertNone("read-only run-now")
	if _, err := x.runNow(x.session(alice, true), "rot_missing", false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown rotation run-now = %v", err)
	}
	if _, err := x.runNow(x.session(bob, true), rot.ID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-org run-now = %v", err)
	}
	assertNone("refused callers")
	rotationHuman(t, db, rotPublisher, "read", "publish")
	if _, err := x.runNow(x.session(rotPublisher, true), rot.ID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("publish-only run-now = %v", err)
	}
	rotationHuman(t, db, "usr_rot_noreveal", "read", "edit", "publish")
	if _, err := x.runNow(x.session("usr_rot_noreveal", true), rot.ID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("run-now without reveal = %v", err)
	}
	assertNone("publisher run-now")
	if _, err := x.runNow(x.actor, rot.ID, false); err != nil {
		t.Fatalf("run-now by a person holding the resume formula: %v", err)
	}
	if got := x.manualRequest(rot.ID); got == "" {
		t.Fatal("run-now left no request")
	}

	// Pausing drops the request; resuming does not bring it back and re-anchors the
	// schedule, so nothing runs.
	if _, err := x.transition(x.actor, "pause", rot.ID); err != nil {
		t.Fatal(err)
	}
	assertNone("pause")
	if _, err := x.runNow(x.actor, rot.ID, false); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("run-now on a paused rotation = %v", err)
	}
	if _, err := x.transition(x.actor, "resume", rot.ID); err != nil {
		t.Fatal(err)
	}
	x.drain()
	if n := queryInt(t, db, "SELECT COUNT(*) FROM rotation_runs WHERE rotation_id='"+rot.ID+"'"); n != 0 {
		t.Fatalf("%d runs after pause and resume with nothing due", n)
	}
}

func seedConvergenceTarget(t *testing.T, db *store.DB, f *rotationFixture, id, state, converged string, keys ...string) {
	t.Helper()
	execRaw(t, db, fmt.Sprintf(`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,converged_revision,created_at) VALUES ('%s','org_a','prj_a1','%s','adp_rot_conv','repository','acme','%s',42,'',1,'%s','never',%s,%s)`, id, f.env, id, state, converged, ts))
	for _, k := range keys {
		execRaw(t, db, fmt.Sprintf(`INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES ('org_a','prj_a1','%s','%s','adp_rot_conv','%s')`, f.env, id, k))
	}
}

func TestRotationRunNowSkipOverlapReportsConsumers(t *testing.T) {
	forEngines(t, runRotationRunNowSkipOverlap)
}

func runRotationRunNowSkipOverlap(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	first := x.cycle(rot.ID)
	role1 := x.lastRun(rot.ID, "candidate_handle")

	execRaw(t, db, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_rot_conv','org_a','prj_a1','forgejo','https://rot-conv.example','usr_alice','active',`+ts+`)`)
	// Counted: a mirror of either rotation key that is behind or never converged.
	seedConvergenceTarget(t, db, x.rotationFixture, "tgt_rot_behind", "active", "1", rotPasswordKey)
	seedConvergenceTarget(t, db, x.rotationFixture, "tgt_rot_never", "active", "NULL", rotUsernameKey)
	// Not counted: converged past any revision, tombstoned, or mirroring another key.
	seedConvergenceTarget(t, db, x.rotationFixture, "tgt_rot_current", "active", "999999", rotPasswordKey)
	seedConvergenceTarget(t, db, x.rotationFixture, "tgt_rot_gone", "tombstoned", "NULL", rotPasswordKey)
	seedConvergenceTarget(t, db, x.rotationFixture, "tgt_rot_other", "active", "NULL", rotSecondKey)

	res, err := x.runNow(x.actor, rot.ID, true)
	if err != nil || !res.SkipOverlap {
		t.Fatalf("skip-overlap run-now = %+v, %v", res, err)
	}
	x.drain()
	// The delay is zero for this run only: it published and revoked the superseded
	// role at once, without waiting out the rotation's overlap.
	if got := x.phase(res.RunID); got != "done" {
		t.Fatalf("skip-overlap run phase = %s, want done", got)
	}
	if got := x.lastRun(rot.ID, "revocation_delay_seconds"); got != "0" {
		t.Fatalf("run delay = %q, want 0", got)
	}
	if x.ctl.has(role1) {
		t.Fatalf("the superseded role %s survived a skip-overlap run", role1)
	}
	role2 := x.lastRun(rot.ID, "candidate_handle")
	if !x.ctl.has(role2) {
		t.Fatal("the new candidate must be live")
	}
	if got := x.lastRun(rot.ID, "not_converged_adapter_targets"); got != "2" {
		t.Fatalf("not_converged_adapter_targets = %q, want 2", got)
	}
	// The same number rides the revoke outcome audit row.
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.run_outcome' AND correlation_id='"+res.RunID+"' AND payload LIKE '%not_converged_adapter_targets%2%'"); n == 0 {
		t.Fatal("no rotation.run_outcome audit carries not_converged_adapter_targets=2")
	}
	// The rotation's own delay is untouched, so the next run's candidate and the
	// schedule still use it.
	if got := x.state(rot.ID).RevocationDelaySeconds; got != 3600 {
		t.Fatalf("rotation delay = %d", got)
	}

	// The runs list shows the report for the manual run; the first run had no superseded role to revoke, so it has none.
	page, err := x.svc.Runs(tctx(t), x.actor, x.scope, service.RotationRunsRequest{RotationID: rot.ID})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("runs = %+v, %v", page, err)
	}
	if got := page.Items[0]; got.ID != res.RunID || got.Trigger != "manual" || got.NotConvergedAdapterTargets == nil || *got.NotConvergedAdapterTargets != 2 {
		t.Fatalf("newest run = %+v", got)
	}
	if got := page.Items[1]; got.ID != first || got.NotConvergedAdapterTargets != nil {
		t.Fatalf("scheduled run = %+v", got)
	}
}

func TestRotationRunsListing(t *testing.T) { forEngines(t, runRotationRunsListing) }

func runRotationRunsListing(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.cycle(rot.ID)
	x.cycle(rot.ID)
	x.cycle(rot.ID)
	want := queryStrings(t, db, "SELECT id || ',' FROM rotation_runs WHERE rotation_id='"+rot.ID+"' ORDER BY started_at DESC, id DESC")

	var ids []string
	after := ""
	pages := 0
	for {
		page, err := x.svc.Runs(ctx, x.actor, x.scope, service.RotationRunsRequest{RotationID: rot.ID, Limit: 2, After: after})
		if err != nil {
			t.Fatalf("runs: %v", err)
		}
		pages++
		for _, it := range page.Items {
			ids = append(ids, it.ID)
			if it.Phase != "done" || it.Trigger != "schedule" || it.PublishedRevision == 0 || it.ErrorCode != "" {
				t.Fatalf("run = %+v", it)
			}
		}
		if page.NextAfter == "" {
			break
		}
		after = page.NextAfter
		if pages > 5 {
			t.Fatal("runs paging did not end")
		}
	}
	if got := strings.Join(ids, ",") + ","; got != want || pages != 2 {
		t.Fatalf("runs = %s over %d pages, want %s over 2", got, pages, want)
	}

	// Nothing a run row holds beyond fixed words and numbers reaches the wire shape.
	page, err := x.svc.Runs(ctx, x.actor, x.scope, service.RotationRunsRequest{RotationID: rot.ID})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{x.lastRun(rot.ID, "candidate_handle"), x.lastRun(rot.ID, "superseded_handle"), x.ctl.roles[x.lastRun(rot.ID, "candidate_handle")].password, rotAdminSecret} {
		if secret != "" && strings.Contains(string(raw), secret) {
			t.Fatalf("a run list carries %q", secret)
		}
	}

	// Refusals: unknown rotation, bad limit, foreign org, sibling environment, a
	// principal without read.
	if _, err := x.svc.Runs(ctx, x.actor, x.scope, service.RotationRunsRequest{RotationID: "rot_missing"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown rotation runs = %v", err)
	}
	if _, err := x.svc.Runs(ctx, x.actor, x.scope, service.RotationRunsRequest{RotationID: rot.ID, Limit: -1}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("negative limit = %v", err)
	}
	if _, err := x.svc.Runs(ctx, x.session(bob, true), x.scope, service.RotationRunsRequest{RotationID: rot.ID}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-org runs = %v", err)
	}
	sibling := domain.Scope{Org: orgA, Project: prjA1, Env: envProd}
	if _, err := x.svc.Runs(ctx, x.actor, sibling, service.RotationRunsRequest{RotationID: rot.ID}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("sibling environment runs = %v", err)
	}
	rotationHuman(t, db, "usr_rot_nogrant")
	if _, err := x.svc.Runs(ctx, x.session("usr_rot_nogrant", true), x.scope, service.RotationRunsRequest{RotationID: rot.ID}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("no-grant runs = %v", err)
	}
}

// A halted or a deleted rotation takes no run-now either, not only a paused one.
func TestRotationRunNowRefusedUnlessActive(t *testing.T) {
	forEngines(t, runRotationRunNowRefusedUnlessActive)
}

func runRotationRunNowRefusedUnlessActive(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	halted := x.create(rotPasswordKey, rotUsernameKey)
	execRaw(t, db, `UPDATE rotations SET state='halted', next_run_at=NULL, halt_reason='authority lost' WHERE id='`+halted.ID+`'`)
	if _, err := x.runNow(x.actor, halted.ID, false); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("run-now on a halted rotation = %v", err)
	}
	gone := x.create(rotSecondKey, rotSecondUser)
	if _, err := x.transition(x.actor, "delete", gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := x.runNow(x.actor, gone.ID, false); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("run-now on a deleted rotation = %v", err)
	}
	for _, id := range []string{halted.ID, gone.ID} {
		if got := x.manualRequest(id); got != "" {
			t.Fatalf("a refused run-now left request %q on %s", got, id)
		}
	}
}

// Run-now, pause, resume and delete authorize both stored keys as keys (#148,
// S2b-2): a person whose edit, publish and reveal come through rules narrowed to
// exactly the two keys may drive their rotation, a person narrowed to only one of
// the keys may not, and each refusal is recorded under the operation itself.
func TestRotationTransitionsAreKeyAware(t *testing.T) {
	forEngines(t, runRotationTransitionsAreKeyAware)
}

func runRotationTransitionsAreKeyAware(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
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
				t.Fatalf("create %s rule for %s: %v", c, p, err)
			}
		}
	}
	const oneKey = domain.PrincipalID("usr_rot_onekey")
	narrow(rotNarrow, rotPasswordKey, rotUsernameKey)
	narrow(oneKey, rotPasswordKey)
	rotationHuman(t, db, "usr_rot_noreveal", "read", "edit", "publish")
	rotationHuman(t, db, rotPublisher, "read", "publish")

	denials := func(p domain.PrincipalID, op string) int64 {
		return queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE outcome='denied' AND actor_id='`+string(p)+`' AND payload LIKE '%`+op+`%'`)
	}
	req := service.RotationStateRequest{RotationID: rot.ID}

	// Refused: the uniform not-found, a denial under the operation, no effect.
	for _, p := range []domain.PrincipalID{oneKey, "usr_rot_noreveal", rotPublisher} {
		if _, err := x.runNow(x.session(p, true), rot.ID, false); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("run-now by %s = %v, want not-found", p, err)
		}
		if denials(p, "rotation.run_now") == 0 {
			t.Fatalf("no denial under rotation.run_now for %s", p)
		}
	}
	if got := x.manualRequest(rot.ID); got != "" {
		t.Fatalf("a refused run-now left request %q", got)
	}
	// Pause needs publish only, so the publisher passes it (and the key-narrowed
	// holder resumes it) while the one-key person does not: a key they cannot
	// reach refuses even a reducing change.
	if _, err := x.svc.Pause(ctx, x.session(oneKey, true), x.scope, req); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("pause by the one-key holder = %v, want not-found", err)
	}
	if denials(oneKey, "rotation.pause") == 0 {
		t.Fatal("no denial under rotation.pause for the one-key holder")
	}

	// Allowed: the key-narrowed holder of both keys drives the whole lifecycle.
	if _, err := x.runNow(x.session(rotNarrow, true), rot.ID, false); err != nil {
		t.Fatalf("run-now by the key-narrowed holder = %v", err)
	}
	if x.manualRequest(rot.ID) == "" {
		t.Fatal("the key-narrowed run-now requested nothing")
	}
	x.drain()
	if got, err := x.svc.Pause(ctx, x.session(rotPublisher, true), x.scope, req); err != nil || got.State != store.RotationStatePaused {
		t.Fatalf("pause by the publisher = %+v, %v", got, err)
	}
	if _, err := x.svc.Resume(ctx, x.session(rotNarrow, true), x.scope, req); err != nil {
		t.Fatalf("resume by the key-narrowed holder after the publisher's pause = %v", err)
	}
	paused, err := x.svc.Pause(ctx, x.session(rotNarrow, true), x.scope, req)
	if err != nil {
		t.Fatalf("pause by the key-narrowed holder = %v", err)
	}
	if _, err := x.svc.Resume(ctx, x.session(oneKey, true), x.scope, req); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("resume by the one-key holder = %v, want not-found", err)
	}
	if denials(oneKey, "rotation.resume") == 0 {
		t.Fatal("no denial under rotation.resume for the one-key holder")
	}
	if paused.State != store.RotationStatePaused {
		t.Fatalf("state after pause = %s", paused.State)
	}
	if _, err := x.svc.Resume(ctx, x.session(rotNarrow, true), x.scope, req); err != nil {
		t.Fatalf("resume by the key-narrowed holder = %v", err)
	}
	if _, err := x.svc.Delete(ctx, x.session(oneKey, true), x.scope, req); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete by the one-key holder = %v, want not-found", err)
	}
	if denials(oneKey, "rotation.delete") == 0 {
		t.Fatal("no denial under rotation.delete for the one-key holder")
	}
	if _, err := x.svc.Delete(ctx, x.session(rotNarrow, true), x.scope, service.RotationStateRequest{RotationID: rot.ID, Abandon: true}); err != nil {
		t.Fatalf("delete by the key-narrowed holder = %v", err)
	}
}

// A manual run whose Gate fails halts the rotation like a scheduled one and
// clears its request: nothing is minted.
func TestRotationRunNowGateFailureHalts(t *testing.T) {
	forEngines(t, runRotationRunNowGateFailureHalts)
}

func runRotationRunNowGateFailureHalts(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	if _, err := x.runNow(x.actor, rot.ID, false); err != nil {
		t.Fatal(err)
	}
	restore := x.parkAuthority()
	x.drain()
	restore()
	if got := x.state(rot.ID).State; got != store.RotationStateHalted {
		t.Fatalf("state after a manual run whose Gate failed = %s, want halted", got)
	}
	if got := x.manualRequest(rot.ID); got != "" {
		t.Fatalf("halt left request %q", got)
	}
	if n := x.ctl.count("create:"); n != 0 {
		t.Fatalf("a manual run without authority minted %d candidates", n)
	}
	if n := x.halts("authority_lost"); n != 1 {
		t.Fatalf("%d halt audits naming authority_lost", n)
	}
}

// Delete, and the system pause a protected environment causes, drop a request
// that has not started; and a plain pause already did (see the refusals test).
func TestRotationRunNowRequestClearedByDeleteAndSystemPause(t *testing.T) {
	forEngines(t, runRotationRunNowRequestClearedByDeleteAndSystemPause)
}

func runRotationRunNowRequestClearedByDeleteAndSystemPause(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	doomed := x.create(rotPasswordKey, rotUsernameKey)
	if _, err := x.runNow(x.actor, doomed.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := x.transition(x.actor, "delete", doomed.ID); err != nil {
		t.Fatal(err)
	}
	if got := x.manualRequest(doomed.ID); got != "" {
		t.Fatalf("delete left request %q", got)
	}

	paused := x.create(rotSecondKey, rotSecondUser)
	if _, err := x.runNow(x.actor, paused.ID, true); err != nil {
		t.Fatal(err)
	}
	settings := &service.ProjectSettings{DB: db, Auth: authService(t, db)}
	if _, err := settings.SetEnvironment(ctx, service.LocalPrincipal(orgAdmin), x.scope, service.EnvironmentSettings{Protected: true}); err != nil {
		t.Fatalf("protect the environment: %v", err)
	}
	if got := x.state(paused.ID); got.State != store.RotationStatePaused {
		t.Fatalf("state after the environment was protected = %s", got.State)
	}
	if got := x.manualRequest(paused.ID); got != "" {
		t.Fatalf("a system pause left request %q", got)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM rotations WHERE manual_skip_overlap AND id='"+paused.ID+"'"); n != 0 {
		t.Fatal("a system pause left manual_skip_overlap set")
	}
}

// StartRun re-selects instead of starting a scheduled run when a run-now request
// landed after the due read: the stale selection must not consume the request or
// start a run.
func TestRotationStartRunReselectsAfterRunNow(t *testing.T) {
	forEngines(t, runRotationStartRunReselectsAfterRunNow)
}

func runRotationStartRunReselectsAfterRunNow(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.advance(24*time.Hour + time.Minute)
	due, ok, err := x.exec.Runtime.DueRotation(tctx(t), x.now())
	if err != nil || !ok || due.ID != rot.ID || due.ManualRunID != "" {
		t.Fatalf("due = %+v, %v, %v", due, ok, err)
	}
	res, err := x.runNow(x.actor, rot.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	_, started, err := x.exec.Runtime.StartRun(tctx(t), due, store.StartRun{RunID: "run_stale", Worker: "wrk_a", Now: x.now(), Trigger: "schedule"})
	if err != nil || started {
		t.Fatalf("a stale selection started a run: started=%v err=%v", started, err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM rotation_runs WHERE rotation_id='"+rot.ID+"'"); n != 0 {
		t.Fatalf("%d runs after the stale start", n)
	}
	if got := x.manualRequest(rot.ID); got != res.RunID {
		t.Fatalf("request = %q, want it intact (%q)", got, res.RunID)
	}
	// The next sweep re-selects it as the manual run.
	x.drain()
	if got := x.lastRun(rot.ID, "run_trigger"); got != "manual" {
		t.Fatalf("trigger after the re-select = %q", got)
	}
}

// Abandoning closes runs by hand, so only a signed-in person may do it; a plain
// delete that has nothing in flight is unchanged.
func TestRotationAbandonIsHumanOnly(t *testing.T) { forEngines(t, runRotationAbandonIsHumanOnly) }

func runRotationAbandonIsHumanOnly(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	local := service.LocalPrincipal(alice)
	// Nothing in flight: the flag alone is refused to a non-human.
	if _, err := x.svc.Delete(tctx(t), local, x.scope, service.RotationStateRequest{RotationID: rot.ID, Abandon: true}); !errors.Is(err, service.ErrRotationHumanOnly) {
		t.Fatalf("abandon with nothing in flight by a non-human = %v", err)
	}
	if x.state(rot.ID).State != store.RotationStateActive {
		t.Fatal("a refused abandon changed the rotation")
	}
	x.advance(24*time.Hour + time.Minute)
	x.drain()
	if x.openRunID(rot.ID) == "" {
		t.Fatal("no run in flight to abandon")
	}
	if _, err := x.svc.Delete(tctx(t), local, x.scope, service.RotationStateRequest{RotationID: rot.ID, Abandon: true}); !errors.Is(err, service.ErrRotationHumanOnly) {
		t.Fatalf("abandon by a non-human = %v", err)
	}
	if x.openRunID(rot.ID) == "" || x.state(rot.ID).State != store.RotationStateActive {
		t.Fatal("a refused abandon changed the rotation")
	}
	if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID, Abandon: true}); err != nil {
		t.Fatalf("abandon by a signed-in person: %v", err)
	}
}
