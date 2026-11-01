//go:build darwin || linux

package app

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	hikyocrypto "github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/securityfeed"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

// This composes the production TUF repository and maintenance engine with the
// existing real packaged-upgrade fixture. Only systemd process management is
// substituted. Both backup/drill cycles, release authentication, compiled target
// gate, migration CLI, durable source/target ledger and HTTP readiness are real.
// It establishes a released source through the real legacy bridge before asking
// the security checker to maintain it; enrollment never invents source identity.
func TestSecurityMaintenanceAuthenticatedFeedAppliesPackagedRoute(t *testing.T) {
	// automaticProcessRoute builds two real fixture binaries. Keep its inherited
	// go-build concurrency bounded without changing the shared fixture owner.
	t.Setenv("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -p=2"))
	initial := newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
	baseRoute, claims, pinned := automaticProcessRoute(t, initial)
	first := baseRoute.Plan.Steps()[0].Target
	for _, scenario := range []string{"success", "postwrite-health-failure"} {
		t.Run(scenario, func(t *testing.T) {
			source := initial
			if scenario != "success" {
				source = newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
			}
			firstPlan, err := baseRoute.Bundle.Plan(upgradecompat.InstalledSource{Identity: source.source.Source, Migrations: baseRoute.Plan.Steps()[0].SourceMigrations, SchemaSHA256: source.source.SchemaDigest}, first)
			if err != nil || len(firstPlan.Steps()) != 1 {
				t.Fatalf("fixture requires an actual legacy-to-first release route: %v", err)
			}
			firstRoute := baseRoute
			firstRoute.Plan, firstRoute.Instance = firstPlan, source.source.InstanceID
			driver := newSecurityProcessDriver(t, source, firstRoute, pinned)
			if err := driver.applyRealRoute(t.Context()); err != nil {
				t.Fatalf("establish actual released source: %v", err)
			}
			before, err := driver.Observe(t.Context())
			if err != nil || !before.Healthy() || before.Identity != first || driver.host.migrations != 1 || driver.proofs != 1 {
				t.Fatalf("legacy setup did not become an admitted running release: %+v %v", before, err)
			}
			sourceManifest := firstPlan.Steps()[0].TargetMigrations
			actual, err := upgrade.InspectInstalled(t.Context(), driver.database.config, sourceManifest)
			if err != nil {
				t.Fatal(err)
			}
			securityPlan, err := baseRoute.Bundle.Plan(upgradecompat.InstalledSource{Identity: actual.Source, Migrations: sourceManifest, SchemaSHA256: actual.SchemaDigest}, baseRoute.Plan.Target())
			if err != nil || len(securityPlan.Steps()) != 1 || !securityPlan.Source().IsRelease() || securityPlan.RequiresOperatorAttestation() {
				t.Fatalf("security operation must start at the measured release through an ordinary signed edge: %v", err)
			}
			driver.route = baseRoute
			driver.route.Plan, driver.route.Instance = securityPlan, actual.InstanceID
			feed, repository, catalog := securityProcessFeed(t, securityPlan.Target(), first)
			stateDirectory := filepath.Join(securityIntegrationTemp(t), "coordinator")
			state, err := securitymaintenance.NewStore(stateDirectory)
			if err != nil {
				t.Fatal(err)
			}
			policy := securitymaintenance.DefaultPolicy()
			policy.Channel = updatecheck.ChannelNightly
			engine := &securitymaintenance.Engine{Store: state, Feed: feed, Driver: driver, Platform: runtime.GOOS + "/" + runtime.GOARCH, Policy: policy}
			if _, err := engine.Enroll(t.Context()); err != nil {
				t.Fatal(err)
			}
			status, err := engine.RunOnce(t.Context(), false)
			if err != nil || status.Phase != "applicable" || status.CatalogSequence != catalog.Sequence || status.LastVerified.IsZero() || driver.preparations != 0 || driver.applies != 0 || driver.host.command == nil {
				t.Fatalf("verified check changed the workload or missed the advisory: %+v %v", status, err)
			}
			driver.host.failHealth = scenario == "postwrite-health-failure"
			status, err = engine.RunOnce(t.Context(), true)
			if driver.preparations != 1 || driver.applies != 1 || driver.proofs != 2 || driver.host.migrations != 2 {
				t.Fatalf("security operation did not run one real prepared backup/drill/migration: preparations=%d applies=%d proofs=%d migrations=%d err=%v", driver.preparations, driver.applies, driver.proofs, driver.host.migrations, err)
			}
			if driver.host.failHealth {
				if err == nil || status.Phase != "failed" || status.Operation == nil || status.Operation.Target.Identity != securityPlan.Target() || driver.host.command != nil || !driver.host.fenced || driver.journal.Phase != "restore-required" {
					t.Fatalf("postwrite failure did not retain exact-target recovery: %+v %v", status, err)
				}
				control, readErr := driver.database.Control(t.Context())
				if readErr != nil || control.Pending == nil || control.Pending.Phase != upgrade.RestoreRequired || !control.Maintenance || control.Applied.Release != first {
					t.Fatalf("failed target changed the durable applied source or removed its fence: %+v %v", control, readErr)
				}
				// Reopen the durable checker state as after coordinator death. The
				// next scheduled check must not retry or restart the old executable.
				reopened, readErr := securitymaintenance.NewStore(stateDirectory)
				if readErr != nil {
					t.Fatal(readErr)
				}
				engine.Store = reopened
				status, readErr = engine.RunOnce(t.Context(), true)
				if readErr == nil || status.Reason != "operation_requires_reconciliation" || status.Operation == nil || driver.applies != 1 || driver.host.migrations != 2 || driver.host.command != nil {
					t.Fatalf("cold retry executed an uncertain target or restarted the old source: %+v %v", status, readErr)
				}
				return
			}
			if err != nil || status.Phase != "healthy" || status.Operation != nil || status.Running == nil || status.Running.Identity != securityPlan.Target() || status.Running.Generation <= before.Generation || status.CoverageThrough != catalog.Targets[0].CoverageThrough || driver.journal.Phase != "complete" {
				t.Fatalf("signed feed did not become the exact healthy running target: %+v %v", status, err)
			}
			pid := driver.host.command.Process.Pid
			status, err = engine.RunOnce(t.Context(), true)
			if err != nil || status.Phase != "current" || driver.applies != 1 || driver.preparations != 1 || driver.host.command.Process.Pid != pid {
				t.Fatalf("already fixed long-lived target was reapplied: %+v %v", status, err)
			}
			timestampPath := filepath.Join(repository, "metadata/timestamp.json")
			timestamp := automaticProcessRead(t, timestampPath)
			if err := os.Remove(timestampPath); err != nil {
				t.Fatal(err)
			}
			status, err = engine.RunOnce(t.Context(), true)
			if writeErr := os.WriteFile(timestampPath, timestamp, 0644); writeErr != nil {
				t.Fatal(writeErr)
			}
			if err == nil || status.Phase != "stale" || driver.host.command == nil || driver.host.command.Process.Pid != pid {
				t.Fatalf("feed outage shut down or silently blessed the healthy admitted service: %+v %v", status, err)
			}
			if current, err := driver.Observe(t.Context()); err != nil || !current.Healthy() || current.Identity != securityPlan.Target() {
				t.Fatalf("long-lived service lost exact readiness during feed outage: %+v %v", current, err)
			}
			if err := driver.host.FenceAndStop(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertSecurityProcessValuesReadable(t, source, driver, claims[1])
		})
	}
}

