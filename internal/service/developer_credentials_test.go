package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// The approved service lifecycle seam refuses bearer creation before scope-bound ceremony.
func TestDeveloperCredentialMintRequiresFreshCeremony(t *testing.T) {
	db := adapterServiceDB(t)
	bearer := adapterCLISession(t, db)
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO grants(id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES('dev_read','usr_adapter','read','org_adapter','prj_adapter','env_one','2026-08-17T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	s := &DeveloperCredentials{DB: db, Auth: &Auth{DB: db}}
	_, err := s.Mint(t.Context(), Bearer(bearer), domain.Scope{Org: "org_adapter", Project: "prj_adapter", Env: "env_one"}, MintDeveloperCredentialRequest{Lifetime: time.Hour, ConsentCurrentAndFuture: true})
	if !errors.Is(err, ErrNoReauthWindow) {
		t.Fatalf("mint without ceremony=%v", err)
	}
}

func developerLifecycleFixture(t *testing.T) (*DeveloperCredentials, Actor, string, domain.Scope) {
	t.Helper()
	db := adapterServiceDB(t)
	bearer := adapterCLISession(t, db)
	scope := domain.Scope{Org: "org_adapter", Project: "prj_adapter", Env: "env_one"}
	for _, sql := range []string{
		`INSERT INTO accounts(id,principal_id,username,display_name,created_at) VALUES('acc_dev','usr_adapter','developer','Developer','2026-08-17T00:00:00Z')`,
		`INSERT INTO grants(id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES('dev_read','usr_adapter','read','org_adapter','prj_adapter','env_one','2026-08-17T00:00:00Z')`,
		`INSERT INTO grants(id,principal_id,capability,created_at) VALUES('dev_instance','usr_adapter','instance-config','2026-08-17T00:00:00Z')`,
		`INSERT INTO snapshots(id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at) VALUES('snp_dev','org_adapter','prj_adapter','env_one',1,1,'usr_adapter','2026-08-17T00:00:00Z')`,
	} {
		if _, err := db.SQLiteWrite().ExecContext(t.Context(), sql); err != nil {
			t.Fatal(err)
		}
	}
	now := store.CanonTime(time.Now().UTC())
	s := &DeveloperCredentials{DB: db, Auth: &Auth{DB: db, ReauthWindow: 5 * time.Minute}, Now: func() time.Time { return now }}
	return s, Bearer(bearer), bearer, scope
}
func openDeveloperMintWindow(t *testing.T, s *DeveloperCredentials, scope domain.Scope, ttl time.Duration) {
	t.Helper()
	intent, err := NewDeveloperCredentialReauthIntent(string(scope.Env), nil, ttl, true)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := intent.bindingFor("")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		id, err := newID("raw")
		if err != nil {
			return err
		}
		now := s.now()
		return az.OpenReauthWindow(ctx, authz.NewReauthWindow{ID: id, SessionID: "ses_adapter", EnvironmentID: string(scope.Env), FactorClass: "totp", SingleDecision: true, AuthenticatedAt: now, WindowExpiresAt: now.Add(time.Minute), HardExpiresAt: now.Add(time.Minute), CredentialEpoch: 1, CreatedAt: now, BoundPurpose: string(binding.purpose), BoundOperation: string(binding.operation), BoundKeySet: binding.keySet})
	}); err != nil {
		t.Fatal(err)
	}
}
func TestDeveloperCredentialMintCapAndRevocation(t *testing.T) {
	s, actor, _, scope := developerLifecycleFixture(t)
	request := MintDeveloperCredentialRequest{Lifetime: time.Hour, ConsentCurrentAndFuture: true}
	var first MintedDeveloperCredential
	for i := 0; i < 4; i++ {
		openDeveloperMintWindow(t, s, scope, time.Hour)
		c, err := s.Mint(t.Context(), actor, scope, request)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = c
		}
		if c.Credential.Scope != scope || c.Credential.AuthorityPrincipal != "usr_adapter" || c.Credential.PrincipalID == "usr_adapter" {
			t.Fatal("delegation authority not fixed")
		}
	}
	openDeveloperMintWindow(t, s, scope, time.Hour)
	if _, err := s.Mint(t.Context(), actor, scope, request); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("fifth mint=%v", err)
	}
	if err := s.Revoke(t.Context(), actor, domain.Scope{}, first.Credential.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(t.Context(), actor, domain.Scope{}, first.Credential.ID, false); err != nil {
		t.Fatalf("idempotent revoke=%v", err)
	}
	openDeveloperMintWindow(t, s, scope, time.Hour)
	if _, err := s.Mint(t.Context(), actor, scope, request); err != nil {
		t.Fatal(err)
	}
}
func TestDeveloperCredentialLowerCeilingDoesNotReviveOnRaise(t *testing.T) {
	s, actor, _, scope := developerLifecycleFixture(t)
	openDeveloperMintWindow(t, s, scope, 8*time.Hour)
	minted, err := s.Mint(t.Context(), actor, scope, MintDeveloperCredentialRequest{Lifetime: 8 * time.Hour, ConsentCurrentAndFuture: true})
	if err != nil {
		t.Fatal(err)
	}
	original := s.now()
	s.Now = func() time.Time { return original.Add(2 * time.Hour) }
	if _, err := s.SetPolicy(t.Context(), LocalPrincipal("usr_adapter"), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPolicy(t.Context(), LocalPrincipal("usr_adapter"), 8*time.Hour); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return original }
	rows, err := s.List(t.Context(), actor, domain.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].ExpiresAt.Equal(original.Add(time.Hour)) {
		t.Fatalf("durable expiry=%v", rows)
	}
	if err := tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		_, err := az.AuthenticateCaller(ctx, minted.Value, original.Add(2*time.Hour))
		return err
	}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("expired credential after ceiling raise=%v", err)
	}
}

