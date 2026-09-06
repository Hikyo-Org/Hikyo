//go:build darwin || linux

package upgradegate_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// CI supplies the previously published archive's executable and the candidate
// executable. Local tests build two distinct signed release identities using
// ephemeral test trust. Neither path uses development admission or --dev.
func TestPackagedNightlyReleaseUpgrade(t *testing.T) {
	bundleDir, pinned, binaries, claims := nightlyProcessInputs(t)
	bundle, err := upgradebundle.Load(t.Context(), bundleDir, pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	nodes := make([]upgradecompat.VerifiedNode, 2)
	for i := range nodes {
		nodes[i], err = bundle.MatchBuild(claims[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	if nodes[0].Identity() == nodes[1].Identity() {
		t.Fatal("upgrade fixture requires different releases")
	}
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := upgradegate.GateStoreForProcessTest(t, engine)
			work := t.TempDir()
			if err := os.Mkdir(filepath.Join(work, "installation"), 0700); err != nil {
				t.Fatal(err)
			}
			root := bytes.Repeat([]byte{37}, crypto.KeySize)
			operator := trustfixture.New(t) // disposable installation operator, never release authority
			putNightly(t, filepath.Join(work, "root.key"), []byte(fmt.Sprintf("%x\n", root)))
			putNightly(t, filepath.Join(work, "operator.pub"), operator.PrimaryPublic)
			dsn := "sqlite:" + cfg.Path
			if engine == releaseidentity.Postgres {
				dsn = cfg.DSN
			}
			environment := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + work, "HIKYO_DB=" + dsn,
				"HIKYO_EXTERNAL_ORIGIN=https://nightly.fixture.invalid", "HIKYO_UPDATE_CHANNEL=off",
				"HIKYO_UPGRADE_BUNDLE=" + bundleDir, "HIKYO_UPGRADE_STATE_DIR=" + filepath.Join(work, "installation"),
				"HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY=" + filepath.Join(work, "operator.pub")}
			bootPackagedNightly(t, binaries[0], work, environment)
			open := func(node upgradecompat.VerifiedNode) (*store.DB, upgrade.State) {
				t.Helper()
				var admission upgrade.Admission
				var state upgrade.State
				err := upgrade.WithLock(t.Context(), cfg, func(session *upgrade.Session) error {
					var err error
					state, err = session.Read(t.Context())
					if err != nil {
						return err
					}
					admission, err = session.Admit(t.Context(), state, node)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				db, err := store.Open(t.Context(), processStoreConfig(cfg), admission)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Close() })
				return db, state
			}
			db, sourceState := open(nodes[0])
			kr, err := crypto.LoadKeyring(t.Context(), &keyring.Store{DB: db}, bytes.Clone(root))
			if err != nil {
				t.Fatal(err)
			}
			populateProcessSource(t, db, kr)
			manifest, err := nodes[0].Manifest(engine)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := bundle.Plan(upgradecompat.InstalledSource{Identity: sourceState.Applied, Migrations: manifest, SchemaSHA256: sourceState.SchemaDigest}, nodes[1].Identity())
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Steps()) != 1 || plan.RequiresOperatorAttestation() {
				t.Fatal("expected ordinary authenticated nightly maintenance edge")
			}
			identity, recipient, err := backup.GenerateIdentity()
			if err != nil {
				t.Fatal(err)
			}
			exported, err := (&service.Backup{DB: db, Options: backup.Options{Recipients: []string{recipient}}}).ExportUpgrade(t.Context(), t.TempDir(), plan, nil)
			if err != nil {
				t.Fatal(err)
			}
			receipt := readNightly(t, exported.ReceiptPath)
			ciphertext, err := backupreceipt.PinCiphertext(t.Context(), exported.Path, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ciphertext.Close()
			pin, err := backupreceipt.PinOperator(sourceState.InstanceID, operator.PrimaryPublic)
			if err != nil {
				t.Fatal(err)
			}
			drill, err := app.DrillUpgrade(t.Context(), app.UpgradeDrillRequest{Scratch: processStoreConfig(upgradegate.GateStoreForProcessTest(t, engine)), Ciphertext: ciphertext, Receipt: receipt, Plan: plan, Operator: pin, Unlock: backup.Unlock{Identity: identity}, RootKey: bytes.Clone(root), Principal: domain.PrincipalID("usr_process"), Scope: domain.Scope{Org: "org_process", Project: "prj_process"}, Now: time.Now().UTC(), Lifetime: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			if !drill.HierarchyReadable || drill.SecretProof != "existing-secret-readable" || drill.CredentialProof != "reconciled-minted-revoked" {
				t.Fatal("missing actual readable restore and credential proof")
			}
			evidence := filepath.Join(work, "evidence")
			putNightly(t, filepath.Join(evidence, "receipt.json"), receipt)
			putNightly(t, filepath.Join(evidence, "attestation.json"), drill.Attestation)
			putNightly(t, filepath.Join(evidence, "attestation.sigstore.json"), trustfixture.Sign(t, operator.PrimarySigner, drill.Attestation))
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			bootPackagedNightly(t, binaries[1], work, append(environment, "HIKYO_UPGRADE_EVIDENCE="+evidence, "HIKYO_UPGRADE_BACKUP="+exported.Path))
			final, targetState := open(nodes[1])
			if targetState.Generation != sourceState.Generation+1 || targetState.Applied.Release != nodes[1].Identity() || targetState.Maintenance {
				t.Fatal("target did not complete the exact upgrade")
			}
			keys, err := crypto.LoadKeyring(t.Context(), &keyring.Store{DB: final}, bytes.Clone(root))
			if err != nil {
				t.Fatal(err)
			}
			readable, err := (&service.Backup{DB: final}).ProveValuesReadable(t.Context(), keys)
			if err != nil || !readable {
				t.Fatal("upgrade lost protected value", err)
			}
			if err := final.Close(); err != nil {
				t.Fatal(err)
			}
			bootPackagedNightly(t, binaries[1], work, environment) // consumed evidence is unnecessary on healthy restart
			t.Logf("%s -> %s: populated %s upgrade, readable secret and production restart passed", nodes[0].Identity().Version, nodes[1].Identity().Version, engine)
		})
	}
}

func bootPackagedNightly(t *testing.T, binary, work string, environment []string) {
	t.Helper()
	log, err := os.CreateTemp(work, "boot-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command := exec.CommandContext(t.Context(), binary, "server", "--root-key-file="+filepath.Join(work, "root.key"), "--listen=localhost:0", "--operational-listen=127.0.0.1:0")
	command.Env, command.Stdout, command.Stderr = environment, log, log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer command.Process.Kill()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	ready := false
	deadline := time.After(60 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
waiting:
	for {
		select {
		case err := <-done:
			t.Fatalf("production binary exited: %v\n%s", err, readNightly(t, log.Name()))
		case <-deadline:
			break waiting
		case <-ticker.C:
			for _, line := range bytes.Split(readNightly(t, log.Name()), []byte("\n")) {
				var event struct {
					Message     string `json:"msg"`
					Operational string `json:"operational_addr"`
				}
				if json.Unmarshal(line, &event) != nil || event.Message != "server ready" {
					continue
				}
				response, err := client.Get("http://" + event.Operational + "/readyz")
				if err != nil {
					continue
				}
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					ready = true
					break waiting
				}
			}
		}
	}
	if !ready {
		t.Fatalf("production readiness timed out\n%s", readNightly(t, log.Name()))
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("production shutdown: %v\n%s", err, readNightly(t, log.Name()))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("production shutdown timed out")
	}
}

func putNightly(t testing.TB, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func readNightly(t testing.TB, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func nightlyProcessInputs(t *testing.T) (string, releasetrust.PinnedTrust, []string, [][]byte) {
	t.Helper()
	if previous := os.Getenv("HIKYO_NIGHTLY_PREVIOUS_BINARY"); previous != "" {
		trust := os.Getenv("HIKYO_NIGHTLY_TRUST")
		rootRaw := readNightly(t, filepath.Join(trust, "root.json"))
		var root releasetrust.Root
		if err := json.Unmarshal(rootRaw, &root); err != nil {
			t.Fatal(err)
		}
		pinned := releasetrust.PinnedTrust{Root: rootRaw, RecoveryPublicKey: readNightly(t, filepath.Join(trust, root.Recovery.PublicKey))}
		return os.Getenv("HIKYO_NIGHTLY_BUNDLE"), pinned,
			[]string{previous, os.Getenv("HIKYO_NIGHTLY_TARGET_BINARY")},
			[][]byte{readNightly(t, os.Getenv("HIKYO_NIGHTLY_PREVIOUS_COMPATIBILITY")), readNightly(t, os.Getenv("HIKYO_NIGHTLY_TARGET_COMPATIBILITY"))}
	}
	work := t.TempDir()
	_, declaration, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	declaration.Profile, declaration.Version, declaration.Sequence, declaration.Commit = releaseidentity.NightlyV1, "1.1.0-nightly.1", 2, strings.Repeat("a", 40)
	first := trustfixture.JSON(t, declaration)
	f, material, _ := trustfixture.Nightly(t, first, false)
	snapshot := f.Snapshot(t)
	write := func(material releasetrust.NightlyMaterial) releasetrust.VerifiedRelease {
		t.Helper()
		directory := t.TempDir()
		for name, reader := range material.Artifacts {
			raw, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			putNightly(t, filepath.Join(directory, name), raw)
		}
		putNightly(t, filepath.Join(directory, "release-manifest.json"), material.Manifest)
		putNightly(t, filepath.Join(directory, "release-manifest.sigstore.json"), material.Bundle)
		release, err := upgradebundle.CopyNightlyRelease(t.Context(), directory, filepath.Join(work, "bundle", "releases"), snapshot)
		if err != nil {
			t.Fatal(err)
		}
		return release
	}
	source := write(material)
	for i, engine := range declaration.Engines {
		declaration.Engines[i].Sources = append(engine.Sources, upgradecompat.SourceEdge{Source: releaseidentity.Source{Release: source.Identity()}, Migrations: engine.Migrations, SchemaSHA256: engine.SchemaSHA256, Mode: upgradecompat.Maintenance})
	}
	declaration.Version, declaration.Sequence = "1.1.0-nightly.2", 3
	second := trustfixture.JSON(t, declaration)
	target := write(f.SignNightly(second, declaration.Version, declaration.Sequence))
	directory := filepath.Join(work, "bundle")
	snap := f.Material(t)
	for name, raw := range map[string][]byte{"metadata.json": snap.Metadata, "metadata.sigstore.json": snap.MetadataSignature, "catalog.json": snap.Catalog, "catalog.sigstore.json": snap.CatalogSignature, "keys/test-primary.pub": f.PrimaryPublic} {
		putNightly(t, filepath.Join(directory, name), raw)
	}
	index := upgradebundle.Index{Format: upgradebundle.IndexFormat, PrimaryKeyIDs: []string{"test-primary"}, Releases: []upgradebundle.ReleaseEntry{{Profile: releaseidentity.NightlyV1, ManifestSHA256: source.Identity().ManifestSHA256}, {Profile: releaseidentity.NightlyV1, ManifestSHA256: target.Identity().ManifestSHA256}}, Bridges: []releaseidentity.Digest{}}
	putNightly(t, filepath.Join(directory, "index.json"), trustfixture.JSON(t, index))
	binaries, claims := []string{}, [][]byte{first, second}
	for i, claim := range claims {
		binary := filepath.Join(work, fmt.Sprintf("hikyo-%d", i))
		flags := "-X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedTrustRoot=" + base64.StdEncoding.EncodeToString(f.Pinned.Root) +
			" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedRecoveryPublicKey=" + base64.StdEncoding.EncodeToString(f.Pinned.RecoveryPublicKey) +
			" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedDeclaration=" + base64.StdEncoding.EncodeToString(claim) +
			" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.declarationSHA256=" + string(releaseidentity.Hash(claim))
		command := exec.CommandContext(t.Context(), "go", "build", "-ldflags", flags, "-o", binary, "../../cmd/hikyo")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build release fixture: %v\n%s", err, output)
		}
		binaries = append(binaries, binary)
	}
	return directory, f.Pinned, binaries, claims
}