func securityIntegrationTemp(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

type securityProcessDriver struct {
	t            *testing.T
	source       upgradeDrillFixture
	route        automaticRoute
	pinned       releasetrust.PinnedTrust
	database     automaticStore
	host         *automaticProcessHost
	operator     *trustfixture.Fixture
	journal      *automaticJournal
	proofs       int
	preparations int
	applies      int
}

func newSecurityProcessDriver(t *testing.T, source upgradeDrillFixture, route automaticRoute, pinned releasetrust.PinnedTrust) *securityProcessDriver {
	t.Helper()
	work := securityIntegrationTemp(t)
	installation := filepath.Join(work, "installation")
	if err := os.Mkdir(installation, 0700); err != nil {
		t.Fatal(err)
	}
	operator := trustfixture.New(t)
	publicPath := filepath.Join(work, "operator.pub")
	automaticProcessPut(t, publicPath, operator.PrimaryPublic)
	automaticProcessPut(t, filepath.Join(work, "root.key"), []byte(hikyocrypto.EncodeRootKey(source.root)))
	host := &automaticProcessHost{work: work, installed: filepath.Join(work, "hikyo"), fenced: true, baseEnv: []string{"PATH=" + os.Getenv("PATH"), "HOME=" + work, "HIKYO_DB=sqlite:" + source.cfg.Path, "HIKYO_EXTERNAL_ORIGIN=https://security-maintenance.fixture.invalid", "HIKYO_UPDATE_CHANNEL=off", "HIKYO_UPGRADE_STATE_DIR=" + installation}}
	host.evidence = hostupgrade.RuntimeEvidence{BundleDirectory: route.Directory, OperatorPublicKey: publicPath, TargetManifest: string(route.Plan.Target().ManifestSHA256), LegacyWritersStopped: true}
	driver := &securityProcessDriver{t: t, source: source, route: route, pinned: pinned, database: automaticStore{upgrade.Config{Engine: releaseidentity.SQLite, Path: source.cfg.Path}}, host: host, operator: operator}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.FenceAndStop(ctx); err != nil {
			t.Errorf("stop owned security integration process: %v", err)
		}
	})
	return driver
}

