package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

func nodeCandidate(t *testing.T, srv *Server, edit func(map[string]string, map[string]string)) *runtimeconfig.Bundle {
	t.Helper()
	srv.owner.mu.Lock()
	owner, node := maps.Clone(srv.owner.values), maps.Clone(srv.owner.nodeValues)
	srv.owner.mu.Unlock()
	edit(owner, node)
	raw, err := runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{srv.selfConfig.NodeID: node})
	if err != nil {
		t.Fatal(err)
	}
	owner[config.ManagedNodeOverridesKey] = raw
	bundle, err := runtimeconfig.Prepare(owner)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func activateNode(t *testing.T, srv *Server, bundle *runtimeconfig.Bundle) {
	t.Helper()
	prepared, err := srv.owner.Prepare(t.Context(), bundle)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func availableNodeAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func TestNodeRuntimeInstallsIndependentCapacityAndPolicy(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	old := srv.owner.current.graph
	backupDirectory := t.TempDir()
	bundle := nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) {
		node["HIKYO_ADMISSION_BUDGET_MIB"] = "528"
		node["HIKYO_BACKUP_DIR"] = backupDirectory
		node["HIKYO_TRUSTED_PROXY_CIDRS"] = "192.0.2.0/24"
		node["HIKYO_OIDC_EGRESS_POLICY_JSON"] = `{"https://issuer.example":["192.0.2.0/24"]}`
		node["HIKYO_ADAPTER_EGRESS_POLICY_JSON"] = `{"https://adapter.example":["198.51.100.0/24"]}`
		node["HIKYO_DYNAMIC_EGRESS_POLICY_JSON"] = `{"postgres://operator@db.example/app":["203.0.113.0/24"]}`
	})
	prepared, err := srv.owner.Prepare(t.Context(), bundle)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if old.limiter.Concurrency() != 4 {
		t.Fatalf("initial concurrency = %d", old.limiter.Concurrency())
	}
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	next := srv.owner.current.graph
	if next.limiter.Concurrency() != 8 || next.auth.Admission != next.limiter {
		t.Fatal("node admission capacity did not change")
	}
	if next.cfg.BackupDir != backupDirectory {
		t.Fatal("backup consumer retained old directory")
	}
	if len(next.auth.FederationPolicy.AllowedCIDRs["https://issuer.example"]) != 1 || len(next.cfg.AdapterEgressPolicy["https://adapter.example"]) != 1 || len(next.cfg.DynamicEgressPolicy["postgres://operator@db.example/app"]) != 1 {
		t.Fatal("node egress consumers retained old policy")
	}
	if got := ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta"); got != http.StatusOK {
		t.Fatalf("HTTP after node install = %d", got)
	}
	// A candidate that omits this node cannot borrow a remote node's values.
	remote, err := bundle.NodeValues(srv.selfConfig.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{"other-node": remote})
	if err != nil {
		t.Fatal(err)
	}
	values := bundle.OwnerValues()
	values[config.ManagedNodeOverridesKey] = raw
	foreign, err := runtimeconfig.Prepare(values)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.owner.Prepare(t.Context(), foreign); !errors.Is(err, runtimeconfig.ErrNodeNotConfigured) {
		t.Fatalf("foreign node fallback = %v", err)
	}
	if srv.owner.current.graph != next {
		t.Fatal("missing node changed active graph")
	}
}

func TestNodeRuntimeReservesMovesAndSwapsListeners(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	oldAddress, oldOperational := srv.Addr, srv.OperationalAddr
	newAddress := availableNodeAddress(t)
	bundle := nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) { node["HIKYO_LISTEN"] = newAddress })
	prepared, err := srv.owner.Prepare(t.Context(), bundle)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if ln, err := net.Listen("tcp", newAddress); err == nil {
		_ = ln.Close()
		t.Fatal("candidate did not reserve new address")
	}
	if srv.Addr != oldAddress || ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta") != http.StatusOK {
		t.Fatal("preparation changed active endpoint")
	}
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if srv.Addr != newAddress || srv.OperationalAddr != oldOperational {
		t.Fatal("listener addresses did not install")
	}
	if got := ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta"); got != http.StatusOK {
		t.Fatalf("moved listener = %d", got)
	}
	if conn, err := net.DialTimeout("tcp", oldAddress, time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("retired socket still accepts")
	}
	// Both occupied addresses are reused when their roles swap.
	activateNode(t, srv, nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) {
		node["HIKYO_LISTEN"], node["HIKYO_OPERATIONAL_LISTEN"] = oldOperational, newAddress
	}))
	if srv.Addr != oldOperational || srv.OperationalAddr != newAddress {
		t.Fatal("listener role swap failed")
	}
	if got := ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta"); got != http.StatusOK {
		t.Fatalf("swapped public listener = %d", got)
	}
	res, err := http.Get("http://" + srv.OperationalAddr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("swapped operational listener = %d", res.StatusCode)
	}
}

