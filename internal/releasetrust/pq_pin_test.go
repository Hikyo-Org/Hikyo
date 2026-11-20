package releasetrust_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/json/v2"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func spkiPEM(t *testing.T, public any) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// v1 readers must refuse the PQ forms instead of ignoring the requirement.
func TestV1ReadersRefuseThePQRootAndNeverParseMLDSAKeys(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	var root releasetrust.Root
	if err := json.Unmarshal(fx.RootV2, &root); err != nil {
		t.Fatal(err)
	}
	if err := releasetrust.ValidateRoot(root, fx.Pinned.RecoveryPublicKey); err == nil {
		t.Fatal("a v1 root validator accepted the PQ root schema")
	}
	pinned := fx.Pinned
	pinned.Root = fx.RootV2
	if _, err := releasetrust.VerifySnapshot(pinned, fx.Material, releasetrust.SnapshotFloor{}); err == nil {
		t.Fatal("a v1 snapshot verifier accepted the PQ root as its pinned root")
	}
	mldsaKey, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatal(err)
	}
	public := spkiPEM(t, mldsaKey.PublicKey())
	if _, err := releasetrust.OperatorKeyID(public); err == nil {
		t.Fatal("operator pin policy widened to ML-DSA")
	}
	bundle := testfixture.JSON(t, releasetrust.LegacyBundle{Base64Signature: "AAAA"})
	if err := releasetrust.VerifyKeySignature(public, bundle, []byte("payload")); err == nil {
		t.Fatal("pinned classical key verifier accepted an ML-DSA key")
	}
	// Existing v1 root bytes are unchanged and a v2 pin extends, never replaces, them.
	if got := releaseidentity.Hash(fx.Pinned.Root); got == fx.Pin.Digest() {
		t.Fatal("v1 root digest equals the PQ pin digest")
	}
}

func TestNewPQPinFailsClosed(t *testing.T) {
	mk := func(t *testing.T, params mldsa.Parameters) []byte {
		key, err := mldsa.GenerateKey(params)
		if err != nil {
			t.Fatal(err)
		}
		return spkiPEM(t, key.PublicKey())
	}
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	for _, c := range []struct {
		name string
		want error
		edit func(t *testing.T, fx *testfixture.PQFixture, root *releasetrust.PQRoot, files map[string][]byte)
	}{
		{"ML-DSA-44 under the mldsa65 label", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			f["recovery-mldsa-1.pub"] = mk(t, mldsa.MLDSA44())
			r.PQ.Recovery[1].SHA256 = string(releaseidentity.Hash(f["recovery-mldsa-1.pub"]))
		}},
		{"ML-DSA-87 under the mldsa65 label", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			f["recovery-mldsa-1.pub"] = mk(t, mldsa.MLDSA87())
			r.PQ.Recovery[1].SHA256 = string(releaseidentity.Hash(f["recovery-mldsa-1.pub"]))
		}},
		{"P-384 under the P-256 label", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			name := r.PQ.Recovery[0].PublicKey
			f[name] = spkiPEM(t, &p384.PublicKey)
			r.PQ.Recovery[0].SHA256 = string(releaseidentity.Hash(f[name]))
			r.Recovery.SHA256 = r.PQ.Recovery[0].SHA256
		}},
		{"ML-DSA key in the classical slot", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			r.PQ.Recovery[0], r.PQ.Recovery[1] = r.PQ.Recovery[1], r.PQ.Recovery[0]
		}},
		{"classical recovery differs from the v1 recovery key", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			r.PQ.Recovery[0].ID = "other"
		}},
		{"key file digest mismatch", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			f["recovery-mldsa-1.pub"] = mk(t, mldsa.MLDSA65())
		}},
		{"key file absent", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			delete(f, "recovery-mldsa-1.pub")
		}},
		{"ML-DSA id equals the bootstrap primary id", releasetrust.ErrPQScope, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			r.PQ.Recovery[1].ID = r.BootstrapPrimary.ID
		}},
		{"generation zero", releasetrust.ErrPQMalformed, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			r.PQ.Generation = 0
		}},
		{"unknown profile", releasetrust.ErrPQMalformed, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			r.PQ.Profile = "pqc-auth/2"
		}},
		{"v1 schema carrying a pq member", releasetrust.ErrPQUnsupported, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			r.Schema = "hikyo.dev/trust-root/v1"
		}},
		{"third recovery member", releasetrust.ErrPQMalformed, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			r.PQ.Recovery = append(r.PQ.Recovery, r.PQ.Recovery[1])
		}},
		{"non-canonical PEM", releasetrust.ErrPQMalformed, func(t *testing.T, fx *testfixture.PQFixture, r *releasetrust.PQRoot, f map[string][]byte) {
			f["recovery-mldsa-1.pub"] = append([]byte("junk\n"), f["recovery-mldsa-1.pub"]...)
			r.PQ.Recovery[1].SHA256 = string(releaseidentity.Hash(f["recovery-mldsa-1.pub"]))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fx := testfixture.NewPQStableWorkflow(t)
			var root releasetrust.PQRoot
			if err := json.Unmarshal(fx.RootV2, &root); err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{}
			for name, raw := range fx.KeyFiles {
				files[name] = bytes.Clone(raw)
			}
			c.edit(t, fx, &root, files)
			_, err := releasetrust.NewPQPin(testfixture.JSON(t, root), files)
			if err == nil {
				t.Fatal("accepted a substituted PQ root")
			}
			class(t, err, c.want)
		})
	}
	t.Run("unknown member", func(t *testing.T) {
		fx := testfixture.NewPQStableWorkflow(t)
		raw := bytes.Replace(fx.RootV2, []byte(`"pq":`), []byte(`"extra":1,"pq":`), 1)
		_, err := releasetrust.NewPQPin(raw, fx.KeyFiles)
		class(t, err, releasetrust.ErrPQMalformed)
	})
}

