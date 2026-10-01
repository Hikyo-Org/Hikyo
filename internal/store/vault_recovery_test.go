package store_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/vaultkv"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type recoveryVaultPath struct {
	version int64
	values  map[int64]string
	markers map[string]string
}

type recoveryVaultAPI struct {
	vaultkv.API // Any unexpected protocol operation panics instead of being mocked successful.
	mount       vaultkv.Mount
	paths       map[string]*recoveryVaultPath
	mutations   int
}

func (f *recoveryVaultAPI) MountInfo(context.Context, string) (vaultkv.Mount, error) {
	return f.mount, nil
}
func (f *recoveryVaultAPI) ReadMetadata(_ context.Context, _, path string) (vaultkv.Metadata, error) {
	p := f.paths[path]
	if p == nil {
		return vaultkv.Metadata{}, &vaultkv.ResponseError{Status: 404}
	}
	return vaultkv.Metadata{CurrentVersion: p.version, CustomMetadata: maps.Clone(p.markers), Versions: map[int64]vaultkv.VersionMetadata{}}, nil
}
func (f *recoveryVaultAPI) PatchCustomMetadata(_ context.Context, _, path string, patch map[string]*string) error {
	p := f.paths[path]
	if p == nil {
		return &vaultkv.ResponseError{Status: 404}
	}
	f.mutations++
	if p.markers == nil {
		p.markers = map[string]string{}
	}
	for key, value := range patch {
		if value == nil {
			delete(p.markers, key)
		} else {
			p.markers[key] = *value
		}
	}
	return nil
}
func (f *recoveryVaultAPI) WriteCAS(_ context.Context, _, path, value string, cas int64) (int64, error) {
	p := f.paths[path]
	if p == nil {
		p = &recoveryVaultPath{values: map[int64]string{}, markers: map[string]string{}}
		f.paths[path] = p
	}
	if p.version != cas {
		return 0, &vaultkv.ResponseError{Status: 400, CASMismatch: true}
	}
	f.mutations++
	p.version++
	p.values[p.version] = value
	return p.version, nil
}
func (f *recoveryVaultAPI) DeleteVersion(context.Context, string, string, int64) error {
	f.mutations++
	return errors.New("unexpected prune during recovery")
}

