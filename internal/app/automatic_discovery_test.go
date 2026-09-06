package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradeassembly"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type automaticDiscoveryFixture struct {
	t          *testing.T
	root       string
	trust      *trustfixture.Fixture
	options    upgradeassembly.Options
	prepared   map[releaseidentity.Identity]selfupdate.PreparedNightly
	identities []releaseidentity.Identity
	source     upgradecompat.InstalledSource
	fetched    []releaseidentity.Identity
	assembled  int
	forbidden  map[releaseidentity.Identity]bool
}

func (f *automaticDiscoveryFixture) Releases(context.Context) ([]updatecheck.Release, error) {
	return nil, errors.New("unexpected broad release discovery")
}
func (f *automaticDiscoveryFixture) ReleaseByVersion(_ context.Context, version string) (updatecheck.Release, error) {
	for _, identity := range f.identities {
		if identity.Version == version {
			return updatecheck.Release{Version: version, Prerelease: true, Immutable: true, Assets: []updatecheck.Asset{{Name: "release-manifest.sigstore.json"}}}, nil
		}
	}
	return updatecheck.Release{}, errors.New("unavailable historical release")
}
func (f *automaticDiscoveryFixture) PrepareNightlySource(_ context.Context, status updatecheck.Status, expected releaseidentity.Identity) (selfupdate.PreparedNightly, error) {
	f.fetched = append(f.fetched, expected)
	if f.forbidden[expected] {
		return selfupdate.PreparedNightly{}, errors.New("unrelated historical payload unavailable")
	}
	if status.LatestVersion != expected.Version {
		return selfupdate.PreparedNightly{}, errors.New("wrong exact release requested")
	}
	prepared, ok := f.prepared[expected]
	if !ok {
		return prepared, errors.New("unknown source")
	}
	return prepared, nil
}
func (f *automaticDiscoveryFixture) AssembleNightlyRoute(ctx context.Context, target selfupdate.PreparedNightly, sources []selfupdate.PreparedNightly) (string, error) {
	f.assembled++
	options := f.options
	options.OutputDirectory = filepath.Join(f.t.TempDir(), "bundle")
	options.Nightlies = []string{target.Directory}
	for _, source := range sources {
		options.Nightlies = append(options.Nightlies, source.Directory)
	}
	return options.OutputDirectory, upgradeassembly.Assemble(ctx, options)
}

func newAutomaticDiscoveryFixture(t *testing.T) *automaticDiscoveryFixture {
	t.Helper()
	return newAutomaticDiscoveryGraph(t, [][]int{{}, {0}, {1}})
}

