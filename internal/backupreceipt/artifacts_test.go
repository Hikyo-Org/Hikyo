package backupreceipt

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
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
		"wrong format":     func(r *Receipt) { r.Format = "backup-receipt/v9" },
		"v2 with x25519":   func(r *Receipt) { r.Format = HybridReceiptFormat },
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

func hybridFingerprints(count int) []string {
	out := make([]string, count)
	for i := range out {
		out[i] = fmt.Sprintf("%s%02x%s", backup.FingerprintPrefixHybrid, i, strings.Repeat("7", 62))
	}
	return out
}

func hybridReceipt(count int) Receipt {
	r := fixtureReceipt()
	r.Format = HybridReceiptFormat
	r.Snapshot.RecipientFingerprints = hybridFingerprints(count)
	return r
}

// A historical v1 receipt keeps its exact meaning: the classical namespace and
// the v1 schema validate unchanged and report the classical algorithm.
func TestHistoricalClassicalReceiptValidatesUnchanged(t *testing.T) {
	// Bytes of a v1 receipt written before hybrid support existed.
	const historical = `{"format":"backup-receipt/v1","ciphertext_sha256":"305531dcc50ebca31cf1d5b31e9fc76ed51f66b3b6dd5a030c6539ae6532f979","ciphertext_bytes":256,"encrypted_manifest_sha256":"76ce83a4af39da72cf92e8f55b9e5535f49889bc1d89094a135266ac408f7f78","snapshot":{"authority":"applied-ledger/v1","backup_id":"1111111111111111111111111111111111111111111111111111111111111111","instance_id":"ins_22222222222222222222222222222222","engine":"sqlite","source_identity":{"release":{"profile":"stable/v1","version":"1.0.0","sequence":1,"commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","compatibility_sha256":"fa528d3adef61d199a8348ffb48ef0143603ea2e0ad007121c39edc6e3aeb567","manifest_sha256":"05b3abf2579a5eb66403cd78be557fd860633a1fe2103c7642030defe32c657f"}},"source_schema_sha256":"bcd0a19a7c715e7fc24054fa7d99863802d498ae649d2227974d4d6e03e4f2c0","migration_set_sha256":"32a298528a44fb0b4ac5d18d40b22e37337cbb3b010fbc2b872f1b9eecff4349","restore_epoch":0,"recovery_incarnation":"3333333333333333333333333333333333333333333333333333333333333333","source_generation":1,"route_generation":2,"created_at":"2026-09-05T00:00:00Z","recipient_fingerprints":["age-x25519-sha256:4444444444444444444444444444444444444444444444444444444444444444"]}}`
	parsed, err := ParseReceipt([]byte(historical))
	if err != nil {
		t.Fatalf("historical v1 receipt refused: %v", err)
	}
	if algorithm, err := parsed.Snapshot.RecipientAlgorithm(); err != nil || algorithm != backup.AlgorithmX25519 || parsed.Format != "backup-receipt/v1" {
		t.Fatalf("historical receipt classified as %q (%v), format %q", algorithm, err, parsed.Format)
	}
}

func TestHybridReceiptSchemaIsBoundToItsAlgorithm(t *testing.T) {
	if _, err := ParseReceipt(mustEncode(t, hybridReceipt(1))); err != nil {
		t.Fatalf("hybrid v2 receipt refused: %v", err)
	}
	if _, err := ParseReceipt(mustEncode(t, hybridReceipt(backup.MaxHybridRecipients))); err != nil {
		t.Fatalf("32 hybrid fingerprints refused: %v", err)
	}
	for name, mutate := range map[string]func(*Receipt){
		"33 hybrid fingerprints": func(r *Receipt) {
			r.Snapshot.RecipientFingerprints = hybridFingerprints(backup.MaxHybridRecipients + 1)
		},
		"hybrid under v1": func(r *Receipt) { r.Format = ReceiptFormat },
		"mixed inventory": func(r *Receipt) {
			r.Snapshot.RecipientFingerprints = append(r.Snapshot.RecipientFingerprints, backup.FingerprintPrefixX25519+strings.Repeat("4", 64))
		},
		"unknown algorithm": func(r *Receipt) {
			r.Snapshot.RecipientFingerprints = []string{"age-mlkem1024-sha256:" + strings.Repeat("4", 64)}
		},
		"relabeled classical key": func(r *Receipt) {
			r.Snapshot.RecipientFingerprints = []string{"age-hybrid-mlkem768x25519-sha256:" + strings.Repeat("4", 63)}
		},
		"uppercase digest": func(r *Receipt) {
			r.Snapshot.RecipientFingerprints = []string{backup.FingerprintPrefixHybrid + strings.Repeat("A", 64)}
		},
		"duplicate": func(r *Receipt) {
			r.Snapshot.RecipientFingerprints = append(r.Snapshot.RecipientFingerprints, r.Snapshot.RecipientFingerprints[0])
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := hybridReceipt(2)
			mutate(&r)
			if _, err := ParseReceipt(mustEncode(t, r)); err == nil {
				t.Fatal("invalid hybrid receipt accepted")
			}
		})
	}
	// A hybrid fingerprint in a v1 receipt is refused by the v1 schema itself.
	classical := fixtureReceipt()
	classical.Snapshot.RecipientFingerprints = hybridFingerprints(1)
	if _, err := ParseReceipt(mustEncode(t, classical)); err == nil {
		t.Fatal("a v1 receipt accepted a hybrid fingerprint")
	}
}
