# Transit ML-DSA-65 signing keys

Status: **draft, not operative.** The mechanism is implemented for #1129; the
transit ADR amendment becomes operative only when that implementation PR (#1172)
merges, and even then the public API, CLI and WebUI do not offer the algorithm
(see [Exposure](#exposure-hold)). Nothing here authorizes production key creation.

## What exists

| Item | Value |
| --- | --- |
| Algorithm identifier | `mldsa65` (pure ML-DSA-65, FIPS 204, no pre-hash). No other parameter set is accepted |
| Key identity | A new transit key with algorithm `mldsa65`. Algorithm and purpose are fixed at creation. Rotation appends a version of the same algorithm and never converts a key; an existing `ed25519` key stays `ed25519` |
| Operations | `sign`, `verify` (the key's allowed set is a subset of these, fixed at creation) |
| Derivation | The 32-byte version material is run through HKDF-SHA256 under the existing `hikyo/transit-key/v1` info (algorithm, org, project, environment, key, version); the 32 derived bytes are the FIPS 204 seed. The material, not the seed, is sealed under the project DEK as before |
| Public key, stored | The raw 1952-byte FIPS 204 encoding, beside the version row (as Ed25519 stores its raw 32 bytes) |
| Public key, published | ML-DSA-65: the standard SubjectPublicKeyInfo DER (1974 bytes, OID 2.16.840.1.101.3.4.3.18) in the same base64 field, as DEC-07 specifies. Ed25519 stays the raw 32 bytes. A consumer therefore reads the field by the key's `algorithm`. The conversion is in `crypto.ExposeTransitPublicKey` and the response builder; no public route returns an ML-DSA key yet |
| Signature | `hikyo:v<N>:` followed by unpadded base64url of a 3311-byte payload: the two-byte big-endian algorithm identifier `0x0905` (the TLS code point for ML-DSA-65), then the 3309-byte signature. 4415 characters after the prefix, 4424 in all |
| Context | The ML-DSA context string is the Transit label `hikyo/transit/sign/v1` (21 bytes, limit 255). Signing is hedged, so two signatures of one message differ |
| Signed bytes | The caller's message, unchanged, exactly as for Ed25519 (see [Departures](#departures-from-the-pqc-profiles-for-the-owner-to-accept)) |
| Bounds | Unchanged: message 64 KiB, wire value 128 KiB |

Ed25519 keys, signatures and wire values are byte for byte unchanged.

## Verifying without Hikyo

An independent verifier needs the version's public key and nothing else from
Hikyo:

1. Split the wire value as `hikyo:v<N>:<b64url>`; decode the payload (strict,
   unpadded) and require 3311 bytes with first two bytes `0x09 0x05`.
2. Parse the key: `x509.ParsePKIXPublicKey` on the published SPKI, or
   `mldsa.NewPublicKey(mldsa.MLDSA65(), raw)` on the raw encoding.
3. `mldsa.Verify(pk, message, payload[2:], &mldsa.Options{Context: "hikyo/transit/sign/v1"})`.

Frozen vectors and a test that does exactly this with the Go standard library
alone are in `internal/crypto/transit_mldsa_vectors_test.go` and
`internal/crypto/testdata/transit_mldsa65_vectors.json`. A non-Go check was run
locally (not in CI), OpenSSL 4.0.3 on the frozen SPKI, public key and signature:

```sh
openssl pkeyutl -verify -pubin -inkey pub.pem -in msg.bin -sigfile sig.bin \
  -pkeyopt context-string:hikyo/transit/sign/v1     # Signature Verified Successfully
# the same without the option, with another context string, or with a tampered
# message: Signature Verification Failure
```

## Verification rules

Hikyo's own verify derives the algorithm from the key row, never from the
presented signature. A well-formed transit value whose signature is wrong for
this key, algorithm, context or message answers `valid=false` (another key's or
another algorithm's signature, a wrong identifier, a wrong length, a flipped
byte). Everything else is a refusal, not `valid=false`: a malformed transit
value or oversized input (invalid input), a version the key does not have, a
version below `min_decrypt_version` or a compromised version (version and
compromised conflicts), and a key in the wrong state. Retired keys still verify;
disabled and pending-deletion keys refuse. Verification stays local (no private
operation) but, as for every transit operation, an unavailable or unregistered
custody provider refuses it.

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
4. **Ending classical signing.** The version floors do **not** do this. They only
   cut off versions older than the latest: `min_encrypt_version` and
   `min_decrypt_version` cannot exceed `latest_version`, so on a key with one
   version both already sit at latest, the key still signs under `v1` and its old
   signatures still verify (pinned by `runTransitFloorsDoNotEndSigning`). To stop
   new Ed25519 signatures, **`retire` the old key**: `sign` then refuses and
   `verify` continues. To stop accepting its old signatures, **`disable` it** (or
   `schedule-deletion`, after which it is destroyed). Disabling is reversible with
   `enable`; destruction is not. Raising the floors plus `trim` is for dropping
   older versions of a key that has been rotated.
5. **Compromise.** `compromise` on the `mldsa65` key refuses both signing and
   verifying compromised versions; recovery is rotate, then re-sign.

## Rollback floor and retention

- Migration 00083 is roll-forward only and applies to **every** instance that
  upgrades to a binary containing it, whether or not an `mldsa65` key is ever
  created, because it rebuilds the `transit_keys` table. Merging this change
  therefore moves the rollback floor for everyone: the downgrade path is
  restoring an archive taken **before migration 83** with a compatible binary
  (see [Upgrades](../site/src/content/docs/docs/upgrades.mdx), which requires a
  pre-upgrade archive), not running a down migration.
- Whether a release binary older than this change refuses a database already at
  migration 83 is not established. `migrate.Check` would refuse it but has no
  production caller, and the base `app.Boot` refusal observed in development was
  for custody reasons, not schema: NOTRUN.
- Backups contain sealed `mldsa65` material like any transit material. Trimming
  or destroying a key erases live material only; retained backups keep it.
- A restored backup restores the key set as of that backup. Signatures made under
  versions created later fail with the unknown-version refusal.
- Software custody is the only shipped provider. The mock external provider is
  test-only; no HSM or KMS claim is made.

## Residual risks

- **Key memory is not erased.** `crypto/mldsa` keeps the seed and the expanded
  private key inside the library and offers no way to zero them. Every sign
  derives a key, uses it and drops the reference, but the bytes stay in the Go heap
  until the memory is reused, unlike the AEAD, HMAC and Ed25519 key bytes that
  `Destroy` zeroes. A heap or core dump taken shortly after a signing operation
  can contain the expanded key. Owner: #1129 (this change). Recheck trigger: every
  Go minor release and any change to `crypto/mldsa` that adds an erase method; the
  `shortcut:` comment in `TransitKey.Destroy` marks the place.
- **Library availability.** `crypto/mldsa` is unavailable under the frozen FIPS
  module v1.0.0, so key opening fails there: creation and signing refuse, and
  verification answers false. There is no fallback algorithm.
- The two departures below.

## Departures from the PQC profiles, for the owner to accept

The [profiles](../security/pqc/profiles.md#encodings-fingerprints-domain-separation-identifiers)
(DEC-07 rows) say algorithm identifiers appear inside the signed bytes and that
the label is also the first length-prefixed field of every signature input. Transit
does not do this, and the register has no row accepting the difference:

1. **Signed bytes are the raw message, not a label-prefixed structure.** The label
   is bound only as the ML-DSA context, and the algorithm identifier `0x0905`
   travels outside the signed bytes. Conforming would mean signing
   `LP(label) || LP(algorithm) || message`, so every verifier would have to rebuild
   that input; Transit's contract (ADR D4, "over the message bytes") is that any
   verifier holding the public key checks the caller's own bytes. That is a change
   to what callers verify, so I did not make it. Residual risk: cross-algorithm
   confusion is closed by the key row (the verifier supplies the algorithm and
   the public key fixes the parameter set), but what is signed does not name the
   algorithm, and separation from other uses of the same key rests on the ML-DSA
   context string alone. The owner decides; the owning ticket for any change is
   #1129.
2. **Algorithm identifier lives in the payload, not in a fingerprint or key
   identity string.** Transit publishes no `mldsa65:` text form and no
   fingerprint, so the DEC-07 fingerprint and text-form rules have nothing to
   attach to here. The public key itself does follow DEC-07 (SPKI when published).
   Residual risk: a surface that later wants a fingerprint of a Transit key must
   define it then.

## Exposure (HOLD)

`TransitAlgorithm` in `api/openapi.yaml` is a closed enum and `v1.0.0` exists, so
the freeze guard (`scripts/ci/check-api-freeze.sh`) refuses adding a value, and
also refuses opening the enum, on every response that carries it. The API,
generated clients, CLI help and WebUI therefore keep their current closed list,
and a real server refuses `mldsa65` at request validation (tested through
`app.Boot` and the CLI). Lifting this is an owner decision on the
[api-cli-surface](../adr/api-cli-surface.md) freeze rule; the options and their
consequences are in the [handoff](../handoff/1129-transit-mldsa.md#decision-needed-from-the-owner-the-freeze).

## Measured baseline (DEC-17)

See the handoff for the numbers, machine and Go version. They set no threshold.
