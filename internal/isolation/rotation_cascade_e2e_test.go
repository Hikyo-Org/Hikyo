package isolation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Round-3 coverage for #148 S1: cascade pauses, environment delete blocking,
// key-aware resume authority, bounded listing and creation races.

// A rotation publishes unattended, so it must not stay active in an environment
// that becomes protected or gains an enabled approval policy. Each cascade pauses
// the active rotations in the same transaction, audits the pause with the reason
// and the caller as the actor, and leaves deleted rotations alone.
func TestRotationCascadePauses(t *testing.T) { forEngines(t, runRotationCascadePauses) }

func runRotationCascadePauses(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	settings := &service.ProjectSettings{DB: db, Auth: authService(t, db)}
	approvals := &service.Approvals{DB: db, Auth: authService(t, db), Keyring: probeKeyring(t, db)}
	pausedFor := func(reason, actor string) int64 {
		return queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.configured' AND actor_id='`+actor+`' AND payload LIKE '%"reason":"`+reason+`"%' AND payload LIKE '%"action":"paused"%'`)
	}

	active := f.create(rotPasswordKey, rotUsernameKey)
	gone := f.create(rotSecondKey, rotSecondUser)
	if _, err := f.transition(f.actor, "delete", gone.ID); err != nil {
		t.Fatal(err)
	}

	// 1. Protecting the environment pauses the active rotation, attributed to the caller.
	if _, err := settings.SetEnvironment(ctx, service.LocalPrincipal(orgAdmin), f.scope, service.EnvironmentSettings{Protected: true}); err != nil {
		t.Fatalf("protect the environment: %v", err)
	}
	if got := f.state(active.ID); got.State != store.RotationStatePaused || got.NextRunAt != "" {
		t.Fatalf("rotation after the environment was protected = %+v", got)
	}
	if got := f.state(gone.ID); got.State != store.RotationStateDisabled {
		t.Fatalf("a deleted rotation changed to %s", got.State)
	}
	if n := pausedFor("environment_protected", string(orgAdmin)); n != 1 {
		t.Fatalf("protect pause audit rows = %d, want 1 attributed to the org admin", n)
	}
	if _, err := f.transition(f.actor, "resume", active.ID); !errors.Is(err, service.ErrRotationProtectedEnvironment) {
		t.Fatalf("resume in the protected environment = %v", err)
	}
	if _, err := settings.SetEnvironment(ctx, service.LocalPrincipal(orgAdmin), f.scope, service.EnvironmentSettings{}); err != nil {
		t.Fatalf("unprotect the environment: %v", err)
	}
	if resumed, err := f.transition(f.actor, "resume", active.ID); err != nil || resumed.State != store.RotationStateActive {
		t.Fatalf("resume once unprotected = %+v, %v", resumed, err)
	}

	// 2. A DISABLED policy covers nothing: creating one leaves the rotation active.
	policy := service.ApprovalPolicyInput{
		EnvironmentID: string(f.env), MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: false,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	}
	created, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), prjScope(), policy)
	if err != nil {
		t.Fatalf("create disabled policy: %v", err)
	}
	if got := f.state(active.ID); got.State != store.RotationStateActive {
		t.Fatalf("a disabled policy paused the rotation: %+v", got)
	}
	// 3. Enabling it through an update pauses the rotation, attributed to the editor.
	policy.Enabled = true
	if _, err := approvals.UpdatePolicy(ctx, service.LocalPrincipal(orgAdmin), prjScope(), created.ID, policy); err != nil {
		t.Fatalf("enable policy: %v", err)
	}
	if got := f.state(active.ID); got.State != store.RotationStatePaused {
		t.Fatalf("rotation after the policy was enabled = %+v", got)
	}
	if n := pausedFor("approval_policy_enabled", string(orgAdmin)); n != 1 {
		t.Fatalf("policy update pause audit rows = %d, want 1 attributed to the org admin", n)
	}
	if _, err := f.transition(f.actor, "resume", active.ID); !errors.Is(err, service.ErrRotationApprovalPolicy) {
		t.Fatalf("resume under an approval policy = %v", err)
	}
	if err := approvals.DeletePolicy(ctx, service.LocalPrincipal(orgAdmin), prjScope(), created.ID); err != nil {
		t.Fatal(err)
	}
	if resumed, err := f.transition(f.actor, "resume", active.ID); err != nil || resumed.State != store.RotationStateActive {
		t.Fatalf("resume once the policy is gone = %+v, %v", resumed, err)
	}

	// 4. Creating an enabled policy pauses it too, and a project-wide policy
	// (no environment) reaches every environment of the project.
	for name, environment := range map[string]string{"environment policy": string(f.env), "project-wide policy": ""} {
		in := policy
		in.EnvironmentID = environment
		made, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), prjScope(), in)
		if err != nil {
			t.Fatalf("%s: create: %v", name, err)
		}
		if got := f.state(active.ID); got.State != store.RotationStatePaused {
			t.Fatalf("%s: rotation = %+v", name, got)
		}
		if err := approvals.DeletePolicy(ctx, service.LocalPrincipal(orgAdmin), prjScope(), made.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.transition(f.actor, "resume", active.ID); err != nil {
			t.Fatalf("%s: resume after delete: %v", name, err)
		}
	}
	if n := pausedFor("approval_policy_enabled", string(orgAdmin)); n != 3 {
		t.Fatalf("policy pause audit rows = %d, want 3", n)
	}
}

