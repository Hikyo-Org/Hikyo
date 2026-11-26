package isolation

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Release (#1039 S5, ADR O4 A): a separate audited act that needs a credential
// establishment since containment, or an attested linked identity, and never
// revives or regrants anything.

const farFuture = `'2099-01-01T00:00:00.000000Z'`

func (w *incidentWorld) containedIncident(t *testing.T, withIdentity bool) string {
	t.Helper()
	if withIdentity {
		execRaw(t, w.db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_rel','acc_itarget','oidc','https://idp.example.test','subject-rel','',`+acctEpoch(t, w.db)+`,`+ts+`)`)
	}
	w.realOwners()
	id := w.openAccount(t, incResponder).Incident.ID
	w.resume(t, incResponder, id)
	if s := w.containmentState(t, incTarget); s != "contained" {
		t.Fatalf("containment_state = %q, want contained", s)
	}
	return id
}

func TestIncidentReleaseRequiresEstablishmentOrAttestation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		id := w.containedIncident(t, true)
		release := func(actor domain.PrincipalID, incident string) error {
			return w.svc.ReleasePrincipal(ctx, w.as(actor), incident, incTarget, "")
		}

		// An unattested linked identity holds the release.
		if err := release(incResponder, id); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with an unattested identity = %v, want ErrReleaseNotReady", err)
		}
		// A caller without authority over the incident, and an unknown incident,
		// get the same answer.
		if err := release(incOrgB, id); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("foreign caller = %v, want not found", err)
		}
		if err := release(incResponder, "inc_00000000-0000-0000-0000-000000000000"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unknown incident = %v, want not found", err)
		}
		if s := w.containmentState(t, incTarget); s != "contained" {
			t.Fatalf("a refused release changed containment_state to %q", s)
		}

		if err := w.svc.Attest(ctx, w.as(incResponder), id, w.itemIDByKey(t, incResponder, id, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		gen := queryInt(t, db, `SELECT session_generation FROM principals WHERE id='usr_itarget'`)
		if err := release(incResponder, id); err != nil {
			t.Fatalf("release after attestation: %v", err)
		}
		if s := w.containmentState(t, incTarget); s != "none" {
			t.Errorf("containment_state = %q, want none", s)
		}
		if got := queryInt(t, db, `SELECT session_generation FROM principals WHERE id='usr_itarget'`); got <= gen {
			t.Errorf("session_generation %d did not advance past %d", got, gen)
		}
		// Release revives and regrants nothing (O4 A).
		if n := queryInt(t, db, `SELECT COUNT(*) FROM developer_credentials WHERE id='devc_it' AND revoked_at IS NULL`); n != 0 {
			t.Error("release revived the revoked delegation")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM grants WHERE principal_id='usr_itarget'`); n != 2 {
			t.Errorf("grants = %d, want the 2 originals unchanged", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM sessions WHERE principal_id='usr_itarget'`); n != 0 {
			t.Errorf("%d sessions after release", n)
		}
		if n := w.correlated(t, "incident.principal_released", id); n != 1 {
			t.Errorf("principal_released events = %d, want 1", n)
		}
		// A second release finds nothing contained.
		if err := release(incResponder, id); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("second release = %v, want not found", err)
		}
	})
}

func TestIncidentReleaseNeedsEstablishmentForAPasswordAccount(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		execRaw(t, db, `INSERT INTO password_credentials (account_id, verifier, kdf_memory_kib, kdf_time, kdf_parallelism, dek_version, credential_epoch, row_version, updated_at) VALUES ('acc_itarget', `+blobLit(db, []byte("pw-verifier"))+`, 1, 1, 1, 1, `+acctEpoch(t, db)+`, 1, `+ts+`)`)
		id := w.containedIncident(t, false)
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, "") }

		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release before any establishment = %v, want ErrReleaseNotReady", err)
		}
		// An authority minted after containment but never consumed is not an
		// establishment.
		execRaw(t, db, `INSERT INTO credential_authorities (id, verifier, account_id, purpose, issued_by, credential_epoch, expires_at, consumed_at, created_at) VALUES ('ca_rel_1', `+blobLit(db, []byte("ca-1"))+`, 'acc_itarget', 'establish-credential', 'credential-reset', `+acctEpoch(t, db)+`, `+farFuture+`, NULL, `+farFuture+`)`)
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with an unconsumed authority = %v, want ErrReleaseNotReady", err)
		}
		reset, err := authService(t, db).ResetCredential(ctx, w.as(incResponder), string(incTarget), "response")
		if err != nil {
			t.Fatalf("reset of the contained account: %v", err)
		}
		if err := authService(t, db).EstablishCredential(ctx, reset.Authority, "a password established after containment"); err != nil {
			t.Fatalf("establishing the reset authority: %v", err)
		}
		if err := release(); err != nil {
			t.Fatalf("release after establishment: %v", err)
		}
		if s := w.containmentState(t, incTarget); s != "none" {
			t.Errorf("containment_state = %q, want none", s)
		}
	})
}

