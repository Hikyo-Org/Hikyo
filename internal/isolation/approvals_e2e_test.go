package isolation

import (
	"context"
	"errors"
	"sync"
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
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(alice), envScope, req1, "approve"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("self/ineligible vote = %v, want unauthorized", err)
	}

	// 4. The approver approves; a repeated identical vote is idempotent.
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), envScope, req1, "approve"); err != nil {
		t.Fatalf("approve vote: %v", err)
	}
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), envScope, req1, "approve"); err != nil {
		t.Fatalf("idempotent repeat vote: %v", err)
	}
	// A conflicting second decision is refused.
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), envScope, req1, "reject"); !errors.Is(err, domain.ErrConflict) {
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

	// A rejection resolves immediately, but replaying that exact decision stays
	// idempotent instead of becoming a terminal-state conflict.
	rejectedID := stagePublish("APPROVAL_REJECT").CreatedApprovalRequest.ID
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), envScope, rejectedID, "reject"); err != nil {
		t.Fatalf("reject vote: %v", err)
	}
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), envScope, rejectedID, "reject"); err != nil {
		t.Fatalf("idempotent repeat reject: %v", err)
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
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), envScope, req2, "approve"); !errors.Is(err, domain.ErrConflict) {
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

	tooMany := make([]service.ApprovalApproverSpec, 101)
	for i := range tooMany {
		tooMany[i] = service.ApprovalApproverSpec{Kind: "principal", SubjectID: string(custodian)}
	}
	if _, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, service.ApprovalPolicyInput{
		MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true, Approvers: tooMany,
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("create policy with 101 approvers = %v, want invalid", err)
	}
	tooManyBypassers := make([]string, 101)
	for i := range tooManyBypassers {
		tooManyBypassers[i] = string(alice)
	}
	if _, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, service.ApprovalPolicyInput{
		MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
		Bypassers: tooManyBypassers,
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("create policy with 101 bypassers = %v, want invalid", err)
	}

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
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), scopeA, reqA, "approve"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("vote after edit = %v, want conflict", err)
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

	// (d) Policy scope is immutable, and an environment-policy request cannot
	// be merged under a project-wide fallback merely because both are version 1.
	envD, scopeD := newCoveredEnv("edges-d")
	policyD := policyIDForEnv(t, db, approvals, envD)
	other, err := environments.Create(ctx, service.LocalPrincipal(alice), projectScope, "edges-other", nil)
	if err != nil {
		t.Fatalf("create other environment: %v", err)
	}
	if _, err := approvals.UpdatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, policyD, service.ApprovalPolicyInput{
		EnvironmentID: other.ID, MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("move policy scope = %v, want invalid", err)
	}
	reqD := stage(scopeD, "EDGE_D").CreatedApprovalRequest.ID
	if _, err := approvals.UpdatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, policyD, service.ApprovalPolicyInput{
		EnvironmentID: envD, MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: false,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	}); err != nil {
		t.Fatalf("disable environment policy: %v", err)
	}
	fallback, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, service.ApprovalPolicyInput{
		MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	})
	if err != nil {
		t.Fatalf("create project fallback policy: %v", err)
	}
	if _, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), scopeD,
		service.PublishRequest{ApprovalRequestID: reqD}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("merge under replacement policy = %v, want conflict", err)
	}
	if state := requestState(t, db, reqD); state != "invalidated" {
		t.Fatalf("replacement-policy request state = %s, want invalidated", state)
	}
	if _, err := approvals.UpdatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, fallback.ID, service.ApprovalPolicyInput{
		MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: false,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	}); err != nil {
		t.Fatalf("disable project fallback policy: %v", err)
	}

	// (e) A covered copy destination refuses before source secret material is
	// opened, so the durable disclosure count cannot be erased by rollback.
	source, err := environments.Create(ctx, service.LocalPrincipal(alice), projectScope, "edges-copy-source", nil)
	if err != nil {
		t.Fatalf("create copy source: %v", err)
	}
	destination, err := environments.Create(ctx, service.LocalPrincipal(alice), projectScope, "edges-copy-destination", nil)
	if err != nil {
		t.Fatalf("create copy destination: %v", err)
	}
	if _, err := keys.Create(ctx, service.LocalPrincipal(alice), projectScope, service.KeySpec{
		Name: "EDGE_COPY_SECRET", Classification: string(schema.Secret),
		Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
		Presence:    schema.DefaultPresenceRules(),
	}, nil); err != nil {
		t.Fatalf("create copy key: %v", err)
	}
	sourceScope := domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID(source.ID)}
	staged, err := values.Set(ctx, service.LocalPrincipal(alice), sourceScope, "EDGE_COPY_SECRET", "secret", nil)
	if err != nil {
		t.Fatalf("stage copy source: %v", err)
	}
	if _, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), sourceScope, service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
		t.Fatalf("publish copy source: %v", err)
	}
	if _, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, service.ApprovalPolicyInput{
		EnvironmentID: destination.ID, MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	}); err != nil {
		t.Fatalf("create copy destination policy: %v", err)
	}
	grants := &service.Grants{DB: db}
	for _, target := range []string{source.ID, destination.ID} {
		if _, err := grants.Create(ctx, service.LocalPrincipal(orgAdmin), service.GrantSpec{
			Target: alice, Capability: domain.CapReveal,
			Scope: domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID(target)},
		}); err != nil {
			t.Fatalf("grant reveal on %s: %v", target, err)
		}
	}
	beforeDisclosures := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'disclosure.value_revealed'")
	if _, err := values.Copy(ctx, service.LocalPrincipal(alice), projectScope, service.CopyRequest{
		SourceEnvironmentID: source.ID, KeyNames: []string{"EDGE_COPY_SECRET"},
		DestinationEnvironmentIDs: []string{destination.ID},
	}); !errors.Is(err, service.ErrApprovalRequired) {
		t.Fatalf("copy into covered destination = %v, want approval required", err)
	}
	if after := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'disclosure.value_revealed'"); after != beforeDisclosures {
		t.Fatalf("covered copy opened source material: disclosures %d -> %d", beforeDisclosures, after)
	}

	// (f) An approved restore request already pins the preview digest. Its merge
	// must not demand the one-shot preview token a second time.
	restoreEnv, err := environments.Create(ctx, service.LocalPrincipal(alice), projectScope, "edges-restore", nil)
	if err != nil {
		t.Fatalf("create restore environment: %v", err)
	}
	restoreScope := domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID(restoreEnv.ID)}
	if _, err := keys.Create(ctx, service.LocalPrincipal(alice), projectScope, service.KeySpec{
		Name: "EDGE_RESTORE", Classification: string(schema.Config),
		Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
		Presence:    schema.DefaultPresenceRules(),
	}, nil); err != nil {
		t.Fatalf("create restore key: %v", err)
	}
	first, err := values.Set(ctx, service.LocalPrincipal(alice), restoreScope, "EDGE_RESTORE", "v1", nil)
	if err != nil {
		t.Fatalf("stage first restore value: %v", err)
	}
	firstPublished, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), restoreScope, service.PublishRequest{VersionIDs: []string{first.VersionID}})
	if err != nil {
		t.Fatalf("publish first restore value: %v", err)
	}
	second, err := values.Set(ctx, service.LocalPrincipal(alice), restoreScope, "EDGE_RESTORE", "v2", nil)
	if err != nil {
		t.Fatalf("stage second restore value: %v", err)
	}
	if _, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), restoreScope, service.PublishRequest{VersionIDs: []string{second.VersionID}}); err != nil {
		t.Fatalf("publish second restore value: %v", err)
	}
	restored, err := revisions.Restore(ctx, service.LocalPrincipal(alice), restoreScope, firstPublished.Environments[0].Revision, "EDGE_RESTORE")
	if err != nil {
		t.Fatalf("stage restore: %v", err)
	}
	if len(restored.Changes) != 1 || restored.Preview.Token == "" {
		t.Fatalf("restore staged %d changes with token %q, want one change and a preview token", len(restored.Changes), restored.Preview.Token)
	}
	if _, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, service.ApprovalPolicyInput{
		EnvironmentID: restoreEnv.ID, MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	}); err != nil {
		t.Fatalf("create restore policy: %v", err)
	}
	beforeRestoreRequests := queryInt(t, db, "SELECT COUNT(*) FROM approval_requests WHERE environment_id = '"+restoreEnv.ID+"'")
	for name, token := range map[string]string{"missing": "", "forged": "forged-preview-token"} {
		if _, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), restoreScope, service.PublishRequest{
			VersionIDs: []string{restored.Changes[0].VersionID}, PreviewToken: token,
		}); !errors.Is(err, service.ErrStalePreview) {
			t.Fatalf("%s restore preview = %v, want stale preview", name, err)
		}
	}
	if after := queryInt(t, db, "SELECT COUNT(*) FROM approval_requests WHERE environment_id = '"+restoreEnv.ID+"'"); after != beforeRestoreRequests {
		t.Fatalf("invalid restore preview persisted %d requests, want %d", after, beforeRestoreRequests)
	}
	restoreRequest, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), restoreScope, service.PublishRequest{
		VersionIDs: []string{restored.Changes[0].VersionID}, PreviewToken: restored.Preview.Token,
	})
	if err != nil {
		t.Fatalf("create restore approval request: %v", err)
	}
	if restoreRequest.CreatedApprovalRequest == nil {
		t.Fatal("restore publish did not create an approval request")
	}
	restoreRequestID := restoreRequest.CreatedApprovalRequest.ID
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), restoreScope, restoreRequestID, "approve"); err != nil {
		t.Fatalf("approve restore request: %v", err)
	}
	mergedRestore, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), restoreScope, service.PublishRequest{
		ApprovalRequestID: restoreRequestID,
	})
	if err != nil {
		t.Fatalf("merge approved restore without preview token: %v", err)
	}
	if len(mergedRestore.Published) != 1 {
		t.Fatalf("approved restore published %d changes, want 1", len(mergedRestore.Published))
	}

	// (g) An approved request loses quorum when its recorded approver loses
	// publish authority. Merge persists an approver_removed invalidation.
	_, quorumScope := newCoveredEnv("edges-quorum")
	quorumRequestID := stage(quorumScope, "EDGE_QUORUM").CreatedApprovalRequest.ID
	if _, err := approvals.Vote(ctx, service.LocalPrincipal(custodian), quorumScope, quorumRequestID, "approve"); err != nil {
		t.Fatalf("approve quorum request: %v", err)
	}
	if err := (&service.Grants{DB: db}).Revoke(ctx, service.LocalPrincipal(orgAdmin), service.GrantSpec{
		Target: custodian, Capability: domain.CapPublish, Scope: domain.Scope{Org: orgA},
	}); err != nil {
		t.Fatalf("revoke custodian publish: %v", err)
	}
	if _, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), quorumScope,
		service.PublishRequest{ApprovalRequestID: quorumRequestID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("merge after approver removal = %v, want conflict", err)
	}
	if state := requestState(t, db, quorumRequestID); state != "invalidated" {
		t.Fatalf("quorum-lost request state = %s, want invalidated", state)
	}
	if cause := queryString(t, db, "SELECT invalidated_cause FROM approval_requests WHERE id = '"+quorumRequestID+"'"); cause != "approver_removed" {
		t.Fatalf("quorum-lost invalidation cause = %s, want approver_removed", cause)
	}
}

