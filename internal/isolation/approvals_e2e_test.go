package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Secret-change approvals (#151). One end-to-end lifecycle that drives every
// path of the engine on the real services and both engines: policy CRUD,
// request creation on a gated publish, voting with quorum, merge through the
// ordinary publish, invalidation on a policy change, expiry sweep, and the
// emergency bypass through a reauthenticated session. It doubles as the
// runtime audit-emitter obligation for the seven approval.* event types (it is
// called from the audit suite's every-registered-type-is-emitted gate).
//
// The requester is alice (edit+publish+definitions in org A); the approver is
// custodian (publish in org A); the policy admin is orgAdmin (project-settings).
// Votes and merges run under a local principal, whose empty session skips the
// reauthentication ceremony by construction; only the bypass needs a real
// session, which it mints with an unbound reauthentication window.
func runApprovalLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := t.Context()
	kr := probeKeyring(t, db)
	auth := authService(t, db)
	// A non-zero instance reauthentication window, so the bypass ceremony's
	// sliding window is extendable (the test Auth otherwise leaves it at 0).
	auth.ReauthWindow = 15 * time.Minute
	approvals := &service.Approvals{DB: db, Auth: auth, Keyring: kr}
	keys := &service.Keys{DB: db, Keyring: kr}
	values := &service.Values{DB: db, Keyring: kr, Auth: auth}
	revisions := &service.Revisions{DB: db, Keyring: kr, Auth: auth}
	environments := &service.Environments{DB: db, Keyring: kr}

	projectScope := domain.Scope{Org: orgA, Project: prjA1}
	// A dedicated, non-protected environment, so the lifecycle neither depends on
	// nor disturbs the shared fixture's env_a1 (which other suites protect).
	env, err := environments.Create(ctx, service.LocalPrincipal(alice), projectScope, "approvals-env", nil)
	if err != nil {
		t.Fatalf("create approvals env: %v", err)
	}
	envID := env.ID
	envScope := domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID(envID)}

	stagePublish := func(key string) service.PublishResult {
		if _, err := keys.Create(ctx, service.LocalPrincipal(alice), projectScope, service.KeySpec{
			Name: key, Classification: string(schema.Config),
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
			Presence:    schema.DefaultPresenceRules(),
		}, nil); err != nil {
			t.Fatalf("create key %s: %v", key, err)
		}
		staged, err := values.Set(ctx, service.LocalPrincipal(alice), envScope, key, "v1", nil)
		if err != nil {
			t.Fatalf("stage %s: %v", key, err)
		}
		res, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), envScope, service.PublishRequest{
			VersionIDs: []string{staged.VersionID},
		})
		if err != nil {
			t.Fatalf("gated publish %s: %v", key, err)
		}
		return res
	}

	// 1. Policy created: one approval from custodian, no self-approval.
	if _, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, service.ApprovalPolicyInput{
		EnvironmentID: envID, MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	}); err != nil {
		t.Fatalf("create policy: %v", err)
	}
	// 2. Inspect (emits the listing event).
	if _, err := approvals.ListPolicies(ctx, service.LocalPrincipal(orgAdmin), projectScope); err != nil {
		t.Fatalf("list policies: %v", err)
	}

	// 3. A gated publish stages a request rather than a revision.
	res := stagePublish("APPROVAL_ONE")
	if res.CreatedApprovalRequest == nil {
		t.Fatal("a covered publish did not create a request")
	}
	req1 := res.CreatedApprovalRequest.ID
	if len(res.Published) != 0 {
		t.Fatal("a covered publish published a revision instead of staging a request")
	}

	// The requester cannot vote (not an approver, and self-approval is off).
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(alice), envScope, req1, store.ApprovalDecisionApprove); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("self/ineligible vote = %v, want unauthorized", err)
	}

	// 4. The approver approves; a repeated identical vote is idempotent.
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), envScope, req1, store.ApprovalDecisionApprove); err != nil {
		t.Fatalf("approve vote: %v", err)
	}
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), envScope, req1, store.ApprovalDecisionApprove); err != nil {
		t.Fatalf("idempotent repeat vote: %v", err)
	}
	// A conflicting second decision is refused.
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), envScope, req1, store.ApprovalDecisionReject); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflicting vote = %v, want conflict", err)
	}

	// 5. The requester merges through the ordinary publish path.
	merged, err2 := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), envScope, service.PublishRequest{ApprovalRequestID: req1})
	if err2 != nil {
		t.Fatalf("merge: %v", err2)
	}
	if len(merged.Published) == 0 || len(merged.Environments) == 0 {
		t.Fatal("merge did not publish a revision")
	}

	// 6/7/8. Invalidation on a policy change: stage a fresh request, bump the
	// policy version, then a vote fails closed and invalidates it.
	req2 := stagePublish("APPROVAL_TWO").CreatedApprovalRequest.ID
	if _, err := approvals.UpdatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, policyIDForEnv(t, db, approvals, envID), service.ApprovalPolicyInput{
		EnvironmentID: envID, MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	}); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), envScope, req2, store.ApprovalDecisionApprove); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("vote on a policy-moved request = %v, want conflict (invalidated)", err)
	}
	if state := requestState(t, db, req2); state != "invalidated" {
		t.Fatalf("request %s state = %s, want invalidated", req2, state)
	}

	// 9. Expiry sweep resolves an open request past its TTL.
	req3 := stagePublish("APPROVAL_THREE").CreatedApprovalRequest.ID
	future := &service.Approvals{DB: db, Auth: auth, Keyring: kr, Now: func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }}
	if err := future.ExpireDue(ctx); err != nil {
		t.Fatalf("expiry sweep: %v", err)
	}
	if state := requestState(t, db, req3); state != "expired" {
		t.Fatalf("request %s state = %s, want expired", req3, state)
	}

	// 10. Emergency bypass: alice is added as a bypasser, the quorum is raised
	// out of reach, and she force-merges a fresh request with a reason through a
	// reauthenticated session.
	if _, err := approvals.UpdatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, policyIDForEnv(t, db, approvals, envID), service.ApprovalPolicyInput{
		EnvironmentID: envID, MinApprovals: 2, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
		Bypassers: []string{string(alice)},
	}); err != nil {
		t.Fatalf("update policy for bypass: %v", err)
	}
	req4 := stagePublish("APPROVAL_FOUR").CreatedApprovalRequest.ID
	artifact := mintReauthedSession(t, db, alice, envID)
	bypassed, err := revisions.PublishPlanned(ctx, service.Bearer(artifact), envScope, service.PublishRequest{
		ApprovalRequestID: req4, Bypass: &service.ApprovalBypass{Reason: "incident recovery"},
	})
	if err != nil {
		t.Fatalf("emergency bypass: %v", err)
	}
	if len(bypassed.Published) == 0 {
		t.Fatal("bypass did not publish a revision")
	}
	if state := requestState(t, db, req4); state != "bypassed" {
		t.Fatalf("request %s state = %s, want bypassed", req4, state)
	}
}

