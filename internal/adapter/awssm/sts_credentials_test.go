package awssm

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

const (
	stsTestOrigin  = "https://secretsmanager.eu-west-1.amazonaws.com"
	stsTestHost    = "sts.eu-west-1.amazonaws.com"
	stsTestRoleARN = "arn:aws:iam::123456789012:role/hikyo-test"
	stsTestToken   = "synthetic-web-identity-token"
)

// stsRecorder is a controlled STS: a vetted client whose transport answers
// from an in-process handler, after asserting the request was addressed to the
// AWS regional STS host the product selects for workload identity.
type stsRecorder struct {
	mu     sync.Mutex
	forms  []url.Values
	header http.Header
	hosts  []string
	reply  func(w http.ResponseWriter, action string)
}

func (s *stsRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.forms = append(s.forms, r.PostForm)
		s.header = r.Header.Clone()
		s.hosts = append(s.hosts, r.URL.Scheme+"://"+r.URL.Host)
		s.mu.Unlock()
		s.reply(w, r.PostForm.Get("Action"))
	}).ServeHTTP(rec, req)
	return rec.Result(), nil
}

func stsSuccess(w http.ResponseWriter, action string) {
	w.Header().Set("Content-Type", "text/xml")
	_, _ = fmt.Fprintf(w, `<%[1]sResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><%[1]sResult><Credentials>`+
		`<AccessKeyId>AKIDSTSSYNTHETIC</AccessKeyId><SecretAccessKey>synthetic-secret</SecretAccessKey>`+
		`<SessionToken>synthetic-session</SessionToken><Expiration>2099-01-01T00:00:00Z</Expiration>`+
		`</Credentials></%[1]sResult></%[1]sResponse>`, action)
}

func stsAccessDenied(w http.ResponseWriter, _ string) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusForbidden)
	_, _ = fmt.Fprint(w, `<ErrorResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><Error><Type>Sender</Type>`+
		`<Code>AccessDenied</Code><Message>PRIVATE_STS_CANARY</Message></Error><RequestId>req-1</RequestId></ErrorResponse>`)
}

// isolateAmbient points the SDK default chain at one static key and nothing
// else, so no test reaches IMDS, ECS, a profile, or a real web identity file.
func isolateAmbient(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_CONFIG_FILE", empty)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", empty)
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "")
	t.Setenv("AWS_ROLE_ARN", "")
	t.Setenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "")
	t.Setenv("AWS_CONTAINER_CREDENTIALS_FULL_URI", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDSOURCESYNTHETIC")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "source-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
}

func TestSTSCredentialAcquisition(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(stsTestToken), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		d        Descriptor
		action   string
		duration string
		wantForm map[string]string
		signed   bool
	}{
		{
			name:     "assume-role carries role, external id, default duration and session name",
			d:        Descriptor{Mode: AuthAssumeRole, Region: "eu-west-1", RoleARN: stsTestRoleARN, ExternalID: "tenant-external-id"},
			action:   "AssumeRole",
			duration: "900",
			wantForm: map[string]string{"RoleArn": stsTestRoleARN, "ExternalId": "tenant-external-id", "RoleSessionName": roleSessionName},
			signed:   true,
		},
		{
			name:     "assume-role carries a configured duration and omits an absent external id",
			d:        Descriptor{Mode: AuthAssumeRole, Region: "eu-west-1", RoleARN: stsTestRoleARN, SessionSeconds: 1800},
			action:   "AssumeRole",
			duration: "1800",
			wantForm: map[string]string{"RoleArn": stsTestRoleARN, "ExternalId": "", "RoleSessionName": roleSessionName},
			signed:   true,
		},
		{
			name:     "web-identity carries role, session name, duration and the token file contents",
			d:        Descriptor{Mode: AuthWebIdentity, Region: "eu-west-1", RoleARN: stsTestRoleARN, SessionSeconds: 3600},
			action:   "AssumeRoleWithWebIdentity",
			duration: "3600",
			wantForm: map[string]string{"RoleArn": stsTestRoleARN, "RoleSessionName": roleSessionName, "WebIdentityToken": stsTestToken},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateAmbient(t)
			if tc.d.Mode == AuthWebIdentity {
				t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", tokenFile)
			}
			r, err := resolveRoute(stsTestOrigin, tc.d)
			if err != nil {
				t.Fatal(err)
			}
			if r.sts != "https://"+stsTestHost {
				t.Fatalf("workload identity routed to %q, want the AWS regional STS", r.sts)
			}
			sts := &stsRecorder{reply: stsSuccess}
			provider, err := credentialProvider(tc.d, r, &http.Client{Transport: sts}, true)
			if err != nil {
				t.Fatal(err)
			}
			creds, err := provider.Retrieve(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if creds.AccessKeyID != "AKIDSTSSYNTHETIC" || creds.SessionToken != "synthetic-session" {
				t.Fatalf("credentials were not the STS answer: %q", creds.AccessKeyID)
			}
			if len(sts.forms) != 1 || sts.hosts[0] != "https://"+stsTestHost {
				t.Fatalf("want one request to https://%s, got %v", stsTestHost, sts.hosts)
			}
			form := sts.forms[0]
			if form.Get("Action") != tc.action || form.Get("Version") != "2011-06-15" {
				t.Fatalf("Action/Version = %q/%q, want %s/2011-06-15", form.Get("Action"), form.Get("Version"), tc.action)
			}
			if form.Get("DurationSeconds") != tc.duration {
				t.Errorf("DurationSeconds = %q, want %q (descriptor sessionDuration)", form.Get("DurationSeconds"), tc.duration)
			}
			for key, want := range tc.wantForm {
				if form.Get(key) != want {
					t.Errorf("%s = %q, want %q", key, form.Get(key), want)
				}
			}
			auth := sts.header.Get("Authorization")
			if tc.signed != strings.Contains(auth, "Credential=AKIDSOURCESYNTHETIC/") {
				t.Errorf("Authorization %q: signed-with-source-credentials = %v, want %v", auth, !tc.signed, tc.signed)
			}
			if !tc.signed && auth != "" {
				t.Errorf("web identity request must be unsigned, got Authorization %q", auth)
			}
		})
	}
}

func TestSTSCredentialAccessDeniedIsAuthClass(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte(stsTestToken), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, d := range []Descriptor{
		{Mode: AuthAssumeRole, Region: "eu-west-1", RoleARN: stsTestRoleARN, ExternalID: "tenant-external-id"},
		{Mode: AuthWebIdentity, Region: "eu-west-1", RoleARN: stsTestRoleARN},
	} {
		t.Run(string(d.Mode), func(t *testing.T) {
			isolateAmbient(t)
			t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", map[bool]string{true: tokenFile}[d.Mode == AuthWebIdentity])
			r, err := resolveRoute(stsTestOrigin, d)
			if err != nil {
				t.Fatal(err)
			}
			sts := &stsRecorder{reply: stsAccessDenied}
			provider, err := credentialProvider(d, r, &http.Client{Transport: sts}, true)
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Retrieve(t.Context())
			if err == nil {
				t.Fatal("AccessDenied from STS produced credentials")
			}
			// Client.do classifies every Retrieve failure through credentialError.
			err = credentialError(err)
			if !errors.Is(err, adapter.ErrProviderAuth) {
				t.Fatalf("AccessDenied mapped to %v, want adapter.ErrProviderAuth", err)
			}
			if strings.Contains(err.Error(), "PRIVATE_STS_CANARY") {
				t.Fatal("STS error message leaked into the adapter error")
			}
		})
	}
}