func TestApprovalEdges(t *testing.T) {
	forEngines(t, runApprovalEdges)
}

// runProtectedApprovalBypass proves a protected bypass spends only its bound
// PurposeBypass decision. Requiring a second PurposePublish decision would make
// the single-decision passkey window impossible to use.
func runProtectedApprovalBypass(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := t.Context()
	ceremony := ceremonyFixture(t, db, "approval-protected-bypass")
	auth, token := ceremony.admin.auth, ceremony.admin.token
	auth.ReauthHardCap = time.Hour
	kr := probeKeyring(t, db)
	scope := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
	if _, err := (&service.ProjectSettings{DB: db, Auth: auth}).SetEnvironment(ctx, service.Bearer(token), scope,
		service.EnvironmentSettings{Protected: true}); err != nil {
		t.Fatalf("protect approval environment: %v", err)
	}
	approvals := &service.Approvals{DB: db, Auth: auth, Keyring: kr}
	requester := ceremony.admin.boot.PrincipalID
	if _, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), domain.Scope{Org: orgA, Project: prjA1}, service.ApprovalPolicyInput{
		EnvironmentID: string(envA1), MinApprovals: 2, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
		Bypassers: []string{string(requester)},
	}); err != nil {
		t.Fatalf("create protected bypass policy: %v", err)
	}
	staged, err := ceremony.values.Set(ctx, service.Bearer(token), scope, ceremonySecretA, "bypassed", nil)
	if err != nil {
		t.Fatalf("stage protected bypass value: %v", err)
	}
	revisions := &service.Revisions{DB: db, Keyring: kr, Auth: auth}
	created, err := revisions.PublishPlanned(ctx, service.Bearer(token), scope,
		service.PublishRequest{VersionIDs: []string{staged.VersionID}})
	if err != nil || created.CreatedApprovalRequest == nil {
		t.Fatalf("create protected bypass request: result=%+v err=%v", created, err)
	}
	decision := passkeyCeremony(t, auth, ctx, token, service.PurposeBypass, string(envA1),
		[]string{"key_" + ceremonySecretA}, ceremony.device)
	token = decision.SessionToken
	bypassed, err := revisions.PublishPlanned(ctx, service.Bearer(token), scope, service.PublishRequest{
		ApprovalRequestID: created.CreatedApprovalRequest.ID,
		Bypass:            &service.ApprovalBypass{Reason: "protected incident recovery"},
	})
	if err != nil {
		t.Fatalf("protected emergency bypass: %v", err)
	}
	if len(bypassed.Published) != 1 {
		t.Fatalf("protected bypass published %d changes, want 1", len(bypassed.Published))
	}
}

