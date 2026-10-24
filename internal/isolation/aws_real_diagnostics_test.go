package isolation

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm/awssmtest"
)

// Keep both vocabularies in agreement with the trusted runner's exact parser.
// Never format err or pass its message or an unrecognized provider code through.
func realAWSDiagnosticLine(operation string, err error) (string, bool) {
	if err == nil {
		return "", false
	}
	switch operation {
	case "DescribeSecret", "CreateSecret", "PutSecretValue", "UpdateSecretVersionStage", "TagResource", "DeleteSecret", "RestoreSecret", "GetSecretValue":
	default:
		return "", false
	}
	code := "other"
	var provider *awssm.ResponseError
	var fixture *realAWSResponseError
	if errors.As(err, &provider) {
		code = provider.Code
	} else if errors.As(err, &fixture) {
		code = fixture.code
	}
	switch code {
	case "AccessDeniedException", "DecryptionFailure", "EncryptionFailure", "ExpiredToken", "ExpiredTokenException", "InvalidClientTokenId", "InvalidParameterException", "InvalidRequestException", "LimitExceededException", "ResourceExistsException", "ResourceNotFoundException", "ThrottlingException", "UnrecognizedClientException":
	default:
		code = "other"
	}
	return "synthetic AWS diagnostic: " + operation + " " + code, true
}

func logRealAWSDiagnostic(t *testing.T, operation string, err error) {
	if t == nil {
		return
	}
	if line, ok := realAWSDiagnosticLine(operation, err); ok {
		// Deliberately retain this source filename for the runner's anchored
		// allowlist. Calling t.Helper here would attribute an arbitrary caller.
		t.Logf("%s", line)
	}
}

type unprintableAWSDiagnosticError struct{}

func (unprintableAWSDiagnosticError) Error() string {
	panic("diagnostics must not inspect an error message")
}

func TestAWSRealDiagnosticsRejectUnboundedDetails(t *testing.T) {
	for _, row := range []struct {
		name      string
		operation string
		err       error
		want      string
		accepted  bool
	}{
		{"success", "CreateSecret", nil, "", false},
		{"unknown-operation", "credential-canary", unprintableAWSDiagnosticError{}, "", false},
		{"provider-code", "CreateSecret", &awssm.ResponseError{Status: 400, Code: "InvalidRequestException"}, "synthetic AWS diagnostic: CreateSecret InvalidRequestException", true},
		{"fixture-code", "DescribeSecret", &realAWSResponseError{status: 400, code: "ResourceNotFoundException"}, "synthetic AWS diagnostic: DescribeSecret ResourceNotFoundException", true},
		{"unrecognized-code", "CreateSecret", &awssm.ResponseError{Status: 400, Code: "credential-canary\nsynthetic AWS diagnostic: CreateSecret AccessDeniedException"}, "synthetic AWS diagnostic: CreateSecret other", true},
		{"unprintable-message", "PutSecretValue", unprintableAWSDiagnosticError{}, "synthetic AWS diagnostic: PutSecretValue other", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, accepted := realAWSDiagnosticLine(row.operation, row.err)
			if got != row.want || accepted != row.accepted {
				t.Fatalf("diagnostic = %q, %v; want %q, %v", got, accepted, row.want, row.accepted)
			}
		})
	}
}

func TestAWSRealDiagnosticsFromCreateFailure(t *testing.T) {
	m := realAWSManifestFixture(time.Now())
	remote := awssmtest.New(m.Account, m.Region)
	t.Cleanup(remote.Close)
	roots := x509.NewCertPool()
	roots.AddCert(remote.Certificate())
	raw, err := json.Marshal(awssm.Descriptor{Mode: awssm.AuthStatic, Region: m.Region, AccessKeyID: "AKIAHIKYOTEST0000001", SecretKey: "synthetic-diagnostic-credential-canary"})
	if err != nil {
		t.Fatal("cannot encode synthetic descriptor")
	}
	client, err := awssm.NewClient(awssm.ClientConfig{
		Origin: remote.URL, Credential: string(raw), Deadline: 5 * time.Second,
		RootCAs: roots, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
	})
	if err != nil {
		t.Fatal("local diagnostic client unavailable")
	}
	t.Cleanup(client.Forget)
	s := &realAWSSandbox{manifest: m, owned: make(map[string]string)}
	writer := &realAWSWriter{API: client, t: t, s: s}
	// A synthetic rejection exercises the actual signed client and fixture
	// wrapper without making an AWS call or claiming provider acceptance.
	remote.FailNext("CreateSecret", 400, "InvalidRequestException", "")
	err = writer.CreateSecret(t.Context(), awssm.CreateSecretInput{
		Name: m.Prefix + "static/sqlite/document", ClientRequestToken: strings.Repeat("a", 64),
		Tags: map[string]string{adapter.SentinelName: "diagnostic-target"},
	})
	var response *awssm.ResponseError
	if !errors.As(err, &response) || response.Code != "InvalidRequestException" || !awssm.IsDefinite(err) {
		t.Fatal("signed create refusal classification changed")
	}
	if len(s.creations) != 1 || len(s.owned) != 0 {
		t.Fatal("failed create refunded its reservation or invented owned custody")
	}
}
