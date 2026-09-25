package isolation

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestAccountProfileSelfService(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := bootstrapAdmin(t, db, adminOpts{username: "profile-admin", displayName: "Profile Admin", password: "profile password long enough", login: true})
		ctx := t.Context()
		before, err := admin.auth.MyProfile(ctx, admin.token)
		if err != nil {
			t.Fatal(err)
		}
		if before.Username != "profile-admin" || before.Email != nil || before.EmailVerified || before.Managed {
			t.Fatalf("initial profile: %+v", before)
		}
		// A legacy 00049 contact address is kept unverified by 00057: the read
		// shows it and reports it as not verified.
		execRaw(t, db, fmt.Sprintf("UPDATE accounts SET email = 'contact@example.test' WHERE id = '%s'", admin.accountID))
		legacy, err := admin.auth.MyProfile(ctx, admin.token)
		if err != nil || legacy.Email == nil || *legacy.Email != "contact@example.test" || legacy.EmailVerified {
			t.Fatalf("unverified profile %+v: %v", legacy, err)
		}
		// The verified login email is written only by verified local sign-up
		// (#608); stand in for that writer. No profile update can change it.
		execRaw(t, db, fmt.Sprintf("UPDATE accounts SET email = 'admin@example.test', email_verified_at = '2026-09-06T00:00:00.000000Z' WHERE id = '%s'", admin.accountID))
		email := "admin@example.test"
		next := service.ProfileUpdate{Username: "pretty-admin", DisplayName: "Pretty Admin"}
		want := service.AccountProfile{Username: next.Username, DisplayName: next.DisplayName, Email: &email, EmailVerified: true, UsernameEditable: true}
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
		same := func(got service.AccountProfile) bool {
			return got.Username == want.Username && got.DisplayName == want.DisplayName && got.Managed == want.Managed &&
				got.UsernameEditable == want.UsernameEditable && got.Email != nil && *got.Email == *want.Email && got.EmailVerified == want.EmailVerified
		}
		if !same(after) {
			t.Fatalf("saved profile %+v, want %+v", after, want)
		}
		read, err := admin.auth.MyProfile(ctx, admin.token)
		if err != nil || !same(read) {
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
		// A profile update leaves the email exactly as it was: it cannot clear it.
		if _, err := admin.auth.UpdateMyProfile(ctx, admin.token, next, admin.password); err != nil {
			t.Fatalf("metadata update: %v", err)
		}
		if got := queryInt(t, db, fmt.Sprintf("SELECT COUNT(*) FROM accounts WHERE id = '%s' AND email = 'admin@example.test' AND email_verified_at IS NOT NULL", admin.accountID)); got != 1 {
			t.Fatal("a profile update changed the verified email")
		}
		binding, _ := newSCIMBinding(t, db, "profile-scim")
		execRaw(t, db, fmt.Sprintf("INSERT INTO scim_users (id,org_id,binding_id,account_id,user_name,user_name_lower,subject,created_at,updated_at) VALUES ('scu_profile','%s','%s','%s','pretty-admin','pretty-admin','profile-subject','2026-09-06T00:00:00.000000Z','2026-09-06T00:00:00.000000Z')", orgA, binding, admin.accountID))
		managed, err := admin.auth.MyProfile(ctx, admin.token)
		if err != nil || !managed.Managed {
			t.Fatalf("managed profile %+v: %v", managed, err)
		}
		managedUpdate := service.ProfileUpdate{Username: managed.Username, DisplayName: "Local replacement"}
		if _, err := admin.auth.UpdateMyProfile(ctx, admin.token, managedUpdate, admin.password); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("managed name update: %v", err)
		}
		execRaw(t, db, fmt.Sprintf("DELETE FROM password_credentials WHERE account_id='%s'", admin.accountID))
		passwordless, err := admin.auth.MyProfile(ctx, admin.token)
		if err != nil || passwordless.UsernameEditable {
			t.Fatalf("passwordless profile %+v: %v", passwordless, err)
		}
		managedUpdate.DisplayName = next.DisplayName
		if _, err := admin.auth.UpdateMyProfile(ctx, admin.token, managedUpdate, ""); err != nil {
			t.Fatalf("managed no-op update: %v", err)
		}
	})
}
