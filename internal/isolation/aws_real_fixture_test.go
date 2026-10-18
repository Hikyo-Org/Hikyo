package isolation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

// Real AWS is opt-in twice: a bounded operator manifest and an explicit mutation
// switch. Neither this file nor the suite provisions an account, key, or role.
// All values are synthetic. Only the external oracle can read them. Credentials
// enter through owner-only regular files, never secret-bearing flags or logs.
type realAWSActor struct {
	CredentialFile        string `json:"credential_file"`
	PrincipalARN          string `json:"principal_arn"`
	SourceFile            string `json:"source_file,omitempty"`
	SourcePrincipalARN    string `json:"source_principal_arn,omitempty"`
	TokenFile             string `json:"token_file,omitempty"`
	WorkloadRoleARN       string `json:"workload_role_arn,omitempty"`
	ExpiredCredentialFile string `json:"expired_credential_file,omitempty"`
	ExpiredSourceFile     string `json:"expired_source_file,omitempty"`
	ExpiredTokenFile      string `json:"expired_token_file,omitempty"`
}

type realAWSManifest struct {
	Account         string                  `json:"account"`
	Region          string                  `json:"region"`
	AuthorizationID string                  `json:"authorization_id"`
	BudgetUSD       float64                 `json:"budget_usd"`
	Prefix          string                  `json:"prefix"`
	KMSKeyARN       string                  `json:"kms_key_arn"`
	ExpiresAt       time.Time               `json:"expires_at"`
	Writers         map[string]realAWSActor `json:"writers"`
	Oracle          realAWSActor            `json:"oracle"`
	Cleanup         realAWSActor            `json:"cleanup"`
}