func TestNodeRuntimeListenerFailureKeepsOldGraphAndReleasesReservation(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	reserved := availableNodeAddress(t)
	old := srv.owner.current.graph
	bundle := nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) {
		node["HIKYO_LISTEN"] = reserved
		node["HIKYO_OPERATIONAL_LISTEN"] = occupied.Addr().String()
	})
	if _, err := srv.owner.Prepare(t.Context(), bundle); err == nil {
		t.Fatal("occupied listener accepted")
	}
	ln, err := net.Listen("tcp", reserved)
	if err != nil {
		t.Fatalf("failed preparation leaked first socket: %v", err)
	}
	_ = ln.Close()
	if srv.owner.current.graph != old || ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta") != http.StatusOK {
		t.Fatal("bind failure replaced active graph")
	}
}

func TestNodeRuntimeTLSRotationAndPlaintextTransition(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	address := srv.Addr
	plain := &http.Client{Transport: &http.Transport{}, Timeout: 3 * time.Second}
	defer plain.CloseIdleConnections()
	read := func(client *http.Client, scheme string) *http.Response {
		t.Helper()
		res, err := client.Get(scheme + "://" + address + "/api/v1/meta")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s response = %d", scheme, res.StatusCode)
		}
		return res
	}
	read(plain, "http") // leaves a plaintext keep-alive to retire
	cert, key, leaf := tlstest.MintServerCert(t, "127.0.0.1")
	secondCert, secondKey, secondLeaf := tlstest.MintServerCert(t, "127.0.0.1")
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	roots.AddCert(secondLeaf)
	secure := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, Timeout: 3 * time.Second}
	defer secure.CloseIdleConnections()
	activateNode(t, srv, nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) {
		node["HIKYO_TLS_CERT_PEM"], node["HIKYO_TLS_KEY_PEM"] = string(cert), string(key)
	}))
	if srv.Addr != address {
		t.Fatal("TLS change rebound the address")
	}
	res := read(secure, "https")
	if res.TLS.PeerCertificates[0].SerialNumber.Cmp(leaf.SerialNumber) != 0 {
		t.Fatal("initial managed certificate not served")
	}
	if res, err := plain.Get("http://" + address + "/api/v1/meta"); err == nil {
		_ = res.Body.Close()
		if res.StatusCode == http.StatusOK {
			t.Fatal("old plaintext connection bypassed TLS")
		}
	}
	activateNode(t, srv, nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) {
		node["HIKYO_TLS_CERT_PEM"], node["HIKYO_TLS_KEY_PEM"] = string(secondCert), string(secondKey)
	}))
	res = read(secure, "https")
	if res.TLS.PeerCertificates[0].SerialNumber.Cmp(secondLeaf.SerialNumber) != 0 {
		t.Fatal("rotated managed certificate not served")
	}
	bad := maps.Clone(srv.owner.nodeValues)
	bad["HIKYO_TLS_KEY_PEM"] = string(key)
	if _, err := runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{srv.selfConfig.NodeID: bad}); err == nil {
		t.Fatal("mismatched pair accepted")
	}
	if res := read(secure, "https"); res.TLS.PeerCertificates[0].SerialNumber.Cmp(secondLeaf.SerialNumber) != 0 {
		t.Fatal("invalid candidate changed certificate")
	}
	activateNode(t, srv, nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) {
		delete(node, "HIKYO_TLS_CERT_PEM")
		delete(node, "HIKYO_TLS_KEY_PEM")
	}))
	read(plain, "http")
}

