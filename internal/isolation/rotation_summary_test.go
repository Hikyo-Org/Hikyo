package isolation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// S4 of #148: halt_reason and the last-run / owed-drop summary on the rotation
// itself. Both come from one read for a whole page, never one per rotation, and
// owed_drop_runs is by construction the number of runs the runs list marks as owing
// a drop: phase revoke_refused or quarantine, whatever the error code.

// rotationKeyPair creates one more password and username key and returns their ids.
func rotationKeyPair(t *testing.T, db *store.DB, n int) (password, username string) {
	t.Helper()
	password, username = fmt.Sprintf("key_sum_pw_%d", n), fmt.Sprintf("key_sum_user_%d", n)
	rotationKey(t, db, password, fmt.Sprintf("SUM_PASSWORD_%d", n), "secret")
	rotationKey(t, db, username, fmt.Sprintf("SUM_USER_%d", n), "config")
	return password, username
}

// seedRunState gives a rotation the run history one scenario needs and returns the
// run ids, oldest first. The rows are raw because the phases (quarantine,
// revoke_refused) come from the executor's own error paths.
func seedRunState(t *testing.T, db *store.DB, f *rotationFixture, rotationID string, runs []sumRun) {
	t.Helper()
	for _, r := range runs {
		revision := ""
		if r.published {
			revision = "7"
		}
		rotationRunRow(t, db, f, r.id, rotationID, r.phase, revision, "")
		if r.code != "" {
			execRaw(t, db, `UPDATE rotation_runs SET last_error='`+r.code+`' WHERE id='`+r.id+`'`)
		}
	}
}

type sumRun struct {
	id, phase, code string
	published       bool
}

// countOwedRuns is the definition every surface shares, applied to the runs list a
// person reads: the CLI ATTENTION lines and the browser notice count these.
func countOwedRuns(t *testing.T, f *rotationFixture, rotationID string) (owed, versionsChanged int64) {
	t.Helper()
	page, err := f.svc.Runs(tctx(t), f.actor, f.scope, service.RotationRunsRequest{RotationID: rotationID, Limit: 500})
	if err != nil {
		t.Fatalf("runs of %s: %v", rotationID, err)
	}
	for _, run := range page.Items {
		if run.Phase != "revoke_refused" && run.Phase != "quarantine" {
			continue
		}
		owed++
		if run.ErrorCode == "published_versions_changed" {
			versionsChanged++
		}
	}
	return owed, versionsChanged
}

func TestRotationSummaryCountsOwedDropsAndLastRun(t *testing.T) { forEngines(t, runRotationSummary) }

