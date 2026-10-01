package awssm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

const (
	testAccount = "123456789012"
	testRegion  = "eu-west-1"
	testTarget  = "tgt_aws"
)

type fakeSecret struct {
	tags    map[string]string
	stages  map[string][]string
	values  map[string]string
	kms     string
	deleted bool
}

// fakeAPI models the Secrets Manager behaviour the module depends on:
// idempotency tokens become version ids, VersionStages move atomically, a
// deleted secret keeps its name, and nothing can return a value.
type fakeAPI struct {
	account  string
	secrets  map[string]*fakeSecret
	calls    []string
	failOn   map[string]error
	failOnce map[string]error
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{account: testAccount, secrets: map[string]*fakeSecret{}, failOn: map[string]error{}, failOnce: map[string]error{}}
}

func (f *fakeAPI) fail(op, name string) error {
	key := op + ":" + name
	f.calls = append(f.calls, key)
	if err, ok := f.failOnce[key]; ok {
		delete(f.failOnce, key)
		return err
	}
	return f.failOn[key]
}

func (f *fakeAPI) Region() string { return testRegion }

func (f *fakeAPI) ResolveIdentity(context.Context) (Identity, error) {
	if err := f.fail("identity", ""); err != nil {
		return Identity{}, err
	}
	return Identity{Account: f.account, ARN: "arn:aws:iam::" + f.account + ":role/hikyo"}, nil
}

func (f *fakeAPI) DescribeSecret(_ context.Context, name string) (SecretMetadata, error) {
	if err := f.fail("describe", name); err != nil {
		return SecretMetadata{}, err
	}
	secret, ok := f.secrets[name]
	if !ok {
		return SecretMetadata{}, &ResponseError{Status: 400, Code: "ResourceNotFoundException"}
	}
	stages := map[string][]string{}
	for version, labels := range secret.stages {
		stages[version] = slices.Clone(labels)
	}
	tags := map[string]string{}
	for k, v := range secret.tags {
		tags[k] = v
	}
	return SecretMetadata{ARN: "arn:aws:secretsmanager:" + testRegion + ":" + testAccount + ":secret:" + name + "-AbCdEf", Name: name, KMSKeyID: secret.kms, Deleted: secret.deleted, Tags: tags, Stages: stages}, nil
}

