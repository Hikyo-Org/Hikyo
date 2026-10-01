package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestAdapterProviderFenceRetryRechecksSecurityClock(t *testing.T) {
	for _, kind := range []string{"expired session", "expired reauthentication"} {
		t.Run(kind, func(t *testing.T) {
			db := adapterServiceDB(t)
			bearer := adapterCLISession(t, db)
			now := store.CanonTime(time.Now().UTC())
			if _, err := db.SQLiteWrite().ExecContext(t.Context(), `UPDATE adapter_targets SET provider_lease_job_id='held-job',provider_lease_effect_id='held-effect',provider_lease_expires_at=? WHERE id='tgt_one'`, now.Add(24*time.Hour).Format("2006-01-02T15:04:05.000000Z")); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_retry_reveal_two','usr_adapter','reveal','org_adapter','prj_adapter','env_two','2026-08-17T00:00:00Z')`); err != nil {
				t.Fatal(err)
			}
			advance := 2 * time.Hour
			if kind == "expired reauthentication" {
				advance = 2 * time.Second
				if err := storetx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
					for _, env := range []string{"env_one", "env_two"} {
						if err := az.OpenReauthWindow(ctx, authz.NewReauthWindow{
							ID: "rw_retry_" + env, SessionID: "ses_adapter", EnvironmentID: env, FactorClass: "totp",
							AuthenticatedAt: now, WindowExpiresAt: now.Add(time.Second), HardExpiresAt: now.Add(time.Second),
							CredentialEpoch: 1, CreatedAt: now,
						}); err != nil {
							return err
						}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			var calls atomic.Int32
			svc := &Adapters{DB: db, Auth: &Auth{DB: db, ReauthWindow: time.Minute}, Now: func() time.Time {
				if calls.Add(1) == 1 {
					return now
				}
				return now.Add(advance)
			}}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var err error
			if kind == "expired session" {
				_, err = svc.PauseTarget(ctx, Bearer(bearer), adapterScope, "tgt_one")
				if !errors.Is(err, domain.ErrUnauthenticated) {
					t.Fatalf("expired session retry = %v", err)
				}
			} else {
				_, err = svc.SyncTarget(ctx, Bearer(bearer), adapterScope, "tgt_one")
				if !errors.Is(err, ErrReauthRequired) {
					t.Fatalf("expired reauthentication retry = %v", err)
				}
			}
			if calls.Load() < 2 {
				t.Fatal("provider-fence retry did not re-read the security clock")
			}
			var generation, jobs int
			if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT generation FROM adapter_targets WHERE id='tgt_one'`).Scan(&generation); err != nil {
				t.Fatal(err)
			}
			if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM adapter_outbox`).Scan(&jobs); err != nil {
				t.Fatal(err)
			}
			if generation != 1 || jobs != 0 {
				t.Fatalf("expired authority mutated generation=%d jobs=%d", generation, jobs)
			}
		})
	}
}

