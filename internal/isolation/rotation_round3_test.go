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

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// S2b-2 review round 2.

func deniedUnder(t *testing.T, db *store.DB, p domain.PrincipalID, op string) int64 {
	t.Helper()
	return queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE outcome='denied' AND actor_id='`+string(p)+`' AND payload LIKE '%`+op+`%'`)
}

func allDenied(t *testing.T, db *store.DB, p domain.PrincipalID) int64 {
	t.Helper()
	return queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE outcome='denied' AND actor_id='`+string(p)+`'`)
}

// A member whose read and publish come only from member rules (no grants at all)
// transitions a rotation exactly as the rules allow: all-keys rules pass, a
// publish rule narrowed to both keys passes with read from an all-keys rule, one
// narrowed to a single key is refused and recorded under the real operation. An
// allowed call records no denial at all.
func TestRotationRuleOnlyMemberTransitions(t *testing.T) {
	forEngines(t, runRotationRuleOnlyMemberTransitions)
}

func runRotationRuleOnlyMemberTransitions(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	rules := &service.Rules{DB: db}
	envMode, envs := onlyEnvs(x.env)
	grant := func(p domain.PrincipalID, c domain.Capability, keyMode domain.AxisMode, keys ...string) {
		t.Helper()
		items := make([]domain.RuleKeyItem, len(keys))
		for i, k := range keys {
			items[i] = domain.RuleKeyItem{KeyID: k}
		}
		if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{
			Target: p, Capability: c, Org: orgA, Where: whereIn(envMode, envs, keyMode, items...),
		}); err != nil {
			t.Fatalf("rule %s for %s: %v", c, p, err)
		}
	}
	pause := func(p domain.PrincipalID) (service.RotationView, error) {
		return x.svc.Pause(ctx, x.session(p, true), x.scope, service.RotationStateRequest{RotationID: rot.ID})
	}
	resume := func() {
		t.Helper()
		if _, err := x.svc.Resume(ctx, x.session(alice, true), x.scope, service.RotationStateRequest{RotationID: rot.ID}); err != nil {
			t.Fatalf("resume: %v", err)
		}
	}

	const allKeys, narrowBoth, narrowOne = domain.PrincipalID("usr_zr_allkeys"), domain.PrincipalID("usr_zr_narrow2"), domain.PrincipalID("usr_zr_narrow1")
	for _, p := range []domain.PrincipalID{allKeys, narrowBoth, narrowOne} {
		rotationHuman(t, db, p)
		grant(p, domain.CapRead, domain.AxisAll)
	}
	grant(allKeys, domain.CapPublish, domain.AxisAll)
	grant(narrowBoth, domain.CapPublish, domain.AxisOnly, rotPasswordKey, rotUsernameKey)
	grant(narrowOne, domain.CapPublish, domain.AxisOnly, rotPasswordKey)

	for name, p := range map[string]domain.PrincipalID{"all-keys rules": allKeys, "publish narrowed to both keys, read by rule": narrowBoth} {
		view, err := pause(p)
		if err != nil || view.State != store.RotationStatePaused {
			t.Fatalf("pause by %s = %+v, %v", name, view, err)
		}
		if n := allDenied(t, db, p); n != 0 {
			t.Fatalf("%s: an allowed pause left %d denial rows", name, n)
		}
		resume()
	}

	if _, err := pause(narrowOne); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("pause by the one-key holder = %v, want not-found", err)
	}
	if got := deniedUnder(t, db, narrowOne, "rotation.pause"); got != 1 {
		t.Fatalf("one-key holder denial rows under rotation.pause = %d, want exactly 1", got)
	}
	if got := deniedUnder(t, db, narrowOne, "rotation.inspect"); got != 0 {
		t.Fatalf("one-key holder denial rows under rotation.inspect = %d", got)
	}
	if x.state(rot.ID).State != store.RotationStateActive {
		t.Fatal("a refused pause changed the rotation")
	}
}

// A caller who can read looks the rotation up and is then refused on a key or
// finds nothing: the answer is the identical not-found and the audit trail is the
// same too, one denial under the operation, so neither tells an existing rotation
// from an absent id. (The caller cannot read either trail, and both are recorded.)
func TestRotationReaderRefusalIsIndistinguishable(t *testing.T) {
	forEngines(t, runRotationReaderRefusalIsIndistinguishable)
}

