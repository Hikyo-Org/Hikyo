package isolation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// publishRevisions creates n keys (each key create materializes a schema
// revision with no changed keys) and then publishes n single-key value
// revisions into (A1, env_a1) as the custodian: 2n revisions, of which the n
// newest each carry exactly one changed key.
func publishRevisions(t *testing.T, db *store.DB, n int) {
	t.Helper()
	keys := keySvc(t, db)
	values := valueSvc(t, db)
	revisions := revisionSvc(t, db)
	scope := scopeEnv(orgA, prjA1, envA1)
	for i := range n {
		name := fmt.Sprintf("PAGE_KEY_%02d", i)
		spec := service.KeySpec{Name: name, Classification: string(schema.Config), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}
		if _, err := keys.Create(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), spec, nil); err != nil {
			t.Fatalf("create key %d: %v", i, err)
		}
	}
	for i := range n {
		name := fmt.Sprintf("PAGE_KEY_%02d", i)
		staged, err := values.Set(t.Context(), service.LocalPrincipal(custodian), scope, name, fmt.Sprintf("v-%d", i), nil)
		if err != nil {
			t.Fatalf("stage %d: %v", i, err)
		}
		if _, err := revisions.PublishPlanned(t.Context(), service.LocalPrincipal(custodian), scope, service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
}

// TestHistoryPageCostDoesNotGrowWithPageSize is the F04 regression: one
// history page issues exactly one lineage read whatever its size, and the
// total statement count of a page is the same for one revision as for five.
// The sqlite driver is the counting seam; the query shape is engine-neutral.
func TestHistoryPageCostDoesNotGrowWithPageSize(t *testing.T) {
	db := seededDB(t, func(t *testing.T) *store.DB {
		t.Helper()
		db, err := openIsolationFixture(t, store.Config{
			Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "history-page.db"), SQLiteDriver: corsQueryDriverName,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	})
	publishRevisions(t, db, 5)
	revisions := revisionSvc(t, db)
	actor := service.LocalPrincipal(custodian)
	scope := scopeEnv(orgA, prjA1, envA1)

	measure := func(limit int) (page []service.RevisionView, statements, lineageReads int64) {
		t.Helper()
		queriesBefore, lineageBefore := corsQueryDriver.queries.Load(), corsQueryDriver.lineageReads.Load()
		page, err := revisions.History(t.Context(), actor, scope, service.HistoryFromNewest, limit)
		if err != nil {
			t.Fatalf("history limit %d: %v", limit, err)
		}
		return page, corsQueryDriver.queries.Load() - queriesBefore, corsQueryDriver.lineageReads.Load() - lineageBefore
	}
	one, oneStatements, oneLineage := measure(1)
	five, fiveStatements, fiveLineage := measure(5)
	if len(one) != 1 || len(five) != 5 {
		t.Fatalf("page sizes = %d and %d, want 1 and 5", len(one), len(five))
	}
	if oneLineage != 1 || fiveLineage != 1 {
		t.Fatalf("lineage reads per page = %d (limit 1) and %d (limit 5), want exactly 1 each", oneLineage, fiveLineage)
	}
	if oneStatements != fiveStatements {
		t.Fatalf("statements per page = %d (limit 1) and %d (limit 5), want the page cost independent of its size", oneStatements, fiveStatements)
	}
	for _, view := range five {
		if len(view.ChangedKeys) != 1 {
			t.Fatalf("revision %d changed keys = %+v, want the one published key", view.Revision, view.ChangedKeys)
		}
	}
}

// TestHistoryPagingIsContiguous proves the cursor contract every surface
// relies on: paging from the newest by the smallest revision returned walks
// the whole history exactly once, newest first, with each page's lineage
// matching the unpaged view, and bad cursors or limits are refused by name.
func TestHistoryPagingIsContiguous(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		publishRevisions(t, db, 5)
		revisions := revisionSvc(t, db)
		actor := service.LocalPrincipal(custodian)
		scope := scopeEnv(orgA, prjA1, envA1)
		full, err := revisions.History(t.Context(), actor, scope, service.HistoryFromNewest, service.MaxHistoryLimit)
		if err != nil {
			t.Fatal(err)
		}
		if len(full) != 10 {
			t.Fatalf("history = %d revisions, want 10 (5 schema, 5 value)", len(full))
		}
		var paged []service.RevisionView
		before := service.HistoryFromNewest
		const limit = 3
		for pages := 0; ; pages++ {
			page, err := revisions.History(t.Context(), actor, scope, before, limit)
			if err != nil {
				t.Fatalf("page before %d: %v", before, err)
			}
			paged = append(paged, page...)
			if len(page) < limit {
				if pages != 3 {
					t.Fatalf("short page after %d full pages, want 3", pages)
				}
				break
			}
			before = page[len(page)-1].Revision
		}
		if len(paged) != len(full) {
			t.Fatalf("paged %d revisions, full %d", len(paged), len(full))
		}
		for i := range full {
			if paged[i].Revision != full[i].Revision || paged[i].PublishedByName != full[i].PublishedByName ||
				!slices.Equal(paged[i].ChangedKeys, full[i].ChangedKeys) {
				t.Fatalf("entry %d: paged %+v, full %+v", i, paged[i], full[i])
			}
			if wantChanged := 1; i >= 5 {
				wantChanged = 0
			} else if len(full[i].ChangedKeys) != wantChanged {
				t.Fatalf("entry %d (revision %d): changed keys %+v, want %d", i, full[i].Revision, full[i].ChangedKeys, wantChanged)
			}
			if i > 0 && paged[i].Revision != paged[i-1].Revision-1 {
				t.Fatalf("entry %d: revision %d does not follow %d", i, paged[i].Revision, paged[i-1].Revision)
			}
		}
		for _, bad := range []struct {
			before int64
			limit  int
		}{{0, 10}, {service.HistoryFromNewest, 0}, {service.HistoryFromNewest, service.MaxHistoryLimit + 1}} {
			if _, err := revisions.History(t.Context(), actor, scope, bad.before, bad.limit); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("history(before=%d, limit=%d) error = %v, want ErrInvalid", bad.before, bad.limit, err)
			}
		}
	})
}

