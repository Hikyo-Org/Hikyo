package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestCheckBindingAgainstIssuerGitHubPins(t *testing.T) {
	issuer := authz.FederationIssuer{Type: domain.IssuerGitHubActions}
	const full = `{"repository_id":"74","repository_owner_id":"65","event_name":"push",` +
		`"ref_protected":"true","runner_environment":"github-hosted","job_workflow_ref":"o/r/.github/workflows/x.yml@refs/heads/main"}`
	if err := checkBindingAgainstIssuer(issuer, "hikyo://x", full); err != nil {
		t.Fatalf("full string pin set = %v, want acceptance", err)
	}
	for name, claims := range map[string]string{
		"missing ref_protected":      `{"repository_id":"74","repository_owner_id":"65","event_name":"push","runner_environment":"github-hosted","job_workflow_ref":"w"}`,
		"missing runner_environment": `{"repository_id":"74","repository_owner_id":"65","event_name":"push","ref_protected":"true","job_workflow_ref":"w"}`,
		"missing job_workflow_ref":   `{"repository_id":"74","repository_owner_id":"65","event_name":"push","ref_protected":"true","runner_environment":"github-hosted"}`,
	} {
		if err := checkBindingAgainstIssuer(issuer, "hikyo://x", claims); !errors.Is(err, ErrBindingImmutableID) {
			t.Errorf("%s = %v, want ErrBindingImmutableID", name, err)
		}
	}
	// Regression: the web UI once pinned repository_id as a number, which GitHub's
	// string claim can never match.
	for name, want := range map[string]string{
		"repository_id": strings.Replace(full, `"repository_id":"74"`, `"repository_id":74`, 1),
		"ref_protected": strings.Replace(full, `"ref_protected":"true"`, `"ref_protected":true`, 1),
	} {
		err := checkBindingAgainstIssuer(issuer, "hikyo://x", want)
		if !errors.Is(err, ErrBindingPinKind) || !strings.Contains(err.Error(), name) {
			t.Errorf("non-string %s = %v, want ErrBindingPinKind naming the claim", name, err)
		}
	}
}
