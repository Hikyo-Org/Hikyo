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
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
)

// The Floci lane (#158): the real-AWS suite body against an emulator with IAM
// enforcement on. The test provisions its own identities through the
// emulator's IAM and STS, renders the same sandbox policies the real gate
// uses, and refuses to continue when Floci stops matching the fidelity facts
// in docs/testing/aws-acceptance-matrix.md.

const awsSandboxPolicyDir = "../../docs/testing/aws-sandbox"

// Floci populates only aws:RequestedRegion among the condition keys the
// sandbox policies use, so a conditioned Allow on any of these never grants.
// Lowercase: condition keys are case-insensitive.
var flociUnsupportedConditionKeys = map[string]bool{
	"aws:currenttime":                           true,
	"secretsmanager:kmskeyarn":                  true,
	"secretsmanager:recoverywindowindays":       true,
	"secretsmanager:forcedeletewithoutrecovery": true,
	"kms:viaservice":                            true,
	"kms:calleraccount":                         true,
	"kms:encryptioncontext:secretarn":           true,
}

// renderSandboxPolicy fills a policy template and fails on any leftover
// placeholder.
func renderSandboxPolicy(file string, vars map[string]string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(awsSandboxPolicyDir, file))
	if err != nil {
		return "", err
	}
	pairs := make([]string, 0, 2*len(vars))
	for k, v := range vars {
		pairs = append(pairs, "${"+k+"}", v)
	}
	out := strings.NewReplacer(pairs...).Replace(string(raw))
	if strings.Contains(out, "${") {
		return "", fmt.Errorf("%s keeps an unresolved placeholder", file)
	}
	return out, nil
}

// stripFlociUnsupportedConditions removes, from Allow statements only, the
// condition keys Floci never populates. aws:RequestedRegion stays. Any other
// key fails, so a new condition in a sandbox policy cannot be silently
// dropped. Deny statements are left as they are.
func stripFlociUnsupportedConditions(doc string) (string, error) {
	var policy map[string]any
	if err := json.Unmarshal([]byte(doc), &policy); err != nil {
		return "", errors.New("policy is not JSON")
	}
	statements, ok := policy["Statement"].([]any)
	if !ok || len(statements) == 0 {
		return "", errors.New("policy statement must be a non-empty array")
	}
	for _, raw := range statements {
		st, ok := raw.(map[string]any)
		if !ok {
			return "", errors.New("policy statement is not an object")
		}
		switch st["Effect"] {
		case "Deny":
			continue
		case "Allow":
		default:
			return "", errors.New("policy statement has an unknown effect")
		}
		rawConditions, present := st["Condition"]
		if !present {
			continue
		}
		conditions, ok := rawConditions.(map[string]any)
		if !ok {
			return "", errors.New("policy condition is not an object")
		}
		for operator, rawKeys := range conditions {
			keys, ok := rawKeys.(map[string]any)
			if !ok {
				return "", errors.New("policy condition operator is not an object")
			}
			for key := range keys {
				switch lower := strings.ToLower(key); {
				case lower == "aws:requestedregion":
				case flociUnsupportedConditionKeys[lower]:
					delete(keys, key)
				default:
					return "", fmt.Errorf("allow statement uses condition key %q that Floci handling does not cover", key)
				}
			}
			if len(keys) == 0 {
				delete(conditions, operator)
			}
		}
		if len(conditions) == 0 {
			delete(st, "Condition")
		}
	}
	out, err := json.Marshal(policy)
	return string(out), err
}

func sandboxPolicyVars(env awsEmulatorEnv, kmsARN, prefix, mode string, expires time.Time) map[string]string {
	run := "arn:aws:secretsmanager:" + env.region + ":" + env.account + ":secret:" + prefix
	at := expires.UTC().Format(time.RFC3339)
	return map[string]string{
		"ACCOUNT": env.account, "REGION": env.region, "KMS_KEY_ARN": kmsARN,
		"RUN_SECRET_ARN_PATTERN": run + "*", "WRITER_SECRET_ARN_PATTERN": run + mode + "/*",
		"AUTH_EXPIRES_AT": at, "CLEANUP_EXPIRES_AT": at,
	}
}

func emulatorRunID() string { return strings.ToLower(strconv.FormatInt(time.Now().UnixNano(), 36)) }

