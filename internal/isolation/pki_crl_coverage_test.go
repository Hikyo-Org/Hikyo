package isolation

import (
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestPKIRetirementRetainsRevocationCoverage(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		clock := &pkiClock{now: time.Now().UTC().Truncate(time.Second)}
		svc := pkiSvc(t, db, clock)
		op := service.LocalPrincipal(root)
		if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "parent", CommonName: "Parent", TTL: 365 * 24 * time.Hour}); err != nil {
			t.Fatal(err)
		}
		child, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "intermediate", Name: "issuing", CommonName: "Child", ParentName: "parent", TTL: 48 * time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.RetireIssuer(ctx, op, "parent", 1); !errors.Is(err, service.ErrPKIIssuerLive) {
			t.Fatalf("parent retired with unexpired child: %v", err)
		}
		env := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
		human := service.LocalPrincipal(alice)
		pkiGrant(t, db, "g_crl_coverage", alice, domain.CapIssueCertificate, envA1)
		if _, err := svc.CreateProfile(ctx, op, "web", pkiWebPolicy()); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.BindProfile(ctx, op, "web", string(orgA), string(prjA1), string(envA1)); err != nil {
			t.Fatal(err)
		}
		csr, _ := pkiCSR(t)
		leaf, err := svc.IssueCertificate(ctx, human, env, service.CertificateIssueRequest{Profile: "web", CSRPEM: csr, DNSNames: []string{"api.svc.example.com"}, TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.RevokeCertificate(ctx, human, env, leaf.Certificate.ID, "key-compromise"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.RetireIssuer(ctx, op, "issuing", 1); !errors.Is(err, service.ErrPKIIssuerLive) {
			t.Fatalf("issuer retired with unexpired revoked leaf: %v", err)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_issuers WHERE id='"+child.Issuer.ID+"' AND encrypted_private_key IS NOT NULL"); got != 1 {
			t.Fatal("refused retirement destroyed key")
		}
		clock.Advance(2 * time.Hour)
		if _, err := svc.RetireIssuer(ctx, op, "issuing", 1); err != nil {
			t.Fatalf("retire after leaf expiry: %v", err)
		}
		if _, err := svc.RetireIssuer(ctx, op, "parent", 1); !errors.Is(err, service.ErrPKIIssuerLive) {
			t.Fatalf("parent retired with unexpired retired child: %v", err)
		}
		clock.Advance(48 * time.Hour)
		if _, err := svc.RetireIssuer(ctx, op, "parent", 1); err != nil {
			t.Fatalf("retire after child expiry: %v", err)
		}
	})
}

func TestPKIRevokedIntermediatePublishedInParentCRL(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, retiring := range []bool{false, true} {
			name := "active-parent"
			if retiring {
				name = "retiring-parent"
			}
			t.Run(name, func(t *testing.T) {
				ctx := t.Context()
				clock := &pkiClock{now: time.Now().UTC().Truncate(time.Second)}
				svc := pkiSvc(t, db, clock)
				op := service.LocalPrincipal(root)
				parent, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: name, CommonName: name, TTL: 365 * 24 * time.Hour})
				if err != nil {
					t.Fatal(err)
				}
				child, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "intermediate", Name: name + "-child", CommonName: "Child", ParentName: name, TTL: 48 * time.Hour})
				if err != nil {
					t.Fatal(err)
				}
				if retiring {
					if _, err := svc.RotateIssuer(ctx, op, name, service.PKIIssuerRequest{}); err != nil {
						t.Fatal(err)
					}
				}
				sweepPKI(t, svc)
				if _, err := svc.RevokeIssuer(ctx, op, name+"-child", 1); err != nil {
					t.Fatal(err)
				}
				if got := queryInt(t, db, "SELECT revocation_seq-crl_revocation_seq FROM pki_issuers WHERE id='"+parent.Issuer.ID+"'"); got != 1 {
					t.Fatalf("parent revocation not scheduled: delta=%d", got)
				}
				if _, err := svc.RetireIssuer(ctx, op, name, 1); !errors.Is(err, service.ErrPKIIssuerLive) {
					t.Fatalf("parent retired with revoked unexpired child: %v", err)
				}
				childCerts, _, err := pki.ParseCertificates([]byte(child.Issuer.CertificatePEM))
				if err != nil {
					t.Fatal(err)
				}
				parentCerts, _, err := pki.ParseCertificates([]byte(parent.Issuer.CertificatePEM))
				if err != nil {
					t.Fatal(err)
				}
				check := func() {
					t.Helper()
					der, err := svc.IssuerCRL(ctx, op, name, 1)
					if err != nil {
						t.Fatal(err)
					}
					crl, err := x509.ParseRevocationList(der)
					if err != nil {
						t.Fatal(err)
					}
					if err := crl.CheckSignatureFrom(parentCerts[0]); err != nil {
						t.Fatal(err)
					}
					if reason, ok := crlSerials(t, der)[pki.SerialHex(childCerts[0].SerialNumber)]; !ok || reason != 2 {
						t.Fatal("parent CRL lacks child CA compromise serial")
					}
				}
				sweepPKI(t, svc)
				check()
				if _, err := svc.PublishIssuerCRL(ctx, op, name, 1); err != nil {
					t.Fatal(err)
				}
				check()
			})
		}
	})
}