func runRotationReaderRefusalIsIndistinguishable(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	const reader = domain.PrincipalID("usr_zr_reader")
	rotationHuman(t, db, reader, "read")
	session := x.session(reader, true)

	_, existing := x.svc.Pause(ctx, session, x.scope, service.RotationStateRequest{RotationID: rot.ID})
	if !errors.Is(existing, domain.ErrNotFound) {
		t.Fatalf("pause of an existing rotation by a reader = %v, want not-found", existing)
	}
	afterExisting := deniedUnder(t, db, reader, "rotation.pause")
	_, absent := x.svc.Pause(ctx, session, x.scope, service.RotationStateRequest{RotationID: "rot_does_not_exist"})
	if !errors.Is(absent, domain.ErrNotFound) || absent.Error() != existing.Error() {
		t.Fatalf("pause of an absent id = %v, existing = %v: want the identical not-found", absent, existing)
	}
	afterAbsent := deniedUnder(t, db, reader, "rotation.pause")
	if afterExisting != 1 || afterAbsent != 2 {
		t.Fatalf("denial rows under rotation.pause: existing=%d, then absent=%d; want 1 then 2 (each refusal recorded once)", afterExisting, afterAbsent)
	}
	if got := deniedUnder(t, db, reader, "rotation.inspect"); got != 0 {
		t.Fatalf("a refused transition was recorded under rotation.inspect %d times", got)
	}
}

// The expiry an operator is pointed at is the one of the role still owed a drop.
func TestRotationRunValidUntil(t *testing.T) { forEngines(t, runRotationRunValidUntil) }

func runRotationRunValidUntil(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	x.cycle(rot.ID)
	first := x.state(rot.ID).CredentialExpiresAt
	if _, err := x.runNow(x.actor, rot.ID, false); err != nil {
		t.Fatalf("run-now: %v", err)
	}
	x.drain()
	second := x.openRunID(rot.ID)
	secondUntil := x.state(rot.ID).CredentialExpiresAt
	if first == "" || secondUntil == "" || first == secondUntil {
		t.Fatalf("setup: delivered expiries %q then %q", first, secondUntil)
	}
	got := func(run string) (string, bool) {
		t.Helper()
		page, err := x.svc.Runs(ctx, x.actor, x.scope, service.RotationRunsRequest{RotationID: rot.ID})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range page.Items {
			if r.ID == run {
				if r.CredentialValidUntil.IsZero() {
					return "", r.CredentialValidUntilLowerBound
				}
				return r.CredentialValidUntil.Format(time.RFC3339Nano), r.CredentialValidUntilLowerBound
			}
		}
		t.Fatalf("run %s not listed", run)
		return "", false
	}
	same := func(label, a, b string) {
		t.Helper()
		if a == "" || b == "" || !mustParseStamp(t, a).Equal(mustParseStamp(t, b)) {
			t.Fatalf("%s: got %q, want %q", label, a, b)
		}
	}

	if until, _ := got(second); until != "" {
		t.Fatalf("a run in its overlap names an expiry: %q", until)
	}
	execRaw(t, db, "UPDATE rotation_runs SET phase='revoke_refused' WHERE id='"+second+"'")
	until, lower := got(second)
	same("revoke_refused names the superseded role", until, first)
	if lower {
		t.Fatal("a settled expiry is flagged as a lower bound")
	}

	// Post-publish quarantine: the candidate is the serving credential, so the
	// superseded role's expiry is named, never the candidate's.
	execRaw(t, db, "UPDATE rotation_runs SET phase='quarantine' WHERE id='"+second+"'")
	until, _ = got(second)
	same("a published quarantine run names the superseded role", until, first)
	if mustParseStamp(t, until).Equal(mustParseStamp(t, secondUntil)) {
		t.Fatal("a published quarantine run points the operator at the serving credential")
	}

	// Pre-publish quarantine: the candidate is the role owed.
	execRaw(t, db, "UPDATE rotation_runs SET published_revision=NULL WHERE id='"+second+"'")
	until, _ = got(second)
	same("an unpublished quarantine run names its candidate", until, secondUntil)
	execRaw(t, db, "UPDATE rotation_runs SET published_revision=(SELECT published_revision FROM (SELECT published_revision FROM rotation_runs WHERE rotation_id='"+rot.ID+"' AND published_revision IS NOT NULL ORDER BY id LIMIT 1) p) WHERE id='"+second+"'")

	// An extend with no settled outcome leaves the superseded role's recorded
	// expiry as a lower bound only.
	execRaw(t, db, "UPDATE rotation_runs SET phase='revoke_refused' WHERE id='"+second+"'")
	firstRun := queryString(t, db, "SELECT id FROM rotation_runs WHERE rotation_id='"+rot.ID+"' ORDER BY id LIMIT 1")
	execRaw(t, db, "UPDATE rotation_effects SET kind='extend', outcome='unknown' WHERE run_id='"+firstRun+"' AND kind='mint'")
	until, lower = got(second)
	same("an uncertain extend still names the recorded expiry", until, first)
	if !lower {
		t.Fatal("an unsettled extend is not flagged as a lower bound")
	}
}

