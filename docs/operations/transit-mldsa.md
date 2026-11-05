# Transit ML-DSA-65 signing keys

Status: **implemented in #1172 (#1129), operative when #1172 merges.** Once it
merges, the API, the CLI (`hikyo transit key create --algorithm mldsa65`) and the
WebUI create dialog all offer `mldsa65`, under the declared one-time contract
growth in the [api-cli-surface](../adr/api-cli-surface.md) ADR. The merge does not
create a key, enable a signer or activate any trust, and nothing here authorizes
production key creation. Migration 00083 reaches every instance that upgrades
(see [Rollback floor and retention](#rollback-floor-and-retention)). Open items
are registered in the [handoff](../handoff/1129-transit-mldsa.md#remaining-work-and-next-authorized-step).

## What exists

| Item | Value |
| --- | --- |
| Algorithm identifier | `mldsa65` (pure ML-DSA-65, FIPS 204, no pre-hash). No other parameter set is accepted |
| Key identity | A new transit key with algorithm `mldsa65`. Algorithm and purpose are fixed at creation. Rotation appends a version of the same algorithm and never converts a key; an existing `ed25519` key stays `ed25519` |
| Operations | `sign`, `verify` (the key's allowed set is a subset of these, fixed at creation) |
| Derivation | The 32-byte version material is run through HKDF-SHA256 under the existing `hikyo/transit-key/v1` info (algorithm, org, project, environment, key, version); the 32 derived bytes are the FIPS 204 seed. The material, not the seed, is sealed under the project DEK as before |
| Public key, stored | The raw 1952-byte FIPS 204 encoding, beside the version row (as Ed25519 stores its raw 32 bytes) |
| Public key, published | ML-DSA-65: the standard SubjectPublicKeyInfo DER (1974 bytes, OID 2.16.840.1.101.3.4.3.18) in the same base64 field, as DEC-07 specifies. Ed25519 stays the raw 32 bytes. A consumer therefore reads the field by the key's `algorithm`. The conversion is in `crypto.ExposeTransitPublicKey` and the response builder, so create, show, list, rotate and the lifecycle responses all publish it that way. The field is about 2632 base64 characters, which the WebUI shows whole in a read-only text area |
| Signature | `hikyo:v<N>:` followed by unpadded base64url of a 3311-byte payload: the two-byte big-endian algorithm identifier `0x0905` (the TLS code point for ML-DSA-65), then the 3309-byte signature. 4415 characters after the prefix, 4424 in all |
| Context | The ML-DSA context string is the Transit label `hikyo/transit/sign/v1` (21 bytes, limit 255). Signing is hedged, so two signatures of one message differ |
| Signed bytes | The caller's message, unchanged, exactly as for Ed25519 (see [Departure](#departure-from-the-pqc-profiles)) |
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
`internal/crypto/testdata/transit_mldsa65_vectors.json`.

A stock tool needs the `public_key` field, which is base64 DER, wrapped as PEM. The
default `hikyo transit key show` table has no public-key column, so read it from the
JSON output. `n` is a zero-based index into `versions`, not a version number:

```sh
PUBLIC_KEY=$(hikyo transit key show <key> -o json | jq -r '.items[0].versions[n].public_key')
{ echo '-----BEGIN PUBLIC KEY-----'; printf '%s\n' "$PUBLIC_KEY" | fold -w 64
  echo '-----END PUBLIC KEY-----'; } > pub.pem
```

A non-Go check was run locally (not in CI), OpenSSL 4.0.3 on the frozen SPKI, public
key and signature:

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
   `verify` continues. To stop Hikyo accepting its old signatures, **`disable` it** (or
   `schedule-deletion`, after which it is destroyed); a consumer that verifies
   locally with the pinned public key, as in "Verifying without Hikyo", keeps
   accepting them until it drops that key. Disabling is reversible with
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
  verification answers false. There is no fallback algorithm. The same failure
  applies when a response is built: publishing a stored ML-DSA public key parses it,
  so a list or show that touches an `mldsa65` key fails as a whole there (the server
  refuses rather than publish an unparsed key). A deployment on the frozen FIPS module therefore cannot use this algorithm, and an `mldsa65` key created elsewhere makes the list unreadable there.
- The departure below.

## Registered limit: the CLI key list

`hikyo transit key list` reads at most 8 MiB of a response, and the list endpoint is
unpaginated and returns every version's public key. An ML-DSA-65 version costs about
2.7 KB of JSON, so the cap falls at about 3,080 versions in one environment, well
inside the limits of 256 keys and 1,024 versions per key. Four keys at 1,024 versions,
or four hourly-rotating keys left untrimmed for about 32 days, reach it. The CLI then
exits with an internal error ("the server's response did not match the contract").
`hikyo transit key show` still works, since one key tops out at about 2.7 MiB, as do
the API and clients without that cap. Raising a key's floors and then running `trim` avoids it, at a cost: Hikyo then
refuses verification below the floor, and those versions' published public keys are
gone. A key in pending deletion stays listed and refuses `trim`. Open under #1129.

## Departure from the PQC profiles

The [profiles](../security/pqc/profiles.md#encodings-fingerprints-domain-separation-identifiers)
(DEC-07 rows) say algorithm identifiers appear inside the signed bytes and that
the label is also the first length-prefixed field of every signature input. Transit
does not do this for the signed bytes. The owner accepted this on 2026-11-04 (item 10 of the decisions list in [the program handoff](../handoff/1123-post-quantum-program.md)), so the format stays. The register has no row for it:

1. **Signed bytes are the raw message, not a label-prefixed structure.** The label
   is bound only as the ML-DSA context, and the algorithm identifier `0x0905`
   travels outside the signed bytes. Conforming for `mldsa65` would mean signing
   `LP(label) || LP(algorithm) || message`. That is possible: no `mldsa65` key has
   signed anywhere yet (the merge makes the first one possible), the Ed25519 contract is untouched,
   and the transit ADR's "pure Ed25519 over the message bytes" says nothing about
   the ML-DSA input. What is at stake is verifier convenience: every ML-DSA
   verifier, stock tools such as OpenSSL included, would have to rebuild the
   prefix before checking a signature. The security gain looks small, because the
   context already fixes the label and ML-DSA binds the public key into the hash it
   signs; I did not analyze it further. This was an owner trade-off, not an
   impossibility. **The choice is cheap only until the first `mldsa65` key signs**:
   after that, changing the signed input means a second signature format and a
   version the verifiers must learn. The signature format was not changed. Residual
   risk, accepted: what is signed does not name the algorithm, and
   separation from other uses of the same key rests on the context string and the
   key row. The owning ticket for any change is #1129.

Not a departure, but not provided: the `mldsa65:` text form and a key fingerprint.
Transit publishes neither today, so a surface that later wants one must define it
then. The public key itself follows DEC-07 (SPKI when published).

## Exposure and client skew

The owner chose option A for the `/api/v1` freeze (2026-11-04). `TransitAlgorithm`
in `api/openapi.yaml` now lists `mldsa65` (merged with #1212, closed list
`MLDSA65EnumGrowth` in `api/freeze.go`, exactly 20 findings), and #1172 adds the
mechanism behind it. The algorithm is reachable through the API, the generated
clients, the CLI and the WebUI. Any other ML-DSA name (`mldsa44`, `mldsa87`,
`MLDSA65`) is still refused by the contract, and PKI stays behind
`service.PKI.MLDSAEnabled`, which production wiring leaves false.

**The break happens only once an operator creates an `mldsa65` key.** A consumer
that validates responses with the `v1.0.0` generated Zod schemas then fails the
whole key list of that environment, Ed25519 keys included, and fails a single
show, create or rotate response for that key. The Go client and the CLI decode it
because the generated enum type is a plain string, and the `v1.0.0` TypeScript SDK
has no response validator. The frozen-client skew lane asserts the Zod and SDK part
and the current Go decode; it has no CLI step, and no test runs a `v1.0.0` CLI
(`TestFrozenGeneratedClientAgainstCurrentServer`: the `v1.0.0` Zod list schema
rejects a list holding `mldsa65` on `items.N.algorithm` only, while the `v1.0.0`
SDK creates, signs, verifies, rotates and lists the key). The WebUI ships in the
same artifact as the server, so only a browser tab loaded before the upgrade is
affected. Before creating the first `mldsa65` key, check every consumer that
parses Transit responses with a closed schema.

## Measured baseline (DEC-17)

See the handoff for the numbers, machine and Go version. They set no threshold.
