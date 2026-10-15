package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	vw "github.com/descope/virtualwebauthn"
)

const missingProject = "prj_00000000-0000-7000-8000-000000000000"
const missingEnvironment = "env_00000000-0000-7000-8000-000000000000"

type entity struct {
	ID        string `json:"id"`
	Principal string `json:"principal_id"`
}
type issued struct {
	Value      string `json:"value"`
	Credential entity `json:"credential"`
}
type login struct {
	Token     string `json:"session_token"`
	Principal entity `json:"principal"`
}
type fixture struct {
	admin, password, org, otherOrg, project, sibling, foreign, environment string
	siblingEnvironment, foreignEnvironment                                 string
	account                                                                entity
	first, second, generated                                               issued
	cursor, mcpCursor                                                      string
	config, secret, metadata                                               string
	device                                                                 *passkey
	granted                                                                bool
	adminPrincipal                                                         string
}

func (f *fixture) projectPath() string { return "/api/v1/orgs/" + f.org + "/projects/" + f.project }
func (f *fixture) envPath() string     { return f.projectPath() + "/environments/" + f.environment }
func (f *fixture) forbidden() []string { return []string{f.secret, f.metadata, f.sibling, f.foreign} }
func (f *fixture) login(t *target) error {
	var out login
	if err := t.expect("POST", "/api/v1/auth/local/login", "", map[string]string{"username": "artifact-admin", "password": f.password, "artifact": "cli"}, 200, &out); err != nil {
		return err
	}
	if out.Token == "" {
		return errors.New("native login omitted token")
	}
	f.admin = out.Token
	if out.Principal.ID == "" {
		return errors.New("native login omitted principal identity")
	}
	f.adminPrincipal = out.Principal.ID
	if f.device != nil {
		return f.stepUp(t)
	}
	return nil
}

// passkey is the browser/authenticator half of the public WebAuthn protocol.
// The released server still validates attestation, origin, challenge and each
// signature through its ordinary go-webauthn path. It does not mint sessions.
type passkey struct {
	rp         vw.RelyingParty
	opts       vw.AuthenticatorOptions
	credential vw.Credential
}