func (f *fakeAPI) ListSecretNames(_ context.Context, prefix string, limit int) ([]string, error) {
	if err := f.fail("list", prefix); err != nil {
		return nil, err
	}
	var out []string
	for name := range f.secrets {
		if strings.HasPrefix(name, prefix) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeAPI) CreateSecret(_ context.Context, input CreateSecretInput) error {
	if err := f.fail("create", input.Name); err != nil {
		return err
	}
	if _, ok := f.secrets[input.Name]; ok {
		return &ResponseError{Status: 400, Code: "ResourceExistsException"}
	}
	tags := map[string]string{}
	for k, v := range input.Tags {
		tags[k] = v
	}
	f.secrets[input.Name] = &fakeSecret{tags: tags, stages: map[string][]string{}, values: map[string]string{}, kms: input.KMSKeyID}
	return nil
}

func (f *fakeAPI) PutSecretValue(_ context.Context, name, token, value string) error {
	if err := f.fail("put", name); err != nil {
		return err
	}
	secret, ok := f.secrets[name]
	if !ok {
		return &ResponseError{Status: 400, Code: "ResourceNotFoundException"}
	}
	if secret.deleted {
		return &ResponseError{Status: 400, Code: "InvalidRequestException"}
	}
	if prior, ok := secret.values[token]; ok {
		if prior != value {
			return &ResponseError{Status: 400, Code: "ResourceExistsException"}
		}
		return nil
	}
	first := len(secret.values) == 0
	secret.values[token] = value
	f.moveStages(secret, token, PendingStage)
	if first {
		f.moveStages(secret, token, awsCurrent)
	}
	return nil
}

func (f *fakeAPI) UpdateSecretVersionStage(_ context.Context, name, stage, moveTo, removeFrom string) error {
	if err := f.fail("stage-"+stage, name); err != nil {
		return err
	}
	secret, ok := f.secrets[name]
	if !ok {
		return &ResponseError{Status: 400, Code: "ResourceNotFoundException"}
	}
	if secret.deleted {
		return &ResponseError{Status: 400, Code: "InvalidRequestException"}
	}
	if _, exists := secret.values[moveTo]; !exists {
		return &ResponseError{Status: 400, Code: "InvalidParameterException"}
	}
	owner := ""
	for id, stages := range secret.stages {
		if slices.Contains(stages, stage) {
			owner = id
		}
	}
	if (owner != "" && owner != moveTo && removeFrom != owner) || (removeFrom != "" && removeFrom != owner) {
		return &ResponseError{Status: 400, Code: "InvalidParameterException"}
	}
	f.moveStages(secret, moveTo, stage)
	if stage == awsCurrent && owner != "" && owner != moveTo {
		f.moveStages(secret, owner, "AWSPREVIOUS")
	}
	return nil
}

func (f *fakeAPI) moveStages(secret *fakeSecret, version string, stages ...string) {
	for id, labels := range secret.stages {
		secret.stages[id] = slices.DeleteFunc(labels, func(label string) bool { return slices.Contains(stages, label) })
	}
	secret.stages[version] = append(secret.stages[version], stages...)
}

// externalPut is what an operator's `aws secretsmanager put-secret-value`
// does: it moves only AWSCURRENT.
func (f *fakeAPI) externalPut(name, version, value string) {
	secret := f.secrets[name]
	secret.values[version] = value
	f.moveStages(secret, version, awsCurrent)
}

func (f *fakeAPI) TagSecret(_ context.Context, name string, tags map[string]string) error {
	if err := f.fail("tag", name); err != nil {
		return err
	}
	for k, v := range tags {
		f.secrets[name].tags[k] = v
	}
	return nil
}

func (f *fakeAPI) RestoreSecret(_ context.Context, name string) error {
	if err := f.fail("restore", name); err != nil {
		return err
	}
	f.secrets[name].deleted = false
	return nil
}

func (f *fakeAPI) DeleteSecret(_ context.Context, name string) error {
	if err := f.fail("delete", name); err != nil {
		return err
	}
	secret, ok := f.secrets[name]
	if !ok {
		return &ResponseError{Status: 400, Code: "ResourceNotFoundException"}
	}
	secret.deleted = true
	return nil
}

func (f *fakeAPI) current(name string) string {
	secret := f.secrets[name]
	for version, labels := range secret.stages {
		if slices.Contains(labels, awsCurrent) {
			return secret.values[version]
		}
	}
	return ""
}

func (f *fakeAPI) writes() []string {
	var out []string
	for _, call := range f.calls {
		if !strings.HasPrefix(call, "describe:") && !strings.HasPrefix(call, "identity:") && !strings.HasPrefix(call, "list:") {
			out = append(out, call)
		}
	}
	return out
}

type fakeJournal struct {
	states   map[string]adapter.LedgerState
	intents  []string
	outcomes []adapter.Outcome
	refusals int
	gateErr  error
}

func newFakeJournal() *fakeJournal { return &fakeJournal{states: map[string]adapter.LedgerState{}} }

func (j *fakeJournal) Gate(context.Context, adapter.Effect) error { return j.gateErr }

func (j *fakeJournal) Reserve(_ context.Context, effect adapter.Effect) (adapter.LedgerState, error) {
	if state, ok := j.states[effect.EffectiveName]; ok {
		return state, nil
	}
	j.states[effect.EffectiveName] = adapter.Reserved
	return adapter.Reserved, nil
}

func (j *fakeJournal) Prepare(_ context.Context, effect adapter.Effect, _ adapter.LedgerState) error {
	j.intents = append(j.intents, string(effect.Disposition)+":"+effect.EffectiveName)
	j.states[effect.EffectiveName] = adapter.Dispatched
	return nil
}

func (j *fakeJournal) Finish(_ context.Context, effect adapter.Effect, completion adapter.Completion) error {
	if err := adapter.ValidateCompletion(completion); err != nil {
		return err
	}
	j.outcomes = append(j.outcomes, completion.Outcome)
	if completion.ReleaseLedger {
		delete(j.states, effect.EffectiveName)
	} else {
		j.states[effect.EffectiveName] = completion.State
	}
	return nil
}

func (j *fakeJournal) Refuse(_ context.Context, effect adapter.Effect) error {
	j.refusals++
	delete(j.states, effect.EffectiveName)
	return nil
}

func (j *fakeJournal) ReleaseReservation(_ context.Context, effect adapter.Effect) error {
	if j.states[effect.EffectiveName] != adapter.Reserved {
		return adapter.ErrSuperseded
	}
	delete(j.states, effect.EffectiveName)
	return nil
}

func (j *fakeJournal) ledger() []adapter.LedgerEntry {
	var out []adapter.LedgerEntry
	for name, state := range j.states {
		if state != adapter.Released {
			out = append(out, adapter.LedgerEntry{Surface: adapter.Secret, EffectiveName: name, State: state})
		}
	}
	return out
}

func perKeyTarget() adapter.Target {
	return adapter.Target{ID: testTarget, Environment: "env_prod", Generation: 1, NamePrefix: "APP_", Destination: adapter.Destination{Kind: adapter.PerKey, Owner: testAccount, Name: "prod/", NumericID: 123456789012}}
}

func jsonTarget() adapter.Target {
	return adapter.Target{ID: testTarget, Environment: "env_prod", Generation: 1, Destination: adapter.Destination{Kind: adapter.JSONObject, Owner: testAccount, Name: "prod/app", NumericID: 123456789012, Environment: "alias/hikyo"}}
}

func manifest() []adapter.ManifestEntry {
	return []adapter.ManifestEntry{
		{KeyID: "key_db", CanonicalName: "DATABASE_URL", Classification: adapter.SecretClassification, Value: "postgres://u:p@db/app?x=<1>&y"},
		{KeyID: "key_log", CanonicalName: "LOG_LEVEL", Classification: adapter.ConfigClassification, Value: "debug\nmultiline é"},
	}
}

func TestPerKeyClaimUpdatePruneAndTeardown(t *testing.T) {
	api, journal := newFakeAPI(), newFakeJournal()
	module := &Module{API: api}
	target := perKeyTarget()
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest(), JobID: "job_1"}, journal); err != nil {
		t.Fatal(err)
	}
	for _, entry := range manifest() {
		name := "prod/APP_" + entry.CanonicalName
		if got := api.current(name); got != entry.Value {
			t.Fatalf("%s = %q, want byte-exact %q", name, got, entry.Value)
		}
		if api.secrets[name].tags[adapter.SentinelName] != testTarget {
			t.Fatalf("%s is missing the ownership tag: %v", name, api.secrets[name].tags)
		}
		if journal.states[name] != adapter.Owned {
			t.Fatalf("%s ledger = %q", name, journal.states[name])
		}
	}

	// A second job writes a new version; the first job's replay does not.
	next := manifest()
	next[0].Value = "rotated"
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: next[:1], Ledger: journal.ledger(), JobID: "job_2"}, journal); err != nil {
		t.Fatal(err)
	}
	if got := api.current("prod/APP_DATABASE_URL"); got != "rotated" {
		t.Fatalf("update = %q", got)
	}
	if !api.secrets["prod/APP_LOG_LEVEL"].deleted || journal.states["prod/APP_LOG_LEVEL"] != adapter.Released {
		t.Fatalf("deselected key was not pruned with a recovery window: %+v %q", api.secrets["prod/APP_LOG_LEVEL"], journal.states["prod/APP_LOG_LEVEL"])
	}

	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Ledger: journal.ledger(), Teardown: true, JobID: "job_3"}, journal); err != nil {
		t.Fatal(err)
	}
	if !api.secrets["prod/APP_DATABASE_URL"].deleted {
		t.Fatal("teardown did not schedule deletion")
	}
	for name, state := range journal.states {
		if state != adapter.Released {
			t.Fatalf("teardown left %s = %q", name, state)
		}
	}
}

