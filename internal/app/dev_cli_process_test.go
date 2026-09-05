package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// Exercise the real default CLI configuration, including its relative SQLite
// and root-key paths. Supplying absolute fixture paths hid this regression.
func TestActualDefaultDevCLIStartsAndRestarts(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "hikyo")
	build := exec.CommandContext(t.Context(), "go", "build", "-p", "1", "-o", binary, "../../cmd/hikyo")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual CLI: %v\n%s", err, output)
	}
	directory := t.TempDir()
	var env []string
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "HIKYO_") {
			env = append(env, value)
		}
	}
	var root []byte
	var generation int64
	for attempt := range 2 {
		stop := startDefaultDevCLI(t, binary, directory, env)
		state, err := upgrade.InspectControl(t.Context(), upgrade.Config{Engine: releaseidentity.SQLite, Path: filepath.Join(directory, "hikyo-dev.db")})
		if err != nil || state.Maintenance || state.Pending == nil || state.Pending.Phase != upgrade.Healthy || state.TrustDomain != upgrade.LocalDevelopment {
			stop()
			t.Fatalf("default dev startup %d not admitted: %v", attempt, err)
		}
		current, err := os.ReadFile(filepath.Join(directory, devRootKeyName))
		if err != nil {
			stop()
			t.Fatal(err)
		}
		if attempt == 0 {
			root = current
			generation = state.Generation
			// Merely targeting a development database never opts a host
			// command into that trust domain.
			refused := exec.CommandContext(t.Context(), binary, "admin", "create", "--username", "implicit-admin", "--output-file", filepath.Join(directory, "refused-authority.txt"))
			refused.Dir = directory
			refused.Env = append(slices.Clone(env), "HIKYO_DB=sqlite:"+filepath.Join(directory, "hikyo-dev.db"))
			if err := refused.Run(); err == nil {
				stop()
				t.Fatal("host command implicitly adopted development datastore")
			}
			afterRefusal, err := upgrade.InspectControl(t.Context(), upgrade.Config{Engine: releaseidentity.SQLite, Path: filepath.Join(directory, "hikyo-dev.db")})
			if err != nil || !reflect.DeepEqual(state, afterRefusal) {
				stop()
				t.Fatal("omitted dev flag changed datastore authority", err)
			}
			authority := filepath.Join(directory, "administrator-authority.txt")
			create := exec.CommandContext(t.Context(), binary, "admin", "--dev", "create", "--username", "default-admin", "--display-name", "--dev", "--output-file", authority)
			create.Dir, create.Env = directory, env
			if output, err := create.CombinedOutput(); err != nil {
				stop()
				t.Fatalf("actual explicit-dev admin creation failed: %v\n%s", err, output)
			}
			info, err := os.Stat(authority)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() == 0 {
				stop()
				t.Fatal("admin authority was not delivered to private output file", err)
			}
		} else if !bytes.Equal(root, current) || generation != state.Generation {
			stop()
			t.Fatal("default dev restart replaced root or advanced generation")
		}
		stop()
	}
}

func startDefaultDevCLI(t *testing.T, binary, directory string, env []string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	path := filepath.Join(t.TempDir(), "server.log")
	log, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary, "server", "--dev", "--listen", "localhost:0", "--operational-listen", "127.0.0.1:0")
	command.Dir, command.Env, command.Stdout, command.Stderr = directory, env, log, log
	if err := command.Start(); err != nil {
		cancel()
		log.Close()
		t.Fatal(err)
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
	pattern := regexp.MustCompile(`operational_addr=(127\.0\.0\.1:[0-9]+)`)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second}
	for {
		select {
		case err := <-done:
			stopped = true
			cancel()
			log.Close()
			raw, _ := os.ReadFile(path)
			t.Fatalf("default dev CLI exited before readiness: %v\n%s", err, raw)
		case <-ctx.Done():
			t.Fatal("default dev CLI readiness deadline")
		case <-ticker.C:
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			address := pattern.FindSubmatch(raw)
			if len(address) != 2 {
				continue
			}
			response, err := client.Get("http://" + string(address[1]) + "/readyz")
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

func TestDefaultDevelopmentCustodyRefusesSymlink(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	target := t.TempDir()
	sentinel := filepath.Join(target, "operator-owned")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, ".hikyo-development")); err != nil {
		t.Fatal(err)
	}
	cfg := devConfig(t)
	cfg.Store.Path = "hikyo-dev.db"
	_, cleanup, err := upgradeRequest(t.Context(), cfg, nil, upgradegate.Migrate)
	cleanup()
	if err == nil {
		t.Fatal("default custody followed a symlink")
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 1 || entries[0].Name() != "operator-owned" {
		t.Fatal("refusal wrote into symlink target", err)
	}
	contents, err := os.ReadFile(sentinel)
	if err != nil || string(contents) != "unchanged" {
		t.Fatal("refusal changed symlink target", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "hikyo-dev.db")); !os.IsNotExist(err) {
		t.Fatal("refused custody created datastore")
	}
}