func (d *securityProcessDriver) Observe(ctx context.Context) (securitymaintenance.Observation, error) {
	state, err := d.database.Control(ctx)
	if err != nil {
		return securitymaintenance.Observation{}, err
	}
	observed, err := securityControlObservation(state)
	if err != nil {
		return observed, err
	}
	observed.Engine = releaseidentity.SQLite
	manifest, err := releaseidentity.BuildMigrationManifest(store.MigrationsFS, "migrations/sqlite", releaseidentity.SQLite)
	if err != nil {
		return observed, err
	}
	actual, err := upgrade.InspectInstalled(ctx, d.database.config, manifest)
	if err != nil || actual.Source != state.Applied || actual.SchemaDigest != state.SchemaDigest || actual.MigrationDigest != state.MigrationDigest || actual.Ledger == nil || actual.Ledger.Generation != state.Generation {
		return observed, errors.Join(err, errors.New("real security fixture catalog differs from the applied ledger"))
	}
	if d.host.command == nil || d.host.fenced {
		observed.Ready = false
		return observed, nil
	}
	prepared, ok := d.route.Executables[observed.Identity]
	if !ok {
		return observed, errors.New("running security fixture release has no independently selected executable")
	}
	raw, err := os.ReadFile(d.host.installed)
	if err != nil || releaseidentity.Hash(raw) != prepared.BinarySHA256 {
		return observed, errors.Join(err, errors.New("real installed executable differs from selected release bytes"))
	}
	select {
	case processErr := <-d.host.done:
		d.host.command, d.host.fenced = nil, true
		return observed, errors.Join(processErr, errors.New("real security fixture process exited"))
	default:
	}
	if err := securityProcessReadiness(ctx, d.host.command); err != nil {
		return observed, err
	}
	return observed, nil
}