func TestJSONObjectIsDeterministicAndCarriesKMSKey(t *testing.T) {
	api, journal := newFakeAPI(), newFakeJournal()
	if _, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "job_1"}, journal); err != nil {
		t.Fatal(err)
	}
	want := `{"DATABASE_URL":"postgres://u:p@db/app?x=<1>&y","LOG_LEVEL":"debug\nmultiline é"}`
	if got := api.current("prod/app"); got != want {
		t.Fatalf("document = %s, want %s", got, want)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(api.current("prod/app")), &decoded); err != nil || decoded["LOG_LEVEL"] != manifest()[1].Value {
		t.Fatalf("document does not round-trip: %v %v", decoded, err)
	}
	if api.secrets["prod/app"].kms != "alias/hikyo" {
		t.Fatalf("kms = %q", api.secrets["prod/app"].kms)
	}
	reversed := manifest()
	slices.Reverse(reversed)
	doc, err := adapter.AWSJSONDocument("", reversed)
	if err != nil || doc != want {
		t.Fatalf("manifest order changed the document: %s %v", doc, err)
	}
}

func TestReplayOfOneJobWritesOneVersion(t *testing.T) {
	api, journal := newFakeAPI(), newFakeJournal()
	module := &Module{API: api}
	target := jsonTarget()
	// The write lands but the response is lost: the outcome is unknown.
	api.failOnce["put:prod/app"] = errors.New("connection reset after request")
	api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{adapter.SentinelName: testTarget}, stages: map[string][]string{}, values: map[string]string{}}
	journal.states["prod/app"] = adapter.Owned
	_, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_1"}, journal)
	if !errors.Is(err, adapter.ErrIndeterminate) {
		t.Fatalf("lost response = %v, want indeterminate", err)
	}
	if journal.states["prod/app"] != adapter.Dispatched || journal.outcomes[len(journal.outcomes)-1] != adapter.OutcomeUnknown {
		t.Fatalf("ambiguous write settled as %q/%v, want dispatched/unknown", journal.states["prod/app"], journal.outcomes)
	}
	for range 2 {
		if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_1"}, journal); err != nil {
			t.Fatal(err)
		}
	}
	if versions := len(api.secrets["prod/app"].values); versions != 1 {
		t.Fatalf("replays of one job created %d versions, want 1", versions)
	}
	if journal.states["prod/app"] != adapter.Owned {
		t.Fatalf("replay ledger = %q", journal.states["prod/app"])
	}
}

