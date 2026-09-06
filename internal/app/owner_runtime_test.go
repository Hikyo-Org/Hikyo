package app

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/Hikyo-Org/hikyo/internal/remotefetch"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func startOwnerServer(t *testing.T, srv *Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done, ready := make(chan error, 1), make(chan struct{})
	go func() { done <- srv.ServeWithReady(ctx, func() { close(ready) }) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("serve before ready: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("server shutdown: %v", err)
		}
	})
}

func ownerCandidate(t *testing.T, srv *Server, edit func(map[string]string)) *runtimeconfig.Bundle {
	t.Helper()
	srv.owner.mu.Lock()
	values := maps.Clone(srv.owner.values)
	srv.owner.mu.Unlock()
	edit(values)
	bundle, err := runtimeconfig.Prepare(values)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func ownerHTTPStatus(t *testing.T, srv *Server, method, path string) int {
	t.Helper()
	body := `{}`
	if path == "/mcp" {
		body = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"` + mcpserver.ProtocolVersion + `","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"owner-runtime-test","version":"1"}}}}`
	}
	req, err := http.NewRequest(method, "http://"+srv.Addr+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if path == "/mcp" {
		origin, err := url.Parse(srv.owner.current.graph.cfg.ExternalOrigin)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = origin.Host
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", mcpserver.ProtocolVersion)
		req.Header.Set("Mcp-Method", "tools/list")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

func TestOwnerRuntimeInstallsHTTPAuthAndRetentionGraph(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	old := srv.owner.current.graph
	db, keyring, coordinator, auth := srv.db, srv.keyring, srv.selfConfig, srv.selfConfig.Auth
	public, operational := srv.publicLn, srv.operationalLn
	if got := ownerHTTPStatus(t, srv, http.MethodPost, "/mcp"); got != http.StatusNotFound {
		t.Fatalf("disabled MCP = %d", got)
	}
	candidate := ownerCandidate(t, srv, func(values map[string]string) {
		values["HIKYO_MCP_ENABLED"] = "true"
		values["HIKYO_EXTERNAL_ORIGIN"] = "http://" + srv.Addr
		values["HIKYO_ARGON2_TIME"] = "4"
		values["HIKYO_AUDIT_ACCESS_RETAIN_DAYS"] = "30"
	})
	prepared, err := srv.owner.Prepare(t.Context(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if got := ownerHTTPStatus(t, srv, http.MethodPost, "/mcp"); got != http.StatusNotFound {
		t.Fatalf("preparation changed live MCP = %d", got)
	}
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := ownerHTTPStatus(t, srv, http.MethodPost, "/mcp"); got != http.StatusOK {
		t.Fatalf("enabled MCP tool discovery = %d", got)
	}
	next := srv.owner.current.graph
	if next == old || next.auth == old.auth || next.auth.KDF.Time != 4 {
		t.Fatal("authentication graph was not replaced")
	}
	health, err := next.retention.OperationalHealth(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range health.Diagnostics.Findings {
		found = found || finding.Code == "argon2-floor" && strings.Contains(finding.Message, "time=4")
	}
	if !found || next.retention.AuditPolicy.AccessDays != 30 {
		t.Fatal("operational retention service retained the previous authentication/audit policy")
	}
	if srv.db != db || srv.keyring != keyring || srv.selfConfig != coordinator || srv.selfConfig.Auth != auth || srv.publicLn != public || srv.operationalLn != operational {
		t.Fatal("owner activation replaced a process-lifetime resource")
	}
	// Disposing an activated preparation must not close its serving graph.
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if got := ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta"); got != http.StatusOK {
		t.Fatalf("active graph after disposal = %d", got)
	}
}

func TestOwnerRuntimePreparationFailureAndMailOnlyKeepActiveGraph(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	old := srv.owner.current.graph
	unchanged := ownerCandidate(t, srv, func(values map[string]string) { values["HIKYO_UPDATE_CHANNEL"] = "off" })
	prepared, err := srv.owner.Prepare(t.Context(), unchanged)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = prepared.Close()
	if srv.owner.current.graph != old {
		t.Fatal("release-channel-only apply rebuilt the graph")
	}
	injected := errors.New("candidate client cannot be prepared")
	srv.owner.resources.newDirectoryClient = func(remotefetch.Config) (*remotefetch.Client, error) { return nil, injected }
	candidate := ownerCandidate(t, srv, func(values map[string]string) { values["HIKYO_ARGON2_TIME"] = "4" })
	if _, err := srv.owner.Prepare(t.Context(), candidate); !errors.Is(err, injected) {
		t.Fatalf("prepare error = %v", err)
	}
	if srv.owner.current.graph != old || ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta") != http.StatusOK {
		t.Fatal("failed preparation disturbed the usable graph")
	}
}

func TestOwnerRuntimeDrainsRequestsBeforeInstalling(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	old := srv.owner.current.graph
	ordinary := old.publicHandler
	old.publicHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/held-owner-request" {
			ordinary.ServeHTTP(w, r)
			return
		}
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	startOwnerServer(t, srv)
	requestDone := make(chan error, 1)
	go func() {
		res, err := http.Get("http://" + srv.Addr + "/held-owner-request")
		if err == nil {
			res.Body.Close()
		}
		requestDone <- err
	}()
	<-entered
	candidate := ownerCandidate(t, srv, func(values map[string]string) { values["HIKYO_ARGON2_TIME"] = "4" })
	prepared, err := srv.owner.Prepare(t.Context(), candidate)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	defer prepared.Close()
	done := make(chan error, 1)
	go func() { done <- prepared.Activate(t.Context()) }()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		srv.owner.mu.Lock()
		transitioning := srv.owner.transitioning
		srv.owner.mu.Unlock()
		if transitioning {
			break
		}
		select {
		case err := <-done:
			close(release)
			t.Fatalf("activation completed before old request drained: %v", err)
		case <-deadline.C:
			close(release)
			t.Fatal("activation did not enter its drain")
		case <-time.After(time.Millisecond):
		}
	}
	if got := ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta"); got != http.StatusServiceUnavailable {
		close(release)
		t.Fatalf("new request entered retiring graph: %d", got)
	}
	close(release)
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if srv.owner.current.graph == old || ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta") != http.StatusOK {
		t.Fatal("replacement did not become usable after drain")
	}
}

func TestOwnerRuntimeActivationFailureResumesAdministrativeGraph(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	old := srv.owner.current.graph
	candidate := ownerCandidate(t, srv, func(values map[string]string) { values["HIKYO_ARGON2_TIME"] = "4" })
	prepared, err := srv.owner.Prepare(t.Context(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	activation, ok := prepared.(*preparedOwnerActivation)
	if !ok {
		t.Fatal("changed owner values did not prepare a graph")
	}
	// Inject a candidate that has already admitted traffic. Counter transfer
	// must refuse it after draining instead of resetting the old limits.
	activation.graph.limiter.AllowDiscovery("192.0.2.1")
	if err := prepared.Activate(t.Context()); err == nil {
		t.Fatal("dirty candidate admission state was installed")
	}
	if srv.owner.current.graph != old {
		t.Fatal("failed activation replaced the previous graph")
	}
	if got := ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta"); got != http.StatusOK {
		t.Fatalf("previous administrative graph unavailable after failed installation: %d", got)
	}
	if err := srv.db.SQLiteRead().PingContext(t.Context()); err != nil {
		t.Fatalf("failed activation closed the shared datastore: %v", err)
	}
}

type refusedOwnerInstaller struct{}

func (refusedOwnerInstaller) Prepare(context.Context, *runtimeconfig.Bundle) (runtimeconfig.PreparedActivation, error) {
	return nil, errors.New("injected owner installation failure")
}

func TestOwnerRuntimeFailedTargetFencesBusinessAndPreservesHTTPRepair(t *testing.T) {
	cfg := devConfig(t)
	cfg.MCPEnabled = true
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := srv.owner.current.graph.auth.BootstrapAdmin(t.Context(), "owner", "Owner", "stdout")
	if err != nil {
		_ = srv.Close()
		t.Fatal(err)
	}
	if err := srv.selfConfig.LoadRuntime(t.Context()); err != nil {
		_ = srv.Close()
		t.Fatal(err)
	}
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactBrowserSession)
	if err != nil {
		_ = srv.Close()
		t.Fatal(err)
	}
	now := time.Now()
	err = tx.Write(t.Context(), srv.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		generation, err := az.PrincipalGeneration(ctx, bootstrap.PrincipalID)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		return az.MintSession(ctx, authz.NewSession{ID: "ses_owner_http", PrincipalID: bootstrap.PrincipalID, Verifier: verifier, Artifact: "browser", SessionGeneration: generation, CredentialEpoch: epoch, AuthMethod: "local-passkey", Factors: `["webauthn"]`, AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour), SourceIP: "127.0.0.1", UserAgent: "owner-http-test"})
	})
	if err != nil {
		_ = srv.Close()
		t.Fatal(err)
	}
	old := srv.owner.current.graph
	candidate := ownerCandidate(t, srv, func(values map[string]string) { values["HIKYO_ARGON2_TIME"] = "4" })
	prepared, err := srv.owner.Prepare(t.Context(), candidate)
	if err != nil {
		_ = srv.Close()
		t.Fatal(err)
	}
	activation, ok := prepared.(*preparedOwnerActivation)
	if !ok {
		_ = prepared.Close()
		_ = srv.Close()
		t.Fatal("changed owner values did not prepare a graph")
	}
	activation.graph.limiter.AllowDiscovery("192.0.2.1")
	if err := prepared.Activate(t.Context()); err == nil {
		_ = prepared.Close()
		_ = srv.Close()
		t.Fatal("injected installation failure succeeded")
	}
	_ = prepared.Close()
	// Model a committed target the failed installer cannot acknowledge. Keep
	// reconciliation failing instead of relying on a race with its next tick.
	if _, err := srv.db.SQLiteWrite().ExecContext(t.Context(), `UPDATE self_config_binding SET generation=generation+1 WHERE id=1`); err != nil {
		_ = srv.Close()
		t.Fatal(err)
	}
	srv.selfConfig.Installer = refusedOwnerInstaller{}
	startOwnerServer(t, srv)
	for _, test := range []struct {
		path string
		want int
	}{
		{"/api/v1/auth/whoami", http.StatusOK},
		{"/api/v1/instance/config", http.StatusOK},
		{"/api/v1/orgs/org_00000000-0000-7000-8000-000000000001/projects", http.StatusServiceUnavailable},
	} {
		req, err := http.NewRequest(http.MethodGet, "http://"+srv.Addr+test.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+artifact)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != test.want {
			t.Fatalf("%s = %d, want %d: %s", test.path, res.StatusCode, test.want, body)
		}
	}
	if got := ownerHTTPStatus(t, srv, http.MethodPost, "/mcp"); got != http.StatusServiceUnavailable {
		t.Fatalf("stale MCP discovery admitted: %d", got)
	}
	if srv.owner.current.graph != old {
		t.Fatal("repair did not run on the previous usable graph")
	}
}
