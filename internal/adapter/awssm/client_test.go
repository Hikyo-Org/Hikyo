package awssm

import (
	"context"
	"crypto/x509"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm/awssmtest"
)

// The API is the import boundary Sync can link. Its exact method set is
// value-blind, and the client can only sign operations in the closed registry.
func TestNoValueReadPathExists(t *testing.T) {
	typeOf := reflect.TypeOf((*API)(nil)).Elem()
	got := make([]string, 0, typeOf.NumMethod())
	for i := range typeOf.NumMethod() {
		got = append(got, typeOf.Method(i).Name)
	}
	want := []string{"CreateSecret", "DeleteSecret", "DescribeSecret", "ListSecretNames", "PutSecretValue", "ResolveIdentity", "RestoreSecret", "TagSecret"}
	if !slices.Equal(got, want) {
		t.Fatalf("linked AWS API operations = %v, want closed value-blind set %v", got, want)
	}
	targets := make([]string, 0, len(operationRegistry))
	for _, target := range operationRegistry {
		targets = append(targets, target)
	}
	slices.Sort(targets)
	wantTargets := []string{"CreateSecret", "DeleteSecret", "DescribeSecret", "ListSecrets", "PutSecretValue", "RestoreSecret", "TagResource"}
	if !slices.Equal(targets, wantTargets) {
		t.Fatalf("operation registry = %v, want %v", targets, wantTargets)
	}
}

// A source scan backs the reflection check: no identifier or string literal
// in the production package may name a value-returning or unrecoverable
// operation, whatever helper it hides behind. Comments are not code.
func TestProductionSourceNeverNamesAValueRead(t *testing.T) {
	forbidden := []string{"GetSecretValue", "BatchGetSecretValue", "GetRandomPassword", "ForceDeleteWithoutRecovery", "ListSecretVersionIds"}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	scanned := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		scanned++
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			var text string
			switch n := node.(type) {
			case *ast.Ident:
				text = n.Name
			case *ast.BasicLit:
				text = n.Value
			default:
				return true
			}
			for _, name := range forbidden {
				if strings.Contains(text, name) {
					t.Errorf("%s references forbidden operation %s", file, name)
				}
			}
			return true
		})
	}
	if scanned < 3 {
		t.Fatalf("scanned %d production files; the scan lost its subject", scanned)
	}
}

func staticDescriptor(region string) string {
	return `{"mode":"static","region":"` + region + `","access_key_id":"AKIAHIKYOTEST0000001","secret_access_key":"test-secret-access-key"}`
}

func emulatorClient(t *testing.T, server *awssmtest.Server) *Client {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client, err := NewClient(ClientConfig{
		Origin: server.URL, Credential: staticDescriptor(server.Region), Deadline: 5 * time.Second,
		AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, RootCAs: roots,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Forget)
	return client
}

func TestSignedWireLifecycleAgainstEmulator(t *testing.T) {
	server := awssmtest.New(testAccount, testRegion)
	defer server.Close()
	client := emulatorClient(t, server)
	module := &Module{API: client}
	journal := newFakeJournal()
	target := jsonTarget()
	target.Destination.Environment = ""
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest(), JobID: "job_1"}, journal); err != nil {
		t.Fatal(err)
	}
	value, ok := server.Value("prod/app")
	if !ok || !strings.Contains(value, `"LOG_LEVEL":"debug\nmultiline é"`) {
		t.Fatalf("delivered value = %q %v", value, ok)
	}
	if server.Tag("prod/app", adapter.SentinelName) != testTarget {
		t.Fatal("ownership tag missing on the wire")
	}
	// Replay of the same job is idempotent on the wire, too.
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest(), Ledger: journal.ledger(), JobID: "job_1"}, journal); err != nil {
		t.Fatal(err)
	}
	if server.Versions("prod/app") != 1 {
		t.Fatalf("versions = %d, want 1", server.Versions("prod/app"))
	}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Ledger: journal.ledger(), Teardown: true, JobID: "job_2"}, journal); err != nil {
		t.Fatal(err)
	}
	if !server.Deleted("prod/app") {
		t.Fatal("teardown did not schedule deletion")
	}
	for _, operation := range server.Operations() {
		if strings.Contains(operation, "GetSecretValue") {
			t.Fatalf("value read on the wire: %v", server.Operations())
		}
	}
}

