package upgradegate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// Child configuration stays in an owned mode-0600 file. Database credentials
// never enter process arguments, test names or diagnostic output.
type gateProcessConfig struct {
	Store             upgrade.Config
	Directory         string
	Pinned            releasetrust.PinnedTrust
	Claim             []byte
	Identity          releaseidentity.Identity
	Boundary          durableBoundary
	Marker            string
	Root              []byte
	Target            releaseidentity.Identity
	Evidence          backupreceipt.EvidenceMaterial
	CiphertextPath    string
	OperatorPublicKey []byte
	OperatorInstance  string
}

func TestGateCrashChild(t *testing.T) {
	path := os.Getenv("HIKYO_GATE_PROCESS_CONFIG")
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read child configuration")
	}
	var cfg gateProcessConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal("decode child configuration")
	}
	request := Request{Store: cfg.Store, BundleDirectory: cfg.Directory, Pinned: cfg.Pinned, Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(cfg.Store.Engine), Mode: Boot, AllowMigrations: true, RootKey: bytes.Repeat([]byte{37}, crypto.KeySize)}
	if len(cfg.Root) > 0 {
		request.RootKey = cfg.Root
	}
	request.Target, request.Evidence = cfg.Target, cfg.Evidence
	if cfg.CiphertextPath != "" {
		request.Ciphertext, err = backupreceipt.PinCiphertext(t.Context(), cfg.CiphertextPath, filepath.Dir(path))
		if err != nil {
			t.Fatal("pin child ciphertext")
		}
		defer request.Ciphertext.Close()
		request.Operator, err = backupreceipt.PinOperator(cfg.OperatorInstance, cfg.OperatorPublicKey)
		if err != nil {
			t.Fatal("pin child operator")
		}
	}
	request.afterBoundary = func(point durableBoundary) {
		if point != cfg.Boundary {
			return
		}
		if err := os.WriteFile(cfg.Marker, []byte("ready"), 0600); err != nil {
			t.Fatal("write boundary marker")
		}
		// Only the parent terminates this process. No deferred datastore cleanup
		// can stand in for the operating system's crash recovery.
		<-t.Context().Done()
	}
	if cfg.Boundary == boundaryHealthFailed {
		request.CheckConfiguration = func(context.Context, *upgrade.CandidateConfiguration, map[string]string) error {
			return errors.New("injected candidate configuration failure")
		}
	}
	verify := func(node upgradecompat.VerifiedNode) error {
		if !node.Valid() || node.Identity() != cfg.Identity {
			return errors.New("different child build")
		}
		return nil
	}
	if _, err := run(t.Context(), request, cfg.Claim, upgrade.Production, verify); err != nil {
		t.Fatal("child gate failed before expected checkpoint")
	}
	t.Fatal("child gate returned before external kill")
}

func killGateAtBoundary(t *testing.T, request Request, claim []byte, boundary durableBoundary) {
	t.Helper()
	bundle, err := upgradebundle.Load(t.Context(), request.BundleDirectory, request.Pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	node, err := bundle.MatchBuild(claim)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "boundary")
	cfg := gateProcessConfig{Store: request.Store, Directory: request.BundleDirectory, Pinned: request.Pinned, Claim: claim, Identity: node.Identity(), Boundary: boundary, Marker: marker}
	cfg.Root, cfg.Target, cfg.Evidence = request.RootKey, request.Target, request.Evidence
	if request.Ciphertext != nil {
		cfg.CiphertextPath = filepath.Join(dir, "ciphertext.age")
		in, err := request.Ciphertext.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		out, err := os.OpenFile(cfg.CiphertextPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, copyErr := io.Copy(out, in)
		if err := errors.Join(copyErr, out.Sync(), out.Close()); err != nil {
			t.Fatal(err)
		}
		cfg.OperatorInstance = request.Operator.InstanceID()
		cfg.OperatorPublicKey = request.InitialOperatorPublicKey
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "child.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestGateCrashChild$", "-test.timeout=80s")
	command.Env = append(os.Environ(), "HIKYO_GATE_PROCESS_CONFIG="+path)
	log, err := os.OpenFile(filepath.Join(dir, "child.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() { _ = command.Process.Kill() }()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("child exited before checkpoint (private log retained): %v", err)
		case <-ctx.Done():
			_ = command.Process.Kill()
			<-done
			t.Fatal("child did not reach durable checkpoint before deadline")
		case <-tick.C:
			if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				t.Fatal(err)
			}
			if err := command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			var exit *exec.ExitError
			if err := <-done; !errors.As(err, &exit) || exit.Success() {
				t.Fatalf("expected externally killed child: %v", err)
			}
			return
		}
	}
}

