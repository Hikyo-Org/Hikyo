package app

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestLegacyDrillFixtureRefusesPendingAuthenticationEvidence(t *testing.T) {
	const childVariable = "HIKYO_TEST_LEGACY_REFUSAL_EVIDENCE"
	if engine := os.Getenv(childVariable); engine != "" {
		newUpgradeDrillFixture(t, store.Engine(engine), true, true, func(db *store.DB) {
			drillExec(t, db, `UPDATE auth_refusal_window SET window_start=60,state='{"counts":[1,0,0,0,0],"samples":[]}' WHERE id=1`)
		})
		t.Fatal("legacy fixture discarded authentication evidence")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		if engine == store.EnginePostgres && os.Getenv("HIKYO_TEST_POSTGRES_DSN") == "" {
			if os.Getenv("CI") != "" {
				t.Fatal("CI requires HIKYO_TEST_POSTGRES_DSN")
			}
			continue
		}
		t.Run(string(engine), func(t *testing.T) {
			command := exec.CommandContext(t.Context(), executable, "-test.run=^TestLegacyDrillFixtureRefusesPendingAuthenticationEvidence$")
			command.Env = append(os.Environ(), childVariable+"="+string(engine))
			output, err := command.CombinedOutput()
			if err == nil || !bytes.Contains(output, []byte("legacy drill fixture cannot discard")) || !bytes.Contains(output, []byte("SELECT COUNT(*) FROM auth_refusal_window")) {
				t.Fatalf("legacy fixture did not retain authentication evidence: %v\n%s", err, output)
			}
		})
	}
}
