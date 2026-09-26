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
| verified `conflict` | bare reservation released with a conflict artifact; claimed state kept | conflict, needs attention; retried until an operator adopts or renames |
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
