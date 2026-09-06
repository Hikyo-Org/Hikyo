package testfixture

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/secure-systems-lab/go-securesystemslib/dsse"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	common "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protodsse "github.com/sigstore/protobuf-specs/gen/pb-go/dsse"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/rekor/pkg/util"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"github.com/sigstore/sigstore/pkg/signature"
	sigdsse "github.com/sigstore/sigstore/pkg/signature/dsse"
	"github.com/transparency-dev/merkle/rfc6962"
	"google.golang.org/protobuf/encoding/protojson"
)

// All authenticity here is real: ephemeral certificate chain, maintained DSSE
// signer, Rekor SET, Merkle proof and signed checkpoint, then serialized bundle.
func Nightly(t testing.TB, compatibility []byte, wrongCommit bool) (*Fixture, releasetrust.NightlyMaterial, *protobundle.Bundle) {
	return NightlyWithPayloads(t, compatibility, wrongCommit, nil, nil)
}

// NightlyWithPayloads signs caller-provided test payload bytes using the same
// ephemeral certificate and real transparency-log proof as Nightly.
func NightlyWithPayloads(t testing.TB, compatibility []byte, wrongCommit bool, payloads map[string][]byte, artifacts []releasetrust.Artifact) (*Fixture, releasetrust.NightlyMaterial, *protobundle.Bundle) {
	t.Helper()
	virtual, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	rootCert, rootKey, err := ca.GenerateRootCa()
	if err != nil {
		t.Fatal(err)
	}
	rekorKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rekorSigner, err := signature.LoadSigner(rekorKey, crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	rekorDER, err := x509.MarshalPKIXPublicKey(&rekorKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	rekorID := releaseidentity.Hash(rekorDER)
	rekorIDBytes, err := hex.DecodeString(string(rekorID))
	if err != nil {
		t.Fatal(err)
	}
	rekorLogs := map[string]*root.TransparencyLog{string(rekorID): {BaseURL: "https://rekor.fixture.invalid", ID: rekorIDBytes, ValidityPeriodStart: rootCert.NotBefore, ValidityPeriodEnd: rootCert.NotAfter, HashFunc: crypto.SHA256, SignatureHashFunc: crypto.SHA256, PublicKey: &rekorKey.PublicKey}}
	trust, err := root.NewTrustedRoot(root.TrustedRootMediaType01, []root.CertificateAuthority{&root.FulcioCertificateAuthority{Root: rootCert, URI: "https://fixture.fulcio.invalid", ValidityPeriodStart: rootCert.NotBefore, ValidityPeriodEnd: rootCert.NotAfter}}, nil, nil, rekorLogs)
	if err != nil {
		t.Fatal(err)
	}
	rootRaw, err := trust.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	policy := releasetrust.NightlyPolicy{Schema: "hikyo.dev/nightly-policy/v1", TrustedRootSHA256: releaseidentity.Hash(rootRaw), Issuer: "https://token.actions.githubusercontent.com", RepositoryURI: "https://github.com/synthetic/hikyo", RepositoryID: "123", RepositoryOwnerURI: "https://github.com/synthetic", RepositoryOwnerID: "456", WorkflowPath: ".github/workflows/nightly.yml", ProtectedRef: "refs/heads/main", RunnerEnvironment: "github-hosted", RevokedManifests: []releaseidentity.Digest{}}
	policyRaw := JSON(t, policy)
	requireSCT := false
	policy.RequireSCT = &requireSCT
	policy.RekorLogID = rekorID
	policy.CheckpointOrigin = "rekor.fixture.invalid - 1"
	policyRaw = JSON(t, policy)
	f := New(t)
	f.Catalog.NightlyPolicies = append(f.Catalog.NightlyPolicies, releaseidentity.Hash(policyRaw))
	f.NightlyPolicy = policyRaw
	sign := func(compatibility []byte, version string, sequence uint64, payloads map[string][]byte, artifacts []releasetrust.Artifact) (releasetrust.NightlyMaterial, *protobundle.Bundle) {
		commit := strings.Repeat("a", 40)
		if payloads == nil {
			payloads = map[string][]byte{releasetrust.CompatibilityArtifact: compatibility, "hikyo_linux_arm64.tar.gz": []byte("real bytes bound by fixture"), "binary-provenance.json": []byte("{}"), "checksums.txt": []byte("fixture checksums")}
			artifacts = []releasetrust.Artifact{{Name: releasetrust.CompatibilityArtifact, Kind: "upgrade-compatibility"}, {Name: "hikyo_linux_arm64.tar.gz", Kind: "binary", Platform: "linux/arm64"}, {Name: "binary-provenance.json", Kind: "binary-provenance"}, {Name: "checksums.txt", Kind: "checksum"}}
		}
		payloads["nightly-policy.json"], payloads["sigstore-trusted-root.json"] = policyRaw, rootRaw
		artifacts = append(artifacts, releasetrust.Artifact{Name: "nightly-policy.json", Kind: "nightly-policy"}, releasetrust.Artifact{Name: "sigstore-trusted-root.json", Kind: "sigstore-trusted-root"})
		for i := range artifacts {
			artifacts[i].SHA256 = string(releaseidentity.Hash(payloads[artifacts[i].Name]))
		}
		manifest := JSON(t, releasetrust.NightlyManifest{Schema: "hikyo.dev/nightly-manifest/v1", Profile: releaseidentity.NightlyV1, Version: version, Tag: "v" + version, SourceCommit: commit, ReleaseSequence: sequence, Artifacts: artifacts})
		workflow := policy.RepositoryURI + "/" + policy.WorkflowPath + "@" + policy.ProtectedRef
		uri, err := url.Parse(workflow)
		if err != nil {
			t.Fatal(err)
		}
		certCommit := commit
		if wrongCommit {
			certCommit = strings.Repeat("b", 40)
		}
		extensions := []struct {
			oid   asn1.ObjectIdentifier
			value string
		}{
			{certificate.OIDIssuerV2, policy.Issuer}, {certificate.OIDBuildSignerURI, workflow}, {certificate.OIDBuildSignerDigest, certCommit},
			{certificate.OIDRunnerEnvironment, policy.RunnerEnvironment}, {certificate.OIDSourceRepositoryURI, policy.RepositoryURI}, {certificate.OIDSourceRepositoryDigest, certCommit},
			{certificate.OIDSourceRepositoryRef, policy.ProtectedRef}, {certificate.OIDSourceRepositoryIdentifier, policy.RepositoryID}, {certificate.OIDSourceRepositoryOwnerURI, policy.RepositoryOwnerURI},
			{certificate.OIDSourceRepositoryOwnerIdentifier, policy.RepositoryOwnerID}, {certificate.OIDBuildConfigURI, workflow}, {certificate.OIDBuildConfigDigest, certCommit},
		}
		// The leaf is expired at verification wall time. Its signed integrated time
		// remains inside validity, proving verification never substitutes time.Now.
		integrated := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
		template := &x509.Certificate{SerialNumber: big.NewInt(2), URIs: []*url.URL{uri}, NotBefore: integrated.Add(-time.Minute), NotAfter: integrated.Add(time.Minute), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}}
		for _, extension := range extensions {
			raw, err := asn1.Marshal(extension.value)
			if err != nil {
				t.Fatal(err)
			}
			template.ExtraExtensions = append(template.ExtraExtensions, pkix.Extension{Id: extension.oid, Value: raw})
		}
		private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		certRaw, err := x509.CreateCertificate(rand.Reader, template, rootCert, &private.PublicKey, rootKey)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(certRaw)
		if err != nil {
			t.Fatal(err)
		}
		signer, err := signature.LoadSigner(private, crypto.SHA256)
		if err != nil {
			t.Fatal(err)
		}
		dsseSigner, err := dsse.NewEnvelopeSigner(&sigdsse.SignerAdapter{SignatureSigner: signer, Pub: cert.PublicKey})
		if err != nil {
			t.Fatal(err)
		}
		statement := JSON(t, map[string]any{"_type": "https://in-toto.io/Statement/v1", "subject": []any{map[string]any{"name": "release-manifest.json", "digest": map[string]string{"sha256": string(releaseidentity.Hash(manifest))}}}, "predicateType": "https://hikyo.dev/nightly-manifest/v1", "predicate": map[string]any{}})
		envelope, err := dsseSigner.SignPayload(context.Background(), "application/vnd.in-toto+json", statement)
		if err != nil {
			t.Fatal(err)
		}
		sig, err := base64.StdEncoding.DecodeString(envelope.Signatures[0].Sig)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := virtual.GenerateTlogEntry(cert, envelope, sig, integrated.Unix(), true)
		if err != nil {
			t.Fatal(err)
		}
		// The maintained fixture's deprecated constructor keeps the SET and kind
		// outside its protobuf. Populate the wire representation explicitly.
		tlogEntry := entry.TransparencyLogEntry()
		var body struct {
			Kind       string `json:"kind"`
			APIVersion string `json:"apiVersion"`
		}
		if err := json.Unmarshal(tlogEntry.CanonicalizedBody, &body); err != nil {
			t.Fatal(err)
		}
		tlogEntry.KindVersion = &protorekor.KindVersion{Kind: body.Kind, Version: body.APIVersion}
		tlogEntry.LogId.KeyId = rekorIDBytes
		// Rekor v1 signs the global (virtual) index in its SET, while the
		// inclusion proof below uses the index within the current shard.
		// Model a nonempty previous shard so tests cannot conflate the two.
		tlogEntry.LogIndex = 121904262
		payload := JSON(t, tlog.RekorPayload{LogID: string(rekorID), IntegratedTime: integrated.Unix(), LogIndex: tlogEntry.LogIndex, Body: base64.StdEncoding.EncodeToString(tlogEntry.CanonicalizedBody)})
		canonical, err := jsoncanonicalizer.Transform(payload)
		if err != nil {
			t.Fatal(err)
		}
		set, err := rekorSigner.SignMessage(bytes.NewReader(canonical))
		if err != nil {
			t.Fatal(err)
		}
		tlogEntry.InclusionPromise = &protorekor.InclusionPromise{SignedEntryTimestamp: set}
		leafHash := rfc6962.DefaultHasher.HashLeaf(tlogEntry.CanonicalizedBody)
		checkpoint, err := util.CreateAndSignCheckpoint(context.Background(), "rekor.fixture.invalid", 1, 1, leafHash, rekorSigner)
		if err != nil {
			t.Fatal(err)
		}
		tlogEntry.InclusionProof = &protorekor.InclusionProof{LogIndex: 0, TreeSize: 1, RootHash: leafHash, Hashes: [][]byte{}, Checkpoint: &protorekor.Checkpoint{Envelope: string(checkpoint)}}
		pb := &protobundle.Bundle{MediaType: "application/vnd.dev.sigstore.bundle.v0.3+json", VerificationMaterial: &protobundle.VerificationMaterial{Content: &protobundle.VerificationMaterial_Certificate{Certificate: &common.X509Certificate{RawBytes: certRaw}}, TlogEntries: []*protorekor.TransparencyLogEntry{entry.TransparencyLogEntry()}}, Content: &protobundle.Bundle_DsseEnvelope{DsseEnvelope: &protodsse.Envelope{Payload: statement, PayloadType: envelope.PayloadType, Signatures: []*protodsse.Signature{{Sig: sig}}}}}
		bundleRaw, err := protojson.Marshal(pb)
		if err != nil {
			t.Fatal(err)
		}
		readers := map[string]io.Reader{}
		for name, raw := range payloads {
			readers[name] = bytes.NewReader(raw)
		}
		return releasetrust.NightlyMaterial{Policy: policyRaw, TrustedRoot: rootRaw, Manifest: manifest, Bundle: bundleRaw, Compatibility: payloads[releasetrust.CompatibilityArtifact], Artifacts: readers}, pb
	}
	f.SignNightly = func(compatibility []byte, version string, sequence uint64) releasetrust.NightlyMaterial {
		material, _ := sign(compatibility, version, sequence, nil, nil)
		return material
	}
	material, pb := sign(compatibility, "1.1.0-nightly.1", 2, payloads, artifacts)
	return f, material, pb
}
