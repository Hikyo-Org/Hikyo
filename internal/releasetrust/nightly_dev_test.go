package releasetrust_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestDevNightlyAuthenticatesDistinctSourceAndPublisher(t *testing.T) {
	f, material, _ := testfixture.DevNightly(t, []byte("signed compatibility fixture"), testfixture.NightlyOptions{})
	var manifest releasetrust.NightlyManifest
	if err := json.Unmarshal(material.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SourceCommit == manifest.PublisherCommit {
		t.Fatal("fixture must exercise distinct dev source and trusted main publisher")
	}
	release, err := releasetrust.VerifyNightly(f.Snapshot(t), material)
	if err != nil {
		t.Fatal(err)
	}
	if release.Identity().Commit != manifest.SourceCommit || release.Identity().Profile != releaseidentity.NightlyV1 {
		t.Fatal("release identity must remain the built dev source")
	}
	artifacts, err := release.NightlyArtifacts("linux/arm64")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, artifact := range artifacts {
		found = found || artifact.Name == releasetrust.NightlySourceValidationArtifact
	}
	if !found {
		t.Fatal("native downloads must retain the signed source-validation receipt")
	}
}

func TestDevNightlyRefusesSignedInvalidSourceAuthority(t *testing.T) {
	proofMutation := func(mutate func(*releasetrust.NightlySourceValidation)) testfixture.NightlyOptions {
		return testfixture.NightlyOptions{Manifest: func(manifest *releasetrust.NightlyManifest) { mutate(manifest.SourceValidation) }}
	}
	cases := map[string]testfixture.NightlyOptions{
		"publisher certificate differs":  {CertificateCommit: strings.Repeat("b", 40)},
		"dev certificate replaces main":  {CertificateRef: "refs/heads/dev"},
		"missing publisher":              {Manifest: func(m *releasetrust.NightlyManifest) { m.PublisherCommit = "" }},
		"uppercase publisher":            {Manifest: func(m *releasetrust.NightlyManifest) { m.PublisherCommit = strings.Repeat("C", 40) }},
		"unbound source":                 {Manifest: func(m *releasetrust.NightlyManifest) { m.SourceCommit = strings.Repeat("d", 40) }},
		"missing proof":                  {Manifest: func(m *releasetrust.NightlyManifest) { m.SourceValidation = nil }},
		"wrong schema":                   proofMutation(func(p *releasetrust.NightlySourceValidation) { p.Schema = "hikyo.dev/source-validation/v0" }),
		"wrong repository":               proofMutation(func(p *releasetrust.NightlySourceValidation) { p.Repository = "attacker/hikyo" }),
		"wrong repository ID":            proofMutation(func(p *releasetrust.NightlySourceValidation) { p.RepositoryID = 124 }),
		"wrong source":                   proofMutation(func(p *releasetrust.NightlySourceValidation) { p.Commit = strings.Repeat("d", 40) }),
		"wrong ref":                      proofMutation(func(p *releasetrust.NightlySourceValidation) { p.Ref = "refs/heads/main" }),
		"wrong validation workflow":      proofMutation(func(p *releasetrust.NightlySourceValidation) { p.WorkflowPath = ".github/workflows/ci-feedback.yml" }),
		"PR event":                       proofMutation(func(p *releasetrust.NightlySourceValidation) { p.Event = "pull_request" }),
		"zero run":                       proofMutation(func(p *releasetrust.NightlySourceValidation) { p.RunID = 0 }),
		"zero attempt":                   proofMutation(func(p *releasetrust.NightlySourceValidation) { p.RunAttempt = 0 }),
		"zero workflow":                  proofMutation(func(p *releasetrust.NightlySourceValidation) { p.WorkflowID = 0 }),
		"different retained run":         proofMutation(func(p *releasetrust.NightlySourceValidation) { p.RunID++ }),
		"different retained attempt":     proofMutation(func(p *releasetrust.NightlySourceValidation) { p.RunAttempt++ }),
		"different retained workflow":    proofMutation(func(p *releasetrust.NightlySourceValidation) { p.WorkflowID++ }),
		"inexact numeric ID":             proofMutation(func(p *releasetrust.NightlySourceValidation) { p.RunID = 9007199254740992 }),
		"different policy authority":     proofMutation(func(p *releasetrust.NightlySourceValidation) { p.AuthorityCommit = strings.Repeat("d", 40) }),
		"malformed configuration digest": proofMutation(func(p *releasetrust.NightlySourceValidation) { p.ConfigurationSHA256 = "bad" }),
		"substituted proof digest": proofMutation(func(p *releasetrust.NightlySourceValidation) {
			p.ProofSHA256 = releaseidentity.Hash([]byte("other proof"))
		}),
		"unbound proof artifact": {Manifest: func(m *releasetrust.NightlyManifest) {
			for i, artifact := range m.Artifacts {
				if artifact.Name == releasetrust.NightlySourceValidationArtifact {
					m.Artifacts[i].Kind = "sbom"
				}
			}
		}},
		"v2 under legacy policy": {Policy: func(p *releasetrust.NightlyPolicy) {
			p.Schema, p.SourceRef, p.SourceWorkflowPath = "hikyo.dev/nightly-policy/v1", "", ""
		}},
		"legacy manifest with dev authority": {Manifest: func(m *releasetrust.NightlyManifest) { m.Schema = "hikyo.dev/nightly-manifest/v1" }},
		"unknown manifest field": {RawManifest: func(raw []byte) []byte {
			return append([]byte(`{"unreviewed":true,`), raw[1:]...)
		}},
		"duplicate source field": {RawManifest: func(raw []byte) []byte {
			return append([]byte(`{"source_commit":"`+strings.Repeat("a", 40)+`",`), raw[1:]...)
		}},
	}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			f, material, _ := testfixture.DevNightly(t, []byte("signed compatibility fixture"), options)
			if _, err := releasetrust.VerifyNightly(f.Snapshot(t), material); err == nil {
				t.Fatal("signed but invalid dev publication authority accepted")
			}
		})
	}
}

