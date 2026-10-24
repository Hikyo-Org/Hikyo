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
	var afterPromotion func(context.Context) error
	var finishAdoption func(string) error
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
		} else if afterPromotion != nil {
			api = &awsPromotedAdoptionClient{Client: client, afterPromotion: afterPromotion}
		} else if afterStage != nil {
			api = &awsInterruptedAdoptionClient{Client: client, afterStage: afterStage}
		}
		var module adapter.Module = &awssm.Module{API: api}
		if finishAdoption != nil {
			module = &awsAdoptionFinishModule{Module: module, finish: finishAdoption}
		}
		return adapter.NewModuleLease(module, client.Forget)
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

	// Initial adoption must retain the exact plan-time resource and version,
	// not infer consent from whichever value exists when the worker starts.
	for _, mutation := range []string{"external-edit", "replacement"} {
		for _, timing := range []string{"before-adopt", "after-adopt"} {
			t.Run("adoption-witness/"+mutation+"/"+timing, func(t *testing.T) {
				name := "prod/witness-" + mutation + "-" + timing
				emulator.Seed(name, "reviewed legacy", map[string]string{})
				target, err := svc.AddTarget(ctx, operator, scope, created.Adapter.ID, service.AdapterTargetInput{
					EnvironmentID: string(envA1), DestinationKind: string(adapter.JSONObject),
					DestinationOwner: awsAccount, DestinationName: name, KeySelection: selection,
				})
				if err != nil {
					t.Fatal(err)
				}
				planned, err := svc.Plan(ctx, operator, scope, target.ID)
				if err != nil || planned.ArtifactID == "" {
					t.Fatal("initial adoption plan failed")
				}
				mutate := func() {
					if mutation == "replacement" {
						emulator.Recreate(name, "unreviewed external")
					} else {
						emulator.ExternalPut(name, "unreviewed external")
					}
				}
				if timing == "before-adopt" {
					mutate()
				}
				if _, err := svc.Adopt(ctx, operator, scope, service.AdoptAdapterRequest{TargetID: target.ID, ArtifactID: planned.ArtifactID, ExpectedGeneration: target.Generation, ExpectedDestinationID: target.DestinationID, Entries: []store.AdapterConflictEntry{{Surface: string(adapter.Secret), EffectiveName: name}}}); err != nil {
					t.Fatal(err)
				}
				if timing == "after-adopt" {
					mutate()
				}
				versions := emulator.Versions(name)
				drain()
				state := inspect(target.ID)
				if got, _ := emulator.Value(name); got != "unreviewed external" || emulator.Versions(name) != versions || state.LastErrorClass != adapter.ErrorClassConflict || state.Health() == adapter.HealthConverged {
					t.Fatal("stale adoption witness allowed an unreviewed resource/version")
				}
				if queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state='owned'`, target.ID)) != 1 {
					t.Fatal("stale-witness refusal lost custody")
				}
				// A genuinely fresh plan may authorize this new resource/version.
				fresh, err := svc.Plan(ctx, operator, scope, target.ID)
				if err != nil || fresh.ArtifactID == "" {
					t.Fatal("fresh adoption plan failed")
				}
				if _, err := svc.Adopt(ctx, operator, scope, service.AdoptAdapterRequest{TargetID: target.ID, ArtifactID: fresh.ArtifactID, ExpectedGeneration: state.Generation, ExpectedDestinationID: state.DestinationID, Entries: []store.AdapterConflictEntry{{Surface: string(adapter.Secret), EffectiveName: name}}}); err != nil {
					t.Fatal(err)
				}
				drain()
				if inspect(target.ID).Health() != adapter.HealthConverged || emulator.Versions(name) != versions+1 {
					t.Fatal("fresh reviewed adoption did not recover exactly once")
				}
			})
		}
	}

	t.Run("legacy-adoption-matching-tag", func(t *testing.T) {
		name := "prod/legacy-adoption"
		emulator.Seed(name, "legacy value", map[string]string{})
		target, err := svc.AddTarget(ctx, operator, scope, created.Adapter.ID, service.AdapterTargetInput{
			EnvironmentID: string(envA1), DestinationKind: string(adapter.JSONObject),
			DestinationOwner: awsAccount, DestinationName: name, KeySelection: selection,
		})
		if err != nil {
			t.Fatal(err)
		}
		planned, err := svc.Plan(ctx, operator, scope, target.ID)
		if err != nil || planned.ArtifactID == "" {
			t.Fatalf("initial plan: %v", err)
		}
		adopt := func(artifact string, generation int64) {
			t.Helper()
			if _, err := svc.Adopt(ctx, operator, scope, service.AdoptAdapterRequest{TargetID: target.ID, ArtifactID: artifact, ExpectedGeneration: generation, ExpectedDestinationID: target.DestinationID, Entries: []store.AdapterConflictEntry{{Surface: string(adapter.Secret), EffectiveName: name}}}); err != nil {
				t.Fatal(err)
			}
		}
		adopt(planned.ArtifactID, target.Generation)
		// Reproduce an upgraded legacy artifact and the ownership tag that
		// landed before an interrupted first write. No successful effect exists.
		execRealAdoption(t, db, `UPDATE adapter_conflicts SET observed_provider_witness='' WHERE target_id=$1`, target.ID)
		emulator.Seed(name, "legacy value", map[string]string{adapter.SentinelName: target.ID})
		versions := emulator.Versions(name)
		drain()
		state := inspect(target.ID)
		if state.LastErrorClass != adapter.ErrorClassConflict || emulator.Versions(name) != versions {
			t.Fatal("legacy missing-witness adoption was not refused without writes")
		}
		if n := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state='owned'`, target.ID)); n != 1 {
			t.Fatal("legacy witness refusal lost owned custody")
		}
		fresh, err := svc.Plan(ctx, operator, scope, target.ID)
		if err != nil || fresh.ArtifactID == "" || len(fresh.Plan.Changes) != 1 || fresh.Plan.Changes[0].Disposition != adapter.Conflict {
			t.Fatalf("fresh plan cannot recover matching-tag legacy adoption: %+v %v", fresh.Plan, err)
		}
		if n := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_conflicts WHERE artifact_id='%s' AND observed_provider_witness<>'' AND adopted_at IS NULL`, fresh.ArtifactID)); n != 1 {
			t.Fatal("service did not persist fresh metadata witness")
		}
		if value, _ := emulator.Value(name); value != "legacy value" || emulator.Versions(name) != versions || emulator.Tag(name, adapter.SentinelName) != target.ID {
			t.Fatal("replanning changed legacy value or ownership")
		}
		adopt(fresh.ArtifactID, state.Generation)
		drain()
		if inspect(target.ID).Health() != adapter.HealthConverged || emulator.Versions(name) != versions+1 {
			t.Fatal("fresh explicit adoption did not recover exactly once")
		}
		value, _ := emulator.Value(name)
		var recovered map[string]string
		if err := json.Unmarshal([]byte(value), &recovered); err != nil || len(recovered) != 1 || recovered["SHARED_KEY"] != awsSecretV2 {
			t.Fatal("recovered adoption did not deliver the exact current snapshot")
		}
	})

	t.Run("stale-adoption-matching-tag", func(t *testing.T) {
		name := "prod/stale-adoption"
		emulator.Seed(name, "reviewed synthetic", map[string]string{})
		target, err := svc.AddTarget(ctx, operator, scope, created.Adapter.ID, service.AdapterTargetInput{
			EnvironmentID: string(envA1), DestinationKind: string(adapter.JSONObject),
			DestinationOwner: awsAccount, DestinationName: name, KeySelection: selection,
		})
		if err != nil {
			t.Fatal(err)
		}
		authenticated := *svc
		authenticated.Auth = authService(t, db)
		authenticated.Now = func() time.Time { return clock }
		actor := service.Bearer(seedSessionFactors(t, db, alice, `["password","totp"]`))
		sessionID := queryString(t, db, `SELECT id FROM sessions WHERE principal_id='usr_alice' ORDER BY created_at DESC LIMIT 1`)
		planned, err := authenticated.Plan(ctx, actor, scope, target.ID)
		if err != nil || len(planned.Plan.Changes) != 1 || planned.Plan.Changes[0].Disposition != adapter.Conflict {
			t.Fatalf("initial conflict plan: %+v %v", planned.Plan, err)
		}
		adopt := func(artifact string, generation int64) service.AdoptAdapterResult {
			t.Helper()
			request := service.AdoptAdapterRequest{TargetID: target.ID, ArtifactID: artifact, ExpectedGeneration: generation, ExpectedDestinationID: target.DestinationID, Entries: []store.AdapterConflictEntry{{Surface: string(adapter.Secret), EffectiveName: name}}}
			if _, err := authenticated.Adopt(ctx, actor, scope, request); !errors.Is(err, service.ErrReauthRequired) {
				t.Fatalf("adoption without a fresh ceremony: %v", err)
			}
			windowID := fmt.Sprintf("rw_stale_adoption_%d", generation)
			if err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				epoch, err := az.CredentialEpoch(ctx)
				if err != nil {
					return err
				}
				// Start after verification by a controlled authenticator. Keep the
				// zero-window policy and exercise actual single-decision consumption.
				if err := az.CreateWebAuthnCeremony(ctx, authz.NewWebAuthnCeremony{
					ID: windowID, SessionID: sessionID, EnvironmentID: string(envA1), Purpose: "reauth",
					ChallengeVerifier: []byte("verified-" + windowID), SessionData: []byte("{}"),
					OperationBinding: `{"purpose":"adapter","operation":"adapter.adopt","environment_id":"env_a1","environment_ids":["env_a1"]}`,
					CredentialEpoch:  epoch, ExpiresAt: clock.Add(time.Minute), CreatedAt: clock,
				}); err != nil {
					return err
				}
				return az.OpenReauthWindow(ctx, authz.NewReauthWindow{
					ID: windowID, SessionID: sessionID, EnvironmentID: string(envA1), CeremonyID: windowID, FactorClass: "webauthn", SingleDecision: true,
					AuthenticatedAt: clock, WindowExpiresAt: clock.Add(5 * time.Minute), HardExpiresAt: clock.Add(10 * time.Minute), CredentialEpoch: epoch, CreatedAt: clock,
					BoundPurpose: string(service.PurposeAdapter), BoundOperation: string(authz.OpAdapterAdopt), BoundEnvironmentSet: service.CanonicalEnvironmentSet([]string{string(envA1)}),
				})
			}); err != nil {
				t.Fatal(err)
			}
			result, err := authenticated.Adopt(ctx, actor, scope, request)
			if err != nil {
				t.Fatal(err)
			}
			if n := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM reauth_windows WHERE id='%s' AND consumed_at IS NOT NULL`, windowID)); n != 1 {
				t.Fatal("adoption did not consume its exact operation-bound ceremony")
			}
			return result
		}
		adopted := adopt(planned.ArtifactID, target.Generation)
		priorWitness := queryString(t, db, fmt.Sprintf(`SELECT observed_provider_witness FROM adapter_conflicts WHERE artifact_id='%s'`, planned.ArtifactID))
		if priorWitness == "" {
			t.Fatal("initial plan did not persist genuine consent")
		}
		// Preserve the actual adopted witness. Model the ownership tag landing
		// before a failed first write, then change only AWSCURRENT externally.
		emulator.Seed(name, "reviewed synthetic", map[string]string{adapter.SentinelName: target.ID})
		emulator.ExternalPut(name, "later synthetic")
		versions, before := emulator.Versions(name), len(emulator.Operations())
		assertNoWrites := func() {
			t.Helper()
			for _, operation := range emulator.Operations()[before:] {
				if operation != "sts:GetCallerIdentity" && operation != "DescribeSecret" && operation != "ListSecrets" {
					t.Fatalf("provider write before renewed consent: %s", operation)
				}
			}
			if value, _ := emulator.Value(name); value != "later synthetic" || emulator.Versions(name) != versions || emulator.Tag(name, adapter.SentinelName) != target.ID {
				t.Fatal("stale refusal or replanning changed the provider")
			}
		}
		drain()
		state := inspect(target.ID)
		if state.Generation != adopted.Generation || state.LastErrorClass != adapter.ErrorClassConflict || state.Health() == adapter.HealthConverged {
			t.Fatal("stale pending consent was not refused")
		}
		if n := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state='owned'`, target.ID)); n != 1 {
			t.Fatal("stale witness refusal lost owned custody")
		}
		if got := queryString(t, db, fmt.Sprintf(`SELECT observed_provider_witness FROM adapter_conflicts WHERE artifact_id='%s' AND adopted_at IS NOT NULL`, planned.ArtifactID)); got != priorWitness {
			t.Fatal("stale refusal rewrote historical consent")
		}
		assertNoWrites()
		fresh, err := authenticated.Plan(ctx, actor, scope, target.ID)
		if err != nil || len(fresh.Plan.Changes) != 1 || fresh.Plan.Changes[0].Disposition != adapter.Conflict {
			t.Fatalf("fresh plan cannot renew stale witness: %+v %v", fresh.Plan, err)
		}
		freshWitness := queryString(t, db, fmt.Sprintf(`SELECT observed_provider_witness FROM adapter_conflicts WHERE artifact_id='%s' AND adopted_at IS NULL`, fresh.ArtifactID))
		if freshWitness == "" || freshWitness == priorWitness || freshWitness != fresh.Plan.Changes[0].ObservedProviderWitness {
			t.Fatal("fresh service artifact did not retain its exact current metadata witness")
		}
		assertNoWrites()
		renewed := adopt(fresh.ArtifactID, state.Generation)
		if renewed.Generation != state.Generation+1 || renewed.JobID == adopted.JobID {
			t.Fatal("renewed consent did not commit a new generation and job")
		}
		before = len(emulator.Operations())
		drain()
		puts := 0
		for _, operation := range emulator.Operations()[before:] {
			if operation == "PutSecretValue" {
				puts++
			}
		}
		value, _ := emulator.Value(name)
		var recovered map[string]string
		if err := json.Unmarshal([]byte(value), &recovered); err != nil || len(recovered) != 1 || recovered["SHARED_KEY"] != awsSecretV2 || inspect(target.ID).Health() != adapter.HealthConverged || emulator.Versions(name) != versions+1 || puts != 1 {
			t.Fatal("renewed consent did not deliver the exact snapshot in one write")
		}
	})

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

	// A promotion may commit while the adopted ledger remains Owned and its
	// success outcome is absent. Replay must retain the original durable
	// witness and repair only this job's version, including after crash closure.
	for _, interruption := range []string{"finish-failure", "definite-marker-failure"} {
		for _, mutation := range []string{"unchanged", "foreign-current", "replacement"} {
			t.Run("promoted-adoption/"+interruption+"/"+mutation, func(t *testing.T) {
				name := "prod/promoted-" + interruption + "-" + mutation
				emulator.Seed(name, "reviewed legacy", map[string]string{})
				target, err := svc.AddTarget(ctx, operator, scope, created.Adapter.ID, service.AdapterTargetInput{
					EnvironmentID: string(envA1), DestinationKind: string(adapter.JSONObject),
					DestinationOwner: awsAccount, DestinationName: name, KeySelection: selection,
				})
				if err != nil {
					t.Fatal(err)
				}
				planned, err := svc.Plan(ctx, operator, scope, target.ID)
				if err != nil || planned.ArtifactID == "" {
					t.Fatal("fresh adoption artifact unavailable")
				}
				if _, err := svc.Adopt(ctx, operator, scope, service.AdoptAdapterRequest{TargetID: target.ID, ArtifactID: planned.ArtifactID, ExpectedGeneration: target.Generation, ExpectedDestinationID: target.DestinationID, Entries: []store.AdapterConflictEntry{{Surface: string(adapter.Secret), EffectiveName: name}}}); err != nil {
					t.Fatal(err)
				}
				interrupted := false
				if interruption == "finish-failure" {
					finishAdoption = func(id string) error {
						if id != target.ID || interrupted {
							return nil
						}
						interrupted = true
						return errors.New("injected journal transaction loss after promotion")
					}
				} else {
					afterPromotion = func(context.Context) error {
						interrupted = true
						afterPromotion = nil
						emulator.FailNext("UpdateSecretVersionStage", 400, "InvalidRequestException", "")
						return nil
					}
				}
				defer func() { finishAdoption, afterPromotion = nil, nil }()
				loader := &awsAdoptionReplayLoader{Loader: worker.Loader, t: t, targetID: target.ID}
				replayWorker := *worker
				replayWorker.Loader = loader
				for attempt := 0; attempt < 16 && !interrupted; attempt++ {
					clock = clock.Add(time.Second)
					// The injected Finish failure leaves the old fence unresolved; the
					// immediate retry can refuse it as superseded before crash recovery.
					if worked, err := replayWorker.RunOnce(ctx); !worked || (err != nil && !(interrupted && interruption == "finish-failure" && errors.Is(err, adapter.ErrSuperseded))) {
						t.Fatalf("before promoted interruption: worked=%v err=%v", worked, err)
					}
				}
				finishAdoption, afterPromotion = nil, nil
				if !interrupted || emulator.Versions(name) != 2 || queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state='owned'`, target.ID)) != 1 {
					t.Fatal("fixture did not retain Owned custody after promotion")
				}
				var delivered map[string]string
				got, _ := emulator.Value(name)
				if err := json.Unmarshal([]byte(got), &delivered); err != nil || delivered["SHARED_KEY"] != awsSecretV2 {
					t.Fatal("injected failure occurred before the value became current")
				}
				if mutation == "foreign-current" {
					emulator.ExternalPut(name, "later external")
				} else if mutation == "replacement" {
					emulator.Recreate(name, "later external")
				}
				versions := emulator.Versions(name)
				// Expire the actual provider fence as well as the worker lease.
				// ClaimDue must close an unfinished effect before LoadExecution.
				clock = clock.Add(adapter.LeaseTime + time.Minute)
				for attempt := 0; attempt < 16 && loader.loads < 2; attempt++ {
					if worked, err := replayWorker.RunOnce(ctx); err != nil || !worked {
						t.Fatalf("promoted replay: worked=%v err=%v", worked, err)
					}
				}
				if loader.loads != 2 || emulator.Versions(name) != versions {
					t.Fatal("replay did not load durable consent or duplicated a version")
				}
				if interruption == "finish-failure" && queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE target_id='%s' AND outcome='unknown'`, target.ID)) != 1 {
					t.Fatal("crash-window reconciliation did not close the unfinished effect")
				}
				state := inspect(target.ID)
				if mutation == "unchanged" {
					if state.Health() != adapter.HealthConverged {
						t.Fatal("same-job promotion could not repair metadata")
					}
				} else if got, _ := emulator.Value(name); got != "later external" || state.LastErrorClass != adapter.ErrorClassConflict || state.Health() == adapter.HealthConverged {
					t.Fatal("replay overwrote an unreviewed current version or replacement")
				}
				if queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state='owned'`, target.ID)) != 1 {
					t.Fatal("replay lost custody")
				}
			})
		}
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