func runRotationSummary(t *testing.T, db *store.DB) {
	f := newRotationFixture(t, db)

	// A: a settled run, a quarantine, and a drop a restore refused (the newest run).
	a := f.create(rotPasswordKey, rotUsernameKey)
	seedRunState(t, db, f, a.ID, []sumRun{
		{id: "run_sum_a1", phase: "done", published: true},
		{id: "run_sum_a2", phase: "quarantine", code: "provider_unavailable"},
		{id: "run_sum_a3", phase: "revoke_refused", code: "published_versions_changed", published: true},
	})
	// B: no run at all.
	b := f.create(rotSecondKey, rotSecondUser)
	// C: settled runs only: nothing owed.
	pw, user := rotationKeyPair(t, db, 3)
	c := f.create(pw, user)
	seedRunState(t, db, f, c.ID, []sumRun{
		{id: "run_sum_c1", phase: "done", published: true},
		{id: "run_sum_c2", phase: "failed", code: "mint_failed"},
	})
	// D: a refused drop that is not a restore case.
	pw, user = rotationKeyPair(t, db, 4)
	d := f.create(pw, user)
	seedRunState(t, db, f, d.ID, []sumRun{
		{id: "run_sum_d1", phase: "revoke_refused", code: "role_owns_objects", published: true},
	})
	// E: deleted with a quarantine its delete left behind.
	pw, user = rotationKeyPair(t, db, 5)
	e := f.create(pw, user)
	seedRunState(t, db, f, e.ID, []sumRun{{id: "run_sum_e1", phase: "quarantine"}})
	if _, err := f.transition(f.actor, "delete", e.ID); err != nil {
		t.Fatalf("delete %s: %v", e.ID, err)
	}

	type want struct {
		owed, changed int64
		last          string // run id, "" for none
		phase, code   string
	}
	wants := map[string]want{
		a.ID: {2, 1, "run_sum_a3", "revoke_refused", "published_versions_changed"},
		b.ID: {0, 0, "", "", ""},
		c.ID: {0, 0, "run_sum_c2", "failed", "mint_failed"},
		d.ID: {1, 0, "run_sum_d1", "revoke_refused", "role_owns_objects"},
		e.ID: {1, 0, "run_sum_e1", "quarantine", ""},
	}
	check := func(source string, v service.RotationView) {
		t.Helper()
		w, ok := wants[v.ID]
		if !ok {
			return
		}
		if v.OwedDropRuns != w.owed || v.OwedDropRunsVersionsChanged != w.changed {
			t.Errorf("%s: rotation %s owes %d (%d restore-refused), want %d (%d)", source, v.ID, v.OwedDropRuns, v.OwedDropRunsVersionsChanged, w.owed, w.changed)
		}
		switch {
		case w.last == "" && v.LastRunSummary != nil:
			t.Errorf("%s: rotation %s with no run has last run %+v", source, v.ID, *v.LastRunSummary)
		case w.last != "" && v.LastRunSummary == nil:
			t.Errorf("%s: rotation %s has no last run, want %s", source, v.ID, w.last)
		case w.last != "" && (v.LastRunSummary.ID != w.last || v.LastRunSummary.Phase != w.phase || v.LastRunSummary.ErrorCode != w.code || v.LastRunSummary.PhaseAt.IsZero()):
			t.Errorf("%s: rotation %s last run %+v, want %s %s %q", source, v.ID, *v.LastRunSummary, w.last, w.phase, w.code)
		}
		// The summary is the runs list, counted the way every surface counts it.
		owed, changed := countOwedRuns(t, f, v.ID)
		if owed != v.OwedDropRuns || changed != v.OwedDropRunsVersionsChanged {
			t.Errorf("%s: rotation %s summary %d/%d disagrees with its runs list %d/%d", source, v.ID, v.OwedDropRuns, v.OwedDropRunsVersionsChanged, owed, changed)
		}
	}

	items, err := f.list(f.actor, f.scope, true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	seen := 0
	for _, v := range items {
		if _, ok := wants[v.ID]; ok {
			seen++
		}
		check("list", v)
		check("show", f.state(v.ID))
	}
	if seen != len(wants) {
		t.Fatalf("list returned %d of the %d seeded rotations", seen, len(wants))
	}

	// A restore holds every rotation. The hold changes nothing about what is owed,
	// and the part a restore refused stays counted so no surface reads it as "drop it".
	holdRestored(t, db)
	held := f.state(a.ID)
	if !held.RestoreHold || held.OwedDropRuns != 2 || held.OwedDropRunsVersionsChanged != 1 {
		t.Fatalf("held rotation: hold=%v owes %d (%d restore-refused), want true 2 (1)", held.RestoreHold, held.OwedDropRuns, held.OwedDropRunsVersionsChanged)
	}
}

// halt_reason reaches the view only as one of the fixed words: a stored value the
// server did not write, or none at all, reads as unknown, and a rotation that is
// not halted has none whatever its column holds.
func TestRotationHaltReasonIsAFixedWord(t *testing.T) { forEngines(t, runRotationHaltReason) }

func runRotationHaltReason(t *testing.T, db *store.DB) {
	f := newRotationFixture(t, db)
	secretish := "provider said: password=hunter2 for role app_x"
	cases := []struct {
		name, stored, want string
	}{
		{"retries_exhausted", "'retries_exhausted'", "retries_exhausted"},
		{"authority_lost", "'authority_lost'", "authority_lost"},
		{"gate_failed", "'gate_failed'", "gate_failed"},
		{"incident_contained", "'incident_contained'", "incident_contained"},
		{"legacy text", "'" + secretish + "'", "unknown"},
		{"a near miss", "'Retries_Exhausted'", "unknown"},
		{"no reason", "NULL", "unknown"},
	}
	ids := make([]string, len(cases))
	for i, tc := range cases {
		pw, user := rotationKeyPair(t, db, 10+i)
		ids[i] = f.create(pw, user).ID
		execRaw(t, db, `UPDATE rotations SET state='halted', halt_reason=`+tc.stored+`, next_run_at=NULL WHERE id='`+ids[i]+`'`)
	}
	active := f.create(rotPasswordKey, rotUsernameKey)

	items, err := f.list(f.actor, f.scope, false)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]service.RotationView{}
	for _, v := range items {
		byID[v.ID] = v
	}
	for i, tc := range cases {
		for source, v := range map[string]service.RotationView{"list": byID[ids[i]], "show": f.state(ids[i])} {
			if v.State != "halted" || v.HaltReason != tc.want {
				t.Errorf("%s (%s): state %s halt_reason %q, want halted %q", tc.name, source, v.State, v.HaltReason, tc.want)
			}
			if strings.Contains(v.HaltReason, "hunter2") {
				t.Errorf("%s (%s): the stored text reached the view: %q", tc.name, source, v.HaltReason)
			}
		}
	}
	if got := byID[active.ID]; got.State != "active" || got.HaltReason != "" {
		t.Fatalf("an active rotation has halt_reason %q", got.HaltReason)
	}
}

