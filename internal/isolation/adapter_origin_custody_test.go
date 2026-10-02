package isolation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
)

func seedOriginGuardForeignTarget(t *testing.T, db *store.DB) {
	t.Helper()
	execRealAdoption(t, db, `INSERT INTO adapters(id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_origin_foreign','org_gitlab','prj_gitlab','gitlab','https://gitlab.old.example:443','usr_gitlab','active','2026-10-01T00:00:00Z')`)
	execRealAdoption(t, db, `UPDATE adapter_targets SET adapter_id='adp_origin_foreign',destination_id=42,destination_scope='production' WHERE id='tgt_gitlab_b'`)
}

func insertOriginGuardLedger(t *testing.T, db *store.DB, id, target, environment, origin, scope, state string) {
	t.Helper()
	execRealAdoption(t, db, `INSERT INTO adapter_ledger(id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,destination_scope,surface,effective_name,normalized_name,state,updated_at) VALUES ($1,'org_gitlab','prj_gitlab',$2,$3,$4,'repository',0,42,$5,'secret','P_TOKEN','P_TOKEN',$6,'2026-10-01T00:00:00Z')`, id, environment, target, origin, scope, state)
}

func TestAdapterOriginCustodyGuardsReserveAndPrepare(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "production")
		seedOriginGuardForeignTarget(t, db)
		runtime := generatedAdapterRuntime(db)
		job := claimReservationJob(t, runtime)
		journal := runtime.Journal(job)
		effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_TOKEN", Disposition: adapter.Create}
		for _, tc := range []struct {
			name, configured, held, scope, state, provider string
			own, refuse, blank                             bool
		}{
			{name: "foreign_default_port_alias", held: "https://GITLAB.old.example:443/", refuse: true},
			{name: "foreign_legacy_empty_default", configured: "https://gitlab.com", blank: true, refuse: true},
			{name: "foreign_trailing_dot_alias", held: "https://gitlab.old.example./", refuse: true},
			{name: "ipv4_mapped_ipv6_alias", configured: "https://127.0.0.1", held: "https://[::ffff:127.0.0.1]/", refuse: true},
			{name: "ipv4_default_port_alias", configured: "https://127.0.0.1", held: "https://127.0.0.1:443/", refuse: true},
			{name: "ipv6_expanded_alias", configured: "https://[2001:db8::1]", held: "https://[2001:0db8:0:0:0:0:0:1]:443/", refuse: true},
			{name: "idna_unicode_alias", configured: "https://xn--bcher-kva.example", held: "https://bücher.example/", refuse: true},
			{name: "idna_fullwidth_alias", held: "https://ｇｉｔｌａｂ.old.example/", refuse: true},
			{name: "own_historical_different_origin", held: "https://other.example", own: true, refuse: true},
			{name: "noncanonical_configuration", configured: "https://GITLAB.old.example:443/", refuse: true},
			{name: "released_alias", held: "https://GITLAB.old.example:443/", state: "released"},
			{name: "different_scope", held: "https://GITLAB.old.example:443/", scope: "staging"},
			{name: "different_provider", held: "https://GITLAB.old.example:443/", provider: "forgejo"},
			{name: "different_authority", held: "https://gitlab.old.example.attacker.test"},
			{name: "different_port", held: "https://gitlab.old.example:444"},
			{name: "same_canonical_own_claim", held: "https://gitlab.old.example", own: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				execRealAdoption(t, db, `DELETE FROM adapter_ledger WHERE target_id IN ('tgt_gitlab_a','tgt_gitlab_b')`)
				configured := tc.configured
				if configured == "" {
					configured = "https://gitlab.old.example"
				}
				provider := tc.provider
				if provider == "" {
					provider = "gitlab"
				}
				execRealAdoption(t, db, `UPDATE adapters SET origin=$1 WHERE id='adp_gitlab'`, configured)
				execRealAdoption(t, db, `UPDATE adapters SET provider=$1 WHERE id='adp_origin_foreign'`, provider)
				foreignOrigin := "https://gitlab.old.example:443"
				if tc.blank {
					foreignOrigin = ""
				}
				execRealAdoption(t, db, `UPDATE adapters SET origin=$1 WHERE id='adp_origin_foreign'`, foreignOrigin)
				if tc.held != "" || tc.blank {
					target, env := "tgt_gitlab_b", "env_gitlab_second"
					if tc.own {
						target, env = "tgt_gitlab_a", "env_gitlab_e2e"
					}
					scope := tc.scope
					if scope == "" {
						scope = "production"
					}
					state := tc.state
					if state == "" {
						state = "owned"
					}
					insertOriginGuardLedger(t, db, "led_origin_case", target, env, tc.held, scope, state)
				}
				state, err := journal.Reserve(t.Context(), effect)
				if tc.refuse {
					if !errors.Is(err, adapter.ErrOperatorReview) {
						t.Fatalf("Reserve = %s %v, want operator review", state, err)
					}
					if !tc.own {
						canonical, canonicalErr := adapter.CanonicalOrigin(adapter.GitLabProvider, configured)
						if canonicalErr != nil {
							t.Fatal(canonicalErr)
						}
						insertOriginGuardLedger(t, db, "led_origin_own", "tgt_gitlab_a", "env_gitlab_e2e", canonical, "production", "reserved")
					}
					before := queryInt(t, db, `SELECT COUNT(*) FROM adapter_effects WHERE job_id='job_gitlab'`)
					err = journal.Prepare(t.Context(), effect, adapter.Reserved)
					if !errors.Is(err, adapter.ErrOperatorReview) {
						t.Fatalf("Prepare = %v, want operator review", err)
					}
					if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_effects WHERE job_id='job_gitlab'`); got != before {
						t.Fatal("refused origin committed provider INTENT")
					}
					if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets WHERE id='tgt_gitlab_a' AND provider_lease_job_id IS NOT NULL`); got != 0 {
						t.Fatal("refused origin acquired provider lease")
					}
					if tc.blank {
						_, err := (&service.Adapters{DB: db}).RemoveTarget(t.Context(), service.LocalPrincipal("usr_gitlab"), domain.Scope{Org: "org_gitlab", Project: "prj_gitlab"}, "tgt_gitlab_b", true)
						if err != nil {
							t.Fatalf("explicit legacy keep-remote retirement: %v", err)
						}
						if _, err := journal.Reserve(t.Context(), effect); err != nil {
							t.Fatalf("canonical claim after explicit retirement: %v", err)
						}
					}
					return
				}
				if err != nil {
					t.Fatalf("Reserve control = %s %v", state, err)
				}
				if err := journal.Prepare(t.Context(), effect, state); err != nil {
					t.Fatalf("Prepare control: %v", err)
				}
				if err := journal.Finish(t.Context(), effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned}); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
}

