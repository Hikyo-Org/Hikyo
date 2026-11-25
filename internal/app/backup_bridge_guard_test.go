package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	trustfixture "github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// bridgedBundle writes an offline bundle whose only route to the target is a
// recovery bridge after one ordinary hop from the installed legacy source.
func bridgedBundle(t *testing.T, source upgradecompat.InstalledSource, notAfter time.Time) (directory string, pinned releasetrust.PinnedTrust, target releaseidentity.Identity, bridge releaseidentity.Digest) {
	t.Helper()
	declare := func(version string, sequence uint64, commit string, sources []upgradecompat.SourceEdge) []byte {
		return trustfixture.JSON(t, upgradecompat.Declaration{Schema: upgradecompat.Schema, Profile: releaseidentity.StableV1, Version: version, Sequence: sequence, Commit: commit,
			Engines: []upgradecompat.EngineDeclaration{{Migrations: source.Migrations.Clone(), SchemaSHA256: source.SchemaSHA256, Sources: sources}}})
	}
	f := trustfixture.New(t)
	a := f.AddStable(t, "1.0.0", 1, strings.Repeat("a", 40), declare("1.0.0", 1, strings.Repeat("a", 40), []upgradecompat.SourceEdge{{Source: source.Identity, Migrations: source.Migrations.Clone(), SchemaSHA256: source.SchemaSHA256, Mode: upgradecompat.Maintenance}}))
	b := f.AddStable(t, "1.1.0", 2, strings.Repeat("b", 40), declare("1.1.0", 2, strings.Repeat("b", 40), []upgradecompat.SourceEdge{}))
	snapshot := f.Snapshot(t)
	verifiedA, err := releasetrust.VerifyStable(snapshot, a.Material)
	if err != nil {
		t.Fatal(err)
	}
	verifiedB, err := releasetrust.VerifyStable(snapshot, b.Material)
	if err != nil {
		t.Fatal(err)
	}
	raw := trustfixture.JSON(t, releasetrust.BridgeStatement{Schema: releasetrust.BridgeSchemaV2, NotAfter: notAfter.UTC().Truncate(time.Second).Format(time.RFC3339),
		Source: a.Identity, Target: b.Identity, SourcePolicySHA256: verifiedA.PolicyDigest(), TargetPolicySHA256: verifiedB.PolicyDigest(),
		SourceMigrations: source.Migrations.Clone(), TargetMigrations: source.Migrations.Clone(), SourceSchemaSHA256: source.SchemaSHA256, TargetSchemaSHA256: source.SchemaSHA256, Mode: "maintenance"})
	bridge = releaseidentity.Hash(raw)
	f.Catalog.Bridges, f.Catalog.Sequence = append(f.Catalog.Bridges, bridge), f.Catalog.Sequence+1
	material := f.Material(t)
	index := upgradebundle.Index{Format: upgradebundle.IndexFormat, PrimaryKeyIDs: []string{"test-primary"}, Releases: []upgradebundle.ReleaseEntry{}, Bridges: []releaseidentity.Digest{bridge}}
	directory = t.TempDir()
	write := func(name string, content []byte) {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, release := range []trustfixture.SignedRelease{a, b} {
		index.Releases = append(index.Releases, upgradebundle.ReleaseEntry{Profile: releaseidentity.StableV1, ManifestSHA256: release.Identity.ManifestSHA256})
		root := "releases/" + string(release.Identity.ManifestSHA256) + "/"
		for name, content := range map[string][]byte{"manifest.json": release.Material.Manifest, "manifest.sigstore.json": release.Material.ManifestSignature, "release-candidate.json": release.Material.Candidate, "upgrade-compatibility.json": release.Material.Compatibility} {
			write(root+name, content)
		}
		for name, content := range release.Payloads {
			write(root+name, content)
		}
	}
	write("bridges/"+string(bridge)+"/statement.json", raw)
	write("bridges/"+string(bridge)+"/statement.sigstore.json", trustfixture.Sign(t, f.RecoverySigner, raw))
	for name, content := range map[string][]byte{"index.json": trustfixture.JSON(t, index), "keys/test-primary.pub": f.PrimaryPublic, "metadata.json": material.Metadata, "metadata.sigstore.json": material.MetadataSignature, "catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature} {
		write(name, content)
	}
	// The recovery-signed list that revokes the bridge, stored where an installation keeps its last verified list.
	list := trustfixture.JSON(t, releasetrust.RevocationList{Schema: releasetrust.RevocationSchema, Sequence: 1, IssuedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339), TrustRootSHA256: releaseidentity.Hash(f.Pinned.Root), Revoked: []releaseidentity.Digest{bridge}})
	write("revocations/bridge-revocations.json", list)
	write("revocations/bridge-revocations.sigstore.json", trustfixture.Sign(t, f.RecoverySigner, list))
	return directory, f.Pinned, b.Identity, bridge
}

