package isolation

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestAdapterClaimedGenerationAbortPreservesChainAndLeaseOwner(t *testing.T) {
	for _, kind := range []adapter.JobKind{adapter.Converge, adapter.Scrub} {
		t.Run(string(kind), func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				seedGitLabMoves(t, db, "staging")
				runtime := generatedAdapterRuntime(db)
				now := time.Now().UTC()
				if _, err := runtime.Enqueue(t.Context(), adapter.Job{OrgID: "org_gitlab", ProjectID: "prj_gitlab", EnvironmentID: "env_gitlab_e2e", TargetID: "tgt_gitlab_a", Kind: kind, AuthorityPrincipal: "usr_gitlab"}, now); err != nil {
					t.Fatal(err)
				}
				job, found, err := runtime.ClaimDue(t.Context(), "abort-worker", now.Add(time.Second), now.Add(time.Minute))
				if err != nil || !found {
					t.Fatalf("claim: %+v found=%v err=%v", job, found, err)
				}
				// The closed worker retains this exact claimed job while the target's
				// generation advances. Aborting must settle the old job, not the new target.
				execRaw(t, db, fmt.Sprintf("UPDATE adapter_targets SET generation=generation+1 WHERE id='%s'", job.TargetID))
				targetSnapshot := func() string {
					return queryString(t, db, fmt.Sprintf("SELECT CAST(generation AS TEXT)||'|'||sync_status FROM adapter_targets WHERE id='%s'", job.TargetID))
				}
				beforeTarget := targetSnapshot()
				beforeAudit := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events")
				jobSnapshot := func() string {
					return queryString(t, db, fmt.Sprintf("SELECT state||'|'||COALESCE(lease_owner,'') FROM adapter_outbox WHERE id='%s'", job.ID))
				}
				beforeJob := jobSnapshot()
				wrongOwner := job
				wrongOwner.LeaseOwner = "other-worker"
				if err := runtime.Fail(t.Context(), wrongOwner, 0, now.Add(2*time.Second), adapter.ErrSuperseded); !errors.Is(err, adapter.ErrSuperseded) {
					t.Fatalf("wrong-owner abort: %v", err)
				}
				if jobSnapshot() != beforeJob || targetSnapshot() != beforeTarget || queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events") != beforeAudit {
					t.Fatal("wrong-owner abort changed job, target, or audit")
				}
				if err := runtime.Fail(t.Context(), job, 0, now.Add(2*time.Second), adapter.ErrSuperseded); err != nil {
					t.Fatalf("claimed stale-generation abort: %v", err)
				}
				if got := jobSnapshot(); got != "failed|" {
					t.Fatalf("old claimed job not settled: %s", got)
				}
				if targetSnapshot() != beforeTarget {
					t.Fatal("old claimed job changed the new target generation or outcome")
				}
				types := []string{"adapter.abort"}
				if kind == adapter.Scrub {
					types = append(types, "adapter.scrub")
				}
				if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events"); got != beforeAudit+int64(len(types)) {
					t.Fatalf("abort audit count: %d -> %d", beforeAudit, got)
				}
				for _, typ := range types {
					if got := queryInt(t, db, fmt.Sprintf("SELECT COUNT(*) FROM audit_tenant_events WHERE type='%s' AND outcome='failure' AND org_id='%s' AND project_id='%s' AND env_id='%s' AND object_id='%s' AND authority_id='%s' AND correlation_id='%s' AND actor_class='system'", typ, job.OrgID, job.ProjectID, job.EnvironmentID, job.TargetID, job.AuthorityPrincipal, job.ID)); got != 1 {
						t.Fatalf("%s lost the immutable claimed job chain: %d matching audits", typ, got)
					}
				}
				var payload struct {
					Cause string `json:"cause"`
				}
				raw := queryString(t, db, fmt.Sprintf("SELECT CAST(payload AS TEXT) FROM audit_tenant_events WHERE type='adapter.abort' AND correlation_id='%s'", job.ID))
				if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Cause != "generation" {
					t.Fatalf("generation abort payload: %+v %v", payload, err)
				}
			})
		})
	}
}