func TestAdapterAdoptSharedEnvironmentConsumesSingleDecisionOnce(t *testing.T) {
	db := adapterServiceDB(t)
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `UPDATE adapter_targets SET environment_id='env_one',destination_name='other',destination_id=43 WHERE id='tgt_two'`); err != nil {
		t.Fatal(err)
	}
	recordAdapterPlanArtifact(t, db, "plan_shared_env")
	bearer := adapterCLISession(t, db)
	now := store.CanonTime(time.Now().UTC())
	intent, err := NewAdapterReauthIntent(string(authz.OpAdapterAdopt), []string{"env_one"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := intent.bindingFor("env_one")
	if err != nil {
		t.Fatal(err)
	}
	if err := storetx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		// This fixture starts after passkey verification. Exercise the real
		// persisted ceremony binding and atomic single-decision consumption.
		if err := az.CreateWebAuthnCeremony(ctx, authz.NewWebAuthnCeremony{
			ID: "wac_shared_env", ChallengeVerifier: []byte("verified-fixture-challenge"), SessionData: []byte("{}"),
			SessionID: "ses_adapter", Purpose: "reauth", OperationBinding: binding.challengeBinding,
			EnvironmentID: "env_one", CredentialEpoch: 1, ExpiresAt: now.Add(time.Minute), CreatedAt: now,
		}); err != nil {
			return err
		}
		return az.OpenReauthWindow(ctx, authz.NewReauthWindow{
			ID: "rw_shared_env", SessionID: "ses_adapter", EnvironmentID: "env_one", CeremonyID: "wac_shared_env",
			FactorClass: "webauthn", SingleDecision: true, AuthenticatedAt: now,
			WindowExpiresAt: now.Add(time.Minute), HardExpiresAt: now.Add(time.Minute), CredentialEpoch: 1, CreatedAt: now,
			BoundPurpose: string(binding.purpose), BoundOperation: string(binding.operation), BoundEnvironmentSet: binding.environmentSet,
		})
	}); err != nil {
		t.Fatal(err)
	}
	svc := &Adapters{DB: db, Auth: &Auth{DB: db}}
	request := AdoptAdapterRequest{TargetID: "tgt_one", ArtifactID: "plan_shared_env", ExpectedGeneration: 1, ExpectedDestinationID: 42, Entries: []store.AdapterConflictEntry{{Surface: "secret", EffectiveName: "ONE_TOKEN"}}}
	result, err := svc.Adopt(t.Context(), Bearer(bearer), adapterScope, request)
	if err != nil || result.Generation != 2 || result.JobID == "" {
		t.Fatalf("shared-environment adoption = %+v, %v", result, err)
	}
	var consumed int
	if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM reauth_windows WHERE id='rw_shared_env' AND consumed_at IS NOT NULL`).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed != 1 {
		t.Fatalf("single-decision consumption = %d", consumed)
	}
	request.ExpectedGeneration = 2
	if _, err := svc.Adopt(t.Context(), Bearer(bearer), adapterScope, request); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("spent window replay = %v", err)
	}
}

func routingBudgetService(t *testing.T, module adapter.Module) (*Adapters, UpdateAdapterTargetRequest) {
	t.Helper()
	db := adapterServiceDB(t)
	seedKey(t, db, "key_routing_budget", "API_TOKEN")
	for _, statement := range []string{
		`INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES ('org_adapter','prj_adapter','env_one','tgt_one','adp_1','key_routing_budget')`,
		`UPDATE adapters SET provider='github-actions' WHERE id='adp_1'`,
		`UPDATE adapter_targets SET destination_kind='organization',destination_name='',visibility='selected',selected_repository_ids='[22]' WHERE id='tgt_one'`,
	} {
		if _, err := db.SQLiteWrite().ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	kr := adapterKeyring(t, db)
	sealer, err := kr.ForProject(t.Context(), "org_adapter", "prj_adapter")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `UPDATE adapters SET credential_ciphertext=? WHERE id='adp_1'`, sealAdapterCredential(t, sealer, "github_pat_fine")); err != nil {
		t.Fatal(err)
	}
	svc := &Adapters{DB: db, Keyring: kr, Auth: &Auth{DB: db}, Budget: NewBudget(), ModuleFactory: testModuleFactory(func(adapter.Provider, string, string) (adapter.Module, func(), error) { return module, nil, nil })}
	return svc, UpdateAdapterTargetRequest{TargetID: "tgt_one", ExpectedGeneration: 1, Target: AdapterTargetInput{EnvironmentID: "env_one", DestinationKind: "organization", DestinationOwner: "acme", Visibility: "selected", SelectedRepositoryIDs: []int64{11}, NamePrefix: "ONE_", KeyIDs: []string{"key_routing_budget"}}}
}

func TestRoutingPreflightRefusalsConsumeAdapterBudget(t *testing.T) {
	var calls int
	svc, request := routingBudgetService(t, fakeAdapterTestModule{gates: &calls})
	bearer := adapterCLISession(t, svc.DB)
	for i := 0; i < BudgetAdapterRatePerMin; i++ {
		_, err := svc.ApplyTargetMutation(t.Context(), Bearer(bearer), adapterScope, request, false)
		if !errors.Is(err, ErrReauthRequired) {
			// The authenticated caller has no reauthentication window.
			t.Fatalf("request %d ceremony refusal = %v", i, err)
		}
	}
	if calls != 2*BudgetAdapterRatePerMin {
		t.Fatalf("provider calls = %d", calls)
	}
	_, err := svc.ApplyTargetMutation(t.Context(), Bearer(bearer), adapterScope, request, false)
	if !errors.Is(err, admission.ErrOverloaded) || calls != 2*BudgetAdapterRatePerMin {
		t.Fatalf("over-budget provider work: err=%v calls=%d", err, calls)
	}
}

func TestRoutingPreflightAndCommitChargeOnce(t *testing.T) {
	var calls int
	svc, request := routingBudgetService(t, fakeAdapterTestModule{gates: &calls})
	if _, err := svc.DB.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_routing_reveal_two','usr_adapter','reveal','org_adapter','prj_adapter','env_two','2026-08-17T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyTargetMutation(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, request, false); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < BudgetAdapterRatePerMin; i++ {
		var charged bool
		if err := svc.Budget.chargeOnce(&charged, budgetAdapterRate, budgetKeys{Principal: "usr_adapter"}); err != nil {
			t.Fatalf("successful preflight charged more than once: request %d: %v", i, err)
		}
	}
	var charged bool
	if err := svc.Budget.chargeOnce(&charged, budgetAdapterRate, budgetKeys{Principal: "usr_adapter"}); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("expected tenth additional charge to exceed budget: %v", err)
	}
}

func TestRoutingPreflightHoldsOrgSlotBeforeProviderWork(t *testing.T) {
	started := make(chan struct{}, BudgetAdapterOrgConcurrency)
	proceed := make(chan struct{})
	svc, request := routingBudgetService(t, blockingRoutingPreflightModule{started: started, proceed: proceed})
	done := make(chan error, BudgetAdapterOrgConcurrency)
	for i := 0; i < BudgetAdapterOrgConcurrency; i++ {
		go func() {
			_, err := svc.ApplyTargetMutation(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, request, false)
			done <- err
		}()
	}
	for i := 0; i < BudgetAdapterOrgConcurrency; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(proceed)
			t.Fatal("provider preflight did not start")
		}
	}
	_, err := svc.ApplyTargetMutation(t.Context(), LocalPrincipal("usr_adapter"), adapterScope, request, false)
	close(proceed)
	for i := 0; i < BudgetAdapterOrgConcurrency; i++ {
		<-done
	}
	if !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("fifth preflight = %v", err)
	}
	select {
	case <-started:
		t.Fatal("over-budget request reached the provider")
	default:
	}
}
