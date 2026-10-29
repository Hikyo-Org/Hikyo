//go:build darwin || linux

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/jackc/pgx/v5"
)

const postgresImage = "postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941"

type serverProcess struct {
	cmd         *exec.Cmd
	done        chan error
	address     string
	identity    string
	operational string
}

func command(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", errors.New("owned resource command failed; output withheld")
	}
	return strings.TrimSpace(string(output)), nil
}

func randomHex() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("system randomness unavailable")
	}
	return hex.EncodeToString(b[:])
}

func local(ctx context.Context, dir, binary string) (result error) {
	if !filepath.IsAbs(binary) {
		return errors.New("absolute local server executable required")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		return errors.New("local runtime directory must be empty")
	}
	r := newReceipt()
	r.Identities["server_binary_sha256"] = fileHash(binary)
	if !sha256Pattern.MatchString(r.Identities["server_binary_sha256"]) {
		return errors.New("server executable identity unavailable")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	r.Identities["helper_binary_sha256"] = fileHash(self)
	r.Identities["codex_binary_sha256"] = fileHash(os.Getenv("MCP651_CODEX"))
	r.Identities["inspector_cli_sha256"] = fileHash(os.Getenv("MCP651_INSPECTOR_ENTRY"))
	r.Identities["source_commit"], err = command(ctx, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	r.Identities["source_tree"], err = command(ctx, "git", "rev-parse", "HEAD^{tree}")
	if err != nil {
		return err
	}
	name := "hikyo-mcp-651-" + randomHex()[:12]
	var container, network string
	var processes []*serverProcess
	defer func() {
		for _, p := range processes {
			stopServer(p, 7*time.Second)
		}
		clean := cleanupDocker(name, container, network, boundedDocker)
		if clean {
			r.Checks["owned_container_network_process_cleanup"] = "PASS"
		} else {
			r.Checks["owned_container_network_process_cleanup"] = "FAIL"
			result = errors.New("owned resource cleanup failed")
		}
		if result == nil {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if e.Name() == "smoke-receipt.json" || e.Name() == "wire-receipt.json" {
					continue
				}
				if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
					result = errors.New("private scratch cleanup failed")
				}
			}
			if result == nil {
				r.Checks["private_scratch_cleanup"] = "PASS"
			}
		} else {
			r.Checks["outcome"] = "FAIL; private diagnostic scratch retained pending repair"
		}
		if err := writePrivate(filepath.Join(dir, "local-receipt.json"), r); err != nil {
			result = err
		}
	}()
	network, err = command(ctx, "docker", "network", "create", "--label", "hikyo.acceptance=651", name)
	if err != nil {
		return err
	}
	r.Identities["owned_network_id"] = network
	password := randomHex()
	if err := os.WriteFile(filepath.Join(dir, "postgres.env"), []byte("POSTGRES_USER=hikyo\nPOSTGRES_DB=hikyo_mcp_651\nPOSTGRES_PASSWORD="+password+"\n"), 0600); err != nil {
		return err
	}
	container, err = command(ctx, "docker", "run", "-d", "--name", name, "--label", "hikyo.acceptance=651", "--network", network, "--memory", "512m", "--cpus", "1", "--pids-limit", "128", "--mount", "type=tmpfs,destination=/var/lib/postgresql,tmpfs-size=536870912", "--env-file", filepath.Join(dir, "postgres.env"), "--publish", "127.0.0.1::5432", postgresImage, "-c", "max_connections=40", "-c", "shared_buffers=32MB")
	if err != nil {
		return err
	}
	r.Identities["owned_container_id"] = container
	address, err := command(ctx, "docker", "port", container, "5432/tcp")
	if err != nil || !strings.HasPrefix(address, "127.0.0.1:") {
		return errors.New("database did not bind loopback only")
	}
	dsn := "postgres://hikyo:" + password + "@" + address + "/hikyo_mcp_651?sslmode=disable"
	if err := wait(ctx, 20*time.Second, func(ctx context.Context) bool {
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return false
		}
		defer c.Close(context.Background())
		return c.Ping(ctx) == nil
	}); err != nil {
		return errors.New("owned PostgreSQL readiness failed")
	}
	state := filepath.Join(dir, "installation")
	if err := os.Mkdir(state, 0700); err != nil {
		return err
	}
	root := filepath.Join(dir, "root.key")
	if err := os.WriteFile(root, []byte(randomHex()), 0600); err != nil {
		return err
	}
	cfg := settings{DSN: dsn, Root: root, State: state, Development: true}
	if err := writePrivate(filepath.Join(dir, "settings.json"), cfg); err != nil {
		return err
	}
	db, kr, err := openFreshFixture(ctx, cfg)
	if err != nil {
		return errors.New("local development admission failed")
	}
	defer db.Close()
	if err := seed(ctx, dir, db, kr); err != nil {
		return err
	}
	r.Checks["supported_service_provisioning_nonempty_disjoint_scope"] = "PASS"
	if requireEmptyFixture(ctx, cfg) == nil {
		return errors.New("nonempty datastore pre-admission guard failed")
	}
	r.Checks["nonempty_seed_refused_before_admission"] = "PASS"
	var f fixture
	if err := readPrivate(filepath.Join(dir, "fixture.json"), &f); err != nil {
		return err
	}
	certFile, keyFile, err := localCertificate(dir)
	if err != nil {
		return err
	}
	front, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer front.Close()
	_, frontPort, _ := net.SplitHostPort(front.Addr().String())
	origin := "https://localhost:" + frontPort
	for i := 0; i < 2; i++ {
		p, err := startServer(ctx, dir, binary, cfg, origin, certFile, keyFile, i)
		if err != nil {
			return err
		}
		processes = append(processes, p)
	}
	// Actual OS process identities, independently observed before the gateway
	// labels a response. The client cannot choose an upstream or session.
	r.Checks["actual_server_pids"] = fmt.Sprintf("PASS: %d, %d", processes[0].cmd.Process.Pid, processes[1].cmd.Process.Pid)
	caFile := filepath.Join(dir, "local-ca.pem")
	raw, err := os.ReadFile(caFile)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(raw)
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}}
	defer transport.CloseIdleConnections()
	var sequence atomic.Uint64
	wire := &wireRecorder{directory: dir}
	proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		i := int(sequence.Add(1)-1) % 2
		u, _ := url.Parse("https://" + processes[i].address)
		pr.SetURL(u)
		pr.Out.Host = pr.In.Host
		pr.Out.Header.Del("X-Fixture-Process")
		pr.SetXForwarded()
		pr.Out.Header.Set("X-Forwarded-Proto", "https")
		pr.Out.Header.Set("X-Observed-Fixture-Upstream", processes[i].identity)
	}, Transport: transport, ModifyResponse: func(h *http.Response) error {
		h.Header.Set("X-Fixture-Process", h.Request.Header.Get("X-Observed-Fixture-Upstream"))
		return wire.response(h)
	}, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) { w.WriteHeader(502) }, FlushInterval: -1}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: wire.wrap(proxy), ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(tls.NewListener(front, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}))
	t := target{URL: origin + "/mcp", CAFile: caFile, Processes: []string{processes[0].identity, processes[1].identity}}
	if err := writePrivate(filepath.Join(dir, "target.json"), t); err != nil {
		return err
	}
	p, err := newProbe(t, f, r)
	if err != nil {
		return err
	}
	defer p.client.CloseIdleConnections()
	if err := p.baseline(ctx); err != nil {
		return err
	}
	// Preserve direct TLS observations separately from the TLS proxy path.
	for i := 0; i < 2; i++ {
		directTransport := transport.Clone()
		address := processes[i].address
		directTransport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		directClient := *p.client
		directClient.Transport = directTransport
		direct := *p
		direct.client = &directClient
		r, err := direct.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), f.Credentials["lifecycle"].Token)
		directTransport.CloseIdleConnections()
		if err != nil || !successful(r) {
			return errors.New("separate direct verified TLS control failed")
		}
	}
	r.Checks["direct_verified_TLS_each_process"] = "PASS; distinct from local proxy cancellation"
	// Revocation first-call proof: no polling or background requests use the
	// predecessor after the supported writer returns from its committed write.
	for i := 0; i < 2; i++ {
		v, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), f.Credentials["rotating"].Token)
		if err != nil || !successful(v) {
			return errors.New("predecessor live positive control failed")
		}
	}
	if err := mutate(ctx, dir, db, kr, "revoke"); err != nil {
		return errors.New("supported revocation refused")
	}
	r.event("credential_revocation_committed", "", 0)
	if err := p.firstDenials(ctx, f.Credentials["rotating"].Token, true); err != nil {
		return err
	}
	r.Checks["committed_revocation_first_request_each_process_replacement"] = "PASS"
	if err := mutate(ctx, dir, db, kr, "grant-remove"); err != nil {
		return errors.New("supported grant removal refused")
	}
	r.event("grant_removal_committed", "", 0)
	if err := p.firstDenials(ctx, f.Credentials["live"].Token, false); err != nil {
		return err
	}
	if err := mutate(ctx, dir, db, kr, "grant-restore"); err != nil {
		return errors.New("supported grant restoration refused")
	}
	r.event("grant_restoration_committed", "", 0)
	for i := 0; i < 2; i++ {
		v, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), f.Credentials["live"].Token)
		if err != nil || !successful(v) {
			return errors.New("same-bearer restoration failed")
		}
	}
	r.Checks["supported_grant_remove_first_denials_restore_both"] = "PASS"
	if err := p.smoke(ctx, dir, func() error { return mutate(ctx, dir, db, kr, "revoke-smoke") }); err != nil {
		return err
	}
	if err := p.namedClients(ctx, dir); err != nil {
		return err
	}
	if !wire.valid(p.codexInvocation) {
		return errors.New("Codex requested and negotiated protocol not independently observed")
	}
	r.Checks["codex_0.160.1_wire_requested_negotiated_2025-06-18"] = "PASS"
	if err := p.lifecycle(ctx, db.PG(), processes, &sequence); err != nil {
		return err
	}
	for i, old := range processes {
		select {
		case <-old.done:
			logPath := filepath.Join(dir, fmt.Sprintf("server-%d.log", i))
			if err := os.Rename(logPath, filepath.Join(dir, fmt.Sprintf("server-%d-stopped.log", i))); err != nil {
				return err
			}
			replacement, err := startServer(ctx, dir, binary, cfg, origin, certFile, keyFile, i)
			if err != nil {
				return err
			}
			processes[i] = replacement
			p.target.Processes[i] = replacement.identity
			r.Identities["replacement_process_pid"] = fmt.Sprint(replacement.cmd.Process.Pid)
		default:
		}
	}
	seenRecovery := map[string]bool{}
	for i := 0; i < 2; i++ {
		reply, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), f.Credentials["live"].Token)
		if err != nil || !successful(reply) || !contains(p.target.Processes, reply.Process) {
			return errors.New("post-drain recovery failed")
		}
		seenRecovery[reply.Process] = true
	}
	if len(seenRecovery) != 2 {
		return errors.New("post-drain recovery missed a process")
	}
	r.Checks["restarted_process_and_peer_recover_same_custody"] = "PASS"
	logs, err := filepath.Glob(filepath.Join(dir, "server-*.log"))
	if err != nil {
		return err
	}
	for _, filename := range logs {
		b, err := os.ReadFile(filename)
		if err != nil {
			return errors.New("server log unavailable; disclosure scan incomplete")
		}
		for _, canary := range f.Canaries {
			if strings.Contains(string(b), canary) {
				return errors.New("server log canary disclosure")
			}
		}
		for _, c := range f.Credentials {
			if strings.Contains(string(b), c.Token) {
				return errors.New("server log credential disclosure")
			}
		}
	}
	r.Checks["bounded_JSON_no_open_stream_claim"] = "PASS"
	r.Checks["public_candidate_acceptance"] = "NOTRUN; local private-CA development rehearsal only"
	r.Checks["outcome"] = "PASS"
	return nil
}

