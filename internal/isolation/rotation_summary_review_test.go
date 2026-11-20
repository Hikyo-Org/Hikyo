package isolation

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// assertLastRunIsNewestListed pins the definition of "last run": the summary's run is
// the first row of the runs list a person reads (newest by id), on runs the executor
// itself started. The summary orders by (started_at, id) so one index serves it; the
// two agree because a run's id is a time-ordered UUIDv7 made at the instant the same
// clock stamps started_at, and a rotation has at most one open run at a time.
func assertLastRunIsNewestListed(t *testing.T, f *rotationFixture, rotationID string) {
	t.Helper()
	runs, err := f.svc.Runs(tctx(t), f.actor, f.scope, service.RotationRunsRequest{RotationID: rotationID, Limit: 500})
	if err != nil || len(runs.Items) == 0 {
		t.Fatalf("runs of %s: %d, %v", rotationID, len(runs.Items), err)
	}
	last := f.state(rotationID).LastRunSummary
	if last == nil || last.ID != runs.Items[0].ID || last.Phase != runs.Items[0].Phase || last.ErrorCode != runs.Items[0].ErrorCode {
		t.Fatalf("last run %+v is not the newest listed run %+v", last, runs.Items[0])
	}
}

// The default list still shows a deleted rotation that owns a run owing a provider
// role a drop, so the warning does not disappear with the delete; a deleted rotation
// that owes nothing stays out. Pages stay stable in id order across that boundary.
func TestRotationDefaultListKeepsDeletedRotationsThatOweADrop(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newRotationFixture(t, db)
		deleted := func(n int, runs ...sumRun) string {
			pw, user := rotationKeyPair(t, db, n)
			r := f.create(pw, user)
			for i := range runs {
				runs[i].id += "_" + r.ID[len(r.ID)-6:]
			}
			// Deleted first, runs after: a delete refuses an open run, but a restore or a
			// racing run can leave a refused or quarantined one on a deleted rotation.
			if _, err := f.transition(f.actor, "delete", r.ID); err != nil {
				t.Fatalf("delete %s: %v", r.ID, err)
			}
			seedRunState(t, db, f, r.ID, runs)
			return r.ID
		}
		active := func(n int) string {
			pw, user := rotationKeyPair(t, db, n)
			return f.create(pw, user).ID
		}
		// Created in this order, so id order is this order.
		act1 := active(40)
		clean := deleted(41, sumRun{id: "run_dl_done", phase: "done", published: true})
		quarantined := deleted(42, sumRun{id: "run_dl_q", phase: "quarantine"})
		act2 := active(43)
		none := deleted(44)
		refused := deleted(45, sumRun{id: "run_dl_old", phase: "done", published: true}, sumRun{id: "run_dl_zz", phase: "revoke_refused", code: "role_owns_objects", published: true})
		_ = clean
		_ = none
		want := []string{act1, quarantined, act2, refused}

		for _, limit := range []int{1, 2, 3, 100} {
			var got []string
			after := ""
			for pages := 0; ; pages++ {
				if pages > 10 {
					t.Fatalf("limit %d: paging never ended after %v", limit, got)
				}
				page, err := f.svc.List(tctx(t), f.actor, f.scope, service.RotationListRequest{After: after, Limit: limit})
				if err != nil {
					t.Fatalf("limit %d: %v", limit, err)
				}
				if len(page.Items) > limit {
					t.Fatalf("limit %d: a page of %d", limit, len(page.Items))
				}
				for _, v := range page.Items {
					got = append(got, v.ID)
					switch v.ID {
					case quarantined, refused:
						if v.State != "disabled" || v.OwedDropRuns != 1 {
							t.Errorf("limit %d: deleted %s is %s owing %d, want disabled owing 1", limit, v.ID, v.State, v.OwedDropRuns)
						}
					default:
						if v.OwedDropRuns != 0 {
							t.Errorf("limit %d: %s owes %d, want 0", limit, v.ID, v.OwedDropRuns)
						}
					}
				}
				if page.NextAfter == "" {
					break
				}
				after = page.NextAfter
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("limit %d: default list = %v, want %v", limit, got, want)
			}
		}

		all, err := f.list(f.actor, f.scope, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 6 {
			t.Fatalf("include_disabled lists %d rotations, want all 6", len(all))
		}

		// The generated RotationListOrOwed query itself (its reviewed pin names this test):
		// the same four rotations for the addressed chain, none under another org.
		listed := func(org string) []string {
			t.Helper()
			var ids []string
			if db.Engine() == store.EngineSQLite {
				rows, err := sqlitegen.New(db.SQLiteWrite()).RotationListOrOwed(tctx(t), sqlitegen.RotationListOrOwedParams{ChainOrg: org, ChainProject: "prj_a1", ChainEnv: string(f.env), PageLimit: 100})
				if err != nil {
					t.Fatalf("RotationListOrOwed: %v", err)
				}
				for _, r := range rows {
					ids = append(ids, r.ID)
				}
				return ids
			}
			rows, err := pggen.New(db.PG()).RotationListOrOwed(tctx(t), pggen.RotationListOrOwedParams{ChainOrg: org, ChainProject: "prj_a1", ChainEnv: string(f.env), PageLimit: 100})
			if err != nil {
				t.Fatalf("RotationListOrOwed: %v", err)
			}
			for _, r := range rows {
				ids = append(ids, r.ID)
			}
			return ids
		}
		if got := listed("org_a"); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("RotationListOrOwed = %v, want %v", got, want)
		}
		if got := listed("org_b"); len(got) != 0 {
			t.Fatalf("RotationListOrOwed under another org = %v, want none", got)
		}
	})
}

