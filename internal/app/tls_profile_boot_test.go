package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

type strictBootFixture struct {
	dir      string
	certPath string
	keyPath  string
	roots    *x509.CertPool
	logs     *syncBuffer
}

func newStrictBootFixture(t *testing.T) strictBootFixture {
	t.Helper()
	dir := t.TempDir()
	certPEM, keyPEM, _ := tlstest.MintServerCert(t, "127.0.0.1")
	certPath, keyPath := tlstest.WritePair(t, dir, certPEM, keyPEM)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	return strictBootFixture{dir: dir, certPath: certPath, keyPath: keyPath, roots: roots, logs: &syncBuffer{}}
}

// boot runs the real Boot against the fixture database. An empty nodeID keeps the
// default node; withCertificate supplies the bootstrap pair for this process.
func (f strictBootFixture) boot(t *testing.T, nodeID string, withCertificate bool, extra map[string]string) (*Server, error) {
	t.Helper()
	environment := map[string]string{
		"HIKYO_DB":                 "sqlite:" + filepath.Join(f.dir, "hikyo.db"),
		"HIKYO_OPERATIONAL_LISTEN": "localhost:0",
		"HIKYO_TLS_PROFILE":        "strict",
		"HIKYO_NODE_ID":            nodeID,
	}
	if withCertificate {
		environment["HIKYO_TLS_CERT_FILE"], environment["HIKYO_TLS_KEY_FILE"] = f.certPath, f.keyPath
	}
	for k, v := range extra {
		environment[k] = v
	}
	cfg, _, err := config.LoadBootstrap("server", []string{"--dev", "--listen", "127.0.0.1:0"}, func(key string) string { return environment[key] }, nil)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	return Boot(t.Context(), cfg, slog.New(slog.NewTextHandler(f.logs, nil)))
}

func (f strictBootFixture) requireHybridOnly(t *testing.T, srv *Server) {
	t.Helper()
	for name, cfg := range map[string]*tls.Config{
		"classical TLS 1.3": {RootCAs: f.roots, MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}},
		"TLS 1.2":           {RootCAs: f.roots, MaxVersion: tls.VersionTLS12},
	} {
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, DisableKeepAlives: true}}
		if resp, err := client.Get("https://" + srv.Addr + "/api/v1/meta"); err == nil {
			resp.Body.Close()
			t.Fatalf("%s peer was served by a node that adopted strict", name)
		}
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: f.roots}, DisableKeepAlives: true}}
	resp, err := client.Get("https://" + srv.Addr + "/api/v1/meta")
	if err != nil {
		t.Fatalf("hybrid peer refused: %v", err)
	}
	resp.Body.Close()
	if resp.TLS.Version != tls.VersionTLS13 || resp.TLS.CurveID != tls.X25519MLKEM768 {
		t.Fatalf("negotiated %x %v", resp.TLS.Version, resp.TLS.CurveID)
	}
}

