package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func seedOriginAdoptionPeer(t *testing.T, db *store.DB, provider, currentOrigin, foreignOrigin, foreignProvider, foreignState, foreignScope string, aws bool) {
	t.Helper()
	seedAdoptionFixture(t, db)
	execAdapter(t, db, fmt.Sprintf(`UPDATE adapters SET provider='%s',origin='%s' WHERE id='adp_1'`, provider, currentOrigin))
	if aws {
		execAdapter(t, db, `UPDATE adapter_targets SET destination_kind='json-object',destination_owner='123456789012',destination_name='TOKEN',destination_id=123456789012 WHERE id='tgt_1'`)
		execAdapter(t, db, `UPDATE adapter_conflicts SET destination_id=123456789012 WHERE target_id='tgt_1'`)
	}
	execAdapter(t, db, fmt.Sprintf(`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_peer','org_adopt','prj_adopt','%s','%s','usr_adopt','active','2026-08-17T00:00:00Z')`, foreignProvider, foreignOrigin))
	execAdapter(t, db, `INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,destination_scope,name_prefix,generation,state,sync_status,created_at) SELECT 'tgt_peer',org_id,project_id,environment_id,'adp_peer',destination_kind,destination_owner,destination_name,destination_id,'','',1,'active','never',created_at FROM adapter_targets WHERE id='tgt_1'`)
	if foreignState != "" {
		execAdapter(t, db, fmt.Sprintf(`INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,destination_scope,repository_id,destination_id,surface,effective_name,normalized_name,state,updated_at) SELECT 'led_peer',org_id,project_id,environment_id,'tgt_peer','%s',destination_kind,'%s',repository_id,destination_id,'secret','TOKEN','TOKEN','%s',created_at FROM adapter_targets WHERE id='tgt_1'`, foreignOrigin, foreignScope, foreignState))
	}
}

