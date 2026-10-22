package isolation

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

// Negative credentials/tokens must be authentic, previously issued material
// retained through its natural expiry by the approved projection path. This
// fixture neither signs JWTs nor changes a token's claims. File shape alone
// does not prove expiry; the regional STS refusal below must name expiry.
func (s *realAWSSandbox) expiredStaticDescriptor(t *testing.T, path, freshPath string) string {
	t.Helper()
	raw := s.descriptor(t, realAWSActor{CredentialFile: path})
	fresh := s.descriptor(t, realAWSActor{CredentialFile: freshPath})
	d, err := awssm.ParseDescriptor(raw)
	current, currentErr := awssm.ParseDescriptor(fresh)
	if err != nil || currentErr != nil || d.Mode != awssm.AuthStatic || current.Mode != awssm.AuthStatic || d.SessionToken == "" || current.SessionToken == "" || d.AccessKeyID == current.AccessKeyID {
		t.Fatal("expired fixture requires a separate protected temporary static session")
	}
	return raw
}

func (s *realAWSSandbox) validateExpiredInputs(t *testing.T, mode string) {
	t.Helper()
	a := s.manifest.Writers[mode]
	switch mode {
	case "static":
		s.expiredStaticDescriptor(t, a.ExpiredCredentialFile, a.CredentialFile)
	case "assume-role":
		s.expiredStaticDescriptor(t, a.ExpiredSourceFile, a.SourceFile)
	case "ambient", "web-identity":
		expired, err := readAWSProtectedFile(s.file(a.ExpiredTokenFile))
		if err != nil {
			t.Fatal(err)
		}
		defer crypto.Zero(expired)
		fresh, err := readAWSProtectedFile(s.file(a.TokenFile))
		if err != nil {
			t.Fatal(err)
		}
		defer crypto.Zero(fresh)
		if len(bytes.TrimSpace(expired)) == 0 || bytes.Equal(bytes.TrimSpace(expired), bytes.TrimSpace(fresh)) {
			t.Fatal("expired workload fixture requires a separate protected token")
		}
	}
}

func realAWSExpiredRefusal(err error, workload bool) bool {
	var api smithy.APIError
	if !errors.As(err, &api) {
		return false
	}
	if api.ErrorCode() == "ExpiredToken" || api.ErrorCode() == "ExpiredTokenException" {
		return true
	}
	// STS can report an expired OIDC token as InvalidIdentityToken. A generic
	// invalid signature/audience/issuer does not satisfy this assertion. The
	// service diagnostic is inspected in memory and never printed or retained.
	return workload && api.ErrorCode() == "InvalidIdentityToken" && strings.Contains(strings.ToLower(api.ErrorMessage()), "expired")
}

func realAWSAuthSTS(s *realAWSSandbox, d awssm.Descriptor) *sts.Client {
	var provider aws.CredentialsProvider = aws.AnonymousCredentials{}
	if d.Mode == awssm.AuthStatic {
		provider = credentials.NewStaticCredentialsProvider(d.AccessKeyID, d.SecretKey, d.SessionToken)
	}
	return sts.New(sts.Options{
		Region: s.manifest.Region, BaseEndpoint: aws.String("https://sts." + s.manifest.Region + ".amazonaws.com"),
		Credentials: provider, RetryMaxAttempts: 1,
		HTTPClient: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	})
}

