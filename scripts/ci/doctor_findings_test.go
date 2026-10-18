package ci_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type ciDoctorFinding struct {
	Provider string `json:"provider"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type ciDoctorReport struct {
	Status   string            `json:"status"`
	Findings []ciDoctorFinding `json:"findings"`
}

func TestDeploymentDoctorFindingContract(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatal("deployment doctor acceptance coverage requires jq:", err)
	}
	for _, deployment := range []struct{ engine, volume string }{
		{"sqlite", "ok"}, {"sqlite", "warn"}, {"sqlite", "error"}, {"postgres", "unknown"},
	} {
		for _, tc := range []struct {
			name, changeCode, severity string
			omit, duplicate, allowed   bool
		}{
			{name: "fresh unscheduled deployment", allowed: true},
			{name: "retention error", changeCode: "backup-retention", severity: "error"},
			{name: "retention falsely healthy", changeCode: "backup-retention", severity: "ok"},
			{name: "retention missing", changeCode: "backup-retention", omit: true},
			{name: "retention duplicate", changeCode: "backup-retention", duplicate: true},
			{name: "unrelated failure", changeCode: "adapter-targets", severity: "error"},
		} {
			t.Run(deployment.engine+"/"+deployment.volume+"/"+tc.name, func(t *testing.T) {
				report := ciDoctorReport{Status: "warning"}
				if deployment.volume == "error" {
					report.Status = "error"
				}
				for code, severity := range map[string]string{
					"adapter-targets": "ok", "argon2-floor": "ok", "backup-retention": "warn",
					"backup-rpo": "warn", "data-volume": deployment.volume, "database-durability": "ok",
					"pin-expiry": "ok", "pki": "ok", "project-storage": "ok", "reencrypt": "ok",
					"restore-drill": "warn", "retention-prune": "ok", "root-escrow": "warn", "root-rotation": "ok",
				} {
					if code == tc.changeCode && tc.omit {
						continue
					}
					if code == tc.changeCode && tc.severity != "" {
						severity = tc.severity
					}
					finding := ciDoctorFinding{Provider: "-", Code: code, Severity: severity, Message: "measured fixture finding"}
					if code == "data-volume" && severity == "error" {
						finding.Message = "volume 95.0% used"
					}
					report.Findings = append(report.Findings, finding)
					if code == tc.changeCode && tc.duplicate {
						report.Findings = append(report.Findings, finding)
					}
				}
				raw, err := json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.CommandContext(t.Context(), "jq", "-e", "--arg", "engine", deployment.engine,
					"--arg", "volume_severity", deployment.volume, "-f", filepath.Join(repositoryRoot(t), "scripts/ci/assert-doctor-findings.jq"))
				cmd.Stdin = bytes.NewReader(raw)
				output, err := cmd.CombinedOutput()
				if (err == nil) != tc.allowed || strings.TrimSpace(string(output)) != strconv.FormatBool(tc.allowed) {
					t.Fatalf("doctor acceptance: allowed=%v, error=%v, output=%s", tc.allowed, err, output)
				}
			})
		}
	}
}