// A rotation recreated over the same keys finds the role its predecessor minted:
// the superseded role is looked up by chain and handle, never by rotation.
func TestRotationRunValidUntilAfterRecreate(t *testing.T) {
	forEngines(t, runRotationRunValidUntilAfterRecreate)
}

func runRotationRunValidUntilAfterRecreate(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	old := x.create(rotPasswordKey, rotUsernameKey)
	x.cycle(old.ID)
	first := x.state(old.ID).CredentialExpiresAt
	if _, err := x.svc.Delete(ctx, x.session(alice, true), x.scope, service.RotationStateRequest{RotationID: old.ID}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	fresh := x.create(rotPasswordKey, rotUsernameKey)
	if _, err := x.runNow(x.actor, fresh.ID, false); err != nil {
		t.Fatalf("run-now: %v", err)
	}
	x.drain()
	run := x.openRunID(fresh.ID)
	if queryString(t, db, "SELECT COALESCE(superseded_handle,'') FROM rotation_runs WHERE id='"+run+"'") == "" {
		t.Fatal("setup: the recreated rotation's run supersedes nothing")
	}
	execRaw(t, db, "UPDATE rotation_runs SET phase='revoke_refused' WHERE id='"+run+"'")
	page, err := x.svc.Runs(ctx, x.actor, x.scope, service.RotationRunsRequest{RotationID: fresh.ID})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("runs = %+v, %v", page, err)
	}
	if got := page.Items[0].CredentialValidUntil; got.IsZero() || !got.Equal(mustParseStamp(t, first)) {
		t.Fatalf("a recreated rotation's refused run names %v, want its predecessor's role expiry %q", got, first)
	}
}

// release-hold of an ACTIVE rotation runs the Gate's own checks, so it never
// accepts a rotation the first Gate would halt: a protected environment and a
// pending authority draft are refused with the Gate's named errors, the hold
// stays and the ceremony is not spent. A paused or halted rotation is released
// without them (resume re-checks everything), even when its provider is gone.
func TestRotationReleaseHoldUsesTheGate(t *testing.T) {
	forEngines(t, runRotationReleaseHoldUsesTheGate)
}