func TestExistingUnownedSecretIsRefusedNotCaptured(t *testing.T) {
	api, journal := newFakeAPI(), newFakeJournal()
	api.secrets["prod/APP_DATABASE_URL"] = &fakeSecret{tags: map[string]string{}, stages: map[string][]string{"v1": {awsCurrent}}, values: map[string]string{"v1": "theirs"}}
	result, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: perKeyTarget(), Manifest: manifest(), JobID: "job_1"}, journal)
	if !errors.Is(err, adapter.ErrConflict) {
		t.Fatalf("err = %v, want exists-unowned", err)
	}
	if api.current("prod/APP_DATABASE_URL") != "theirs" || journal.refusals != 1 {
		t.Fatalf("unowned secret overwritten or not refused: %q refusals=%d", api.current("prod/APP_DATABASE_URL"), journal.refusals)
	}
	// Partial target failure is explicit: the other key still converged.
	if api.current("prod/APP_LOG_LEVEL") != manifest()[1].Value || len(result.Conflicts) != 1 || len(result.Changes) != 1 {
		t.Fatalf("per-key outcomes not independent: result=%+v", result)
	}
}

func TestSecretTaggedForAnotherTargetIsRefused(t *testing.T) {
	api, journal := newFakeAPI(), newFakeJournal()
	api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{adapter.SentinelName: "tgt_other"}, stages: map[string][]string{}, values: map[string]string{}}
	journal.states["prod/app"] = adapter.Owned
	_, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_1"}, journal)
	if !errors.Is(err, adapter.ErrConflict) || slices.Contains(api.writes(), "put:prod/app") {
		t.Fatalf("err=%v writes=%v", err, api.writes())
	}
}

func TestExternalUpdateFailsLoudWithoutOverwrite(t *testing.T) {
	// AWS keeps custom labels on superseded versions; some compatible
	// services (moto) drop them. The check must hold under both.
	for _, dropsLabels := range []bool{false, true} {
		t.Run(map[bool]string{false: "labels kept", true: "labels dropped"}[dropsLabels], func(t *testing.T) {
			api, journal := newFakeAPI(), newFakeJournal()
			module := &Module{API: api}
			if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "job_1"}, journal); err != nil {
				t.Fatal(err)
			}
			api.externalPut("prod/app", "console-edit", "hand edited")
			if dropsLabels {
				for version, labels := range api.secrets["prod/app"].stages {
					api.secrets["prod/app"].stages[version] = slices.DeleteFunc(labels, func(label string) bool { return label == CurrentStage })
				}
			}
			api.calls = nil
			_, err := module.Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_2"}, journal)
			if !errors.Is(err, adapter.ErrConflict) || adapter.ClassifyError(err) != adapter.ErrorClassConflict || !strings.Contains(err.Error(), VersionTag+"=console-edit") {
				t.Fatalf("external update = %v, want conflict naming the consent tag", err)
			}
			if api.current("prod/app") != "hand edited" || len(api.writes()) != 0 {
				t.Fatalf("blind overwrite: current=%q writes=%v", api.current("prod/app"), api.writes())
			}
			// The operator accepts overwriting exactly that version.
			api.secrets["prod/app"].tags[VersionTag] = "console-edit"
			if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_3"}, journal); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(api.current("prod/app"), "hand edited") {
				t.Fatal("accepted overwrite did not converge")
			}
			// A later edit is not covered by the earlier consent.
			api.externalPut("prod/app", "second-edit", "edited again")
			if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_4"}, journal); !errors.Is(err, adapter.ErrConflict) {
				t.Fatalf("second external edit = %v, want conflict", err)
			}
		})
	}
}