// A run's error code reaches the summary and the runs list only as one of the fixed
// words the executor writes (or `abandoned`): any other stored text reads as unknown,
// so an escape sequence or a secret-looking value never reaches a terminal or a page.
func TestRotationErrorCodeIsAFixedWordInSummaryAndRunsList(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newRotationFixture(t, db)
		for i, tc := range []struct{ stored, want string }{
			{"published_versions_changed", "published_versions_changed"},
			{"abandoned", "abandoned"},
			{"x\x1b[31mred password=hunter2", "unknown"},
			{"Provider_Unavailable", "unknown"},
		} {
			pw, user := rotationKeyPair(t, db, 50+i)
			r := f.create(pw, user)
			seedRunState(t, db, f, r.ID, []sumRun{{id: "run_ec_" + r.ID[len(r.ID)-6:], phase: "failed"}})
			// Written raw: only a row the server did not write can hold such text.
			execRaw(t, db, `UPDATE rotation_runs SET last_error='`+tc.stored+`' WHERE rotation_id='`+r.ID+`'`)
			last := f.state(r.ID).LastRunSummary
			runs, err := f.svc.Runs(tctx(t), f.actor, f.scope, service.RotationRunsRequest{RotationID: r.ID, Limit: 10})
			if err != nil || len(runs.Items) != 1 {
				t.Fatalf("runs: %d, %v", len(runs.Items), err)
			}
			if last == nil || last.ErrorCode != tc.want || runs.Items[0].ErrorCode != tc.want {
				t.Errorf("stored %q: summary %+v and runs list %q, want %q in both", tc.stored, last, runs.Items[0].ErrorCode, tc.want)
			}
		}
	})
}