func TestProtectedApprovalBypass(t *testing.T) {
	forEngines(t, runProtectedApprovalBypass)
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
func TestApprovalsLifecycle(t *testing.T) {
	forEngines(t, runApprovalLifecycle)
}

func runConcurrentApprovalVotes(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := t.Context()
	kr := probeKeyring(t, db)
	auth := authService(t, db)
	projectScope := domain.Scope{Org: orgA, Project: prjA1}
	if _, err := (&service.Grants{DB: db}).Create(ctx, service.LocalPrincipal(orgAdmin), service.GrantSpec{
		Target: bob, Capability: domain.CapPublish, Scope: domain.Scope{Org: orgA},
	}); err != nil {
		t.Fatalf("grant second approver publish: %v", err)
	}
	env, err := (&service.Environments{DB: db, Keyring: kr}).Create(ctx, service.LocalPrincipal(alice), projectScope, "approval-concurrency", nil)
	if err != nil {
		t.Fatalf("create concurrency environment: %v", err)
	}
	scope := domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID(env.ID)}
	if _, err := (&service.Keys{DB: db, Keyring: kr}).Create(ctx, service.LocalPrincipal(alice), projectScope, service.KeySpec{
		Name: "APPROVAL_CONCURRENT", Classification: string(schema.Config),
		Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
		Presence:    schema.DefaultPresenceRules(),
	}, nil); err != nil {
		t.Fatalf("create concurrency key: %v", err)
	}
	staged, err := (&service.Values{DB: db, Keyring: kr, Auth: auth}).Set(ctx, service.LocalPrincipal(alice), scope, "APPROVAL_CONCURRENT", "v1", nil)
	if err != nil {
		t.Fatalf("stage concurrency value: %v", err)
	}
	nodeA := &service.Approvals{DB: db, Auth: auth, Keyring: kr}
	nodeB := &service.Approvals{DB: db, Auth: auth, Keyring: kr}
	if _, err := nodeA.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, service.ApprovalPolicyInput{
		EnvironmentID: env.ID, MinApprovals: 2, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{
			{Kind: "principal", SubjectID: string(custodian)},
			{Kind: "principal", SubjectID: string(bob)},
		},
	}); err != nil {
		t.Fatalf("create concurrency policy: %v", err)
	}
	created, err := (&service.Revisions{DB: db, Keyring: kr, Auth: auth}).PublishPlanned(ctx,
		service.LocalPrincipal(alice), scope, service.PublishRequest{VersionIDs: []string{staged.VersionID}})
	if err != nil || created.CreatedApprovalRequest == nil {
		t.Fatalf("create concurrency request: result=%+v err=%v", created, err)
	}
	requestID := created.CreatedApprovalRequest.ID
	actors := []struct {
		node  *service.Approvals
		actor domain.PrincipalID
	}{{nodeA, custodian}, {nodeB, bob}}
	errs := make(chan error, len(actors))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, vote := range actors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := vote.node.Vote(ctx, service.LocalPrincipal(vote.actor), scope, requestID, "approve")
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent vote: %v", err)
		}
	}
	if state := requestState(t, db, requestID); state != "approved" {
		t.Fatalf("concurrent request state = %s, want approved", state)
	}
	if votes := queryInt(t, db, "SELECT COUNT(*) FROM approval_votes WHERE request_id = '"+requestID+"'"); votes != 2 {
		t.Fatalf("concurrent vote rows = %d, want 2", votes)
	}
}

