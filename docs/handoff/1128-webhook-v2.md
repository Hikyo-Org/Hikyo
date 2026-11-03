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
| Crypto (`internal/crypto/sealedhook/v2.go`) | New file beside v1. Dual suite `ed25519+mldsa65` (both required), hybrid recipient, version-separated path, media types and labels, `ed25519-sha256:` / `mldsa65-sha256:` / `mlkem768x25519-sha256:` ids, `swh2-...` fingerprint, `ciphertext_digest`, exact-length fields checked before base64 decoding, one-stanza age header allowlist with a 4096-byte header bound (copy of the #1126 scan: this leaf package may not import `crypto/backup`), duplicate-member refusal. v1 code only gained shared validators (`validateScope`, `validateWindow`, `validateStatus`, `destinationID`, `idempotencyKey`) |
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

## Acceptance

Legend: DONE, PARTIAL, HOLD, NOTRUN.

| Criterion | Status | Notes |
| --- | --- | --- |
| Specify a new protocol version with authenticated identifiers, scope, times, replay context, ciphertext digest, ack binding; refuse unknown versions and cross-version reinterpretation | DONE (text), review NOTRUN | Spec 11.1 to 11.4. Replay context is the idempotency key (no separate nonce, deliberately). Independent review has not run |
| Approved hybrid recipient and signatures; validate pinned identities and key ownership before enabling | DONE | Boot fingerprint confirmation; key parsing; ownership of receiver keys is proven by the `adapter test` probe (a dual-signed ack bound to the request) |
| Protected key generation and import, algorithm-aware fingerprints; target configuration, API, CLI, UI and reference tooling | PARTIAL | Registry, fingerprints and reference tooling DONE. No API, CLI or UI consumes the registry (nothing to change). **No product-binary generator** for the sender keys: `sender-keygen` lives in the unshipped reference tool; adding a `hikyo` CLI verb would add frozen surface. Keys are `0600` files, the v1 custody model, not sealed under the instance hierarchy |
| Reject classical-only encryption, stripping, wrong key, other algorithm/target/version, mixed unlock paths; dual signatures all-required | DONE | Crypto, adapter and vector negatives; two mutations (skip ML-DSA on envelopes, on acks) each fail tests |
| Bounds for hybrid and ML-DSA sizes; malformed, overlarge, truncated, expired, future, tampered refused without unsafe allocation | DONE | Spec 11.5; largest legal envelope 1,407,839 of 1,529,176 bytes; fuzz targets for envelope and ack (15 s and 10 s local runs, no finding) |
| Preserve authN/Z, egress, destination pinning, INTENT/OUTCOME audit, fences, retry, duplicate suppression, ambiguity | DONE | Journal path unchanged; exercised by the isolation test on both engines |
| Exercise encrypt, send, receiver verify/decrypt, signed ack, reconcile; retry/restart, duplicate delivery, concurrent key rotation, multi-node takeover | DONE (local) | `TestAdapterSealedWebhookProtocolTwoEndToEnd` on SQLite and PostgreSQL with the real service, stores, outbox, worker, egress dialer and TLS against the in-tree receiver |
| Keep v1 targets; PQ-required targets never downgrade; v1 data only under its policy | DONE | v1 tests and KAT unchanged and passing; skew matrix; no v1 request after any v2 error |
| Skew, reader-first, key overlap and retirement, rollback to a v2-capable reader, restore of custody without reviving authority | DONE (local) | `TestV2VersionSkewMatrix`, `TestV2SenderPairRotationOverlapAndRetirement`, `TestV2TrustBoundaryChangesFailClosedBeforeAnyRequest`, isolation steps 6 and 7 |
| Publish vectors, receiver migration instructions, exact candidate evidence; no live push or production key change | DONE | No live target was contacted and no production key was made |

## Evidence

Local on Apple M2 Pro (arm64), Go 1.27.3, host load 10 to 12 from other jobs.

| Class | Result |
| --- | --- |
| Unit and adapter | `go test ./internal/crypto/sealedhook ./internal/adapter/sealedwebhook ./internal/config ./internal/app ./cmd/hikyo-receiver-example`: pass |
| Both engines | `TestAdapterSealedWebhookProtocolTwoEndToEnd` (SQLite, PostgreSQL 18) via `agent-heavy`: pass |
| Cross-implementation | ML-DSA-65 signatures and context verified with OpenSSL 3.5.9; ciphertext decrypts with the `age` CLI v1.3.2 (same upstream as the library, so not independent for age) |
| Lint, pins, fast-feedback, signatures, DCO, doc status | see "Final checks" |
| Hosted CI, real receiver, deployment | NOTRUN |

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

## Registered limits and HOLDs

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

Candidate: the head of the PR branch when this file was written; the PR body names the exact
head and the result of every check below. Base `f5d549182` (PR #1156).

| Check | Result |
| --- | --- |
| `scripts/ci/fast-feedback.sh` against base `f5d549182`, through the queue, under the pinned Node 26.12.1 | pass (Go format, package tests of the changed set, docs) |
| `scripts/ci/check-commit-signatures.sh`, `scripts/ci/check-dco.sh` against the PR base | pass, 4 commits at the time |
| `go test ./internal/boundary/...` | pass |
| `go test -p 2 -timeout 40m ./internal/lint/...` | **did not finish**: the 40 minute timeout fired during package loading under host load (load 10 to 12). A second run with `-timeout 100m` is recorded in the PR body. No pin was refreshed: this branch changes no migration and no raw SQL |
| `scripts/ci/check-doc-status.mjs` | NOTRUN: the docs site dependencies are not installed on this host |
| Hosted CI, amd64, real receiver, live push | NOTRUN |
