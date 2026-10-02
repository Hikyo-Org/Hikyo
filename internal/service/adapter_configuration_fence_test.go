package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

type adapterPausedConnectionModule struct {
	started    chan<- struct{}
	resume     <-chan struct{}
	connection adapter.Connection
}

func (adapterPausedConnectionModule) ValidateConfig(adapter.Config) error { return nil }
func (m adapterPausedConnectionModule) TestConnection(ctx context.Context, r adapter.ConnectionRequest) (adapter.Connection, error) {
	if err := r.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	if m.started != nil {
		close(m.started)
		select {
		case <-m.resume:
		case <-ctx.Done():
			return adapter.Connection{}, ctx.Err()
		}
	}
	return m.connection, nil
}
func (adapterPausedConnectionModule) Plan(context.Context, adapter.PlanRequest) (adapter.Plan, error) {
	return adapter.Plan{}, nil
}
func (adapterPausedConnectionModule) Sync(context.Context, adapter.SyncRequest, adapter.Journal) (adapter.SyncResult, error) {
	return adapter.SyncResult{}, nil
}

func TestAdapterAddTargetRefusesObsoleteProviderVerification(t *testing.T) {
	for _, originMove := range []bool{false, true} {
		t.Run(fmt.Sprintf("origin-move=%v", originMove), func(t *testing.T) {
			svc, bearer, input, now, _ := adapterClockFixture(t)
			if _, err := svc.DB.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO adapter_target_keys(org_id,project_id,environment_id,target_id,adapter_id,key_id) SELECT org_id,project_id,environment_id,id,adapter_id,'key_clock' FROM adapter_targets`); err != nil {
				t.Fatal(err)
			}
			adapterClockWindows(t, svc.DB, authz.OpAdapterConfigure, []string{"env_one", "env_two"}, now, now.Add(time.Minute))
			started, resume := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-resume:
				default:
					close(resume)
				}
			}()
			oldExpiry, newExpiry := now.Add(24*time.Hour), now.Add(48*time.Hour)
			svc.ModuleFactory = providerBlindTestModuleFactory(func(_ string, credential string) (adapter.Module, func(), error) {
				if credential == "provider-token" {
					return adapterPausedConnectionModule{started: started, resume: resume, connection: adapter.Connection{DestinationID: 77, CredentialExpiresAt: oldExpiry}}, nil, nil
				}
				return adapterPausedConnectionModule{connection: adapter.Connection{DestinationID: 999, CredentialExpiresAt: newExpiry}}, nil, nil
			})
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := svc.AddTarget(ctx, Bearer(bearer), adapterScope, "adp_1", input)
				done <- err
			}()
			select {
			case <-started:
			case err := <-done:
				t.Fatalf("addition did not reach provider verification: %v", err)
			case <-ctx.Done():
				t.Fatal("addition did not reach provider verification")
			}
			wantOrigin := "https://git.example"
			if originMove {
				wantOrigin = "https://git.next.example"
				move, err := svc.MoveOrigin(ctx, LocalPrincipal("usr_adapter"), adapterScope, "adp_1", wantOrigin, []byte("replacement-token"), true)
				if err != nil {
					t.Fatal(err)
				}
				runtime := store.NewAdapterRuntime(svc.DB, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
				for i := range move.Targets {
					at := now.Add(time.Duration(i+1) * time.Second)
					job, ok, err := runtime.ClaimDue(ctx, "configuration-fence", at, at.Add(adapter.LeaseTime))
					if err != nil || !ok || job.Kind != adapter.Activate || job.RouteMoveID != move.MoveID {
						t.Fatalf("claim actual origin activation: %+v %v %v", job, ok, err)
					}
					if err := runtime.Activate(ctx, job, adapter.Connection{Version: "configuration-fence", DestinationID: int64(101 + i), CredentialExpiresAt: newExpiry}, at); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				if _, err := svc.ReplaceCredential(ctx, LocalPrincipal("usr_adapter"), adapterScope, "adp_1", []byte("replacement-token")); err != nil {
					t.Fatal(err)
				}
				if _, err := svc.TestTarget(ctx, LocalPrincipal("usr_adapter"), adapterScope, "tgt_one"); err != nil {
					t.Fatal(err)
				}
			}
			close(resume)
			if err := <-done; !errors.Is(err, store.ErrConflict) {
				t.Fatalf("obsolete provider verification committed: %v", err)
			}
			var targets int
			var origin, expiry, state string
			if err := svc.DB.SQLiteRead().QueryRowContext(ctx, `SELECT COUNT(*) FROM adapter_targets`).Scan(&targets); err != nil || targets != 2 {
				t.Fatalf("stale verification changed targets: %d %v", targets, err)
			}
			if err := svc.DB.SQLiteRead().QueryRowContext(ctx, `SELECT origin,credential_expires_at,state FROM adapters WHERE id='adp_1'`).Scan(&origin, &expiry, &state); err != nil {
				t.Fatal(err)
			}
			if parsed, err := time.Parse(time.RFC3339Nano, expiry); err != nil || !parsed.Equal(store.CanonTime(newExpiry)) || origin != wantOrigin || state != "active" {
				t.Fatalf("stale verification changed replacement configuration: origin=%q expiry=%q state=%q %v", origin, expiry, state, err)
			}
			// A new explicit attempt verifies the replacement configuration and
			// uses its destination identity rather than the stale result.
			fresh, err := svc.AddTarget(ctx, LocalPrincipal("usr_adapter"), adapterScope, "adp_1", input)
			if err != nil || fresh.DestinationID != 999 || fresh.Origin != wantOrigin {
				t.Fatalf("fresh provider verification failed: %+v %v", fresh, err)
			}
		})
	}
}
