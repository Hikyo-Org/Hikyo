package awssm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"
)

type expiringTestCredentials struct {
	calls int
	refuseRefresh bool
}

func (p *expiringTestCredentials) Retrieve(context.Context) (aws.Credentials, error) {
	p.calls++
	if p.calls > 1 && p.refuseRefresh {
		return aws.Credentials{}, &smithy.GenericAPIError{Code: "ExpiredToken", Message: "PRIVATE_REFRESH_CANARY"}
	}
	key := "AKIDFIRSTSYNTHETIC"
	if p.calls > 1 {
		key = "AKIDSECONDSYNTHETIC"
	}
	return aws.Credentials{AccessKeyID: key, SecretAccessKey: "synthetic-secret", SessionToken: "synthetic-token", CanExpire: true, Expires: time.Now().Add(30 * time.Minute)}, nil
}

// The SDK expiry window moves the first cached expiry into the past without
// sleeping or changing the host clock. This exercises one actual Client and
// CredentialsCache across two signed requests, not STS issuance/projection.
func TestClientRefreshesExpiredCachedCredentials(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		name := "refresh-success"
		if refuse {
			name = "refresh-refused"
		}
		t.Run(name, func(t *testing.T) {
			client, err := NewClient(ClientConfig{Origin: "https://secretsmanager.eu-west-1.amazonaws.com", Credential: staticDescriptor("eu-west-1"), Deadline: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Forget()
			provider := &expiringTestCredentials{refuseRefresh: refuse}
			client.credentials = aws.NewCredentialsCache(provider, func(o *aws.CredentialsCacheOptions) {
				o.ExpiryWindow = time.Hour
				o.ExpiryWindowJitterFrac = 0
			})
			requests := 0
			client.http.Transport = safeErrorTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				key := "AKIDFIRSTSYNTHETIC"
				if requests > 1 {
					key = "AKIDSECONDSYNTHETIC"
				}
				if !strings.Contains(req.Header.Get("Authorization"), "Credential="+key+"/") {
					t.Error("request did not use expected refreshed synthetic identity")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})
			if err := client.PutSecretValue(t.Context(), "synthetic", "token-first", "synthetic-value"); err != nil {
				t.Fatal("initial controlled write failed")
			}
			err = client.PutSecretValue(t.Context(), "synthetic", "token-second", "synthetic-value")
			if provider.calls != 2 {
				t.Fatal("same client did not retrieve after cached expiry")
			}
			if refuse {
				if !errors.Is(err, adapter.ErrProviderAuth) || requests != 1 || strings.Contains(err.Error(), "PRIVATE_REFRESH_CANARY") {
					t.Fatal("refused refresh sent a request, lost auth classification or exposed provider content")
				}
			} else if err != nil || requests != 2 {
				t.Fatal("same-client refresh did not resume with new credentials")
			}
		})
	}
}