type writeRaceAPI struct {
	*fakeAPI
	beforePut bool
	afterPut  bool
}

func (f *writeRaceAPI) PutSecretValue(ctx context.Context, name, token, value string) error {
	if f.beforePut {
		f.beforePut = false
		f.externalPut(name, "concurrent-edit", "concurrent external value")
	}
	err := f.fakeAPI.PutSecretValue(ctx, name, token, value)
	if err == nil && f.afterPut {
		f.afterPut = false
		f.externalPut(name, "concurrent-edit", "concurrent external value")
	}
	return err
}

func TestConcurrentExternalWriteIsNotOverwritten(t *testing.T) {
	for _, timing := range []string{"before-put", "after-put"} {
		t.Run(timing, func(t *testing.T) {
			api, journal := newFakeAPI(), newFakeJournal()
			if _, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "initial"}, journal); err != nil {
				t.Fatal(err)
			}
			oldVersion := api.secrets["prod/app"].tags[VersionTag]
			race := &writeRaceAPI{fakeAPI: api, beforePut: timing == "before-put", afterPut: timing == "after-put"}
			result, err := (&Module{API: race}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "next"}, journal)
			if !errors.Is(err, adapter.ErrConflict) || len(result.Conflicts) != 1 || api.current("prod/app") != "concurrent external value" {
				t.Fatalf("concurrent edit overwritten or silently accepted: err=%v result=%+v current=%q", err, result, api.current("prod/app"))
			}
			if !slices.Contains(api.secrets["prod/app"].stages[oldVersion], CurrentStage) || api.secrets["prod/app"].tags[VersionTag] != oldVersion {
				t.Fatal("refused promotion discarded the previous Hikyo ownership evidence")
			}
			api.secrets["prod/app"].tags[VersionTag] = "concurrent-edit"
			if _, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "consented"}, journal); err != nil {
				t.Fatalf("explicit consent to the observed external version failed: %v", err)
			}
		})
	}
}

func TestConcurrentFirstVersionIsNotOverwritten(t *testing.T) {
	for _, timing := range []string{"before-put", "after-put"} {
		t.Run(timing, func(t *testing.T) {
			api, journal := newFakeAPI(), newFakeJournal()
			race := &writeRaceAPI{fakeAPI: api, beforePut: timing == "before-put", afterPut: timing == "after-put"}
			result, err := (&Module{API: race}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "initial"}, journal)
			if !errors.Is(err, adapter.ErrConflict) || len(result.Conflicts) != 1 || api.current("prod/app") != "concurrent external value" {
				t.Fatalf("first-version concurrent edit lost: err=%v result=%+v current=%q", err, result, api.current("prod/app"))
			}
			if api.secrets["prod/app"].tags[VersionTag] != "" {
				t.Fatal("failed first-version promotion recorded ownership of the external version")
			}
		})
	}
}

type lostWriteResponseAPI struct {
	*fakeAPI
	operation string
}

func (f *lostWriteResponseAPI) lost(operation string, err error) error {
	if err == nil && f.operation == operation {
		f.operation = ""
		return errors.New("connection closed after committed operation")
	}
	return err
}

func (f *lostWriteResponseAPI) PutSecretValue(ctx context.Context, name, token, value string) error {
	return f.lost("put", f.fakeAPI.PutSecretValue(ctx, name, token, value))
}

func (f *lostWriteResponseAPI) UpdateSecretVersionStage(ctx context.Context, name, stage, moveTo, removeFrom string) error {
	return f.lost(stage, f.fakeAPI.UpdateSecretVersionStage(ctx, name, stage, moveTo, removeFrom))
}

func (f *lostWriteResponseAPI) TagSecret(ctx context.Context, name string, tags map[string]string) error {
	err := f.fakeAPI.TagSecret(ctx, name, tags)
	if _, versionTag := tags[VersionTag]; versionTag {
		return f.lost("version-tag", err)
	}
	return err
}

