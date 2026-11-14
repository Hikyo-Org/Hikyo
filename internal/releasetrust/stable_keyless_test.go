package releasetrust_test

import (
	"bytes"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func TestStableWorkflowUsesRecoveryDelegationAndExactTag(t *testing.T) {
	f, material, stable, sign := testfixture.StableWorkflow(t)
	snapshot, err := releasetrust.VerifySnapshot(f.Pinned, material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	release, err := releasetrust.VerifyStable(snapshot, stable)
	if err != nil {
		t.Fatal(err)
	}
	if err := releasetrust.RequireLatestStable(snapshot, release); err != nil {
		t.Fatal(err)
	}
	payload := []byte("streamed artifact")
	signature := sign(payload, release.Identity().Commit, "v1.0.0")
	if err := releasetrust.VerifyStableArtifactSignatureReader(snapshot, release, signature, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	if err := releasetrust.VerifyStableArtifactSignature(snapshot, release, signature, []byte("substituted")); err == nil {
		t.Fatal("accepted substituted artifact")
	}
	for _, tag := range []string{"v1.0.1", "v1.0.0-nightly.1"} {
		stable.ManifestSignature = sign(stable.Manifest, release.Identity().Commit, tag)
		if _, err := releasetrust.VerifyStable(snapshot, stable); err == nil {
			t.Fatalf("accepted tag %s", tag)
		}
	}
}

func TestStableWorkflowDelegationFailsClosed(t *testing.T) {
	for _, name := range []string{"unsigned policy", "substituted root", "wrong metadata tag", "wrong metadata commit", "wrong catalog tag", "catalog bridge escalation", "catalog nightly escalation", "primary escalation", "missing latest", "policy SCT disabled", "policy unsigned modification", "policy substitution with valid recovery signature", "metadata rollback", "catalog equivocation"} {
		t.Run(name, func(t *testing.T) {
			f, m, _, sign := testfixture.StableWorkflow(t)
			floor := releasetrust.SnapshotFloor{}
			switch name {
			case "unsigned policy":
				m.StablePolicySignature = nil
			case "substituted root":
				m.StableTrustedRoot = append(m.StableTrustedRoot, ' ')
			case "wrong metadata tag":
				m.MetadataSignature = sign(m.Metadata, f.Metadata.SourceCommit, "v1.0.1")
			case "wrong metadata commit":
				m.MetadataSignature = sign(m.Metadata, strings.Repeat("b", 40), "v1.0.0")
			case "wrong catalog tag":
				m.CatalogSignature = sign(m.Catalog, f.Metadata.SourceCommit, "v1.0.1")
			case "catalog bridge escalation", "catalog nightly escalation":
				if name == "catalog bridge escalation" {
					f.Catalog.Bridges = append(f.Catalog.Bridges, releaseidentity.Hash([]byte("bridge")))
				} else {
					f.Catalog.NightlyPolicies = append(f.Catalog.NightlyPolicies, releaseidentity.Hash([]byte("nightly")))
				}
				m.Catalog = testfixture.JSON(t, f.Catalog)
				m.CatalogSignature = sign(m.Catalog, f.Metadata.SourceCommit, "v1.0.0")
			case "primary escalation", "missing latest":
				if name == "primary escalation" {
					f.Metadata.PrimaryKeys[0].Revoked = true
				} else {
					f.Metadata.Releases = nil
				}
				m.Metadata = testfixture.JSON(t, f.Metadata)
				m.MetadataSignature = sign(m.Metadata, f.Metadata.SourceCommit, "v1.0.0")
			case "policy SCT disabled", "policy unsigned modification", "policy substitution with valid recovery signature":
				var p releasetrust.StablePolicy
				if err := json.Unmarshal(m.StablePolicy, &p); err != nil {
					t.Fatal(err)
				}
				if name == "policy SCT disabled" {
					*p.RequireSCT = false
				} else {
					p.RepositoryID = "999"
				}
				m.StablePolicy = testfixture.JSON(t, p)
				if name != "policy unsigned modification" {
					m.StablePolicySignature = testfixture.Sign(t, f.RecoverySigner, m.StablePolicy)
				}
			case "metadata rollback":
				floor.MetadataSequence = 3
				floor.MetadataSHA256 = releaseidentity.Hash([]byte("newer"))
			case "catalog equivocation":
				floor.CatalogSequence = 2
				floor.CatalogSHA256 = releaseidentity.Hash([]byte("different"))
			}
			if _, err := releasetrust.VerifySnapshot(f.Pinned, m, floor); err == nil {
				t.Fatal("accepted invalid delegated trust")
			}
		})
	}
}

func TestStableProvenanceRejectsAuthenticatedFalseClaims(t *testing.T) {
	for _, name := range []string{"subject omitted", "subject duplicated", "subject digest", "source commit", "source ref", "builder", "invocation"} {
		t.Run(name, func(t *testing.T) {
			f, m, stable, sign := testfixture.StableWorkflow(t)
			var statement releasetrust.StableProvenance
			if err := json.Unmarshal(stable.Provenance, &statement); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "subject omitted":
				statement.Subject = statement.Subject[1:]
			case "subject duplicated":
				statement.Subject[1] = statement.Subject[0]
			case "subject digest":
				statement.Subject[0].Digest["sha256"] = string(releaseidentity.Hash([]byte("other")))
			case "source commit":
				statement.Predicate.BuildDefinition.ResolvedDependencies[0].Digest["gitCommit"] = strings.Repeat("b", 40)
			case "source ref":
				statement.Predicate.BuildDefinition.ExternalParameters.Ref = "refs/heads/main"
			case "builder":
				statement.Predicate.RunDetails.Builder.ID = "https://attacker.invalid"
			case "invocation":
				statement.Predicate.RunDetails.Metadata.InvocationID = "https://attacker.invalid"
			}
			// Re-sign every changed document so this reaches semantic checks rather
			// than merely rejecting stale hashes or invalid cryptographic signatures.
			stable.Provenance = testfixture.JSON(t, statement)
			stable.ProvenanceSignature = sign(stable.Provenance, f.Metadata.SourceCommit, f.Metadata.ReleaseTag)
			var manifest releasetrust.Manifest
			if err := json.Unmarshal(stable.Manifest, &manifest); err != nil {
				t.Fatal(err)
			}
			for i := range manifest.Artifacts {
				if manifest.Artifacts[i].Kind == "build-provenance" {
					manifest.Artifacts[i].SHA256 = string(releaseidentity.Hash(stable.Provenance))
				}
			}
			stable.Manifest = testfixture.JSON(t, manifest)
			stable.ManifestSignature = sign(stable.Manifest, f.Metadata.SourceCommit, f.Metadata.ReleaseTag)
			f.Metadata.Releases[0].ManifestSHA256 = string(releaseidentity.Hash(stable.Manifest))
			m.Metadata = testfixture.JSON(t, f.Metadata)
			m.MetadataSignature = sign(m.Metadata, f.Metadata.SourceCommit, f.Metadata.ReleaseTag)
			f.Catalog.StableMetadataSHA256 = releaseidentity.Hash(m.Metadata)
			m.Catalog = testfixture.JSON(t, f.Catalog)
			m.CatalogSignature = sign(m.Catalog, f.Metadata.SourceCommit, f.Metadata.ReleaseTag)
			snapshot, err := releasetrust.VerifySnapshot(f.Pinned, m, releasetrust.SnapshotFloor{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := releasetrust.VerifyStable(snapshot, stable); err == nil {
				t.Fatal("accepted false provenance")
			}
		})
	}
}

// Changing nightly or bridge authority (revocation, bridges) after workflow-signed
// metadata needs recovery-shaped trust; the next workflow release then needs a
// recovery-re-signed stable policy carrying the new inventories.
func TestRecoveryReauthorizationOverWorkflowSignedTrust(t *testing.T) {
	extra := releaseidentity.Hash([]byte("revoking nightly policy"))
	commit := strings.Repeat("a", 40)
	// withDelegation keeps the recovery-signed delegation next to new snapshot files.
	withDelegation := func(base, changed releasetrust.SnapshotMaterial) releasetrust.SnapshotMaterial {
		changed.StablePolicy, changed.StablePolicySignature, changed.StableTrustedRoot = base.StablePolicy, base.StablePolicySignature, base.StableTrustedRoot
		return changed
	}
	t.Run("recovery catalog over workflow metadata is refused", func(t *testing.T) {
		f, m, _, _ := testfixture.StableWorkflow(t)
		f.Catalog.NightlyPolicies = append(f.Catalog.NightlyPolicies, extra)
		m.Catalog = testfixture.JSON(t, f.Catalog)
		m.CatalogSignature = testfixture.Sign(t, f.RecoverySigner, m.Catalog)
		if _, err := releasetrust.VerifySnapshot(f.Pinned, m, releasetrust.SnapshotFloor{}); err == nil || !strings.Contains(err.Error(), "stable catalog signature") {
			t.Fatalf("recovery catalog over workflow metadata: %v", err)
		}
	})
	t.Run("recovery metadata keeping delegated identity is refused", func(t *testing.T) {
		f, m, _, _ := testfixture.StableWorkflow(t)
		f.Metadata.Event.SignedBy, f.Metadata.Sequence, f.Catalog.Sequence = "test-recovery", 3, 3
		f.Catalog.NightlyPolicies = append(f.Catalog.NightlyPolicies, extra)
		if _, err := releasetrust.VerifySnapshot(f.Pinned, withDelegation(m, f.Material(t)), releasetrust.SnapshotFloor{}); err == nil || !strings.Contains(err.Error(), "legacy metadata carries delegated identity") {
			t.Fatalf("recovery metadata kept delegated identity: %v", err)
		}
	})
	for _, name := range []string{"without stable policy digest", "with stable policy digest"} {
		t.Run("recovery metadata without delegated identity is accepted "+name, func(t *testing.T) {
			f, m, _, sign := testfixture.StableWorkflow(t)
			base, err := releasetrust.VerifySnapshot(f.Pinned, m, releasetrust.SnapshotFloor{})
			if err != nil {
				t.Fatal(err)
			}
			f.Metadata.Event.SignedBy, f.Metadata.SourceCommit, f.Metadata.ReleaseTag = "test-recovery", "", ""
			f.Metadata.Sequence, f.Catalog.Sequence = 3, 3
			f.Catalog.NightlyPolicies = append(f.Catalog.NightlyPolicies, extra)
			if name == "without stable policy digest" {
				f.Catalog.StablePolicySHA256 = ""
			}
			reauthorized, err := releasetrust.VerifySnapshot(f.Pinned, withDelegation(m, f.Material(t)), base.Floor())
			if err != nil {
				t.Fatal(err)
			}
			// The next workflow release must catch up: the old stable policy
			// refuses the changed inventory, a recovery-re-signed one accepts it.
			f.Metadata.Event.SignedBy, f.Metadata.SourceCommit, f.Metadata.ReleaseTag = releasetrust.StableWorkflowSigner, commit, "v1.0.0"
			f.Metadata.Sequence, f.Catalog.Sequence = 4, 4
			f.Catalog.StablePolicySHA256 = releaseidentity.Hash(m.StablePolicy)
			next := withDelegation(m, f.Material(t))
			next.MetadataSignature, next.CatalogSignature = sign(next.Metadata, commit, "v1.0.0"), sign(next.Catalog, commit, "v1.0.0")
			if _, err := releasetrust.VerifySnapshot(f.Pinned, next, reauthorized.Floor()); err == nil || !strings.Contains(err.Error(), "stable workflow changed nightly or bridge authority") {
				t.Fatalf("old stable policy accepted a changed inventory: %v", err)
			}
			var policy releasetrust.StablePolicy
			if err := json.Unmarshal(m.StablePolicy, &policy); err != nil {
				t.Fatal(err)
			}
			policy.NightlyPolicies = f.Catalog.NightlyPolicies
			m.StablePolicy = testfixture.JSON(t, policy)
			m.StablePolicySignature = testfixture.Sign(t, f.RecoverySigner, m.StablePolicy)
			f.Catalog.StablePolicySHA256 = releaseidentity.Hash(m.StablePolicy)
			next = withDelegation(m, f.Material(t))
			next.MetadataSignature, next.CatalogSignature = sign(next.Metadata, commit, "v1.0.0"), sign(next.Catalog, commit, "v1.0.0")
			if _, err := releasetrust.VerifySnapshot(f.Pinned, next, reauthorized.Floor()); err != nil {
				t.Fatalf("re-authorized stable policy refused the next workflow release: %v", err)
			}
		})
	}
}