func runRotationReleaseHoldUsesTheGate(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	const releaser = domain.PrincipalID("usr_zr_releaser")
	rotationHuman(t, db, releaser, "read", "edit", "publish", "reveal")
	holdRestored(t, db)
	session := x.session(releaser, true)
	// A consumed ceremony slides the session's reauthentication window to the
	// environment's effective idle time, away from the 24 hours the fixture opened it
	// with, so an unmoved window shows the ceremony was not spent.
	windowExpiry := func() string {
		return queryString(t, db, `SELECT CAST(window_expires_at AS TEXT) FROM reauth_windows WHERE session_id LIKE 'ses_rot_`+string(releaser)+`%'`)
	}
	opened := windowExpiry()
	spent := func() int64 {
		if windowExpiry() != opened {
			return 1
		}
		return 0
	}
	req := service.RotationStateRequest{RotationID: rot.ID}
	release := func() error {
		_, err := x.svc.ReleaseHold(ctx, session, x.scope, req)
		return err
	}
	held := func() bool { return x.state(rot.ID).RestoreHold }

	execRaw(t, db, `UPDATE environments SET protected=TRUE WHERE id='`+string(x.env)+`'`)
	if err := release(); !errors.Is(err, service.ErrRotationProtectedEnvironment) {
		t.Fatalf("release in a protected environment = %v", err)
	}
	execRaw(t, db, `UPDATE environments SET protected=FALSE WHERE id='`+string(x.env)+`'`)

	x.stageDraft(alice, "ROT_DB_USER", "alice-draft")
	if err := release(); !errors.Is(err, service.ErrRotationDraftPending) {
		t.Fatalf("release over the authority's draft = %v", err)
	}
	if !held() {
		t.Fatal("a refused release cleared the hold")
	}
	if n := spent(); n != 0 {
		t.Fatalf("refused releases (protected environment, authority draft) spent the ceremony window: %d consumed", n)
	}
	execRaw(t, db, `DELETE FROM pending_changes WHERE owner_id='`+string(alice)+`'`)

	// A refused authority is recorded under release-hold, not resume.
	execRaw(t, db, `UPDATE principals SET containment_state='contained' WHERE id='`+string(alice)+`'`)
	if err := release(); !errors.Is(err, service.ErrRotationAuthorityLost) {
		t.Fatalf("release with a contained authority = %v", err)
	}
	if n := spent(); n != 0 {
		t.Fatalf("a release refused for its authority spent the ceremony window: %d consumed", n)
	}
	if deniedUnder(t, db, releaser, "rotation.release_hold") != 1 || deniedUnder(t, db, releaser, "rotation.resume") != 0 {
		t.Fatalf("authority refusal recorded under release_hold=%d, resume=%d", deniedUnder(t, db, releaser, "rotation.release_hold"), deniedUnder(t, db, releaser, "rotation.resume"))
	}
	execRaw(t, db, `UPDATE principals SET containment_state='none' WHERE id='`+string(alice)+`'`)

	// A paused held rotation whose provider is gone can still be released.
	execRaw(t, db, `UPDATE rotations SET state='paused', next_run_at=NULL WHERE id='`+rot.ID+`'`)
	if err := x.dyn.RevokeCredential(ctx, service.LocalPrincipal(alice), prjScope(), x.provider); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatalf("release of a paused rotation whose provider is gone = %v", err)
	}
	if got := x.state(rot.ID); got.RestoreHold || got.State != store.RotationStatePaused {
		t.Fatalf("after the release: held=%v state=%s", got.RestoreHold, got.State)
	}
	// The ceremony is spent by the release that succeeds, which proves the checks
	// above could have seen a spent window.
	if n := spent(); n != 1 {
		t.Fatalf("a successful release consumed %d ceremony windows, want 1", n)
	}
	if g, err := store.NewRotationRuntime(db).Gauges(ctx, x.now()); err != nil || g.RestoreHeld != 0 {
		t.Fatalf("held gauge after the release = %+v, %v", g, err)
	}
}

// A publish that rolls back because a restore holds the rotation records
// rotation_held, never rotation_paused.
func TestRotationPublishRollbackRecordsHeld(t *testing.T) {
	for _, tc := range []struct{ name, set, want string }{
		{"held by a restore", "restore_hold=TRUE", "rotation_held"},
		{"paused", "state='paused', next_run_at=NULL", "rotation_paused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				x := newRotExec(t, db)
				rot := x.create(rotPasswordKey, rotUsernameKey)
				x.advance(24*time.Hour + time.Minute)
				x.ctl.set(func() {
					x.ctl.at = func(p, _ string) {
						if p == "create:after" {
							x.ctl.set(func() { x.ctl.at = nil })
							execRaw(t, db, `UPDATE rotations SET `+tc.set+` WHERE id='`+rot.ID+`'`)
						}
					}
				})
				x.requireSweep(x.worker, true)
				run := x.lastRun(rot.ID, "id")
				if got := queryString(t, db, `SELECT last_error FROM rotation_runs WHERE id='`+run+`'`); got != tc.want {
					t.Fatalf("rolled-back publish last_error = %q, want %q", got, tc.want)
				}
				if x.snapshots() != 0 {
					t.Fatalf("a held publish left %d revisions", x.snapshots())
				}
			})
		})
	}
}

