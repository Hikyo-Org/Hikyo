package api

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
)

// Login transport legs cannot quietly gain a machine/developer credential path.
// The browser-only join is additionally enforced against CLI sessions in the
// both-engine service suite; the wire vocabulary groups human session artifacts.
func TestCLILoginArtifactAdmission(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		artifacts    []string
	}{
		{http.MethodPost, "/auth/cli-login/start", []string{ArtifactNone}},
		{http.MethodPost, "/auth/cli-login/device/start", []string{ArtifactNone}},
		{http.MethodGet, "/auth/cli-login/transactions/reference", []string{ArtifactHumanSession}},
		{http.MethodPost, "/auth/cli-login/approve", []string{ArtifactHumanSession}},
		{http.MethodPost, "/auth/cli-login/redeem", []string{ArtifactNone}},
		{http.MethodPost, "/auth/cli-login/device/token", []string{ArtifactNone}},
	} {
		req := &http.Request{Method: tc.method, URL: &url.URL{Path: PathPrefix + tc.path}, Header: http.Header{}}
		matched, err := MatchRequest(req)
		if err != nil {
			t.Fatal(err)
		}
		op := matched.Operation()
		if !slices.Equal(op.Artifacts(), tc.artifacts) {
			t.Fatalf("%s artifacts=%v want%v", op.ID, op.Artifacts(), tc.artifacts)
		}
	}
}
func TestCLIDeviceWireIsRFC8628(t *testing.T) {
	doc, err := Doc()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/auth/cli-login/device/start", "/api/v1/auth/cli-login/device/token"} {
		op := doc.Paths.Value(path).Post
		if op.RequestBody.Value.Content.Get("application/x-www-form-urlencoded") == nil {
			t.Fatalf("%s lacks form transport", path)
		}
	}
	for name, required := range map[string][]string{
		"CLIDeviceStart": {"device_code", "user_code", "verification_uri", "expires_in", "interval"},
		"CLIDeviceToken": {"access_token", "token_type", "expires_in"},
	} {
		schema := doc.Components.Schemas[name].Value
		for _, field := range required {
			if !slices.Contains(schema.Required, field) {
				t.Fatalf("%s missing required %s", name, field)
			}
		}
	}
}
