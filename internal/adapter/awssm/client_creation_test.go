package awssm

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func TestWriteRequestTokensOmitEmptyAndRetainStableToken(t *testing.T) {
	for _, operation := range []string{"CreateSecret", "PutSecretValue"} {
		for _, token := range []string{"", strings.Repeat("a", 64)} {
			t.Run(operation+"/empty="+fmt.Sprint(token == ""), func(t *testing.T) {
				client, err := NewClient(ClientConfig{Origin: "https://secretsmanager.eu-west-1.amazonaws.com", Credential: staticDescriptor(testRegion), Deadline: time.Second})
				if err != nil {
					t.Fatal(err)
				}
				defer client.Forget()
				client.http.Transport = safeErrorTransport(func(req *http.Request) (*http.Response, error) {
					var request map[string]json.RawMessage
					if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
						t.Fatal(err)
					}
					raw, exists := request["ClientRequestToken"]
					if exists != (token != "") {
						t.Errorf("ClientRequestToken present=%v, want %v", exists, token != "")
					}
					if token != "" && string(raw) != `"`+token+`"` {
						t.Error("stable token changed on the wire")
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
				})
				if operation == "CreateSecret" {
					err = client.CreateSecret(t.Context(), CreateSecretInput{Name: "offline/metadata", ClientRequestToken: token})
				} else {
					err = client.PutSecretValue(t.Context(), "offline/metadata", token, "synthetic value")
				}
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// The existing error-only API must not acquire a response-shape requirement.
// Only the explicit metadata identity path requires ARN and Name.
func TestCreateSecretIdentityPreservesLegacyContract(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Amz-Target") != "secretsmanager.CreateSecret" {
			t.Error("unexpected operation")
		}
		var request map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid request")
		}
		if _, exists := request["SecretString"]; exists {
			t.Error("metadata create added a value")
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client, err := NewClient(ClientConfig{Origin: server.URL, Credential: staticDescriptor(testRegion), Deadline: time.Second, RootCAs: roots, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Forget()
	input := CreateSecretInput{Name: "offline/metadata", ClientRequestToken: strings.Repeat("a", 64)}
	if err := client.CreateSecret(t.Context(), input); err != nil {
		t.Fatal("legacy create began requiring response identity")
	}
	if _, err := client.CreateSecretIdentity(t.Context(), input, testAccount); err == nil {
		t.Fatal("strict creation identity accepted an absent identity")
	}
	if calls.Load() != 2 {
		t.Fatal("create path repeated a write")
	}
	if _, err := client.CreateSecretIdentity(t.Context(), input, "invalid"); err == nil || calls.Load() != 2 {
		t.Fatal("invalid expected account reached provider")
	}
}

func TestCreateSecretIdentityUsesExactMetadataIdentity(t *testing.T) {
	identity := CreatedSecret{ARN: "arn:aws:secretsmanager:" + testRegion + ":" + testAccount + ":secret:offline/metadata-AbCdEf", Name: "offline/metadata"}
	for _, mutation := range []string{"none", "account", "region", "name", "arn-name", "suffix", "partition"} {
		t.Run(mutation, func(t *testing.T) {
			response := identity
			switch mutation {
			case "account":
				response.ARN = strings.ReplaceAll(response.ARN, testAccount, "999999999999")
			case "region":
				response.ARN = strings.ReplaceAll(response.ARN, testRegion, "eu-central-1")
			case "name":
				response.Name = "other"
			case "arn-name":
				response.ARN = strings.ReplaceAll(response.ARN, "offline/metadata", "other")
			case "suffix":
				response.ARN = strings.ReplaceAll(response.ARN, "AbCdEf", "AbCdE!")
			case "partition":
				response.ARN = strings.ReplaceAll(response.ARN, "arn:aws:", "arn:other:")
			}
			client, err := NewClient(ClientConfig{Origin: "https://secretsmanager.eu-west-1.amazonaws.com", Credential: staticDescriptor(testRegion), Deadline: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Forget()
			client.http.Transport = safeErrorTransport(func(*http.Request) (*http.Response, error) {
				raw, err := json.Marshal(response)
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})
			got, err := client.CreateSecretIdentity(t.Context(), CreateSecretInput{Name: identity.Name, ClientRequestToken: strings.Repeat("a", 64)}, testAccount)
			if mutation == "none" {
				if err != nil || got != identity {
					t.Fatalf("valid creation receipt refused: %v", err)
				}
			} else if !errors.Is(err, adapter.ErrDestinationID) || got != (CreatedSecret{}) {
				t.Fatalf("mismatched creation receipt accepted: %v", err)
			}
		})
	}
}
