package isolation

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm/awssmtest"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type adapterRetirementFixture struct {
	svc        *service.Adapters
	target     store.AdapterTarget
	remote     *awssmtest.Server
	runtime    *store.AdapterRuntime
	worker     *adapter.Worker
	scope      domain.Scope
	credential []byte
	publish    func(string)
	drain      func()
}

// Keep real service, credential custody, publish, job and journal boundaries;
// only the remote AWS endpoint is replaced by its signed-wire fixture.
func newAdapterRetirementFixture(t *testing.T, db *store.DB) adapterRetirementFixture {
	t.Helper()
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('retirement_manage','usr_alice','manage-adapters','org_a','prj_a1',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('retirement_reveal','usr_alice','reveal','org_a','prj_a1','env_a1',`+ts+`)`)
	remote := awssmtest.New(awsAccount, awsRegion)
	t.Cleanup(remote.Close)
	roots := x509.NewCertPool()
	roots.AddCert(remote.Certificate())
	build := func(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
		client, err := awssm.NewClient(awssm.ClientConfig{Origin: config.Origin, Credential: credential, Deadline: 5 * time.Second, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, STSAllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, RootCAs: roots})
		if err != nil {
			return nil, err
		}
		return adapter.NewModuleLease(&awssm.Module{API: client}, client.Forget)
	}
	kr := probeKeyring(t, db)
	svc := &service.Adapters{DB: db, Keyring: kr, ModuleFactory: func(_ adapter.Provider, config adapter.Config, credential string) (*adapter.ModuleLease, error) {
		return build(config, credential)
	}}
	scope := domain.Scope{Org: orgA, Project: prjA1}
	credential := []byte(`{"mode":"static","region":"` + awsRegion + `","access_key_id":"AKIAHIKYOE2E0000001","secret_access_key":"retirement-fixture-key"}`)
	created, err := svc.Create(t.Context(), service.LocalPrincipal(alice), scope, service.CreateAdapterRequest{Provider: string(adapter.AWSSecretsManagerProvider), Origin: remote.URL, Credential: credential, Target: service.AdapterTargetInput{EnvironmentID: string(envA1), DestinationKind: string(adapter.JSONObject), DestinationOwner: awsAccount, DestinationName: "prod/lifecycle", KeyIDs: []string{keyA1}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime := store.NewAdapterRuntime(db, func(ctx context.Context, job adapter.Job, _ adapter.Effect) error {
		return tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID(job.AuthorityPrincipal), Class: domain.ClassHuman}, authz.OpAdapterPush, domain.Scope{Org: domain.OrgID(job.OrgID), Project: domain.ProjectID(job.ProjectID), Env: domain.EnvID(job.EnvironmentID)})
			return err
		})
	})
	worker := &adapter.Worker{Store: runtime, Loader: awsTestLoader{runtime: runtime, keyring: kr, build: build}, ID: "retirement-worker", Jitter: func(d time.Duration) time.Duration { return d }}
	values, revisions := &service.Values{DB: db, Keyring: kr}, &service.Revisions{DB: db, Keyring: kr}
	publish := func(value string) {
		t.Helper()
		staged, err := values.Set(t.Context(), service.LocalPrincipal(custodian), domain.Scope{Org: orgA, Project: prjA1, Env: envA1}, "SHARED_KEY", value, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := revisions.PublishPlanned(t.Context(), service.LocalPrincipal(custodian), domain.Scope{Org: orgA, Project: prjA1, Env: envA1}, service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
			t.Fatal(err)
		}
	}
	drain := func() {
		t.Helper()
		for range 8 {
			worked, err := worker.RunOnce(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !worked {
				return
			}
		}
		t.Fatal("retirement fixture outbox did not drain")
	}
	return adapterRetirementFixture{svc: svc, target: created.Targets[0], remote: remote, runtime: runtime, worker: worker, scope: scope, credential: credential, publish: publish, drain: drain}
}

func TestAdapterPausedTeardownStillScrubsAndErasesCredential(t *testing.T) {
	for _, wholeAdapter := range []bool{false, true} {
		t.Run(fmt.Sprintf("whole-adapter=%v", wholeAdapter), func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				f := newAdapterRetirementFixture(t, db)
				f.publish("delivered-before-pause")
				f.drain()
				if f.remote.Versions("prod/lifecycle") != 1 {
					t.Fatal("fixture never delivered a remote value")
				}
				if _, err := f.svc.PauseTarget(t.Context(), service.LocalPrincipal(alice), f.scope, f.target.ID); err != nil {
					t.Fatal(err)
				}
				f.publish("paused-publication")
				if worked, err := f.worker.RunOnce(t.Context()); worked || err != nil {
					t.Fatalf("paused converge was claimed: %v, %v", worked, err)
				}
				if wholeAdapter {
					if _, err := f.svc.Delete(t.Context(), service.LocalPrincipal(alice), f.scope, f.target.AdapterID, false); err != nil {
						t.Fatal(err)
					}
				} else if _, err := f.svc.RemoveTarget(t.Context(), service.LocalPrincipal(alice), f.scope, f.target.ID, false); err != nil {
					t.Fatal(err)
				}
				f.drain()
				if !f.remote.Deleted("prod/lifecycle") {
					t.Fatal("paused teardown stranded the remote value")
				}
				checks := []string{
					fmt.Sprintf(`SELECT COUNT(*) FROM adapter_outbox WHERE target_id='%s' AND kind='scrub' AND state='succeeded'`, f.target.ID),
					fmt.Sprintf(`SELECT COUNT(*) FROM adapters WHERE id='%s' AND credential_ciphertext IS NULL AND credential_set_at IS NULL AND credential_expires_at IS NULL`, f.target.AdapterID),
					fmt.Sprintf(`SELECT COUNT(*) FROM adapter_targets WHERE id='%s' AND active_job_id IS NULL`, f.target.ID),
				}
				for _, query := range checks {
					if got := queryInt(t, db, query); got != 1 {
						t.Fatalf("teardown check %q = %d", query, got)
					}
				}
				if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state<>'released'`, f.target.ID)); got != 0 {
					t.Fatalf("teardown retained live custody: %d", got)
				}
			})
		})
	}
}