func newAutomaticDiscoveryGraph(t *testing.T, predecessors [][]int) *automaticDiscoveryFixture {
	t.Helper()
	f := &automaticDiscoveryFixture{t: t, root: t.TempDir(), prepared: map[releaseidentity.Identity]selfupdate.PreparedNightly{}, forbidden: map[releaseidentity.Identity]bool{}}
	migrations := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{{Version: 1, SHA256: releaseidentity.Hash([]byte("actual SQL"))}}}
	schema := releaseidentity.Hash([]byte("actual catalog"))
	f.source = upgradecompat.InstalledSource{Identity: releaseidentity.Source{Genesis: releaseidentity.LegacyGenesisV1}, Migrations: migrations, SchemaSHA256: schema}
	materials := []releasetrust.NightlyMaterial{}
	for index := range len(predecessors) {
		declaration := upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.NightlyV1, Version: fmt.Sprintf("1.1.0-nightly.%d", index+1), Sequence: uint64(index + 2), Commit: strings.Repeat("a", 40), Engines: []upgradecompat.EngineDeclaration{{Migrations: migrations, SchemaSHA256: schema, Sources: []upgradecompat.SourceEdge{}}}}
		for _, predecessor := range predecessors[index] {
			declaration.Engines[0].Sources = append(declaration.Engines[0].Sources, upgradecompat.SourceEdge{Source: releaseidentity.Source{Release: f.identities[predecessor]}, Migrations: migrations, SchemaSHA256: schema, Mode: upgradecompat.Maintenance})
		}
		claim := trustfixture.JSON(t, declaration)
		var material releasetrust.NightlyMaterial
		if index == 0 {
			f.trust, material, _ = trustfixture.Nightly(t, claim, false)
		} else {
			material = f.trust.SignNightly(claim, declaration.Version, declaration.Sequence)
		}
		materials = append(materials, material)
		f.identities = append(f.identities, releaseidentity.Identity{Profile: declaration.Profile, Version: declaration.Version, Sequence: declaration.Sequence, Commit: declaration.Commit, CompatibilitySHA256: releaseidentity.Hash(claim), ManifestSHA256: releaseidentity.Hash(material.Manifest)})
	}
	bridge := f.trust.AddBridge(t, releasetrust.BridgeStatement{Schema: "hikyo.dev/legacy-nightly-bridge/v1", SourceGenesis: releaseidentity.LegacyGenesisV1, Target: f.identities[0], TargetPolicySHA256: releaseidentity.Hash(materials[0].Policy), SourceMigrations: migrations, TargetMigrations: migrations, SourceSchemaSHA256: schema, TargetSchemaSHA256: schema, Mode: "maintenance"})
	write := func(path string, raw []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := f.trust.Material(t)
	for name, raw := range map[string][]byte{"metadata.json": snapshot.Metadata, "metadata.sigstore.json": snapshot.MetadataSignature, "catalog.json": snapshot.Catalog, "catalog.sigstore.json": snapshot.CatalogSignature} {
		write(filepath.Join(f.root, "snapshot", name), raw)
	}
	write(filepath.Join(f.root, "keys", "primary.pub"), f.trust.PrimaryPublic)
	write(filepath.Join(f.root, "bridge", "statement.json"), bridge.Statement)
	write(filepath.Join(f.root, "bridge", "statement.sigstore.json"), bridge.Signature)
	f.options = upgradeassembly.Options{Pinned: f.trust.Pinned, SnapshotDirectory: filepath.Join(f.root, "snapshot"), KeysDirectory: filepath.Join(f.root, "keys"), Bridges: []string{filepath.Join(f.root, "bridge")}}
	for index, material := range materials {
		directory := filepath.Join(f.root, fmt.Sprintf("flat-%d", index))
		for name, reader := range material.Artifacts {
			raw, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(directory, name), raw)
		}
		write(filepath.Join(directory, "release-manifest.json"), material.Manifest)
		write(filepath.Join(directory, "release-manifest.sigstore.json"), material.Bundle)
		options := f.options
		options.Nightlies = []string{directory}
		options.OutputDirectory = filepath.Join(f.root, fmt.Sprintf("bundle-%d", index))
		if err := upgradeassembly.Assemble(t.Context(), options); err != nil {
			t.Fatal(err)
		}
		identity := f.identities[index]
		f.prepared[identity] = selfupdate.PreparedNightly{Identity: identity, Directory: directory, BundleDirectory: options.OutputDirectory}
	}
	return f
}

func (f *automaticDiscoveryFixture) inspection(index int) *automaticTestInspection {
	source := f.source.Identity
	absent := index < 0
	if !absent {
		source = releaseidentity.Source{Release: f.identities[index]}
	}
	digest, _ := f.source.Migrations.Digest()
	return &automaticTestInspection{absent: absent, state: upgrade.State{Applied: source, Floor: f.trust.Snapshot(f.t).Floor()}, installed: upgrade.InstalledSource{Source: source, MigrationDigest: digest, SchemaDigest: f.source.SchemaSHA256, InstanceID: "actual-installation"}}
}

func TestAutomaticDiscoveryNoopDoesNotFetchHistoricalPredecessors(t *testing.T) {
	f := newAutomaticDiscoveryFixture(t)
	for _, identity := range f.identities[:2] {
		f.forbidden[identity] = true
	}
	result, err := discoverAutomaticRoute(t.Context(), f, f, f.prepared[f.identities[2]], f.trust.Pinned, f.inspection(2), releaseidentity.SQLite, nil)
	if err != nil || len(result.Plan.Steps()) != 0 || len(f.fetched) != 0 || f.assembled != 0 {
		t.Fatalf("same-release restart fetched historical artifacts: %v fetched=%v assemblies=%d", err, f.fetched, f.assembled)
	}
}

func TestAutomaticDiscoveryRecentUpgradeDoesNotFetchAncientHistory(t *testing.T) {
	f := newAutomaticDiscoveryFixture(t)
	f.forbidden[f.identities[0]] = true
	result, err := discoverAutomaticRoute(t.Context(), f, f, f.prepared[f.identities[2]], f.trust.Pinned, f.inspection(1), releaseidentity.SQLite, nil)
	if err != nil || len(result.Plan.Steps()) != 1 || !slices.Equal(f.fetched, []releaseidentity.Identity{f.identities[1]}) || f.assembled != 1 {
		t.Fatalf("direct upgrade fetched unrelated predecessor: %v fetched=%v assemblies=%d", err, f.fetched, f.assembled)
	}
}

