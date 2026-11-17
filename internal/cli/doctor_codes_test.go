package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// TestDoctorCodesAreInTheDeploymentAcceptanceList keeps the CI acceptance filter
// (scripts/ci/assert-doctor-findings.jq) in step with the findings doctor really
// emits: a new client-side finding that the filter does not list fails the
// deployment acceptance job, so it must fail here first.
func TestDoctorCodesAreInTheDeploymentAcceptanceList(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "scripts", "ci", "assert-doctor-findings.jq"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)def expected_codes:\s*\[(.*?)\]`).FindSubmatch(raw)
	if block == nil {
		t.Fatal("expected_codes list not found")
	}
	listed := regexp.MustCompile(`"([a-z0-9-]+)"`).FindAllStringSubmatch(string(block[1]), -1)
	var expected []string
	for _, m := range listed {
		expected = append(expected, m[1])
	}
	now := time.Now().UTC()
	zero := 0
	health := apigen.RetentionHealth{LastPruneSuccess: &now, Backup: healthyBackup(now), PkiCertificatesUnknown: &zero, PkiIssuersOnHold: &zero,
		RotationHalted: &zero, RotationRestoreHeld: &zero, RotationRunsQuarantined: &zero, RotationRunsRevokeRefused: &zero, RotationRunsUnknownOutcome: &zero, RotationOverdue: &zero, RotationCredentialsExpiring: &zero}
	result, _ := doctorResults(apigen.SamlProviderList{Providers: []apigen.SamlProvider{}}, health, now)
	for _, f := range result.Findings {
		// "diagnostics" is the placeholder for a server that sent no findings; the
		// acceptance run always has a measured server.
		if f.Code == "diagnostics" || strings.HasPrefix(f.Code, "saml") {
			continue
		}
		if !slices.Contains(expected, f.Code) {
			t.Errorf("doctor emits %q but assert-doctor-findings.jq does not list it", f.Code)
		}
	}
}