// An account with neither a password nor a linked identity has nothing that
// records a replaced way to sign in, so release is refused.
func TestIncidentReleaseRefusesAnAccountWithNothingToReplace(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		id := w.containedIncident(t, false)
		if err := w.svc.ReleasePrincipal(t.Context(), w.as(incResponder), id, incTarget, ""); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release = %v, want ErrReleaseNotReady", err)
		}
	})
}

// The real re-establishment path: an operator reset mints an authority after
// containment, establishing it creates the password, and that is the evidence
// release reads (ADR O4 A).
func TestIncidentReleaseAcceptsARealResetEstablishment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		auth := authService(t, db)
		id := w.containedIncident(t, false)
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, "") }

		reset, err := auth.ResetCredential(ctx, w.as(incResponder), string(incTarget), "response")
		if err != nil {
			t.Fatalf("reset of the contained account: %v", err)
		}
		// Minted but not yet established: a password-less account with nothing
		// attested stays contained.
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release before establishing = %v, want ErrReleaseNotReady", err)
		}
		if err := auth.EstablishCredential(ctx, reset.Authority, "a password established after containment"); err != nil {
			t.Fatalf("establishing the reset authority: %v", err)
		}
		if err := release(); err != nil {
			t.Fatalf("release after a real establishment: %v", err)
		}
		if s := w.containmentState(t, incTarget); s != "none" {
			t.Errorf("containment_state = %q, want none", s)
		}
	})
}

// An org incident lists each member's linked identities, so release waits on
// their attestation like an account incident does, and a member with no password
// is releasable once they are attested (no break-glass needed).
func TestIncidentReleaseOrgIncidentWaitsForIdentityAttestation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_rel','acc_itarget','oidc','https://idp.example.test','subject-rel','',`+acctEpoch(t, db)+`,`+ts+`)`)
		w.realOwners()
		id := w.openOrg(t, incResponder)
		w.resume(t, incResponder, id)
		if s := w.containmentState(t, incTarget); s != "contained" {
			t.Fatalf("containment_state = %q, want contained", s)
		}
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, "") }
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with an unattested identity = %v, want ErrReleaseNotReady", err)
		}
		if err := w.svc.Attest(ctx, w.as(incResponder), id, w.itemIDByKey(t, incResponder, id, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		if err := release(); err != nil {
			t.Fatalf("release of a password-less member after attestation: %v", err)
		}
	})
}

// Release applies containment's own classification: a target in two orgs needs
// instance scope, so the holder of one org's credential-reset cannot release it
// even through the org incident that contained it.
func TestIncidentReleaseKeepsContainmentsOrgBound(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_rel_b', 'usr_itarget', 'read', 'org_b', `+ts+`)`)
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_rel','acc_itarget','oidc','https://idp.example.test','subject-rel','',`+acctEpoch(t, db)+`,`+ts+`)`)
		w.realOwners()
		id := w.openOrg(t, incInstance)
		w.resume(t, incInstance, id)
		if s := w.containmentState(t, incTarget); s != "contained" {
			t.Fatalf("containment_state = %q, want contained", s)
		}
		if err := w.svc.Attest(ctx, w.as(incInstance), id, w.itemIDByKey(t, incInstance, id, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, ""); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("an org-only holder released a two-org target = %v, want not found", err)
		}
		if s := w.containmentState(t, incTarget); s != "contained" {
			t.Fatalf("a refused release changed containment_state to %q", s)
		}
		if err := w.svc.ReleasePrincipal(ctx, w.as(incInstance), id, incTarget, ""); err != nil {
			t.Fatalf("the instance holder's release: %v", err)
		}
	})
}