// awsEmulatorAdmin provisions through the emulator's account-root credential
// (test/test). Responses are bounded and neither bodies nor credentials are
// ever logged.
type awsEmulatorAdmin struct {
	env    awsEmulatorEnv
	creds  aws.Credentials
	client *http.Client
}

func newAWSEmulatorAdmin(t *testing.T, env awsEmulatorEnv) *awsEmulatorAdmin {
	t.Helper()
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: env.roots}}
	t.Cleanup(transport.CloseIdleConnections)
	return &awsEmulatorAdmin{env: env, creds: aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, client: &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

var emulatorErrorCode = regexp.MustCompile(`<Code>([A-Za-z0-9.]{1,64})</Code>|"__type"\s*:\s*"([^"]{1,128})"`)

func (a *awsEmulatorAdmin) post(ctx context.Context, service, contentType, target string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.env.endpoint+"/", bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("cannot build emulator request")
	}
	req.Header.Set("Content-Type", contentType)
	if target != "" {
		req.Header.Set("X-Amz-Target", target)
	}
	sum := sha256.Sum256(body)
	if err := v4.NewSigner().SignHTTP(ctx, a.creds, req, hex.EncodeToString(sum[:]), service, a.env.region, time.Now().UTC()); err != nil {
		return nil, errors.New("cannot sign emulator request")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("emulator %s transport failed", service)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("emulator %s response unreadable or too large", service)
	}
	if resp.StatusCode != http.StatusOK {
		code := "unknown"
		if m := emulatorErrorCode.FindSubmatch(raw); m != nil {
			code = string(m[1]) + string(m[2])
		}
		return nil, fmt.Errorf("emulator %s request refused (HTTP %d, %s)", service, resp.StatusCode, code)
	}
	return raw, nil
}

func (a *awsEmulatorAdmin) iam(ctx context.Context, action string, params url.Values) error {
	params.Set("Action", action)
	params.Set("Version", "2010-05-08")
	_, err := a.post(ctx, "iam", "application/x-www-form-urlencoded; charset=utf-8", "", []byte(params.Encode()))
	return err
}

func (a *awsEmulatorAdmin) createKey(ctx context.Context) (string, error) {
	raw, err := a.post(ctx, "kms", "application/x-amz-json-1.1", "TrentService.CreateKey", []byte(`{}`))
	if err != nil {
		return "", err
	}
	var out struct{ KeyMetadata struct{ Arn string } }
	if json.Unmarshal(raw, &out) != nil || !strings.HasPrefix(out.KeyMetadata.Arn, "arn:aws:kms:") {
		return "", errors.New("emulator CreateKey returned no key ARN")
	}
	return out.KeyMetadata.Arn, nil
}

func (a *awsEmulatorAdmin) roleARN(name string) string {
	return "arn:aws:iam::" + a.env.account + ":role/" + name
}

// provisionRole creates a role whose trust is the account root, attaches the
// inline policies, and returns the session credentials of an AssumeRole.
// The sandbox fixture requires session credentials for every actor, and the
// account root's test key is not a session.
func (a *awsEmulatorAdmin) provisionRole(t *testing.T, name string, policies map[string]string) aws.Credentials {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	trust := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::` + a.env.account + `:root"},"Action":"sts:AssumeRole"}]}`
	if err := a.iam(ctx, "CreateRole", url.Values{"RoleName": {name}, "AssumeRolePolicyDocument": {trust}}); err != nil {
		t.Fatalf("provision role: %v", err)
	}
	names := make([]string, 0, len(policies))
	for policyName := range policies {
		names = append(names, policyName)
	}
	sort.Strings(names)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, policyName := range names {
			_ = a.iam(ctx, "DeleteRolePolicy", url.Values{"RoleName": {name}, "PolicyName": {policyName}})
		}
		_ = a.iam(ctx, "DeleteRole", url.Values{"RoleName": {name}})
	})
	for _, policyName := range names {
		if err := a.iam(ctx, "PutRolePolicy", url.Values{"RoleName": {name}, "PolicyName": {policyName}, "PolicyDocument": {policies[policyName]}}); err != nil {
			t.Fatalf("provision role policy: %v", err)
		}
	}
	client := sts.New(sts.Options{
		Region: a.env.region, BaseEndpoint: aws.String(a.env.endpoint), RetryMaxAttempts: 1, HTTPClient: a.client,
		Credentials: credentials.NewStaticCredentialsProvider(a.creds.AccessKeyID, a.creds.SecretAccessKey, ""),
	})
	out, err := client.AssumeRole(ctx, &sts.AssumeRoleInput{RoleArn: aws.String(a.roleARN(name)), RoleSessionName: aws.String("hikyo158"), DurationSeconds: aws.Int32(3600)})
	if err != nil || out.Credentials == nil {
		t.Fatal("provision role: emulator AssumeRole refused")
	}
	c := out.Credentials
	return aws.Credentials{AccessKeyID: aws.ToString(c.AccessKeyId), SecretAccessKey: aws.ToString(c.SecretAccessKey), SessionToken: aws.ToString(c.SessionToken)}
}

