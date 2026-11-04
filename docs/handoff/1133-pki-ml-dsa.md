# #1133: ML-DSA-65 private certificate lifecycle, handoff

Status: **implementation of the mechanism, below the API. The public API, CLI and
WebUI surface is HOLD on the owner's API-freeze decision.** The owner authorized
the merge to `dev` on 2026-11-04, accepted publication in the next nightly, and
that instruction is the maintainer security approval for this PR. What merging
means: `MLDSAEnabled` stays false in production, so the product creates, imports
and rotates no ML-DSA key; the locked relying-party matrix condition is unmet
(choice 4 below, criterion 9 PARTIAL); the five implementation choices below stay
open and can still change; the open decisions listed under "Next authorized step"
are undecided. **One behavior reaches production without the opt-in:** the
chain-path check in `VerifyCA` (`internal/service/pki.go`, create-intermediate with
`--parent`, install and import) does not consult the gate, so an operator who never
enabled ML-DSA now gets a refusal ("the path mixes classical and post-quantum keys
or signatures") when importing, rotating or installing a classical issuer under an
ML-DSA root; before this change that was accepted. A classical chain under a
classical root is unaffected. Owner and maintainer are one person, so independent
adversarial review compensates; cross-provider review is SKIPPED by explicit
owner instruction (2026-11-02) and is not CLEAN. Operator-facing text:
[Private PKI with ML-DSA-65](../operations/pki-ml-dsa.md).

Continues draft PR #1141 (branch `draft/issue-1133`), which held a pre-decision
proposal; the decision register wins where they differed, and the proposal file
`docs/adr/pki-ml-dsa.md` is deleted because the banner on `docs/adr/pki.md`
replaces it. Stacked on the transport-policy branch (PR #1156) by a signed merge
commit. Refs [#1133](https://github.com/Hikyo-Org/Hikyo/issues/1133); the PR does
not close it.

## The finding that shapes this PR

`v1.0.0` exists and `/api/v1` is additive-only. The PKI `key_algorithm` enum on
issuer create and rotate, and `key_algorithms` on profile create and update,
certificate issue and every profile response, are closed. Adding `mldsa65`, or
turning them into open enums, were both run against the guard
(`go run ./scripts/ci/api-freeze-guard --base <v1.0.0 spec> --revised <edit>`
on throwaway copies; `api/openapi.yaml` is unchanged) and both fail.

Adding the value (12 findings, abridged):

```
[request-property-enum-value-added] POST /api/v1/instance/pki/issuers: added the new `mldsa65` enum value to the request property `key_algorithm`
[request-property-enum-value-added] POST /api/v1/instance/pki/issuers/{issuer}/rotate: ... `key_algorithm`
[request-property-enum-value-added] POST /api/v1/instance/pki/profiles: ... `policy/key_algorithms/items/`
[request-property-enum-value-added] PUT /api/v1/instance/pki/profiles/{profile}: ... `policy/key_algorithms/items/`
[request-property-enum-value-added] POST .../environments/{environment}/certificates: ... `key_algorithm`
[response-property-enum-value-added] GET|POST /api/v1/instance/pki/profiles, GET|PUT .../profiles/{profile}, POST|DELETE .../profiles/{profile}/bindings..., GET .../certificate-profiles: ... `policy/key_algorithms/items/` (7 response paths)
```

Converting to `x-extensible-enum` (12 findings, abridged):

```
[request-property-became-not-enum] POST /api/v1/instance/pki/issuers: removed the enum constraint from the request property `key_algorithm`
... the same five request properties, then [response-property-became-not-enum] on the same seven profile response paths
```

I did not weaken the guard, add an allowlist entry or invent a second encoding.
Note the issuer and certificate **response** `key_algorithm` fields are plain
strings, not enums, so an ML-DSA issuer or certificate would already serialize;
the closed parts are the request enums and the profile `key_algorithms`.

**Decision needed from the owner.** This handoff makes no recommendation; the
facts for each option, verified against this tree and `v1.0.0`:

1. **Let these closed enums grow, by amending [api-cli-surface](../adr/api-cli-surface.md) (#25).**
   Then add `mldsa65` to the OpenAPI enums and regenerate.
   - It needs an exception to the freeze guard, whose allowlist deliberately omits
     `*-enum-value-added`: the guard reproduces 12 findings for the add (5 request, 7
     response), so this changes the guard and its policy, not only the spec.
   - It breaks the rule that an old client works against a new server, narrowly. The
     generated Go type `PkiPolicyKeyAlgorithms` is a plain string and `.Valid()` is not
     called in the CLI or server PKI code, so Go decoding accepts `mldsa65`; the
     `v1.0.0` TypeScript SDK has no response validators. But `zPkiPolicy` is a closed
     `z.enum`, the SPA imports it (`web/src/api/pki.ts:167`, `usePkiProfiles`) and parses
     responses with it (`operation.response.parse`, `web/src/api/client.ts:254`). The SPA
     ships in the same artifact as the server (`internal/webui` embeds `dist`), and this
     option regenerates the clients, so a same-artifact SPA would accept the value.
     Profile reads would still fail in a browser tab loaded before the upgrade (it
     holds the old schema), and in today's SPA against ML-DSA profile rows that already
     exist, for example after a restore (see G2). Issuer and certificate responses are
     not affected (`key_algorithm` is `z.string()` there).
   - No client-skew coverage exists for PKI in the frozen-client lane, so that lane
     stays green and gives no evidence either way.
2. **Expose ML-DSA through a new endpoint family** (`endpoint-added` is permitted)
   with its own schemas. The existing profile list and show routes must then not
   return ML-DSA profiles, so they become incomplete views; the CLI and SPA read both
   families; a new surface to freeze.
3. **Wait for `/api/v2`.** No ML-DSA PKI until then. `api-cli-surface.md` (line 146) says v1 explicitly plans none.

(Same shape as the corrected wording in
[1129-transit-mldsa.md](https://github.com/Hikyo-Org/Hikyo/blob/dunky13/pqc-1129-transit-mldsa/docs/handoff/1129-transit-mldsa.md).)

Until then the mechanism is complete below the API and the real server refuses
it. **One hole the enum does not close, found here and closed here:** the
import path (`pki issuer import`, rotate of an imported issuer) takes its
algorithm from the key, not from an enum, so an ML-DSA CA key would have been
accepted through the frozen API. `service.PKI.MLDSAEnabled` (zero value false,
never set by production wiring) makes the service refuse `mldsa65` for issuer
generation and import, profile create and update, generated-key issuance and
leaf reservation (so also renewal). `TestPKICLISQLite` and `TestPKICLIPostgres`
drive the real `app.Boot` server and the real CLI: a classical import succeeds
as a positive control, and `create-root`, `rotate`, `import` of a real ML-DSA key,
`profile create` and `cert issue --generate-key` with `mldsa65` are each refused
with no row created.

## What was built

| Area | Change |
| --- | --- |
| `internal/pki/keys.go` | `MLDSA65` in `LeafKeyAlgorithms` and `CAKeyAlgorithms`; `GenerateKey`; `AlgorithmOf` accepts only `*mldsa.PublicKey` with ML-DSA-65 parameters (44 and 87 refused); `IsPQ` |
| `internal/pki/sign.go` | `CheckPath` and `CheckPQPath`; `VerifyCA` runs `CheckPath` over the CA certificate and its chain, so install and import refuse a mixed path, and for an ML-DSA path `CheckPQPath` also requires the chain to end at a self-issued certificate whose self-signature verifies (review finding P1: a list stopping below the real classical root, and a root with a damaged self-signature, used to install; both are now tests that fail on the old code). `CheckPQPath` has one production caller, `VerifyCA`; the TLS relying-party walk is test code; `SignIntermediate` refuses a parent and child on different sides of the line. Leaf, CRL, CSR, PKCS#8 and `applicableKeyUsage` needed no change: `crypto/x509` and `crypto/mldsa` do the work and ML-DSA keys already get `digitalSignature` only |
| `internal/service` | `MLDSAEnabled` gate; `checkRotationAlgorithm` (rotation never crosses classical and post-quantum, before the CPU-heavy generation and again for an imported key) |
| `internal/audit/registry.go` | `pki.certificate_key_disclosed.key_algorithm` enum gains `mldsa65`; without it a generated ML-DSA leaf would fail at its disclosure audit |
| Schema and queries | **No migration.** `key_algorithm` is `TEXT NOT NULL` with no CHECK in both engines (00064 lines 34 and 103), all generated queries carry the raw string, and the certificate, CSR, chain and CRL columns have no length constraint. So no migration number was taken (PR #1172 holds 00083), `development.json` is unchanged, and `reviewed_pins.json` needs no change from this branch's SQL |
| Docs | Banner on `docs/adr/pki.md`; the proposal block in `post-quantum-migration.md` is a pointer; `docs/operations/pki-ml-dsa.md`; this file; `docs/adr/pki-ml-dsa.md` deleted |

## Choices wider than the proposal, awaiting owner confirmation

These are **not** locked text. The ADR carries the proposal word for word in the
locked-amendment banner (checked by script against the proposal at `e5539c280`) and
everything below in a second banner marked "awaiting owner confirmation". Five
choices need the owner: the path and anchor rule, rotation, the `sha256:`
fingerprints, unconditional ML-DSA leaves, and mixed key and issuer combinations.

- **Mixed paths are refused, not labelled.** The proposal says a classical root
  over an ML-DSA intermediate "is not a PQ-authenticated chain". Hikyo cannot
  attach a label to a chain it stores, so it refuses to create, install or import
  one, in both directions (classical under ML-DSA too), and requires the ML-DSA
  path to end at a self-signed anchor. Existing classical chains are unaffected,
  including a root outside Hikyo's key set. The check is on the path as supplied; it
  cannot know the supplied root is the one relying parties trust.
- **"Needs an offline PQ root" is policy, not enforced.** An online ML-DSA
  evaluation root passes every check (the evaluation root is a documented
  classical-root precedent, PKI ADR D3).
- **Rotation never crosses classical and post-quantum.** The draft amendment says
  changing issuer signature policy must not bypass narrowing through the profile's
  issuer-name reference; refusing the crossing is the smallest rule that holds.
- **Fingerprints keep the `sha256:` spelling over the SPKI. This is a departure
  from DEC-07 (a)** (new algorithm-bearing namespaces and labels), not compliance
  with it. The SPKI carries the algorithm OID, so the digest is already
  algorithm-bound and no existing identity is reinterpreted, but the register chose
  (a); the owner confirms the departure or asks for a new namespace.
- **ML-DSA leaf keys are allowed unconditionally. This departs from the locked
  condition** that the key set gains `mldsa65` for leaves "only if the #1133
  relying-party matrix passes". The matrix covers Go and OpenSSL only and criterion 9
  is PARTIAL, so the condition is not met; the code does not enforce it. The owner
  confirms the departure or asks for a gate.
- **Mixed key and issuer combinations are allowed** (a fifth choice): a classical
  leaf key under an ML-DSA issuer and an ML-DSA leaf under a classical issuer; neither
  is a PQ-authenticated chain. They are separate dimensions and the register did not
  ask to couple them. This is the basis of the first gate-off limit in G2.
- **Empty ML-DSA context** for certificates and CRLs, as RFC 9881 and `crypto/tls`
  require. The `hikyo/<surface>/<purpose>/v<N>` label rule applies to Hikyo-defined
  signature inputs (Transit), not to X.509.
- **OpenSSL's default ML-DSA key file is not importable.** It writes seed plus
  expanded PKCS#8; D2 is seed-only. The guide gives the generate and convert forms
  (both run).

## Acceptance

Legend: DONE, PARTIAL, HOLD (blocked by a decision outside the register), NOTRUN.

| Criterion | Status | Notes |
| --- | --- | --- |
| 1. Land the scoped PKI amendment | PARTIAL, pending owner confirmation | The locked-amendment banner on `pki.md` is the proposal word for word (script-checked) with the required sentence; a second banner holds the implementation's additions and five choices the owner has not confirmed (path and anchor rule, rotation rule, `sha256:` fingerprints as a DEC-07 (a) departure, unconditional ML-DSA leaves, mixed key and issuer combinations). The pointer in the migration ADR says the text is carried there and operative on merge. The proposal file is deleted |
| 2. Issuer generation and import, protected PKCS#8 custody, evaluation root, intermediate CSR and install, activation, leaf issuance and renewal | DONE at service level on both engines; **HOLD through the API, CLI and WebUI** | `TestPKIMLDSALifecycle`: ML-DSA evaluation root, Hikyo-signed intermediate, offline-signed intermediate through CSR and `InstallIssuerCertificate`, import, CSR and generated-key leaves, renewal. Refusals through the service on both engines: wrong key, classical root over the PQ CSR, truncated chain, damaged anchor (with an intact-root control), ML-DSA-44 and -87, and a seed-plus-expanded PKCS#8 built from the seed of a key that matches the certificate (refused as an unparseable private key; fails if the seed-only rule is lost, shown by a local mutation). The seed-only rule is `crypto/x509`'s PKCS#8 parser, which Hikyo calls; the expanded-only and short-seed forms are covered at parser level by `internal/pki` (`mldsa_test.go`), not through the service |
| 3. Closed enums, constraints, generated types, clients, fingerprints, size limits | PARTIAL | DONE: service vocabulary, audit enum, schemas (no constraint exists), queries (raw strings), fingerprints (spelling unchanged, see DEC-07 above), **request** caps measured (12% and 9% of the cap). **Not covered: response and list sizes** (registered gap G1). **HOLD: OpenAPI enums, generated Go and TypeScript clients, CLI help, WebUI lists** (freeze) |
| 4. Preserve managed policy, environment authorization, SAN/EKU/TTL/name rules, no export, display-once | DONE (service level) | Lifecycle test: reader refused, name outside profile refused, algorithm mismatch refused both ways, adding `mldsa65` to a classical profile is `domain.ErrConflict` (widening), generated key not in any row or audit payload, a later read returns no key. Existing classical suites unchanged. The gate-off limits G2 are the exception |
| 5. Certificate and CRL validation, issuance, renewal, revocation, overlap and retirement, unsupported relying-party refusal; reject unknown or mismatched algorithms, invalid chains and CSRs, wrong keys, tampered signatures | DONE after the review repairs (P1, S2, S6) | Independent validation by `crypto/x509`, `crypto/mldsa` directly and OpenSSL 4.0.3. Tampered leaf, CRL, CSR and root self-signature, swapped CSR key, unrelated root, truncated chains, mixed chains all refused. Overlap: version 2 issues, version 1 keeps signing its CRL; retirement now runs: leaves expire, the old version retires, its key is destroyed and its last CRL stays readable. A real classical anchor is accepted by `crypto/x509` and refused only by a `VerifyConnection` walk (test harness) |
| 6. Actual TLS with a Go client and server, chain and key exchange proved separately | PARTIAL: the Go client and server evidence is DONE for Go 1.27.3 only; the product `VerifyConnection` walk is not built (G3) | Mutual TLS on loopback with service-issued chains and `tlspolicy` strict. Group: `CurveID == X25519MLKEM768` on both ends. Authentication: client offered `MLDSA65`, ML-DSA-65 leaf keys, every certificate of both verified chains signed ML-DSA-65. The same chain over `X25519` works, a classical-group client is refused by a strict server. Refusals: a root that did not sign the path (UnknownAuthority only), a `VerifyConnection` walk against a classical chain and against a real classical anchor (test harness; no product hop performs it), and a client built with `GOFIPS140=v1.0.0`. Browsers, OpenSSH and other stacks: **not inferred, unqualified** |
| 7. DEK rotation and re-encryption, concurrent issue/rotate/revoke, multi-node and restart, restore hold, reconciliation, prior-issuer CRL on both engines | PARTIAL | DONE on both engines: `rotate-dek --instance` plus `ReencryptInstance` re-seals the ML-DSA key (ciphertext changes, it still signs under the same identity), manual restore hold, release and CRL publication, retiring-issuer CRL. **Concurrency, weaker claim:** a concurrent issue, rotate and revoke block runs and its invariants hold (at least one issuance succeeds, exactly one active version, every issued leaf verifies; PostgreSQL may exhaust serializable retries, accepted as a clean conflict), but it does **not** prove a race: rotate and revoke start only after the first issuance succeeded, conflict errors are tolerated, and no overlap is asserted. Overlap was seen in instrumented runs only. **NOTRUN, registered as G6 and G7:** a real process restart with ML-DSA state, `restore reconcile`, a real archive restore with an ML-DSA issuer, true multi-node takeover (single-process harness only) and instance-wide corruption refusal beyond the existing classical test |
| 8. Reader-first deployment, CA trust distribution, classical compatibility, rollback, cross-signing only if approved | PARTIAL | Documented in the operations guide; classical compatibility tested (existing suites plus the classical path in the TLS test, a P-521 root chain). Cross-signing and hybrid chains are not offered (not approved). NOTRUN, registered as G7: an older Hikyo binary against a database holding an ML-DSA issuer; interrupted-activation rehearsal beyond "a refused install leaves the version pending" (tested) |
| 9. Benchmarks, offline-root procedure, compatibility matrix with exact versions | PARTIAL | Benchmarks and the procedure are done (guide validated against the code and OpenSSL; amd64 not measured; thresholds are the owner's). The compatibility matrix covers Go and OpenSSL only; **platform verifiers and browsers are not qualified**; the owner is #1133 per `qualification.md` line 57 unless moved (G4) |
| 10. Retain scope | DONE | No public CA, ACME, OCSP, HSM or KMIP, no production root activation, no SSH signature migration |

## Registered gaps

Each gap has a limited claim, a residual risk and an owning ticket among the existing
tickets. #1133 stays open, so its acceptance still depends on these; #1135 verifies
and aggregates. The owner may move any of them.

- **G1, response and list size.** *Limited claim:* the D9 "caps suffice" statement
  covers the **request** caps only. *Facts:* `cert list` returns up to 500 rows and each
  carries its PEMs: 22,665 PEM bytes for an ML-DSA leaf under an ML-DSA issuer
  (measured; about 23.6 KB as JSON by the reviewer's count) and 20,094 for an ECDSA leaf
  under an ML-DSA issuer. The CLI reads at most 8 MiB (`internal/cli/client.go:308`), so
  past about 355 ML-DSA rows (about 398 classical leaves under an ML-DSA issuer)
  `hikyo cert list` fails with "did not match the contract". *Residual risk:* a busy
  environment cannot list its certificates by CLI. *Owner:* #1133 (its acceptance asks
  that expansion be bounded); #1135 verifies. Not fixed: a cap or paging change is a
  contract and bound decision.
- **G2, gate off over existing rows.** *Limited claim:* the gate refuses new ML-DSA
  keys; it is not a rollback. With `MLDSAEnabled` false and ML-DSA rows present (for
  example from a restore), it does not stop: `install` activating a pending ML-DSA
  version; classical leaves being newly issued and renewed with ML-DSA signatures by an
  existing ML-DSA issuer a classical profile names (the mixed combination of choice 5);
  a classical profile naming an ML-DSA issuer being created; a stored `mldsa65` profile
  being returned, a value outside the frozen response enum. All five are asserted by
  `TestPKIMLDSALifecycle` as current behavior. *Residual risk:* an ML-DSA row can
  surface through the frozen API after a restore, and "switch it off" does not undo
  anything. *Owner:* #1133. The owner chooses between extending the gate to these paths
  and accepting them; no new mechanism was added here.
- **G3, the product relying-party walk is not built.** *Limited claim:* `pki.CheckPQPath`
  is used by `VerifyCA` for installs; the `VerifyConnection` walk that refuses a real
  classical anchor exists only in tests, and no Hikyo hop enforces PQ authentication
  (pin-based clients are E-20). *Residual risk:* a strict hop negotiates the hybrid group
  but accepts a classical or mixed chain. *Owner:* #1133, unless the owner moves it:
  `docs/handoff/1127-transport-policy.md` (line 184, "`VerifyConnection` ML-DSA
  authentication on strict-marked clients: #1133") and `profiles.md` line 160
  ("#1127, #1133") place it there, and #1127's profile is group-only. Criterion 6 is
  PARTIAL because of it.
- **G4, platform-verifier and browser qualification.** *Limited claim:* the matrix covers
  Go 1.27.3 and OpenSSL 4.0.3 only; nothing is claimed for browsers, OpenSSH, Java, NSS,
  BoringSSL or the Windows and macOS platform verifiers. *Residual risk:* an operator
  could assume support that was never tested (E-18 for the single listener pair).
  *Owner:* #1133. `qualification.md` line 57 assigns the PKI relying-party matrix "incl.
  platform verifiers" to #1133, which stays open; #1134's text mentions neither PKI,
  browsers nor platform verifiers. Proposed owner change, for the owner: either add them
  to #1134's scope, or record in `qualification.md` that #1133 qualifies Go and OpenSSL
  only.
- **G5, zeroization.** *Limited claim:* PKCS#8 buffers are zeroed after use
  (`wencrypto.Zero`); parsed `*mldsa.PrivateKey` and other key objects are not, as for
  classical keys, so PKI ADR D2's "zeroed after" holds for the buffers only and the
  encryption model disclaims memory secrecy. *Residual risk:* key objects may persist in
  process memory until collected. *Owner:* #1133 (D2 wording), no change planned.
- **G6, restart and restore with ML-DSA state: NOTRUN.** *Limited claim:* what was
  exercised is a fresh service struct over the same `*store.DB` in one process (not a
  process restart), and a manual `UPDATE pki_issuers SET restore_hold=1` followed by the
  network `release-hold`. NOTRUN: (a) a real process restart with an ML-DSA issuer (the
  CLI test's real restart is classical only), (b) `restore reconcile`, the local-host
  reconciliation surface, (c) a real archive restore with an ML-DSA issuer on both
  engines (`HoldRestoredPKIIssuers` sets the flag on every row regardless of algorithm,
  which is inference from the code, not an observation). *Residual risk:* a restore of
  an ML-DSA issuer could surface behavior (G2, a stale CRL, a held issuer) nobody has
  seen. *Owner:* #1133: `qualification.md` line 57 requires both-engine restore there;
  #1135 verifies. It is blocked on the freeze for (a) and (c) through the public path.
- **G7, multi-node, corruption and downgrade rehearsals: NOTRUN.** *Limited claim:*
  the evidence is a single-process harness over one database. Not exercised: (a) true
  multi-node takeover with ML-DSA issuer state (criterion 7), (b) instance-wide
  corruption refusal with an ML-DSA row, beyond the existing classical test
  (criterion 7), (c) an older Hikyo binary reading a database that holds an ML-DSA
  issuer (criterion 8), and (d) an interrupted activation beyond "a refused install
  leaves the version pending" (criterion 8). *Residual risk:* an older binary or a
  second node could mishandle an ML-DSA issuer row in a way nobody has seen; the guide
  says an older build is unsupported once an ML-DSA issuer exists, which is a
  statement, not a test. Production creates no such row while `MLDSAEnabled` is false.
  *Owner:* #1133 (`qualification.md` line 57 requires multi-node and both-engine
  evidence there); #1135 verifies; the owner may move it.

## Measured baseline (DEC-17)

Local and noisy: Apple M2 Pro (arm64, 12 cores), Go 1.27.3, load average about
14 from other jobs, `go test -bench ... -benchtime 200x -count 3`, medians of
three (`internal/pki/mldsa_bench_test.go`). No threshold is proposed: the owner
sets them from numbers on the target machines.

| Operation | ecdsa-p256 | mldsa65 |
| --- | --- | --- |
| Sign a leaf (serial, window, certificate) | 139 us | 903 us |
| Verify a three-certificate chain | 156 us | 342 us |
| Parse a CSR and check its signature | 95 us | 179 us |
| Create a CRL, 0 entries | 158 us | 760 us |
| Create a CRL, 1000 entries | 2.76 ms | 3.25 ms |
| Parse and verify a CRL, 1000 entries | 744 us | 983 us |
| Full TLS 1.3 handshake, hybrid group, in memory | 0.81 ms | 1.51 ms |

Sizes are in the operations guide. Two facts the thresholds will need: a handshake
carries 15.8 KB from the server instead of 2.3 KB for a two-certificate chain, and
a CRL grows by a constant 3.2 KB with no per-entry cost.

## Tests and results

| Command | Result |
| --- | --- |
| `go test -count=1 ./internal/pki/ ./internal/audit/` | PASS |
| `go test -count=1 -run TestMLDSAOpenSSLInterop ./internal/pki/` (OpenSSL 4.0.3) | PASS; skips when no OpenSSL with ML-DSA-65 is on PATH, so hosted CI without one does not run it |
| `go test -count=1 -run TestMLDSARelyingPartyWithoutMLDSARefuses ./internal/pki/` | PASS (builds a client with `GOFIPS140=v1.0.0`; skips when the toolchain has no FIPS snapshot) |
| `go test -p 2 -count=1 -run TestPKIMLDSALifecycle ./internal/isolation/` with a throwaway PostgreSQL 18 (CI-pinned digest) | PASS on SQLite and PostgreSQL |
| `go test -p 2 -count=1 -run 'TestPKICLISQLite\|TestPKICLIPostgres' ./internal/isolation/` | PASS: real boot and CLI, the HOLD and a positive control |
| Mutations: disabling the `VerifyCA` path check; disabling the ML-DSA anchor check | `TestClassicalRootOverMLDSAIntermediateIsNotPQ` fails; `TestMLDSAAnchorMustBeARealSelfSignedRoot` fails on both attacks ("a truncated ML-DSA chain was installed", "installed under a root with a damaged self-signature") |
| Freeze guard, add and open-enum variants | both FAIL as shown above |
| `go test -p 2 -timeout 40m ./internal/lint/...` | see "Final checks" |
| `scripts/ci/fast-feedback.sh`, signature and DCO scripts, doc status | see "Final checks" |

## Consumers checked

`internal/pki` (keys, sign, policy, pem); `internal/service/pki*.go`;
`internal/audit/registry.go` (the only audit field naming an algorithm);
`internal/store` row adapters and generated queries (raw strings); both 00064
schemas; `internal/service/reencrypt.go` (`pki_issuers` is registered with the
versioned re-sealer and is algorithm-blind); `HoldRestoredPKIIssuers`;
`internal/app/generation.go` (the one construction of `service.PKI`, leaves
`MLDSAEnabled` false); `internal/server/pki.go` (maps strings); `api/openapi.yaml`
and the generated Go and TypeScript clients (unchanged, HOLD); `internal/cli/pki.go`
help at line 175 and the `--key-algorithm` flag for `cert issue` (unchanged);
`web/src/api/pki.ts`, `PkiIssuersPanel.tsx`, `CertificatesTab.tsx` algorithm lists
(unchanged, mirror OpenAPI); the published `private-pki.mdx` (unchanged, no PQ
claim).

## Key retention and rollback

Never delete an ML-DSA issuer version, its CRL or its sealed key to make a
rollback work. Switching the algorithm gate off is not a rollback (G2): relying parties still need the CRL until the last leaf expires, and
retire destroys only an eligible key. Once an ML-DSA issuer exists, an older Hikyo
build is not supported (NOTRUN, see criterion 8). The offline root key and the
trust-install receipts stay with the operator. Production use needs a named
environment, custody and a separate authorization; this ticket authorizes none.

## Next authorized step

1. The owner decides the freeze question above. Then a follow-up lands the
   contract change, regenerates the clients, extends the CLI and WebUI lists and
   flips `MLDSAEnabled` in `internal/app/generation.go` (the one wiring point).
2. Merge to `dev` is authorized (2026-11-04) once required CI and the independent
   reviews are green; merge order is bottom-up in the stack. Merging does not
   authorize release or deployment and does not close #1133.
3. The owner confirms or moves the owners named in G1 to G7, in particular who
   qualifies platform verifiers and browsers for the PKI (G4); #1135 aggregates qualification and needs the thresholds from the owner's
   reading of the baseline. The owner also confirms or changes the five
   implementation choices and decides G1 to G3, G6 and G7.

## Final checks

Last review round, run on the candidate before the final handoff-only commit (the
head moves by that commit only; the PR body names the pushed head and the hosted
results read for it).

| Check | Result |
| --- | --- |
| `TestPKIMLDSALifecycle` (also truncated chain, damaged anchor with an intact-root control, seed-plus-expanded PKCS#8 through the service) and `TestPKICLISQLite`, `TestPKICLIPostgres` (import control with key, certificate and chain files), throwaway PostgreSQL 18 at the CI-pinned digest | PASS on SQLite and PostgreSQL in the previous review round. After the last repair (the seed-plus-expanded test): PASS on SQLite only, PostgreSQL not run (no Docker on the repair host, `HIKYO_TEST_POSTGRES_DSN` unset); the change is test-only and engine-independent |
| `go test -count=1 ./internal/pki/` | PASS |
| Earlier this round's consumer sweep (isolation, service, server, cli, store, audit, api), queued, both engines | PASS at `de7d8313f`; the changes since are tests and docs in `internal/isolation` and Markdown |
| `internal/lint` | **Not run locally, by instruction.** The reviewer reports hosted `repository invariants` (including `internal/lint`) passed at `de7d8313f` (112.7 s) and at `53327d51e`. Hosted result for this head: in the PR body |
| `scripts/ci/fast-feedback.sh`, base the updated transport-policy tip, Node 26.12.1, queued | PASS (55 s) |
| `check-commit-signatures.sh`, `check-dco.sh` against `origin/dunky13/pqc-1127-transport-policy` | PASS (13 signed, 10 DCO) |
| `check-doc-status.mjs --check` (dependencies borrowed from another checkout) | PASS |
| Locked banner vs the proposal at `e5539c280` | Script: the banner body equals the proposal's body word for word (91 words after the header); every added clause is in the second banner |
| Hosted CI | At earlier heads only the Go lane's `go test -c` ran (no PKI or isolation test ran hosted). No workflow installs an OpenSSL with ML-DSA, so `TestMLDSAOpenSSLInterop` should skip hosted. Real-provider, runtime, deployment: NOTRUN |