func runCanonicalOriginAdoption(t *testing.T, engine store.Engine) {
	for _, tc := range []struct {
		name, provider, current, foreign, foreignProvider, state, scope string
		aws, refuse                                                     bool
	}{
		{"slash", "forgejo", "https://git.example", "https://git.example/", "forgejo", "owned", "", false, true},
		{"case_port", "forgejo", "https://git.example", "https://GIT.EXAMPLE:00443/", "forgejo", "owned", "", false, true},
		{"idna", "forgejo", "https://xn--bcher-kva.example", "https://BÜCHER.example/", "forgejo", "owned", "", false, true},
		{"other_host", "forgejo", "https://git.example", "https://git.example.evil", "forgejo", "owned", "", false, false},
		{"released", "forgejo", "https://git.example", "https://git.example/", "forgejo", "released", "", false, false},
		{"different_provider", "forgejo", "https://git.example", "https://git.example/", "gitlab", "owned", "", false, false},
		{"different_scope", "gitlab", "https://git.example", "https://git.example/", "gitlab", "owned", "staging", false, false},
		{"legacy_default", "github-actions", "https://api.github.com", "", "github-actions", "owned", "", false, true},
		{"aws_fips", "aws-secrets-manager", "https://secretsmanager.us-east-1.amazonaws.com", "https://secretsmanager-fips.us-east-1.amazonaws.com", "aws-secrets-manager", "owned", "", true, true},
		{"aws_other_region", "aws-secrets-manager", "https://secretsmanager.us-east-1.amazonaws.com", "https://secretsmanager.us-west-2.amazonaws.com", "aws-secrets-manager", "owned", "", true, false},
		{"aws_forged_custom", "aws-secrets-manager", "https://secretsmanager.us-east-1.amazonaws.com", "https://attacker.example", "aws-secrets-manager", "owned", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := repositoryRecoveryDB(t, engine)
			seedOriginAdoptionPeer(t, db, tc.provider, tc.current, tc.foreign, tc.foreignProvider, tc.state, tc.scope, tc.aws)
			err := adoptTOKEN(t, db)
			if tc.refuse {
				if !errors.Is(err, adapter.ErrOperatorReview) || !errors.Is(err, store.ErrConflict) {
					t.Fatalf("alias adoption=%v; want operator review/conflict", err)
				}
				if recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_conflicts WHERE artifact_id='plan_1' AND adopted_at IS NOT NULL`) != 0 || recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE id='led_adopt'`) != 0 {
					t.Fatal("refused alias consumed consent or claimed custody")
				}
			} else if err != nil {
				t.Fatalf("independent/released namespace adoption=%v", err)
			}
			if recoveryCount(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE id='led_peer' AND provider_origin='%s' AND state='%s'`, tc.foreign, tc.state)) != 1 {
				t.Fatal("alias guard rewrote foreign custody")
			}
		})
	}
}

func TestCanonicalOriginAdoptionSQLite(t *testing.T) {
	runCanonicalOriginAdoption(t, store.EngineSQLite)
}
func TestCanonicalOriginAdoptionPostgres(t *testing.T) {
	runCanonicalOriginAdoption(t, store.EnginePostgres)
}

func runConcurrentAWSOriginAdoption(t *testing.T, engine store.Engine) {
	db := repositoryRecoveryDB(t, engine)
	seedOriginAdoptionPeer(t, db, "aws-secrets-manager", "https://secretsmanager.us-east-1.amazonaws.com", "https://secretsmanager-fips.us-east-1.amazonaws.com", "aws-secrets-manager", "", "", true)
	if err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterPlan, domain.Scope{Org: "org_adopt", Project: "prj_adopt"})
		if err != nil {
			return err
		}
		return repos.Adapters().RecordPlan(ctx, p, "tgt_peer", "plan_peer", 1, 0, 123456789012, []store.AdapterConflictEntry{{Surface: "secret", EffectiveName: "TOKEN"}}, time.Now().UTC())
	}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, peer := range []bool{false, true} {
		go func() {
			<-start
			results <- storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
				p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_adopt"}, authz.OpAdapterAdopt, domain.Scope{Org: "org_adopt", Project: "prj_adopt"})
				if err != nil {
					return err
				}
				target, artifact, job, ledger := "tgt_1", "plan_1", "job_adopt", "led_adopt"
				if peer {
					target, artifact, job, ledger = "tgt_peer", "plan_peer", "job_peer", "led_peer"
				}
				_, err = repos.Adapters().Adopt(ctx, p, store.AdapterAdoption{TargetID: target, ArtifactID: artifact, Entries: []store.AdapterConflictEntry{{Surface: "secret", EffectiveName: "TOKEN"}}, AuthorityPrincipalID: "usr_adopt", LedgerIDs: []string{ledger}, JobID: job, AuditAt: time.Now().UTC()})
				return err
			})
		}()
	}
	close(start)
	a, b := <-results, <-results
	if (a == nil) == (b == nil) {
		t.Fatalf("regional/FIPS concurrent adoption must have one winner:%v / %v", a, b)
	}
	for _, err := range []error{a, b} {
		if err != nil && !errors.Is(err, adapter.ErrOperatorReview) {
			t.Fatalf("unexpected concurrent failure:%v", err)
		}
	}
	if recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE state='owned'`) != 1 || recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_conflicts WHERE adopted_at IS NOT NULL`) != 1 {
		t.Fatal("semantic AWS aliases both claimed/consumed consent")
	}
}

func TestConcurrentAWSOriginAdoptionSQLite(t *testing.T) {
	runConcurrentAWSOriginAdoption(t, store.EngineSQLite)
}
func TestConcurrentAWSOriginAdoptionPostgres(t *testing.T) {
	runConcurrentAWSOriginAdoption(t, store.EnginePostgres)
}

func runAWSSharedKindAdoption(t *testing.T, engine store.Engine) {
	for _, currentKind := range []string{"json-object", "per-key"} {
		t.Run(currentKind, func(t *testing.T) {
			db := repositoryRecoveryDB(t, engine)
			seedOriginAdoptionPeer(t, db, "aws-secrets-manager", "https://secretsmanager.us-east-1.amazonaws.com", "https://secretsmanager-fips.us-east-1.amazonaws.com", "aws-secrets-manager", "owned", "", true)
			foreignKind := "per-key"
			if currentKind == "per-key" {
				foreignKind = "json-object"
			}
			// The object name TOKEN and empty per-key prefix plus key TOKEN
			// address the same physical AWS secret despite their route kinds.
			for targetID, kind := range map[string]string{"tgt_1": currentKind, "tgt_peer": foreignKind} {
				name := "TOKEN"
				if kind == "per-key" {
					name = ""
				}
				execAdapter(t, db, fmt.Sprintf(`UPDATE adapter_targets SET destination_kind='%s',destination_name='%s' WHERE id='%s'`, kind, name, targetID))
			}
			execAdapter(t, db, fmt.Sprintf(`UPDATE adapter_ledger SET destination_kind='%s' WHERE id='led_peer'`, foreignKind))
			if err := adoptTOKEN(t, db); !errors.Is(err, adapter.ErrOperatorReview) || !errors.Is(err, store.ErrConflict) {
				t.Fatalf("AWS shared-kind adoption=%v; want operator review/conflict", err)
			}
			if recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_conflicts WHERE artifact_id='plan_1' AND adopted_at IS NOT NULL`) != 0 || recoveryCount(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE id='led_adopt'`) != 0 {
				t.Fatal("shared-kind refusal consumed consent or claimed custody")
			}
			if recoveryCount(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM adapter_ledger WHERE id='led_peer' AND state='owned' AND destination_kind='%s'`, foreignKind)) != 1 {
				t.Fatal("shared-kind refusal modified foreign held custody")
			}
		})
	}
}

func TestAWSSharedKindAdoptionSQLite(t *testing.T) {
	runAWSSharedKindAdoption(t, store.EngineSQLite)
}

func TestAWSSharedKindAdoptionPostgres(t *testing.T) {
	runAWSSharedKindAdoption(t, store.EnginePostgres)
}
