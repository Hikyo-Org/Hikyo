package isolation

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Tenant isolation (#23): the principals an SSH profile or an access or
// approval policy names are confined to the actor's orgs exactly as grant
// targets are. A principal outside them is answered like a nonexistent one, on
// the real HTTP stack and on both engines. Entries already stored on the
// edited policy or profile are kept without re-checking.

func TestPolicyMemberFootprint(t *testing.T) {
	forEngines(t, runPolicyMemberFootprint)
}

// fpID is a principal ID in the wire grammar.
func fpID(n int) string { return fmt.Sprintf("usr_0193f0b4-1f2a-7c31-9c1e-%012x", n) }

// rosterSite is one writer of a principal roster: where it lives on the wire,
// how to build its body naming ids, and which tables a write would touch.
type rosterSite struct {
	name   string
	path   string
	tables []string
	body   func(ids []string, edit bool) map[string]any
	read   func(id string) string
}

type footprintFixture struct {
	t  *testing.T
	db *store.DB
	e  accessWireEnv
	// The writers: org A only, both orgs, the instance, and project scope in org A.
	admin, bothAdmin, instAdmin, projectWriter string
	// The named: members of A, of both, of nothing, of B only, of the instance
	// only, and nobody. bAdmin administers org B, which owns bSA.
	memberA, shared, nobody, bOnly, instOnly, ghost string
	bAdmin                                          string
	bSA                                             domain.PrincipalID
	orgA, projA, envA, orgB, projB                  string
	sites                                           []rosterSite
	grantSeq                                        int
	tokens                                          map[string]string // minted once, so a failure lands on the fixture's own test
}

func runPolicyMemberFootprint(t *testing.T, db *store.DB) {
	f := newFootprintFixture(t, db)
	adminToken := f.token(f.admin)
	miss := []string{f.ghost}
	foreign := map[string][]string{"org_b_only": {f.bOnly}, "instance_only": {f.instOnly}, "org_b_service_account": {string(f.bSA)}}

	t.Run("foreign_principals_look_nonexistent", func(t *testing.T) {
		f := f.in(t)
		for _, s := range f.sites {
			f.fresh()
			// POST: refused identically to a nonexistent ID, and nothing is written.
			rows := f.rows(s)
			wantCode, want := f.call(adminToken, http.MethodPost, s.path, s.body(miss, false))
			if wantCode != http.StatusBadRequest {
				t.Fatalf("%s POST nonexistent = %d %s, want 400", s.name, wantCode, want)
			}
			for label, ids := range foreign {
				code, body := f.call(adminToken, http.MethodPost, s.path, s.body(ids, false))
				if code != wantCode || !bytes.Equal(body, want) {
					t.Fatalf("%s POST %s = %d %s\nnonexistent  = %d %s", s.name, label, code, body, wantCode, want)
				}
			}
			if got := f.rows(s); got != rows {
				t.Fatalf("%s: refused POSTs changed rows %d -> %d", s.name, rows, got)
			}

			// PUT: the same, and the stored roster and version stay as they were.
			id := f.create(adminToken, s, []string{f.memberA})
			stored, rows := f.stored(adminToken, s, id), f.rows(s)
			wantCode, want = f.call(adminToken, http.MethodPut, f.item(s, id), s.body(miss, true))
			if wantCode != http.StatusBadRequest {
				t.Fatalf("%s PUT nonexistent = %d %s, want 400", s.name, wantCode, want)
			}
			for label, ids := range foreign {
				code, body := f.call(adminToken, http.MethodPut, f.item(s, id), s.body(ids, true))
				if code != wantCode || !bytes.Equal(body, want) {
					t.Fatalf("%s PUT %s = %d %s\nnonexistent  = %d %s", s.name, label, code, body, wantCode, want)
				}
			}
			if got := f.stored(adminToken, s, id); got != stored || f.rows(s) != rows {
				t.Fatalf("%s: refused PUTs changed the stored state:\n%s\n%s", s.name, stored, got)
			}
		}
	})

	t.Run("refused_bypasser_leaves_foreign_machines_removable", func(t *testing.T) {
		f := f.in(t)
		machine, err := identitySvc(db).CreateServiceAccount(t.Context(), service.LocalPrincipal(domain.PrincipalID(f.bAdmin)), domain.Scope{Org: domain.OrgID(f.orgB), Project: domain.ProjectID(f.projB)}, "fp-machine", domain.ClassWorkload)
		if err != nil {
			t.Fatalf("service account: %v", err)
		}
		seedSCIMProvider(t, db, "fp-scim", "https://fp-scim.example.test", true)
		scim := &service.SCIM{DB: db}
		binding, err := scim.CreateBinding(t.Context(), service.LocalPrincipal(root), domain.OrgID(f.orgB), service.SCIMBindingInput{
			ProviderKind: domain.ProviderOIDC, ProviderSlug: "fp-scim", SubjectSource: domain.SubjectSourceExternalID,
		})
		if err != nil {
			t.Fatalf("scim binding: %v", err)
		}
		for _, s := range f.sites[1:] { // every policy roster; the SSH list holds no machine authority to remove
			for _, id := range []string{string(machine.Principal), binding.ConnectionPrincipalID} {
				if code, body := f.call(adminToken, http.MethodPost, s.path, s.body([]string{id}, false)); code != http.StatusBadRequest {
					t.Fatalf("%s naming a foreign machine = %d %s, want 400", s.name, code, body)
				}
			}
		}
		del := api.PathPrefix + "/orgs/" + f.orgB + "/projects/" + f.projB + "/service-accounts/" + machine.ID
		if code, body := f.call(f.token(f.bAdmin), http.MethodDelete, del, nil); code != http.StatusNoContent {
			t.Fatalf("org B deleting its service account = %d %s, want 204", code, body)
		}
		if err := scim.DeleteBinding(t.Context(), service.LocalPrincipal(root), domain.OrgID(f.orgB), binding.ID); err != nil {
			t.Fatalf("org B retiring its binding: %v", err)
		}
	})

	t.Run("principals_inside_reach_are_accepted", func(t *testing.T) {
		f := f.in(t)
		for _, c := range []struct {
			actor string
			names []string
		}{
			{f.admin, []string{f.shared, f.memberA, f.nobody, f.admin}},
			{f.bothAdmin, []string{f.bOnly, string(f.bSA)}},
			{f.instAdmin, []string{f.bOnly, f.instOnly, string(f.bSA)}},
			{f.projectWriter, []string{f.memberA, f.shared}},
		} {
			for _, s := range f.sites {
				if c.actor == f.projectWriter && s.name == "ssh_requester" {
					continue // profiles need environment-scope authority this writer lacks by design
				}
				for _, name := range c.names {
					f.fresh()
					if code, body := f.call(f.token(c.actor), http.MethodPost, s.path, s.body([]string{name}, false)); code/100 != 2 {
						t.Fatalf("%s: %s naming %s = %d %s, want 2xx", s.name, c.actor, name, code, body)
					}
				}
			}
		}
	})

	t.Run("stored_entries_are_kept", func(t *testing.T) {
		f := f.in(t)
		for _, s := range f.sites {
			f.fresh()
			id := f.create(f.token(f.instAdmin), s, []string{f.bOnly})
			if code, body := f.call(adminToken, http.MethodPut, f.item(s, id), s.body([]string{f.bOnly}, true)); code/100 != 2 {
				t.Fatalf("%s: editing an unrelated field while keeping a stored foreign entry = %d %s", s.name, code, body)
			}
			stored := f.stored(adminToken, s, id)
			wantCode, want := f.call(adminToken, http.MethodPut, f.item(s, id), s.body([]string{f.bOnly, f.ghost}, true))
			code, body := f.call(adminToken, http.MethodPut, f.item(s, id), s.body([]string{f.bOnly, f.instOnly}, true))
			if wantCode != http.StatusBadRequest || code != wantCode || !bytes.Equal(body, want) {
				t.Fatalf("%s: adding a foreign entry = %d %s\nadding a nonexistent one = %d %s", s.name, code, body, wantCode, want)
			}
			if got := f.stored(adminToken, s, id); got != stored {
				t.Fatalf("%s: a refused edit changed the stored state:\n%s\n%s", s.name, stored, got)
			}
		}
	})

	// An entry kept is kept in its own role, so moving one to the other role names
	// it anew, whatever was stored for it.
	t.Run("stored_entry_moved_to_the_other_role_is_checked", func(t *testing.T) {
		f := f.in(t)
		approverRow := func(table, id string, i int, subject string) string {
			return fmt.Sprintf(`INSERT INTO %s (id, org_id, project_id, policy_id, kind, subject_id, scope_binding_id) VALUES ('fp_moved_%d', '%s', '%s', '%s', 'principal', '%s', '')`,
				table, i, f.orgA, f.projA, id, subject)
		}
		bypasserRow := func(table, id string, i int, subject string) string {
			return fmt.Sprintf(`INSERT INTO %s (id, org_id, project_id, policy_id, principal_id) VALUES ('fp_moved_%d', '%s', '%s', '%s', '%s')`,
				table, i, f.orgA, f.projA, id, subject)
		}
		for _, c := range []struct {
			site, table string
			row         func(table, id string, i int, subject string) string
			stored      []string
		}{
			{"access_bypasser", "access_policy_approvers", approverRow, []string{f.ghost, f.bOnly}},
			{"approval_bypasser", "approval_policy_approvers", approverRow, []string{f.ghost, f.bOnly}},
			// A bypasser row references its principal, so only the foreign one can be stored.
			{"access_approver", "access_policy_bypassers", bypasserRow, []string{f.bOnly}},
			{"approval_approver", "approval_policy_bypassers", bypasserRow, []string{f.bOnly}},
		} {
			s := f.sites[slices.IndexFunc(f.sites, func(r rosterSite) bool { return r.name == c.site })]
			f.fresh()
			id := f.create(adminToken, s, []string{f.memberA})
			for i, subject := range c.stored {
				execRaw(t, db, c.row(c.table, id, i, subject))
			}
			wantCode, want := f.call(adminToken, http.MethodPut, f.item(s, id), s.body(miss, true))
			code, body := f.call(adminToken, http.MethodPut, f.item(s, id), s.body([]string{f.bOnly}, true))
			if wantCode != http.StatusBadRequest || code != wantCode || !bytes.Equal(body, want) {
				t.Fatalf("%s: moving a foreign entry = %d %s\nmoving a nonexistent one = %d %s", s.name, code, body, wantCode, want)
			}
		}
	})

	// Writers whose rosters overlap must not take the same principal rows in
	// opposite orders. Holding a row stops a write there, with higher rows free.
	t.Run("roster_writes_lock_named_principals_in_sorted_order", func(t *testing.T) {
		if db.Engine() == store.EngineSQLite {
			t.Skip("one writer at a time: there are no row locks to order")
		}
		f := f.in(t)
		for _, s := range f.sites {
			f.fresh()
			id := f.create(adminToken, s, []string{f.memberA})
			// A kept entry takes its place in the order, wherever the body lists it.
			f.stopsAtRow(s.name, f.memberA, f.shared, func() (int, []byte) {
				return f.call(adminToken, http.MethodPut, f.item(s, id), s.body([]string{f.shared, f.memberA}, true))
			})
			// So does the actor, which the write references without naming it.
			f.fresh()
			f.stopsAtRow(s.name, f.admin, f.shared, func() (int, []byte) {
				return f.call(adminToken, http.MethodPost, s.path, s.body([]string{f.shared}, false))
			})
		}
	})

	t.Run("stored_requester_without_a_principal_does_not_block_an_edit", func(t *testing.T) {
		f := f.in(t)
		s := f.sites[0]
		f.fresh()
		id := f.create(adminToken, s, []string{f.memberA})
		// The requester list has no foreign key: deleting a principal leaves its entry behind.
		execRaw(t, db, `INSERT INTO ssh_profile_requesters (org_id, project_id, environment_id, profile_id, principal_id, created_at) VALUES `+
			`('`+f.orgA+`', '`+f.projA+`', '`+f.envA+`', '`+id+`', '`+f.ghost+`', `+ts+`)`)
		if code, body := f.call(adminToken, http.MethodPut, f.item(s, id), s.body([]string{f.memberA, f.ghost}, true)); code/100 != 2 {
			t.Fatalf("editing a profile that keeps a vanished requester = %d %s", code, body)
		}
	})
}

func newFootprintFixture(t *testing.T, db *store.DB) *footprintFixture {
	t.Helper()
	ctx := t.Context()
	f := &footprintFixture{t: t, db: db, ghost: fpID(0xdead)}
	f.admin, f.bothAdmin, f.instAdmin, f.projectWriter = fpID(1), fpID(2), fpID(3), fpID(4)
	f.memberA, f.shared, f.nobody, f.bOnly, f.instOnly, f.bAdmin = fpID(5), fpID(6), fpID(7), fpID(8), fpID(9), fpID(10)
	for _, p := range []string{f.admin, f.bothAdmin, f.instAdmin, f.projectWriter, f.memberA, f.shared, f.nobody, f.bOnly, f.instOnly, f.bAdmin} {
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('`+p+`', 'human', `+ts+`)`)
	}

	h := newAccessHarness(t, db)
	rootActor := service.LocalPrincipal(root)
	newOrg := func(name string) (org, project, env string) {
		o, err := (&service.Orgs{DB: db}).Create(ctx, rootActor, name, true, []byte(`{}`))
		if err != nil {
			t.Fatalf("org: %v", err)
		}
		p, err := (&service.Projects{DB: db}).Create(ctx, rootActor, domain.OrgID(o.ID), name+"-p")
		if err != nil {
			t.Fatalf("project: %v", err)
		}
		e, err := (&service.Environments{DB: db, Keyring: probeKeyring(t, db)}).Create(ctx, rootActor, domain.Scope{Org: domain.OrgID(o.ID), Project: domain.ProjectID(p.ID)}, name+"-e", nil)
		if err != nil {
			t.Fatalf("environment: %v", err)
		}
		return o.ID, p.ID, e.ID
	}
	f.orgA, f.projA, f.envA = newOrg("fp-a")
	f.orgB, f.projB, _ = newOrg("fp-b")

	// Org-scope authority for the writers, instance-scope for the instance admin.
	admin := []string{"manage-members", "project-settings", "manage-identities", "crypto-manage", "read"}
	f.grant(f.admin, f.orgA, "", admin...)
	f.grant(f.bothAdmin, f.orgA, "", admin...)
	f.grant(f.bothAdmin, f.orgB, "", "manage-members")
	f.grant(f.bAdmin, f.orgB, "", admin...)
	f.grant(f.instAdmin, "", "", admin...)
	f.grant(f.projectWriter, f.orgA, f.projA, "manage-members", "project-settings")
	f.grant(f.memberA, f.orgA, "", "read")
	f.grant(f.shared, f.orgA, "", "read")
	f.grant(f.shared, f.orgB, "", "read")
	f.grant(f.bOnly, f.orgB, "", "read")
	f.grant(f.instOnly, "", "", "read")
	seedOrigins(t, db)
	sa, err := identitySvc(db).CreateServiceAccount(ctx, service.LocalPrincipal(domain.PrincipalID(f.bAdmin)), domain.Scope{Org: domain.OrgID(f.orgB), Project: domain.ProjectID(f.projB)}, "fp-b-sa", domain.ClassWorkload)
	if err != nil {
		t.Fatalf("service account: %v", err)
	}
	f.bSA = sa.Principal

	node := newSSHNode(t, db)
	srv := httptest.NewServer(server.New(&server.API{
		Auth: h.auth, Grants: h.grants, Orgs: &service.Orgs{DB: db},
		Approvals: &service.Approvals{DB: db, Auth: h.auth}, Access: h.access, SSH: node.svc,
		Identities: identitySvc(db), Version: "wire",
	}, nil))
	t.Cleanup(srv.Close)
	f.e = accessWireEnv{srv: srv, db: db}

	envScope := domain.Scope{Org: domain.OrgID(f.orgA), Project: domain.ProjectID(f.projA), Env: domain.EnvID(f.envA)}
	ca, err := node.svc.CreateCA(ctx, service.LocalPrincipal(domain.PrincipalID(f.admin)), envScope, service.CreateSSHCARequest{Name: "fp"})
	if err != nil {
		t.Fatalf("ssh ca: %v", err)
	}

	principalApprovers := func(ids []string) []map[string]any {
		out := []map[string]any{}
		for _, id := range ids {
			out = append(out, map[string]any{"kind": "principal", "subject_id": id})
		}
		return out
	}
	policy := func(env string, edit bool) map[string]any {
		ttl := 3600
		if edit {
			ttl = 7200
		}
		return map[string]any{"environment_id": env, "capabilities": []string{"read"}, "max_duration_seconds": 3600, "min_approvals": 1,
			"request_ttl_seconds": ttl, "enabled": false, "allow_self_approval": false, "approvers": []any{}}
	}
	// The access policy API requires capabilities; the approval one ignores the extra field.
	approvalPolicy := func(env string, edit bool) map[string]any {
		m := policy(env, edit)
		delete(m, "capabilities")
		delete(m, "max_duration_seconds")
		return m
	}
	project := f.project()
	envPath := project + "/environments/" + f.envA
	policyRead := func(list string) func(string) string {
		return func(string) string { return project + "/" + list }
	}
	f.tokens = map[string]string{}
	for _, p := range []string{f.admin, f.bothAdmin, f.instAdmin, f.projectWriter, f.bAdmin} {
		f.tokens[p] = h.sessionArtifact(domain.PrincipalID(p), false)
	}
	f.sites = []rosterSite{
		{"ssh_requester", envPath + "/ssh-profiles", []string{"ssh_profiles", "ssh_profile_requesters"},
			func(ids []string, edit bool) map[string]any {
				ttl := 60
				if edit {
					ttl = 120
				}
				return map[string]any{"ca_id": ca.ID, "name": "fp-profile", "principals": []string{"x"}, "key_algorithms": []string{"ed25519"},
					"default_ttl_seconds": 60, "max_ttl_seconds": ttl, "requesters": ids}
			},
			func(id string) string { return envPath + "/ssh-profiles/" + id }},
		{"access_bypasser", project + "/access-policies", []string{"access_policies", "access_policy_approvers", "access_policy_bypassers"},
			func(ids []string, edit bool) map[string]any {
				m := policy(f.envA, edit)
				m["approvers"], m["bypassers"] = principalApprovers([]string{f.admin}), ids
				return m
			}, policyRead("access-policies")},
		{"access_approver", project + "/access-policies", []string{"access_policies", "access_policy_approvers", "access_policy_bypassers"},
			func(ids []string, edit bool) map[string]any {
				m := policy("", edit)
				m["approvers"] = principalApprovers(ids)
				return m
			}, policyRead("access-policies")},
		{"approval_bypasser", project + "/approval-policies", []string{"approval_policies", "approval_policy_approvers", "approval_policy_bypassers"},
			func(ids []string, edit bool) map[string]any {
				m := approvalPolicy(f.envA, edit)
				m["bypassers"] = ids
				return m
			}, policyRead("approval-policies")},
		{"approval_approver", project + "/approval-policies", []string{"approval_policies", "approval_policy_approvers", "approval_policy_bypassers"},
			func(ids []string, edit bool) map[string]any {
				m := approvalPolicy("", edit)
				m["approvers"] = principalApprovers(ids)
				return m
			}, policyRead("approval-policies")},
	}
	return f
}

// grant inserts one grant per capability; an empty org is instance scope and
// an empty project is org scope.
func (f *footprintFixture) grant(principal, org, project string, caps ...string) {
	null := func(v string) string {
		if v == "" {
			return "NULL"
		}
		return "'" + v + "'"
	}
	for _, c := range caps {
		f.grantSeq++
		execRaw(f.t, f.db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_fp_%d', '%s', '%s', %s, %s, NULL, %s)`,
			f.grantSeq, principal, c, null(org), null(project), ts))
	}
}