// Quarantine is not terminal for an environment delete: a quarantined run may
// still hold a live role. Only done runs and failed runs that never published
// are purgeable, and a refusal names the blocking rotations.
func TestRotationEnvironmentDeleteBlockedByRuns(t *testing.T) {
	forEngines(t, runRotationEnvironmentDeleteBlockedByRuns)
}

func runRotationEnvironmentDeleteBlockedByRuns(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
	created := f.create(rotPasswordKey, rotUsernameKey)

	// A live rotation is named in the refusal.
	err := envs.DeleteConfirming(ctx, service.LocalPrincipal(alice), f.scope, nil)
	if !errors.Is(err, service.ErrEnvironmentHasLiveRotation) || !strings.Contains(safeError(err), created.ID) {
		t.Fatalf("delete with a live rotation = %v (%q)", err, safeError(err))
	}
	if _, err := f.transition(f.actor, "delete", created.ID); err != nil {
		t.Fatal(err)
	}

	blocked := func(phase, published, label string) {
		t.Helper()
		id := "run_blk_" + label
		rotationRunRow(t, db, f, id, created.ID, "minting", "", "")
		if published != "" {
			// A published run is first stored in a published phase, then moved.
			execRaw(t, db, `UPDATE rotation_runs SET published_revision=`+published+`, phase='`+phase+`' WHERE id='`+id+`'`)
		} else {
			execRaw(t, db, `UPDATE rotation_runs SET phase='`+phase+`' WHERE id='`+id+`'`)
		}
	}

	// Quarantine blocks, and the refusal names the owning rotation.
	blocked("quarantine", "", "quarantine")
	err = envs.DeleteConfirming(ctx, service.LocalPrincipal(alice), f.scope, nil)
	if !errors.Is(err, service.ErrEnvironmentHasLiveRotation) || !strings.Contains(safeError(err), created.ID) {
		t.Fatalf("delete with a quarantined run = %v (%q)", err, safeError(err))
	}
	execRaw(t, db, `UPDATE rotation_runs SET phase='failed' WHERE id='run_blk_quarantine'`)

	// A failed run that never published still blocks until its candidate drop is
	// recorded as confirmed.
	if err := envs.DeleteConfirming(ctx, service.LocalPrincipal(alice), f.scope, nil); !errors.Is(err, service.ErrEnvironmentHasLiveRotation) {
		t.Fatalf("delete with an unconfirmed pre-publish failure = %v", err)
	}
	execRaw(t, db, `INSERT INTO rotation_effects (id, org_id, project_id, environment_id, run_id, kind, intent_audit_id, outcome_audit_id, outcome, created_at, finished_at) VALUES ('rfx_blk_drop','org_a','prj_a1','`+string(f.env)+`','run_blk_quarantine','drop_candidate','aud_blk_i','aud_blk_o','success',`+ts+`,`+ts+`)`)

	// A failed run that had published may hold the live role.
	blocked("failed", "5", "failed_published")
	if err := envs.DeleteConfirming(ctx, service.LocalPrincipal(alice), f.scope, nil); !errors.Is(err, service.ErrEnvironmentHasLiveRotation) {
		t.Fatalf("delete with a failed run that had published = %v", err)
	}
	execRaw(t, db, `UPDATE rotation_runs SET phase='done' WHERE id='run_blk_failed_published'`)

	// A revoke the provider refused is non-terminal and blocks as well.
	blocked("revoke_refused", "6", "revoke_refused")
	if err := envs.DeleteConfirming(ctx, service.LocalPrincipal(alice), f.scope, nil); !errors.Is(err, service.ErrEnvironmentHasLiveRotation) {
		t.Fatalf("delete with a revoke_refused run = %v", err)
	}
	execRaw(t, db, `UPDATE rotation_runs SET phase='done' WHERE id='run_blk_revoke_refused'`)

	// Done runs and a pre-publish failure (candidate confirmed dropped) are purgeable.
	if err := envs.DeleteConfirming(ctx, service.LocalPrincipal(alice), f.scope, nil); err != nil {
		t.Fatalf("delete with only done and pre-publish failed runs: %v", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotation_runs WHERE environment_id='"+string(f.env)+"'"); got != 0 {
		t.Fatalf("%d runs outlived the environment", got)
	}
}

// The definitions apply path deletes an environment through its own executor and
// must refuse and purge exactly like Environments.Delete.
func TestRotationDefinitionsApplyEnvironmentDelete(t *testing.T) {
	forEngines(t, runRotationDefinitionsApplyEnvironmentDelete)
}

func runRotationDefinitionsApplyEnvironmentDelete(t *testing.T, db *store.DB) {
	f := seedDefinitionsProject(t, db, "rotenv", true)
	svc := definitionsService(t, db)
	project := string(f.project)
	execRaw(t, db, `INSERT INTO dynamic_providers (id, org_id, project_id, kind, origin, tls_mode, grant_role, authority_principal_id, state, created_at) VALUES ('dp_rotenv','org_a','`+project+`','postgres','postgres://db.rotenv.example:5432/app','verify-full','app_rw','usr_alice','active',`+ts+`)`)
	execRaw(t, db, `INSERT INTO rotations (id, org_id, project_id, environment_id, provider_id, key_id, username_key_id, interval_seconds, revocation_delay_seconds, authority_principal_id, created_by, state, next_run_at, generation, created_at, updated_at) VALUES ('rot_rotenv','org_a','`+project+`','`+f.env+`','dp_rotenv','rk_rotenv_pw','rk_rotenv_user',86400,3600,'usr_alice','usr_alice','paused',NULL,1,`+ts+`,`+ts+`)`)

	bundle := parseDefinitions(t, exportDefinitions(t, svc, f))
	bundle.Environments = nil
	raw := encodeDefinitions(t, bundle)

	// A paused rotation is live: the apply refuses, naming the rotation, and the
	// environment and its rotation survive.
	plan := planDefinitions(t, svc, f, raw)
	_, err := svc.Apply(t.Context(), service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true})
	if !errors.Is(err, service.ErrEnvironmentHasLiveRotation) || !strings.Contains(safeError(err), "rot_rotenv") {
		t.Fatalf("apply deleting an environment with a live rotation = %v (%q)", err, safeError(err))
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM environments WHERE project_id='"+project+"'"); got != 1 {
		t.Fatalf("environments after the refusal = %d", got)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations WHERE project_id='"+project+"'"); got != 1 {
		t.Fatalf("rotations after the refusal = %d", got)
	}

	// A quarantined run blocks even once the rotation itself is disabled.
	execRaw(t, db, `UPDATE rotations SET state='disabled', next_run_at=NULL WHERE id='rot_rotenv'`)
	insertRun := func(id, phase, published string) {
		revision := "NULL"
		if published != "" {
			revision = published
		}
		execRaw(t, db, `INSERT INTO rotation_runs (id, org_id, project_id, environment_id, rotation_id, generation, run_trigger, phase, candidate_handle, published_revision, revocation_delay_seconds, started_at, phase_at, next_attempt_at) VALUES ('`+id+`','org_a','`+project+`','`+f.env+`','rot_rotenv',1,'schedule','`+phase+`','role_`+id+`',`+revision+`,3600,`+ts+`,`+ts+`,`+ts+`)`)
	}
	insertRun("run_rotenv_q", "quarantine", "")
	plan = planDefinitions(t, svc, f, raw)
	if _, err := svc.Apply(t.Context(), service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true}); !errors.Is(err, service.ErrEnvironmentHasLiveRotation) {
		t.Fatalf("apply with a quarantined run = %v", err)
	}

	// Done history with run and effect rows is purged with the environment.
	execRaw(t, db, `UPDATE rotation_runs SET phase='failed' WHERE id='run_rotenv_q'`)
	execRaw(t, db, `INSERT INTO rotation_effects (id, org_id, project_id, environment_id, run_id, kind, intent_audit_id, outcome_audit_id, outcome, created_at, finished_at) VALUES ('rfx_rotenv_drop','org_a','`+project+`','`+f.env+`','run_rotenv_q','drop_candidate','aud_rotenv_di','aud_rotenv_do','success',`+ts+`,`+ts+`)`)
	insertRun("run_rotenv_done", "done", "9")
	execRaw(t, db, `INSERT INTO rotation_effects (id, org_id, project_id, environment_id, run_id, kind, intent_audit_id, created_at) VALUES ('rfx_rotenv','org_a','`+project+`','`+f.env+`','run_rotenv_done','mint','aud_rotenv',`+ts+`)`)
	plan = planDefinitions(t, svc, f, raw)
	if _, err := svc.Apply(t.Context(), service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true}); err != nil {
		t.Fatalf("apply deleting an environment with only purgeable history: %v", err)
	}
	for _, table := range []string{"rotations", "rotation_runs", "rotation_effects", "rotation_key_claims"} {
		if got := queryInt(t, db, "SELECT COUNT(*) FROM "+table+" WHERE project_id='"+project+"'"); got != 0 {
			t.Errorf("%s still has %d rows after the environment was deleted", table, got)
		}
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM environments WHERE project_id='"+project+"'"); got != 0 {
		t.Fatalf("environments after the delete = %d", got)
	}
}

