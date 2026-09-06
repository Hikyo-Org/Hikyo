//go:build darwin || linux

package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// This test substitutes subprocess management for systemd only. The backup CLI,
// release/OIDC/bridge verification, restore drill, migration CLI, durable ledger,
// production server and HTTP readiness are all the real implementations.
func TestAutomaticUpgradePackagedNightlyRoute(t *testing.T) {
	source := newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
	route, claims := automaticProcessRoute(t, source)
	for _, scenario := range []string{"success", "install-interruption", "health-failure"} {
		t.Run(scenario, func(t *testing.T) {
			failInstall := scenario == "install-interruption"
			f := source
			if scenario != "success" {
				f = newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
			}
			host, journal := automaticProcessProof(t, f, route)
			host.failInstall = failInstall
			host.failHealth = scenario == "health-failure"
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = host.FenceAndStop(ctx)
			})
			database := automaticStore{upgrade.Config{Engine: releaseidentity.SQLite, Path: f.cfg.Path}}
			staged := map[releaseidentity.Identity]string{}
			for id, prepared := range route.Executables {
				staged[id] = prepared.BinaryPath
			}
			path := filepath.Join(host.work, "operation.json")
			if err := writeAutomaticJournal(path, journal); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err := applyAutomaticRoute(t.Context(), host, database, route, staged, journal, path, &output)
			if host.failHealth {
				if err == nil || !host.fenced || host.command != nil || journal.Phase != "restore-required" {
					t.Fatal("actual post-migration candidate boot failure did not require explicit recovery")
				}
				if !strings.Contains(err.Error(), crypto.ErrRootKeyMismatch.Error()) {
					t.Fatalf("candidate failed for a different reason than the injected root mismatch: %v", err)
				}
				state, inspectErr := database.Control(t.Context())
				if inspectErr != nil || state.Pending == nil || state.Pending.Phase != upgrade.RestoreRequired || !state.Maintenance {
					t.Fatal("candidate failure did not persist database restore-required", inspectErr)
				}
				if err := applyAutomaticRoute(t.Context(), host, database, route, staged, journal, path, &output); err == nil || host.migrations != 1 {
					t.Fatal("failed candidate was retried or old binary restarted automatically")
				}
				return
			}
			if failInstall {
				if err == nil || !host.fenced || host.command != nil {
					t.Fatal("post-write failure did not leave service stopped and fenced")
				}
				state, inspectErr := database.Control(t.Context())
				if inspectErr != nil || state.Pending == nil || state.Pending.Phase != upgrade.SchemaApplied || !state.Maintenance {
					t.Fatalf("post-write failure lost durable schema-applied admission: %v", inspectErr)
				}
				if host.migrations != 1 {
					t.Fatal("failure occurred outside first actual migration")
				}
				journal, err = readAutomaticJournal(path)
				if err != nil {
					t.Fatal(err)
				}
				host.failInstall = false
				err = applyAutomaticRoute(t.Context(), host, database, route, staged, journal, path, &output)
			}
			if err != nil {
				t.Fatalf("automatic packaged route: %v\n%s", err, output.String())
			}
			if journal.Phase != "complete" || host.fenced || host.command == nil || host.migrations != 2 || host.intermediateHealth != 1 || host.finalHealth != 1 {
				t.Fatalf("incomplete real route: phase=%s fenced=%t migrations=%d intermediate=%d final=%d", journal.Phase, host.fenced, host.migrations, host.intermediateHealth, host.finalHealth)
			}
			if host.evidence.CiphertextPath != "" || host.evidence.EvidenceDirectory != "" || host.evidence.LegacyWritersStopped {
				t.Fatal("completed runtime retained one-use upgrade evidence")
			}
			if err := host.FenceAndStop(t.Context()); err != nil {
				t.Fatal(err)
			}
			var admission upgrade.Admission
			node, err := route.Bundle.MatchBuild(claims[1])
			if err != nil {
				t.Fatal(err)
			}
			err = upgrade.WithLock(t.Context(), database.config, func(session *upgrade.Session) error {
				state, err := session.Read(t.Context())
				if err != nil {
					return err
				}
				admission, err = session.Admit(t.Context(), state, node)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			db, err := store.Open(t.Context(), f.cfg, admission)
			if err != nil {
				t.Fatal(err)
			}
			kr, err := crypto.LoadKeyring(t.Context(), &keyring.Store{DB: db}, bytes.Clone(f.root))
			if err != nil {
				db.Close()
				t.Fatal(err)
			}
			readable, err := (&service.Backup{DB: db}).ProveValuesReadable(t.Context(), kr)
			if err != nil || !readable {
				db.Close()
				t.Fatal("automatic route lost actual protected value", err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			// A healthy restart uses only public long-lived trust and the root
			// credential. Consumed backup evidence is no longer supplied.
			if err := host.StartCandidate(t.Context(), string(route.Executables[route.Plan.Target()].BinarySHA256), true, 45*time.Second); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func automaticProcessRoute(t *testing.T, source upgradeDrillFixture) (automaticRoute, [][]byte) {
	t.Helper()
	work := t.TempDir()
	directory := filepath.Join(work, "bundle")
	_, declaration, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	declaration.Profile, declaration.Version, declaration.Sequence, declaration.Commit = releaseidentity.NightlyV1, "1.1.0-nightly.1", 2, strings.Repeat("a", 40)
	first := trustfixture.JSON(t, declaration)
	f, material, _ := trustfixture.Nightly(t, first, false)
	writeRelease := func(material releasetrust.NightlyMaterial) releasetrust.VerifiedRelease {
		t.Helper()
		payloads := t.TempDir()
		for name, reader := range material.Artifacts {
			raw, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			automaticProcessPut(t, filepath.Join(payloads, name), raw)
		}
		automaticProcessPut(t, filepath.Join(payloads, "release-manifest.json"), material.Manifest)
		automaticProcessPut(t, filepath.Join(payloads, "release-manifest.sigstore.json"), material.Bundle)
		release, err := upgradebundle.CopyNightlyRelease(t.Context(), payloads, filepath.Join(directory, "releases"), f.Snapshot(t))
		if err != nil {
			t.Fatal(err)
		}
		return release
	}
	previous := writeRelease(material)
	var sqlite upgradecompat.EngineDeclaration
	for _, engine := range declaration.Engines {
		if engine.Migrations.Engine == releaseidentity.SQLite {
			sqlite = engine
		}
	}
	legacyManifest, err := upgrade.PinnedLegacyManifest(releaseidentity.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	bridge := f.AddBridge(t, releasetrust.BridgeStatement{Schema: "hikyo.dev/legacy-nightly-bridge/v1", SourceGenesis: releaseidentity.LegacyGenesisV1, Target: previous.Identity(), TargetPolicySHA256: previous.PolicyDigest(), SourceMigrations: legacyManifest, TargetMigrations: sqlite.Migrations, SourceSchemaSHA256: source.source.SchemaDigest, TargetSchemaSHA256: sqlite.SchemaSHA256, Mode: "maintenance"})
	for i, engine := range declaration.Engines {
		declaration.Engines[i].Sources = append(engine.Sources, upgradecompat.SourceEdge{Source: releaseidentity.Source{Release: previous.Identity()}, Migrations: engine.Migrations, SchemaSHA256: engine.SchemaSHA256, Mode: upgradecompat.Maintenance})
	}
	declaration.Version, declaration.Sequence = "1.1.0-nightly.2", 3
	second := trustfixture.JSON(t, declaration)
	target := writeRelease(f.SignNightly(second, declaration.Version, declaration.Sequence))
	bridgeDigest := releaseidentity.Hash(bridge.Statement)
	snapshot := f.Material(t)
	for name, raw := range map[string][]byte{"metadata.json": snapshot.Metadata, "metadata.sigstore.json": snapshot.MetadataSignature, "catalog.json": snapshot.Catalog, "catalog.sigstore.json": snapshot.CatalogSignature, "keys/test-primary.pub": f.PrimaryPublic,
		"bridges/" + string(bridgeDigest) + "/statement.json": bridge.Statement, "bridges/" + string(bridgeDigest) + "/statement.sigstore.json": bridge.Signature} {
		automaticProcessPut(t, filepath.Join(directory, name), raw)
	}
	index := upgradebundle.Index{Format: upgradebundle.IndexFormat, PrimaryKeyIDs: []string{"test-primary"}, Releases: []upgradebundle.ReleaseEntry{{Profile: releaseidentity.NightlyV1, ManifestSHA256: previous.Identity().ManifestSHA256}, {Profile: releaseidentity.NightlyV1, ManifestSHA256: target.Identity().ManifestSHA256}}, Bridges: []releaseidentity.Digest{bridgeDigest}}
	automaticProcessPut(t, filepath.Join(directory, "index.json"), trustfixture.JSON(t, index))
	bundle, err := upgradebundle.Load(t.Context(), directory, f.Pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := bundle.Plan(upgradecompat.InstalledSource{Identity: source.source.Source, Migrations: legacyManifest, SchemaSHA256: source.source.SchemaDigest}, target.Identity())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps()) != 2 || !plan.RequiresOperatorAttestation() {
		t.Fatal("fixture must require signed legacy bridge and two-hop route")
	}
	route := automaticRoute{Bundle: bundle, Directory: directory, Plan: plan, Instance: source.source.InstanceID, Executables: map[releaseidentity.Identity]selfupdate.PreparedNightly{}}
	claims := [][]byte{first, second}
	for i, identity := range []releaseidentity.Identity{previous.Identity(), target.Identity()} {
		binary := filepath.Join(work, fmt.Sprintf("hikyo-%d", i))
		flags := "-X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedTrustRoot=" + base64.StdEncoding.EncodeToString(f.Pinned.Root) +
			" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedRecoveryPublicKey=" + base64.StdEncoding.EncodeToString(f.Pinned.RecoveryPublicKey) +
			" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedDeclaration=" + base64.StdEncoding.EncodeToString(claims[i]) +
			" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.declarationSHA256=" + string(releaseidentity.Hash(claims[i]))
		command := exec.CommandContext(t.Context(), "go", "build", "-ldflags", flags, "-o", binary, "../../cmd/hikyo")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build production fixture: %v\n%s", err, output)
		}
		raw := automaticProcessRead(t, binary)
		route.Executables[identity] = selfupdate.PreparedNightly{Identity: identity, BinaryPath: binary, BinarySHA256: releaseidentity.Hash(raw), BundleDirectory: directory}
	}
	return route, claims
}

func automaticProcessProof(t *testing.T, f upgradeDrillFixture, route automaticRoute) (*automaticProcessHost, *automaticJournal) {
	t.Helper()
	work := t.TempDir()
	installation := filepath.Join(work, "installation")
	if err := os.Mkdir(installation, 0700); err != nil {
		t.Fatal(err)
	}
	operator := trustfixture.New(t)
	publicPath := filepath.Join(work, "operator.pub")
	automaticProcessPut(t, publicPath, operator.PrimaryPublic)
	automaticProcessPut(t, filepath.Join(work, "root.key"), []byte(crypto.EncodeRootKey(f.root)))
	host := &automaticProcessHost{work: work, installed: filepath.Join(work, "hikyo"), fenced: true, baseEnv: []string{"PATH=" + os.Getenv("PATH"), "HOME=" + work, "HIKYO_DB=sqlite:" + f.cfg.Path, "HIKYO_EXTERNAL_ORIGIN=https://automatic.fixture.invalid", "HIKYO_UPDATE_CHANNEL=off", "HIKYO_UPGRADE_STATE_DIR=" + installation}}
	host.evidence = hostupgrade.RuntimeEvidence{BundleDirectory: route.Directory, OperatorPublicKey: publicPath, TargetManifest: string(route.Plan.Target().ManifestSHA256), LegacyWritersStopped: true}
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(work, "backup")
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), route.Executables[route.Plan.Target()].BinaryPath, "backup", "upgrade-export", "--json", "--out", output, "--recipient", recipient)
	command.Env = host.environment(host.evidence)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("real JSON backup-export: %v\n%s", err, stderr.String())
	}
	var exported struct {
		Ciphertext string `json:"ciphertext"`
		Receipt    string `json:"receipt"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &exported); err != nil {
		t.Fatalf("backup output must be JSON: %v\n%s", err, stdout.String())
	}
	receipt := automaticProcessRead(t, exported.Receipt)
	pinned, err := backupreceipt.PinCiphertext(t.Context(), exported.Ciphertext, work)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	pin, err := backupreceipt.PinOperator(f.source.InstanceID, operator.PrimaryPublic)
	if err != nil {
		t.Fatal(err)
	}
	drill, err := DrillUpgrade(t.Context(), UpgradeDrillRequest{Scratch: store.Config{Engine: store.EngineSQLite, Path: filepath.Join(work, "scratch.db")}, Ciphertext: pinned, Receipt: receipt, Plan: route.Plan, Operator: pin, Unlock: backup.Unlock{Identity: identity}, RootKey: bytes.Clone(f.root), AutoCredentialProof: true, Now: time.Now(), Lifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if !drill.HierarchyReadable || drill.SecretProof != "existing-secret-readable" || drill.CredentialProof != "reconciled-minted-revoked" {
		t.Fatal("real recovery proof incomplete")
	}
	evidence := filepath.Join(work, "evidence")
	for name, raw := range map[string][]byte{"receipt.json": receipt, "attestation.json": drill.Attestation, "attestation.sigstore.json": trustfixture.Sign(t, operator.PrimarySigner, drill.Attestation)} {
		automaticProcessPut(t, filepath.Join(evidence, name), raw)
	}
	host.evidence.EvidenceDirectory, host.evidence.CiphertextPath = evidence, exported.Ciphertext
	manifest, err := route.Plan.SourceManifest(releaseidentity.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	return host, &automaticJournal{Format: "hikyo.host-upgrade/v1", Phase: "proved", Target: route.Plan.Target(), Source: upgradecompat.InstalledSource{Identity: route.Plan.Source(), Migrations: manifest, SchemaSHA256: route.Plan.SourceSchemaDigest()}, Instance: f.source.InstanceID, Route: route.Plan.Digest(), Runtime: host.evidence}
}

type automaticProcessHost struct {
	work, installed                             string
	baseEnv                                     []string
	evidence                                    hostupgrade.RuntimeEvidence
	command                                     *exec.Cmd
	done                                        chan error
	fenced, failInstall, failHealth             bool
	migrations, intermediateHealth, finalHealth int
}

func (h *automaticProcessHost) environment(e hostupgrade.RuntimeEvidence) []string {
	return append(append([]string{}, h.baseEnv...), "HIKYO_UPGRADE_BUNDLE="+e.BundleDirectory, "HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY="+e.OperatorPublicKey, "HIKYO_UPGRADE_TARGET_MANIFEST="+e.TargetManifest, "HIKYO_UPGRADE_EVIDENCE="+e.EvidenceDirectory, "HIKYO_UPGRADE_BACKUP="+e.CiphertextPath, fmt.Sprintf("HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED=%t", e.LegacyWritersStopped))
}
func (h *automaticProcessHost) FenceAndStop(ctx context.Context) error {
	h.fenced = true
	if h.command == nil {
		return nil
	}
	_ = h.command.Process.Signal(os.Interrupt)
	select {
	case err := <-h.done:
		h.command = nil
		return err
	case <-ctx.Done():
		_ = h.command.Process.Kill()
		<-h.done
		h.command = nil
		return ctx.Err()
	}
}
func (h *automaticProcessHost) Migrate(ctx context.Context, candidate string, e hostupgrade.RuntimeEvidence) ([]byte, error) {
	if !h.fenced || h.command != nil {
		return nil, errors.New("migration attempted without full stop")
	}
	h.migrations++
	command := exec.CommandContext(ctx, candidate, "migrate")
	command.Env = h.environment(e)
	raw, err := command.CombinedOutput()
	if err != nil {
		return raw, fmt.Errorf("real migrate: %w: %s", err, raw)
	}
	return raw, nil
}
func (h *automaticProcessHost) InstallBinary(_ context.Context, candidate, digest string) error {
	if h.failInstall {
		return errors.New("injected post-schema installation interruption")
	}
	raw, err := os.ReadFile(candidate)
	if err != nil {
		return err
	}
	if string(releaseidentity.Hash(raw)) != digest {
		return errors.New("candidate executable digest mismatch")
	}
	return os.WriteFile(h.installed, raw, 0700)
}
func (h *automaticProcessHost) ConfigureRuntime(_ context.Context, e hostupgrade.RuntimeEvidence) error {
	h.evidence = e
	return nil
}
func (h *automaticProcessHost) Complete(context.Context) error { h.fenced = false; return nil }
func (h *automaticProcessHost) StartCandidate(ctx context.Context, digest string, final bool, timeout time.Duration) error {
	if !h.fenced || h.command != nil {
		return errors.New("candidate requires a stopped fenced service")
	}
	raw, err := os.ReadFile(h.installed)
	if err != nil || string(releaseidentity.Hash(raw)) != digest {
		return errors.New("installed process digest differs from candidate")
	}
	if h.failHealth {
		// Corrupt only the runtime root credential after actual migrations.
		// Real startup must reject the unreadable hierarchy before serving.
		if err := os.WriteFile(filepath.Join(h.work, "root.key"), []byte(crypto.EncodeRootKey(bytes.Repeat([]byte{11}, crypto.KeySize))), 0600); err != nil {
			return err
		}
	}
	log, err := os.CreateTemp(h.work, "server-*.log")
	if err != nil {
		return err
	}
	defer log.Close()
	command := exec.CommandContext(ctx, h.installed, "server", "--auto-migrate=false", "--root-key-file="+filepath.Join(h.work, "root.key"), "--listen=localhost:0", "--operational-listen=127.0.0.1:0")
	command.Env, command.Stdout, command.Stderr = h.environment(h.evidence), log, log
	if err := command.Start(); err != nil {
		return err
	}
	h.command, h.done = command, make(chan error, 1)
	go func() { h.done <- command.Wait() }()
	check, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	for {
		select {
		case err := <-h.done:
			h.command = nil
			logs, _ := os.ReadFile(log.Name())
			return fmt.Errorf("real production server exited: %v: %s", err, logs)
		case <-check.Done():
			logs, _ := os.ReadFile(log.Name())
			return fmt.Errorf("real production readiness timed out: %w: %s", check.Err(), logs)
		case <-ticker.C:
			logs, err := os.ReadFile(log.Name())
			if err != nil {
				return err
			}
			for _, line := range bytes.Split(logs, []byte("\n")) {
				var event struct {
					Message     string `json:"msg"`
					Operational string `json:"operational_addr"`
				}
				if json.Unmarshal(line, &event) != nil || (event.Message != "server ready" && !strings.HasPrefix(event.Message, "maintenance active;")) {
					continue
				}
				wantReady := http.StatusServiceUnavailable
				if final {
					wantReady = http.StatusOK
				}
				all := true
				for path, expected := range map[string]int{"/healthz": http.StatusOK, "/readyz": wantReady} {
					request, err := http.NewRequestWithContext(check, http.MethodGet, "http://"+event.Operational+path, nil)
					if err != nil {
						return err
					}
					response, err := client.Do(request)
					if err != nil {
						all = false
						break
					}
					response.Body.Close()
					if response.StatusCode != expected {
						all = false
						break
					}
				}
				if all {
					if final {
						h.finalHealth++
					} else {
						h.intermediateHealth++
					}
					return nil
				}
			}
		}
	}
}

func automaticProcessPut(t testing.TB, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func automaticProcessRead(t testing.TB, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