func TestAutomaticDiscoveryLegacyAndInterruptedRouteKeepExactBridge(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprintf("resume=%t", resume), func(t *testing.T) {
			f := newAutomaticDiscoveryFixture(t)
			target := f.prepared[f.identities[1]]
			inspection := f.inspection(-1)
			var previous *automaticJournal
			if resume {
				full, err := f.AssembleNightlyRoute(t.Context(), target, []selfupdate.PreparedNightly{f.prepared[f.identities[0]]})
				if err != nil {
					t.Fatal(err)
				}
				bundle, err := upgradebundle.Load(t.Context(), full, f.trust.Pinned, releaseidentity.SnapshotFloor{})
				if err != nil {
					t.Fatal(err)
				}
				plan, err := bundle.Plan(f.source, target.Identity)
				if err != nil {
					t.Fatal(err)
				}
				previous = &automaticJournal{Phase: "schema-applied", Target: target.Identity, Source: f.source, Instance: "actual-installation", Route: plan.Digest(), Hop: 1}
				inspection = f.inspection(0)
				// Original-route recovery must not mistake the intermediate database for
				// a new independent upgrade and invalidate the accepted backup route.
				inspection.installed.InstanceID = "must-not-inspect-new-source"
				f.assembled = 0
			}
			result, err := discoverAutomaticRoute(t.Context(), f, f, target, f.trust.Pinned, inspection, releaseidentity.SQLite, previous)
			if err != nil || len(result.Plan.Steps()) != 2 || len(result.Plan.BridgeDigests()) != 1 || !slices.Equal(f.fetched, []releaseidentity.Identity{f.identities[0]}) {
				t.Fatalf("legacy bridge route unavailable: %v fetched=%v", err, f.fetched)
			}
			if resume && (result.Plan.Digest() != previous.Route || result.Instance != previous.Instance || inspection.installedCalls != 0) {
				t.Fatal("resume replanned from intermediate source")
			}
		})
	}
}

func TestAutomaticDiscoveryRejectsSnapshotBelowInstalledFloorBeforeFetching(t *testing.T) {
	f := newAutomaticDiscoveryFixture(t)
	inspection := f.inspection(1)
	inspection.state.Floor.CatalogSequence++
	if _, err := discoverAutomaticRoute(t.Context(), f, f, f.prepared[f.identities[2]], f.trust.Pinned, inspection, releaseidentity.SQLite, nil); err == nil || len(f.fetched) != 0 {
		t.Fatal("stale trust evidence caused historical downloads")
	}
}

func TestAutomaticDiscoveryResolvesShallowAlternativesBeforeLongerHistory(t *testing.T) {
	// The newer predecessor offers a three-hop route, while the older branch
	// offers two. Stop after authenticating the shorter branch and never fetch
	// the unnecessary interior node of the longer branch.
	f := newAutomaticDiscoveryGraph(t, [][]int{{}, {0}, {0}, {2}, {3, 1}})
	f.forbidden[f.identities[2]] = true
	result, err := discoverAutomaticRoute(t.Context(), f, f, f.prepared[f.identities[4]], f.trust.Pinned, f.inspection(0), releaseidentity.SQLite, nil)
	if err != nil || len(result.Plan.Steps()) != 2 || result.Plan.Steps()[0].Target != f.identities[1] {
		t.Fatalf("shortest authenticated branch was not selected: %v fetched=%v", err, f.fetched)
	}
	if slices.Contains(f.fetched, f.identities[2]) {
		t.Fatal("downloaded unnecessary longer-route history")
	}
}

func TestAutomaticDiscoveryPreservesDeterministicRouteTieBreak(t *testing.T) {
	f := newAutomaticDiscoveryGraph(t, [][]int{{}, {}, {0}, {0}, {3, 2}})
	f.forbidden[f.identities[1]] = true
	result, err := discoverAutomaticRoute(t.Context(), f, f, f.prepared[f.identities[4]], f.trust.Pinned, f.inspection(0), releaseidentity.SQLite, nil)
	if err != nil || len(result.Plan.Steps()) != 2 || result.Plan.Steps()[0].Target != f.identities[2] {
		t.Fatalf("partial graph changed deterministic tie break: %v fetched=%v", err, f.fetched)
	}
	if !slices.Contains(f.fetched, f.identities[2]) || !slices.Contains(f.fetched, f.identities[3]) {
		t.Fatal("did not authenticate both relevant shallow branches")
	}
}
