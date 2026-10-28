package isolation

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm/awssmtest"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/webauthntest"
)

const awsSurfacePassword = "correct horse battery staple aws surface"
const awsSurfaceSecret = "aws-surface-synthetic-writer-secret"
const awsSurfaceReplacement = "aws-surface-synthetic-replacement-secret"

// This joins the actual TLS router, cookie/CSRF authentication, CLI dispatcher,
// pinned trust store, bearer custody, browser handoff, passkey validation,
// purpose-bound consent, SQL store and worker. Only AWS is emulated. The
// authenticator is the existing software test device, not a rendered browser;
// this fixture does not claim desktop/mobile UI or real IAM/STS acceptance.
type awsAuthenticatedSurface struct {
	t *testing.T
	db *store.DB
	auth *service.Auth
	svc *service.Adapters
	remote *awssmtest.Server
	front *httptest.Server
	origin string
	browser *http.Client
	device *webauthntest.Device
	state *cli.State
	env cli.Env
	worker *adapter.Worker
	clock time.Time
	forbidden []string
	wantOperation string
	opened int
	lastStderr string
}

// A localhost certificate keeps the transport origin, WebAuthn RP and signed
// authenticator origin identical. No certificate or hostname check is bypassed.
func awsSurfaceTLS(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil { t.Fatal(err) }
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{"localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil { t.Fatal(err) }
	front := httptest.NewUnstartedServer(handler)
	front.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	front.StartTLS()
	t.Cleanup(front.Close)
	return front
}

func newAWSAuthenticatedSurface(t *testing.T, db *store.DB) *awsAuthenticatedSurface {
	t.Helper()
	h := &awsAuthenticatedSurface{t: t, db: db, clock: time.Now().UTC(), forbidden: []string{awsSurfacePassword, awsSurfaceSecret, awsSurfaceReplacement, "AKIAHIKYOE2E0000001", "synthetic external value", awsSecretV1, awsSecretV2}}
	kr := probeKeyring(t, db)
	h.auth = authServiceForKeyring(t, db, kr)
	h.auth.ReauthWindow = 0
	h.auth.Now = func() time.Time { return h.clock }
	h.remote = awssmtest.New(awsAccount, awsRegion)
	t.Cleanup(h.remote.Close)
	roots := x509.NewCertPool()
	roots.AddCert(h.remote.Certificate())
	build := func(config adapter.Config, credential string) (*adapter.ModuleLease, error) {
		client, err := awssm.NewClient(awssm.ClientConfig{
			Origin: config.Origin, Credential: credential, Deadline: 5 * time.Second,
			AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
			STSAllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, RootCAs: roots,
		})
		if err != nil {
			// The production app factory is package-private. Match its
			// awsConstructionError classification while injecting only the
			// emulator trust roots and loopback egress above. No successful
			// provider operation, auth decision or worker result is fabricated.
			var configError *awssm.ConfigError
			if errors.As(err, &configError) {
				if errors.Is(err, awssm.ErrWorkloadIdentityDisabled) { return nil, errors.Join(domain.ErrInvalid, adapter.ErrProviderAuth, err) }
				return nil, errors.Join(domain.ErrInvalid, err)
			}
			return nil, err
		}
		return adapter.NewModuleLease(&awssm.Module{API: client}, client.Forget)
	}
	h.svc = &service.Adapters{DB: db, Auth: h.auth, Keyring: kr, Now: func() time.Time { return h.clock }, ModuleFactory: func(provider adapter.Provider, config adapter.Config, credential string) (*adapter.ModuleLease, error) {
		if provider != adapter.AWSSecretsManagerProvider { return nil, fmt.Errorf("unexpected surface provider %s", provider) }
		return build(config, credential)
	}}
	h.front = awsSurfaceTLS(t, server.New(&server.API{Auth: h.auth, Adapters: h.svc}, nil))
	parsed, err := url.Parse(h.front.URL)
	if err != nil { t.Fatal(err) }
	parsed.Host = "localhost:" + parsed.Port()
	h.origin = parsed.String()
	h.auth.ExternalOrigin = h.origin
	if err := h.auth.ConfigureWebAuthnRP(); err != nil { t.Fatal(err) }
	administrator := bootstrapAdmin(t, db, adminOpts{username: "aws-surface", displayName: "AWS Surface", password: awsSurfacePassword, auth: h.auth, login: true})
	h.forbidden = append(h.forbidden, administrator.token, administrator.boot.Authority)
	for _, capability := range []string{"read", "manage-adapters", "reveal"} {
		id := "aws_surface_" + capability
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('`+id+`','`+string(administrator.boot.PrincipalID)+`','`+capability+`','org_a','prj_a1',NULL,`+ts+`)`)
		execRaw(t, db, `INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('origin_`+id+`','`+id+`','manual','`+string(administrator.boot.PrincipalID)+`',`+ts+`)`)
	}
	h.device = webauthntest.New("localhost", h.origin)
	_ = enrolPasskey(t, h.auth, t.Context(), administrator.token, awsSurfacePassword, h.device)
	browser := *h.front.Client()
	h.browser = &browser
	h.browser.Jar, err = cookiejar.New(nil)
	if err != nil { t.Fatal(err) }
	options := h.request(h.browser, "", http.MethodPost, "/auth/webauthn/login/start", nil, http.StatusOK)
	assertion, err := h.device.Assert(options)
	if err != nil { t.Fatal(err) }
	h.request(h.browser, "", http.MethodPost, "/auth/webauthn/login/finish", json.RawMessage(assertion), http.StatusOK)
	if h.cookie("__Host-hikyo") == "" || h.cookie("__Host-hikyo-csrf") == "" { t.Fatal("real passkey login did not deliver browser cookies") }
	h.forbidden = append(h.forbidden, h.cookie("__Host-hikyo"), h.cookie("__Host-hikyo-csrf"))
	stateDir := t.TempDir()
	h.env = cli.Env{Getenv: func(name string) string { if name == "HIKYO_STATE_DIR" { return stateDir }; return "" }}
	h.state, err = cli.NewState(h.env)
	if err != nil { t.Fatal(err) }
	if err := h.state.Trust().Put(cli.TrustEntry{Name: "aws-surface", Origin: h.origin, SPKIPin: cli.SPKIFingerprint(h.front.Certificate())}); err != nil { t.Fatal(err) }
	h.command(cli.ExitOK, "", "", "login", h.origin, "--local", "--as", "aws-surface", "--name", "aws-surface")
	identity, err := h.auth.Identity(t.Context(), h.artifact().Token)
	if err != nil || identity.Artifact != service.ArtifactCLI || identity.Principal != administrator.boot.PrincipalID { t.Fatal("CLI did not establish its own real human session") }
	if h.artifact().Token == h.cookie("__Host-hikyo") { t.Fatal("CLI reused browser credential") }
	runtime := store.NewAdapterRuntime(db, func(ctx context.Context, job adapter.Job, _ adapter.Effect) error {
		return tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID(job.AuthorityPrincipal), Class: domain.ClassHuman}, authz.OpAdapterPush, scopeEnv(domain.OrgID(job.OrgID), domain.ProjectID(job.ProjectID), domain.EnvID(job.EnvironmentID)))
			return err
		})
	})
	h.worker = &adapter.Worker{Store: runtime, Loader: awsRealLoader{awsTestLoader{runtime: runtime, keyring: kr, build: build}}, ID: "aws-authenticated-surface", Now: func() time.Time { return h.clock }, Jitter: func(d time.Duration) time.Duration { return d }, Wait: func(context.Context, time.Duration) error { return nil }}
	return h
}

func (h *awsAuthenticatedSurface) cookie(name string) string {
	u, err := url.Parse(h.origin)
	if err != nil { h.t.Fatal(err) }
	for _, cookie := range h.browser.Jar.Cookies(u) { if cookie.Name == name { return cookie.Value } }
	return ""
}

func (h *awsAuthenticatedSurface) request(client *http.Client, bearer, method, path string, input any, status int) []byte {
	h.t.Helper()
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil { h.t.Fatal(err) }
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(h.t.Context(), method, h.origin+api.PathPrefix+path, body)
	if err != nil { h.t.Fatal(err) }
	if input != nil { req.Header.Set("Content-Type", "application/json") }
	if bearer != "" { req.Header.Set("Authorization", "Bearer "+bearer) }
	if client == h.browser { req.Header.Set("X-Hikyo-CSRF", h.cookie("__Host-hikyo-csrf")) }
	response, err := client.Do(req)
	if err != nil { h.t.Fatal(err) }
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil { h.t.Fatal(err) }
	if response.StatusCode != status { h.t.Fatalf("%s %s: status %d, want %d", method, path, response.StatusCode, status) }
	if err := api.ValidateResponse(req, response.StatusCode, response.Header, raw); err != nil { h.t.Fatalf("%s response contract: %v", path, err) }
	if strings.Contains(path, "/adapters") || strings.Contains(path, "/adapter-targets") { h.noSecrets(raw) }
	return raw
}

func (h *awsAuthenticatedSurface) noSecrets(raw []byte) {
	h.t.Helper()
	for _, secret := range h.forbidden { if secret != "" && bytes.Contains(raw, []byte(secret)) { h.t.Fatal("surface or audit disclosed synthetic secret material") } }
	for _, field := range []string{"secret_access_key", "credential_ciphertext"} { if bytes.Contains(raw, []byte(field)) { h.t.Fatal("surface or audit exposed credential fields") } }
}

func (h *awsAuthenticatedSurface) artifact() cli.SessionArtifact {
	h.t.Helper()
	sessions, err := h.state.Sessions()
	if err != nil { h.t.Fatal(err) }
	artifact, ok := sessions["aws-surface"]
	if !ok || artifact.Token == "" { h.t.Fatal("CLI custody missing real session") }
	return artifact
}

func (h *awsAuthenticatedSurface) reauth(operation string) {
	h.t.Helper()
	before := h.cookie("__Host-hikyo")
	options := h.request(h.browser, "", http.MethodPost, "/auth/reauth/passkey/start", map[string]any{
		"operation": "adapter", "adapter_operation": operation, "environment_id": string(envA1), "environment_ids": []string{string(envA1)}, "key_ids": []string{},
	}, http.StatusOK)
	assertion, err := h.device.Assert(options)
	if err != nil { h.t.Fatal(err) }
	raw := h.request(h.browser, "", http.MethodPost, "/auth/reauth/passkey/finish", json.RawMessage(assertion), http.StatusOK)
	var result apigen.ReauthResult
	if err := json.Unmarshal(raw, &result); err != nil { h.t.Fatal(err) }
	if !result.SingleDecision || result.SessionToken != nil || h.cookie("__Host-hikyo") == before { h.t.Fatal("passkey did not open single-decision consent and rotate on the cookie channel") }
	h.forbidden = append(h.forbidden, before, h.cookie("__Host-hikyo"), h.cookie("__Host-hikyo-csrf"))
}

func (h *awsAuthenticatedSurface) openURL(target string) error {
	h.t.Helper()
	u, err := url.Parse(target)
	if err != nil { return err }
	if u.Scheme+"://"+u.Host != h.origin || u.Path != "/reauth/cli" || h.wantOperation == "" || len(u.Query()) != 1 || u.Query().Get("transaction") == "" { return fmt.Errorf("unexpected CLI browser continuation") }
	var transaction apigen.CLIReauthTransaction
	raw := h.request(h.browser, "", http.MethodGet, "/auth/cli-reauth/transactions/"+url.PathEscape(u.Query().Get("transaction")), nil, http.StatusOK)
	if err := json.Unmarshal(raw, &transaction); err != nil { return err }
	if string(transaction.Purpose) != "adapter" || string(transaction.Operation) != h.wantOperation || len(transaction.Environments) != 1 || string(transaction.Environments[0].EnvironmentId) != string(envA1) || len(transaction.KeyIds) != 0 { return fmt.Errorf("CLI handoff lost its exact operation or environment binding") }
	h.reauth(h.wantOperation)
	var approved apigen.CLIReauthApproved
	raw = h.request(h.browser, "", http.MethodPost, "/auth/cli-reauth/approve", apigen.CLIReauthApproveRequest{State: transaction.State}, http.StatusOK)
	if err := json.Unmarshal(raw, &approved); err != nil { return err }
	if approved.State != transaction.State || approved.RedirectUri != transaction.RedirectUri || approved.Code == "" { return fmt.Errorf("approval changed the exact CLI handoff") }
	callback, err := url.Parse(approved.RedirectUri)
	if err != nil { return err }
	if callback.Scheme != "http" || callback.Hostname() != "127.0.0.1" || callback.Path != "/callback" || callback.Port() == "" { return fmt.Errorf("invalid loopback callback") }
	callback.RawQuery = url.Values{"code": {approved.Code}, "state": {approved.State}}.Encode()
	response, err := http.DefaultClient.Get(callback.String())
	if err != nil { return err }
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK { return fmt.Errorf("CLI loopback refused real approval") }
	h.forbidden = append(h.forbidden, approved.Code, approved.State)
	h.opened++
	return nil
}

func (h *awsAuthenticatedSurface) command(want int, operation, input string, args ...string) []byte {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	var before string
	if operation != "" { before = h.artifact().Token }
	h.wantOperation = operation
	opened := h.opened
	ios := cli.IO{Env: h.env, Workdir: h.t.TempDir(), Stdin: strings.NewReader(input), Stdout: &stdout, Stderr: &stderr, OpenURL: h.openURL, ReadPassword: func(string) (string, error) { return awsSurfacePassword, nil }}
	if args[0] != "login" { args = append(args, "--instance", "aws-surface", "--org", string(orgA), "--project", string(prjA1)) }
	exit := cli.Run(h.t.Context(), ios, args)
	if operation != "" {
		after := h.artifact().Token
		if h.opened != opened+1 || after == before { h.t.Fatal("CLI skipped real browser handoff or did not persist the rotated bearer") }
		if _, err := h.auth.Identity(h.t.Context(), before); err == nil { h.t.Fatal("CLI reauthentication left its old bearer live") }
		h.forbidden = append(h.forbidden, before, after)
	} else if args[0] == "login" && exit == cli.ExitOK { h.forbidden = append(h.forbidden, h.artifact().Token) }
	h.noSecrets(stdout.Bytes())
	h.noSecrets(stderr.Bytes())
	h.lastStderr = stderr.String()
	if want != cli.ExitOK && stdout.Len() != 0 { h.t.Fatal("refused CLI mutation rendered a success payload") }
	if exit != want { h.t.Fatalf("CLI %s exit %d, want %d", strings.Join(args[:2], " "), exit, want) }
	h.wantOperation = ""
	return bytes.Clone(stdout.Bytes())
}

func (h *awsAuthenticatedSurface) drain() {
	h.t.Helper()
	for range 24 {
		h.clock = h.clock.Add(time.Second)
		worked, err := h.worker.RunOnce(h.t.Context())
		if err != nil { h.t.Fatal(err) }
		if !worked { return }
	}
	h.t.Fatal("authenticated AWS fixture outbox did not drain")
}

func (h *awsAuthenticatedSurface) publish(value string) {
	h.t.Helper()
	publishValue(h.t, &service.Values{DB: h.db, Keyring: h.svc.Keyring}, service.LocalPrincipal(custodian), scopeEnv(orgA, prjA1, envA1), "SHARED_KEY", value)
}

func (h *awsAuthenticatedSurface) inspect(target string) apigen.AdapterTargetDetail {
	h.t.Helper()
	var detail apigen.AdapterTargetDetail
	raw := h.request(h.browser, "", http.MethodGet, "/orgs/org_a/projects/prj_a1/adapter-targets/"+target, nil, http.StatusOK)
	if err := json.Unmarshal(raw, &detail); err != nil { h.t.Fatal(err) }
	return detail
}

func (h *awsAuthenticatedSurface) delivered(name, kind, want string) {
	h.t.Helper()
	value, ok := h.remote.Value(name)
	if !ok { h.t.Fatal("no AWS value was delivered") }
	if kind == "json-object" {
		expected, err := json.Marshal(map[string]string{"SHARED_KEY": want})
		if err != nil { h.t.Fatal(err) }
		if value != string(expected) { h.t.Fatal("JSON delivery lost deterministic byte-exact source value") }
	} else if value != want { h.t.Fatal("per-key delivery lost byte-exact source value") }
}

func TestAWSAuthenticatedAPIAndCLILifecycle(t *testing.T) {
	for _, surface := range []string{"api", "cli"} {
		for _, kind := range []string{"json-object", "per-key"} {
			t.Run(surface+"/"+kind, func(t *testing.T) {
				forEngines(t, func(t *testing.T, db *store.DB) {
					h := newAWSAuthenticatedSurface(t, db)
					h.publish(awsSecretV1)
					base := "/orgs/org_a/projects/prj_a1"
					name, remoteName := "prod/surface", "prod/surface"
					if kind == "per-key" { name, remoteName = "prod/surface/", "prod/surface/SHARED_KEY" }
					h.remote.Seed(remoteName, "synthetic external value", nil)
					descriptor := `{"mode":"static","region":"`+awsRegion+`","access_key_id":"AKIAHIKYOE2E0000001","secret_access_key":"`+awsSurfaceSecret+`"}`
					input := apigen.CreateAdapterRequest{Provider: "aws-secrets-manager", Origin: h.remote.URL, Credential: descriptor, Target: apigen.AdapterTargetInput{EnvironmentId: apigen.ID(envA1), DestinationKind: apigen.AdapterDestinationKind(kind), DestinationOwner: awsAccount, DestinationName: name, KeyIds: []apigen.ID{apigen.ID(keyA1)}, SelectedRepositoryIds: []int64{}}}
					before := len(h.remote.Operations())
					h.request(h.front.Client(), "", http.MethodPost, base+"/adapters", input, http.StatusUnauthorized)
					withoutCSRF := *h.browser
					h.request(&withoutCSRF, "", http.MethodPost, base+"/adapters", input, http.StatusUnauthorized)
					h.request(h.browser, "", http.MethodPost, base+"/adapters", input, http.StatusConflict)
					if len(h.remote.Operations()) != before || queryInt(t, db, "SELECT COUNT(*) FROM adapters") != 0 { t.Fatal("missing authentication or consent reached AWS or persisted configuration") }
					var configured apigen.Adapter
					if surface == "cli" {
						raw := h.command(cli.ExitOK, "adapter.configure", awsSurfaceSecret+"\n", "adapter", "create", "--provider", "aws-secrets-manager", "--origin", h.remote.URL, "--env", string(envA1), "--kind", kind, "--owner", awsAccount, "--secret", name, "--keys", keyA1, "--aws-auth", "static", "--aws-region", awsRegion, "--aws-access-key-id", "AKIAHIKYOE2E0000001", "--stdin", "-o", "json")
						var list apigen.AdapterList
						if err := json.Unmarshal(raw, &list); err != nil || len(list.Items) != 1 { t.Fatal("CLI configuration did not render the actual API adapter") }
						configured = list.Items[0]
					} else {
						h.reauth("adapter.configure")
						raw := h.request(h.browser, "", http.MethodPost, base+"/adapters", input, http.StatusCreated)
						if err := json.Unmarshal(raw, &configured); err != nil { t.Fatal(err) }
					}
					if configured.Provider != "aws-secrets-manager" || !configured.CredentialPresent || len(configured.Targets) != 1 { t.Fatal("configured AWS adapter lost provider, custody or target") }
					adapterID, targetID := string(configured.Id), string(configured.Targets[0].Id)
					targetPath := base+"/adapter-targets/"+targetID
					credentialPath := base+"/adapters/"+adapterID+"/credential"
					// The successful create spent its configure decision. Neither an
					// API caller nor the CLI's stored session inherits another one.
					h.request(h.browser, "", http.MethodPost, base+"/adapters", input, http.StatusConflict)
					var plan apigen.AdapterPlan
					var raw []byte
					if surface == "cli" { raw = h.command(cli.ExitOK, "", "", "adapter", "plan", "--target", targetID, "-o", "json") } else { raw = h.request(h.browser, "", http.MethodPost, targetPath+"/plan", nil, http.StatusOK) }
					if err := json.Unmarshal(raw, &plan); err != nil { t.Fatal(err) }
					if plan.ArtifactId == "" || len(plan.Changes) != 1 || plan.Changes[0].EffectiveName != remoteName || string(plan.Changes[0].Disposition) != "conflict" { t.Fatal("plan did not persist the exact external-name conflict") }
					detail := h.inspect(targetID)
					if len(detail.Conflicts) != 1 || detail.Conflicts[0].Id != plan.ArtifactId { t.Fatal("plan artifact did not survive real API inspection") }
					artifact := detail.Conflicts[0]
					adoption := apigen.AdapterAdoptionRequest{ArtifactId: artifact.Id, TargetGeneration: artifact.TargetGeneration, DestinationId: artifact.DestinationId, RepositoryId: artifact.RepositoryId, Entries: artifact.Entries}
					h.request(h.browser, "", http.MethodPost, targetPath+"/adoptions", adoption, http.StatusConflict)
					if surface == "cli" { h.command(cli.ExitOK, "adapter.adopt", "", "adapter", "adopt", "--target", targetID, "--artifact", string(artifact.Id), remoteName) } else { h.reauth("adapter.adopt"); h.request(h.browser, "", http.MethodPost, targetPath+"/adoptions", adoption, http.StatusAccepted) }
					h.drain()
					h.delivered(remoteName, kind, awsSecretV1)
					if h.remote.Tag(remoteName, adapter.SentinelName) != targetID { t.Fatal("adoption did not claim the inspected resource") }
					// A credential replacement must be a new consent operation,
					// never a reuse of the adoption or configure window.
					replacement := strings.ReplaceAll(descriptor, awsSurfaceSecret, awsSurfaceReplacement)
					h.request(h.browser, "", http.MethodPut, credentialPath, apigen.SetAdapterCredentialRequest{Credential: replacement}, http.StatusConflict)
					if surface == "cli" { h.command(cli.ExitOK, "adapter.credential-set", awsSurfaceReplacement+"\n", "adapter", "credential", "set", "--adapter", adapterID, "--aws-auth", "static", "--aws-region", awsRegion, "--aws-access-key-id", "AKIAHIKYOE2E0000001", "--stdin") } else { h.reauth("adapter.credential-set"); h.request(h.browser, "", http.MethodPut, credentialPath, apigen.SetAdapterCredentialRequest{Credential: replacement}, http.StatusNoContent) }
					view, err := h.svc.Get(t.Context(), service.Bearer(h.artifact().Token), scopeProject(orgA, prjA1), adapterID)
					if err != nil { t.Fatal(err) }
					sealer, err := h.svc.Keyring.ForProject(t.Context(), string(orgA), string(prjA1))
					if err != nil { t.Fatal(err) }
					plain, err := sealer.OpenField(adapter.CredentialAAD(string(orgA), string(prjA1), adapterID), view.Adapter.CredentialCiphertext)
					if err != nil { t.Fatal(err) }
					stored, parseErr := awssm.ParseDescriptor(string(plain))
					crypto.Zero(plain)
					if parseErr != nil || stored.SecretKey != awsSurfaceReplacement { t.Fatal("replacement descriptor was not sealed in actual credential custody") }
					// Workload descriptors may only address AWS-owned endpoints.
					// Use that real route for the disabled opt-in refusal: its
					// constructor must stop before credentials, DNS or AWS I/O.
					// Reusing the emulator origin would prove a different refusal.
					for _, mode := range []string{"ambient", "assume-role", "web-identity"} {
						before = len(h.remote.Operations())
						blocked := input
						blocked.Origin = "https://secretsmanager.eu-west-1.amazonaws.com"
						blocked.Target.DestinationName = "prod/disabled-"+mode
						if kind == "per-key" { blocked.Target.DestinationName += "/" }
						d := awssm.Descriptor{Mode: awssm.AuthMode(mode), Region: awsRegion}
						if mode != "ambient" { d.RoleARN, d.SessionSeconds = "arn:aws:iam::123456789012:role/hikyo", 900 }
						descriptorBytes, err := json.Marshal(d)
						if err != nil { t.Fatal(err) }
						blocked.Credential = string(descriptorBytes)
						if surface == "cli" {
							args := []string{"adapter", "create", "--provider", "aws-secrets-manager", "--origin", blocked.Origin, "--env", string(envA1), "--kind", kind, "--owner", awsAccount, "--secret", blocked.Target.DestinationName, "--keys", keyA1, "--aws-auth", mode, "--aws-region", awsRegion}
							if mode != "ambient" { args = append(args, "--aws-role-arn", d.RoleARN, "--aws-session-seconds", "900") }
							h.command(cli.ExitRefused, "adapter.configure", "", args...)
							if !strings.Contains(h.lastStderr, "server workload identity is disabled") { t.Fatal("CLI lost the actual workload opt-in refusal detail") }
						} else {
							h.reauth("adapter.configure")
							raw = h.request(h.browser, "", http.MethodPost, base+"/adapters", blocked, http.StatusBadRequest)
							if !bytes.Contains(raw, []byte("server workload identity is disabled")) { t.Fatal("API lost the actual workload opt-in refusal detail") }
						}
						if len(h.remote.Operations()) != before || queryInt(t, db, "SELECT COUNT(*) FROM adapters") != 1 { t.Fatal("disabled workload configuration contacted the provider or persisted an adapter") }
					}
					// The refused configurations cannot poison existing custody.
					// This is fresh static-client recovery, not an STS refresh claim.
					if surface == "cli" { h.command(cli.ExitOK, "", "", "adapter", "test", "--target", targetID) } else { h.request(h.browser, "", http.MethodPost, targetPath+"/test", nil, http.StatusOK) }
					h.publish(awsSecretV2)
					// Revoke while work is queued. No provider call may spend the
					// retired descriptor, even though its module was once usable.
					before = len(h.remote.Operations())
					generation := h.inspect(targetID).Target.Generation
					queuedID := queryString(t, db, `SELECT active_job_id FROM adapter_targets WHERE id='`+targetID+`'`)
					if queuedID == "" || queryString(t, db, `SELECT state FROM adapter_outbox WHERE id='`+queuedID+`'`) != "queued" { t.Fatal("revocation fixture did not actually queue work") }
					if surface == "cli" { h.command(cli.ExitOK, "", "", "adapter", "credential", "revoke", "--adapter", adapterID) } else { h.request(h.browser, "", http.MethodDelete, credentialPath, nil, http.StatusNoContent) }
					h.drain()
					if len(h.remote.Operations()) != before { t.Fatal("worker used credentials after real API/CLI revocation") }
					if queryInt(t, db, `SELECT COUNT(*) FROM adapters WHERE id='`+adapterID+`' AND credential_ciphertext IS NULL AND credential_set_at IS NULL AND credential_expires_at IS NULL`) != 1 { t.Fatal("credential revoke did not destroy stored custody") }
					if queryString(t, db, `SELECT state FROM adapter_outbox WHERE id='`+queuedID+`'`) != "superseded" || queryInt(t, db, `SELECT COUNT(*) FROM adapter_targets WHERE id='`+targetID+`' AND active_job_id IS NULL`) != 1 { t.Fatal("credential revoke did not retire the exact queued job") }
					if h.inspect(targetID).Target.Generation <= generation { t.Fatal("credential revoke did not fence the queued target generation") }
					h.delivered(remoteName, kind, awsSecretV1)
					// Recovery requires explicit fresh custody and a fresh sync
					// ceremony; it cannot resurrect the retired job or old bearer.
					if surface == "cli" {
						h.command(cli.ExitOK, "adapter.credential-set", awsSurfaceReplacement+"\n", "adapter", "credential", "set", "--adapter", adapterID, "--aws-auth", "static", "--aws-region", awsRegion, "--aws-access-key-id", "AKIAHIKYOE2E0000001", "--stdin")
						h.command(cli.ExitOK, "adapter.sync", "", "adapter", "sync", "--target", targetID)
					} else {
						h.reauth("adapter.credential-set"); h.request(h.browser, "", http.MethodPut, credentialPath, apigen.SetAdapterCredentialRequest{Credential: replacement}, http.StatusNoContent)
						h.reauth("adapter.sync"); h.request(h.browser, "", http.MethodPost, targetPath+"/sync", nil, http.StatusAccepted)
					}
					h.drain()
					h.delivered(remoteName, kind, awsSecretV2)
					if string(h.inspect(targetID).Target.SyncStatus) != string(adapter.HealthConverged) { t.Fatal("fresh static credential did not recover the target") }
					// Pause is a narrowing operation. Resume must acquire fresh
					// adapter.sync consent and catch up to the published revision.
					if surface == "cli" { h.command(cli.ExitOK, "", "", "adapter", "target", "pause", targetID) } else { h.request(h.browser, "", http.MethodPost, targetPath+"/pause", nil, http.StatusOK) }
					if h.inspect(targetID).Target.PausedAt == nil { t.Fatal("pause was not persisted") }
					before = len(h.remote.Operations())
					h.publish(awsSecretV1)
					h.drain()
					if len(h.remote.Operations()) != before { t.Fatal("paused target sent provider requests") }
					h.delivered(remoteName, kind, awsSecretV2)
					if surface == "cli" { h.command(cli.ExitOK, "adapter.sync", "", "adapter", "target", "resume", targetID) } else { h.reauth("adapter.sync"); h.request(h.browser, "", http.MethodPost, targetPath+"/resume", nil, http.StatusAccepted) }
					h.drain()
					h.delivered(remoteName, kind, awsSecretV1)
					if h.inspect(targetID).Target.PausedAt != nil { t.Fatal("resume did not clear persisted pause") }
					// Exercise both retention choices using the existing resources:
					// JSON moves retain the old name and prune on final teardown;
					// per-key moves prune the old name and retain on teardown.
					keepMove := kind == "json-object"
					movedName, movedRemote := "prod/surface-moved", "prod/surface-moved"
					if kind == "per-key" { movedName, movedRemote = "prod/surface-moved/", "prod/surface-moved/SHARED_KEY" }
					current := h.inspect(targetID).Target
					var move apigen.AdapterMove
					if surface == "cli" {
						args := []string{"adapter", "update", adapterID, "--target", targetID, "--env", string(envA1), "--kind", kind, "--owner", awsAccount, "--secret", movedName, "--keys", keyA1, "-o", "json"}
						if keepMove { args = append(args, "--keep-remote") }
						raw = h.command(cli.ExitOK, "adapter.configure", "", args...)
					} else {
						h.reauth("adapter.configure")
						raw = h.request(h.browser, "", http.MethodPatch, targetPath, apigen.UpdateAdapterTargetRequest{EnvironmentId: apigen.ID(envA1), DestinationKind: apigen.AdapterDestinationKind(kind), DestinationOwner: awsAccount, DestinationName: movedName, KeyIds: []apigen.ID{apigen.ID(keyA1)}, SelectedRepositoryIds: []int64{}, ExpectedGeneration: current.Generation, KeepRemote: &keepMove}, http.StatusAccepted)
					}
					// Retain skips destructive scrub but still activates asynchronously.
					// Prune must scrub before the same delayed route activation.
					wantMoveState := "scrubbing"
					if keepMove { wantMoveState = "activating" }
					if err := json.Unmarshal(raw, &move); err != nil || move.Id == "" || string(move.State) != wantMoveState { t.Fatal("destination change did not return its exact retain/prune move state") }
					if h.inspect(targetID).Target.DestinationName != name { t.Fatal("destination activated before its durable worker transition") }
					h.drain()
					activated := h.inspect(targetID).Target
					if activated.DestinationName != movedName || activated.Generation <= current.Generation || string(activated.SyncStatus) != string(adapter.HealthConverged) { t.Fatal("destination move did not activate and converge its new generation") }
					h.delivered(movedRemote, kind, awsSecretV1)
					if h.remote.Deleted(remoteName) == keepMove { t.Fatal("destination move lost its retain/prune choice") }
					if keepMove { h.delivered(remoteName, kind, awsSecretV1) }
					if queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='`+targetID+`' AND effective_name='`+remoteName+`' AND state<>'released'`) != 0 { t.Fatal("destination move retained old-route custody") }
					keepFinal := !keepMove
					before = len(h.remote.Operations())
					if surface == "cli" {
						args := []string{"adapter", "delete", adapterID}
						if keepFinal { args = append(args, "--keep-remote") }
						h.command(cli.ExitOK, "", "", args...)
					} else {
						path := base+"/adapters/"+adapterID
						if keepFinal { path += "?keep_remote=true" }
						h.request(h.browser, "", http.MethodDelete, path, nil, http.StatusOK)
					}
					h.drain()
					if h.remote.Deleted(movedRemote) == keepFinal { t.Fatal("teardown lost its retain/prune choice") }
					if keepFinal {
						h.delivered(movedRemote, kind, awsSecretV1)
						if len(h.remote.Operations()) != before { t.Fatal("retained teardown made a remote request") }
					}
					if queryInt(t, db, `SELECT COUNT(*) FROM adapter_ledger WHERE target_id='`+targetID+`' AND state<>'released'`) != 0 || queryInt(t, db, `SELECT COUNT(*) FROM adapters WHERE id='`+adapterID+`' AND credential_ciphertext IS NULL`) != 1 { t.Fatal("teardown retained credential or remote custody") }
					// These assertions inspect both trails without printing tokens
					// or values on failure. Provider reads remain metadata-only.
					h.noSecrets([]byte(queryStrings(t, db, "SELECT payload FROM audit_tenant_events")))
					h.noSecrets([]byte(queryStrings(t, db, "SELECT payload FROM audit_instance_events")))
					for _, operation := range h.remote.Operations() { if operation == "GetSecretValue" || operation == "BatchGetSecretValue" { t.Fatal("product read a remote secret value") } }
				})
			})
		}
	}
}