func startServer(ctx context.Context, dir, binary string, cfg settings, origin, cert, key string, index int) (*serverProcess, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	address := listener.Addr().String()
	listener.Close()
	operational, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	operationalAddress := operational.Addr().String()
	operational.Close()
	log, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("server-%d.log", index)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(binary, "server", "--dev", "--listen="+address, "--operational-listen="+operationalAddress, "--root-key-file="+cfg.Root, "--tls-cert-file="+cert, "--tls-key-file="+key)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "HIKYO_DB=" + cfg.DSN + "&application_name=mcp651-" + strconv.Itoa(index), "HIKYO_UPGRADE_STATE_DIR=" + cfg.State, "HIKYO_EXTERNAL_ORIGIN=" + origin, "HIKYO_MCP_ENABLED=true", "HIKYO_NODE_ID=mcp651-" + strconv.Itoa(index), "HIKYO_PG_POOL_MAX=4", "HIKYO_UPDATE_CHANNEL=off"}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, errors.New("server launch failed")
	}
	p := &serverProcess{cmd: cmd, done: make(chan error, 1), address: address, operational: operationalAddress, identity: fmt.Sprintf("process-%d", cmd.Process.Pid)}
	go func() { err := cmd.Wait(); log.Close(); p.done <- err; close(p.done) }()
	if err := wait(ctx, 30*time.Second, func(ctx context.Context) bool {
		c, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}); err != nil {
		stopServer(p, 7*time.Second)
		return nil, errors.New("server readiness failed; protected process log retained")
	}
	return p, nil
}

