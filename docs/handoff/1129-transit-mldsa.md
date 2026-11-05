# #1129: policy-bound ML-DSA-65 signing and verification in Transit, handoff

Status: **draft. The mechanism and the public surface (API, generated clients, CLI
help, WebUI option) are implemented in #1172; not merged, not marked ready.** The
owner's hold of 2026-11-04 (public surface held until the `/api/v1` freeze is
decided) ended when the owner chose option A for the freeze the same day, which
merged to `dev` as #1212 (OpenAPI enum, regenerated Go and TypeScript clients, the
closed freeze-guard list). The owner also accepted the signed-bytes departure
(below). The PR needs maintainer security approval before merge (**security
approval: not given**) and independent review of the new head. Owner and maintainer
are one person, so independent adversarial review compensates; cross-provider
review is SKIPPED by explicit owner instruction (2026-11-02) and is not CLEAN.
Migration 00083 is roll-forward only and **moves the rollback floor of every
instance that upgrades**, whether or not a key is created; see
[Key retention and rollback](#key-retention-and-rollback).
Operator-facing text is in [Transit ML-DSA-65 signing keys](../operations/transit-mldsa.md).

Refs [#1129](https://github.com/Hikyo-Org/Hikyo/issues/1129), parent
[#1123](https://github.com/Hikyo-Org/Hikyo/issues/1123); the PR does not close
either.

## Candidate and base

| Item | Value |
| --- | --- |
| Pull request | #1172, base `dev` (it was the stacked `dunky13/pqc-adr-amendments` until that merged). Public-surface round: `origin/dev` `833d39606` merged as `ad88cde54` with no conflict and no semantic repair; the new head is in the PR body. Earlier rounds were taken at fork point `e5539c280b37ea3f6e3a8371a949aad8190ff0ad`; no hosted run covers the merged tree until CI runs on the new head. Every PostgreSQL leg of the new tests is NOTRUN (see Tests and results) |
| First review round | `ae1855d2c962aca8ddb6f07166e237333c8b0edb` |
| Validated code revision | `8e11e9a36` (last code-validated commit); first repair `1f10525f7626503f8ce4a7f6d4e8a851986aa000`. Later commits change documentation (handoff, operations note, ADR banner) and tests only, except as listed in "Round 3" below |
| Source re-pin | `origin/main` is `dd41e86b5` (2026-11-03). Between the issue's historical assessment `2ac7bb36633d3ae0a99d7724968ca8c3cbf18334` (which is also the `v1.0.0` commit) and `origin/main`, nothing changed in the Transit crypto, custody, service, server or CLI files, `api/openapi.yaml`, the web Transit routes or the migrations directory (`git diff --stat` empty). `main` ends at migration 82, so 83 is free there |
| Inventory reconciliation (I22) | Re-ran the [census](../security/pqc/inventory.md#census-method-and-reconciliation) search 1 at `ae1855d2c`: 179 non-test Go files, the same count as the draft; the only Transit match is `internal/crypto/transit.go` (this change adds a `crypto/x509` import inside `internal/crypto`, inside the chokepoint). Search 4 over `api`, migrations, `clients` and `web/src` finds the I22 consumers (OpenAPI, generated Go and TypeScript clients, migrations 00065 and 00083, `web/src/api/transit.ts`, `web/src/routes/TransitKeyManagement.tsx`, `TransitOperations.tsx`); no new Transit consumer outside the I22 row. The row's bound note (message 64 KiB, context 1 KiB, wire 128 KiB; closed CHECK constraints and API enums) matches source |

## The freeze: decided, option A (2026-11-04)

The owner chose option 1 below (let the closed enum grow once, for `mldsa65` only).
It is recorded in a declared-amendment banner on
[api-cli-surface](../adr/api-cli-surface.md) (not owner-locked wording) and landed
on `dev` with #1212: `api/openapi.yaml` lists `mldsa65` on `TransitAlgorithm`, the
`public_key` description names both encodings, the Go and TypeScript clients are
regenerated, and `api/freeze.go` carries the closed list `MLDSA65EnumGrowth` of
exactly 20 findings without allowlisting `*-enum-value-added`. This PR does not add
to that list: `scripts/ci/check-api-freeze.sh` is run against it (see Tests). What
this PR adds on top is the mechanism's public use: the CLI and WebUI options,
corrected operation summaries (sign and verify named Ed25519 only), the lifecycle
tests and the skew case.

The rest of this section is the analysis as it stood before the decision, kept as
history. Its statement that the real server refuses `mldsa65` described this branch
before it was merged with `dev`, whose enum lets the value through to the service.

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

The api-cli-surface ADR (lines 146 to 148) says closed enums never grow and that
an old client keeps working against a new server. The decision register selected
`mldsa65` (DEC-02) and the Transit proposal says D2 gains it, but nothing
reconciles that with the v1 freeze. I did not weaken the guard, add an
allowlist entry or invent a second encoding. The options, with what each costs,
for the coordinating session to present (this handoff makes no recommendation):

1. **Let this closed enum grow, by amending api-cli-surface (#25).** Then add
   `mldsa65` to the OpenAPI enum and regenerate. Consequences:
   - It needs an exception to the freeze guard (the allowlist deliberately omits
     `*-enum-value-added`), which is a change to the guard and its policy, not only
     to the spec.
   - It breaks the rule that an old client works against a new server. The closed
     Zod enum sits inside the list schema: with the generated `v1.0.0`-shaped client
     schema, `zTransitKeyList.safeParse` of an environment holding one Ed25519 key
     and one `mldsa65` key fails the whole list (`items.1.algorithm: invalid_value`;
     the Ed25519-only list parses). So one ML-DSA key in an environment makes that
     environment's list unreadable to any consumer that parses with Zod, Ed25519
     keys included. That is narrower than "every old client": a decode into
     `apigen.TransitKeyList` succeeds with `mldsa65` (at `v1.0.0` the Go enum type is
     a plain string, and the CLI decodes into this type), and the `v1.0.0`
     TypeScript SDK has no response validator. The consumers that fail are the Zod
     ones, such as the WebUI (and any integrator who validates with the generated
     schemas). The same closed enum is used by `show`, `rotate`, `configure`,
     lifecycle and `trim` responses.
   - Client skew coverage does not exist for Transit today:
     `scripts/ci/frozen-client.mjs` and `internal/isolation/client_skew_e2e_test.go`
     never mention Transit, so the frozen-client lane stays green and gives no
     evidence either way. If the owner wants the old-client break above detected or
     accepted explicitly, that coverage must be added; it is not an existing gate.
   - `api/openapi.yaml` line 24890 describes `public_key` as "The Ed25519 public
     key"; the description must change (description changes are permitted) and the
     published encoding differs by algorithm (Ed25519 raw, ML-DSA-65 SPKI).
   - Then the work in [Remaining work](#remaining-work-and-next-authorized-step).
2. **Expose ML-DSA keys through a new endpoint family** (`endpoint-added` is
   permitted) whose responses use a new schema. Avoids changing the existing enum
   and keeps old clients working. The existing list and show routes must then not
   return ML-DSA keys, so those routes become incomplete views of the environment's
   keys; the CLI and WebUI need to read both families. A new surface to freeze.
3. **Wait for `/api/v2`.** No ML-DSA Transit key until then. The register says v1
   plans no v2.
4. **Keep the mechanism unexposed and decide later** (the state of this PR). Cost
   depends on whether #1172 merges meanwhile:
   - *If it merges:* migration 83 moves the rollback floor for every instance
     (downgrade means restoring a pre-83 archive) while nobody can create an
     `mldsa65` key, so every instance carries the cost and none gets the capability;
     the ADR amendment is operative for a mechanism no public surface reaches.
   - *If it does not merge:* the amendment stays inoperative, the mechanism and its
     tests stay on the branch and age against `main`, and migration number 83 stays
     reserved or must be renumbered later.

The refusal test `TestTransitPublicSurfaceHoldsMLDSA65{SQLite,Postgres}` that
proved this at the time was replaced by
`TestTransitPublicSurfaceMLDSA65{SQLite,Postgres}`, which keeps its Ed25519 half and
its refusal of `mldsa44`, `mldsa87` and `MLDSA65`, and now runs create, sign,
verify, rotate and list for `mldsa65` through the same real boot and CLI.

## What was built

| Area | Change |
| --- | --- |
| Crypto (`internal/crypto/transit.go`) | `TransitMLDSA65` in the closed set. `OpenTransitKey` feeds the HKDF-derived 32 bytes to `mldsa.NewPrivateKey(MLDSA65, seed)`; the algorithm is already in the HKDF info. `PublicKey` returns the 1952-byte encoding. `Sign` is hedged pure ML-DSA-65 with context `hikyo/transit/sign/v1`, behind a two-byte algorithm identifier `0x0905`. `VerifyTransitSignature` takes the algorithm of the key row and refuses wrong sizes, wrong identifier, bad public keys and unknown algorithms. `ExposeTransitPublicKey` publishes ML-DSA-65 as SPKI and leaves Ed25519 raw. Ed25519 behavior is byte identical |
| Server | The response builder publishes the key through `ExposeTransitPublicKey`; a stored key the library refuses fails the response instead of being published |
| Custody (`internal/transit`) | `PublicKeyFor` covers both signing algorithms; software and mock providers need no other change. Material stays 32 bytes sealed as before |
| Service | `transitAlgorithmOps` gains `mldsa65: sign, verify`; `Verify` takes the algorithm from the key row |
| Audit | `TransitAlgorithmValues` gains `mldsa65` (create event enum). Operation events are unchanged: sizes only |
| Migration 00083 (both engines) | Widens the `transit_keys.algorithm` CHECK. SQLite rebuilds the table (00067 pattern, `legacy_alter_table`); PostgreSQL drops and re-adds the constraint. No row changes. sqlc was regenerated: no generated query changed |
| Legacy drill fixture | `internal/app/backup_upgrade_drill_test.go` enumerated migrations 45 to 82 and the ledger rows to delete; it now covers 83. No schema reversal is needed because the fixture drops the transit tables whole |
| Build identity | `internal/buildcompat/development.json` regenerated against the CI-pinned `postgres:18@sha256:06cad38a...`; the diff is exactly the version-83 entry and the schema digest per engine. Without it every isolation test fails with "embedded migration bytes differ from verified build" |
| ADR | Banner on `docs/adr/transit.md`: the proposal text, marked owner-locked, plus a separate paragraph marked not owner-locked and neutral on the freeze outcome. "Operative" means the merge of #1172; the API, CLI and WebUI stay as they are. The proposal block in the migration ADR is a pointer |
| Public surface (public-surface round, after the freeze decision) | CLI: `--algorithm` lists `mldsa65` in `internal/cli/verbs.go`, `transit.go` and the golden `internal/cli/testdata/help.txt`. OpenAPI: the sign and verify summaries now say "Ed25519 or ML-DSA-65" (they named Ed25519 only); `api/apigen` and `clients/ts` regenerated, comment-only diffs, and `scripts/ci/check-api-freeze.sh` passes without growing `MLDSA65EnumGrowth`. WebUI: `TransitKeyManagement.tsx` offers a real `mldsa65` option (sign and verify only), `TransitKeys.tsx` notes that ML-DSA-65 public keys are SPKI and Ed25519 keys raw (the list already parses the algorithm through the generated schema), story `PostQuantumSigningKey`. PKI is untouched and still refuses `mldsa65` |
| Public-surface tests | `TestTransitPublicSurfaceMLDSA65{SQLite,Postgres}` (real boot, real CLI: create, sign, verify, refusals, rotate, show, list, independent SPKI verification); `TestTransitMLDSA65APILifecycle` (raw HTTP, `forEngines`); `TestMLDSA65ServiceLine` and `TestMLDSA65OverHTTP` flipped for Transit and still refusing PKI; the frozen-client skew lane gains an `mldsa65` list case and a `v1.0.0` SDK lifecycle on an `mldsa65` key; web unit and Storybook tests |
| Docs | `docs/operations/transit-mldsa.md`, `docs/site/src/content/docs/docs/transit.mdx`, the ADR banners (not-owner-locked text only), `docs/operations/pqc-staged-rollout.md`, the qualification packet, the #1123 ledger |

## Departures from the PQC profiles

The register has no row accepting these; I did not edit any DEC row. Details and
residual risk are in the [operations note](../operations/transit-mldsa.md#departure-from-the-pqc-profiles).

1. **Signed bytes (DEC-07, profiles.md "Domain separation").** The owner accepted
   this departure on 2026-11-04, so the signature format stays. Because the public
   surface is now open, the first `mldsa65` signature can be produced, which is the
   point after which a change needs a second signature format. The original
   reasoning follows. The profile says
   algorithm identifiers appear inside the signed bytes and the label is the first
   length-prefixed field of every signature input. Transit signs the caller's raw
   message, with the label only as the ML-DSA context and `0x0905` outside the
   signature. Conforming for `mldsa65` alone (signing
   `LP(label) || LP(algorithm) || message`) was possible while no caller existed, and
   Ed25519 stays untouched; the
   transit ADR (`transit.md` D4) says "pure Ed25519 over the message bytes" and the
   owner-locked amendment says nothing about the ML-DSA input. What is at stake is
   that every ML-DSA verifier, stock tools included, would have to rebuild the
   prefix, against a security gain that looks small (the context fixes the label
   and ML-DSA binds the public key into the signed hash; not analyzed further). This
   is an owner trade-off. **It is cheap only until the first `mldsa65` key signs**;
   after that a change needs a second signature format. The format was not changed.
   Owning ticket for any change: #1129.
2. **Public key encoding (DEC-07, profiles.md "ML-DSA public key").** My first
   round limited the SPKI rule to fingerprint surfaces; that limit is not in
   profiles.md, and conforming was cheap and correct (it changes no verification
   input), so **this is now conformed, not a departure**: the stored form stays raw
   (as Ed25519's does), and the published form (`ExposeTransitPublicKey`, used by
   the response builder) is SPKI for ML-DSA-65. Ed25519 stays raw 32 bytes for
   compatibility, so the `public_key` field is read by the key's `algorithm`. The
   `mldsa65:` text form and a key fingerprint are not provided: Transit publishes
   none today.
3. **The algorithm identifier is in the payload.** The profile requires wire formats
   to carry an algorithm id (done: `0x0905`, the profiles' TLS code point for
   ML-DSA-65). The proposal did not say where or which; this is my choice within
   the rule, not a departure, listed so the owner can veto the code point.

## Other choices to confirm

- **Context label** `hikyo/transit/sign/v1`, per the `hikyo/<surface>/<purpose>/v<N>`
  rule. Independent verifiers need this exact string.
- **Verification outcome classes.** A well-formed transit value whose signature is
  wrong for this key, algorithm, context or message answers `valid=false` (wrong
  key, other algorithm, wrong identifier or length, flipped byte). A malformed
  transit wire value or oversized input, a version the key does not have, a version
  below the floor, a compromised version, a key in the wrong state and unavailable
  custody are errors, not `valid=false`.
- **No hybrid or dual signature in Transit.** DEC-03 composition belongs to the
  consumer's protocol; Transit signs one message with one key.

## Acceptance

Legend: DONE, PARTIAL, HOLD (blocked by a decision not in the register), NOTRUN.
"Restart" below means a new service struct and keyring over the same datastore, not
a process restart.

| Criterion | Status | Notes |
| --- | --- | --- |
| Land the scoped ADR amendment; only approved parameter sets; distinct key identities; rotation never converts | PARTIAL | The text lands as a banner in the implementation commit, not before it, and the originating ticket is #156 (reopened) and the contract ticket #1124 is still open; the banner is operative only when #1172 merges (the not-owner-locked text now says so; the owner-locked paragraph is unchanged). `mldsa65` only; parse refuses `mldsa44/87`, case and padding variants; duplicate-name conflict across algorithms is tested; rotation keeps algorithm and key id (tested) |
| Software custody, sealed material encoding, public key representation, signature wire, algorithm/version binding; preserve AAD and HKDF | PARTIAL | Seed derivation, sealing, wire and binding are DONE and proven by independent vectors and transplant tests. Public key representation now follows DEC-07 (SPKI published). Signed bytes depart from profiles.md (above); the owner accepted that on 2026-11-04 |
| Propagate the closed enum and bounds: both schemas, generated queries, request/response validation, OpenAPI, clients, CLI, WebUI, audit, observability | DONE (local, SQLite) | Both schemas via migration 00083, sqlc (no change needed), service validation, audit enum, custody, metrics (label-free gauges, nothing to change). OpenAPI enum landed in #1212; this PR corrects the sign and verify summaries and regenerates `apigen` and `clients/ts` (comment-only). CLI help and golden, WebUI option, list parse and public-key note are in. The server accepts Transit `mldsa65` and still refuses `mldsa44`, `mldsa87`, `MLDSA65` and every PKI use. PostgreSQL leg of the surface tests NOTRUN locally |
| Create, sign, independent verify, rotate, minimum version, disable/retire/destroy, historical policy, export restriction | DONE (SQLite); PostgreSQL NOTRUN for the public-surface legs | Through the real service, store and audit on SQLite and PostgreSQL, both custody providers (`TestTransitMLDSA65EndToEnd`), including that version floors do not end signing while retire and disable do (`runTransitFloorsDoNotEndSigning`). Export: no path exists, `exportable` is refused, and the key view is checked not to carry sealed material. Through the public API (`TestTransitMLDSA65APILifecycle`), the real CLI (`TestTransitPublicSurfaceMLDSA65SQLite`) and the `v1.0.0` TypeScript SDK (frozen-client lane): create, sign, verify, tamper, rotate, list. The PostgreSQL legs are written and skip locally without `HIKYO_TEST_POSTGRES_DSN` |
| Grants, environment binding, admission, bounds, authorization before crypto, redacted audit | DONE (service level) | Reader, other org, sibling environment and per-key caller entries refused before any custody call (mock call counter unchanged). The audit search covers the message bodies, the `hikyo:v` text, the context label and, per run, every public key (standard and URL base64, hex) and a 60-character slice of every signature collected during the run. Admission budget path is shared with the other operations and unchanged |
| Custody contract: software and mock conformance, fail closed, no fallback, local public verification | DONE | Conformance extended to `mldsa65` (public key size, transplant with a positive control, unavailable sign); unavailable and unregistered custody refuse sign and verify and do no work. A mock is not evidence of an HSM or KMS |
| Malformed and wrong-sized keys and signatures, wrong algorithm, key/version/scope, substitution, duplicates, disabled/compromised, minimum-version boundaries | DONE | Table test (22 malformed cases), service-level cases, boundary at the floor |
| Restart, restore holds, project/instance re-encryption, rotation races, multi-node takeover on both engines | PARTIAL | DONE: restart, rotate-dek plus project reencrypt (material re-sealed, public key and earlier signatures unchanged), concurrent rotation (at least one winner, contiguous versions, distinct signing public keys). NOTRUN: restore-hold behavior with an ML-DSA key (no drill was run), multi-node takeover (single-process harness only), instance-wide reencrypt |
| Bounds fit actual encodings without weakening admission | PARTIAL | Wire value 4424 characters inside the 128 KiB cap; message cap unchanged; no global bound raised. Registered limit: `hikyo transit key list` reads at most 8 MiB of the unpaginated list, which ML-DSA reaches at about 3,080 versions in one environment (see the operations note). Open under #1129 |
| Document consumer migration, verifier compatibility, cutoffs, rollback floor, measured size and performance | PARTIAL | Operations note corrected for the floors (retire and disable end signing and verification, floors do not) and for the rollback floor (migration 83 moves it for every instance). NOTRUN: whether a release binary older than this change refuses a migration-83 database (`migrate.Check` has no production caller); amd64 not measured |
| Frozen maintained-reference vectors, fuzz/parser regressions, API/CLI/client/WebUI evidence, screenshots | PARTIAL | Vectors frozen from the Go standard library and verified with it independently of the Transit path, and locally with OpenSSL 4.0.3 (non-Go; not in CI). Two fuzz targets, whose oracle is now the frozen authentic triple. API, CLI, client and WebUI evidence: see Tests and results. Before and after screenshots of the Transit keys route (desktop 1280 and mobile 390, detail and create dialog) came from Storybook with mocked API fixtures, not a live server, and are kept out of Git. NOTRUN: pinned Wycheproof vectors (DEC-09 wants source, version and digest pinned) |

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
| Public key, stored | 32 bytes | 1952 bytes |
| Public key, published | 32 bytes | 1974 bytes (SPKI) |
| Signature payload | 64 bytes | 3311 bytes |
| Wire value (`hikyo:v1:...`) | 95 characters | 4424 characters |

The rate budget (600 per minute per principal) is unchanged and is the only
admission control; an ML-DSA sign costs about 16 times an Ed25519 sign in this
measurement.

## Tests and results

All commands run from the repository root with `go test -p 2`; PostgreSQL legs use
a throwaway `postgres:18-alpine` container (`HIKYO_TEST_POSTGRES_DSN`) that was
removed afterwards. "Round 1" is `ae1855d2c`; "round 2" is the validated code
revision `1f10525f7626503f8ce4a7f6d4e8a851986aa000`.

| Command | Round | Result |
| --- | --- | --- |
| `go test -p 2 -count=1 ./internal/crypto/... ./internal/transit/... ./internal/audit/ ./api/ ./internal/server/` | 1 and 2 | PASS (server 70 s). Round 2 adds the SPKI publication tests (`TestExposeTransitPublicKey`, `TestTransitKeyResponsePublishesTheAlgorithmEncoding`), the frozen-triple fuzz oracles and the conformance control |
| `go test -fuzz FuzzVerifyTransitMLDSA65 -fuzztime 10s`, `-fuzz FuzzParseTransitSignatureValue -fuzztime 8s` | 2 and 3 | PASS, no finding (short runs). Oracle: only the frozen authentic public key, message and payload bytes may verify, and the standard library must agree. The parse target compares the decoded payload with `bytes.Equal`, not the string, because `hikyo:v4:` plus the frozen payload also verifies (the key version is enforced by the service, `consumingVersion`, not by the signature); round 2 wrongly claimed only the frozen string could verify, which would have surfaced as a false finding in CI fuzzing |
| `go test -p 2 -parallel 2 -count=1 ./internal/store/migrate/` with PostgreSQL | 2 | PASS (56 s). The migration test now seeds exact material and public-key bytes and compares them byte for byte after the rebuild, and asserts that the exportable, custody, state, version-window, compromise, latest, deletion-deadline and rotation-period CHECKs still refuse, on SQLite and PostgreSQL |
| `go test -p 2 -parallel 2 -count=1 -run Transit ./internal/isolation/` with PostgreSQL | 2 | PASS (103 s): `TestTransitMLDSA65EndToEnd` (both engines, both custody providers, now including `floors_do_not_end_classical_signing`, the audit search for collected keys and signatures, the export-view check and the at-least-one-winner rotation assertion), the existing Transit end-to-end suite, and `TestTransitPublicSurfaceHoldsMLDSA65{SQLite,Postgres}` (real `app.Boot`, real CLI) |
| Mutation check of the audit search: add `mldsa65` to the searched needles | 2 | The audit subtest fails with "audit trail carries "mldsa65" in 9 events", so the search bites; reverted |
| Round 1 queued suites: `./internal/store/`, `-run Drill\|LegacyDrill ./internal/app/ ./internal/store/upgrade/ ./internal/upgradegate/...` with PostgreSQL, `go run ./scripts/release/compatibility --development --check internal/buildcompat/development.json` against the pinned PostgreSQL 18 image | 1 | PASS (store 306 s, app drill 485 s). Not rerun in round 2: nothing they cover changed |
| `openssl pkeyutl -verify -keyform DER` (OpenSSL 4.0.3) on the pinned SPKI `public_key_spki_hex`, the frozen signature and message, `-pkeyopt context-string:hikyo/transit/sign/v1` | 2 and 3 | Local, outside CI, run by me: "Signature Verified Successfully". Without the context option, with another context string, and with a tampered message: "Signature Verification Failure" (exit 1). Round 3 pins the SPKI bytes in the vector file (generated once by a stdlib-only program), so "frozen SPKI" is literally true and `TestExposeTransitPublicKey` compares the published bytes with it |
| Generated Zod schema proof for the freeze decision: `zTransitKeyList.safeParse` with one `ed25519` and one `mldsa65` item | 2 | Fails on `items.1.algorithm` (`invalid_value`); the Ed25519-only list parses |
| `node scripts/ci/check-doc-status.mjs --check` | 2 | PASS (35 ledger entries) |
| `scripts/ci/check-commit-signatures.sh` and `scripts/ci/check-dco.sh` against the PR base | 2 | PASS: 5 commits cryptographically verified, 5 signed off (head `99dcfe97f`) |
| `scripts/ci/fast-feedback.sh` (base `origin/dunky13/pqc-adr-amendments`) | 2 | PASS at `99dcfe97f` (clean tree, base `e5539c280`): diff check, Go formatting of 18 files, test compile of nine packages. The first attempt inside the validation job was refused because the checkout was dirty with the documentation edits, so it was rerun after committing them; the queue job that carried it therefore shows exit 1 although every test in it passed |
| `go test -p 2 -timeout 40m -count=1 ./internal/lint/...` | 2 | INCONCLUSIVE: `panic: test timed out after 40m0s` at `99dcfe97f` under queue and host load, with no `--- FAIL` in 1147 lines of output. Not rerun in round 3, as instructed. This is not a pass, and hosted lint did not run for round 2 either (next row) |
| `go test -p 2 -count=1 ./internal/boundary/...` | 3 | Round 2 broke it and I never ran it: `internal/server/transit_test.go` imported `internal/store` (new in `1f10525f7`), so `TestStoreImportAllowlist` and `TestForbiddenEdges` failed ("handlers cannot reach the datastore directly") locally at `8f5137bf4`. Round 3 builds the view by field assignment with `slices.Grow`, no `store` import. PASS at `8e11e9a36` (`./internal/boundary/...`, 9.7 s). Also at `8e11e9a36`, locally: `./internal/crypto/... ./internal/transit/... ./internal/audit/ ./api/ ./internal/server/` PASS; `-run Transit ./internal/isolation/` PASS on SQLite and PostgreSQL (112 s); `-run TransitMLDSA65 ./internal/store/migrate/` PASS on both engines |
| Hosted CI on `ae1855d2c` (round 1 head) | 1 | `repository invariants (boundary, lint, CI policy)` PASS, `ci-feedback` go, frontend and tooling PASS, `ops-floor` amd64 and arm64 PASS, DCO report PASS. `ci-queue-required` FAILS, as expected: the base is a stacked branch, not `main` |
| Hosted CI on `99dcfe97f` and `8f5137bf4` (round 2 heads) | 2 | `repository invariants (boundary, lint, CI policy)` **FAIL** at both, on the two boundary tests above; its step "Lint driver tests" was SKIPPED in both, so `internal/lint` has no hosted result for round 2 and a lint failure may have been hidden behind the boundary failure. My earlier text that hosted CI "runs lint" and was "pending" was wrong: the failure had finished before I wrote it and I had not read it. `ci-feedback` (go, frontend, tooling) and `ops-floor` (amd64, arm64) passed. `ci-queue-required` fails as expected (stacked base) |
| Hosted CI on the round 3 head | 3 | Read with `gh pr checks 1172` and `gh run view` for head `8e11e9a36`: `repository invariants (boundary, lint, CI policy)` **PASS** (8m13s) and its step "Lint driver tests" ran and PASSED, so `internal/lint` now has a hosted result; `ci-feedback` (go, frontend, tooling) PASS; `ops-floor` arm64 PASS (10m33s); `ops-floor` amd64 was still running when I looked; DCO report PASS. `ci-queue-required` still FAILS, as expected (stacked base). The final handoff-only commit that records this triggers another run that I have not read |

### Public-surface round (after the merge with `dev`)

Local, Apple M2 Pro, `go test -p 2 -parallel 2`. `HIKYO_TEST_POSTGRES_DSN` was not
set and Docker was not running, so **every PostgreSQL leg skipped (NOTRUN)**. The
test `TestTransitPublicSurfaceHoldsMLDSA65*` named in the round 2 rows was renamed
`TestTransitPublicSurfaceMLDSA65*` in this round.

| Command | Result |
| --- | --- |
| `go test -count=1 -run 'TestMLDSA65ServiceLine\|TestMLDSA65OverHTTP\|TestTransitPublicSurfaceMLDSA65SQLite\|TestTransitMLDSA65APILifecycle' ./internal/isolation/` | PASS on SQLite. `TestTransitPublicSurfaceMLDSA65Postgres` and the PostgreSQL legs of `TestTransitMLDSA65APILifecycle` skip |
| `go test -count=1 -run 'Transit\|MLDSA65' ./internal/store/migrate/ ./internal/isolation/` | PASS (migrate 0.8 s, isolation 28.6 s), SQLite. Migration 00083 is still the next number (`dev` ends at 82) and applies on SQLite |
| `go test -count=1 ./internal/cli/ ./api/ ./internal/crypto/... ./internal/transit/... ./internal/audit/ ./internal/server/ ./internal/boundary/... ./internal/service/` | PASS. The CLI help golden matches |
| `HIKYO_FROZEN_CLIENT_ROOT=<git archive v1.0.0 clients/ts> go test -count=1 -run TestFrozenGeneratedClientAgainstCurrentServer ./internal/isolation/` | PASS, SQLite leg. The Go decode of a list holding `mldsa65` succeeds |
| `node --test` on the skew evidence and cleanup tests | 5 pass. `scripts/ci/check-client-skew.sh` needs a PostgreSQL DSN and was NOTRUN as a whole; the `v1.0.0` Zod rejection and SDK lifecycle in `scripts/ci/frozen-client.mjs` ran only inside the Go test above |
| `sh scripts/ci/check-api-freeze.sh` | PASS, `MLDSA65EnumGrowth` unchanged (20 findings) |
| `node scripts/ci/check-doc-status.mjs --check` | PASS (35 entries) |
| Web: `vitest` Transit unit tests (18 across two files), `vitest --project storybook` Transit story test (9), `tsc --noEmit`, oxlint, `design/markup-check`, `adherence-check` | PASS |
| Mutation checks | Publishing the raw key instead of SPKI fails `TestTransitMLDSA65APILifecycle`; removing the web option or the SPKI note fails the web tests; running the skew lane with the HEAD client as the "frozen" client fails with "must reject a list holding mldsa65". Each was restored |
| `internal/lint`, hosted CI, the buildcompat PostgreSQL 18 digest check | NOTRUN locally (lint is not run locally; Docker was down). The PR's required check only compiles Go tests (`go test -c`), so the frozen-client lane, the freeze guard and the PostgreSQL legs first run in post-merge `ci-dev` |

Round 1 local lint: it passed once (499 s) before a comment-only edit, then two
reruns did not complete (the default 10 minute test timeout under host load; then
the 30 minute background limit). A reviewer's local run also timed out at 40
minutes under load with no `--- FAIL`, which is inconclusive, not a pass. Hosted
`repository invariants (boundary, lint, CI policy)` passing at `ae1855d2c` is the
only completed lint evidence for that round.


## Round 3 (second re-review)

Changes since `8f5137bf4`: server test no longer imports `internal/store`
(boundary); sealed-material check in the end-to-end export-view test queries per
version and was proven by injecting each version's blob into the rendered view
(both injections fail the test with "v3: the key view carries sealed material"; the
clean tree passes); parse fuzz oracle compares payload bytes; SPKI pinned in the
vectors; the signed-bytes departure reasoning, the freeze options (including
option 4's cost in both cases and the narrowed Zod claim, checked against the
`v1.0.0` Go and TypeScript generated code), the `disable` guidance for local
verifiers, a sentence on response building under the FIPS module, and wording
fixes. No crypto, migration or signature format change.

## Migration number

This PR takes **00083** on both engines (sqlite and postgres). `main` ends at 82.
A sibling that also adds a migration will collide: renumber, and regenerate
`internal/buildcompat/development.json` again (it holds a digest per version and a
schema digest per engine), then rerun the lint pins.

## Key retention and rollback

- No production key material, key creation, provider write or deployment was made.
  Throwaway databases and PostgreSQL containers were created and removed.
- Migration 00083 is roll-forward only and applies to every instance that upgrades,
  whether or not an `mldsa65` key is ever created: it rebuilds `transit_keys`.
  **Merging moves the rollback floor for everyone.** The downgrade path is
  restoring an archive from before migration 83 (see the upgrades guide), not a
  down migration. Whether a release binary older than this change refuses a
  migration-83 database is NOTRUN. The real path is the upgrade gate
  (`upgradegate`, `upgradebundle.Load`, `inspectSource`, `appliedMatches`); `migrate.Check`
  has no production caller, and the base `app.Boot` refusal observed in development was
  for custody reasons, not schema.
- Backups carry sealed `mldsa65` material like all transit material; trim and
  destroy erase live material only.
- **Zeroization.** `Destroy` zeroes the AEAD, HMAC and Ed25519 key bytes. It cannot
  zero an ML-DSA key: `crypto/mldsa` keeps the seed and the expanded key inside the
  library and has no erase method, so they stay in the Go heap until reused. Our own
  copy of the derived seed and the opened material are zeroed. Residual risk,
  owner #1129, recheck at every Go minor release and when `crypto/mldsa` gains an
  erase method; recorded in the operations note.
- `crypto/mldsa` is unavailable under the frozen FIPS module v1.0.0: key opening and
  public-key parsing then fail, so creation and signing refuse and verification
  answers false. No fallback exists.

## Remaining work and next authorized step

The freeze and signed-bytes decisions are made (2026-11-04). What is left is
registered open, with its risk; none of it is built in this PR.

1. **Hosted CI, PostgreSQL legs and the new head.** Run `ci-feedback` and the PostgreSQL
   legs of `TestTransitPublicSurfaceMLDSA65Postgres`, `TestTransitMLDSA65APILifecycle`
   and `scripts/ci/check-client-skew.sh` (needs a DSN). Risk: an engine-specific
   difference is not yet observed.
2. **Pin Wycheproof ML-DSA vectors** (source, version, digest; DEC-09). Risk: the
   vectors in the tree come from the Go standard library only, plus a local OpenSSL
   check outside CI.
3. **Restore-hold behavior with an ML-DSA key.** No restore drill was run. Risk: a
   restore that holds or drops Transit material is untested for `mldsa65`.
4. **Instance-wide reencrypt** with an ML-DSA key (project reencrypt is covered).
   Risk: sealed material is not proven re-sealed at instance scope.
5. **Multi-node takeover** with an ML-DSA key (single-process harness only). Risk:
   custody handover between nodes is untested for `mldsa65`.
6. **Release-binary refusal against a migration-83 database.** Risk: whether a binary
   older than this change refuses the migrated database is unknown: no test drives the
   upgrade gate against it. The rollback floor moves regardless; see above.
7. **Owner thresholds** from a target-machine baseline (amd64 included; not measured).
8. **Maintainer security approval** and **independent adversarial review of the new
   head**. Cross-provider review stays SKIPPED, not CLEAN.
9. **Consumers.** One `mldsa65` key in an environment makes that environment's key
   list unreadable to any consumer that validates with the `v1.0.0` Zod schemas
   (accepted, option A). The WebUI in this PR parses it; integrators must update
   their generated schemas first.
10. **The CLI key list cap.** `hikyo transit key list` reads at most 8 MiB of an
    unpaginated response, which ML-DSA reaches at about 3,080 versions in one
    environment. Risk: past that the command fails with an internal error, while
    `hikyo transit key show` and the API still work. See the
    [operations note](../operations/transit-mldsa.md#registered-limit-the-cli-key-list).
