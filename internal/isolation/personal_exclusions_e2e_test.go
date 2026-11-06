package isolation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Personal values never leave the owner's own surfaces (personal-values ADR,
// What this guarantee is, and Implementation acceptance). Every check compares
// against a random sentinel and never prints it.

const pvConfigKey = "PV_EXCLUSION_CONFIG"

func personalSentinel(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return "pv-sentinel-" + hex.EncodeToString(b)
}

// carries reports whether v's JSON form holds the sentinel. A surface that
// cannot be marshalled fails the test rather than passing unseen.
func carries(t *testing.T, sentinel string, v any) bool {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	return strings.Contains(string(raw), sentinel)
}

func grantEnvCap(t *testing.T, db *store.DB, id string, principal domain.PrincipalID, capability domain.Capability, env domain.EnvID) {
	t.Helper()
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) `+
		`VALUES ('`+id+`', '`+string(principal)+`', '`+string(capability)+`', 'org_a', 'prj_a1', '`+string(env)+`', `+ts+`)`)
	execRaw(t, db, `INSERT INTO grant_origins (id, grant_id, kind, subject, created_at) `+
		`VALUES ('gor_`+id+`', '`+id+`', 'manual', '`+string(principal)+`', `+ts+`)`)
}

func dropGrants(t *testing.T, db *store.DB, principal domain.PrincipalID, caps ...string) {
	t.Helper()
	sel := `SELECT id FROM grants WHERE principal_id = '` + string(principal) + `' AND capability IN ('` + strings.Join(caps, "', '") + `')`
	execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id IN (`+sel+`)`)
	execRaw(t, db, `DELETE FROM grants WHERE id IN (`+sel+`)`)
}