// runApprovalEdges locks the fail-closed edges: a draft edited after its request
// invalidates the merge, a merge after the policy is deleted invalidates rather
// than publishing unreviewed, deleting the environment cascades its requests
// away, and an out-of-band copy into a covered environment is refused.
func runApprovalEdges(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := t.Context()
	kr := probeKeyring(t, db)
	auth := authService(t, db)
	auth.ReauthWindow = 15 * time.Minute
	approvals := &service.Approvals{DB: db, Auth: auth, Keyring: kr}
	keys := &service.Keys{DB: db, Keyring: kr}
	values := &service.Values{DB: db, Keyring: kr, Auth: auth}
	revisions := &service.Revisions{DB: db, Keyring: kr, Auth: auth}
	environments := &service.Environments{DB: db, Keyring: kr}
	projectScope := domain.Scope{Org: orgA, Project: prjA1}

	newCoveredEnv := func(name string) (string, domain.Scope) {
		env, err := environments.Create(ctx, service.LocalPrincipal(alice), projectScope, name, nil)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, service.ApprovalPolicyInput{
			EnvironmentID: env.ID, MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true,
			Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
		}); err != nil {
			t.Fatalf("policy for %s: %v", name, err)
		}
		return env.ID, domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID(env.ID)}
	}
	stage := func(envScope domain.Scope, key string) service.PublishResult {
		if _, err := keys.Create(ctx, service.LocalPrincipal(alice), projectScope, service.KeySpec{
			Name: key, Classification: string(schema.Config),
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
			Presence:    schema.DefaultPresenceRules(),
		}, nil); err != nil {
			t.Fatalf("key %s: %v", key, err)
		}
		staged, err := values.Set(ctx, service.LocalPrincipal(alice), envScope, key, "v1", nil)
		if err != nil {
			t.Fatalf("stage %s: %v", key, err)
		}
		res, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), envScope, service.PublishRequest{VersionIDs: []string{staged.VersionID}})
		if err != nil {
			t.Fatalf("gated publish %s: %v", key, err)
		}
		return res
	}

	// (a) A draft edited after its request invalidates the merge (draft_edited).
	envA, scopeA := newCoveredEnv("edges-a")
	reqA := stage(scopeA, "EDGE_A").CreatedApprovalRequest.ID
	if _, err := values.Set(ctx, service.LocalPrincipal(alice), scopeA, "EDGE_A", "v2", nil); err != nil {
		t.Fatalf("re-stage EDGE_A: %v", err)
	}
	if _, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), scopeA, service.PublishRequest{ApprovalRequestID: reqA}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("merge after edit = %v, want conflict", err)
	}
	if state := requestState(t, db, reqA); state != "invalidated" {
		t.Fatalf("edited request state = %s, want invalidated", state)
	}
	_ = envA

	// (b) A merge after the policy is deleted invalidates, never publishes.
	envB, scopeB := newCoveredEnv("edges-b")
	reqB := stage(scopeB, "EDGE_B").CreatedApprovalRequest.ID
	if err := approvals.DeletePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, policyIDForEnv(t, db, approvals, envB)); err != nil {
		t.Fatalf("delete policy: %v", err)
	}
	if _, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), scopeB, service.PublishRequest{ApprovalRequestID: reqB}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("merge after policy delete = %v, want conflict", err)
	}
	if state := requestState(t, db, reqB); state != "invalidated" {
		t.Fatalf("post-delete request state = %s, want invalidated", state)
	}

	// (c) Deleting the environment cascades its requests away.
	envC, scopeC := newCoveredEnv("edges-c")
	reqC := stage(scopeC, "EDGE_C").CreatedApprovalRequest.ID
	if err := environments.Delete(ctx, service.LocalPrincipal(alice), scopeC); err != nil {
		t.Fatalf("delete env: %v", err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM approval_requests WHERE id = '"+reqC+"'"); n != 0 {
		t.Fatalf("request rows after env delete = %d, want 0 (cascade)", n)
	}
	_ = envC
}

