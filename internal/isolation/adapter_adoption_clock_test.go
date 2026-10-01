package isolation

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func adoptionClockFixture(t *testing.T, db *store.DB) (*service.Adapters, store.AdapterTarget, service.AdoptAdapterRequest, time.Time) {
	t.Helper()
	svc, target, _, _, _, _ := newConfigurationRaceFixture(t, db)
	now := store.CanonTime(time.Now().UTC())
	scope := domain.Scope{Org: orgA, Project: prjA1}
	entries := []store.AdapterConflictEntry{{Surface: "secret", EffectiveName: "FIRST_TOKEN"}}
	if err := storetx.Write(t.Context(), db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: alice}, authz.OpAdapterPlan, scope)
		if err != nil {
			return err
		}
		return r.Adapters().RecordPlan(ctx, p, target.ID, "plan_adoption_clock", target.Generation, target.RepositoryID, target.DestinationID, entries, now)
	}); err != nil {
		t.Fatal(err)
	}
	return svc, target, service.AdoptAdapterRequest{TargetID: target.ID, ArtifactID: "plan_adoption_clock", ExpectedGeneration: target.Generation, ExpectedRepositoryID: target.RepositoryID, ExpectedDestinationID: target.DestinationID, Entries: entries}, now
}

func adoptionTemporaryReveal(t *testing.T, db *store.DB, now time.Time) {
	t.Helper()
	execRaw(t, db, `DELETE FROM grants WHERE principal_id='usr_alice' AND capability='reveal' AND org_id='org_a' AND project_id='prj_a1' AND env_id='env_a1'`)
	stamp, expiry := now.Format("2006-01-02T15:04:05.000000Z"), now.Add(time.Second).Format("2006-01-02T15:04:05.000000Z")
	execRaw(t, db, fmt.Sprintf(`INSERT INTO access_requests(id,org_id,project_id,environment_id,policy_id,policy_version,requester_principal_id,capabilities,duration_seconds,reason,state,created_at,review_expires_at,granted_at,expires_at) VALUES ('ar_adoption_clock','org_a','prj_a1','env_a1','policy_fixture',1,'usr_alice','["reveal"]',1,'adoption clock fixture','granted','%s','%s','%s','%s')`, stamp, expiry, stamp, expiry))
	execRaw(t, db, fmt.Sprintf(`INSERT INTO access_grants(id,principal_id,capability,org_id,project_id,env_id,request_id,created_at,expires_at) VALUES ('ag_adoption_clock','usr_alice','reveal','org_a','prj_a1','env_a1','ar_adoption_clock','%s','%s')`, stamp, expiry))
}

func assertAdoptionClockRollback(t *testing.T, db *store.DB, target store.AdapterTarget) {
	t.Helper()
	if got := queryInt(t, db, fmt.Sprintf(`SELECT generation FROM adapter_targets WHERE id='%s'`, target.ID)); got != target.Generation {
		t.Fatalf("expired adoption advanced generation: %d", got)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM adapter_ledger`,
		`SELECT COUNT(*) FROM adapter_conflicts WHERE adopted_at IS NOT NULL`,
		`SELECT COUNT(*) FROM audit_tenant_events WHERE type='adapter.adopt'`,
		`SELECT COUNT(*) FROM reauth_windows WHERE consumed_at IS NOT NULL`,
	} {
		if got := queryInt(t, db, query); got != 0 {
			t.Fatalf("expired adoption retained state: %s = %d", query, got)
		}
	}
}

func TestAdapterAdoptionCommitRechecksTemporaryRevealBothEngines(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc, target, request, now := adoptionClockFixture(t, db)
		adoptionTemporaryReveal(t, db, now)
		var calls atomic.Int32
		svc.Now = func() time.Time {
			if calls.Add(1) == 1 {
				return now
			}
			return now.Add(2 * time.Second)
		}
		if _, err := svc.Adopt(t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, request); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expired temporary authority committed adoption: %v", err)
		}
		assertAdoptionClockRollback(t, db, target)
	})
}

func TestAdapterPostgresAdoptionRechecksSessionAfterTargetLock(t *testing.T) {
	db := seededDB(t, openPostgres)
	svc, target, request, now := adoptionClockFixture(t, db)
	bearer := seedSessionFactors(t, db, alice, `["password","totp"]`)
	sessionID := queryString(t, db, `SELECT id FROM sessions WHERE principal_id='usr_alice' ORDER BY created_at DESC LIMIT 1`)
	svc.Auth = authService(t, db)
	svc.Auth.ReauthWindow = 5 * time.Minute
	if err := storetx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		return az.OpenReauthWindow(ctx, authz.NewReauthWindow{
			ID: "rw_adoption_clock", SessionID: sessionID, EnvironmentID: string(envA1), CeremonyID: "verified_adoption_clock", FactorClass: "totp",
			AuthenticatedAt: now, WindowExpiresAt: now.Add(5 * time.Minute), HardExpiresAt: now.Add(10 * time.Minute), CredentialEpoch: epoch, CreatedAt: now,
			BoundPurpose: string(service.PurposeAdapter), BoundOperation: string(authz.OpAdapterAdopt), BoundEnvironmentSet: service.CanonicalEnvironmentSet([]string{string(envA1)}),
		})
	}); err != nil {
		t.Fatal(err)
	}
	var offset atomic.Int64
	svc.Now = func() time.Time { return now.Add(time.Duration(offset.Load())) }
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	held, err := db.PG().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(t.Context())
	if _, err := held.Exec(ctx, `SELECT id FROM adapter_targets WHERE id=$1 FOR UPDATE`, target.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.Adopt(ctx, service.Bearer(bearer), domain.Scope{Org: orgA, Project: prjA1}, request)
		done <- err
	}()
	for queryInt(t, db, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%adapter_targets%'`) == 0 {
		select {
		case err := <-done:
			t.Fatalf("adoption did not reach target lock: %v", err)
		case <-ctx.Done():
			t.Fatal("adoption never waited for target lock")
		case <-time.After(5 * time.Millisecond):
		}
	}
	offset.Store(int64(2 * time.Hour))
	// Release without changing any row, so no serialization retry can
	// incidentally refresh authority on the old implementation.
	if err := held.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("expired session committed adoption after target lock: %v", err)
	}
	assertAdoptionClockRollback(t, db, target)
}
