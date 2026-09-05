package releasetrust_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestNightlyOfflineCompleteProofAndExactInventory(t *testing.T) {
	f, m, _ := testfixture.Nightly(t, []byte("signed compatibility fixture"), false)
	release, err := releasetrust.VerifyNightly(f.Snapshot(t), m)
	if err != nil {
		t.Fatal(err)
	}
	if release.Identity().Profile != releaseidentity.NightlyV1 {
		t.Fatal("nightly authorized stable identity")
	}
	for _, name := range []string{"unsigned", "wrong commit", "missing promise", "missing proof", "bad checkpoint", "wrong tree size", "wrong entry index", "missing asset", "extra asset", "substitution", "wrong root", "policy revoked", "wrong issuer", "wrong repository", "wrong owner", "wrong workflow", "wrong ref", "wrong log", "wrong checkpoint origin", "manifest revoked", "required SCT missing", "SCT choice omitted"} {
		t.Run(name, func(t *testing.T) {
			f, m, pb := testfixture.Nightly(t, []byte("signed compatibility fixture"), name == "wrong commit")
			switch name {
			case "unsigned":
				m.Bundle = nil
			case "missing promise":
				pb.VerificationMaterial.TlogEntries[0].InclusionPromise = nil
			case "missing proof":
				pb.VerificationMaterial.TlogEntries[0].InclusionProof = nil
			case "bad checkpoint":
				pb.VerificationMaterial.TlogEntries[0].InclusionProof.Checkpoint.Envelope += "tampered"
			case "wrong tree size":
				pb.VerificationMaterial.TlogEntries[0].InclusionProof.TreeSize++
			case "wrong entry index":
				pb.VerificationMaterial.TlogEntries[0].InclusionProof.LogIndex++
			case "missing asset":
				delete(m.Artifacts, "checksums.txt")
			case "extra asset":
				m.Artifacts["extra-executable"] = strings.NewReader("bad")
			case "substitution":
				m.Artifacts["hikyo_linux_arm64.tar.gz"] = strings.NewReader("substituted")
			case "wrong root":
				m.TrustedRoot = append(m.TrustedRoot, ' ')
			case "policy revoked":
				f.Catalog.NightlyPolicies = []releaseidentity.Digest{}
			case "wrong issuer", "wrong repository", "wrong owner", "wrong workflow", "wrong ref", "wrong log", "wrong checkpoint origin", "manifest revoked", "required SCT missing", "SCT choice omitted":
				var policy releasetrust.NightlyPolicy
				if err := json.Unmarshal(m.Policy, &policy); err != nil {
					t.Fatal(err)
				}
				switch name {
				case "wrong issuer":
					policy.Issuer = "https://attacker.invalid"
				case "wrong repository":
					policy.RepositoryID = "999"
				case "wrong owner":
					policy.RepositoryOwnerID = "999"
				case "wrong workflow":
					policy.WorkflowPath = ".github/workflows/other.yml"
				case "wrong ref":
					policy.ProtectedRef = "refs/heads/unprotected"
				case "wrong log":
					policy.RekorLogID = releaseidentity.Hash([]byte("other log"))
				case "wrong checkpoint origin":
					policy.CheckpointOrigin = "other checkpoint"
				case "manifest revoked":
					policy.RevokedManifests = append(policy.RevokedManifests, releaseidentity.Hash(m.Manifest))
				case "required SCT missing":
					required := true
					policy.RequireSCT = &required
				case "SCT choice omitted":
					policy.RequireSCT = nil
				}
				m.Policy = testfixture.JSON(t, policy)
				f.Catalog.NightlyPolicies = []releaseidentity.Digest{releaseidentity.Hash(m.Policy)}
			}
			if name == "missing promise" || name == "missing proof" || name == "bad checkpoint" || name == "wrong tree size" || name == "wrong entry index" {
				var err error
				m.Bundle, err = protojson.Marshal(pb)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := releasetrust.VerifyNightly(f.Snapshot(t), m); err == nil {
				t.Fatal("invalid nightly evidence accepted")
			}
		})
	}
}
