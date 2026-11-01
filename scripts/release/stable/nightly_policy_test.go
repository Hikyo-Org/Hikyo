package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

type nightlyRotationFixture struct {
	trust    string
	fixture  *testfixture.Fixture
	material releasetrust.SnapshotMaterial
	release  releasetrust.StableMaterial
	nightly  releasetrust.NightlyPolicy
}

func newNightlyRotationFixture(t *testing.T, delegated bool, mutate func(*testfixture.Fixture, *releasetrust.StablePolicy)) nightlyRotationFixture {
	t.Helper()
	f, original, historical, sign := testfixture.StableWorkflow(t)
	var stable releasetrust.StablePolicy
	if err := json.Unmarshal(original.StablePolicy, &stable); err != nil {
		t.Fatal(err)
	}
	nightly := stable.NightlyPolicy
	nightly.Schema, nightly.WorkflowPath, nightly.ProtectedRef = "hikyo.dev/nightly-policy/v1", ".github/workflows/nightly.yml", "refs/heads/main"
	nightlyRaw := testfixture.JSON(t, nightly)
	f.Catalog.NightlyPolicies = []releaseidentity.Digest{releaseidentity.Hash(nightlyRaw)}
	f.Catalog.Bridges = []releaseidentity.Digest{releaseidentity.Hash([]byte("existing authorized bridge"))}
	stable.NightlyPolicies, stable.Bridges = slices.Clone(f.Catalog.NightlyPolicies), slices.Clone(f.Catalog.Bridges)
	if !delegated {
		f.Metadata.Event.Type, f.Metadata.Event.SignedBy = "release-candidate", f.Metadata.Recovery.ID
		f.Metadata.SourceCommit, f.Metadata.ReleaseTag = "", ""
		f.Metadata.PendingRelease = &releasetrust.Release{Version: "1.1.0", Sequence: 2, ManifestSHA256: strings.Repeat("d", 64)}
	}
	if mutate != nil {
		mutate(f, &stable)
	}
	material := f.Material(t)
	material.StablePolicy = testfixture.JSON(t, stable)
	material.StablePolicySignature = testfixture.Sign(t, f.RecoverySigner, material.StablePolicy)
	material.StableTrustedRoot = original.StableTrustedRoot
	material.NightlyPolicy = nightlyRaw
	f.Catalog.StableMetadataSHA256 = releaseidentity.Hash(material.Metadata)
	f.Catalog.StablePolicySHA256 = releaseidentity.Hash(material.StablePolicy)
	material.Catalog = testfixture.JSON(t, f.Catalog)
	if delegated {
		material.MetadataSignature = sign(material.Metadata, f.Metadata.SourceCommit, f.Metadata.ReleaseTag)
		material.CatalogSignature = sign(material.Catalog, f.Metadata.SourceCommit, f.Metadata.ReleaseTag)
	} else {
		material.CatalogSignature = testfixture.Sign(t, f.RecoverySigner, material.Catalog)
	}
	trust := t.TempDir()
	writeNightlyRotationTrust(t, trust, f, material, original.StableTrustedRoot)
	if _, err := loadTrust(trust); err != nil {
		t.Fatal(err)
	}
	return nightlyRotationFixture{trust: trust, fixture: f, material: material, release: historical, nightly: nightly}
}

func writeNightlyRotationTrust(t *testing.T, trust string, fixture *testfixture.Fixture, material releasetrust.SnapshotMaterial, nightlyRoot []byte) {
	t.Helper()
	var root releasetrust.Root
	if err := json.Unmarshal(fixture.Pinned.Root, &root); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"root.json": fixture.Pinned.Root, root.Recovery.PublicKey: fixture.Pinned.RecoveryPublicKey,
		root.BootstrapPrimary.PublicKey: fixture.PrimaryPublic,
		"metadata.json":                 material.Metadata, "metadata.sigstore.json": material.MetadataSignature,
		"catalog.json": material.Catalog, "catalog.sigstore.json": material.CatalogSignature,
		"stable-policy.json": material.StablePolicy, "stable-policy.sigstore.json": material.StablePolicySignature,
		"stable-trusted-root.json": material.StableTrustedRoot,
		"nightly/policy.json":      material.NightlyPolicy, "nightly/trusted-root.json": nightlyRoot,
	}
	for name, raw := range files {
		if len(raw) == 0 {
			continue
		}
		path := filepath.Join(trust, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func readRotationProposal(t *testing.T, directory, name string, value any) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		if err := json.Unmarshal(raw, value); err != nil {
			t.Fatal(err)
		}
	}
	return raw
}

