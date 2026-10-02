package awssm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"
)

func assertResponseErrorDoesNotLog(t *testing.T, err error, marker string) {
	t.Helper()
	if err == nil {
		t.Fatal("provider refusal unexpectedly succeeded")
	}
	var logs bytes.Buffer
	slog.New(slog.NewTextHandler(&logs, nil)).Error("provider failed", "error", err)
	for _, text := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), logs.String()} {
		if strings.Contains(text, marker) {
			t.Fatalf("receiver code escaped into error/log: %q", text)
		}
	}
}

func responseTestClient(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{Origin: "https://secretsmanager.eu-west-1.amazonaws.com", Credential: staticDescriptor("eu-west-1"), Deadline: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Forget)
	return client
}

func TestReceiverErrorCodesNeverEnterLogs(t *testing.T) {
	const marker = "PrivateReceiverSecretCanary"
	for _, source := range []string{"header", "body"} {
		t.Run(source, func(t *testing.T) {
			client := responseTestClient(t)
			client.http.Transport = safeErrorTransport(func(*http.Request) (*http.Response, error) {
				headers := make(http.Header)
				body := "{}"
				if source == "header" {
					headers.Set("X-Amzn-ErrorType", marker)
				} else {
					raw, err := json.Marshal(map[string]string{"__type": marker})
					if err != nil {
						t.Fatal(err)
					}
					body = string(raw)
				}
				return &http.Response{StatusCode: http.StatusBadRequest, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			_, err := client.DescribeSecret(t.Context(), "test")
			assertResponseErrorDoesNotLog(t, err, marker)
			if !IsDefinite(err) || codeOf(err) != marker {
				t.Fatal("typed refusal code/classification changed")
			}
		})
	}
}

type refusedCredentials struct{ err error }

func (p refusedCredentials) Retrieve(context.Context) (aws.Credentials, error) {
	return aws.Credentials{}, p.err
}

func TestCredentialAPICodesNeverEnterLogs(t *testing.T) {
	const marker = "PrivateCredentialSecretCanary"
	for _, source := range []string{"credential_provider", "sts_response"} {
		t.Run(source, func(t *testing.T) {
			client := responseTestClient(t)
			var err error
			if source == "credential_provider" {
				client.credentials = refusedCredentials{err: &smithy.GenericAPIError{Code: marker, Message: marker}}
				_, err = client.DescribeSecret(t.Context(), "test")
			} else {
				client.stsHTTP.Transport = safeErrorTransport(func(*http.Request) (*http.Response, error) {
					body := "<ErrorResponse><Error><Type>Sender</Type><Code>" + marker + "</Code><Message>" + marker + "</Message></Error><RequestId>test</RequestId></ErrorResponse>"
					return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": []string{"text/xml"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
				})
				_, err = client.ResolveIdentity(t.Context())
			}
			assertResponseErrorDoesNotLog(t, err, marker)
			if !errors.Is(err, adapter.ErrProviderAuth) {
				t.Fatal("credential refusal classification lost")
			}
		})
	}
}

func TestKnownRefusalCodesKeepClassification(t *testing.T) {
	for _, code := range []string{"ResourceNotFoundException", "ResourceExistsException", "AccessDeniedException", "ThrottlingException", "LimitExceededException"} {
		t.Run(code, func(t *testing.T) {
			client := responseTestClient(t)
			client.http.Transport = safeErrorTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"X-Amzn-Errortype": []string{code}}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})
			_, err := client.DescribeSecret(t.Context(), "test")
			if codeOf(err) != code || !IsDefinite(err) || IsNotFound(err) != (code == "ResourceNotFoundException") || IsExists(err) != (code == "ResourceExistsException") || errors.Is(err, adapter.ErrProviderAuth) != (code == "AccessDeniedException") || errors.Is(err, adapter.ErrRateLimited) != (code == "ThrottlingException") {
				t.Fatalf("refusal classification changed: %v", err)
			}
			credential := credentialError(&smithy.GenericAPIError{Code: code, Message: "ignored"})
			if errors.Is(credential, adapter.ErrRateLimited) != (code == "ThrottlingException") || errors.Is(credential, adapter.ErrProviderAuth) != (code != "ThrottlingException") {
				t.Fatalf("credential classification changed: %v", credential)
			}
		})
	}
}
