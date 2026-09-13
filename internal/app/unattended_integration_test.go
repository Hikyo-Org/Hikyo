//go:build darwin || linux

package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

type unattendedChildFixture struct {
	Config    config.Config
	Directory string
	Root      []byte
	Pinned    releasetrust.PinnedTrust
	Target    selfupdate.PreparedNightly
	Releases  []selfupdate.PreparedNightly
}

type unattendedFixtureInstaller struct{ releases []selfupdate.PreparedNightly }

func (f unattendedFixtureInstaller) Releases(context.Context) ([]updatecheck.Release, error) {
	return nil, errors.New("fixture cannot select latest")
}
func (f unattendedFixtureInstaller) ReleaseByVersion(_ context.Context, version string) (updatecheck.Release, error) {
	for _, release := range f.releases {
		if release.Identity.Version == version {
			return updatecheck.Release{Version: version, Prerelease: true, Immutable: true, Assets: []updatecheck.Asset{{Name: "release-manifest.sigstore.json"}}}, nil
		}
	}
	return updatecheck.Release{}, errors.New("exact fixture release missing")
}
func (f unattendedFixtureInstaller) PrepareRelease(context.Context, updatecheck.Status) (selfupdate.PreparedNightly, error) {
	return selfupdate.PreparedNightly{}, errors.New("fixture cannot select target")
}
func (f unattendedFixtureInstaller) PrepareReleaseSource(_ context.Context, _ updatecheck.Status, identity releaseidentity.Identity) (selfupdate.PreparedNightly, error) {
	for _, release := range f.releases {
		if release.Identity == identity {
			return release, nil
		}
	}
	return selfupdate.PreparedNightly{}, errors.New("exact prepared fixture missing")
}
func (f unattendedFixtureInstaller) AssembleReleaseRoute(_ context.Context, target selfupdate.PreparedNightly, _ []selfupdate.PreparedNightly) (string, error) {
	return target.BundleDirectory, nil
}