// No personal value reaches machine delivery, a non-developer human delivery,
// offline receipts, snapshots or revisions, diffs, copy, clone, the values
// export or the definitions export. A consented developer credential receives
// it, which proves the sentinel was live.
func TestPersonalValueExcludedFromSharedSurfaces(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-excl")
		ctx := t.Context()
		sentinel := personalSentinel(t)
		owner := e.admin.boot.PrincipalID
		adminID := domain.PrincipalID(queryString(t, db, `SELECT principal_id FROM accounts WHERE username = 'pv-excl-admin'`))
		admin := service.LocalPrincipal(adminID)
		project := scopeProject(orgA, prjA1)
		keyID := "key_" + pvConfigKey

		// A config key, so read-only machine delivery, clone and copy all carry
		// its shared plaintext and would carry a personal value if they could.
		execRaw(t, db, `INSERT INTO keys (id, org_id, project_id, name, folder_path, classification, `+
			`description, deprecated, deprecation_note, declaration, required_mode, forbidden_mode, `+
			`group_id, created_at) VALUES ('`+keyID+`', 'org_a', 'prj_a1', '`+pvConfigKey+`', '', 'config', `+
			`'', FALSE, '', '{"rule":{"type":"string"}}', 'none', 'none', NULL, `+ts+`)`)
		shared := "plaintext-" + pvConfigKey
		publishValue(t, e.values, service.LocalPrincipal(custodian), scopeEnv(orgA, prjA1, envA1), pvConfigKey, shared)

		snapshots := `SELECT COUNT(*) FROM snapshots WHERE environment_id = 'env_a1'`
		changes := `SELECT COUNT(*) FROM revision_key_changes WHERE environment_id = 'env_a1'`
		beforeSnapshots, beforeChanges := queryInt(t, db, snapshots), queryInt(t, db, changes)
		e.setOwn(t, pvConfigKey, keyID, sentinel)
		e.setOwn(t, ceremonySecretA, e.keyA, sentinel)
		// Snapshots and revisions: a personal set writes neither.
		if queryInt(t, db, snapshots) != beforeSnapshots || queryInt(t, db, changes) != beforeChanges {
			t.Fatal("a personal set wrote a snapshot or a revision change")
		}

		// Positive control: the owner's consented developer credential receives it.
		minted, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, true)
		del := deliverySvc(t, db)
		mine, err := del.Fetch(ctx, minted.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !valueIs(deliveredKey(t, mine, pvConfigKey), sentinel) {
			t.Fatal("positive control: the owner's developer credential did not receive the personal value")
		}

		// Machine delivery and its offline receipts.
		ident := identitySvc(db)
		sa, err := ident.CreateServiceAccount(ctx, admin, project, "pv-excl-workload", domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		cred, err := ident.MintCredential(ctx, admin, project, sa.ID, service.MintRequest{})
		if err != nil {
			t.Fatal(err)
		}
		grantEnvCap(t, db, "g_pv_excl_machine_read", sa.Principal, domain.CapRead, envA1)
		machine, err := del.Fetch(ctx, cred.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if k := deliveredKey(t, machine, pvConfigKey); !valueIs(k, shared) || k.Personal != "" || carries(t, sentinel, machine) {
			t.Fatal("machine delivery carried a personal value or lost the shared one")
		}
		// The owner's own human session and a below-network principal outside
		// a developer credential.
		session, err := del.Fetch(ctx, e.admin.token, envScope(envA1), "", service.FetchOptions{})
		if err != nil || carries(t, sentinel, session) {
			t.Fatalf("human-session delivery failed (err=%v) or carried a personal value", err)
		}
		if !valueIs(deliveredKey(t, session, pvConfigKey), shared) {
			t.Fatal("human-session delivery lost the shared value")
		}
		local, err := del.FetchAs(ctx, service.LocalPrincipal(owner), envScope(envA1), "", service.FetchOptions{})
		if err != nil || carries(t, sentinel, local) {
			t.Fatalf("local delivery failed (err=%v) or carried a personal value", err)
		}

		// Revisions: history, the latest revision and its diff, and the export.
		revs := revisionSvc(t, db)
		latest := int64(queryInt(t, db, `SELECT MAX(revision) FROM snapshots WHERE environment_id = 'env_a1'`))
		history, err := revs.History(ctx, admin, envScope(envA1), latest+1, 50)
		if err != nil || carries(t, sentinel, history) {
			t.Fatalf("revision history: err=%v", err)
		}
		detail, err := revs.Show(ctx, admin, envScope(envA1), latest)
		if err != nil || carries(t, sentinel, detail) {
			t.Fatalf("revision show: err=%v", err)
		}
		revDiff, err := revs.Diff(ctx, admin, envScope(envA1), latest-1, latest, keyID)
		if err != nil || carries(t, sentinel, revDiff) {
			t.Fatalf("revision diff: err=%v", err)
		}
		// Positive control: the latest revision published the config key, so
		// its diff carries the shared plaintext.
		if !slices.ContainsFunc(revDiff.Items, func(r service.RevisionDiffRow) bool {
			return r.KeyID == keyID && r.After != nil && *r.After == shared
		}) {
			t.Fatal("revision diff lost the shared value")
		}
		exported, _, err := revs.ExportWithParameters(ctx, service.LocalPrincipal(custodian), envScope(envA1), 0, true, nil)
		if err != nil || carries(t, sentinel, exported) {
			t.Fatalf("values export: err=%v", err)
		}

		// Clone, copy and the environment diff.
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		clone, cloned, err := envs.Clone(ctx, admin, project, "pv-excl-clone", string(envA1), nil)
		if err != nil {
			t.Fatal(err)
		}
		if carries(t, sentinel, cloned) || queryInt(t, db, `SELECT COUNT(*) FROM personal_values WHERE environment_id = '`+clone.ID+`'`) != 0 {
			t.Fatal("clone carried a personal value")
		}
		copied, err := e.values.Copy(ctx, admin, project, service.CopyRequest{
			SourceEnvironmentID: string(envA1), KeyNames: []string{pvConfigKey}, DestinationEnvironmentIDs: []string{clone.ID},
		})
		if err != nil || carries(t, sentinel, copied) {
			t.Fatalf("copy: err=%v", err)
		}
		cloneScope := scopeEnv(orgA, prjA1, domain.EnvID(clone.ID))
		cloneValues, _, err := revs.ExportWithParameters(ctx, service.LocalPrincipal(custodian), cloneScope, 0, true, nil)
		if err != nil || carries(t, sentinel, cloneValues) {
			t.Fatalf("clone export: err=%v", err)
		}
		found := false
		for _, v := range cloneValues {
			found = found || (v.Name == pvConfigKey && v.Value == shared)
		}
		if !found {
			t.Fatal("clone and copy lost the shared value")
		}
		diff, err := e.values.Diff(ctx, service.LocalPrincipal(custodian), project, string(envA1), clone.ID, true)
		if err != nil || carries(t, sentinel, diff) {
			t.Fatalf("environment diff: err=%v", err)
		}
		if !slices.ContainsFunc(diff, func(r service.DiffRow) bool { return r.KeyID == keyID && r.Left.Value == shared }) {
			t.Fatal("environment diff lost the shared value")
		}

		// Definitions export, both forms.
		defs := definitionsService(t, db)
		for _, portable := range []bool{false, true} {
			raw, err := defs.Export(ctx, admin, project, portable)
			if err != nil || strings.Contains(string(raw), sentinel) {
				t.Fatalf("definitions export (portable=%t): err=%v", portable, err)
			}
		}

		// No audit row and no stored column holds the plaintext.
		for _, table := range []string{"audit_tenant_events", "audit_instance_events"} {
			if n := queryInt(t, db, `SELECT COUNT(*) FROM `+table+` WHERE payload LIKE '%`+sentinel+`%'`); n != 0 {
				t.Fatalf("%s holds the personal value", table)
			}
		}
	})
}

