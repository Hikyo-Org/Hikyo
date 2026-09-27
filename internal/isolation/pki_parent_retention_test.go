package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Parent deletion must not turn certificate history into a missing CRL entry.
// The policy deliberately retains expired history too: deletion is not pruning.
func TestPKIParentDeletionRetainsCertificateHistory(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		clock := &pkiClock{now: time.Now().UTC().Truncate(time.Second)}
		svc := pkiSvc(t, db, clock)
		op, human := service.LocalPrincipal(root), service.LocalPrincipal(alice)
		env := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
		if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "issuing", CommonName: "Retention CA", TTL: 365 * 24 * time.Hour}); err != nil {
			t.Fatal(err)
		}
		policy := pkiWebPolicy()
		policy.MachineIssuance = true
		if _, err := svc.CreateProfile(ctx, op, "retention", policy); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.BindProfile(ctx, op, "retention", string(orgA), string(prjA1), string(envA1)); err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_retention_manage','usr_alice','manage-identities','org_a','prj_a1',NULL,`+ts+`)`)
		ident := identitySvc(db)
		sa, err := ident.CreateServiceAccount(ctx, human, prjScope(), "retained", domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		credential, err := ident.MintCredential(ctx, human, prjScope(), sa.ID, service.MintRequest{})
		if err != nil {
			t.Fatal(err)
		}
		pkiGrant(t, db, "g_retention_issue", sa.Principal, domain.CapIssueCertificate, envA1)
		pkiGrant(t, db, "g_retention_read", sa.Principal, domain.CapRead, envA1)
		pkiGrant(t, db, "g_retention_human", alice, domain.CapIssueCertificate, envA1)
		csr, _ := pkiCSR(t)
		leaf, err := svc.IssueCertificate(ctx, service.Bearer(credential.Value), env, service.CertificateIssueRequest{Profile: "retention", CSRPEM: csr, DNSNames: []string{"api.svc.example.com"}, TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.RevokeCertificate(ctx, human, env, leaf.Certificate.ID, "key-compromise"); err != nil {
			t.Fatal(err)
		}
		envs := &service.Environments{DB: db, Keyring: svc.Keyring}
		for _, expired := range []bool{false, true} {
			if expired {
				clock.Advance(2 * time.Hour)
			}
			before := rowCounts(t, db)
			for name, remove := range map[string]func() error{
				"environment": func() error { return envs.Delete(ctx, human, env) },
				"shared store used by definitions apply": func() error {
					return tx.Write(ctx, db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
						proof, err := az.Authorize(ctx, authz.Identity{Principal: alice}, authz.OpEnvDelete, env)
						if err != nil {
							return err
						}
						return r.Environments().Delete(ctx, proof)
					})
				},
				"service account": func() error { return ident.DeleteServiceAccount(ctx, human, prjScope(), sa.ID) },
			} {
				err := remove()
				if !errors.Is(err, domain.ErrPKICertificateRetention) || !errors.Is(err, domain.ErrConflict) {
					t.Fatalf("%s deletion (expired=%v): %v", name, expired, err)
				}
				var detail interface{ SafeDetail() string }
				if !errors.As(err, &detail) || detail.SafeDetail() == "" {
					t.Fatalf("%s has no operator guidance: %v", name, err)
				}
				assertRowCountsEqual(t, db, before)
			}
			if got := authenticate(t, db, credential.Value); got.Principal != sa.Principal {
				t.Fatal("refused deletion changed credential")
			}
			if !expired {
				if _, err := svc.PublishIssuerCRL(ctx, op, "issuing", 1); err != nil {
					t.Fatal(err)
				}
				der, err := svc.IssuerCRL(ctx, op, "issuing", 1)
				if err != nil {
					t.Fatal(err)
				}
				if len(crlSerials(t, der)) != 1 {
					t.Fatal("refused deletion lost CRL coverage")
				}
			}
			if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_certificates WHERE id='"+leaf.Certificate.ID+"' AND state='revoked'"); got != 1 {
				t.Fatal("revocation evidence removed")
			}
		}
		// A workload can hold grants beyond its home project. Retention covers
		// those references while account lookup remains confined to its home.
		execRaw(t, db, "UPDATE pki_certificates SET project_id='prj_a2',environment_id='env_a2' WHERE id='"+leaf.Certificate.ID+"'")
		if err := ident.DeleteServiceAccount(ctx, human, prjScope(), sa.ID); !errors.Is(err, domain.ErrPKICertificateRetention) {
			t.Fatalf("cross-project history: %v", err)
		}
		if err := ident.DeleteServiceAccount(ctx, human, domain.Scope{Org: orgA, Project: prjA2}, sa.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("wrong home scope: %v", err)
		}
		// The supported deprovisioning operation remains usable while history exists.
		if err := ident.RevokeCredential(ctx, human, prjScope(), sa.ID, credential.Credential.ID); err != nil {
			t.Fatal(err)
		}
		empty, err := ident.CreateServiceAccount(ctx, human, prjScope(), "empty", domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		if err := ident.DeleteServiceAccount(ctx, human, prjScope(), empty.ID); err != nil {
			t.Fatalf("unreferenced account deletion: %v", err)
		}
	})
}

// Ordinary runtime storage never treats missing PKI tables as historical
// recovery authority. A damaged current schema must refuse before revocation.
func TestPKICurrentDeletionFailsClosedWithoutCertificateStorage(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_missing_pki_manage','usr_alice','manage-identities','org_a','prj_a1',NULL,`+ts+`)`)
		ident := identitySvc(db)
		human := service.LocalPrincipal(alice)
		sa, err := ident.CreateServiceAccount(ctx, human, prjScope(), "retain-on-damage", domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		credential, err := ident.MintCredential(ctx, human, prjScope(), sa.ID, service.MintRequest{})
		if err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, "DROP TABLE pki_certificates")
		if err := ident.DeleteServiceAccount(ctx, human, prjScope(), sa.ID); err == nil {
			t.Fatal("current deletion bypassed missing PKI storage")
		}
		if got := authenticate(t, db, credential.Value); got.Principal != sa.Principal {
			t.Fatal("failed current deletion revoked credential")
		}
	})
}
