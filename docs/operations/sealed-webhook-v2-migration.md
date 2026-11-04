# Sealed webhook protocol 2: receiver and sender migration

Status: **protocol 2 of [#1128](https://github.com/Hikyo-Org/Hikyo/issues/1128)
is usable after the implementation merges, when a target is explicitly
configured for it.** The owner authorized merge on 2026-11-04 and accepted the
declared-amendment revision route for the spec. That is not a claim that the
registered limits below are closed, and cross-provider review was SKIPPED by
owner instruction, not CLEAN. The protocol is
[section 11 of the sealed webhook spec](../spec/sealed-webhook.md#11-protocol-version-2).
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
protocol 2 target never sends a protocol 1 request **after an error**: a
receiver that cannot read protocol 2 leaves the effect unknown and retried.
That is the only downgrade ruled out. An administrator can still restore the
protocol 1 registry entry, after which retained targets deliver classically
again (see the limits below).

## Read this first: the reference tool is not production software

`hikyo-receiver-example` is a documentation and test harness. It is **not
shipped** in the Hikyo release and the release has no key generator for the
instance's signing keys; build it from this repository's source
(`go build ./cmd/hikyo-receiver-example`) on a host you trust. As a receiver it
keeps all state in one plaintext JSON file (mode `0600`) that holds every
delivered value, rewrites it whole on every apply (it syncs the file and then the directory
before it acknowledges, but there is no journal or backup), and keeps every idempotency
key forever (the map is unbounded). If the directory sync fails after the rename (some
network filesystems and Windows reject it), the new state is already on disk but the apply
answers 503 and the in-memory state is rolled back, so it trails the file until restart;
a protocol 1 receiver that worked before can then answer 503 on every apply. Run it on a
local Unix filesystem. Spec section 7 requires a durable apply
before the acknowledgement; a real receiver must provide that itself. Do not
point production traffic at the example.

## Limits you must know

- The TLS hop to your receiver is TLS 1.2 or newer with the system trust store
  and a classical certificate. Strict TLS policy does not reach this hop
  (register row X19 on PR #1171), so there is no post-quantum transport or
  authenticity claim for it; the payload is protected by the hybrid recipient.
- While a receiver serves **both** versions, a stolen or forged Ed25519 sender
  key can still write or prune the values a route owns through the protocol 1
  path, including values first delivered under protocol 2. The protocol 2
  authenticity claim holds only for state written after protocol 1 is retired
  and the shared store has been reset (R-1128-A in the spec). The
  actor also needs the namespace binding credential, which a future Ed25519
  forger reads from recorded protocol 1 traffic. Such an actor picks the
  revision and idempotency key: a forged very high revision makes a genuine
  protocol 2 write answer `stale_revision` and leaves the forged value stored,
  and a forged future idempotency key makes the genuine delivery answer
  `already_applied` without a write, which the sender records as success.
- There is no rollback floor for this surface: restoring the protocol 1
  registry entry returns retained targets to classical delivery (R-1128-B).
- The age file key is 16 bytes (category 1 floor).
- Values delivered under protocol 1 before the move are exposed to a recording
  adversary; rotate long-lived ones after the target is on protocol 2.
- Only the dual suite exists. A signature set of ML-DSA-65 alone is a later
  suite and trust generation, not built.

## Rules that decide the plan

- **One generation per receiver.** The reference receiver, like the protocol,
  checks one `generation` for both versions. **Keep the generation unchanged
  when you move an origin to protocol 2.** Raising it on a receiver that serves
  both versions breaks protocol 1 senders (and the reverse), so a generation
  bump is for rotating the recipient or an acknowledgement key, and it moves
  every target on that receiver.
- **Keep the existing Ed25519 signing key.** One signer serves every target in
  the registry. Replacing `signing_key_file` would make your remaining protocol
  1 targets sign with a key their receivers do not pin. Generate only the
  ML-DSA key; the Ed25519 half of the pair is the key you already use.
- **Ownership is per route in one shared store, and a namespace has one
  sentinel.** A second route in a namespace another route holds conflicts. Bind
  the protocol 2 route in a new namespace, or prune the old route first.
- **A conflict is recorded per revision.** After the receiver operator releases
  names, publish a new revision; the same revision replays the recorded
  conflict.

## Steps

### 1. Generate the receiver's protocol 2 keys (no configuration change yet)

Receiver first. These commands change nothing on a running receiver, so protocol
1 senders see no change. With the reference receiver (the directory must exist;
the tool does not create it):

```sh
umask 077
mkdir -p ./v2-keys
hikyo-receiver-example keygen -protocol 2 -dir ./v2-keys
```

This writes `identity-hybrid.age`, `ack.pem` and `ack-mldsa65.pem` with mode
`0600` and prints only `recipient_key` (`age1pq1...`), `ack_key` and
`ack_key_mldsa65`. The Ed25519 acknowledgement key (`ack_key_file`) is shared by
both versions: if you already run a protocol 1 receiver, **keep your existing
`ack.pem` and its `ack_key`**, delete the new `v2-keys/ack.pem`, and use the
existing `ack_key` in step 3 and in the registry. Back up the new files in a
failure domain separate from the receiver host.

### 2. Generate the instance's ML-DSA signing key

On the Hikyo host, through protected input, with a binary built from source:

```sh
umask 077
mkdir -p /etc/hikyo/sealed
hikyo-receiver-example sender-keygen -dir /etc/hikyo/sealed
```

It writes `signing-mldsa65.pem` (mode `0600`) and prints its public key and key
id. It does **not** touch your Ed25519 key (`-with-ed25519` exists only for a
new installation). Read your existing Ed25519 public key with
`hikyo-receiver-example pubkey -pem /path/to/signing_key_file`.

### 3. Pin the sender pair, then enable protocol 2 on the receiver

Pin the existing Ed25519 public key and the new ML-DSA public key together, as
one **pair**, and only then add the protocol 2 members to the receiver
configuration (keep the protocol 1 members). A receiver with the hybrid members
and no pinned pair refuses to start (`at least one pinned sender pair is
required`) and takes protocol 1 down with it, so add all three together:

```json
{
  "hybrid_identity_file": "v2-keys/identity-hybrid.age",
  "ack_key_mldsa65_file": "v2-keys/ack-mldsa65.pem",
  "senders_v2": [{ "key": "ed25519:<hex>", "key_mldsa65": "mldsa65:<hex>" }]
}
```

Restart the receiver. It now serves both `/hikyo/v1/sync` and `/hikyo/v2/sync`
with its generation unchanged. Compute the fingerprint, and read it over an
independent channel (voice or another channel you control) to the instance admin:

```sh
hikyo-receiver-example fingerprint -protocol 2 \
  -origin https://recv.example -recipient age1pq1... \
  -ack-key ed25519:<your existing ack_key> -ack-key-mldsa65 mldsa65:<hex> -generation <the current generation>
```

### 4. Change the registry entry to protocol 2

The registry (`HIKYO_SEALED_WEBHOOK_FILE`) gains a top-level
`signing_key_file_mldsa65` and, per target, `"protocol": 2` and
`ack_key_mldsa65`. An origin is configured once, so you change the entry **in
place**: `recipient_key`, both ack keys, `protocol` and `confirmed_fingerprint`
move together; `generation` and `signing_key_file` stay as they are.

```json
{
  "instance_id": "hikyo-prod",
  "signing_key_file": "sealed/signing-ed25519.pem",
  "signing_key_file_mldsa65": "sealed/signing-mldsa65.pem",
  "targets": [{
    "id": "payments", "origin": "https://recv.example", "protocol": 2,
    "recipient_key": "age1pq1...", "ack_key": "ed25519:...", "ack_key_mldsa65": "mldsa65:...",
    "generation": 1, "timeout_seconds": 5, "max_response_bytes": 8192,
    "confirmed_fingerprint": "swh2-mlkem768x25519-ed25519-mldsa65-sha256:..."
  }]
}
```

`max_response_bytes` must be at least **5120** for a protocol 2 target (a real
acknowledgement is about 4,961 bytes); activation refuses less. The server also
refuses to start on a mismatched fingerprint, a protocol 2 target without the
ML-DSA material, ML-DSA material on a protocol 1 target, an unused
`signing_key_file_mldsa65`, or a key file readable by group or other.

The fingerprint changes, so Hikyo targets bound under the protocol 1 entry fail
closed with a destination mismatch before any request. Re-bind them: prune
through the old boundary first (or retain them and have the receiver operator
release their names), then add a new target, in a new namespace if the old
route is retained. `hikyo adapter test` on the new target checks that the
responder holds **both acknowledgement keys** (a verified acknowledgement signed
by both) and that the receiver processed a probe. It does not prove the
responder holds the hybrid identity; confirm that with the receiver operator.

### 5. Verify with a real push to a non-production namespace

Use a test namespace you own. Confirm the receiver log shows only
`/hikyo/v2/sync` for the new target, a stripped or altered message is refused
with no acknowledgement, and a replay is answered `already_applied`. Expect a
manual resync to re-deliver the same values (new generation, new keys).

### 6. Rollback

- **Sender:** restore the protocol 1 entry (its fingerprint and unchanged
  generation) and remove `signing_key_file_mldsa65`. Targets still bound under
  protocol 1 and not pruned resume, unless their names were released and a
  protocol 2 route took the namespace (then they conflict, as below). Protocol 2
  routes fail closed. A route you
  pruned does not come back: a re-bound protocol 1 route conflicts with names
  the protocol 2 route now owns until the receiver operator releases them, and
  you then publish a new revision.
- **Receiver:** only to a build that still reads protocol 2. Rolling it back to
  protocol 1 leaves protocol 2 targets unconverged; they do not downgrade.
- Do not re-enable a retired key pair by restoring its files: the receiver
  refuses a pair after its `not_after`, and a changed fingerprint fails closed
  on the sender.
- Rolling the registry back is an administrator action that removes PQ
  verification for those targets. This surface has no floor that prevents it
  (R-1128-B); record the decision.

### 7. Key overlap and retirement

- **Sender pair rotation:** pin the new pair on the receiver, give the old pair
  a `not_after` at least one outbox retry window ahead (default guidance 24
  hours), install the new key files and restart Hikyo, then drop the old pair
  after the window. A pair rotates as a unit: a half-rotated pair matches
  nothing. Rotation does not change the fingerprint.
- **Receiver recipient or acknowledgement key rotation:** a new generation and
  fingerprint and a re-bind; the generation moves for both versions on a
  receiver that serves both.
- **Retire protocol 1 on the receiver** (remove `identity_file`, the protocol 1
  `senders` and answer 404 on `/hikyo/v1/sync`) only after no sender uses it.
  That is a separate decision with its own record, and it is what ends new
  classical writes (R-1128-A). Nothing here makes it. At retirement, reset the
  receiver's shared applied-values and idempotency store: entries written through
  protocol 1 while both versions were served are not repaired by retirement, and
  a forged revision or idempotency key stays in force. Inspection is not enough,
  because entries record no protocol version and only an implausible revision
  stands out. On the reference receiver, stop it (state is loaded at startup and
  rewritten whole from memory), remove its `-state` file, restart it, then have
  every sender redeliver, because the reset also drops genuine values.
- Do not destroy the old identity while retained ciphertext still needs it.

## Troubleshooting

| Symptom | Cause |
|---|---|
| Hikyo does not start: `endpoint fingerprint was not confirmed` | The fingerprint was computed for other inputs; recompute with `-protocol 2` and the exact origin, keys and generation |
| Hikyo does not start: `max response bytes must be at least 5120` | A protocol 2 acknowledgement is about 5 KB; set `max_response_bytes` to 5120 or more (8192 is a good value) |
| Probes and pushes report `acknowledgement exceeded the configured size bound` | The same bound on a build that predates the check; raise it. The receiver may already have applied the value; the retry is answered `already_applied` |
| Every push is unknown and retried | The receiver does not serve protocol 2 yet (step 3), or the sender pair is not pinned as a pair |
| `rejected: generation_mismatch` | The sender's generation differs from the receiver's one generation. Keep them equal for both versions |
| Every protocol 1 target stopped signing correctly | `signing_key_file` was replaced; restore the Ed25519 key the receivers pin and use `signing_key_file_mldsa65` for the new key |
| `conflict: exists_unowned` on `MANAGED_BY_HIKYO` | Another route holds the namespace sentinel; use a new namespace or prune the old route, then publish a new revision |
| `provider rejected: unauthorized` | The tenant binding does not authorize the namespace on the receiver |
| Targets fail with a destination mismatch | The fingerprint or generation changed; re-bind (step 4) |