// Only an establishment while the containing incident held the principal counts,
// and a sweep never is one.
// A reset minted before containment and swept by it is not one; an authority
// established before containment is not one; and a skewed-ahead node's mint (R1)
// swept by a later correct reset (R2) is not one, though R1 outranks R2 by mint
// time. Establishing R2 is, and releases.
func TestIncidentReleaseCountsOnlyAnEstablishmentAfterContainment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		auth := authService(t, db)
		execRaw(t, db, `INSERT INTO password_credentials (account_id, verifier, kdf_memory_kib, kdf_time, kdf_parallelism, dek_version, credential_epoch, row_version, updated_at) VALUES ('acc_itarget', `+blobLit(db, []byte("pw-verifier"))+`, 1, 1, 1, 1, `+acctEpoch(t, db)+`, 1, `+ts+`)`)
		if _, err := auth.ResetCredential(ctx, w.as(incResponder), string(incTarget), "response"); err != nil {
			t.Fatalf("reset before containment: %v", err)
		}
		id := w.containedIncident(t, false)
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, "") }
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release after a pre-containment reset was swept = %v, want ErrReleaseNotReady", err)
		}
		// Established before the containment: no incident held the principal, so
		// nothing is stamped, whatever its consumed_at reads.
		execRaw(t, db, `INSERT INTO credential_authorities (id, verifier, account_id, purpose, issued_by, credential_epoch, expires_at, consumed_at, created_at) VALUES ('ca_rel_old', `+blobLit(db, []byte("ca-old"))+`, 'acc_itarget', 'establish-credential', 'credential-reset', `+acctEpoch(t, db)+`, `+farFuture+`, `+farFuture+`, `+ts+`)`)
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with an establishment before containment = %v, want ErrReleaseNotReady", err)
		}
		// R1: minted after containment by a clock far ahead. R2: the correct reset,
		// which sweeps R1 and is minted with an earlier created_at than R1.
		execRaw(t, db, `INSERT INTO credential_authorities (id, verifier, account_id, purpose, issued_by, credential_epoch, expires_at, consumed_at, created_at) VALUES ('ca_rel_r1', `+blobLit(db, []byte("ca-r1"))+`, 'acc_itarget', 'establish-credential', 'credential-reset', `+acctEpoch(t, db)+`, `+farFuture+`, NULL, `+farFuture+`)`)
		r2, err := auth.ResetCredential(ctx, w.as(incResponder), string(incTarget), "response")
		if err != nil {
			t.Fatalf("second reset: %v", err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities WHERE id = 'ca_rel_r1' AND consumed_at IS NOT NULL AND established_in_incident IS NULL`); n != 1 {
			t.Fatal("the second reset did not sweep R1 without stamping it")
		}
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with R1 swept and R2 unestablished = %v, want ErrReleaseNotReady", err)
		}
		if err := auth.EstablishCredential(ctx, r2.Authority, "a password established after the second reset"); err != nil {
			t.Fatalf("establishing R2: %v", err)
		}
		if err := release(); err != nil {
			t.Fatalf("release after establishing R2: %v", err)
		}
	})
}

// A cut keyed to one member holds that member only, and an attested cut covers
// no identity: each must have its own attested item, so release is decidable per target
// and never depends on a clock.
func TestIncidentReleaseTruncationIsKeyedPerMember(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_rel','acc_itarget','oidc','https://idp.example.test','subject-rel','',`+acctEpoch(t, db)+`,`+ts+`)`)
		w.realOwners()
		id := w.openOrg(t, incResponder)
		w.resume(t, incResponder, id)
		if err := w.svc.Attest(ctx, w.as(incResponder), id, w.itemIDByKey(t, incResponder, id, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, "") }
		cut := func(itemID, member string) {
			execRaw(t, db, `INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, row_version, created_at, updated_at) VALUES ('`+itemID+`', '`+id+`', 'external_identity.remove', 'enumeration', '`+member+`', 'org/org_a', 'unknown', 'enumeration_truncated', 1, `+ts+`, `+ts+`)`)
		}
		// Another member's cut does not hold this target, and an identity the
		// enumeration never listed does.
		cut("itm_cut_other", "usr_someone_else")
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_unlisted','acc_itarget','oidc','https://idp.example.test','subject-unlisted','',`+acctEpoch(t, db)+`,`+ts+`)`)
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with an unlisted identity = %v, want ErrReleaseNotReady", err)
		}
		// A cut keyed to the target holds it until attested. Attesting it records an
		// attested item for each identity linked at that moment, so coverage is the
		// ledger's, never a clock's: an identity linked afterwards on a node whose clock
		// ran slow (created long before the cut) still holds release.
		cut("itm_cut_target", string(incTarget))
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with the target's cut unattested = %v, want ErrReleaseNotReady", err)
		}
		if err := w.svc.Attest(ctx, w.as(incResponder), id, "itm_cut_target"); err != nil {
			t.Fatal(err)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE incident_id='`+id+`' AND object_id='xid_unlisted' AND outcome='attested'`); got != 1 {
			t.Fatalf("attesting the cut recorded %d attested items for the unlisted identity, want 1", got)
		}
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_slow','acc_itarget','oidc','https://idp.example.test','subject-slow','',`+acctEpoch(t, db)+`,'2000-01-01T00:00:00.000000Z')`)
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with an identity linked after the cut was attested = %v, want ErrReleaseNotReady", err)
		}
		// The late identity needs its own attested item (a reopen lists it, an operator attests it).
		execRaw(t, db, `INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, row_version, created_at, updated_at, attested_by, attested_at) VALUES ('itm_slow', '`+id+`', 'external_identity.remove', 'external_identity', 'xid_slow', 'org/org_a', 'attested', '', 2, `+ts+`, `+ts+`, 'usr_irsp', `+ts+`)`)
		if err := release(); err != nil {
			t.Fatalf("release with every identity attested: %v", err)
		}
	})
}

