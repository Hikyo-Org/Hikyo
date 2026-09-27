package awssm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkloadIdentityRejectsNonAWSDestinations(t *testing.T) {
	for _, mode := range []AuthMode{AuthAmbient, AuthAssumeRole, AuthWebIdentity} {
		for _, origin := range []string{
			"https://attacker.example", "https://secretsmanager.eu-west-1.amazonaws.com.attacker.example",
			"https://attacker.amazonaws.com", "https://vpce-1.ec2.eu-west-1.vpce.amazonaws.com",
			"https://vpce-1.secretsmanager.eu-west-1.vpce.amazonaws.com.attacker.example",
			"https://secretsmanager.eu-west-1.amazonaws.com:8443",
			"https://secretsmanager.cn-north-1.amazonaws.com",
			"https://secretsmanager.eu-west-1.amazonaws.com.cn",
			"https://vpce-1.secretsmanager.us-east-1.vpce.amazonaws.com",
		} {
			if _, err := resolveRoute(origin, Descriptor{Mode: mode, Region: "eu-west-1"}); err == nil {
				t.Errorf("%s accepted %s", mode, origin)
			}
		}
	}
}

func TestWorkloadIdentityRecognizesAWSServiceEndpoints(t *testing.T) {
	for _, tc := range []struct{ host, region string }{
		{"secretsmanager.eu-west-1.amazonaws.com", "eu-west-1"},
		{"secretsmanager-fips.us-gov-west-1.amazonaws.com", "us-gov-west-1"},
		{"secretsmanager.cn-north-1.amazonaws.com.cn", "cn-north-1"},
		{"secretsmanager.eu-west-1.amazonaws.com:443", "eu-west-1"},
		{"vpce-1234a5678b9012c-12345678.secretsmanager.eu-west-1.vpce.amazonaws.com", "eu-west-1"},
		{"vpce-1234a5678b9012c-12345678-eu-west-1a.secretsmanager.eu-west-1.vpce.amazonaws.com", "eu-west-1"},
		{"vpce-123-secretsmanager.us-east-1.vpce.amazonaws.com", "us-east-1"},
		{"vpce-123.secretsmanager-fips.us-gov-west-1.vpce.amazonaws.com", "us-gov-west-1"},
		{"vpce-123.secretsmanager.cn-north-1.vpce.amazonaws.com.cn", "cn-north-1"},
	} {
		for _, mode := range []AuthMode{AuthAmbient, AuthAssumeRole, AuthWebIdentity} {
			if _, err := resolveRoute("https://"+tc.host, Descriptor{Mode: mode, Region: tc.region}); err != nil {
				t.Errorf("%s %s: %v", mode, tc.host, err)
			}
		}
	}
}

func TestTenantEndpointCannotReceiveWorkloadIdentity(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(200) }))
	defer server.Close()
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAEXAMPLENODEKEY")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "node-secret")
	t.Setenv("AWS_SESSION_TOKEN", "node-session-token")
	for _, mode := range []AuthMode{AuthAmbient, AuthAssumeRole, AuthWebIdentity} {
		d := Descriptor{Mode: mode, Region: "eu-west-1"}
		if mode != AuthAmbient {
			d.RoleARN = "arn:aws:iam::123456789012:role/test"
		}
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		client, err := NewClient(ClientConfig{Origin: server.URL, Credential: string(raw), Deadline: time.Second, WorkloadIdentity: true, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
		if err == nil || client != nil {
			if client != nil {
				client.Forget()
			}
			t.Fatalf("%s accepted tenant endpoint", mode)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("tenant endpoint received a request")
	}
}