var realAWSPrefix = regexp.MustCompile(`^hikyo-e2e-[a-z0-9]{8,32}/$`)
var realAWSAccount = regexp.MustCompile(`^[0-9]{12}$`)
var realAWSRegion = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[0-9]+$`)
var realAWSKeyID = regexp.MustCompile(`^(?:[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}|mrk-[a-f0-9]{32})$`)
var realAWSName = regexp.MustCompile(`^[A-Za-z0-9/_+=.@-]{1,512}$`)
var realAWSRole = regexp.MustCompile(`^arn:aws:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$`)

func (m realAWSManifest) validate(now time.Time) error {
	if !realAWSAccount.MatchString(m.Account) || m.Account == "000000000000" || !realAWSRegion.MatchString(m.Region) || strings.HasPrefix(m.Region, "cn-") || strings.HasPrefix(m.Region, "us-gov-") {
		return errors.New("real AWS fixture requires an explicit commercial AWS account and region")
	}
	if m.AuthorizationID == "" || len(m.AuthorizationID) > 128 || m.BudgetUSD <= 0 || m.BudgetUSD > 1 {
		return errors.New("real AWS fixture requires a scope approval reference and a positive budget at most one USD")
	}
	if !realAWSPrefix.MatchString(m.Prefix) || !m.ExpiresAt.After(now) || m.ExpiresAt.After(now.Add(2*time.Hour)) {
		return errors.New("real AWS fixture requires a unique run prefix and authorization expiring within two hours")
	}
	keyID, keyScopeOK := strings.CutPrefix(m.KMSKeyARN, "arn:aws:kms:"+m.Region+":"+m.Account+":key/")
	if !keyScopeOK || !realAWSKeyID.MatchString(keyID) {
		return errors.New("real AWS fixture requires an existing customer key in the approved account and region")
	}
	if len(m.Writers) != 5 {
		return errors.New("real AWS fixture requires static, ambient, assume-role, web-identity and kms-denied writers")
	}
	if m.Writers["static"].ExpiredCredentialFile == "" || m.Writers["assume-role"].ExpiredSourceFile == "" {
		return errors.New("real AWS fixture requires protected expired temporary static and source descriptors")
	}
	actors := []realAWSActor{m.Oracle, m.Cleanup}
	for _, mode := range []string{"static", "ambient", "assume-role", "web-identity", "kms-denied"} {
		a, ok := m.Writers[mode]
		if !ok {
			return errors.New("real AWS fixture is missing a writer mode")
		}
		actors = append(actors, a)
	}
	seen := make(map[string]bool)
	for _, a := range actors {
		if a.CredentialFile == "" || !realAWSRole.MatchString(a.PrincipalARN) || !strings.HasPrefix(a.PrincipalARN, "arn:aws:iam::"+m.Account+":") || seen[a.PrincipalARN] {
			return errors.New("real AWS actors require distinct explicit roles in the sandbox account")
		}
		seen[a.PrincipalARN] = true
	}
	for _, mode := range []string{"ambient", "web-identity"} {
		a := m.Writers[mode]
		if !realAWSRole.MatchString(a.PrincipalARN) || a.TokenFile == "" || a.ExpiredTokenFile == "" || (mode == "ambient" && a.WorkloadRoleARN != a.PrincipalARN) {
			return errors.New("workload fixture requires a projected token and an explicit sandbox role")
		}
	}
	source := m.Writers["assume-role"]
	if source.SourceFile == "" || !realAWSRole.MatchString(source.PrincipalARN) || !realAWSRole.MatchString(source.SourcePrincipalARN) || !strings.HasPrefix(source.SourcePrincipalARN, "arn:aws:iam::"+m.Account+":role/") || seen[source.SourcePrincipalARN] {
		return errors.New("assume-role fixture requires a separate sandbox source role")
	}
	return nil
}

// Errors deliberately omit paths, JSON bytes, and decoder/provider messages.
func readAWSProtectedFile(path string) ([]byte, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("AWS fixture file custody currently requires POSIX permissions")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 {
		return nil, errors.New("AWS fixture input must be an owner-only regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("AWS fixture input could not be opened")
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Mode().Perm()&0077 != 0 {
		return nil, errors.New("AWS fixture input changed during open")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 32769))
	if err != nil || len(raw) > 32768 {
		crypto.Zero(raw)
		return nil, errors.New("AWS fixture input is unreadable or exceeds 32 KiB")
	}
	return raw, nil
}

func decodeAWSManifest(raw []byte) (realAWSManifest, error) {
	var m realAWSManifest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, errors.New("invalid AWS sandbox manifest")
	}
	var trailing json.RawMessage
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) {
		return m, errors.New("AWS sandbox manifest contains trailing data")
	}
	return m, nil
}

const realAWSMaxCreations = 32

type realAWSSandbox struct {
	t               *testing.T
	manifest        realAWSManifest
	dir             string
	oracle, cleanup externalAWS
	mu              sync.Mutex
	owned           map[string]string // exact ARN -> name; never delete by a reusable name
	current         map[string]string // name -> most recently recorded ARN; historical ARNs stay owned
	creations       []string          // cumulative reservations, including failed/ambiguous creates; never refunded
	replayOnly      bool              // child replay cannot create or reset the parent budget
	ops             []string          // operation names only, no headers, descriptors, or values
}

func loadRealAWSSandbox(t *testing.T) *realAWSSandbox {
	t.Helper()
	path := os.Getenv("HIKYO_TEST_AWS_REAL_MANIFEST")
	if path == "" {
		requireExternalAWS(t, "HIKYO_TEST_AWS_REAL_REQUIRED", "HIKYO_TEST_AWS_REAL_MANIFEST")
	}
	if os.Getenv("HIKYO_TEST_AWS_REAL_MUTATIONS") != "approved" {
		t.Fatal("real AWS mutations are not authorized; set the mutation switch only after the sandbox scope is approved")
	}
	// Both datastore legs are mandatory before the first provider operation.
	if os.Getenv("HIKYO_TEST_POSTGRES_DSN") == "" {
		t.Fatal("real AWS acceptance requires both datastore engines")
	}
	raw, err := readAWSProtectedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(raw)
	m, err := decodeAWSManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.validate(time.Now()); err != nil {
		t.Fatal(err)
	}
	s := &realAWSSandbox{t: t, manifest: m, dir: filepath.Dir(path), owned: make(map[string]string)}
	s.oracle = s.staticActor(t, m.Oracle)
	s.cleanup = s.staticActor(t, m.Cleanup)
	if s.oracle.creds.AccessKeyID == s.cleanup.creds.AccessKeyID {
		t.Fatal("oracle and cleanup credentials are not separate")
	}
	t.Cleanup(func() { s.oracle.creds = aws.Credentials{}; s.cleanup.creds = aws.Credentials{} })
	t.Cleanup(func() { s.clean(t) })
	// Verify every supplied identity before any test starts its first write.
	for _, mode := range []string{"static", "ambient", "assume-role", "web-identity", "kms-denied"} {
		if !t.Run("preflight-"+mode, func(t *testing.T) {
			s.validateExpiredInputs(t, mode)
			s.writer(t, mode)
		}) {
			t.Fatal("sandbox identity preflight failed before mutations")
		}
	}
	// A failed or interrupted attempt must be reconciled, not silently
	// restarted with a fresh budget for the same namespace.
	if err := claimRealAWSRun(filepath.Dir(path), m); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *realAWSSandbox) file(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(s.dir, path)
}

func (s *realAWSSandbox) descriptor(t *testing.T, a realAWSActor) string {
	t.Helper()
	raw, err := readAWSProtectedFile(s.file(a.CredentialFile))
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(raw)
	d, err := awssm.ParseDescriptor(string(raw))
	if err != nil || d.Region != s.manifest.Region || d.STSOrigin != "" {
		t.Fatal("AWS fixture descriptor must match the approved region and use AWS STS")
	}
	return string(raw)
}

func matchesAWSPrincipal(actual, expected string) bool {
	if actual == expected {
		return true
	}
	parts := strings.SplitN(expected, ":role/", 2)
	if len(parts) != 2 || !realAWSRole.MatchString(expected) {
		return false
	}
	role := filepath.Base(parts[1])
	account := strings.Split(expected, ":")[4]
	prefix := "arn:aws:sts::" + account + ":assumed-role/" + role + "/"
	return strings.HasPrefix(actual, prefix) && len(actual) > len(prefix) && !strings.Contains(strings.TrimPrefix(actual, prefix), "/")
}

func (s *realAWSSandbox) client(t *testing.T, raw string, workload bool, principal string) *awssm.Client {
	t.Helper()
	c, err := awssm.NewClient(awssm.ClientConfig{Origin: "https://secretsmanager." + s.manifest.Region + ".amazonaws.com", Credential: raw, WorkloadIdentity: workload, Deadline: 15 * time.Second})
	if err != nil {
		t.Fatal("AWS fixture client configuration refused")
	}
	t.Cleanup(c.Forget)
	id, err := c.ResolveIdentity(t.Context())
	if err != nil || id.Account != s.manifest.Account || !matchesAWSPrincipal(id.ARN, principal) {
		t.Fatal("AWS fixture identity failed or does not match the explicitly approved sandbox principal")
	}
	return c
}

func (s *realAWSSandbox) staticActor(t *testing.T, a realAWSActor) externalAWS {
	t.Helper()
	raw := s.descriptor(t, a)
	d, err := awssm.ParseDescriptor(raw)
	if err != nil || d.Mode != awssm.AuthStatic || d.SessionToken == "" {
		t.Fatal("oracle and cleanup require protected temporary static descriptors")
	}
	s.client(t, raw, false, a.PrincipalARN)
	return externalAWS{origin: "https://secretsmanager." + s.manifest.Region + ".amazonaws.com", account: s.manifest.Account, region: s.manifest.Region, creds: aws.Credentials{AccessKeyID: d.AccessKeyID, SecretAccessKey: d.SecretKey, SessionToken: d.SessionToken}}
}

// isolateAWSChain prevents an unrelated local profile, ECS endpoint, or IMDS
// identity from silently becoming the role source. Ambient mode below uses an
// actual operator-projected WebIdentity token, not fabricated credentials.
func isolateAWSChain(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "empty-aws-config")
	if err := os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatal("cannot isolate AWS config")
	}
	for _, name := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_ROLE_ARN", "AWS_ROLE_SESSION_NAME", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_CONTAINER_AUTHORIZATION_TOKEN", "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE", "AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_STS", "AWS_CA_BUNDLE"} {
		t.Setenv(name, "")
	}
	t.Setenv("AWS_CONFIG_FILE", empty)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", empty)
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "true")
}

func (s *realAWSSandbox) writer(t *testing.T, mode string) (string, *awssm.Client) {
	t.Helper()
	isolateAWSChain(t)
	a := s.manifest.Writers[mode]
	raw := s.descriptor(t, a)
	d, err := awssm.ParseDescriptor(raw)
	want := awssm.AuthMode(mode)
	if mode == "kms-denied" {
		want = awssm.AuthStatic
	}
	if err != nil || d.Mode != want || (want == awssm.AuthStatic && d.SessionToken == "") {
		t.Fatal("writer descriptor does not match its declared test mode")
	}
	if d.AccessKeyID != "" && (d.AccessKeyID == s.oracle.creds.AccessKeyID || d.AccessKeyID == s.cleanup.creds.AccessKeyID) {
		t.Fatal("writer borrows oracle or cleanup credentials")
	}
	if mode == "assume-role" {
		if a.SourceFile == "" || a.SourcePrincipalARN == "" || d.RoleARN != a.PrincipalARN || d.ExternalID == "" {
			t.Fatal("assume-role requires an explicit approved source and destination")
		}
		source := s.staticActor(t, realAWSActor{CredentialFile: a.SourceFile, PrincipalARN: a.SourcePrincipalARN})
		if source.creds.AccessKeyID == s.oracle.creds.AccessKeyID || source.creds.AccessKeyID == s.cleanup.creds.AccessKeyID {
			t.Fatal("role source borrows oracle or cleanup credentials")
		}
		t.Setenv("AWS_ACCESS_KEY_ID", source.creds.AccessKeyID)
		t.Setenv("AWS_SECRET_ACCESS_KEY", source.creds.SecretAccessKey)
		t.Setenv("AWS_SESSION_TOKEN", source.creds.SessionToken)
	}
	if mode == "ambient" || mode == "web-identity" {
		role := d.RoleARN
		if mode == "ambient" {
			role = a.WorkloadRoleARN
		}
		if a.TokenFile == "" || role != a.PrincipalARN {
			t.Fatal("workload modes require an approved projected token and role")
		}
		token, err := readAWSProtectedFile(s.file(a.TokenFile))
		if err != nil {
			t.Fatal(err)
		}
		if len(bytes.TrimSpace(token)) == 0 {
			t.Fatal("projected workload token is empty")
		}
		crypto.Zero(token)
		t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", s.file(a.TokenFile))
		t.Setenv("AWS_ROLE_ARN", role)
		t.Setenv("AWS_ROLE_SESSION_NAME", "hikyo-acceptance")
	}
	return raw, s.client(t, raw, d.Mode != awssm.AuthStatic, a.PrincipalARN)
}

func (s *realAWSSandbox) checkName(name string) error {
	if !realAWSName.MatchString(name) || !strings.HasPrefix(name, s.manifest.Prefix) || len(name) <= len(s.manifest.Prefix) {
		return errors.New("AWS test refused a name outside its approved run")
	}
	return nil
}

// Reserve before either writer or cleanup can send CreateSecret. Reserving
// again for the same name consumes another slot: replacement, failures and
// retries must never manufacture additional resource allowance.
func (s *realAWSSandbox) reserveCreate(name string) error {
	if s.replayOnly {
		return errors.New("replay child cannot create resources")
	}
	if err := s.checkName(name); err != nil {
		return err
	}
	if !s.manifest.ExpiresAt.After(time.Now()) {
		return errors.New("sandbox mutation authorization expired")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.creations) >= realAWSMaxCreations {
		return errors.New("AWS full-suite creation budget exhausted")
	}
	s.creations = append(s.creations, name)
	if s.t != nil {
		s.t.Logf("reserved synthetic AWS create: %d %s", len(s.creations), name)
	}
	return nil
}

// This nonsecret single-use marker belongs in the operator's protected fixture
// directory. Keep it after success, failure and process exit. A fresh process
// cannot silently reset the namespace's budget. Reconciliation/new scope is
// required for a rerun; the harness never deletes the marker.
func claimRealAWSRun(dir string, m realAWSManifest) error {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("AWS fixture directory must be private and writable for its single-use marker")
	}
	sum := sha256.Sum256([]byte(m.Account + "\n" + m.Region + "\n" + m.Prefix))
	name := filepath.Join(dir, ".hikyo-aws-run-"+hex.EncodeToString(sum[:])+".used")
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("AWS run is already claimed or cannot create its single-use marker; reconcile before using a new approved namespace")
	}
	_, writeErr := f.WriteString("Single-use AWS fixture namespace. Do not remove to restart a run.\n")
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("AWS run marker could not be persisted; namespace remains reserved")
	}
	return nil
}

func (s *realAWSSandbox) remember(name, arn string) error {
	if err := s.checkName(name); err != nil {
		return err
	}
	prefix := "arn:aws:secretsmanager:" + s.manifest.Region + ":" + s.manifest.Account + ":secret:" + name + "-"
	if !strings.HasPrefix(arn, prefix) || len(strings.TrimPrefix(arn, prefix)) != 6 {
		return errors.New("AWS fixture returned an unexpected resource ARN")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reserved := false
	for _, candidate := range s.creations {
		if candidate == name {
			reserved = true
			break
		}
	}
	if !reserved {
		return errors.New("AWS resource has no creation reservation in this run")
	}
	s.owned[arn] = name
	if s.current == nil {
		s.current = make(map[string]string)
	}
	s.current[name] = arn
	if s.t != nil {
		s.t.Logf("recorded synthetic AWS resource: %s", arn)
	}
	return nil
}

type realAWSResponseError struct {
	code   string
	status int
}

func (e *realAWSResponseError) Error() string {
	return fmt.Sprintf("AWS test request refused (HTTP %d)", e.status)
}
func realAWSCode(err error, code string) bool {
	var e *realAWSResponseError
	return errors.As(err, &e) && e.code == code
}

// Only the oracle can read. Only cleanup can seed external synthetic edits or
// force-delete recorded ARNs. This unrestricted signer never enters production.
func (s *realAWSSandbox) call(ctx context.Context, oracle bool, operation, id string, in map[string]any, out any) error {
	env := s.cleanup
	if oracle {
		env = s.oracle
		if operation != "GetSecretValue" {
			return errors.New("operation outside oracle policy")
		}
	} else {
		switch operation {
		case "CreateSecret", "PutSecretValue", "DescribeSecret", "DeleteSecret":
		default:
			return errors.New("operation outside cleanup fixture policy")
		}
	}
	if !oracle && operation != "DeleteSecret" && operation != "DescribeSecret" && !s.manifest.ExpiresAt.After(time.Now()) {
		return errors.New("sandbox mutation authorization expired")
	}
	if operation == "CreateSecret" {
		if err := s.reserveCreate(id); err != nil {
			return err
		}
		in["Name"] = id
	} else {
		s.mu.Lock()
		_, known := s.owned[id]
		s.mu.Unlock()
		if !known {
			return errors.New("AWS test refused an unrecorded resource ARN")
		}
		in["SecretId"] = id
	}
	err := requestAWSFixture(ctx, env, operation, in, out)
	if operation != "DescribeSecret" || !realAWSCode(err, "ResourceNotFoundException") {
		logRealAWSDiagnostic(s.t, operation, err)
	}
	return err
}

// The raw signer stays outside production. Callers must first bind the exact
// sandbox actor, operation and recorded synthetic ARN; it adds no adapter API.
func requestAWSFixture(ctx context.Context, env externalAWS, operation string, in map[string]any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return errors.New("cannot encode AWS fixture request")
	}
	defer crypto.Zero(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.origin+"/", bytes.NewReader(body))
	if err != nil {
		return errors.New("cannot build AWS fixture request")
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager."+operation)
	sum := sha256.Sum256(body)
	if err := v4.NewSigner().SignHTTP(ctx, env.creds, req, hex.EncodeToString(sum[:]), "secretsmanager", env.region, time.Now().UTC()); err != nil {
		return errors.New("cannot sign AWS fixture request")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("AWS fixture transport failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	defer crypto.Zero(raw)
	if err != nil || len(raw) > 1<<20 {
		return errors.New("AWS fixture response unreadable or too large")
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Type string `json:"__type"`
		}
		_ = json.Unmarshal(raw, &e)
		code := e.Type
		if i := strings.LastIndex(code, "#"); i >= 0 {
			code = code[i+1:]
		}
		return &realAWSResponseError{code: code, status: resp.StatusCode}
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return errors.New("AWS fixture response invalid")
	}
	return nil
}

func (s *realAWSSandbox) arn(t *testing.T, name string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if arn, ok := s.current[name]; ok {
		return arn
	}
	t.Fatal("AWS fixture has not recorded the expected resource")
	return ""
}

func (s *realAWSSandbox) value(t *testing.T, name string) string {
	t.Helper()
	var out struct {
		SecretString string `json:"SecretString"`
	}
	if err := s.call(t.Context(), true, "GetSecretValue", s.arn(t, name), map[string]any{"VersionStage": "AWSCURRENT"}, &out); err != nil {
		t.Fatal("synthetic-value oracle failed")
	}
	return out.SecretString
}

func (s *realAWSSandbox) clean(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	owned := make(map[string]string, len(s.owned))
	for arn, name := range s.owned {
		owned[arn] = name
	}
	s.mu.Unlock()
	for arn := range owned {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		err := s.call(ctx, false, "DeleteSecret", arn, map[string]any{"ForceDeleteWithoutRecovery": true}, nil)
		if err != nil && !realAWSCode(err, "ResourceNotFoundException") {
			cancel()
			t.Errorf("sandbox cleanup refused for recorded ARN %s", arn)
			continue
		}
		gone := false
		for ctx.Err() == nil {
			err = s.call(ctx, false, "DescribeSecret", arn, map[string]any{}, nil)
			if realAWSCode(err, "ResourceNotFoundException") {
				gone = true
				break
			}
			if err != nil {
				break
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
		cancel()
		if !gone {
			t.Errorf("sandbox cleanup not verified for recorded ARN %s", arn)
		} else {
			t.Logf("verified synthetic AWS resource absent: %s", arn)
		}
	}
}

// The wrapper exposes exactly the production API. It records operation names
// and guards the scope before mutations; it adds no read-value capability.
type realAWSWriter struct {
	awssm.API
	t                 *testing.T
	s                 *realAWSSandbox
	losePut           *bool
	afterPut          func(context.Context, string) error
	failCurrentMarker *bool
}

func (a *realAWSWriter) Region() string { return a.s.manifest.Region }

func (a *realAWSWriter) DescribeSecret(ctx context.Context, id string) (awssm.SecretMetadata, error) {
	if !strings.HasPrefix(id, "arn:") {
		if err := a.s.checkName(id); err != nil {
			return awssm.SecretMetadata{}, err
		}
	} else {
		a.s.mu.Lock()
		_, known := a.s.owned[id]
		a.s.mu.Unlock()
		if !known {
			return awssm.SecretMetadata{}, errors.New("metadata request outside recorded sandbox resources")
		}
	}
	meta, err := a.API.DescribeSecret(ctx, id)
	if !awssm.IsNotFound(err) {
		logRealAWSDiagnostic(a.t, "DescribeSecret", err)
	}
	return meta, err
}

func (a *realAWSWriter) ListSecretNames(ctx context.Context, prefix string, limit int) ([]string, error) {
	if !strings.HasPrefix(prefix, a.s.manifest.Prefix) {
		return nil, errors.New("metadata listing outside sandbox prefix")
	}
	return a.API.ListSecretNames(ctx, prefix, limit)
}

func (a *realAWSWriter) CreateSecret(ctx context.Context, in awssm.CreateSecretInput) error {
	if err := a.s.reserveCreate(in.Name); err != nil {
		return err
	}
	// Refuse occupied names before create; never claim an unrelated fixture.
	if _, err := a.DescribeSecret(ctx, in.Name); !awssm.IsNotFound(err) {
		return errors.New("sandbox name is not provably unused")
	}
	err := a.API.CreateSecret(ctx, in)
	logRealAWSDiagnostic(a.t, "CreateSecret", err)
	meta, describeErr := a.DescribeSecret(ctx, in.Name)
	if describeErr == nil && meta.Tags[adapter.SentinelName] == in.Tags[adapter.SentinelName] && in.Tags[adapter.SentinelName] != "" {
		if rememberErr := a.s.remember(in.Name, meta.ARN); rememberErr != nil {
			return rememberErr
		}
	} else if err == nil || !awssm.IsDefinite(err) {
		// An ambiguous create can outlive this process. Report only its
		// synthetic name; never force-delete a name without a verified ARN.
		if a.s.t != nil {
			a.s.t.Errorf("unresolved synthetic AWS create; reconcile name %s before rerunning", in.Name)
		}
		if err == nil {
			return errors.New("created resource identity could not be recorded for cleanup")
		}
	}
	return err
}

func (a *realAWSWriter) mutation(id, operation string) error {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	if _, ok := a.s.owned[id]; !ok {
		return errors.New("writer mutation outside the recorded sandbox ARNs")
	}
	if !a.s.manifest.ExpiresAt.After(time.Now()) {
		return errors.New("sandbox mutation authorization expired")
	}
	a.s.ops = append(a.s.ops, operation)
	return nil
}
func (a *realAWSWriter) PutSecretValue(ctx context.Context, id, token, value string) error {
	if err := a.mutation(id, "PutSecretValue"); err != nil {
		return err
	}
	if err := a.API.PutSecretValue(ctx, id, token, value); err != nil {
		logRealAWSDiagnostic(a.t, "PutSecretValue", err)
		return err
	}
	if a.afterPut != nil {
		if err := a.afterPut(ctx, id); err != nil {
			return err
		}
	}
	if a.losePut != nil && *a.losePut {
		*a.losePut = false
		return errors.New("test lost response after the real provider accepted the value")
	}
	return nil
}
func (a *realAWSWriter) UpdateSecretVersionStage(ctx context.Context, id, stage, to, from string) error {
	if err := a.mutation(id, "UpdateSecretVersionStage"); err != nil {
		return err
	}
	if stage == awssm.CurrentStage && a.failCurrentMarker != nil && *a.failCurrentMarker {
		*a.failCurrentMarker = false
		// Local deterministic refusal AFTER a real successful promotion.
		// This is replay fault injection, not evidence of an AWS IAM denial.
		return &awssm.ResponseError{Status: 400, Code: "InvalidRequestException"}
	}
	err := a.API.UpdateSecretVersionStage(ctx, id, stage, to, from)
	logRealAWSDiagnostic(a.t, "UpdateSecretVersionStage", err)
	return err
}
func (a *realAWSWriter) TagSecret(ctx context.Context, id string, tags map[string]string) error {
	if err := a.mutation(id, "TagResource"); err != nil {
		return err
	}
	err := a.API.TagSecret(ctx, id, tags)
	logRealAWSDiagnostic(a.t, "TagResource", err)
	return err
}
func (a *realAWSWriter) RestoreSecret(ctx context.Context, id string) error {
	if err := a.mutation(id, "RestoreSecret"); err != nil {
		return err
	}
	err := a.API.RestoreSecret(ctx, id)
	logRealAWSDiagnostic(a.t, "RestoreSecret", err)
	return err
}
func (a *realAWSWriter) DeleteSecret(ctx context.Context, id string) error {
	if err := a.mutation(id, "DeleteSecret"); err != nil {
		return err
	}
	err := a.API.DeleteSecret(ctx, id)
	logRealAWSDiagnostic(a.t, "DeleteSecret", err)
	return err
}
