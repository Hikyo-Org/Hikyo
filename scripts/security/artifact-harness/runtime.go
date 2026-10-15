package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Only explicitly selected runtime settings reach the target. No inherited
// GitHub token, cloud credential, proxy or Hikyo deployment settings survive.
func cleanEnv(dir string) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "TMPDIR=" + dir, "HIKYO_UPDATE_CHANNEL=off"}
}
func command(ctx context.Context, binary, dir string, env []string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, binary, args...)
	c.Dir = dir
	c.Env = append(cleanEnv(dir), env...)
	var out limitedBuffer
	c.Stdout = &out
	c.Stderr = io.Discard
	err := c.Run()
	return out.Bytes(), err
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > 1<<20 {
		return 0, errors.New("subprocess output limit")
	}
	_, err := b.Buffer.Write(p)
	return n, err
}

type response struct {
	status  int
	body    []byte
	headers http.Header
}
type target struct {
	ctx               context.Context
	binary, dir, base string
	operational       string
	env               []string
	client            *http.Client
	process           *exec.Cmd
	done              chan error
	authTimes         []time.Time
	mcpTimes          []time.Time
}

func unusedAddress() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	a := l.Addr().String()
	err = l.Close()
	return a, err
}

func ownedDisposableDatabaseIdentifier(database string) (string, error) {
	nonce, owned := strings.CutPrefix(database, "hikyo_security_")
	if !owned || len(nonce) != 24 {
		return "", errors.New("invalid owned disposable database name")
	}
	decoded, err := hex.DecodeString(nonce)
	if err != nil || hex.EncodeToString(decoded) != nonce {
		return "", errors.New("invalid owned disposable database name")
	}
	return pgx.Identifier{database}.Sanitize(), nil
}

func createOwnedDisposableDatabase(ctx context.Context, conn *pgx.Conn, database string) error {
	identifier, err := ownedDisposableDatabaseIdentifier(database)
	if err != nil {
		return err
	}
	_, err = conn.Exec(ctx, "CREATE DATABASE "+identifier)
	return err
}

func dropOwnedDisposableDatabase(ctx context.Context, conn *pgx.Conn, database string) error {
	identifier, err := ownedDisposableDatabaseIdentifier(database)
	if err != nil {
		return err
	}
	_, err = conn.Exec(ctx, "DROP DATABASE "+identifier)
	return err
}

func newTarget(ctx context.Context, o options, engine string) (t *target, cleanup func() error, err error) {
	dir, err := os.MkdirTemp("", "hikyo-artifact-")
	if err != nil {
		return nil, nil, err
	}
	var conn *pgx.Conn
	var database string
	databaseCreated := false
	cleanup = func() error {
		var cleanupErr error
		if t != nil {
			cleanupErr = t.stop()
		}
		if conn != nil {
			cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var dropErr error
			if databaseCreated {
				dropErr = dropOwnedDisposableDatabase(cctx, conn, database)
			}
			cleanupErr = errors.Join(cleanupErr, dropErr, conn.Close(cctx))
		}
		return errors.Join(cleanupErr, os.RemoveAll(dir))
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, cleanup())
		}
	}()
	db := "sqlite:" + filepath.Join(dir, "hikyo.db")
	if engine == "postgres" {
		u, connectionConfig, validateErr := validatePostgresDSN(o.postgres)
		if validateErr != nil {
			return nil, cleanup, validateErr
		}
		conn, err = pgx.ConnectConfig(ctx, connectionConfig)
		if err != nil {
			return nil, cleanup, errors.New("connect disposable PostgreSQL")
		}
		var nonce [12]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return nil, cleanup, err
		}
		database = "hikyo_security_" + hex.EncodeToString(nonce[:])
		if err = createOwnedDisposableDatabase(ctx, conn, database); err != nil {
			return nil, cleanup, errors.New("create isolated PostgreSQL database (CREATEDB permission required)")
		}
		databaseCreated = true
		u.Path = "/" + database
		u.RawPath = ""
		db = u.String()
	}
	address, err := unusedAddress()
	if err != nil {
		return nil, cleanup, err
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, cleanup, err
	}
	publicOrigin := "https://" + net.JoinHostPort("localhost", port)
	operational, err := unusedAddress()
	if err != nil {
		return nil, cleanup, err
	}
	pool, err := certificate(dir)
	if err != nil {
		return nil, cleanup, err
	}
	root := make([]byte, 32)
	if _, err = rand.Read(root); err != nil {
		return nil, cleanup, err
	}
	if err = os.WriteFile(filepath.Join(dir, "root"), []byte(hex.EncodeToString(root)), 0600); err != nil {
		return nil, cleanup, err
	}
	operatorKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, cleanup, err
	}
	operatorDER, err := x509.MarshalPKIXPublicKey(&operatorKey.PublicKey)
	if err != nil {
		return nil, cleanup, err
	}
	if err = os.WriteFile(filepath.Join(dir, "operator.pub"), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: operatorDER}), 0600); err != nil {
		return nil, cleanup, err
	}
	t = &target{ctx: ctx, binary: o.binary, dir: dir, base: publicOrigin, operational: "http://" + operational, env: []string{"HIKYO_DB=" + db, "HIKYO_ROOT_KEY_FILE=" + filepath.Join(dir, "root"), "HIKYO_LISTEN=" + address, "HIKYO_OPERATIONAL_LISTEN=" + operational, "HIKYO_EXTERNAL_ORIGIN=" + publicOrigin, "HIKYO_TLS_CERT_FILE=" + filepath.Join(dir, "cert.pem"), "HIKYO_TLS_KEY_FILE=" + filepath.Join(dir, "key.pem"), "HIKYO_MCP_ENABLED=true"}, client: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if o.bundle != "" {
		t.env = append(t.env, "HIKYO_UPGRADE_BUNDLE="+o.bundle, "HIKYO_UPGRADE_STATE_DIR="+dir, "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY="+filepath.Join(dir, "operator.pub"))
	}
	return t, cleanup, nil
}