// Two incidents that both recorded the principal contained: the state belongs to
// the first, but the second's ledger reads done. Release is refused, naming the
// other incident, until it is closed.
func TestIncidentReleaseWaitsForAnotherOpenIncidentHoldingThePrincipal(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_rel','acc_itarget','oidc','https://idp.example.test','subject-rel','',`+acctEpoch(t, db)+`,`+ts+`)`)
		w.realOwners()
		// Both incidents enumerate before either contains, so both list the account.
		first := w.openAccount(t, incResponder).Incident.ID
		second := w.openOrg(t, incResponder)
		w.resume(t, incResponder, first)
		w.resume(t, incResponder, second)
		if err := w.svc.Attest(ctx, w.as(incResponder), first, w.itemIDByKey(t, incResponder, first, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		if got := w.itemMap(t, incResponder, second)["account.contain|account|usr_itarget"]; got != "done/" {
			t.Fatalf("the second incident's account.contain item = %q, want done/", got)
		}
		err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), first, incTarget, "")
		if !errors.Is(err, service.ErrReleaseNotReady) || !strings.Contains(err.Error(), second) {
			t.Fatalf("release while %s is open = %v, want ErrReleaseNotReady naming it", second, err)
		}
		if _, err := w.svc.Close(ctx, w.as(incResponder), second, true); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), first, incTarget, ""); err != nil {
			t.Fatalf("release after the other incident closed: %v", err)
		}
	})
}