// Two creates racing for one key: exactly one wins and the other is refused with
// the in-use refusal on both engines, never a raw constraint error.
func TestRotationConcurrentCreate(t *testing.T) { forEngines(t, runRotationConcurrentCreate) }

func runRotationConcurrentCreate(t *testing.T, db *store.DB) {
	f := newRotationFixture(t, db)
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, 2)
	users := []string{rotUsernameKey, rotSecondUser}
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, results[i] = f.svc.Create(context.WithoutCancel(f.t.Context()), f.actor, f.scope, f.request(rotPasswordKey, users[i]))
		}()
	}
	close(start)
	wg.Wait()
	var won, refused int
	for _, err := range results {
		switch {
		case err == nil:
			won++
		case errors.Is(err, service.ErrRotationKeyInUse):
			refused++
		default:
			t.Errorf("racing create = %v, want success or the in-use refusal", err)
		}
	}
	if won != 1 || refused != 1 {
		t.Fatalf("racing creates: %d won, %d refused (%v)", won, refused, results)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 1 {
		t.Fatalf("racing creates left %d rotations", got)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotation_key_claims"); got != 2 {
		t.Fatalf("racing creates left %d key claims, want 2", got)
	}
}

// A local host principal has no session to consume a ceremony from, so create and
// resume refuse it with the human-only refusal, not a missing-grant answer.
func TestRotationLocalPrincipalHumanOnly(t *testing.T) {
	forEngines(t, runRotationLocalPrincipalHumanOnly)
}