func TestAdapterOriginCustodyContinuesPastFirstCandidatePage(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "production")
		seedOriginGuardForeignTarget(t, db)
		for i := 0; i < 257; i++ {
			id := fmt.Sprintf("tgt_origin_page_%03d", i)
			execRealAdoption(t, db, `INSERT INTO adapter_targets(id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at,destination_scope) VALUES ($1,'org_gitlab','prj_gitlab','env_gitlab_second','adp_origin_foreign','repository','team',$2,42,'P_',1,'active','never','2026-10-01T00:00:00Z','production')`, id, fmt.Sprintf("page-%03d", i))
			origin := fmt.Sprintf("https://gitlab.old.example:%d", 10000+i)
			if i == 256 {
				origin = "https://GITLAB.old.example:443/"
			}
			insertOriginGuardLedger(t, db, fmt.Sprintf("led_origin_page_%03d", i), id, "env_gitlab_second", origin, "production", "owned")
		}
		runtime := generatedAdapterRuntime(db)
		job := claimReservationJob(t, runtime)
		_, err := runtime.Journal(job).Reserve(t.Context(), adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_TOKEN", Disposition: adapter.Create})
		if !errors.Is(err, adapter.ErrOperatorReview) {
			t.Fatalf("held alias on second page = %v", err)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_gitlab_a'`); got != 0 {
			t.Fatalf("page refusal wrote %d claims", got)
		}
	})
}

func TestAdapterOriginCustodyConcurrentAWSRegionalAliases(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "production")
		seedOriginGuardForeignTarget(t, db)
		execRealAdoption(t, db, `UPDATE adapters SET provider='aws-secrets-manager',origin='https://secretsmanager.us-east-1.amazonaws.com' WHERE id='adp_gitlab'`)
		execRealAdoption(t, db, `UPDATE adapters SET provider='aws-secrets-manager',origin='https://secretsmanager-fips.us-east-1.amazonaws.com' WHERE id='adp_origin_foreign'`)
		execRealAdoption(t, db, `UPDATE adapter_targets SET destination_kind='per-key',destination_owner='123456789012',destination_id=123456789012,repository_id=0,destination_scope='' WHERE id IN ('tgt_gitlab_a','tgt_gitlab_b')`)
		runtime := generatedAdapterRuntime(db)
		if _, err := runtime.Enqueue(t.Context(), adapter.Job{OrgID: "org_gitlab", ProjectID: "prj_gitlab", EnvironmentID: "env_gitlab_second", TargetID: "tgt_gitlab_b", Kind: adapter.Converge, AuthorityPrincipal: "usr_gitlab"}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		jobs := []adapter.Job{claimReservationJob(t, runtime), claimReservationJob(t, runtime)}
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, job := range jobs {
			go func() {
				<-start
				_, err := runtime.Journal(job).Reserve(t.Context(), adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_TOKEN", Disposition: adapter.Create})
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
				t.Fatalf("unexpected competing reservation result: %v", err)
			}
		}
		if successes != 1 {
			t.Fatalf("regional/FIPS concurrent reservation successes=%d, want exactly one", successes)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE destination_kind='per-key' AND destination_id=123456789012 AND normalized_name='P_TOKEN' AND state<>'released'`); got != 1 {
			t.Fatalf("equivalent AWS namespace held claims=%d", got)
		}
		// Every later attempt must observe the winning semantic namespace.
		for _, job := range jobs {
			_, err := runtime.Journal(job).Reserve(t.Context(), adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_TOKEN", Disposition: adapter.Create})
			if err != nil && !errors.Is(err, adapter.ErrOperatorReview) {
				t.Fatalf("retry reservation: %v", err)
			}
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE destination_kind='per-key' AND destination_id=123456789012 AND normalized_name='P_TOKEN' AND state<>'released'`); got != 1 {
			t.Fatalf("retry duplicated AWS custody=%d", got)
		}
	})
}