// TestProjectRevisionsAggregatesLatestPerEnvironment is the F18 regression:
// the definitions pin reads one row per environment, the maximum revision,
// not the environment's lifetime history.
func TestProjectRevisionsAggregatesLatestPerEnvironment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": "postgres://dev-2"})
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": "postgres://dev-3"})
		if err := tx.Read(t.Context(), db, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
			p, err := az.Authorize(ctx, authz.Identity{Principal: identAdmin}, authz.OpDefinitionsPlanCreate, scopeProject(orgA, prjA1))
			if err != nil {
				return err
			}
			got, err := r.Snapshots().ProjectRevisions(ctx, p)
			if err != nil {
				return err
			}
			want := map[string]int64{string(envA1): 3, string(envProd): 1}
			if len(got) != len(want) {
				t.Fatalf("project revisions = %v, want %v", got, want)
			}
			for env, revision := range want {
				if got[env] != revision {
					t.Fatalf("project revisions = %v, want %v", got, want)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

// TestSecretOccurrenceRecordIsIdempotent is the F16 regression: a publish
// that re-materializes an unchanged secret value entry records its occurrence
// again without reading the lifetime occurrence history first, and the row
// stays unique.
func TestSecretOccurrenceRecordIsIdempotent(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM secret_value_occurrences WHERE environment_id = 'env_a1'`); n != 1 {
			t.Fatalf("occurrences after the first publish = %d, want 1", n)
		}
		// The secret cell is unchanged; only the config cell moves, so the
		// second publish re-materializes the same secret value entry.
		publishDeliveryValues(t, db, envA1, map[string]string{"DATABASE_URL": "postgres://dev-2"})
		if n := queryInt(t, db, `SELECT COUNT(*) FROM secret_value_occurrences WHERE environment_id = 'env_a1'`); n != 1 {
			t.Fatalf("occurrences after re-materializing the same entry = %d, want 1", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM snapshots WHERE environment_id = 'env_a1'`); n != 2 {
			t.Fatalf("snapshots = %d, want 2", n)
		}
	})
}