// The other incident is named only to a caller who can read it. One in another
// org still holds the release, with a generic reason.
func TestIncidentReleaseDoesNotNameAnUnreadableOtherIncident(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		id := w.containedIncident(t, true)
		if err := w.svc.Attest(ctx, w.as(incResponder), id, w.itemIDByKey(t, incResponder, id, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `INSERT INTO incidents (id, scope_kind, scope_id, org_id, status, opened_by, opened_at) VALUES ('inc_foreign_holder', 'org', 'org_b', 'org_b', 'open', 'usr_irspb', `+ts+`)`)
		execRaw(t, db, `INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, row_version, created_at, updated_at) VALUES ('itm_foreign_hold', 'inc_foreign_holder', 'account.contain', 'account', 'usr_itarget', 'org/org_b', 'done', '', 1, `+ts+`, `+ts+`)`)
		denied := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'grant.denied'`)
		}
		before := denied()
		err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, "")
		if !errors.Is(err, service.ErrReleaseNotReady) || strings.Contains(err.Error(), "inc_foreign_holder") {
			t.Fatalf("release with an unreadable other holder = %v, want ErrReleaseNotReady without its id", err)
		}
		// The probe is not an authorization, so it writes no denial into the other org's audit.
		if after := denied(); after != before {
			t.Fatalf("grant.denied rows %d -> %d, want unchanged", before, after)
		}
	})
}

// An instance-scope incident holding the principal is named only to a caller
// with the instance incident capability, and the probe records no denial.
func TestIncidentReleaseNamesAnInstanceScopeHolderOnlyToAnInstanceCaller(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		id := w.containedIncident(t, true)
		if err := w.svc.Attest(ctx, w.as(incResponder), id, w.itemIDByKey(t, incResponder, id, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `INSERT INTO incidents (id, scope_kind, scope_id, org_id, status, opened_by, opened_at) VALUES ('inc_inst_holder', 'account', 'usr_itarget', '', 'open', 'usr_iinst', `+ts+`)`)
		execRaw(t, db, `INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, row_version, created_at, updated_at) VALUES ('itm_inst_hold', 'inc_inst_holder', 'account.contain', 'account', 'usr_itarget', 'instance', 'done', '', 1, `+ts+`, `+ts+`)`)
		denied := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'grant.denied'`) +
				queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'grant.denied'`)
		}
		before := denied()
		err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, "")
		if !errors.Is(err, service.ErrReleaseNotReady) || strings.Contains(err.Error(), "inc_inst_holder") {
			t.Fatalf("release by an org caller with an instance-scope holder = %v, want ErrReleaseNotReady without its id", err)
		}
		if after := denied(); after != before {
			t.Fatalf("grant.denied rows %d -> %d, want unchanged", before, after)
		}
		err = w.svc.ReleasePrincipal(ctx, w.as(incInstance), id, incTarget, "")
		if !errors.Is(err, service.ErrReleaseNotReady) || !strings.Contains(err.Error(), "inc_inst_holder") {
			t.Fatalf("release by an instance caller = %v, want ErrReleaseNotReady naming inc_inst_holder", err)
		}
	})
}

// An attested cut row vouches only for the identities the enumeration left
// unlisted: a listed identity still needs its own item attested.
func TestIncidentReleaseCutDoesNotStandInForAListedIdentity(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_rel','acc_itarget','oidc','https://idp.example.test','subject-rel','',`+acctEpoch(t, db)+`,`+ts+`)`)
		w.realOwners()
		id := w.openOrg(t, incResponder)
		w.resume(t, incResponder, id)
		execRaw(t, db, `INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, row_version, created_at, updated_at) VALUES ('itm_cut_target', '`+id+`', 'external_identity.remove', 'enumeration', '`+string(incTarget)+`', 'org/org_a', 'unknown', 'enumeration_truncated', 1, `+ts+`, `+ts+`)`)
		if err := w.svc.Attest(ctx, w.as(incResponder), id, "itm_cut_target"); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, ""); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with only the cut attested = %v, want ErrReleaseNotReady", err)
		}
		if err := w.svc.Attest(ctx, w.as(incResponder), id, w.itemIDByKey(t, incResponder, id, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, ""); err != nil {
			t.Fatalf("release with the listed identity and the cut attested: %v", err)
		}
	})
}

// An attested cut vouches only for identities that existed when the enumeration
// was cut; one linked afterwards is not covered.
func TestIncidentReleaseCutDoesNotCoverAnIdentityLinkedAfterIt(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		w.realOwners()
		id := w.openOrg(t, incResponder)
		w.resume(t, incResponder, id)
		execRaw(t, db, `INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, row_version, created_at, updated_at) VALUES ('itm_cut_late', '`+id+`', 'external_identity.remove', 'enumeration', '`+string(incTarget)+`', 'org/org_a', 'unknown', 'enumeration_truncated', 1, `+ts+`, `+ts+`)`)
		if err := w.svc.Attest(ctx, w.as(incResponder), id, "itm_cut_late"); err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_late','acc_itarget','oidc','https://idp.example.test','subject-late','',`+acctEpoch(t, db)+`,`+farFuture+`)`)
		if err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, ""); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with an identity linked after the cut = %v, want ErrReleaseNotReady", err)
		}
	})
}

