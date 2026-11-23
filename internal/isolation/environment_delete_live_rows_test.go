package isolation

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// #1420: a minted lease or an issued certificate pins its environment through a
// restrict foreign key. A live one refuses the delete by name; a terminal one is
// purged with the environment, by Environments.Delete and by the definitions
// apply executor alike.

func seedLeaseRows(t *testing.T, db *store.DB, f definitionsFixture) {
	t.Helper()
	project := string(f.project)
	execRaw(t, db, `INSERT INTO dynamic_providers (id,org_id,project_id,kind,origin,tls_mode,grant_role,authority_principal_id,state,created_at) VALUES ('dpr_`+project+`','org_a','`+project+`','postgres','postgres://`+project+`.example/db','verify-full','reader','usr_alice','active',`+ts+`)`)
	lease := func(id, state string) {
		execRaw(t, db, `INSERT INTO dynamic_leases (id,org_id,project_id,environment_id,provider_id,principal_id,principal_class,provider_handle,state,max_ttl_seconds,last_transition_at,next_attempt_at,created_at) VALUES ('`+id+`','org_a','`+project+`','`+f.env+`','dpr_`+project+`','usr_alice','human','h_`+id+`','`+state+`',3600,`+ts+`,`+ts+`,`+ts+`)`)
		execRaw(t, db, `INSERT INTO dynamic_effects (id,org_id,project_id,environment_id,lease_id,kind,intent_audit_id,created_at) VALUES ('dfx_`+id+`','org_a','`+project+`','`+f.env+`','`+id+`','mint','aud_`+id+`',`+ts+`)`)
	}
	lease(project+"_live", "active")
	for _, state := range []string{"revoked", "expired", "failed"} {
		lease(project+"_"+state, state)
	}
}

func seedCertificateRows(t *testing.T, db *store.DB, f definitionsFixture, now time.Time) {
	t.Helper()
	project := string(f.project)
	issuer := "pki_iss_" + project
	execRaw(t, db, `INSERT INTO pki_issuers (id,name,version,kind,origin,state,key_algorithm,key_fingerprint,subject_cn,created_by,created_at,updated_at) VALUES ('`+issuer+`','`+issuer+`',1,'root','generated','revoked','ecdsa-p256','sha256:x','`+issuer+`','usr_alice',`+pkiStamp(db, now)+`,`+pkiStamp(db, now)+`)`)
	cert := func(id, state string, notAfter time.Time) {
		revoked := "NULL, NULL"
		if state == "revoked" {
			revoked = pkiStamp(db, now) + ", 'unspecified'"
		}
		der := "'\\x00'::bytea"
		if db.Engine() != store.EnginePostgres {
			der = "x'00'"
		}
		execRaw(t, db, `INSERT INTO pki_certificates (id,org_id,project_id,environment_id,profile_id,profile_name,issuer_id,serial,state,key_source,key_algorithm,key_fingerprint,sans,not_before,not_after,certificate_der,principal_id,principal_class,revoked_at,revocation_reason,issuing_deadline,created_at,updated_at) VALUES ('`+id+`','org_a','`+project+`','`+f.env+`','pkip_x','web','`+issuer+`','`+id+`','`+state+`','csr','ecdsa-p256','sha256:x','{"dns":[],"ip":[],"uri":[]}',`+pkiStamp(db, now.Add(-48*time.Hour))+`,`+pkiStamp(db, notAfter)+`,`+der+`,'usr_alice','human',`+revoked+`,`+pkiStamp(db, now)+`,`+pkiStamp(db, now)+`,`+pkiStamp(db, now)+`)`)
	}
	cert(project+"_issued", "issued", now.Add(24*time.Hour))
	cert(project+"_revoked", "revoked", now.Add(24*time.Hour))
	cert(project+"_expired", "expired", now.Add(-time.Hour))
	cert(project+"_old_revoked", "revoked", now.Add(-time.Hour))
}

func TestEnvironmentDeleteLiveLease(t *testing.T) { forEngines(t, runEnvironmentDeleteLiveLease) }

func runEnvironmentDeleteLiveLease(t *testing.T, db *store.DB) {
	f := seedDefinitionsProject(t, db, "lease", true)
	seedLeaseRows(t, db, f)
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
	live := string(f.project) + "_live"

	// Every non-terminal state refuses by name and keeps the environment.
	for _, state := range []string{"active", "minting", "renewing", "revoking", "unknown"} {
		execRaw(t, db, `UPDATE dynamic_leases SET state='`+state+`' WHERE id='`+live+`'`)
		err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope())
		if !errors.Is(err, service.ErrEnvironmentHasLiveLease) || !errors.Is(err, domain.ErrConflict) || !strings.Contains(safeError(err), live) {
			t.Fatalf("delete with a %s lease = %v (%q)", state, err, safeError(err))
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM environments WHERE id='"+f.env+"'"); got != 1 {
			t.Fatalf("environment rows after the %s refusal = %d", state, got)
		}
	}

	// Terminal leases and their effects go with the environment.
	execRaw(t, db, `UPDATE dynamic_leases SET state='revoked' WHERE id='`+live+`'`)
	if err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope()); err != nil {
		t.Fatalf("delete with only terminal leases: %v", err)
	}
	for _, table := range []string{"dynamic_leases", "dynamic_effects", "environments"} {
		if got := queryInt(t, db, "SELECT COUNT(*) FROM "+table+" WHERE project_id='"+string(f.project)+"'"); got != 0 {
			t.Errorf("%s still has %d rows after the environment was deleted", table, got)
		}
	}
}