func TestStagedWriteReplayAfterActualLostResponses(t *testing.T) {
	for _, operation := range []string{"put", awsCurrent, CurrentStage, "version-tag"} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%v", operation, existing), func(t *testing.T) {
				api, journal := newFakeAPI(), newFakeJournal()
				if existing {
					if _, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "initial"}, journal); err != nil {
						t.Fatal(err)
					}
				}
				wrapped := &lostWriteResponseAPI{fakeAPI: api, operation: operation}
				req := adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "lost-response"}
				if _, err := (&Module{API: wrapped}).Sync(t.Context(), req, journal); !errors.Is(err, adapter.ErrIndeterminate) {
					t.Fatalf("actual committed operation's lost response = %v", err)
				}
				req.Ledger = journal.ledger()
				if _, err := (&Module{API: wrapped}).Sync(t.Context(), req, journal); err != nil {
					t.Fatalf("same-job replay failed: %v", err)
				}
				wantVersions := 1
				if existing {
					wantVersions++
				}
				secret := api.secrets["prod/app"]
				token := idempotencyToken(req.JobID, req.Target.ID, req.Target.Generation, "prod/app")
				if len(secret.values) != wantVersions || secret.tags[VersionTag] != token || !slices.Contains(secret.stages[token], CurrentStage) || !slices.Contains(secret.stages[token], awsCurrent) || journal.states["prod/app"] != adapter.Owned {
					t.Fatalf("replay duplicated a version or failed to settle ownership: versions=%d tags=%v stages=%v state=%v", len(secret.values), secret.tags, secret.stages, journal.states["prod/app"])
				}
			})
		}
	}
}

func TestValueWrittenIntoAFreshHikyoSecretIsRefused(t *testing.T) {
	api, journal := newFakeAPI(), newFakeJournal()
	// Created by this target (tagged) but written by someone else before
	// Hikyo's first value landed.
	api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{adapter.SentinelName: testTarget}, stages: map[string][]string{"theirs": {awsCurrent}}, values: map[string]string{"theirs": "x"}}
	journal.states["prod/app"] = adapter.Dispatched
	if _, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_1"}, journal); !errors.Is(err, adapter.ErrConflict) {
		t.Fatalf("foreign first value = %v, want conflict", err)
	}
}

func TestAdoptedSecretIsTaggedBeforeWrite(t *testing.T) {
	api, journal := newFakeAPI(), newFakeJournal()
	api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{}, stages: map[string][]string{"v1": {awsCurrent}}, values: map[string]string{"v1": "legacy"}}
	journal.states["prod/app"] = adapter.Owned // explicit adoption
	if _, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_1"}, journal); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(api.writes(), []string{"tag:prod/app", "put:prod/app", "stage-AWSCURRENT:prod/app", "stage-HIKYO_CURRENT:prod/app", "tag:prod/app"}) {
		t.Fatalf("adoption writes = %v, want ownership tag, put, version tag", api.writes())
	}
}

func TestDefiniteLaterFailurePreservesCustodyOfCreatedSecret(t *testing.T) {
	for _, operation := range []string{"put", "tag"} {
		t.Run(operation, func(t *testing.T) {
			api, journal := newFakeAPI(), newFakeJournal()
			api.failOnce[operation+":prod/app"] = &ResponseError{Status: 403, Code: "AccessDeniedException"}
			_, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{
				Target: jsonTarget(), Manifest: manifest(), JobID: "job_partial",
			}, journal)
			if err == nil {
				t.Fatal("partial mutation unexpectedly succeeded")
			}
			if api.secrets["prod/app"] == nil {
				t.Fatal("fixture did not create the remote secret")
			}
			if journal.states["prod/app"] != adapter.Dispatched {
				t.Fatalf("ledger state = %q, want dispatched custody", journal.states["prod/app"])
			}
		})
	}
}

func TestOwnedSecretScheduledForDeletionIsRestored(t *testing.T) {
	api, journal := newFakeAPI(), newFakeJournal()
	api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{adapter.SentinelName: testTarget}, stages: map[string][]string{}, values: map[string]string{}, deleted: true}
	if _, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "job_1"}, journal); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(api.writes(), []string{"restore:prod/app", "put:prod/app", "stage-AWSCURRENT:prod/app", "stage-HIKYO_CURRENT:prod/app", "tag:prod/app"}) || api.secrets["prod/app"].deleted {
		t.Fatalf("writes = %v", api.writes())
	}
}

