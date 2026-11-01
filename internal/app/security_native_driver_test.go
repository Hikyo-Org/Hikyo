//go:build darwin || linux

package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	bundlefixture "github.com/Hikyo-Org/hikyo/internal/upgradebundle/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type nativeDriverHost struct {
	configuration hostupgrade.Config
	digest        releaseidentity.Digest
	check         func()
	failure       error
}

func (h *nativeDriverHost) Config() hostupgrade.Config { return h.configuration }
func (h *nativeDriverHost) Environment() map[string]string {
	return map[string]string{"HIKYO_DB": "sqlite:fixture.db"}
}
func (h *nativeDriverHost) CheckRunning(_ context.Context, digest string) error {
	if h.check != nil {
		h.check()
	}
	if h.failure != nil {
		return h.failure
	}
	if string(h.digest) != digest {
		return errors.New("running payload mismatch")
	}
	return nil
}

type nativeDriverInspection struct {
	t            *testing.T
	state        upgrade.State
	actualSchema releaseidentity.Digest
	lastManifest releaseidentity.MigrationManifest
}

func (s *nativeDriverInspection) snapshot() upgrade.State {
	s.t.Helper()
	raw, err := json.Marshal(s.state)
	if err != nil {
		s.t.Fatal(err)
	}
	var result upgrade.State
	if err := definitions.DecodeStrict(raw, &result); err != nil {
		s.t.Fatal(err)
	}
	return result
}

func (s *nativeDriverInspection) Control(context.Context) (upgrade.State, error) {
	return s.snapshot(), nil
}
func (s *nativeDriverInspection) Installed(_ context.Context, manifest releaseidentity.MigrationManifest) (upgrade.InstalledSource, error) {
	s.lastManifest = manifest.Clone()
	state := s.snapshot()
	digest, err := manifest.Digest()
	if err != nil || digest != state.MigrationDigest {
		return upgrade.InstalledSource{}, upgrade.ErrConflict
	}
	schema := state.SchemaDigest
	if s.actualSchema != "" {
		schema = s.actualSchema
	}
	return upgrade.InstalledSource{Source: state.Applied, MigrationDigest: digest, SchemaDigest: schema, InstanceID: state.InstanceID, RestoreEpoch: state.RestoreEpoch, Ledger: &state}, nil
}

type nativeDriverFixture struct {
	driver     *nativeSecurityDriver
	host       *nativeDriverHost
	inspection *nativeDriverInspection
	proofs     map[releaseidentity.Identity]preparedSecurityRelease
	target     securitymaintenance.Target
	fetched    []releaseidentity.Identity
	now        time.Time
}