func (d *securityProcessDriver) Prepare(ctx context.Context, target securitymaintenance.Target) (securitymaintenance.Preparation, error) {
	d.preparations++
	observed, err := d.Observe(ctx)
	if err != nil || !observed.Healthy() || target.Identity != d.route.Plan.Target() || target.Image != "" {
		return securitymaintenance.Preparation{}, errors.Join(err, errors.New("security fixture requires the exact healthy released source and target"))
	}
	state, err := d.database.Control(ctx)
	if err != nil {
		return securitymaintenance.Preparation{}, err
	}
	bundle, err := upgradebundle.Load(ctx, d.route.Directory, d.pinned, state.Floor)
	if err != nil {
		return securitymaintenance.Preparation{}, err
	}
	manifest, err := d.route.Plan.SourceManifest(releaseidentity.SQLite)
	if err != nil {
		return securitymaintenance.Preparation{}, err
	}
	actual, err := upgrade.InspectInstalled(ctx, d.database.config, manifest)
	if err != nil {
		return securitymaintenance.Preparation{}, err
	}
	plan, err := bundle.Plan(upgradecompat.InstalledSource{Identity: actual.Source, Migrations: manifest, SchemaSHA256: actual.SchemaDigest}, target.Identity)
	if err != nil || plan.Digest() != d.route.Plan.Digest() {
		return securitymaintenance.Preparation{}, errors.Join(err, errors.New("independently authenticated security route changed"))
	}
	impact := securitymaintenance.Impact{Restart: true}
	for _, step := range plan.Steps() {
		prepared, ok := d.route.Executables[step.Target]
		if !ok || prepared.Identity != step.Target || prepared.BinarySHA256.Validate() != nil {
			return securitymaintenance.Preparation{}, errors.New("security route lacks an exact staged executable")
		}
		raw, err := os.ReadFile(prepared.BinaryPath)
		if err != nil || releaseidentity.Hash(raw) != prepared.BinarySHA256 {
			return securitymaintenance.Preparation{}, errors.Join(err, errors.New("staged executable changed before stopping writers"))
		}
		impact.Maintenance = impact.Maintenance || step.Mode == upgradecompat.Maintenance
		impact.Schema = impact.Schema || step.SourceSchemaSHA256 != step.TargetSchemaSHA256
	}
	return securitymaintenance.Preparation{Target: target.Identity, Source: observed, RouteSHA256: plan.Digest(), Impact: impact}, nil
}

func (d *securityProcessDriver) Apply(ctx context.Context, target securitymaintenance.Target) error {
	d.applies++
	if target.Identity != d.route.Plan.Target() || !target.EligibilityUntil.After(time.Now()) {
		return errors.New("security target or freshly verified eligibility changed")
	}
	return d.applyRealRoute(ctx)
}

func (d *securityProcessDriver) applyRealRoute(ctx context.Context) error {
	if err := d.host.FenceAndStop(ctx); err != nil {
		return err
	}
	journal, err := d.proveBackup(ctx)
	if err != nil {
		return err
	}
	d.journal = journal
	path := filepath.Join(d.host.work, "operation.json")
	if err := writeAutomaticJournal(path, journal); err != nil {
		return err
	}
	staged := map[releaseidentity.Identity]string{}
	for identity, prepared := range d.route.Executables {
		staged[identity] = prepared.BinaryPath
	}
	return applyAutomaticRoute(ctx, d.host, d.database, d.route, staged, journal, path, io.Discard)
}

