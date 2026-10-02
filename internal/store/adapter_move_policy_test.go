package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/githubactions"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func movePolicyWrite(t *testing.T, db *store.DB, fn func(context.Context, store.Repos, authz.Proof) error) error {
	t.Helper()
	return storetx.Write(t.Context(), db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_744"}, authz.OpAdapterConfigure, domain.Scope{Org: "org_744", Project: "prj_744"})
		if err != nil {
			return err
		}
		return fn(ctx, r, p)
	})
}

func movePolicyTarget(id, name string) store.AdapterTargetMutation {
	return store.AdapterTargetMutation{ID: id, AdapterID: "adp_move", EnvironmentID: "env_744", DestinationKind: "repository", DestinationOwner: "team", DestinationName: name, DestinationID: 42, KeyIDs: []string{"key_744"}}
}

func seedMovePolicy(t *testing.T, db *store.DB, mixed bool) {
	t.Helper()
	seedAdapterBase(t, db)
	if err := movePolicyWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
		_, _, err := r.Adapters().Create(ctx, p, store.AdapterCreate{ID: "adp_move", Provider: "forgejo", Origin: "https://old.example", CredentialCiphertext: []byte("sealed"), AuthorityPrincipalID: "usr_744", Target: movePolicyTarget("tgt_pause", "app"), At: time.Now().UTC()})
		if err != nil || !mixed {
			return err
		}
		target := movePolicyTarget("tgt_live", "other")
		target.DestinationID = 43
		_, err = r.Adapters().AddTarget(ctx, p, store.AdapterTargetUpdate{Target: target, AuthorityPrincipalID: "usr_744", At: time.Now().UTC()})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"tgt_pause", "tgt_live"} {
		if id == "tgt_live" && !mixed {
			continue
		}
		execAdapter(t, db, fmt.Sprintf(`INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,destination_id,surface,effective_name,normalized_name,state,updated_at) SELECT 'led_'||id,org_id,project_id,environment_id,id,'https://old.example',destination_kind,destination_id,'secret','DB_URL','DB_URL','owned',created_at FROM adapter_targets WHERE id='%s'`, id))
	}
}

func movePolicyTargets(t *testing.T, db *store.DB) []store.AdapterTarget {
	t.Helper()
	var targets []store.AdapterTarget
	if err := storetx.Read(t.Context(), db, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_744"}, authz.OpAdapterInspect, domain.Scope{Org: "org_744", Project: "prj_744"})
		if err != nil {
			return err
		}
		targets, err = r.Adapters().ListTargets(ctx, p, "adp_move")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return targets
}

func movePolicyTargetState(t *testing.T, targets []store.AdapterTarget) []byte {
	t.Helper()
	// All returned target fields are exported without omission tags. Preserve
	// nil/empty slices and pointer values while avoiding reflection in a test
	// package that handles authorization proofs.
	state, err := json.Marshal(targets)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func runPausedAdapterMoves(t *testing.T, engine store.Engine) {
	for _, action := range []string{"destination", "origin_single", "origin_mixed"} {
		for _, keep := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/keep_%t", action, keep), func(t *testing.T) {
				db := repositoryRecoveryDB(t, engine)
				seedMovePolicy(t, db, action == "origin_mixed")
				if err := movePolicyWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
					_, err := r.Adapters().PauseTarget(ctx, p, "tgt_pause", time.Now().UTC())
					return err
				}); err != nil {
					t.Fatal(err)
				}
				before := movePolicyTargets(t, db)
				beforeState := movePolicyTargetState(t, before)
				owned := recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE state='owned'`)
				err := movePolicyWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
					if action == "destination" {
						target := movePolicyTarget("tgt_pause", "next")
						_, err := r.Adapters().MoveTarget(ctx, p, store.AdapterRouteMoveMutation{MoveID: "move_paused", Target: target, ExpectedGeneration: before[0].Generation, AuthorityPrincipalID: "usr_744", KeepRemote: keep, At: time.Now().UTC()})
						return err
					}
					_, err := r.Adapters().MoveOrigin(ctx, p, store.AdapterOriginMoveMutation{MoveID: "move_paused", AdapterID: "adp_move", Origin: "https://new.example", PendingCredentialCiphertext: []byte("pending"), AuthorityPrincipalID: "usr_744", KeepRemote: keep, At: time.Now().UTC()})
					return err
				})
				if !errors.Is(err, domain.ErrConflict) {
					t.Errorf("paused move = %v; want conflict", err)
				}
				if after := movePolicyTargets(t, db); !bytes.Equal(beforeState, movePolicyTargetState(t, after)) {
					t.Errorf("paused move changed targets: before=%+v after=%+v", before, after)
				}
				if recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_route_moves`) != 0 || recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_outbox WHERE route_move_id IS NOT NULL`) != 0 || recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE state='owned'`) != owned {
					t.Error("paused move committed jobs, a partial route, or custody release")
				}
			})
		}
	}
}

