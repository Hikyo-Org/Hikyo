package app

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/config"
)

// These tests exercise the real AWS module over the dev-only in-memory peer,
// not AWS authentication or HTTP.
func devAWSFixture(t *testing.T, kind adapter.DestinationKind) (*devFakeProvider, adapter.Module, *devAWSAPI, adapter.Target, []adapter.ManifestEntry) {
	t.Helper()
	provider := newDevFakeProvider()
	lease, err := provider.factory(adapter.AWSSecretsManagerProvider, adapter.Config{Origin: "https://aws-fixture.example"}, `{"mode":"static","region":"eu-west-1","access_key_id":"AKIAHIKYOTEST0000001","secret_access_key":"e2e-aws-unowned"}`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	wrapper, ok := lease.Module.(*devAWSModule)
	if !ok {
		t.Fatal("AWS factory did not retain the provider-specific module")
	}
	destination := adapter.Destination{Kind: kind, Owner: "123456789012", Name: "browser/config", NumericID: 123456789012}
	if kind == adapter.PerKey {
		destination.Name = "browser/keys/"
	}
	target := adapter.Target{ID: "tgt_dev_aws", Generation: 7, Environment: "env_prod", NamePrefix: "AWS_UI_", Destination: destination}
	module, err := wrapper.module(destination)
	if err != nil {
		t.Fatal(err)
	}
	api, ok := module.API.(*devAWSAPI)
	if !ok {
		t.Fatal("AWS fixture peer missing")
	}
	connection, err := lease.Module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: destination, Gate: func(context.Context) error { return nil }})
	if err != nil || connection.DestinationID != 123456789012 || provider.hasState() {
		t.Fatalf("unowned fixture connection failed or seeded before Plan: %v", err)
	}
	manifest := []adapter.ManifestEntry{{KeyID: "key_token", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: "published-value-\n-é"}}
	return provider, lease.Module, api, target, manifest
}

func devAWSPlan(t *testing.T, module adapter.Module, target adapter.Target, manifest []adapter.ManifestEntry) adapter.Change {
	t.Helper()
	plan, err := module.Plan(t.Context(), adapter.PlanRequest{Target: target, Manifest: manifest, Gate: func(context.Context) error { return nil }})
	if err != nil || len(plan.Changes) != 1 {
		t.Fatalf("plan changes=%d error=%v", len(plan.Changes), err)
	}
	change := plan.Changes[0]
	if change.Disposition != adapter.Conflict || change.ObservedProviderWitness == "" {
		t.Fatal("real AWS plan did not bind the unowned resource metadata")
	}
	return change
}

func devAWSExternalVersion(api *devAWSAPI, name, version, value string) {
	api.provider.mu.Lock()
	defer api.provider.mu.Unlock()
	secret := api.store.secrets[name]
	secret.values[version] = value
	devAWSMoveStage(secret, version, "AWSCURRENT")
}

