package recoverybridge_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/recoverybridge"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradeassembly"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// evidenceEnv names a directory holding rel100/ and rel110/, the public proof
// files of the real published v1.0.0 and v1.1.0 (release-manifest.json and its
// signature, release-candidate.json, upgrade-compatibility.json, build-provenance
// and its signature). The real releases are authenticated by their published
// Sigstore signatures against the committed production stable policy bytes, not
// re-signed; only the recovery layer is a throwaway key and root.
const evidenceEnv = "HIKYO_BRIDGE_EVIDENCE"

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func write(t *testing.T, directory, name string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(directory, name)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readAll(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// throwawayTrust is the production stable policy and Sigstore root re-rooted under
// a throwaway recovery key, with the real releases authorized in its metadata.
type throwawayTrust struct {
	f                 *testfixture.Fixture
	policy, trustRoot []byte
	evidence          string
}

func newThrowaway(t *testing.T) *throwawayTrust {
	t.Helper()
	evidence := os.Getenv(evidenceEnv)
	if evidence == "" {
		t.Skip(evidenceEnv + " not set: needs the downloaded public proofs of the real v1.0.0 and v1.1.0")
	}
	f := testfixture.New(t)
	f.Metadata.Sequence, f.Catalog.Sequence = 4, 6
	for _, r := range []struct {
		directory, version string
		sequence           int64
	}{{"rel100", "1.0.0", 1}, {"rel110", "1.1.0", 2}} {
		f.Metadata.Releases = append(f.Metadata.Releases, releasetrust.Release{Version: r.version, Sequence: r.sequence, ManifestSHA256: string(releaseidentity.Hash(readAll(t, filepath.Join(evidence, r.directory, "release-manifest.json"))))})
	}
	highest, sequence := "1.1.0", int64(2)
	f.Metadata.HighestRelease, f.Metadata.HighestReleaseSequence = &highest, &sequence
	trust := filepath.Join(repoRoot(t), "release", "trust")
	return &throwawayTrust{f: f, policy: readAll(t, filepath.Join(trust, "stable-policy.json")), trustRoot: readAll(t, filepath.Join(trust, "stable-trusted-root.json")), evidence: evidence}
}

// tree writes the snapshot, keys and root the tools consume. catalogBridges is
// the catalog inventory; policy bytes are written exactly as given.
func (w *throwawayTrust) tree(t *testing.T, policy []byte, catalogBridges []releaseidentity.Digest) (root, recovery, snapshot, keys string) {
	t.Helper()
	dir := t.TempDir()
	w.f.Catalog.NightlyPolicies = []releaseidentity.Digest{releaseidentity.Hash([]byte("unused"))}
	// The production catalog also lists the two legacy nightly bridges; they are
	// signed by the real recovery key, which this throwaway root cannot reproduce.
	w.f.Catalog.Bridges = append([]releaseidentity.Digest{}, catalogBridges...)
	w.f.Catalog.StablePolicySHA256 = releaseidentity.Hash(policy)
	material := w.f.Material(t)
	write(t, dir, "snapshot/metadata.json", material.Metadata)
	write(t, dir, "snapshot/metadata.sigstore.json", material.MetadataSignature)
	write(t, dir, "snapshot/catalog.json", material.Catalog)
	write(t, dir, "snapshot/catalog.sigstore.json", material.CatalogSignature)
	write(t, dir, "snapshot/stable-policy.json", policy)
	write(t, dir, "snapshot/stable-policy.sigstore.json", testfixture.Sign(t, w.f.RecoverySigner, policy))
	write(t, dir, "snapshot/stable-trusted-root.json", w.trustRoot)
	write(t, dir, "keys/primary.pub", w.f.PrimaryPublic)
	write(t, dir, "root.json", w.f.Pinned.Root)
	write(t, dir, "recovery.pub", w.f.Pinned.RecoveryPublicKey)
	return filepath.Join(dir, "root.json"), filepath.Join(dir, "recovery.pub"), filepath.Join(dir, "snapshot"), filepath.Join(dir, "keys")
}

// derived is the evidence a ceremony starts from: statements derived from the
// real published releases under the unchanged production stable policy.
func (w *throwawayTrust) derived(t *testing.T) ([]recoverybridge.Statement, []upgradecompat.Declaration, []releasetrust.VerifiedRelease) {
	t.Helper()
	_, _, snapshotDir, keysDir := w.tree(t, w.policy, nil)
	_, releases, declarations, err := upgradeassembly.LoadEvidence(t.Context(), w.f.Pinned, snapshotDir, keysDir, filepath.Join(w.evidence, "rel100"), filepath.Join(w.evidence, "rel110"))
	if err != nil {
		t.Fatalf("the real published releases must verify from the committed production policy and Sigstore root: %v", err)
	}
	if releases[0].PolicyDigest() != releaseidentity.Hash(w.policy) || releases[1].PolicyDigest() != releases[0].PolicyDigest() {
		t.Fatal("both releases must share the stable policy digest")
	}
	now := time.Now().UTC().Truncate(time.Second)
	statements, err := recoverybridge.Derive(recoverybridge.Evidence{Release: releases[0], Declaration: declarations[0]}, recoverybridge.Evidence{Release: releases[1], Declaration: declarations[1]}, now, now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return statements, declarations, releases
}

// bundle assembles the offline runtime bundle exactly as an operator would, with
// the given signer over every statement.
func (w *throwawayTrust) bundle(t *testing.T, policy []byte, statements []recoverybridge.Statement, sign func([]byte) []byte, listed []releaseidentity.Digest) (upgradebundle.Bundle, error) {
	t.Helper()
	root, recovery, snapshotDir, keysDir := w.tree(t, policy, listed)
	var bridges []string
	for _, s := range statements {
		dir := filepath.Join(t.TempDir(), string(s.Digest))
		write(t, dir, "statement.json", s.Bytes)
		write(t, dir, "statement.sigstore.json", sign(s.Bytes))
		bridges = append(bridges, dir)
	}
	pinned := releasetrust.PinnedTrust{Root: readAll(t, root), RecoveryPublicKey: readAll(t, recovery)}
	out := filepath.Join(t.TempDir(), "bundle")
	err := upgradeassembly.Assemble(t.Context(), upgradeassembly.Options{Pinned: pinned, SnapshotDirectory: snapshotDir, KeysDirectory: keysDir, OutputDirectory: out, Releases: []string{filepath.Join(w.evidence, "rel100"), filepath.Join(w.evidence, "rel110")}, Bridges: bridges})
	if err != nil {
		return upgradebundle.Bundle{}, err
	}
	return upgradebundle.Load(t.Context(), out, pinned, releaseidentity.SnapshotFloor{})
}

func installed(declaration upgradecompat.Declaration, release releasetrust.VerifiedRelease, engine releaseidentity.Engine) upgradecompat.InstalledSource {
	for _, e := range declaration.Engines {
		if e.Migrations.Engine == engine {
			return upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: release.Identity()}, Migrations: e.Migrations, SchemaSHA256: e.SchemaSHA256}
		}
	}
	return upgradecompat.InstalledSource{}
}

func digestsOf(statements []recoverybridge.Statement) []releaseidentity.Digest {
	var out []releaseidentity.Digest
	for _, s := range statements {
		out = append(out, s.Digest)
	}
	return out
}

func TestRealReleasesBridgeThroughRecoveryShapedSnapshot(t *testing.T) {
	w := newThrowaway(t)
	statements, declarations, releases := w.derived(t)
	if len(statements) != 2 || statements[0].Engine != releaseidentity.Postgres || statements[1].Engine != releaseidentity.SQLite {
		t.Fatalf("one statement per engine expected, got %d", len(statements))
	}
	recoverySign := func(raw []byte) []byte { return testfixture.Sign(t, w.f.RecoverySigner, raw) }
	// The policy stays byte-identical: only the recovery-signed catalog lists the
	// bridges. Both engines then route v1.0.0 -> v1.1.0 through exactly one bridge.
	bundle, err := w.bundle(t, w.policy, statements, recoverySign, digestsOf(statements))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range statements {
		t.Run(string(s.Engine), func(t *testing.T) {
			plan, err := bundle.Plan(installed(declarations[0], releases[0], s.Engine), releases[1].Identity())
			if err != nil {
				t.Fatal(err)
			}
			steps := plan.Steps()
			if len(steps) != 1 || steps[0].Mode != upgradecompat.Maintenance || steps[0].BridgeSHA256 != s.Digest || !plan.RequiresOperatorAttestation() {
				t.Fatalf("expected one attested maintenance bridge step, got %+v", steps)
			}
		})
	}
}

func TestRealReleasesBridgeFailsClosed(t *testing.T) {
	w := newThrowaway(t)
	statements, declarations, releases := w.derived(t)
	recoverySign := func(raw []byte) []byte { return testfixture.Sign(t, w.f.RecoverySigner, raw) }
	listed := digestsOf(statements)

	t.Run("no bridge in the catalog leaves the route missing", func(t *testing.T) {
		bundle, err := w.bundle(t, w.policy, nil, recoverySign, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bundle.Plan(installed(declarations[0], releases[0], releaseidentity.SQLite), releases[1].Identity()); err == nil {
			t.Fatal("v1.0.0 routed to v1.1.0 without a bridge")
		}
	})
	t.Run("a bridge signed by another key is refused at assembly", func(t *testing.T) {
		other := testfixture.New(t)
		if _, err := w.bundle(t, w.policy, statements, func(raw []byte) []byte { return testfixture.Sign(t, other.RecoverySigner, raw) }, listed); err == nil {
			t.Fatal("assembled a bridge signed by a key other than the pinned recovery key")
		}
	})
	t.Run("a statement missing from the catalog is refused", func(t *testing.T) {
		if _, err := w.bundle(t, w.policy, statements, recoverySign, listed[:1]); err == nil {
			t.Fatal("assembled a bundle whose bridges differ from the catalog inventory")
		}
	})
	t.Run("a tampered statement is not in the catalog", func(t *testing.T) {
		tampered := slices.Clone(statements)
		tampered[1].Bytes = []byte(strings.Replace(string(tampered[1].Bytes), `"mode": "maintenance"`, `"mode": "restart"`, 1))
		tampered[1].Digest = releaseidentity.Hash(tampered[1].Bytes)
		if _, err := w.bundle(t, w.policy, tampered, recoverySign, listed); err == nil {
			t.Fatal("assembled a tampered statement")
		}
	})
	t.Run("a source schema that differs from the statement is refused", func(t *testing.T) {
		bundle, err := w.bundle(t, w.policy, statements, recoverySign, listed)
		if err != nil {
			t.Fatal(err)
		}
		source := installed(declarations[0], releases[0], releaseidentity.SQLite)
		source.SchemaSHA256 = releaseidentity.Hash([]byte("a different installed schema"))
		if _, err := bundle.Plan(source, releases[1].Identity()); err == nil {
			t.Fatal("planned a bridge for a database whose schema differs from the signed source")
		}
	})
	t.Run("a policy that lists the bridges invalidates them", func(t *testing.T) {
		// Statements bind the stable policy digest; a policy listing their digests
		// has a different digest, so the same statements can never match it.
		var policy map[string]any
		if err := json.Unmarshal(w.policy, &policy); err != nil {
			t.Fatal(err)
		}
		bridges, _ := policy["bridges"].([]any)
		for _, d := range listed {
			bridges = append(bridges, string(d))
		}
		policy["bridges"] = bridges
		listing, err := json.Marshal(policy)
		if err != nil {
			t.Fatal(err)
		}
		bundle, err := w.bundle(t, listing, statements, recoverySign, listed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bundle.Plan(installed(declarations[0], releases[0], releaseidentity.SQLite), releases[1].Identity()); err == nil || !strings.Contains(err.Error(), "bridge target policy") {
			t.Fatalf("expected a policy binding mismatch, got %v", err)
		}
	})
}
