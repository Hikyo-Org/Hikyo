package isolation

import (
	"encoding/json/v2"
	"fmt"
	"regexp"
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

// TestRuleEnvNamesMigrationShapeProjectList pins the Go constants 00102
// hard-codes on both engines: the whole-project capabilities
// (domain.ShapeProject), the capabilities other than See that keys may not
// narrow (domain.ShapeEnv), and domain.MaxRuleItems. A capability gaining a
// shape, or a changed cap, cannot then be split into a live rule.
func TestRuleEnvNamesMigrationShapeProjectList(t *testing.T) {
	list := func(want domain.RuleShape) string {
		var quoted []string
		for _, c := range domain.RuleCapabilities() {
			if shape, _ := domain.RuleShapeOf(c); shape == want && c != domain.CapRead {
				quoted = append(quoted, "'"+string(c)+"'")
			}
		}
		return "r.capability IN (" + strings.Join(quoted, ", ") + ")"
	}
	whole, envOnly := list(domain.ShapeProject), list(domain.ShapeEnv)
	capLists := regexp.MustCompile(`r\.capability IN \([^)]*\)`)
	itemCap := regexp.MustCompile(`FROM rule_items AS c WHERE c\.rule_id = r\.id\) > (\d+)`)
	for _, engine := range []string{"postgres", "sqlite"} {
		raw, err := store.MigrationsFS.ReadFile("migrations/" + engine + "/00102_rule_env_names.sql")
		if err != nil {
			t.Fatal(err)
		}
		sql := string(raw)
		got := capLists.FindAllString(sql, -1)
		if !slices.Contains(got, whole) || !slices.Contains(got, envOnly) {
			t.Errorf("%s 00102 capability lists %v, want %s and %s", engine, got, whole, envOnly)
		}
		for _, l := range got {
			if l != whole && l != envOnly {
				t.Errorf("%s 00102 lists %s, neither %s nor %s", engine, l, whole, envOnly)
			}
		}
		if m := itemCap.FindAllStringSubmatch(sql, -1); len(m) != 1 || m[0][1] != fmt.Sprint(domain.MaxRuleItems) {
			t.Errorf("%s 00102 item caps %v, want one of %d (domain.MaxRuleItems)", engine, m, domain.MaxRuleItems)
		}
	}
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
	// Fail closed: project settings needs whole projects (ShapeProject), so a
	// stored one naming environments never validated and reached nothing.
	// rul_ps excepts a/prod only, which the split would turn into a valid,
	// live whole-project rule over b; rul_psc excepts prod in both, which the
	// conversion would keep narrowed.
	stmts = append(stmts, rule("rul_ps", "usr_ps", "project-settings", "only", "all", "all")...)
	stmts = append(stmts, projects("rul_ps")...)
	stmts = append(stmts, item("rul_ps_e1", "rul_ps", "env", "prj_a", "env_a_prod", ""))
	stmts = append(stmts, rule("rul_psc", "usr_psc", "project-settings", "only", "all", "all")...)
	stmts = append(stmts, projects("rul_psc")...)
	stmts = append(stmts, item("rul_psc_e1", "rul_psc", "env", "prj_a", "env_a_prod", ""), item("rul_psc_e2", "rul_psc", "env", "prj_b", "env_b_prod", ""))
	// Fail closed: an item naming an environment that is gone (seeded below,
	// past the foreign key), and one environment listed twice (the same name
	// twice in one project).
	stmts = append(stmts, rule("rul_gone", "usr_gone", "read", "only", "only", "all")...)
	stmts = append(stmts, item("rul_gone_pa", "rul_gone", "project", "prj_a", "", ""))
	stmts = append(stmts, rule("rul_twice", "usr_twice", "read", "only", "only", "all")...)
	stmts = append(stmts, item("rul_twice_pa", "rul_twice", "project", "prj_a", "", ""),
		item("rul_twice_e1", "rul_twice", "env", "prj_a", "env_a_prod", ""), item("rul_twice_e2", "rul_twice", "env", "prj_a", "env_a_prod", ""))
	// Fail closed: a rule written past MaxRuleItems (raw SQL, as a row
	// bypassing Validate) never validated and reached nothing. c and d each
	// select the same 250 names, 2 + 500 items by id; converted, 2 + 250
	// names would validate and go live.
	stmts = append(stmts, `INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_c', 'org_en', 'c', `+at+`)`,
		`INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_d', 'org_en', 'd', `+at+`)`)
	stmts = append(stmts, rule("rul_big", "usr_big", "read", "only", "only", "all")...)
	stmts = append(stmts, item("rul_big_pc", "rul_big", "project", "prj_c", "", ""), item("rul_big_pd", "rul_big", "project", "prj_d", "", ""))
	var bigEnvs []domain.EnvRef
	for i := range 250 {
		for _, project := range []string{"prj_c", "prj_d"} {
			id, name := fmt.Sprintf("env_big_%s_%03d", project[len(project)-1:], i), fmt.Sprintf("big%03d", i)
			bigEnvs = append(bigEnvs, domain.EnvRef{Project: domain.ProjectID(project), ID: domain.EnvID(id), Name: name})
			stmts = append(stmts, `INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('`+
				id+`', 'org_en', '`+project+`', '`+name+`', '', `+at+`, `+fmt.Sprint(i)+`)`,
				item("rul_big_"+id, "rul_big", "env", project, id, ""))
		}
	}
	// Fail closed: rules that did not validate before 00102 for a reason a
	// split lifts. Each names a/prod only (except or only), so it splits, and
	// a part without the fault would validate and go live:
	//   rul_dupp names a twice (two live a parts);
	//   rul_kout and rul_knull hold a folder outside their projects (in c,
	//   and in none, which no project item matches either);
	//   rul_konly is only-keys with no key item (See parts keep every key);
	//   rul_pin is Pin narrowed by a folder in a (b's part holds every key);
	//   rul_mch is a workload's rule over two projects (a's part names one);
	//   rul_dupf and rul_dupk name one folder, and one key, twice in a;
	//   rul_kemp, rul_pemp and rul_eemp hold an item naming an empty key,
	//   project and environment id (b's part holds every key or every
	//   environment of b).
	// rul_psk is project settings narrowed by a folder, with no environment
	// item: nothing splits it, and it is faulted all the same.
	stmts = append(stmts, rule("rul_dupp", "usr_dupp", "read", "only", "only", "all")...)
	stmts = append(stmts, projects("rul_dupp")...)
	stmts = append(stmts, item("rul_dupp_pa2", "rul_dupp", "project", "prj_a", "", ""), item("rul_dupp_e1", "rul_dupp", "env", "prj_a", "env_a_prod", ""))
	for _, c := range []struct{ id, keyMode, folderProject string }{{"rul_kout", "only", "prj_c"}, {"rul_knull", "only", ""}, {"rul_konly", "only", "-"}} {
		stmts = append(stmts, rule(c.id, "usr_"+strings.TrimPrefix(c.id, "rul_"), "read", "only", "all", c.keyMode)...)
		stmts = append(stmts, projects(c.id)...)
		stmts = append(stmts, item(c.id+"_e1", c.id, "env", "prj_a", "env_a_prod", ""))
		if c.folderProject != "-" {
			stmts = append(stmts, item(c.id+"_f1", c.id, "folder", c.folderProject, "", "svc/"))
		}
	}
	stmts = append(stmts, rule("rul_pin", "usr_pin", "pin", "only", "all", "all")...)
	stmts = append(stmts, projects("rul_pin")...)
	stmts = append(stmts, item("rul_pin_e1", "rul_pin", "env", "prj_a", "env_a_prod", ""), item("rul_pin_f1", "rul_pin", "folder", "prj_a", "", "svc/"))
	stmts = append(stmts, rule("rul_psk", "usr_psk", "project-settings", "only", "all", "all")...)
	stmts = append(stmts, projects("rul_psk")...)
	stmts = append(stmts, item("rul_psk_f1", "rul_psk", "folder", "prj_a", "", "svc/"))
	stmts = append(stmts, `INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_split', 'machine', 'workload', `+at+`)`,
		`INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, created_by, created_at) VALUES ('rul_mch', 'mch_split', 'read', 'org_en', 'only', 'only', 'all', 'usr_admin', `+at+`)`)
	stmts = append(stmts, projects("rul_mch")...)
	stmts = append(stmts, item("rul_mch_e1", "rul_mch", "env", "prj_a", "env_a_prod", ""))
	for _, id := range []string{"rul_dupf", "rul_dupk", "rul_kemp"} {
		stmts = append(stmts, rule(id, "usr_"+strings.TrimPrefix(id, "rul_"), "read", "only", "all", "all")...)
		stmts = append(stmts, projects(id)...)
		stmts = append(stmts, item(id+"_e1", id, "env", "prj_a", "env_a_prod", ""))
	}
	stmts = append(stmts, item("rul_dupf_f1", "rul_dupf", "folder", "prj_a", "", "svc/"), item("rul_dupf_f2", "rul_dupf", "folder", "prj_a", "", "svc/"))
	// A key named twice by id, a key and a project with an empty id: rows
	// only a corrupted database holds, so the referenced rows are seeded too.
	const keyCols = `INSERT INTO keys (id, org_id, project_id, name, folder_path, classification, description, deprecated, deprecation_note, declaration, required_mode, forbidden_mode, created_at) VALUES `
	keyItem := func(id, rule, key string) string {
		return `INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path) VALUES ('` + id + `', '` + rule + `', 'org_en', 'key', 'prj_a', NULL, '` + key + `', NULL)`
	}
	stmts = append(stmts, keyCols+`('key_svc', 'org_en', 'prj_a', 'SVC', 'svc/', 'secret', '', FALSE, '', '{"rule":{"type":"string"}}', 'none', 'none', `+at+`)`,
		keyCols+`('', 'org_en', 'prj_a', 'EMPTY', '', 'secret', '', FALSE, '', '{"rule":{"type":"string"}}', 'none', 'none', `+at+`)`,
		keyItem("rul_dupk_k1", "rul_dupk", "key_svc"), keyItem("rul_dupk_k2", "rul_dupk", "key_svc"), keyItem("rul_kemp_k1", "rul_kemp", ""),
		`INSERT INTO projects (id, org_id, name, created_at) VALUES ('', 'org_en', 'empty', `+at+`)`)
	stmts = append(stmts, rule("rul_pemp", "usr_pemp", "read", "only", "all", "all")...)
	stmts = append(stmts, item("rul_pemp_pb", "rul_pemp", "project", "prj_b", "", ""),
		`INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path) VALUES ('rul_pemp_pe', 'rul_pemp', 'org_en', 'project', '', NULL, NULL, NULL)`, item("rul_pemp_e1", "rul_pemp", "env", "prj_b", "env_b_prod", ""))
	stmts = append(stmts, `INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_f', 'org_en', 'f', `+at+`)`,
		`INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('', 'org_en', 'prj_f', 'blank', '', `+at+`, 0)`)
	stmts = append(stmts, rule("rul_eemp", "usr_eemp", "read", "only", "all", "all")...)
	stmts = append(stmts, item("rul_eemp_pb", "rul_eemp", "project", "prj_b", "", ""), item("rul_eemp_pf", "rul_eemp", "project", "prj_f", "", ""),
		`INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path) VALUES ('rul_eemp_e1', 'rul_eemp', 'org_en', 'env', 'prj_f', '', NULL, NULL)`)
	// The item cap, counted as base Validate counts (projects, environment
	// items by id, keys): rul_b500 holds 2 + 2 + 496 and stays live, rul_b501
	// one folder more. Converted, rul_b501 would name prod once (500) and go
	// live.
	for id, folders := range map[string]int{"rul_b500": 496, "rul_b501": 497} {
		stmts = append(stmts, rule(id, "usr_"+strings.TrimPrefix(id, "rul_"), "read", "only", "only", "all")...)
		stmts = append(stmts, projects(id)...)
		stmts = append(stmts, item(id+"_e1", id, "env", "prj_a", "env_a_prod", ""), item(id+"_e2", id, "env", "prj_b", "env_b_prod", ""))
		for i := range folders {
			stmts = append(stmts, item(fmt.Sprintf("%s_f%03d", id, i), id, "folder", "prj_a", "", fmt.Sprintf("f%03d/", i)))
		}
	}
	// rul_wide holds 1001 environment items: its rule.disabled lists keep the
	// first 1000 and env_items counts all.
	stmts = append(stmts, `INSERT INTO projects (id, org_id, name, created_at) VALUES ('prj_e', 'org_en', 'e', `+at+`)`)
	stmts = append(stmts, rule("rul_wide", "usr_wide", "read", "only", "only", "all")...)
	stmts = append(stmts, item("rul_wide_pe", "rul_wide", "project", "prj_e", "", ""))
	var wideEnvs, wideItems []string
	for i := range 1001 {
		wideEnvs = append(wideEnvs, fmt.Sprintf(`('env_e_%04d', 'org_en', 'prj_e', 'e%04d', '', %s, %d)`, i, i, at, i))
		wideItems = append(wideItems, fmt.Sprintf(`('rul_wide_%04d', 'rul_wide', 'org_en', 'env', 'prj_e', 'env_e_%04d', NULL, NULL)`, i, i))
	}
	stmts = append(stmts, `INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES `+strings.Join(wideEnvs, ", "),
		`INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path) VALUES `+strings.Join(wideItems, ", "))
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
		{"usr_ps", domain.CapProjectSettings, nil, nil, nil},
		{"usr_psc", domain.CapProjectSettings, nil, nil, nil},
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
	for _, e := range bigEnvs {
		scope := domain.Scope{Org: "org_en", Project: e.Project, Env: e.ID}
		if slices.ContainsFunc(bigRules, func(r domain.Rule) bool { return r.Reaches(domain.CapRead, domain.LevelEnv, scope, nil) }) {
			t.Errorf("usr_big reaches %s after 00102; it reached nothing before", e.ID)
		}
	}
	// The rules that did not validate before 00102: one faulted row each,
	// no split, audited, reaching nothing.
	otherKey := &domain.RuleKey{ID: "key_other", Folder: "other/"}
	const invalid = "Disabled by the upgrade to environment names: it did not form a valid rule before the upgrade, so it reached nothing. It reaches no environment; revoke it and create it again."
	for _, c := range []struct {
		id, principal string
		capability    domain.Capability
	}{
		{"rul_big", "usr_big", domain.CapRead}, {"rul_dupp", "usr_dupp", domain.CapRead}, {"rul_kout", "usr_kout", domain.CapRead},
		{"rul_knull", "usr_knull", domain.CapRead}, {"rul_konly", "usr_konly", domain.CapRead}, {"rul_pin", "usr_pin", domain.CapPin},
		{"rul_psk", "usr_psk", domain.CapProjectSettings}, {"rul_mch", "mch_split", domain.CapRead},
		{"rul_dupf", "usr_dupf", domain.CapRead}, {"rul_dupk", "usr_dupk", domain.CapRead}, {"rul_kemp", "usr_kemp", domain.CapRead},
		{"rul_pemp", "usr_pemp", domain.CapRead}, {"rul_eemp", "usr_eemp", domain.CapRead}, {"rul_b501", "usr_b501", domain.CapRead},
		{"rul_wide", "usr_wide", domain.CapRead},
	} {
		if n := db.count(`SELECT COUNT(*) FROM rules WHERE principal_id = '` + c.principal + `'`); n != 1 {
			t.Errorf("%s holds %d rules after 00102, want its one faulted rule", c.principal, n)
		}
		if n := db.count(`SELECT COUNT(*) FROM rules WHERE id = '` + c.id + `' AND env_mode = 'only' AND env_fault = '` + invalid + `'`); n != 1 {
			t.Errorf("%s is not faulted as invalid before the upgrade", c.id)
		}
		if n := db.count(`SELECT COUNT(*) FROM rule_items WHERE rule_id = '` + c.id + `' AND axis = 'env'`); n != 0 {
			t.Errorf("%s kept %d environment items", c.id, n)
		}
		if n := db.count(`SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.disabled' AND object_id = '` + c.id + `'`); n != 1 {
			t.Errorf("%s has %d rule.disabled events, want 1", c.id, n)
		}
		if n := db.count(`SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.split' AND object_id = '` + c.id + `'`); n != 0 {
			t.Errorf("%s was split", c.id)
		}
		rules, err := db.rules.Rules(ctx, domain.PrincipalID(c.principal))
		if err != nil {
			t.Fatal(err)
		}
		machine, err := db.rules.MachineRules(ctx, domain.PrincipalID(c.principal))
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range machine {
			if domain.CheckMachineRule(domain.ClassWorkload, r) == domain.MachineRuleOK {
				rules = append(rules, r)
			}
		}
		for _, e := range envs {
			scope := domain.Scope{Org: "org_en", Project: e.Project, Env: e.ID}
			for _, key := range []*domain.RuleKey{nil, otherKey} {
				if slices.ContainsFunc(rules, func(r domain.Rule) bool { return r.Reaches(c.capability, domain.LevelEnv, scope, key) }) {
					t.Errorf("%s reaches %s after 00102; it reached nothing before", c.id, e.ID)
				}
			}
		}
	}
	// One item under the cap stays live, converted, reaching what it did.
	if n := db.count(`SELECT COUNT(*) FROM rules WHERE principal_id = 'usr_b500' AND env_fault IS NULL`); n != 1 {
		t.Errorf("usr_b500 holds %d live rules, want 1", n)
	}
	if got, want := reached("usr_b500", domain.CapRead, otherKey), []domain.EnvID{"env_a_prod", "env_b_prod"}; !slices.Equal(got, want) {
		t.Errorf("usr_b500 reaches %v after 00102, want %v as before", got, want)
	}
	var wide struct {
		EnvItems int      `json:"env_items"`
		EnvIDs   []string `json:"env_ids"`
		EnvNames []string `json:"env_names"`
	}
	if err := json.Unmarshal([]byte(db.text(`SELECT payload FROM audit_tenant_events WHERE type = 'rule.disabled' AND object_id = 'rul_wide'`)), &wide); err != nil {
		t.Fatalf("rul_wide rule.disabled payload: %v", err)
	}
	if wide.EnvItems != 1001 || len(wide.EnvIDs) != 1000 || wide.EnvIDs[0] != "prj_e/env_e_0000" || wide.EnvIDs[999] != "prj_e/env_e_0999" ||
		len(wide.EnvNames) != 1000 || wide.EnvNames[0] != "e0000" || wide.EnvNames[999] != "e0999" {
		t.Errorf("rul_wide rule.disabled = %d items, %d ids (%v ... %v), %d names; want 1001, the first 1000 ids and names",
			wide.EnvItems, len(wide.EnvIDs), wide.EnvIDs[:1], wide.EnvIDs[len(wide.EnvIDs)-1:], len(wide.EnvNames))
	}

	// Consistent: converted in place to one rule-wide name item.
	if n := db.count(`SELECT COUNT(*) FROM rule_items WHERE rule_id = 'rul_cons' AND axis = 'env' AND project_id IS NULL AND env_name = 'prod'`); n != 1 {
		t.Errorf("rul_cons has %d rule-wide prod items, want 1", n)
	}
	// Splits: originals gone, one rule per project that reached something.
	splits := map[string]struct {
		principal string
		projects  int
	}{"rul_only": {"usr_only", 1}, "rul_exc": {"usr_exc", 2}, "rul_fe": {"usr_fe", 1}, "rul_fr": {"usr_fr", 2}}
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
		// The audit names exactly the stored replacements: both sorted, a
		// repeated id kept, so [A, A] for replacements A and B fails.
		var stored []string
		for i := range want.projects {
			stored = append(stored, db.text(fmt.Sprintf(`SELECT id FROM rules WHERE principal_id = '%s' ORDER BY id LIMIT 1 OFFSET %d`, want.principal, i)))
		}
		// The engines' collations differ, so both lists sort in Go.
		slices.Sort(stored)
		named := slices.Clone(split.RuleIDs)
		slices.Sort(named)
		if split.RuleID != old || !slices.Equal(named, stored) {
			t.Errorf("rule.split for %s = %+v, want the stored replacements %v", old, split, stored)
		}
	}
	// The Edit folder rule keeps exactly its folder in a: one rule, only svc/,
	// no key or folder gained. The See twin keeps b with all keys.
	feRules, err := db.rules.Rules(ctx, "usr_fe")
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := map[domain.ProjectID][]domain.RuleKeyItem{"prj_a": {{Folder: "svc/", IsFolder: true}}}
	if len(feRules) != 1 || feRules[0].Where.KeyMode != domain.AxisOnly || !reflectx.Equal(feRules[0].Where.Keys, wantKeys) {
		t.Errorf("usr_fe split keys = %+v, want one rule over only %v", feRules, wantKeys)
	}
	if n := db.count(`SELECT COUNT(*) FROM rules AS r JOIN rule_items AS i ON i.rule_id = r.id AND i.axis = 'project' WHERE r.principal_id = 'usr_fr' AND i.project_id = 'prj_b' AND r.key_mode = 'all'`); n != 1 {
		t.Errorf("usr_fr split has no all-keys rule in b: %d", n)
	}
	// Fail closed: kept, no environment, a manager-facing fault, audited.
	// The audit keeps the removed selection, so a manager can rebuild it.
	removed := map[string]string{"rul_allp": "prj_a/env_a_prod", "rul_out": "prj_b/env_b_prod"}
	// rul_allp's item is outside its (no) projects too; its own reason wins.
	faults := map[string]string{
		"rul_allp": "Disabled by the upgrade to environment names: this rule covers all projects and names environments, which it could not hold before. It reaches no environment; revoke it and create it again.",
		"rul_out":  "Disabled by the upgrade to environment names: it names an environment of a project the rule does not cover. It reaches no environment; revoke it and create it again.",
	}
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
		if got := db.text(`SELECT env_fault FROM rules WHERE id = '` + id + `' AND env_mode = 'only'`); got != faults[id] {
			t.Errorf("%s fault = %q, want %q", id, got, faults[id])
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
	// The whole-project rules keep their one row, faulted and audited, and
	// neither is split: no live rule over b appears.
	for id, envIDs := range map[string][]string{"rul_ps": {"prj_a/env_a_prod"}, "rul_psc": {"prj_a/env_a_prod", "prj_b/env_b_prod"}} {
		const fault = "Disabled by the upgrade to environment names: its permission needs whole projects but it names environments, so it reached nothing. It reaches no environment; revoke it and create it again."
		principal := "usr_" + strings.TrimPrefix(id, "rul_")
		if n := db.count(`SELECT COUNT(*) FROM rules WHERE principal_id = '` + principal + `'`); n != 1 {
			t.Errorf("%s holds %d rules after 00102, want its one faulted rule", principal, n)
		}
		if got := db.text(`SELECT env_fault FROM rules WHERE id = '` + id + `' AND env_mode = 'only'`); got != fault {
			t.Errorf("%s fault = %q, want %q", id, got, fault)
		}
		if n := db.count(`SELECT COUNT(*) FROM rule_items WHERE rule_id = '` + id + `' AND axis = 'env'`); n != 0 {
			t.Errorf("%s kept %d environment items", id, n)
		}
		if n := db.count(`SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'rule.split' AND object_id = '` + id + `'`); n != 0 {
			t.Errorf("%s was split", id)
		}
		var disabled struct {
			EnvMode string   `json:"env_mode"`
			EnvIDs  []string `json:"env_ids"`
		}
		if err := json.Unmarshal([]byte(db.text(`SELECT payload FROM audit_tenant_events WHERE type = 'rule.disabled' AND object_id = '`+id+`'`)), &disabled); err != nil {
			t.Fatalf("%s rule.disabled payload: %v", id, err)
		}
		if disabled.EnvMode != "all" || !slices.Equal(disabled.EnvIDs, envIDs) {
			t.Errorf("%s rule.disabled records %+v, want all except %v", id, disabled, envIDs)
		}
		if got := reached(domain.PrincipalID(principal), domain.CapProjectSettings, nil); len(got) != 0 {
			t.Errorf("disabled rule of %s reaches %v", principal, got)
		}
	}
	if n := db.count(`SELECT COUNT(*) FROM audit_tenant_events WHERE type IN ('rule.split', 'rule.disabled')`); n != 25 {
		t.Errorf("%d migration audit events, want 25", n)
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
		// JSON decodes a count as float64 and a list as []any; the registry
		// types them int64 and []string.
		if n, ok := e.Payload["env_items"].(float64); ok {
			e.Payload["env_items"] = int64(n)
		}
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
	// env_fault is copied as stored: a faulted part revokes only by a manager
	// over its whole scope, so dropping it would hand the live store a valid rule.
	migrated := lines(`SELECT 'INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, env_fault, created_by, created_at) VALUES (' || ` +
		lit("id") + ` || ', ''usr_rvtarget'', ' || ` + lit("capability") + ` || ', ''org_rv'', ' || ` + lit("project_mode") + ` || ', ' || ` +
		lit("env_mode") + ` || ', ' || ` + lit("key_mode") + ` || ', ' || ` + lit("env_fault") + ` || ', ''usr_rvmgr'', ` + strings.ReplaceAll(at, "'", "''") + `)' FROM rules WHERE principal_id = 'usr_rvtarget' ORDER BY id`)
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
		if id, ok := strings.CutPrefix(stmt, "INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, env_fault, created_by, created_at) VALUES ('"); ok {
			ids = append(ids, id[:strings.Index(id, "'")])
		}
	}
	if len(ids) != 2 {
		t.Fatalf("migrated rules %v, want one per project", ids)
	}
	// Each part holds one project and that project's own name.
	if got := lines(`SELECT p.project_id || '/' || n.env_name FROM rules AS r
		JOIN rule_items AS p ON p.rule_id = r.id AND p.axis = 'project'
		JOIN rule_items AS n ON n.rule_id = r.id AND n.axis = 'env'
		WHERE r.principal_id = 'usr_rvtarget' ORDER BY 1`); !slices.Equal(got, []string{"prj_rva/dev", "prj_rvb/prod"}) {
		t.Fatalf("migrated parts select %v, want [prj_rva/dev prj_rvb/prod]", got)
	}
	for _, id := range ids {
		if err := svc.Revoke(ctx, service.LocalPrincipal("usr_rvmgr"), "org_rv", id); err != nil {
			t.Fatalf("revoke migrated rule %s by the manager of its environments: %v (migrated rows %q)", id, err, migrated)
		}
	}
}
