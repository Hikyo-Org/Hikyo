package isolation

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5/pgtype"
)

// #1420: a minted lease, an issued certificate, an adapter target or a dynamic
// provider pins its scope through a restrict foreign key. Each refuses the
// delete by name; terminal leases, terminal certificate history (owner decision
// B on #1420, 2026-11-23) and tombstoned providers are purged with their scope.
// Adapter history is retained, never purged.

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

// seedCertificateRows seeds one certificate per state that matters: four live
// ones (issued, issuing, renewed, unexpired revoked) and two terminal ones.
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
	future := now.Add(24 * time.Hour)
	cert(project+"_issued", "issued", future)
	cert(project+"_issuing", "issuing", future)
	cert(project+"_renewed", "renewed", future)
	cert(project+"_revoked", "revoked", future)
	cert(project+"_expired", "expired", now.Add(-time.Hour))
	cert(project+"_failed", "failed", now.Add(-time.Hour))
}

func count(t *testing.T, db *store.DB, table string, f definitionsFixture) int64 {
	t.Helper()
	return queryInt(t, db, "SELECT COUNT(*) FROM "+table+" WHERE project_id='"+string(f.project)+"'")
}

func TestEnvironmentDeleteLiveLease(t *testing.T) { forEngines(t, runEnvironmentDeleteLiveLease) }

func runEnvironmentDeleteLiveLease(t *testing.T, db *store.DB) {
	f := seedDefinitionsProject(t, db, "lease", true)
	seedLeaseRows(t, db, f)
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
	live := string(f.project) + "_live"

	// Every non-terminal state refuses by name and keeps the environment, the
	// leases and their effects.
	for _, state := range []string{"active", "minting", "renewing", "revoking", "unknown"} {
		execRaw(t, db, `UPDATE dynamic_leases SET state='`+state+`' WHERE id='`+live+`'`)
		err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope())
		if !errors.Is(err, service.ErrEnvironmentHasLiveLease) || !errors.Is(err, domain.ErrConflict) || !strings.Contains(safeError(err), live) {
			t.Fatalf("delete with a %s lease = %v (%q)", state, err, safeError(err))
		}
		for table, want := range map[string]int64{"environments": 1, "dynamic_leases": 4, "dynamic_effects": 4} {
			if got := count(t, db, table, f); got != want {
				t.Fatalf("%s rows after the %s refusal = %d, want %d", table, state, got, want)
			}
		}
	}

	// Terminal leases and their effects go with the environment.
	execRaw(t, db, `UPDATE dynamic_leases SET state='revoked' WHERE id='`+live+`'`)
	if err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope()); err != nil {
		t.Fatalf("delete with only terminal leases: %v", err)
	}
	for _, table := range []string{"dynamic_leases", "dynamic_effects", "environments"} {
		if got := count(t, db, table, f); got != 0 {
			t.Errorf("%s still has %d rows after the environment was deleted", table, got)
		}
	}
}

func TestEnvironmentDeleteCertificates(t *testing.T) {
	forEngines(t, runEnvironmentDeleteCertificates)
}

func runEnvironmentDeleteCertificates(t *testing.T, db *store.DB) {
	f := seedDefinitionsProject(t, db, "cert", true)
	now := time.Now().UTC().Truncate(time.Second)
	seedCertificateRows(t, db, f, now)
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
	project := string(f.project)
	refused := func(label, id string) {
		t.Helper()
		err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope())
		if !errors.Is(err, service.ErrEnvironmentHasLiveCertificate) || !errors.Is(err, domain.ErrPKICertificateRetention) || !errors.Is(err, domain.ErrConflict) || !strings.Contains(safeError(err), id) {
			t.Fatalf("delete with %s = %v (%q)", label, err, safeError(err))
		}
		if got := count(t, db, "pki_certificates", f); got != 6 {
			t.Fatalf("certificates after the %s refusal = %d, want 6", label, got)
		}
		if got := count(t, db, "environments", f); got != 1 {
			t.Fatalf("environments after the %s refusal = %d", label, got)
		}
	}

	// Each live state refuses by name, one after the other as it lapses; a revoked
	// certificate stays live until it expires because it keeps its CRL entry.
	refused("an issued certificate", project+"_issued")
	execRaw(t, db, `UPDATE pki_certificates SET state='unknown' WHERE id='`+project+`_issued'`)
	refused("an unknown certificate", project+"_issued")
	execRaw(t, db, `UPDATE pki_certificates SET state='expired' WHERE id='`+project+`_issued'`)
	refused("an issuing certificate", project+"_issuing")
	execRaw(t, db, `UPDATE pki_certificates SET state='failed' WHERE id='`+project+`_issuing'`)
	refused("a renewed certificate", project+"_renewed")
	execRaw(t, db, `UPDATE pki_certificates SET state='expired' WHERE id='`+project+`_renewed'`)
	refused("an unexpired revoked certificate", project+"_revoked")

	// Only terminal history is left: it is purged with the environment, the
	// issuer survives, and the delete audit event records the purge count.
	execRaw(t, db, `UPDATE pki_certificates SET not_after=`+pkiStamp(db, now.Add(-time.Minute))+` WHERE id='`+project+`_revoked'`)
	if err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope()); err != nil {
		t.Fatalf("delete with only terminal certificate history: %v", err)
	}
	if got := count(t, db, "pki_certificates", f); got != 0 {
		t.Errorf("%d certificates outlived the environment", got)
	}
	if got := count(t, db, "environments", f); got != 0 {
		t.Errorf("environment survived the delete: %d", got)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM pki_issuers WHERE id='pki_iss_"+project+"'"); got != 1 {
		t.Errorf("issuer rows = %d, want the issuer kept", got)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='settings.environment_deleted' AND project_id='"+project+"' AND CAST(payload AS TEXT) LIKE '%\"purged_certificates\":6%'"); got != 1 {
		t.Errorf("environment_deleted events recording 6 purged certificates = %d, want 1", got)
	}
}