// No preset, explicit grant or member access rule over the whole environment
// reads another person's value, and an administrator's own set never
// overwrites it. Each authority is shown to be live on the shared value.
func TestPersonalValueNoGrantPresetOrRuleReadsAnother(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-reach")
		ctx := t.Context()
		sentinel := personalSentinel(t)
		e.setOwn(t, ceremonySecretA, e.keyA, sentinel)
		adminID := domain.PrincipalID(queryString(t, db, `SELECT principal_id FROM accounts WHERE username = 'pv-reach-admin'`))

		refused := func(how string) {
			t.Helper()
			e.adminTok = personalReauth(t, e.admin.auth, e.adminTok, e.adminDev, e.keyA)
			sharedValue, err := e.values.Get(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, true)
			if err != nil || sharedValue.Value != "plaintext-"+ceremonySecretA {
				t.Fatalf("%s: the authority does not reveal the shared value (err=%v)", how, err)
			}
			e.adminTok = personalReauth(t, e.admin.auth, e.adminTok, e.adminDev, e.keyA)
			got, err := e.pv.Reveal(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA)
			if !errors.Is(err, domain.ErrNotFound) || got.Value == sentinel {
				t.Fatalf("%s: reveal of another person's value = %v", how, err)
			}
			if mine, err := e.pv.List(ctx, service.Bearer(e.adminTok), envScope(envA1)); err != nil || carries(t, sentinel, mine) {
				t.Fatalf("%s: list err=%v", how, err)
			}
			for _, h := range mustHolders(t, e) {
				if carries(t, sentinel, h) {
					t.Fatalf("%s: members carried the value", how)
				}
			}
		}

		// The administrator preset: read and reveal from the template.
		refused("preset")
		// Explicit environment grants in place of the preset's.
		dropGrants(t, db, adminID, "read", "reveal")
		grantEnvCap(t, db, "g_pv_reach_read", adminID, domain.CapRead, envA1)
		grantEnvCap(t, db, "g_pv_reach_reveal", adminID, domain.CapReveal, envA1)
		refused("grant")
		// Member access rules over every environment and key of the project.
		dropGrants(t, db, adminID, "read", "reveal")
		for _, c := range []string{"read", "reveal"} {
			id := "rul_pv_reach_" + c
			execRaw(t, db, `INSERT INTO rules (id, principal_id, capability, org_id, env_mode, key_mode, created_by, created_at) `+
				`VALUES ('`+id+`', '`+string(adminID)+`', '`+c+`', 'org_a', 'all', 'all', '`+string(adminID)+`', `+ts+`)`)
			execRaw(t, db, `INSERT INTO rule_items (id, rule_id, org_id, axis, project_id) VALUES ('rli_`+id+`', '`+id+`', 'org_a', 'project', 'prj_a1')`)
		}
		refused("rule")

		// The administrator's own set is theirs alone.
		e.adminTok = personalReauth(t, e.admin.auth, e.adminTok, e.adminDev, e.keyA)
		if _, err := e.pv.Set(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, "administrator-own-synthetic"); err != nil {
			t.Fatal(err)
		}
		e.admin.token = personalReauth(t, e.admin.auth, e.admin.token, e.device, e.keyA)
		if own, err := e.pv.Reveal(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA); err != nil || own.Value != sentinel {
			t.Fatalf("the owner's value changed after an administrator's set (err=%v)", err)
		}
	})
}