func TestDevNightlyRetainsReceiptIntegrityAndTransparencyRequirements(t *testing.T) {
	for _, mutation := range []string{"receipt substituted", "receipt missing", "unsigned", "missing SET", "missing checkpoint", "revoked policy", "revoked manifest"} {
		t.Run(mutation, func(t *testing.T) {
			f, material, bundle := testfixture.DevNightly(t, []byte("signed compatibility fixture"), testfixture.NightlyOptions{})
			switch mutation {
			case "receipt substituted":
				material.Artifacts[releasetrust.NightlySourceValidationArtifact] = strings.NewReader("substituted proof")
			case "receipt missing":
				delete(material.Artifacts, releasetrust.NightlySourceValidationArtifact)
			case "unsigned":
				material.Bundle = nil
			case "missing SET":
				bundle.VerificationMaterial.TlogEntries[0].InclusionPromise = nil
				material.Bundle, _ = protojson.Marshal(bundle)
			case "missing checkpoint":
				bundle.VerificationMaterial.TlogEntries[0].InclusionProof.Checkpoint = nil
				material.Bundle, _ = protojson.Marshal(bundle)
			case "revoked policy":
				f.Catalog.NightlyPolicies = []releaseidentity.Digest{releaseidentity.Hash([]byte("different recovery-authorized policy"))}
				f.NightlyPolicy = nil
			case "revoked manifest":
				var current releasetrust.NightlyPolicy
				if err := json.Unmarshal(material.Policy, &current); err != nil {
					t.Fatal(err)
				}
				current.RevokedManifests = []releaseidentity.Digest{releaseidentity.Hash(material.Manifest)}
				f.NightlyPolicy = testfixture.JSON(t, current)
				f.Catalog.NightlyPolicies = append(f.Catalog.NightlyPolicies, releaseidentity.Hash(f.NightlyPolicy))
			}
			if _, err := releasetrust.VerifyNightly(f.Snapshot(t), material); err == nil {
				t.Fatal("invalid signature, log, revocation or receipt accepted")
			}
		})
	}
}

