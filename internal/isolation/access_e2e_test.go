package isolation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Approval-mediated temporary access (#152), end to end on the real services
// and both engines.
//
// The requester is reader (exactly `read` in org A); the approver is custodian
// (read/edit/publish/reveal in org A, so able to grant what reader asks for);
// the policy administrator is orgAdmin (manage-members in org A). Every
// request-side act runs in a real session, because temporary access refuses
// session-less callers by design.

type accessHarness struct {
	t      *testing.T
	db     *store.DB
	auth   *service.Auth
	access *service.Access
	values *service.Values
	grants *service.Grants
	env    string
	scope  domain.Scope
	proj   domain.Scope
	seq    int
}

func newAccessHarness(t *testing.T, db *store.DB) *accessHarness {
	t.Helper()
	kr := probeKeyring(t, db)
	auth := authService(t, db)
	auth.ReauthWindow = 15 * time.Minute
	environments := &service.Environments{DB: db, Keyring: kr}
	proj := domain.Scope{Org: orgA, Project: prjA1}
	env, err := environments.Create(t.Context(), service.LocalPrincipal(alice), proj, "access-env", nil)
	if err != nil {
		t.Fatalf("create access env: %v", err)
	}
	return &accessHarness{
		t: t, db: db, auth: auth,
		access: &service.Access{DB: db, Auth: auth},
		values: &service.Values{DB: db, Keyring: kr, Auth: auth},
		grants: &service.Grants{DB: db, Auth: auth},
		env:    env.ID, proj: proj,
		scope: domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID(env.ID)},
	}
}

// session mints a fresh session for principal (and, when reauth is set, an
// unbound reauthentication window over the harness environment) and returns
// its bearer actor.
func (h *accessHarness) session(principal domain.PrincipalID, reauth bool) service.Actor {
	h.t.Helper()
	return service.Bearer(h.sessionArtifact(principal, reauth))
}

// sessionArtifact is session, returning the presented bearer itself so a test
// can resolve the same session again under a different clock.
func (h *accessHarness) sessionArtifact(principal domain.PrincipalID, reauth bool) string {
	h.t.Helper()
	h.seq++
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactCLISession)
	if err != nil {
		h.t.Fatal(err)
	}
	now := time.Now().UTC()
	generation := int64(queryInt(h.t, h.db, "SELECT session_generation FROM principals WHERE id = '"+string(principal)+"'"))
	sessionID := fmt.Sprintf("ses_access_%s_%d", principal, h.seq)
	if err := tx.Write(h.t.Context(), h.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if err := az.MintSession(ctx, authz.NewSession{
			ID: sessionID, PrincipalID: principal, Verifier: verifier, Artifact: "cli",
			SessionGeneration: generation, CredentialEpoch: epoch,
			AuthMethod: "local-passkey", Factors: `["webauthn","mfa"]`,
			AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour),
			AbsoluteExpiresAt: now.Add(24 * time.Hour), SourceIP: "127.0.0.1", UserAgent: "access-e2e",
		}); err != nil {
			return err
		}
		if !reauth {
			return nil
		}
		return az.OpenReauthWindow(ctx, authz.NewReauthWindow{
			ID: fmt.Sprintf("raw_access_%s_%d", principal, h.seq), SessionID: sessionID, EnvironmentID: h.env,
			FactorClass: "totp", SingleDecision: false, AuthenticatedAt: now,
			WindowExpiresAt: now.Add(24 * time.Hour), HardExpiresAt: now.Add(48 * time.Hour),
			CredentialEpoch: epoch, CreatedAt: now,
		})
	}); err != nil {
		h.t.Fatalf("mint session: %v", err)
	}
	return artifact
}

func (h *accessHarness) policy(bypassers []string, approvers ...domain.PrincipalID) service.AccessPolicyInput {
	in := service.AccessPolicyInput{
		EnvironmentID: h.env, Capabilities: []string{"read", "reveal", "edit"},
		MaxDurationSeconds: 3600, MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true,
		Bypassers: bypassers,
	}
	for _, a := range approvers {
		in.Approvers = append(in.Approvers, service.ApprovalApproverSpec{Kind: "principal", SubjectID: string(a)})
	}
	return in
}