func TestPQFloorIsSequenceOnlyAndRefusesRollbackAndNewerFormats(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	snapshot, keyset, floor, release := verified(t, fx)
	floor, err := releasetrust.VerifyPQRelease(fx.Pin, keyset, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, floor)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := releasetrust.EncodePQFloor(floor)
	if err != nil {
		t.Fatal(err)
	}
	back, err := releasetrust.DecodePQFloor(raw)
	if err != nil || back != floor {
		t.Fatalf("round trip: %v", err)
	}
	if err := back.AuthenticityClaim(fx.Pin); err != nil {
		t.Fatal(err)
	}
	t.Run("newer format is the format floor", func(t *testing.T) {
		newer := bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":2`), 1)
		_, err := releasetrust.DecodePQFloor(newer)
		class(t, err, releasetrust.ErrPQUnsupported)
	})
	t.Run("unknown member and trailing data", func(t *testing.T) {
		for _, bad := range [][]byte{bytes.Replace(raw, []byte(`{`), []byte(`{"extra":1,`), 1), append(bytes.Clone(raw), '{', '}')} {
			if _, err := releasetrust.DecodePQFloor(bad); err == nil {
				t.Fatal("decoded a non-closed floor")
			}
		}
	})
	t.Run("verifier below the persisted generation", func(t *testing.T) {
		higher := floor
		higher.Generation = 2
		_, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, higher)
		class(t, err, releasetrust.ErrPQRollback)
	})
	t.Run("same generation different pin bytes", func(t *testing.T) {
		other := floor
		other.PinSHA256 = releaseidentity.Hash([]byte("another root"))
		_, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, other)
		class(t, err, releasetrust.ErrPQRollback)
	})
	t.Run("metadata and catalog equivocation", func(t *testing.T) {
		other := floor
		other.MetadataSHA256 = releaseidentity.Hash([]byte("other metadata"))
		_, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, other)
		class(t, err, releasetrust.ErrPQRollback)
		other = floor
		other.CatalogSequence, other.CatalogSHA256 = 3, releaseidentity.Hash([]byte("newer catalog"))
		_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, other)
		class(t, err, releasetrust.ErrPQRollback)
	})
	t.Run("advance refuses any counter going backwards", func(t *testing.T) {
		for _, edit := range []func(*releasetrust.PQFloor){
			func(f *releasetrust.PQFloor) {
				f.MetadataSequence--
				f.MetadataSHA256 = releaseidentity.Hash([]byte("x"))
			},
			func(f *releasetrust.PQFloor) { f.ReleaseSequence, f.ReleasePin = 0, "" },
			func(f *releasetrust.PQFloor) { f.Generation = 0 },
		} {
			next := floor
			edit(&next)
			if floor.Advance(next) == nil {
				t.Fatal("advance accepted a rollback")
			}
		}
	})
	t.Run("claim needs this pin and every stage", func(t *testing.T) {
		partial := floor
		partial.ReleaseSequence, partial.ReleasePin = 0, ""
		if partial.AuthenticityClaim(fx.Pin) == nil {
			t.Fatal("claim without a verified release")
		}
		other := testfixture.NewPQStableWorkflow(t)
		if floor.AuthenticityClaim(other.Pin) == nil {
			t.Fatal("claim carried over to a different pin")
		}
	})
	t.Run("operator-installed higher generation keeps sequences but is not authenticated", func(t *testing.T) {
		// Generation advance is root replacement by whoever installs the new
		// root (E-11). The check keeps every sequence floor and nothing more.
		next := floor
		next.Generation = 2
		next.PinSHA256 = releaseidentity.Hash([]byte("operator installed root"))
		if err := floor.Advance(next); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPQLegacyTrackRequiresTheFullRecoverySet(t *testing.T) {
	fx := testfixture.NewPQLegacy(t)
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	_, floor, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
	if err != nil {
		t.Fatal(err)
	}
	if floor.MetadataSequence != 1 || floor.CatalogSequence != 1 || floor.KeysetSequence != 1 {
		t.Fatalf("floor %+v", floor)
	}
	rec := func(kind string) []testfixture.PQMember { return fx.Recovery(t, kind) }
	for name, c := range map[string]struct {
		want error
		set  func(kind string) []byte
		sign func(kind string, members []testfixture.PQMember) []testfixture.PQMember
	}{
		"ML-DSA recovery signature stripped":    {releasetrust.ErrPQMissing, nil, func(_ string, m []testfixture.PQMember) []testfixture.PQMember { return m[:1] }},
		"classical recovery signature stripped": {releasetrust.ErrPQMissing, nil, func(_ string, m []testfixture.PQMember) []testfixture.PQMember { return m[1:] }},
	} {
		t.Run(name, func(t *testing.T) {
			sets := fx.Sets
			members := rec(releasetrust.PQKindMetadata)
			sets.MetadataSet = fx.Set(t, members, c.sign("", members), fx.TrustBinding(releasetrust.PQKindMetadata, "metadata.json", 1, fx.Policy()), fx.Material.Metadata)
			_, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, sets, releasetrust.PQFloor{})
			class(t, err, c.want)
			sets = fx.Sets
			members = rec(releasetrust.PQKindCatalog)
			sets.CatalogSet = fx.Set(t, members, c.sign("", members), fx.TrustBinding(releasetrust.PQKindCatalog, "catalog.json", 1, fx.Policy()), fx.Material.Catalog)
			_, _, err = releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, sets, releasetrust.PQFloor{})
			class(t, err, c.want)
		})
	}
	t.Run("release role cannot sign the recovery-signed track", func(t *testing.T) {
		sets := fx.Sets
		rel := fx.ReleaseMembers(t, releasetrust.PQKindMetadata)
		sets.MetadataSet = fx.Set(t, rel, rel, fx.TrustBinding(releasetrust.PQKindMetadata, "metadata.json", 1, fx.Policy()), fx.Material.Metadata)
		_, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, sets, releasetrust.PQFloor{})
		class(t, err, releasetrust.ErrPQMissing)
	})
	t.Run("keyed historical releases have no PQ signer", func(t *testing.T) {
		release, err := releasetrust.VerifyStable(snapshot, fx.Stable)
		if err != nil {
			t.Fatal(err)
		}
		keyset, floor, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = releasetrust.VerifyPQRelease(fx.Pin, keyset, snapshot, release, fx.Stable.Manifest, fx.ManifestSet, floor)
		class(t, err, releasetrust.ErrPQScope)
	})
}