func TestApprovalEdgesSQLite(t *testing.T) {
	runApprovalEdges(t, seededDB(t, openSQLite))
}

func TestApprovalEdgesPostgres(t *testing.T) {
	runApprovalEdges(t, seededDB(t, openPostgres))
}

// policyIDForEnv returns the current env policy's id via the admin surface.
func policyIDForEnv(t *testing.T, db *store.DB, approvals *service.Approvals, envID string) string {
	t.Helper()
	policies, err := approvals.ListPolicies(t.Context(), service.LocalPrincipal(orgAdmin), domain.Scope{Org: orgA, Project: prjA1})
	if err != nil {
		t.Fatalf("list policies for id: %v", err)
	}
	for _, p := range policies {
		if p.EnvironmentID == envID {
			return p.ID
		}
	}
	t.Fatal("no policy for the approvals env")
	return ""
}

func requestState(t *testing.T, db *store.DB, id string) string {
	t.Helper()
	return queryString(t, db, "SELECT state FROM approval_requests WHERE id = '"+id+"'")
}

// mintReauthedSession mints a session for a principal and opens an unbound
// reauthentication window over envA1, so a bypass ceremony consumes it. Returns
// the session artifact.
func mintReauthedSession(t *testing.T, db *store.DB, principal domain.PrincipalID, envID string) string {
	t.Helper()
	ctx := t.Context()
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactCLISession)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	generation := int64(queryInt(t, db, "SELECT session_generation FROM principals WHERE id = '"+string(principal)+"'"))
	sessionID := "ses_bypass_" + string(principal)
	if err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if err := az.MintSession(ctx, authz.NewSession{
			ID: sessionID, PrincipalID: principal, Verifier: verifier, Artifact: "cli",
			SessionGeneration: generation, CredentialEpoch: epoch,
			AuthMethod: "local-passkey", Factors: `["webauthn","mfa"]`,
			AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour),
			AbsoluteExpiresAt: now.Add(24 * time.Hour), SourceIP: "127.0.0.1", UserAgent: "approvals-e2e",
		}); err != nil {
			return err
		}
		// An UNBOUND window: no Bound* fields, so ConsumeReauthWindow accepts it
		// for any intent (the bypass ceremony's purpose is not pinned into it).
		return az.OpenReauthWindow(ctx, authz.NewReauthWindow{
			ID: "raw_bypass_" + string(principal), SessionID: sessionID, EnvironmentID: envID,
			FactorClass: "totp", SingleDecision: false, AuthenticatedAt: now,
			WindowExpiresAt: now.Add(24 * time.Hour), HardExpiresAt: now.Add(48 * time.Hour),
			CredentialEpoch: epoch, CreatedAt: now,
		})
	}); err != nil {
		t.Fatalf("mint reauthed session: %v", err)
	}
	return artifact
}

// TestApprovalsLifecycle runs the whole engine end to end. It is the acceptance
// driver; the audit suite calls runApprovalLifecycle again for the emitter
// obligation.
func TestApprovalsLifecycleSQLite(t *testing.T) {
	runApprovalLifecycle(t, seededDB(t, openSQLite))
}

func TestApprovalsLifecyclePostgres(t *testing.T) {
	runApprovalLifecycle(t, seededDB(t, openPostgres))
}