func runRotationLocalPrincipalHumanOnly(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	local := service.LocalPrincipal(alice)
	if _, err := f.svc.Create(ctx, local, f.scope, f.request(rotPasswordKey, rotUsernameKey)); !errors.Is(err, service.ErrRotationHumanOnly) {
		t.Fatalf("local principal create = %v, want the human-only refusal", err)
	}
	created := f.create(rotPasswordKey, rotUsernameKey)
	if _, err := f.transition(f.actor, "pause", created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.transition(local, "resume", created.ID); !errors.Is(err, service.ErrRotationHumanOnly) {
		t.Fatalf("local principal resume = %v, want the human-only refusal", err)
	}
	if got := f.state(created.ID); got.State != store.RotationStatePaused {
		t.Fatalf("a refused local resume changed the state to %s", got.State)
	}
}

// A recorded authority that holds the create formula ONLY through key-narrowed
// rules still holds, so an environment-wide second person can resume the
// rotation; once those rules are gone the resume is refused with the authority
// refusal, and the denial is attributed to the caller under the resume
// operation, never rotation.create.
func TestRotationResumeNarrowedAuthority(t *testing.T) {
	forEngines(t, runRotationResumeNarrowedAuthority)
}

func runRotationResumeNarrowedAuthority(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	rotationHuman(t, db, rotNarrow, "read")
	rules := &service.Rules{DB: db}
	envMode, envs := onlyEnvs(f.env)
	var ruleIDs []string
	for _, c := range []domain.Capability{domain.CapRead, domain.CapEdit, domain.CapPublish, domain.CapReveal} {
		view, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{
			Target: rotNarrow, Capability: c, Org: orgA,
			Where: whereIn(envMode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: rotPasswordKey}, domain.RuleKeyItem{KeyID: rotUsernameKey}),
		})
		if err != nil {
			t.Fatalf("create %s rule: %v", c, err)
		}
		ruleIDs = append(ruleIDs, view.ID)
	}
	narrow := f.session(rotNarrow, true)
	created, err := f.svc.Create(ctx, narrow, f.scope, f.request(rotPasswordKey, rotUsernameKey))
	if err != nil {
		t.Fatalf("create through key-narrowed rules: %v", err)
	}
	if _, err := f.transition(f.actor, "pause", created.ID); err != nil {
		t.Fatal(err)
	}

	// (a) The authority holds only through the rules and the caller is environment-wide.
	custodianActor := f.session(custodian, true)
	if resumed, err := f.transition(custodianActor, "resume", created.ID); err != nil || resumed.State != store.RotationStateActive {
		t.Fatalf("resume of a key-narrowed authority's rotation = %+v, %v", resumed, err)
	}
	if _, err := f.transition(custodianActor, "pause", created.ID); err != nil {
		t.Fatal(err)
	}

	// (b) Remove the rules: the authority no longer holds anything on the keys.
	for _, id := range ruleIDs {
		if err := rules.Revoke(ctx, service.LocalPrincipal(orgAdmin), orgA, id); err != nil {
			t.Fatalf("revoke rule %s: %v", id, err)
		}
	}
	custodianActor = f.session(custodian, true)
	if _, err := f.transition(custodianActor, "resume", created.ID); !errors.Is(err, service.ErrRotationAuthorityLost) {
		t.Fatalf("resume after the narrowed rules were removed = %v", err)
	}
	denied := `FROM audit_tenant_events WHERE outcome='denied' AND actor_id='usr_custodian' AND authority_id='` + string(rotNarrow) + `'`
	if n := queryInt(t, db, `SELECT COUNT(*) `+denied+` AND payload LIKE '%rotation.resume%'`); n == 0 {
		t.Fatal("no denial attributes the lost authority to the caller under rotation.resume")
	}
	if n := queryInt(t, db, `SELECT COUNT(*) `+denied+` AND payload LIKE '%rotation.create%'`); n != 0 {
		t.Fatalf("%d resume denials were recorded under rotation.create", n)
	}
	if wrong := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE outcome='denied' AND actor_id='`+string(rotNarrow)+`' AND authority_id='`+string(rotNarrow)+`'`); wrong != 0 {
		t.Fatalf("%d denials blame the authority principal as the actor", wrong)
	}
	if got := f.state(created.ID); got.State != store.RotationStatePaused {
		t.Fatalf("a refused resume changed the state to %s", got.State)
	}
}

// RecordedPrincipalHolds resolves the key on every call: a principal whose
// environment-wide grants cover the operation does not hold it for a key that no
// longer exists.
func TestRecordedPrincipalHoldsResolvesKey(t *testing.T) {
	forEngines(t, runRecordedPrincipalHoldsResolvesKey)
}

func runRecordedPrincipalHoldsResolvesKey(t *testing.T, db *store.DB) {
	f := newRotationFixture(t, db)
	caller := authz.Identity{Principal: custodian, Class: domain.ClassHuman}
	holds := func(key string) bool {
		t.Helper()
		var out bool
		target := authz.KeyByID(key)
		if err := tx.Write(f.t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			var err error
			out, err = az.RecordedPrincipalHolds(ctx, caller, alice, authz.OpRotationResume, f.scope, &target)
			return err
		}); err != nil {
			t.Fatalf("RecordedPrincipalHolds(%s): %v", key, err)
		}
		return out
	}
	if !holds(rotSecondKey) {
		t.Fatal("alice, who holds the formula environment-wide, does not hold it for an existing key")
	}
	execRaw(t, db, `DELETE FROM keys WHERE id='`+rotSecondKey+`'`)
	if holds(rotSecondKey) {
		t.Fatal("a deleted key is still held through environment-wide grants")
	}
	if holds("key_rot_never_existed") {
		t.Fatal("a key that never existed is held")
	}
}

// Listing is paged: a page never exceeds the limit, NextAfter continues it, the
// last page ends the sequence, and an out-of-range limit is refused. Credential
// expiries come from one batched read, so a page of rotations still reports each
// key's own expiry.
func TestRotationListPaging(t *testing.T) { forEngines(t, runRotationListPaging) }

func runRotationListPaging(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	const total = 5
	for i := range total {
		execRaw(t, db, fmt.Sprintf(`INSERT INTO rotations (id, org_id, project_id, environment_id, provider_id, key_id, username_key_id, interval_seconds, revocation_delay_seconds, authority_principal_id, created_by, state, next_run_at, generation, created_at, updated_at) VALUES ('rot_pg_%d','org_a','prj_a1','%s','%s','rk_pg_pw_%d','rk_pg_user_%d',86400,3600,'usr_alice','usr_alice','disabled',NULL,1,%s,%s)`, i, f.env, f.provider, i, i, ts, ts))
	}
	// Two of the five keys have a published credential; the batched expiry read
	// must attach each to its own rotation.
	for i, until := range map[int]string{1: "2031-01-01T00:00:00.000000Z", 3: "2032-02-02T00:00:00.000000Z"} {
		id := fmt.Sprintf("run_pg_%d", i)
		execRaw(t, db, fmt.Sprintf(`INSERT INTO rotation_runs (id, org_id, project_id, environment_id, rotation_id, generation, run_trigger, phase, candidate_handle, candidate_valid_until, published_revision, revocation_delay_seconds, started_at, phase_at, next_attempt_at) VALUES ('%s','org_a','prj_a1','%s','rot_pg_%d',1,'schedule','done','role_%s','%s',3,3600,%s,%s,%s)`, id, f.env, i, id, until, ts, ts, ts))
	}

	var seen []string
	after := ""
	for page := 0; ; page++ {
		got, err := f.svc.List(ctx, f.actor, f.scope, service.RotationListRequest{IncludeDisabled: true, Limit: 2, After: after})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(got.Items) > 2 {
			t.Fatalf("page %d returned %d rows for a limit of 2", page, len(got.Items))
		}
		for _, item := range got.Items {
			seen = append(seen, item.ID)
			switch item.ID {
			case "rot_pg_1":
				if !mustParseStamp(t, item.CredentialExpiresAt).Equal(mustParseStamp(t, "2031-01-01T00:00:00.000000Z")) {
					t.Errorf("rot_pg_1 expiry = %q", item.CredentialExpiresAt)
				}
			case "rot_pg_3":
				if !mustParseStamp(t, item.CredentialExpiresAt).Equal(mustParseStamp(t, "2032-02-02T00:00:00.000000Z")) {
					t.Errorf("rot_pg_3 expiry = %q", item.CredentialExpiresAt)
				}
			default:
				if item.CredentialExpiresAt != "" {
					t.Errorf("%s reports an expiry %q it never published", item.ID, item.CredentialExpiresAt)
				}
			}
		}
		if got.NextAfter == "" {
			break
		}
		after = got.NextAfter
		if page > total {
			t.Fatal("paging did not terminate")
		}
	}
	if want := "rot_pg_0 rot_pg_1 rot_pg_2 rot_pg_3 rot_pg_4"; strings.Join(seen, " ") != want {
		t.Fatalf("paged ids = %v, want %s", seen, want)
	}
	// A page of exactly the remaining rows does not advertise a next page.
	exact, err := f.svc.List(ctx, f.actor, f.scope, service.RotationListRequest{IncludeDisabled: true, Limit: total})
	if err != nil || len(exact.Items) != total || exact.NextAfter != "" {
		t.Fatalf("exact page = %d rows, next %q, %v", len(exact.Items), exact.NextAfter, err)
	}
	for _, limit := range []int{-1, service.RotationListMaxLimit + 1} {
		if _, err := f.svc.List(ctx, f.actor, f.scope, service.RotationListRequest{Limit: limit}); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("limit %d = %v, want the invalid-argument refusal", limit, err)
		}
	}
}

// A halted rotation (the executor's named stop) is settled in the schema now. It
// is not scheduled, it carries a short reason, and resume from it runs the same
// full authorization as a resume from paused, clearing the reason.
func TestRotationHaltedSlot(t *testing.T) { forEngines(t, runRotationHaltedSlot) }

func runRotationHaltedSlot(t *testing.T, db *store.DB) {
	f := newRotationFixture(t, db)
	created := f.create(rotPasswordKey, rotUsernameKey)

	// halt_reason is only legal on a halted rotation, and a halted rotation is unscheduled.
	if err := execRawErr(t, db, `UPDATE rotations SET halt_reason='authority lost' WHERE id='`+created.ID+`'`); err == nil {
		t.Fatal("a halt reason on an active rotation was accepted")
	}
	if err := execRawErr(t, db, `UPDATE rotations SET state='halted', halt_reason='authority lost' WHERE id='`+created.ID+`'`); err == nil {
		t.Fatal("a halted rotation with a next run was accepted")
	}
	execRaw(t, db, `UPDATE rotations SET state='halted', next_run_at=NULL, halt_reason='authority lost' WHERE id='`+created.ID+`'`)
	if _, err := f.transition(f.actor, "pause", created.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("pause of a halted rotation = %v", err)
	}

	// Resume re-runs the full authorization: a lost recorded authority refuses it.
	execRaw(t, db, `UPDATE grants SET capability='read' WHERE id='g_rot_reveal'`)
	custodianActor := f.session(custodian, true)
	if _, err := f.transition(custodianActor, "resume", created.ID); !errors.Is(err, service.ErrRotationAuthorityLost) {
		t.Fatalf("resume of a halted rotation after the authority lost reveal = %v", err)
	}
	execRaw(t, db, `UPDATE grants SET capability='reveal' WHERE id='g_rot_reveal'`)
	if got := f.state(created.ID); got.State != store.RotationStateHalted {
		t.Fatalf("a refused resume changed the state to %s", got.State)
	}
	resumed, err := f.transition(custodianActor, "resume", created.ID)
	if err != nil || resumed.State != store.RotationStateActive {
		t.Fatalf("resume of a halted rotation = %+v, %v", resumed, err)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM rotations WHERE id='`+created.ID+`' AND halt_reason IS NOT NULL`); got != 0 {
		t.Fatal("resume left the halt reason behind")
	}
	// The run phase slot for a refused revoke exists and keeps the open-run slot.
	rotationRunRow(t, db, f, "run_halt_rr", created.ID, "minting", "", "")
	execRaw(t, db, `UPDATE rotation_runs SET published_revision=3, phase='revoke_refused' WHERE id='run_halt_rr'`)
	if err := rotationRunRowErr(t, db, f, "run_halt_2", created.ID, "minting", "", "", ts); err == nil {
		t.Fatal("a revoke_refused run released the open-run slot")
	}
}

