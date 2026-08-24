package updater

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExampleProfilesReferenceShippedExecutableAdapters(t *testing.T) {
	root := filepath.Join("..", "..", "install", "updater")
	for _, backend := range []Backend{BackendFlux, BackendCompose, BackendSystemd} {
		profilePath := filepath.Join(root, string(backend)+".json.example")
		raw, err := os.ReadFile(profilePath)
		if err != nil {
			t.Fatal(err)
		}
		var config Config
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		if err := config.Validate(); err != nil {
			t.Fatal(err)
		}
		adapter := "hikyo-update-" + string(backend)
		for _, command := range []Command{
			config.Commands.Plan, config.Commands.Backup, config.Commands.Verify,
			config.Commands.Apply, config.Commands.Health, config.Commands.Rollback,
		} {
			if filepath.Base(command.Name) != adapter {
				t.Fatalf("%s references %q, want shipped %s", profilePath, command.Name, adapter)
			}
		}
		assertExecutableShell(t, filepath.Join(root, adapter))
	}
	assertExecutableShell(t, filepath.Join(root, "hikyo-update-common"))
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
