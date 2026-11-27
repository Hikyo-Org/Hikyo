package isolation

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
	"github.com/Hikyo-Org/hikyo/internal/testutil/reflectx"
)

// Migration 00102 (#1447, owner decision 2026-11-26 option (c)) moves
// environment items from ids to names. Each rule must reach exactly what it
// reached before: consistent rules convert in place, others split per project
// (audited with the old and new ids), and undecidable ones fail closed.

type envMigrationDB struct {
	cfg  store.Config
	exec func(string) error
	// unchecked runs one statement with foreign keys unenforced, as data
	// written without them was.
	unchecked func(string) error
	count     func(string) int
	text      func(string) string
	rules     *authn.Resolver
	events    func(where string) []migratedEvent
	// historical is the guarded-recovery resolver bound to a source schema.
	historical func(version uint64) *authn.Resolver
}

// migratedEvent is an audit row in its stored text form.
type migratedEvent struct {
	id, typ, outcome, origin, actorClass, objectType, objectID, occurred, recorded, payload string
	schemaVersion                                                                           int
}

// eventsQuery selects the columns of migratedEvent; times render as the
// store's text form on PostgreSQL, where they are TIMESTAMPTZ.
func eventsQuery(pg bool, where string) string {
	occurred, recorded := "occurred_at", "recorded_at"
	if pg {
		occurred = `to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`
		recorded = `to_char(recorded_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`
	}
	return `SELECT id, type, outcome, origin, actor_class, object_type, object_id, ` + occurred + `, ` + recorded + `, payload, schema_version FROM audit_tenant_events ` + where
}

func TestRuleEnvNamesMigrationSQLite(t *testing.T) {
	testRuleEnvNamesMigration(t, envNamesMigrationDB(t, store.EngineSQLite))
}

func TestRuleEnvNamesMigrationPostgres(t *testing.T) {
	testRuleEnvNamesMigration(t, envNamesMigrationDB(t, store.EnginePostgres))
}

// envNamesMigrationDB is a scratch database of the engine, unmigrated.
func envNamesMigrationDB(t *testing.T, engine store.Engine) envMigrationDB {
	t.Helper()
	if engine == store.EnginePostgres {
		return envNamesMigrationPostgres(t)
	}
	cfg, db := scratchSQLite(t, "env-names.db")
	ctx := t.Context()
	return envMigrationDB{
		cfg:  cfg,
		exec: func(s string) error { _, err := db.ExecContext(ctx, s); return err },
		unchecked: func(s string) error {
			_, err := db.ExecContext(ctx, "PRAGMA foreign_keys = OFF; "+s+"; PRAGMA foreign_keys = ON")
			return err
		},
		count: func(s string) int {
			var n int
			if err := db.QueryRowContext(ctx, s).Scan(&n); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
			return n
		},
		text: func(s string) string {
			var v string
			if err := db.QueryRowContext(ctx, s).Scan(&v); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
			return v
		},
		rules: authn.NewSQLite(db),
		events: func(where string) []migratedEvent {
			rows, err := db.QueryContext(ctx, eventsQuery(false, where))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rows.Close() }()
			var out []migratedEvent
			for rows.Next() {
				var e migratedEvent
				if err := rows.Scan(&e.id, &e.typ, &e.outcome, &e.origin, &e.actorClass, &e.objectType, &e.objectID, &e.occurred, &e.recorded, &e.payload, &e.schemaVersion); err != nil {
					t.Fatal(err)
				}
				out = append(out, e)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return out
		},
		historical: func(v uint64) *authn.Resolver { return authn.NewHistoricalRecoverySQLite(db, v) },
	}
}

