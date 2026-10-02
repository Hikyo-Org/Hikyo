package isolation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// The migration package separately verifies the real 71->72 upgrade. Replay
// those same embedded repair bytes here over a legacy-shaped move so its
// supported recovery is exercised through real authorization and services.
func TestGitLabScopeRepairMoveCanCancelThenExplicitlyResume(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		execRealAdoption(t, db, `INSERT INTO adapter_ledger(id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,destination_scope,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_legacy_move','org_gitlab','prj_gitlab','env_gitlab_e2e','tgt_gitlab_a','https://gitlab.old.example','repository',0,42,'','secret','P_TOKEN','P_TOKEN','owned','2026-10-01T00:00:00Z')`)
		now := time.Now().UTC()
		if err := gitLabMoveWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
			_, err := r.Adapters().MoveTarget(ctx, p, store.AdapterRouteMoveMutation{MoveID: "arm_repair", Target: gitLabPendingTarget("tgt_gitlab_a", "production", "new-api"), ExpectedGeneration: 1, AuthorityPrincipalID: "usr_gitlab", At: now})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		runtime := generatedAdapterRuntime(db)
		old, ok, err := runtime.ClaimDue(t.Context(), "repair-old-worker", now.Add(time.Second), now.Add(time.Minute))
		if err != nil || !ok || old.Kind != adapter.Scrub {
			t.Fatalf("claim unfinished scrub: %+v %v %v", old, ok, err)
		}
		if err := runtime.Journal(old).Gate(t.Context(), adapter.Effect{}); err != nil {
			t.Fatalf("unfinished move gate: %v", err)
		}
		repair, err := fs.ReadFile(store.MigrationsFS, "migrations/"+string(db.Engine())+"/00072_adapter_custody_repairs.sql")
		if err != nil {
			t.Fatal(err)
		}
		execRealAdoption(t, db, string(repair))
		if got := queryString(t, db, `SELECT state FROM adapter_route_moves WHERE id='arm_repair'`); got != "attention_required" {
			t.Fatalf("repair stranded move in %s", got)
		}
		if err := runtime.Journal(old).Gate(t.Context(), adapter.Effect{}); !errors.Is(err, adapter.ErrSuperseded) {
			t.Fatalf("pre-upgrade scrub retained provider authority: %v", err)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_route_move_claims WHERE move_id='arm_repair'`); got == 0 {
			t.Fatal("repair silently released pending route claims")
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_gitlab_a' AND state='moving' AND paused_at IS NOT NULL AND active_job_id IS NULL`); got != 1 {
			t.Fatal("repair did not hold the moving target")
		}
		for _, capability := range []string{"manage-adapters", "reveal"} {
			execRealAdoption(t, db, fmt.Sprintf(`INSERT INTO grants(id,principal_id,capability,org_id,project_id,created_at) VALUES ('repair_alice_%s','usr_alice','%s','org_gitlab','prj_gitlab','2026-10-01T00:00:00Z')`, capability, capability))
		}
		token := seedSessionFactors(t, db, alice, `["password","totp"]`)
		actor := service.Bearer(token)
		sessionID := queryString(t, db, `SELECT id FROM sessions WHERE principal_id='usr_alice' ORDER BY created_at DESC LIMIT 1`)
		auth := authService(t, db)
		auth.ReauthWindow = 5 * time.Minute
		svc := &service.Adapters{DB: db, Auth: auth}
		scope := domain.Scope{Org: "org_gitlab", Project: "prj_gitlab"}
		openWindows := func(prefix string, operation authz.Operation, environments []string) {
			t.Helper()
			if err := storetx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				epoch, err := az.CredentialEpoch(ctx)
				if err != nil {
					return err
				}
				at := time.Now().UTC()
				for i, environment := range environments {
					if err := az.OpenReauthWindow(ctx, authz.NewReauthWindow{
						ID: fmt.Sprintf("%s_%d", prefix, i), SessionID: sessionID, EnvironmentID: environment,
						CeremonyID: prefix, FactorClass: "totp", AuthenticatedAt: at,
						WindowExpiresAt: at.Add(5 * time.Minute), HardExpiresAt: at.Add(10 * time.Minute),
						CredentialEpoch: epoch, CreatedAt: at, BoundPurpose: string(service.PurposeAdapter),
						BoundOperation: string(operation), BoundEnvironmentSet: service.CanonicalEnvironmentSet(environments),
					}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := svc.CancelMove(t.Context(), actor, scope, "arm_repair"); !errors.Is(err, service.ErrReauthRequired) {
			t.Fatalf("cancellation skipped fresh ceremony: %v", err)
		}
		openWindows("repair_cancel", authz.OpAdapterConfigure, []string{"env_gitlab_e2e"})
		if _, err := svc.CancelMove(t.Context(), actor, scope, "arm_repair"); err != nil {
			t.Fatalf("supported cancel after repair: %v", err)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_gitlab_a' AND state='active' AND paused_at IS NOT NULL`); got != 1 {
			t.Fatal("cancel silently resumed repaired target")
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_route_move_claims WHERE move_id='arm_repair'`); got != 0 {
			t.Fatal("supported cancel retained pending claims")
		}
		if job, ok, err := runtime.ClaimDue(t.Context(), "repair-paused-worker", time.Now().UTC(), time.Now().UTC().Add(time.Minute)); err != nil || ok {
			t.Fatalf("cancel bypassed repair pause: %+v %v %v", job, ok, err)
		}
		openWindows("repair_partial", authz.OpAdapterSync, []string{"env_gitlab_e2e"})
		if _, err := svc.ResumeTarget(t.Context(), actor, scope, "tgt_gitlab_a"); !errors.Is(err, service.ErrReauthRequired) {
			t.Fatalf("resume accepted partial environment consent: %v", err)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_gitlab_a' AND paused_at IS NOT NULL`); got != 1 {
			t.Fatal("refused ceremony cleared pause")
		}
		openWindows("repair_resume", authz.OpAdapterSync, []string{"env_gitlab_e2e", "env_gitlab_second"})
		resumed, err := svc.ResumeTarget(t.Context(), actor, scope, "tgt_gitlab_a")
		if err != nil {
			t.Fatalf("explicit full-ceremony resume: %v", err)
		}
		at := time.Now().UTC()
		job, ok, err := runtime.ClaimDue(t.Context(), "repair-resumed-worker", at, at.Add(time.Minute))
		if err != nil || !ok || job.ID != resumed.Enqueue.JobID {
			t.Fatalf("resumed job claim: %+v %v %v", job, ok, err)
		}
		if err := runtime.Journal(job).Gate(t.Context(), adapter.Effect{}); err != nil {
			t.Fatalf("fresh recorded authority after resume: %v", err)
		}
		if got := queryString(t, db, `SELECT state FROM adapter_ledger WHERE id='led_legacy_move'`); got != "released" {
			t.Fatalf("repair/recovery inferred ownership: %s", got)
		}
	})
}