func (h *accessHarness) request(caps ...string) string {
	h.t.Helper()
	view, err := h.access.Request(h.t.Context(), h.session(reader, false), h.scope, service.AccessRequestInput{
		Capabilities: caps, DurationSeconds: 600, Reason: "investigate incident",
	})
	if err != nil {
		h.t.Fatalf("request: %v", err)
	}
	if view.State != "open" {
		h.t.Fatalf("new request state = %s, want open", view.State)
	}
	return view.ID
}

func (h *accessHarness) approve(id string) service.AccessRequestView {
	h.t.Helper()
	view, err := h.access.Vote(h.t.Context(), h.session(custodian, false), h.scope, id, "approve")
	if err != nil {
		h.t.Fatalf("approve: %v", err)
	}
	return view
}

// authorizedAt reports whether principal holds op at the harness environment
// with the transaction clock fixed at `at`.
func (h *accessHarness) authorizedAt(principal domain.PrincipalID, op authz.Operation, at time.Time) bool {
	h.t.Helper()
	var err error
	if werr := tx.Write(h.t.Context(), h.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		az.SetClock(at)
		_, err = az.Authorize(ctx, authz.Identity{Principal: principal}, op, h.scope)
		return nil
	}); werr != nil {
		h.t.Fatalf("authorize tx: %v", werr)
	}
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		h.t.Fatalf("authorize: %v", err)
	}
	return err == nil
}

// bearerAuthorizedAt is authorizedAt through a presented session: the bearer
// is resolved (session, generation and epoch checks) and op authorized with
// the transaction clock fixed at `at`, the path every network call takes.
func (h *accessHarness) bearerAuthorizedAt(artifact string, op authz.Operation, at time.Time) bool {
	h.t.Helper()
	var err error
	if werr := tx.Write(h.t.Context(), h.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		caller, aerr := az.AuthenticateCaller(ctx, artifact, at)
		if aerr != nil {
			return aerr
		}
		az.SetClock(at)
		_, err = az.Authorize(ctx, caller, op, h.scope)
		return nil
	}); werr != nil {
		h.t.Fatalf("authenticate %s at %s: %v", op, at, werr)
	}
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		h.t.Fatalf("authorize: %v", err)
	}
	return err == nil
}

func (h *accessHarness) state(id string) string {
	return queryString(h.t, h.db, "SELECT state FROM access_requests WHERE id = '"+id+"'")
}

func (h *accessHarness) liveRows(id string) int64 {
	return queryInt(h.t, h.db, "SELECT COUNT(*) FROM access_grants WHERE request_id = '"+id+"'")
}

func (h *accessHarness) expiresAt(id string) time.Time {
	h.t.Helper()
	var at time.Time
	if err := tx.Write(h.t.Context(), h.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		rows, err := az.LiveAccessGrants(ctx, reader, time.Now().UTC().Add(-time.Hour))
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.RequestID == id {
				at = row.ExpiresAt
			}
		}
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
	if at.IsZero() {
		h.t.Fatalf("request %s has no live temporary rows", id)
	}
	return at
}

func (h *accessHarness) future(d time.Duration) *service.Access {
	return &service.Access{DB: h.db, Auth: h.auth, Now: func() time.Time { return time.Now().UTC().Add(d) }}
}

func runAccessLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := t.Context()
	h := newAccessHarness(t, db)
	admin := service.LocalPrincipal(orgAdmin)

	// Nothing is requestable before an administrator defines a policy.
	if _, err := h.access.Request(ctx, h.session(reader, false), h.scope, service.AccessRequestInput{
		Capabilities: []string{"edit"}, DurationSeconds: 60, Reason: "x",
	}); !errors.Is(err, service.ErrAccessNotRequestable) {
		t.Fatalf("request without a policy = %v, want not requestable", err)
	}

	// 1. Policy administration: manage-members only, and never an
	// administrative capability.
	if _, err := h.access.CreatePolicy(ctx, service.LocalPrincipal(alice), h.proj, h.policy(nil, custodian)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("policy create without manage-members = %v, want not found", err)
	}
	bad := h.policy(nil, custodian)
	bad.Capabilities = []string{"manage-members"}
	if _, err := h.access.CreatePolicy(ctx, admin, h.proj, bad); !errors.Is(err, service.ErrAccessExceedsPolicy) {
		t.Fatalf("policy offering manage-members = %v, want refused", err)
	}
	created, err := h.access.CreatePolicy(ctx, admin, h.proj, h.policy(nil, custodian))
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	if _, err := h.access.ListPolicies(ctx, admin, h.proj); err != nil {
		t.Fatalf("list policies: %v", err)
	}
	queue, err := h.access.Queue(ctx, h.session(reader, false), h.scope)
	if err != nil || queue.Offer == nil || queue.Offer.MaxDurationSeconds != 3600 || queue.Offer.CallerMayBypass {
		t.Fatalf("queue offer = %+v, %v", queue.Offer, err)
	}

	// 2. Requests cannot exceed the policy, need a reason, and are refused to
	// a session-less caller.
	for name, in := range map[string]service.AccessRequestInput{
		"capability": {Capabilities: []string{"publish"}, DurationSeconds: 60, Reason: "x"},
		"duration":   {Capabilities: []string{"edit"}, DurationSeconds: 3601, Reason: "x"},
	} {
		if _, err := h.access.Request(ctx, h.session(reader, false), h.scope, in); !errors.Is(err, service.ErrAccessExceedsPolicy) {
			t.Fatalf("%s beyond the policy = %v, want refused", name, err)
		}
	}
	if _, err := h.access.Request(ctx, h.session(reader, false), h.scope, service.AccessRequestInput{
		Capabilities: []string{"edit"}, DurationSeconds: 60,
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("request without reason = %v, want invalid", err)
	}
	if _, err := h.access.Request(ctx, service.LocalPrincipal(reader), h.scope, service.AccessRequestInput{
		Capabilities: []string{"edit"}, DurationSeconds: 60, Reason: "x",
	}); !errors.Is(err, service.ErrAccessHumanOnly) {
		t.Fatalf("session-less request = %v, want human-only", err)
	}
	// A principal who cannot see the environment cannot even ask.
	if _, err := h.access.Request(ctx, h.session(nobody, false), h.scope, service.AccessRequestInput{
		Capabilities: []string{"edit"}, DurationSeconds: 60, Reason: "x",
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("request by an outsider = %v, want not found", err)
	}

	// 3. Before approval the requester cannot use the capability.
	req1 := h.request("edit", "reveal")
	if h.authorizedAt(reader, authz.OpValueStage, time.Now().UTC()) {
		t.Fatal("requester could stage before approval")
	}
	if _, err := h.values.Set(ctx, service.LocalPrincipal(reader), h.scope, "SHARED_KEY", "pending", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("stage before approval = %v, want not found", err)
	}

	// 4. Ineligible voters are refused: the requester, and a project member
	// manager who is not an approver.
	if _, err := h.access.Vote(ctx, h.session(reader, false), h.scope, req1, "approve"); !errors.Is(err, service.ErrAccessNotApprover) {
		t.Fatalf("self vote = %v, want not approver", err)
	}
	if _, err := h.access.Vote(ctx, h.session(prjAdmin, false), h.scope, req1, "approve"); !errors.Is(err, service.ErrAccessNotApprover) {
		t.Fatalf("non-approver vote = %v, want not approver", err)
	}

	// 5. Approval writes one time-bound grant per capability with an absolute
	// expiry; a repeated vote is idempotent, a conflicting one refused.
	preGrant := h.sessionArtifact(reader, false)
	before := time.Now().UTC()
	granted := h.approve(req1)
	if granted.State != "granted" || granted.ExpiresAt == nil {
		t.Fatalf("approved request = %+v, want granted with an expiry", granted)
	}
	if h.liveRows(req1) != 2 {
		t.Fatalf("temporary rows = %d, want 2", h.liveRows(req1))
	}
	expiry := h.expiresAt(req1)
	if expiry.Before(before.Add(600*time.Second)) || expiry.After(time.Now().UTC().Add(600*time.Second)) {
		t.Fatalf("expiry %s is not now+600s", expiry)
	}
	if _, err := h.access.Vote(ctx, h.session(custodian, false), h.scope, req1, "approve"); err != nil {
		t.Fatalf("idempotent approve: %v", err)
	}
	if _, err := h.access.Vote(ctx, h.session(custodian, false), h.scope, req1, "reject"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflicting vote = %v, want conflict", err)
	}

	// 6. Use: the capability works now, in a session minted BEFORE the grant
	// as well as a fresh one.
	if _, err := h.values.Set(ctx, service.Bearer(preGrant), h.scope, "SHARED_KEY", "temporary", nil); err != nil {
		t.Fatalf("stage under temporary access in a pre-grant session: %v", err)
	}
	if _, err := h.values.Set(ctx, h.session(reader, false), h.scope, "SHARED_KEY", "temporary-fresh", nil); err != nil {
		t.Fatalf("stage under temporary access in a fresh session: %v", err)
	}
	// 7. Expiry is evaluated on every operation: on that same pre-grant
	// session, one second before it holds and one second after it refuses,
	// before any sweep has run (C-ACC).
	if !h.bearerAuthorizedAt(preGrant, authz.OpValueStage, expiry.Add(-time.Second)) {
		t.Fatal("temporary access refused before its expiry")
	}
	if h.bearerAuthorizedAt(preGrant, authz.OpValueStage, expiry.Add(time.Second)) {
		t.Fatal("temporary access honoured on a pre-grant session one second after its expiry")
	}
	// 8. A permanent grant for the same triple outlives the temporary one, and
	// the temporary one never becomes permanent.
	if _, err := h.grants.Create(ctx, admin, service.GrantSpec{Target: reader, Capability: domain.CapReveal, Scope: h.scope}); err != nil {
		t.Fatalf("manual grant: %v", err)
	}
	if !h.authorizedAt(reader, authz.OpValueReveal, expiry.Add(time.Hour)) {
		t.Fatal("a manual grant did not outlive the temporary grant")
	}
	if h.authorizedAt(reader, authz.OpValueStage, expiry.Add(time.Hour)) {
		t.Fatal("temporary edit survived its expiry beside a manual reveal grant")
	}
	if err := h.grants.Revoke(ctx, admin, service.GrantSpec{Target: reader, Capability: domain.CapReveal, Scope: h.scope}); err != nil {
		t.Fatalf("revoke manual grant: %v", err)
	}
	// Temporary authority can be used but never re-granted: the temporary
	// rows are invisible to the grantor bound.
	if queryInt(t, db, "SELECT COUNT(*) FROM grants WHERE principal_id = 'usr_reader' AND capability = 'edit'") != 0 {
		t.Fatal("temporary access wrote a permanent grant row")
	}

	// 9. The expiry sweep releases the rows, rotates the holder's sessions and
	// resolves the request expired.
	generation := queryInt(t, db, "SELECT session_generation FROM principals WHERE id = 'usr_reader'")
	if _, err := h.future(2 * time.Hour).ExpireDue(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if h.state(req1) != "expired" || h.liveRows(req1) != 0 {
		t.Fatalf("after sweep state=%s rows=%d, want expired/0", h.state(req1), h.liveRows(req1))
	}
	if queryInt(t, db, "SELECT session_generation FROM principals WHERE id = 'usr_reader'") <= generation {
		t.Fatal("the sweep did not rotate the holder's sessions")
	}
	if _, err := h.future(2 * time.Hour).ExpireDue(ctx); err != nil {
		t.Fatalf("idempotent sweep: %v", err)
	}

	// 10. Early revocation by an approver takes effect immediately.
	req2 := h.request("edit")
	h.approve(req2)
	if !h.authorizedAt(reader, authz.OpValueStage, time.Now().UTC()) {
		t.Fatal("second grant not effective")
	}
	if _, err := h.access.Revoke(ctx, h.session(nobody, false), h.scope, req2); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("revoke by an outsider = %v, want not found", err)
	}
	if _, err := h.access.Revoke(ctx, h.session(custodian, false), h.scope, req2); err != nil {
		t.Fatalf("approver revoke: %v", err)
	}
	if h.state(req2) != "revoked" || h.liveRows(req2) != 0 || h.authorizedAt(reader, authz.OpValueStage, time.Now().UTC()) {
		t.Fatal("revocation did not end the access immediately")
	}
	if _, err := h.access.Revoke(ctx, h.session(custodian, false), h.scope, req2); err != nil {
		t.Fatalf("idempotent revoke: %v", err)
	}

	// 11. The holder may relinquish; a member manager may revoke.
	req3 := h.request("edit")
	h.approve(req3)
	if _, err := h.access.Revoke(ctx, h.session(reader, false), h.scope, req3); err != nil {
		t.Fatalf("self relinquish: %v", err)
	}
	req4 := h.request("edit")
	h.approve(req4)
	if _, err := h.access.Revoke(ctx, h.session(orgAdmin, false), h.scope, req4); err != nil {
		t.Fatalf("member-manager revoke: %v", err)
	}

	// 12. Cancel: requester only; reject: resolves immediately.
	req5 := h.request("edit")
	if _, err := h.access.Cancel(ctx, h.session(custodian, false), h.scope, req5); !errors.Is(err, service.ErrAccessNotYours) {
		t.Fatalf("cancel by another = %v, want not yours", err)
	}
	if _, err := h.access.Cancel(ctx, h.session(reader, false), h.scope, req5); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := h.access.Cancel(ctx, h.session(reader, false), h.scope, req5); err != nil {
		t.Fatalf("idempotent cancel: %v", err)
	}
	if _, err := h.access.Vote(ctx, h.session(custodian, false), h.scope, req5, "approve"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("vote on a cancelled request = %v, want conflict", err)
	}
	req6 := h.request("edit")
	if _, err := h.access.Vote(ctx, h.session(custodian, false), h.scope, req6, "reject"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if h.state(req6) != "rejected" || h.liveRows(req6) != 0 {
		t.Fatal("rejection did not resolve the request without a grant")
	}

	// 13. A review window that lapses expires the request (phase review).
	req7 := h.request("edit")
	if _, err := h.future(2 * time.Hour).ExpireDue(ctx); err != nil {
		t.Fatalf("review sweep: %v", err)
	}
	if h.state(req7) != "expired" {
		t.Fatalf("lapsed review state = %s, want expired", h.state(req7))
	}

	// 14. An approver who could not grant a capability cannot approve it.
	if _, err := h.access.UpdatePolicy(ctx, admin, h.proj, created.ID, h.policy(nil, custodian, prjAdmin)); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	req8 := h.request("reveal")
	if _, err := h.access.Vote(ctx, h.session(prjAdmin, false), h.scope, req8, "approve"); !errors.Is(err, service.ErrAccessApproverCannotGrant) {
		t.Fatalf("approval beyond the approver's authority = %v, want refused", err)
	}

	// 15. A policy change invalidates open requests; a disabled policy admits
	// none, and never extends granted access.
	req9 := h.request("edit")
	h.approve(req9)
	grantedExpiry := h.expiresAt(req9)
	if _, err := h.access.UpdatePolicy(ctx, admin, h.proj, created.ID, h.policy(nil, custodian)); err != nil {
		t.Fatalf("bump policy: %v", err)
	}
	if _, err := h.access.Vote(ctx, h.session(custodian, false), h.scope, req8, "approve"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("vote after policy change = %v, want conflict", err)
	}
	if h.state(req8) != "invalidated" {
		t.Fatalf("stale request state = %s, want invalidated", h.state(req8))
	}
	disabled := h.policy(nil, custodian)
	disabled.Enabled = false
	if _, err := h.access.UpdatePolicy(ctx, admin, h.proj, created.ID, disabled); err != nil {
		t.Fatalf("disable policy: %v", err)
	}
	if _, err := h.access.Request(ctx, h.session(reader, false), h.scope, service.AccessRequestInput{
		Capabilities: []string{"edit"}, DurationSeconds: 60, Reason: "x",
	}); !errors.Is(err, service.ErrAccessNotRequestable) {
		t.Fatalf("request under a disabled policy = %v, want not requestable", err)
	}
	if got := h.expiresAt(req9); !got.Equal(grantedExpiry) {
		t.Fatalf("disabling the policy moved the expiry %s -> %s", grantedExpiry, got)
	}
	if h.authorizedAt(reader, authz.OpValueStage, grantedExpiry.Add(time.Second)) {
		t.Fatal("granted access outlived its expiry after the policy was disabled")
	}

	// 16. Emergency access: bypassers only, reauthentication and a reason
	// required, time-bound, never touching the policy.
	if _, err := h.access.UpdatePolicy(ctx, admin, h.proj, created.ID, h.policy([]string{string(reader)}, custodian)); err != nil {
		t.Fatalf("add bypasser: %v", err)
	}
	emergency := service.AccessRequestInput{Capabilities: []string{"edit"}, Reason: "production is down"}
	if _, err := h.access.EmergencyAccess(ctx, h.session(custodian, true), h.scope, emergency); !errors.Is(err, service.ErrAccessNotBypasser) {
		t.Fatalf("emergency by a non-bypasser = %v, want refused", err)
	}
	if _, err := h.access.EmergencyAccess(ctx, h.session(reader, false), h.scope, emergency); !errors.Is(err, service.ErrNoReauthWindow) {
		t.Fatalf("emergency without reauthentication = %v, want reauth required", err)
	}
	if _, err := h.access.EmergencyAccess(ctx, h.session(reader, true), h.scope, service.AccessRequestInput{Capabilities: []string{"edit"}}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("emergency without a reason = %v, want invalid", err)
	}
	version := queryInt(t, db, "SELECT version FROM access_policies WHERE id = '"+created.ID+"'")
	bypass, err := h.access.EmergencyAccess(ctx, h.session(reader, true), h.scope, emergency)
	if err != nil {
		t.Fatalf("emergency access: %v", err)
	}
	if !bypass.Bypassed || bypass.State != "granted" || bypass.DurationSeconds != 3600 || bypass.ExpiresAt == nil {
		t.Fatalf("emergency view = %+v, want bypassed, granted, one hour", bypass)
	}
	if queryInt(t, db, "SELECT version FROM access_policies WHERE id = '"+created.ID+"'") != version {
		t.Fatal("emergency access mutated the policy")
	}
	if h.authorizedAt(reader, authz.OpValueStage, bypass.ExpiresAt.Add(time.Second)) {
		t.Fatal("emergency access is not time-bound")
	}
	if _, err := h.access.Revoke(ctx, h.session(custodian, false), h.scope, bypass.ID); err != nil {
		t.Fatalf("revoke emergency access: %v", err)
	}

	// 17. The chokepoint's principal gates apply to temporary rows exactly as
	// to permanent ones: a principal not reconciled up to the restore epoch,
	// and a restricted (removed) principal, hold nothing.
	req10 := h.request("edit")
	h.approve(req10)
	execRaw(t, db, "UPDATE principals SET reconciled_epoch = -1 WHERE id = 'usr_reader'")
	if h.authorizedAt(reader, authz.OpValueStage, time.Now().UTC()) {
		t.Fatal("temporary access survived an unreconciled restore")
	}
	execRaw(t, db, "UPDATE principals SET reconciled_epoch = 0 WHERE id = 'usr_reader'")
	execRaw(t, db, "UPDATE principals SET privacy_state = 'restricted' WHERE id = 'usr_reader'")
	if h.authorizedAt(reader, authz.OpValueStage, time.Now().UTC()) {
		t.Fatal("temporary access survived removing the principal")
	}
	execRaw(t, db, "UPDATE principals SET privacy_state = 'active' WHERE id = 'usr_reader'")
	if !h.authorizedAt(reader, authz.OpValueStage, time.Now().UTC()) {
		t.Fatal("restoring the principal did not restore its unexpired access")
	}

	// 17b. Deleting the policy records what was removed (enabled, one
	// approver, one bypasser), not zeroed defaults.
	if err := h.access.DeletePolicy(ctx, admin, h.proj, created.ID); err != nil {
		t.Fatalf("delete policy: %v", err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'access.policy_changed' AND object_id = '"+created.ID+
		`' AND payload LIKE '%"action":"deleted"%' AND payload LIKE '%"enabled":true%' AND payload LIKE '%"approver_count":1%' AND payload LIKE '%"bypasser_count":1%'`); n != 1 {
		t.Fatalf("policy deletion audit records = %d, want 1 carrying enabled and the member counts", n)
	}

	// 18. Deleting the environment cascades its requests and rows away.
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
	if err := envs.Delete(ctx, service.LocalPrincipal(alice), h.scope); err != nil {
		t.Fatalf("delete env: %v", err)
	}
	if queryInt(t, db, "SELECT COUNT(*) FROM access_grants WHERE env_id = '"+h.env+"'") != 0 ||
		queryInt(t, db, "SELECT COUNT(*) FROM access_requests WHERE environment_id = '"+h.env+"'") != 0 {
		t.Fatal("environment deletion left temporary access behind")
	}
}

// TestAccessLifecycle is the acceptance driver; the audit suite calls
// runAccessLifecycle again for the emitter obligation.
func TestAccessLifecycle(t *testing.T) {
	forEngines(t, runAccessLifecycle)
}

// TestAccessConcurrentApprovals: two approvers racing on a two-approval
// request from two service instances (two nodes) grant exactly once.
func TestAccessConcurrentApprovals(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		h := newAccessHarness(t, db)
		in := h.policy(nil, custodian, orgAdmin)
		in.MinApprovals = 2
		if _, err := h.access.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), h.proj, in); err != nil {
			t.Fatalf("create policy: %v", err)
		}
		id := h.request("edit")
		voters := []service.Actor{h.session(custodian, false), h.session(orgAdmin, false)}
		nodes := []*service.Access{{DB: db, Auth: h.auth}, {DB: db, Auth: h.auth}}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range 2 {
			wg.Go(func() {
				_, errs[i] = nodes[i].Vote(ctx, voters[i], h.scope, id, "approve")
			})
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil && !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("voter %d: %v", i, err)
			}
			if err != nil {
				// A serialization loser retries, as a client would.
				if _, err := nodes[i].Vote(ctx, voters[i], h.scope, id, "approve"); err != nil {
					t.Fatalf("voter %d retry: %v", i, err)
				}
			}
		}
		if h.state(id) != "granted" || h.liveRows(id) != 1 {
			t.Fatalf("state=%s rows=%d, want granted with exactly one row", h.state(id), h.liveRows(id))
		}
	})
}

