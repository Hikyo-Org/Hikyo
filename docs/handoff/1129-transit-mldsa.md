# #1129: policy-bound ML-DSA-65 signing and verification in Transit, handoff

Status: **draft implementation of the mechanism. The public API, CLI and WebUI
surface is HOLD. Not merge-ready, not operative.** It needs maintainer security
approval before merge (**security approval: not given**). Owner and maintainer are
one person, so independent adversarial review compensates; cross-provider review
is SKIPPED by explicit owner instruction (2026-11-02) and is not CLEAN.
Operator-facing text is in [Transit ML-DSA-65 signing keys](../operations/transit-mldsa.md).

Stacked on the ADR amendment branch (PR #1155). Refs
[#1129](https://github.com/Hikyo-Org/Hikyo/issues/1129); the PR does not close it.

## The finding that shapes this PR

`v1.0.0` exists and the freeze guard is armed. `TransitAlgorithm` is a closed
enum on every transit key response. Adding `mldsa65` to it, and converting it to
an open `x-extensible-enum`, were both run against the guard
(`go run ./scripts/ci/api-freeze-guard --base <v1.0.0 spec> --revised <edit>`) and both
fail:

```
[request-property-enum-value-added] POST .../transit-keys: added the new `mldsa65` enum value to the request property `algorithm`
[response-property-enum-value-added] GET .../transit-keys ... (and the show, configure, lifecycle, rotate and trim responses; 7 response paths)
```
```
[request-property-became-not-enum] POST .../transit-keys: removed the enum constraint from the request property `algorithm`
[response-property-became-not-enum] ... (the same 7 response paths)
```

The api-cli-surface ADR says closed enums never grow. The decision register
selected `mldsa65` (DEC-02) and the Transit proposal says D2 gains it, but nothing
reconciles that with the v1 freeze. I did not weaken the guard, add an
allowlist entry or invent a second encoding. **Decision needed from the owner**
(options, recommendation first):

1. Amend [api-cli-surface](../adr/api-cli-surface.md) (#25) to let this one
   closed enum grow once for the post-quantum program, with a new bound profile,
   then add `mldsa65` to the OpenAPI enum, regenerate Go and TypeScript clients,
   and extend the CLI help and WebUI list. Smallest change to the contract; every
   existing generated client already ignores unknown fields but a Zod client with
   the closed enum would reject a response carrying `mldsa65`, so old clients break
   only when such a key exists. Recommended.
2. Expose ML-DSA keys through a new endpoint family (`endpoint-added` is
   permitted) whose responses use a new schema. Avoids the enum change but forks
   the Transit API and the existing list/show responses would have to hide those
   keys, which is a visible gap.
3. Wait for `/api/v2`. The register says v1 plans none.

Until then the mechanism is complete below the API and the real server refuses
`mldsa65`: `TestTransitPublicSurfaceHoldsMLDSA65{SQLite,Postgres}` boot the
application, drive the real CLI, and show ed25519 create/sign/verify unchanged
while `mldsa65`, `mldsa44`, `mldsa87` and `MLDSA65` are refused with "the request
does not satisfy the API contract (algorithm)" and no row is created.

## What was built

| Area | Change |
| --- | --- |
| Crypto (`internal/crypto/transit.go`) | `TransitMLDSA65` in the closed set. `OpenTransitKey` feeds the HKDF-derived 32 bytes to `mldsa.NewPrivateKey(MLDSA65, seed)`; the algorithm is already in the HKDF info. `PublicKey` returns the 1952-byte encoding. `Sign` is hedged pure ML-DSA-65 with context `hikyo/transit/sign/v1`, behind a two-byte algorithm identifier `0x0905`. `VerifyTransitSignature` now takes the algorithm of the key row (signature changed; callers were the service and tests) and refuses wrong sizes, wrong identifier, bad public keys, unknown algorithms. Ed25519 behavior is byte identical |
| Custody (`internal/transit`) | `PublicKeyFor` covers both signing algorithms; software and mock providers need no other change. Material stays 32 bytes sealed as before |
| Service | `transitAlgorithmOps` gains `mldsa65: sign, verify`; `Verify` takes the algorithm from the key row |
| Audit | `TransitAlgorithmValues` gains `mldsa65` (create event enum). Operation events are unchanged: sizes only |
| Migration 00083 (both engines) | Widens the `transit_keys.algorithm` CHECK. SQLite rebuilds the table (00067 pattern, `legacy_alter_table`); PostgreSQL drops and re-adds the constraint. No row changes. sqlc was regenerated: no generated query changed |
| Legacy drill fixture | `internal/app/backup_upgrade_drill_test.go` enumerated migrations 45 to 82 and the ledger rows to delete; it now covers 83. No schema reversal is needed because the fixture drops the transit tables whole |
| Build identity | `internal/buildcompat/development.json` regenerated against the CI-pinned `postgres:18@sha256:06cad38a...`; the diff is exactly the version-83 entry and the schema digest per engine. Without it every isolation test fails with "embedded migration bytes differ from verified build" |
| ADR | Banner on `docs/adr/transit.md` (the proposal text plus one sentence that the public surface waits on the freeze, which is wider than the proposal); the proposal block in the migration ADR is now a pointer |
| Docs | `docs/operations/transit-mldsa.md` |

## Choices inside the register, to confirm

- **Signature algorithm identifier.** The proposal says the signature carries
  algorithm and parameter set. Raw 3309 bytes do not. I used the two-byte
  big-endian TLS SignatureScheme code point `0x0905`, which the profiles already
  list for ML-DSA-65, as the first bytes of the payload. Ed25519 payloads stay the
  bare 64 bytes. This is a wire detail the register did not spell out.
- **Context label** `hikyo/transit/sign/v1`, per the `hikyo/<surface>/<purpose>/v<N>`
  rule. Independent verifiers need this exact string.
- **Public key representation** is the raw FIPS 204 encoding in the same field the
  Ed25519 key uses (standard base64 at the API when exposed), not SPKI or the
  `mldsa65:` hex text. DEC-07's SPKI and fingerprint rules are candidates for
  surfaces that publish fingerprints; Transit publishes none, so no fingerprint
  was added.
- **Verification outcome classes.** As for Ed25519, an inauthentic or malformed
  signature of the right wire shape answers `valid=false`; only a malformed
  transit wire value, an oversized input, a missing version, a compromised
  version, a version below the floor or unavailable custody are errors.
- **No hybrid or dual signature in Transit.** DEC-03 composition belongs to the
  consumer's protocol; Transit signs one message with one key. Documented in the
  operations doc.

## Acceptance

Legend: DONE, PARTIAL, HOLD (blocked by a decision not in the register), NOTRUN.

| Criterion | Status | Notes |
| --- | --- | --- |
| Land the scoped ADR amendment; only approved parameter sets; distinct key identities; rotation never converts | DONE | Banner on transit ADR; `mldsa65` only; parse refuses `mldsa44/87`, case and padding variants; duplicate-name conflict across algorithms tested; the key id and algorithm are unchanged by rotation (tested) |
| Software custody, sealed material encoding, public key representation, signature wire, algorithm/version binding; preserve AAD and HKDF | DONE | Seed derived under the existing info; vectors prove the derivation independently; version relabel and cross-key/version/env/project/algorithm transplant tests |
| Propagate the closed enum and bounds: both schemas, generated queries, request/response validation, OpenAPI, clients, CLI, WebUI, audit, observability | PARTIAL | DONE: both schemas via migration 00083, sqlc (no change needed), service validation, audit enum, custody, metrics (label-free gauges, nothing to change). **HOLD: OpenAPI, generated Go/TS clients, CLI help, WebUI list** (freeze, above). The real server refusal is tested |
| Create, sign, independent verify, rotate, minimum version, disable/retire/destroy, historical policy, export restriction | PARTIAL | DONE through the real service, store and audit on SQLite and PostgreSQL, both custody providers (`TestTransitMLDSA65EndToEnd`). Export: no path exists and `exportable` is refused (tested). **HOLD: the same through the public API, CLI and generated clients** |
| Grants, environment binding, admission, bounds, authorization before crypto, redacted audit | DONE (service level) | Reader, other org, sibling environment and per-key caller entries refused before any custody call (mock call counter unchanged); audit holds no signature, message or `hikyo:v` text. Admission budget path is shared with the other operations and unchanged |
| Custody contract: software and mock conformance, fail closed, no fallback, local public verification | DONE | Conformance extended to `mldsa65` (public key size, transplant, unavailable sign); unavailable and unregistered custody refuse sign and verify and do no work. A mock is not evidence of an HSM or KMS |
| Malformed and wrong-sized keys and signatures, wrong algorithm, key/version/scope, substitution, duplicates, disabled/compromised, minimum-version boundaries | DONE | Table test (22 malformed cases), service-level cases, boundary at the floor |
| Restart, restore holds, project/instance re-encryption, rotation races, multi-node takeover on both engines | PARTIAL | DONE: restart (fresh service over the same store), rotate-dek plus project reencrypt (material re-sealed, public key and earlier signatures unchanged), concurrent rotation. NOTRUN: restore-hold behavior with an ML-DSA key (the material shape is identical to existing transit material; no drill was run), multi-node takeover (single-process harness only), instance-wide reencrypt |
| Bounds fit actual encodings without weakening admission | DONE | Wire value 4424 characters inside the 128 KiB cap; message cap unchanged; no global bound raised |
| Document consumer migration, verifier compatibility, cutoffs, rollback floor, measured size and performance | DONE | Operations doc and numbers below. Rollback behavior of an older binary against a database holding an `mldsa65` key is NOTRUN |
| Frozen maintained-reference vectors, fuzz/parser regressions, API/CLI/client/WebUI evidence, screenshots | PARTIAL | Vectors frozen from the Go standard library and verified with it independently of the Transit path; two fuzz targets (public key and payload decoding, wire parser plus verifier). NOTRUN: pinned Wycheproof vectors (DEC-09 wants source, version and digest pinned; not fetched), a non-Go implementation. **HOLD: API/CLI/client/WebUI evidence.** Screenshots: not applicable, no rendered UI changed |

## Measured baseline (DEC-17)

Local, noisy: Apple M2 Pro (arm64, 12 cores), Go 1.27.3, host load average 12 to
17 from other jobs, `go test -bench TransitSignAndVerify -benchtime 300x -count 3`
(`internal/crypto/transit_sign_bench_test.go`). Medians of three runs. "Sign"
includes opening the key (HKDF plus expansion) because every custody operation
does that; "verify" includes parsing the public key. No threshold is proposed:
the owner sets them from a baseline measured on the target machines (amd64 not
measured).

| Operation | ed25519 | mldsa65 |
| --- | --- | --- |
| Open derived key | 16 us | 160 us |
| Sign, 256 byte message | 35 us | 575 us |
| Sign, 64 KiB message | 126 us | 650 us |
| Verify, 256 byte message | 41 us | 113 us |
| Verify, 64 KiB message | 86 us | 188 us |

| Size | ed25519 | mldsa65 |
| --- | --- | --- |
| Public key | 32 bytes | 1952 bytes |
| Signature payload | 64 bytes | 3311 bytes |
| Wire value (`hikyo:v1:...`) | 95 characters | 4424 characters |

The rate budget (600 per minute per principal) is unchanged and is the only
admission control; an ML-DSA sign costs about 16 times an Ed25519 sign in this
measurement.

## Tests and results

| Command | Result |
| --- | --- |
| `go test -p 2 -count=1 ./internal/crypto/... ./internal/transit/... ./internal/audit/ ./api/ ./internal/server/` | PASS |
| `go test -fuzz FuzzVerifyTransitMLDSA65 -fuzztime 20s`, `-fuzz FuzzParseTransitSignatureValue -fuzztime 15s` | PASS, no finding (short runs) |
| `go test -p 2 -count=1 ./internal/store/migrate/ -run TransitMLDSA65` with `HIKYO_TEST_POSTGRES_DSN` (throwaway PostgreSQL 18 container) | PASS on SQLite and PostgreSQL: rows, versions and callers survive, the CHECK admits only `mldsa65` beyond the old set, child foreign keys, cascades and the live-name index survive |
| `go test -p 2 -count=1 ./internal/isolation/ -run TestTransitMLDSA65EndToEnd` | PASS on both engines and both custody providers |
| `go test -p 2 -count=1 ./internal/isolation/ -run TestTransitPublicSurfaceHolds` | PASS: real `app.Boot`, real CLI, both engines |
| `go test -p 2 -count=1 ./internal/lint/...` | PASS once (499 s under host load) with no `reviewed_pins.json` change: no raw SQL or reviewed function changed. That run preceded one comment-only edit in `internal/lint/reencrypt_coverage.go` and the drill-fixture test edit. Two reruns of the final tree did not complete: the first hit the default 10 minute test timeout under host load (`panic: test timed out`), the second (30 minute timeout, queued) was stopped by the background limit. Final-tree lint is therefore NOTRUN; hosted CI will run it |
| `go run ./scripts/release/compatibility --development --check internal/buildcompat/development.json` against the pinned PostgreSQL 18 image | PASS after regeneration |
| Queued (`agent-heavy`) `go test -p 2 -parallel 2 -run Transit ./internal/isolation/`, then `./internal/store/migrate/ ./internal/store/`, then `./internal/service/ ./internal/conformance/ ./internal/app/` (the last three matched no Transit-named test) with PostgreSQL | PASS |
| Queued `go test -p 2 -parallel 2 -run 'Drill\|LegacyDrill' ./internal/app/ ./internal/store/upgrade/ ./internal/upgradegate/...` with PostgreSQL | PASS (app 485 s) |
| `scripts/ci/fast-feedback.sh`, signature and DCO scripts, doc status | see the end of this file |

## Migration number

This PR takes **00083** on both engines (sqlite and postgres). A sibling that also
adds a migration will collide: renumber, and regenerate
`internal/buildcompat/development.json` again (it holds a digest per version and a
schema digest per engine), then rerun the lint pins.

## Key retention and rollback

- No production key material, key creation, provider write or deployment was made.
  Throwaway databases and two PostgreSQL containers were created and removed.
- Migration 00083 is roll-forward only. Once an `mldsa65` key exists, rollback of
  the binary means restoring a backup taken before the key. Old-binary behavior on
  such a database was not run.
- Backups carry sealed `mldsa65` material like all transit material; trim and
  destroy erase live material only.
- The ML-DSA private key object inside `crypto/mldsa` cannot be zeroed; `Destroy`
  drops the reference (comment `shortcut:` in the code). Seeds and derived bytes
  are zeroed.
- The standard library's `crypto/mldsa` is unavailable under the frozen FIPS
  module v1.0.0: key opening and public-key parsing then fail, so creation and
  signing refuse and verification answers false. No fallback exists.

## Remaining work and next authorized step

1. Owner decision on the freeze (options above). Then OpenAPI enum, regenerate Go
   server types and the TypeScript client (`pnpm --dir clients/ts generate`), CLI
   help text and its golden `internal/cli/testdata/help.txt`, the WebUI algorithm
   list in `web/src/routes/TransitKeyManagement.tsx`, and real API/CLI/client
   lifecycle tests on both engines; screenshots then apply (matching route, state
   and viewport, kept out of Git).
2. Pin Wycheproof ML-DSA vectors (source, version, digest) and run a second
   implementation against the frozen vectors.
3. Restore-hold, instance reencrypt and multi-node takeover runs with an ML-DSA key.
4. Owner thresholds from a target-machine baseline (amd64 included).
5. Maintainer security approval and independent adversarial review.