// The owed-drop count reads only the requested rotations of the addressed chain:
// another organisation, environment or rotation never leaks into it.
func TestRotationOwedDropCountsStayInsideTheirChain(t *testing.T) {
	// The generated query is called inside this body: its reviewed scope is this test.
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newRotationFixture(t, db)
		a := f.create(rotPasswordKey, rotUsernameKey)
		pw, user := rotationKeyPair(t, db, 60)
		b := f.create(pw, user)
		seedRunState(t, db, f, a.ID, []sumRun{
			{id: "run_oc_a1", phase: "quarantine"},
			{id: "run_oc_a2", phase: "revoke_refused", code: "published_versions_changed", published: true},
		})
		seedRunState(t, db, f, b.ID, []sumRun{{id: "run_oc_b1", phase: "quarantine"}})

		count := func(org, project, env string, ids ...string) map[string][2]int64 {
			t.Helper()
			out := map[string][2]int64{}
			ctx := tctx(t)
			if db.Engine() == store.EngineSQLite {
				rows, err := sqlitegen.New(db.SQLiteWrite()).RotationOwedDropCounts(ctx, sqlitegen.RotationOwedDropCountsParams{ChainOrg: org, ChainProject: project, ChainEnv: env, RotationIds: ids})
				if err != nil {
					t.Fatalf("RotationOwedDropCounts: %v", err)
				}
				for _, r := range rows {
					out[r.RotationID] = [2]int64{r.Owed, r.VersionsChanged}
				}
				return out
			}
			rows, err := pggen.New(db.PG()).RotationOwedDropCounts(ctx, pggen.RotationOwedDropCountsParams{ChainOrg: org, ChainProject: project, ChainEnv: env, RotationIds: ids})
			if err != nil {
				t.Fatalf("RotationOwedDropCounts: %v", err)
			}
			for _, r := range rows {
				out[r.RotationID] = [2]int64{r.Owed, r.VersionsChanged}
			}
			return out
		}
		env := string(f.env)
		if got := count("org_a", "prj_a1", env, a.ID, b.ID); got[a.ID] != [2]int64{2, 1} || got[b.ID] != [2]int64{1, 0} || len(got) != 2 {
			t.Fatalf("both rotations = %v, want %s:2/1 and %s:1/0", got, a.ID, b.ID)
		}
		if got := count("org_a", "prj_a1", env, a.ID); len(got) != 1 || got[a.ID] != [2]int64{2, 1} {
			t.Fatalf("only the requested rotation counts: %v", got)
		}
		for name, got := range map[string]map[string][2]int64{
			"another org":         count("org_b", "prj_a1", env, a.ID, b.ID),
			"another project":     count("org_a", "prj_other", env, a.ID, b.ID),
			"another environment": count("org_a", "prj_a1", "env_other", a.ID, b.ID),
			"no rotation":         count("org_a", "prj_a1", env, "rot_not_here"),
		} {
			if len(got) != 0 {
				t.Errorf("%s: counted %v, want nothing", name, got)
			}
		}
	})
}

// A mutation's response carries the same summary a show would: a pause or delete of a
// rotation that owes a drop, and the release of its restore hold, answer with the
// counts and the last run, not zeros.
func TestRotationMutationResponsesCarryTheSummary(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newRotationFixture(t, db)
		r := f.create(rotPasswordKey, rotUsernameKey)
		seedRunState(t, db, f, r.ID, []sumRun{
			{id: "run_mr_1", phase: "quarantine"},
			{id: "run_mr_2", phase: "revoke_refused", code: "published_versions_changed", published: true},
		})
		check := func(step string, v service.RotationView) {
			t.Helper()
			if v.OwedDropRuns != 2 || v.OwedDropRunsVersionsChanged != 1 || v.LastRunSummary == nil || v.LastRunSummary.ID != "run_mr_2" {
				t.Fatalf("%s answered owing %d (%d), last %+v; want 2 (1) and run_mr_2", step, v.OwedDropRuns, v.OwedDropRunsVersionsChanged, v.LastRunSummary)
			}
		}
		paused, err := f.transition(f.actor, "pause", r.ID)
		if err != nil {
			t.Fatal(err)
		}
		check("pause", paused)
		holdRestored(t, db)
		released, err := f.svc.ReleaseHold(tctx(t), f.actor, f.scope, service.RotationStateRequest{RotationID: r.ID})
		if err != nil {
			t.Fatalf("release hold: %v", err)
		}
		check("release-hold", released)
		// A delete refuses an open run: the refused drop settles into quarantine first.
		execRaw(t, db, `UPDATE rotation_runs SET phase='quarantine' WHERE id='run_mr_2'`)
		deleted, err := f.transition(f.actor, "delete", r.ID)
		if err != nil {
			t.Fatal(err)
		}
		check("delete", deleted)
	})
}