// This child is linked to the exact signed release claim. Only artifact fetch
// uses a local fixture; gate, custody, backup, restore and migration are real.
func TestUnattendedCoordinatorChild(t *testing.T) {
	path := os.Getenv("HIKYO_UNATTENDED_TEST_CHILD")
	if path == "" {
		t.Skip("parent-only fixture")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture unattendedChildFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	installer := unattendedFixtureInstaller{fixture.Releases}
	if err := runPreparedUnattended(t.Context(), &fixture.Config, UnattendedUpgradeOptions{}, fixture.Directory, fixture.Root, fixture.Pinned, installer, installer, fixture.Target); err != nil {
		t.Fatal(err)
	}
}

func TestUnattendedPackagedEnrollmentUpgradeAndIntermediateCredential(t *testing.T) {
	legacy := newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
	route, claims, pinned := automaticProcessRoute(t, legacy)
	identities := []releaseidentity.Identity{route.Plan.Steps()[0].Target, route.Plan.Target()}
	// Exercise the production child adapter's anonymous descriptor using an
	// actual intermediate server after its authenticated migration.
	host, _ := automaticProcessProof(t, legacy, route)
	first := route.Executables[identities[0]]
	if _, err := host.Migrate(t.Context(), first.BinaryPath, host.evidence); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, entry := range host.environment(host.evidence) {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	cfg, _, err := config.Load("server", nil, func(key string) string { return values[key] }, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RootKeyFile = filepath.Join(host.work, "root.key")
	adapter := &unattendedProcessHost{cfg: cfg, root: legacy.root, directory: host.work}
	if err := adapter.InstallBinary(t.Context(), first.BinaryPath, string(first.BinarySHA256)); err != nil {
		t.Fatal(err)
	}
	if err := adapter.StartCandidate(t.Context(), string(first.BinarySHA256), false, 30*time.Second); err != nil {
		raw, _ := os.ReadFile(adapter.logPath)
		t.Fatalf("%v: %s", err, raw)
	}
	if err := adapter.FenceAndStop(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err := upgrade.InspectControl(t.Context(), upgrade.Config{Engine: releaseidentity.SQLite, Path: legacy.cfg.Path})
	if err != nil || state.Pending.Phase != upgrade.Healthy || !state.Maintenance {
		t.Fatalf("intermediate did not stay fenced: %v", err)
	}

	children := make([]string, len(identities))
	for index := range identities {
		children[index] = filepath.Join(t.TempDir(), "coordinator.test")
		flags := "-X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedTrustRoot=" + base64.StdEncoding.EncodeToString(pinned.Root) +
			" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedRecoveryPublicKey=" + base64.StdEncoding.EncodeToString(pinned.RecoveryPublicKey) +
			" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.encodedDeclaration=" + base64.StdEncoding.EncodeToString(claims[index]) +
			" -X github.com/Hikyo-Org/hikyo/internal/buildcompat.declarationSHA256=" + string(releaseidentity.Hash(claims[index]))
		command := exec.CommandContext(t.Context(), "go", "test", "-c", "-ldflags", flags, "-o", children[index], ".")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build signed coordinator: %v\n%s", err, output)
		}
	}
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			live := upgradeDrillDatabase(t, engine)
			stateDir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(stateDir, 0700); err != nil {
				t.Fatal(err)
			}
			directory := filepath.Join(stateDir, "unattended")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			rootPath := filepath.Join(stateDir, "root.key")
			if err := os.WriteFile(rootPath, []byte(crypto.EncodeRootKey(legacy.root)), 0600); err != nil {
				t.Fatal(err)
			}
			values := map[string]string{"HIKYO_DB": "sqlite:" + live.Path, "HIKYO_UPGRADE_STATE_DIR": stateDir, "HIKYO_UPGRADE_UNATTENDED": "true", "HIKYO_ROOT_KEY_FILE": rootPath}
			if engine == store.EnginePostgres {
				values["HIKYO_DB"] = live.DSN
				values["HIKYO_UPGRADE_SCRATCH_POSTGRES_DSN"] = upgradeDrillDatabase(t, engine).DSN
			}
			cfg, _, err := config.Load("server", nil, func(key string) string { return values[key] }, nil)
			if err != nil {
				t.Fatal(err)
			}
			fixture := unattendedChildFixture{Config: *cfg, Directory: directory, Root: legacy.root, Pinned: pinned}
			for _, identity := range identities {
				fixture.Releases = append(fixture.Releases, route.Executables[identity])
			}
			runChild := func(index int) error {
				fixture.Target = route.Executables[identities[index]]
				raw, err := json.Marshal(fixture)
				if err != nil {
					return err
				}
				path := filepath.Join(stateDir, "child.json")
				if err := os.WriteFile(path, raw, 0600); err != nil {
					return err
				}
				command := exec.CommandContext(t.Context(), children[index], "-test.run=^TestUnattendedCoordinatorChild$", "-test.timeout=90s")
				command.Env = append(os.Environ(), "HIKYO_UNATTENDED_TEST_CHILD="+path)
				output, err := command.CombinedOutput()
				if err != nil {
					return errors.New(string(output))
				}
				return nil
			}
			if err := runChild(0); err != nil {
				t.Fatal(err)
			}
			// Simulate process loss after the gate and custody binding, before
			// the final enrollment journal write. The exact image must resume.
			enrollmentPath := filepath.Join(directory, "enrollment.json")
			enrollmentRaw, err := readUnattendedPrivate(enrollmentPath)
			if err != nil {
				t.Fatal(err)
			}
			var enrollment unattendedEnrollment
			if err := json.Unmarshal(enrollmentRaw, &enrollment); err != nil {
				t.Fatal(err)
			}
			enrollment.Instance = ""
			if err := writeUnattendedEnrollment(enrollmentPath, enrollment); err != nil {
				t.Fatal(err)
			}
			if err := runChild(0); err != nil {
				t.Fatalf("interrupted enrollment restart: %v", err)
			}
			if err := runChild(0); err != nil {
				t.Fatalf("enrolled restart: %v", err)
			}
			// A signed historical candidate may still predate platform bundles.
			// The fixture installer supplies that candidate; refusal must happen
			// before creating a journal, scratch backup, or durable maintenance.
			original := route.Executables[identities[1]]
			historical := original
			historical.BinaryPath = filepath.Join(stateDir, "historical-candidate")
			script := []byte("#!/bin/sh\nprintf '" + upgradebundle.IndexFormat + "\\n'\n")
			if err := os.WriteFile(historical.BinaryPath, script, 0700); err != nil {
				t.Fatal(err)
			}
			historical.BinarySHA256 = releaseidentity.Hash(script)
			route.Executables[identities[1]] = historical
			refusal := runChild(1)
			route.Executables[identities[1]] = original
			if refusal == nil || !strings.Contains(refusal.Error(), "lacks platform-specific bundle support") {
				t.Fatalf("historical candidate did not refuse in preflight: %v", refusal)
			}
			before, err := upgrade.InspectControl(t.Context(), upgrade.Config{Engine: releaseidentity.Engine(engine), Path: live.Path, DSN: live.DSN})
			if err != nil || before.Applied.Release != identities[0] || before.Pending.Phase != upgrade.Healthy || before.Maintenance {
				t.Fatalf("preflight refusal changed healthy source: %v", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "operation.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preflight refusal created upgrade journal: %v", err)
			}
			if err := runChild(1); err != nil {
				t.Fatalf("image replacement: %v", err)
			}
			if err := runChild(1); err != nil {
				t.Fatalf("upgraded restart: %v", err)
			}
			state, err := upgrade.InspectControl(t.Context(), upgrade.Config{Engine: releaseidentity.Engine(engine), Path: live.Path, DSN: live.DSN})
			if err != nil || state.Applied.Release != identities[1] || state.Maintenance {
				t.Fatalf("replacement did not finish: %v", err)
			}
			if err := runChild(0); err == nil {
				t.Fatal("older image silently rolled back")
			}
			// A replacement state volume cannot silently adopt an existing DB.
			orphanCfg := *cfg
			local := unattendedFixtureInstaller{fixture.Releases}
			err = runPreparedUnattended(t.Context(), &orphanCfg, UnattendedUpgradeOptions{}, t.TempDir(), legacy.root, pinned, local, local, fixture.Target)
			if err == nil || !strings.Contains(err.Error(), "not enrolled") {
				t.Fatalf("existing database silently enrolled: %v", err)
			}

		})
	}
}
