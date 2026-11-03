# #1133: ML-DSA-65 private certificate lifecycle, handoff

Status: **draft implementation of the mechanism. The public API, CLI and WebUI
surface is HOLD on the owner's API-freeze decision. Not merge-ready, not
operative.** It needs maintainer security approval before merge (**security
approval: not given**). Owner and maintainer are one person, so independent
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
     `z.enum`, the SPA imports it (`web/src/api/pki.ts`) and parses responses with it
     (`operation.response.parse` in `web/src/api/client.ts`), and the SPA ships in the
     same artifact as the server (`internal/webui` embeds `dist`). So one profile
     listing `mldsa65` would make profile reads fail in that SPA. Issuer and
     certificate responses are not affected (`key_algorithm` is `z.string()` there).
   - No client-skew coverage exists for PKI in the frozen-client lane, so that lane
     stays green and gives no evidence either way.
2. **Expose ML-DSA through a new endpoint family** (`endpoint-added` is permitted)
   with its own schemas. The existing profile list and show routes must then not
   return ML-DSA profiles, so they become incomplete views; the CLI and SPA read both
   families; a new surface to freeze.
3. **Wait for `/api/v2`.** No ML-DSA PKI until then. The register says v1 plans none.

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

These are **not** locked text: the ADR carries them in a separate banner marked
"awaiting owner confirmation".

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
- **ML-DSA leaf keys are allowed**, not only issuers. The proposal's "leaves only
  if the relying-party matrix passes" is read as satisfied for the stacks in the
  matrix; every other stack is unqualified.