func TestGateProcessCrashRestart(t *testing.T) {
	points := []struct {
		name     string
		boundary durableBoundary
		phase    upgrade.Phase
	}{
		{"prepared", boundaryPrepared, upgrade.Prepared},
		{"write-started", boundaryWriteStarted, upgrade.SchemaWriteStarted},
		{"sql-complete", boundarySQLComplete, upgrade.SchemaWriteStarted},
		{"schema-applied", boundarySchemaApplied, upgrade.SchemaApplied},
		{"healthy", boundaryHealthy, upgrade.Healthy},
		{"health-failed", boundaryHealthFailed, upgrade.RestoreRequired},
	}
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		for _, point := range points {
			t.Run(string(engine)+"/"+point.name, func(t *testing.T) {
				request, claim, verify := signedFreshGate(t, engine)
				request.Mode, request.AllowMigrations = Boot, true
				killGateAtBoundary(t, request, claim, point.boundary)
				stored, err := upgrade.InspectControl(t.Context(), request.Store)
				if err != nil || stored.Pending.Phase != point.phase {
					t.Fatalf("crash boundary phase %s: %v", point.phase, err)
				}
				if _, err := run(t.Context(), request, append(bytes.Clone(claim), ' '), upgrade.Production, verify); err == nil {
					t.Fatal("different exact build resumed crashed operation")
				}
				after, err := upgrade.InspectControl(t.Context(), request.Store)
				if err != nil || !reflect.DeepEqual(stored, after) {
					t.Fatalf("refused candidate changed durable state: %v", err)
				}
				resumed, err := run(t.Context(), request, claim, upgrade.Production, verify)
				if point.boundary == boundaryHealthFailed {
					if !errors.Is(err, ErrRestoreRequired) {
						t.Fatalf("failed health resumed without restore: %v", err)
					}
					after, readErr := upgrade.InspectControl(t.Context(), request.Store)
					if readErr != nil || !reflect.DeepEqual(stored, after) {
						t.Fatal("restore-required refusal changed state")
					}
					return
				}
				if err != nil || !resumed.Admission.Valid() || resumed.State.Pending.Phase != upgrade.Healthy || resumed.State.Maintenance || resumed.State.Generation != stored.Generation {
					t.Fatalf("exact crash resume did not become healthy at same generation: %v", err)
				}
			})
		}
	}
}

// These bridges exist only in the test binary. External package acceptance can
// call the real app export/drill without creating an app -> gate import cycle.
// No production entrypoint or mutable runtime hook is exported.
func GateStoreForProcessTest(t *testing.T, engine releaseidentity.Engine) upgrade.Config {
	return gateConfig(t, engine)
}
func RunSignedGateProcessFixture(t *testing.T, request Request, claim []byte) (Result, error) {
	t.Helper()
	bundle, err := upgradebundle.Load(t.Context(), request.BundleDirectory, request.Pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		return Result{}, err
	}
	node, err := bundle.MatchBuild(claim)
	if err != nil {
		return Result{}, err
	}
	return run(t.Context(), request, claim, upgrade.Production, func(actual upgradecompat.VerifiedNode) error {
		if !actual.Valid() || actual.Identity() != node.Identity() {
			return errors.New("fixture executable differs from signed release")
		}
		return nil
	})
}
func KillSignedGateProcessFixture(t *testing.T, request Request, claim []byte, point string) {
	t.Helper()
	boundaries := map[string]durableBoundary{"prepared": boundaryPrepared, "write-started": boundaryWriteStarted, "sql-complete": boundarySQLComplete, "schema-applied": boundarySchemaApplied, "healthy": boundaryHealthy}
	boundary, ok := boundaries[point]
	if !ok {
		t.Fatal("unknown public crash test checkpoint")
	}
	killGateAtBoundary(t, request, claim, boundary)
}