func TestFailureClassesFailLoud(t *testing.T) {
	for _, tt := range []struct {
		name  string
		err   error
		class adapter.ErrorClass
		state adapter.LedgerState
	}{
		{"kms denial", errors.Join(adapter.ErrProviderAuth, &ResponseError{Status: 400, Code: "EncryptionFailure"}), adapter.ErrorClassAuth, adapter.Owned},
		{"expired credentials", errors.Join(adapter.ErrProviderAuth, &ResponseError{Status: 400, Code: "ExpiredTokenException"}), adapter.ErrorClassAuth, adapter.Owned},
		{"throttled", &throttled{ResponseError: &ResponseError{Status: 400, Code: "ThrottlingException"}, at: time.Now().Add(time.Minute)}, adapter.ErrorClassProviderLimit, adapter.Owned},
		{"server error", &ResponseError{Status: 500, Code: "InternalServiceError"}, adapter.ErrorClassProviderAmbiguous, adapter.Dispatched},
		{"timeout", context.DeadlineExceeded, adapter.ErrorClassProviderAmbiguous, adapter.Dispatched},
	} {
		t.Run(tt.name, func(t *testing.T) {
			api, journal := newFakeAPI(), newFakeJournal()
			api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{adapter.SentinelName: testTarget}, stages: map[string][]string{}, values: map[string]string{}}
			journal.states["prod/app"] = adapter.Owned
			api.failOn["put:prod/app"] = tt.err
			result, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_1"}, journal)
			if err == nil || adapter.ClassifyError(err) != tt.class {
				t.Fatalf("err = %v class %q, want %q", err, adapter.ClassifyError(err), tt.class)
			}
			if len(result.Changes) != 0 || len(result.Failed) != 1 {
				t.Fatalf("false success: %+v", result)
			}
			if journal.states["prod/app"] != tt.state {
				t.Fatalf("ledger = %q, want %q", journal.states["prod/app"], tt.state)
			}
			if tt.class == adapter.ErrorClassProviderLimit {
				if _, ok := adapter.ProviderRetryAt(err); !ok {
					t.Fatal("throttle lost its retry-after deadline")
				}
			}
		})
	}
}

func TestAccountAndRegionMovementIsRefused(t *testing.T) {
	api, journal := newFakeAPI(), newFakeJournal()
	api.account = "210987654321"
	_, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "job_1"}, journal)
	if !errors.Is(err, adapter.ErrDestinationID) || len(api.writes()) != 0 {
		t.Fatalf("account move = %v writes=%v", err, api.writes())
	}
	if err := verifyARN("arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/app-AbCdEf", testAccount, testRegion); !errors.Is(err, adapter.ErrDestinationID) {
		t.Fatalf("region move = %v", err)
	}
}

func TestTestConnectionResolvesAccountAsDestinationID(t *testing.T) {
	api := newFakeAPI()
	gates := 0
	connection, err := (&Module{API: api}).TestConnection(t.Context(), adapter.ConnectionRequest{
		Destination: adapter.Destination{Kind: adapter.PerKey, Owner: testAccount, Name: "prod/"},
		Gate:        func(context.Context) error { gates++; return nil },
	})
	if err != nil || connection.DestinationID != 123456789012 || connection.Version != "secretsmanager:"+testRegion || gates != 2 {
		t.Fatalf("connection=%+v err=%v gates=%d", connection, err, gates)
	}
	if _, err := (&Module{API: api}).TestConnection(t.Context(), adapter.ConnectionRequest{Destination: adapter.Destination{Kind: adapter.PerKey, Owner: testAccount}}); !errors.Is(err, adapter.ErrUnauthorized) {
		t.Fatalf("ungated connection test = %v", err)
	}
}

