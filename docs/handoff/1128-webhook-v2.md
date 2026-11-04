# #1128: versioned hybrid-encrypted, PQ-signed sealed webhook delivery (protocol 2), handoff

Status: **draft implementation. Not merge-ready, not operative. Security approval: not given.**
Owner and maintainer are one person, so independent adversarial review compensates;
cross-provider review is SKIPPED by explicit owner instruction (2026-11-02) and is not CLEAN.
Refs [#1128](https://github.com/Hikyo-Org/Hikyo/issues/1128); the PR does not close it.
Stacked on PR #1156 (`dunky13/pqc-1127-transport-policy`). Operator text:
[receiver and sender migration](../operations/sealed-webhook-v2-migration.md). Protocol text:
[spec section 11](../spec/sealed-webhook.md#11-protocol-version-2-proposed-not-operative).

## Decisions applied, nothing newly selected

DEC-01 (age `mlkem768x25519`), DEC-02 (ML-DSA-65 only), DEC-03 (all required signatures verify,
required set from the verifier's pinned state, suite-bound bytes), DEC-07 (algorithm-bearing
identifiers, new labels), DEC-08 (webhook v2 field bounds), DEC-09 (closed error classes),
receiver-first order from the migration policy. Age and `crypto/mldsa` are used as shipped; no
primitive or composition was added.

## What was built

| Area | Change |
| --- | --- |
| Spec | Section 11 of `docs/spec/sealed-webhook.md`, marked proposed; sections 1 to 10 (v1) untouched |
| Crypto (`internal/crypto/sealedhook/v2.go`) | New file beside v1. Dual suite `ed25519+mldsa65` (both required), hybrid recipient, version-separated path, media types and labels, `ed25519-sha256:` / `mldsa65-sha256:` / `mlkem768x25519-sha256:` ids, `swh2-...` fingerprint, `ciphertext_digest`, exact-length fields checked before base64 decoding, one-stanza age header allowlist with a 4096-byte header bound (copy of the #1126 scan: this leaf package may not import `crypto/backup`), an exact-case unique member set and the names bound checked on the token stream before any struct decode, canonical base64. v1 code only gained shared validators (`validateScope`, `validateWindow`, `validateStatus`, `destinationID`, `idempotencyKey`) |
| Sender (`internal/adapter/sealedwebhook`) | `Endpoint`, `Client`, `Module` select the protocol per target; route table has `deliver` and `deliver.v2`; a protocol 2 client verifies both ack signatures and never emits a v1 request. `Client.RootCAs` test wiring added (same pattern as awssm; `Endpoint.ClientConfig` never sets it) |
| Registry (`internal/config`, `internal/app`) | Per-target `protocol` (absent = 1), `ack_key_mldsa65`, top-level `signing_key_file_mldsa65` (same `0600` check). Mixed, missing or unused ML-DSA material refuses boot |
| Reference receiver (`internal/sealedreceiver`, `cmd/hikyo-receiver-example`) | Serves v1 and v2 independently; PQ-required receiver omits v1 and answers 404 there; Content-Length bound; tool gains `keygen -protocol 2`, `sender-keygen`, `pubkey`, `fingerprint -protocol 2` |
| Vectors | `internal/crypto/sealedhook/testdata/sealed-webhook-v2-vectors.json` (positive, signed bytes, derived ids, negatives) |
| Docs | Migration guide; site registry notes |

## Freeze, migration and consumer checks

- **API freeze:** the target configuration is the operator file registry, not an API object. No
  registry field exists in `api/openapi.yaml`; `AdapterProvider` is `x-extensible-enum` and its
  values are unchanged. This branch changes nothing under `api/`, `clients/`, `web/` or
  `internal/store` (`git diff origin/dunky13/pqc-1127-transport-policy -- api clients web internal/store` is empty), so the guard has nothing to refuse. No allowlist entry, no second
  encoding, no HOLD from the freeze.
- **Database migration: none.** Destination ids derive from the fingerprint, and the v2 fingerprint
  and destination labels are new, so v1-bound targets fail closed. No migration number is
  reserved (00083 is taken by #1172 and #1129; #1141 may take the next). `internal/buildcompat/development.json` is untouched.
- **Audit:** `adapter.test` payload `version` is an open string; `sealed-webhook/v2` needs no
  catalogue change. INTENT and OUTCOME behavior is the unchanged Journal path.
- **Redaction:** no list matches age keys except the vendored scanner rule, which does not match
  `AGE-SECRET-KEY-PQ-1`; that is a #1126 requirement and is not edited here.

## Review repair

An independent review of `9de6a536b` found no forgery, stripping or downgrade-on-error break and
eight defects plus lows; each was verified before it was changed. Verified true: the 4,096-byte
example bound refused every real ack; the single generation and single signer made the documented
transition and rollback impossible; the "concurrent" rotation never overlapped (the receiver was
swapped after all deliveries); `encoding/json` matched `SIG_ED25519` and `V` case-insensitively and
a body of empty name objects allocated 99 MB per parse; and the other documentation claims. The
repairs are in the commits after `9de6a536b`.

## Second review repair (re-review of `8bad510a5`)

Eight remaining defects, each verified first. Item 1 was reproduced with the built binary against a
live v1 receiver: `keygen -dir` does not create the directory, and a receiver with the hybrid
members and an empty `senders_v2` exits with `at least one pinned sender pair is required`, taking
v1 down; the guide now creates the directory, generates keys first, and adds the hybrid members
only together with a pinned pair (walked through again with the built binary: both paths answered,
the fingerprint printed; the walkthrough also showed the fingerprint must use the existing
`ack_key`, now stated). Item 2: rollback text qualified. Item 3: the "before allocation" claim is
scoped to the body, names and member set, and the two-pass string cost is measured (49 MiB for one
1.5 MB invalid-UTF-8 string, 41 MiB for 256 large names) and registered; the reviewer's v1
comparison was not reproduced. Item 4: stray-bits cases and two vector negatives; removing
`.Strict()` from either decode now fails. Item 5: real classical identities in the receiver test;
removing the ack-signer check now fails. Item 6: scope fields at maximum, 1,478,120 bytes. Item 7:
this file. Item 8: the parent directory is synced after the rename; the sync order is testable
through two hooks and removing either sync fails; "after an error" added to the site and registry
comment. The reference tool's directory sync is Unix-only.

## Acceptance

Legend: DONE, PARTIAL, HOLD, NOTRUN.

| Criterion | Status | Notes |
| --- | --- | --- |
| Specify a new protocol version with authenticated identifiers, scope, times, replay context, ciphertext digest, ack binding; refuse unknown versions and cross-version reinterpretation | DONE (text), second review NOTRUN | Spec 11.1 to 11.4, 11.10. Replay context is the idempotency key (no separate nonce, deliberately). A first independent review ran against `9de6a536b`; its findings are repaired here and need re-review |
| Approved hybrid recipient and signatures; validate pinned identities and key ownership before enabling | **PARTIAL** | Fingerprint confirmation, key parsing and pair pinning DONE. The `adapter test` probe proves the responder holds both acknowledgement keys, **not** the hybrid identity: an acknowledgement carries nothing derived from the plaintext, so recipient ownership is not proven. Proving it needs a plaintext-derived ack field, a new mechanism, not built |
| Protected key generation and import, algorithm-aware fingerprints; target configuration, API, CLI, UI and reference tooling | PARTIAL | Registry, fingerprints and reference tooling DONE (`sender-keygen` now mints only the ML-DSA key and leaves the instance's Ed25519 key alone; `pubkey` prints one prefixed form). No API, CLI or UI consumes the registry. **No product-binary generator**: the tool is unshipped. Keys are `0600` files (the v1 custody model), not sealed under the instance hierarchy |
| Reject classical-only encryption, stripping, wrong key, other algorithm/target/version, mixed unlock paths; dual signatures all-required | DONE | Crypto, adapter and vector negatives; mutations (skip ML-DSA on envelopes, on acks, the not_after check) each fail tests. "Never downgrades" means no error path falls back; it does not stop an administrator restoring the v1 entry (R-1128-B) |
| Bounds for hybrid and ML-DSA sizes; malformed, overlarge, truncated, expired, future, tampered refused without unsafe allocation | DONE | Member names are exact-case, unique and bounded (256 names) on the token stream before any struct decode (the 1.5 MB empty-names body now allocates under 4 MiB, was 99 MB); canonical base64; a protocol 2 response bound below 5,120 is refused at activation; largest legal envelope **1,478,120** of 1,529,176 bytes (256 names of 256 characters, every identifier and integer at its maximum, ciphertext at the 1 MiB limit), largest legal ack 5,038 bytes |
| Preserve authN/Z, egress, destination pinning, INTENT/OUTCOME audit, fences, retry, duplicate suppression, ambiguity | DONE | Journal path unchanged; exercised on both engines |
| Exercise encrypt, send, receiver verify/decrypt, signed ack, reconcile; retry/restart, duplicate delivery, concurrent key rotation, multi-node takeover | DONE (local) | `TestAdapterSealedWebhookProtocolTwoEndToEnd` on SQLite and PostgreSQL: rotation now holds the first revision 3 request at the receiver while the pins change, asserts the 400 and the same-key retry under the new pair; the takeover stalls node 1 after the apply and before the ack, asserts the superseded fence, `already_applied` and at most one `applied` per key; failing phases assert attempts happened and the outbox state |
| Keep v1 targets; PQ-required targets never downgrade on error; v1 data only under its policy | DONE | v1 tests and KAT unchanged and passing; skew matrix; no v1 request after any v2 error |
| Skew, reader-first, key overlap and retirement, rollback to a v2-capable reader, restore of custody without reviving authority | DONE (local), with limits | `TestV2TransitionAndRollbackAgainstOneReceiver` runs the sequence against one receiver with one generation, one Ed25519 key and one shared store (and found the sentinel and per-route ownership limits now documented); pair retirement is exercised with a real `not_after` |
| Publish vectors, receiver migration instructions, exact candidate evidence; no live push or production key change | PARTIAL (instructions) | Vectors DONE (18 negatives incl. case variants, stray trailing bits in the ciphertext and signature base64, nested duplicates, a line break in the ciphertext). The guide was corrected (bound, generation, key, ownership, rollback, probe, tool warning) and its commands are covered by tool tests, but it was not followed hands-on on a clean host. No live target was contacted and no production key was made |

## Evidence

Local on Apple M2 Pro (arm64), Go 1.27.3, host load 10 to 12 from other jobs.

| Class | Result |
| --- | --- |
| Unit and adapter | `go test ./internal/crypto/sealedhook ./internal/adapter/sealedwebhook ./internal/sealedreceiver ./internal/config ./cmd/hikyo-receiver-example` and `go test -run SealedWebhook ./internal/app`: pass (the whole `internal/app` package is not claimed: see Final checks) |
| Both engines | `TestAdapterSealedWebhookProtocolTwoEndToEnd` (SQLite, PostgreSQL 18) via `agent-heavy`: pass |
| Cross-implementation | ML-DSA-65 signatures and context verified with OpenSSL 3.5.9; ciphertext decrypts with the `age` CLI v1.3.2 (same upstream as the library, so not independent for age) |
| Lint, pins, fast-feedback, signatures, DCO, doc status | see "Final checks" |
| Hosted CI | see Final checks (results read per head) |
| Real receiver, live push, deployment | NOTRUN |

Measured baseline (DEC-17; no threshold proposed): one-value envelope 7,873 bytes, ack 4,961
bytes; seal 0.86 ms and open 0.50 ms per operation (includes both signatures, age hybrid wrap and
unwrap), `go test -bench V2SealOpen`, three runs, loaded host. amd64 NOTRUN.

## Findings worth a reviewer's time

1. A manual resync bumps the target generation, which is an input of the idempotency key, so its
   effects carry new keys and are re-applied (idempotent content). Retries inside one job reuse the
   key. This is existing v1 behavior and is not changed.
2. A protocol 2 acknowledgement missing a signature or with a downgraded suite is malformed, so it
   is "no acknowledgement" (unknown, retried), not "forged" (terminal). It is never a success.
3. The server cannot prove the receiver stripped nothing on the TLS hop (X19, below).
4. The reference receiver records a conflict outcome per idempotency key, so after a name is
   released the same revision replays the conflict; publish a new revision. A namespace has one
   sentinel, so two routes cannot share a namespace. Both are existing protocol-1 behavior and are
   now in the guide.
5. The idempotency key does not include the namespace; a route is bound to one namespace, so this
   is harmless inside Hikyo, but it is why the transition test uses distinct route ids.

## Registered limits and HOLDs

- **R-1128-A (classical write path while both versions are served):** a stolen or forged Ed25519
  key can rewrite or prune values a route owns through the v1 path, including values delivered
  under v2, within the same route, generation and revision checks. The v2 authenticity claim holds
  only after v1 is retired on the receiver. Owning tickets: #1128, #1135 (closure).
- **R-1128-B (no rollback floor):** the registry is an operator file; an administrator can restore
  the v1 entry and retained targets resume classical delivery. DEC-11's floor does not exist for
  this surface, and the migration policy forbids rollback by stripping PQ verification. Owning
  tickets: #1128, #1135. "Never downgrades" is true for errors only.
- **Recipient ownership is not proven by the probe** (see acceptance).
- **Reference tool is not production software:** plaintext state file, whole-file rewrite (now
  fsynced), unbounded idempotency map; documented in the guide.
- **Test coverage of the reference receiver:** `internal/sealedreceiver` now has its own small
  package test; its protocol behavior is covered by the adapter and isolation tests.

- **X19:** strict `tlspolicy` does not reach this hop (PR #1171, `dunky13/pqc-1134-external-compat`).
  The client keeps `MinVersion` TLS 1.2 and system roots. No PQ transport or authenticity claim.
  No per-hop selector was built.
- **Suite:** only the dual suite exists. PQ-only (the DEC-03 end state) is a later suite and trust
  generation: not built.
- **Revision route (HOLD, owner):** DEC-18 states none for this owner. Proposal: a declared-amendment
  banner on `docs/adr/deployment-adapter.md`, the route #163 used, carrying section 11 once the
  implementation is accepted. The ADR is not edited here.
- **Threat model:** the "future quantum forger" attacker class named for the authenticity children
  is the parent's call; not landed.
- **Receiver reach (E-17):** third-party receivers cannot be observed, so the reader-first gate for
  them is an operator attestation.
- **Key custody:** sender keys are `0600` files; the reference receiver's state file is plaintext.
  Hybrid age file keys are 16 bytes (DEC-04 floor). Imported keys are accepted by path; the
  "maintained tool only" entropy rule of DEC-12 is not enforced by code.
- **Vectors:** ML-DSA signing is hedged and age is randomized, so the vector file is one frozen
  generated sample plus negatives (regenerate with `HIKYO_UPDATE_VECTORS=1`); Wycheproof vectors
  (DEC-09) are NOTRUN. The vector file contains one test-only age identity.
- **Scanner:** `AGE-SECRET-KEY-PQ-1` is not matched by the vendored rule (#1126).

## Key retention and rollback

Do not destroy the receiver's old identity while retained ciphertext needs it. Sender rollback is
the v1 registry entry (its fingerprint and generation) plus removing `signing_key_file_mldsa65`;
receiver rollback only to a build that reads v2. Never restore a retired pair: the receiver refuses
it after `not_after` and a changed generation fails closed on the sender. Retiring v1 on a receiver
is a separate recorded decision.

## Next authorized step

Independent Standards, Spec and security review of this draft, the owner's decision on the revision
route, and maintainer security approval. Then, only with explicit target authorization, a push to a
non-production receiver using the migration guide.

## Final checks

Repair candidate: PR head `bde65e00c` (base `f5d549182`, PR #1156). Read at 2026-11-03T23:30Z.

| Check | Result |
| --- | --- |
| Isolation `TestAdapterSealedWebhookProtocolTwoEndToEnd`, SQLite and PostgreSQL 18, through `agent-heavy`, on the final code | pass (31.7 s) |
| `go test` of `internal/boundary`, `internal/crypto/sealedhook`, `internal/adapter/sealedwebhook`, `internal/sealedreceiver`, `internal/config`, `cmd/hikyo-receiver-example` | pass |
| `go test -run SealedWebhook ./internal/app ./internal/config` | pass. The whole `internal/app` package hit the default 10 minute test timeout locally on an unrelated heavy test (`TestUnattendedPackagedEnrollmentUpgradeAndIntermediateCredential`, 3m42s under host load); it was not rerun locally. Hosted `feedback (go)` passed on the head |
| `scripts/ci/fast-feedback.sh` against base `f5d549182`, queue, pinned Node 26.12.1 | pass (Go format, package tests, docs) |
| `scripts/ci/check-commit-signatures.sh`, `scripts/ci/check-dco.sh` against the PR base | pass |
| `internal/lint` locally | not run (by instruction). Hosted `repository invariants (boundary, lint, CI policy)` on `bde65e00c`: **pass** (7m3s, run 41736052591) |
| Hosted `feedback (go/tooling/frontend)`, `ops-floor (amd64, arm64)` on `bde65e00c` | pass |
| Hosted `ci-queue-required` | fail: "Only the protected default branch can enter this merge queue." (stacked PR on a non-default base; not a code finding) |
| `scripts/ci/check-doc-status.mjs` | NOTRUN: the docs site dependencies are not installed on this host (fast-feedback's docs check passed) |
| amd64 benchmark, real receiver, live push, deployment | NOTRUN |