func envNamesMigrationPostgres(t *testing.T) envMigrationDB {
	t.Helper()
	cfg, conn := scratchPostgres(t, "env_names")
	ctx := t.Context()
	return envMigrationDB{
		cfg:  cfg,
		exec: func(s string) error { _, err := conn.Exec(ctx, s); return err },
		unchecked: func(s string) error {
			_, err := conn.Exec(ctx, "BEGIN; SET LOCAL session_replication_role = replica; "+s+"; COMMIT")
			return err
		},
		count: func(s string) int {
			var n int
			if err := conn.QueryRow(ctx, s).Scan(&n); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
			return n
		},
		text: func(s string) string {
			var v string
			if err := conn.QueryRow(ctx, s).Scan(&v); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
			return v
		},
		rules: authn.NewPG(conn),
		events: func(where string) []migratedEvent {
			rows, err := conn.Query(ctx, eventsQuery(true, where))
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var out []migratedEvent
			for rows.Next() {
				var e migratedEvent
				if err := rows.Scan(&e.id, &e.typ, &e.outcome, &e.origin, &e.actorClass, &e.objectType, &e.objectID, &e.occurred, &e.recorded, &e.payload, &e.schemaVersion); err != nil {
					t.Fatal(err)
				}
				out = append(out, e)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return out
		},
		historical: func(v uint64) *authn.Resolver { return authn.NewHistoricalRecoveryPG(conn, v) },
	}
}

func testRuleEnvNamesMigration(t *testing.T, db envMigrationDB) {
	ctx := t.Context()
	if err := migrate.RunUpTo(ctx, db.cfg, 99); err != nil {
		t.Fatal(err)
	}
	const at = `'2026-01-01T00:00:00Z'`
	stmts := []string{
		`INSERT INTO orgs (id, name, active, metadata, created_at) VALUES ('org_en', 'en', TRUE, '{}', ` + at + `)`,
		`INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_a', 'org_en', 'a', ` + at + `)`,
		`INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_b', 'org_en', 'b', ` + at + `)`,
	}
	for i, env := range []struct{ id, project, name string }{
		{"env_a_prod", "prj_a", "prod"}, {"env_a_dev", "prj_a", "dev"},
		{"env_b_prod", "prj_b", "prod"}, {"env_b_dev", "prj_b", "dev"},
	} {
		stmts = append(stmts, `INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('`+
			env.id+`', 'org_en', '`+env.project+`', '`+env.name+`', '', `+at+`, `+string(rune('0'+i))+`)`)
	}
	rule := func(id, principal, capability, projectMode, envMode, keyMode string) []string {
		return []string{
			`INSERT INTO principals (id, kind, created_at) VALUES ('` + principal + `', 'human', ` + at + `)`,
			`INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, created_by, created_at) VALUES ('` +
				id + `', '` + principal + `', '` + capability + `', 'org_en', '` + projectMode + `', '` + envMode + `', '` + keyMode + `', '` + principal + `', ` + at + `)`,
		}
	}
	item := func(id, rule, axis, project, env, folder string) string {
		v := func(s string) string {
			if s == "" {
				return "NULL"
			}
			return "'" + s + "'"
		}
		return `INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path) VALUES ('` +
			id + `', '` + rule + `', 'org_en', '` + axis + `', ` + v(project) + `, ` + v(env) + `, NULL, ` + v(folder) + `)`
	}
	projects := func(rule string) []string {
		return []string{item(rule+"_pa", rule, "project", "prj_a", "", ""), item(rule+"_pb", rule, "project", "prj_b", "", "")}
	}
	// Consistent: prod in both projects is named in both.
	stmts = append(stmts, rule("rul_cons", "usr_cons", "read", "only", "only", "all")...)
	stmts = append(stmts, projects("rul_cons")...)
	stmts = append(stmts, item("rul_cons_e1", "rul_cons", "env", "prj_a", "env_a_prod", ""), item("rul_cons_e2", "rul_cons", "env", "prj_b", "env_b_prod", ""))
	// Split, only prod: prod is named in a but not b, so by name it would gain b's prod.
	stmts = append(stmts, rule("rul_only", "usr_only", "read", "only", "only", "all")...)
	stmts = append(stmts, projects("rul_only")...)
	stmts = append(stmts, item("rul_only_e1", "rul_only", "env", "prj_a", "env_a_prod", ""))
	// Split, all except prod: by name it would lose b's prod.
	stmts = append(stmts, rule("rul_exc", "usr_exc", "edit", "only", "all", "all")...)
	stmts = append(stmts, projects("rul_exc")...)
	stmts = append(stmts, item("rul_exc_e1", "rul_exc", "env", "prj_a", "env_a_prod", ""))
	// Split with a folder in a only: Edit keeps no rule in b (it reached no key
	// there), See keeps b whole (See ignores keys, ADR D5).
	for _, c := range []struct{ id, principal, capability string }{{"rul_fe", "usr_fe", "edit"}, {"rul_fr", "usr_fr", "read"}} {
		stmts = append(stmts, rule(c.id, c.principal, c.capability, "only", "all", "only")...)
		stmts = append(stmts, projects(c.id)...)
		stmts = append(stmts, item(c.id+"_e1", c.id, "env", "prj_a", "env_a_dev", ""), item(c.id+"_f1", c.id, "folder", "prj_a", "", "svc/"))
	}
	// Fail closed: an all-projects rule holding an environment item, and an
	// environment item of a project the rule does not name.
	stmts = append(stmts, rule("rul_allp", "usr_allp", "read", "all", "only", "all")...)
	stmts = append(stmts, item("rul_allp_e1", "rul_allp", "env", "prj_a", "env_a_prod", ""))
	stmts = append(stmts, rule("rul_out", "usr_out", "read", "only", "only", "all")...)
	stmts = append(stmts, item("rul_out_pa", "rul_out", "project", "prj_a", "", ""), item("rul_out_e1", "rul_out", "env", "prj_b", "env_b_prod", ""))
	// Fail closed: an item naming an environment that is gone (seeded below,
	// past the foreign key), and one environment listed twice (the same name
	// twice in one project).
	stmts = append(stmts, rule("rul_gone", "usr_gone", "read", "only", "only", "all")...)
	stmts = append(stmts, item("rul_gone_pa", "rul_gone", "project", "prj_a", "", ""))
	stmts = append(stmts, rule("rul_twice", "usr_twice", "read", "only", "only", "all")...)
	stmts = append(stmts, item("rul_twice_pa", "rul_twice", "project", "prj_a", "", ""),
		item("rul_twice_e1", "rul_twice", "env", "prj_a", "env_a_prod", ""), item("rul_twice_e2", "rul_twice", "env", "prj_a", "env_a_prod", ""))
	// Consistent but too large once names match in every project: 125 names
	// unique to each of c and d list 2 + 250 items by id, yet 2 * (1 + 250)
	// by name, past MaxRuleItems. It splits so every part can be saved again.
	stmts = append(stmts, `INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_c', 'org_en', 'c', `+at+`)`,
		`INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_d', 'org_en', 'd', `+at+`)`)
	stmts = append(stmts, rule("rul_big", "usr_big", "read", "only", "only", "all")...)
	stmts = append(stmts, item("rul_big_pc", "rul_big", "project", "prj_c", "", ""), item("rul_big_pd", "rul_big", "project", "prj_d", "", ""))
	var bigEnvs []domain.EnvRef
	for i := range 250 {
		project := []string{"prj_c", "prj_d"}[i%2]
		id, name := fmt.Sprintf("env_big_%03d", i), fmt.Sprintf("big%03d", i)
		bigEnvs = append(bigEnvs, domain.EnvRef{Project: domain.ProjectID(project), ID: domain.EnvID(id), Name: name})
		stmts = append(stmts, `INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('`+
			id+`', 'org_en', '`+project+`', '`+name+`', '', `+at+`, `+fmt.Sprint(i)+`)`,
			item(fmt.Sprintf("rul_big_e%03d", i), "rul_big", "env", project, id, ""))
	}
	for i, stmt := range stmts {
		if err := db.exec(stmt); err != nil {
			t.Fatalf("seed %d %s: %v", i, stmt, err)
		}
	}
	if err := db.unchecked(item("rul_gone_e1", "rul_gone", "env", "prj_a", "env_a_gone", "")); err != nil {
		t.Fatalf("seed an item naming a gone environment: %v", err)
	}
	if n := db.count(`SELECT COUNT(*) FROM rule_items WHERE id = 'rul_gone_e1'`); n != 1 {
		t.Fatalf("the gone-environment item was not seeded: %d", n)
	}
	// The 99/102 recovery boundary: bound to schema 99, recovery cannot read
	// id-bound environment items as names, so those rules fail closed while
	// a rule without environment items still reads.
	stmts = rule("rul_plain", "usr_plain", "read", "only", "all", "all")
	stmts = append(stmts, item("rul_plain_pa", "rul_plain", "project", "prj_a", "", ""))
	for i, stmt := range stmts {
		if err := db.exec(stmt); err != nil {
			t.Fatalf("seed plain %d: %v", i, err)
		}
	}
	at99 := db.historical(99)
	if got, err := at99.Rules(ctx, "usr_plain"); err != nil || len(got) != 1 {
		t.Fatalf("schema 99 recovery of a rule without environment items = %+v, %v", got, err)
	}
	for _, p := range []domain.PrincipalID{"usr_cons", "usr_only", "usr_exc"} {
		if got, err := at99.Rules(ctx, p); err != nil || len(got) != 0 {
			t.Fatalf("schema 99 recovery read id-bound environment rules of %s as %+v, %v; want none (fail closed)", p, got, err)
		}
	}
	if err := migrate.RunUpTo(ctx, db.cfg, 102); err != nil {
		t.Fatal(err)
	}

	envs := []domain.EnvRef{
		{Project: "prj_a", ID: "env_a_prod", Name: "prod"}, {Project: "prj_a", ID: "env_a_dev", Name: "dev"},
		{Project: "prj_b", ID: "env_b_prod", Name: "prod"}, {Project: "prj_b", ID: "env_b_dev", Name: "dev"},
	}
	// reached is every environment a principal's rules reach for capability.
	reached := func(principal domain.PrincipalID, capability domain.Capability, key *domain.RuleKey) []domain.EnvID {
		rules, err := db.rules.Rules(ctx, principal)
		if err != nil {
			t.Fatalf("rules of %s: %v", principal, err)
		}
		var out []domain.EnvID
		for _, e := range envs {
			scope := domain.Scope{Org: "org_en", Project: e.Project, Env: e.ID}
			if slices.ContainsFunc(rules, func(r domain.Rule) bool { return r.Reaches(capability, domain.LevelEnv, scope, key) }) {
				out = append(out, e.ID)
			}
		}
		return out
	}
	// reachedProjects is every project a principal's rules reach whole: every
	// environment of it, including later ones.
	reachedProjects := func(principal domain.PrincipalID, capability domain.Capability, key *domain.RuleKey) []domain.ProjectID {
		rules, err := db.rules.Rules(ctx, principal)
		if err != nil {
			t.Fatalf("rules of %s: %v", principal, err)
		}
		var out []domain.ProjectID
		for _, project := range []domain.ProjectID{"prj_a", "prj_b"} {
			scope := domain.Scope{Org: "org_en", Project: project}
			if slices.ContainsFunc(rules, func(r domain.Rule) bool { return r.Reaches(capability, domain.LevelProject, scope, key) }) {
				out = append(out, project)
			}
		}
		return out
	}
	for _, c := range []struct {
		principal  domain.PrincipalID
		capability domain.Capability
		key        *domain.RuleKey
		want       []domain.EnvID
		projects   []domain.ProjectID
	}{
		{"usr_cons", domain.CapRead, nil, []domain.EnvID{"env_a_prod", "env_b_prod"}, nil},
		{"usr_only", domain.CapRead, nil, []domain.EnvID{"env_a_prod"}, nil},
		// Except prod in a only: b, which excepted nothing, stays whole.
		{"usr_exc", domain.CapEdit, nil, []domain.EnvID{"env_a_dev", "env_b_prod", "env_b_dev"}, []domain.ProjectID{"prj_b"}},
		{"usr_fr", domain.CapRead, nil, []domain.EnvID{"env_a_prod", "env_b_prod", "env_b_dev"}, []domain.ProjectID{"prj_b"}},
		{"usr_allp", domain.CapRead, nil, nil, nil},
		{"usr_out", domain.CapRead, nil, nil, nil},
		// The Edit folder rule reaches a key in svc/ where it did before: a's
		// environments but dev, and nothing in b.
		{"usr_fe", domain.CapEdit, &domain.RuleKey{ID: "key_svc", Folder: "svc/"}, []domain.EnvID{"env_a_prod"}, nil},
	} {
		if got := reached(c.principal, c.capability, c.key); !slices.Equal(got, c.want) {
			t.Errorf("%s reaches %v after 00102, want %v as before", c.principal, got, c.want)
		}
		if got := reachedProjects(c.principal, c.capability, c.key); !slices.Equal(got, c.projects) {
			t.Errorf("%s reaches projects %v whole after 00102, want %v as before", c.principal, got, c.projects)
		}
	}

	at102 := db.historical(102)
	for _, p := range []domain.PrincipalID{"usr_plain", "usr_cons", "usr_only", "usr_exc", "usr_fr"} {
		want, err := db.rules.Rules(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		got, err := at102.Rules(ctx, p)
		if err != nil || len(want) == 0 || !reflectx.Equal(got, want) {
			t.Errorf("schema 102 recovery of %s = %+v, %v; want the current resolver's %+v", p, got, err, want)
		}
	}

	bigRules, err := db.rules.Rules(ctx, "usr_big")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range bigRules {
		if n := r.Where.ListedItems(); n > domain.MaxRuleItems {
			t.Errorf("rul_big part %s lists %d items, above %d", r.ID, n, domain.MaxRuleItems)
		}
	}
	for _, e := range bigEnvs {
		scope := domain.Scope{Org: "org_en", Project: e.Project, Env: e.ID}
		if !slices.ContainsFunc(bigRules, func(r domain.Rule) bool { return r.Reaches(domain.CapRead, domain.LevelEnv, scope, nil) }) {
			t.Errorf("usr_big lost %s after 00102", e.ID)
		}
	}

	// Consistent: converted in place to one rule-wide name item.
	if n := db.count(`SELECT COUNT(*) FROM rule_items WHERE rule_id = 'rul_cons' AND axis = 'env' AND project_id IS NULL AND env_name = 'prod'`); n != 1 {
		t.Errorf("rul_cons has %d rule-wide prod items, want 1", n)
	}
	// Splits: originals gone, one rule per project that reached something.
	splits := map[string]struct {
		principal string
		projects  int
	}{"rul_only": {"usr_only", 1}, "rul_exc": {"usr_exc", 2}, "rul_fe": {"usr_fe", 1}, "rul_fr": {"usr_fr", 2}, "rul_big": {"usr_big", 2}}
	for old, want := range splits {
		if n := db.count(`SELECT COUNT(*) FROM rules WHERE id = '` + old + `'`); n != 0 {
			t.Errorf("split rule %s survived", old)
		}
		if n := db.count(`SELECT COUNT(*) FROM rules WHERE principal_id = '` + want.principal + `'`); n != want.projects {
			t.Errorf("%s split into %d rules, want %d", old, n, want.projects)
		}
		payload := db.text(`SELECT payload FROM audit_tenant_events WHERE type = 'rule.split' AND object_id = '` + old + `'`)
		var split struct {
			RuleID  string   `json:"rule_id"`
			RuleIDs []string `json:"rule_ids"`
		}
		if err := json.Unmarshal([]byte(payload), &split); err != nil {
			t.Fatalf("rule.split payload %q: %v", payload, err)
		}
		if split.RuleID != old || len(split.RuleIDs) != want.projects {
			t.Errorf("rule.split for %s = %+v, want %d new ids", old, split, want.projects)
		}
		for _, id := range split.RuleIDs {
			if n := db.count(`SELECT COUNT(*) FROM rules WHERE id = '` + id + `' AND principal_id = '` + want.principal + `'`); n != 1 {
				t.Errorf("rule.split for %s names %s, which is not a rule of %s", old, id, want.principal)
			}
		}
	}
	// The Edit folder rule keeps its folder in a only; the See twin keeps b with all keys.
	if n := db.count(`SELECT COUNT(*) FROM rules AS r JOIN rule_items AS i ON i.rule_id = r.id WHERE r.principal_id = 'usr_fe' AND r.key_mode = 'only' AND i.axis = 'folder' AND i.project_id = 'prj_a' AND i.folder_path = 'svc/'`); n != 1 {
		t.Errorf("usr_fe split lost its folder in a: %d", n)
	}
	if n := db.count(`SELECT COUNT(*) FROM rules AS r JOIN rule_items AS i ON i.rule_id = r.id AND i.axis = 'project' WHERE r.principal_id = 'usr_fr' AND i.project_id = 'prj_b' AND r.key_mode = 'all'`); n != 1 {
		t.Errorf("usr_fr split has no all-keys rule in b: %d", n)
	}
	// Fail closed: kept, no environment, a manager-facing fault, audited.
	// The audit keeps the removed selection, so a manager can rebuild it.
	removed := map[string]string{"rul_allp": "prj_a/env_a_prod", "rul_out": "prj_b/env_b_prod"}
	for _, id := range []string{"rul_allp", "rul_out"} {
		var disabled struct {
			EnvMode  string   `json:"env_mode"`
			EnvIDs   []string `json:"env_ids"`
			EnvNames []string `json:"env_names"`
		}
		if err := json.Unmarshal([]byte(db.text(`SELECT payload FROM audit_tenant_events WHERE type = 'rule.disabled' AND object_id = '`+id+`'`)), &disabled); err != nil {
			t.Fatalf("%s rule.disabled payload: %v", id, err)
		}
		if disabled.EnvMode != "only" || !slices.Equal(disabled.EnvIDs, []string{removed[id]}) || !slices.Equal(disabled.EnvNames, []string{"prod"}) {
			t.Errorf("%s rule.disabled records %+v, want only %s named prod", id, disabled, removed[id])
		}
		if n := db.count(`SELECT COUNT(*) FROM rules WHERE id = '` + id + `' AND env_mode = 'only' AND env_fault LIKE 'Disabled by the upgrade to environment names: %'`); n != 1 {
			t.Errorf("%s did not fail closed with a fault", id)
		}
		if n := db.count(`SELECT COUNT(*) FROM rule_items WHERE rule_id = '` + id + `' AND axis = 'env'`); n != 0 {
			t.Errorf("%s kept %d environment items", id, n)
		}
		if n := db.count(`SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.disabled' AND object_id = '` + id + `'`); n != 1 {
			t.Errorf("%s has %d rule.disabled events, want 1", id, n)
		}
	}
	for id, fault := range map[string]string{
		"rul_gone":  "Disabled by the upgrade to environment names: it names an environment that no longer exists, so its reach cannot be kept exactly. It reaches no environment; revoke it and create it again.",
		"rul_twice": "Disabled by the upgrade to environment names: it names one environment name twice in a project. It reaches no environment; revoke it and create it again.",
	} {
		if got := db.text(`SELECT env_fault FROM rules WHERE id = '` + id + `' AND env_mode = 'only'`); got != fault {
			t.Errorf("%s fault = %q, want %q", id, got, fault)
		}
		if n := db.count(`SELECT COUNT(*) FROM rule_items WHERE rule_id = '` + id + `' AND axis = 'env'`); n != 0 {
			t.Errorf("%s kept %d environment items", id, n)
		}
		if n := db.count(`SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.disabled' AND object_id = '` + id + `'`); n != 1 {
			t.Errorf("%s has %d rule.disabled events, want 1", id, n)
		}
	}
	for _, p := range []domain.PrincipalID{"usr_gone", "usr_twice"} {
		if got := reached(p, domain.CapRead, nil); len(got) != 0 {
			t.Errorf("disabled rule of %s reaches %v", p, got)
		}
	}
	if n := db.count(`SELECT COUNT(*) FROM audit_tenant_events WHERE type IN ('rule.split', 'rule.disabled')`); n != 9 {
		t.Errorf("%d migration audit events, want 9", n)
	}
	// Each migration event reads back as the store reads it and passes the
	// closed registry, like any event the application writes.
	for _, row := range db.events(`WHERE type IN ('rule.split', 'rule.disabled')`) {
		var e audit.Event
		var err error
		e.ID, e.Type, e.SchemaVersion, e.Outcome, e.Origin = row.id, audit.EventType(row.typ), row.schemaVersion, audit.Outcome(row.outcome), audit.Origin(row.origin)
		e.Actor.Class, e.Object = audit.ActorClass(row.actorClass), audit.Object{Type: row.objectType, ID: row.objectID}
		if e.OccurredAt, err = audit.ParseTime(row.occurred); err != nil {
			t.Fatal(err)
		}
		if _, err := audit.ParseTime(row.recorded); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(row.payload), &e.Payload); err != nil {
			t.Fatal(err)
		}
		// JSON decodes a list as []any; the registry types it []string.
		for _, field := range []string{"rule_ids", "env_ids", "env_names"} {
			ids, ok := e.Payload[field].([]any)
			if !ok {
				continue
			}
			list := make([]string, 0, len(ids))
			for _, id := range ids {
				s, ok := id.(string)
				if !ok {
					t.Fatalf("%s holds %T", field, id)
				}
				list = append(list, s)
			}
			e.Payload[field] = list
		}
		if err := audit.Validate(e, audit.TrailTenant, domain.Scope{Org: "org_en"}); err != nil {
			t.Errorf("migration event %s %s: %v", row.typ, row.objectID, err)
		}
	}
	// Nothing can match environments by id after 00102.
	if err := db.exec(`SELECT env_id FROM rule_items`); err == nil {
		t.Error("rule_items.env_id survived 00102")
	}
}

// A rule selecting different names in its projects (only a/dev and b/prod,
// neither name in the other project) splits rather than converting (#1447):
// converted, each project would carry a name no environment there has, which
// a manager holding only the rule's environments does not cover. The manager
// who could revoke it before the upgrade still can, part by part. The
// migrated rows are copied into a live store of the same engine, since the
// development gate admits only a freshly created database.
func TestRuleEnvNamesMigrationKeepsDelegatedRevokeSQLite(t *testing.T) {
	cfg, db := scratchSQLite(t, "env-names-revoke.db")
	ctx := t.Context()
	testRuleEnvNamesMigrationKeepsDelegatedRevoke(t, cfg, openSQLite,
		func(s string) error { _, err := db.ExecContext(ctx, s); return err },
		func(s string) []string {
			rows, err := db.QueryContext(ctx, s)
			if err != nil {
				t.Fatalf("%s: %v", s, err)
			}
			defer func() { _ = rows.Close() }()
			var out []string
			for rows.Next() {
				var v string
				if err := rows.Scan(&v); err != nil {
					t.Fatal(err)
				}
				out = append(out, v)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return out
		})
}

func TestRuleEnvNamesMigrationKeepsDelegatedRevokePostgres(t *testing.T) {
	cfg, conn := scratchPostgres(t, "env_names_revoke")
	ctx := t.Context()
	testRuleEnvNamesMigrationKeepsDelegatedRevoke(t, cfg, openPostgres,
		func(s string) error { _, err := conn.Exec(ctx, s); return err },
		func(s string) []string {
			rows, err := conn.Query(ctx, s)
			if err != nil {
				t.Fatalf("%s: %v", s, err)
			}
			defer rows.Close()
			var out []string
			for rows.Next() {
				var v string
				if err := rows.Scan(&v); err != nil {
					t.Fatal(err)
				}
				out = append(out, v)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return out
		})
}

func testRuleEnvNamesMigrationKeepsDelegatedRevoke(t *testing.T, cfg store.Config, open func(*testing.T) *store.DB, exec func(string) error, lines func(string) []string) {
	ctx := t.Context()
	if err := migrate.RunUpTo(ctx, cfg, 99); err != nil {
		t.Fatal(err)
	}
	const at = `'2026-01-01T00:00:00Z'`
	// The topology and the manager's environment grants, the same before the
	// upgrade and in the live store.
	base := []string{
		`INSERT INTO orgs (id, name, active, metadata, created_at) VALUES ('org_rv', 'rv', TRUE, '{}', ` + at + `)`,
		`INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_rva', 'org_rv', 'rva', ` + at + `)`,
		`INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_rvb', 'org_rv', 'rvb', ` + at + `)`,
		`INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('env_rva_dev', 'org_rv', 'prj_rva', 'dev', '', ` + at + `, 0)`,
		`INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('env_rvb_prod', 'org_rv', 'prj_rvb', 'prod', '', ` + at + `, 0)`,
		`INSERT INTO principals (id, kind, created_at) VALUES ('usr_rvtarget', 'human', ` + at + `)`,
		`INSERT INTO principals (id, kind, created_at) VALUES ('usr_rvmgr', 'human', ` + at + `)`,
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_rvmgr_a', 'usr_rvmgr', 'manage-members', 'org_rv', 'prj_rva', 'env_rva_dev', ` + at + `)`,
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_rvmgr_b', 'usr_rvmgr', 'manage-members', 'org_rv', 'prj_rvb', 'env_rvb_prod', ` + at + `)`,
	}
	legacy := []string{
		`INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, created_by, created_at) VALUES ('rul_rvdiff', 'usr_rvtarget', 'read', 'org_rv', 'only', 'only', 'all', 'usr_rvmgr', ` + at + `)`,
		`INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path) VALUES ('rli_rv_pa', 'rul_rvdiff', 'org_rv', 'project', 'prj_rva', NULL, NULL, NULL)`,
		`INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path) VALUES ('rli_rv_pb', 'rul_rvdiff', 'org_rv', 'project', 'prj_rvb', NULL, NULL, NULL)`,
		`INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path) VALUES ('rli_rv_ea', 'rul_rvdiff', 'org_rv', 'env', 'prj_rva', 'env_rva_dev', NULL, NULL)`,
		`INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path) VALUES ('rli_rv_eb', 'rul_rvdiff', 'org_rv', 'env', 'prj_rvb', 'env_rvb_prod', NULL, NULL)`,
	}
	for i, stmt := range append(slices.Clone(base), legacy...) {
		if err := exec(stmt); err != nil {
			t.Fatalf("seed %d %s: %v", i, stmt, err)
		}
	}
	if err := migrate.RunUpTo(ctx, cfg, 102); err != nil {
		t.Fatal(err)
	}
	lit := func(col string) string {
		return `CASE WHEN ` + col + ` IS NULL THEN 'NULL' ELSE '''' || ` + col + ` || '''' END`
	}
	migrated := lines(`SELECT 'INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, created_by, created_at) VALUES (' || ` +
		lit("id") + ` || ', ''usr_rvtarget'', ' || ` + lit("capability") + ` || ', ''org_rv'', ' || ` + lit("project_mode") + ` || ', ' || ` +
		lit("env_mode") + ` || ', ' || ` + lit("key_mode") + ` || ', ''usr_rvmgr'', ` + strings.ReplaceAll(at, "'", "''") + `)' FROM rules WHERE principal_id = 'usr_rvtarget' ORDER BY id`)
	migrated = append(migrated, lines(`SELECT 'INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_name, key_id, folder_path) VALUES (' || `+
		lit("i.id")+` || ', ' || `+lit("i.rule_id")+` || ', ''org_rv'', ' || `+lit("i.axis")+` || ', ' || `+lit("i.project_id")+` || ', ' || `+
		lit("i.env_name")+` || ', ' || `+lit("i.key_id")+` || ', ' || `+lit("i.folder_path")+` || ')' FROM rule_items AS i JOIN rules AS r ON r.id = i.rule_id
		WHERE r.principal_id = 'usr_rvtarget' ORDER BY i.id`)...)

	if got := lines(`SELECT CAST(COUNT(*) AS TEXT) FROM audit_tenant_events WHERE type = 'rule.split' AND object_id = 'rul_rvdiff'`); !slices.Equal(got, []string{"1"}) {
		t.Fatalf("rule.split events for rul_rvdiff = %v, want 1", got)
	}

	db := open(t)
	for _, stmt := range append(base, migrated...) {
		execRaw(t, db, stmt)
	}
	svc := &service.Rules{DB: db}
	var ids []string
	for _, stmt := range migrated {
		if id, ok := strings.CutPrefix(stmt, "INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, created_by, created_at) VALUES ('"); ok {
			ids = append(ids, id[:strings.Index(id, "'")])
		}
	}
	for _, id := range ids {
		if err := svc.Revoke(ctx, service.LocalPrincipal("usr_rvmgr"), "org_rv", id); err != nil {
			t.Fatalf("revoke migrated rule %s by the manager of its environments: %v (migrated rows %q)", id, err, migrated)
		}
	}
	if len(ids) != 2 {
		t.Fatalf("migrated rules %v, want one per project", ids)
	}
}
