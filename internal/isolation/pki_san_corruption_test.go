package isolation

import (
	"crypto/x509"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestPKICertificateSANRecoveryPreservesCustodyAndFailSafeRevocation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		clock := &pkiClock{now: time.Now().UTC().Truncate(time.Second)}
		svc := pkiSvc(t, db, clock)
		op := service.LocalPrincipal(root)
		human := service.LocalPrincipal(alice)
		scope := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
		pkiGrant(t, db, "g_san_issue", alice, domain.CapIssueCertificate, envA1)
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_san_manage','usr_alice','manage-identities','org_a','prj_a1',NULL,`+ts+`)`)
		if _, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "issuing", CommonName: "Root", TTL: 365 * 24 * time.Hour}); err != nil {
			t.Fatal(err)
		}
		policy := pkiWebPolicy()
		policy.URIPatterns = []string{"spiffe://example.com/*"}
		if _, err := svc.CreateProfile(ctx, op, "san", policy); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.BindProfile(ctx, op, "san", string(orgA), string(prjA1), string(envA1)); err != nil {
			t.Fatal(err)
		}
		// Corrupt the settled row inside the outcome transaction. Decoding must
		// fail before commit, leaving no live certificate without its key.
		if db.Engine() == store.EngineSQLite {
			execRaw(t, db, `CREATE TRIGGER corrupt_settled_sans AFTER UPDATE OF state ON pki_certificates WHEN NEW.state='issued' BEGIN UPDATE pki_certificates SET sans='{"ip":42}' WHERE id=NEW.id; END`)
		} else {
			execRaw(t, db, `CREATE FUNCTION corrupt_settled_sans() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='issued' THEN NEW.sans='{"ip":42}'; END IF; RETURN NEW; END; $$; CREATE TRIGGER corrupt_settled_sans BEFORE UPDATE ON pki_certificates FOR EACH ROW EXECUTE FUNCTION corrupt_settled_sans()`)
		}
		failed, err := svc.IssueCertificate(ctx, human, scope, service.CertificateIssueRequest{Profile: "san", GenerateKey: true, DNSNames: []string{"corrupt.svc.example.com"}})
		if err == nil || !strings.Contains(err.Error(), "SAN metadata") || len(failed.PrivateKeyPEM) != 0 {
			t.Fatalf("corrupt settlement result=%+v err=%v", failed, err)
		}
		if db.Engine() == store.EngineSQLite {
			execRaw(t, db, `DROP TRIGGER corrupt_settled_sans`)
		} else {
			execRaw(t, db, `DROP TRIGGER corrupt_settled_sans ON pki_certificates; DROP FUNCTION corrupt_settled_sans()`)
		}
		rows, err := svc.ListCertificates(ctx, human, scope)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].State != "issuing" {
			t.Fatalf("corrupt settlement committed: %+v", rows)
		}
		issued, err := svc.IssueCertificate(ctx, human, scope, service.CertificateIssueRequest{Profile: "san", GenerateKey: true, DNSNames: []string{"api.svc.example.com"}, IPAddresses: []string{"10.1.2.3"}, URIs: []string{"spiffe://example.com/api"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(issued.PrivateKeyPEM) == 0 {
			t.Fatal("successful generated issuance lost display-once key")
		}
		id := issued.Certificate.ID
		csr, _ := pkiCSR(t)
		healthy, err := svc.IssueCertificate(ctx, human, scope, service.CertificateIssueRequest{Profile: "san", CSRPEM: csr, DNSNames: []string{"healthy.svc.example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		assertRecovered := func(view service.CertificateView) {
			t.Helper()
			if !slices.Equal(view.DNSNames, issued.Certificate.DNSNames) || !slices.Equal(view.IPAddresses, issued.Certificate.IPAddresses) || !slices.Equal(view.URIs, issued.Certificate.URIs) {
				t.Fatalf("recovered SANs differ from signed leaf: %+v", view)
			}
		}
		clock.Advance(policy.DefaultTTL - policy.RenewWindow + time.Minute)
		for _, corrupt := range []string{`{"dns":["fabricated.example"],"ip":42}`, `{"dns":`} {
			execRaw(t, db, "UPDATE pki_certificates SET sans='"+corrupt+"' WHERE id='"+id+"'")
			view, err := svc.ShowCertificate(ctx, human, scope, id)
			if err != nil {
				t.Fatalf("show signed recovery: %v", err)
			}
			assertRecovered(view)
			rows, err := svc.ListCertificates(ctx, human, scope)
			if err != nil || len(rows) != 3 {
				t.Fatalf("list lost healthy rows: %+v %v", rows, err)
			}
			var foundHealthy, foundRecovered bool
			for _, row := range rows {
				if row.ID == healthy.Certificate.ID {
					foundHealthy = true
				}
				if row.ID == id {
					foundRecovered = true
					assertRecovered(row)
				}
			}
			if !foundHealthy || !foundRecovered {
				t.Fatal("list omitted healthy or recovered certificate")
			}
			if _, err := svc.RenewCertificate(ctx, human, scope, id); err == nil || !strings.Contains(err.Error(), "SAN metadata") {
				t.Fatalf("renew accepted corrupt SAN metadata: %v", err)
			}
		}
		// Recovery must not accept another signed certificate or public key.
		for _, replacement := range []struct{ field, value, want string }{
			{"serial", "1", "serial mismatch"},
			{"key_fingerprint", healthy.Certificate.KeyFingerprint, "public key mismatch"},
		} {
			execRaw(t, db, "UPDATE pki_certificates SET "+replacement.field+"='"+replacement.value+"' WHERE id='"+id+"'")
			if _, err := svc.ShowCertificate(ctx, human, scope, id); err == nil || !strings.Contains(err.Error(), replacement.want) {
				t.Fatalf("unbound recovery %s: %v", replacement.field, err)
			}
			original := issued.Certificate.Serial
			if replacement.field == "key_fingerprint" {
				original = issued.Certificate.KeyFingerprint
			}
			execRaw(t, db, "UPDATE pki_certificates SET "+replacement.field+"='"+original+"' WHERE id='"+id+"'")
		}
		otherIssuer, err := svc.CreateIssuer(ctx, op, service.PKIIssuerRequest{Mode: "root", Name: "other-issuer", CommonName: "Other Root", TTL: 365 * 24 * time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, "UPDATE pki_certificates SET issuer_id='"+otherIssuer.Issuer.ID+"' WHERE id='"+id+"'")
		if _, err := svc.ShowCertificate(ctx, human, scope, id); err == nil || !strings.Contains(err.Error(), "SAN recovery signature") {
			t.Fatalf("recovery trusted a leaf from another issuer: %v", err)
		}
		execRaw(t, db, "UPDATE pki_certificates SET issuer_id='"+issued.Certificate.IssuerID+"' WHERE id='"+id+"'")
		valid := `{"dns":["api.svc.example.com"],"ip":["10.1.2.3"],"uri":["spiffe://example.com/api"]}`
		execRaw(t, db, "UPDATE pki_certificates SET sans='"+valid+"' WHERE id='"+id+"'")
		successor, err := svc.RenewCertificate(ctx, human, scope, id)
		if err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, "UPDATE pki_certificates SET sans='{\"ip\":42}' WHERE id='"+successor.ID+"'")
		if _, err := svc.RenewCertificate(ctx, human, scope, id); err == nil || !strings.Contains(err.Error(), "SAN metadata") {
			t.Fatalf("already-renewed successor accepted corrupt metadata: %v", err)
		}
		// Audit failure still rolls back the safety action. Only response-view
		// corruption is allowed to survive a committed revocation.
		if db.Engine() == store.EngineSQLite {
			execRaw(t, db, `CREATE TRIGGER refuse_corrupt_revoke_audit BEFORE INSERT ON audit_tenant_events WHEN NEW.type='pki.certificate_transition_outcome' BEGIN SELECT RAISE(ABORT,'revoke audit unavailable'); END`)
		} else {
			execRaw(t, db, `CREATE FUNCTION refuse_corrupt_revoke_audit() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN IF NEW.type='pki.certificate_transition_outcome' THEN RAISE EXCEPTION 'revoke audit unavailable'; END IF; RETURN NEW; END; $$; CREATE TRIGGER refuse_corrupt_revoke_audit BEFORE INSERT ON audit_tenant_events FOR EACH ROW EXECUTE FUNCTION refuse_corrupt_revoke_audit()`)
		}
		if _, err := svc.RevokeCertificate(ctx, human, scope, successor.ID, "unspecified"); err == nil || !strings.Contains(err.Error(), "revoke audit unavailable") {
			t.Fatalf("audit failure not propagated: %v", err)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_certificates WHERE id='"+successor.ID+"' AND state='issued'"); got != 1 {
			t.Fatal("audit failure committed revocation")
		}
		if db.Engine() == store.EngineSQLite {
			execRaw(t, db, `DROP TRIGGER refuse_corrupt_revoke_audit`)
		} else {
			execRaw(t, db, `DROP TRIGGER refuse_corrupt_revoke_audit ON audit_tenant_events; DROP FUNCTION refuse_corrupt_revoke_audit()`)
		}
		revoked, err := svc.RevokeCertificate(ctx, human, scope, successor.ID, "unspecified")
		if err != nil || revoked.State != "revoked" {
			t.Fatalf("SAN-only corruption blocked revocation: %+v %v", revoked, err)
		}
		assertRecovered(revoked)
		if _, err := svc.RevokeCertificate(ctx, human, scope, successor.ID, "unspecified"); err != nil {
			t.Fatalf("idempotent corrupt revocation: %v", err)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='pki.certificate_transition_outcome' AND CAST(payload AS TEXT) LIKE '%revoke%'"); got != 1 {
			t.Fatalf("revocation audit count=%d", got)
		}
		// If DER is corrupt too, rendering fails explicitly, but the authorized
		// fail-safe action and its audit still commit. No empty SAN fabrication.
		badDER := "X'626164'"
		if db.Engine() == store.EnginePostgres {
			badDER = "decode('626164','hex')"
		}
		execRaw(t, db, "UPDATE pki_certificates SET sans='{\"ip\":42}',certificate_der="+badDER+" WHERE id='"+healthy.Certificate.ID+"'")
		if _, err := svc.ListCertificates(ctx, human, scope); err == nil {
			t.Fatal("untrustworthy list record was silently omitted or fabricated")
		}
		if _, err := svc.RevokeCertificate(ctx, human, scope, healthy.Certificate.ID, "unspecified"); err == nil || !strings.Contains(err.Error(), "certificate is revoked, but response metadata unavailable") {
			t.Fatalf("double corruption must report committed revoke/display failure: %v", err)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_certificates WHERE id='"+healthy.Certificate.ID+"' AND state='revoked'"); got != 1 {
			t.Fatal("display error rolled back revocation")
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='pki.certificate_transition_outcome' AND CAST(payload AS TEXT) LIKE '%revoke%'"); got != 2 {
			t.Fatalf("double-corruption audit count=%d", got)
		}
		if _, err := svc.RevokeCertificate(ctx, human, scope, healthy.Certificate.ID, "unspecified"); err == nil {
			t.Fatal("already-revoked double corruption fabricated display metadata")
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='pki.certificate_transition_outcome' AND CAST(payload AS TEXT) LIKE '%revoke%'"); got != 2 {
			t.Fatalf("idempotent revoke duplicated audit: %d", got)
		}
		if _, err := svc.PublishIssuerCRL(ctx, op, "issuing", 0); err != nil {
			t.Fatal(err)
		}
		der, err := svc.IssuerCRL(ctx, op, "issuing", 0)
		if err != nil {
			t.Fatal(err)
		}
		crl, err := x509.ParseRevocationList(der)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range crl.RevokedCertificateEntries {
			if pki.SerialHex(entry.SerialNumber) == healthy.Certificate.Serial {
				found = true
			}
		}
		if !found {
			t.Fatal("double-corruption revocation missing from CRL")
		}
	})
}
