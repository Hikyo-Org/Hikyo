# Transit ML-DSA-65 signing keys

Status: **draft, not operative.** The mechanism is implemented for #1129; the
transit ADR amendment becomes operative only when that implementation merges, and
the public API, CLI and WebUI do not offer the algorithm yet (see
[Exposure](#exposure-hold)). Nothing here authorizes production key creation.

## What exists

| Item | Value |
| --- | --- |
| Algorithm identifier | `mldsa65` (pure ML-DSA-65, FIPS 204, no pre-hash). No other parameter set is accepted |
| Key identity | A new transit key with algorithm `mldsa65`. Algorithm and purpose are fixed at creation. Rotation appends a version of the same algorithm and never converts a key; an existing `ed25519` key stays `ed25519` |
| Operations | `sign`, `verify` (the key's allowed set is a subset of these, fixed at creation) |
| Derivation | The 32-byte version material is run through HKDF-SHA256 under the existing `hikyo/transit-key/v1` info (algorithm, org, project, environment, key, version); the 32 derived bytes are the FIPS 204 seed. The material, not the seed, is sealed under the project DEK as before |
| Public key | The 1952-byte FIPS 204 encoding of the version's public key, stored beside the version and returned as standard base64 by key reads in the same field Ed25519 keys use (service layer today) |
| Signature | `hikyo:v<N>:` followed by unpadded base64url of a 3311-byte payload: the two-byte big-endian algorithm identifier `0x0905` (the TLS code point for ML-DSA-65), then the 3309-byte signature. That is 4415 characters after the prefix |
| Context | The ML-DSA context string is the Transit label `hikyo/transit/sign/v1` (21 bytes, limit 255). Signing is hedged, so two signatures of one message differ |
| Bounds | Unchanged: message 64 KiB, wire value 128 KiB. A signature wire value is about 4.4 KiB |

Ed25519 keys, signatures and wire values are byte for byte unchanged.

## Verifying without Hikyo

An independent verifier needs the version's public key and nothing else from
Hikyo:

1. Split the wire value as `hikyo:v<N>:<b64url>`; decode the payload (strict,
   unpadded) and require 3311 bytes with first two bytes `0x09 0x05`.
2. `mldsa.NewPublicKey(mldsa.MLDSA65(), publicKey)`.
3. `mldsa.Verify(pk, message, payload[2:], &mldsa.Options{Context: "hikyo/transit/sign/v1"})`.

Frozen vectors and a test that does exactly this with the Go standard library
alone are in `internal/crypto/transit_mldsa_vectors_test.go` and
`internal/crypto/testdata/transit_mldsa65_vectors.json`. A second maintained
implementation (for example OpenSSL 3.5 or later) has not been run against them
in CI: NOTRUN.

## Verification rules

Hikyo's own verify derives the algorithm from the key row, never from the
presented signature. A signature from another algorithm, another key, another
version, a wrong identifier, a wrong length or a different context is
`valid=false`, never an error. Compromised versions refuse; versions below
`min_decrypt_version` refuse; retired keys still verify; disabled and
pending-deletion keys refuse. Verification stays local (no private operation) but,
as for every transit operation, an unavailable or unregistered custody provider
refuses it.

## Consumer migration

1. **Verifiers first.** A consumer that will check Hikyo signatures needs an
   ML-DSA-65 verifier and a way to hold a public key and a key version before the
   first key is created (transit ADR D10). A consumer that pins an Ed25519 public
   key cannot read a `mldsa65` signature.
2. **New identity.** Create a new `mldsa65` key. It is not a rotation of an
   Ed25519 key, so a consumer pins the new key name, version and public key.
3. **Overlap.** During migration the signer can produce under both keys. A
   verifier that requires both signatures must be told so out of band: the
   required set belongs to the verifier's own trust state, not to the presented
   value (DEC-03). Transit signs one message with one key per call; combining two
   signatures is the consumer's protocol, not a Transit feature.
4. **Cutoff.** Raise `min_encrypt_version` on the old key to stop new Ed25519
   output, then `min_decrypt_version` to stop accepting old signatures. Raising
   `min_decrypt_version` and trimming is irreversible for the trimmed versions
   (backups keep them).
5. **Compromise.** `compromise` on the `mldsa65` key refuses both signing and
   verifying compromised versions; recovery is rotate, then re-sign.

## Rollback floor and retention

- A binary without this change does not know `mldsa65`. The database CHECK was
  widened by migration 00083, which is roll-forward only. After the first
  `mldsa65` key exists, binary rollback means restoring a backup taken before the
  key; behavior of an older binary against a database that already holds such a
  key was not run: NOTRUN.
- Backups contain sealed `mldsa65` material like any transit material. Trimming
  or destroying a key erases live material only; retained backups keep it.
- A restored backup restores the key set as of that backup. Signatures made under
  versions created later fail with the unknown-version refusal.
- Software custody is the only shipped provider. The mock external provider is
  test-only; no HSM or KMS claim is made.

## Exposure (HOLD)

`TransitAlgorithm` in `api/openapi.yaml` is a closed enum and `v1.0.0` exists, so
the freeze guard (`scripts/ci/check-api-freeze.sh`) refuses adding a value, and
also refuses converting it to an open enum, on every response that carries it. The
API, generated clients, CLI help and WebUI therefore keep their current closed
list, and a real server refuses `mldsa65` at request validation (tested through
`app.Boot` and the CLI). Lifting this needs an owner decision on the
[api-cli-surface](../adr/api-cli-surface.md) freeze rule; see the
[handoff](../handoff/1129-transit-mldsa.md).

## Measured baseline (DEC-17)

See the handoff for the numbers, machine and Go version. They set no threshold.