func runVaultCrashRecovery(t *testing.T, db *store.DB) {
	seedAdoptionFixture(t, db)
	api := &recoveryVaultAPI{mount: vaultkv.Mount{Type: "kv", Version: "2", UUID: "vault-recovery-mount"}, paths: map[string]*recoveryVaultPath{
		"apps/TOKEN": {version: 1, values: map[int64]string{1: "previous-hikyo-value"}, markers: map[string]string{vaultkv.MarkerKey: "tgt_1", vaultkv.VersionKey: "1"}},
	}}
	destinationID, err := vaultkv.DestinationID(api.mount, "apps")
	if err != nil {
		t.Fatal(err)
	}
	credential := "X'01'"
	if db.Engine() == store.EnginePostgres {
		credential = "decode('01','hex')"
	}
	execAdapter(t, db, `UPDATE adapters SET provider='vault-kv',credential_ciphertext=`+credential+` WHERE id='adp_1'`)
	execAdapter(t, db, fmt.Sprintf(`UPDATE adapter_targets SET destination_owner='secret',destination_name='apps',destination_id=%d WHERE id='tgt_1'`, destinationID))
	execAdapter(t, db, fmt.Sprintf(`UPDATE adapter_conflicts SET destination_id=%d WHERE target_id='tgt_1'`, destinationID))
	execAdapter(t, db, fmt.Sprintf(`INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_owned','org_adopt','prj_adopt','env_adopt','tgt_1','https://git.example','repository',0,%d,'secret','TOKEN','TOKEN','owned','2026-08-17T00:00:00Z')`, destinationID))
	gate := func(context.Context, adapter.Job, adapter.Effect) error { return nil }
	runtime := store.NewAdapterRuntime(db, gate)
	claim := func(worker string) adapter.Job {
		t.Helper()
		now := time.Now().UTC()
		job, ok, err := runtime.ClaimDue(t.Context(), worker, now, now.Add(adapter.LeaseTime))
		if err != nil || !ok {
			t.Fatalf("claim %s: %v, %v", worker, ok, err)
		}
		return job
	}
	job := claim("before_crash")
	effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "TOKEN", Disposition: adapter.Update, RequireExplicitRecovery: true}
	if err := runtime.Journal(job).Prepare(t.Context(), effect, adapter.Owned); err != nil {
		t.Fatal(err)
	}
	execution, err := runtime.LoadExecution(t.Context(), job)
	if err != nil || len(execution.Ledger) != 1 || execution.Ledger[0].State != adapter.Dispatched {
		t.Fatalf("pre-mutation durable custody: %+v, %v", execution.Ledger, err)
	}
	// Crash after marking N+1 but before receiving any PUT acknowledgement.
	api.paths["apps/TOKEN"].markers[vaultkv.PendingKey] = "2"
	api.paths["apps/TOKEN"].version = 2
	api.paths["apps/TOKEN"].values[2] = "external-writer"
	markers := maps.Clone(api.paths["apps/TOKEN"].markers)
	// Expire only the dead process's leases, then use a genuinely new runtime.
	execAdapter(t, db, `UPDATE adapter_targets SET provider_lease_expires_at='2026-01-01T00:00:00Z' WHERE id='tgt_1'`)
	execAdapter(t, db, `UPDATE adapter_outbox SET lease_expires_at='2026-01-01T00:00:00Z' WHERE id='job_1'`)
	runtime = store.NewAdapterRuntime(db, gate)
	job = claim("after_crash")
	module := &vaultkv.Module{API: api}
	manifest := []adapter.ManifestEntry{{KeyID: "key_token", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: "new-hikyo-value"}}
	assertReview := func(job adapter.Job) {
		t.Helper()
		execution, err := runtime.LoadExecution(t.Context(), job)
		if err != nil {
			t.Fatal(err)
		}
		for _, teardown := range []bool{false, true} {
			_, err = module.Sync(t.Context(), adapter.SyncRequest{Target: execution.Target, Manifest: manifest, Ledger: execution.Ledger, Teardown: teardown}, runtime.Journal(job))
			if !errors.Is(err, adapter.ErrOperatorReview) || api.mutations != 0 || api.paths["apps/TOKEN"].values[2] != "external-writer" || !maps.Equal(markers, api.paths["apps/TOKEN"].markers) {
				t.Fatalf("crash/retry changed provider custody: %v, mutations=%d", err, api.mutations)
			}
		}
		if err := runtime.Fail(t.Context(), job, 0, time.Now().UTC(), adapter.ErrOperatorReview); err != nil {
			t.Fatal(err)
		}
	}
	assertReview(job)
	// A newly enqueued resync is not authority to infer the dead writer either.
	_, err = runtime.Enqueue(t.Context(), adapter.Job{ID: "job_resync", OrgID: job.OrgID, ProjectID: job.ProjectID, EnvironmentID: job.EnvironmentID, TargetID: job.TargetID, AuthorityPrincipal: job.AuthorityPrincipal, Kind: adapter.Converge}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	job = claim("new_resync")
	assertReview(job)
	scope := domain.Scope{Org: "org_adopt", Project: "prj_adopt"}
	recordPlan := func(artifact string, witness *int64) {
		t.Helper()
		if err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
			proof, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterPlan, scope)
			if err != nil {
				return err
			}
			return repos.Adapters().RecordPlan(ctx, proof, "tgt_1", artifact, job.Generation, 0, destinationID, []store.AdapterConflictEntry{{Surface: "secret", EffectiveName: "TOKEN", ObservedProviderVersion: witness}}, time.Now().UTC())
		}); err != nil {
			t.Fatal(err)
		}
	}
	adopt := func(artifact, id string) error {
		return storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
			proof, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterAdopt, scope)
			if err != nil {
				return err
			}
			_, err = repos.Adapters().Adopt(ctx, proof, store.AdapterAdoption{TargetID: "tgt_1", ArtifactID: artifact, Entries: []store.AdapterConflictEntry{{Surface: "secret", EffectiveName: "TOKEN"}}, AuthorityPrincipalID: "usr_adopt", LedgerIDs: []string{"unused_" + id}, JobID: id, AuditAt: time.Now().UTC()})
			return err
		})
	}
	for i, witness := range []*int64{nil, new(int64)} {
		artifact := "invalid_" + strconv.Itoa(i)
		recordPlan(artifact, witness)
		if err := adopt(artifact, "job_invalid"); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("missing/zero witness adopted: %v", err)
		}
	}
	if err := adopt("plan_1", "job_legacy"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("legacy/stale artifact adopted: %v", err)
	}
	planFresh := func(artifact string) {
		t.Helper()
		// Failed jobs cannot be loaded; the operator's canonical PlanMaterial
		// path remains available and carries the same current held ledger.
		var material store.AdapterPlanMaterial
		if err := storetx.Read(t.Context(), db, func(ctx context.Context, repos store.ReadRepos, az *authz.TxAuthorizer) error {
			proof, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterPlan, scope)
			if err != nil {
				return err
			}
			material, err = repos.Adapters().PlanMaterial(ctx, proof, "tgt_1")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		target := adapter.Target{ID: "tgt_1", Generation: material.Target.Generation, Destination: adapter.Destination{Kind: adapter.Repository, Owner: "secret", Name: "apps", NumericID: destinationID}}
		plan, err := module.Plan(t.Context(), adapter.PlanRequest{Target: target, Manifest: manifest, Ledger: material.Ledger, Gate: func(context.Context) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		for _, change := range plan.Changes {
			if change.EffectiveName == "TOKEN" && change.Disposition == adapter.Conflict {
				if change.ObservedProviderVersion == nil || *change.ObservedProviderVersion != api.paths["apps/TOKEN"].version {
					t.Fatal("plan lost actual metadata witness")
				}
				recordPlan(artifact, change.ObservedProviderVersion)
				return
			}
		}
		t.Fatal("held uncertain path was not a fresh adoption conflict")
	}
	planFresh("fresh_two")
	for _, mismatch := range []string{"provider_origin='https://foreign.example'", "destination_id=99"} {
		execAdapter(t, db, `UPDATE adapter_ledger SET `+mismatch+` WHERE id='led_owned'`)
		if err := adopt("fresh_two", "job_wrong_custody"); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("foreign held custody %s adopted: %v", mismatch, err)
		}
		execAdapter(t, db, fmt.Sprintf(`UPDATE adapter_ledger SET provider_origin='https://git.example',destination_id=%d WHERE id='led_owned'`, destinationID))
	}
	api.paths["apps/TOKEN"].version = 3
	api.paths["apps/TOKEN"].values[3] = "later-external-writer"
	if err := adopt("fresh_two", "job_stale_version"); err != nil {
		t.Fatal(err)
	}
	job = claim("stale_version")
	execution, err = runtime.LoadExecution(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	if len(execution.Ledger) != 1 || execution.Ledger[0].AdoptionVersion == nil || *execution.Ledger[0].AdoptionVersion != 2 {
		t.Fatalf("version witness not loaded: %+v", execution.Ledger)
	}
	_, err = module.Sync(t.Context(), adapter.SyncRequest{Target: execution.Target, Manifest: manifest, Ledger: execution.Ledger}, runtime.Journal(job))
	if !errors.Is(err, adapter.ErrOperatorReview) && !errors.Is(err, adapter.ErrConflict) {
		t.Fatalf("stale provider witness = %v", err)
	}
	if api.paths["apps/TOKEN"].version != 3 || api.paths["apps/TOKEN"].values[3] != "later-external-writer" || !maps.Equal(markers, api.paths["apps/TOKEN"].markers) {
		t.Fatal("stale adoption changed external value/metadata")
	}
	if err := runtime.Fail(t.Context(), job, 0, time.Now().UTC(), adapter.ErrOperatorReview); err != nil {
		t.Fatal(err)
	}
	planFresh("fresh_three")
	if err := adopt("fresh_three", "job_fresh_version"); err != nil {
		t.Fatal(err)
	}
	job = claim("fresh_version")
	execution, err = runtime.LoadExecution(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: execution.Target, Manifest: manifest, Ledger: execution.Ledger}, runtime.Journal(job)); err != nil {
		t.Fatal(err)
	}
	p := api.paths["apps/TOKEN"]
	if p.version != 4 || p.values[4] != "new-hikyo-value" || p.values[2] != "external-writer" || p.values[3] != "later-external-writer" || p.markers[vaultkv.VersionKey] != "4" || p.markers[vaultkv.PendingKey] != "" {
		t.Fatalf("fresh version CAS did not preserve external history: %+v", p)
	}
	execution, err = runtime.LoadExecution(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range execution.Ledger {
		if entry.EffectiveName == "TOKEN" && (entry.State != adapter.Owned || entry.AdoptionPending || entry.AdoptionVersion != nil) {
			t.Fatalf("successful journal did not consume adoption: %+v", entry)
		}
	}
	if err := adopt("fresh_three", "job_consumed"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("consumed witness adopted again: %v", err)
	}
}

func TestVaultCrashHoldAndVersionBoundOperatorRecoverySQLite(t *testing.T) {
	runVaultCrashRecovery(t, openKeyTestDB(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "vault-recovery.db")}))
}
func TestVaultCrashHoldAndVersionBoundOperatorRecoveryPostgres(t *testing.T) {
	runVaultCrashRecovery(t, postgresTestDB(t))
}