// A snapshot that every v1 check accepts, signed only by the delegated CI
// identity, satisfies no PQ requirement: the classical path is not an alternative.
func TestClassicalOnlyEvidenceCannotBypassTheRequiredSet(t *testing.T) {
	fx := testfixture.NewPQStableWorkflow(t)
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := releasetrust.VerifyStable(snapshot, fx.Stable); err != nil {
		t.Fatal(err)
	}
	if _, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, fx.Material, releasetrust.PQSnapshotSets{}, releasetrust.PQFloor{}); err == nil {
		t.Fatal("classical-only evidence satisfied the PQ requirement")
	}
	t.Run("pin that does not extend the snapshot root", func(t *testing.T) {
		other := testfixture.NewPQStableWorkflow(t)
		_, _, err := releasetrust.VerifyPQSnapshot(other.Pin, snapshot, fx.Material, fx.Sets, releasetrust.PQFloor{})
		class(t, err, releasetrust.ErrPQScope)
	})
	t.Run("evidence from a different snapshot", func(t *testing.T) {
		m := fx.Material
		m.Catalog = append(bytes.Clone(m.Catalog), ' ')
		_, _, err := releasetrust.VerifyPQSnapshot(fx.Pin, snapshot, m, fx.Sets, releasetrust.PQFloor{})
		class(t, err, releasetrust.ErrPQScope)
	})
}

