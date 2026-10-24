package isolation

import (
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestExternalAWSCallTypedErrorsAndPrivacy(t *testing.T) {
	for _, mode := range []string{"success", "body-code", "header-code", "invalid-success", "oversized", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			redirects := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					redirects++
					w.WriteHeader(http.StatusOK)
					return
				}
				if r.Header.Get("X-Amz-Target") != "secretsmanager.DescribeSecret" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
					t.Error("missing signed operation")
				}
				w.Header().Set("Content-Type", "application/x-amz-json-1.1")
				switch mode {
				case "success":
					_, _ = w.Write([]byte(`{"Name":"synthetic"}`))
				case "body-code":
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"__type":"aws#ResourceNotFoundException","Message":"PRIVATE_BODY_CANARY"}`))
				case "header-code":
					w.Header().Set("X-Amzn-ErrorType", "ResourceNotFoundException:http://PRIVATE_HEADER_CANARY")
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte("PRIVATE_BODY_CANARY"))
				case "invalid-success":
					_, _ = w.Write([]byte("PRIVATE_BODY_CANARY"))
				case "oversized":
					_, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
				case "redirect":
					w.Header().Set("Location", "/redirected")
					w.WriteHeader(http.StatusTemporaryRedirect)
				}
			}))
			defer server.Close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			env := externalAWS{origin: server.URL + "/", region: "eu-west-1", roots: roots, creds: aws.Credentials{AccessKeyID: "SYNTHETIC", SecretAccessKey: "synthetic"}}
			var out struct{ Name string }
			err := externalAWSCall(t.Context(), env, "DescribeSecret", map[string]string{"SecretId": "synthetic"}, &out)
			if mode == "success" {
				if err != nil || out.Name != "synthetic" {
					t.Fatal("shared transport lost response")
				}
				return
			}
			if err == nil || strings.Contains(err.Error(), "PRIVATE_") || redirects != 0 {
				t.Fatal("shared transport lost refusal/privacy/redirect policy")
			}
			if mode == "body-code" || mode == "header-code" {
				var response *realAWSResponseError
				if !errors.As(err, &response) || response.status != 400 || !realAWSCode(err, "ResourceNotFoundException") {
					t.Fatal("typed provider classification lost")
				}
			}
		})
	}
}