// A revoked bridge must stop the export and the drill before anything is
// written, so no operator attestation can be minted for it. Mutating the
// guardBridges call in either command makes this test fail.
func TestUpgradeExportAndDrillRefuseARevokedBridge(t *testing.T) {
	f := newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
	migrations, err := upgrade.PinnedLegacyManifest(releaseidentity.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	directory, pinned, target, _ := bridgedBundle(t, upgradecompat.InstalledSource{Identity: f.source.Source, Migrations: migrations, SchemaSHA256: f.source.SchemaDigest}, time.Now().Add(24*time.Hour))
	_, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	trust := TrustContext{BundleDirectory: directory, Pinned: pinned, Target: target, OperatorPin: f.request.Operator}
	configure := func(state string) *config.Config {
		cfg := &config.Config{Store: config.Datastore{Engine: config.EngineSQLite, Path: f.cfg.Path}, BackupRecipients: []string{recipient}}
		cfg.Upgrade.RevocationCheck, cfg.Upgrade.StateDirectory = "off", state
		return cfg
	}
	revoked := t.TempDir()
	for _, name := range []string{"bridge-revocations.json", "bridge-revocations.sigstore.json"} {
		raw, err := os.ReadFile(filepath.Join(directory, "revocations", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(revoked, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	clean := t.TempDir()

	custody := t.TempDir()
	write := func(name string, raw []byte) string {
		path := filepath.Join(custody, name)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	receipt := write("receipt.json", f.request.Receipt)
	identity := write("identity.txt", []byte(f.request.Unlock.Identity))
	root := write("root.txt", []byte(crypto.EncodeRootKey(f.root)))
	commands := map[string]func(out string) []string{
		"upgrade-export": func(out string) []string { return []string{"upgrade-export", "--out", out} },
		"upgrade-drill": func(out string) []string {
			return []string{"upgrade-drill", "--from", f.archive, "--receipt", receipt, "--identity-file", identity, "--root-key-file", root,
				"--target-sqlite", filepath.Join(t.TempDir(), "scratch.db"), "--principal", "usr_drill", "--project", "org_drill/prj_drill", "--out", out, "--signing-key", filepath.Join(custody, "absent.key")}
		},
	}
	for name, args := range commands {
		t.Run(name, func(t *testing.T) {
			out := t.TempDir()
			err := RunUpgradeBackup(t.Context(), configure(revoked), append(args(out), "--bundle", directory), new(strings.Builder), trust)
			if err == nil || !strings.Contains(err.Error(), "recovery bridge revoked") {
				t.Fatalf("a revoked bridge must be refused at the guard: %v", err)
			}
			if entries, err := os.ReadDir(out); err != nil || len(entries) != 0 {
				t.Fatalf("a refused command wrote %d files: %v", len(entries), err)
			}
			// Control: with nothing revoked the same command gets past the guard.
			err = RunUpgradeBackup(t.Context(), configure(clean), append(args(t.TempDir()), "--bundle", directory), new(strings.Builder), trust)
			if err != nil && strings.Contains(err.Error(), "recovery bridge") {
				t.Fatalf("an unrevoked bridge must pass the guard: %v", err)
			}
		})
	}
}

// The three route derivations differ only in what they refuse. A new admission
// refuses an expired or revoked bridge; resuming an admitted upgrade ignores
// expiry but not revocation; a security rollback ignores both. The resume and
// rollback call sites (the gate, automatic_release.go, security_rollback.go and
// maintenance_configuration.go) use PlanAdmitted and PlanRollback accordingly.
func TestRouteDerivationsForAdmissionResumeAndRollback(t *testing.T) {
	f := newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
	migrations, err := upgrade.PinnedLegacyManifest(releaseidentity.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	source := upgradecompat.InstalledSource{Identity: f.source.Source, Migrations: migrations, SchemaSHA256: f.source.SchemaDigest}
	for _, c := range []struct {
		name                    string
		notAfter                time.Time
		revoked                 bool
		admission, resume, back string // "" accepts, otherwise the refusal
	}{
		{"live", time.Now().Add(24 * time.Hour), false, "", "", ""},
		{"expired", time.Now().Add(-24 * time.Hour), false, "no authenticated upgrade route", "", ""},
		{"revoked", time.Now().Add(24 * time.Hour), true, "recovery bridge revoked", "recovery bridge revoked", ""},
		{"expired and revoked", time.Now().Add(-24 * time.Hour), true, "no authenticated upgrade route", "recovery bridge revoked", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			directory, pinned, target, bridge := bridgedBundle(t, source, c.notAfter)
			bundle, err := upgradebundle.Load(t.Context(), directory, pinned, releaseidentity.SnapshotFloor{})
			if err != nil {
				t.Fatal(err)
			}
			bundle = bundle.WithBridgeRevocations(func() ([]releaseidentity.Digest, error) {
				if c.revoked {
					return []releaseidentity.Digest{bridge}, nil
				}
				return nil, nil
			})
			check := func(name string, plan upgradecompat.Plan, err error, want string) releaseidentity.Digest {
				t.Helper()
				if want != "" {
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("%s: want refusal %q, got %v", name, want, err)
					}
					return ""
				}
				if err != nil || len(plan.BridgeDigests()) != 1 || plan.BridgeDigests()[0] != bridge {
					t.Fatalf("%s: want the bridge route, got %v", name, err)
				}
				return plan.Digest()
			}
			plan, err := bundle.Plan(source, target)
			admission := check("admission", plan, err, c.admission)
			plan, err = bundle.PlanAdmitted(source, target)
			resume := check("resume", plan, err, c.resume)
			plan, err = bundle.PlanRollback(source, target)
			back := check("rollback", plan, err, c.back)
			if back == "" || (admission != "" && admission != back) || (resume != "" && resume != back) {
				t.Fatalf("every accepted derivation must name the same route: %s %s %s", admission, resume, back)
			}
		})
	}
}
