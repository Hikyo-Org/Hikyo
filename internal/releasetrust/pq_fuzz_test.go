package releasetrust_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// The PQ parsers read release metadata that arrives over a classical channel.
// Go fuzz workers re-run this setup in each process, so every key here comes
// from a fixed seed: seeds the coordinator stores stay valid in the workers and
// the accept branches below can actually be reached. These keys are
// nonproduction and public in this file.

type fuzzPQ struct {
	rootRaw   []byte
	keyFiles  map[string][]byte
	pin       releasetrust.PQPin
	keyset    []byte
	keysetSet []byte
}

func fixedMLDSA(f *testing.F, fill byte) *mldsa.PrivateKey {
	key, err := mldsa.NewPrivateKey(mldsa.MLDSA65(), bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		f.Fatal(err)
	}
	return key
}

func pemOf(f *testing.F, public any) []byte {
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		f.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func newFuzzPQ(f *testing.F) fuzzPQ {
	f.Helper()
	classical, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		f.Fatal(err)
	}
	recoveryKey, releaseKey := fixedMLDSA(f, 0x21), fixedMLDSA(f, 0x31)
	classicalPEM, recoveryPEM, releasePEM := pemOf(f, &classical.PublicKey), pemOf(f, recoveryKey.PublicKey()), pemOf(f, releaseKey.PublicKey())
	v1 := releasetrust.Root{
		Recovery:         releasetrust.RootKey{ID: "fuzz-recovery", PublicKey: "recovery.pub", SHA256: string(releaseidentity.Hash(classicalPEM))},
		BootstrapPrimary: releasetrust.RootKey{ID: "fuzz-primary", PublicKey: "primary.pub", SHA256: string(releaseidentity.Hash([]byte("primary")))},
	}
	v1.Schema = releasetrust.PQRootSchema
	root, err := json.Marshal(releasetrust.PQRoot{Root: v1, PQ: releasetrust.PQRootPolicy{Profile: releasetrust.PQProfile, Generation: 1, Recovery: []releasetrust.PQRootKey{
		{Algorithm: releasetrust.AlgorithmECDSAP256, ID: "fuzz-recovery", PublicKey: "recovery.pub", SHA256: v1.Recovery.SHA256},
		{Algorithm: releasetrust.AlgorithmMLDSA65, ID: "fuzz-recovery-mldsa", PublicKey: "recovery-mldsa.pub", SHA256: string(releaseidentity.Hash(recoveryPEM))},
	}}})
	if err != nil {
		f.Fatal(err)
	}
	files := map[string][]byte{"recovery.pub": classicalPEM, "recovery-mldsa.pub": recoveryPEM, "release-mldsa.pub": releasePEM}
	pin, err := releasetrust.NewPQPin(root, files)
	if err != nil {
		f.Fatal(err)
	}
	keyset, err := json.Marshal(releasetrust.PQKeyset{Schema: releasetrust.PQKeysetSchema, Sequence: 1, Keys: []releasetrust.PQReleaseKey{{Algorithm: releasetrust.AlgorithmMLDSA65, ID: "fuzz-release", PublicKey: "release-mldsa.pub", SHA256: string(releaseidentity.Hash(releasePEM)), ValidFromReleaseSequence: 1}}})
	if err != nil {
		f.Fatal(err)
	}
	classicalID, _ := releasetrust.PQKeyID(releasetrust.AlgorithmECDSAP256, classicalPEM)
	mldsaID, _ := releasetrust.PQKeyID(releasetrust.AlgorithmMLDSA65, recoveryPEM)
	suite := []releasetrust.PQSuiteMember{{Algorithm: releasetrust.AlgorithmECDSAP256, KeyID: classicalID}, {Algorithm: releasetrust.AlgorithmMLDSA65, KeyID: mldsaID}}
	message, err := releasetrust.PQSigningInput(pin, suite, releasetrust.PQBinding{Kind: releasetrust.PQKindKeyset, Channel: string(releaseidentity.StableV1), Subject: "pq-release-keys.json", Sequence: 1, Policy: pin.Digest()}, keyset)
	if err != nil {
		f.Fatal(err)
	}
	digest := sha256.Sum256(message)
	classicalSig, err := ecdsa.SignASN1(rand.Reader, classical, digest[:])
	if err != nil {
		f.Fatal(err)
	}
	mldsaSig, err := recoveryKey.SignDeterministic(message, &mldsa.Options{Context: "hikyo/release-trust/pq-keyset/v1"})
	if err != nil {
		f.Fatal(err)
	}
	set, err := json.Marshal(releasetrust.PQSignatureSet{Schema: releasetrust.PQSignatureSetSchema, Signatures: []releasetrust.PQSignature{
		{Algorithm: releasetrust.AlgorithmECDSAP256, KeyID: classicalID, Signature: base64.StdEncoding.EncodeToString(classicalSig)},
		{Algorithm: releasetrust.AlgorithmMLDSA65, KeyID: mldsaID, Signature: base64.StdEncoding.EncodeToString(mldsaSig)},
	}})
	if err != nil {
		f.Fatal(err)
	}
	if _, _, err := releasetrust.VerifyPQKeyset(pin, releasetrust.PQFloor{}, keyset, set, files); err != nil {
		f.Fatalf("the fixed-seed fixture must verify so the accept branches are reachable: %v", err)
	}
	return fuzzPQ{rootRaw: root, keyFiles: files, pin: pin, keyset: keyset, keysetSet: set}
}

