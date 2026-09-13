//go:build darwin || linux

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

func TestAutomaticBundleFormatRejectsHistoricalCandidateBeforeApplication(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		accepted     bool
	}{
		{"platform-aware", upgradebundle.IndexFormat + "\\n" + upgradebundle.PlatformIndexFormat, true},
		{"historical", upgradebundle.IndexFormat, false},
		{"unknown-command", "unknown command", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "candidate")
			script := "#!/bin/sh\n[ \"$1\" = --upgrade-bundle-formats ] || exit 2\nprintf '" + tc.output + "\\n'\n"
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			if err := checkAutomaticBundleFormat(t.Context(), binary); (err == nil) != tc.accepted {
				t.Fatalf("accepted=%t, err=%v", tc.accepted, err)
			}
		})
	}
}

func TestUnattendedPreflightRequiresEverySelectedExecutable(t *testing.T) {
	for _, failure := range []string{"missing", "mismatched", "substituted", "historical"} {
		t.Run(failure, func(t *testing.T) {
			route, _, _, _ := automaticApplyFixture(t, 2)
			for _, step := range route.Plan.Steps() {
				prepared := route.Executables[step.Target]
				prepared.BinaryPath = filepath.Join(t.TempDir(), "candidate")
				script := []byte("#!/bin/sh\nprintf '" + upgradebundle.PlatformIndexFormat + "\\n'\n")
				if err := os.WriteFile(prepared.BinaryPath, script, 0700); err != nil {
					t.Fatal(err)
				}
				prepared.BinarySHA256 = releaseidentity.Hash(script)
				route.Executables[step.Target] = prepared
			}
			if err := preflightUnattendedRoute(t.Context(), route); err != nil {
				t.Fatalf("valid route refused: %v", err)
			}
			first := route.Plan.Steps()[0].Target
			prepared := route.Executables[first]
			switch failure {
			case "missing":
				delete(route.Executables, first)
			case "mismatched":
				prepared.Identity = route.Plan.Target()
				route.Executables[first] = prepared
			case "substituted", "historical":
				script := []byte("#!/bin/sh\nprintf '" + upgradebundle.IndexFormat + "\\n'\n")
				if err := os.WriteFile(prepared.BinaryPath, script, 0700); err != nil {
					t.Fatal(err)
				}
				if failure == "historical" {
					prepared.BinarySHA256 = releaseidentity.Hash(script)
					route.Executables[first] = prepared
				}
			}
			if err := preflightUnattendedRoute(t.Context(), route); err == nil {
				t.Fatal("invalid selected intermediate passed preflight")
			}
		})
	}
}