func TestDeveloperMintBusinessRefusalsRemainAuditedAfterRollback(t *testing.T) {
	for _, cause := range []string{"protected-environment", "lifetime-ceiling", "not-materialized", "live-credential-cap"} {
		t.Run(cause, func(t *testing.T) {
			s, actor, _, scope := developerLifecycleFixture(t)
			request := MintDeveloperCredentialRequest{Lifetime: time.Hour, ConsentCurrentAndFuture: true}
			var setupSQL string
			switch cause {
			case "protected-environment":
				setupSQL = `UPDATE environments SET protected=TRUE WHERE id='env_one'`
			case "lifetime-ceiling":
				setupSQL = `UPDATE developer_credential_policy SET max_lifetime_seconds=60 WHERE id=1`
			case "not-materialized":
				setupSQL = `DELETE FROM snapshots WHERE id='snp_dev'`
			case "live-credential-cap":
				for range MaxLiveDeveloperCredentials {
					openDeveloperMintWindow(t, s, scope, time.Hour)
					if _, err := s.Mint(t.Context(), actor, scope, request); err != nil {
						t.Fatal(err)
					}
				}
			}
			if setupSQL != "" {
				if _, err := s.DB.SQLiteWrite().ExecContext(t.Context(), setupSQL); err != nil {
					t.Fatal(err)
				}
			}
			openDeveloperMintWindow(t, s, scope, time.Hour)
			if _, err := s.Mint(t.Context(), actor, scope, request); err == nil {
				t.Fatal("business refusal minted a bearer")
			}
			var count int
			err := s.DB.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_instance_events WHERE type='identity.developer_credential_mint_refused' AND outcome='failure' AND actor_id='usr_adapter' AND actor_credential_id='ses_adapter' AND authority_id='usr_adapter' AND json_extract(payload,'$.cause')=? AND json_extract(payload,'$.scope')=?`, cause, renderScope(scope)).Scan(&count)
			if err != nil || count != 1 {
				t.Fatalf("durable attributed refusal count=%d err=%v", count, err)
			}
		})
	}
}

func TestDeveloperReauthFullCatalogueBinding(t *testing.T) {
	keys := make([]string, 1000)
	for i := range keys {
		keys[i] = fmt.Sprintf("key_%04d", i)
	}
	intent, err := NewDeveloperCredentialReauthIntent("env_catalogue", keys, time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseDeveloperCredentialBinding(intent.developerBinding)
	if err != nil || len(parsed.KeyIDs()) != 1000 || parsed.keySet != intent.keySet {
		t.Fatalf("full catalogue binding did not round trip: %v", err)
	}
	changed := append([]string(nil), keys...)
	changed[999] = "key_changed"
	other, err := NewDeveloperCredentialReauthIntent("env_catalogue", changed, time.Hour, true)
	if err != nil || other.keySet == intent.keySet {
		t.Fatal("1000th key was omitted from exact ceremony binding")
	}
	if _, err := NewDeveloperCredentialReauthIntent("env_catalogue", append(keys, "key_overflow"), time.Hour, true); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("catalogue overflow admitted: %v", err)
	}
}