// This follows automaticProcessProof's real export and restore-drill flow, while
// retaining one operator signer and installation directory across two upgrades.
// The existing helper intentionally returns no signer, so calling it again for
// a released source would incorrectly attempt to replace the durable pin.
func (d *securityProcessDriver) proveBackup(ctx context.Context) (_ *automaticJournal, returnErr error) {
	if !d.host.fenced || d.host.command != nil {
		return nil, errors.New("real security backup requires stopped writers")
	}
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		return nil, err
	}
	output, err := os.MkdirTemp(d.host.work, "backup-")
	if err != nil {
		return nil, err
	}
	evidence := hostupgrade.RuntimeEvidence{BundleDirectory: d.route.Directory, OperatorPublicKey: d.host.evidence.OperatorPublicKey, TargetManifest: string(d.route.Plan.Target().ManifestSHA256), LegacyWritersStopped: d.route.Plan.Source().Genesis == releaseidentity.LegacyGenesisV1}
	command := exec.CommandContext(ctx, d.route.Executables[d.route.Plan.Target()].BinaryPath, "backup", "upgrade-export", "--json", "--out", output, "--recipient", recipient)
	command.Env = d.host.environment(evidence)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("actual security backup export: %w: %s", err, stderr.String())
	}
	var exported struct {
		Ciphertext string `json:"ciphertext"`
		Receipt    string `json:"receipt"`
	}
	if definitions.DecodeStrict(stdout.Bytes(), &exported) != nil {
		return nil, errors.New("security backup export did not return its closed artifact contract")
	}
	receipt, err := os.ReadFile(exported.Receipt)
	if err != nil {
		return nil, err
	}
	ciphertext, err := backupreceipt.PinCiphertext(ctx, exported.Ciphertext, d.host.work)
	if err != nil {
		return nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, ciphertext.Close()) }()
	operator, err := backupreceipt.PinOperator(d.source.source.InstanceID, d.operator.PrimaryPublic)
	if err != nil {
		return nil, err
	}
	scratch, err := os.MkdirTemp(d.host.work, "scratch-")
	if err != nil {
		return nil, err
	}
	drill, err := DrillUpgrade(ctx, UpgradeDrillRequest{Scratch: store.Config{Engine: store.EngineSQLite, Path: filepath.Join(scratch, "restore.db")}, Ciphertext: ciphertext, Receipt: receipt, Plan: d.route.Plan, Operator: operator, Unlock: backup.Unlock{Identity: identity}, RootKey: bytes.Clone(d.source.root), AutoCredentialProof: true, Now: time.Now().UTC(), Lifetime: time.Hour})
	if err != nil {
		return nil, err
	}
	if !drill.HierarchyReadable || drill.SecretProof != "existing-secret-readable" || drill.CredentialProof != "reconciled-minted-revoked" {
		return nil, errors.New("real security restore drill did not prove hierarchy, stored values and recovered credentials")
	}
	public, err := os.MkdirTemp(d.host.work, "evidence-")
	if err != nil {
		return nil, err
	}
	for name, raw := range map[string][]byte{"receipt.json": receipt, "attestation.json": drill.Attestation, "attestation.sigstore.json": trustfixture.Sign(d.t, d.operator.PrimarySigner, drill.Attestation)} {
		automaticProcessPut(d.t, filepath.Join(public, name), raw)
	}
	evidence.EvidenceDirectory, evidence.CiphertextPath = public, exported.Ciphertext
	manifest, err := d.route.Plan.SourceManifest(releaseidentity.SQLite)
	if err != nil {
		return nil, err
	}
	d.proofs++
	return &automaticJournal{Format: "hikyo.host-upgrade/v1", Phase: "proved", Target: d.route.Plan.Target(), Source: upgradecompat.InstalledSource{Identity: d.route.Plan.Source(), Migrations: manifest, SchemaSHA256: d.route.Plan.SourceSchemaDigest()}, Instance: d.source.source.InstanceID, Route: d.route.Plan.Digest(), Runtime: evidence}, nil
}

func securityProcessReadiness(ctx context.Context, command *exec.Cmd) error {
	log, ok := command.Stdout.(*os.File)
	if !ok {
		return errors.New("real security process lacks its owned structured log")
	}
	raw, err := os.ReadFile(log.Name())
	if err != nil || len(raw) > 1<<20 {
		return errors.Join(err, errors.New("real security process log is unavailable or unbounded"))
	}
	operational := ""
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var event struct {
			Message     string `json:"msg"`
			Operational string `json:"operational_addr"`
		}
		if json.Unmarshal(line, &event) == nil && event.Message == "server ready" {
			operational = event.Operational
		}
	}
	host, _, err := net.SplitHostPort(operational)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		return errors.New("real security process did not report a loopback readiness endpoint")
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	for _, path := range []string{"/healthz", "/readyz"} {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+operational+path, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return errors.New("real security process failed HTTP health/readiness")
		}
	}
	return nil
}