// A restore lists a contained principal as such, and reconciling it re-asserts
// its grants without lifting containment: release is the separate incident act.
func TestRestoreReconcileDoesNotLiftContainment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		w.containedIncident(t, true)
		// A real archive restore and reopen: the restored authentication state is inert.
		db = restoreIsolationFixture(t, db)
		restore := &service.Restore{DB: db}
		status, err := restore.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(status.Contained, incTarget) || slices.Contains(status.Contained, incResponder) {
			t.Fatalf("contained pending = %v, want exactly the contained target", status.Contained)
		}
		after, err := restore.Reconcile(ctx, incTarget)
		if err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(after.Pending, func(r authz.PrincipalRef) bool { return r.ID == incTarget }) {
			t.Error("the target is still pending after its reconciliation")
		}
		if s := queryString(t, db, `SELECT containment_state FROM principals WHERE id='usr_itarget'`); s != "contained" {
			t.Errorf("containment_state = %q after reconcile, want contained", s)
		}
	})
}

// The establishing node's clock must not decide release: a password established
// before containment by a node whose clock ran far ahead carries a consumed_at
// later than contained_at, and it must still not count. The establishment stamps
// the incident it read, so the stamp, not any timestamp, is the evidence.
func TestIncidentReleaseIgnoresAClockAheadEstablishmentBeforeContainment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		auth := authService(t, db)
		execRaw(t, db, `INSERT INTO password_credentials (account_id, verifier, kdf_memory_kib, kdf_time, kdf_parallelism, dek_version, credential_epoch, row_version, updated_at) VALUES ('acc_itarget', `+blobLit(db, []byte("pw-verifier"))+`, 1, 1, 1, 1, `+acctEpoch(t, db)+`, 1, `+farFuture+`)`)
		execRaw(t, db, `INSERT INTO credential_authorities (id, verifier, account_id, purpose, issued_by, credential_epoch, expires_at, consumed_at, created_at) VALUES ('ca_rel_fast', `+blobLit(db, []byte("ca-fast"))+`, 'acc_itarget', 'establish-credential', 'credential-reset', `+acctEpoch(t, db)+`, `+farFuture+`, `+farFuture+`, `+farFuture+`)`)
		id := w.containedIncident(t, false)
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, "") }
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with only a clock-ahead establishment before containment = %v, want ErrReleaseNotReady", err)
		}
		if s := w.containmentState(t, incTarget); s != "contained" {
			t.Fatalf("containment_state = %q after a refused release, want contained", s)
		}
		// A stamp naming a different incident is not this incident's establishment.
		execRaw(t, db, `INSERT INTO credential_authorities (id, verifier, account_id, purpose, issued_by, credential_epoch, expires_at, consumed_at, established_in_incident, created_at) VALUES ('ca_rel_other', `+blobLit(db, []byte("ca-other"))+`, 'acc_itarget', 'establish-credential', 'credential-reset', `+acctEpoch(t, db)+`, `+farFuture+`, `+ts+`, 'inc_00000000-0000-0000-0000-000000000000', `+ts+`)`)
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with another incident's stamp = %v, want ErrReleaseNotReady", err)
		}
		// A real establishment during the incident stamps it and releases.
		r, err := auth.ResetCredential(ctx, w.as(incResponder), string(incTarget), "response")
		if err != nil {
			t.Fatalf("reset: %v", err)
		}
		if err := auth.EstablishCredential(ctx, r.Authority, "a password established during the incident"); err != nil {
			t.Fatalf("establish: %v", err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities WHERE established_in_incident = '`+id+`'`); n != 1 {
			t.Fatalf("authorities stamped with the incident = %d, want 1", n)
		}
		if err := release(); err != nil {
			t.Fatalf("release after an establishment during the incident: %v", err)
		}
	})
}

// Release through an incident that did not record the containment is one answer,
// "not found", whatever else is true of that incident. The first incident
// contains the account and is closed; the second also holds the principal and has
// its identity attested. Its release attempt must not reach the readiness checks
// (which would answer ErrReleaseNotReady for an unattested identity) nor clear the
// state.
func TestIncidentReleaseRefusesANonContainingIncident(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_rel','acc_itarget','oidc','https://idp.example.test','subject-rel','',`+acctEpoch(t, db)+`,`+ts+`)`)
		w.realOwners()
		// Both enumerate before either contains, so both list the account.
		first := w.openAccount(t, incResponder).Incident.ID
		second := w.openOrg(t, incResponder)
		w.resume(t, incResponder, first)
		w.resume(t, incResponder, second)
		if got := queryString(t, db, `SELECT contained_incident_id FROM principals WHERE id='usr_itarget'`); got != first {
			t.Fatalf("contained_incident_id = %q, want the first incident %q", got, first)
		}
		if _, err := w.svc.Close(ctx, w.as(incResponder), first, true); err != nil {
			t.Fatal(err)
		}
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), second, incTarget, "") }
		// Unattested: a missing containing-incident check would answer ErrReleaseNotReady.
		if err := release(); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("release through a non-containing incident, identity unattested = %v, want not found", err)
		}
		if err := w.svc.Attest(ctx, w.as(incResponder), second, w.itemIDByKey(t, incResponder, second, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		if err := release(); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("release through a non-containing incident, identity attested = %v, want not found", err)
		}
		if s := w.containmentState(t, incTarget); s != "contained" {
			t.Fatalf("containment_state = %q after a refused release, want contained", s)
		}
	})
}

