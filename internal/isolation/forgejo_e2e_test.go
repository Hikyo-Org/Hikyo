package isolation

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/forgejo"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// TestForgejoRealLifecycle is deliberately external-gated: it proves the
// actual v1.21+ endpoints and status semantics without making the ordinary
// unit suite depend on a provider. CI or a release workstation supplies a
// disposable repository and a PAT with write:repository.
func TestForgejoRealLifecycle(t *testing.T) {
	origin := os.Getenv("HIKYO_TEST_FORGEJO_URL")
	token := os.Getenv("HIKYO_TEST_FORGEJO_TOKEN")
	owner := os.Getenv("HIKYO_TEST_FORGEJO_OWNER")
	repository := os.Getenv("HIKYO_TEST_FORGEJO_REPOSITORY")
	if origin == "" || token == "" || owner == "" || repository == "" {
		t.Skip("set HIKYO_TEST_FORGEJO_URL/TOKEN/OWNER/REPOSITORY for the real Forgejo adapter lifecycle")
	}
	for _, engine := range []struct {
		name string
		open func(*testing.T) *store.DB
	}{{"sqlite", openSQLite}, {"postgres", openPostgres}} {
		t.Run(engine.name, func(t *testing.T) {
			runForgejoRealLifecycle(t, engine.open(t), origin, token, owner, repository)
		})
	}
}

