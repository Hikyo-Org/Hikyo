package isolation

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestOrgRenameRequiresScopedManageMembers(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		orgs, _, _ := services(t, db)
		execRaw(t, db, "DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE principal_id = 'usr_root' AND capability = 'manage-members')")
		execRaw(t, db, "DELETE FROM grants WHERE principal_id = 'usr_root' AND capability = 'manage-members'")
		if _, err := orgs.Rename(t.Context(), service.LocalPrincipal(root), orgA, "refused"); err == nil {
			t.Fatal("bare instance-config principal renamed an organisation")
		}
		execRaw(t, db, "INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_rename_members', 'usr_alice', 'manage-members', 'org_a', "+ts+")")
		renamed, err := orgs.Rename(t.Context(), service.LocalPrincipal(alice), orgA, "Org admin renamed")
		if err != nil {
			t.Fatal(err)
		}
		if renamed.Name != "Org admin renamed" {
			t.Fatalf("rename did not persist: %+v", renamed)
		}
		if _, err := orgs.Rename(t.Context(), service.LocalPrincipal(alice), orgB, "cross-org"); err == nil {
			t.Fatal("org-scoped grant renamed another organisation")
		}
		if err := orgs.Delete(t.Context(), service.LocalPrincipal(alice), orgA); err == nil {
			t.Fatal("org member administrator deleted the organisation")
		}
	})
}