// These live STS-only negatives consume no CreateSecret reservations. Recovery
// reconstructs the product client from the same approved fresh source. It does
// not claim automatic refresh of an already cached 900-second role session.
func runRealAWSAuthBoundaries(t *testing.T, s *realAWSSandbox) {
	t.Helper()
	s.mu.Lock()
	creations, mutations := len(s.creations), len(s.ops)
	s.mu.Unlock()
	for _, mode := range []string{"static", "assume-role", "ambient", "web-identity"} {
		t.Run("expired-"+mode, func(t *testing.T) {
			raw, _ := s.writer(t, mode)
			a := s.manifest.Writers[mode]
			positive, err := awssm.ParseDescriptor(raw)
			if err != nil {
				t.Fatal("invalid approved auth fixture")
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			var negativeErr error
			switch mode {
			case "static", "assume-role":
				path, freshPath := a.ExpiredCredentialFile, a.CredentialFile
				if mode == "assume-role" {
					path, freshPath = a.ExpiredSourceFile, a.SourceFile
				}
				expiredRaw := s.expiredStaticDescriptor(t, path, freshPath)
				expired, _ := awssm.ParseDescriptor(expiredRaw)
				client := realAWSAuthSTS(s, expired)
				if mode == "static" {
					raw = expiredRaw
					_, negativeErr = client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
				} else {
					t.Setenv("AWS_ACCESS_KEY_ID", expired.AccessKeyID)
					t.Setenv("AWS_SECRET_ACCESS_KEY", expired.SecretKey)
					t.Setenv("AWS_SESSION_TOKEN", expired.SessionToken)
					_, negativeErr = client.AssumeRole(ctx, &sts.AssumeRoleInput{RoleArn: aws.String(positive.RoleARN), RoleSessionName: aws.String("hikyo-expired-source"), ExternalId: aws.String(positive.ExternalID), DurationSeconds: aws.Int32(900)})
				}
			case "ambient", "web-identity":
				token, err := readAWSProtectedFile(s.file(a.ExpiredTokenFile))
				if err != nil {
					t.Fatal(err)
				}
				defer crypto.Zero(token)
				t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", s.file(a.ExpiredTokenFile))
				_, negativeErr = realAWSAuthSTS(s, awssm.Descriptor{}).AssumeRoleWithWebIdentity(ctx, &sts.AssumeRoleWithWebIdentityInput{RoleArn: aws.String(a.PrincipalARN), RoleSessionName: aws.String("hikyo-expired-token"), WebIdentityToken: aws.String(strings.TrimSpace(string(token))), DurationSeconds: aws.Int32(900)})
			}
			if !realAWSExpiredRefusal(negativeErr, mode == "ambient" || mode == "web-identity") {
				t.Fatal("real regional STS did not establish expiry-specific refusal")
			}
			client, err := awssm.NewClient(awssm.ClientConfig{Origin: s.cleanup.origin, Credential: raw, WorkloadIdentity: mode != "static", Deadline: 15 * time.Second})
			if err != nil {
				t.Fatal("expired auth fixture did not reach credential acquisition")
			}
			_, err = client.ResolveIdentity(ctx)
			client.Forget()
			if !errors.Is(err, adapter.ErrProviderAuth) {
				t.Fatal("expired source was not a product auth refusal")
			}
			// Re-isolate all sources and bind STS identity again, so recovery
			// cannot silently borrow the oracle, cleanup actor or local profile.
			s.writer(t, mode)
		})
	}
	t.Run("configured-identity-refusal", func(t *testing.T) {
		raw, client := s.writer(t, "static")
		api := &realAWSIdentityOnly{API: client}
		module := &awssm.Module{API: api}
		otherAccount := "111122223333"
		if otherAccount == s.manifest.Account {
			otherAccount = "444455556666"
		}
		_, err := module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: adapter.Destination{Kind: adapter.JSONObject, Owner: otherAccount, Name: s.manifest.Prefix + "static/identity-refusal"}, Gate: func(context.Context) error { return nil }})
		if !errors.Is(err, adapter.ErrDestinationID) || api.otherCalls != 0 {
			t.Fatal("configured account mismatch reached provider metadata or was accepted")
		}
		d, err := awssm.ParseDescriptor(raw)
		if err != nil {
			t.Fatal("invalid approved region fixture")
		}
		d.Region = "us-east-1"
		if d.Region == s.manifest.Region {
			d.Region = "eu-west-1"
		}
		encoded, err := json.Marshal(d)
		if err != nil {
			t.Fatal("cannot encode region refusal fixture")
		}
		defer crypto.Zero(encoded)
		mismatched, err := awssm.NewClient(awssm.ClientConfig{Origin: s.cleanup.origin, Credential: string(encoded), Deadline: 15 * time.Second})
		if mismatched != nil {
			mismatched.Forget()
		}
		if err == nil || mismatched != nil {
			t.Fatal("descriptor region mismatch survived construction")
		}
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.creations) != creations || len(s.ops) != mutations {
		t.Fatal("STS auth boundaries consumed a creation or mutation reservation")
	}
}

type realAWSIdentityOnly struct {
	awssm.API
	otherCalls int
}

func (a *realAWSIdentityOnly) DescribeSecret(context.Context, string) (awssm.SecretMetadata, error) {
	return awssm.SecretMetadata{}, a.unexpected()
}

func (a *realAWSIdentityOnly) ListSecretNames(context.Context, string, int) ([]string, error) {
	return nil, a.unexpected()
}

func (a *realAWSIdentityOnly) unexpected() error {
	a.otherCalls++
	return errors.New("identity refusal unexpectedly attempted a provider operation")
}

func (a *realAWSIdentityOnly) CreateSecret(context.Context, awssm.CreateSecretInput) error {
	return a.unexpected()
}

func (a *realAWSIdentityOnly) PutSecretValue(context.Context, string, string, string) error {
	return a.unexpected()
}

func (a *realAWSIdentityOnly) UpdateSecretVersionStage(context.Context, string, string, string, string) error {
	return a.unexpected()
}

func (a *realAWSIdentityOnly) TagSecret(context.Context, string, map[string]string) error {
	return a.unexpected()
}

func (a *realAWSIdentityOnly) RestoreSecret(context.Context, string) error {
	return a.unexpected()
}

func (a *realAWSIdentityOnly) DeleteSecret(context.Context, string) error {
	return a.unexpected()
}

func TestAWSRealFixtureAuthExpiryClassification(t *testing.T) {
	for _, code := range []string{"ExpiredToken", "ExpiredTokenException"} {
		if !realAWSExpiredRefusal(&smithy.GenericAPIError{Code: code}, false) {
			t.Fatal("explicit STS expiry was not recognized")
		}
	}
	for _, message := range []string{"signature invalid", "audience invalid", "issuer refused"} {
		if realAWSExpiredRefusal(&smithy.GenericAPIError{Code: "InvalidIdentityToken", Message: message}, true) {
			t.Fatal("generic OIDC refusal masqueraded as expiry")
		}
	}
	if !realAWSExpiredRefusal(&smithy.GenericAPIError{Code: "InvalidIdentityToken", Message: "Token is expired"}, true) || realAWSExpiredRefusal(&smithy.GenericAPIError{Code: "AccessDenied", Message: "expired"}, true) || realAWSExpiredRefusal(errors.New("expired"), true) {
		t.Fatal("STS expiry classification lost its service-code binding")
	}
}