// TestAccessExpiryAcrossNodes: a grant expires on every node at the same
// instant without any sweep, and a sweep from a second node finishes the
// bookkeeping idempotently.
func TestAccessExpiryAcrossNodes(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		h := newAccessHarness(t, db)
		if _, err := h.access.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), h.proj, h.policy(nil, custodian)); err != nil {
			t.Fatalf("create policy: %v", err)
		}
		id := h.request("edit")
		h.approve(id)
		expiry := h.expiresAt(id)
		// Node A and node B are simply two transactions: neither consults any
		// local state, so both refuse after expiry.
		for node := range 2 {
			if h.authorizedAt(reader, authz.OpValueStage, expiry.Add(time.Millisecond)) {
				t.Fatalf("node %d honoured an expired grant", node)
			}
		}
		for range 2 {
			if _, err := h.future(time.Hour).ExpireDue(ctx); err != nil {
				t.Fatalf("sweep: %v", err)
			}
		}
		if h.state(id) != "expired" {
			t.Fatalf("state = %s, want expired", h.state(id))
		}
	})
}

func TestAccessEmergencyRejectsDurationOverflow(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h := newAccessHarness(t, db)
		if _, err := h.access.CreatePolicy(t.Context(), service.LocalPrincipal(orgAdmin), h.proj, h.policy([]string{string(reader)}, custodian)); err != nil {
			t.Fatal(err)
		}
		actor := h.session(reader, false)
		for _, seconds := range []int{-1, 3601, 18446744074 + 3600} {
			_, err := h.access.EmergencyAccess(t.Context(), actor, h.scope, service.AccessRequestInput{Capabilities: []string{"edit"}, DurationSeconds: seconds, Reason: "restore service"})
			if !errors.Is(err, service.ErrAccessExceedsPolicy) {
				t.Fatalf("duration %d: %v", seconds, err)
			}
		}
	})
}