// A rotation whose start was deferred and then ran is not overdue: the anchor
// clears with the real schedule write.
func TestRotationDeferredThenStartedIsNotOverdue(t *testing.T) {
	forEngines(t, runRotationDeferredThenStartedIsNotOverdue)
}

func runRotationDeferredThenStartedIsNotOverdue(t *testing.T, db *store.DB) {
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	rt := store.NewRotationRuntime(db)
	overdue := func() int64 {
		g, err := rt.Gauges(tctx(t), x.now())
		if err != nil {
			t.Fatal(err)
		}
		return g.Overdue
	}
	due := mustParseStamp(t, x.state(rot.ID).NextRunAt)
	x.advance(due.Sub(x.now()) + store.RotationOverdueGrace + time.Minute)
	d := store.DueRotation{ID: rot.ID, OrgID: string(orgA), ProjectID: string(prjA1), EnvironmentID: string(x.env)}
	if err := rt.DeferRotation(tctx(t), d, x.now().Add(time.Minute), x.now()); err != nil {
		t.Fatalf("defer: %v", err)
	}
	if got := overdue(); got != 1 {
		t.Fatalf("a deferred overdue rotation counted %d, want 1", got)
	}
	x.advance(2 * time.Minute)
	x.drain()
	if got := queryInt(t, db, `SELECT COUNT(*) FROM rotation_runs WHERE rotation_id='`+rot.ID+`'`); got == 0 {
		t.Fatal("setup: the deferred rotation never started")
	}
	if got := overdue(); got != 0 {
		t.Fatalf("a rotation that started after its deferral counted %d overdue, want 0", got)
	}
	if got := queryString(t, db, `SELECT COALESCE(CAST(deferred_from AS TEXT),'') FROM rotations WHERE id='`+rot.ID+`'`); got != "" {
		t.Fatalf("deferred_from survived the start: %q", got)
	}
}

// The doctor-path read of the rotation counts is bounded and runs once; the
// metrics-side OperationalHealth read does not run it at all.
type rotationGaugeProbe struct {
	calls    int
	deadline time.Duration
}

func (p *rotationGaugeProbe) Gauges(ctx context.Context, _ time.Time) (store.RotationGauges, error) {
	p.calls++
	if d, ok := ctx.Deadline(); ok {
		p.deadline = time.Until(d)
	}
	return store.RotationGauges{Halted: 3}, nil
}

func TestRotationHealthReadIsBoundedAndSeparate(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		probe := &rotationGaugeProbe{}
		svc := &service.Retention{DB: db, Rotation: probe}
		ctx := tctx(t)
		if _, err := svc.OperationalHealth(ctx); err != nil {
			t.Fatal(err)
		}
		if probe.calls != 0 {
			t.Fatalf("OperationalHealth read the rotation counts %d times; /metrics has its own collector", probe.calls)
		}
		health, err := svc.GetHealth(ctx, service.LocalPrincipal(root))
		if err != nil {
			t.Fatal(err)
		}
		if probe.calls != 1 || !health.RotationKnown || health.Rotation.Halted != 3 {
			t.Fatalf("GetHealth: calls=%d known=%v rotation=%+v", probe.calls, health.RotationKnown, health.Rotation)
		}
		if probe.deadline <= 0 || probe.deadline > 2*time.Second {
			t.Fatalf("the doctor-path read carried a %v bound, want within 2s", probe.deadline)
		}
	})
}

// release-hold is a human act: a workload machine, an automation (CI API token)
// credential and a session-less local principal are all refused as non-human even
// when their grants would otherwise pass, and the same callers through the router
// meet the identical uniform not-found as a person on an absent rotation, so the
// transport adds no oracle. The hold stays.
func TestRotationReleaseHoldRefusesNonSessions(t *testing.T) {
	forEngines(t, runRotationReleaseHoldRefusesNonSessions)
}

