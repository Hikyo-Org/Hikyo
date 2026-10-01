package isolation

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/scimproto"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestSCIMCaseVariantAttributesPersistOnce(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc := scimSvc(db)
		binding, token := newSCIMBinding(t, db, "casepatch")
		actor := service.SCIMCredentialActor(token, binding)
		user, err := svc.CreateUser(t.Context(), actor, orgA, binding, service.DesiredUser{
			UserName: "case-user", ExternalID: "case-subject", Active: true,
			Attributes: map[string]any{"title": "Engineer", "displayName": "Before"},
		})
		if err != nil {
			t.Fatal(err)
		}
		patched, err := svc.PatchUser(t.Context(), actor, orgA, binding, user.ID, []service.UserPatchCommand{
			service.UserPatchMergeAttributes{Attributes: map[string]any{"TITLE": "Lead", "DISPLAYNAME": nil}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(patched.Attributes) != 1 || patched.Attributes["title"] != "Lead" {
			t.Fatalf("patch attributes = %#v", patched.Attributes)
		}
		loaded, err := svc.GetUser(t.Context(), actor, orgA, binding, user.ID)
		if err != nil || len(loaded.Attributes) != 1 || loaded.Attributes["title"] != "Lead" {
			t.Fatalf("persisted attributes = %#v, err %v", loaded.Attributes, err)
		}
	})
}

