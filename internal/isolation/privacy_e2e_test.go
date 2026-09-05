package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestPrivacySubjectLifecycle(t *testing.T) {
	for _, engine := range []struct {
		name string
		open func(*testing.T) *store.DB
	}{{"sqlite", openSQLite}, {"postgres", openPostgres}} {
		t.Run(engine.name, func(t *testing.T) {
			db := engine.open(t)
			auth := authService(t, db)
			ctx := tctx(t)
			boot, err := auth.BootstrapAdmin(ctx, "operator", "Operator", "file")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := auth.ApplyPrivacySubject(ctx, string(boot.PrincipalID), "restrict", ""); !errors.Is(err, service.ErrPrivacyLastManager) {
				t.Fatalf("last manager restriction: %v", err)
			}
			err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				for _, id := range []string{"usr_subject", "usr_other"} {
					if err := az.CreateHumanPrincipal(ctx, domain.PrincipalID(id), time.Now()); err != nil {
						return err
					}
					if err := az.CreateAccount(ctx, authz.Account{ID: "acc_" + id, PrincipalID: domain.PrincipalID(id), Username: id, DisplayName: "Private " + id, CreatedAt: time.Now()}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				return az.CreateGrant(ctx, "grant_privacy_subject", domain.PrincipalID("usr_subject"), domain.Grant{Capability: domain.CapInstanceConfig}, time.Now())
			})
			if err != nil {
				t.Fatal(err)
			}
			authorizeSubject := func() error {
				return tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
					_, err := az.Authorize(ctx, authz.Identity{Principal: "usr_subject"}, authz.OpOrgList, domain.Scope{})
					return err
				})
			}
			if err := authorizeSubject(); err != nil {
				t.Fatalf("active principal grants: %v", err)
			}
			reset, err := auth.BreakGlassResetCredential(ctx, "usr_subject", "file")
			if err != nil {
				t.Fatal(err)
			}
			const password = "correct horse battery staple"
			if err := auth.EstablishCredential(ctx, reset.Authority, password); err != nil {
				t.Fatal(err)
			}
			session, err := auth.LocalLogin(ctx, "usr_subject", password, service.ArtifactCLI)
			if err != nil {
				t.Fatal(err)
			}
			export, err := auth.ExportPrivacySubject(ctx, "usr_subject")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(export)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{session.SessionToken, reset.Authority, password, "usr_other", "verifier", "csrf"} {
				if strings.Contains(string(raw), forbidden) {
					t.Fatalf("export disclosed %q", forbidden)
				}
			}
			if len(export.Sessions) != 1 || len(export.Activity) == 0 {
				t.Fatalf("missing subject metadata: %+v", export)
			}
			receipt, err := auth.ApplyPrivacySubject(ctx, "usr_subject", "restrict", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := authorizeSubject(); !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("restricted principal grants authorized: %v", err)
			}
			if _, err := auth.Identity(ctx, session.SessionToken); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("restricted session: %v", err)
			}
			if _, err := auth.LocalLogin(ctx, "usr_subject", password, service.ArtifactCLI); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("restricted login: %v", err)
			}
			if _, err := auth.ApplyPrivacySubject(ctx, "usr_subject", "release", ""); err != nil {
				t.Fatal(err)
			}
			if _, err := auth.LocalLogin(ctx, "usr_subject", password, service.ArtifactCLI); err != nil {
				t.Fatalf("release login: %v", err)
			}
			if _, err := auth.CorrectPrivacySubject(ctx, "usr_subject", "usr_other", "Collision"); err == nil {
				t.Fatal("accepted duplicate username")
			}
			if _, err := auth.CorrectPrivacySubject(ctx, "usr_subject", "corrected-subject", "Corrected subject"); err != nil {
				t.Fatal(err)
			}
			if _, err := auth.LocalLogin(ctx, "corrected-subject", password, service.ArtifactCLI); err != nil {
				t.Fatalf("corrected login: %v", err)
			}
			wrong := receipt
			wrong.InstanceID = "another-instance"
			if _, err := auth.ReapplyPrivacyReceipt(ctx, wrong); err == nil {
				t.Fatal("accepted foreign instance receipt")
			}
			if _, err := auth.ReapplyPrivacyReceipt(ctx, receipt); err != nil {
				t.Fatalf("reapply: %v", err)
			}
			undo := authn.SetMutationFailureObserver(func(query string) error {
				if strings.Contains(query, "PrivacyErasePasswords") {
					return errors.New("injected privacy erasure failure")
				}
				return nil
			})
			_, failed := auth.ApplyPrivacySubject(ctx, "usr_subject", "erase", "")
			undo()
			if failed == nil {
				t.Fatal("injected erasure failure was swallowed")
			}
			snapshot, err := auth.ExportPrivacySubject(ctx, "usr_subject")
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Account.State != "restricted" || snapshot.Account.Username != "corrected-subject" {
				t.Fatal("failed erasure partially committed")
			}
			erased, err := auth.ApplyPrivacySubject(ctx, "usr_subject", "erase", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := auth.ReapplyPrivacyReceipt(ctx, erased); err != nil {
				t.Fatalf("repeat erase: %v", err)
			}
			if _, err := auth.ApplyPrivacySubject(ctx, "usr_subject", "release", ""); err == nil {
				t.Fatal("released erased account")
			}
			if _, err := auth.ApplyPrivacySubject(ctx, "usr_subject", "restrict", ""); err == nil {
				t.Fatal("downgraded erased receipt")
			}
			export, err = auth.ExportPrivacySubject(ctx, "usr_subject")
			if err != nil {
				t.Fatal(err)
			}
			if export.Account.DisplayName != "" || export.Account.Username == "usr_subject" || export.Account.State != "erased" || len(export.Sessions) != 0 || len(export.Identities) != 0 || len(export.Grants) != 0 {
				t.Fatalf("erasure incomplete: %+v", export)
			}
			for _, table := range []string{"password_credentials", "credential_authorities", "totp_credentials", "recovery_codes", "webauthn_credentials", "external_identities"} {
				if n := queryInt(t, db, "SELECT COUNT(*) FROM "+table+" WHERE account_id='acc_usr_subject'"); n != 0 {
					t.Fatalf("%s retained %d subject rows", table, n)
				}
			}
			if queryInt(t, db, "SELECT COUNT(*) FROM accounts WHERE username='usr_other'") != 1 {
				t.Fatal("erasure changed another subject")
			}
		})
	}
}

func runPrivacyAuditLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := tctx(t)
	auth := authService(t, db)
	err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		if err := az.CreateHumanPrincipal(ctx, "usr_privacy_audit", time.Now()); err != nil {
			return err
		}
		return az.CreateAccount(ctx, authz.Account{ID: "acc_privacy_audit", PrincipalID: "usr_privacy_audit", Username: "privacy-audit", DisplayName: "Privacy Audit", CreatedAt: time.Now()})
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ExportPrivacySubject(ctx, "usr_privacy_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CorrectPrivacySubject(ctx, "usr_privacy_audit", "privacy-corrected", "Corrected"); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"restrict", "release", "erase"} {
		if _, err := auth.ApplyPrivacySubject(ctx, "usr_privacy_audit", action, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPrivacyConcurrentLastManagers(t *testing.T) {
	for _, engine := range []struct {
		name string
		open func(*testing.T) *store.DB
	}{{"sqlite", openSQLite}, {"postgres", openPostgres}} {
		t.Run(engine.name, func(t *testing.T) {
			db := engine.open(t)
			auth := authService(t, db)
			ctx := tctx(t)
			err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				for _, id := range []string{"usr_manager_a", "usr_manager_b"} {
					if err := az.CreateHumanPrincipal(ctx, domain.PrincipalID(id), time.Now()); err != nil {
						return err
					}
					if err := az.CreateAccount(ctx, authz.Account{ID: "acc_" + id, PrincipalID: domain.PrincipalID(id), Username: id, DisplayName: id, CreatedAt: time.Now()}); err != nil {
						return err
					}
					if err := az.CreateGrant(ctx, "grant_"+id, domain.PrincipalID(id), domain.Grant{Capability: domain.CapManageMembers}, time.Now()); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for _, id := range []string{"usr_manager_a", "usr_manager_b"} {
				wg.Go(func() { <-start; _, err := auth.ApplyPrivacySubject(ctx, id, "restrict", ""); results <- err })
			}
			close(start)
			wg.Wait()
			close(results)
			successes := 0
			for err := range results {
				if err == nil {
					successes++
				}
			}
			if successes != 1 {
				t.Fatalf("concurrent restriction successes=%d, want1", successes)
			}
			if n := queryInt(t, db, "SELECT COUNT(*) FROM principals WHERE privacy_state='active'"); n != 1 {
				t.Fatalf("remaining active managers=%d", n)
			}
		})
	}
}

// A serialization retry must publish only the committed snapshot, including
// when an identity removed by another writer appeared in the first attempt.
func TestPrivacyExportDiscardsRetriedIdentitySnapshot(t *testing.T) {
	for _, engine := range []struct {
		name string
		open func(*testing.T) *store.DB
	}{{"sqlite", openSQLite}, {"postgres", openPostgres}} {
		t.Run(engine.name, func(t *testing.T) {
			db := engine.open(t)
			auth := authService(t, db)
			ctx := tctx(t)
			boot, err := auth.BootstrapAdmin(ctx, "privacy-retry", "Privacy Retry", "file")
			if err != nil {
				t.Fatal(err)
			}
			err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				epoch, err := az.CredentialEpoch(ctx)
				if err != nil {
					return err
				}
				return az.CreateExternalIdentity(ctx, authz.NewExternalIdentity{ID: "eid_retry", AccountID: boot.AccountID, Kind: "oidc", Issuer: "https://idp.example", Subject: "removed-before-commit", ProviderID: "provider_retry", CredentialEpoch: epoch, CreatedAt: time.Now()})
			})
			if err != nil {
				t.Fatal(err)
			}
			attempts := 0
			restore := authn.SetMutationFailureObserver(func(query string) error {
				if !strings.Contains(query, "InsertInstanceAuditEvent") {
					return nil
				}
				attempts++
				if attempts != 1 {
					return nil
				}
				// PostgreSQL allows another writer to remove the link while this export
				// still holds its old read snapshot. SQLite's single writer instead tests
				// that the unchanged identity appears once, not once per retry attempt.
				if db.Engine() == store.EnginePostgres {
					execRaw(t, db, "DELETE FROM external_identities WHERE id='eid_retry'")
				}
				return store.ErrRetrySerialization
			})
			defer restore()
			got, err := auth.ExportPrivacySubject(ctx, string(boot.PrincipalID))
			if err != nil {
				t.Fatal(err)
			}
			if attempts != 2 {
				t.Fatalf("attempts=%d, want2", attempts)
			}
			want := 1
			if db.Engine() == store.EnginePostgres {
				want = 0
			}
			if len(got.Identities) != want {
				t.Fatalf("export includes retried identity state: %+v", got.Identities)
			}
			if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='privacy.subject_exported'"); n != 1 {
				t.Fatalf("committed export audit count=%d", n)
			}
		})
	}
}