func TestPQBridgeNeedsTheFullRecoverySet(t *testing.T) {
	build := func(t *testing.T) (*testfixture.PQFixture, releasetrust.BridgeMaterial, releasetrust.BridgeStatement) {
		fx := testfixture.NewPQLegacy(t)
		statement := releasetrust.BridgeStatement{Schema: "hikyo.dev/legacy-nightly-bridge/v1", SourceGenesis: releaseidentity.LegacyGenesisV1, Mode: "maintenance",
			Target:             releaseidentity.Identity{Profile: releaseidentity.NightlyV1, Version: "1.1.0-nightly.20260101.1.gabcdef12", Sequence: 2, Commit: strings.Repeat("c", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("c")), ManifestSHA256: releaseidentity.Hash([]byte("m"))},
			TargetPolicySHA256: releaseidentity.Hash([]byte("policy")), SourceSchemaSHA256: releaseidentity.Hash([]byte("s")), TargetSchemaSHA256: releaseidentity.Hash([]byte("t"))}
		statement.SourceMigrations = releaseidentity.MigrationManifest{Engine: "sqlite", Entries: []releaseidentity.Migration{{Version: 1, SHA256: releaseidentity.Hash([]byte("1"))}}}
		statement.TargetMigrations = statement.SourceMigrations
		return fx, fx.AddBridge(t, statement), statement
	}
	fx, bridge, statement := build(t)
	// The catalog changed, so rebuild the fixture material and PQ evidence over it.
	fx.Material = fx.Fixture.Material(t)
	fx.Resign(t)
	snapshot, err := releasetrust.VerifySnapshot(fx.Pinned, fx.Material, releasetrust.SnapshotFloor{})
	if err != nil {
		t.Fatal(err)
	}
	target := statement.Target
	binding := releasetrust.PQBinding{Kind: releasetrust.PQKindBridge, Channel: string(target.Profile), Version: target.Version, Subject: "bridge.json", Source: target.Commit, Sequence: target.Sequence, Policy: statement.TargetPolicySHA256}
	rec := fx.Recovery(t, releasetrust.PQKindBridge)
	good := fx.Set(t, rec, rec, binding, bridge.Statement)
	if _, err := releasetrust.VerifyPQBridge(fx.Pin, snapshot, bridge, good); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		want error
		set  func() []byte
		mat  releasetrust.BridgeMaterial
	}{
		"ML-DSA stripped":    {releasetrust.ErrPQMissing, func() []byte { return fx.Set(t, rec, rec[:1], binding, bridge.Statement) }, bridge},
		"classical stripped": {releasetrust.ErrPQMissing, func() []byte { return fx.Set(t, rec, rec[1:], binding, bridge.Statement) }, bridge},
		"release role signs": {releasetrust.ErrPQMissing, func() []byte {
			rel := fx.ReleaseMembers(t, releasetrust.PQKindBridge)
			return fx.Set(t, rel, rel, binding, bridge.Statement)
		}, bridge},
		"signed for another target policy": {releasetrust.ErrPQSignature, func() []byte {
			b := binding
			b.Policy = releaseidentity.Hash([]byte("other"))
			return fx.Set(t, rec, rec, b, bridge.Statement)
		}, bridge},
		"no set":                     {releasetrust.ErrPQMissing, func() []byte { return nil }, bridge},
		"bridge outside the catalog": {releasetrust.ErrPQScope, func() []byte { return good }, releasetrust.BridgeMaterial{Statement: []byte(`{"schema":"x"}`), Signature: bridge.Signature}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := releasetrust.VerifyPQBridge(fx.Pin, snapshot, c.mat, c.set())
			if err == nil {
				t.Fatal("accepted tampered bridge evidence")
			}
			class(t, err, c.want) // the unchanged catalog check refuses first, wrapped in the closed set
		})
	}
}