func runRotationReleaseHoldRefusesNonSessions(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	x := newRotExec(t, db)
	rot := x.create(rotPasswordKey, rotUsernameKey)
	holdRestored(t, db)
	execRaw(t, db, `UPDATE projects SET machine_reveal=TRUE WHERE id='prj_a1'`)
	ident := identitySvc(db)
	bearers := map[string]string{}
	for i, class := range []domain.PrincipalClass{domain.ClassWorkload, domain.ClassAutomation} {
		sa, err := ident.CreateServiceAccount(ctx, service.LocalPrincipal(alice), prjScope(), "rel-machine-"+string(class), class)
		if err != nil {
			t.Fatalf("%s: %v", class, err)
		}
		minted, err := ident.MintCredential(ctx, service.LocalPrincipal(alice), prjScope(), sa.ID, service.MintRequest{})
		if err != nil {
			t.Fatalf("%s: %v", class, err)
		}
		bearers[string(class)] = minted.Value
		for j, capability := range []string{"edit", "publish", "reveal", "read"} {
			execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_rel_ns_`+string(rune('a'+i))+string(rune('a'+j))+`','`+string(sa.Principal)+`','`+capability+`','org_a','prj_a1','`+string(x.env)+`',`+ts+`)`)
		}
	}
	req := service.RotationStateRequest{RotationID: rot.ID}
	for name, actor := range map[string]service.Actor{
		"workload machine":       service.Bearer(bearers[string(domain.ClassWorkload)]),
		"automation (API token)": service.Bearer(bearers[string(domain.ClassAutomation)]),
		"local host":             service.LocalPrincipal(alice),
	} {
		if _, err := x.svc.ReleaseHold(ctx, actor, x.scope, req); !errors.Is(err, service.ErrRotationHumanOnly) {
			t.Fatalf("%s: release = %v, want the human-only refusal", name, err)
		}
	}
	if !x.state(rot.ID).RestoreHold {
		t.Fatal("a refused release cleared the hold")
	}

	srv := httptest.NewServer(server.New(&server.API{Auth: x.svc.Auth, CredentialRotations: x.svc}, nil))
	t.Cleanup(srv.Close)
	const id = "00000000-0000-7000-8000-000000000001"
	url := srv.URL + "/api/v1/orgs/org_" + id + "/projects/prj_" + id + "/environments/env_" + id + "/rotations/rot_" + id + "/release-hold"
	var want string
	for _, token := range []string{x.sessionToken(alice, true), bearers[string(domain.ClassWorkload)], bearers[string(domain.ClassAutomation)]} {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+token)
		resp, err := srv.Client().Do(httpReq)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		got := resp.Status + " " + string(body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("release-hold over HTTP = %s, want the uniform not-found", got)
		}
		if want == "" {
			want = got
		} else if got != want {
			t.Fatalf("transport answers differ by credential class: %q vs %q", want, got)
		}
	}
}

// A restore hold that lands between an extend's claim and its Gate refuses the
// extend, but must leave the settled run claimable once the hold is released: the
// claim skips held rotations, so an extend parked "never" would leave the delivered
// credential to lapse with nothing left to extend it.
func TestRotationExtendRacingARestoreHoldIsRetriedAfterRelease(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		run := x.cycle(rot.ID)
		role := x.lastRun(rot.ID, "candidate_handle")
		// Failing mints keep this run the newest published one until its credential
		// is due for an extension.
		x.ctl.set(func() { x.ctl.createErr = dynamic.ErrUnreachable })
		due := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run+"'"))
		if d := due.Sub(x.now()); d > 0 {
			x.advance(d + time.Second)
		}
		fired := false
		x.exec.AfterExtendSupersessionCheck = func() {
			x.exec.AfterExtendSupersessionCheck = nil
			fired = true
			execRaw(t, db, `UPDATE rotations SET restore_hold=TRUE WHERE id='`+rot.ID+`'`)
		}
		if _, err := x.sweep(x.worker); err != nil {
			t.Fatalf("sweep: %v", err)
		}
		if !fired {
			t.Fatal("the extend never reached its supersession check")
		}
		if n := x.ctl.count("extend:" + role); n != 0 {
			t.Fatalf("a held rotation's credential was extended %d times", n)
		}
		if !x.state(rot.ID).RestoreHold {
			t.Fatal("setup: the hold did not land")
		}

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
