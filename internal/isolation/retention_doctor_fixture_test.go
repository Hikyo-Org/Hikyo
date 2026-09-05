package isolation

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/storagehealth"
)

type retentionDoctorFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type retentionDoctorOutput struct {
	Status   string                   `json:"status"`
	Findings []retentionDoctorFinding `json:"findings"`
}

// Only a correctly measured critical SQLite volume may explain ExitRefused.
// The host is sampled independently after the CLI returns. A tenth-percent
// tolerance permits rounding and concurrent host writes, without crossing the
// real 90% critical threshold in either sample or accepting a different error.
func validateRetentionDoctor(raw string, exit int, capacity storagehealth.Capacity, pruneSeverity, pruneMessage string) error {
	var output retentionDoctorOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return fmt.Errorf("invalid doctor JSON: %w", err)
	}
	findings := make(map[string]retentionDoctorFinding)
	status := "ok"
	for _, finding := range output.Findings {
		if _, exists := findings[finding.Code]; exists {
			return fmt.Errorf("duplicate finding %s", finding.Code)
		}
		findings[finding.Code] = finding
		switch finding.Severity {
		case "ok":
		case "warn", "unknown":
			if status == "ok" {
				status = "warning"
			}
		case "error":
			if finding.Code != "data-volume" {
				return fmt.Errorf("unexpected error finding %s", finding.Code)
			}
			measured := storagehealth.FromCapacity(capacity)
			var used float64
			var available uint64
			const format = "Datastore volume %.1f%% used; %d bytes available; at or above the 90%% critical storage threshold"
			n, err := fmt.Sscanf(finding.Message, "Datastore volume %f%% used; %d bytes available", &used, &available)
			reported := storagehealth.FromCapacity(storagehealth.Capacity{TotalBytes: capacity.TotalBytes, AvailableBytes: available})
			if err != nil || n != 2 || !measured.Known || !reported.Known || measured.UsedPercent < 90 ||
				reported.UsedPercent < 90 || math.Abs(measured.UsedPercent-reported.UsedPercent) > 0.1 ||
				fmt.Sprintf(format, reported.UsedPercent, available) != finding.Message ||
				fmt.Sprintf("%.1f", used) != fmt.Sprintf("%.1f", reported.UsedPercent) {
				return fmt.Errorf("data-volume refusal does not match independently measured critical capacity")
			}
			status = "error"
		default:
			return fmt.Errorf("invalid severity for %s", finding.Code)
		}
	}
	prune, exists := findings["retention-prune"]
	if !exists || prune.Severity != pruneSeverity || !strings.Contains(prune.Message, pruneMessage) {
		return fmt.Errorf("retention-prune does not match expected %s state", pruneSeverity)
	}
	if storage, exists := findings["project-storage"]; !exists || storage.Severity != "ok" {
		return fmt.Errorf("project-storage is not healthy")
	}
	wantExit := cli.ExitOK
	if status == "error" {
		wantExit = cli.ExitRefused
	}
	if exit != wantExit || output.Status != status {
		return fmt.Errorf("doctor exit/status inconsistent with findings")
	}
	return nil
}

func TestRetentionDoctorFixtureKeepsOtherRefusalsBlocking(t *testing.T) {
	const highVolume = "Datastore volume 95.0% used; 5 bytes available; at or above the 90% critical storage threshold"
	base := retentionDoctorOutput{Status: "error", Findings: []retentionDoctorFinding{
		{Code: "retention-prune", Severity: "ok", Message: "last_prune_success is 0s old"},
		{Code: "project-storage", Severity: "ok"},
		{Code: "data-volume", Severity: "error", Message: highVolume},
	}}
	for _, test := range []struct {
		name     string
		change   func(*retentionDoctorOutput)
		capacity storagehealth.Capacity
		exit     int
		valid    bool
	}{
		{"measured critical volume", func(*retentionDoctorOutput) {}, storagehealth.Capacity{TotalBytes: 100, AvailableBytes: 5}, cli.ExitRefused, true},
		{"roomy host cannot excuse refusal", func(*retentionDoctorOutput) {}, storagehealth.Capacity{TotalBytes: 100, AvailableBytes: 50}, cli.ExitRefused, false},
		{"unknown remote volume cannot excuse refusal", func(*retentionDoctorOutput) {}, storagehealth.Capacity{}, cli.ExitRefused, false},
		{"unrelated error still fails", func(o *retentionDoctorOutput) {
			o.Findings = append(o.Findings, retentionDoctorFinding{Code: "root-escrow", Severity: "error"})
		}, storagehealth.Capacity{TotalBytes: 100, AvailableBytes: 5}, cli.ExitRefused, false},
		{"wrong prune still fails", func(o *retentionDoctorOutput) { o.Findings[0].Severity = "warn" }, storagehealth.Capacity{TotalBytes: 100, AvailableBytes: 5}, cli.ExitRefused, false},
		{"success exit cannot hide critical finding", func(*retentionDoctorOutput) {}, storagehealth.Capacity{TotalBytes: 100, AvailableBytes: 5}, cli.ExitOK, false},
		{"status cannot hide critical finding", func(o *retentionDoctorOutput) { o.Status = "warning" }, storagehealth.Capacity{TotalBytes: 100, AvailableBytes: 5}, cli.ExitRefused, false},
		{"claimed percentage must match capacity", func(o *retentionDoctorOutput) { o.Findings[2].Message = strings.Replace(highVolume, "95.0", "91.0", 1) }, storagehealth.Capacity{TotalBytes: 100, AvailableBytes: 5}, cli.ExitRefused, false},
		{"warning-only healthy volume succeeds", func(o *retentionDoctorOutput) {
			o.Status = "warning"
			o.Findings[2] = retentionDoctorFinding{Code: "backup-rpo", Severity: "warn"}
		}, storagehealth.Capacity{TotalBytes: 100, AvailableBytes: 50}, cli.ExitOK, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := retentionDoctorOutput{Status: base.Status, Findings: append([]retentionDoctorFinding(nil), base.Findings...)}
			test.change(&output)
			raw, err := json.Marshal(output)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateRetentionDoctor(string(raw), test.exit, test.capacity, "ok", "last_prune_success is 0s old"); (err == nil) != test.valid {
				t.Fatalf("valid=%t, error=%v", test.valid, err)
			}
		})
	}
}