// rawRotation inserts a disabled rotation row directly: it is history, so only
// its runs can block an environment delete.
func rawRotation(t *testing.T, db *store.DB, f *rotationFixture, id, keyID string) {
	t.Helper()
	execRaw(t, db, fmt.Sprintf(`INSERT INTO rotations (id, org_id, project_id, environment_id, provider_id, key_id, username_key_id, interval_seconds, revocation_delay_seconds, authority_principal_id, created_by, state, next_run_at, generation, created_at, updated_at) VALUES ('%s','org_a','prj_a1','%s','%s','%s','%s_user',86400,3600,'usr_alice','usr_alice','disabled',NULL,1,%s,%s)`, id, f.env, f.provider, keyID, keyID, ts, ts))
}

// rawRun inserts a run directly at a start time (an RFC 3339 literal).
func rawRun(t *testing.T, db *store.DB, f *rotationFixture, id, rotationID, phase, published, validUntil, startedAt string) {
	t.Helper()
	until := "NULL"
	if validUntil != "" {
		until = "'" + validUntil + "'"
	}
	execRaw(t, db, fmt.Sprintf(`INSERT INTO rotation_runs (id, org_id, project_id, environment_id, rotation_id, generation, run_trigger, phase, candidate_handle, candidate_valid_until, published_revision, revocation_delay_seconds, started_at, phase_at, next_attempt_at) VALUES ('%s','org_a','prj_a1','%s','%s',1,'schedule','%s','role_%s',%s,%s,3600,'%s',%s,%s)`, id, f.env, rotationID, phase, id, until, published, startedAt, ts, ts))
}

