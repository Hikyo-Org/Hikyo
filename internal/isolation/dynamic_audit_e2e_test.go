package isolation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// fakeDynamicControl toggles the provider double's behaviour between worker
// runs so one lifecycle can drive success, ambiguity, and reconcile.
type fakeDynamicControl struct{ ambiguousDrop bool }

// fakeDynamicProvider stands in for a real PostgreSQL engine. Configuration,
// minting, the lease worker, effect ledger, and audit writes all traverse the
// real service/runtime/store boundaries; only the external engine is replaced.
type fakeDynamicProvider struct{ ctl *fakeDynamicControl }

func (fakeDynamicProvider) CreateRole(context.Context, dynamic.CreateRoleRequest) error { return nil }
func (fakeDynamicProvider) ExtendRole(context.Context, string, time.Time) error         { return nil }
func (p fakeDynamicProvider) DropRole(context.Context, string) error {
	if p.ctl.ambiguousDrop {
		return dynamic.ErrAmbiguous
	}
	return nil
}
func (fakeDynamicProvider) RoleStatus(context.Context, string) (dynamic.RoleStatus, error) {
	return dynamic.RoleStatus{Exists: false}, nil
}
func (fakeDynamicProvider) Close() {}

// runDynamicLifecycle drives every dynamic-secret audit event through the real
// service and worker so the registry-emitter closure check finds a row for each
// (#147). It runs against a provider double, since the isolation suite has no
// external PostgreSQL target; the lease state machine itself is real.
func runDynamicLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := tctx(t)
	projScope := domain.Scope{Org: orgA, Project: prjA1}
	envScope := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}

	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_dyn_manage','usr_alice','manage-identities','org_a','prj_a1',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_dyn_read','usr_alice','read','org_a','prj_a1','env_a1',`+ts+`)`)
	// The workload that mints: a real machine bearer, so its class resolves to
	// workload and the machine-reveal opt-in (not the human ceremony) is the
	// disclosure authority. The credential is minted BEFORE the workload is
	// granted any reveal-reachable environment, so the credential-mint's own
	// disclosure ceremony is vacuous (the delivery suite's technique).
	ident := identitySvc(db)
	sa, err := ident.CreateServiceAccount(ctx, service.LocalPrincipal(alice), prjScope(), "dyn-workload", domain.ClassWorkload)
	if err != nil {
		t.Fatalf("create workload: %v", err)
	}
	minted, err := ident.MintCredential(ctx, service.LocalPrincipal(alice), prjScope(), sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatalf("mint workload credential: %v", err)
	}
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_dyn_sa_read','`+string(sa.Principal)+`','read','org_a','prj_a1','env_a1',`+ts+`)`)
	// Mint requires read AND reveal over the environment even for a machine; the
	// opt-in enables machine reveal, the grant authorizes it here.
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_dyn_sa_reveal','`+string(sa.Principal)+`','reveal','org_a','prj_a1','env_a1',`+ts+`)`)
	execRaw(t, db, `UPDATE projects SET machine_reveal=true WHERE id='prj_a1'`)
	minter := service.Bearer(minted.Value)

	ctl := &fakeDynamicControl{}
	svc := &service.Dynamic{
		DB: db, Keyring: probeKeyring(t, db), Runtime: store.NewDynamicRuntime(db),
		LeaseDeadline: 5 * time.Second,
		ProviderFactory: func(dynamic.Kind, string, string, string) (dynamic.Provider, error) {
			return fakeDynamicProvider{ctl: ctl}, nil
		},
	}

	provider, err := svc.Configure(ctx, service.LocalPrincipal(alice), projScope, service.CreateDynamicProviderRequest{
		Kind: string(dynamic.KindPostgres), Origin: "postgres://admin@db.audit.example:5432/app",
		GrantRole: "app_reader", Credential: []byte("audit-admin-credential"),
	})
	if err != nil {
		t.Fatalf("dynamic configure: %v", err)
	}
	if _, err := svc.List(ctx, service.LocalPrincipal(alice), projScope); err != nil {
		t.Fatalf("dynamic list: %v", err)
	}
	if err := svc.ReplaceCredential(ctx, service.LocalPrincipal(alice), projScope, provider.ID, []byte("audit-admin-replacement")); err != nil {
		t.Fatalf("dynamic replace credential: %v", err)
	}

	mint, err := svc.MintLease(ctx, minter, envScope, service.MintLeaseRequest{
		ProviderID: provider.ID, MaxTTLSeconds: 3600,
	})
	if err != nil {
		t.Fatalf("dynamic mint: %v", err)
	}
	if mint.Password == "" || mint.Username == "" {
		t.Fatal("dynamic mint returned an empty display-once credential")
	}

	worker := func(tag string) {
		if _, err := svc.RunLeaseSweep(ctx, "audit-dynamic-worker"); err != nil {
			t.Fatalf("dynamic worker (%s): %v", tag, err)
		}
	}

	if _, err := svc.RenewLease(ctx, service.LocalPrincipal(alice), envScope, mint.Lease.ID, 0); err != nil {
		t.Fatalf("dynamic renew enqueue: %v", err)
	}
	worker("renew")

	if _, err := svc.RevokeLease(ctx, service.LocalPrincipal(alice), envScope, mint.Lease.ID); err != nil {
		t.Fatalf("dynamic revoke enqueue: %v", err)
	}
	ctl.ambiguousDrop = true
	worker("revoke-ambiguous") // ambiguous drop -> lease enters unknown
	ctl.ambiguousDrop = false

	if _, err := svc.SettleLease(ctx, service.LocalPrincipal(alice), envScope, mint.Lease.ID); err != nil {
		t.Fatalf("dynamic settle enqueue: %v", err)
	}
	worker("reconcile-settle") // unknown lease resumes the revoke and settles revoked

	if err := svc.RevokeCredential(ctx, service.LocalPrincipal(alice), projScope, provider.ID); err != nil {
		t.Fatalf("dynamic revoke credential: %v", err)
	}
	if _, err := svc.Delete(ctx, service.LocalPrincipal(alice), projScope, provider.ID, false); err != nil {
		t.Fatalf("dynamic delete: %v", err)
	}

	for _, typ := range []string{
		"dynamic.provider_configured", "dynamic.provider_inspected", "dynamic.provider_credential_replace",
		"dynamic.provider_credential_revoke", "dynamic.provider_deleted",
		"dynamic.lease_transition_intent", "dynamic.lease_transition_outcome", "dynamic.lease_disclosed",
		"dynamic.lease_settle_requested",
	} {
		if got := queryInt(t, db, fmt.Sprintf("SELECT COUNT(*) FROM audit_tenant_events WHERE type='%s'", typ)); got == 0 {
			t.Errorf("dynamic audit lifecycle did not emit %s", typ)
		}
	}
}
