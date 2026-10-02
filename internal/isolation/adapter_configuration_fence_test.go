package isolation

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type configurationRaceModule struct {
	credential string
	started    chan<- struct{}
	resume     <-chan struct{}
	oldExpiry  time.Time
	newExpiry  time.Time
}

func (configurationRaceModule) ValidateConfig(adapter.Config) error { return nil }
func (m configurationRaceModule) TestConnection(ctx context.Context, r adapter.ConnectionRequest) (adapter.Connection, error) {
	if err := r.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	if m.credential == "old-token" {
		if r.Destination.Name == "new" {
			close(m.started)
			select {
			case <-m.resume:
			case <-ctx.Done():
				return adapter.Connection{}, ctx.Err()
			}
		}
		return adapter.Connection{Version: "configuration-race", DestinationID: 42, CredentialExpiresAt: m.oldExpiry}, nil
	}
	return adapter.Connection{Version: "configuration-race", DestinationID: 99, CredentialExpiresAt: m.newExpiry}, nil
}
func (configurationRaceModule) Plan(context.Context, adapter.PlanRequest) (adapter.Plan, error) {
	return adapter.Plan{}, nil
}
func (configurationRaceModule) Sync(context.Context, adapter.SyncRequest, adapter.Journal) (adapter.SyncResult, error) {
	return adapter.SyncResult{}, nil
}

func newConfigurationRaceFixture(t *testing.T, db *store.DB) (*service.Adapters, store.AdapterTarget, service.AdapterTargetInput, chan struct{}, chan struct{}, time.Time) {
	t.Helper()
	execRaw(t, db, `INSERT INTO grants(id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('config_manage','usr_alice','manage-adapters','org_a','prj_a1',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants(id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('config_reveal','usr_alice','reveal','org_a','prj_a1','env_a1',`+ts+`)`)
	started, resume := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
	})
	now := store.CanonTime(time.Now().UTC())
	newExpiry := now.Add(48 * time.Hour)
	svc := &service.Adapters{DB: db, Keyring: probeKeyring(t, db), ModuleFactory: func(_ adapter.Provider, _ adapter.Config, credential string) (*adapter.ModuleLease, error) {
		return adapter.NewModuleLease(configurationRaceModule{credential: credential, started: started, resume: resume, oldExpiry: now.Add(24 * time.Hour), newExpiry: newExpiry}, func() {})
	}}
	input := service.AdapterTargetInput{EnvironmentID: string(envA1), DestinationKind: string(adapter.Repository), DestinationOwner: "acme", DestinationName: "app", NamePrefix: "FIRST_", KeyIDs: []string{keyA1}}
	created, err := svc.Create(t.Context(), service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, service.CreateAdapterRequest{Provider: string(adapter.ForgejoProvider), Origin: "https://git.example", Credential: []byte("old-token"), Target: input})
	if err != nil {
		t.Fatal(err)
	}
	input.DestinationName, input.NamePrefix = "new", "SECOND_"
	return svc, created.Targets[0], input, started, resume, newExpiry
}