func TestWireErrorClassification(t *testing.T) {
	server := awssmtest.New(testAccount, testRegion)
	defer server.Close()
	client := emulatorClient(t, server)
	server.FailNext("DescribeSecret", 400, "AccessDeniedException", "")
	if _, err := client.DescribeSecret(t.Context(), "x"); !errors.Is(err, adapter.ErrProviderAuth) || !IsDefinite(err) {
		t.Fatalf("access denied = %v", err)
	}
	server.FailNext("DescribeSecret", 400, "ThrottlingException", "7")
	_, err := client.DescribeSecret(t.Context(), "x")
	at, ok := adapter.ProviderRetryAt(err)
	if !errors.Is(err, adapter.ErrRateLimited) || !ok || time.Until(at) < 5*time.Second {
		t.Fatalf("throttle = %v retry-at=%v", err, at)
	}
	if _, err := client.DescribeSecret(t.Context(), "missing"); !IsNotFound(err) {
		t.Fatalf("not found = %v", err)
	}
	server.FailNext("PutSecretValue", 500, "InternalServiceError", "")
	if err := client.PutSecretValue(t.Context(), "x", strings.Repeat("a", 64), "v"); IsDefinite(err) {
		t.Fatalf("5xx must be ambiguous, got definite %v", err)
	}
}

func TestErrorsNeverEchoProviderMessages(t *testing.T) {
	if got := errorCode("com.amazonaws.secretsmanager#ResourceNotFoundException:http://internal/"); got != "ResourceNotFoundException" {
		t.Fatalf("errorCode = %q", got)
	}
	if got := errorCode("value was s3cr3t with spaces"); got != "" {
		t.Fatalf("free text leaked as an error code: %q", got)
	}
}

func TestDescriptorContract(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want string
	}{
		{`{"mode":"static","access_key_id":"AKIAHIKYOTEST0000001"}`, "secret_access_key"},
		{`{"mode":"assume-role"}`, "role ARN"},
		{`{"mode":"assume-role","role_arn":"arn:aws:iam::123456789012:role/r","session_seconds":7200}`, "session_seconds"},
		{`{"mode":"ambient","role_arn":"arn:aws:iam::123456789012:role/r"}`, "no role"},
		{`{"mode":"web-identity","role_arn":"arn:aws:iam::123456789012:role/r","external_id":"xx"}`, "external id"},
		{`{"mode":"static","secret_access_key":"super-secret","unknown":1}`, "not a valid JSON"},
		{`not json super-secret`, "not a valid JSON"},
	} {
		_, err := ParseDescriptor(tt.raw)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("ParseDescriptor(%s) = %v, want %q", tt.raw, err, tt.want)
		}
		if err != nil && strings.Contains(err.Error(), "super-secret") {
			t.Errorf("descriptor error echoed secret material: %v", err)
		}
	}
	for _, suffix := range []string{"]", "}", " {}", " true", " invalid"} {
		if _, err := ParseDescriptor(`{"mode":"ambient"}` + suffix); err == nil {
			t.Errorf("accepted descriptor with trailing data %q", suffix)
		}
	}
	if _, err := ParseDescriptor(`{"mode":"assume-role","role_arn":"arn:aws:iam::123456789012:role/hikyo","external_id":"tenant-42","session_seconds":1800}`); err != nil {
		t.Fatal(err)
	}
}

func TestWorkloadIdentityRequiresInstanceOptIn(t *testing.T) {
	for _, raw := range []string{`{"mode":"ambient"}`, `{"mode":"assume-role","role_arn":"arn:aws:iam::123456789012:role/r"}`, `{"mode":"web-identity","role_arn":"arn:aws:iam::123456789012:role/r"}`} {
		_, err := NewClient(ClientConfig{Origin: "https://secretsmanager.eu-west-1.amazonaws.com", Credential: raw, Deadline: time.Second})
		if !errors.Is(err, ErrWorkloadIdentityDisabled) {
			t.Errorf("%s without opt-in = %v", raw, err)
		}
	}
	client, err := NewClient(ClientConfig{Origin: "https://secretsmanager.eu-west-1.amazonaws.com", Credential: `{"mode":"assume-role","role_arn":"arn:aws:iam::123456789012:role/r","session_seconds":3600}`, Deadline: time.Second, WorkloadIdentity: true})
	if err != nil {
		t.Fatal(err)
	}
	client.Forget()
}