// One rotation with many blocking runs must not hide the other blocking rotations
// under the page limit, and the "first N shown" marker must appear exactly when
// more rotations block than the refusal names.
func TestRotationEnvironmentDeleteNamesEveryBlockingRotation(t *testing.T) {
	forEngines(t, runRotationEnvironmentDeleteNamesEveryBlockingRotation)
}

func runRotationEnvironmentDeleteNamesEveryBlockingRotation(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
	refusal := func() string {
		t.Helper()
		err := envs.DeleteConfirming(ctx, service.LocalPrincipal(alice), f.scope, nil)
		if !errors.Is(err, service.ErrEnvironmentHasLiveRotation) {
			t.Fatalf("delete = %v, want the live-rotation refusal", err)
		}
		return safeError(err)
	}

	// rot_blk_00 sorts first and holds 12 blocking runs (failed after publish, so
	// terminal and not constrained to one open run). rot_blk_01 holds one
	// quarantined run and would be cut off by a page of repeats.
	rawRotation(t, db, f, "rot_blk_00", "rk_blk_00")
	for i := range 12 {
		rawRun(t, db, f, fmt.Sprintf("run_blk_00_%02d", i), "rot_blk_00", "failed", "3", "", "2026-01-01T00:00:00.000000Z")
	}
	rawRotation(t, db, f, "rot_blk_01", "rk_blk_01")
	rawRun(t, db, f, "run_blk_01_00", "rot_blk_01", "quarantine", "NULL", "", "2026-01-01T00:00:00.000000Z")
	got := refusal()
	if !strings.Contains(got, "rot_blk_00") || !strings.Contains(got, "rot_blk_01") {
		t.Fatalf("refusal must name both blocking rotations: %q", got)
	}
	if strings.Contains(got, "shown") {
		t.Fatalf("two blocking rotations must not carry the truncation marker: %q", got)
	}
	if n := strings.Count(got, "rot_blk_00"); n != 1 {
		t.Fatalf("rot_blk_00 named %d times, want once: %q", n, got)
	}

	// Exactly the limit of distinct blocking rotations is still complete.
	for i := 2; i < store.RotationBlockingIDsLimit; i++ {
		id := fmt.Sprintf("rot_blk_%02d", i)
		rawRotation(t, db, f, id, "rk_"+id[4:])
		rawRun(t, db, f, "run_"+id[4:]+"_00", id, "quarantine", "NULL", "", "2026-01-01T00:00:00.000000Z")
	}
	got = refusal()
	if strings.Contains(got, "shown") || !strings.Contains(got, fmt.Sprintf("rot_blk_%02d", store.RotationBlockingIDsLimit-1)) {
		t.Fatalf("exactly %d blocking rotations must be named in full without a marker: %q", store.RotationBlockingIDsLimit, got)
	}

	// One more distinct rotation exceeds the limit: the marker is now honest.
	rawRotation(t, db, f, "rot_blk_99", "rk_blk_99")
	rawRun(t, db, f, "run_blk_99_00", "rot_blk_99", "quarantine", "NULL", "", "2026-01-01T00:00:00.000000Z")
	got = refusal()
	if !strings.Contains(got, fmt.Sprintf("(first %d shown)", store.RotationBlockingIDsLimit)) {
		t.Fatalf("more than %d blocking rotations must carry the marker: %q", store.RotationBlockingIDsLimit, got)
	}
	if strings.Contains(got, "rot_blk_99") {
		t.Fatalf("the eleventh rotation must be the one left out: %q", got)
	}
}