func TestNightlyPolicyProposalConservesAuthorityAndNeedsRecoverySignatures(t *testing.T) {
	for _, delegated := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovery current metadata", true: "delegated current metadata"}[delegated], func(t *testing.T) {
			fixture := newNightlyRotationFixture(t, delegated, nil)
			current, err := loadTrust(fixture.trust)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "proposal")
			if err := prepareNightlyPolicyRotation(current, fixture.trust, "", target); err != nil {
				t.Fatal(err)
			}
			var metadata releasetrust.Metadata
			metadataRaw := readRotationProposal(t, target, "metadata.json", &metadata)
			var catalog releasetrust.Catalog
			catalogRaw := readRotationProposal(t, target, "catalog.json", &catalog)
			var stable releasetrust.StablePolicy
			stableRaw := readRotationProposal(t, target, "stable-policy.json", &stable)
			var nightly releasetrust.NightlyPolicy
			nightlyRaw := readRotationProposal(t, target, "nightly-policy.json", &nightly)
			if nightly.Schema != "hikyo.dev/nightly-policy/v2" || nightly.SourceRef != "refs/heads/dev" || nightly.SourceWorkflowPath != ".github/workflows/ci-dev.yml" {
				t.Fatal("proposal did not select exact dev policy")
			}
			identity := nightly
			identity.Schema, identity.SourceRef, identity.SourceWorkflowPath = fixture.nightly.Schema, "", ""
			if !reflect.DeepEqual(identity, fixture.nightly) {
				t.Fatal("nightly identity or revocation inputs changed")
			}
			if metadata.Sequence != current.metadata.Sequence+1 || metadata.Event.Type != "rotation" || metadata.Event.SignedBy != current.metadata.Recovery.ID || metadata.SourceCommit != "" || metadata.ReleaseTag != "" {
				t.Fatal("proposal did not use the existing recovery rotation event")
			}
			conserved := metadata
			conserved.Sequence, conserved.Event, conserved.SourceCommit, conserved.ReleaseTag = current.metadata.Sequence, current.metadata.Event, current.metadata.SourceCommit, current.metadata.ReleaseTag
			if !reflect.DeepEqual(conserved, current.metadata) {
				t.Fatal("release, pending, primary or history metadata changed")
			}
			previousPolicy, _ := current.snapshot.StablePolicy()
			conservedPolicy := stable
			conservedPolicy.NightlyPolicies = previousPolicy.NightlyPolicies
			if !reflect.DeepEqual(conservedPolicy, previousPolicy) {
				t.Fatal("unrelated stable authority changed")
			}
			wantPolicies := append(slices.Clone(current.catalog.NightlyPolicies), releaseidentity.Hash(nightlyRaw))
			if !slices.Equal(stable.NightlyPolicies, wantPolicies) || !slices.Equal(catalog.NightlyPolicies, wantPolicies) || catalog.Sequence != current.catalog.Sequence+1 || catalog.StablePolicySHA256 != releaseidentity.Hash(stableRaw) || catalog.StableMetadataSHA256 != releaseidentity.Hash(metadataRaw) || !slices.Equal(catalog.Bridges, current.catalog.Bridges) {
				t.Fatal("catalog/policy authorization or binding not conserved")
			}
			var summary struct {
				Status               string                            `json:"status"`
				RequiredSignatures   []string                          `json:"required_signatures"`
				SourceSnapshotSHA256 releaseidentity.Digest            `json:"source_snapshot_sha256"`
				Proposed             map[string]releaseidentity.Digest `json:"proposed"`
			}
			readRotationProposal(t, target, "proposal.json", &summary)
			if summary.Status != "UNSIGNED" || len(summary.RequiredSignatures) != 3 || summary.SourceSnapshotSHA256 != current.snapshot.Digest() {
				t.Fatal("unsigned proposal summary lost its authority or source binding")
			}
			for _, name := range []string{"nightly-policy.json", "stable-policy.json", "metadata.json", "catalog.json"} {
				if summary.Proposed[name] != releaseidentity.Hash(readRotationProposal(t, target, name, nil)) {
					t.Fatal("proposal digest summary mismatch")
				}
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 5 {
				t.Fatal("proposal contains unexpected output or signatures")
			}
			material := fixture.material
			material.Metadata, material.Catalog, material.StablePolicy, material.NightlyPolicy = metadataRaw, catalogRaw, stableRaw, nightlyRaw
			if _, err := releasetrust.VerifySnapshot(current.pinned, material, current.snapshot.Floor()); err == nil {
				t.Fatal("unsigned changed authority was accepted")
			}
			material.MetadataSignature = testfixture.Sign(t, fixture.fixture.RecoverySigner, metadataRaw)
			material.CatalogSignature = testfixture.Sign(t, fixture.fixture.RecoverySigner, catalogRaw)
			material.StablePolicySignature = testfixture.Sign(t, fixture.fixture.RecoverySigner, stableRaw)
			rotated, err := releasetrust.VerifySnapshot(current.pinned, material, current.snapshot.Floor())
			if err != nil {
				t.Fatalf("existing recovery ceremony cannot authorize this proposal: %v", err)
			}
			if rotated.Floor().HighestReleaseSequence != current.snapshot.Floor().HighestReleaseSequence {
				t.Fatal("release rollback floor changed")
			}
			if _, err := releasetrust.VerifyStable(rotated, fixture.release); err != nil {
				t.Fatalf("rotation lost authorized historical release: %v", err)
			}
			after, err := loadTrust(fixture.trust)
			if err != nil || after.snapshot.Digest() != current.snapshot.Digest() {
				t.Fatal("proposal changed the production trust directory")
			}
			if err := prepareNightlyPolicyRotation(current, fixture.trust, "", target); err == nil {
				t.Fatal("existing proposal was overwritten")
			}
		})
	}
}

