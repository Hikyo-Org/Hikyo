package upgradegate

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

// This builds and executes the actual multicall CLI. Development has separately
// signed installation custody but shares production's boot/migration dispatch.
// Production signed-envelope and phase crash acceptance live in gate_test and
// gate_process_test; this test never replaces the embedded build verifier.
func TestActualCLIMigrateBootRestart(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "hikyo")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../cmd/hikyo")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := gateConfig(t, engine)
			stateDir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(stateDir, 0700); err != nil {
				t.Fatal(err)
			}
			db := "sqlite:" + cfg.Path
			if engine == releaseidentity.Postgres {
				db = cfg.DSN
			}
			env := cliProcessEnvironment(db, stateDir)
			command := exec.CommandContext(t.Context(), binary, "migrate", "--dev")
			command.Env = env
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("actual CLI migration failed: %s", strings.ReplaceAll(string(output), db, "[test datastore]"))
			}
			if !strings.Contains(string(output), "maintenance=true") || !strings.Contains(string(output), "phase=schema-applied") {
				t.Fatal("CLI did not report schema-only maintenance")
			}
			migrated, err := upgrade.InspectControl(t.Context(), cfg)
			if err != nil || !migrated.Maintenance || migrated.Pending.Phase != upgrade.SchemaApplied {
				t.Fatal("CLI migration did not persist schema-applied maintenance")
			}
			for restart := range 2 {
				stop := startActualCLI(t, binary, env)
				state, err := upgrade.InspectControl(t.Context(), cfg)
				if err != nil || state.Maintenance || state.Pending.Phase != upgrade.Healthy || state.Generation != migrated.Generation {
					stop()
					t.Fatalf("boot %d did not preserve exact healthy generation", restart)
				}
				stop() // Actual abrupt process death, followed by a fresh boot.
			}
			before, err := upgrade.InspectControl(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			wrongRoot := exec.CommandContext(t.Context(), binary, "server", "--dev")
			wrongRoot.Env = append(env, "HIKYO_ROOT_KEY="+strings.Repeat("a5", 32))
			if err := wrongRoot.Run(); err == nil {
				t.Fatal("wrong root admitted actual CLI")
			}
			after, err := upgrade.InspectControl(t.Context(), cfg)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("wrong-root refusal changed release authority")
			}
		})
	}
}

func cliProcessEnvironment(db, stateDir string) []string {
	var env []string
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "HIKYO_") {
			env = append(env, value)
		}
	}
	return append(env, "HIKYO_DB="+db, "HIKYO_UPGRADE_STATE_DIR="+stateDir,
		"HIKYO_LISTEN=localhost:0", "HIKYO_OPERATIONAL_LISTEN=127.0.0.1:0")
}

func startActualCLI(t *testing.T, binary string, env []string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	logPath := filepath.Join(t.TempDir(), "server.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary, "server", "--dev")
	command.Env, command.Stdout, command.Stderr = env, log, log
	if err := command.Start(); err != nil {
		cancel()
		log.Close()
		t.Fatal("start actual CLI")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		<-done
		log.Close()
	}
	t.Cleanup(stop)
	address := regexp.MustCompile(`operational_addr=(127\.0\.0\.1:[0-9]+)`)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	client := &http.Client{Timeout: time.Second}
	for {
		select {
		case err := <-done:
			stopped = true
			cancel()
			log.Close()
			raw, _ := os.ReadFile(logPath)
			message := string(raw)
			for _, value := range env {
				if datastore, ok := strings.CutPrefix(value, "HIKYO_DB="); ok {
					message = strings.ReplaceAll(message, datastore, "[test datastore]")
				}
			}
			t.Fatalf("CLI exited before readiness: %v\n%s", err, message)
		case <-ctx.Done():
			t.Fatal("CLI did not become ready before deadline")
		case <-tick.C:
			raw, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			match := address.FindSubmatch(raw)
			if len(match) != 2 {
				continue
			}
			response, err := client.Get("http://" + string(match[1]) + "/readyz")
			if err != nil {
				continue
			}
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return stop
			}
		}
	}
}
