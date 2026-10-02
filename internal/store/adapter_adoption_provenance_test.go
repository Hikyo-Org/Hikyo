package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func runAdapterAdoptionProvenance(t *testing.T, db *store.DB, queries []string) {
	seedAdoptionFixture(t, db)
	if err := adoptTOKEN(t, db); err != nil {
		t.Fatal(err)
	}
	if db.Engine() == store.EngineSQLite {
		execAdapter(t, db, `UPDATE adapters SET credential_ciphertext=X'01' WHERE id='adp_1'`)
	} else {
		execAdapter(t, db, `UPDATE adapters SET credential_ciphertext=decode('01','hex') WHERE id='adp_1'`)
	}
	runtime := store.NewAdapterRuntime(db, func(context.Context, adapter.Job, adapter.Effect) error { return nil })
	now := time.Now().UTC()
	job, ok, err := runtime.ClaimDue(t.Context(), "adoption_worker", now, now.Add(adapter.LeaseTime))
	if err != nil || !ok {
		t.Fatalf("claim adopted job: %v, %v", ok, err)
	}
	assertPending := func(want bool) {
		t.Helper()
		check := func(ledger []adapter.LedgerEntry) {
			t.Helper()
			if len(ledger) != 1 || ledger[0].EffectiveName != "TOKEN" || ledger[0].AdoptionPending != want {
				t.Fatalf("ledger adoption evidence = %+v; want pending=%v", ledger, want)
			}
		}
		for _, query := range queries {
			switch query {
			case "AdapterPlanLedger":
				if err := storetx.Read(t.Context(), db, func(ctx context.Context, repos store.ReadRepos, az *authz.TxAuthorizer) error {
					p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterPlan, domain.Scope{Org: "org_adopt", Project: "prj_adopt"})
					if err != nil {
						return err
					}
					material, err := repos.Adapters().PlanMaterial(ctx, p, "tgt_1")
					if err == nil {
						check(material.Ledger)
					}
					return err
				}); err != nil {
					t.Fatal(err)
				}
			case "AdapterWorkerLoadExecutionLedgerQuery":
				execution, err := runtime.LoadExecution(t.Context(), job)
				if err != nil {
					t.Fatal(err)
				}
				check(execution.Ledger)
			default:
				t.Fatalf("unrecognized provenance boundary query %q", query)
			}
		}
	}
	assertPending(true)
	effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "TOKEN", Disposition: adapter.Update}
	journal := runtime.Journal(job)
	if err := journal.Prepare(t.Context(), effect, adapter.Owned); err != nil {
		t.Fatal(err)
	}
	if err := journal.Finish(t.Context(), effect, adapter.Completion{Outcome: adapter.OutcomeFailure, State: adapter.Owned}); err != nil {
		t.Fatal(err)
	}
	assertPending(true) // A definitive failed write did not consume explicit consent.
	if err := journal.Prepare(t.Context(), effect, adapter.Owned); err != nil {
		t.Fatal(err)
	}
	if err := journal.Finish(t.Context(), effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned, ProviderStatus: 204}); err != nil {
		t.Fatal(err)
	}
	assertPending(false)
	// Whole seconds sort after fractional seconds lexically in SQLite, but
	// their actual temporal order must consume already-used adoption consent.
	execAdapter(t, db, `UPDATE adapter_conflicts SET adopted_at='2026-10-01T12:00:00Z' WHERE target_id='tgt_1'`)
	execAdapter(t, db, `UPDATE adapter_effects SET finished_at='2026-10-01T12:00:00.001Z' WHERE target_id='tgt_1' AND outcome='success'`)
	assertPending(false)
	execAdapter(t, db, `UPDATE adapter_effects SET finished_at='2026-10-01T11:59:59.999Z' WHERE target_id='tgt_1' AND outcome='success'`)
	assertPending(true) // A new explicit adoption may follow an older write.
	execAdapter(t, db, `UPDATE adapter_conflicts SET destination_id=99 WHERE target_id='tgt_1'`)
	assertPending(false)
	execAdapter(t, db, `UPDATE adapter_conflicts SET destination_id=42,effective_name='OTHER' WHERE target_id='tgt_1'`)
	assertPending(false)
	execAdapter(t, db, `UPDATE adapter_conflicts SET effective_name='TOKEN' WHERE target_id='tgt_1'`)
	assertPending(true)
	execAdapter(t, db, `UPDATE adapter_effects SET finished_at='2026-10-01T12:00:00Z' WHERE target_id='tgt_1' AND outcome='success'`)
	assertPending(false) // Ties conservatively consume consent.
	if db.Engine() == store.EngineSQLite {
		execAdapter(t, db, `UPDATE adapter_effects SET finished_at='invalid-time' WHERE target_id='tgt_1' AND outcome='success'`)
		assertPending(false)
		execAdapter(t, db, `UPDATE adapter_effects SET finished_at='2026-10-01T11:59:59Z' WHERE target_id='tgt_1' AND outcome='success'`)
		execAdapter(t, db, `UPDATE adapter_conflicts SET adopted_at='invalid-time' WHERE target_id='tgt_1'`)
		assertPending(false)
	}
	foreignJob := job
	foreignJob.OrgID = "org_other"
	if _, err := runtime.LoadExecution(t.Context(), foreignJob); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign scoped execution = %v; want not found", err)
	}
	// A subsequent route/config generation must not inherit unused adoption
	// consent even when the destination numeric ID and key name are unchanged.
	execAdapter(t, db, `UPDATE adapter_conflicts SET adopted_at='2026-10-01T12:00:00Z' WHERE target_id='tgt_1'`)
	execAdapter(t, db, `UPDATE adapter_effects SET finished_at='2026-10-01T11:59:59Z' WHERE target_id='tgt_1' AND outcome='success'`)
	assertPending(true)
	execAdapter(t, db, `UPDATE adapter_targets SET generation=generation+1 WHERE id='tgt_1'`)
	execAdapter(t, db, `UPDATE adapter_outbox SET generation=generation+1 WHERE id='job_adopt'`)
	job.Generation++
	assertPending(false)
	execAdapter(t, db, `UPDATE adapter_conflicts SET adopted_at=NULL WHERE target_id='tgt_1'`)
	assertPending(false) // Owned without explicit adoption is never consent.
}

func TestAdapterAdoptionProvenanceSQLite(t *testing.T) {
	runAdapterAdoptionProvenance(t, openKeyTestDB(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "adoption-provenance.db")}), []string{"AdapterPlanLedger", "AdapterWorkerLoadExecutionLedgerQuery"})
}

func TestAdapterAdoptionProvenancePostgres(t *testing.T) {
	runAdapterAdoptionProvenance(t, postgresTestDB(t), []string{"AdapterPlanLedger", "AdapterWorkerLoadExecutionLedgerQuery"})
}