func TestSCIMCaseVariantImmutableExtensionLeafPersistsOnce(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc := scimSvc(db)
		seedSCIMProvider(t, db, "leafcase", "https://leafcase.example.test", true)
		binding, err := svc.CreateBinding(t.Context(), service.LocalPrincipal(orgAdmin), orgA, service.SCIMBindingInput{
			ProviderKind: domain.ProviderOIDC, ProviderSlug: "leafcase",
			SubjectSource: scimproto.SchemaEnterpriseExt + ":employeeNumber",
		})
		if err != nil {
			t.Fatal(err)
		}
		mint, err := svc.MintCredential(t.Context(), service.LocalPrincipal(orgAdmin), orgA, binding.ID, false, "")
		if err != nil {
			t.Fatal(err)
		}
		actor := service.SCIMCredentialActor(mint.Token, binding.ID)
		user, err := svc.CreateUser(t.Context(), actor, orgA, binding.ID, service.DesiredUser{
			UserName: "leaf-user", Active: true,
			Attributes: map[string]any{scimproto.SchemaEnterpriseExt: map[string]any{"employeeNumber": "immutable-subject"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.PatchUser(t.Context(), actor, orgA, binding.ID, user.ID, []service.UserPatchCommand{
			service.UserPatchMergeAttributes{Attributes: map[string]any{strings.ToUpper(scimproto.SchemaEnterpriseExt): map[string]any{"EMPLOYEENUMBER": "immutable-subject", "department": "Ops"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := svc.GetUser(t.Context(), actor, orgA, binding.ID, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		nested, ok := loaded.Attributes[scimproto.SchemaEnterpriseExt].(map[string]any)
		subjectLeaves := 0
		for name, value := range nested {
			if strings.EqualFold(name, "employeeNumber") && value == "immutable-subject" {
				subjectLeaves++
			}
		}
		if !ok || len(loaded.Attributes) != 1 || len(nested) != 2 || subjectLeaves != 1 || nested["department"] != "Ops" {
			t.Fatalf("persisted extension has stale or duplicate leaves: %#v", loaded.Attributes)
		}
	})
}

func TestSCIMGroupMembershipCeilingIsAtomicAndLegacyReadsFailClosed(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc := scimSvc(db)
		binding, token := newSCIMBinding(t, db, "memberbound")
		actor := service.SCIMCredentialActor(token, binding)
		// Seed distinct real directory identities without running 1001 unrelated
		// provisioning ceremonies. All database constraints remain enabled.
		seedSCIMPageDirectory(t, db, binding, "memberbound", store.MaxSCIMGroupMembers+1)
		members := make([]string, store.MaxSCIMGroupMembers)
		for i := range members {
			members[i] = fmt.Sprintf("memberbound%06d", i)
		}
		group, err := svc.CreateGroup(t.Context(), actor, orgA, binding, service.DesiredGroup{DisplayName: "Bounded", Members: members[:1]})
		if err != nil {
			t.Fatal(err)
		}
		extra := fmt.Sprintf("memberbound%06d", store.MaxSCIMGroupMembers)
		// Duplicate additions do not consume distinct-member capacity.
		if _, err := svc.PatchGroup(t.Context(), actor, orgA, binding, group.ID, []service.GroupPatchCommand{service.GroupPatchAddMembers{Members: []string{members[0]}}}); err != nil {
			t.Fatal(err)
		}
		var tuples []string
		for i, member := range members[1:] {
			tuples = append(tuples, fmt.Sprintf("('bounded-member-%d','%s','%s','%s','%s',%s)", i, orgA, binding, group.ID, member, ts))
		}
		execRaw(t, db, "INSERT INTO scim_group_members(id,org_id,binding_id,group_id,user_id,created_at) VALUES "+strings.Join(tuples, ","))
		for _, mutate := range []func() error{
			func() error {
				_, err := svc.PatchGroup(t.Context(), actor, orgA, binding, group.ID, []service.GroupPatchCommand{service.GroupPatchAddMembers{Members: []string{extra}}})
				return err
			},
			func() error {
				_, err := svc.ReplaceGroup(t.Context(), actor, orgA, binding, group.ID, service.DesiredGroup{DisplayName: "Rejected rename", Members: append(append([]string{}, members...), extra)})
				return err
			},
			func() error {
				_, err := svc.CreateGroup(t.Context(), actor, orgA, binding, service.DesiredGroup{DisplayName: "Rejected create", Members: append(append([]string{}, members...), extra)})
				return err
			},
		} {
			if err := mutate(); !errors.Is(err, store.ErrSCIMGroupMemberLimit) || !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("oversized mutation = %v", err)
			}
		}
		loaded, err := svc.GetGroup(t.Context(), actor, orgA, binding, group.ID)
		if err != nil || loaded.DisplayName != "Bounded" || len(loaded.Members) != store.MaxSCIMGroupMembers {
			t.Fatalf("failed mutation committed effects: %#v, %v", loaded, err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM scim_groups WHERE binding_id = '"+binding+"' AND display_name LIKE 'Rejected%'"); n != 0 {
			t.Fatalf("failed create persisted %d groups", n)
		}
		// Simulate an archive made before the ceiling existed. Read the real
		// bounded repository query, not a post-buffer slice of all members.
		execRaw(t, db, "INSERT INTO scim_group_members(id, org_id, binding_id, group_id, user_id, created_at) VALUES ('legacy-extra', '"+string(orgA)+"', '"+binding+"', '"+group.ID+"', '"+extra+"', "+ts+")")
		if got, err := svc.GetGroup(t.Context(), actor, orgA, binding, group.ID); !errors.Is(err, store.ErrSCIMGroupMemberLimit) || len(got.Members) != 0 {
			t.Fatalf("legacy oversized read was silently truncated: %d members, %v", len(got.Members), err)
		}
		if got, _, err := svc.ListGroups(t.Context(), actor, orgA, binding, scimproto.Filter{Shape: scimproto.FilterDisplayNameEq, Value: "Bounded"}, scimproto.Page{StartIndex: 1, Count: 1}); !errors.Is(err, store.ErrSCIMGroupMemberLimit) || len(got) != 0 {
			t.Fatalf("legacy oversized list was silently truncated: %d groups, %v", len(got), err)
		}
		if !strings.Contains(store.ErrSCIMGroupMemberLimit.Error(), "1000") {
			t.Fatal("membership refusal does not name the ceiling")
		}
		// The IdP can retire an excess User in a bounded transaction. Its
		// membership references and origins are reconciled together, unlike
		// direct database edits. The account survives for later reprovisioning.
		if err := svc.DeleteUser(t.Context(), actor, orgA, binding, extra); err != nil {
			t.Fatalf("cannot retire excess legacy user: %v", err)
		}
		if got, err := svc.GetGroup(t.Context(), actor, orgA, binding, group.ID); err != nil || len(got.Members) != store.MaxSCIMGroupMembers {
			t.Fatalf("bounded remediation did not restore group reads: %d members, %v", len(got.Members), err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM accounts WHERE id = '"+extra+"'"); n != 1 {
			t.Fatal("temporary directory retirement removed the account")
		}
	})
}
