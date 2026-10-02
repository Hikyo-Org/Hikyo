package service

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
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type adapterResponseClockModule struct{ respond func() }

func (adapterResponseClockModule) ValidateConfig(adapter.Config) error { return nil }
func (m adapterResponseClockModule) TestConnection(ctx context.Context, r adapter.ConnectionRequest) (adapter.Connection, error) {
	if err := r.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	// The request was authorized before dispatch; its response arrives later.
	// No extra provider gate is promised after that response.
	m.respond()
	return adapter.Connection{Version: "clock-fixture", DestinationID: 77}, nil
}
func (m adapterResponseClockModule) Plan(ctx context.Context, r adapter.PlanRequest) (adapter.Plan, error) {
	if err := r.Gate(ctx); err != nil {
		return adapter.Plan{}, err
	}
	m.respond()
	return adapter.Plan{}, nil
}
func (adapterResponseClockModule) Sync(context.Context, adapter.SyncRequest, adapter.Journal) (adapter.SyncResult, error) {
	return adapter.SyncResult{}, nil
}

func adapterClockWindows(t *testing.T, db *store.DB, operation authz.Operation, environments []string, now, expiry time.Time) {
	t.Helper()
	intent, err := NewAdapterReauthIntent(string(operation), environments)
	if err != nil {
		t.Fatal(err)
	}
	if err := storetx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		for _, env := range environments {
			binding, err := intent.bindingFor(env)
			if err != nil {
				return err
			}
			ceremonyID := "wac_clock_" + env
			if err := az.CreateWebAuthnCeremony(ctx, authz.NewWebAuthnCeremony{
				ID: ceremonyID, ChallengeVerifier: []byte("verified-clock-fixture-" + env), SessionData: []byte("{}"),
				SessionID: "ses_adapter", Purpose: "reauth", OperationBinding: binding.challengeBinding,
				EnvironmentID: env, CredentialEpoch: 1, ExpiresAt: expiry, CreatedAt: now,
			}); err != nil {
				return err
			}
			if err := az.OpenReauthWindow(ctx, authz.NewReauthWindow{
				ID: "rw_clock_" + env, SessionID: "ses_adapter", EnvironmentID: env, CeremonyID: ceremonyID,
				FactorClass: "webauthn", SingleDecision: true, AuthenticatedAt: now,
				WindowExpiresAt: expiry, HardExpiresAt: expiry, CredentialEpoch: 1, CreatedAt: now,
				BoundPurpose: string(binding.purpose), BoundOperation: string(binding.operation), BoundEnvironmentSet: binding.environmentSet,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func adapterClockFixture(t *testing.T) (*Adapters, string, AdapterTargetInput, time.Time, *atomic.Int64) {
	t.Helper()
	db := adapterServiceDB(t)
	seedKey(t, db, "key_clock", "API_TOKEN")
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO grants(id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_clock_reveal_two','usr_adapter','reveal','org_adapter','prj_adapter','env_two','2026-08-17T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	kr := adapterKeyring(t, db)
	sealer, err := kr.ForProject(t.Context(), "org_adapter", "prj_adapter")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `UPDATE adapters SET credential_ciphertext=? WHERE id='adp_1'`, sealAdapterCredential(t, sealer, "provider-token")); err != nil {
		t.Fatal(err)
	}
	bearer := adapterCLISession(t, db)
	now := store.CanonTime(time.Now().UTC())
	offset := new(atomic.Int64)
	svc := &Adapters{DB: db, Keyring: kr, Auth: &Auth{DB: db, ReauthWindow: time.Minute}, Now: func() time.Time { return now.Add(time.Duration(offset.Load())) }}
	input := AdapterTargetInput{EnvironmentID: "env_one", DestinationKind: "repository", DestinationOwner: "acme", DestinationName: "new", NamePrefix: "CLOCK_", KeyIDs: []string{"key_clock"}}
	return svc, bearer, input, now, offset
}

func TestAdapterConfigureFinalWriteRechecksAfterProviderResponse(t *testing.T) {
	for _, add := range []bool{false, true} {
		for _, expireSession := range []bool{false, true} {
			t.Run(fmt.Sprintf("add=%v/expired-session=%v", add, expireSession), func(t *testing.T) {
				svc, bearer, input, now, offset := adapterClockFixture(t)
				environments := []string{"env_one"}
				if add {
					environments = append(environments, "env_two")
				}
				adapterClockWindows(t, svc.DB, authz.OpAdapterConfigure, environments, now, now.Add(time.Second))
				advance := 2 * time.Second
				if expireSession {
					advance = 2 * time.Hour
				}
				svc.ModuleFactory = providerBlindTestModuleFactory(func(string, string) (adapter.Module, func(), error) {
					return adapterResponseClockModule{respond: func() { offset.Store(int64(advance)) }}, nil, nil
				})
				var err error
				if add {
					_, err = svc.AddTarget(t.Context(), Bearer(bearer), adapterScope, "adp_1", input)
				} else {
					_, err = svc.Create(t.Context(), Bearer(bearer), adapterScope, CreateAdapterRequest{Origin: "https://clock.example", Credential: []byte("provider-token"), Target: input})
				}
				if expireSession && !errors.Is(err, domain.ErrUnauthenticated) {
					t.Fatalf("expired session admitted final provider-backed write: %v", err)
				}
				if !expireSession && err != nil {
					t.Fatalf("valid staged consent was consumed again after response: %v", err)
				}
				var targets, consumed int
				if err := svc.DB.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM adapter_targets`).Scan(&targets); err != nil {
					t.Fatal(err)
				}
				wantTargets := 3
				if expireSession {
					wantTargets = 2
				}
				if targets != wantTargets {
					t.Fatalf("final targets=%d want %d", targets, wantTargets)
				}
				if err := svc.DB.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM reauth_windows WHERE consumed_at IS NOT NULL`).Scan(&consumed); err != nil {
					t.Fatal(err)
				}
				if consumed != len(environments) {
					t.Fatalf("staged consent consumption=%d want %d", consumed, len(environments))
				}
			})
		}
	}
}

func TestAdapterCreateCeremonyRechecksAfterWriterAcquisition(t *testing.T) {
	for _, expireSession := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired-session=%v", expireSession), func(t *testing.T) {
			svc, bearer, input, now, offset := adapterClockFixture(t)
			adapterClockWindows(t, svc.DB, authz.OpAdapterConfigure, []string{"env_one"}, now, now.Add(time.Second))
			var providerCalls atomic.Int32
			svc.ModuleFactory = providerBlindTestModuleFactory(func(string, string) (adapter.Module, func(), error) {
				return adapterResponseClockModule{respond: func() { providerCalls.Add(1) }}, nil, nil
			})
			hold, err := svc.DB.SQLiteWrite().BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer hold.Rollback()
			waitBefore := svc.DB.SQLiteWrite().Stats().WaitCount
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := svc.Create(ctx, Bearer(bearer), adapterScope, CreateAdapterRequest{Origin: "https://clock.example", Credential: []byte("provider-token"), Target: input})
				done <- err
			}()
			for svc.DB.SQLiteWrite().Stats().WaitCount == waitBefore {
				select {
				case err := <-done:
					t.Fatalf("create never waited for writer acquisition: %v", err)
				case <-ctx.Done():
					t.Fatal("writer acquisition was not reached")
				case <-time.After(time.Millisecond):
				}
			}
			advance := 2 * time.Second
			want := ErrReauthRequired
			if expireSession {
				advance, want = 2*time.Hour, domain.ErrUnauthenticated
			}
			offset.Store(int64(advance))
			if err := hold.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, want) {
				t.Fatalf("expired authority admitted after writer acquisition: %v want %v", err, want)
			}
			if providerCalls.Load() != 0 {
				t.Fatal("expired staging ceremony reached provider")
			}
			var consumed, adapters int
			if err := svc.DB.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM reauth_windows WHERE consumed_at IS NOT NULL`).Scan(&consumed); err != nil {
				t.Fatal(err)
			}
			if err := svc.DB.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM adapters`).Scan(&adapters); err != nil {
				t.Fatal(err)
			}
			if consumed != 0 || adapters != 1 {
				t.Fatalf("expired staging mutation: consumed=%d adapters=%d", consumed, adapters)
			}
		})
	}
}