// The generated RotationLastRuns query is bounded: one row per requested rotation
// whatever its history, nothing for an unrequested rotation and nothing under another
// organisation, so a page of rotations never reads a run history.
func TestRotationLastRunsQueryIsBounded(t *testing.T) {
	// The query is called inside this body: the reviewed pin for RotationLastRuns names it.
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newRotationFixture(t, db)
		const history = 40
		a := f.create(rotPasswordKey, rotUsernameKey)
		pw, user := rotationKeyPair(t, db, 20)
		b := f.create(pw, user)
		for _, id := range []string{a.ID, b.ID} {
			runs := make([]sumRun, 0, history)
			for i := range history {
				// The newest run is an unpublished failure; the older ones are published and done.
				run := sumRun{id: fmt.Sprintf("run_lr_%s_%03d", id[len(id)-4:], i), phase: "done", published: true}
				if i == history-1 {
					run.phase, run.published = "failed", false
				}
				runs = append(runs, run)
			}
			seedRunState(t, db, f, id, runs)
		}
		rowsFor := func(org string, ids ...string) []string {
			t.Helper()
			ctx := tctx(t)
			var out []string
			if db.Engine() == store.EngineSQLite {
				rows, err := sqlitegen.New(db.SQLiteWrite()).RotationLastRuns(ctx, sqlitegen.RotationLastRunsParams{ChainOrg: org, ChainProject: "prj_a1", ChainEnv: string(f.env), RotationIds: ids})
				if err != nil {
					t.Fatalf("RotationLastRuns: %v", err)
				}
				for _, r := range rows {
					out = append(out, r.ID)
				}
				return out
			}
			rows, err := pggen.New(db.PG()).RotationLastRuns(ctx, pggen.RotationLastRunsParams{ChainOrg: org, ChainProject: "prj_a1", ChainEnv: string(f.env), RotationIds: ids})
			if err != nil {
				t.Fatalf("RotationLastRuns: %v", err)
			}
			for _, r := range rows {
				out = append(out, r.ID)
			}
			return out
		}
		if got := rowsFor("org_a", a.ID, b.ID); len(got) != 2 {
			t.Fatalf("last runs of two rotations over %d runs each = %v, want 2 rows", history, got)
		}
		if got := rowsFor("org_a", a.ID); len(got) != 1 || got[0] != fmt.Sprintf("run_lr_%s_%03d", a.ID[len(a.ID)-4:], history-1) {
			t.Fatalf("last run of one rotation = %v, want its newest run id", got)
		}
		if got := rowsFor("org_a"); len(got) != 0 {
			t.Fatalf("last runs of no requested rotation = %v, want none", got)
		}
		if got := rowsFor("org_b", a.ID, b.ID); len(got) != 0 {
			t.Fatalf("last runs under another org = %v, want none", got)
		}
	})
}