func TestNodeRuntimeImportsTLSFilesOnce(t *testing.T) {
	cfg := devConfig(t)
	cert, key, leaf := tlstest.MintServerCert(t, "127.0.0.1")
	cfg.TLSCertFile, cfg.TLSKeyFile = tlstest.WritePair(t, t.TempDir(), cert, key)
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	if err := os.Remove(cfg.TLSCertFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg.TLSKeyFile); err != nil {
		t.Fatal(err)
	}
	if err := srv.ReloadTLS(); err == nil {
		t.Fatal("SIGHUP claimed to reload immutable managed TLS")
	}
	seed, err := srv.selfConfig.SeedNode()
	if err != nil || seed["HIKYO_TLS_CERT_PEM"] != string(cert) {
		t.Fatalf("seed reread old source: %v", err)
	}
	activateNode(t, srv, nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) { node["HIKYO_ADMISSION_BUDGET_MIB"] = "528" }))
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	res, err := client.Get("https://" + srv.Addr + "/api/v1/meta")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("file-free managed graph = %d", res.StatusCode)
	}
}

func TestNodeRuntimeManagedRestartDoesNotReadDeletedTLSFiles(t *testing.T) {
	cfg := devConfig(t)
	cert, key, leaf := tlstest.MintServerCert(t, "127.0.0.1")
	cfg.TLSCertFile, cfg.TLSKeyFile = tlstest.WritePair(t, t.TempDir(), cert, key)
	first, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.owner.current.graph.auth.BootstrapAdmin(t.Context(), "operator", "Operator", "stdout"); err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg.TLSCertFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg.TLSKeyFile); err != nil {
		t.Fatal(err)
	}
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	res, err := client.Get("https://" + srv.Addr + "/api/v1/meta")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.TLS.PeerCertificates[0].SerialNumber.Cmp(leaf.SerialNumber) != 0 {
		t.Fatal("managed restart did not use encrypted certificate")
	}
}

func TestNodeRuntimeCancelledActivationReleasesNewListener(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	address := availableNodeAddress(t)
	prepared, err := srv.owner.Prepare(t.Context(), nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) { node["HIKYO_LISTEN"] = address }))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := prepared.Activate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled activation = %v", err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("cancelled preparation retained socket: %v", err)
	}
	_ = ln.Close()
}

func TestNodeRuntimeScheduledBackupUsesInstalledDestination(t *testing.T) {
	cfg := devConfig(t)
	oldDirectory := t.TempDir()
	cfg.BackupDir = oldDirectory
	srv, err := Boot(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	newDirectory := t.TempDir()
	activateNode(t, srv, nodeCandidate(t, srv, func(owner, node map[string]string) {
		owner["HIKYO_BACKUP_RECIPIENTS"] = recipient
		node["HIKYO_BACKUP_DIR"] = newDirectory
	}))
	found := false
	for _, job := range srv.owner.current.graph.scheduler.Jobs {
		if job.Name == "backup_export" {
			found = true
			if err := job.Run(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found || archiveCount(t, newDirectory) != 1 || archiveCount(t, oldDirectory) != 0 {
		t.Fatal("scheduled backup did not use installed node destination")
	}
}

func TestNodeRuntimeListenerMoveDrainsOldHTTPResponse(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	old := srv.owner.current.graph
	next := old.publicHandler
	old.publicHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/held-node-response" {
			next.ServeHTTP(w, r)
			return
		}
		close(entered)
		<-release
		_, _ = io.WriteString(w, "completed before listener retirement")
	})
	startOwnerServer(t, srv)
	response := make(chan error, 1)
	oldAddress := srv.Addr
	go func() {
		res, err := http.Get("http://" + oldAddress + "/held-node-response")
		if err != nil {
			response <- err
			return
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err == nil && string(body) != "completed before listener retirement" {
			err = errors.New("old response was truncated")
		}
		response <- err
	}()
	<-entered
	prepared, err := srv.owner.Prepare(t.Context(), nodeCandidate(t, srv, func(_ map[string]string, node map[string]string) { node["HIKYO_LISTEN"] = availableNodeAddress(t) }))
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	defer prepared.Close()
	applied := make(chan error, 1)
	go func() { applied <- prepared.Activate(t.Context()) }()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		srv.owner.mu.Lock()
		draining := srv.owner.transitioning
		srv.owner.mu.Unlock()
		if draining {
			break
		}
		select {
		case <-deadline.C:
			close(release)
			t.Fatal("node installation did not begin draining")
		case <-tick.C:
		}
	}
	select {
	case err := <-applied:
		close(release)
		t.Fatalf("node installed before response completed: %v", err)
	default:
	}
	close(release)
	if err := <-response; err != nil {
		t.Fatal(err)
	}
	if err := <-applied; err != nil {
		t.Fatal(err)
	}
	if got := ownerHTTPStatus(t, srv, http.MethodGet, "/api/v1/meta"); got != http.StatusOK {
		t.Fatalf("listener after drain = %d", got)
	}
}