func TestNightlyPolicySuccessorIsExplicitAndLegacyRemainsClosed(t *testing.T) {
	_, legacy, _ := testfixture.Nightly(t, []byte("legacy compatibility"), false)
	current, dev, _ := testfixture.DevNightly(t, []byte("dev compatibility"), testfixture.NightlyOptions{})
	current.Catalog.NightlyPolicies = append(current.Catalog.NightlyPolicies, releaseidentity.Hash(legacy.Policy))
	if _, err := releasetrust.VerifyNightly(current.Snapshot(t), legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := releasetrust.VerifyNightly(current.Snapshot(t), dev); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"source_ref":""`, `"source_ref":null`, `"source_workflow_path":""`} {
		bad := append([]byte("{"+field+","), legacy.Policy[1:]...)
		if _, err := releasetrust.ParseNightlyPolicy(bad); err == nil {
			t.Fatal("legacy policy accepted successor-only field")
		}
	}
	for _, field := range []string{`"publisher_commit":""`, `"source_validation":null`} {
		options := testfixture.NightlyOptions{RawManifest: func(raw []byte) []byte { return append([]byte("{"+field+","), raw[1:]...) }}
		fixture, material, _ := testfixture.NightlyWithOptions(t, []byte("compatibility"), options)
		if _, err := releasetrust.VerifyNightly(fixture.Snapshot(t), material); err == nil {
			t.Fatal("legacy manifest accepted successor-only field")
		}
	}
	for _, mutate := range []func(*releasetrust.NightlyPolicy){
		func(p *releasetrust.NightlyPolicy) { p.SourceRef = "refs/heads/main" },
		func(p *releasetrust.NightlyPolicy) { p.SourceWorkflowPath = ".github/workflows/ci-feedback.yml" },
		func(p *releasetrust.NightlyPolicy) { p.ProtectedRef = "refs/heads/dev" },
		func(p *releasetrust.NightlyPolicy) { p.Schema = "hikyo.dev/nightly-policy/v3" },
	} {
		var policy releasetrust.NightlyPolicy
		if err := json.Unmarshal(dev.Policy, &policy); err != nil {
			t.Fatal(err)
		}
		mutate(&policy)
		if _, err := releasetrust.ParseNightlyPolicy(testfixture.JSON(t, policy)); err == nil {
			t.Fatal("unsupported dev policy accepted")
		}
	}
}

func TestDevNightlyReceiptBindingRejectsDiscardedFields(t *testing.T) {
	_, material, _ := testfixture.DevNightly(t, []byte("compatibility"), testfixture.NightlyOptions{})
	policy, err := releasetrust.ParseNightlyPolicy(material.Policy)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(material.Artifacts[releasetrust.NightlySourceValidationArtifact])
	if err != nil {
		t.Fatal(err)
	}
	proof, err := releasetrust.BindNightlySourceValidation(policy, raw, strings.Repeat("a", 40), strings.Repeat("c", 40))
	if err != nil || proof.ProofSHA256 != releaseidentity.Hash(raw) {
		t.Fatalf("valid exact receipt refused: %v", err)
	}
	for _, bad := range [][]byte{
		append([]byte(`{"proof_sha256":null,`), raw[1:]...),
		append([]byte(`{"extra":true,`), raw[1:]...),
		append([]byte(`{"commit":"`+strings.Repeat("a", 40)+`",`), raw[1:]...),
		bytes.Replace(raw, []byte(`"run_id":42`), []byte(`"run_id":1.5`), 1),
	} {
		if _, err := releasetrust.BindNightlySourceValidation(policy, bad, strings.Repeat("a", 40), strings.Repeat("c", 40)); err == nil {
			t.Fatal("malformed or extended receipt accepted")
		}
	}
}

func TestLegacyStablePolicyRejectsDevOnlyDocumentFields(t *testing.T) {
	for _, field := range []string{`"source_ref":""`, `"source_ref":null`, `"source_workflow_path":""`} {
		f, material, _, _ := testfixture.StableWorkflow(t)
		material.StablePolicy = append([]byte("{"+field+","), material.StablePolicy[1:]...)
		material.StablePolicySignature = testfixture.Sign(t, f.RecoverySigner, material.StablePolicy)
		if _, err := releasetrust.VerifySnapshot(f.Pinned, material, releasetrust.SnapshotFloor{}); err == nil {
			t.Fatal("legacy stable delegation accepted newly recognized dev-only fields")
		}
	}
}
