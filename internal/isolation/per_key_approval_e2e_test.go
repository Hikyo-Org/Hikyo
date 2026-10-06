package isolation

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestPerKeyApprovalPublish(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedRulesFixture(t, db)
		ctx := t.Context()
		kr := probeKeyring(t, db)
		auth := authService(t, db)
		auth.ReauthWindow = 15 * time.Minute
		approvals := &service.Approvals{DB: db, Auth: auth, Keyring: kr}
		revisions := &service.Revisions{DB: db, Auth: auth, Keyring: kr}
		project := scopeProject(orgA, prjA1)
		env, err := (&service.Environments{DB: db, Keyring: kr}).Create(ctx, service.LocalPrincipal(custodian), project, "per-key-approval", nil)
		if err != nil {
			t.Fatal(err)
		}
		scope := scopeEnv(orgA, prjA1, domain.EnvID(env.ID))
		mode, envs := onlyEnvs(domain.EnvID(env.ID))
		where := whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})
		for _, capability := range []domain.Capability{domain.CapEdit, domain.CapPublish} {
			f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: capability, Org: orgA, Where: where})
		}
		approverRule := f.create(t, orgAdmin, service.RuleSpec{Target: hank, Capability: domain.CapPublish, Org: orgA, Where: where})
		if _, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), project, service.ApprovalPolicyInput{EnvironmentID: env.ID, MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true, Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(hank)}}, Bypassers: []string{string(frank)}}); err != nil {
			t.Fatal(err)
		}
		stageRequest := func(actor domain.PrincipalID, name, value string) string {
			t.Helper()
			staged, err := f.values.Set(ctx, service.LocalPrincipal(actor), scope, name, value, nil)
			if err != nil {
				t.Fatalf("stage %s: %v", name, err)
			}
			result, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(actor), scope, service.PublishRequest{VersionIDs: []string{staged.VersionID}})
			if err != nil || result.CreatedApprovalRequest == nil {
				t.Fatalf("create %s approval: %+v %v", name, result, err)
			}
			return result.CreatedApprovalRequest.ID
		}
		request := stageRequest(frank, "DB_PASSWORD", "approved-narrowed")
		if binding, err := approvals.CeremonyBinding(ctx, service.LocalPrincipal(hank), scope, request); err != nil || len(binding.KeyIDs) != 1 || binding.KeyIDs[0] != f.dbKeyID {
			t.Fatalf("bind narrowed cross-owner approval: %+v %v", binding, err)
		}
		sibling := stageRequest(custodian, "STRIPE_KEY", "outside-scope")
		if _, err := approvals.CeremonyBinding(ctx, service.LocalPrincipal(hank), scope, sibling); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("bind sibling approval: %v", err)
		}
		if _, err := approvals.Vote(ctx, service.LocalPrincipal(hank), scope, sibling, "approve"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("vote sibling approval: %v", err)
		}
		if _, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(frank), scope, service.PublishRequest{ApprovalRequestID: sibling}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("merge sibling approval: %v", err)
		}
		// Admission through the first key never authorizes the second pinned
		// key, even though both belong to one environment and one request.
		first, err := f.values.Set(ctx, service.LocalPrincipal(custodian), scope, "DB_PASSWORD", "two-key-db", nil)
		if err != nil {
			t.Fatal(err)
		}
		second, err := f.values.Set(ctx, service.LocalPrincipal(custodian), scope, "STRIPE_KEY", "two-key-stripe", nil)
		if err != nil {
			t.Fatal(err)
		}
		multi, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(custodian), scope, service.PublishRequest{VersionIDs: []string{first.VersionID, second.VersionID}})
		if err != nil || multi.CreatedApprovalRequest == nil {
			t.Fatalf("create multi-key control request: %+v %v", multi, err)
		}
		multiID := multi.CreatedApprovalRequest.ID
		multiBinding, err := approvals.CeremonyBinding(ctx, service.LocalPrincipal(custodian), scope, multiID)
		if err != nil || len(multiBinding.KeyIDs) != 2 {
			t.Fatalf("control multi-key ceremony binding: %+v %v", multiBinding, err)
		}
		firstWhere := whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: multiBinding.KeyIDs[0]})
		f.create(t, orgAdmin, service.RuleSpec{Target: carol, Capability: domain.CapPublish, Org: orgA, Where: firstWhere})
		if _, err := approvals.CeremonyBinding(ctx, service.LocalPrincipal(carol), scope, multiID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("first-key holder cannot bind second pinned key: %v", err)
		}
		if _, err := approvals.Vote(ctx, service.LocalPrincipal(carol), scope, multiID, "approve"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("first-key holder cannot vote on second pinned key: %v", err)
		}
		for _, metadata := range []string{"[]", "{}", "invalid-json", `[""]`} {
			execRaw(t, db, "UPDATE approval_requests SET key_ids = '"+metadata+"' WHERE id = '"+multiID+"'")
			if _, err := approvals.CeremonyBinding(ctx, service.LocalPrincipal(carol), scope, multiID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("malformed pinned metadata %q refuses uniformly: %v", metadata, err)
			}
		}

		view, err := approvals.Vote(ctx, service.LocalPrincipal(hank), scope, request, "approve")
		if err != nil || view.Approvals != 1 {
			t.Fatalf("approve held request and count rule-held quorum: %+v %v", view, err)
		}
		if err := f.rules.Revoke(ctx, service.LocalPrincipal(orgAdmin), orgA, approverRule); err != nil {
			t.Fatal(err)
		}
		if _, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(frank), scope, service.PublishRequest{ApprovalRequestID: request}); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("revoked per-key approver no longer counts: %v", err)
		}
		f.create(t, orgAdmin, service.RuleSpec{Target: hank, Capability: domain.CapPublish, Org: orgA, Where: where})
		request = stageRequest(frank, "DB_PASSWORD", "approved-after-restored-authority")
		if _, err := approvals.Vote(ctx, service.LocalPrincipal(hank), scope, request, "approve"); err != nil {
			t.Fatalf("approve fresh request after invalidated quorum: %v", err)
		}
		merged, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(frank), scope, service.PublishRequest{ApprovalRequestID: request})
		if err != nil || len(merged.Published) != 1 {
			t.Fatalf("merge approved narrowed request: %+v %v", merged, err)
		}
		bypassRequest := stageRequest(frank, "DB_PASSWORD", "bypassed-narrowed")
		artifact := mintReauthedSession(t, db, frank, env.ID)
		bypassed, err := revisions.PublishPlanned(ctx, service.Bearer(artifact), scope, service.PublishRequest{ApprovalRequestID: bypassRequest, Bypass: &service.ApprovalBypass{Reason: "scoped incident recovery"}})
		if err != nil || len(bypassed.Published) != 1 {
			t.Fatalf("bypass narrowed request: %+v %v", bypassed, err)
		}
	})
}
