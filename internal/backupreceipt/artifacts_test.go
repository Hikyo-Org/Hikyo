package backupreceipt

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func fixtureSnapshot() Snapshot {
	return Snapshot{
		Authority: LedgerAuthority,
		BackupID:  Nonce(strings.Repeat("1", 64)), InstanceID: "ins_" + strings.Repeat("2", 32), Engine: releaseidentity.SQLite,
		SourceIdentity: releaseidentity.Source{Release: fixtureRelease()}, SourceSchemaSHA256: releaseidentity.Hash([]byte("actual domain catalog")), MigrationSHA256: releaseidentity.Hash([]byte("migration inventory")),
		RestoreEpoch: 0, RecoveryIncarnation: Nonce(strings.Repeat("3", 64)), SourceGeneration: 1, RouteGeneration: 2,
		CreatedAt: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), RecipientFingerprints: []string{"age-x25519-sha256:" + strings.Repeat("4", 64)},
	}
}

func fixtureRelease() releaseidentity.Identity {
	return releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compatibility")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
}

func fixtureReceipt() Receipt {
	return Receipt{Format: ReceiptFormat, CiphertextSHA256: releaseidentity.Hash([]byte("ciphertext")), CiphertextBytes: 256, ManifestSHA256: releaseidentity.Hash([]byte("encrypted manifest")), Snapshot: fixtureSnapshot()}
}

func fixtureAttestation() Attestation {
	s := fixtureSnapshot()
	return Attestation{Authority: LedgerAuthority, Format: AttestationFormat, ReceiptSHA256: releaseidentity.Hash([]byte("receipt")), RouteSHA256: releaseidentity.Hash([]byte("route")), BridgeSHA256: []releaseidentity.Digest{}, TargetIdentity: fixtureRelease(), InstanceID: s.InstanceID, RestoreEpoch: s.RestoreEpoch, RecoveryIncarnation: s.RecoveryIncarnation, SourceGeneration: s.SourceGeneration, RouteGeneration: s.RouteGeneration, OperatorKeyID: releaseidentity.Hash([]byte("public key")), IssuedAt: s.CreatedAt, ExpiresAt: s.CreatedAt.Add(time.Hour), Nonce: Nonce(strings.Repeat("5", 64))}
}

