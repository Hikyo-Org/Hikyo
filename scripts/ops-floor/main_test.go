package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/storagehealth"
)

func TestFloorLimitsRefuseWrongRuntime(t *testing.T) {
	for _, architecture := range []string{"arm64", "amd64"} {
		cpu := "400000 100000"
		if architecture == "amd64" {
			cpu = "200000 100000"
		}
		valid := limits{architecture, cpu, "4294967296", "0"}
		if err := valid.validate(architecture, "linux"); err != nil {
			t.Fatal(err)
		}
		for name, change := range map[string]func(*limits){"unlimited CPU": func(v *limits) { v.CPU = "max 100000" }, "unlimited memory": func(v *limits) { v.Memory = "max" }, "swap": func(v *limits) { v.Swap = "1" }, "wrong architecture": func(v *limits) { v.Architecture = "other" }} {
			t.Run(architecture+"/"+name, func(t *testing.T) {
				bad := valid
				change(&bad)
				if bad.validate(architecture, "linux") == nil {
					t.Fatal("invalid runtime accepted")
				}
			})
		}
		if valid.validate(architecture, "darwin") == nil || valid.validate("", "linux") == nil {
			t.Fatal("non-Linux or unspecified target accepted")
		}
	}
}

func TestSelectedTestsMustExecuteWithoutSkips(t *testing.T) {
	valid := "=== RUN   TestOne\n--- PASS: TestOne (0.1s)\nPASS\n"
	if err := verifyTests(valid, []string{"TestOne"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"PASS\n", strings.ReplaceAll(valid, "TestOne", "TestOther"), valid + "--- SKIP: TestOne/subcase (0.0s)\n", valid + "--- FAIL: TestOne/subcase (0.0s)\n"} {
		if verifyTests(bad, []string{"TestOne"}) == nil {
			t.Fatal("missing, failed or skipped execution accepted")
		}
	}
}

func TestDoctorEvidenceRequiresCompleteStates(t *testing.T) {
	type finding struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
	}
	type state struct {
		Status   string                 `json:"status"`
		Volume   storagehealth.Capacity `json:"measured_volume"`
		ExitCode int                    `json:"exit_code"`
		Findings []finding              `json:"findings"`
	}
	states := map[string]state{}
	for _, name := range doctorStates {
		status := "warning"
		if name == "healthy" || name == "recovered" {
			status = "ok"
		}
		if name == "backup-error" || name == "provider-error" {
			status = "error"
		}
		var findings []finding
		for _, code := range []string{"retention-prune", "project-storage", "backup-rpo", "restore-drill", "adapter-targets", "data-volume", "root-escrow", "pin-expiry", "root-rotation", "reencrypt", "database-durability", "argon2-floor"} {
			severity := "ok"
			if expectedFindings[name].Code == code {
				severity = expectedFindings[name].Severity
			}
			findings = append(findings, finding{code, severity})
		}
		if name == "provider-error" {
			findings = append(findings, finding{"metadata_expired", "error"})
		}
		states[name] = state{status, storagehealth.Capacity{TotalBytes: 100, AvailableBytes: 100}, doctorExit(status), findings}
	}
	encoded := func() []byte {
		b, err := json.Marshal(states)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if err := verifyDoctor(encoded()); err != nil {
		t.Fatal(err)
	}
	// A truly critical host remains explicitly critical through fixture
	// recovery. Independent capacity bytes must support that finding.
	originalStates := states
	states = make(map[string]state, len(originalStates))
	for name, original := range originalStates {
		changed := append([]finding(nil), original.Findings...)
		for i := range changed {
			if changed[i].Code == "data-volume" {
				changed[i].Severity = "error"
			}
		}
		states[name] = state{"error", storagehealth.Capacity{TotalBytes: 100, AvailableBytes: 5}, 4, changed}
	}
	if err := verifyDoctor(encoded()); err != nil {
		t.Fatalf("truthful critical host refused: %v", err)
	}
	falseMeasurement := states["recovered"]
	falseMeasurement.Volume.AvailableBytes = 100
	states["recovered"] = falseMeasurement
	if verifyDoctor(encoded()) == nil {
		t.Fatal("critical finding without supporting capacity accepted")
	}
	states = originalStates
	for _, name := range doctorStates {
		original := states[name]
		mutated := append([]finding(nil), original.Findings...)
		mutated[0].Severity = "error"
		states[name] = state{original.Status, original.Volume, original.ExitCode, mutated}
		if verifyDoctor(encoded()) == nil {
			t.Fatalf("%s: wrong finding severity accepted", name)
		}
		states[name] = original
	}
	provider := states["provider-error"]
	states["provider-error"] = state{provider.Status, provider.Volume, provider.ExitCode, provider.Findings[:len(provider.Findings)-1]}
	if verifyDoctor(encoded()) == nil {
		t.Fatal("missing provider severity accepted")
	}
	states["provider-error"] = provider
	recovered := states["recovered"]
	delete(states, "recovered")
	if verifyDoctor(encoded()) == nil {
		t.Fatal("omitted recovery accepted")
	}
	states["recovered"] = state{"error", recovered.Volume, 4, recovered.Findings}
	if verifyDoctor(encoded()) == nil {
		t.Fatal("failed recovery accepted")
	}
	states["recovered"] = state{"ok", recovered.Volume, 0, recovered.Findings[1:]}
	if verifyDoctor(encoded()) == nil {
		t.Fatal("partial checklist accepted")
	}
}

func doctorExit(status string) int {
	if status == "error" {
		return 4
	}
	return 0
}