func TestEnvironmentDeleteAdapterTargets(t *testing.T) {
	forEngines(t, runEnvironmentDeleteAdapterTargets)
}

func runEnvironmentDeleteAdapterTargets(t *testing.T, db *store.DB) {
	f := seedDefinitionsProject(t, db, "adapter", true)
	project := string(f.project)
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
	execRaw(t, db, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_`+project+`','org_a','`+project+`','forgejo','https://git.example','usr_alice','active',`+ts+`)`)
	execRaw(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_`+project+`','org_a','`+project+`','`+f.env+`','adp_`+project+`','repository','acme','app',42,'',1,'tombstoned','never',`+ts+`)`)

	// A tombstoned target still refuses, by name.
	err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope())
	if !errors.Is(err, service.ErrEnvironmentHasAdapterTargets) || !errors.Is(err, domain.ErrConflict) || !strings.Contains(safeError(err), "tgt_"+project) {
		t.Fatalf("delete with a tombstoned adapter target = %v (%q)", err, safeError(err))
	}
	if got := count(t, db, "environments", f); got != 1 {
		t.Fatalf("environments after the refusal = %d", got)
	}
	if got := count(t, db, "adapter_targets", f); got != 1 {
		t.Fatalf("adapter targets after the refusal = %d", got)
	}
}

// A configure fence is committed before its target row, with no foreign key to
// it, so a failed environment setup leaves a fence with no target. It still
// refuses by name rather than hitting the raw foreign-key conflict.
func TestEnvironmentDeleteOrphanConfigureFence(t *testing.T) {
	forEngines(t, runEnvironmentDeleteOrphanConfigureFence)
}

func runEnvironmentDeleteOrphanConfigureFence(t *testing.T, db *store.DB) {
	f := seedDefinitionsProject(t, db, "fence", true)
	project := string(f.project)
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
	execRaw(t, db, `INSERT INTO adapter_configure_fences (target_id,org_id,project_id,environment_id,destination_kind,destination_owner,destination_name,destination_environment,generation,effect_id,lease_expires_at,state,created_at) VALUES ('tgt_`+project+`','org_a','`+project+`','`+f.env+`','environment','acme','app','staging',1,'eff_`+project+`',`+ts+`,'failed',`+ts+`)`)
	err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope())
	if !errors.Is(err, service.ErrEnvironmentHasAdapterTargets) || !errors.Is(err, domain.ErrConflict) || !strings.Contains(safeError(err), "tgt_"+project) {
		t.Fatalf("delete with an orphan configure fence = %v (%q)", err, safeError(err))
	}
	if got := count(t, db, "adapter_configure_fences", f); got != 1 {
		t.Fatalf("fences after the refusal = %d", got)
	}
}

// A refusal names at most ten ids and says when more block.
func TestEnvironmentDeleteRefusalTruncates(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedDefinitionsProject(t, db, "trunc", true)
		seedLeaseRows(t, db, f)
		project := string(f.project)
		for i := 0; i < 11; i++ {
			id := fmt.Sprintf("%s_more_%02d", project, i)
			execRaw(t, db, `INSERT INTO dynamic_leases (id,org_id,project_id,environment_id,provider_id,principal_id,principal_class,provider_handle,state,max_ttl_seconds,last_transition_at,next_attempt_at,created_at) VALUES ('`+id+`','org_a','`+project+`','`+f.env+`','dpr_`+project+`','usr_alice','human','h_`+id+`','active',3600,`+ts+`,`+ts+`,`+ts+`)`)
		}
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope())
		detail := safeError(err)
		if !errors.Is(err, service.ErrEnvironmentHasLiveLease) || !strings.Contains(detail, "(first 10 shown)") ||
			strings.Contains(detail, project+"_more_09") || !strings.Contains(detail, project+"_more_08") {
			t.Fatalf("truncated refusal = %v (%q)", err, detail)
		}
	})
}