func TestAdapterAdditionFencesActualOriginAndCredentialChanges(t *testing.T) {
	for _, originMove := range []bool{false, true} {
		t.Run(fmt.Sprintf("origin-move=%v", originMove), func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				svc, target, input, started, resume, newExpiry := newConfigurationRaceFixture(t, db)
				scope := domain.Scope{Org: orgA, Project: prjA1}
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					_, err := svc.AddTarget(ctx, service.LocalPrincipal(alice), scope, target.AdapterID, input)
					done <- err
				}()
				select {
				case <-started:
				case err := <-done:
					t.Fatalf("addition did not pause provider verification: %v", err)
				case <-ctx.Done():
					t.Fatal("addition never reached provider")
				}
				wantOrigin := "https://git.example"
				if originMove {
					wantOrigin = "https://git.next.example"
					move, err := svc.MoveOrigin(ctx, service.LocalPrincipal(alice), scope, target.AdapterID, wantOrigin, []byte("replacement-token"), true)
					if err != nil {
						t.Fatal(err)
					}
					runtime := generatedAdapterRuntime(db)
					at := time.Now().UTC().Add(time.Second)
					job, ok, err := runtime.ClaimDue(ctx, "configuration-race", at, at.Add(adapter.LeaseTime))
					if err != nil || !ok || job.Kind != adapter.Activate || job.RouteMoveID != move.MoveID {
						t.Fatalf("claim actual origin activation: %+v %v %v", job, ok, err)
					}
					if err := runtime.Activate(ctx, job, adapter.Connection{Version: "configuration-race", DestinationID: 99, CredentialExpiresAt: newExpiry}, at); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := svc.ReplaceCredential(ctx, service.LocalPrincipal(alice), scope, target.AdapterID, []byte("replacement-token")); err != nil {
						t.Fatal(err)
					}
					if _, err := svc.TestTarget(ctx, service.LocalPrincipal(alice), scope, target.ID); err != nil {
						t.Fatal(err)
					}
				}
				close(resume)
				if err := <-done; !errors.Is(err, domain.ErrConflict) {
					t.Fatalf("stale addition accepted after configuration changed: %v", err)
				}
				if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets`); got != 1 {
					t.Fatalf("stale addition wrote target: %d", got)
				}
				if got := queryString(t, db, fmt.Sprintf(`SELECT origin FROM adapters WHERE id='%s'`, target.AdapterID)); got != wantOrigin {
					t.Fatalf("stale addition changed origin: %s", got)
				}
				view, err := svc.Get(ctx, service.LocalPrincipal(alice), scope, target.AdapterID)
				if err != nil {
					t.Fatal(err)
				}
				storedExpiry := view.Adapter.CredentialExpiresAt
				if parsed, err := time.Parse(time.RFC3339Nano, storedExpiry); err != nil || !parsed.Equal(newExpiry) {
					t.Fatalf("stale addition overwrote replacement expiry: %s %v", storedExpiry, err)
				}
				fresh, err := svc.AddTarget(ctx, service.LocalPrincipal(alice), scope, target.AdapterID, input)
				if err != nil || fresh.DestinationID != 99 || fresh.Origin != wantOrigin {
					t.Fatalf("fresh verification cannot add target: %+v %v", fresh, err)
				}
			})
		})
	}
}

func TestAdapterPostgresAdditionRechecksAfterParentLockWait(t *testing.T) {
	db := seededDB(t, openPostgres)
	svc, target, input, started, resume, _ := newConfigurationRaceFixture(t, db)
	scope := domain.Scope{Org: orgA, Project: prjA1}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	addition := make(chan error, 1)
	go func() {
		_, err := svc.AddTarget(ctx, service.LocalPrincipal(alice), scope, target.AdapterID, input)
		addition <- err
	}()
	select {
	case <-started:
	case err := <-addition:
		t.Fatalf("addition did not reach provider: %v", err)
	case <-ctx.Done():
		t.Fatal("addition did not reach provider")
	}
	held, err := db.PG().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(t.Context())
	if _, err := held.Exec(ctx, `SELECT id FROM adapters WHERE id=$1 FOR UPDATE`, target.AdapterID); err != nil {
		t.Fatal(err)
	}
	credential := make(chan error, 1)
	go func() {
		_, err := svc.ReplaceCredential(ctx, service.LocalPrincipal(alice), scope, target.AdapterID, []byte("replacement-token"))
		credential <- err
	}()
	waitForLocks := func(want int64) {
		t.Helper()
		until := time.Now().Add(3 * time.Second)
		for queryInt(t, db, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%adapters%'`) < want {
			if time.Now().After(until) {
				t.Fatalf("canonical transactions did not reach parent lock: want %d", want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitForLocks(1)
	close(resume)
	waitForLocks(2)
	if err := held.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-credential; err != nil {
		t.Fatalf("actual queued credential replacement failed: %v", err)
	}
	if err := <-addition; !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("addition reused pre-lock configuration after retry: %v", err)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets`); got != 1 {
		t.Fatalf("blocked stale addition wrote target: %d", got)
	}
	if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapters WHERE id='%s' AND credential_expires_at IS NULL`, target.AdapterID)); got != 1 {
		t.Fatal("stale addition filled replacement credential expiry")
	}
}

func TestAdapterPostgresAdditionRechecksAuthorityAfterParentLockWait(t *testing.T) {
	for _, sessionExpiry := range []bool{false, true} {
		t.Run(fmt.Sprintf("session-expiry=%v", sessionExpiry), func(t *testing.T) {
			db := seededDB(t, openPostgres)
			svc, target, input, started, resume, _ := newConfigurationRaceFixture(t, db)
			scope := domain.Scope{Org: orgA, Project: prjA1}
			actor := service.LocalPrincipal(alice)
			var sessionID string
			if sessionExpiry {
				actor = service.Bearer(seedSessionFactors(t, db, alice, `["password","totp"]`))
				sessionID = queryString(t, db, `SELECT id FROM sessions WHERE principal_id='usr_alice' ORDER BY created_at DESC LIMIT 1`)
				svc.Auth = authService(t, db)
				svc.Auth.ReauthWindow = 5 * time.Minute
			}
			now := store.CanonTime(time.Now().UTC())
			offset := new(atomic.Int64)
			svc.Now = func() time.Time { return now.Add(time.Duration(offset.Load())) }
			if sessionExpiry {
				if err := storetx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
					epoch, err := az.CredentialEpoch(ctx)
					if err != nil {
						return err
					}
					return az.OpenReauthWindow(ctx, authz.NewReauthWindow{
						ID: "rw_configuration_lock", SessionID: sessionID, EnvironmentID: string(envA1), CeremonyID: "verified_configuration_lock", FactorClass: "totp",
						AuthenticatedAt: now, WindowExpiresAt: now.Add(5 * time.Minute), HardExpiresAt: now.Add(10 * time.Minute), CredentialEpoch: epoch, CreatedAt: now,
						BoundPurpose: string(service.PurposeAdapter), BoundOperation: string(authz.OpAdapterConfigure), BoundEnvironmentSet: service.CanonicalEnvironmentSet([]string{string(envA1)}),
					})
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				execRaw(t, db, `DELETE FROM grants WHERE principal_id='usr_alice' AND capability='reveal' AND org_id='org_a' AND project_id='prj_a1' AND env_id='env_a1'`)
				stamp, expiry := now.Format("2006-01-02T15:04:05.000000Z"), now.Add(time.Second).Format("2006-01-02T15:04:05.000000Z")
				execRaw(t, db, fmt.Sprintf(`INSERT INTO access_requests(id,org_id,project_id,environment_id,policy_id,policy_version,requester_principal_id,capabilities,duration_seconds,reason,state,created_at,review_expires_at,granted_at,expires_at) VALUES ('ar_configuration_lock','org_a','prj_a1','env_a1','policy_fixture',1,'usr_alice','["reveal"]',1,'lock clock fixture','granted','%s','%s','%s','%s')`, stamp, expiry, stamp, expiry))
				execRaw(t, db, fmt.Sprintf(`INSERT INTO access_grants(id,principal_id,capability,org_id,project_id,env_id,request_id,created_at,expires_at) VALUES ('ag_configuration_lock','usr_alice','reveal','org_a','prj_a1','env_a1','ar_configuration_lock','%s','%s')`, stamp, expiry))
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := svc.AddTarget(ctx, actor, scope, target.AdapterID, input)
				done <- err
			}()
			select {
			case <-started:
			case err := <-done:
				t.Fatalf("live authority did not reach provider: %v", err)
			case <-ctx.Done():
				t.Fatal("addition did not reach provider")
			}
			held, err := db.PG().Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Rollback(t.Context())
			if _, err := held.Exec(ctx, `SELECT id FROM adapters WHERE id=$1 FOR UPDATE`, target.AdapterID); err != nil {
				t.Fatal(err)
			}
			before, err := svc.Get(ctx, service.LocalPrincipal(alice), scope, target.AdapterID)
			if err != nil {
				t.Fatal(err)
			}
			close(resume)
			until := time.Now().Add(3 * time.Second)
			for queryInt(t, db, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%adapters%'`) == 0 {
				if time.Now().After(until) {
					t.Fatal("addition did not wait on actual parent lock")
				}
				time.Sleep(5 * time.Millisecond)
			}
			advance, want := 2*time.Second, domain.ErrNotFound
			if sessionExpiry {
				advance, want = 2*time.Hour, domain.ErrUnauthenticated
			}
			offset.Store(int64(advance))
			// No configuration mutation: rollback releases the lock without a
			// serialization conflict that could incidentally refresh authority.
			if err := held.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, want) {
				t.Fatalf("expired authority admitted after parent lock: %v want %v", err, want)
			}
			if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets`); got != 1 {
				t.Fatalf("expired authority changed target count: %d", got)
			}
			after, err := svc.Get(ctx, service.LocalPrincipal(alice), scope, target.AdapterID)
			if err != nil || after.Adapter.CredentialExpiresAt != before.Adapter.CredentialExpiresAt {
				t.Fatalf("expired authority changed credential expiry: %q %v", after.Adapter.CredentialExpiresAt, err)
			}
		})
	}
}