// adoptStrict boots a node with the bootstrap pair and strict, adopts the running
// server's seed as an administrator does, and stops it, so strict is a stored
// managed value and not only a bootstrap environment input.
func (f strictBootFixture) adoptStrict(t *testing.T, extra map[string]string) {
	t.Helper()
	first, err := f.boot(t, "", true, extra)
	if err != nil {
		t.Fatal(err)
	}
	stop := serveUntilStopped(t, first)
	f.requireHybridOnly(t, first)
	adminCfg, _, err := config.Load("admin", []string{"--dev"}, func(key string) string {
		if key == "HIKYO_DB" {
			return "sqlite:" + filepath.Join(f.dir, "hikyo.db")
		}
		return ""
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RunAdmin(t.Context(), adminCfg, testLogger(), []string{"create", "--username", "owner", "--output-file", filepath.Join(f.dir, "authority.txt")}, io.Discard, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := first.selfConfig.ReconcileRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	bundle, err := first.selfConfig.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.OwnerValues()[config.TLSProfileKey] != config.TLSProfileStrict {
		t.Fatalf("adopted profile = %q, want strict", bundle.OwnerValues()[config.TLSProfileKey])
	}
	stop()
}

// A node missing from the adopted node document falls back to the bootstrap seed
// to stay administrable. That seed must never carry a weaker profile than the
// adopted one: the node either keeps strict or refuses to boot. These drive the
// real Boot path with later boots that carry no bootstrap profile, as a restart
// of a managed installation does.
func TestRealBootKeepsAdoptedStrictWhenTheNodeIsMissingFromTheNodeDocument(t *testing.T) {
	unset := map[string]string{"HIKYO_TLS_PROFILE": ""}
	t.Run("new node id without a certificate refuses to boot", func(t *testing.T) {
		f := newStrictBootFixture(t)
		f.adoptStrict(t, nil)
		srv, err := f.boot(t, "node-without-certificate", false, unset)
		if err == nil {
			_ = srv.Close()
			t.Fatal("a node that adopted strict booted without a certificate")
		}
	})
	t.Run("new node id with a certificate keeps strict", func(t *testing.T) {
		f := newStrictBootFixture(t)
		f.adoptStrict(t, nil)
		srv, err := f.boot(t, "node-with-certificate", true, unset)
		if err != nil {
			t.Fatal(err)
		}
		defer serveUntilStopped(t, srv)()
		f.requireHybridOnly(t, srv)
		if srv.owner.current.graph.cfg.TLSProfile != config.TLSProfileStrict {
			t.Fatalf("effective profile = %q", srv.owner.current.graph.cfg.TLSProfile)
		}
	})
}

// serveUntilStopped serves srv and returns a function that stops it and waits.
func serveUntilStopped(t *testing.T, srv *Server) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done, ready := make(chan error, 1), make(chan struct{})
	go func() { done <- srv.ServeWithReady(ctx, func() { close(ready) }) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("serve before ready: %v", err)
	}
	return func() {
		cancel()
		<-done
	}
}

// The helper-level case a full boot cannot reach: a certificate-bearing node whose
// first apply is refused for an unrelated reason (an adopted IP origin) used to
// fall back to the seed baseline with compat and its TLS 1.2 floor.
func TestBootNodeConfigurationCarriesAdoptedStrictIntoTheRecoveryBaseline(t *testing.T) {
	f := newStrictBootFixture(t)
	environment := map[string]string{
		"HIKYO_DB": "sqlite:" + filepath.Join(f.dir, "hikyo.db"), "HIKYO_NODE_ID": "joining",
		"HIKYO_TLS_CERT_FILE": f.certPath, "HIKYO_TLS_KEY_FILE": f.keyPath,
	}
	cfg, _, err := config.LoadBootstrap("server", []string{"--dev", "--listen", "127.0.0.1:0"}, func(key string) string { return environment[key] }, nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := newSelfConfig(cfg, nil, nil, nil)
	seed, err := coordinator.Seed()
	if err != nil {
		t.Fatal(err)
	}
	node, err := coordinator.SeedNode()
	if err != nil {
		t.Fatal(err)
	}
	for _, adopted := range []map[string]string{
		{"HIKYO_TLS_PROFILE": "strict", "HIKYO_EXTERNAL_ORIGIN": "https://127.0.0.1:8443"},
		{"HIKYO_TLS_PROFILE": "strict", "HIKYO_TRUSTED_PROXY_CIDRS": "10.0.0.0/8"},
	} {
		owner := maps.Clone(seed)
		for k, v := range adopted {
			if k != "HIKYO_TRUSTED_PROXY_CIDRS" { // a node-scoped key
				owner[k] = v
			}
		}
		raw, err := runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{"another-node": node})
		if err != nil {
			t.Fatal(err)
		}
		owner[config.ManagedNodeOverridesKey] = raw
		bundle, err := runtimeconfig.Prepare(owner)
		if err != nil {
			t.Fatal(err)
		}
		effective, _, _, missing, err := bootNodeConfiguration(cfg, coordinator, bundle)
		if !missing {
			t.Fatal("fixture must exercise the missing-node fallback")
		}
		if err == nil && effective.TLSProfile != config.TLSProfileStrict {
			t.Fatalf("recovery baseline weakened the adopted profile to %q", effective.TLSProfile)
		}
	}
}

// After adoption the managed value decides. A differing or invalid bootstrap
// value is ignored with a warning that does not echo it.
func TestAdoptedProfileOutranksAndWarnsAboutADifferingBootstrapValue(t *testing.T) {
	f := newStrictBootFixture(t)
	f.adoptStrict(t, nil)
	for _, env := range []string{"compat", "not-a-profile"} {
		srv, err := f.boot(t, "", true, map[string]string{"HIKYO_TLS_PROFILE": env})
		if err != nil {
			t.Fatalf("bootstrap %q: %v", env, err)
		}
		stop := serveUntilStopped(t, srv)
		f.requireHybridOnly(t, srv)
		stop()
		logged := f.logs.String()
		if !strings.Contains(logged, "HIKYO_TLS_PROFILE in the environment is ignored") || !strings.Contains(logged, "adopted_profile=strict") {
			t.Fatalf("no warning for bootstrap %q:\n%s", env, logged)
		}
		if strings.Contains(logged, env) && env != "compat" {
			t.Fatalf("the invalid bootstrap value was echoed")
		}
	}
}

// The configured-evidence record belongs to the generation that became active.
func TestConfiguredEvidenceIsLoggedAtActivationNotPreparation(t *testing.T) {
	f := newStrictBootFixture(t)
	srv, err := f.boot(t, "", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serveUntilStopped(t, srv)()
	count := func() int { return strings.Count(f.logs.String(), "native listener TLS policy") }
	before := count()
	if before == 0 {
		t.Fatal("the booted generation must log its configured profile")
	}
	candidate := ownerCandidate(t, srv, func(values map[string]string) { values[config.TLSProfileKey] = config.TLSProfileCompat })
	prepared, err := srv.owner.Prepare(t.Context(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if count() != before {
		t.Fatal("a merely prepared candidate logged a profile that is not active")
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if count() != before {
		t.Fatal("a discarded candidate logged a profile")
	}
	activateOwner := ownerCandidate(t, srv, func(values map[string]string) { values[config.TLSProfileKey] = config.TLSProfileCompat })
	prepared, err = srv.owner.Prepare(t.Context(), activateOwner)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if count() != before+1 || !strings.Contains(f.logs.String(), "profile=compat") {
		t.Fatalf("activation must log the new configured profile once:\n%s", f.logs.String())
	}
}

// Activating strict over an unchanged certificate must end classical service on
// the live listener, including a keep-alive connection opened before.
func TestActivatingStrictClosesClassicalKeepAliveAndServesOnlyHybrid(t *testing.T) {
	f := newStrictBootFixture(t)
	srv, err := f.boot(t, "", true, map[string]string{"HIKYO_TLS_PROFILE": "compat"})
	if err != nil {
		t.Fatal(err)
	}
	defer serveUntilStopped(t, srv)()
	classical := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: f.roots, MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}}}}
	get := func(c *http.Client) (*http.Response, error) { return c.Get("https://" + srv.Addr + "/api/v1/meta") }
	resp, err := get(classical)
	if err != nil {
		t.Fatalf("compat must serve a classical peer: %v", err)
	}
	resp.Body.Close()
	if resp.TLS.CurveID != tls.X25519 {
		t.Fatalf("control negotiated %v", resp.TLS.CurveID)
	}
	prepared, err := srv.owner.Prepare(t.Context(), ownerCandidate(t, srv, func(v map[string]string) { v[config.TLSProfileKey] = config.TLSProfileStrict }))
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if resp, err := get(classical); err == nil {
		resp.Body.Close()
		t.Fatal("the classical keep-alive client was still served after strict activated")
	}
	fresh := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: f.roots, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}, DisableKeepAlives: true}}
	if resp, err := get(fresh); err == nil {
		resp.Body.Close()
		t.Fatal("a TLS 1.2 client was served after strict activated")
	}
	f.requireHybridOnly(t, srv)
}
