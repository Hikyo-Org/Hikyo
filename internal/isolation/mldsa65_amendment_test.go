package isolation

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The api-cli-surface amendment of 2026-11-04 lets the /api/v1 contract name
// mldsa65 (Transit algorithm, PKI key algorithms). It enables nothing: the
// Transit mechanism is not in this tree and PKI stays behind
// service.PKI.MLDSAEnabled, which production wiring leaves false. Because the
// contract now lets the value past request validation, the service is the line
// that holds, so each surface must refuse it as a validation error (never a
// 500) and leave no row behind.
func TestMLDSA65IsAcceptedByTheContractButRefusedByTheService(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx, actor := tctx(t), service.LocalPrincipal(alice)
		rows := func(table string) int64 { return queryInt(t, db, `SELECT COUNT(*) FROM `+table) }

		// shortcut: the Transit mechanism lands with #1172; this case flips to a
		// positive lifecycle there.
		tsvc, _ := transitEnv(t, db, "mldsa")
		_, err := tsvc.CreateKey(ctx, actor, transitScope, service.CreateTransitKeyRequest{Name: "pq", Algorithm: "mldsa65", AllowedOperations: []string{"sign", "verify"}})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("transit create with mldsa65: %v, want a validation error", err)
		}
		if n := rows("transit_keys") + rows("transit_key_versions"); n != 0 {
			t.Errorf("a refused transit key left %d rows", n)
		}

		pkisvc := pkiSvc(t, db, &pkiClock{now: time.Now().UTC().Truncate(time.Second)}) // production wiring: MLDSAEnabled false
		op := service.LocalPrincipal(root)
		pkiGrant(t, db, "g_mldsa65_alice_issue", alice, domain.CapIssueCertificate, envA1)
		refusals := map[string]func() error{
			"issuer": func() error {
				_, err := pkisvc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "pq-root", CommonName: "x", KeyAlgorithm: "mldsa65"})
				return err
			},
			"profile": func() error {
				_, err := pkisvc.CreateProfile(ctx, op, "pq-profile", pki.Policy{
					AllowedIssuers: []string{"whatever"}, DNSPatterns: []string{"*.svc.example.com"},
					KeyAlgorithms: []pki.KeyAlgorithm{pki.MLDSA65}, KeyUsages: []pki.KeyUsage{pki.UsageDigitalSignature},
					ExtKeyUsages: []pki.ExtKeyUsage{pki.ExtUsageServerAuth}, MaxTTL: 72 * time.Hour, DefaultTTL: 24 * time.Hour, RenewWindow: 8 * time.Hour,
					AllowCSR: true, AllowGeneratedKey: true,
				})
				return err
			},
			"generated leaf key": func() error {
				_, err := pkisvc.IssueCertificate(ctx, service.LocalPrincipal(alice), transitScope, service.CertificateIssueRequest{Profile: "none", GenerateKey: true, KeyAlgorithm: "mldsa65", DNSNames: []string{"pq.svc.example.com"}})
				return err
			},
		}
		for name, run := range refusals {
			if err := run(); !errors.Is(err, service.ErrPKIAlgorithmHeld) || !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("pki %s with mldsa65: %v, want ErrPKIAlgorithmHeld wrapping a validation error", name, err)
			}
		}
		if n := rows("pki_issuers") + rows("pki_profiles") + rows("pki_certificates"); n != 0 {
			t.Errorf("a refused pki request left %d rows", n)
		}
	})
}