func TestDevAWSModuleAdoptsOnlyReviewedResourceAndVersion(t *testing.T) {
	for _, kind := range []adapter.DestinationKind{adapter.JSONObject, adapter.PerKey} {
		for _, mutation := range []string{"none", "no consent", "version", "replacement", "other target"} {
			t.Run(string(kind)+"/"+mutation, func(t *testing.T) {
				provider, module, api, target, manifest := devAWSFixture(t, kind)
				change := devAWSPlan(t, module, target, manifest)
				name := change.EffectiveName
				before, err := api.DescribeSecret(t.Context(), name)
				if err != nil {
					t.Fatal(err)
				}
				switch mutation {
				case "version":
					devAWSExternalVersion(api, name, "external-after-plan", "preserve-external")
				case "replacement":
					provider.mu.Lock()
					delete(api.store.secrets, name)
					provider.mu.Unlock()
					if err := api.CreateSecret(t.Context(), awssm.CreateSecretInput{Name: name}); err != nil {
						t.Fatal(err)
					}
					devAWSExternalVersion(api, name, "external-fixture-version", "preserve-replacement")
					if _, err := api.DescribeSecret(t.Context(), before.ARN); !awssm.IsNotFound(err) {
						t.Fatal("old full ARN followed the same-name replacement")
					}
				case "other target":
					if err := api.TagSecret(t.Context(), name, map[string]string{adapter.SentinelName: "tgt_someone_else"}); err != nil {
						t.Fatal(err)
					}
				}
				journal := &devAWSJournal{}
				ledger := []adapter.LedgerEntry{{Surface: adapter.Secret, EffectiveName: name, State: adapter.Owned, AdoptionPending: true, AdoptionWitness: change.ObservedProviderWitness}}
				if mutation == "no consent" {
					ledger = nil
				}
				result, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest, JobID: "job_adopt", Ledger: ledger}, journal)
				api.provider.mu.Lock()
				secret := api.store.secrets[name]
				versions := len(secret.values)
				owner := secret.tags[adapter.SentinelName]
				currentValue := ""
				for version, stages := range secret.stages {
					if slices.Contains(stages, "AWSCURRENT") {
						currentValue = secret.values[version]
					}
				}
				api.provider.mu.Unlock()
				if mutation == "none" {
					if err != nil || len(result.Changes) != 1 || len(result.Conflicts) != 0 || journal.prepares != 1 || journal.successes != 1 || versions != 2 || owner != target.ID {
						t.Fatalf("reviewed adoption did not converge: error=%v changes=%d conflicts=%d prepares=%d successes=%d versions=%d", err, len(result.Changes), len(result.Conflicts), journal.prepares, journal.successes, versions)
					}
					want := manifest[0].Value
					if kind == adapter.JSONObject {
						var encodeErr error
						want, encodeErr = adapter.AWSJSONDocument(target.NamePrefix, manifest)
						if encodeErr != nil {
							t.Fatal(encodeErr)
						}
					}
					if currentValue != want {
						t.Fatal("dev peer did not preserve the module's exact delivered value")
					}
				} else if !errors.Is(err, adapter.ErrConflict) || len(result.Conflicts) != 1 || journal.prepares != 0 || journal.successes != 0 || owner == target.ID {
					t.Fatalf("changed metadata obtained authority: error=%v conflicts=%d prepares=%d successes=%d", err, len(result.Conflicts), journal.prepares, journal.successes)
				}
				if mutation != "none" {
					want, count := "external-fixture-value", 1
					if mutation == "version" {
						want, count = "preserve-external", 2
					} else if mutation == "replacement" {
						want = "preserve-replacement"
					}
					if currentValue != want || versions != count {
						t.Fatal("refused consent modified external value/version state")
					}
				}
				if !provider.hasState() {
					t.Fatal("AWS resource was invisible to the generation-switch guard")
				}
			})
		}
	}
}

func TestDevAWSUnownedFixtureSurvivesWorkerBeforePlan(t *testing.T) {
	for _, kind := range []adapter.DestinationKind{adapter.JSONObject, adapter.PerKey} {
		t.Run(string(kind), func(t *testing.T) {
			_, module, api, target, manifest := devAWSFixture(t, kind)
			journal := &devAWSJournal{}
			result, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest, JobID: "job_before_plan"}, journal)
			if !errors.Is(err, adapter.ErrConflict) || len(result.Conflicts) != 1 || journal.prepares != 0 || journal.successes != 0 {
				t.Fatalf("worker before Plan did not preserve foreign resource: error=%v conflicts=%d prepares=%d successes=%d", err, len(result.Conflicts), journal.prepares, journal.successes)
			}
			change := devAWSPlan(t, module, target, manifest)
			metadata, err := api.DescribeSecret(t.Context(), change.EffectiveName)
			if err != nil || metadata.Tags[adapter.SentinelName] != "" || !slices.Contains(metadata.Stages["external-fixture-version"], "AWSCURRENT") {
				t.Fatal("worker replaced the pre-existing fixture before operator consent")
			}
		})
	}
}

func TestDevAWSWorkerDoesNotSeedWithoutAuthorizedValidDelivery(t *testing.T) {
	for _, refusal := range []string{"gate", "manifest", "journal", "teardown"} {
		t.Run(refusal, func(t *testing.T) {
			provider, module, _, target, manifest := devAWSFixture(t, adapter.JSONObject)
			request := adapter.SyncRequest{Target: target, Manifest: manifest, JobID: "job_refused_seed"}
			journal := &devAWSJournal{}
			var supplied adapter.Journal = journal
			switch refusal {
			case "gate":
				journal.gateErr = adapter.ErrUnauthorized
			case "manifest":
				request.Manifest[0].CanonicalName = "invalid-lowercase"
			case "journal":
				supplied = nil
			case "teardown":
				request.Teardown = true
			}
			_, err := module.Sync(t.Context(), request, supplied)
			if refusal != "teardown" && err == nil {
				t.Fatal("invalid fixture delivery was accepted")
			}
			if provider.hasState() || journal.prepares != 0 || journal.successes != 0 {
				t.Fatal("fixture created remote state without authorized valid delivery")
			}
		})
	}
}