func TestAdapterOriginCustodyAWSAuthorityClasses(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "production")
		seedOriginGuardForeignTarget(t, db)
		execRealAdoption(t, db, `UPDATE adapters SET provider='aws-secrets-manager' WHERE id IN ('adp_gitlab','adp_origin_foreign')`)
		execRealAdoption(t, db, `UPDATE adapter_targets SET destination_kind='per-key',destination_owner='123456789012',destination_id=123456789012,repository_id=0,destination_scope='' WHERE id IN ('tgt_gitlab_a','tgt_gitlab_b')`)
		runtime := generatedAdapterRuntime(db)
		job := claimReservationJob(t, runtime)
		for _, tc := range []struct {
			name, current, foreign string
			refuse                 bool
		}{
			{name: "known_FIPS_same_region", foreign: "https://secretsmanager-fips.us-east-1.amazonaws.com", refuse: true},
			{name: "known_VPC_same_region", foreign: "https://vpce-0123456789abcdef0-abcde.secretsmanager.us-east-1.vpce.amazonaws.com", refuse: true},
			{name: "known_different_region", foreign: "https://secretsmanager.eu-west-1.amazonaws.com"},
			{name: "forged_custom_account_cannot_deny_known", foreign: "https://attacker.custom.test"},
			{name: "known_cannot_deny_custom", current: "https://owned.custom.test", foreign: "https://secretsmanager.us-east-1.amazonaws.com"},
			{name: "different_custom_account_not_authority", current: "https://owned.custom.test", foreign: "https://attacker.custom.test"},
			{name: "same_custom_authority_still_held", current: "https://owned.custom.test", foreign: "https://OWNED.custom.test:443/", refuse: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				execRealAdoption(t, db, `DELETE FROM adapter_ledger WHERE target_id IN ('tgt_gitlab_a','tgt_gitlab_b')`)
				current := tc.current
				if current == "" {
					current = "https://secretsmanager.us-east-1.amazonaws.com"
				}
				execRealAdoption(t, db, `UPDATE adapters SET origin=$1 WHERE id='adp_gitlab'`, current)
				execRealAdoption(t, db, `UPDATE adapters SET origin=$1 WHERE id='adp_origin_foreign'`, tc.foreign)
				execRealAdoption(t, db, `INSERT INTO adapter_ledger(id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,destination_scope,surface,effective_name,normalized_name,state,updated_at) VALUES ('led_aws_authority','org_gitlab','prj_gitlab','env_gitlab_second','tgt_gitlab_b',$1,'per-key',0,123456789012,'','secret','P_TOKEN','P_TOKEN','owned','2026-10-01T00:00:00Z')`, tc.foreign)
				_, err := runtime.Journal(job).Reserve(t.Context(), adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_TOKEN", Disposition: adapter.Create})
				if tc.refuse {
					if !errors.Is(err, adapter.ErrOperatorReview) {
						t.Fatalf("held namespace = %v", err)
					}
				} else if err != nil {
					t.Fatalf("independent authority refused: %v", err)
				}
			})
		}
	})
}

func TestAdapterOriginCustodyRechecksLeaseAfterBlockedMetadata(t *testing.T) {
	db := seededDB(t, openPostgres)
	seedGitLabMoves(t, db, "production")
	runtime := generatedAdapterRuntime(db)
	job := claimReservationJob(t, runtime)
	expires := time.Now().UTC().Add(2 * time.Second)
	execRealAdoption(t, db, `UPDATE adapter_outbox SET lease_expires_at=$1 WHERE id=$2`, expires, job.ID)
	held, err := db.PG().Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(context.Background())
	if _, err := held.Exec(t.Context(), `LOCK TABLE adapter_ledger IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := runtime.Journal(job).Reserve(ctx, adapter.Effect{Surface: adapter.Secret, EffectiveName: "P_TOKEN", Disposition: adapter.Create})
		finished <- err
	}()
	blockedDeadline := time.Now().Add(time.Second)
	for queryInt(t, db, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%l.provider_origin%'`) == 0 {
		if time.Now().After(blockedDeadline) {
			t.Fatal("origin metadata query did not reach held ledger table")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if wait := time.Until(expires) + 50*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	if err := held.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, adapter.ErrSuperseded) {
		t.Fatalf("metadata scan crossed lease expiry: %v", err)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='tgt_gitlab_a'`); got != 0 {
		t.Fatalf("expired lease wrote %d custody rows", got)
	}
}