func TestAccessEmergencyPolicyCannotBypassGrantorBound(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h := newAccessHarness(t, db)
		manager := service.LocalPrincipal(domain.PrincipalID("usr_prjadmin"))
		input := h.policy([]string{"usr_prjadmin"}, custodian)
		if _, err := h.access.CreatePolicy(t.Context(), manager, h.proj, input); !errors.Is(err, service.ErrGrantorLacksCapability) {
			t.Fatalf("project manager created unheld emergency delegation: %v", err)
		}
		input.Bypassers = nil
		policy, err := h.access.CreatePolicy(t.Context(), manager, h.proj, input)
		if err != nil {
			t.Fatalf("ordinary approval policy: %v", err)
		}
		input.Bypassers = []string{"usr_prjadmin"}
		if _, err := h.access.UpdatePolicy(t.Context(), manager, h.proj, policy.ID, input); !errors.Is(err, service.ErrGrantorLacksCapability) {
			t.Fatalf("project manager added unheld emergency delegation: %v", err)
		}
		if _, err := h.access.UpdatePolicy(t.Context(), service.LocalPrincipal(orgAdmin), h.proj, policy.ID, input); err != nil {
			t.Fatalf("org admin emergency delegation: %v", err)
		}
		input.Enabled = false
		if _, err := h.access.UpdatePolicy(t.Context(), manager, h.proj, policy.ID, input); err != nil {
			t.Fatalf("manager could not disable emergency delegation: %v", err)
		}
		input.Enabled = true
		if _, err := h.access.UpdatePolicy(t.Context(), manager, h.proj, policy.ID, input); !errors.Is(err, service.ErrGrantorLacksCapability) {
			t.Fatalf("manager reenabled unheld emergency delegation: %v", err)
		}
	})
}