func TestConcurrentApprovalVotes(t *testing.T) {
	forEngines(t, runConcurrentApprovalVotes)
}

func runApprovalExpiryBatches(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := t.Context()
	kr := probeKeyring(t, db)
	auth := authService(t, db)
	base := time.Now().UTC().Add(-time.Minute)
	approvals := &service.Approvals{DB: db, Auth: auth, Keyring: kr, Now: func() time.Time { return base }}
	projectScope := domain.Scope{Org: orgA, Project: prjA1}
	env, err := (&service.Environments{DB: db, Keyring: kr}).Create(ctx, service.LocalPrincipal(alice), projectScope, "approval-expiry-batch", nil)
	if err != nil {
		t.Fatalf("create expiry environment: %v", err)
	}
	scope := domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID(env.ID)}
	if _, err := (&service.Keys{DB: db, Keyring: kr}).Create(ctx, service.LocalPrincipal(alice), projectScope, service.KeySpec{
		Name: "APPROVAL_EXPIRY_BATCH", Classification: string(schema.Config),
		Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
		Presence:    schema.DefaultPresenceRules(),
	}, nil); err != nil {
		t.Fatalf("create expiry key: %v", err)
	}
	if _, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), projectScope, service.ApprovalPolicyInput{
		EnvironmentID: env.ID, MinApprovals: 1, RequestTTLSeconds: 1, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	}); err != nil {
		t.Fatalf("create expiry policy: %v", err)
	}
	staged, err := (&service.Values{DB: db, Keyring: kr, Auth: auth}).Set(ctx, service.LocalPrincipal(alice), scope, "APPROVAL_EXPIRY_BATCH", "v1", nil)
	if err != nil {
		t.Fatalf("stage expiry value: %v", err)
	}
	revisions := &service.Revisions{DB: db, Keyring: kr, Auth: auth}
	for i := 0; i < 101; i++ {
		if result, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(alice), scope,
			service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil || result.CreatedApprovalRequest == nil {
			t.Fatalf("create expiry request %d: result=%+v err=%v", i, result, err)
		}
	}
	sweeper := &service.Approvals{DB: db, Auth: auth, Keyring: kr, Now: func() time.Time { return time.Now().UTC().Add(time.Hour) }}
	if err := sweeper.ExpireDue(ctx); err != nil {
		t.Fatalf("first expiry batch: %v", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM approval_requests WHERE environment_id = '"+env.ID+"' AND state = 'expired'"); got != 100 {
		t.Fatalf("first expiry batch = %d, want 100", got)
	}
	if err := sweeper.ExpireDue(ctx); err != nil {
		t.Fatalf("second expiry batch: %v", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM approval_requests WHERE environment_id = '"+env.ID+"' AND state = 'expired'"); got != 101 {
		t.Fatalf("drained expiry backlog = %d, want 101", got)
	}
}

func TestApprovalExpiryBatches(t *testing.T) {
	forEngines(t, runApprovalExpiryBatches)
}
