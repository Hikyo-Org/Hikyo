package releasetrust_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

func v2Statement(schema, notAfter string) releasetrust.BridgeStatement {
	digest := releaseidentity.Hash([]byte("fixture"))
	source := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), CompatibilitySHA256: digest, ManifestSHA256: digest}
	target := source
	target.Version, target.Sequence = "1.1.0", 2
	migrations := releaseidentity.MigrationManifest{Engine: releaseidentity.SQLite, Entries: []releaseidentity.Migration{}}
	return releasetrust.BridgeStatement{
		Schema: schema, Source: source, Target: target, SourcePolicySHA256: digest, TargetPolicySHA256: digest,
		SourceMigrations: migrations, TargetMigrations: migrations, SourceSchemaSHA256: digest, TargetSchemaSHA256: digest,
		Mode: "maintenance", NotAfter: notAfter,
	}
}

func TestBridgeNotAfterIsRequiredCanonicalAndEnforced(t *testing.T) {
	for _, tc := range []struct {
		name, schema, notAfter, refusal string
	}{
		{"v2 canonical", releasetrust.BridgeSchemaV2, "2026-12-24T00:00:00Z", ""},
		{"v2 without not_after", releasetrust.BridgeSchemaV2, "", "requires not_after"},
		{"v1 carrying not_after", "hikyo.dev/recovery-bridge/v1", "2026-12-24T00:00:00Z", "requires not_after"},
		{"v2 with an offset", releasetrust.BridgeSchemaV2, "2026-12-24T01:00:00+01:00", "canonical UTC"},
		{"v2 with fractional seconds", releasetrust.BridgeSchemaV2, "2026-12-24T00:00:00.5Z", "canonical UTC"},
		{"v2 malformed", releasetrust.BridgeSchemaV2, "tomorrow", "canonical UTC"},
		{"v1 without not_after", "hikyo.dev/recovery-bridge/v1", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := testfixture.New(t)
			material := f.AddBridge(t, v2Statement(tc.schema, tc.notAfter))
			bridge, err := releasetrust.VerifyBridge(f.Snapshot(t), material)
			if tc.refusal != "" {
				if err == nil || !strings.Contains(err.Error(), tc.refusal) {
					t.Fatalf("want refusal %q, got %v", tc.refusal, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.notAfter == "" {
				if bridge.Expired(time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC)) {
					t.Fatal("a v1 statement has no expiry")
				}
				return
			}
			edge := time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)
			for _, probe := range []struct {
				at      time.Time
				expired bool
			}{{edge.Add(-time.Second), false}, {edge, true}, {edge.Add(time.Second), true}} {
				if bridge.Expired(probe.at) != probe.expired {
					t.Fatalf("Expired(%s) = %v, want %v", probe.at, !probe.expired, probe.expired)
				}
			}
		})
	}
}

func revocationDocument(f *testfixture.Fixture, t *testing.T, mutate func(*releasetrust.RevocationList)) ([]byte, []byte) {
	t.Helper()
	list := releasetrust.RevocationList{Schema: releasetrust.RevocationSchema, Sequence: 3, TrustRootSHA256: releaseidentity.Hash(f.Pinned.Root), Revoked: []releaseidentity.Digest{releaseidentity.Hash([]byte("a")), releaseidentity.Hash([]byte("b"))}}
	if list.Revoked[0] > list.Revoked[1] {
		list.Revoked[0], list.Revoked[1] = list.Revoked[1], list.Revoked[0]
	}
	if mutate != nil {
		mutate(&list)
	}
	raw := testfixture.JSON(t, list)
	return raw, testfixture.Sign(t, f.RecoverySigner, raw)
}

func TestRevocationListIsAuthenticatedAndBoundToTheRoot(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	raw, signature := revocationDocument(f, t, nil)
	verified, err := snapshot.VerifyRevocations(raw, signature)
	if err != nil || verified.Sequence() != 3 || len(verified.Revoked()) != 2 || verified.Digest() != releaseidentity.Hash(raw) {
		t.Fatalf("a signed, root-bound list verifies: %+v %v", verified, err)
	}
	other := testfixture.New(t)
	foreign, foreignSignature := revocationDocument(other, t, nil)
	for name, tc := range map[string]struct {
		raw, signature []byte
		want           string
	}{
		"signed by another key":  {raw, testfixture.Sign(t, other.RecoverySigner, raw), ""},
		"another trust root":     {foreign, foreignSignature, ""},
		"bound to another root":  {revoke(f, t, func(l *releasetrust.RevocationList) { l.TrustRootSHA256 = releaseidentity.Hash([]byte("x")) }), nil, "different trust root"},
		"sequence zero":          {revoke(f, t, func(l *releasetrust.RevocationList) { l.Sequence = 0 }), nil, "schema or sequence"},
		"wrong schema":           {revoke(f, t, func(l *releasetrust.RevocationList) { l.Schema = "other" }), nil, "schema or sequence"},
		"unsorted":               {revoke(f, t, func(l *releasetrust.RevocationList) { l.Revoked[0], l.Revoked[1] = l.Revoked[1], l.Revoked[0] }), nil, "sorted"},
		"duplicate":              {revoke(f, t, func(l *releasetrust.RevocationList) { l.Revoked[1] = l.Revoked[0] }), nil, "sorted"},
		"invalid digest":         {revoke(f, t, func(l *releasetrust.RevocationList) { l.Revoked = []releaseidentity.Digest{"zz"} }), nil, "sorted"},
		"too many":               {revoke(f, t, func(l *releasetrust.RevocationList) { l.Revoked = manyDigests(releasetrust.MaxRevocations + 1) }), nil, "too long"},
		"not json":               {[]byte("{"), nil, ""},
		"unknown field":          {[]byte(`{"schema":"` + releasetrust.RevocationSchema + `","sequence":1,"extra":1}`), nil, ""},
		"tampered after signing": {append([]byte(" "), raw...), signature, ""},
	} {
		t.Run(name, func(t *testing.T) {
			signature := tc.signature
			if signature == nil {
				signature = testfixture.Sign(t, f.RecoverySigner, tc.raw)
			}
			_, err := snapshot.VerifyRevocations(tc.raw, signature)
			if err == nil || (tc.want != "" && !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("want refusal %q, got %v", tc.want, err)
			}
		})
	}
}

func revoke(f *testfixture.Fixture, t *testing.T, mutate func(*releasetrust.RevocationList)) []byte {
	raw, _ := revocationDocument(f, t, mutate)
	return raw
}

func manyDigests(n int) []releaseidentity.Digest {
	out := make([]releaseidentity.Digest, 0, n)
	for i := range n {
		out = append(out, releaseidentity.Hash([]byte{byte(i), byte(i >> 8)}))
	}
	return out
}