func TestNightlyPolicyProposalRejectsUnsupportedInputs(t *testing.T) {
	fixture := newNightlyRotationFixture(t, true, nil)
	current, err := loadTrust(fixture.trust)
	if err != nil {
		t.Fatal(err)
	}
	next := fixture.nightly
	next.Schema, next.SourceRef, next.SourceWorkflowPath = "hikyo.dev/nightly-policy/v2", "refs/heads/dev", ".github/workflows/ci-dev.yml"
	for name, mutate := range map[string]func(*releasetrust.NightlyPolicy){
		"old schema": func(p *releasetrust.NightlyPolicy) {
			p.Schema = "hikyo.dev/nightly-policy/v1"
			p.SourceRef = ""
			p.SourceWorkflowPath = ""
		},
		"wrong source":       func(p *releasetrust.NightlyPolicy) { p.SourceRef = "refs/heads/main" },
		"wrong workflow":     func(p *releasetrust.NightlyPolicy) { p.SourceWorkflowPath = ".github/workflows/ci-feedback.yml" },
		"changed issuer":     func(p *releasetrust.NightlyPolicy) { p.Issuer = "https://attacker.invalid" },
		"changed repository": func(p *releasetrust.NightlyPolicy) { p.RepositoryID = "124" },
		"changed root": func(p *releasetrust.NightlyPolicy) {
			p.TrustedRootSHA256 = releaseidentity.Hash([]byte("different root"))
		},
		"changed runner":    func(p *releasetrust.NightlyPolicy) { p.RunnerEnvironment = "self-hosted" },
		"changed publisher": func(p *releasetrust.NightlyPolicy) { p.WorkflowPath = ".github/workflows/other.yml" },
		"changed main ref":  func(p *releasetrust.NightlyPolicy) { p.ProtectedRef = "refs/heads/dev" },
		"disabled SCT":      func(p *releasetrust.NightlyPolicy) { disabled := false; p.RequireSCT = &disabled },
		"changed revocation": func(p *releasetrust.NightlyPolicy) {
			p.RevokedManifests = []releaseidentity.Digest{releaseidentity.Hash([]byte("new revocation"))}
		},
	} {
		t.Run(name, func(t *testing.T) {
			policy := next
			mutate(&policy)
			path := filepath.Join(t.TempDir(), "policy.json")
			if err := os.WriteFile(path, testfixture.JSON(t, policy), 0600); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "proposal")
			if err := prepareNightlyPolicyRotation(current, fixture.trust, path, out); err == nil {
				t.Fatal("unsupported policy change accepted")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("refused proposal left an output directory")
			}
		})
	}
	for _, mutation := range []string{"unverified cache", "changed cached bytes", "changed cached metadata", "output in trust", "symlink output in trust", "symlink candidate", "unknown candidate field", "duplicate candidate field", "missing output"} {
		t.Run(mutation, func(t *testing.T) {
			cached := current
			path := filepath.Join(t.TempDir(), "policy.json")
			if err := os.WriteFile(path, testfixture.JSON(t, next), 0600); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "proposal")
			switch mutation {
			case "unverified cache":
				cached = publicTrust{}
			case "changed cached bytes":
				cached.material.Metadata = append(bytes.Clone(cached.material.Metadata), ' ')
			case "changed cached metadata":
				cached.metadata.Sequence++
			case "output in trust":
				out = filepath.Join(fixture.trust, "proposal")
			case "symlink output in trust":
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(fixture.trust, alias); err != nil {
					t.Fatal(err)
				}
				out = filepath.Join(alias, "proposal")
			case "symlink candidate":
				alias := filepath.Join(t.TempDir(), "alias.json")
				if err := os.Symlink(path, alias); err != nil {
					t.Fatal(err)
				}
				path = alias
			case "unknown candidate field":
				raw := testfixture.JSON(t, next)
				if err := os.WriteFile(path, append([]byte(`{"unreviewed":true,`), raw[1:]...), 0600); err != nil {
					t.Fatal(err)
				}
			case "duplicate candidate field":
				raw := testfixture.JSON(t, next)
				if err := os.WriteFile(path, append([]byte(`{"schema":"hikyo.dev/nightly-policy/v2",`), raw[1:]...), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing output":
				out = ""
			}
			if err := prepareNightlyPolicyRotation(cached, fixture.trust, path, out); err == nil {
				t.Fatal("unsafe input accepted")
			}
			if out != "" {
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatal("refused proposal left an output directory")
				}
			}
		})
	}
}

func TestNightlyPolicyProposalRefusesExhaustionAndInconsistentAuthority(t *testing.T) {
	for _, name := range []string{"metadata sequence", "catalog sequence", "catalog policy mismatch"} {
		t.Run(name, func(t *testing.T) {
			fixture := newNightlyRotationFixture(t, false, func(f *testfixture.Fixture, p *releasetrust.StablePolicy) {
				switch name {
				case "metadata sequence":
					f.Metadata.Sequence = 9007199254740991
				case "catalog sequence":
					f.Catalog.Sequence = 9007199254740991
				case "catalog policy mismatch":
					f.Catalog.NightlyPolicies = append(f.Catalog.NightlyPolicies, releaseidentity.Hash([]byte("other authorized policy")))
				}
			})
			current, err := loadTrust(fixture.trust)
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "proposal")
			if err := prepareNightlyPolicyRotation(current, fixture.trust, "", out); err == nil {
				t.Fatal("exhausted or inconsistent policy authority accepted")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("refused proposal left an output directory")
			}
		})
	}
}