// provisionSandboxRole renders sandbox policy files and strips what Floci
// cannot evaluate, unless unstripped is asked for (canary only).
func (a *awsEmulatorAdmin) provisionSandboxRole(t *testing.T, name string, vars map[string]string, files []string, unstripped bool) aws.Credentials {
	t.Helper()
	policies := make(map[string]string)
	for _, file := range files {
		doc, err := renderSandboxPolicy(file, vars)
		if err == nil && !unstripped {
			doc, err = stripFlociUnsupportedConditions(doc)
		}
		if err != nil {
			t.Fatalf("render %s: %v", file, err)
		}
		policies[strings.TrimSuffix(file, ".policy.json")] = doc
	}
	return a.provisionRole(t, name, policies)
}

const matrixHint = "update docs/testing/aws-acceptance-matrix.md and the Floci handling in aws_emulator_acceptance_test.go"

// canarySecret creates a throwaway secret outside the run prefix. The admin
// credential force-deletes it afterwards whatever the outcome.
func canarySecret(t *testing.T, env awsEmulatorEnv, creds aws.Credentials, name, kmsARN string) error {
	t.Helper()
	forceDeleteCanary(t, env, name)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	return externalAWSCall(ctx, externalAWS{origin: env.endpoint, region: env.region, roots: env.roots, creds: creds}, "CreateSecret", map[string]any{"Name": name, "SecretString": "canary-synthetic", "KmsKeyId": kmsARN, "ClientRequestToken": strings.Repeat("a", 32)}, nil)
}

func forceDeleteCanary(t *testing.T, env awsEmulatorEnv, name string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = externalAWSCall(ctx, externalAWS{origin: env.endpoint, region: env.region, roots: env.roots, creds: aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}}, "DeleteSecret", map[string]any{"SecretId": name, "ForceDeleteWithoutRecovery": true}, nil)
	})
}

