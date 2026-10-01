package isolation

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm/awssmtest"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// AWS Secrets Manager (#158), end to end through the real service, store,
// outbox runtime, worker, sealed credential, and snapshot decryption, with
// only AWS itself replaced by the in-process awssmtest emulator speaking the
// signed awsJson1.1 and STS wire protocols. It proves value delivery in both
// destination modes, the no-read API closure on the wire, one-version replay
// of a job, fail-loud handling of an external update, teardown with the AWS
// recovery window, and value-blind audit.

const (
	awsAccount  = "123456789012"
	awsRegion   = "eu-west-1"
	awsSecretV1 = "postgres://svc:first@db/app?sslmode=verify-full\nline two é"
	awsSecretV2 = "postgres://svc:second@db/app"
)

// awsTestLoader is the production loader's contract for one job: it opens the
// sealed descriptor and the pinned snapshot values under their exact AAD, and
// forgets both when the attempt releases them.
type awsTestLoader struct {
	runtime *store.AdapterRuntime
	keyring *crypto.Keyring
	build   func(adapter.Config, string) (*adapter.ModuleLease, error)
}

type awsRevokeAfterCreateClient struct {
	*awssm.Client
	afterCreate func(context.Context) error
}

type awsInterruptedAdoptionClient struct {
	*awssm.Client
	afterStage func(context.Context) error
}

func (client *awsInterruptedAdoptionClient) PutSecretValue(ctx context.Context, id, token, value string) error {
	if err := client.Client.PutSecretValue(ctx, id, token, value); err != nil {
		return err
	}
	return client.afterStage(ctx)
}

func (client *awsRevokeAfterCreateClient) CreateSecret(ctx context.Context, input awssm.CreateSecretInput) error {
	if err := client.Client.CreateSecret(ctx, input); err != nil {
		return err
	}
	return client.afterCreate(ctx)
}

func (l awsTestLoader) Load(ctx context.Context, job adapter.Job, journal adapter.Journal) (adapter.LoadedSync, error) {
	if err := journal.Gate(ctx, adapter.Effect{Surface: adapter.Secret, EffectiveName: "manifest", Disposition: adapter.Update}); err != nil {
		return adapter.LoadedSync{}, err
	}
	material, err := l.runtime.LoadExecution(ctx, job)
	if err != nil {
		return adapter.LoadedSync{}, err
	}
	sealer, err := l.keyring.ForProject(ctx, job.OrgID, job.ProjectID)
	if err != nil {
		return adapter.LoadedSync{}, err
	}
	credential, err := sealer.OpenField(adapter.CredentialAAD(job.OrgID, job.ProjectID, material.CredentialOwnerID), material.CredentialCiphertext)
	if err != nil {
		return adapter.LoadedSync{}, err
	}
	lease, err := l.build(adapter.Config{Origin: material.Origin}, string(credential))
	crypto.Zero(credential)
	if err != nil {
		return adapter.LoadedSync{}, err
	}
	manifest := make([]adapter.ManifestEntry, 0, len(material.Entries))
	for _, row := range material.Entries {
		plain, err := sealer.OpenField(crypto.ProjectFieldAAD{
			OrgID: job.OrgID, ProjectID: job.ProjectID,
			OwnerTable: "snapshot_entries", OwnerRowID: row.ID, FieldTag: "snapshot_value",
			EnvironmentID: job.EnvironmentID, KeyID: row.KeyID, SnapshotID: row.SnapshotID,
		}, row.Ciphertext)
		if err != nil {
			lease.Release()
			return adapter.LoadedSync{}, err
		}
		manifest = append(manifest, adapter.ManifestEntry{KeyID: row.KeyID, CanonicalName: row.KeyName, Classification: adapter.Classification(row.Classification), Value: string(plain)})
		crypto.Zero(plain)
	}
	return adapter.LoadedSync{
		Module: lease.Module, Revision: material.Revision, Release: lease.Release,
		Request: adapter.SyncRequest{Config: adapter.Config{Origin: material.Origin}, Target: material.Target, Manifest: manifest, Ledger: material.Ledger},
	}, nil
}

func runAWSSecretsManagerLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := tctx(t)
	scope := domain.Scope{Org: orgA, Project: prjA1}
	envScope := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_aws_manage','usr_alice','manage-adapters','org_a','prj_a1',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_aws_reveal','usr_alice','reveal','org_a','prj_a1','env_a1',`+ts+`)`)

	emulator := awssmtest.New(awsAccount, awsRegion)
	t.Cleanup(emulator.Close)
	roots := x509.NewCertPool()
	roots.AddCert(emulator.Certificate())
	var afterCreate func(context.Context) error
	var afterStage func(context.Context) error
	build := func(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
		client, err := awssm.NewClient(awssm.ClientConfig{
			Origin: config.Origin, Credential: credential, Deadline: 5 * time.Second,
			AllowedCIDRs:    []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
			STSAllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, RootCAs: roots,
		})
		if err != nil {
			return nil, err
		}
		var api awssm.API = client
		if afterCreate != nil {
			api = &awsRevokeAfterCreateClient{Client: client, afterCreate: afterCreate}
		} else if afterStage != nil {
			api = &awsInterruptedAdoptionClient{Client: client, afterStage: afterStage}
		}
		return adapter.NewModuleLease(&awssm.Module{API: api}, client.Forget)
	}
	kr := probeKeyring(t, db)
	svc := &service.Adapters{
		DB: db, Keyring: kr,
		ModuleFactory: func(provider adapter.Provider, config adapter.Config, credential string) (*adapter.ModuleLease, error) {
			if provider != adapter.AWSSecretsManagerProvider {
				return nil, fmt.Errorf("aws e2e: unexpected provider %s", provider)
			}
			return build(config, credential)
		},
	}
	operator := service.LocalPrincipal(alice)
	descriptor := `{"mode":"static","region":"` + awsRegion + `","access_key_id":"AKIAHIKYOE2E0000001","secret_access_key":"e2e-secret-access-key"}`
	selection := &service.AdapterKeySelection{Include: []string{"SHARED_*"}}
	created, err := svc.Create(ctx, operator, scope, service.CreateAdapterRequest{
		Provider: string(adapter.AWSSecretsManagerProvider), Origin: emulator.URL, Credential: []byte(descriptor),
		Target: service.AdapterTargetInput{
			EnvironmentID: string(envA1), DestinationKind: string(adapter.JSONObject),
			DestinationOwner: awsAccount, DestinationName: "prod/app", KeySelection: selection,
		},
	})
	if err != nil {
		t.Fatalf("create aws adapter: %v", err)
	}
	jsonTarget := created.Targets[0]
	if jsonTarget.DestinationID != 123456789012 {
		t.Fatalf("json target destination id = %d, want the account", jsonTarget.DestinationID)
	}
	perKey, err := svc.AddTarget(ctx, operator, scope, created.Adapter.ID, service.AdapterTargetInput{
		EnvironmentID: string(envA1), DestinationKind: string(adapter.PerKey),
		DestinationOwner: awsAccount, DestinationName: "prod/keys/", NamePrefix: "APP_", KeySelection: selection,
	})
	if err != nil {
		t.Fatalf("add per-key target: %v", err)
	}
	// One account and region is one secret namespace across both modes: a
	// disjoint per-key target is admitted, but a json-object target whose
	// secret name another target already owns is refused before any provider
	// call.
	if _, err := svc.AddTarget(ctx, operator, scope, created.Adapter.ID, service.AdapterTargetInput{
		EnvironmentID: string(envA1), DestinationKind: string(adapter.PerKey),
		DestinationOwner: awsAccount, DestinationName: "prod/", NamePrefix: "", KeySelection: &service.AdapterKeySelection{Names: []string{"SHARED_KEY"}},
	}); err != nil {
		t.Fatalf("disjoint per-key target refused: %v", err)
	}
	if _, err := svc.AddTarget(ctx, operator, scope, created.Adapter.ID, service.AdapterTargetInput{
		EnvironmentID: string(envA1), DestinationKind: string(adapter.JSONObject),
		DestinationOwner: awsAccount, DestinationName: "prod/SHARED_KEY", KeySelection: selection,
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("json-object target on a name a per-key target owns = %v, want conflict", err)
	}

	publisher := service.LocalPrincipal(custodian)
	values := &service.Values{DB: db, Keyring: kr}
	revisions := &service.Revisions{DB: db, Keyring: kr}
	publish := func(plaintext string) {
		t.Helper()
		staged, err := values.Set(ctx, publisher, envScope, "SHARED_KEY", plaintext, nil)
		if err != nil {
			t.Fatalf("stage: %v", err)
		}
		if _, err := revisions.PublishPlanned(ctx, publisher, envScope, service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	runtime := store.NewAdapterRuntime(db, func(ctx context.Context, job adapter.Job, _ adapter.Effect) error {
		return tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID(job.AuthorityPrincipal), Class: domain.ClassHuman}, authz.OpAdapterPush, domain.Scope{
				Org: domain.OrgID(job.OrgID), Project: domain.ProjectID(job.ProjectID), Env: domain.EnvID(job.EnvironmentID),
			})
			return err
		})
	})
	clock := time.Now().UTC()
	worker := &adapter.Worker{
		Store: runtime, Loader: awsTestLoader{runtime: runtime, keyring: kr, build: build}, ID: "aws-node-1",
		// Full backoff keeps a refused converge parked instead of re-claimed
		// on every tick of the fixed test clock.
		Now: func() time.Time { return clock }, Jitter: func(delay time.Duration) time.Duration { return delay },
		Wait: func(context.Context, time.Duration) error { return nil },
	}
	inspect := func(id string) store.AdapterTarget {
		t.Helper()
		view, err := svc.InspectTarget(ctx, operator, scope, id)
		if err != nil {
			t.Fatalf("inspect %s: %v", id, err)
		}
		return view.Target
	}

	drain := func() {
		t.Helper()
		for range 16 {
			clock = clock.Add(time.Second)
			worked, err := worker.RunOnce(ctx)
			if err != nil {
				t.Fatalf("worker: %v", err)
			}
			if !worked {
				return
			}
		}
		for _, id := range []string{jsonTarget.ID, perKey.ID} {
			target := inspect(id)
			t.Logf("target %s health=%q class=%q failures=%v", id, target.Health(), target.LastErrorClass, target.FailureNames)
		}
		t.Logf("operations: %v", emulator.Operations())
		t.Fatal("worker never drained the outbox")
	}
	// Value delivery in both modes, byte-exact through JSON encoding.
	publish(awsSecretV1)
	drain()
	document, ok := emulator.Value("prod/app")
	if !ok {
		t.Fatal("json-object secret was not delivered")
	}
	var members map[string]string
	if err := json.Unmarshal([]byte(document), &members); err != nil || members["SHARED_KEY"] != awsSecretV1 || len(members) != 1 {
		t.Fatalf("json-object document = %q (%v)", document, err)
	}
	if got, _ := emulator.Value("prod/keys/APP_SHARED_KEY"); got != awsSecretV1 {
		t.Fatalf("per-key value = %q", got)
	}
	for _, id := range []string{jsonTarget.ID, perKey.ID} {
		if target := inspect(id); target.Health() != adapter.HealthConverged {
			t.Fatalf("target %s health = %q class %q", id, target.Health(), target.LastErrorClass)
		}
		if emulator.Tag(map[string]string{jsonTarget.ID: "prod/app", perKey.ID: "prod/keys/APP_SHARED_KEY"}[id], adapter.SentinelName) != id {
			t.Fatalf("target %s did not tag its secret as owned", id)
		}
	}
	view, err := svc.InspectTarget(ctx, operator, scope, jsonTarget.ID)
	if err != nil || !strings.Contains(view.Workflow, `secret_id: "prod/app"`) {
		t.Fatalf("json-object consumption snippet = %q %v", view.Workflow, err)
	}

	// Plan is value-blind: the owned names read back as updates.
	plan, err := svc.Plan(ctx, operator, scope, perKey.ID)
	if err != nil || len(plan.Plan.Changes) != 1 || plan.Plan.Changes[0].Disposition != adapter.Update {
		t.Fatalf("per-key plan = %+v %v", plan.Plan.Changes, err)
	}

	// A new publish writes exactly one new version per owned secret.
	publish(awsSecretV2)
	drain()
	if got, _ := emulator.Value("prod/keys/APP_SHARED_KEY"); got != awsSecretV2 || emulator.Versions("prod/keys/APP_SHARED_KEY") != 2 {
		t.Fatalf("second publish value=%q versions=%d", got, emulator.Versions("prod/keys/APP_SHARED_KEY"))
	}

	// An external console edit moves AWSCURRENT without HIKYO_CURRENT: the
	// next converge refuses to overwrite and asks for an operator decision.
	emulator.ExternalPut("prod/app", `{"SHARED_KEY":"hand edited"}`)
	if _, err := svc.SyncTarget(ctx, operator, scope, jsonTarget.ID); err != nil {
		t.Fatalf("resync: %v", err)
	}
	drain()
	if got, _ := emulator.Value("prod/app"); got != `{"SHARED_KEY":"hand edited"}` {
		t.Fatalf("external update was blindly overwritten: %q", got)
	}
	drifted := inspect(jsonTarget.ID)
	if drifted.LastErrorClass != adapter.ErrorClassConflict || !drifted.DriftAttention || drifted.Health() == adapter.HealthConverged {
		t.Fatalf("drifted target = health %q class %q attention %v", drifted.Health(), drifted.LastErrorClass, drifted.DriftAttention)
	}
	if other := inspect(perKey.ID); other.Health() != adapter.HealthConverged {
		t.Fatalf("drift on one target leaked into another: %q", other.Health())
	}

	// Teardown schedules deletion with the recovery window and releases custody.
	for _, id := range []string{jsonTarget.ID, perKey.ID} {
		if _, err := svc.RemoveTarget(ctx, operator, scope, id, false); err != nil {
			t.Fatalf("remove %s: %v", id, err)
		}
	}
	clock = clock.Add(2 * adapter.LeaseTime)
	drain()
	if !emulator.Deleted("prod/keys/APP_SHARED_KEY") || !emulator.Deleted("prod/app") {
		t.Fatal("teardown did not schedule deletion of every owned secret")
	}
	if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state<>'released'`, perKey.ID)); got != 0 {
		t.Fatalf("per-key custody after teardown = %d rows", got)
	}

	// No value-returning operation ever reached the wire.
	for _, operation := range emulator.Operations() {
		if strings.Contains(operation, "GetSecretValue") || strings.Contains(operation, "GetRandomPassword") {
			t.Fatalf("value read on the wire: %s", operation)
		}
	}
	// Value-blind audit and INTENT/OUTCOME linkage.
	for _, plaintext := range []string{awsSecretV1, awsSecretV2, "first@db", "second@db", "e2e-secret-access-key"} {
		if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM audit_tenant_events WHERE payload LIKE '%%%s%%'`, strings.ReplaceAll(plaintext, "'", "''"))); got != 0 {
			t.Fatalf("audit payload carries plaintext %q", plaintext)
		}
	}
	linked := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects e JOIN audit_tenant_events i ON i.id=e.intent_audit_id JOIN audit_tenant_events o ON o.id=e.outcome_audit_id WHERE e.target_id='%s' AND i.type='adapter.push_intent' AND o.type='adapter.push_outcome'`, perKey.ID))
	if linked < 3 {
		t.Fatalf("per-key INTENT/OUTCOME pairs = %d, want create, update, and delete", linked)
	}

	for _, interruption := range []string{"lost-stage-response", "pre-promotion-refusal"} {
		t.Run("adoption/"+interruption, func(t *testing.T) {
			name := "prod/adopt-" + interruption
			emulator.Seed(name, "approved legacy", map[string]string{})
			target, err := svc.AddTarget(ctx, operator, scope, created.Adapter.ID, service.AdapterTargetInput{
				EnvironmentID: string(envA1), DestinationKind: string(adapter.JSONObject),
				DestinationOwner: awsAccount, DestinationName: name, KeySelection: selection,
			})
			if err != nil {
				t.Fatal(err)
			}
			planned, err := svc.Plan(ctx, operator, scope, target.ID)
			if err != nil || planned.ArtifactID == "" {
				t.Fatalf("fresh adoption artifact: %v", err)
			}
			if _, err := svc.Adopt(ctx, operator, scope, service.AdoptAdapterRequest{
				TargetID: target.ID, ArtifactID: planned.ArtifactID,
				ExpectedGeneration: target.Generation, ExpectedDestinationID: target.DestinationID,
				Entries: []store.AdapterConflictEntry{{Surface: string(adapter.Secret), EffectiveName: name}},
			}); err != nil {
				t.Fatalf("explicit adoption: %v", err)
			}
			interrupted := false
			afterStage = func(context.Context) error {
				interrupted = true
				afterStage = nil
				if interruption == "lost-stage-response" {
					return errors.New("response lost after staged value committed")
				}
				emulator.FailNext("UpdateSecretVersionStage", 503, "ServiceUnavailable", "")
				return nil
			}
			defer func() { afterStage = nil }()
			for attempt := 0; attempt < 8 && !interrupted; attempt++ {
				clock = clock.Add(time.Second)
				if worked, err := worker.RunOnce(ctx); err != nil || !worked {
					t.Fatalf("adoption worker before injected interruption: worked=%v err=%v", worked, err)
				}
			}
			if !interrupted || emulator.Versions(name) != 2 {
				t.Fatal("fixture did not actually stage the adopted value")
			}
			if got, _ := emulator.Value(name); got != "approved legacy" {
				t.Fatalf("promotion unexpectedly committed before retry: %q", got)
			}
			if count := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state='dispatched'`, target.ID)); count != 1 {
				t.Fatalf("ambiguous adoption did not persist Dispatched: %d", count)
			}
			clock = clock.Add(time.Minute)
			drain()
			got, _ := emulator.Value(name)
			var delivered map[string]string
			if err := json.Unmarshal([]byte(got), &delivered); err != nil || delivered["SHARED_KEY"] != awsSecretV2 || emulator.Versions(name) != 2 || inspect(target.ID).Health() != adapter.HealthConverged {
				t.Fatalf("adoption retry did not converge once: value=%q versions=%d err=%v", got, emulator.Versions(name), err)
			}
		})
	}

	for _, loss := range []string{"tag-removed", "resource-recreated"} {
		for _, state := range []string{"owned", "dispatched"} {
			for _, foreign := range []bool{false, true} {
				t.Run(fmt.Sprintf("fresh-held-adoption/%s/%s/foreign=%v", loss, state, foreign), func(t *testing.T) {
					name := fmt.Sprintf("prod/recover-%s-%s-%v", loss, state, foreign)
					target, err := svc.AddTarget(ctx, operator, scope, created.Adapter.ID, service.AdapterTargetInput{EnvironmentID: string(envA1), DestinationKind: string(adapter.JSONObject), DestinationOwner: awsAccount, DestinationName: name, KeySelection: selection})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := svc.SyncTarget(ctx, operator, scope, target.ID); err != nil {
						t.Fatal(err)
					}
					drain()
					if emulator.Versions(name) != 1 {
						t.Fatal("fixture did not establish previously delivered custody")
					}
					if state == "dispatched" {
						execRaw(t, db, fmt.Sprintf(`UPDATE adapter_ledger SET state='dispatched' WHERE target_id='%s'`, target.ID))
					}
					if loss == "tag-removed" {
						emulator.RemoveTag(name, adapter.SentinelName)
					} else {
						emulator.Recreate(name, "replacement value")
					}
					planned, err := svc.Plan(ctx, operator, scope, target.ID)
					if err != nil || planned.ArtifactID == "" || len(planned.Plan.Changes) != 1 || planned.Plan.Changes[0].Disposition != adapter.Conflict {
						t.Fatalf("fresh held adoption plan = %+v, %v", planned, err)
					}
					current := inspect(target.ID)
					adoption := service.AdoptAdapterRequest{TargetID: target.ID, ArtifactID: planned.ArtifactID, ExpectedGeneration: current.Generation, ExpectedDestinationID: current.DestinationID, Entries: []store.AdapterConflictEntry{{Surface: string(adapter.Secret), EffectiveName: name}}}
					// A fresh artifact cannot repurpose held custody for a
					// different provider origin or resolved destination.
					if state == "owned" && !foreign {
						for _, change := range []struct{ mutation, restore string }{
							{"provider_origin=provider_origin||'/foreign'", fmt.Sprintf("provider_origin='%s'", emulator.URL)},
							{"destination_id=destination_id+1", fmt.Sprintf("destination_id=%d", current.DestinationID)},
						} {
							execRaw(t, db, fmt.Sprintf(`UPDATE adapter_ledger SET %s WHERE target_id='%s'`, change.mutation, target.ID))
							if _, err := svc.Adopt(ctx, operator, scope, adoption); !errors.Is(err, domain.ErrConflict) {
								t.Fatalf("adoption rebound foreign custody: %v", err)
							}
							execRaw(t, db, fmt.Sprintf(`UPDATE adapter_ledger SET %s WHERE target_id='%s'`, change.restore, target.ID))
						}
					}
					if _, err := svc.Adopt(ctx, operator, scope, adoption); err != nil {
						t.Fatalf("fresh adoption of held custody: %v", err)
					}
					if foreign {
						afterStage = func(context.Context) error {
							afterStage = nil
							emulator.ExternalPut(name, "concurrent external value")
							return nil
						}
						defer func() { afterStage = nil }()
					}
					drain()
					if rows := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state<>'released'`, target.ID)); rows != 1 {
						t.Fatalf("held adoption duplicated or lost custody: %d", rows)
					}
					got, _ := emulator.Value(name)
					if foreign {
						if got != "concurrent external value" || inspect(target.ID).LastErrorClass != adapter.ErrorClassConflict {
							t.Fatalf("fresh adoption overwrote later foreign CAS: value=%q target=%+v", got, inspect(target.ID))
						}
						return
					}
					var delivered map[string]string
					if err := json.Unmarshal([]byte(got), &delivered); err != nil || delivered["SHARED_KEY"] != awsSecretV2 || emulator.Tag(name, adapter.SentinelName) != target.ID || inspect(target.ID).Health() != adapter.HealthConverged {
						t.Fatalf("fresh held adoption did not converge: value=%q err=%v", got, err)
					}
				})
			}
		}
	}

	t.Run("revocation-after-create-stops-plaintext", func(t *testing.T) {
		// The provider accepted creation, but authority is independently
		// withdrawn before its response reaches apply's next request boundary.
		// Exercise the real durable gate, not a simulated journal refusal.
		revoked, err := svc.AddTarget(ctx, operator, scope, created.Adapter.ID, service.AdapterTargetInput{
			EnvironmentID: string(envA1), DestinationKind: string(adapter.JSONObject),
			DestinationOwner: awsAccount, DestinationName: "prod/revoked", KeySelection: selection,
		})
		if err != nil {
			t.Fatal(err)
		}
		requestsAtRevocation := -1
		afterCreate = func(context.Context) error {
			execRaw(t, db, `DELETE FROM grants WHERE id='g_aws_reveal'`)
			requestsAtRevocation = len(emulator.Operations())
			afterCreate = nil
			return nil
		}
		defer func() {
			afterCreate = nil
			execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_aws_reveal','usr_alice','reveal','org_a','prj_a1','env_a1',`+ts+`)`)
		}()
		if _, err := svc.SyncTarget(ctx, operator, scope, revoked.ID); err != nil {
			t.Fatal(err)
		}
		drain()
		if requestsAtRevocation < 0 || len(emulator.Operations()) != requestsAtRevocation {
			t.Fatalf("provider requests continued after real grant revocation: at=%d operations=%v", requestsAtRevocation, emulator.Operations())
		}
		if _, delivered := emulator.Value("prod/revoked"); delivered {
			t.Fatal("plaintext delivered after real grant revocation")
		}
		if emulator.Tag("prod/revoked", adapter.SentinelName) != revoked.ID {
			t.Fatal("fixture did not actually create the tagged provider resource")
		}
		if count := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND effective_name='prod/revoked' AND state='dispatched'`, revoked.ID)); count != 1 {
			t.Fatalf("partially created resource lost durable custody: %d rows", count)
		}
	})
}

func TestAdapterAWSSecretsManagerLifecycle(t *testing.T) {
	forEngines(t, runAWSSecretsManagerLifecycle)
}