func validatePostgresDSN(raw string) (*url.URL, *pgx.ConnConfig, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || !loopbackHost(u.Hostname()) || u.User == nil || u.User.Username() == "" || u.Path == "" || u.Path == "/" || u.Fragment != "" {
		return nil, nil, errors.New("PostgreSQL requires an explicit loopback admin URL with user and database")
	}
	// libpq query parameters can override URL host, role and database. Only
	// explicitly supported TLS/connection knobs are admitted, before opening
	// any connection or creating any database.
	allowed := map[string]bool{"sslmode": true, "sslrootcert": true, "sslcert": true, "sslkey": true, "connect_timeout": true}
	for key, values := range u.Query() {
		if !allowed[key] || len(values) != 1 {
			return nil, nil, errors.New("PostgreSQL DSN contains an unsupported target override or option")
		}
	}
	config, err := pgx.ParseConfig(raw)
	if err != nil {
		return nil, nil, errors.New("PostgreSQL fixture DSN invalid")
	}
	port := uint64(5432)
	if u.Port() != "" {
		port, err = strconv.ParseUint(u.Port(), 10, 16)
		if err != nil || port == 0 {
			return nil, nil, errors.New("PostgreSQL fixture port invalid")
		}
	}
	password, _ := u.User.Password()
	if config.Host != u.Hostname() || uint64(config.Port) != port || config.Database != strings.TrimPrefix(u.Path, "/") || config.User != u.User.Username() || config.Password != password {
		return nil, nil, errors.New("PostgreSQL effective connection differs from explicit fixture URL")
	}
	for _, fallback := range config.Fallbacks {
		if fallback.Host != config.Host || fallback.Port != config.Port {
			return nil, nil, errors.New("PostgreSQL fallback escapes explicit fixture address")
		}
	}
	for key := range config.RuntimeParams {
		if key != "application_name" {
			return nil, nil, errors.New("PostgreSQL ambient options alter fixture configuration")
		}
	}
	config.RuntimeParams = map[string]string{"application_name": "hikyo-artifact-harness-control"}
	return u, config, nil
}
func loopbackHost(host string) bool {
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}
func certificate(dir string) (*x509.CertPool, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "synthetic-artifact-target"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err = os.WriteFile(filepath.Join(dir, "cert.pem"), certPEM, 0600); err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(dir, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		return nil, errors.New("trust private certificate")
	}
	return pool, nil
}
func (t *target) start() error {
	t.process = exec.CommandContext(t.ctx, t.binary, "server")
	t.process.Dir = t.dir
	t.process.Env = append(cleanEnv(t.dir), t.env...)
	t.process.Stdout = io.Discard
	t.process.Stderr = io.Discard
	if err := t.process.Start(); err != nil {
		return errors.New("start supplied production server")
	}
	t.done = make(chan error, 1)
	p, done := t.process, t.done
	go func() { done <- p.Wait() }()
	return t.waitReady()
}