func runForgejoRealLifecycle(t *testing.T, db *store.DB, origin, token, owner, repository string) {
	t.Helper()
	var allowed []netip.Prefix
	if raw := os.Getenv("HIKYO_TEST_FORGEJO_ALLOWED_CIDR"); raw != "" {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			t.Fatalf("HIKYO_TEST_FORGEJO_ALLOWED_CIDR: %v", err)
		}
		allowed = append(allowed, prefix)
	}
	client, err := forgejo.NewClient(forgejo.ClientConfig{Origin: origin, Credential: token, AllowedCIDRs: allowed, Deadline: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Forget)
	destination := adapter.Destination{Kind: adapter.Repository, Owner: owner, Name: repository}
	connection, err := (&forgejo.Module{API: client}).TestConnection(t.Context(), adapter.ConnectionRequest{Destination: destination, Gate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	destination.NumericID = connection.DestinationID
	prefix := "HIKYO_E2E_" + strconv.FormatInt(time.Now().UnixNano(), 36) + "_"
	prefix = strings.ToUpper(prefix)
	target := adapter.Target{ID: "external", Environment: "external", Destination: destination, NamePrefix: prefix, Generation: 1}
	module := &forgejo.Module{API: client}
	journal := newForgejoLifecycleJournal()
	manifest := []adapter.ManifestEntry{
		{KeyID: "key_secret", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: "claim-value"},
		{KeyID: "key_config", CanonicalName: "MODE", Classification: adapter.ConfigClassification, Value: "claim"},
	}
	t.Cleanup(func() {
		// Go cancels t.Context before cleanup; provider scrub needs its own
		// bounded live context even when an assertion failed.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := module.Sync(ctx, adapter.SyncRequest{Target: target, Ledger: journal.ledger(), Teardown: true}, journal); err != nil {
			t.Errorf("cleanup provider fixture: %v", err)
		}
	})
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest}, journal); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Ledger: journal.ledger()}, journal); err != nil {
		t.Fatalf("prune to sentinels: %v", err)
	}

	// Explicit adoption is the only path that turns an existing provider name
	// into overwrite authority. This proof traverses the real Plan artifact,
	// service authorization, store transition, outbox, and durable journal.
	adoptPrefix := prefix + "ADOPT_"
	adoptedName := adoptPrefix + "ADOPTED"
	if err := client.PutSecret(t.Context(), destination, adoptedName, "pre-existing"); err != nil {
		t.Fatal(err)
	}
	adoptTarget := target
	adoptTarget.ID, adoptTarget.Environment, adoptTarget.NamePrefix = "tgt_external_adopt", "env_external_adopt", adoptPrefix
	adoptManifest := []adapter.ManifestEntry{{KeyID: "key_adopted", CanonicalName: "ADOPTED", Classification: adapter.SecretClassification, Value: "managed"}}
	plan, err := module.Plan(t.Context(), adapter.PlanRequest{Target: adoptTarget, Manifest: adoptManifest, Gate: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	var observed bool
	for _, change := range plan.Changes {
		if change.Surface == adapter.Secret && change.EffectiveName == adoptedName && change.Disposition == adapter.Conflict {
			observed = true
		}
	}
	if !observed {
		t.Fatalf("Plan did not observe pre-existing %s: %+v", adoptedName, plan.Changes)
	}
	seedRealAdoptionDB(t, db, origin, destination.NumericID, adoptPrefix)
	projectScope := domain.Scope{Org: "org_external", Project: "prj_external"}
	runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
	now := time.Now().UTC()
	replayJob, ok, err := runtime.ClaimDue(t.Context(), "external-replay-worker", now, now.Add(adapter.LeaseTime))
	if err != nil || !ok {
		t.Fatalf("claim replay job: %+v %v %v", replayJob, ok, err)
	}

	// A process crash before dispatch leaves a durable reserved row. Rebuild
	// the Journal from the outbox job, create a third-party secret in the gap,
	// and prove the real provider write is refused and the reservation released.
	reservedName := adoptPrefix + "RESERVED_GAP"
	reservedEffect := adapter.Effect{Surface: adapter.Secret, EffectiveName: reservedName, Disposition: adapter.Create, KeyID: "gap"}
	if state, err := runtime.Journal(replayJob).Reserve(t.Context(), reservedEffect); err != nil || state != adapter.Reserved {
		t.Fatalf("durable reserve = %q, %v", state, err)
	}
	if err := client.PutSecret(t.Context(), destination, reservedName, "third-party"); err != nil {
		t.Fatal(err)
	}
	_, err = module.Sync(t.Context(), adapter.SyncRequest{
		Target: adoptTarget, Manifest: []adapter.ManifestEntry{{KeyID: "gap", CanonicalName: "RESERVED_GAP", Classification: adapter.SecretClassification, Value: "must-not-land"}}, Ledger: realLedger(t, db, adoptTarget.ID),
	}, runtime.Journal(replayJob))
	if !errors.Is(err, adapter.ErrConflict) {
		t.Fatalf("durable reserved replay = %v, want exists-unowned", err)
	}
	if err := client.DeleteSecret(t.Context(), destination, reservedName); err != nil && !forgejo.IsNotFound(err) {
		t.Fatal(err)
	}

	// A crash after INTENT/dispatch may have landed. Expire only the crashed
	// provider fence, reconstruct the Journal, and prove replay updates the real
	// variable while retaining the original INTENT-without-OUTCOME artifact.
	dispatchedName := adoptPrefix + "DISPATCHED_GAP"
	dispatchedEffect := adapter.Effect{Surface: adapter.Variable, EffectiveName: dispatchedName, Disposition: adapter.Create, KeyID: "gap2"}
	crashedJournal := runtime.Journal(replayJob)
	state, err := crashedJournal.Reserve(t.Context(), dispatchedEffect)
	if err != nil || state != adapter.Reserved {
		t.Fatalf("dispatch reserve = %q, %v", state, err)
	}
	if err := crashedJournal.Prepare(t.Context(), dispatchedEffect, state); err != nil {
		t.Fatal(err)
	}
	if err := client.CreateVariable(t.Context(), destination, dispatchedName, "landed-before-crash"); err != nil {
		t.Fatal(err)
	}
	execRealAdoption(t, db, `UPDATE adapter_targets SET provider_lease_expires_at='2000-01-01T00:00:00Z' WHERE id=$1`, adoptTarget.ID)
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{
		Target: adoptTarget, Manifest: []adapter.ManifestEntry{{KeyID: "gap2", CanonicalName: "DISPATCHED_GAP", Classification: adapter.ConfigClassification, Value: "reconciled"}}, Ledger: realLedger(t, db, adoptTarget.ID),
	}, runtime.Journal(replayJob)); err != nil {
		t.Fatalf("durable dispatch replay: %v", err)
	}
	var durableState string
	var unresolvedIntents int
	if err := queryRealAdoptionRow(t, db, `SELECT state FROM adapter_ledger WHERE target_id=$1 AND surface='variable' AND normalized_name=$2`, adoptTarget.ID, strings.ToUpper(dispatchedName)).Scan(&durableState); err != nil {
		t.Fatal(err)
	}
	if err := queryRealAdoptionRow(t, db, `SELECT COUNT(*) FROM adapter_effects WHERE job_id=$1 AND outcome IS NULL`, replayJob.ID).Scan(&unresolvedIntents); err != nil {
		t.Fatal(err)
	}
	if durableState != "owned" || unresolvedIntents < 1 {
		t.Fatalf("dispatch replay state=%q unresolved intents=%d", durableState, unresolvedIntents)
	}
	if err := runtime.Succeed(t.Context(), replayJob, 0, nil, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	artifactID := "plan_external_adopt"
	entry := store.AdapterConflictEntry{Surface: string(adapter.Secret), EffectiveName: adoptedName}
	if err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_external"}, authz.OpAdapterPlan, projectScope)
		if err != nil {
			return err
		}
		return repos.Adapters().RecordPlan(ctx, p, adoptTarget.ID, artifactID, 1, 0, destination.NumericID, []store.AdapterConflictEntry{entry}, time.Now().UTC())
	}); err != nil {
		t.Fatal(err)
	}
	adoptionService := &service.Adapters{DB: db}
	if _, err := adoptionService.Adopt(t.Context(), service.LocalPrincipal("usr_external"), projectScope, service.AdoptAdapterRequest{TargetID: adoptTarget.ID, ArtifactID: artifactID, ExpectedGeneration: adoptTarget.Generation, ExpectedDestinationID: adoptTarget.Destination.NumericID, Entries: []store.AdapterConflictEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	claimAt := time.Now().UTC()
	adoptJob, ok, err := runtime.ClaimDue(t.Context(), "external-worker", claimAt, claimAt.Add(adapter.LeaseTime))
	if err != nil || !ok {
		t.Fatalf("claim adopted converge: %+v %v %v", adoptJob, ok, err)
	}
	adoptLedger := realLedger(t, db, adoptTarget.ID)
	adoptTarget.Generation = adoptJob.Generation
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: adoptTarget, Manifest: adoptManifest, Ledger: adoptLedger}, runtime.Journal(adoptJob)); err != nil {
		t.Fatalf("adoption converge: %v", err)
	}
	if err := runtime.Succeed(t.Context(), adoptJob, 1, nil, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	teardown, err := adoptionService.RemoveTarget(t.Context(), service.LocalPrincipal("usr_external"), projectScope, adoptTarget.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(teardown.Targets) != 1 || teardown.Targets[0].JobID == "" {
		t.Fatalf("service teardown = %+v", teardown)
	}
	scrubJob := adapter.Job{}
	scrubJob, ok, err = runtime.ClaimDue(t.Context(), "external-worker", time.Now().UTC(), time.Now().UTC().Add(adapter.LeaseTime))
	if err != nil || !ok {
		t.Fatalf("claim adoption scrub: %+v %v %v", scrubJob, ok, err)
	}
	adoptTarget.Generation = scrubJob.Generation
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: adoptTarget, Ledger: realLedger(t, db, adoptTarget.ID), Teardown: true}, runtime.Journal(scrubJob)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Succeed(t.Context(), scrubJob, 0, nil, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Ledger: journal.ledger(), Teardown: true}, journal); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	for name, state := range journal.states {
		if state != adapter.Released {
			t.Fatalf("teardown ledger %s = %q, want released history", name, state)
		}
	}
}