// FuzzNewPQPin only holds that arbitrary root bytes never panic. Root bytes with
// a different generation or added whitespace are legitimately accepted as other
// pins, so there is no stronger oracle here.
func FuzzNewPQPin(f *testing.F) {
	fz := newFuzzPQ(f)
	f.Add(fz.rootRaw)
	f.Add(fz.rootRaw[:len(fz.rootRaw)/2])
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if pin, err := releasetrust.NewPQPin(raw, fz.keyFiles); err == nil && !pin.Valid() {
			t.Fatal("an accepted root produced an invalid pin")
		}
	})
}

// FuzzDecodePQFloor holds the round trip: whatever decodes encodes and decodes
// to the same value.
func FuzzDecodePQFloor(f *testing.F) {
	pin := releaseidentity.Hash([]byte("pin"))
	stage := releaseidentity.Hash([]byte("stage"))
	valid, err := releasetrust.EncodePQFloor(releasetrust.PQFloor{Format: releasetrust.PQFormatVersion, Profile: releasetrust.PQProfile, Generation: 1, PinSHA256: pin,
		KeysetSequence: 1, KeysetSHA256: stage, KeysetPinSHA256: pin, MetadataSequence: 2, MetadataSHA256: stage, MetadataPin: pin,
		CatalogSequence: 2, CatalogSHA256: stage, CatalogPin: pin, ReleaseSequence: 1, ReleasePin: pin})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		got, err := releasetrust.DecodePQFloor(raw)
		if err != nil {
			return
		}
		again, err := releasetrust.EncodePQFloor(got)
		if err != nil {
			t.Fatalf("an accepted floor does not encode: %v", err)
		}
		back, err := releasetrust.DecodePQFloor(again)
		if err != nil || back != got {
			t.Fatalf("round trip changed the floor: %v", err)
		}
	})
}

// FuzzVerifyPQKeyset feeds arbitrary keysets and signature sets to the verifier.
// A keyset is authenticated only by the signature over its exact bytes, so any
// accepted keyset must be the signed one. The set may differ in encoding.
func FuzzVerifyPQKeyset(f *testing.F) {
	fz := newFuzzPQ(f)
	f.Add(fz.keyset, fz.keysetSet)
	f.Add(fz.keyset, fz.keysetSet[:len(fz.keysetSet)/2])
	f.Add(append(bytes.Clone(fz.keyset), ' '), fz.keysetSet)
	f.Add([]byte(`{}`), []byte(`{}`))
	f.Fuzz(func(t *testing.T, keyset, set []byte) {
		_, _, err := releasetrust.VerifyPQKeyset(fz.pin, releasetrust.PQFloor{}, keyset, set, fz.keyFiles)
		if err == nil && !bytes.Equal(keyset, fz.keyset) {
			t.Fatal("accepted keyset bytes that were never signed")
		}
	})
}
