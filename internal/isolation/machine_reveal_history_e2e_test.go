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

func TestWorkloadRevealHistoryPinSQLite(t *testing.T) {
	runWorkloadRevealHistoryPin(t, seededDB(t, openSQLite))
}

func TestWorkloadRevealHistoryPinPostgres(t *testing.T) {
	runWorkloadRevealHistoryPin(t, seededDB(t, openPostgres))
}

// runWorkloadRevealHistoryPin exercises the grant and delivery seams together:
// the grant exists only while an active pin routes historical material, then
// stays stored but becomes inert when releasing the pin moves the cursor.
func runWorkloadRevealHistoryPin(t *testing.T, db *store.DB) {
	identityFixtures(t, db)
	seedDeliveryCatalogue(t, db) // revision 1
	ctx := t.Context()
	env := scopeEnv(orgA, prjA1, envA1)
	identities := identitySvc(db)
	grants := grantSvcWithAuth(db)
	pins := &service.Pins{DB: db, Keyring: probeKeyring(t, db)}

	// Both disclosure atoms share the project opt-in. The pin authority also
	// holds the rights Pins.Set and delivery's recorded-authority recheck need.
	execRaw(t, db, `UPDATE projects SET machine_reveal = TRUE WHERE id = 'prj_a1'`)
	for _, stmt := range []string{
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_rh_pin', 'usr_ident', 'pin', 'org_a', 'prj_a1', 'env_a1', ` + ts + `)`,
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_rh_pin_authority', 'usr_ident', 'reveal-history', 'org_a', 'prj_a1', 'env_a1', ` + ts + `)`,
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_rh_settings', 'usr_idrev', 'project-settings', 'org_a', 'prj_a1', NULL, ` + ts + `)`,
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_rh_project_reveal', 'usr_idrev', 'reveal', 'org_a', 'prj_a1', NULL, ` + ts + `)`,
		// usr_rvonly can grant an unheld atom but still lacks the disclosure
		// authority needed by the stricter machine-widening formula.
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_rh_org_mm', 'usr_rvonly', 'manage-members', 'org_a', NULL, NULL, ` + ts + `)`,
	} {
		execRaw(t, db, stmt)
	}
	seedOrigins(t, db)

	workload, err := identities.CreateServiceAccount(ctx, service.LocalPrincipal(identAdmin),
		prjScope(), "pinned-historian", domain.ClassWorkload)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := identities.MintCredential(ctx, service.LocalPrincipal(identAdmin),
		prjScope(), workload.ID, service.MintRequest{})
	if err != nil {
		t.Fatal(err)
	}
	grantMachineRead(t, db, workload.Principal, envA1)
	spec := service.GrantSpec{
		Target: workload.Principal, Capability: domain.CapRevealHistory, Scope: envScope(envA1),
	}

	// Opt-in on is not enough: absent, current, and expired pins do not require
	// historical delivery and therefore cannot justify a reveal-history grant.
	if _, err := grants.Create(ctx, service.LocalPrincipal(identRevatr), spec); !errors.Is(err, service.ErrMachineRevealHistoryPin) {
		t.Fatalf("grant without a pin: got %v, want ErrMachineRevealHistoryPin", err)
	} else if !strings.Contains(err.Error(), string(envA1)) {
		t.Fatalf("pin refusal does not name %s: %v", envA1, err)
	}
	if _, err := pins.Set(ctx, service.LocalPrincipal(identAdmin), env,
		service.SetPinRequest{WorkloadPrincipalID: workload.Principal, Revision: 1}); err != nil {
		t.Fatalf("pin current revision: %v", err)
	}
	if _, err := grants.Create(ctx, service.LocalPrincipal(identRevatr), spec); !errors.Is(err, service.ErrMachineRevealHistoryPin) {
		t.Fatalf("grant with a current pin: got %v, want ErrMachineRevealHistoryPin", err)
	}
	execRaw(t, db, `UPDATE revision_pins SET expires_at = '2000-01-01T00:00:00.000000Z' WHERE workload_principal_id = '`+string(workload.Principal)+`'`)
	if _, err := grants.Create(ctx, service.LocalPrincipal(identRevatr), spec); !errors.Is(err, service.ErrMachineRevealHistoryPin) {
		t.Fatalf("grant with an expired pin: got %v, want ErrMachineRevealHistoryPin", err)
	}

	// Restore liveness, then overtake revision 1. The stored current pin now
	// requires reveal-history without being rewritten.
	execRaw(t, db, `UPDATE revision_pins SET expires_at = '2999-01-01T00:00:00.000000Z' WHERE workload_principal_id = '`+string(workload.Principal)+`'`)
	publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": "postgres://dev-v2"})
	if latest := latestRevision(t, db, string(envA1)); latest != 2 {
		t.Fatalf("latest revision = %d, want 2", latest)
	}

	// Model a grant waiting on the target-principal lock until the pin expires:
	// the first clock read supplies the write timestamp, while the pin check
	// must take a fresh reading after the lock instead of reusing it.
	clockReads := 0
	grantsAfterWait := grantSvcWithAuth(db)
	grantsAfterWait.Now = func() time.Time {
		clockReads++
		if clockReads == 1 {
			return time.Date(2998, 12, 31, 23, 59, 59, 0, time.UTC)
		}
		return time.Date(2999, 1, 1, 0, 0, 1, 0, time.UTC)
	}
	if _, err := grantsAfterWait.Create(ctx, service.LocalPrincipal(identRevatr), spec); !errors.Is(err, service.ErrMachineRevealHistoryPin) {
		t.Fatalf("grant whose pin expires while awaiting its lock: got %v, want ErrMachineRevealHistoryPin", err)
	}

	// The pin-specific admission does not weaken the existing widening gate.
	if _, err := grants.Create(ctx, service.LocalPrincipal(revealOnly), spec); !errors.Is(err, service.ErrDisclosureAuthority) {
		t.Fatalf("grant without actor reveal-history: got %v, want ErrDisclosureAuthority", err)
	}
	if _, err := grants.Create(ctx, service.LocalPrincipal(identRevatr), spec); !errors.Is(err, service.ErrReauthRequired) {
		t.Fatalf("grant without a live mint window: got %v, want ErrReauthRequired", err)
	}
	actor := service.Bearer(sessionWithWindows(t, db, identRevatr, envA1))
	if _, err := grants.Create(ctx, actor, spec); err != nil {
		t.Fatalf("grant under active historical pin: %v", err)
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'grant.created' AND payload LIKE '%reveal-history%'`); n != 1 {
		t.Fatalf("grant.created audit rows = %d, want 1", n)
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'identity.grant_widened' AND payload LIKE '%reveal-history%'`); n != 1 {
		t.Fatalf("identity.grant_widened audit rows = %d, want 1", n)
	}

	delivery := deliverySvc(t, db)
	beforeRelease, err := delivery.Fetch(ctx, credential.Value, env, "", service.FetchOptions{})
	if err != nil {
		t.Fatalf("fetch through historical pin: %v", err)
	}
	if value := deliveredByName(beforeRelease.Keys)["DATABASE_PASSWORD"].Value; value == nil || *value != "dev-secret" {
		t.Fatalf("historical secret = %v, want revision-1 plaintext", value)
	}

	// The opt-in remains a live conjunct after the grant. Withdrawing it strips
	// reveal-history and moves the cursor; re-enabling restores pinned delivery.
	settings := settingsSvc(t, db)
	operator := service.LocalPrincipal(identRevatr)
	if _, err := settings.SetMachineReveal(ctx, operator, prjScope(), false); err != nil {
		t.Fatalf("withdraw machine reveal: %v", err)
	}
	withoutOptIn, err := delivery.Fetch(ctx, credential.Value, env, beforeRelease.Cursor, service.FetchOptions{})
	if err != nil {
		t.Fatalf("fetch after opt-in withdrawal: %v", err)
	}
	if withoutOptIn.Current {
		t.Fatal("opt-in withdrawal left the historical cursor current")
	}
	if value := deliveredByName(withoutOptIn.Keys)["DATABASE_PASSWORD"].Value; value != nil {
		t.Fatal("reveal-history disclosed while the machine-reveal opt-in was off")
	}
	if _, err := settings.SetMachineReveal(ctx, operator, prjScope(), true); err != nil {
		t.Fatalf("restore machine reveal: %v", err)
	}
	restored, err := delivery.Fetch(ctx, credential.Value, env, withoutOptIn.Cursor, service.FetchOptions{})
	if err != nil {
		t.Fatalf("fetch after opt-in restore: %v", err)
	}
	if restored.Current {
		t.Fatal("opt-in restore left the presence-only cursor current")
	}
	if value := deliveredByName(restored.Keys)["DATABASE_PASSWORD"].Value; value == nil || *value != "dev-secret" {
		t.Fatalf("restored historical secret = %v, want revision-1 plaintext", value)
	}

	if _, err := pins.Release(ctx, service.LocalPrincipal(identAdmin), env, workload.Principal); err != nil {
		t.Fatalf("release pin: %v", err)
	}
	if !held(t, db, workload.Principal, domain.CapRevealHistory, envScope(envA1)) {
		t.Fatal("pin release deleted the reveal-history grant")
	}
	afterRelease, err := delivery.Fetch(ctx, credential.Value, env, restored.Cursor, service.FetchOptions{})
	if err != nil {
		t.Fatalf("fetch after pin release: %v", err)
	}
	if afterRelease.Current {
		t.Fatal("pin release left the historical cursor current")
	}
	if value := deliveredByName(afterRelease.Keys)["DATABASE_PASSWORD"].Value; value != nil {
		t.Fatal("stale reveal-history grant disclosed current secret after pin release")
	}
	repeat, err := delivery.Fetch(ctx, credential.Value, env, afterRelease.Cursor, service.FetchOptions{})
	if err != nil {
		t.Fatalf("repeat fetch after pin release: %v", err)
	}
	if !repeat.Current {
		t.Fatal("pin release moved the cursor more than once")
	}

	// Automation cannot satisfy a workload-only pin condition.
	automation, err := identities.CreateServiceAccount(ctx, service.LocalPrincipal(identAdmin),
		prjScope(), "automation-historian", domain.ClassAutomation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := grants.Create(ctx, service.LocalPrincipal(identRevatr), service.GrantSpec{
		Target: automation.Principal, Capability: domain.CapRevealHistory, Scope: envScope(envA1),
	}); !errors.Is(err, service.ErrMachineCapability) {
		t.Fatalf("automation reveal-history: got %v, want ErrMachineCapability", err)
	}
}