func TestPausedAdapterMovesSQLite(t *testing.T)   { runPausedAdapterMoves(t, store.EngineSQLite) }
func TestPausedAdapterMovesPostgres(t *testing.T) { runPausedAdapterMoves(t, store.EnginePostgres) }

// The external API fixture returns different, independently chosen identities
// for each endpoint. Production Module.TestConnection and runtime activation
// retain responsibility for checking and persisting both pins.
type moveIdentityAPI struct {
	githubactions.API
	repositoryID, destinationID int64
}

func (a moveIdentityAPI) ResolveDestination(_ context.Context, d adapter.Destination) (githubactions.DestinationIdentity, error) {
	if d.RepositoryID != 0 && d.RepositoryID != a.repositoryID {
		return githubactions.DestinationIdentity{}, adapter.ErrDestinationID
	}
	return githubactions.DestinationIdentity{ID: a.destinationID, RepositoryID: a.repositoryID}, nil
}
func (moveIdentityAPI) VerifySelectedRepositories(context.Context, adapter.Destination) error {
	return nil
}
func (moveIdentityAPI) ListSecretNames(context.Context, adapter.Destination) ([]string, error) {
	return nil, nil
}
func (moveIdentityAPI) PublicKey(context.Context, adapter.Destination) (githubactions.PublicKey, error) {
	return githubactions.PublicKey{}, nil
}