// The delivered credential's expiry is the newest published run per key, read in
// one bounded query: a long run history changes neither the answer nor the number
// of rows read, a newer published run under another rotation of the same key
// wins, and an unpublished newer run never does.
func TestRotationCredentialExpiryOverLongHistory(t *testing.T) {
	// The body stays inline: the reviewed query pin names this test, and the
	// lint requires the query's name inside it.
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := tctx(t)
		f := newRotationFixture(t, db)
		rawRotation(t, db, f, "rot_exp_a", "rk_exp_a")
		rawRotation(t, db, f, "rot_exp_a2", "rk_exp_a")
		rawRotation(t, db, f, "rot_exp_b", "rk_exp_b")
		const history = 60
		// rot_exp_a: the newest start has the EARLIEST expiry, so ordering by expiry
		// instead of by start would pick the wrong run.
		for i := range history {
			start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour).Format("2006-01-02T15:04:05.000000Z")
			until := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Hour).Format("2006-01-02T15:04:05.000000Z")
			rawRun(t, db, f, fmt.Sprintf("run_exp_a_%02d", i), "rot_exp_a", "done", "2", until, start)
			rawRun(t, db, f, fmt.Sprintf("run_exp_b_%02d", i), "rot_exp_b", "done", "2", "2028-06-06T00:00:00.000000Z", start)
		}
		newestA := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(history-1) * time.Hour)
		expiries := func() map[string]time.Time {
			t.Helper()
			out := map[string]time.Time{}
			got, err := f.svc.List(ctx, f.actor, f.scope, service.RotationListRequest{IncludeDisabled: true, Limit: service.RotationListMaxLimit})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			for _, item := range got.Items {
				if item.CredentialExpiresAt != "" {
					out[item.ID] = mustParseStamp(t, item.CredentialExpiresAt)
				}
			}
			return out
		}
		got := expiries()
		if !got["rot_exp_a"].Equal(newestA) || !got["rot_exp_b"].Equal(time.Date(2028, 6, 6, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("expiries over %d runs = %v, want a=%s b=2028-06-06", history, got, newestA)
		}
		if !got["rot_exp_a2"].Equal(newestA) {
			t.Fatalf("a rotation sharing the key must report the key's newest run: %v", got)
		}
		// A newer unpublished run delivered nothing and changes no expiry.
		rawRun(t, db, f, "run_exp_a_unpub", "rot_exp_a", "failed", "NULL", "2035-01-01T00:00:00.000000Z", "2030-01-01T00:00:00.000000Z")
		if again := expiries(); !again["rot_exp_a"].Equal(newestA) {
			t.Fatalf("an unpublished newer run changed the expiry to %v", again["rot_exp_a"])
		}
		// A newer published run under the other rotation of the same key wins for both.
		later := time.Date(2029, 3, 3, 0, 0, 0, 0, time.UTC)
		rawRun(t, db, f, "run_exp_a2_new", "rot_exp_a2", "done", "9", later.Format("2006-01-02T15:04:05.000000Z"), "2031-01-01T00:00:00.000000Z")
		again := expiries()
		if !again["rot_exp_a"].Equal(later) || !again["rot_exp_a2"].Equal(later) {
			t.Fatalf("the newest published run across rotations must win: %v", again)
		}
		// The generated query itself is bounded: one row per requested key whatever the
		// history, nothing for an unrequested key, and nothing under another chain.
		rowsFor := func(org string, keys ...string) int {
			t.Helper()
			if db.Engine() == store.EngineSQLite {
				rows, err := sqlitegen.New(db.SQLiteWrite()).RotationCredentialExpiries(ctx, sqlitegen.RotationCredentialExpiriesParams{ChainOrg: org, ChainProject: "prj_a1", ChainEnv: string(f.env), KeyIds: keys})
				if err != nil {
					t.Fatalf("RotationCredentialExpiries: %v", err)
				}
				return len(rows)
			}
			rows, err := pggen.New(db.PG()).RotationCredentialExpiries(ctx, pggen.RotationCredentialExpiriesParams{ChainOrg: org, ChainProject: "prj_a1", ChainEnv: string(f.env), KeyIds: keys})
			if err != nil {
				t.Fatalf("RotationCredentialExpiries: %v", err)
			}
			return len(rows)
		}
		if n := rowsFor("org_a", "rk_exp_a", "rk_exp_b"); n != 2 {
			t.Fatalf("expiry rows for two keys over %d runs each = %d, want 2", history, n)
		}
		if n := rowsFor("org_a", "rk_exp_a"); n != 1 {
			t.Fatalf("expiry rows for one requested key = %d, want 1", n)
		}
		if n := rowsFor("org_a"); n != 0 {
			t.Fatalf("expiry rows for no requested key = %d, want 0", n)
		}
		if n := rowsFor("org_b", "rk_exp_a", "rk_exp_b"); n != 0 {
			t.Fatalf("expiry rows under another org = %d, want 0", n)
		}
		// The fixture really holds a long history behind the two keys.
		if n := queryInt(t, db, "SELECT COUNT(*) FROM rotation_runs WHERE environment_id='"+string(f.env)+"' AND published_revision IS NOT NULL"); n < 2*history {
			t.Fatalf("fixture holds %d published runs, want at least %d", n, 2*history)
		}
	})
}

