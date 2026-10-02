package isolation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
)

type absentAWSNamespaceAPI struct {
	awssm.API
	creates atomic.Int64
}

func (*absentAWSNamespaceAPI) ResolveIdentity(context.Context) (awssm.Identity, error) {
	return awssm.Identity{Account: "123456789012"}, nil
}
func (*absentAWSNamespaceAPI) DescribeSecret(context.Context, string) (awssm.SecretMetadata, error) {
	return awssm.SecretMetadata{}, &awssm.ResponseError{Status: 400, Code: "ResourceNotFoundException"}
}
func (api *absentAWSNamespaceAPI) CreateSecret(context.Context, awssm.CreateSecretInput) error {
	api.creates.Add(1)
	return errors.New("test: unexpected provider create reached")
}

func seedAWSPhysicalNamespace(t *testing.T, db *store.DB) {
	t.Helper()
	seedGitLabMoves(t, db, "production")
	for _, q := range []string{
		`INSERT INTO projects(id,org_id,name,created_at) VALUES ('prj_aws_foreign','org_gitlab','Foreign','2026-10-01T00:00:00Z')`,
		`INSERT INTO environments(id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_aws_foreign','org_gitlab','prj_aws_foreign','prod','','2026-10-01T00:00:00Z',0)`,
		`INSERT INTO adapters(id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_aws_foreign','org_gitlab','prj_aws_foreign','aws-secrets-manager','https://secretsmanager-fips.us-east-1.amazonaws.com','usr_gitlab','active','2026-10-01T00:00:00Z')`,
		`INSERT INTO adapter_targets(id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_aws_foreign','org_gitlab','prj_aws_foreign','env_aws_foreign','adp_aws_foreign','json-object','123456789012','prod/TOKEN',123456789012,'',1,'active','never','2026-10-01T00:00:00Z')`,
		`INSERT INTO grants(id,principal_id,capability,org_id,project_id,created_at) VALUES ('gr_aws_foreign_manage','usr_gitlab','manage-adapters','org_gitlab','prj_aws_foreign','2026-10-01T00:00:00Z')`,
		`INSERT INTO grants(id,principal_id,capability,org_id,project_id,created_at) VALUES ('gr_aws_foreign_reveal','usr_gitlab','reveal','org_gitlab','prj_aws_foreign','2026-10-01T00:00:00Z')`,
		`UPDATE adapters SET provider='aws-secrets-manager',origin='https://secretsmanager.us-east-1.amazonaws.com' WHERE id='adp_gitlab'`,
		`UPDATE adapter_targets SET destination_kind='per-key',destination_owner='123456789012',destination_name='prod/',destination_id=123456789012,repository_id=0,destination_scope='',name_prefix='' WHERE id='tgt_gitlab_a'`,
	} {
		execRealAdoption(t, db, q)
	}
}