func runOriginMoveEndpointIdentity(t *testing.T, engine store.Engine) {
	db := repositoryRecoveryDB(t, engine)
	seedAdapterBase(t, db)
	if err := movePolicyWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
		first := movePolicyTarget("tgt_pause", "app")
		first.DestinationKind, first.DestinationEnvironment, first.RepositoryID, first.DestinationID = "environment", "production", 111, 101
		_, _, err := r.Adapters().Create(ctx, p, store.AdapterCreate{ID: "adp_move", Provider: "github-actions", Origin: "https://old.example/api/v3", CredentialCiphertext: []byte("old-sealed"), AuthorityPrincipalID: "usr_744", Target: first, At: time.Now().UTC()})
		if err != nil {
			return err
		}
		second := first
		second.ID, second.DestinationName, second.RepositoryID, second.DestinationID = "tgt_live", "other", 112, 102
		_, err = r.Adapters().AddTarget(ctx, p, store.AdapterTargetUpdate{Target: second, AuthorityPrincipalID: "usr_744", At: time.Now().UTC()})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := movePolicyWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
		_, err := r.Adapters().MoveOrigin(ctx, p, store.AdapterOriginMoveMutation{MoveID: "move_identity", AdapterID: "adp_move", Origin: "https://new.example/api/v3", PendingCredentialCiphertext: []byte("new-sealed"), AuthorityPrincipalID: "usr_744", KeepRemote: true, At: time.Now().UTC()})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertPendingUnpinned := func(stage string) {
		t.Helper()
		if err := movePolicyWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
			move, err := r.Adapters().Move(ctx, p, "move_identity")
			if err != nil {
				return err
			}
			for _, target := range move.Targets {
				if target.RepositoryID != 0 || target.DestinationID != 0 {
					t.Fatalf("%s pending identity=%d/%d; want unresolved0/0", stage, target.RepositoryID, target.DestinationID)
				}
				current, err := r.Adapters().Target(ctx, p, target.TargetID)
				if err != nil {
					return err
				}
				old := map[string][2]int64{"tgt_pause": {111, 101}, "tgt_live": {112, 102}}[target.TargetID]
				if current.Origin != "https://old.example/api/v3" || current.RepositoryID != old[0] || current.DestinationID != old[1] {
					t.Fatalf("%s changed the still-active old route identity: %+v", stage, current)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertPendingUnpinned("begin")
	runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
	claim := func() adapter.Job {
		t.Helper()
		now := time.Now().UTC()
		job, ok, err := runtime.ClaimDue(t.Context(), "identity-worker", now.Add(time.Second), now.Add(adapter.LeaseTime))
		if err != nil || !ok || job.Kind != adapter.Activate {
			t.Fatalf("activation claim=%+v,%t,%v", job, ok, err)
		}
		return job
	}
	activate := func(job adapter.Job, origin string, repositoryID, destinationID int64) {
		t.Helper()
		material, err := runtime.LoadActivation(t.Context(), job)
		if err != nil {
			t.Fatal(err)
		}
		if material.Origin != origin || material.Target.Destination.RepositoryID != 0 || material.Target.Destination.NumericID != 0 {
			t.Fatalf("pending endpoint material=%+v", material)
		}
		journal := runtime.Journal(job)
		module := githubactions.Module{API: moveIdentityAPI{repositoryID: repositoryID, destinationID: destinationID}}
		connection, err := module.TestConnection(t.Context(), adapter.ConnectionRequest{Config: adapter.Config{Origin: origin}, Destination: material.Target.Destination, Access: adapter.Access{Credential: "github_pat_identity_fixture"}, Gate: func(ctx context.Context) error {
			return journal.Gate(ctx, adapter.Effect{Surface: adapter.Secret, EffectiveName: "route", Disposition: adapter.Update})
		}})
		if err != nil {
			t.Fatalf("new endpoint connection=%v", err)
		}
		if err := runtime.Activate(t.Context(), job, connection, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	// A real partial activation leaves resolved pins for one target. Failure
	// on the other target enters the supported attention/replacement workflow.
	first := claim()
	activate(first, "https://new.example/api/v3", 222, 202)
	second := claim()
	if err := runtime.Fail(t.Context(), second, 0, time.Now().UTC(), adapter.ErrProviderAuth); err != nil {
		t.Fatal(err)
	}
	if err := movePolicyWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
		_, err := r.Adapters().ReplaceMoveOrigin(ctx, p, "move_identity", "https://replacement.example/api/v3", []byte("replacement-sealed"), "usr_744", time.Now().UTC())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertPendingUnpinned("replacement")
	want := map[string][2]int64{"tgt_pause": {333, 303}, "tgt_live": {334, 304}}
	for range 2 {
		job := claim()
		identity := want[job.TargetID]
		activate(job, "https://replacement.example/api/v3", identity[0], identity[1])
	}
	activated := movePolicyTargets(t, db)
	if len(activated) != 2 {
		t.Fatalf("activated target count=%d; want2", len(activated))
	}
	for _, target := range activated {
		identity := want[target.ID]
		if target.Origin != "https://replacement.example/api/v3" || target.RepositoryID != identity[0] || target.DestinationID != identity[1] || target.State != "active" {
			t.Fatalf("activated target=%+v; want replacement identity=%v", target, identity)
		}
	}
}

func TestOriginMoveEndpointIdentitySQLite(t *testing.T) {
	runOriginMoveEndpointIdentity(t, store.EngineSQLite)
}
func TestOriginMoveEndpointIdentityPostgres(t *testing.T) {
	runOriginMoveEndpointIdentity(t, store.EnginePostgres)
}