func TestPlanIsValueBlindAndMarksConflicts(t *testing.T) {
	api := newFakeAPI()
	api.secrets["prod/APP_DATABASE_URL"] = &fakeSecret{tags: map[string]string{}, stages: map[string][]string{}, values: map[string]string{}}
	entries := manifest()
	for i := range entries {
		entries[i].Value = ""
	}
	plan, err := (&Module{API: api}).Plan(t.Context(), adapter.PlanRequest{Target: perKeyTarget(), Manifest: entries, Gate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	want := []adapter.Change{
		{Surface: adapter.Secret, EffectiveName: "prod/APP_DATABASE_URL", Disposition: adapter.Conflict},
		{Surface: adapter.Secret, EffectiveName: "prod/APP_LOG_LEVEL", Disposition: adapter.Create},
	}
	if !slices.Equal(plan.Changes, want) {
		t.Fatalf("plan = %+v", plan.Changes)
	}
	if len(api.writes()) != 0 {
		t.Fatalf("plan wrote: %v", api.writes())
	}
}

func TestManifestRefusals(t *testing.T) {
	for _, tt := range []struct {
		name        string
		destination adapter.Destination
		entries     []adapter.ManifestEntry
		want        string
	}{
		{"account", adapter.Destination{Kind: adapter.PerKey, Owner: "12345"}, nil, "12-digit"},
		{"json name", adapter.Destination{Kind: adapter.JSONObject, Owner: testAccount, Name: "path/"}, nil, "does not end in /"},
		{"path prefix", adapter.Destination{Kind: adapter.PerKey, Owner: testAccount, Name: "prod"}, nil, "must end in /"},
		{"arn suffix", adapter.Destination{Kind: adapter.JSONObject, Owner: testAccount, Name: "app-abcdef"}, nil, "six characters"},
		{"empty value", adapter.Destination{Kind: adapter.PerKey, Owner: testAccount}, []adapter.ManifestEntry{{CanonicalName: "A", Classification: adapter.ConfigClassification}}, "empty SecretString"},
		{"non utf8", adapter.Destination{Kind: adapter.JSONObject, Owner: testAccount, Name: "app"}, []adapter.ManifestEntry{{CanonicalName: "A", Classification: adapter.SecretClassification, Value: "\xff"}}, "non-UTF-8"},
		{"too large", adapter.Destination{Kind: adapter.JSONObject, Owner: testAccount, Name: "app"}, []adapter.ManifestEntry{{CanonicalName: "A", Classification: adapter.SecretClassification, Value: strings.Repeat("x", adapter.AWSSecretValueLimit)}}, "SecretString limit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := adapter.ValidateAWSSecretsManagerManifest(tt.destination, "", tt.entries, true)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

type adoptionTagFailureAPI struct {
	*fakeAPI
	landed  bool
	failure error
}

func (f *adoptionTagFailureAPI) TagSecret(ctx context.Context, name string, tags map[string]string) error {
	if f.failure != nil {
		err := f.failure
		f.failure = nil
		if f.landed {
			if tagErr := f.fakeAPI.TagSecret(ctx, name, tags); tagErr != nil {
				return tagErr
			}
		}
		return err
	}
	return f.fakeAPI.TagSecret(ctx, name, tags)
}
func TestAdoptionRetainsAuthorityAcrossUnclearTagOutcome(t *testing.T) {
	for _, landed := range []bool{false, true} {
		for _, failure := range []error{context.DeadlineExceeded, &ResponseError{Status: 503}} {
			api, journal := newFakeAPI(), newFakeJournal()
			api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{}, stages: map[string][]string{"v1": {awsCurrent}}, values: map[string]string{"v1": "legacy"}}
			journal.states["prod/app"] = adapter.Owned
			wrapped := &adoptionTagFailureAPI{fakeAPI: api, landed: landed, failure: failure}
			module := &Module{API: wrapped}
			req := adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_adopt"}
			if _, err := module.Sync(t.Context(), req, journal); err == nil {
				t.Fatal("tag failure succeeded")
			}
			if journal.states["prod/app"] != adapter.Owned || api.current("prod/app") != "legacy" {
				t.Fatalf("landed=%v: adoption authority or value changed", landed)
			}
			req.Ledger = journal.ledger()
			if _, err := module.Sync(t.Context(), req, journal); err != nil {
				t.Fatalf("landed=%v: retry: %v", landed, err)
			}
			if journal.states["prod/app"] != adapter.Owned || api.current("prod/app") == "legacy" {
				t.Fatalf("landed=%v: retry did not write", landed)
			}
		}
	}
}

func TestOwnedTagRetryDoesNotOverridePriorValueEvidence(t *testing.T) {
	for _, versionTag := range []bool{false, true} {
		api, journal := newFakeAPI(), newFakeJournal()
		secret := &fakeSecret{tags: map[string]string{adapter.SentinelName: testTarget}, stages: map[string][]string{"external": {awsCurrent}}, values: map[string]string{"external": "external-value"}}
		if versionTag {
			secret.tags[VersionTag] = "prior"
		} else {
			secret.stages["prior"] = []string{CurrentStage}
		}
		api.secrets["prod/app"] = secret
		journal.states["prod/app"] = adapter.Owned
		_, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_retry"}, journal)
		if !errors.Is(err, adapter.ErrConflict) || len(api.writes()) != 0 || api.current("prod/app") != "external-value" {
			t.Fatalf("versionTag=%v: external edit not protected: %v", versionTag, err)
		}
	}
}