func TestDevAWSAPIConditionalPromotionReplayAndDeletion(t *testing.T) {
	provider, _, api, _, _ := devAWSFixture(t, adapter.JSONObject)
	ctx := t.Context()
	if provider.hasState() {
		t.Fatal("empty AWS namespace claimed remote state")
	}
	if err := api.CreateSecret(ctx, awssm.CreateSecretInput{Name: "resource", KMSKeyID: "alias/fixture"}); err != nil {
		t.Fatal(err)
	}
	metadata, err := api.DescribeSecret(ctx, "resource")
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"first", "next"} {
		if err := api.PutSecretValue(ctx, metadata.ARN, version, version+"-value"); err != nil {
			t.Fatal(err)
		}
	}
	if err := api.PutSecretValue(ctx, metadata.ARN, "next", "next-value"); err != nil {
		t.Fatal("same-token replay failed")
	}
	if err := api.PutSecretValue(ctx, metadata.ARN, "next", "different-value"); err == nil {
		t.Fatal("same token accepted a different value")
	}
	if err := api.UpdateSecretVersionStage(ctx, metadata.ARN, "AWSCURRENT", "next", "wrong-predecessor"); err == nil {
		t.Fatal("conditional promotion ignored its predecessor")
	}
	after, err := api.DescribeSecret(ctx, metadata.ARN)
	if err != nil || !slices.Contains(after.Stages["first"], "AWSCURRENT") || slices.Contains(after.Stages["next"], "AWSCURRENT") {
		t.Fatal("refused promotion moved current")
	}
	if err := api.UpdateSecretVersionStage(ctx, metadata.ARN, "AWSCURRENT", "next", "first"); err != nil {
		t.Fatal(err)
	}
	// Caller-owned metadata cannot change the provider's witnesses or tags.
	after.Stages["first"][0] = "tampered"
	after.Tags["injected"] = "tampered"
	fresh, err := api.DescribeSecret(ctx, metadata.ARN)
	if err != nil || fresh.Tags["injected"] != "" || slices.Contains(fresh.Stages["first"], "tampered") {
		t.Fatal("metadata aliases the provider store")
	}
	if err := api.DeleteSecret(ctx, metadata.ARN); err != nil {
		t.Fatal(err)
	}
	owner := &ownerRuntime{fakeProvider: provider}
	if err := owner.checkDevelopmentProviderSwitch(ctx, &config.Config{Dev: true}); err == nil {
		t.Fatal("generation switch abandoned scheduled-deleted AWS state")
	}
	names, err := api.ListSecretNames(ctx, "res", 0)
	if err != nil || !slices.Equal(names, []string{"resource"}) || !provider.hasState() {
		t.Fatal("planned-deletion listing lost retained resource identity")
	}
	if err := api.CreateSecret(ctx, awssm.CreateSecretInput{Name: "resource"}); !awssm.IsExists(err) {
		t.Fatal("scheduled deletion released its name")
	}
	if err := api.PutSecretValue(ctx, metadata.ARN, "blocked", "blocked"); err == nil {
		t.Fatal("scheduled-deleted secret accepted a value")
	}
	if err := api.RestoreSecret(ctx, metadata.ARN); err != nil {
		t.Fatal(err)
	}
	fresh, err = api.DescribeSecret(ctx, "resource")
	if err != nil || fresh.Deleted || fresh.ARN != metadata.ARN || fresh.KMSKeyID != "alias/fixture" {
		t.Fatal("restore changed identity, KMS or deletion state")
	}
}