func seedRealAdoptionDB(t *testing.T, db *store.DB, origin string, destinationID int64, prefix string) {
	t.Helper()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_external','External',TRUE,'{}','2026-08-17T00:00:00Z')`, nil},
		{`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_external','org_external','External','2026-08-17T00:00:00Z')`, nil},
		{`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_external_adopt','org_external','prj_external','external','','2026-08-17T00:00:00Z',0)`, nil},
		{`INSERT INTO principals (id,kind,created_at) VALUES ('usr_external','human','2026-08-17T00:00:00Z')`, nil},
		{`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_external_manage','usr_external','manage-adapters','org_external','prj_external',NULL,'2026-08-17T00:00:00Z')`, nil},
		{`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_external_reveal','usr_external','reveal','org_external','prj_external','env_external_adopt','2026-08-17T00:00:00Z')`, nil},
		{`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_external','org_external','prj_external','forgejo',$1,'usr_external','active','2026-08-17T00:00:00Z')`, []any{origin}},
		{`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_external_adopt','org_external','prj_external','env_external_adopt','adp_external','repository','external','external',$1,$2,1,'active','failed','2026-08-17T00:00:00Z')`, []any{destinationID, prefix}},
		{`INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,next_attempt_at,state,created_at) VALUES ('job_external_replay','org_external','prj_external','env_external_adopt','tgt_external_adopt','converge','usr_external',1,'tgt_external_adopt','2026-08-17T00:00:00Z','queued','2026-08-17T00:00:00Z')`, nil},
		{`UPDATE adapter_targets SET active_job_id='job_external_replay' WHERE id='tgt_external_adopt'`, nil},
	}
	for _, statement := range statements {
		execRealAdoption(t, db, statement.query, statement.args...)
	}
}

