package isolation

import (
	"errors"
	"fmt"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"testing"
)

func TestAccountProfileSelfService(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := bootstrapAdmin(t, db, adminOpts{username: "profile-admin", displayName: "Profile Admin", password: "profile password long enough", login: true})
		ctx := t.Context()
		before, err := admin.auth.MyProfile(ctx, admin.token)
		if err != nil {
			t.Fatal(err)
		}
		if before.Username != "profile-admin" || before.Email != "" || before.Managed {
			t.Fatalf("initial profile: %+v", before)
		}
		next := service.AccountProfile{Username: "pretty-admin", DisplayName: "Pretty Admin", Email: "admin@example.test", UsernameEditable: true}
		if _, err := admin.auth.UpdateMyProfile(ctx, "", next, admin.password); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("anonymous update: %v", err)
		}
		if _, err := admin.auth.UpdateMyProfile(ctx, admin.token, next, ""); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("missing proof: %v", err)
		}
		after, err := admin.auth.UpdateMyProfile(ctx, admin.token, next, admin.password)
		if err != nil {
			t.Fatal(err)
		}
		if after != next {
			t.Fatalf("saved profile %+v, want %+v", after, next)
		}
		read, err := admin.auth.MyProfile(ctx, admin.token)
		if err != nil || read != next {
			t.Fatalf("profile read %+v: %v", read, err)
		}
		who, err := admin.auth.Identity(ctx, admin.token)
		if err != nil || who.DisplayName != next.DisplayName || who.Principal != admin.boot.PrincipalID {
			t.Fatalf("whoami %+v: %v", who, err)
		}
		if _, err := admin.auth.LocalLogin(ctx, next.Username, admin.password, service.ArtifactCLI); err != nil {
			t.Fatalf("new username login: %v", err)
		}
		if _, err := admin.auth.LocalLogin(ctx, "profile-admin", admin.password, service.ArtifactCLI); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("old username login: %v", err)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.profile_updated'"); got != 1 {
			t.Fatalf("audit events=%d", got)
		}
		execRaw(t, db, fmt.Sprintf("INSERT INTO accounts (id, principal_id, username, display_name, created_at) VALUES ('acc_profile_other', '%s', 'taken-name', 'Other User', '2026-09-06T00:00:00.000000Z')", alice))
		taken := next
		taken.Username = "taken-name"
		if _, err := admin.auth.UpdateMyProfile(ctx, admin.token, taken, admin.password); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("duplicate username: %v", err)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.profile_updated'"); got != 1 {
			t.Fatalf("failed update wrote audit=%d", got)
		}
		next.Email = ""
		if _, err := admin.auth.UpdateMyProfile(ctx, admin.token, next, admin.password); err != nil {
			t.Fatalf("clear email: %v", err)
		}
		binding, _ := newSCIMBinding(t, db, "profile-scim")
		execRaw(t, db, fmt.Sprintf("INSERT INTO scim_users (id,org_id,binding_id,account_id,user_name,user_name_lower,subject,created_at,updated_at) VALUES ('scu_profile','%s','%s','%s','pretty-admin','pretty-admin','profile-subject','2026-09-06T00:00:00.000000Z','2026-09-06T00:00:00.000000Z')", orgA, binding, admin.accountID))
		managed, err := admin.auth.MyProfile(ctx, admin.token)
		if err != nil || !managed.Managed {
			t.Fatalf("managed profile %+v: %v", managed, err)
		}
		managed.DisplayName = "Local replacement"
		if _, err := admin.auth.UpdateMyProfile(ctx, admin.token, managed, admin.password); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("managed name update: %v", err)
		}
		execRaw(t, db, fmt.Sprintf("DELETE FROM password_credentials WHERE account_id='%s'", admin.accountID))
		passwordless, err := admin.auth.MyProfile(ctx, admin.token)
		if err != nil || passwordless.UsernameEditable {
			t.Fatalf("passwordless profile %+v: %v", passwordless, err)
		}
		managed.DisplayName = next.DisplayName
		managed.Email = "contact@example.test"
		if _, err := admin.auth.UpdateMyProfile(ctx, admin.token, managed, ""); err != nil {
			t.Fatalf("managed contact update: %v", err)
		}
	})
}
