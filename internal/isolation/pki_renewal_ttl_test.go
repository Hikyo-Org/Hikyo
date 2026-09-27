package isolation

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestPKIRenewalPreservesSecondPrecisionBounds(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		clock := &pkiClock{now: time.Now().UTC().Truncate(time.Second).Add(123 * time.Millisecond)}
		svc := pkiSvc(t, db, clock)
		ctx := t.Context()
		op := service.LocalPrincipal(root)
		if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "issuing", CommonName: "Root", TTL: 365 * 24 * time.Hour}); err != nil {
			t.Fatal(err)
		}
		scope := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
		human := service.LocalPrincipal(alice)
		pkiGrant(t, db, "g_renew_ttl", alice, domain.CapIssueCertificate, envA1)
		csr, _ := pkiCSR(t)
		for _, seconds := range []int{300, 330} {
			name := map[int]string{300: "minimum", 330: "fractional-minute"}[seconds]
			policy := pkiWebPolicy()
			policy.MaxTTL = time.Duration(seconds) * time.Second
			policy.DefaultTTL = policy.MaxTTL
			policy.RenewWindow = time.Minute
			if _, err := svc.CreateProfile(ctx, op, name, policy); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.BindProfile(ctx, op, name, string(orgA), string(prjA1), string(envA1)); err != nil {
				t.Fatal(err)
			}
			issued, err := svc.IssueCertificate(ctx, human, scope, service.CertificateIssueRequest{Profile: name, CSRPEM: csr, DNSNames: []string{"api.svc.example.com"}})
			if err != nil {
				t.Fatal(err)
			}
			clock.Advance(policy.MaxTTL - 30*time.Second)
			renewed, err := svc.RenewCertificate(ctx, human, scope, issued.Certificate.ID)
			if err != nil {
				t.Fatalf("renew %ds: %v", seconds, err)
			}
			lifetime := renewed.NotAfter.Sub(renewed.NotBefore) - time.Minute
			if lifetime < 300*time.Second || lifetime > policy.MaxTTL {
				t.Fatalf("renew %ds lifetime %s", seconds, lifetime)
			}
		}
	})
}