func TestAdapterCredentialChangesRetireWorkWithoutBlockingSyncOrPublish(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		for _, running := range []bool{false, true} {
			t.Run(fmt.Sprintf("revoke=%v/running=%v", revoke, running), func(t *testing.T) {
				forEngines(t, func(t *testing.T, db *store.DB) {
					f := newAdapterRetirementFixture(t, db)
					f.publish("previously-delivered")
					f.drain()
					historyQuery := fmt.Sprintf(`SELECT id||'|'||state||'|'||CAST(finished_at AS TEXT) FROM adapter_outbox WHERE target_id='%s' AND state='succeeded'`, f.target.ID)
					history := queryString(t, db, historyQuery)
					f.publish("queued-before-credential-change")
					oldID := queryString(t, db, fmt.Sprintf(`SELECT active_job_id FROM adapter_targets WHERE id='%s'`, f.target.ID))
					var claimed adapter.Job
					var heldJournal adapter.Journal
					heldEffect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "prod/lifecycle", Disposition: adapter.Update}
					var heldPrior adapter.LedgerState
					if running {
						var ok bool
						var err error
						now := time.Now().UTC()
						claimed, ok, err = f.runtime.ClaimDue(t.Context(), "old-credential-worker", now, now.Add(adapter.LeaseTime))
						if err != nil || !ok || claimed.ID != oldID {
							t.Fatalf("claim before credential change: %+v, %v", claimed, err)
						}
						if revoke {
							heldJournal = f.runtime.Journal(claimed)
							heldPrior, err = heldJournal.Reserve(t.Context(), heldEffect)
							if err != nil {
								t.Fatal(err)
							}
							if err := heldJournal.Prepare(t.Context(), heldEffect, heldPrior); err != nil {
								t.Fatal(err)
							}
						}
					}
					if revoke {
						if _, err := f.svc.RevokeCredential(t.Context(), service.LocalPrincipal(alice), f.scope, f.target.AdapterID); err != nil {
							t.Fatal(err)
						}
					} else if _, err := f.svc.ReplaceCredential(t.Context(), service.LocalPrincipal(alice), f.scope, f.target.AdapterID, f.credential); err != nil {
						t.Fatal(err)
					}
					if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_targets WHERE id='%s' AND active_job_id IS NULL`, f.target.ID)); got != 1 {
						t.Fatal("credential change retained stale active job")
					}
					if got := queryString(t, db, fmt.Sprintf(`SELECT state FROM adapter_outbox WHERE id='%s'`, oldID)); got != "superseded" {
						t.Fatalf("old pending job was not retired: %s", got)
					}
					if got := queryString(t, db, historyQuery); got != history {
						t.Fatal("credential retirement rewrote a terminal outcome")
					}
					if revoke {
						if running {
							if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_targets WHERE id='%s' AND provider_lease_job_id='%s' AND provider_lease_effect_id IS NOT NULL`, f.target.ID, oldID)); got != 1 {
								t.Fatal("revocation released a live provider effect fence")
							}
							// This fixture prepared but never dispatched a provider
							// call. Its known failure may settle held custody after
							// revocation; only then is replacement safe.
							if err := heldJournal.Finish(t.Context(), heldEffect, adapter.Completion{Outcome: adapter.OutcomeFailure, State: heldPrior}); err != nil {
								t.Fatal(err)
							}
						}
						if _, err := f.svc.ReplaceCredential(t.Context(), service.LocalPrincipal(alice), f.scope, f.target.AdapterID, f.credential); err != nil {
							t.Fatal(err)
						}
					}
					manual, err := f.svc.SyncTarget(t.Context(), service.LocalPrincipal(alice), f.scope, f.target.ID)
					if err != nil {
						t.Fatalf("manual sync after credential change: %v", err)
					}
					if running {
						before := queryString(t, db, fmt.Sprintf(`SELECT state||'|'||CAST(finished_at AS TEXT) FROM adapter_outbox WHERE id='%s'`, oldID))
						if err := f.runtime.Fail(t.Context(), claimed, 0, time.Now().UTC(), adapter.ErrSuperseded); !errors.Is(err, adapter.ErrSuperseded) {
							t.Fatalf("retired worker rewrote terminal outcome: %v", err)
						}
						if got := queryString(t, db, fmt.Sprintf(`SELECT state||'|'||CAST(finished_at AS TEXT) FROM adapter_outbox WHERE id='%s'`, oldID)); got != before {
							t.Fatal("stale settlement changed historical job outcome")
						}
						if got := queryString(t, db, fmt.Sprintf(`SELECT active_job_id FROM adapter_targets WHERE id='%s'`, f.target.ID)); got != manual.JobID {
							t.Fatal("stale settlement detached the replacement job")
						}
					}
					f.publish("published-after-credential-change")
					f.drain()
					if f.remote.Versions("prod/lifecycle") != 2 {
						t.Fatal("publish/manual sync failed to converge after credential retirement")
					}
				})
			})
		}
	}
}
