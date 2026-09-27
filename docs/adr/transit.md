# Hikyo transit: versioned cryptographic operations (ADR, decision locked 2026-09-26)

> **Status: decision locked; operative upon the implementing PR merging.** The
> owner approved redrawing the map for
> [#156](https://github.com/Hikyo-Org/Hikyo/issues/156) on 2026-09-26 and chose
> the recommendations of the issue's implementation handoff. Both blockers
> ([#75](https://github.com/Hikyo-Org/Hikyo/issues/75) key rotation and
> [#146](https://github.com/Hikyo-Org/Hikyo/issues/146) multi-node HA) are
> merged. This document is the normative contract; the declared amendments to
> [mvp-boundary.md](./mvp-boundary.md), [encryption-model.md](./encryption-model.md),
> [permission-model.md](./permission-model.md) and
> [tenant-isolation.md](./tenant-isolation.md) are pointers to it. The
> cross-provider adversarial review normally required by the
> [oss-mechanics.md](./oss-mechanics.md) amendment procedure is owed and is
> recorded as open in [the handoff](../handoff/156-transit.md).

## Context

The MVP boundary listed "Encryption-as-a-service API" as an explicit non-goal
with no trigger, returnable only through a new map. The owner redrew that map:
applications that already trust Hikyo with their configuration also want
policy-bound cryptography (encrypt a column, sign a webhook, MAC a token,
envelope-encrypt a blob) without holding long-lived key material themselves.
What they must never get is a second, weaker key-management system bolted
beside the first. So transit reuses every existing boundary: the identity
chokepoint, the grant model, the key hierarchy and its rotation machinery, the
audit closure and the multi-node coordination.

Transit is **not** a Vault or OpenBao replacement. It is one bounded surface:
named keys, a closed algorithm set, and a closed operation set. No PKI, no SSH
certificates, no arbitrary crypto API, no key import, no key export.

## Decisions

### D1. The managed key is environment-scoped

A **transit key** belongs to exactly one `(org, project, environment)` and has
a name unique among that environment's non-destroyed keys. The name grammar is
`[a-z0-9][a-z0-9._-]{0,62}` because it travels in URL paths and CLI arguments.
Names are immutable (there is no rename), so a caller that addresses a key by
name addresses the same key for its whole life.

Environment scope is what makes workloads first-class callers: a workload
credential is granted only at environment depth (permission-model machine
depths), so a project-scoped key would be unreachable by the principals that
most need it. A production key and a development key are different keys, which
is the separation operators expect anyway.

### D2. Closed algorithm set, immutable purpose

| Algorithm | Purpose | Operations |
|---|---|---|
| `xchacha20-poly1305` | encryption | `encrypt`, `decrypt`, `rewrap`, `datakey`, `datakey-plaintext` |
| `ed25519` | signing | `sign`, `verify` |
| `hmac-sha256` | message authentication | `hmac`, `hmac-verify` |

The algorithm, and therefore the purpose, is fixed at creation. The
**allowed-operations** set is chosen at creation as a subset of the
algorithm's operations and is immutable; the default is every operation of the
algorithm except `datakey-plaintext`, which must be named explicitly.

No key serves two purposes. A signing key never encrypts and an encryption key
never MACs, so a caller who can reach one operation cannot turn the key into an
oracle for another.

### D3. Key material and custody

Each **key version** has 32 bytes of fresh material from `crypto/rand`. The
per-operation key is derived, never the material itself:

```
opKey = HKDF-SHA256(material, info = LP("hikyo/transit-key/v1", algorithm,
                                         org_id, project_id, env_id,
                                         transit_key_id, version))
```

`LP` is the encryption-model ADR's injective length-prefixed encoding. For
`ed25519` the derived bytes are the RFC 8032 seed; the public key is public
metadata stored beside the version and returned by ordinary reads.

**Custody is a provider seam** chosen immutably at key creation:

- **`software`** (the only production provider in this release): the material
  is sealed under the owning **project DEK** as a `project_field` envelope
  bound to `owner_table=transit_key_versions`, the version row id, field tag
  `material`, and the environment and key ids. Consequences, all intended: the
  material participates in `rotate-dek`, `reencrypt`, `rotate-master-key` and
  project crypto-shred with no new machinery, and it lands under the project
  DEK, never the instance DEK (encryption-model CI invariant 16).
- **external custody** (HSM, KMIP, cloud KMS): the provider holds the material
  and Hikyo stores only an opaque reference. The seam is operation-level
  (create version, encrypt, decrypt, sign, MAC, destroy, availability), so a
  hardware provider never has to release material. This release ships the
  interface and an in-memory **mock external provider used only in tests**; a
  conformance suite proves the software and mock providers have identical
  authorization, versioning, failure and audit semantics.

**An unavailable custody provider fails closed.** A key whose custody provider
is not registered, or whose provider reports itself unavailable, refuses every
operation with a retryable unavailable error. There is no fallback to software
custody under any condition: the custody kind is read from the key row, never
chosen per request.

### D4. Ciphertext, signature and MAC formats

Transit ciphertext is an encryption-model envelope record of the new kind
**`transit`** (kind byte 8) under `opKey`:

```
header        = the unchanged encryption-model header (format, kind, algorithm,
                wrapping_key_id = transit key id, wrapping_key_version = version,
                nonce)
AAD (transit) = LP(org_id) ‖ LP(project_id) ‖ LP(env_id) ‖ LP(transit_key_id)
                ‖ LP(context)
wire          = "hikyo:v" ‖ decimal(version) ‖ ":" ‖ base64url(record)
```

`context` is optional caller-supplied associated data (at most 1 KiB). A
ciphertext opens only under the same key, version, environment and context:
moving it to another key, environment or project, or presenting another
context, is a decrypt failure. The textual version in the wire prefix must equal
the authenticated header version.

Signatures are pure Ed25519 over the message bytes; MACs are HMAC-SHA256 under
`opKey` over the message bytes. Both travel as
`"hikyo:v" ‖ decimal(version) ‖ ":" ‖ base64url(bytes)`. MAC verification is
constant-time.

**Data keys** are 16, 32 or 64 random bytes (128, 256 or 512 bits; 256 by
default) returned wrapped as a transit ciphertext under the key. The plaintext
data key is returned only for `datakey-plaintext`, which the key must allow
explicitly (D2). It is display-once: Hikyo never stores it.

### D5. Versions and rotation

Rotation appends version `latest + 1` under a compare-and-swap on the key row,
so concurrent rotations resolve to one winner and one retryable conflict, never
a torn or duplicated version.

Each key carries `min_encrypt_version` and `min_decrypt_version` with
`1 ≤ min_decrypt_version ≤ min_encrypt_version ≤ latest_version`.

- **Producing** operations (`encrypt`, `datakey*`, `sign`, `hmac`, and the output
  side of `rewrap`) use the latest version unless the caller names an explicit
  version inside `[min_encrypt_version, latest]`.
- **Consuming** operations (`decrypt`, `verify`, `hmac-verify`, the input side of
  `rewrap`) accept only versions `≥ min_decrypt_version`.
- **`rewrap`** decrypts and re-encrypts under the latest version in one server
  step. The plaintext is never returned, logged or audited.
- **`trim`** permanently deletes versions below `min_decrypt_version`, erasing
  their material. It is irreversible and ordinary backups still contain the
  erased versions (D8).

An optional **rotation period** (at least one hour) makes the hourly scheduler
append a version when the latest version is older than the period. At most
1024 versions may exist at once; rotation beyond that bound is refused until
the operator raises `min_decrypt_version` and trims.

### D6. Lifecycle, fail closed

| State | Producing ops | Consuming ops | Reachable from |
|---|---|---|---|
| `active` | yes | yes | create, `enable` |
| `retired` | no | yes | `retire` from active/disabled |
| `disabled` | no | no | `disable` from active/retired, `cancel-deletion` |
| `pending-deletion` | no | no | `schedule-deletion` from active/retired/disabled |
| `destroyed` | no | no | the scheduler, after `deletion_after` |

`schedule-deletion` takes a delay of at least 24 hours and at most 90 days
(default 7 days). `cancel-deletion` returns the key to `disabled`, never
straight to `active`, so reviving a key is two deliberate acts. It is accepted
only while the delay is running: once `deletion_after` has passed, the key is
committed to destruction and no transition revives it, because the purge may
already have destroyed part of its external material. When the delay
elapses, the scheduler erases every version's material (software custody) or
asks the external provider to destroy it, and leaves a `destroyed` tombstone so
the id is never reused. An external provider that is unavailable at purge time
leaves the key `pending-deletion` and retries; the key stays unusable either
way.

**Compromise** is orthogonal to state: `compromise` records
`compromised_through_version = latest`. Producing operations then refuse every
version at or below it, so encryption and signing resume only after a rotation.
`verify` and `hmac-verify` refuse compromised versions (an attacker holding the
material can forge), while `decrypt` and `rewrap` still accept them so data can
be recovered and moved off the compromised version. The recovery path is always
explicit: rotate, rewrap, then raise `min_decrypt_version` and trim.

Every state check reads the key row inside the operation's own transaction.
There is no cache of key state, so a disable, compromise or deletion on one
node takes effect for the next request on every node (#146), and a restarted
node has nothing to rebuild.

### D7. Authorization

Two new capability atoms (permission-model amendment):

- **`crypto-use`**, deepest level environment. It authorizes the data-plane
  operations of D2. It is on the **workload** and **automation** machine
  allowlists unconditionally, because transit exists for workloads, and it is
  never implied by `read` or `reveal`.
- **`crypto-manage`**, deepest level project. It authorizes creating keys,
  changing their configuration, rotation, lifecycle transitions and trim. It is
  held by humans only and is on no machine allowlist.

Neither atom is added to any role template: templates are the
permission-model ADR's verbatim table, so both atoms are granted explicitly.

Key inspection (metadata, versions, public keys, never material) rides
`read@environment` and is audited-none, like lease inspection.

**Scoped caller grants.** A key may carry up to 64 per-key caller entries, each
a principal id and a subset of the key's allowed operations. An empty list
admits every principal holding `crypto-use` on the environment. A non-empty list
admits only the listed principals for their listed operations. Caller entries
only ever **narrow** authority: `crypto-use` is still required, and an entry
cannot name an operation the key does not allow.

A policy refusal (state, version window, compromise, caller list, operation
not allowed) is a distinct conflict or forbidden answer only after the caller
has passed the chokepoint for the environment; before that, a key the caller
cannot reach answers exactly like one that does not exist.

### D8. Audit, logs and non-export

Every transit operation writes one `transit.operation` event recording the
**decision only**: key id, key version, operation, outcome, and the byte sizes
of input and output. The closed schema has no field that can carry plaintext,
ciphertext, context, signature, MAC, digest, data key or key material, and the
audit forbidden-content test pins that. Management events record key id,
versions, states and policy values.

There is **no export path** for key material in any form: no API, no CLI verb,
no backup diagnostic, no log line, no audit payload, no process argument.
`exportable` is a key attribute fixed to `false` in this release; a request for
an exportable key is refused. Plaintext inputs travel only in request bodies
(or CLI stdin and files), never in URLs or process arguments.

**Erasure is honest.** Trimming or destroying a software-custody key erases the
live material only. Every retained backup contains it, exactly like any other
DEK-sealed field (encryption-model § Erasure). Restoring a backup restores the
key as of that backup, including versions later trimmed or keys later
destroyed; ciphertext produced under versions created after the backup fails to
decrypt with an explicit unknown-version error. External custody is the path to
real crypto-shredding, because its material never enters a backup.

### D9. Bounds and rate limits

| Bound | Value |
|---|---|
| plaintext, message or decrypted output | 64 KiB |
| context | 1 KiB |
| ciphertext, signature or MAC input string | 128 KiB |
| keys per environment | 256 |
| live versions per key | 1024 |
| caller entries per key | 64 |
| data key sizes | 128, 256, 512 bits |

Data-plane operations are a named expensive-path budget category, `transit`:
600 per minute per principal and 6000 per minute per organisation. Under
multi-node HA the counters are the #146 installation-wide admission counters,
so node hopping cannot multiply the allowance. Management operations ride the
authenticated-API budget.

### D10. Surfaces

- API under `.../environments/{environment}/transit-keys`, one route per
  operation. Bodies carry base64 inputs.
- CLI `hikyo transit key create|list|show|configure|rotate|disable|enable|retire|compromise|schedule-deletion|cancel-deletion|trim`
  and `hikyo transit encrypt|decrypt|rewrap|datakey|sign|verify|hmac|hmac-verify`,
  reading inputs from stdin or files.
- The client SDK is the generated TypeScript client and the generated Go
  models; there is no bespoke SDK.
- Label-free gauges for live keys, keys due for rotation and keys pending
  deletion, guarded by a known-flag gauge like the dynamic-secret gauges.

## Rejected

- **Tier-3 keyring keys per managed key.** The key hierarchy serializes tier-3
  creation against master rotation; one hierarchy row per customer key would
  make master rotation proportional to customer keys and put customer key
  lifecycle inside the fenced key hierarchy. Sealing material under the project
  DEK gets rotation, re-encryption and crypto-shred for free.
- **Exportable keys, key import, BYOK.** Each is a separate amendment with its
  own custody story.
- **A generic operation endpoint.** One route per operation keeps each
  operation's request and response schema closed.
- **Software fallback when external custody is down.** It would silently move
  material custody, which is the one decision custody exists to make.

## CI-enforced invariants

1. Known-answer vectors for every algorithm and the derivation are frozen.
2. Transplant: a ciphertext moved to another key, version, environment,
   project or context fails to decrypt; a header or wire-prefix version edit
   fails.
3. Fuzz targets cover the wire parsers and never panic.
4. The chokepoint test still holds: no primitive import outside
   `internal/crypto`.
5. The custody conformance suite passes identically for software and mock
   external custody, and an unavailable external provider never produces
   output.
6. The audit schema for `transit.operation` rejects any payload carrying
   material, plaintext, ciphertext, context or digests.
7. Both-engine end-to-end coverage exercises every operation, every lifecycle
   refusal and every negative authorization path.