- **A classical leaf key under an ML-DSA issuer and an ML-DSA leaf under a
  classical issuer are both allowed** and neither is a PQ path. They are separate
  dimensions; the register did not ask to couple them.
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
| 1. Land the scoped PKI amendment | PARTIAL, pending owner confirmation | Locked-text banner on `pki.md` with the required sentence, and a separate banner for four implementation choices (mixed-path and anchor rule, rotation rule, `sha256:` fingerprints as a DEC-07 (a) departure, ML-DSA leaves) that the owner has not confirmed. The pointer in the migration ADR says the text is carried there and operative on merge. The proposal file is deleted |
| 2. Issuer generation and import, protected PKCS#8 custody, evaluation root, intermediate CSR and install, activation, leaf issuance and renewal | DONE at service level on both engines; **HOLD through the API, CLI and WebUI** | `TestPKIMLDSALifecycle`: ML-DSA evaluation root, Hikyo-signed intermediate, offline-signed intermediate through CSR and `InstallIssuerCertificate`, import, CSR and generated-key leaves, renewal. Refusals: wrong key, classical root over the PQ CSR, truncated chain, damaged anchor, ML-DSA-44 and -87, expanded PKCS#8 |
| 3. Closed enums, constraints, generated types, clients, fingerprints, size limits | PARTIAL | DONE: service vocabulary, audit enum, schemas (no constraint exists), queries (raw strings), fingerprints (spelling unchanged, see DEC-07 above), **request** caps measured (12% and 9% of the cap). **Not covered: response and list sizes** (registered gap G1). **HOLD: OpenAPI enums, generated Go and TypeScript clients, CLI help, WebUI lists** (freeze) |
| 4. Preserve managed policy, environment authorization, SAN/EKU/TTL/name rules, no export, display-once | DONE (service level) | Lifecycle test: reader refused, name outside profile refused, algorithm mismatch refused both ways, adding `mldsa65` to a classical profile is `domain.ErrConflict` (widening), generated key not in any row or audit payload, a later read returns no key. Existing classical suites unchanged. The gate-off limits G2 are the exception |
| 5. Certificate and CRL validation, issuance, renewal, revocation, overlap and retirement, unsupported relying-party refusal; reject unknown or mismatched algorithms, invalid chains and CSRs, wrong keys, tampered signatures | DONE after the review repairs (P1, S2, S6) | Independent validation by `crypto/x509`, `crypto/mldsa` directly and OpenSSL 4.0.3. Tampered leaf, CRL, CSR and root self-signature, swapped CSR key, unrelated root, truncated chains, mixed chains all refused. Overlap: version 2 issues, version 1 keeps signing its CRL; retirement now runs: leaves expire, the old version retires, its key is destroyed and its last CRL stays readable. A real classical anchor is accepted by `crypto/x509` and refused only by a `VerifyConnection` walk (test harness) |
| 6. Actual TLS with a Go client and server, chain and key exchange proved separately | DONE for Go 1.27.3 only | Mutual TLS on loopback with service-issued chains and `tlspolicy` strict. Group: `CurveID == X25519MLKEM768` on both ends. Authentication: client offered `MLDSA65`, ML-DSA-65 leaf keys, every certificate of both verified chains signed ML-DSA-65. The same chain over `X25519` works, a classical-group client is refused by a strict server. Refusals: a root that did not sign the path (UnknownAuthority only), a `VerifyConnection` walk against a classical chain and against a real classical anchor (test harness; no product hop performs it), and a client built with `GOFIPS140=v1.0.0`. Browsers, OpenSSH and other stacks: **not inferred, unqualified** |
| 7. DEK rotation and re-encryption, concurrent issue/rotate/revoke, multi-node and restart, restore hold, reconciliation, prior-issuer CRL on both engines | PARTIAL | DONE on both engines: `rotate-dek --instance` plus `ReencryptInstance` re-seals the ML-DSA key (ciphertext changes, it still signs under the same identity), concurrent issue/rotate/revoke (at least one issuance must succeed; exactly one active version; every issued leaf verifies; PostgreSQL may exhaust serializable retries, accepted as a clean conflict), manual restore hold, release and CRL publication, retiring-issuer CRL. **NOTRUN:** a real process restart with ML-DSA state (the test uses a fresh service struct over the same `*store.DB` in one process; the CLI test's real restart is classical only); `restore reconcile` (the local-host reconciliation surface; only the network `release-hold` was exercised); a real archive restore with an ML-DSA issuer (the hold sets the flag on every row regardless of algorithm, `HoldRestoredPKIIssuers`); true multi-node takeover (single-process harness only); instance-wide corruption refusal beyond the existing classical test |
| 8. Reader-first deployment, CA trust distribution, classical compatibility, rollback, cross-signing only if approved | PARTIAL | Documented in the operations guide; classical compatibility tested (existing suites plus the classical path in the TLS test, a P-521 root chain). Cross-signing and hybrid chains are not offered (not approved). NOTRUN: an older Hikyo binary against a database holding an ML-DSA issuer; interrupted-activation rehearsal beyond "a refused install leaves the version pending" (tested) |
| 9. Benchmarks, offline-root procedure, compatibility matrix with exact versions | PARTIAL | Benchmarks and the procedure are done (guide validated against the code and OpenSSL; amd64 not measured; thresholds are the owner's). The compatibility matrix covers Go and OpenSSL only; **platform verifiers and browsers are not qualified and have no owner** (G4) |
| 10. Retain scope | DONE | No public CA, ACME, OCSP, HSM or KMIP, no production root activation, no SSH signature migration |

## Registered gaps

Each is a claim limited to what was measured, with the residual risk and an owner to
be named (this task cannot create or edit issues).

- **G1, response and list size (review P3).** The "caps suffice" claim (D9) is limited
  to the **request** caps. `cert list` returns up to 500 rows and each carries its PEMs.
  Measured PEM bytes per row: 22,665 for an ML-DSA leaf under an ML-DSA issuer (about
  23.6 KB as JSON by the reviewer's count), 20,094 for an ECDSA leaf under an ML-DSA
  issuer. The CLI reads at most 8 MiB (`internal/cli/client.go:308`), so past about 355
  ML-DSA rows (about 398 classical leaves under an ML-DSA issuer) `hikyo cert list`
  fails with "did not match the contract". Residual risk: a busy environment cannot list
  its certificates by CLI. Not fixed: raising the cap or paging is a contract and bound
  decision. Owning ticket: none yet; proposed a #1133 follow-up or a #1135 bound item.
- **G2, gate off over existing rows (review P6).** With `MLDSAEnabled` false and ML-DSA
  rows already present (for example from a restore), the gate refuses new ML-DSA keys
  but does not stop: `install` activating a pending ML-DSA version; classical leaves
  being newly issued and renewed with ML-DSA signatures by an existing ML-DSA issuer a
  classical profile names; a classical profile naming an ML-DSA issuer being created;
  a stored `mldsa65` profile being returned (a value outside the frozen response enum).
  All five are asserted by `TestPKIMLDSALifecycle` as current behavior. Residual risk:
  "switch it off" is not a rollback and an ML-DSA row can surface through the frozen
  API after a restore. No new mechanism was added. Owning ticket: none yet; the owner
  chooses between extending the gate to these paths and accepting them.
- **G3, the relying-party walk is not a product feature.** `pki.CheckPQPath` runs in
  `VerifyCA`; the `VerifyConnection` walk that would refuse a real classical anchor
  exists only in tests. No Hikyo hop enforces PQ authentication (E-20 covers pin-based
  clients). #1127's profile is group-only.
- **G4, qualification ownership (review P4).** `qualification.md` line 57 assigns the
  PKI relying-party matrix "incl. platform verifiers" to #1133; #1134's text mentions
  neither PKI, browsers nor platform verifiers; the earlier draft assigned them to
  #1134 without a basis. Proposed owner change, for the owner to decide: either add
  PKI chains, browsers and the platform verifiers to #1134's scope, or record in
  `qualification.md` that #1133 qualifies Go and OpenSSL only and leaves the rest
  unowned until named.
- **G5, zeroization (review P10).** PKCS#8 buffers are zeroed after use (`wencrypto.Zero`);
  parsed `*mldsa.PrivateKey` and other key objects are not, exactly as for classical keys.
  PKI ADR D2's "zeroed after" therefore holds for the buffers only; the encryption model
  disclaims memory secrecy.

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
2. Independent adversarial Standards and Spec review of this candidate, then
   maintainer security approval, then merge only if the owner authorizes it.
3. The owner settles who qualifies platform verifiers and browsers for the PKI
   (G4); #1135 aggregates qualification and needs the thresholds from the owner's
   reading of the baseline. The owner also confirms or changes the four
   implementation choices and decides G1 and G2.

## Final checks

Run on the candidate before the final handoff-only commit (the head moves by that
commit only; the PR body names the pushed head). First round and review-repair round.

| Check | Result |
| --- | --- |
| `go test -p 2 -parallel 2 -run 'PKI\|Pki\|Certificate\|Audit\|MLDSA\|Anchor\|RealClassical\|OpenSSL\|RelyingParty'` over `internal/isolation`, `pki`, `service`, `server`, `cli`, `store/...`, `audit`, `api/...`, queued, throwaway PostgreSQL 18 (CI-pinned digest) | PASS after the repairs (isolation 832 s). The lifecycle test also passed repeatedly (4 runs) after the concurrency gate |
| `internal/lint` | **Not rerun locally in this round, by instruction.** Round 1 locally: the 40 minute limit expired under load; a 100 minute rerun failed only `TestGrantLockCatchesLocklessWriter/ui` (`exec: WaitDelay expired`), which passes alone. The reviewer reports hosted `repository invariants` passed at 241c53d35 including `internal/lint` in 73.9 s. Hosted result for the repaired head: read from `gh pr checks`, see the PR body |
| `scripts/ci/fast-feedback.sh` with base `f5d5491829d6`, Node 26.12.1, queued | PASS (109 s): whitespace, Go formatting, test compile, shellcheck, actionlint, docs content check |
| `check-commit-signatures.sh`, `check-dco.sh` against `origin/dunky13/pqc-1127-transport-policy` | PASS (55 signed, 49 DCO) |
| `node scripts/ci/check-doc-status.mjs --check` (dependencies borrowed from another checkout) | PASS |
| Hosted CI | Not a substitute for the above. At the previous head only the Go lane's `go test -c` ran (no PKI or isolation test ran hosted); `feedback (frontend)` failed on a checkout timeout (infrastructure). No workflow installs an OpenSSL with ML-DSA, so `TestMLDSAOpenSSLInterop` should skip hosted. Real-provider, runtime, deployment: NOTRUN |