func TestAWSPhysicalNamespaceCrossProjectAbsentRemote(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedAWSPhysicalNamespace(t, db)
		runtime := generatedAdapterRuntime(db)
		job := claimReservationJob(t, runtime)
		for _, kind := range []adapter.DestinationKind{adapter.PerKey, adapter.JSONObject} {
			for _, classification := range []adapter.Classification{adapter.SecretClassification, adapter.ConfigClassification} {
				t.Run(string(kind)+"/"+string(classification), func(t *testing.T) {
					currentName, foreignKind, foreignName := "prod/", adapter.JSONObject, "prod/TOKEN"
					if kind == adapter.JSONObject {
						currentName, foreignKind, foreignName = "prod/TOKEN", adapter.PerKey, "prod/"
					}
					execRealAdoption(t, db, `DELETE FROM adapter_ledger WHERE target_id IN ('tgt_gitlab_a','tgt_aws_foreign')`)
					execRealAdoption(t, db, `UPDATE adapter_targets SET destination_kind=$1,destination_name=$2 WHERE id='tgt_gitlab_a'`, string(kind), currentName)
					execRealAdoption(t, db, `UPDATE adapter_targets SET destination_kind=$1,destination_name=$2 WHERE id='tgt_aws_foreign'`, string(foreignKind), foreignName)
					execRealAdoption(t, db, `INSERT INTO adapter_ledger(id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,destination_scope,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_aws_physical','org_gitlab','prj_aws_foreign','env_aws_foreign','tgt_aws_foreign','https://secretsmanager-fips.us-east-1.amazonaws.com',$1,0,123456789012,'','secret','prod/TOKEN','PROD/TOKEN','owned','2026-10-01T00:00:00Z')`, string(foreignKind))
					request := adapter.SyncRequest{JobID: job.ID, Target: adapter.Target{ID: job.TargetID, Generation: job.Generation, Destination: adapter.Destination{Kind: kind, Owner: "123456789012", Name: currentName, NumericID: 123456789012}}, Manifest: []adapter.ManifestEntry{{KeyID: "key_gitlab_move", CanonicalName: "TOKEN", Classification: classification, Value: "test-value"}}}
					rows, err := adapter.AWSDesiredRows(request.Target.Destination, "", request.Manifest)
					if err != nil || len(rows) != 1 || rows[0].Surface != adapter.Secret || rows[0].EffectiveName != "prod/TOKEN" {
						t.Fatalf("physical namespace projection = %+v %v", rows, err)
					}
					api := &absentAWSNamespaceAPI{}
					_, err = (&awssm.Module{API: api}).Sync(t.Context(), request, runtime.Journal(job))
					if !errors.Is(err, adapter.ErrOperatorReview) {
						t.Fatalf("foreign opposite-kind held claim with absent provider secret = %v", err)
					}
					if api.creates.Load() != 0 {
						t.Fatal("foreign held namespace reached provider creation")
					}
					if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_gitlab_a'`); got != 0 {
						t.Fatalf("refused opposite-kind claim wrote %d ledger rows", got)
					}
					// A prior own reservation cannot bypass the dispatch boundary.
					execRealAdoption(t, db, `INSERT INTO adapter_ledger(id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,destination_scope,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_aws_own','org_gitlab','prj_gitlab','env_gitlab_e2e','tgt_gitlab_a','https://secretsmanager.us-east-1.amazonaws.com',$1,0,123456789012,'','secret','prod/TOKEN','PROD/TOKEN','reserved','2026-10-01T00:00:00Z')`, string(kind))
					err = runtime.Journal(job).Prepare(t.Context(), adapter.Effect{Surface: adapter.Secret, EffectiveName: "prod/TOKEN", Disposition: adapter.Create}, adapter.Reserved)
					if !errors.Is(err, adapter.ErrOperatorReview) {
						t.Fatalf("opposite-kind Prepare = %v", err)
					}
					if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_effects WHERE job_id='job_gitlab'`); got != 0 {
						t.Fatalf("refused opposite-kind preparation wrote %d effects", got)
					}
				})
			}
		}
	})
}

func TestAWSPhysicalNamespaceCrossKindConcurrentReservations(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedAWSPhysicalNamespace(t, db)
		runtime := generatedAdapterRuntime(db)
		if _, err := runtime.Enqueue(t.Context(), adapter.Job{OrgID: "org_gitlab", ProjectID: "prj_aws_foreign", EnvironmentID: "env_aws_foreign", TargetID: "tgt_aws_foreign", Kind: adapter.Converge, AuthorityPrincipal: "usr_gitlab"}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		jobs := []adapter.Job{claimReservationJob(t, runtime), claimReservationJob(t, runtime)}
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, job := range jobs {
			go func() {
				<-start
				_, err := runtime.Journal(job).Reserve(t.Context(), adapter.Effect{Surface: adapter.Secret, EffectiveName: "prod/TOKEN", Disposition: adapter.Create})
				results <- err
			}()
		}
		close(start)
		successes := 0
		for range jobs {
			err := <-results
			if err == nil {
				successes++
				continue
			}
			var pgErr *pgconn.PgError
			if !errors.Is(err, adapter.ErrOperatorReview) && !(errors.As(err, &pgErr) && pgErr.Code == "40001") {
				t.Fatalf("concurrent cross-kind result = %v", err)
			}
		}
		if successes != 1 {
			t.Fatalf("same physical secret cross-kind successes = %d", successes)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE destination_id=123456789012 AND normalized_name='PROD/TOKEN' AND state<>'released'`); got != 1 {
			t.Fatalf("same AWS physical secret has %d held claims", got)
		}
	})
}