func TestDevAWSNamespaceAndSeedAreStableAcrossLeases(t *testing.T) {
	provider, module, api, target, manifest := devAWSFixture(t, adapter.PerKey)
	if _, err := module.Plan(t.Context(), adapter.PlanRequest{Target: target, Manifest: manifest, Gate: func(context.Context) error { return adapter.ErrUnauthorized }}); !errors.Is(err, adapter.ErrUnauthorized) || provider.hasState() {
		t.Fatal("unauthorized plan seeded remote fixture state")
	}
	change := devAWSPlan(t, module, target, manifest)
	devAWSExternalVersion(api, change.EffectiveName, "external-after-plan", "preserve-this")
	second := devAWSPlan(t, module, target, manifest)
	if second.ObservedProviderWitness == change.ObservedProviderWitness {
		t.Fatal("repeated plan reseeded or hid the external current version")
	}
	for _, credential := range []string{"revoked", `{"mode":"ambient"}`, `{"mode":"static","region":"eu-west-1","access_key_id":"bad","secret_access_key":"do-not-echo"}`} {
		if _, err := provider.factory(adapter.AWSSecretsManagerProvider, adapter.Config{Origin: "https://aws-fixture.example"}, credential); err == nil {
			t.Fatal("invalid dev AWS descriptor was accepted")
		}
	}
	lease, err := provider.factory(adapter.AWSSecretsManagerProvider, adapter.Config{Origin: "https://aws-fixture.example"}, `{"mode":"ambient","region":"eu-west-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	third := devAWSPlan(t, lease.Module, target, manifest)
	if third.ObservedProviderWitness != second.ObservedProviderWitness {
		t.Fatal("credential replacement abandoned the existing namespace")
	}
	// Destination kind and KMS do not split one AWS account's name namespace.
	wrapper := &devAWSModule{provider: provider, origin: "https://aws-fixture.example", region: "eu-west-1"}
	other := target.Destination
	other.Kind, other.Name, other.Environment = adapter.JSONObject, change.EffectiveName, "alias/different"
	peer, err := wrapper.module(other)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := peer.Plan(t.Context(), adapter.PlanRequest{Target: adapter.Target{ID: "tgt_other", Destination: other}, Manifest: manifest, Gate: func(context.Context) error { return nil }})
	if err != nil || len(plan.Changes) != 1 || plan.Changes[0].ObservedProviderWitness != second.ObservedProviderWitness {
		t.Fatal("destination kind or KMS split the same resource namespace")
	}
	for _, separation := range []string{"origin", "region", "account"} {
		isolated := &devAWSModule{provider: provider, origin: wrapper.origin, region: wrapper.region}
		destination := other
		switch separation {
		case "origin":
			isolated.origin = "https://different-aws-fixture.example"
		case "region":
			isolated.region = "eu-central-1"
		case "account":
			destination.Owner, destination.NumericID = "999999999999", 999999999999
		}
		module, err := isolated.module(destination)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := module.Plan(t.Context(), adapter.PlanRequest{Target: adapter.Target{ID: "tgt_isolated", Destination: destination}, Manifest: manifest, Gate: func(context.Context) error { return nil }})
		if err != nil || len(plan.Changes) != 1 || plan.Changes[0].Disposition != adapter.Create || plan.Changes[0].ObservedProviderWitness != "" {
			t.Fatalf("%s namespace borrowed another resource: %v", separation, err)
		}
	}
}

func TestDevAWSSeedNeverOverwritesAnExistingResource(t *testing.T) {
	_, module, api, target, manifest := devAWSFixture(t, adapter.JSONObject)
	name := target.Destination.Name
	if err := api.CreateSecret(t.Context(), awssm.CreateSecretInput{Name: name, KMSKeyID: "alias/existing"}); err != nil {
		t.Fatal(err)
	}
	devAWSExternalVersion(api, name, "pre-existing-version", "pre-existing-value")
	before, err := api.DescribeSecret(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	devAWSPlan(t, module, target, manifest)
	after, err := api.DescribeSecret(t.Context(), name)
	if err != nil || after.ARN != before.ARN || after.KMSKeyID != before.KMSKeyID || !slices.Contains(after.Stages["pre-existing-version"], "AWSCURRENT") || len(after.Stages) != 1 {
		t.Fatal("unowned fixture marker overwrote an existing resource")
	}
}

func TestDevAWSFactoryKeepsOtherProvidersOnTheirExistingFake(t *testing.T) {
	provider := newDevFakeProvider()
	for _, kind := range adapter.SupportedProviders() {
		if kind == adapter.AWSSecretsManagerProvider {
			continue
		}
		t.Run(string(kind), func(t *testing.T) {
			lease, err := provider.factory(kind, adapter.Config{Origin: "https://generic-fixture.example"}, "generic-fixture-credential")
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Release()
			if _, ok := lease.Module.(*devFakeModule); !ok {
				t.Fatal("AWS dispatch replaced another provider's existing fake")
			}
			destination := adapter.Destination{Kind: adapter.Repository, Owner: "dev", Name: "repo"}
			connection, err := lease.Module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: destination})
			if err != nil || connection.Version != "dev-fake" || connection.DestinationID != devFakeDestinationID(destination) {
				t.Fatal("another provider's fake connection behavior changed")
			}
		})
	}
}

func TestDevAWSMarkerDoesNotEnableAProductionFallback(t *testing.T) {
	factory := newAdapterModuleFactory(nil, nil, adapterProviderPolicy{})
	lease, err := factory.Build(adapter.AWSSecretsManagerProvider, adapter.Config{Origin: "https://secretsmanager.eu-west-1.amazonaws.com"}, "e2e-aws-unowned")
	if lease != nil {
		lease.Release()
	}
	if err == nil || lease != nil {
		t.Fatal("production construction accepted the development fixture marker")
	}
	// A valid descriptor carrying the marker must still construct the real
	// production client. Inspect the lease only; no provider request is made.
	lease, err = factory.Build(adapter.AWSSecretsManagerProvider, adapter.Config{Origin: "https://secretsmanager.eu-west-1.amazonaws.com"}, `{"mode":"static","region":"eu-west-1","access_key_id":"AKIAHIKYOTEST0000001","secret_access_key":"e2e-aws-unowned"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	module, ok := lease.Module.(*awssm.Module)
	if !ok {
		t.Fatal("valid production descriptor selected a development module")
	}
	if _, ok := module.API.(*awssm.Client); !ok {
		t.Fatal("production AWS module selected a development API")
	}
}

type devAWSJournal struct {
	prepares, successes int
	gateErr             error
}

func (j *devAWSJournal) Gate(context.Context, adapter.Effect) error { return j.gateErr }
func (*devAWSJournal) Reserve(context.Context, adapter.Effect) (adapter.LedgerState, error) {
	return adapter.Reserved, nil
}
func (j *devAWSJournal) Prepare(context.Context, adapter.Effect, adapter.LedgerState) error {
	j.prepares++
	return nil
}
func (j *devAWSJournal) Finish(_ context.Context, _ adapter.Effect, completion adapter.Completion) error {
	if completion.Outcome == adapter.OutcomeSuccess {
		j.successes++
	}
	return adapter.ValidateCompletion(completion)
}
func (*devAWSJournal) Refuse(context.Context, adapter.Effect) error             { return nil }
func (*devAWSJournal) ReleaseReservation(context.Context, adapter.Effect) error { return nil }

func TestDevAWSPerKeyPlannedDeletionRequiresAdoptionAndRestoresIdentity(t *testing.T) {
	_, module, api, target, manifest := devAWSFixture(t, adapter.PerKey)
	change := devAWSPlan(t, module, target, manifest)
	before, err := api.DescribeSecret(t.Context(), change.EffectiveName)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.DeleteSecret(t.Context(), before.ARN); err != nil {
		t.Fatal(err)
	}
	deleted := devAWSPlan(t, module, target, manifest)
	if deleted.EffectiveName != change.EffectiveName || deleted.ObservedProviderWitness != change.ObservedProviderWitness {
		t.Fatal("scheduled deletion hid the unowned resource or changed its identity/version witness")
	}
	journal := &devAWSJournal{}
	request := adapter.SyncRequest{Target: target, Manifest: manifest, JobID: "job_deleted_adopt"}
	result, err := module.Sync(t.Context(), request, journal)
	if !errors.Is(err, adapter.ErrConflict) || len(result.Conflicts) != 1 || journal.prepares != 0 {
		t.Fatal("scheduled-deleted unowned resource bypassed explicit adoption")
	}
	stillDeleted, err := api.DescribeSecret(t.Context(), before.ARN)
	if err != nil || !stillDeleted.Deleted || stillDeleted.ARN != before.ARN {
		t.Fatal("refused adoption changed scheduled-deleted resource identity")
	}
	request.Ledger = []adapter.LedgerEntry{{Surface: adapter.Secret, EffectiveName: deleted.EffectiveName, State: adapter.Owned, AdoptionPending: true, AdoptionWitness: deleted.ObservedProviderWitness}}
	result, err = module.Sync(t.Context(), request, journal)
	if err != nil || len(result.Changes) != 1 || len(result.Conflicts) != 0 || journal.prepares != 1 || journal.successes != 1 {
		t.Fatal("consented scheduled-deleted resource did not restore and converge")
	}
	after, err := api.DescribeSecret(t.Context(), before.ARN)
	if err != nil || after.Deleted || after.ARN != before.ARN || after.Tags[adapter.SentinelName] != target.ID {
		t.Fatal("restored adoption lost exact resource identity or ownership")
	}
	api.provider.mu.Lock()
	defer api.provider.mu.Unlock()
	secret := api.store.secrets[deleted.EffectiveName]
	current := ""
	for version, labels := range secret.stages {
		if slices.Contains(labels, "AWSCURRENT") {
			current = secret.values[version]
		}
	}
	if len(secret.values) != 2 || current != manifest[0].Value {
		t.Fatal("restored adoption did not deliver the exact value in one new version")
	}
}