// firstVersionCanary pins why lifecycle stays off Floci. The product creates a
// secret without a value, then stages the first value under HIKYO_PENDING and
// relies on AWS making that first version AWSCURRENT. Floci 2.4.0 breaks both
// steps: its value-less CreateSecret already adds an empty AWSCURRENT version,
// and it never adds AWSCURRENT to a first version staged elsewhere. The staged
// version carries AWSCURRENT only once both match AWS, which is exactly when
// lifecycle can run here. It never reads a value.
func firstVersionCanary(t *testing.T, env awsEmulatorEnv, name string) {
	t.Helper()
	forceDeleteCanary(t, env, name)
	admin := externalAWS{origin: env.endpoint, region: env.region, roots: env.roots, creds: aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	var put struct{ VersionId string }
	var described struct{ VersionIdsToStages map[string][]string }
	if externalAWSCall(ctx, admin, "CreateSecret", map[string]any{"Name": name}, nil) != nil ||
		externalAWSCall(ctx, admin, "PutSecretValue", map[string]any{"SecretId": name, "SecretString": "canary-synthetic", "VersionStages": []string{"HIKYO_PENDING"}}, &put) != nil ||
		externalAWSCall(ctx, admin, "DescribeSecret", map[string]any{"SecretId": name}, &described) != nil {
		t.Fatalf("first-version canary could not run. %s", matrixHint)
	}
	stages := described.VersionIdsToStages[put.VersionId]
	if !slices.Contains(stages, "HIKYO_PENDING") {
		t.Fatalf("first-version canary lost its staged version. %s", matrixHint)
	}
	if slices.Contains(stages, "AWSCURRENT") {
		t.Fatalf("Floci now creates no version on a value-less CreateSecret and makes the first PutSecretValue version AWSCURRENT, like AWS; run lifecycle on Floci, switch TestAWSSecretsManagerEmulatorLifecycle to Floci, remove moto, and update docs/testing/aws-acceptance-matrix.md")
	}
}

func writeEmulatorFile(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAWSSecretsManagerEmulatorAcceptance(t *testing.T) {
	started := time.Now()
	defer func() { t.Logf("emulator acceptance wall time %s", time.Since(started).Round(time.Second)) }()
	env := loadAWSEmulatorEnv(t, "HIKYO_TEST_FLOCI", "HIKYO_TEST_FLOCI_REQUIRED")
	if os.Getenv("HIKYO_TEST_POSTGRES_DSN") == "" {
		t.Fatal("emulator acceptance requires both datastore engines; set HIKYO_TEST_POSTGRES_DSN")
	}
	if env.caFile == "" || env.allowedCIDR == "" {
		t.Fatal("emulator acceptance requires HIKYO_TEST_FLOCI_CA_FILE and HIKYO_TEST_FLOCI_ALLOWED_CIDR")
	}
	origin, err := adapter.CanonicalOrigin(adapter.AWSSecretsManagerProvider, env.endpoint)
	if err != nil || awssm.ValidateOrigin(origin) != nil {
		t.Fatal("HIKYO_TEST_FLOCI_ENDPOINT is not a canonical https origin")
	}
	admin := newAWSEmulatorAdmin(t, env)
	setup, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	kmsARN, err := admin.createKey(setup)
	cancel()
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	run := emulatorRunID()
	prefix := "hikyo-e2e-" + run + "/"
	canaryPrefix := "hikyo-canary-" + run + "/"
	expires := time.Now().Add(time.Hour)

	// Canary (i): enforcement is on.
	fabricated := externalAWS{origin: env.endpoint, region: env.region, roots: env.roots, creds: aws.Credentials{AccessKeyID: "AKIAHIKYOEMULATOR001", SecretAccessKey: "emulator-secret"}}
	if err := externalAWSCall(t.Context(), fabricated, "ListSecrets", map[string]any{}, nil); !realAWSCode(err, "UnrecognizedClientException") {
		t.Fatalf("emulator accepted or mis-refused a fabricated access key; IAM enforcement may be off. %s", matrixHint)
	}

	// Canary (ii): the unstripped template must grant nothing.
	unstripped := admin.provisionSandboxRole(t, "hikyo158-"+run+"-unstripped", sandboxPolicyVars(env, kmsARN, canaryPrefix, "unstripped", expires), []string{"writer.policy.json"}, true)
	if err := canarySecret(t, env, unstripped, canaryPrefix+"unstripped/probe", kmsARN); !realAWSCode(err, "AccessDeniedException") {
		t.Fatalf("Floci now evaluates the sandbox condition keys (unstripped writer policy result: %v); stop stripping them. %s", err, matrixHint)
	}

	// Canary (iii): a KMS identity deny has no effect, so kms-denial stays out.
	denied := admin.provisionSandboxRole(t, "hikyo158-"+run+"-kmsdenied", sandboxPolicyVars(env, kmsARN, canaryPrefix, "kms-denied", expires), []string{"writer.policy.json", "kms-denied.policy.json"}, false)
	if err := canarySecret(t, env, denied, canaryPrefix+"kms-denied/probe", kmsARN); err != nil {
		t.Fatalf("Floci now enforces an identity deny on the CMK (create refused: %v); return kms-denial to the lane. %s", err, matrixHint)
	}

	// Canary (iv): lifecycle stays on moto and the live smoke.
	firstVersionCanary(t, env, canaryPrefix+"first-version/probe")

	vars := sandboxPolicyVars(env, kmsARN, prefix, "static", expires)
	roles := map[string]aws.Credentials{
		"static":  admin.provisionSandboxRole(t, "hikyo158-"+run+"-static", vars, []string{"writer.policy.json", "static-lifecycle.policy.json"}, false),
		"oracle":  admin.provisionSandboxRole(t, "hikyo158-"+run+"-oracle", vars, []string{"oracle.policy.json"}, false),
		"cleanup": admin.provisionSandboxRole(t, "hikyo158-"+run+"-cleanup", vars, []string{"cleanup.policy.json"}, false),
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	actor := func(name string) realAWSActor {
		creds := roles[name]
		writeEmulatorFile(t, filepath.Join(dir, name+".json"), awssm.Descriptor{Mode: awssm.AuthStatic, Region: env.region, AccessKeyID: creds.AccessKeyID, SecretKey: creds.SecretAccessKey, SessionToken: creds.SessionToken, STSOrigin: origin})
		return realAWSActor{CredentialFile: name + ".json", PrincipalARN: admin.roleARN("hikyo158-" + run + "-" + name)}
	}
	caFile, err := filepath.Abs(env.caFile)
	if err != nil {
		t.Fatal(err)
	}
	manifest := realAWSManifest{
		Account: env.account, Region: env.region, AuthorizationID: "floci-emulator", BudgetUSD: 1, Prefix: prefix, KMSKeyARN: kmsARN, ExpiresAt: expires,
		Writers: map[string]realAWSActor{"static": actor("static")}, Oracle: actor("oracle"), Cleanup: actor("cleanup"),
		Emulator: &realAWSEmulator{Origin: origin, STSOrigin: origin, CAFile: caFile, AllowedCIDR: env.allowedCIDR},
	}
	path := filepath.Join(dir, "manifest.json")
	writeEmulatorFile(t, path, manifest)

	runRealAWSSuite(t, openRealAWSSandbox(t, path, true))
}

func TestAWSEmulatorPolicyStripping(t *testing.T) {
	env := awsEmulatorEnv{account: "123456789012", region: "us-east-1"}
	vars := sandboxPolicyVars(env, "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555", "hikyo-e2e-abcdefgh/", "static", time.Now().Add(time.Hour))
	for _, file := range []string{"writer.policy.json", "static-lifecycle.policy.json", "oracle.policy.json", "cleanup.policy.json", "kms-denied.policy.json"} {
		rendered, err := renderSandboxPolicy(file, vars)
		if err != nil {
			t.Fatal(err)
		}
		stripped, err := stripFlociUnsupportedConditions(rendered)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		var before, after struct{ Statement []map[string]any }
		if json.Unmarshal([]byte(rendered), &before) != nil || json.Unmarshal([]byte(stripped), &after) != nil || len(before.Statement) != len(after.Statement) {
			t.Fatalf("%s: stripping changed the statement list", file)
		}
		for i, st := range after.Statement {
			if st["Effect"] == "Deny" {
				if !reflect.DeepEqual(st, before.Statement[i]) {
					t.Fatalf("%s: a Deny statement changed", file)
				}
				continue
			}
			if cond, ok := st["Condition"].(map[string]any); ok {
				for op, keys := range cond {
					if len(keys.(map[string]any)) != 1 || keys.(map[string]any)["aws:RequestedRegion"] != "us-east-1" || op != "StringEquals" {
						t.Fatalf("%s: unexpected condition left on an Allow", file)
					}
				}
			}
		}
	}
	// An emptied Condition block disappears; the region key survives.
	got, err := stripFlociUnsupportedConditions(`{"Statement":[{"Effect":"Allow","Action":"x","Resource":"*","Condition":{"DateLessThan":{"aws:CurrentTime":"t"}}}]}`)
	if err != nil || strings.Contains(got, "Condition") {
		t.Fatalf("emptied condition survived: %s %v", got, err)
	}
	// Unknown keys and effects fail closed; Deny conditions are not inspected.
	for _, bad := range []string{
		`{"Statement":[{"Effect":"Allow","Condition":{"IpAddress":{"aws:SourceIp":"10.0.0.0/8"}}}]}`,
		`{"Statement":[{"Effect":"Allow","Condition":{"StringEquals":{"aws:RequestedRegion":"r","aws:PrincipalTag/x":"y"}}}]}`,
		`{"Statement":[{"Effect":"Maybe"}]}`,
		`not json`,
		`{"Statement":{"Effect":"Allow","Condition":{"IpAddress":{"aws:SourceIp":"x"}}}}`,
		`{"Statement":[]}`,
		`{}`,
		`{"Statement":[{"Effect":"Allow","Condition":"aws:SourceIp"}]}`,
	} {
		if _, err := stripFlociUnsupportedConditions(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	if _, err := stripFlociUnsupportedConditions(`{"Statement":[{"Effect":"Deny","Condition":{"IpAddress":{"aws:SourceIp":"x"}}}]}`); err != nil {
		t.Fatal("a Deny condition was inspected")
	}
	if _, err := renderSandboxPolicy("writer.policy.json", map[string]string{"REGION": "r"}); err == nil {
		t.Fatal("unresolved placeholder accepted")
	}
}