func (t *target) waitReady() error {
	timer := time.NewTimer(35 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return errors.New("server readiness deadline")
		case <-timer.C:
			return errors.New("server not ready within 35s")
		case <-t.done:
			t.process = nil
			return errors.New("supplied server exited before readiness")
		case <-ticker.C:
			r, err := http.NewRequestWithContext(t.ctx, "GET", t.operational+"/readyz", nil)
			if err != nil {
				return err
			}
			response, err := t.client.Do(r)
			if err == nil {
				response.Body.Close()
			}
			if err == nil && response.StatusCode == 200 {
				return nil
			}
		}
	}
}
func (t *target) stop() error {
	if t.process == nil {
		return nil
	}
	p := t.process
	t.process = nil
	_ = p.Process.Signal(os.Interrupt)
	select {
	case <-t.done:
		return nil
	case <-time.After(10 * time.Second):
		_ = p.Process.Kill()
		select {
		case <-t.done:
			return nil
		case <-time.After(5 * time.Second):
			return errors.New("target process cleanup failed")
		}
	}
}
func (t *target) request(method, path, token string, body any) (response, error) {
	// Account ceremony starts and finishes share production's ten-attempt
	// rolling source-IP allowance. Stay below it before sending, without
	// replaying a mutation or changing the server's limiter configuration.
	if path == "/mcp" {
		if err := pace(t.ctx, &t.mcpTimes, 50); err != nil {
			return response{}, err
		}
	}
	if method == "POST" && strings.HasPrefix(path, "/api/v1/auth/") {
		if err := t.paceAuthentication(); err != nil {
			return response{}, err
		}
	}
	var reader io.Reader
	var encoded []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return response{}, err
		}
		reader = bytes.NewReader(data)
		encoded = data
	}
	r, err := http.NewRequestWithContext(t.ctx, method, t.base+path, reader)
	if err != nil {
		return response{}, errors.New("construct target request")
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Hikyo-API-Revision", "3")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if path == "/mcp" {
		r.Header.Set("MCP-Protocol-Version", "2026-07-28")
		r.Header.Set("Accept", "application/json, text/event-stream")
		var mirror struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.Unmarshal(encoded, &mirror) != nil {
			return response{}, errors.New("construct MCP metadata mirror")
		}
		r.Header.Set("Mcp-Method", mirror.Method)
		r.Header.Set("Mcp-Name", mirror.Params.Name)
	}
	rsp, err := t.client.Do(r)
	if err != nil {
		return response{}, errors.New("target HTTP request unavailable")
	}
	defer rsp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(rsp.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return response{}, errors.New("target response limit/read failure")
	}
	return response{status: rsp.StatusCode, body: data, headers: rsp.Header.Clone()}, nil
}

func (t *target) paceAuthentication() error { return pace(t.ctx, &t.authTimes, 8) }
func pace(ctx context.Context, times *[]time.Time, limit int) error {
	now := time.Now()
	kept := (*times)[:0]
	for _, at := range *times {
		if now.Sub(at) < time.Minute {
			kept = append(kept, at)
		}
	}
	*times = kept
	if len(*times) >= limit {
		wait := time.Until((*times)[0].Add(time.Minute)) + 100*time.Millisecond
		select {
		case <-ctx.Done():
			return errors.New("request pacing deadline")
		case <-time.After(wait):
		}
		now = time.Now()
		kept = (*times)[:0]
		for _, at := range *times {
			if now.Sub(at) < time.Minute {
				kept = append(kept, at)
			}
		}
		*times = kept
	}
	*times = append(*times, time.Now())
	return nil
}

func (t *target) expect(method, path, token string, body any, status int, out any) error {
	r, err := t.request(method, path, token, body)
	if err != nil {
		return err
	}
	if r.status != status {
		return fmt.Errorf("%s %s status %d, expected %d (body withheld)", method, path, r.status, status)
	}
	if out != nil {
		if err := json.Unmarshal(r.body, out); err != nil {
			return errors.New("target response shape invalid")
		}
	}
	return nil
}
func (t *target) rpc(token, org, project, cursor string) (response, error) {
	args := map[string]any{"org_id": org, "project_id": project, "page_size": 1}
	if cursor != "" {
		args["cursor"] = cursor
	}
	return t.rpcTool(token, "hikyo_list_definitions", args)
}

