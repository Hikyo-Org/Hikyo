package isolation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Multi-target synchronization (#157), end to end through the real service,
// store, runtime and worker boundaries with only the provider HTTP peer
// replaced. One published revision fans out to two adapters of different
// kinds; a failure at one target never touches the other; pause blocks the
// fan-out and resume catches up to a named revision; resync is idempotent; a
// crash between INTENT and OUTCOME is settled as an unknown OUTCOME on replay;
// and no audit payload ever carries value plaintext.

const (
	fanoutOriginA = "https://forgejo-a.example"
	fanoutOriginB = "https://github-b.example"
	fanoutSecretA = "plaintext-for-revision-one"
	fanoutSecretB = "plaintext-for-revision-two"
)

// fanoutModule is a provider double that honours the journal protocol for
// every desired row. `refuse` makes Sync fail like a revoked credential before
// any INTENT; `crash` makes it write one INTENT and then vanish without an
// OUTCOME, which is what a process death between the two looks like.
type fanoutModule struct {
	mu     sync.Mutex
	refuse bool
	crash  bool
	synced int
}

func (m *fanoutModule) set(refuse, crash bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refuse, m.crash = refuse, crash
}

func (m *fanoutModule) ValidateConfig(adapter.Config) error { return nil }

func (m *fanoutModule) TestConnection(ctx context.Context, request adapter.ConnectionRequest) (adapter.Connection, error) {
	if err := request.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	return adapter.Connection{Version: "double", DestinationID: 7_001}, nil
}

func (m *fanoutModule) Plan(ctx context.Context, request adapter.PlanRequest) (adapter.Plan, error) {
	if err := request.Gate(ctx); err != nil {
		return adapter.Plan{}, err
	}
	ledger, err := adapter.IndexLedger(request.Ledger)
	if err != nil {
		return adapter.Plan{}, err
	}
	return adapter.Plan{Changes: adapter.PlanChanges(adapter.DesiredRows(request.Target.NamePrefix, request.Manifest, true), ledger, nil)}, nil
}

var errFanoutCrash = errors.New("fanout: simulated process death after INTENT")

func (m *fanoutModule) Sync(ctx context.Context, request adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	m.mu.Lock()
	refuse, crash := m.refuse, m.crash
	m.mu.Unlock()
	if refuse {
		return adapter.SyncResult{}, adapter.ErrProviderAuth
	}
	ledger, err := adapter.IndexLedger(request.Ledger)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	result := adapter.SyncResult{}
	for _, row := range adapter.DesiredRows(request.Target.NamePrefix, request.Manifest, !request.Teardown) {
		key := adapter.NewLedgerKey(row.Surface, row.EffectiveName)
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Create, KeyID: row.KeyID}
		record, claimed := ledger[key]
		state := record.State
		if claimed && (state == adapter.Owned || state == adapter.Dispatched) {
			effect.Disposition = adapter.Update
		}
		if !claimed {
			if state, err = journal.Reserve(ctx, effect); err != nil {
				return result, err
			}
		}
		if err := journal.Prepare(ctx, effect, state); err != nil {
			return result, err
		}
		if crash {
			return result, errFanoutCrash
		}
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned}); err != nil {
			return result, err
		}
		ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: adapter.Owned}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
	}
	m.mu.Lock()
	m.synced++
	m.mu.Unlock()
	return result, nil
}

// fanoutLoader reads the leased job's immutable inputs through the real
// runtime (revision, manifest names, ledger) and routes to the module by
// origin. Values stay sealed: the doubles never need plaintext.
type fanoutLoader struct {
	runtime *store.AdapterRuntime
	modules map[string]*fanoutModule
}

func (l fanoutLoader) Load(ctx context.Context, job adapter.Job, journal adapter.Journal) (adapter.LoadedSync, error) {
	if err := journal.Gate(ctx, adapter.Effect{Surface: adapter.Secret, EffectiveName: "manifest", Disposition: adapter.Update}); err != nil {
		return adapter.LoadedSync{}, err
	}
	material, err := l.runtime.LoadExecution(ctx, job)
	if err != nil {
		return adapter.LoadedSync{}, err
	}
	module, ok := l.modules[material.Origin]
	if !ok {
		return adapter.LoadedSync{}, fmt.Errorf("fanout: no module for origin %s", material.Origin)
	}
	manifest := make([]adapter.ManifestEntry, 0, len(material.Entries))
	for _, entry := range material.Entries {
		manifest = append(manifest, adapter.ManifestEntry{KeyID: entry.KeyID, CanonicalName: entry.KeyName, Classification: adapter.Classification(entry.Classification)})
	}
	return adapter.LoadedSync{Module: module, Request: adapter.SyncRequest{Config: adapter.Config{Origin: material.Origin}, Target: material.Target, Manifest: manifest, Ledger: material.Ledger}, Revision: material.Revision}, nil
}