func TestEnvironmentDeleteLiveCertificate(t *testing.T) {
	forEngines(t, runEnvironmentDeleteLiveCertificate)
}

func runEnvironmentDeleteLiveCertificate(t *testing.T, db *store.DB) {
	f := seedDefinitionsProject(t, db, "cert", true)
	now := time.Now().UTC().Truncate(time.Second)
	seedCertificateRows(t, db, f, now)
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}

	// An unexpired issued, unknown or revoked certificate refuses by name: a
	// revoked serial must stay on its CRL until it expires.
	project := string(f.project)
	refused := func(label, id string) {
		t.Helper()
		err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope())
		if !errors.Is(err, service.ErrEnvironmentHasLiveCertificate) || !errors.Is(err, domain.ErrConflict) || !strings.Contains(safeError(err), id) {
			t.Fatalf("delete with %s = %v (%q)", label, err, safeError(err))
		}
	}
	refused("an issued certificate", project+"_issued")
	execRaw(t, db, `UPDATE pki_certificates SET state='unknown' WHERE id='`+project+`_issued'`)
	refused("an unknown certificate", project+"_issued")
	execRaw(t, db, `UPDATE pki_certificates SET state='expired' WHERE id='`+project+`_issued'`)
	refused("an unexpired revoked certificate", project+"_revoked")
	if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_certificates WHERE project_id='"+string(f.project)+"'"); got != 4 {
		t.Fatalf("certificates after the refusals = %d", got)
	}

	// Once the live ones expire or fail, every row is purged with the environment.
	execRaw(t, db, `UPDATE pki_certificates SET not_after=`+pkiStamp(db, now.Add(-time.Minute))+` WHERE id='`+project+`_revoked'`)
	if err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope()); err != nil {
		t.Fatalf("delete with only terminal certificates: %v", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_certificates WHERE project_id='"+string(f.project)+"'"); got != 0 {
		t.Fatalf("%d certificates outlived the environment", got)
	}
}

// The definitions apply executor deletes an environment through its own path and
// must refuse and purge exactly like Environments.Delete.
func TestDefinitionsApplyEnvironmentDeleteLiveRows(t *testing.T) {
	forEngines(t, runDefinitionsApplyEnvironmentDeleteLiveRows)
}

func runDefinitionsApplyEnvironmentDeleteLiveRows(t *testing.T, db *store.DB) {
	f := seedDefinitionsProject(t, db, "liverows", true)
	now := time.Now().UTC().Truncate(time.Second)
	seedLeaseRows(t, db, f)
	seedCertificateRows(t, db, f, now)
	svc := definitionsService(t, db)
	project := string(f.project)

	bundle := parseDefinitions(t, exportDefinitions(t, svc, f))
	bundle.Environments = nil
	raw := encodeDefinitions(t, bundle)
	apply := func() error {
		plan := planDefinitions(t, svc, f, raw)
		_, err := svc.Apply(t.Context(), service.LocalPrincipal(alice), f.scope(), plan.ID, service.ApplyOptions{AllowDelete: true})
		return err
	}

	if err := apply(); !errors.Is(err, service.ErrEnvironmentHasLiveLease) || !strings.Contains(safeError(err), "prj_def_liverows_live") {
		t.Fatalf("apply with a live lease = %v (%q)", err, safeError(err))
	}
	execRaw(t, db, `UPDATE dynamic_leases SET state='expired' WHERE id='prj_def_liverows_live'`)
	if err := apply(); !errors.Is(err, service.ErrEnvironmentHasLiveCertificate) || !strings.Contains(safeError(err), "prj_def_liverows_issued") {
		t.Fatalf("apply with a live certificate = %v (%q)", err, safeError(err))
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM environments WHERE project_id='"+project+"'"); got != 1 {
		t.Fatalf("environments after the refusals = %d", got)
	}
	execRaw(t, db, `UPDATE pki_certificates SET state='expired' WHERE id='prj_def_liverows_issued'`)
	execRaw(t, db, `UPDATE pki_certificates SET not_after=`+pkiStamp(db, now.Add(-time.Minute))+` WHERE id='prj_def_liverows_revoked'`)
	if err := apply(); err != nil {
		t.Fatalf("apply with only terminal rows: %v", err)
	}
	for _, table := range []string{"dynamic_leases", "dynamic_effects", "pki_certificates", "environments"} {
		if got := queryInt(t, db, "SELECT COUNT(*) FROM "+table+" WHERE project_id='"+project+"'"); got != 0 {
			t.Errorf("%s still has %d rows after the apply deleted the environment", table, got)
		}
	}
}
