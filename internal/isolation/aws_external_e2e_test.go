package isolation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
)

// External AWS Secrets Manager coverage (#158). Both tests skip locally and
// fail when their REQUIRED switch is set (HIKYO_TEST_AWS_EMULATOR_REQUIRED,
// HIKYO_TEST_AWS_REAL_REQUIRED), so a CI job that is meant to run one cannot
// pass vacuously. CI runs the emulator leg against a digest-pinned moto
// started by scripts/ci/start-aws-emulator.sh.
//
//   - TestAWSSecretsManagerEmulatorLifecycle runs against an AWS-compatible
//     emulator (LocalStack or Floci) on an HTTPS endpoint:
//     HIKYO_TEST_AWS_ENDPOINT, HIKYO_TEST_AWS_ACCOUNT (required with the
//     endpoint; a non-zero 12-digit account, because LocalStack's default
//     000000000000 parses to 0 and is refused as a destination id),
//     HIKYO_TEST_AWS_REGION (default us-east-1),
//     HIKYO_TEST_AWS_ALLOWED_CIDR for a private address, and
//     HIKYO_TEST_AWS_CA_FILE for a private certificate.
//   - TestAWSSecretsManagerRealSmoke runs against a sandbox AWS account:
//     HIKYO_TEST_AWS_REAL_ACCOUNT, HIKYO_TEST_AWS_REAL_REGION,
//     HIKYO_TEST_AWS_REAL_ACCESS_KEY_ID, HIKYO_TEST_AWS_REAL_SECRET_ACCESS_KEY,
//     optional HIKYO_TEST_AWS_REAL_SESSION_TOKEN. The key needs
//     secretsmanager:{Describe,List,Create,PutSecretValue,UpdateSecretVersionStage,Tag,Restore,Delete}
//     and, for the oracle only, GetSecretValue on the hikyo-e2e-* names.
//
// The adapter itself never reads a value; only the oracle below does, and it
// lives in test code outside the production package's closure scan.

type externalAWS struct {
	origin, account, region string
	creds                   aws.Credentials
	allowed                 []netip.Prefix
	roots                   *x509.CertPool
}

func requireExternalAWS(t *testing.T, required, missing string) {
	t.Helper()
	if os.Getenv(required) != "" {
		t.Fatalf("%s is set but %s is not", required, missing)
	}
	t.Skipf("set %s for external AWS Secrets Manager coverage", missing)
}

func TestAWSSecretsManagerEmulatorLifecycle(t *testing.T) {
	endpoint := os.Getenv("HIKYO_TEST_AWS_ENDPOINT")
	if endpoint == "" {
		requireExternalAWS(t, "HIKYO_TEST_AWS_EMULATOR_REQUIRED", "HIKYO_TEST_AWS_ENDPOINT")
	}
	account := os.Getenv("HIKYO_TEST_AWS_ACCOUNT")
	if account == "" {
		t.Fatal("HIKYO_TEST_AWS_ENDPOINT is set but HIKYO_TEST_AWS_ACCOUNT is not; configure the emulator with a non-zero 12-digit account (000000000000 is refused as a destination id)")
	}
	env := externalAWS{
		origin: endpoint, account: account, region: envOr("HIKYO_TEST_AWS_REGION", "us-east-1"),
		creds: aws.Credentials{AccessKeyID: "AKIAHIKYOEMULATOR001", SecretAccessKey: "emulator-secret"},
	}
	if raw := os.Getenv("HIKYO_TEST_AWS_ALLOWED_CIDR"); raw != "" {
		env.allowed = append(env.allowed, netip.MustParsePrefix(raw))
	}
	if path := os.Getenv("HIKYO_TEST_AWS_CA_FILE"); path != "" {
		pem, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		env.roots = x509.NewCertPool()
		if !env.roots.AppendCertsFromPEM(pem) {
			t.Fatal("HIKYO_TEST_AWS_CA_FILE holds no certificate")
		}
	}
	runExternalAWSLifecycle(t, env)
}

