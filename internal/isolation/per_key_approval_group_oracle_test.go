package isolation

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestPerKeyApprovalGroupDriftDoesNotDiscloseSiblingDraft(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		f := seedRulesFixture(t, db)
		kr := probeKeyring(t, db)
		auth := authService(t, db)
		auth.ReauthWindow = 15 * time.Minute
		approvals := &service.Approvals{DB: db, Auth: auth, Keyring: kr}
		revisions := &service.Revisions{DB: db, Auth: auth, Keyring: kr}
		project := scopeProject(orgA, prjA1)
		env, err := (&service.Environments{DB: db, Keyring: kr}).Create(ctx, service.LocalPrincipal(custodian), project, "approval-group-drift", nil)
		if err != nil {
			t.Fatal(err)
		}
		scope := scopeEnv(orgA, prjA1, domain.EnvID(env.ID))
		group, err := (&service.KeyGroups{DB: db}).Create(ctx, service.LocalPrincipal(custodian), project, "approval-linked", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.keys.SetGroup(ctx, service.LocalPrincipal(custodian), project, f.dbKeyID, group.ID); err != nil {
			t.Fatal(err)
		}
		mode, envs := onlyEnvs(domain.EnvID(env.ID))
		where := whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.dbKeyID})
		for _, capability := range []domain.Capability{domain.CapEdit, domain.CapPublish} {
			f.create(t, orgAdmin, service.RuleSpec{Target: frank, Capability: capability, Org: orgA, Where: where})
		}
		f.create(t, orgAdmin, service.RuleSpec{Target: hank, Capability: domain.CapPublish, Org: orgA, Where: where})
		if _, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), project, service.ApprovalPolicyInput{EnvironmentID: env.ID, MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true, Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(hank)}}}); err != nil {
			t.Fatal(err)
		}
		staged, err := f.values.Set(ctx, service.LocalPrincipal(frank), scope, "DB_PASSWORD", "reviewed", nil)
		if err != nil {
			t.Fatal(err)
		}
		result, err := revisions.PublishPlanned(ctx, service.LocalPrincipal(frank), scope, service.PublishRequest{VersionIDs: []string{staged.VersionID}})
		if err != nil || result.CreatedApprovalRequest == nil {
			t.Fatalf("create single-key request: %+v %v", result, err)
		}
		request := result.CreatedApprovalRequest.ID
		if binding, err := approvals.CeremonyBinding(ctx, service.LocalPrincipal(hank), scope, request); err != nil || len(binding.KeyIDs) != 1 {
			t.Fatalf("single-key admission control: %+v %v", binding, err)
		}
		// Membership changes after the reviewed key list is pinned. The caller still
		// holds Publish on A, but has no authority over the newly coupled sibling B.
		if _, err := f.keys.SetGroup(ctx, service.LocalPrincipal(custodian), project, f.strKeyID, group.ID); err != nil {
			t.Fatal(err)
		}
		_, absent := approvals.Vote(ctx, service.LocalPrincipal(hank), scope, request, "approve")
		if _, err := f.values.Set(ctx, service.LocalPrincipal(custodian), scope, "STRIPE_KEY", "another-owner", nil); err != nil {
			t.Fatal(err)
		}
		_, present := approvals.Vote(ctx, service.LocalPrincipal(hank), scope, request, "approve")
		assertUniformNotFound(t, absent, present)
		if strings.Contains(present.Error(), "STRIPE_KEY") || strings.Contains(present.Error(), "another principal") {
			t.Fatalf("inaccessible group detail leaked: %v", present)
		}
		// A holder of the complete group's Publish authority may learn its named
		// cross-owner conflict. This proves refusals above came from the selector.
		f.create(t, orgAdmin, service.RuleSpec{Target: hank, Capability: domain.CapPublish, Org: orgA, Where: whereIn(mode, envs, domain.AxisOnly, domain.RuleKeyItem{KeyID: f.strKeyID})})
		_, control := approvals.Vote(ctx, service.LocalPrincipal(hank), scope, request, "approve")
		if !errors.Is(control, domain.ErrInvalid) || !strings.Contains(control.Error(), "STRIPE_KEY") {
			t.Fatalf("authorized cross-owner conflict control: %v", control)
		}
	})
}
