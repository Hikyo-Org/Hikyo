package isolation

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestSCIMMembershipUpdatesResourceTimestampOnlyWhenChanged(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx, svc := t.Context(), scimSvc(db)
		binding, credential := newSCIMBinding(t, db, "okta")
		actor := service.SCIMCredentialActor(credential, binding)
		clock := time.Now().UTC().Truncate(time.Microsecond)
		svc.Now = func() time.Time { return clock }
		user, err := svc.CreateUser(ctx, actor, orgA, binding, service.DesiredUser{Active: true, UserName: "timestamp-user", ExternalID: "timestamp-subject", SubjectRaw: "timestamp-subject"})
		if err != nil {
			t.Fatal(err)
		}
		group, err := svc.CreateGroup(ctx, actor, orgA, binding, service.DesiredGroup{DisplayName: "Timestamp group"})
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(time.Minute)
		updated, err := svc.ReplaceGroup(ctx, actor, orgA, binding, group.ID, service.DesiredGroup{DisplayName: group.DisplayName, Members: []string{user.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if !updated.UpdatedAt.Equal(clock) || !updated.UpdatedAt.After(group.UpdatedAt) {
			t.Fatalf("membership change timestamp: %s", updated.UpdatedAt)
		}
		clock = clock.Add(time.Minute)
		unchanged, err := svc.ReplaceGroup(ctx, actor, orgA, binding, group.ID, service.DesiredGroup{DisplayName: group.DisplayName, Members: []string{user.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if !unchanged.UpdatedAt.Equal(updated.UpdatedAt) {
			t.Fatalf("unchanged membership moved timestamp: %s -> %s", updated.UpdatedAt, unchanged.UpdatedAt)
		}
		removed, err := svc.PatchGroup(ctx, actor, orgA, binding, group.ID, []service.GroupPatchCommand{service.GroupPatchClearMembers{}})
		if err != nil {
			t.Fatal(err)
		}
		if !removed.UpdatedAt.Equal(clock) {
			t.Fatalf("membership removal timestamp: %s", removed.UpdatedAt)
		}
	})
}