func mustEncode(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestClosedReceiptRequiresEveryExplicitSnapshotBinding(t *testing.T) {
	base := mustEncode(t, fixtureReceipt())
	if _, err := ParseReceipt(base); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(base, &fields); err != nil {
		t.Fatal(err)
	}
	for field := range fields {
		t.Run(field, func(t *testing.T) {
			var changed map[string]json.RawMessage
			if err := json.Unmarshal(base, &changed); err != nil {
				t.Fatal(err)
			}
			changed[field] = json.RawMessage("null")
			if _, err := ParseReceipt(mustEncode(t, changed)); err == nil {
				t.Fatal("null required field accepted")
			}
			delete(changed, field)
			if _, err := ParseReceipt(mustEncode(t, changed)); err == nil {
				t.Fatal("missing required field accepted")
			}
		})
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(fields["snapshot"], &snapshot); err != nil {
		t.Fatal(err)
	}
	for field := range snapshot {
		t.Run("snapshot/"+field, func(t *testing.T) {
			var changed map[string]json.RawMessage
			if err := json.Unmarshal(fields["snapshot"], &changed); err != nil {
				t.Fatal(err)
			}
			changed[field] = json.RawMessage("null")
			fieldsCopy := map[string]json.RawMessage{}
			for name, value := range fields {
				fieldsCopy[name] = value
			}
			fieldsCopy["snapshot"] = mustEncode(t, changed)
			if _, err := ParseReceipt(mustEncode(t, fieldsCopy)); err == nil {
				t.Fatal("null snapshot binding accepted")
			}
			delete(changed, field)
			fieldsCopy["snapshot"] = mustEncode(t, changed)
			if _, err := ParseReceipt(mustEncode(t, fieldsCopy)); err == nil {
				t.Fatal("missing snapshot binding accepted")
			}
		})
	}
	for _, changed := range []string{
		strings.Replace(string(base), `"restore_epoch":0`, `"restore_epoch":0,"restore_epoch":1`, 1),
		strings.Replace(string(base), `"format":`, `"tenant":"forbidden","format":`, 1),
		strings.Replace(string(base), `"format":`, `"FORMAT":`, 1),
		strings.Replace(string(base), `2026-09-05T00:00:00Z`, `2026-09-05T00:00:00.000Z`, 1),
		strings.Replace(string(base), `2026-09-05T00:00:00Z`, `2026-09-05T00:00:00+00:00`, 1),
		string(base) + ` {}`,
		strings.Repeat(" ", MaxArtifactBytes) + string(base),
	} {
		if _, err := ParseReceipt([]byte(changed)); err == nil {
			t.Fatal("noncanonical receipt accepted")
		}
	}
}

func TestReceiptRefusesInvalidIdentityAndGeneration(t *testing.T) {
	for name, mutate := range map[string]func(*Receipt){
		"wrong format":     func(r *Receipt) { r.Format = "backup-receipt/v2" },
		"zero incarnation": func(r *Receipt) { r.Snapshot.RecoveryIncarnation = Nonce(strings.Repeat("0", 64)) },
		"fresh genesis": func(r *Receipt) {
			r.Snapshot.SourceIdentity = releaseidentity.Source{Genesis: releaseidentity.FreshGenesisV1}
		},
		"unknown genesis":    func(r *Receipt) { r.Snapshot.SourceIdentity = releaseidentity.Source{Genesis: "inferred-legacy"} },
		"unbound generation": func(r *Receipt) { r.Snapshot.RouteGeneration++ },
		"overflow": func(r *Receipt) {
			r.Snapshot.SourceGeneration = math.MaxInt64
			r.Snapshot.RouteGeneration = math.MinInt64
		},
		"negative epoch":     func(r *Receipt) { r.Snapshot.RestoreEpoch = -1 },
		"missing recipients": func(r *Receipt) { r.Snapshot.RecipientFingerprints = []string{} },
		"duplicate recipients": func(r *Receipt) {
			r.Snapshot.RecipientFingerprints = append(r.Snapshot.RecipientFingerprints, r.Snapshot.RecipientFingerprints[0])
		},
		"zero bytes": func(r *Receipt) { r.CiphertextBytes = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			r := fixtureReceipt()
			mutate(&r)
			if _, err := ParseReceipt(mustEncode(t, r)); err == nil {
				t.Fatal("invalid receipt accepted")
			}
		})
	}
	legacy := fixtureReceipt()
	legacy.Snapshot.SourceIdentity = releaseidentity.Source{Genesis: releaseidentity.LegacyGenesisV1}
	legacy.Snapshot.Authority = LegacyProposalAuthority
	legacy.Snapshot.SourceGeneration = 0
	legacy.Snapshot.RouteGeneration = 1
	legacy.Snapshot.SourceSchemaSHA256 = releaseidentity.Hash([]byte("exact legacy schema"))
	if _, err := ParseReceipt(mustEncode(t, legacy)); err != nil {
		t.Fatal("explicit legacy source refused", err)
	}
}

func TestAttestationLifetimeAndClosedBindings(t *testing.T) {
	base := fixtureAttestation()
	if _, err := ParseAttestation(mustEncode(t, base)); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Attestation){
		"over24hours":       func(a *Attestation) { a.ExpiresAt = a.IssuedAt.Add(MaxAttestationLifetime + time.Second) },
		"zero lifetime":     func(a *Attestation) { a.ExpiresAt = a.IssuedAt },
		"negative lifetime": func(a *Attestation) { a.ExpiresAt = a.IssuedAt.Add(-time.Second) },
		"fractional issued": func(a *Attestation) { a.IssuedAt = a.IssuedAt.Add(time.Nanosecond) },
		"null bridges":      func(a *Attestation) { a.BridgeSHA256 = nil },
		"duplicate bridges": func(a *Attestation) {
			d := releaseidentity.Hash([]byte("bridge"))
			a.BridgeSHA256 = []releaseidentity.Digest{d, d}
		},
		"zero nonce": func(a *Attestation) { a.Nonce = Nonce(strings.Repeat("0", 64)) },
	} {
		t.Run(name, func(t *testing.T) {
			a := base
			mutate(&a)
			if _, err := ParseAttestation(mustEncode(t, a)); err == nil {
				t.Fatal("invalid attestation accepted")
			}
		})
	}
	base.ExpiresAt = base.IssuedAt.Add(MaxAttestationLifetime)
	if _, err := ParseAttestation(mustEncode(t, base)); err != nil {
		t.Fatal("exact24hour bound refused", err)
	}
}