func mustHolders(t *testing.T, e personalEnv) []service.PersonalHolder {
	t.Helper()
	holders, err := e.pv.Members(t.Context(), service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	return holders
}

// Only the owner's own surfaces and reencrypt open a personal value: the
// personal repo is reached from personal.go (list, set, reveal, member
// administration, and the metadata-only count a definitions deletion plan
// shows), delivery_personal.go (the developer-credential overlay)
// and reencrypt.go, its owner-bound ciphertext readers only from the first
// two and reencrypt, and the generated ciphertext queries only from
// repos_personal.go. So no adapter payload or plan, offline receipt,
// snapshot, diff, copy, clone or export can carry one.
func TestPersonalValueCiphertextReadersAreConfined(t *testing.T) {
	repoCallers := map[string]bool{
		"internal/service/personal.go":          true,
		"internal/service/delivery_personal.go": true,
		"internal/service/reencrypt.go":         true,
	}
	readers := map[string]map[string]bool{
		"GetOwn":           {"internal/service/personal.go": true},
		"ListOwn":          {"internal/service/personal.go": true, "internal/service/delivery_personal.go": true},
		"ListForReencrypt": {"internal/service/reencrypt.go": true},
	}
	queries := map[string]bool{"GetPersonalValueForOwner": true, "ListPersonalValuesForOwner": true, "ListPersonalValuesForReencrypt": true}
	const queryCaller = "internal/store/repos_personal.go"

	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	inspect := func(rel string, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			// A store accessor call, r.PersonalValues(), not a field of that name.
			if call, ok := n.(*ast.CallExpr); ok && len(call.Args) == 0 {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "PersonalValues" && !strings.HasPrefix(rel, "internal/store/") {
					if !repoCallers[rel] {
						t.Errorf("%s reaches the personal repo: only the owner's own surfaces and reencrypt may", rel)
					}
					seen["PersonalValues "+rel] = true
				}
			}
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			name := sel.Sel.Name
			if queries[name] {
				if rel != queryCaller {
					t.Errorf("%s references the ciphertext query %s: only the personal repo may", rel, name)
				}
				seen[name+" "+rel] = true
			}
			if readers[name] != nil {
				if inner, ok := sel.X.(*ast.CallExpr); ok {
					if repo, ok := inner.Fun.(*ast.SelectorExpr); ok && repo.Sel.Name == "PersonalValues" {
						if !readers[name][rel] {
							t.Errorf("%s calls PersonalValues().%s: only the owner's own surfaces may open a personal value", rel, name)
						}
						seen[name+" "+rel] = true
					}
				}
			}
			return true
		})
	}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// The go tool's own rule, plus fixtures: other checkouts and
				// worktrees nested under a dot directory are never walked.
				if n := d.Name(); strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") || n == "testdata" || n == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			inspect(filepath.ToSlash(rel), file)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	stale := func(key string) {
		if !seen[key] {
			t.Errorf("pin is stale: no %s", key)
		}
	}
	for file := range repoCallers {
		stale("PersonalValues " + file)
	}
	for name, files := range readers {
		for file := range files {
			stale(name + " " + file)
		}
	}
	for name := range queries {
		stale(name + " " + queryCaller)
	}
}
