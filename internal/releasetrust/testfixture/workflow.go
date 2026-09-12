package testfixture

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/url"
	"testing"
	"time"

	ct "github.com/google/certificate-transparency-go"
	"github.com/google/certificate-transparency-go/tls"
	ctx509 "github.com/google/certificate-transparency-go/x509"
	"github.com/google/certificate-transparency-go/x509util"

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

// WorkflowSigner builds real ephemeral Fulcio, SCT, SET and inclusion evidence.
func WorkflowSigner(t testing.TB) (releasetrust.StablePolicy, []byte, func([]byte, string, string) []byte) {
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
	trust, err := root.NewTrustedRoot(root.TrustedRootMediaType01, []root.CertificateAuthority{&root.FulcioCertificateAuthority{Root: rootCert, URI: "https://fixture.fulcio.invalid", ValidityPeriodStart: rootCert.NotBefore, ValidityPeriodEnd: rootCert.NotAfter}}, rekorLogs, nil, rekorLogs)
	if err != nil {
		t.Fatal(err)
	}
	rootRaw, err := trust.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	policy := releasetrust.StablePolicy{NightlyPolicy: releasetrust.NightlyPolicy{Schema: "hikyo.dev/stable-policy/v1", TrustedRootSHA256: releaseidentity.Hash(rootRaw), Issuer: "https://token.actions.githubusercontent.com", RepositoryURI: "https://github.com/synthetic/hikyo", RepositoryID: "123", RepositoryOwnerURI: "https://github.com/synthetic", RepositoryOwnerID: "456", WorkflowPath: ".github/workflows/release.yml", ProtectedRef: "refs/tags/v*", RunnerEnvironment: "github-hosted", RevokedManifests: []releaseidentity.Digest{}, RequireSCT: ptr(true), RekorLogID: rekorID, CheckpointOrigin: "rekor.fixture.invalid - 1"}, MinimumReleaseSequence: 1, NightlyPolicies: []releaseidentity.Digest{}, Bridges: []releaseidentity.Digest{}}
	return policy, rootRaw, func(payload []byte, commit, tag string) []byte {
		workflow := policy.RepositoryURI + "/" + policy.WorkflowPath + "@" + "refs/tags/" + tag
		uri, err := url.Parse(workflow)
		if err != nil {
			t.Fatal(err)
		}
		certCommit := commit

		extensions := []struct {
			oid   asn1.ObjectIdentifier
			value string
		}{
			{certificate.OIDIssuerV2, policy.Issuer}, {certificate.OIDBuildSignerURI, workflow}, {certificate.OIDBuildSignerDigest, certCommit},
			{certificate.OIDRunnerEnvironment, policy.RunnerEnvironment}, {certificate.OIDSourceRepositoryURI, policy.RepositoryURI}, {certificate.OIDSourceRepositoryDigest, certCommit},
			{certificate.OIDSourceRepositoryRef, "refs/tags/" + tag}, {certificate.OIDSourceRepositoryIdentifier, policy.RepositoryID}, {certificate.OIDSourceRepositoryOwnerURI, policy.RepositoryOwnerURI},
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
		certRaw = embedWorkflowSCT(t, template, certRaw, rootCert, rootKey, private, rekorKey, rekorIDBytes, integrated)
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
		statement := JSON(t, map[string]any{"_type": "https://in-toto.io/Statement/v1", "subject": []any{map[string]any{"name": "artifact", "digest": map[string]string{"sha256": string(releaseidentity.Hash(payload))}}}, "predicateType": "https://hikyo.dev/nightly-manifest/v1", "predicate": map[string]any{}})
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
		setPayload := JSON(t, tlog.RekorPayload{LogID: string(rekorID), IntegratedTime: integrated.Unix(), LogIndex: tlogEntry.LogIndex, Body: base64.StdEncoding.EncodeToString(tlogEntry.CanonicalizedBody)})
		canonical, err := jsoncanonicalizer.Transform(setPayload)
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
		return bundleRaw
	}
}
func ptr[T any](v T) *T { return &v }

func embedWorkflowSCT(t testing.TB, template *x509.Certificate, precert []byte, issuer *x509.Certificate, issuerKey, leafKey, logKey *ecdsa.PrivateKey, logID []byte, integrated time.Time) []byte {
	t.Helper()
	parsed, err := x509.ParseCertificate(precert)
	if err != nil {
		t.Fatal(err)
	}
	sct := ct.SignedCertificateTimestamp{SCTVersion: ct.V1, Timestamp: uint64(integrated.UnixMilli())}
	copy(sct.LogID.KeyID[:], logID)
	entry := ct.LogEntry{Leaf: ct.MerkleTreeLeaf{Version: ct.V1, LeafType: ct.TimestampedEntryLeafType, TimestampedEntry: &ct.TimestampedEntry{Timestamp: sct.Timestamp, EntryType: ct.PrecertLogEntryType, PrecertEntry: &ct.PreCert{IssuerKeyHash: sha256.Sum256(issuer.RawSubjectPublicKeyInfo), TBSCertificate: parsed.RawTBSCertificate}}}}
	input, err := ct.SerializeSCTSignatureInput(sct, entry)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(input)
	sig, err := logKey.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	sct.Signature = ct.DigitallySigned{Algorithm: tls.SignatureAndHashAlgorithm{Hash: tls.SHA256, Signature: tls.ECDSA}, Signature: sig}
	list, err := x509util.MarshalSCTsIntoSCTList([]*ct.SignedCertificateTimestamp{&sct})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tls.Marshal(*list)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := asn1.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	template.ExtraExtensions = append(template.ExtraExtensions, pkix.Extension{Id: asn1.ObjectIdentifier(ctx509.OIDExtensionCTSCT), Value: encoded})
	cert, err := x509.CreateCertificate(rand.Reader, template, issuer, &leafKey.PublicKey, issuerKey)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