func securityProcessFeed(t *testing.T, target, source releaseidentity.Identity) (*securityfeed.Repository, string, securitymaintenance.Catalog) {
	t.Helper()
	platform := runtime.GOOS + "/" + runtime.GOARCH
	now := time.Now().UTC()
	catalog := securitymaintenance.Catalog{Format: securitymaintenance.CatalogFormat, Sequence: 1, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Advisories: []securitymaintenance.Advisory{{ID: "HIKYO-INTEGRATION-SECURITY", Summary: "Synthetic packaged-route acceptance fixture", Severity: securitymaintenance.High, Components: []string{"hikyo"}, Affected: []securitymaintenance.Affected{{Profile: releaseidentity.NightlyV1, Introduced: source.Version, Fixed: target.Version, Platforms: []string{platform}}}}}, Targets: []securitymaintenance.Target{{Identity: target, Platforms: []string{platform}, Fixes: []string{"HIKYO-INTEGRATION-SECURITY"}, CoverageThrough: now.Add(-time.Minute).Format(time.DateOnly), Impact: securitymaintenance.Impact{Restart: true, Maintenance: true}}}}
	raw := trustfixture.JSON(t, catalog)
	if _, err := securitymaintenance.DecodeCatalog(raw, now); err != nil {
		t.Fatal(err)
	}
	roles := map[string]securityfeed.RoleKeys{}
	signers := map[string][]signature.Signer{}
	for _, role := range metadata.TOP_LEVEL_ROLE_NAMES {
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		signer, err := signature.LoadSigner(private, crypto.Hash(0))
		if err != nil {
			t.Fatal(err)
		}
		roles[role], signers[role] = securityfeed.RoleKeys{PublicKeys: []crypto.PublicKey{public}, Threshold: 1}, []signature.Signer{signer}
	}
	root, err := securityfeed.CreateRoot(securityfeed.RootConfig{Version: 1, Expires: now.Add(24 * time.Hour), Roles: roles, Signers: signers[metadata.ROOT]})
	if err != nil {
		t.Fatal(err)
	}
	publication, err := securityfeed.BuildPublication(securityfeed.PublishConfig{Root: root, Catalog: raw, Version: 1, Signers: signers, Expires: securityfeed.Expirations{Targets: now.Add(time.Hour), Snapshot: now.Add(45 * time.Minute), Timestamp: now.Add(30 * time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	directory := securityIntegrationTemp(t)
	repository := filepath.Join(directory, "repository")
	if err := publication.Write(repository); err != nil {
		t.Fatal(err)
	}
	feed, err := securityfeed.New(securityfeed.Config{StateDirectory: filepath.Join(directory, "client-state"), TrustedRoot: root, OfflineDirectory: repository})
	if err != nil {
		t.Fatal(err)
	}
	return feed, repository, catalog
}

func assertSecurityProcessValuesReadable(t *testing.T, source upgradeDrillFixture, driver *securityProcessDriver, claim []byte) {
	t.Helper()
	node, err := driver.route.Bundle.MatchBuild(claim)
	if err != nil {
		t.Fatal(err)
	}
	var admission upgrade.Admission
	err = upgrade.WithLock(t.Context(), driver.database.config, func(session *upgrade.Session) error {
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
	db, err := store.Open(t.Context(), source.cfg, admission)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	kr, err := hikyocrypto.LoadKeyring(t.Context(), &keyring.Store{DB: db}, bytes.Clone(source.root))
	if err != nil {
		t.Fatal(err)
	}
	readable, err := (&service.Backup{DB: db}).ProveValuesReadable(t.Context(), kr)
	if err != nil || !readable {
		t.Fatal("security maintenance lost the actual protected value", err)
	}
}
