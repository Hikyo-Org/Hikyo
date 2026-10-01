package isolation

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

// Both scoped membership branches must reach the same global policy once.
// Certificate reads and writes still derive all tenant coordinates from proof.
func TestPKIGeneratedQueryScopeAndBindingMembership(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		clock := &pkiClock{now: time.Now().UTC().Truncate(time.Second)}
		svc := pkiSvc(t, db, clock)
		actor := service.LocalPrincipal(root)
		env := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
		if _, err := svc.CreateIssuer(ctx, actor, service.PKIIssuerRequest{Mode: "root", Name: "issuing", CommonName: "Generated query CA", TTL: 365 * 24 * time.Hour}); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"both", "only-here"} {
			if _, err := svc.CreateProfile(ctx, actor, name, pkiWebPolicy()); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.BindProfile(ctx, actor, name, string(orgA), string(prjA1), string(envA1)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := svc.BindProfile(ctx, actor, "both", string(orgA), string(prjA1), ""); err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_pki_sibling','org_a','prj_a1','pki-sibling','',`+ts+`,1)`)
		profiles, err := svc.BoundProfiles(ctx, actor, env)
		if err != nil || len(profiles) != 2 || profiles[0].Name != "both" || profiles[1].Name != "only-here" {
			t.Fatalf("same environment membership: %+v %v", profiles, err)
		}
		sibling := domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID("env_pki_sibling")}
		profiles, err = svc.BoundProfiles(ctx, actor, sibling)
		if err != nil || len(profiles) != 1 || profiles[0].Name != "both" {
			t.Fatalf("project-wide membership: %+v %v", profiles, err)
		}
		pkiGrant(t, db, "g_pki_sqlc_issue", alice, domain.CapIssueCertificate, envA1)
		human := service.LocalPrincipal(alice)
		for index, scope := range []domain.Scope{sibling, {Org: orgA, Project: prjA2, Env: envA2}, {Org: orgB, Project: prjB1, Env: envB1}} {
			for _, capability := range []domain.Capability{domain.CapRead, domain.CapIssueCertificate} {
				execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_pki_sqlc_%d_%s','usr_alice','%s','%s','%s','%s',%s)`, index, capability, capability, scope.Org, scope.Project, scope.Env, ts))
			}
		}
		csr, _ := pkiCSR(t)
		request := service.CertificateIssueRequest{Profile: "only-here", CSRPEM: csr, DNSNames: []string{"api.svc.example.com"}, CommonName: "api.svc.example.com"}
		if _, err := svc.IssueCertificate(ctx, human, sibling, request); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("exact-environment profile escaped: %v", err)
		}
		issued, err := svc.IssueCertificate(ctx, human, env, request)
		if err != nil {
			t.Fatal(err)
		}
		beforeAudit := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='pki.certificate_transition_outcome'")
		var revokeGenerated func(domain.Scope) (int64, error)
		if db.Engine() == store.EngineSQLite {
			q := sqlitegen.New(db.SQLiteWrite())
			revokeGenerated = func(scope domain.Scope) (int64, error) {
				return q.PKIRevokeCertificate(ctx, sqlitegen.PKIRevokeCertificateParams{At: sql.NullString{String: clock.now.Format(time.RFC3339Nano), Valid: true}, Reason: sql.NullString{String: "unspecified", Valid: true}, ID: issued.Certificate.ID, ChainOrg: string(scope.Org), ChainProject: string(scope.Project), ChainEnv: string(scope.Env)})
			}
		} else {
			q := pggen.New(db.PG())
			revokeGenerated = func(scope domain.Scope) (int64, error) {
				return q.PKIRevokeCertificate(ctx, pggen.PKIRevokeCertificateParams{At: pgtype.Timestamptz{Time: clock.now, Valid: true}, Reason: pgtype.Text{String: "unspecified", Valid: true}, ID: issued.Certificate.ID, ChainOrg: string(scope.Org), ChainProject: string(scope.Project), ChainEnv: string(scope.Env)})
			}
		}
		certificateState := func() string {
			return queryString(t, db, "SELECT state||'|'||CAST(row_version AS TEXT)||'|'||COALESCE(CAST(revoked_at AS TEXT),'')||'|'||CAST(updated_at AS TEXT) FROM pki_certificates WHERE id='"+issued.Certificate.ID+"'")
		}
		beforeCertificate := certificateState()
		for _, outside := range []domain.Scope{{Org: orgB, Project: env.Project, Env: env.Env}, {Org: env.Org, Project: prjA2, Env: env.Env}, sibling} {
			if rows, err := revokeGenerated(outside); err != nil || rows != 0 {
				t.Fatalf("direct scoped revoke escaped %+v: rows=%d %v", outside, rows, err)
			}
			if after := certificateState(); after != beforeCertificate {
				t.Fatalf("direct foreign revoke changed certificate: %s -> %s", beforeCertificate, after)
			}
		}
		for _, outside := range []domain.Scope{sibling, {Org: orgA, Project: prjA2, Env: envA2}, {Org: orgB, Project: prjB1, Env: envB1}} {
			rows, err := svc.ListCertificates(ctx, human, outside)
			if err != nil || len(rows) != 0 {
				t.Fatalf("certificate list escaped %+v: %+v %v", outside, rows, err)
			}
			if _, err := svc.ShowCertificate(ctx, human, outside, issued.Certificate.ID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("certificate read escaped %+v: %v", outside, err)
			}
			if _, err := svc.RevokeCertificate(ctx, human, outside, issued.Certificate.ID, "unspecified"); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("certificate revoke escaped %+v: %v", outside, err)
			}
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='pki.certificate_transition_outcome'"); got != beforeAudit {
			t.Fatalf("out-of-scope writes emitted transition outcomes: %d -> %d", beforeAudit, got)
		}
		row, err := svc.ShowCertificate(ctx, human, env, issued.Certificate.ID)
		if err != nil || row.State != "issued" {
			t.Fatalf("scoped positive read: %+v %v", row, err)
		}
		row, err = svc.RevokeCertificate(ctx, human, env, issued.Certificate.ID, "unspecified")
		if err != nil || row.State != "revoked" {
			t.Fatalf("scoped positive revoke: %+v %v", row, err)
		}
	})
}