// in returns the fixture with its helpers failing on the running subtest.
func (f *footprintFixture) in(t *testing.T) *footprintFixture {
	c := *f
	c.t = t
	return &c
}

func (f *footprintFixture) token(principal string) string { return f.tokens[principal] }

// stopsAtRow runs write while holding held's principal row, and requires the
// write to wait for that row with free's row still unlocked, then to succeed.
func (f *footprintFixture) stopsAtRow(site, held, free string, write func() (int, []byte)) {
	t := f.t
	t.Helper()
	side, err := f.db.PG().Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var sidePID int
	err = side.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&sidePID)
	if err == nil {
		_, err = side.Exec(t.Context(), `SELECT id FROM principals WHERE id = $1 FOR SHARE`, held)
	}
	if err != nil {
		_ = side.Rollback(context.Background())
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	observed := make(chan error, 1)
	go func() { // owns side from here: the write cannot finish until it rolls back
		err := func() error {
			for {
				var waiting int
				if err := f.db.PG().QueryRow(ctx, `SELECT COUNT(*) FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, sidePID).Scan(&waiting); err != nil {
					return fmt.Errorf("the write finished without waiting for the held row: %w", err)
				}
				if waiting > 0 {
					_, err := side.Exec(ctx, `SELECT id FROM principals WHERE id = $1 FOR UPDATE NOWAIT`, free)
					return err
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
		_ = side.Rollback(context.Background())
		observed <- err
	}()
	code, body := write()
	stop()
	if err := <-observed; err != nil {
		t.Fatalf("%s: %v", site, err)
	}
	if code/100 != 2 {
		t.Fatalf("%s: the write = %d %s", site, code, body)
	}
}

func (f *footprintFixture) project() string {
	return api.PathPrefix + "/orgs/" + f.orgA + "/projects/" + f.projA
}

func (f *footprintFixture) call(token, method, path string, body any) (int, []byte) {
	f.t.Helper()
	return f.e.callAs(f.t, token, method, path, body)
}

func (f *footprintFixture) item(s rosterSite, id string) string { return s.path + "/" + id }

// stored is the roster and version as the read route renders them.
func (f *footprintFixture) stored(token string, s rosterSite, id string) string {
	f.t.Helper()
	code, body := f.call(token, http.MethodGet, s.read(id), nil)
	if code != http.StatusOK {
		f.t.Fatalf("%s: reading the stored state = %d %s", s.name, code, body)
	}
	return string(body)
}

func (f *footprintFixture) rows(s rosterSite) int64 {
	var n int64
	for _, table := range s.tables {
		n += queryInt(f.t, f.db, `SELECT COUNT(*) FROM `+table)
	}
	return n
}

// create writes a roster that must be accepted and returns the new ID.
func (f *footprintFixture) create(token string, s rosterSite, ids []string) string {
	f.t.Helper()
	code, body := f.call(token, http.MethodPost, s.path, s.body(ids, false))
	var created struct {
		ID string `json:"id"`
	}
	if code/100 != 2 || json.Unmarshal(body, &created) != nil || created.ID == "" {
		f.t.Fatalf("%s: setup write = %d %s", s.name, code, body)
	}
	return created.ID
}

// fresh clears every roster so each scenario starts without policies or profiles.
func (f *footprintFixture) fresh() {
	for _, table := range []string{
		"ssh_profile_requesters", "ssh_profiles",
		"access_policy_approvers", "access_policy_bypassers", "access_policies",
		"approval_policy_approvers", "approval_policy_bypassers", "approval_policies",
	} {
		execRaw(f.t, f.db, "DELETE FROM "+table)
	}
}
