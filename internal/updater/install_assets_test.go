package updater

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoricalExampleProfilesReferenceShippedRefusalAdapters(t *testing.T) {
	root := filepath.Join("..", "..", "install", "updater")
	for _, backend := range []Backend{BackendFlux, BackendCompose, BackendSystemd} {
		profilePath := filepath.Join(root, string(backend)+".json.example")
		raw, err := os.ReadFile(profilePath)
		if err != nil {
			t.Fatal(err)
		}
		var profile struct {
			Backend  Backend            `json:"backend"`
			Commands map[string]Command `json:"commands"`
		}
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatal(err)
		}
		if profile.Backend != backend || len(profile.Commands) != 6 {
			t.Fatalf("%s does not describe its historical backend and six phases", profilePath)
		}
		adapter := "hikyo-update-" + string(backend)
		for _, phase := range []string{"plan", "backup", "verify", "apply", "health", "rollback"} {
			command, present := profile.Commands[phase]
			if !present {
				t.Fatalf("%s is missing historical phase %s", profilePath, phase)
			}
			if filepath.Base(command.Name) != adapter {
				t.Fatalf("%s references %q, want shipped %s", profilePath, command.Name, adapter)
			}
		}
		assertExecutableShell(t, filepath.Join(root, adapter))
	}
	assertExecutableShell(t, filepath.Join(root, "hikyo-update-common"))
}

func TestRetiredAdaptersRefuseEveryPhaseWithoutDeploymentSideEffects(t *testing.T) {
	for _, backend := range []string{"compose", "systemd", "flux", "common"} {
		for _, phase := range []string{"plan", "backup", "verify", "apply", "health", "rollback"} {
			t.Run(backend+"/"+phase, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join("..", "..", "install", "updater", "hikyo-update-"+backend)
				cmd := exec.Command("/bin/sh", path, phase, "1.2.3", "https://github.com/Hikyo-Org/Hikyo/releases/tag/v1.2.3", "upd_legacy")
				// No deployment binary is available, and no configured workspace
				// may be populated before the refusal.
				cmd.Env = []string{"PATH=/nonexistent", "HIKYO_UPDATE_WORK_DIR=" + dir}
				output, err := cmd.CombinedOutput()
				if err == nil || !strings.Contains(string(output), "migration-safe rollback") || !strings.Contains(string(output), "https://hikyo.app/docs/upgrades/") {
					t.Fatalf("retired phase error=%v output=%s", err, output)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("retired phase changed deployment workspace: entries=%v err=%v", entries, err)
				}
			})
		}
	}
}

func assertExecutableShell(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("%s is not executable", path)
	}
	if out, err := exec.Command("/bin/sh", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("%s syntax: %v\n%s", path, err, out)
	}
}