func runMultiTargetSync(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := tctx(t)
	scope := domain.Scope{Org: orgA, Project: prjA1}
	envScope := domain.Scope{Org: orgA, Project: prjA1, Env: envA1}
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_mt_manage','usr_alice','manage-adapters','org_a','prj_a1',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_mt_reveal','usr_alice','reveal','org_a','prj_a1','env_a1',`+ts+`)`)

	kr := probeKeyring(t, db)
	moduleA, moduleB := &fanoutModule{}, &fanoutModule{}
	modules := map[string]*fanoutModule{fanoutOriginA: moduleA, fanoutOriginB: moduleB}
	svc := &service.Adapters{
		DB: db, Keyring: kr,
		ModuleFactory: func(_ adapter.Provider, config adapter.Config, _ string) (*adapter.ModuleLease, error) {
			module, ok := modules[config.Origin]
			if !ok {
				return nil, fmt.Errorf("fanout: unknown origin %s", config.Origin)
			}
			return adapter.NewModuleLease(module, func() {})
		},
	}
	operator := service.LocalPrincipal(alice)
	create := func(provider, origin, prefix string) store.AdapterTarget {
		t.Helper()
		created, err := svc.Create(ctx, operator, scope, service.CreateAdapterRequest{
			Provider: provider, Origin: origin, Credential: []byte("token-" + prefix),
			Target: service.AdapterTargetInput{
				EnvironmentID: string(envA1), DestinationKind: string(adapter.Repository),
				DestinationOwner: "acme", DestinationName: "app", NamePrefix: prefix,
				KeySelection: &service.AdapterKeySelection{Include: []string{"SHARED_*"}},
			},
		})
		if err != nil {
			t.Fatalf("create %s adapter: %v", provider, err)
		}
		target := created.Targets[0]
		if len(target.Keys) != 1 || target.Keys[0].ID != keyA1 || target.Keys[0].Name != "SHARED_KEY" {
			t.Fatalf("%s target keys resolved to %+v, want SHARED_KEY by id", provider, target.Keys)
		}
		return target
	}
	targetA := create(string(adapter.ForgejoProvider), fanoutOriginA, "A_")
	targetB := create(string(adapter.GitHubActionsProvider), fanoutOriginB, "B_")

	// Stage and publish revision 1 as the value custodian; the publish fans
	// out one immutable job per target under the adapters' recorded authority.
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
	publish(fanoutSecretA)

	inspect := func(id string) store.AdapterTarget {
		t.Helper()
		view, err := svc.InspectTarget(ctx, operator, scope, id)
		if err != nil {
			t.Fatalf("inspect %s: %v", id, err)
		}
		return view.Target
	}
	queuedJobs := func(targetID string) int64 {
		return queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_outbox WHERE target_id='%s' AND state IN ('queued','running')`, targetID))
	}
	for _, target := range []store.AdapterTarget{inspect(targetA.ID), inspect(targetB.ID)} {
		if target.Health() != adapter.HealthPending || queuedJobs(target.ID) != 1 {
			t.Fatalf("after publish target %s health=%q queued=%d", target.ID, target.Health(), queuedJobs(target.ID))
		}
		// One immutable job per target, pinned to the adapter's recorded
		// authority and the target's configuration generation.
		if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_outbox WHERE target_id='%s' AND state='queued' AND authority_principal_id='%s' AND generation=%d`, target.ID, alice, target.Generation)); got != 1 {
			t.Fatalf("jobs for %s pinned to authority %s and generation %d = %d, want 1", target.ID, alice, target.Generation, got)
		}
	}

	authorizePush := func(ctx context.Context, job adapter.Job, _ adapter.Effect) error {
		return tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID(job.AuthorityPrincipal), Class: domain.ClassHuman}, authz.OpAdapterPush, domain.Scope{
				Org: domain.OrgID(job.OrgID), Project: domain.ProjectID(job.ProjectID), Env: domain.EnvID(job.EnvironmentID),
			})
			return err
		})
	}
	runtime := store.NewAdapterRuntime(db, authorizePush)
	clock := time.Now().UTC()
	worker := &adapter.Worker{
		Store: runtime, Loader: fanoutLoader{runtime: runtime, modules: modules}, ID: "fanout-node-1",
		Now: func() time.Time { return clock }, Jitter: func(time.Duration) time.Duration { return 0 },
	}
	drain := func() {
		t.Helper()
		for range 8 {
			clock = clock.Add(time.Second)
			worked, err := worker.RunOnce(ctx)
			if err != nil {
				t.Fatalf("worker: %v", err)
			}
			if !worked {
				return
			}
		}
		t.Fatal("worker never drained the outbox")
	}

	// Target A's provider refuses the credential; B converges regardless.
	moduleA.set(true, false)
	drain()
	a, b := inspect(targetA.ID), inspect(targetB.ID)
	if a.Health() != adapter.HealthFailed || a.LastErrorClass != adapter.ErrorClassAuth || a.LastAttemptedRevision == nil || *a.LastAttemptedRevision != 1 || a.ConvergedRevision != nil {
		t.Fatalf("A after refusal = health %q class %q attempted %v converged %v", a.Health(), a.LastErrorClass, a.LastAttemptedRevision, a.ConvergedRevision)
	}
	if b.Health() != adapter.HealthConverged || b.ConvergedRevision == nil || *b.ConvergedRevision != 1 || b.LastErrorClass != "" {
		t.Fatalf("B beside a failing sibling = health %q converged %v class %q", b.Health(), b.ConvergedRevision, b.LastErrorClass)
	}

	// Pause B, publish revision 2: only A is queued, B keeps its ledger.
	paused, err := svc.PauseTarget(ctx, operator, scope, targetB.ID)
	if err != nil {
		t.Fatalf("pause B: %v", err)
	}
	if paused.Health() != adapter.HealthPaused || paused.PausedAt == nil {
		t.Fatalf("paused B = %q", paused.Health())
	}
	publish(fanoutSecretB)
	if queuedJobs(targetB.ID) != 0 || queuedJobs(targetA.ID) != 1 {
		t.Fatalf("after publish while B paused: queued A=%d B=%d", queuedJobs(targetA.ID), queuedJobs(targetB.ID))
	}
	if _, err := svc.SyncTarget(ctx, operator, scope, targetB.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("manual sync on a paused target = %v, want conflict", err)
	}
	if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE target_id='%s' AND state='owned'`, targetB.ID)); got != 3 {
		t.Fatalf("paused B ledger owned rows = %d, want the two sentinels and the key", got)
	}
	moduleA.set(false, false)
	drain()
	if a = inspect(targetA.ID); a.Health() != adapter.HealthConverged || *a.ConvergedRevision != 2 || a.LastErrorClass != "" {
		t.Fatalf("A after recovery = health %q converged %v class %q", a.Health(), a.ConvergedRevision, a.LastErrorClass)
	}

	// Resume B: one catch-up converge to the revision the response names.
	resumed, err := svc.ResumeTarget(ctx, operator, scope, targetB.ID)
	if err != nil {
		t.Fatalf("resume B: %v", err)
	}
	if resumed.Revision != 2 || resumed.Enqueue.JobID == "" {
		t.Fatalf("resume = %+v, want revision 2", resumed)
	}
	if b = inspect(targetB.ID); b.Health() != adapter.HealthPending || b.PausedAt != nil {
		t.Fatalf("resumed B before converge = %q paused=%v", b.Health(), b.PausedAt)
	}
	drain()
	if b = inspect(targetB.ID); b.Health() != adapter.HealthConverged || *b.ConvergedRevision != 2 {
		t.Fatalf("B after resume = health %q converged %v", b.Health(), b.ConvergedRevision)
	}

	// Resync twice: the second supersedes the first, one live job, and the
	// converge lands on the same revision.
	first, err := svc.SyncTarget(ctx, operator, scope, targetB.ID)
	if err != nil {
		t.Fatalf("resync 1: %v", err)
	}
	second, err := svc.SyncTarget(ctx, operator, scope, targetB.ID)
	if err != nil {
		t.Fatalf("resync 2: %v", err)
	}
	if second.SupersededJobID != first.JobID || queuedJobs(targetB.ID) != 1 {
		t.Fatalf("resync superseded=%q want %q; live jobs=%d", second.SupersededJobID, first.JobID, queuedJobs(targetB.ID))
	}
	drain()
	if b = inspect(targetB.ID); b.Health() != adapter.HealthConverged || *b.ConvergedRevision != 2 {
		t.Fatalf("B after resync = health %q converged %v", b.Health(), b.ConvergedRevision)
	}

	// Crash window on A: INTENT written, process dies before the OUTCOME. The
	// job keeps its lease until it expires; the replay claim settles the
	// effect as unknown, correlated to the job that opened it, and the retry
	// converges from the presumed-written custody.
	if _, err := svc.SyncTarget(ctx, operator, scope, targetA.ID); err != nil {
		t.Fatalf("sync A for crash: %v", err)
	}
	moduleA.set(false, true)
	crashed := &adapter.Worker{
		Store: runtime, Loader: fanoutLoader{runtime: runtime, modules: modules}, ID: "fanout-node-crashed",
		Now: func() time.Time { return clock }, Jitter: func(time.Duration) time.Duration { return 0 },
	}
	clock = clock.Add(time.Second)
	if worked, err := crashed.RunOnce(ctx); !worked || !errors.Is(err, adapter.ErrSuperseded) {
		// The dying attempt cannot even record its retry: its provider-write
		// lease is still held by the effect it never finished.
		t.Fatalf("crashing attempt: worked=%v err=%v", worked, err)
	}
	if open := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE target_id='%s' AND outcome IS NULL`, targetA.ID)); open != 1 {
		t.Fatalf("open effects after crash = %d, want 1", open)
	}
	moduleA.set(false, false)
	clock = clock.Add(2 * adapter.LeaseTime)
	drain()
	if a = inspect(targetA.ID); a.Health() != adapter.HealthConverged || *a.ConvergedRevision != 2 {
		t.Fatalf("A after crash replay = health %q converged %v", a.Health(), a.ConvergedRevision)
	}
	if open := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects WHERE target_id='%s' AND outcome IS NULL`, targetA.ID)); open != 0 {
		t.Fatalf("open effects after replay = %d", open)
	}
	unknown := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_effects e JOIN audit_tenant_events i ON i.id=e.intent_audit_id JOIN audit_tenant_events o ON o.id=e.outcome_audit_id WHERE e.target_id='%s' AND e.outcome='unknown' AND i.type='adapter.push_intent' AND o.type='adapter.push_outcome' AND o.outcome='unknown' AND i.correlation_id=o.correlation_id AND o.correlation_id=e.job_id`, targetA.ID))
	if unknown != 1 {
		t.Fatalf("INTENT/OUTCOME(unknown) pairs linked by the opening job = %d, want 1", unknown)
	}

	// Audit linkage for the control acts, and value blindness throughout.
	for _, want := range []struct{ typ, predicate string }{
		{"adapter.sync_requested", `'on-publish'`},
		{"adapter.sync_requested", `'resume'`},
		{"adapter.sync_requested", `'manual'`},
	} {
		if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM audit_tenant_events WHERE type='%s' AND payload LIKE '%%%s%%'`, want.typ, strings.Trim(want.predicate, "'"))); got == 0 {
			t.Errorf("no %s event with trigger %s", want.typ, want.predicate)
		}
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='adapter.configure' AND payload LIKE '%target-pause%'`); got != 1 {
		t.Errorf("target-pause configure events = %d, want 1", got)
	}
	for _, plaintext := range []string{fanoutSecretA, fanoutSecretB} {
		if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM audit_tenant_events WHERE payload LIKE '%%%s%%'`, plaintext)); got != 0 {
			t.Fatalf("audit payload carries value plaintext %q", plaintext)
		}
	}
	if got := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM audit_tenant_events WHERE type='adapter.push_outcome' AND object_id='%s'`, targetB.ID)); got == 0 {
		t.Fatal("B's converges recorded no OUTCOME events")
	}
}

func TestAdapterMultiTargetSyncSQLite(t *testing.T) {
	runMultiTargetSync(t, seededDB(t, openSQLite))
}

func TestAdapterMultiTargetSyncPostgres(t *testing.T) {
	runMultiTargetSync(t, seededDB(t, openPostgres))
}