// A key that was one rotation's username key and is now another rotation's
// password key reads the newest published run across both roles, and a list and a
// get agree on it. The body keeps the generated queries' names inline: the
// reviewed query pins name this test, and the lint requires each query's name in it.
func TestRotationCredentialExpiryMatchesEitherKeyRole(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := tctx(t)
		f := newRotationFixture(t, db)
		// rot_role_prev drove rk_role_prev (password) and rk_role_prev_user
		// (username); rot_role_next now takes rk_role_prev_user as ITS password key.
		rawRotation(t, db, f, "rot_role_prev", "rk_role_prev")
		rawRotation(t, db, f, "rot_role_next", "rk_role_prev_user")
		older := time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)
		newer := time.Date(2030, 6, 6, 0, 0, 0, 0, time.UTC)
		stamp := func(v time.Time) string { return v.Format("2006-01-02T15:04:05.000000Z") }
		// The previous rotation's run is the newer publish of the shared key.
		rawRun(t, db, f, "run_role_next_old", "rot_role_next", "done", "3", stamp(older), "2026-01-01T00:00:00.000000Z")
		rawRun(t, db, f, "run_role_prev_new", "rot_role_prev", "done", "7", stamp(newer), "2026-02-01T00:00:00.000000Z")

		list, err := f.svc.List(ctx, f.actor, f.scope, service.RotationListRequest{IncludeDisabled: true, Limit: service.RotationListMaxLimit})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		listed := map[string]time.Time{}
		for _, item := range list.Items {
			if item.CredentialExpiresAt != "" {
				listed[item.ID] = mustParseStamp(t, item.CredentialExpiresAt)
			}
		}
		got, err := f.svc.Get(ctx, f.actor, f.scope, "rot_role_next")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		fromGet := mustParseStamp(t, got.CredentialExpiresAt)
		if !fromGet.Equal(newer) {
			t.Fatalf("get reports %s, want the newer publish of the shared key %s", fromGet, newer)
		}
		if !listed["rot_role_next"].Equal(fromGet) {
			t.Fatalf("list reports %s and get reports %s for the same rotation", listed["rot_role_next"], fromGet)
		}
		// The username-role generated query reads the same key from the other side.
		var rows int
		if db.Engine() == store.EngineSQLite {
			r, err := sqlitegen.New(db.SQLiteWrite()).RotationCredentialExpiriesByUsernameKey(ctx, sqlitegen.RotationCredentialExpiriesByUsernameKeyParams{ChainOrg: "org_a", ChainProject: "prj_a1", ChainEnv: string(f.env), KeyIds: []string{"rk_role_prev_user"}})
			if err != nil {
				t.Fatalf("RotationCredentialExpiriesByUsernameKey: %v", err)
			}
			rows = len(r)
		} else {
			r, err := pggen.New(db.PG()).RotationCredentialExpiriesByUsernameKey(ctx, pggen.RotationCredentialExpiriesByUsernameKeyParams{ChainOrg: "org_a", ChainProject: "prj_a1", ChainEnv: string(f.env), KeyIds: []string{"rk_role_prev_user"}})
			if err != nil {
				t.Fatalf("RotationCredentialExpiriesByUsernameKey: %v", err)
			}
			rows = len(r)
		}
		if rows != 1 {
			t.Fatalf("username-role expiry rows = %d, want 1", rows)
		}
	})
}
