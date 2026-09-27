package cli_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/cli"
)

func TestGeneratedCertificateHumanRefusesBeforeIssuanceOrRevealChecks(t *testing.T) {
	requests := 0
	ios, _, stderr := definitionsTestIO(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		// An issue-certificate principal need not hold reveal authority. Neither
		// that unrelated check nor the irreversible issuance may be attempted.
		http.Error(w, "no reveal grant", http.StatusForbidden)
	}))
	code := cli.Run(t.Context(), ios, []string{"cert", "issue", "--profile", "web", "--generate-key", "--dangerously-print", "--instance", "local", "--org", "org_70", "--project", "prj_70", "--env", "env_70"})
	if code != cli.ExitAuth || requests != 0 || !strings.Contains(stderr.String(), "browser's mint ceremony") {
		t.Fatalf("code=%d requests=%d stderr=%s", code, requests, stderr)
	}
}