func TestProjectDeleteDynamicProviders(t *testing.T) {
	forEngines(t, runProjectDeleteDynamicProviders)
}

func runProjectDeleteDynamicProviders(t *testing.T, db *store.DB) {
	f := seedDefinitionsProject(t, db, "provider", false)
	project := string(f.project)
	projects := &service.Projects{DB: db}
	execRaw(t, db, `INSERT INTO dynamic_providers (id,org_id,project_id,kind,origin,tls_mode,grant_role,authority_principal_id,state,created_at) VALUES ('dpr_`+project+`','org_a','`+project+`','postgres','postgres://`+project+`.example/db','verify-full','reader','usr_alice','active',`+ts+`)`)

	err := projects.Delete(t.Context(), service.LocalPrincipal(alice), f.scope())
	if !errors.Is(err, service.ErrProjectHasActiveProvider) || !errors.Is(err, domain.ErrConflict) || !strings.Contains(safeError(err), "dpr_"+project) {
		t.Fatalf("delete with an active provider = %v (%q)", err, safeError(err))
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM projects WHERE id='"+project+"'"); got != 1 {
		t.Fatalf("projects after the refusal = %d", got)
	}

	// A tombstoned provider is purged with the project.
	execRaw(t, db, `UPDATE dynamic_providers SET state='tombstoned' WHERE id='dpr_`+project+`'`)
	if err := projects.Delete(t.Context(), service.LocalPrincipal(alice), f.scope()); err != nil {
		t.Fatalf("delete with only a tombstoned provider: %v", err)
	}
	if got := count(t, db, "dynamic_providers", f); got != 0 {
		t.Fatalf("%d providers outlived the project", got)
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

	if err := apply(); !errors.Is(err, service.ErrEnvironmentHasLiveLease) || !strings.Contains(safeError(err), project+"_live") {
		t.Fatalf("apply with a live lease = %v (%q)", err, safeError(err))
	}
	execRaw(t, db, `UPDATE dynamic_leases SET state='expired' WHERE id='`+project+`_live'`)
	if err := apply(); !errors.Is(err, service.ErrEnvironmentHasLiveCertificate) || !strings.Contains(safeError(err), project+"_issued") {
		t.Fatalf("apply with a live certificate = %v (%q)", err, safeError(err))
	}
	execRaw(t, db, `UPDATE pki_certificates SET state='expired' WHERE state IN ('issued','issuing','renewed')`)
	execRaw(t, db, `UPDATE pki_certificates SET not_after=`+pkiStamp(db, now.Add(-time.Minute))+` WHERE state='revoked'`)
	if got := count(t, db, "environments", f); got != 1 {
		t.Fatalf("environments after the refusals = %d", got)
	}
	if got := count(t, db, "dynamic_leases", f); got != 4 {
		t.Fatalf("leases after the refusals = %d, want 4", got)
	}

	// With only terminal rows left, leases, effects and certificate history are purged.
	if err := apply(); err != nil {
		t.Fatalf("apply with only terminal leases and certificates: %v", err)
	}
	for _, table := range []string{"dynamic_leases", "dynamic_effects", "pki_certificates", "environments"} {
		if got := count(t, db, table, f); got != 0 {
			t.Errorf("%s still has %d rows after the apply deleted the environment", table, got)
		}
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='settings.environment_deleted' AND project_id='"+project+"' AND CAST(payload AS TEXT) LIKE '%\"purged_certificates\":6%'"); got != 1 {
		t.Errorf("apply's environment_deleted events recording 6 purged certificates = %d, want 1", got)
	}
}

// One delete reports every blocking kind together: errors.Is matches each and
// the detail names the ids of all, so neither a caller nor a test sees only the
// first kind that happened to be checked.
func TestEnvironmentDeleteReportsEveryBlockingKind(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedDefinitionsProject(t, db, "multi", true)
		project := string(f.project)
		seedLeaseRows(t, db, f)
		seedCertificateRows(t, db, f, time.Now().UTC().Truncate(time.Second))
		execRaw(t, db, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_`+project+`','org_a','`+project+`','forgejo','https://git.example','usr_alice','active',`+ts+`)`)
		execRaw(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_`+project+`','org_a','`+project+`','`+f.env+`','adp_`+project+`','repository','acme','app',42,'',1,'tombstoned','never',`+ts+`)`)
		execRaw(t, db, `INSERT INTO ssh_cas (id,org_id,project_id,environment_id,name,state,authority_principal_id,created_at) VALUES ('ssh_`+project+`','org_a','`+project+`','`+f.env+`','ca','active','usr_alice',`+ts+`)`)
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope())
		detail := safeError(err)
		if !strings.Contains(detail, "live SSH CA") {
			t.Errorf("combined refusal %q lacks the SSH CA kind", detail)
		}
		for _, want := range []error{service.ErrEnvironmentHasLiveLease, service.ErrEnvironmentHasLiveCertificate, service.ErrEnvironmentHasAdapterTargets, store.ErrSSHLiveCA} {
			if !errors.Is(err, want) {
				t.Fatalf("combined refusal %v does not match %v", err, want)
			}
		}
		for _, id := range []string{project + "_live", project + "_issued", "tgt_" + project} {
			if !strings.Contains(detail, id) {
				t.Errorf("combined refusal %q lacks %s", detail, id)
			}
		}
		if got := count(t, db, "pki_certificates", f); got != 6 {
			t.Errorf("certificates after the combined refusal = %d, want 6 (terminal purge rolled back)", got)
		}
	})
}