// Both drivers support numbered placeholders. These fixture helpers keep raw
// observation and crash injection independent of the engine under test.
func execRealAdoption(t *testing.T, db *store.DB, query string, args ...any) {
	t.Helper()
	var err error
	if db.Engine() == store.EnginePostgres {
		_, err = db.PG().Exec(t.Context(), query, args...)
	} else {
		_, err = db.SQLiteWrite().ExecContext(t.Context(), query, args...)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func queryRealAdoptionRow(t *testing.T, db *store.DB, query string, args ...any) interface{ Scan(...any) error } {
	t.Helper()
	if db.Engine() == store.EnginePostgres {
		return db.PG().QueryRow(t.Context(), query, args...)
	}
	return db.SQLiteRead().QueryRowContext(t.Context(), query, args...)
}

func realLedger(t *testing.T, db *store.DB, targetID string) []adapter.LedgerEntry {
	t.Helper()
	const query = `SELECT surface,effective_name,state FROM adapter_ledger WHERE target_id=$1 ORDER BY surface,effective_name`
	var out []adapter.LedgerEntry
	scan := func(next func() bool, get func(...any) error) {
		for next() {
			var surface, state string
			var entry adapter.LedgerEntry
			if err := get(&surface, &entry.EffectiveName, &state); err != nil {
				t.Fatal(err)
			}
			entry.Surface, entry.State = adapter.Surface(surface), adapter.LedgerState(state)
			out = append(out, entry)
		}
	}
	if db.Engine() == store.EnginePostgres {
		rows, err := db.PG().Query(t.Context(), query, targetID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		scan(rows.Next, rows.Scan)
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	} else {
		rows, err := db.SQLiteRead().QueryContext(t.Context(), query, targetID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		scan(rows.Next, rows.Scan)
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// forgejoLifecycleJournal is intentionally local to the external harness.
// Durable crash-window coverage below uses store.AdapterRuntime; this small
// journal only owns the throwaway provider names used to bracket that proof.
type forgejoLifecycleJournal struct {
	states map[string]adapter.LedgerState
}

func newForgejoLifecycleJournal() *forgejoLifecycleJournal {
	return &forgejoLifecycleJournal{states: map[string]adapter.LedgerState{}}
}

func forgejoLifecycleEffectKey(effect adapter.Effect) string {
	return string(effect.Surface) + ":" + effect.EffectiveName
}

func (j *forgejoLifecycleJournal) Gate(context.Context, adapter.Effect) error { return nil }

func (j *forgejoLifecycleJournal) Reserve(_ context.Context, effect adapter.Effect) (adapter.LedgerState, error) {
	key := forgejoLifecycleEffectKey(effect)
	if state, ok := j.states[key]; ok {
		return state, nil
	}
	j.states[key] = adapter.Reserved
	return adapter.Reserved, nil
}

func (j *forgejoLifecycleJournal) Prepare(_ context.Context, effect adapter.Effect, _ adapter.LedgerState) error {
	j.states[forgejoLifecycleEffectKey(effect)] = adapter.Dispatched
	return nil
}

func (j *forgejoLifecycleJournal) Finish(_ context.Context, effect adapter.Effect, completion adapter.Completion) error {
	key := forgejoLifecycleEffectKey(effect)
	if completion.ReleaseLedger {
		delete(j.states, key)
	} else {
		j.states[key] = completion.State
	}
	return nil
}

func (j *forgejoLifecycleJournal) Refuse(_ context.Context, effect adapter.Effect) error {
	delete(j.states, forgejoLifecycleEffectKey(effect))
	return nil
}

func (j *forgejoLifecycleJournal) ReleaseReservation(_ context.Context, effect adapter.Effect) error {
	key := forgejoLifecycleEffectKey(effect)
	if j.states[key] != adapter.Reserved {
		return adapter.ErrSuperseded
	}
	delete(j.states, key)
	return nil
}

func (j *forgejoLifecycleJournal) ledger() []adapter.LedgerEntry {
	out := make([]adapter.LedgerEntry, 0, len(j.states))
	for key, state := range j.states {
		parts := strings.SplitN(key, ":", 2)
		out = append(out, adapter.LedgerEntry{Surface: adapter.Surface(parts[0]), EffectiveName: parts[1], State: state})
	}
	return out
}