func (t *target) rpcTool(token, tool string, args map[string]any) (response, error) {
	meta := map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": map[string]any{}, "io.modelcontextprotocol/clientInfo": map[string]string{"name": "hikyo-artifact-harness", "version": "1"}}
	return t.request("POST", "/mcp", token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args, "_meta": meta}})
}

func (t *target) inspect(token, org, project, environment string) (response, error) {
	return t.rpcTool(token, "hikyo_inspect_configuration", map[string]any{"org_id": org, "project_id": project, "environment_id": environment, "page_size": 10})
}
func noCanary(body []byte, canaries []string) error {
	// Decode escapes before scanning. A response containing \u0066ixture must
	// not evade an oracle for fixture metadata just because its wire spelling differs.
	var decoded any
	if json.Unmarshal(body, &decoded) == nil {
		var walk func(any) bool
		walk = func(value any) bool {
			switch v := value.(type) {
			case string:
				for _, c := range canaries {
					if c != "" && strings.Contains(v, c) {
						return true
					}
				}
			case []any:
				for _, item := range v {
					if walk(item) {
						return true
					}
				}
			case map[string]any:
				for key, item := range v {
					if walk(key) || walk(item) {
						return true
					}
				}
			}
			return false
		}
		if walk(decoded) {
			return errors.New("protected canary appeared in response")
		}
	}
	for _, c := range canaries {
		if c != "" && bytes.Contains(body, []byte(c)) {
			return errors.New("protected canary appeared in response")
		}
	}
	return nil
}
func noResponseCanary(r response, canaries []string) error {
	if err := noCanary(r.body, canaries); err != nil {
		return err
	}
	for name, values := range r.headers {
		for _, canary := range canaries {
			if canary != "" && strings.Contains(name, canary) {
				return errors.New("protected canary appeared in response header")
			}
			for _, value := range values {
				if canary != "" && strings.Contains(value, canary) {
					return errors.New("protected canary appeared in response header")
				}
			}
		}
	}
	return nil
}
func sameDenial(got, twin response, status int, canaries []string) error {
	if got.status != status || twin.status != status {
		return fmt.Errorf("denial statuses %d/%d, expected %d", got.status, twin.status, status)
	}
	if status == http.StatusOK {
		for _, r := range []response{got, twin} {
			var envelope struct {
				Error  json.RawMessage `json:"error"`
				Result struct {
					IsError bool `json:"isError"`
				} `json:"result"`
			}
			if json.Unmarshal(r.body, &envelope) != nil || !envelope.Result.IsError {
				return errors.New("MCP refusal lacked an explicit tool error")
			}
		}
	}
	if err := noResponseCanary(got, canaries); err != nil {
		return err
	}
	if !maps.EqualFunc(denialHeaders(got.headers), denialHeaders(twin.headers), func(a, b []string) bool { return slices.Equal(a, b) }) {
		return errors.New("denial response headers differ from missing twin")
	}
	if !bytes.Equal(bytes.TrimSpace(got.body), bytes.TrimSpace(twin.body)) {
		return errors.New("denial differs from genuine missing-object twin")
	}
	return nil
}

func denialHeaders(headers http.Header) map[string][]string {
	out := map[string][]string{}
	for name, values := range headers {
		key := strings.ToLower(name)
		if key == "date" {
			continue
		}
		copy := append([]string(nil), values...)
		slices.Sort(copy)
		out[key] = copy
	}
	return out
}
func successfulRPC(r response, required string) error {
	if r.status != 200 {
		return fmt.Errorf("MCP authorized HTTP status %d", r.status)
	}
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError           bool            `json:"isError"`
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	if json.Unmarshal(r.body, &envelope) != nil || len(envelope.Error) > 0 || envelope.Result.IsError || len(envelope.Result.StructuredContent) == 0 {
		return errors.New("MCP did not return a successful structured tool result")
	}
	if required != "" && !strings.Contains(string(envelope.Result.StructuredContent), required) {
		return errors.New("authorized MCP control omitted seeded definition")
	}
	return nil
}