func newNativeDriverFixture(t *testing.T) *nativeDriverFixture {
	t.Helper()
	root := t.TempDir()
	empty := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}
	manifest := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{{Version: 1, SHA256: releaseidentity.Hash([]byte("actual migration"))}}}
	genesis := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Genesis: releaseidentity.FreshGenesisV1}, Migrations: empty, SchemaSHA256: releaseidentity.Hash([]byte("empty catalog"))}
	sourceSchema, targetSchema := releaseidentity.Hash([]byte("source catalog")), releaseidentity.Hash([]byte("target catalog"))
	signed := bundlefixture.Write(t, genesis, []bundlefixture.Target{
		{Version: "1.2.1", Sequence: 1, Commit: strings.Repeat("a", 40), Migrations: manifest, SchemaSHA256: sourceSchema},
		{Version: "1.3.1", Sequence: 2, Commit: strings.Repeat("b", 40), Migrations: manifest, SchemaSHA256: targetSchema},
	})
	installer, err := selfupdate.NewInstaller(selfupdate.Config{StateDir: root,
		TrustRootBase64: base64.StdEncoding.EncodeToString(signed.Pinned.Root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(signed.Pinned.RecoveryPublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	f := &nativeDriverFixture{proofs: map[releaseidentity.Identity]preparedSecurityRelease{}, now: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	for _, identity := range signed.Identities {
		binary := filepath.Join(root, "hikyo-"+identity.Version)
		payload := []byte("#!/bin/sh\n# " + identity.Version + "\n[ \"$1\" = --upgrade-bundle-formats ] || exit 2\nprintf '" + upgradebundle.PlatformIndexFormat + "\\n'\n")
		if err := os.WriteFile(binary, payload, 0700); err != nil {
			t.Fatal(err)
		}
		release, err := signed.Bundle.Release(identity)
		if err != nil {
			t.Fatal(err)
		}
		f.proofs[identity] = preparedSecurityRelease{Installer: installer, Pinned: signed.Pinned,
			Source: &nativeSecurityReleaseSource{}, Target: selfupdate.PreparedNightly{Identity: identity, BundleDirectory: signed.Directory, BinaryPath: binary, BinarySHA256: releaseidentity.Hash(payload)}, Bundle: signed.Bundle, Release: release}
	}
	sourceID := signed.Identities[0]
	admission, err := signed.Bundle.Plan(genesis, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	sourceDigest, _ := empty.Digest()
	targetDigest, _ := manifest.Digest()
	incarnation := upgrade.Incarnation{1}
	floor, rootDigest := signed.Bundle.Snapshot().Floor(), releaseidentity.Hash(signed.Pinned.Root)
	state := upgrade.State{TrustDomain: upgrade.Production, InstanceID: "native-fixture", Applied: releaseidentity.Source{Release: sourceID},
		MigrationDigest: targetDigest, SchemaDigest: sourceSchema, Generation: 1, RecoveryIncarnation: incarnation, Floor: floor, ReleaseRootDigest: rootDigest,
		Pending: &upgrade.Operation{Kind: upgrade.UpgradeOperation, RouteSource: genesis.Identity, Source: genesis.Identity, Target: sourceID,
			SourceMigrationDigest: sourceDigest, TargetMigrationDigest: targetDigest, SourceSchemaDigest: genesis.SchemaSHA256, TargetSchemaDigest: sourceSchema,
			RouteDigest: admission.Digest(), Hop: 0, RouteLength: 1, Generation: 1, RecoveryIncarnation: incarnation, Phase: upgrade.Healthy,
			Acceptance: upgrade.Acceptance{Floor: floor, ReleaseRootDigest: rootDigest}}}
	if err := state.Validate(); err != nil {
		t.Fatal("invalid fixture admission", err)
	}
	f.inspection = &nativeDriverInspection{t: t, state: state}
	c := hostupgrade.DefaultConfig()
	c.StateDirectory = filepath.Join(root, "native-state")
	f.host = &nativeDriverHost{configuration: c, digest: f.proofs[sourceID].Target.BinarySHA256}
	f.driver = &nativeSecurityDriver{configuration: "/etc/hikyo/upgrade.json", out: io.Discard,
		now: func() time.Time { return f.now }, executeHandoff: executeNativeSecurityHandoff, runUpgrade: runNativeSecurityUpgrade}
	f.driver.load = func(context.Context) (nativeSecurityInstallation, error) {
		return nativeSecurityInstallation{Host: f.host, Database: upgrade.Config{Engine: releaseidentity.SQLite}, Inspection: f.inspection}, nil
	}
	// Source artifacts and host/process inspection are the external seams. The
	// source bundle and every route below still use genuine signature verification.
	f.driver.verifySource = func(_ context.Context, _ nativeSecurityInstallation, state upgrade.State) (preparedSecurityRelease, error) {
		proof, ok := f.proofs[state.Applied.Release]
		if !ok {
			return proof, errors.New("local source proof unavailable")
		}
		return proof, nil
	}
	f.driver.prepareRelease = func(_ context.Context, identity releaseidentity.Identity, _ string) (preparedSecurityRelease, error) {
		f.fetched = append(f.fetched, identity)
		proof, ok := f.proofs[identity]
		if !ok {
			return proof, errors.New("unknown independently verified release")
		}
		return proof, nil
	}
	f.target = securitymaintenance.Target{Identity: signed.Target, Impact: securitymaintenance.Impact{Restart: true}}
	return f
}

func TestNativeSecurityDriverObservesInstalledSourceWithoutVersionLookup(t *testing.T) {
	f := newNativeDriverFixture(t)
	observed, err := f.driver.Observe(t.Context())
	if err != nil || !observed.Healthy() || observed.Identity != f.inspection.state.Applied.Release || observed.Engine != releaseidentity.SQLite {
		t.Fatal("failed to prove the installed source independently of the checker build", observed, err)
	}
	if len(f.fetched) != 0 {
		t.Fatal("observation disclosed the installed source in a release lookup", f.fetched)
	}
	digest, _ := f.inspection.lastManifest.Digest()
	if digest != f.inspection.state.MigrationDigest {
		t.Fatal("inspected checker migrations instead of authenticated source migrations")
	}
}

func TestNativeSecurityDriverRejectsSchemaRootAndRuntimeSubstitution(t *testing.T) {
	for _, failure := range []string{"schema", "root", "runtime", "changed-during-readiness"} {
		t.Run(failure, func(t *testing.T) {
			f := newNativeDriverFixture(t)
			switch failure {
			case "schema":
				f.inspection.actualSchema = releaseidentity.Hash([]byte("substituted catalog"))
			case "root":
				root := releaseidentity.Hash([]byte("another root"))
				f.inspection.state.ReleaseRootDigest, f.inspection.state.Pending.Acceptance.ReleaseRootDigest = root, root
			case "runtime":
				f.host.failure = errors.New("running bytes are not the installed source")
			case "changed-during-readiness":
				f.host.check = func() {
					f.inspection.state.Generation++
					f.inspection.state.Pending.Generation++
				}
			}
			if _, err := f.driver.Observe(t.Context()); err == nil {
				t.Fatal("accepted inconsistent source proof")
			}
			if len(f.fetched) != 0 {
				t.Fatal("source failure triggered a release download")
			}
		})
	}
}

func TestNativeSecurityDriverStagesSignedRouteBeforeApplication(t *testing.T) {
	f := newNativeDriverFixture(t)
	prepared, err := f.driver.Prepare(t.Context(), f.target)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Target != f.target.Identity || !prepared.Source.Healthy() || prepared.Image != "" || prepared.RouteSHA256.Validate() != nil || !prepared.Impact.Schema || !prepared.Impact.Maintenance {
		t.Fatal("did not measure and stage the real authenticated route", prepared)
	}
	if !slices.Equal(f.fetched, []releaseidentity.Identity{f.target.Identity}) || f.inspection.state.Generation != 1 {
		t.Fatal("staging downloaded an installed-version locator or changed the source", f.fetched)
	}
	if err := preflightUnattendedRoute(t.Context(), f.driver.prepared.Route); err != nil {
		t.Fatal("reported staged without the actual route payloads", err)
	}
	if _, err := f.driver.Observe(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := preflightUnattendedRoute(t.Context(), f.driver.prepared.Route); err != nil {
		t.Fatal("later source observation destroyed the staged route", err)
	}
}

func TestNativeSecurityDriverPrestopRefusalsPreserveHealthySource(t *testing.T) {
	for _, failure := range []string{"not-staged", "deadline", "changed-source", "changed-payload", "changed-target"} {
		t.Run(failure, func(t *testing.T) {
			f := newNativeDriverFixture(t)
			if failure != "not-staged" {
				if _, err := f.driver.Prepare(t.Context(), f.target); err != nil {
					t.Fatal(err)
				}
			}
			target := f.target
			target.EligibilityUntil = f.now.Add(time.Hour)
			switch failure {
			case "deadline":
				f.now = f.now.Add(2 * time.Hour)
			case "changed-source":
				f.inspection.state.Generation++
				f.inspection.state.Pending.Generation++
			case "changed-payload":
				if err := os.WriteFile(f.proofs[target.Identity].Target.BinaryPath, []byte("substituted"), 0700); err != nil {
					t.Fatal(err)
				}
			case "changed-target":
				target.Identity.ManifestSHA256 = releaseidentity.Hash([]byte("other manifest"))
			}
			invoked := false
			f.driver.runUpgrade = func(context.Context, releaseidentity.Identity, string, nativeSecurityAuthorization, io.Writer) error {
				invoked = true
				return nil
			}
			if err := f.driver.Apply(t.Context(), target); !errors.Is(err, securitymaintenance.ErrNotStarted) || invoked {
				t.Fatal("pre-stop refusal invoked the coordinator or lost safe refusal classification", err)
			}
			if f.inspection.state.Maintenance || f.inspection.state.Pending.Phase != upgrade.Healthy {
				t.Fatal("pre-stop refusal altered the healthy source")
			}
		})
	}
}

func TestNativeSecurityDriverCarriesExactRouteAndDeadlineThroughHandoff(t *testing.T) {
	f := newNativeDriverFixture(t)
	prepared, err := f.driver.Prepare(t.Context(), f.target)
	if err != nil {
		t.Fatal(err)
	}
	target := f.target
	target.EligibilityUntil = f.now.Add(time.Hour)
	target.PolicyFile, target.PolicySHA256 = "/etc/hikyo/security-maintenance.json", releaseidentity.Hash([]byte("operator policy"))
	f.driver.runUpgrade = func(_ context.Context, identity releaseidentity.Identity, configuration string, authorization nativeSecurityAuthorization, _ io.Writer) error {
		if identity != target.Identity || authorization.Route != prepared.RouteSHA256 || !authorization.Until.Equal(target.EligibilityUntil) || authorization.PolicyFile != target.PolicyFile || authorization.PolicySHA256 != target.PolicySHA256 {
			t.Fatal("coordinator lost staged authorization context")
		}
		if authorization.Source == nil || !sameNativeSecurityObservation(authorization.Source.Source, prepared.Source) || authorization.Source.HostSHA256 != f.driver.prepared.SourceGuard.HostSHA256 {
			t.Fatal("coordinator lost original native source or authority")
		}
		handoff, err := automaticReleaseHandoffWithAuthorization(f.proofs[identity].Target, configuration, &identity, authorization)
		if err != nil {
			return err
		}
		return handoff
	}
	executed := false
	f.driver.executeHandoff = func(_ context.Context, handoff *AutomaticHandoff, _ io.Writer) error {
		executed = true
		if !slices.Contains(handoff.Arguments, string(prepared.RouteSHA256)) || !slices.Contains(handoff.Arguments, target.EligibilityUntil.Format(time.RFC3339Nano)) || !slices.Contains(handoff.Arguments, target.PolicyFile) || !slices.Contains(handoff.Arguments, string(target.PolicySHA256)) {
			t.Fatal("verified target handoff lost the exact route or feed expiry", handoff.Arguments)
		}
		index := slices.Index(handoff.Arguments, "--maintenance-source")
		if index < 0 || index+1 >= len(handoff.Arguments) {
			t.Fatal("handoff lost native source guard")
		}
		guard, err := parseAutomaticSecuritySource(handoff.Arguments[index+1], true, &target.Identity)
		if err != nil || !sameNativeSecurityObservation(guard.Source, prepared.Source) || guard.HostSHA256 != f.driver.prepared.SourceGuard.HostSHA256 {
			t.Fatal("handoff changed original native source or authority", err)
		}
		return nil
	}
	if err := f.driver.Apply(t.Context(), target); err != nil || !executed {
		t.Fatal("did not execute the verified coordinator handoff", err)
	}
}

func TestNativeSecurityGuardRefusesRestoredSourceAndChangedAuthority(t *testing.T) {
	f := newNativeDriverFixture(t)
	if _, err := f.driver.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	guard := f.driver.prepared.SourceGuard
	configuration, environment := f.host.Config(), f.host.Environment()
	if err := checkNativeSecuritySourceGuard(guard, configuration, environment, guard.Source); err != nil {
		t.Fatal("unchanged original source refused", err)
	}
	for name, change := range map[string]func(*securitymaintenance.Observation){
		"installation": func(o *securitymaintenance.Observation) { o.Instance += "replacement" },
		"generation":   func(o *securitymaintenance.Observation) { o.Generation++ },
		"restore":      func(o *securitymaintenance.Observation) { o.RestoreEpoch++ },
		"incarnation":  func(o *securitymaintenance.Observation) { o.Incarnation = strings.Repeat("d", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			current := guard.Source
			change(&current)
			if err := checkNativeSecuritySourceGuard(guard, configuration, environment, current); !errors.Is(err, securitymaintenance.ErrNotStarted) {
				t.Fatal("same-release source change was adopted before maintenance", err)
			}
		})
	}
	changed := configuration
	changed.Unit = "hikyo-other.service"
	if err := checkNativeSecuritySourceGuard(guard, changed, environment, guard.Source); !errors.Is(err, securitymaintenance.ErrNotStarted) {
		t.Fatal("changed native configuration was adopted", err)
	}
	environment["HIKYO_DB"] = "sqlite:another.db"
	if err := checkNativeSecuritySourceGuard(guard, configuration, environment, guard.Source); !errors.Is(err, securitymaintenance.ErrNotStarted) {
		t.Fatal("changed native environment was adopted", err)
	}
}

func TestNativeSecurityHandoffPreservesDefiniteRefusalAndAmbiguousFailure(t *testing.T) {
	for _, code := range []int{0, AutomaticUpgradeRefusedExitCode, 1} {
		path := filepath.Join(t.TempDir(), "verified-coordinator-fixture")
		if err := os.WriteFile(path, []byte(fmt.Sprintf("#!/bin/sh\nexit %d\n", code)), 0700); err != nil {
			t.Fatal(err)
		}
		err := executeNativeSecurityHandoff(t.Context(), &AutomaticHandoff{Executable: path}, io.Discard)
		if code == 0 && err != nil || code != 0 && err == nil || errors.Is(err, securitymaintenance.ErrNotStarted) != (code == AutomaticUpgradeRefusedExitCode) {
			t.Fatalf("coordinator exit %d lost its mutation classification: %v", code, err)
		}
		if code == AutomaticUpgradeRefusedExitCode {
			ordinary := exec.CommandContext(t.Context(), path).Run()
			if AutomaticUpgradeExitCode(ordinary) == AutomaticUpgradeRefusedExitCode {
				t.Fatal("ordinary host subprocess status was treated as a verified coordinator refusal")
			}
		}
	}
}

func TestNativeSecurityFirstUseRefusalRetainsRunningSourceProof(t *testing.T) {
	f := newNativeDriverFixture(t)
	if _, err := f.driver.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	guard := &f.driver.prepared.SourceGuard
	cache := t.TempDir()
	pinned := f.proofs[f.target.Identity].Pinned
	installer, err := selfupdate.NewInstaller(selfupdate.Config{StateDir: cache, TrustRootBase64: base64.StdEncoding.EncodeToString(pinned.Root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(pinned.RecoveryPublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	for _, identity := range []releaseidentity.Identity{guard.Source.Identity, f.target.Identity} {
		path := filepath.Join(cache, "stable-"+string(identity.ManifestSHA256)+"-"+runtime.GOOS+"-"+runtime.GOARCH)
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "archive-witness"), []byte("retained source proof"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(struct {
		Floor   releaseidentity.SnapshotFloor `json:"floor"`
		Release releaseidentity.Identity      `json:"release"`
	}{Release: f.target.Identity})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "stable-prepared-trust.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	// First-use native enrollment has no previous host operation journal.
	// Target preparation has advanced trust state, but the serving source
	// remains an active consumer through both initial and refusal cleanup.
	for range 2 {
		if err := installer.PruneNightlyCache(t.Context(), automaticRetainedReleases(nil, guard)...); err != nil {
			t.Fatal(err)
		}
	}
	journal := filepath.Join(t.TempDir(), "operation.json")
	if err := os.WriteFile(journal, []byte("unstarted preparing attempt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreAutomaticUnstartedJournal(journal, nil); err != nil {
		t.Fatal(err)
	}
	for _, identity := range []releaseidentity.Identity{guard.Source.Identity, f.target.Identity} {
		path := filepath.Join(cache, "stable-"+string(identity.ManifestSHA256)+"-"+runtime.GOOS+"-"+runtime.GOARCH, "archive-witness")
		if _, err := os.Stat(path); err != nil {
			t.Fatal("pre-mutation refusal lost offline source/target evidence", err)
		}
	}
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatal("unstarted attempt left a host recovery obligation", err)
	}
}

func TestNativeSecurityCoordinatorClassifiesFailureBeforeFirstFence(t *testing.T) {
	f := newNativeDriverFixture(t)
	if _, err := f.driver.Prepare(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	// A directory is never a valid native configuration file. This takes a
	// real pre-start failure path on Linux/root and the earlier platform gate
	// elsewhere, without granting a test authority to touch a systemd unit.
	err := runNativeSecurityUpgrade(t.Context(), f.target.Identity, "/", nativeSecurityAuthorization{Route: f.driver.prepared.Proof.RouteSHA256, Until: time.Now().Add(time.Hour), Source: &f.driver.prepared.SourceGuard}, io.Discard)
	if !errors.Is(err, securitymaintenance.ErrNotStarted) {
		t.Fatal("ordinary pre-start coordinator error invented a recovery obligation", err)
	}
}

func TestNativeSecuritySourceProofUnavailableDoesNotQueryReleaseDiscovery(t *testing.T) {
	state := upgrade.State{Applied: releaseidentity.Source{Release: nativeSecurityIdentity(releaseidentity.StableV1)}}
	if _, err := verifyNativeSecuritySource(t.Context(), nativeSecurityInstallation{}, state); err == nil || !strings.Contains(err.Error(), "source proof unavailable") {
		t.Fatal("did not provide actionable local source-proof refusal", err)
	}
}
