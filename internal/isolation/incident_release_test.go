package isolation

import (
	"errors"
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
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
			return w.svc.ReleasePrincipal(ctx, w.as(actor), incident, incTarget)
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
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget) }

		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release before any establishment = %v, want ErrReleaseNotReady", err)
		}
		// An authority minted after containment but never consumed is not an
		// establishment.
		execRaw(t, db, `INSERT INTO credential_authorities (id, verifier, account_id, purpose, issued_by, credential_epoch, expires_at, consumed_at, created_at) VALUES ('ca_rel_1', `+blobLit(db, []byte("ca-1"))+`, 'acc_itarget', 'establish-credential', 'credential-reset', `+acctEpoch(t, db)+`, `+farFuture+`, NULL, `+farFuture+`)`)
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with an unconsumed authority = %v, want ErrReleaseNotReady", err)
		}
		execRaw(t, db, `UPDATE credential_authorities SET consumed_at = `+farFuture+` WHERE id = 'ca_rel_1'`)
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
		if err := w.svc.ReleasePrincipal(t.Context(), w.as(incResponder), id, incTarget); !errors.Is(err, service.ErrReleaseNotReady) {
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
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget) }

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
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget) }
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
		if err := w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("an org-only holder released a two-org target = %v, want not found", err)
		}
		if s := w.containmentState(t, incTarget); s != "contained" {
			t.Fatalf("a refused release changed containment_state to %q", s)
		}
		if err := w.svc.ReleasePrincipal(ctx, w.as(incInstance), id, incTarget); err != nil {
			t.Fatalf("the instance holder's release: %v", err)
		}
	})
}

// Only a consumption after the containment is an establishment. A reset minted
// before containment and swept by it is not one, an authority consumed after
// being minted before containment is not one, and neither is a mint stamped
// after containment by a skewed clock that the containment sweep consumed.
func TestIncidentReleaseIgnoresAuthoritiesThatPredateOrWereSweptByContainment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		w := newIncidentWorld(t, db)
		auth := authService(t, db)
		execRaw(t, db, `INSERT INTO password_credentials (account_id, verifier, kdf_memory_kib, kdf_time, kdf_parallelism, dek_version, credential_epoch, row_version, updated_at) VALUES ('acc_itarget', `+blobLit(db, []byte("pw-verifier"))+`, 1, 1, 1, 1, `+acctEpoch(t, db)+`, 1, `+ts+`)`)
		if _, err := auth.ResetCredential(ctx, w.as(incResponder), string(incTarget), "response"); err != nil {
			t.Fatalf("reset before containment: %v", err)
		}
		id := w.containedIncident(t, false)
		release := func() error { return w.svc.ReleasePrincipal(ctx, w.as(incResponder), id, incTarget) }
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release after a pre-containment reset was swept = %v, want ErrReleaseNotReady", err)
		}
		// Minted before containment, consumed after it.
		execRaw(t, db, `INSERT INTO credential_authorities (id, verifier, account_id, purpose, issued_by, credential_epoch, expires_at, consumed_at, created_at) VALUES ('ca_rel_old', `+blobLit(db, []byte("ca-old"))+`, 'acc_itarget', 'establish-credential', 'credential-reset', `+acctEpoch(t, db)+`, `+farFuture+`, `+farFuture+`, `+ts+`)`)
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with an authority minted before containment = %v, want ErrReleaseNotReady", err)
		}
		// Minted after containment on a clock ahead, then swept at the containment instant.
		execRaw(t, db, `INSERT INTO credential_authorities (id, verifier, account_id, purpose, issued_by, credential_epoch, expires_at, consumed_at, created_at) VALUES ('ca_rel_skew', `+blobLit(db, []byte("ca-skew"))+`, 'acc_itarget', 'establish-credential', 'credential-reset', `+acctEpoch(t, db)+`, `+farFuture+`, (SELECT contained_at FROM principals WHERE id = 'usr_itarget'), `+farFuture+`)`)
		if err := release(); !errors.Is(err, service.ErrReleaseNotReady) {
			t.Fatalf("release with a swept skewed authority = %v, want ErrReleaseNotReady", err)
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
