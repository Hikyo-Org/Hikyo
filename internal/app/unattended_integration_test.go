//go:build darwin || linux

package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
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
	// Inspect names a file that receives this image's own security observation.
	Inspect string
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
	if err := json.Unmarshal(raw, &fixture, durationNanos); err != nil {
		t.Fatal(err)
	}
	if fixture.Inspect != "" {
		observed, err := inspectSecurityImage(t.Context(), &fixture.Config)
		if err != nil {
			observed = securitymaintenance.Observation{}
		}
		if err := os.WriteFile(fixture.Inspect, mustJSON(t, observed), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	installer := unattendedFixtureInstaller{fixture.Releases}
	if err := runPreparedUnattended(t.Context(), &fixture.Config, UnattendedUpgradeOptions{}, fixture.Directory, fixture.Root, fixture.Pinned, installer, installer, fixture.Target); err != nil {
		t.Fatal(err)
	}
}

// durationNanos carries config.Config's time.Duration fields between the test
// process and its child as nanosecond numbers, as encoding/json v1 did; v2 has
// no default representation for a Duration.
var durationNanos = json.JoinOptions(
	json.WithMarshalers(json.MarshalToFunc(func(enc *jsontext.Encoder, d time.Duration) error {
		return enc.WriteToken(jsontext.Int(int64(d)))
	})),
	json.WithUnmarshalers(json.UnmarshalFromFunc(func(dec *jsontext.Decoder, d *time.Duration) error {
		var nanos int64
		if err := json.UnmarshalDecode(dec, &nanos); err != nil {
			return err
		}
		*d = time.Duration(nanos)
		return nil
	})),
)

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value, jsonwire.Stable)
	if err != nil {
		t.Fatal(err)
	}
	return raw
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
				raw, err := json.Marshal(fixture, durationNanos)
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
			// A hybrid MANAGED backup policy with the X25519 custody identity must
			// refuse before the journal fence, the scratch and any export. The
			// config comes from LoadBootstrap, which moves owner settings into
			// ManagedInputs and leaves BackupRecipients empty, as in production.
			// The same child, started with nothing pending, must still boot.
			_, hybridRecipient, genErr := backup.GenerateHybridIdentity()
			if genErr != nil {
				t.Fatal(genErr)
			}
			hybridValues := map[string]string{"HIKYO_BACKUP_RECIPIENTS": hybridRecipient, "HIKYO_BACKUP_DIR": t.TempDir()}
			for key, value := range values {
				hybridValues[key] = value
			}
			hybridConfig, _, loadErr := config.LoadBootstrap("server", nil, func(key string) string { return hybridValues[key] }, nil)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if len(hybridConfig.BackupRecipients) != 0 || hybridConfig.ManagedInputs["HIKYO_BACKUP_RECIPIENTS"] != hybridRecipient {
				t.Fatal("fixture is not the production LoadBootstrap shape")
			}
			plain := fixture.Config
			fixture.Config = *hybridConfig
			policyRefusal := runChild(1)
			fixture.Config = plain
			if policyRefusal == nil || !strings.Contains(policyRefusal.Error(), "refusing before any backup") {
				t.Fatalf("hybrid policy with X25519 custody did not refuse the upgrade: %v", policyRefusal)
			}
			// The refusal precedes every write: no journal pins the target image,
			// so nothing is left behind.
			if _, err := os.Stat(filepath.Join(directory, "operation.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("policy refusal left an upgrade journal: %v", err)
			}
			if exports, _ := filepath.Glob(filepath.Join(directory, "backup-*")); len(exports) != 0 {
				t.Fatalf("policy refusal exported a backup: %v", exports)
			}
			if after, err := upgrade.InspectControl(t.Context(), upgrade.Config{Engine: releaseidentity.Engine(engine), Path: live.Path, DSN: live.DSN}); err != nil || after.Applied.Release != identities[0] || after.Pending.Phase != upgrade.Healthy || after.Maintenance {
				t.Fatalf("policy refusal changed the healthy source: %v", err)
			}
			// A refused host still starts on its CURRENT image, under the hybrid
			// policy and under the plain one, because nothing was written.
			fixture.Config = *hybridConfig
			currentHybrid := runChild(0)
			fixture.Config = plain
			if currentHybrid != nil {
				t.Fatalf("refused host did not start on its current image under the hybrid policy: %v", currentHybrid)
			}
			if err := runChild(0); err != nil {
				t.Fatalf("refused host did not start on its current image: %v", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "operation.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("starting the current image created an upgrade journal: %v", err)
			}
			// Reverting the policy (the plain config) lets the upgrade proceed.
			if engine == store.EngineSQLite {
				unattendedTargetFailsBeforeWritingAndSourceResumes(t, live, directory, identities, children, fixture, runChild)
			}
			if err := runChild(1); err != nil {
				t.Fatalf("image replacement: %v", err)
			}
			if err := runChild(1); err != nil {
				t.Fatalf("upgraded restart: %v", err)
			}
			// With nothing pending, a hybrid policy must not stop the container
			// from starting: the refusal applies only where an export would run.
			fixture.Config = *hybridConfig
			hybridRestart := runChild(1)
			fixture.Config = plain
			if hybridRestart != nil {
				t.Fatalf("hybrid policy blocked a start with no upgrade pending: %v", hybridRestart)
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

// A secret that the scratch restore cannot open fails the target's proof after
// the source was frozen and before anything else was written.
func unattendedTargetFailsBeforeWritingAndSourceResumes(t *testing.T, live store.Config, directory string, identities []releaseidentity.Identity, children []string, fixture unattendedChildFixture, runChild func(int) error) {
	t.Helper()
	database := upgrade.Config{Engine: releaseidentity.SQLite, Path: live.Path}
	journalPath := filepath.Join(directory, "operation.json")
	control := func() upgrade.State {
		state, err := upgrade.InspectControl(t.Context(), database)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	inspect := func(index int) securitymaintenance.Observation {
		fixture.Target, fixture.Inspect = fixture.Releases[index], filepath.Join(t.TempDir(), "inspect.json")
		raw, err := json.Marshal(fixture, durationNanos)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "child.json")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), children[index], "-test.run=^TestUnattendedCoordinatorChild$", "-test.timeout=90s")
		command.Env = append(os.Environ(), "HIKYO_UNATTENDED_TEST_CHILD="+path)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("inspect child: %s", output)
		}
		var observed securitymaintenance.Observation
		raw, err = os.ReadFile(fixture.Inspect)
		if err != nil || json.Unmarshal(raw, &observed) != nil {
			t.Fatal("unreadable inspection", err)
		}
		return observed
	}
	sqlDB, err := sql.Open("sqlite", store.SQLiteDSN(live.Path))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	mutate := func(statements ...string) {
		t.Helper()
		for _, statement := range statements {
			if _, err := sqlDB.ExecContext(t.Context(), statement); err != nil {
				t.Fatalf("%s: %v", statement, err)
			}
		}
	}
	mutate(`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_drill','Drill',TRUE,'{}','2026-09-05T00:00:00Z')`,
		`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_drill','org_drill','Drill','2026-09-05T00:00:00Z')`,
		`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_drill','org_drill','prj_drill','drill','','2026-09-05T00:00:00Z',0)`,
		`INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES ('key_drill','org_drill','prj_drill','DRILL_SECRET','','secret','',FALSE,'','{}','none','none','2026-09-05T00:00:00Z')`,
		`INSERT INTO principals (id,kind,created_at) VALUES ('usr_drill','human','2026-09-05T00:00:00Z')`,
		`INSERT INTO value_entries (id,org_id,project_id,environment_id,key_id,ciphertext,updated_at,updated_by) VALUES ('val_drill','org_drill','prj_drill','env_drill','key_drill',x'0102030405','2026-09-05T00:00:00Z','usr_drill')`)
	before := control()

	if err := runChild(1); err == nil {
		t.Fatal("a stored secret the restore cannot open passed the proof")
	}
	frozen := control()
	if frozen.Pending.Phase != upgrade.BackupPreparing || !frozen.Maintenance || frozen.Generation != before.Generation || frozen.Applied != before.Applied || frozen.SchemaDigest != before.SchemaDigest {
		t.Fatalf("the failed proof left something other than the fenced source: %+v", frozen.Pending)
	}
	journal, err := readAutomaticJournal(journalPath)
	if err != nil || journal == nil || journal.Phase != "preparing" || journal.Aborted == "" {
		t.Fatalf("the target did not record that it stopped before writing: %+v %v", journal, err)
	}
	if aborted := inspect(1); !aborted.AbortedBeforeMutation || aborted.Ready || aborted.Identity != identities[0] || aborted.Generation != before.Generation {
		t.Fatalf("the coordinator would not learn the source is untouched: %+v", aborted)
	}
	// The failed attempt's encrypted backup and restore scratch are gone with it.
	if copies := attemptCopies(t, directory); len(copies) != 0 {
		t.Fatalf("the failed attempt left %v on the state volume", copies)
	}
	// A restart finds the marker and parks. Retrying could cross "write-intent"
	// before the coordinator puts the source back, leaving the Deployment on the target.
	if parked := runChild(1); parked == nil || !strings.Contains(parked.Error(), ErrPreparationAborted.Error()) {
		t.Fatalf("a target holding its own abort marker did not park: %v", parked)
	}
	if again, err := readAutomaticJournal(journalPath); err != nil || again == nil || again.Phase != "preparing" || again.Aborted == "" || control().Pending.Phase != upgrade.BackupPreparing || len(attemptCopies(t, directory)) != 0 {
		t.Fatalf("the parked target changed durable state: %+v %v", again, err)
	}

	// Only the journal's own source image may resume: another image stays refused.
	elsewhere := *journal
	elsewhere.Source.Identity = releaseidentity.Source{Release: identities[1]}
	if err := writeAutomaticJournal(journalPath, &elsewhere); err != nil {
		t.Fatal(err)
	}
	if err := runChild(0); err == nil || !strings.Contains(err.Error(), "requires its original exact image") || control().Pending.Phase != upgrade.BackupPreparing {
		t.Fatalf("an image that is not the journal's source resumed: %v", err)
	}
	// "write-intent" is written before the first step that can start a migration.
	// A marker left over from before it can neither release the source nor be read
	// by the coordinator.
	mutated := *journal
	mutated.Phase = "write-intent"
	if err := writeAutomaticJournal(journalPath, &mutated); err != nil {
		t.Fatal(err)
	}
	if err := runChild(0); err == nil || !strings.Contains(err.Error(), "requires its original exact image") || control().Pending.Phase != upgrade.BackupPreparing {
		t.Fatalf("a journal past preparing released the source: %v", err)
	}
	if inspect(1).AbortedBeforeMutation {
		t.Fatal("a marker from a run that went on to write was believed")
	}
	if err := writeAutomaticJournal(journalPath, journal); err != nil {
		t.Fatal(err)
	}

	if err := runChild(0); err != nil {
		t.Fatalf("the source did not resume: %v", err)
	}
	if resumed := control(); resumed.Pending.Phase != upgrade.Healthy || resumed.Maintenance || resumed.Pending.Preparation != nil || resumed.Generation != before.Generation || resumed.Applied != before.Applied {
		t.Fatalf("the source is still frozen: %+v", resumed.Pending)
	}
	if _, err := os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the unfinished journal was not cleared: %v", err)
	}
	if _, err := readUnattendedPrivate(filepath.Join(directory, "preparation-abandoned.json")); err != nil {
		t.Fatalf("the reason was not recorded: %v", err)
	}
	if observed := inspect(0); observed.AbortedBeforeMutation || !observed.Ready || observed.Identity != identities[0] {
		t.Fatalf("the resumed source is not observed as an ordinary healthy release: %+v", observed)
	}
	mutate(`DELETE FROM value_entries`, `DELETE FROM keys`, `DELETE FROM environments`, `DELETE FROM projects`, `DELETE FROM orgs`, `DELETE FROM principals WHERE id='usr_drill'`)
}

func attemptCopies(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() && (strings.HasPrefix(name, "backup-") || strings.HasPrefix(name, "evidence-") || strings.HasPrefix(name, "scratch-") || strings.HasPrefix(name, ".hikyo-upgrade-evidence-")) {
			found = append(found, name)
		}
	}
	return found
}
