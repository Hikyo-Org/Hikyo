# Sealed webhook protocol 2: receiver and sender migration

Status: **draft implementation of [#1128](https://github.com/Hikyo-Org/Hikyo/issues/1128),
not operative, security approval: not given.** The protocol is the proposed
[section 11 of the sealed webhook spec](../spec/sealed-webhook.md#11-protocol-version-2-proposed-not-operative).
Nothing here authorizes a production key change, a push to a live receiver, or
retiring protocol 1. Follow the order: receiver first.

## What changes

| | Protocol 1 (unchanged, default) | Protocol 2 (opt-in per target) |
|---|---|---|
| Path | `/hikyo/v1/sync` | `/hikyo/v2/sync` |
| Payload encryption | age X25519 | age hybrid X25519 + ML-KEM-768 (`age1pq1...`) |
| Envelope and acknowledgement signatures | Ed25519 | Ed25519 **and** ML-DSA-65, both required |
| Receiver keys | `age1...` recipient, Ed25519 ack key | `age1pq1...` recipient, Ed25519 and ML-DSA-65 ack keys |
| Sender keys | one Ed25519 PEM | the Ed25519 PEM plus an ML-DSA-65 seed-only PKCS#8 PEM |
| Fingerprint | `sha256:...` | `swh2-mlkem768x25519-ed25519-mldsa65-sha256:...` |

A protocol 1 target is byte-compatible and keeps working until you move it. A
protocol 2 target never sends a protocol 1 request, not even after an error: a
receiver that cannot read protocol 2 leaves the effect unknown and retried.

## Limits you must know

- The TLS hop to your receiver is TLS 1.2 or newer with the system trust store
  and a classical certificate. Strict TLS policy does not reach this hop
  (register row X19 on PR #1171), so there is no post-quantum transport or
  authenticity claim for it; the payload is protected by the hybrid recipient.
- The age file key is 16 bytes (category 1 floor).
- Values delivered under protocol 1 before the move are exposed to a recording
  adversary; rotate long-lived ones after the target is on protocol 2.
- Only the dual suite exists. A signature set of ML-DSA-65 alone is a later
  suite and trust generation, not built.

## Steps

### 1. Upgrade the receiver to read protocol 2 (receiver first)

Run a receiver build that serves both paths. Protocol 1 senders see no change.
With the reference receiver:

```sh
hikyo-receiver-example keygen -protocol 2 -dir ./v2-keys
```

This writes `identity-hybrid.age`, `ack.pem` and `ack-mldsa65.pem` with mode
`0600` and prints only `recipient_key`, `ack_key` and `ack_key_mldsa65`. Add to
the receiver configuration (keep the protocol 1 members for the transition):

```json
{
  "hybrid_identity_file": "v2-keys/identity-hybrid.age",
  "ack_key_mldsa65_file": "v2-keys/ack-mldsa65.pem",
  "senders_v2": []
}
```

The Ed25519 acknowledgement key (`ack_key_file`) is shared by both versions.
Back up the new files in a failure domain separate from the receiver host.

### 2. Generate the Hikyo instance's signing keys

On the Hikyo host, through protected input:

```sh
umask 077
hikyo-receiver-example sender-keygen -dir /etc/hikyo/sealed
```

It writes `signing-ed25519.pem` and `signing-mldsa65.pem` (mode `0600`) and
prints each public key and key id. Pin both on the receiver as one **pair**:

```json
"senders_v2": [{ "key": "ed25519:<hex>", "key_mldsa65": "mldsa65:<hex>" }]
```

Read the public keys again later with
`hikyo-receiver-example pubkey -pem FILE`.

### 3. Compute the fingerprint and read it over an independent channel

```sh
hikyo-receiver-example fingerprint -protocol 2 \
  -origin https://recv.example -recipient age1pq1... \
  -ack-key ed25519:<hex> -ack-key-mldsa65 mldsa65:<hex> -generation 2
```

Read the result to the instance admin by voice or another channel you control.
Use a **higher generation** than the protocol 1 entry of the same origin.

### 4. Add the protocol 2 target

The registry (`HIKYO_SEALED_WEBHOOK_FILE`) gains a top-level
`signing_key_file_mldsa65` and, per target, `"protocol": 2` and
`ack_key_mldsa65`. One origin is configured once: change the entry in place
(the `recipient_key`, both ack keys, `generation`, `protocol` and
`confirmed_fingerprint` move together).

```json
{
  "instance_id": "hikyo-prod",
  "signing_key_file": "sealed/signing-ed25519.pem",
  "signing_key_file_mldsa65": "sealed/signing-mldsa65.pem",
  "targets": [{
    "id": "payments", "origin": "https://recv.example", "protocol": 2,
    "recipient_key": "age1pq1...", "ack_key": "ed25519:...", "ack_key_mldsa65": "mldsa65:...",
    "generation": 2, "timeout_seconds": 5, "max_response_bytes": 4096,
    "confirmed_fingerprint": "swh2-mlkem768x25519-ed25519-mldsa65-sha256:..."
  }]
}
```

The server refuses to start on a mismatched fingerprint, a protocol 2 target
without the ML-DSA material, ML-DSA material on a protocol 1 target, an unused
`signing_key_file_mldsa65`, or a key file readable by group or other. The
fingerprint changes, so Hikyo targets bound under the protocol 1 entry fail
closed with a destination mismatch before any request. Re-bind them: prune
through the old boundary first (or retain and have the receiver release the
names), then add a new target. Run `hikyo adapter test` on the new target: a
verified acknowledgement signed by both receiver keys also proves the receiver
holds the private keys.

### 5. Verify with a real push to a non-production namespace

Use a test namespace you own. Confirm the receiver log shows only
`/hikyo/v2/sync`, a stripped or altered message is refused with no
acknowledgement, and a replay is answered `already_applied`.

### 6. Rollback

- **Sender:** restore the protocol 1 entry (its generation and fingerprint) in
  the registry and remove `signing_key_file_mldsa65`. Targets bound under
  protocol 1 work again; targets bound under protocol 2 fail closed until the
  registry returns to them.
- **Receiver:** only to a build that still reads protocol 2. Rolling it back to
  protocol 1 only leaves protocol 2 targets unconverged; they never downgrade.
- Never re-enable a retired key pair by restoring its files: the receiver
  refuses a pair after its `not_after`, and a changed generation or fingerprint
  fails closed on the sender.

### 7. Key overlap and retirement

- **Sender pair rotation:** pin the new pair on the receiver, give the old pair
  a `not_after` at least one outbox retry window ahead (default guidance 24
  hours), install the new key files and restart Hikyo, then drop the old pair
  after the window. A pair rotates as a unit: a half-rotated pair matches
  nothing. Rotation does not change the fingerprint.
- **Receiver recipient or acknowledgement key rotation:** a new generation, a
  new fingerprint and a re-bind.
- **Retire protocol 1 on the receiver** (remove `identity_file`, the protocol 1
  `senders` and answer 404 on `/hikyo/v1/sync`) only after no sender uses it.
  That is a separate decision with its own record; nothing here makes it.
- Do not destroy the old identity while retained ciphertext still needs it.

## Troubleshooting

| Symptom | Cause |
|---|---|
| Hikyo does not start: `endpoint fingerprint was not confirmed` | The fingerprint was computed for other inputs; recompute with `-protocol 2` and the exact origin, keys and generation |
| Every push is unknown and retried | The receiver does not serve protocol 2 yet (step 1), or the sender pair is not pinned as a pair |
| `provider rejected: unauthorized` | The tenant binding does not authorize the namespace on the receiver |
| Targets fail with a destination mismatch | The fingerprint or generation changed; re-bind (step 4) |