func (f *fixture) assert(t *target, path string, body any, out any) error {
	var options json.RawMessage
	if err := t.expect("POST", path+"/start", f.admin, body, 200, &options); err != nil {
		return err
	}
	parsed, err := vw.ParseAssertionOptions(string(options))
	if err != nil {
		return errors.New("native assertion options invalid")
	}
	f.device.credential.Counter++
	// credProps belongs to registration. Assertions must not replay that
	// output when their options did not request an extension result.
	assertionOptions := f.device.opts
	assertionOptions.ClientExtensionResults = nil
	authenticator := vw.NewAuthenticatorWithOptions(assertionOptions)
	authenticator.AddCredential(f.device.credential)
	assertion := vw.CreateAssertionResponse(f.device.rp, authenticator, f.device.credential, *parsed)
	return t.expect("POST", path+"/finish", f.admin, json.RawMessage(assertion), 200, out)
}
func (f *fixture) stepUp(t *target) error {
	var elevated login
	if err := f.assert(t, "/api/v1/auth/webauthn/step-up", map[string]any{}, &elevated); err != nil {
		return err
	}
	if elevated.Token == "" {
		return errors.New("native WebAuthn stepup omitted rotated token")
	}
	f.admin = elevated.Token
	return nil
}
func (f *fixture) enrolFactor(t *target) error {
	var options json.RawMessage
	if err := t.expect("POST", "/api/v1/auth/webauthn/enrol/start", f.admin, map[string]string{"password": f.password}, 200, &options); err != nil {
		return err
	}
	parsed, err := vw.ParseAttestationOptions(string(options))
	if err != nil {
		return errors.New("native attestation options invalid")
	}
	origin, err := url.Parse(t.base)
	if err != nil {
		return err
	}
	opts := vw.AuthenticatorOptions{UserHandle: []byte(parsed.UserID), ClientExtensionResults: map[string]any{"credProps": map[string]any{"rk": true}}}
	f.device = &passkey{rp: vw.RelyingParty{Name: "hikyo", ID: origin.Hostname(), Origin: t.base}, opts: opts, credential: vw.NewCredential(vw.KeyTypeEC2)}
	authenticator := vw.NewAuthenticatorWithOptions(opts)
	authenticator.AddCredential(f.device.credential)
	attestation := vw.CreateAttestationResponse(f.device.rp, authenticator, f.device.credential, *parsed)
	var confirmed login
	if err = t.expect("POST", "/api/v1/auth/webauthn/enrol/finish", f.admin, json.RawMessage(attestation), 200, &confirmed); err != nil {
		return err
	}
	f.admin = confirmed.Token
	return f.stepUp(t)
}
func (f *fixture) reauthMint(t *target) error {
	var rotated struct {
		Token          string `json:"session_token"`
		SingleDecision bool   `json:"single_decision"`
	}
	if err := f.assert(t, "/api/v1/auth/webauthn/reauth", map[string]any{"operation": "mint", "environment_id": f.environment, "key_ids": []string{}}, &rotated); err != nil {
		return err
	}
	if rotated.Token == "" || !rotated.SingleDecision {
		return errors.New("native mint reauthentication did not issue a zero-window single-decision session")
	}
	f.admin = rotated.Token
	return nil
}
func (f *fixture) grant(t *target, present bool) error {
	path := f.projectPath() + "/grants"
	method := "POST"
	var body any = map[string]string{"principal": f.account.Principal, "capability": "read"}
	status := 200
	if present && !f.granted {
		if err := f.reauthMint(t); err != nil {
			return err
		}
	}
	if !present {
		method = "DELETE"
		body = nil
		status = 204
		if !f.granted {
			status = 404
		}
		path += "?principal=" + url.QueryEscape(f.account.Principal) + "&capability=read"
	}
	if err := t.expect(method, path, f.admin, body, status, nil); err != nil {
		return err
	}
	f.granted = present
	return nil
}
func setup(t *target) (f fixture, err error) {
	f.password = "Synthetic fixture password 2026 ! " + filepath.Base(t.dir)
	f.config = "fixture-config-value"
	f.secret = "fixture-secret-value"
	f.metadata = "foreign-private-metadata"
	if err = t.start(); err != nil {
		return f, err
	}
	authorityPath := filepath.Join(t.dir, "authority")
	if _, err = command(t.ctx, t.binary, t.dir, t.env, "admin", "create", "--username", "artifact-admin", "--output-file", authorityPath); err != nil {
		return f, errors.New("native local bootstrap ceremony failed")
	}
	if err = t.waitReady(); err != nil {
		return f, err
	}
	authority, err := os.ReadFile(authorityPath)
	if err != nil {
		return f, errors.New("read native bootstrap authority")
	}
	defer os.Remove(authorityPath)
	if err = t.expect("POST", "/api/v1/auth/credential/establish", "", map[string]string{"authority": strings.TrimSpace(string(authority)), "password": f.password}, 204, nil); err != nil {
		return f, err
	}
	if err = f.login(t); err != nil {
		return f, err
	}
	if err = f.enrolFactor(t); err != nil {
		return f, err
	}
	for i, name := range []string{"artifact-a", "artifact-b"} {
		var org entity
		if err = t.expect("POST", "/api/v1/orgs", f.admin, map[string]any{"name": name, "metadata": map[string]string{"private": f.metadata}}, 201, &org); err != nil {
			return f, err
		}
		if i == 0 {
			f.org = org.ID
		} else {
			f.otherOrg = org.ID
		}
		if err = f.login(t); err != nil {
			return f, err
		}
	}
	// Audit-reading is deliberately separate from administrator templates.
	// The instance membership administrator grants it explicitly through the
	// public API, then signs in again after the human privilege increase.
	if err = t.expect("POST", "/api/v1/orgs/"+f.org+"/grants", f.admin, map[string]string{"principal": f.adminPrincipal, "capability": "audit-read"}, 200, nil); err != nil {
		return f, err
	}
	if err = f.login(t); err != nil {
		return f, err
	}
	for i, org := range []string{f.org, f.org, f.otherOrg} {
		var project entity
		if err = t.expect("POST", "/api/v1/orgs/"+org+"/projects", f.admin, map[string]string{"name": fmt.Sprintf("artifact-project-%d", i)}, 201, &project); err != nil {
			return f, err
		}
		switch i {
		case 0:
			f.project = project.ID
		case 1:
			f.sibling = project.ID
		case 2:
			f.foreign = project.ID
		}
	}
	var env entity
	if err = t.expect("POST", f.projectPath()+"/environments", f.admin, map[string]string{"name": "fixture"}, 201, &env); err != nil {
		return f, err
	}
	f.environment = env.ID
	for i, entry := range []struct{ org, project string }{{f.org, f.sibling}, {f.otherOrg, f.foreign}} {
		base := "/api/v1/orgs/" + entry.org + "/projects/" + entry.project
		var protectedEnv entity
		if err = t.expect("POST", base+"/environments", f.admin, map[string]string{"name": "protected-fixture"}, 201, &protectedEnv); err != nil {
			return f, err
		}
		if i == 0 {
			f.siblingEnvironment = protectedEnv.ID
		} else {
			f.foreignEnvironment = protectedEnv.ID
		}
		if err = t.expect("POST", base+"/environments/"+protectedEnv.ID+"/parameters", f.admin, map[string]string{"name": "PRIVATE_PARAMETER", "action": "add", "pattern": f.metadata}, 204, nil); err != nil {
			return f, err
		}
		// Both probed objects really exist and disclose the marker to their
		// authorized administrator. Otherwise a typo could falsely prove isolation.
		control, e := t.request("GET", base+"/environments/"+protectedEnv.ID+"/parameters", f.admin, nil)
		if e != nil || control.status != 200 || !bytes.Contains(control.body, []byte(f.metadata)) {
			return f, errors.New("protected object authorized existence control failed")
		}
		if err = t.expect("POST", base+"/keys", f.admin, map[string]any{"name": "PRIVATE_DEFINITION", "classification": "config", "description": f.metadata, "declaration": map[string]any{"rule": map[string]string{"type": "string"}}}, 201, nil); err != nil {
			return f, err
		}
	}
	var versions []string
	for _, spec := range []struct{ name, class, value string }{{"ALLOWED_CONFIG", "config", f.config}, {"SECOND_CONFIG", "config", "second-config-value"}, {"PROTECTED_SECRET", "secret", f.secret}} {
		var key entity
		body := map[string]any{"name": spec.name, "classification": spec.class, "description": "seeded definition", "declaration": map[string]any{"rule": map[string]string{"type": "string"}}}
		if err = t.expect("POST", f.projectPath()+"/keys", f.admin, body, 201, &key); err != nil {
			return f, err
		}
		var pending struct {
			Version string `json:"version_id"`
		}
		if err = t.expect("PUT", f.envPath()+"/values/"+spec.name, f.admin, map[string]string{"value": spec.value}, 200, &pending); err != nil {
			return f, err
		}
		versions = append(versions, pending.Version)
	}
	if err = t.expect("POST", f.envPath()+"/publish", f.admin, map[string]any{"version_ids": versions}, 200, nil); err != nil {
		return f, err
	}
	if err = t.expect("POST", f.projectPath()+"/service-accounts", f.admin, map[string]string{"name": "artifact-reader", "kind": "automation"}, 201, &f.account); err != nil {
		return f, err
	}
	// Credential issuance while grantless has no disclosure reach. Widening
	// read afterward still uses the native disclosure reauthentication gate.
	if err = t.expect("POST", f.projectPath()+"/service-accounts/"+f.account.ID+"/credentials", f.admin, map[string]any{}, 200, &f.first); err != nil {
		return f, err
	}
	if err = t.expect("POST", f.projectPath()+"/service-accounts/"+f.account.ID+"/credentials", f.admin, map[string]any{}, 200, &f.second); err != nil {
		return f, err
	}
	if err = t.expect("POST", f.projectPath()+"/service-accounts/"+f.account.ID+"/credentials", f.admin, map[string]any{}, 200, &f.generated); err != nil {
		return f, err
	}
	if err = f.grant(t, true); err != nil {
		return f, err
	}
	return f, nil
}

