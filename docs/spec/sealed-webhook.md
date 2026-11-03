# Sealed webhook receiver protocol, version 1

Status: normative for the `sealed-webhook` deployment adapter ([#163](https://github.com/Hikyo-Org/Hikyo/issues/163)).
Owning ADR: [deployment-adapter.md](../adr/deployment-adapter.md), declared amendment of 2026-09-26.
Reference implementation: `internal/crypto/sealedhook` (all cryptography),
`internal/adapter/sealedwebhook` (sender), `internal/sealedreceiver` and
`cmd/hikyo-receiver-example` (receiver).

## 1. Purpose and trust boundary

The sealed webhook adapter pushes secret values from Hikyo to a generic HTTPS
receiver without turning a webhook into a plaintext exfiltration path.

- **Endpoints are instance-admin configuration.** They live in the
  operator-owned registry file named by `HIKYO_SEALED_WEBHOOK_FILE`. A tenant
  can create a `sealed-webhook` adapter only for an origin already present and
  activated there; a tenant never supplies a URL that Hikyo dials.
- **Activation requires an out-of-band fingerprint.** The receiver operator
  computes the fingerprint (section 6) and reads it to the instance admin over
  an independent channel; the admin types it into `confirmed_fingerprint`.
  Trust on first use is refused: a missing or mismatched value refuses boot.
- **Nothing a receiver returns can move the boundary.** There is no callback
  URL, redirects are refused, the path is fixed, the destination address is
  re-resolved and re-vetted at every dial, and the only response Hikyo acts on
  is an acknowledgement signed by the pinned acknowledgement key.
- **Values exist only inside recipient-encrypted ciphertext.** They never
  appear in URLs, headers, envelope metadata, logs, audit payloads, metrics,
  retries, diagnostics, or acknowledgement bodies.

## 2. Keys

| Key | Holder | Algorithm | Text form | Pinned by |
|---|---|---|---|---|
| Recipient | receiver (secret), registry (public) | age X25519 | `age1...` | instance admin, in the fingerprint |
| Acknowledgement | receiver (secret), registry (public) | Ed25519 | `ed25519:` + 64 lowercase hex | instance admin, in the fingerprint |
| Envelope signing | Hikyo instance (secret), receiver (public) | Ed25519 | PKCS#8 PEM file; public `ed25519:` + hex | receiver operator |

A key id is `sha256:` followed by the lowercase hex SHA-256 of the raw 32-byte
Ed25519 public key.

The instance signing key file is referenced by `signing_key_file` and must be
mode `0600` (no group or other access) on Unix. It can be produced with
`openssl genpkey -algorithm ed25519`.

## 3. Transport

- `POST {origin}/hikyo/v1/sync`, where `origin` is an exact `https://host[:port]`
  with no userinfo, path, query, or fragment. No other method or path exists.
- Request `Content-Type: application/vnd.hikyo.sealed-envelope.v1+json` and
  `Accept: application/vnd.hikyo.sealed-ack.v1+json`; no other application
  header is sent (no credential, no idempotency header).
- TLS 1.2 or newer, system roots, no proxy (ambient proxy variables are not
  consulted), redirects refused.
- Every dial resolves the host and refuses the connection if **any** answer is
  private, loopback, link-local, metadata, or otherwise non-public, unless the
  operator's `HIKYO_ADAPTER_EGRESS_POLICY_FILE` grants that CIDR to this exact
  origin. The vetted address is the address dialled (DNS rebinding safe).
- Round-trip timeout 1 to 10 seconds; acknowledgement body bound 1 to 65536
  bytes, both set per endpoint by the instance admin.
- A receiver answers `200` with `Content-Type:
  application/vnd.hikyo.sealed-ack.v1+json` and a signed acknowledgement. Any
  other status, media type, size, or shape is "no acknowledgement".

## 4. Envelope

JSON object with exactly these members (unknown members are refused):

| Member | Type | Rule |
|---|---|---|
| `v` | integer | `1` |
| `target_id` | string | endpoint id, `[a-z0-9][a-z0-9._-]{0,62}` |
| `instance_id` | string | Hikyo instance id, same syntax |
| `namespace` | string | tenant-chosen receiver namespace, same syntax |
| `route` | string | Hikyo adapter target id, or `probe` |
| `generation` | integer | endpoint trust-boundary generation, >= 1 |
| `source` | object | `{org, project, environment, revision}`; all zero for `probe`, otherwise all set and `revision` >= 1 |
| `op` | string | `upsert`, `prune`, or `probe` |
| `names` | array | `[{surface, name}]`; empty for `probe`, 1..256 for others; `surface` is `secret` or `variable`; `name` matches `[A-Z_][A-Z0-9_]{0,255}`; no duplicates |
| `ciphertext` | string | standard base64 of the age ciphertext, at most 1 MiB decoded |
| `idempotency_key` | string | 64 lowercase hex |
| `issued_at`, `expires_at` | string | UTC `YYYY-MM-DDTHH:MM:SSZ`; `0 < expires_at - issued_at <= 5m` |
| `key_id` | string | id of the Hikyo signing key |
| `sig` | string | base64 Ed25519 signature (64 bytes) over the signed bytes |

`names` are canonical effective names, public-class identifiers exactly like
the ownership ledger's. No value, credential, or value-derived digest appears
in any member other than `ciphertext`.

**Signed bytes.** The concatenation of length-prefixed fields (each a uint32
big-endian byte length then the bytes; integers as base-10 ASCII), in order:
the domain string `hikyo/sealed-webhook/envelope/v1`, `v`, `target_id`,
`instance_id`, `namespace`, `route`, `generation`, `source.org`,
`source.project`, `source.environment`, `source.revision`, `op`, the count of
`names`, then `surface` and `name` for each entry, the raw ciphertext bytes,
`idempotency_key`, `issued_at`, `expires_at`, `key_id`. This is the same
injective encoding as the encryption-model AAD schema. A frozen known-answer
vector lives in `internal/crypto/sealedhook/sealedhook_test.go`.

**Payload.** The ciphertext decrypts to JSON:

```json
{"idempotency_key": "<same as envelope>", "binding": "<tenant adapter credential>",
 "values": [{"surface": "secret", "name": "DB_PASSWORD", "value": "..."}]}
```

`values` mirrors `names` exactly (same order) for `upsert` and is empty for
`prune` and `probe`. `binding` is the tenant's write-only adapter credential;
the receiver uses it to authorize the namespace. Because it travels only
inside the ciphertext, no bearer credential crosses the wire in clear.

## 5. Acknowledgement

| Member | Rule |
|---|---|
| `v` | `1` |
| `envelope_digest` | `sha256:` + hex SHA-256 of the exact request body bytes |
| `idempotency_key` | the envelope's |
| `status` | `applied`, `already_applied`, `conflict`, or `rejected` |
| `reason` | absent for success; otherwise `[a-z0-9_]{1,64}` (no free text) |
| `key_id` | id of the acknowledgement key |
| `sig` | Ed25519 over length-prefixed `hikyo/sealed-webhook/ack/v1`, `v`, `envelope_digest`, `idempotency_key`, `status`, `reason`, `key_id` |

Defined reasons: `unauthorized` (binding refused for the namespace),
`exists_unowned` (the name exists and this route does not own it),
`stale_revision`, `generation_mismatch`, `wrong_target`. Receivers may add
reasons within the syntax; Hikyo treats unknown ones as a rejection.

## 6. Fingerprint

`sha256:` + hex SHA-256 over length-prefixed `hikyo/sealed-webhook/fingerprint/v1`,
canonical origin, canonical age recipient, canonical acknowledgement key text,
generation. Changing the host, port, recipient key, acknowledgement key, or
generation changes the fingerprint and therefore requires re-confirmation.
`hikyo-receiver-example fingerprint` prints it.

The ledger's destination id for a target is derived from the fingerprint and
namespace. When the fingerprint changes, every target bound under the old one
fails closed with a destination-id mismatch before any request is sent, and
an operator must re-bind it deliberately: remove the target (prune through
the old boundary before the registry changes, or retain and have the receiver
operator release the route's names), then add a new target. Receiver-side
ownership is per route, so a new target cannot silently take over names
written by an old one.

## 7. Receiver obligations

A conforming receiver MUST, in this order:

1. Refuse without an acknowledgement (any non-200) a body that is too large,
   malformed, signed by an unpinned key, badly signed, expired, or issued more
   than 30 seconds in the future. The signature is verified before any
   decryption.
2. Decrypt, and refuse without an acknowledgement if the payload's
   `idempotency_key` or `values` do not match the envelope.
3. Answer `rejected/wrong_target` or `rejected/generation_mismatch` when the
   envelope names another endpoint or generation, and `rejected/unauthorized`
   when the binding does not authorize the namespace.
4. Answer `applied` for a valid `probe` without state change.
5. If the idempotency key was seen, answer the recorded outcome again
   (`already_applied` for an earlier `applied`) without re-applying.
   Receivers keep keys at least as long as the maximum envelope lifetime.
6. Refuse to overwrite or delete a name the route does not own
   (`conflict/exists_unowned`), and refuse a lower revision than the one that
   last wrote the name (`rejected/stale_revision`).
7. Apply durably, record the outcome under the idempotency key, then sign and
   return the acknowledgement. Acknowledgement bodies never echo values.

A receiver pins Hikyo's signing keys with optional `not_after` instants; a
retiring key is accepted only for envelopes issued at or before it.

## 8. Sender behaviour and outcome mapping

Each ledger effect (one name on one surface) is one envelope. Every envelope is
sealed before the journal's Prepare fence, so no plaintext-bearing work runs
after the provider-write lease is taken except the POST itself. The
idempotency key is derived from the endpoint id, fingerprint, route, target
generation, source revision, op, surface, and name: a retry of the same effect
in the same revision reuses it; a new revision derives a new one.

| Result | Ledger | Outbox |
|---|---|---|
| verified `applied` / `already_applied` | owned (upsert) or released (prune) | continue |
| verified `conflict` | bare reservation released with a conflict artifact; claimed state kept | conflict, needs attention; retried until the receiver releases the name or an operator renames. `adapter adopt` is refused for this provider: the protocol carries no adopt intent, so a receiver never transfers an unowned name to a route |
| verified `rejected/unauthorized` | reservation released; claimed state kept | terminal provider-auth failure |
| verified `rejected/*` | reservation released; claimed state kept | failure, retried |
| no acknowledgement (transport error, timeout, non-200, oversized, malformed, wrong media type) | **dispatched**, outcome **unknown** | retried with the same idempotency key |
| forged acknowledgement (unpinned key, bad signature, wrong digest or idempotency key) | **dispatched** (the receiver may have applied it), outcome failure | **terminal**, needs attention |

Ambiguity is never reported as success. A crash between receiver apply and
acknowledgement surfaces as `unknown`; the retry is answered
`already_applied`, which alone advances the target to owned.

The management sentinel `MANAGED_BY_HIKYO` is upserted first on both surfaces
and pruned last on teardown, exactly as on the other adapters.

## 9. Lifecycle

- **Pause / resume / resync / retain-or-prune / teardown** follow the #157
  target controls unchanged; prune only ever names ledger-owned names.
- **Hikyo signing-key rotation (overlap):** the receiver pins the new key,
  the old key gains a `not_after` at least one maximum outbox retry window in
  the future (default guidance 24 hours), the instance admin replaces
  `signing_key_file`, and after the window the receiver drops the old key.
  Rotation does not change the fingerprint.
- **Recipient or acknowledgement key rotation / host move:** bump
  `generation`, re-confirm the new fingerprint, and re-bind targets. The old
  and new receiver configurations are separate trust boundaries.
- **Multi-node takeover:** workers are fenced by the #146 leases; a worker
  that loses authority fails the journal gate at the start of the sync, before
  Prepare, and again after Prepare, and never reaches the receiver.

## 10. Out of scope

Tenant-configurable URLs, JOSE/JWS envelopes, webhook ingestion into Hikyo,
and receiver SDKs beyond the reference example.

## 11. Protocol version 2 (proposed, not operative)

> **Proposed text ([#1128](https://github.com/Hikyo-Org/Hikyo/issues/1128)).**
> This section is not operative until the implementing PR merges and the
> revision route below is accepted. Sections 1 to 10 are the version 1
> protocol and are unchanged: version 1 targets stay byte-compatible and keep
> their original policy. Independent adversarial review is required;
> cross-provider review is SKIPPED by explicit owner instruction (2026-11-02)
> and is not CLEAN. Security approval: not given.
>
> **Revision route (proposed; DEC-18 does not state one for this owner).** A
> declared-amendment banner on [deployment-adapter.md](../adr/deployment-adapter.md),
> the route #163 used, carrying this section's text. HOLD for the owner: this
> section is not operative until that route is accepted **and** the
> implementation merges, whatever any published guide says.

Decisions applied: DEC-01 (age hybrid recipient `mlkem768x25519`), DEC-02
(ML-DSA-65 only), DEC-03 (all required signatures verify, required set from the
verifier's pinned state, suite-bound bytes), DEC-07 (algorithm-bearing
identifiers, new labels), DEC-08 (numeric bounds below). Version 2 selects
exactly one suite and negotiates nothing:

| Role | Algorithm | Text form |
|---|---|---|
| Payload encryption | age `HybridRecipient`, X25519 + ML-KEM-768, stanza type `mlkem768x25519` | recipient `age1pq1...` (1959 characters); identity `AGE-SECRET-KEY-PQ-1...` |
| Envelope and acknowledgement signing | **both** Ed25519 and ML-DSA-65, both required (suite `ed25519+mldsa65`) | `ed25519:` + 64 hex; `mldsa65:` + 3904 lowercase hex (1952 bytes) |

ML-DSA-65 alone (DEC-03 end state) is a later suite with its own string, label
version and trust generation. It is **not built** here: no verifier accepts a
single-algorithm signature set in version 2.

### 11.1 Version separation

A version 2 message cannot be read as version 1 or the reverse:

- Path `POST {origin}/hikyo/v2/sync`; media types
  `application/vnd.hikyo.sealed-envelope.v2+json` and
  `application/vnd.hikyo.sealed-ack.v2+json`. A version 1 only receiver
  answers 404 or 415, which the sender classifies as "no acknowledgement"
  (outcome unknown, retried). **There is no fallback to version 1 on any error.**
- Every domain label is new: `hikyo/sealed-webhook/{envelope,ack,fingerprint,destination,idempotency,key-id}/v2`.
  A label belongs to one purpose and appears as the first length-prefixed field
  of every signed or hashed input. It is also the ML-DSA `Options.Context`
  (pure ML-DSA, no pre-hash, hedged signing).
- Every JSON member name is spelled exactly as listed, appears once, and
  belongs to the object's member set, at every depth. A case variant
  (`SIG_ED25519`, `V`), a duplicate or an unknown member refuses the message
  before any value is built from it; `encoding/json` alone would match case
  variants and keep the last duplicate.
- The `v` member is `2` and the `suite` member must equal `ed25519+mldsa65`;
  both are inside the signed bytes. Any other version, suite, algorithm,
  member, or key identifier prefix is refused, never defaulted.
- A signature made under another algorithm, target, route, version, suite,
  recipient or key identifier does not verify because every one of them is in
  the signed bytes.

### 11.2 Identifiers

- Key id: `ed25519-sha256:` or `mldsa65-sha256:` followed by the lowercase hex
  SHA-256 over length-prefixed `hikyo/sealed-webhook/key-id/v2`, the algorithm
  name, and the raw public key. Version 1 key ids (`sha256:...`) are never
  accepted in version 2.
- Recipient id: `mlkem768x25519-sha256:` + hex SHA-256 over length-prefixed
  `hikyo/sealed-webhook/key-id/v2`, `age-mlkem768x25519`, and the canonical
  recipient text.
- Fingerprint: `swh2-mlkem768x25519-ed25519-mldsa65-sha256:` + hex SHA-256 over
  length-prefixed `hikyo/sealed-webhook/fingerprint/v2`, canonical origin,
  canonical hybrid recipient, canonical Ed25519 acknowledgement key text,
  canonical ML-DSA-65 acknowledgement key text, the suite string, generation.
  Changing any of them changes the fingerprint and the destination id, so
  targets bound under another fingerprint (including a version 1 fingerprint
  of the same origin) fail closed before any request is sent.

### 11.3 Envelope

JSON object with exactly these members (unknown members refused):

| Member | Rule |
|---|---|
| `v` | `2` |
| `suite` | `ed25519+mldsa65` |
| `key_id_ed25519`, `key_id_mldsa65` | the sender's two key ids, as in 11.2 |
| `recipient_id` | the pinned recipient id; the receiver refuses any other before decrypting |
| `target_id`, `instance_id`, `namespace`, `route`, `generation`, `source`, `op`, `names` | as section 4 |
| `ciphertext` | standard base64 of the age ciphertext, at most 1 MiB decoded (1,398,104 base64 characters) |
| `ciphertext_digest` | `sha256:` + hex SHA-256 of the raw ciphertext bytes |
| `idempotency_key` | 64 lowercase hex, derived under the version 2 idempotency label; also the replay context |
| `issued_at`, `expires_at` | as section 4; `0 < expires_at - issued_at <= 5m` |
| `sig_ed25519` | base64 of exactly 64 bytes (88 characters) |
| `sig_mldsa65` | base64 of exactly 3309 bytes (4412 characters) |

**Signed bytes.** Length-prefixed fields (uint32 big-endian length, base-10 ASCII
integers) in order: `hikyo/sealed-webhook/envelope/v2`, `v`, `suite`,
`key_id_ed25519`, `key_id_mldsa65`, `recipient_id`, `target_id`, `instance_id`,
`namespace`, `route`, `generation`, `source.org`, `source.project`,
`source.environment`, `source.revision`, `op`, the count of `names`, then
`surface` and `name` per entry, `ciphertext_digest`, `idempotency_key`,
`issued_at`, `expires_at`. Both signatures cover these identical bytes; the
ciphertext is bound through its digest, which the receiver recomputes.

**Payload** and its rules are section 4's. The ciphertext is encrypted to
exactly one recipient, the pinned hybrid recipient.

### 11.4 Acknowledgement

Members: `v` (`2`), `suite`, `envelope_digest` (SHA-256 of the exact request
body), `idempotency_key`, `status`, `reason` (rules of section 5), the
acknowledgement signer's `key_id_ed25519` and `key_id_mldsa65`, `sig_ed25519`,
`sig_mldsa65`. Signed bytes: length-prefixed
`hikyo/sealed-webhook/ack/v2`, `v`, `suite`, `key_id_ed25519`,
`key_id_mldsa65`, `envelope_digest`, `idempotency_key`, `status`, `reason`.
The sender accepts an acknowledgement only if **both** signatures verify under
its two pinned acknowledgement keys, the suite and key ids match its pinned
state, and the digest and idempotency key are those of the request. A missing,
duplicate or wrong-size signature, an acknowledgement signed by one algorithm
only, or an acknowledgement of another version is never a success.

### 11.5 Bounds (DEC-08), enforced before allocation or expensive work

| Field | Bound |
|---|---|
| Request body | at most 1,529,176 bytes: 1,398,104 base64 ciphertext characters plus 128 KiB for 256 names (about 300 bytes each), identifiers, the 4,412 and 88 character signatures and key ids. The largest legal envelope measured is 1,477,423 bytes (256 names of 256 characters, ciphertext at the 1 MiB limit). A declared `Content-Length` over the bound is refused before reading |
| Ciphertext | 1 MiB decoded (unchanged); base64 length checked before decoding |
| Age header | at most 4096 bytes before the `---` line, exactly one stanza of type `mlkem768x25519` with one argument decoding to 1120 bytes and a body decoding to 32 bytes; anything else (X25519, scrypt, ssh, plugin, unknown, several stanzas) is refused before any key is tried |
| Signature fields | exact encoded lengths above, checked before base64 decoding and before any verification |
| Public keys | ML-DSA-65 1952 bytes decoded (3904 hex characters), Ed25519 32 bytes, length checked before parsing |
| Acknowledgement | 64 KiB (unchanged). A protocol 2 target must configure at least 5,120 bytes: a real acknowledgement is about 4,961 bytes and the largest legal one is 5,038. A smaller bound would refuse every acknowledgement after the receiver had applied the value, so activation refuses it |
| Names | at most 256, counted on the token stream: a body of empty name objects is refused before any is decoded into a structure |
| Ciphertext base64 | canonical only (no line breaks, no stray trailing bits) |
| Names, lifetime, skew | 256, 5 minutes, 30 seconds (unchanged) |

### 11.6 Receiver obligations

Section 7's order applies with these additions. The required signature set comes
from the receiver's **pinned sender entry** (a pair of one Ed25519 and one
ML-DSA-65 key with an optional `not_after`), never from the presented message.

1. Refuse without an acknowledgement (non-200): wrong path or media type, over
   the body bound, malformed, an unpinned or mismatched key pair, a missing,
   duplicate or wrong-size signature, either signature invalid, a
   `ciphertext_digest` that does not match, a `recipient_id` that is not the
   receiver's own, expired, or issued more than 30 seconds in the future. Both
   signatures are verified before any decryption.
2. Refuse a ciphertext whose header is not exactly one `mlkem768x25519`
   stanza (no classical, passphrase, ssh, plugin or additional recipient).
   This is a Hikyo-side check: age's decrypt path has no label check, so a
   holder of a classical identity elsewhere is outside it.
3. Everything else is section 7 steps 2 to 7. The acknowledgement carries
   both signatures.
4. A receiver that is **PQ-required** serves only `/hikyo/v2/sync` and answers
   404 for the version 1 path even for pinned version 1 sender keys. A
   receiver in transition serves both paths with independent identities,
   acknowledgement signers and sender pins; each message is interpreted only
   under its own version. It has **one** generation, **one** applied-values
   store and **one** idempotency store, shared by both versions (see 11.10).

### 11.7 Sender behavior

- Per-target `protocol` in the operator registry is `1` (default, absent) or
  `2`. A protocol 2 target requires an `age1pq1` recipient, `ack_key` (Ed25519),
  `ack_key_mldsa65` and the top-level `signing_key_file_mldsa65`; a protocol 1
  target refuses all of them. The registry is checked at boot against the
  confirmed fingerprint; any mismatch refuses the boot.
- Outcome mapping, retry, backoff, idempotency, the Journal fences and the
  durable INTENT and OUTCOME audit are those of section 8 and are
  protocol-blind. An acknowledgement that is well formed but fails signature,
  key, digest or idempotency binding is forged (terminal); anything else
  without a valid acknowledgement is unknown and retried under the same key
  **as version 2**. A protocol 2 target never emits a version 1 request after
  an error. That is the only downgrade this section rules out: it does not
  stop an administrator from restoring the protocol 1 registry entry (see
  11.10). A manual resync fences the target with a new generation, which is an
  input of the idempotency key, so it re-delivers under new keys and a receiver
  applies the same values again; replay protection does not stop that, and a
  receiver with apply-time side effects must expect it.
- A private key never appears in an argument vector, log, audit payload, error
  or response. Signing keys are `0600` PKCS#8 PEM files; the ML-DSA key is
  seed-only PKCS#8 (the only form Go accepts).

### 11.8 Rollout and lifecycle

Receiver first, as in the [migration policy](../security/pqc/migration.md):

1. Upgrade the receiver to read version 2 while still serving version 1.
   Version 1 senders are unaffected, provided the receiver keeps its one
   generation: both versions are checked against it.
2. Generate the receiver's hybrid identity and ML-DSA acknowledgement key and
   the instance's **ML-DSA** signing key. The instance **keeps its existing
   Ed25519 signing key**: a single signer serves every target, so replacing it
   would make the remaining protocol 1 targets sign with a key their receivers
   do not pin. Pin the sender pair (the existing Ed25519 public key and the new
   ML-DSA public key) on the receiver.
3. Read the version 2 fingerprint to the instance admin out of band. The admin
   changes the registry entry of that origin **in place** (an origin is
   configured once) to protocol 2, **keeping the generation**. Moving to
   protocol 2 changes the fingerprint and so the destination id: Hikyo targets
   bound under the protocol 1 entry fail closed before any request and must be
   re-bound. A rotation of the recipient or an acknowledgement key is the event
   that raises the generation, and it raises it for both versions on a receiver
   that serves both.
4. Ownership is per route in the one shared store, and a namespace has one
   sentinel. A new route in a namespace the old route still holds conflicts on
   the sentinel: bind the protocol 2 route in a new namespace, or prune the old
   route through the old boundary first (or have the receiver operator release
   its names).
5. **Sender rollback** is the protocol 1 registry entry (its fingerprint and
   generation, which did not change) and removal of `signing_key_file_mldsa65`.
   Targets still bound under protocol 1 and not pruned resume. A protocol 2 route
   fails closed. A route that was pruned does not come back: a re-bound protocol
   1 route is a different route and conflicts with names the protocol 2 route
   now owns until the receiver operator releases them, and a conflict is
   recorded per revision, so publish a new revision afterwards.
   **Receiver rollback** is only to a build that reads version 2.
6. Retire version 1 on the receiver (remove the X25519 identity and version 1
   pins) only after no sender uses it; that is a separate decision.
7. Key rotation overlaps by pinning the new sender pair and giving the old pair
   a `not_after`; a pair is two keys and rotates as one. Changing the
   recipient or either acknowledgement key is a new generation (step 3).

Residual limits: the webhook hop is TLS 1.2 or newer with the system trust
store and the classical certificate; strict `tlspolicy` does not reach it
(register row X19 of PR #1171), so the hop gives no post-quantum transport or
authenticity claim and the payload's protection rests on the hybrid recipient.
The age file key is 16 bytes (category 1 floor, DEC-04). Version 1 messages
already recorded are exposed to a recording adversary.

### 11.10 Registered limits of a dual-serving receiver (proposed)

- **R-1128-A, classical write path while both versions are served.** A receiver
  that still serves version 1 accepts version 1 envelopes signed by the pinned
  Ed25519 key. An actor who steals that key, or a future actor who can forge
  Ed25519, can rewrite or prune the values a route owns in the shared store,
  including values originally delivered under version 2, subject to the same
  route, generation and revision checks. The version 2 all-required rule does
  not protect values a version 1 path can also write. Claim limited: the
  authenticity claim for version 2 delivery holds only after version 1 is
  retired on the receiver (step 6). Owning tickets: #1128 (this child), #1135
  (closure record).
- **R-1128-B, no rollback floor for this surface.** DEC-11 requires a floor
  that a binary or operator cannot lower by stripping PQ verification. No
  floor exists here: the registry is an operator file, and an instance
  administrator can restore the protocol 1 entry, after which retained targets
  resume classical delivery. That is an administrator action, not an
  attacker's, but it is a rollback by removing PQ verification, which
  [migration policy](../security/pqc/migration.md) forbids as a recovery
  method. "Never downgrades" means only that no error path falls back. Owning
  tickets: #1128, #1135.
- **The probe proves the acknowledgement keys, not the recipient.** A verified
  acknowledgement proves the responder holds both acknowledgement keys. It
  contains nothing derived from the plaintext, so it does not prove the
  responder holds the hybrid identity; a party with the acknowledgement keys but
  not the identity could answer. Proving recipient ownership would need an
  acknowledgement field derived from the plaintext, which is a new mechanism and
  is not built.

### 11.9 Published vectors

[`internal/crypto/sealedhook/testdata/sealed-webhook-v2-vectors.json`](../../internal/crypto/sealedhook/testdata/sealed-webhook-v2-vectors.json)
is the protocol vector: one envelope and its acknowledgement, the pinned public
keys, the key ids, recipient id, fingerprint and destination id they derive, the
exact signed bytes of both messages in hex, and negative bodies with the error
class each must produce (stripped Ed25519 or ML-DSA signature, tampered scope,
downgraded suite, version 1 member set, a classical-only recipient with valid
signatures, an extra recipient stanza, another receiver's recipient id, an
acknowledgement with a stripped or forged signature, an acknowledgement for
another envelope). It contains one test-only age identity so a second
implementation can decrypt. A conforming implementation recomputes the signed
bytes from the parsed members, verifies both signatures under the context
string equal to the label, and refuses every negative body.

The ML-DSA signatures were also verified with a non-Go implementation
(OpenSSL 3.5.9): wrap the raw 1952-byte key in the ML-DSA-65 SubjectPublicKeyInfo
header `308207b2300b0609608648016503040312038207a100` and run
`openssl pkeyutl -verify -pubin -keyform DER -inkey KEY.der -in SIGNED_BYTES -sigfile SIG -pkeyopt context-string:hikyo/sealed-webhook/envelope/v2`
(the acknowledgement uses `.../ack/v2`). The ciphertext decrypts with the
`age` command line tool (v1.3.2) and the vector identity, which is the same
upstream project as the library, not an independent implementation of age.