func TestRouteResolution(t *testing.T) {
	for _, tt := range []struct {
		origin, region, sts, want string
		fail                      bool
	}{
		{origin: "https://secretsmanager.eu-west-1.amazonaws.com", want: "eu-west-1|https://sts.eu-west-1.amazonaws.com"},
		{origin: "https://secretsmanager-fips.us-east-1.amazonaws.com", want: "us-east-1|https://sts-fips.us-east-1.amazonaws.com"},
		{origin: "https://secretsmanager.cn-north-1.amazonaws.com.cn", want: "cn-north-1|https://sts.cn-north-1.amazonaws.com.cn"},
		{origin: "https://secretsmanager.eu-west-1.amazonaws.com", region: "us-east-1", fail: true},
		{origin: "https://vpce-1.secretsmanager.eu-west-1.vpce.amazonaws.com", fail: true},
		{origin: "https://vpce-1.secretsmanager.eu-west-1.vpce.amazonaws.com", region: "eu-west-1", sts: "https://vpce-2.sts.eu-west-1.vpce.amazonaws.com", want: "eu-west-1|https://vpce-2.sts.eu-west-1.vpce.amazonaws.com"},
		{origin: "http://secretsmanager.eu-west-1.amazonaws.com", fail: true},
	} {
		r, err := resolveRoute(tt.origin, Descriptor{Mode: AuthStatic, Region: tt.region, STSOrigin: tt.sts})
		if tt.fail != (err != nil) {
			t.Errorf("resolveRoute(%s) err = %v", tt.origin, err)
			continue
		}
		if !tt.fail && r.region+"|"+r.sts != tt.want {
			t.Errorf("resolveRoute(%s) = %s|%s, want %s", tt.origin, r.region, r.sts, tt.want)
		}
	}
}

// Workload identity sends the node's own identity material to STS, so the
// STS endpoint is never tenant-configurable for those modes.
func TestWorkloadIdentityAlwaysUsesAWSSTS(t *testing.T) {
	role := Descriptor{Mode: AuthWebIdentity, RoleARN: "arn:aws:iam::123456789012:role/r", Region: "eu-west-1"}
	r, err := resolveRoute("https://vpce-1.secretsmanager.eu-west-1.vpce.amazonaws.com", role)
	if err != nil || r.sts != "https://sts.eu-west-1.amazonaws.com" {
		t.Fatalf("custom origin with web identity: sts=%q err=%v", r.sts, err)
	}
	role.STSOrigin = "https://attacker.example"
	if _, err := resolveRoute("https://vpce-1.secretsmanager.eu-west-1.vpce.amazonaws.com", role); err == nil {
		t.Fatal("web identity accepted a tenant-configured STS endpoint")
	}
	if r, _ := resolveRoute("https://sm.example", Descriptor{Mode: AuthAmbient, Region: "cn-north-1"}); r.sts != "https://sts.cn-north-1.amazonaws.com.cn" {
		t.Fatalf("china region sts = %q", r.sts)
	}
}

func TestForgetReleasesCredentials(t *testing.T) {
	server := awssmtest.New(testAccount, testRegion)
	defer server.Close()
	client := emulatorClient(t, server)
	client.Forget()
	client.Forget()
	if _, err := client.DescribeSecret(context.Background(), "x"); err == nil {
		t.Fatal("released client still signed a request")
	}
}

func TestPackageDocumentsTheClosure(t *testing.T) {
	raw, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "cannot express a value read") {
		t.Fatal("package documentation lost the no-read closure statement")
	}
}

func TestRetryAfterClampsSecondsBeforeDurationConversion(t *testing.T) {
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	for _, header := range []string{"9223372036854775807", "9223372037", "36000000000"} {
		if got := retryAfter(header, now); !got.Equal(now.Add(adapter.RetryCap)) {
			t.Errorf("Retry-After %s: %v, want cap %v", header, got, now.Add(adapter.RetryCap))
		}
	}
	if got := retryAfter("7", now); !got.Equal(now.Add(7 * time.Second)) {
		t.Errorf("normal delay changed: %v", got)
	}
}