func runBackend(ctx context.Context, o options, b *backendResult) (err error) {
	t, cleanup, err := newTarget(ctx, o, b.Engine)
	if err != nil {
		return err
	}
	defer func() {
		if e := cleanup(); e != nil {
			b.Cleanup = "failed"
			err = errors.Join(err, errors.New("isolated target cleanup failed"))
		} else {
			b.Cleanup = "complete"
		}
		existing := map[string]bool{}
		for _, c := range b.Cases {
			existing[c.ID] = true
		}
		for _, id := range mandatoryCases {
			if !existing[id] {
				b.Cases = append(b.Cases, caseResult{ID: id, Status: "skipped", Expected: "mandatory campaign case", Observed: "prior failure prevented execution"})
			}
		}
	}()
	var f fixture
	if err = record(b, "native-bootstrap", "native ceremony, API-issued human and machine credentials, required second factor", 6, func() error { var e error; f, e = setup(t); return e }); err != nil {
		return err
	}
	control := func(token string) error {
		r, e := t.request("GET", f.envPath()+"/delivery", token, nil)
		if e != nil {
			return e
		}
		if r.status != 200 || !bytes.Contains(r.body, []byte(f.config)) {
			return errors.New("authorized delivery control failed")
		}
		if e = noResponseCanary(r, []string{f.secret}); e != nil {
			return e
		}
		rpc, e := t.rpc(token, f.org, f.project, "")
		if e != nil {
			return e
		}
		if e := successfulRPC(rpc, "ALLOWED_CONFIG"); e != nil {
			return e
		}
		return noResponseCanary(rpc, []string{f.secret})
	}
	if err = record(b, "authorized-controls", "read-only credential receives config and MCP definitions; secret plaintext withheld", 5, func() error {
		if e := control(f.first.Value); e != nil {
			return e
		}
		r, e := t.inspect(f.first.Value, f.org, f.project, f.environment)
		if e != nil {
			return e
		}
		if e = successfulRPC(r, "ALLOWED_CONFIG"); e != nil {
			return e
		}
		return noResponseCanary(r, []string{f.secret})
	}); err != nil {
		return err
	}
	deniedProject := func(org, project, environment string) error {
		got, e := t.request("GET", "/api/v1/orgs/"+org+"/projects/"+project+"/environments/"+environment+"/parameters", f.first.Value, nil)
		if e != nil {
			return e
		}
		twin, e := t.request("GET", f.projectPath()+"/environments/"+missingEnvironment+"/parameters", f.admin, nil)
		if e != nil {
			return e
		}
		if e = sameDenial(got, twin, 404, append(f.forbidden(), f.config)); e != nil {
			return e
		}
		m, e := t.inspect(f.first.Value, org, project, environment)
		if e != nil {
			return e
		}
		missing, e := t.inspect(f.first.Value, f.org, f.project, missingEnvironment)
		if e != nil {
			return e
		}
		return sameDenial(m, missing, 200, append(f.forbidden(), f.config))
	}
	if err = record(b, "cross-project-denial", "sibling project indistinguishable from missing twin over REST and MCP", 4, func() error { return deniedProject(f.org, f.sibling, f.siblingEnvironment) }); err != nil {
		return err
	}
	if err = record(b, "cross-tenant-denial", "foreign tenant indistinguishable from missing twin over REST and MCP", 4, func() error { return deniedProject(f.otherOrg, f.foreign, f.foreignEnvironment) }); err != nil {
		return err
	}
	snapshot := func() ([]byte, error) {
		var all []byte
		for _, path := range []string{f.projectPath() + "/keys", f.envPath() + "/parameters", f.envPath() + "/values", f.envPath() + "/revisions/latest"} {
			r, e := t.request("GET", path, f.admin, nil)
			if e != nil {
				return nil, e
			}
			if r.status != 200 {
				return nil, errors.New("authorized persisted-state read failed")
			}
			all = append(all, r.body...)
		}
		return all, nil
	}
	before, err := snapshot()
	if err != nil {
		return err
	}
	mutation := func() error {
		body := map[string]string{"name": "HOSTILE_PARAMETER", "action": "add", "pattern": ".*"}
		got, e := t.request("POST", f.envPath()+"/parameters", f.first.Value, body)
		if e != nil {
			return e
		}
		missing, e := t.request("POST", f.projectPath()+"/environments/"+missingEnvironment+"/parameters", f.admin, body)
		if e != nil {
			return e
		}
		if e = sameDenial(got, missing, 404, append(f.forbidden(), f.config)); e != nil {
			return e
		}
		after, e := snapshot()
		if e != nil {
			return e
		}
		if !bytes.Equal(before, after) {
			return errors.New("denied mutation changed persisted domain state")
		}
		return nil
	}
	if err = record(b, "denied-mutation-unchanged", "denied write equals missing twin and admin domain snapshot is unchanged", 3, mutation); err != nil {
		return err
	}
	var delivery struct {
		Cursor string `json:"cursor"`
	}
	if err = t.expect("GET", f.envPath()+"/delivery", f.first.Value, nil, 200, &delivery); err != nil {
		return err
	}
	f.cursor = delivery.Cursor
	if f.cursor == "" {
		return errors.New("authorized delivery omitted required cursor")
	}
	page, err := t.rpc(f.first.Value, f.org, f.project, "")
	if err != nil {
		return err
	}
	var rpcPage struct {
		Result struct {
			Structured struct {
				Next string `json:"next_cursor"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if json.Unmarshal(page.body, &rpcPage) != nil || rpcPage.Result.Structured.Next == "" {
		return errors.New("authorized MCP page omitted cursor")
	}
	f.mcpCursor = rpcPage.Result.Structured.Next
	if err = record(b, "cursor-binding", "MCP cursor cannot cross projects; conditional delivery has current authorization", 3, func() error {
		if e := requireCurrentDelivery(t, f.envPath()+"/delivery?cursor="+url.QueryEscape(f.cursor), f.first.Value, append(f.forbidden(), f.config)); e != nil {
			return e
		}
		m, e := t.rpc(f.first.Value, f.org, f.sibling, f.mcpCursor)
		if e != nil {
			return e
		}
		missing, e := t.rpc(f.first.Value, f.org, missingProject, f.mcpCursor)
		if e != nil {
			return e
		}
		return sameDenial(m, missing, 200, append(f.forbidden(), f.config))
	}); err != nil {
		return err
	}
	if err = record(b, "projection-boundary", "config-only omits secret identity and switching projection invalidates current cursor", 3, func() error {
		var d struct {
			Current bool   `json:"current"`
			Cursor  string `json:"cursor"`
		}
		r, e := t.request("GET", f.envPath()+"/delivery?projection=config-only&cursor="+url.QueryEscape(f.cursor), f.first.Value, nil)
		if e != nil {
			return e
		}
		if r.status != 200 || json.Unmarshal(r.body, &d) != nil || d.Current || d.Cursor == f.cursor {
			return errors.New("projection switch did not produce a new authorized snapshot")
		}
		return noResponseCanary(r, []string{f.secret, "PROTECTED_SECRET"})
	}); err != nil {
		return err
	}
	if err = record(b, "revision-boundary", "read-only current revision metadata never contains secret plaintext", 2, func() error {
		r, e := t.request("GET", f.envPath()+"/revisions/latest", f.first.Value, nil)
		if e != nil {
			return e
		}
		if e = requireRevisionMetadata(r); e != nil {
			return e
		}
		return noResponseCanary(r, []string{f.secret, f.config})
	}); err != nil {
		return err
	}
	// Capture missing-object twins while the very same principal is demonstrably
	// authorized. Removing its grant afterward must not turn the comparison
	// into two unauthorized requests that agree for the wrong reason.
	missingDelivery, err := t.request("GET", f.projectPath()+"/environments/"+missingEnvironment+"/delivery", f.first.Value, nil)
	if err != nil {
		return err
	}
	if missingDelivery.status != 404 {
		return errors.New("authorized missing delivery twin did not return404")
	}
	missingConfiguration, err := t.inspect(f.first.Value, f.org, f.project, missingEnvironment)
	if err != nil {
		return err
	}
	missingDefinitions, err := t.rpc(f.first.Value, f.org, missingProject, "")
	if err != nil {
		return err
	}
	removed := func(token string) error {
		got, e := t.request("GET", f.envPath()+"/delivery?cursor="+url.QueryEscape(f.cursor), token, nil)
		if e != nil {
			return e
		}
		if e = sameDenial(got, missingDelivery, 404, append(f.forbidden(), f.config)); e != nil {
			return e
		}
		configuration, e := t.inspect(token, f.org, f.project, f.environment)
		if e != nil {
			return e
		}
		if e = sameDenial(configuration, missingConfiguration, 200, append(f.forbidden(), f.config)); e != nil {
			return e
		}
		rpc, e := t.rpc(token, f.org, f.project, f.mcpCursor)
		if e != nil {
			return e
		}
		return sameDenial(rpc, missingDefinitions, 200, append(f.forbidden(), f.config))
	}
	restart := func() error {
		if e := t.stop(); e != nil {
			return e
		}
		return t.start()
	}
	liveCredential := func(id string) error {
		r, e := t.request("GET", f.projectPath()+"/service-accounts/"+f.account.ID+"/credentials", f.admin, nil)
		if e != nil {
			return e
		}
		if e = requireLiveCredential(r, id, time.Now()); e != nil {
			return e
		}
		return noResponseCanary(r, []string{f.first.Value, f.second.Value, f.generated.Value, f.secret, f.password})
	}
	if err = record(b, "grant-removal", "committed grant removal survives restart: same live credential loses REST conditional and MCP cursor access before and after restart", 9, func() error {
		if e := f.grant(t, false); e != nil {
			return e
		}
		if e := removed(f.first.Value); e != nil {
			return e
		}
		if e := liveCredential(f.first.Credential.ID); e != nil {
			return e
		}
		if e := restart(); e != nil {
			return e
		}
		if e := removed(f.first.Value); e != nil {
			return e
		}
		return liveCredential(f.first.Credential.ID)
	}); err != nil {
		return err
	}
	if err = record(b, "grant-restoration", "same still-live credential works after grant restoration and after another restart", 5, func() error {
		if e := f.grant(t, true); e != nil {
			return e
		}
		if e := control(f.first.Value); e != nil {
			return e
		}
		if e := restart(); e != nil {
			return e
		}
		return control(f.first.Value)
	}); err != nil {
		return err
	}
	// The independent oracle has two booleans, both updated only by acknowledged
	// actions. It never imports production scope/capability/value predicates.
	live, read := true, true
	random := rand.New(rand.NewSource(o.seed))
	sequence := generate(random, o.steps)
	if err = record(b, "generated-sequence", "each seeded transition agrees with independent read/liveness model and unchanged domain state", o.steps*3, func() (runErr error) {
		defer func() {
			if runErr != nil {
				b.FailurePrefix = append([]string(nil), b.Sequence...)
			}
		}()
		for _, action := range sequence {
			b.Sequence = append(b.Sequence, action)
			switch action {
			case "remove-read":
				if e := f.grant(t, false); e != nil {
					return e
				}
				read = false
			case "restore-read":
				if e := f.grant(t, true); e != nil {
					return e
				}
				read = true
			case "restart":
				if e := t.stop(); e != nil {
					return e
				}
				if e := t.start(); e != nil {
					return e
				}
			case "revoke-generated":
				if e := t.expect("DELETE", f.projectPath()+"/service-accounts/"+f.account.ID+"/credentials/"+f.generated.Credential.ID, f.admin, nil, 204, nil); e != nil {
					return e
				}
				live = false
			case "probe":
			}
			var e error
			if live && read {
				e = control(f.generated.Value)
			} else if !live {
				for _, transport := range []string{"rest", "mcp"} {
					var got, twin response
					if transport == "rest" {
						got, e = t.request("GET", f.envPath()+"/delivery?cursor="+url.QueryEscape(f.cursor), f.generated.Value, nil)
						if e == nil {
							twin, e = t.request("GET", f.envPath()+"/delivery?cursor="+url.QueryEscape(f.cursor), "invalid-synthetic-token", nil)
						}
					} else {
						got, e = t.rpc(f.generated.Value, f.org, f.project, f.mcpCursor)
						if e == nil {
							twin, e = t.rpc("invalid-synthetic-token", f.org, f.project, f.mcpCursor)
						}
					}
					if e != nil {
						break
					}
					e = sameDenial(got, twin, 401, append(f.forbidden(), f.config))
					if e != nil {
						break
					}
				}
				if e == nil && read {
					e = control(f.second.Value)
				}
			} else {
				e = removed(f.generated.Value)
			}
			if e != nil {
				b.FailurePrefix = append([]string(nil), b.Sequence...)
				return e
			}
			after, e := snapshot()
			if e != nil {
				return e
			}
			if !bytes.Equal(before, after) {
				b.FailurePrefix = append([]string(nil), b.Sequence...)
				return errors.New("generated action changed protected domain state")
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if !read {
		if err = f.grant(t, true); err != nil {
			return err
		}
	}
	if err = record(b, "credential-revocation", "restored grant remains; revoked token equals invalid token while sibling credential works", 5, func() error {
		if e := control(f.first.Value); e != nil {
			return e
		}
		if e := t.expect("DELETE", f.projectPath()+"/service-accounts/"+f.account.ID+"/credentials/"+f.first.Credential.ID, f.admin, nil, 204, nil); e != nil {
			return e
		}
		for _, path := range []string{f.envPath() + "/delivery", "/mcp"} {
			var revoked, invalid response
			var e error
			if path == "/mcp" {
				revoked, e = t.rpc(f.first.Value, f.org, f.project, f.mcpCursor)
				if e == nil {
					invalid, e = t.rpc("invalid-synthetic-token", f.org, f.project, f.mcpCursor)
				}
			} else {
				revoked, e = t.request("GET", path, f.first.Value, nil)
				if e == nil {
					invalid, e = t.request("GET", path, "invalid-synthetic-token", nil)
				}
			}
			if e != nil {
				return e
			}
			if e = sameDenial(revoked, invalid, 401, append(f.forbidden(), f.config)); e != nil {
				return e
			}
		}
		return control(f.second.Value)
	}); err != nil {
		return err
	}
	if err = record(b, "restart-continuity", "restart preserves domain state and revocation; sibling token still authorized", 4, func() error {
		if e := t.stop(); e != nil {
			return e
		}
		if e := t.start(); e != nil {
			return e
		}
		if e := control(f.second.Value); e != nil {
			return e
		}
		got, e := t.request("GET", f.envPath()+"/delivery", f.first.Value, nil)
		if e != nil {
			return e
		}
		twin, e := t.request("GET", f.envPath()+"/delivery", "invalid-synthetic-token", nil)
		if e != nil {
			return e
		}
		if e = sameDenial(got, twin, 401, append(f.forbidden(), f.config)); e != nil {
			return e
		}
		after, e := snapshot()
		if e != nil {
			return e
		}
		if !bytes.Equal(before, after) {
			return errors.New("restart changed domain snapshot")
		}
		return nil
	}); err != nil {
		return err
	}
	return record(b, "audit-boundary", "at least one matching principal/project MCP key-list denial is durably audited without secret plaintext", 2, func() error {
		r, e := t.request("GET", "/api/v1/orgs/"+f.org+"/audit?limit=500", f.admin, nil)
		if e != nil {
			return e
		}
		if e = requireDenialAudit(r, f.account.Principal, f.org, f.project); e != nil {
			return e
		}
		return noResponseCanary(r, []string{f.secret, f.config, f.first.Value, f.second.Value, f.generated.Value, f.password})
	})
}

// The fixed prefix retains grant removal/restoration regression coverage.
// Seeded middles vary grant state and place at most two additional restarts;
// the fixed tail always revokes the generated credential before restarting.
func generate(r *rand.Rand, n int) []string {
	out := []string{"remove-read", "probe", "restore-read", "probe"}
	actions := []string{"remove-read", "restore-read", "probe", "restart"}
	restarts := 0
	for len(out) < n-4 {
		action := actions[r.Intn(len(actions))]
		if action == "restart" {
			if restarts == 2 {
				action = "probe"
			} else {
				restarts++
			}
		}
		out = append(out, action)
	}
	return append(out, "revoke-generated", "restore-read", "restart", "probe")
}

// Credential metadata is an independent native positive control: a grantless
// denial must not pass because the fixture credential was revoked or expired.
func requireLiveCredential(r response, id string, now time.Time) error {
	var credentials struct {
		Items []struct {
			ID        string     `json:"id"`
			RevokedAt *time.Time `json:"revoked_at"`
			ExpiresAt *time.Time `json:"expires_at"`
		} `json:"items"`
	}
	if r.status != 200 || id == "" || json.Unmarshal(r.body, &credentials) != nil {
		return errors.New("native live credential metadata control failed")
	}
	for _, c := range credentials.Items {
		if c.ID == id && c.RevokedAt == nil && (c.ExpiresAt == nil || c.ExpiresAt.After(now)) {
			return nil
		}
	}
	return errors.New("native live credential metadata missing, revoked or expired")
}
