package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func nameAccount(t *testing.T, db *store.DB, accountID string, principal domain.PrincipalID, username, display string) {
	t.Helper()
	if err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return az.CreateAccount(ctx, authz.Account{ID: accountID, PrincipalID: principal, Username: username, DisplayName: display, CreatedAt: time.Now()})
	}); err != nil {
		t.Fatal(err)
	}
}

// A transit key's caller roster is caller-authored, so only callers who hold a
// grant in the key's own tenant are named: writing another tenant's principal
// id into a roster must not reveal that person's name.
func TestTransitKeyNamesOnlyTenantCallers(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc, _ := transitEnv(t, db, "_names")
		// Roster entries must be prefixed UUIDs, which the fixture principals are not.
		const tenant = domain.PrincipalID("usr_00000000-0000-7000-8000-0000000000a1")
		const outsider = domain.PrincipalID("usr_00000000-0000-7000-8000-0000000000b1")
		const instanceLevel = domain.PrincipalID("usr_00000000-0000-7000-8000-0000000000c1")
		const provisioned = domain.PrincipalID("usr_00000000-0000-7000-8000-0000000000d1")
		for _, id := range []domain.PrincipalID{tenant, outsider, instanceLevel, provisioned} {
			execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('`+string(id)+`', 'human', `+ts+`)`)
		}
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_tr_in', '`+string(tenant)+`', 'read', 'org_a', NULL, NULL, `+ts+`)`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_tr_out', '`+string(outsider)+`', 'read', 'org_b', NULL, NULL, `+ts+`)`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_tr_inst', '`+string(instanceLevel)+`', 'read', NULL, NULL, NULL, `+ts+`)`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_tr_scim', '`+string(provisioned)+`', 'read', 'org_a', NULL, NULL, `+ts+`)`)
		nameAccount(t, db, "acc_tr_inst", instanceLevel, "instance", "Ivy Instance")
		// SCIM stores userName as the display name, and an address is not a name.
		nameAccount(t, db, "acc_tr_scim", provisioned, "scim", "dana@example.test")
		nameAccount(t, db, "acc_tr_in", tenant, "tenant", "Alice Names")
		nameAccount(t, db, "acc_tr_out", outsider, "outsider", "Bob Elsewhere")
		key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "names-key", Algorithm: "xchacha20-poly1305"})
		entries := []service.TransitCallerEntry{
			{PrincipalID: string(tenant), Operations: []string{"encrypt"}},
			{PrincipalID: string(outsider), Operations: []string{"encrypt"}},
			{PrincipalID: string(instanceLevel), Operations: []string{"encrypt"}},
			{PrincipalID: string(provisioned), Operations: []string{"encrypt"}},
		}
		if _, err := svc.ConfigureKey(t.Context(), service.LocalPrincipal(alice), transitScope, key.Name, service.ConfigureTransitKeyRequest{Callers: &entries}); err != nil {
			t.Fatal(err)
		}
		got, err := svc.GetKey(t.Context(), service.LocalPrincipal(alice), transitScope, key.Name)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Callers) != 4 || len(got.PrincipalNames) != 1 || got.PrincipalNames[string(tenant)] != "Alice Names" {
			t.Fatalf("principal names = %+v, want only the tenant caller named", got.PrincipalNames)
		}
		// The listing names nobody, so its lookups do not grow with keys times callers.
		listed, err := svc.ListKeys(t.Context(), service.LocalPrincipal(alice), transitScope)
		if err != nil {
			t.Fatal(err)
		}
		for _, view := range listed {
			if len(view.PrincipalNames) != 0 {
				t.Fatalf("listed %s names %+v, want none", view.Name, view.PrincipalNames)
			}
		}
	})
}

// A value cell names its writer, which the cell already discloses.
func TestValueCellNamesUpdatedBy(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		kr := probeKeyring(t, db)
		keys := &service.Keys{DB: db, Keyring: kr}
		values := &service.Values{DB: db, Keyring: kr, Auth: authService(t, db)}
		nameAccount(t, db, "acc_val_custodian", custodian, "custodian", "Dana Custodian")
		project := domain.Scope{Org: orgA, Project: prjA1}
		if _, err := keys.Create(t.Context(), service.LocalPrincipal(custodian), project, service.KeySpec{
			Name: "NAMED_WRITER", Classification: string(schema.Config),
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
			Presence:    schema.DefaultPresenceRules(),
		}, nil); err != nil {
			t.Fatal(err)
		}
		if _, _, err := values.Declare(t.Context(), service.LocalPrincipal(custodian), project, []string{string(envA1)}, "NAMED_WRITER", "v"); err != nil {
			t.Fatal(err)
		}
		cells, err := values.List(t.Context(), service.LocalPrincipal(custodian), domain.Scope{Org: orgA, Project: prjA1, Env: envA1}, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cells {
			if c.Name == "NAMED_WRITER" {
				if c.UpdatedBy != string(custodian) || c.UpdatedByName != "Dana Custodian" {
					t.Fatalf("cell = %+v, want writer named", c)
				}
				return
			}
		}
		t.Fatal("NAMED_WRITER cell missing")
	})
}

// SCIM userName becomes the account's display name, so it obeys the same rule as
// a name typed by hand: its letters must appear as written.
func TestSCIMRefusesDirectionControlInUserName(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		binding, credential := newSCIMBinding(t, db, "direction-scim")
		desired := service.DesiredUser{Active: true, UserName: "ops\u202eadmin", ExternalID: "direction", SubjectRaw: "direction"}
		if _, err := scimSvc(db).CreateUser(t.Context(), service.SCIMCredentialActor(credential, binding), orgA, binding, desired); !errors.Is(err, service.ErrSCIMUserNameDirection) {
			t.Fatalf("create = %v, want ErrSCIMUserNameDirection", err)
		}
	})
}

func TestInviteRefusesDirectionControlInDisplayName(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		spec := service.InviteSpec{Username: "direction-invite", DisplayName: "ops‮admin", Delivery: "response"}
		if _, err := (&service.Grants{DB: db}).InviteMember(t.Context(), service.LocalPrincipal(root), spec); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invite = %v, want ErrInvalid", err)
		}
	})
}