func adapterClockTemporaryReveal(t *testing.T, svc *Adapters, now time.Time) {
	t.Helper()
	stamp := now.Format("2006-01-02T15:04:05.000000Z")
	expires := now.Add(time.Second).Format("2006-01-02T15:04:05.000000Z")
	for _, statement := range []string{
		`DELETE FROM grants WHERE id='gr_reveal_one'`,
		fmt.Sprintf(`INSERT INTO access_requests(id,org_id,project_id,environment_id,policy_id,policy_version,requester_principal_id,capabilities,duration_seconds,reason,state,created_at,review_expires_at,granted_at,expires_at) VALUES ('ar_clock','org_adapter','prj_adapter','env_one','policy_fixture',1,'usr_adapter','["reveal"]',1,'clock fixture','granted','%s','%s','%s','%s')`, stamp, expires, stamp, expires),
		fmt.Sprintf(`INSERT INTO access_grants(id,principal_id,capability,org_id,project_id,env_id,request_id,created_at,expires_at) VALUES ('ag_clock','usr_adapter','reveal','org_adapter','prj_adapter','env_one','ar_clock','%s','%s')`, stamp, expires),
	} {
		if _, err := svc.DB.SQLiteWrite().ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAdapterAdoptFinalWriteRechecksLiveAuthority(t *testing.T) {
	for _, mode := range []string{"valid", "expired-session", "expired-reveal"} {
		t.Run(mode, func(t *testing.T) {
			svc, bearer, _, now, _ := adapterClockFixture(t)
			recordAdapterPlanArtifact(t, svc.DB, "plan_final_clock")
			adapterClockWindows(t, svc.DB, authz.OpAdapterAdopt, []string{"env_one", "env_two"}, now, now.Add(time.Minute))
			advance := time.Duration(0)
			switch mode {
			case "expired-session":
				advance = 2 * time.Hour
			case "expired-reveal":
				adapterClockTemporaryReveal(t, svc, now)
				advance = 2 * time.Second
			}
			var calls atomic.Int32
			svc.Now = func() time.Time {
				if calls.Add(1) == 1 {
					return now
				}
				return now.Add(advance)
			}
			result, err := svc.Adopt(t.Context(), Bearer(bearer), adapterScope, AdoptAdapterRequest{
				TargetID: "tgt_one", ArtifactID: "plan_final_clock", ExpectedGeneration: 1, ExpectedDestinationID: 42,
				Entries: []store.AdapterConflictEntry{{Surface: "secret", EffectiveName: "ONE_TOKEN"}},
			})
			if mode == "valid" {
				if err != nil || result.Generation != 2 {
					t.Fatalf("valid adoption failed: %+v %v", result, err)
				}
			} else {
				want := domain.ErrUnauthenticated
				if mode == "expired-reveal" {
					want = domain.ErrNotFound
				}
				if !errors.Is(err, want) {
					t.Fatalf("adoption committed expired authority: %v want %v", err, want)
				}
			}
			var generation, ledger, consumed, jobs, audits int
			for query, dest := range map[string]*int{
				`SELECT generation FROM adapter_targets WHERE id='tgt_one'`:           &generation,
				`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_one'`:       &ledger,
				`SELECT COUNT(*) FROM reauth_windows WHERE consumed_at IS NOT NULL`:   &consumed,
				`SELECT COUNT(*) FROM adapter_outbox`:                                 &jobs,
				`SELECT COUNT(*) FROM audit_tenant_events WHERE type='adapter.adopt'`: &audits,
			} {
				if err := svc.DB.SQLiteRead().QueryRowContext(t.Context(), query).Scan(dest); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "valid" {
				if generation != 2 || ledger != 1 || consumed != 2 || jobs != 1 || audits != 1 {
					t.Fatalf("valid adoption state: generation=%d ledger=%d consumed=%d jobs=%d audits=%d", generation, ledger, consumed, jobs, audits)
				}
			} else if generation != 1 || ledger != 0 || consumed != 0 || jobs != 0 || audits != 0 {
				t.Fatalf("expired adoption not rolled back: generation=%d ledger=%d consumed=%d jobs=%d audits=%d", generation, ledger, consumed, jobs, audits)
			}
		})
	}
}

func TestAdapterProviderGateUsesSecurityClock(t *testing.T) {
	for _, configure := range []bool{false, true} {
		t.Run(fmt.Sprintf("configure=%v", configure), func(t *testing.T) {
			svc, bearer, _, now, offset := adapterClockFixture(t)
			adapterClockTemporaryReveal(t, svc, now)
			gate := svc.gate(Bearer(bearer), authz.OpAdapterPush, domain.Scope{Org: adapterScope.Org, Project: adapterScope.Project, Env: "env_one"})
			if configure {
				gate = svc.providerGate(Bearer(bearer), authz.OpAdapterConfigure, adapterScope, "env_one")
			}
			if err := gate(t.Context()); err != nil {
				t.Fatalf("live temporary reveal refused: %v", err)
			}
			offset.Store(int64(2 * time.Second))
			if err := gate(t.Context()); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("provider gate ignored service security clock: %v", err)
			}
		})
	}
}

func TestAdapterConfigureFinalWriteRechecksTemporaryReveal(t *testing.T) {
	for _, add := range []bool{false, true} {
		t.Run(fmt.Sprintf("add=%v", add), func(t *testing.T) {
			svc, bearer, input, now, offset := adapterClockFixture(t)
			adapterClockTemporaryReveal(t, svc, now)
			environments := []string{"env_one"}
			if add {
				environments = append(environments, "env_two")
				// The newly added environment remains authorized. The existing
				// sibling's reveal grant expires while its delegation changes.
				input.EnvironmentID = "env_two"
			}
			adapterClockWindows(t, svc.DB, authz.OpAdapterConfigure, environments, now, now.Add(time.Minute))
			var responded bool
			svc.ModuleFactory = providerBlindTestModuleFactory(func(string, string) (adapter.Module, func(), error) {
				return adapterResponseClockModule{respond: func() { responded = true; offset.Store(int64(2 * time.Second)) }}, nil, nil
			})
			var err error
			if add {
				_, err = svc.AddTarget(t.Context(), Bearer(bearer), adapterScope, "adp_1", input)
			} else {
				_, err = svc.Create(t.Context(), Bearer(bearer), adapterScope, CreateAdapterRequest{Origin: "https://clock.example", Credential: []byte("provider-token"), Target: input})
			}
			if !responded {
				t.Fatalf("did not reach provider with live temporary reveal: %v", err)
			}
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("expired temporary reveal admitted final delegation: %v", err)
			}
			var targets int
			if err := svc.DB.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM adapter_targets`).Scan(&targets); err != nil || targets != 2 {
				t.Fatalf("expired reveal changed target rows: %d %v", targets, err)
			}
		})
	}
}

func adapterClockAfterWriterWait(t *testing.T, svc *Adapters, offset *atomic.Int64, advance time.Duration, call func(context.Context) error) error {
	t.Helper()
	hold, err := svc.DB.SQLiteWrite().BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback()
	waitBefore := svc.DB.SQLiteWrite().Stats().WaitCount
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- call(ctx) }()
	for svc.DB.SQLiteWrite().Stats().WaitCount == waitBefore {
		select {
		case err := <-done:
			t.Fatalf("operation never waited for writer acquisition: %v", err)
		case <-ctx.Done():
			t.Fatal("writer acquisition was not reached")
		case <-time.After(time.Millisecond):
		}
	}
	offset.Store(int64(advance))
	if err := hold.Commit(); err != nil {
		t.Fatal(err)
	}
	return <-done
}

func TestAdapterMoveRecoveryRechecksAfterWriterAcquisition(t *testing.T) {
	for _, operation := range []string{"cancel", "resume target", "resume origin"} {
		for _, expireSession := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/expired-session=%v", operation, expireSession), func(t *testing.T) {
				svc, bearer, input, now, offset := adapterClockFixture(t)
				if _, err := svc.DB.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO adapter_target_keys(org_id,project_id,environment_id,target_id,adapter_id,key_id) SELECT org_id,project_id,environment_id,id,adapter_id,'key_clock' FROM adapter_targets`); err != nil {
					t.Fatal(err)
				}
				if err := storetx.Write(t.Context(), svc.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
					_, proof, err := authorize(ctx, az, LocalPrincipal("usr_adapter"), authz.OpAdapterConfigure, adapterScope, now)
					if err != nil {
						return err
					}
					if operation == "resume origin" {
						_, err = r.Adapters().MoveOrigin(ctx, proof, store.AdapterOriginMoveMutation{MoveID: "arm_clock", AdapterID: "adp_1", Origin: "https://pending.example", PendingCredentialCiphertext: []byte("sealed-fixture"), AuthorityPrincipalID: "usr_adapter", KeepRemote: true, At: now})
					} else {
						_, err = r.Adapters().MoveTarget(ctx, proof, store.AdapterRouteMoveMutation{MoveID: "arm_clock", Target: store.AdapterTargetMutation{ID: "tgt_one", AdapterID: "adp_1", EnvironmentID: "env_one", DestinationKind: "repository", DestinationOwner: "acme", DestinationName: "pending", NamePrefix: "CLOCK_", KeyIDs: input.KeyIDs}, ExpectedGeneration: 1, AuthorityPrincipalID: "usr_adapter", KeepRemote: true, At: now})
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
				runtime := store.NewAdapterRuntime(svc.DB, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
				job, ok, err := runtime.ClaimDue(t.Context(), "clock-move", now, now.Add(time.Minute))
				if err != nil || !ok {
					t.Fatalf("claim move: %+v %v %v", job, ok, err)
				}
				if err := runtime.Fail(t.Context(), job, 0, now, adapter.ErrProviderAuth); err != nil {
					t.Fatal(err)
				}
				environments := []string{"env_one", "env_two"}
				if operation == "cancel" {
					environments = []string{"env_one"}
				}
				adapterClockWindows(t, svc.DB, authz.OpAdapterConfigure, environments, now, now.Add(time.Second))
				svc.ModuleFactory = providerBlindTestModuleFactory(func(string, string) (adapter.Module, func(), error) {
					return adapterResponseClockModule{respond: func() {}}, nil, nil
				})
				advance, want := 2*time.Second, ErrReauthRequired
				if expireSession {
					advance, want = 2*time.Hour, domain.ErrUnauthenticated
				}
				err = adapterClockAfterWriterWait(t, svc, offset, advance, func(ctx context.Context) error {
					var err error
					switch operation {
					case "cancel":
						_, err = svc.CancelMove(ctx, Bearer(bearer), adapterScope, "arm_clock")
					case "resume target":
						_, err = svc.ResumeTargetMove(ctx, Bearer(bearer), adapterScope, "arm_clock", UpdateAdapterTargetRequest{TargetID: "tgt_one", Target: input})
					case "resume origin":
						_, err = svc.ResumeOriginMove(ctx, Bearer(bearer), adapterScope, "arm_clock", "https://replacement.example", []byte("replacement-token"))
					}
					return err
				})
				if !errors.Is(err, want) {
					t.Fatalf("expired move recovery authority admitted: %v want %v", err, want)
				}
				var state string
				var consumed int
				if err := svc.DB.SQLiteRead().QueryRowContext(t.Context(), `SELECT state FROM adapter_route_moves WHERE id='arm_clock'`).Scan(&state); err != nil || state != "attention_required" {
					t.Fatalf("refused recovery changed move: %s %v", state, err)
				}
				if err := svc.DB.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM reauth_windows WHERE consumed_at IS NOT NULL`).Scan(&consumed); err != nil || consumed != 0 {
					t.Fatalf("expired recovery consumed consent: %d %v", consumed, err)
				}
			})
		}
	}
}

func TestAdapterObservationFinalWriteRechecksAfterWriterAcquisition(t *testing.T) {
	for _, plan := range []bool{false, true} {
		t.Run(fmt.Sprintf("plan=%v", plan), func(t *testing.T) {
			svc, bearer, _, _, offset := adapterClockFixture(t)
			svc.ModuleFactory = providerBlindTestModuleFactory(func(string, string) (adapter.Module, func(), error) {
				return adapterResponseClockModule{respond: func() {}}, nil, nil
			})
			err := adapterClockAfterWriterWait(t, svc, offset, 2*time.Hour, func(ctx context.Context) error {
				var err error
				if plan {
					_, err = svc.Plan(ctx, Bearer(bearer), adapterScope, "tgt_one")
				} else {
					_, err = svc.TestTarget(ctx, Bearer(bearer), adapterScope, "tgt_one")
				}
				return err
			})
			if !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("expired observation authority admitted after writer acquisition: %v", err)
			}
			var effects int
			if err := svc.DB.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_tenant_events WHERE type IN ('adapter.plan','adapter.test') AND outcome='success'`).Scan(&effects); err != nil || effects != 0 {
				t.Fatalf("expired observation recorded success: %d %v", effects, err)
			}
		})
	}
}
