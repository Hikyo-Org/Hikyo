package isolation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
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
	// Re-saving settings of an environment that was already protected pauses nothing new.
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
	err := envs.Delete(ctx, service.LocalPrincipal(alice), f.scope)
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
	err = envs.Delete(ctx, service.LocalPrincipal(alice), f.scope)
	if !errors.Is(err, service.ErrEnvironmentHasLiveRotation) || !strings.Contains(safeError(err), created.ID) {
		t.Fatalf("delete with a quarantined run = %v (%q)", err, safeError(err))
	}
	execRaw(t, db, `UPDATE rotation_runs SET phase='failed' WHERE id='run_blk_quarantine'`)

	// A failed run that had published may hold the live role.
	blocked("failed", "5", "failed_published")
	if err := envs.Delete(ctx, service.LocalPrincipal(alice), f.scope); !errors.Is(err, service.ErrEnvironmentHasLiveRotation) {
		t.Fatalf("delete with a failed run that had published = %v", err)
	}
	execRaw(t, db, `UPDATE rotation_runs SET phase='done' WHERE id='run_blk_failed_published'`)

	// A revoke the provider refused is non-terminal and blocks as well.
	blocked("revoke_refused", "6", "revoke_refused")
	if err := envs.Delete(ctx, service.LocalPrincipal(alice), f.scope); !errors.Is(err, service.ErrEnvironmentHasLiveRotation) {
		t.Fatalf("delete with a revoke_refused run = %v", err)
	}
	execRaw(t, db, `UPDATE rotation_runs SET phase='done' WHERE id='run_blk_revoke_refused'`)

	// Done runs and a pre-publish failure (candidate confirmed dropped) are purgeable.
	if err := envs.Delete(ctx, service.LocalPrincipal(alice), f.scope); err != nil {
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