func TestPKIGeneratedRuntimeMicrosecondDeadlineAndStateCAS(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := time.Now().UTC().Truncate(time.Second)
		svc := pkiSvc(t, db, &pkiClock{now: now})
		issuer, err := svc.CreateIssuer(t.Context(), service.LocalPrincipal(root), service.PKIIssuerRequest{Mode: "root", Name: "sweep", CommonName: "Sweep CA", TTL: 365 * 24 * time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		due := now.Add(time.Microsecond)
		stamp := func(v time.Time) string { return "'" + store.CanonTime(v).Format("2006-01-02T15:04:05.000000Z") + "'" }
		execRaw(t, db, `INSERT INTO pki_certificates (id,org_id,project_id,environment_id,profile_id,profile_name,issuer_id,serial,state,key_source,key_algorithm,key_fingerprint,sans,not_before,not_after,principal_id,principal_class,issuing_deadline,created_at,updated_at) VALUES ('cert_sqlc_deadline','org_a','prj_a1','env_a1','pkip_unused','unused','`+issuer.Issuer.ID+`','abcdef9001','issuing','csr','ecdsa-p256','sha256:deadline','{"dns":[],"ip":[],"uri":[]}',`+stamp(now)+`,`+stamp(now.Add(time.Hour))+`,'usr_alice','human',`+stamp(due)+`,`+stamp(now)+`,`+stamp(now)+`)`)
		runtime := store.NewPKIRuntime(db)
		for _, at := range []time.Time{now, due} {
			if moved, err := runtime.SweepStaleIssuing(t.Context(), at, 10); err != nil || moved != 0 {
				t.Fatalf("deadline not passed at %s: %d %v", at, moved, err)
			}
		}
		after := due.Add(time.Microsecond)
		if moved, err := runtime.SweepStaleIssuing(t.Context(), after, 10); err != nil || moved != 1 {
			t.Fatalf("overdue transition: %d %v", moved, err)
		}
		if moved, err := runtime.SweepStaleIssuing(t.Context(), after, 10); err != nil || moved != 0 {
			t.Fatalf("state CAS replay: %d %v", moved, err)
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='pki.certificate_transition_outcome' AND object_id='cert_sqlc_deadline' AND outcome='unknown'"); got != 1 {
			t.Fatalf("unknown transition audit count: %d", got)
		}
		if got := queryInt(t, db, "SELECT revocation_seq FROM pki_issuers WHERE id='"+issuer.Issuer.ID+"'"); got != 1 {
			t.Fatalf("issuer revocation sequence: %d", got)
		}
		gauges, err := runtime.Gauges(t.Context(), after)
		if err != nil || gauges.UnknownCertificates != 1 {
			t.Fatalf("unknown gauge: %+v %v", gauges, err)
		}
	})
}