func wait(ctx context.Context, budget time.Duration, ready func(ctx context.Context) bool) error {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		readyNow := ready(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		if readyNow {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func localCertificate(dir string) (string, string, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "651 local rehearsal CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "local-ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600); err != nil {
		return "", "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "651 local rehearsal only"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return "", "", err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	certFile, keyFile := filepath.Join(dir, "local-cert.pem"), filepath.Join(dir, "local-tls.key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

func (p *probe) firstDenials(ctx context.Context, token string, revoked bool) error {
	seen := map[string]bool{}
	var first response
	for i := 0; i < 2; i++ {
		r, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), token)
		if err != nil || !contains(p.target.Processes, r.Process) || seen[r.Process] {
			return errors.New("first post-commit probe identity failed")
		}
		seen[r.Process] = true
		if revoked {
			if r.Status != 401 || r.Challenge != "Bearer" {
				return errors.New("first post-revocation call was not denied")
			}
			if !sameDenial(r, p.authenticationDenial) {
				return errors.New("revoked denial differs from absent/invalid oracle")
			}
		} else if r.Status != 200 || !r.Result.IsError {
			return errors.New("first post-grant-removal call was not denied")
		}
		if !revoked && !sameDenial(r, p.scopeDenial) {
			return errors.New("grant-removal denial differs from scope oracle")
		}
		alias := "a"
		if r.Process == p.target.Processes[1] {
			alias = "b"
		}
		phase := "first_post_grant_removal_request"
		if revoked {
			phase = "first_post_revocation_request"
		}
		p.receipt.event(phase, alias, r.Status)
		if i == 0 {
			first = r
		} else if !sameDenial(first, r) {
			return errors.New("post-commit denial differs across processes")
		}
	}
	if revoked {
		for i := 0; i < 2; i++ {
			r, err := p.call(ctx, "tools/call", mcpserver.ToolListDefinitions, p.args(), p.fixture.Credentials["live"].Token)
			if err != nil || !successful(r) {
				return errors.New("replacement failed after committed revocation")
			}
		}
	}
	return nil
}

func fileHash(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