// Only terminal certificates plus a tombstoned adapter target and a live SSH CA:
// the certificate purge runs before the refusal and must be rolled back with the
// transaction, so every terminal row is still present afterwards.
func TestEnvironmentDeleteRefusalRollsBackCertificatePurge(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedDefinitionsProject(t, db, "rollback", true)
		project := string(f.project)
		now := time.Now().UTC().Truncate(time.Second)
		seedCertificateRows(t, db, f, now)
		execRaw(t, db, `UPDATE pki_certificates SET state='expired' WHERE state IN ('issued','issuing','renewed') AND project_id='`+project+`'`)
		execRaw(t, db, `UPDATE pki_certificates SET not_after=`+pkiStamp(db, now.Add(-time.Minute))+` WHERE state='revoked' AND project_id='`+project+`'`)
		execRaw(t, db, `INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_`+project+`','org_a','`+project+`','forgejo','https://git.example','usr_alice','active',`+ts+`)`)
		execRaw(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_`+project+`','org_a','`+project+`','`+f.env+`','adp_`+project+`','repository','acme','app',42,'',1,'tombstoned','never',`+ts+`)`)
		for _, withSSH := range []bool{false, true} {
			if withSSH {
				execRaw(t, db, `INSERT INTO ssh_cas (id,org_id,project_id,environment_id,name,state,authority_principal_id,created_at) VALUES ('ssh_`+project+`','org_a','`+project+`','`+f.env+`','ca','active','usr_alice',`+ts+`)`)
			}
			envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
			err := envs.Delete(t.Context(), service.LocalPrincipal(alice), f.envScope())
			if !errors.Is(err, service.ErrEnvironmentHasAdapterTargets) || errors.Is(err, store.ErrSSHLiveCA) != withSSH {
				t.Fatalf("delete (ssh=%v) = %v", withSSH, err)
			}
			if got := count(t, db, "pki_certificates", f); got != 6 {
				t.Fatalf("terminal certificates after the refusal (ssh=%v) = %d, want 6", withSSH, got)
			}
		}
	})
}

// The purge DELETE repeats the terminal predicate, so a live certificate that
// appears after the service's live check is never purged. Run the generated
// query directly with a live row present.
func TestPKIPurgeQueryKeepsLiveCertificates(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedDefinitionsProject(t, db, "purgepred", true)
		project := string(f.project)
		now := time.Now().UTC().Truncate(time.Second)
		seedCertificateRows(t, db, f, now)
		var purged int64
		var err error
		if db.Engine() == store.EngineSQLite {
			purged, err = sqlitegen.New(db.SQLiteWrite()).PKIPurgeTerminalEnvironmentCertificates(t.Context(), sqlitegen.PKIPurgeTerminalEnvironmentCertificatesParams{
				ChainOrg: "org_a", ChainProject: project, ChainEnv: f.env, Now: store.CanonTime(now).Format("2006-01-02T15:04:05.000000Z")})
		} else {
			purged, err = pggen.New(db.PG()).PKIPurgeTerminalEnvironmentCertificates(t.Context(), pggen.PKIPurgeTerminalEnvironmentCertificatesParams{
				ChainOrg: "org_a", ChainProject: project, ChainEnv: f.env, Now: pgtype.Timestamptz{Time: now, Valid: true}})
		}
		if err != nil || purged != 2 {
			t.Fatalf("purged %d terminal rows, err %v; want the expired and failed rows only", purged, err)
		}
		if got := count(t, db, "pki_certificates", f); got != 4 {
			t.Fatalf("live certificates after the purge = %d, want 4 (issued, issuing, renewed, unexpired revoked)", got)
		}
	})
}
