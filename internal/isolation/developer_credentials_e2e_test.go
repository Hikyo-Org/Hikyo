package isolation

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func mintDeveloperWithPasskey(t *testing.T, db *store.DB, f *ceremonyEnv, ttl time.Duration) (service.MintedDeveloperCredential, string) {
	t.Helper()
	scope := envScope(envA1)
	del := deliverySvc(t, db)
	preview, err := del.FetchAs(t.Context(), service.LocalPrincipal(f.admin.boot.PrincipalID), scope, "", service.FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, key := range preview.Keys {
		ids = append(ids, key.KeyID)
	}
	intent, err := service.NewDeveloperCredentialReauthIntent(string(scope.Env), ids, ttl, true)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := f.admin.auth.ReauthPasskeyStart(t.Context(), f.admin.token, intent)
	if err != nil {
		t.Fatal(err)
	}
	response, err := f.device.Assert(opts)
	if err != nil {
		t.Fatal(err)
	}
	reauth, err := f.admin.auth.ReauthPasskeyFinish(t.Context(), f.admin.token, response)
	if err != nil {
		t.Fatal(err)
	}
	f.admin.token = reauth.SessionToken
	svc := &service.DeveloperCredentials{DB: db, Auth: f.admin.auth}
	minted, err := svc.Mint(t.Context(), service.Bearer(f.admin.token), scope, service.MintDeveloperCredentialRequest{Lifetime: ttl, ConsentCurrentAndFuture: true, KeyIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	return minted, f.admin.token
}
func TestDeveloperCredentialPasskeyMintAndClosedDelivery(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "developer-mint-delivery")
		minted, _ := mintDeveloperWithPasskey(t, db, &fixture, time.Hour)
		del := deliverySvc(t, db)
		result, err := del.Fetch(t.Context(), minted.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if result.CredentialID != minted.Credential.ID || !result.SnapshotExpiresAt.IsZero() {
			t.Fatal("developer delivery minted offline authority or wrong credential identity")
		}
		// The durable envelope distinguishes the developer actor from the human authority.
		events := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE actor_id='`+string(minted.Credential.PrincipalID)+`' AND actor_credential_id='`+minted.Credential.ID+`' AND authority_id='`+string(minted.Credential.AuthorityPrincipal)+`' AND type='identity.disclosure'`)
		if events == 0 {
			t.Fatal("developer disclosure lacks envelope credential and human authority")
		}
		found := false
		for _, key := range result.Keys {
			if key.SnapshotReceipt != nil {
				t.Fatal("developer plaintext carried offline receipt")
			}
			if key.Name == ceremonySecretA {
				if key.Value == nil || *key.Value != "plaintext-"+ceremonySecretA {
					t.Fatal("developer secret delivery missing")
				}
				found = true
			}
		}
		if !found {
			t.Fatal("expected secret not delivered")
		}
		execRaw(t, db, `INSERT INTO keys(id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES('key_developer_future','org_a','prj_a1','DEVELOPER_FUTURE','','secret','',FALSE,'','{"rule":{"type":"string"}}','none','none',`+ts+`)`)
		publishValue(t, fixture.values, service.LocalPrincipal(custodian), envScope(envA1), "DEVELOPER_FUTURE", "future-development-value")
		future, err := del.Fetch(t.Context(), minted.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		futureFound := false
		for _, key := range future.Keys {
			if key.Name == "DEVELOPER_FUTURE" && key.Value != nil && *key.Value == "future-development-value" {
				futureFound = true
			}
		}
		if !futureFound {
			t.Fatal("consented future key not delivered")
		}
		if _, err := del.Fetch(t.Context(), minted.Value, envScope(envProd), "", service.FetchOptions{}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cross environment=%v", err)
		}
		if _, err := fixture.values.Get(t.Context(), service.Bearer(minted.Value), envScope(envA1), ceremonySecretA, false); err == nil {
			t.Fatal("developer admitted to metadata export")
		}
		if _, err := (&service.DeveloperCredentials{DB: db, Auth: fixture.admin.auth}).Mint(t.Context(), service.Bearer(minted.Value), envScope(envA1), service.MintDeveloperCredentialRequest{Lifetime: time.Hour, ConsentCurrentAndFuture: true}); err == nil {
			t.Fatal("developer bearer minted another bearer")
		}
		if _, err := del.ReconcileOfflineRecords(t.Context(), minted.Value, envScope(envA1), nil); err == nil {
			t.Fatal("developer accepted offline reconciliation")
		}
	})
}
func TestDeveloperCredentialCurrentAuthorityAndExpiry(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "developer-current-authority")
		minted, _ := mintDeveloperWithPasskey(t, db, &fixture, time.Hour)
		del := deliverySvc(t, db)
		del.Now = func() time.Time { return minted.Credential.ExpiresAt }
		if _, err := del.Fetch(t.Context(), minted.Value, envScope(envA1), "", service.FetchOptions{}); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("at exactexpiry=%v", err)
		}
		del.Now = nil
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id='g_cer_developer-current-authority_reveal'`)
		execRaw(t, db, `DELETE FROM grants WHERE id='g_cer_developer-current-authority_reveal'`)
		if _, err := del.Fetch(t.Context(), minted.Value, envScope(envA1), "", service.FetchOptions{}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("lost current reveal=%v", err)
		}
	})
}