// The store setter binds the containing incident in the same UPDATE that clears
// the state, so a release naming another incident changes nothing even when the
// service check is bypassed.
func TestReleasePrincipalContainmentBindsTheContainingIncident(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		w := newIncidentWorld(t, db)
		id := w.containedIncident(t, false)
		err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			return az.ReleasePrincipalContainment(ctx, incTarget, "inc_00000000-0000-0000-0000-000000000000")
		})
		if !errors.Is(err, authn.ErrContainmentUnchanged) {
			t.Fatalf("store release naming another incident = %v, want ErrContainmentUnchanged", err)
		}
		if s := w.containmentState(t, incTarget); s != "contained" {
			t.Fatalf("containment_state = %q, want contained", s)
		}
		err = tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			return az.ReleasePrincipalContainment(ctx, incTarget, id)
		})
		if err != nil {
			t.Fatalf("store release naming the containing incident: %v", err)
		}
	})
}

// Re-containing a principal in the same incident starts a new episode: the
// establishment that earned the first release must not count for the second.
func TestIncidentReleaseEvidenceDoesNotSurviveARecontainment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		auth := authService(t, db)
		execRaw(t, db, `INSERT INTO password_credentials (account_id, verifier, kdf_memory_kib, kdf_time, kdf_parallelism, dek_version, credential_epoch, row_version, updated_at) VALUES ('acc_itarget', `+blobLit(db, []byte("pw-verifier"))+`, 1, 1, 1, 1, `+acctEpoch(t, db)+`, 1, `+ts+`)`)
		id := w.containedIncident(t, false)
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, "") }
		establish := func(password string) {
			r, err := auth.ResetCredential(ctx, w.as(incResponder), string(incTarget), "response")
			if err != nil {
				t.Fatalf("reset: %v", err)
			}
			if err := auth.EstablishCredential(ctx, r.Authority, password); err != nil {
				t.Fatalf("establish: %v", err)
			}
		}
		establish("the first episode's password")
		if err := release(); err != nil {
			t.Fatalf("first release: %v", err)
		}
		recontain := func() {
			err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				return az.ContainPrincipal(ctx, incTarget, time.Now(), id)
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		recontain()
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release after re-containment with only the old establishment = %v, want ErrReleaseNotReady", err)
		}
		if s := w.containmentState(t, incTarget); s != "contained" {
			t.Fatalf("containment_state = %q, want contained", s)
		}
		establish("the second episode's password")
		if err := release(); err != nil {
			t.Fatalf("release after a new establishment: %v", err)
		}
	})
}

// A release waits for the ledger's own containment item to be terminal, or a later
// resume would contain the account again.
func TestIncidentReleaseRefusesWhileTheContainmentItemIsOpen(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		id := w.containedIncident(t, true)
		if err := w.svc.Attest(ctx, w.as(incResponder), id, w.itemIDByKey(t, incResponder, id, "external_identity.remove|external_identity|xid_rel")); err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `UPDATE incident_items SET outcome='pending', reason='' WHERE incident_id='`+id+`' AND owner_function='account.contain' AND object_id='usr_itarget'`)
		if err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, ""); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with the containment item open = %v, want ErrReleaseNotReady", err)
		}
		execRaw(t, db, `UPDATE incident_items SET outcome='done', reason='' WHERE incident_id='`+id+`' AND owner_function='account.contain' AND object_id='usr_itarget'`)
		if err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget, ""); err != nil {
			t.Fatalf("release with the containment item done: %v", err)
		}
	})
}
