package isolation

import (
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestPKICertificateViewsRefuseCorruptSANMetadata(t *testing.T) {
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
		issued, err := svc.IssueCertificate(ctx, human, scope, service.CertificateIssueRequest{Profile: "san", GenerateKey: true, DNSNames: []string{"api.svc.example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(issued.PrivateKeyPEM) == 0 {
			t.Fatal("successful generated issuance lost display-once key")
		}
		id := issued.Certificate.ID
		clock.Advance(policy.DefaultTTL - policy.RenewWindow + time.Minute)
		for _, corrupt := range []string{`{"dns":["api.svc.example.com"],"ip":42}`, `{"dns":`} {
			execRaw(t, db, "UPDATE pki_certificates SET sans='"+corrupt+"' WHERE id='"+id+"'")
			if _, err := svc.ShowCertificate(ctx, human, scope, id); err == nil || !strings.Contains(err.Error(), "SAN metadata") {
				t.Fatalf("show accepted corrupt SAN metadata: %v", err)
			}
			if _, err := svc.ListCertificates(ctx, human, scope); err == nil || !strings.Contains(err.Error(), "SAN metadata") {
				t.Fatalf("list accepted corrupt SAN metadata: %v", err)
			}
			if _, err := svc.RenewCertificate(ctx, human, scope, id); err == nil || !strings.Contains(err.Error(), "SAN metadata") {
				t.Fatalf("renew accepted corrupt SAN metadata: %v", err)
			}
			if _, err := svc.RevokeCertificate(ctx, human, scope, id, "unspecified"); err == nil || !strings.Contains(err.Error(), "SAN metadata") {
				t.Fatalf("revoke accepted corrupt SAN metadata: %v", err)
			}
		}
		valid := `{"dns":["api.svc.example.com"],"ip":[],"uri":[]}`
		execRaw(t, db, "UPDATE pki_certificates SET sans='"+valid+"' WHERE id='"+id+"'")
		restored, err := svc.ShowCertificate(ctx, human, scope, id)
		if err != nil {
			t.Fatal(err)
		}
		if restored.State != "issued" {
			t.Fatalf("failed corrupt revoke committed state %q", restored.State)
		}
		successor, err := svc.RenewCertificate(ctx, human, scope, id)
		if err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, "UPDATE pki_certificates SET sans='{\"ip\":42}' WHERE id='"+successor.ID+"'")
		if _, err := svc.RenewCertificate(ctx, human, scope, id); err == nil || !strings.Contains(err.Error(), "SAN metadata") {
			t.Fatalf("already-renewed successor accepted corrupt metadata: %v", err)
		}
	})
}
