package isolation

import (
	"context"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// The intentionally simple quadratic Go reference is independent of the SQL
// optimization. Its tiny corpus varies policies, sparse history, collection,
// exact age boundaries and protected roots across tenant chains.
func TestRetentionRankingReference(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			var db *store.DB
			if engine == store.EngineSQLite {
				db = openSQLite(t)
			} else {
				db = openPostgres(t)
			}
			now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
			exec := func(q string) { t.Helper(); execRaw(t, db, q) }
			exec("INSERT INTO principals(id,kind,class,created_at) VALUES('mch_ref','machine','workload','2020-01-01T00:00:00Z'),('usr_ref','human','human','2020-01-01T00:00:00Z')")
			type policy struct {
				count     int
				unlimited bool
			}
			policies := []policy{{3, false}, {1, false}, {7, false}, {3, true}, {2, false}}
			type snapshot struct {
				id, org, project, env string
				revision              int
				published             time.Time
				present, protected    bool
				policy                policy
			}
			var corpus []snapshot
			for group, p := range policies {
				org, project := fmt.Sprintf("org_ref_%d", group), fmt.Sprintf("prj_ref_%d", group)
				mode := "keep-if-either"
				if p.unlimited || group == 4 {
					mode = "unlimited"
				}
				exec(fmt.Sprintf("INSERT INTO orgs(id,name,active,metadata,created_at,retention_revision_count,retention_age_seconds,retention_mode) VALUES('%s','%s',TRUE,'{}','2020-01-01T00:00:00Z',3,86400,'%s')", org, org, mode))
				count, age := "NULL", "NULL"
				if group != 0 && group != 3 {
					count, age = fmt.Sprint(p.count), "86400"
				}
				exec(fmt.Sprintf("INSERT INTO projects(id,org_id,name,created_at,retention_revision_count,retention_age_seconds) VALUES('%s','%s','ref','2020-01-01T00:00:00Z',%s,%s)", project, org, count, age))
				// An empty environment, fewer-than-N histories, and a displaced history.
				for envIndex, size := range []int{0, 2, 8} {
					env := fmt.Sprintf("env_ref_%d_%d", group, envIndex)
					exec(fmt.Sprintf("INSERT INTO environments(id,org_id,project_id,name,note,display_order,created_at) VALUES('%s','%s','%s','env-%d','',%d,'2020-01-01T00:00:00Z')", env, org, project, envIndex, envIndex))
					for i := 0; i < size; i++ {
						s := snapshot{id: fmt.Sprintf("snp_ref_%d_%d_%d", group, envIndex, i), org: org, project: project, env: env, revision: i*i*17 + 1, published: now.Add(-48 * time.Hour), present: i != size-1, policy: p}
						if i == 1 {
							s.published = now.Add(-24 * time.Hour)
						} // exact age boundary stays
						if i == 2 {
							s.published = now.Add(-24*time.Hour + time.Second)
						}
						if i == 3 {
							s.published = now.Add(-24*time.Hour - time.Second)
						}
						s.protected = group == 0 && envIndex == 2 && i == 0
						present, at, reason := "TRUE", "NULL", ""
						if !s.present {
							present, at, reason = "FALSE", "'2026-10-01T00:00:00Z'", "prior-policy"
						}
						exec(fmt.Sprintf("INSERT INTO snapshots(id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at,payload_present,collected_at,collected_policy) VALUES('%s','%s','%s','%s',%d,1,'usr_ref','%s',%s,%s,'%s')", s.id, org, project, env, s.revision, s.published.Format(time.RFC3339), present, at, reason))
						if s.protected {
							exec(fmt.Sprintf("INSERT INTO self_config_retention(slot,snapshot_id) VALUES('desired','%s')", s.id))
						}
						if group == 1 && envIndex == 2 && i == 0 {
							// Expiry exactly at now no longer protects an old payload.
							exec(fmt.Sprintf("INSERT INTO revision_pins(id,org_id,project_id,environment_id,workload_principal_id,snapshot_id,revision,authority_principal_id,expires_at,created_at,authorized_at,history_authorized,schema_override) VALUES('pin_ref','%s','%s','%s','mch_ref','%s',%d,'usr_ref','2026-10-16T12:00:00Z','2020-01-01T00:00:00Z','2020-01-01T00:00:00Z',TRUE,FALSE)", org, project, env, s.id, s.revision))
						}
						corpus = append(corpus, s)
					}
				}
			}
			// Tightening and widening are re-read on the next transaction. The old
			// collected lineage continues to rank even when the window grows.
			for _, lastN := range []int{3, 1, 100, math.MaxInt64, 2} {
				exec(fmt.Sprintf("UPDATE orgs SET retention_revision_count=%d WHERE id='org_ref_0'", lastN))
				for i := range corpus {
					if corpus[i].org == "org_ref_0" {
						corpus[i].policy.count = lastN
					}
				}
				var want []string
				for _, s := range corpus {
					newer := 0
					for _, n := range corpus {
						if n.org == s.org && n.project == s.project && n.env == s.env && n.revision > s.revision {
							newer++
						}
					}
					if s.present && !s.protected && !s.policy.unlimited && s.published.Before(now.Add(-24*time.Hour)) && newer >= s.policy.count {
						want = append(want, s.id)
					}
				}
				// Fixture IDs sort in the same order as tenant chain and revision.
				slices.Sort(want)
				for _, limit := range []int{1, 4, 100} {
					var got []string
					err := storetx.Write(t.Context(), db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
						p, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
						if err != nil {
							return err
						}
						rows, err := r.Retention().Eligible(ctx, p, now, limit)
						if err != nil {
							return err
						}
						for _, row := range rows {
							got = append(got, row.ID)
						}
						return nil
					})
					expected := want[:min(len(want), limit)]
					if err != nil || !slices.Equal(got, expected) {
						t.Fatalf("N=%d limit=%d got=%v err=%v want=%v", lastN, limit, got, err, expected)
					}
				}
			}
		})
	}
}