func TestAWSSecretsManagerRealSmoke(t *testing.T) {
	account, region := os.Getenv("HIKYO_TEST_AWS_REAL_ACCOUNT"), os.Getenv("HIKYO_TEST_AWS_REAL_REGION")
	keyID, secret := os.Getenv("HIKYO_TEST_AWS_REAL_ACCESS_KEY_ID"), os.Getenv("HIKYO_TEST_AWS_REAL_SECRET_ACCESS_KEY")
	if account == "" || region == "" || keyID == "" || secret == "" {
		requireExternalAWS(t, "HIKYO_TEST_AWS_REAL_REQUIRED", "HIKYO_TEST_AWS_REAL_ACCOUNT/REGION/ACCESS_KEY_ID/SECRET_ACCESS_KEY")
	}
	runExternalAWSLifecycle(t, externalAWS{
		origin: "https://secretsmanager." + region + ".amazonaws.com", account: account, region: region,
		creds: aws.Credentials{AccessKeyID: keyID, SecretAccessKey: secret, SessionToken: os.Getenv("HIKYO_TEST_AWS_REAL_SESSION_TOKEN")},
	})
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func runExternalAWSLifecycle(t *testing.T, env externalAWS) {
	t.Helper()
	descriptor, err := json.Marshal(awssm.Descriptor{Mode: awssm.AuthStatic, Region: env.region, AccessKeyID: env.creds.AccessKeyID, SecretKey: env.creds.SecretAccessKey, SessionToken: env.creds.SessionToken})
	if err != nil {
		t.Fatal(err)
	}
	client, err := awssm.NewClient(awssm.ClientConfig{Origin: env.origin, Credential: string(descriptor), AllowedCIDRs: env.allowed, STSAllowedCIDRs: env.allowed, RootCAs: env.roots, Deadline: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Forget)
	module := &awssm.Module{API: client}
	run := strings.ToLower(strconv.FormatInt(time.Now().UnixNano(), 36))
	path := "hikyo-e2e-" + run + "/"
	gate := func(context.Context) error { return nil }

	perKey := adapter.Destination{Kind: adapter.PerKey, Owner: env.account, Name: path}
	connection, err := module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: perKey, Gate: gate})
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	perKey.NumericID = connection.DestinationID
	jsonObject := adapter.Destination{Kind: adapter.JSONObject, Owner: env.account, Name: path + "document", NumericID: connection.DestinationID}
	value := "external e2e\nline two é " + run
	manifest := []adapter.ManifestEntry{{KeyID: "key_e2e", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: value}}
	targets := []adapter.Target{
		{ID: "tgt_e2e_keys_" + run, Environment: "external", Generation: 1, Destination: perKey, NamePrefix: "APP_"},
		{ID: "tgt_e2e_json_" + run, Environment: "external", Generation: 1, Destination: jsonObject},
	}
	owned := []string{path + "APP_TOKEN", path + "document"}
	t.Cleanup(func() {
		// Cleanup may not wait for the recovery window: the harness alone
		// force-deletes its own run-scoped names.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, name := range owned {
			if err := externalAWSCall(ctx, env, "DeleteSecret", map[string]any{"SecretId": name, "ForceDeleteWithoutRecovery": true}, nil); err != nil && !strings.Contains(err.Error(), "ResourceNotFound") {
				t.Errorf("force-delete %s: %v", name, err)
			}
		}
	})
	journals := []*forgejoLifecycleJournal{newForgejoLifecycleJournal(), newForgejoLifecycleJournal()}
	for i, target := range targets {
		for attempt := range 2 {
			// The second attempt is a replay of the same job: one version.
			if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: manifest, Ledger: journals[i].ledger(), JobID: "job_e2e_" + run}, journals[i]); err != nil {
				t.Fatalf("sync %s attempt %d: %v", target.ID, attempt, err)
			}
		}
	}
	oracle := map[string]string{}
	for _, name := range owned {
		var out struct {
			SecretString string `json:"SecretString"`
		}
		if err := externalAWSCall(t.Context(), env, "GetSecretValue", map[string]string{"SecretId": name}, &out); err != nil {
			t.Fatalf("oracle read %s: %v", name, err)
		}
		oracle[name] = out.SecretString
		meta, err := client.DescribeSecret(t.Context(), name)
		if err != nil {
			t.Fatal(err)
		}
		if len(meta.Stages) != 1 || meta.Tags[adapter.SentinelName] == "" {
			t.Fatalf("%s after replay: stages %v tags %v, want one tagged version", name, meta.Stages, meta.Tags)
		}
	}
	if oracle[owned[0]] != value {
		t.Fatalf("per-key value = %q", oracle[owned[0]])
	}
	var document map[string]string
	if err := json.Unmarshal([]byte(oracle[owned[1]]), &document); err != nil || document["TOKEN"] != value {
		t.Fatalf("json-object document = %q (%v)", oracle[owned[1]], err)
	}
	// A console edit moves only AWSCURRENT. The provider's own staging
	// semantics must leave HIKYO_CURRENT behind so the next job refuses.
	external := map[string]any{"SecretId": owned[0], "SecretString": "edited outside Hikyo", "ClientRequestToken": strings.Repeat("e", 32) + run}
	if err := externalAWSCall(t.Context(), env, "PutSecretValue", external, nil); err != nil {
		t.Fatalf("external edit: %v", err)
	}
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: targets[0], Manifest: manifest, Ledger: journals[0].ledger(), JobID: "job_e2e_second_" + run}, journals[0]); !errors.Is(err, adapter.ErrConflict) {
		t.Fatalf("sync after external edit = %v, want conflict", err)
	}
	var edited struct {
		SecretString string `json:"SecretString"`
	}
	if err := externalAWSCall(t.Context(), env, "GetSecretValue", map[string]string{"SecretId": owned[0]}, &edited); err != nil || edited.SecretString != "edited outside Hikyo" {
		t.Fatalf("external edit was overwritten: %q %v", edited.SecretString, err)
	}
	for i, target := range targets {
		if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: target, Ledger: journals[i].ledger(), Teardown: true, JobID: "job_e2e_teardown_" + run}, journals[i]); err != nil {
			t.Fatalf("teardown %s: %v", target.ID, err)
		}
	}
	for _, name := range owned {
		meta, err := client.DescribeSecret(t.Context(), name)
		if err != nil || !meta.Deleted {
			t.Fatalf("%s after teardown: deleted=%v err=%v, want scheduled deletion", name, meta.Deleted, err)
		}
	}
	// Recreate inside the recovery window: the name still exists, scheduled
	// for deletion and tagged for this target, so the next job must restore it
	// rather than create it. Emulators diverge from AWS here, which is why the
	// real smoke shares this step.
	recreated := "recreated after teardown " + run
	manifest[0].Value = recreated
	if _, err := module.Sync(t.Context(), adapter.SyncRequest{Target: targets[1], Manifest: manifest, JobID: "job_e2e_recreate_" + run}, newForgejoLifecycleJournal()); err != nil {
		t.Fatalf("recreate %s: %v", targets[1].ID, err)
	}
	meta, err := client.DescribeSecret(t.Context(), owned[1])
	if err != nil || meta.Deleted || meta.Tags[adapter.SentinelName] != targets[1].ID {
		t.Fatalf("%s after recreate: deleted=%v tags=%v err=%v, want restored and owned", owned[1], meta.Deleted, meta.Tags, err)
	}
	var restored struct {
		SecretString string `json:"SecretString"`
	}
	if err := externalAWSCall(t.Context(), env, "GetSecretValue", map[string]string{"SecretId": owned[1]}, &restored); err != nil {
		t.Fatalf("oracle read after recreate: %v", err)
	}
	if err := json.Unmarshal([]byte(restored.SecretString), &document); err != nil || document["TOKEN"] != recreated {
		t.Fatalf("recreated document = %q (%v)", restored.SecretString, err)
	}
}

// externalAWSCall is the test-only oracle and cleanup path. It signs any
// Secrets Manager operation, which is exactly what production code cannot do.
func externalAWSCall(ctx context.Context, env externalAWS, operation string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(env.origin, "/")+"/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "secretsmanager."+operation)
	sum := sha256.Sum256(body)
	if err := v4.NewSigner().SignHTTP(ctx, env.creds, req, hex.EncodeToString(sum[:]), "secretsmanager", env.region, time.Now().UTC()); err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: env.roots}}}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d %s", operation, resp.StatusCode, resp.Header.Get("X-Amzn-ErrorType"))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
