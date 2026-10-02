package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/compose"
)

type syncApplyObserver struct {
	write func([]byte) (int, error)
}

func (w syncApplyObserver) Write(p []byte) (int, error) { return w.write(p) }

func syncSecurityFixture(t *testing.T) (IO, string, string) {
	t.Helper()
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	server := deliveryServer(t, deliveryResp([]apigen.DeliveredKey{{
		KeyId: "key_1", Name: "DATABASE_URL", Classification: apigen.KeyClassificationConfig,
		Presence: apigen.DeliveredKeyPresenceSet, Value: strPtr("postgres://current"),
	}}))
	t.Cleanup(server.Close)
	projectDir := t.TempDir()
	writeComposeConfig(t, projectDir, server.URL, "org_1", "prj_1", "env_1", runtimeDir, "acme")
	raw := "services:\n  api:\n    env_file:\n      - path: " + runtimeDir + "/${HIKYO_GEN_API:?render}/api.env\n        format: raw\n    labels:\n      hikyo.stamp: \"${HIKYO_GEN_API:?render}\"\n"
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stateDir := machineState(t, server.URL, SPKIFingerprint(server.Certificate()))
	docker := filepath.Join(t.TempDir(), "docker")
	// Model Compose's final-assignment-wins .env interpolation rather than
	// returning a guessed stamp. An unmanaged duplicate survives every render.
	script := fmt.Sprintf(`#!/bin/sh
case "$2" in
version) printf '2.30.0\n' ;;
config)
  stamp=
  while IFS='=' read -r name value; do
    if [ "$name" = HIKYO_GEN_API ]; then stamp=$value; fi
  done < .env
  [ -n "$stamp" ] || exit 1
  printf '{"services":{"api":{"env_file":[{"path":"%%s/%%s/api.env","format":"raw","required":true}],"labels":{"hikyo.stamp":"%%s"}}}}' %q "$stamp" "$stamp"
  ;;
up) printf 'fixture-docker-apply\n' ;;
*) exit 1 ;;
esac
`, runtimeDir)
	if err := os.WriteFile(docker, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	ios, _, stderr := composeIO(stateDir, projectDir, "wl_token", map[string]string{"HIKYO_COMPOSE_DOCKER": docker})
	if code := Run(t.Context(), ios, []string{"compose", "render"}); code != ExitOK {
		t.Fatalf("seed render exit=%d stderr=%s", code, stderr)
	}
	return ios, projectDir, filepath.Join(stateDir, "compose", "acme")
}

func TestComposeSyncRefusesPersistentResolvedStampMismatch(t *testing.T) {
	ios, projectDir, stateDir := syncSecurityFixture(t)
	path := filepath.Join(projectDir, ".env")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte("HIKYO_GEN_API=v1-11111111111111111111111111111111\n")...)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Run(t.Context(), ios, []string{"compose", "sync"}); code != ExitRefused {
		t.Fatalf("sync exit=%d stderr=%v", code, ios.Stderr)
	}
	if strings.Contains(fmt.Sprint(ios.Stderr), "fixture-docker-apply") {
		t.Fatal("Docker applied a generation that disagreed with the managed stamps")
	}
	if !strings.Contains(fmt.Sprint(ios.Stderr), "label_stamp_mismatch") || !strings.Contains(fmt.Sprint(ios.Stderr), "stamp_mismatch") {
		t.Fatalf("missing canonical structural refusal: %v", ios.Stderr)
	}
	if !applyPendingExists(stateDir) {
		t.Fatal("refused apply cleared the pending marker")
	}
	if applied, err := loadAppliedStamps(stateDir); err != nil || len(applied) != 0 {
		t.Fatalf("refused apply recorded success: %v err=%v", applied, err)
	}
}

func TestComposeSyncRetainsProjectLockThroughDockerApply(t *testing.T) {
	ios, projectDir, stateDir := syncSecurityFixture(t)
	originalStderr := ios.Stderr
	applyCount := 0
	ios.Stderr = syncApplyObserver{write: func(p []byte) (int, error) {
		if strings.Contains(string(p), "fixture-docker-apply") {
			applyCount++
			if lock, err := compose.NewWriter(stateDir, nil).BeginRender(projectDir); err == nil {
				lock.Close()
				t.Error("project lock became available while Docker apply was running")
			}
			if !applyPendingExists(stateDir) {
				t.Error("apply-pending marker absent during Docker apply")
			}
			other := ios
			other.Stderr = &strings.Builder{}
			if code := Run(t.Context(), other, []string{"compose", "render"}); code != ExitRefused {
				t.Errorf("concurrent render exit=%d, want refusal while applying", code)
			}
		}
		return originalStderr.Write(p)
	}}
	if code := Run(t.Context(), ios, []string{"compose", "sync"}); code != ExitOK {
		t.Fatalf("sync exit=%d stderr=%v", code, originalStderr)
	}
	if applyCount != 1 || applyPendingExists(stateDir) {
		t.Fatalf("apply count=%d pending=%v", applyCount, applyPendingExists(stateDir))
	}
	current, err := compose.CurrentStamps(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := loadAppliedStamps(stateDir)
	if err != nil || stampsNeedApply(current, applied) {
		t.Fatalf("applied=%v current=%v err=%v", applied, current, err)
	}
	lock, err := compose.NewWriter(stateDir, nil).BeginRender(projectDir)
	if err != nil {
		t.Fatalf("completed sync retained project lock: %v", err)
	}
	lock.Close()
	if code := Run(t.Context(), ios, []string{"compose", "sync"}); code != ExitOK || applyCount != 1 {
		t.Fatalf("unchanged sync exit=%d apply count=%d stderr=%v", code, applyCount, originalStderr)
	}
}
