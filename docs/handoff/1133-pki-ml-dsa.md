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

**Decision needed from the owner** (same shape as #1129, recommendation first):

1. Amend [api-cli-surface](../adr/api-cli-surface.md) (#25) so these closed enums
   may grow once for the post-quantum program with a new bound profile, then add
   `mldsa65`, regenerate the Go and TypeScript clients, and extend the CLI help
   and WebUI lists. Smallest contract change. A closed-enum Zod client breaks only
   when a profile listing `mldsa65` exists. Recommended.
2. A new endpoint family with its own schema (`endpoint-added` is permitted).
   Avoids the enum change, forks the PKI API, and list and show would have to hide
   ML-DSA profiles.
3. Wait for `/api/v2`. The register says v1 plans none.

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
| `internal/pki/sign.go` | `CheckPath` and `CheckPQPath`; `VerifyCA` runs `CheckPath` over the CA certificate and its chain, so install and import refuse a mixed path; `SignIntermediate` refuses a parent and child on different sides of the line. Leaf, CRL, CSR, PKCS#8 and `applicableKeyUsage` needed no change: `crypto/x509` and `crypto/mldsa` do the work and ML-DSA keys already get `digitalSignature` only |
| `internal/service` | `MLDSAEnabled` gate; `checkRotationAlgorithm` (rotation never crosses classical and post-quantum, before the CPU-heavy generation and again for an imported key) |
| `internal/audit/registry.go` | `pki.certificate_key_disclosed.key_algorithm` enum gains `mldsa65`; without it a generated ML-DSA leaf would fail at its disclosure audit |
| Schema and queries | **No migration.** `key_algorithm` is `TEXT NOT NULL` with no CHECK in both engines (00064 lines 34 and 103), all generated queries carry the raw string, and the certificate, CSR, chain and CRL columns have no length constraint. So no migration number was taken (PR #1172 holds 00083), `development.json` is unchanged, and `reviewed_pins.json` needs no change from this branch's SQL |
| Docs | Banner on `docs/adr/pki.md`; the proposal block in `post-quantum-migration.md` is a pointer; `docs/operations/pki-ml-dsa.md`; this file; `docs/adr/pki-ml-dsa.md` deleted |

## Choices wider than the proposal, to confirm

- **Mixed paths are refused, not labelled.** The proposal says a classical root
  over an ML-DSA intermediate "is not a PQ-authenticated chain". Hikyo cannot
  attach a label to a chain it stores, so it refuses to create, install or import
  one, in both directions (classical under ML-DSA too). Existing classical chains
  are unaffected, including a root outside Hikyo's key set.
- **Rotation never crosses classical and post-quantum.** The draft amendment says
  changing issuer signature policy must not bypass narrowing through the profile's
  issuer-name reference; refusing the crossing is the smallest rule that holds.
- **Fingerprints are unchanged** (SHA-256 of the SPKI, which carries the OID).
  DEC-07's new namespace is a candidate rule for surfaces that publish a
  fingerprint across algorithms; PKI's one spelling is already algorithm-bound.
- **ML-DSA leaf keys are allowed**, not only issuers. The proposal's "leaves only
  if the relying-party matrix passes" is read as satisfied by the matrix in the
  operations guide for the stacks tested; every other stack is unqualified.
- **A classical leaf key under an ML-DSA issuer and an ML-DSA leaf under a
  classical issuer are both allowed** and neither is a PQ path
  (`TestPKIMLDSALifecycle`, `TestMLDSALeafUnderClassicalIssuerIsAllowedButNotAPQChain`).
  They are separate dimensions; the register did not ask to couple them.
- **Empty ML-DSA context** for certificates and CRLs, as RFC 9881 and `crypto/tls`
  require. The `hikyo/<surface>/<purpose>/v<N>` label rule applies to Hikyo-defined
  signature inputs (Transit), not to X.509.
- **OpenSSL's default ML-DSA key file is not importable.** It writes seed plus
  expanded PKCS#8; D2 is seed-only. The operator guide gives the
  `-provparam ml-dsa.output_formats=seed-only` form (tested).

## Acceptance

Legend: DONE, PARTIAL, HOLD (blocked by a decision outside the register), NOTRUN.

| Criterion | Status | Notes |
| --- | --- | --- |
| 1. Land the scoped PKI amendment | DONE (text); the banner is operative only on merge | Banner on `pki.md` with the required sentence, pointer in the migration ADR, proposal deleted. Wider text flagged above |
| 2. Issuer generation and import, protected PKCS#8 custody, evaluation root, intermediate CSR and install, activation, leaf issuance and renewal | DONE at service level on both engines; **HOLD through the API, CLI and WebUI** | `TestPKIMLDSALifecycle`: ML-DSA evaluation root, Hikyo-signed intermediate, offline-signed intermediate through CSR and `InstallIssuerCertificate`, import, CSR and generated-key leaves, renewal. Refusals: wrong key, classical root over the PQ CSR, ML-DSA-44 and -87, expanded PKCS#8 |
| 3. Closed enums, constraints, generated types, clients, fingerprints, size limits | PARTIAL | DONE: service vocabulary, audit enum, schemas (no constraint exists), queries (raw strings), fingerprints, caps measured (12% and 9% of the cap). **HOLD: OpenAPI enums, generated Go and TypeScript clients, CLI help, WebUI lists** (freeze) |
| 4. Preserve managed policy, environment authorization, SAN/EKU/TTL/name rules, no export, display-once | DONE (service level) | Lifecycle test: reader refused, name outside profile refused, algorithm mismatch refused both ways, adding `mldsa65` to a classical profile is `domain.ErrConflict` (widening), generated key not in any row or audit payload, a later read returns no key. Existing classical suites unchanged |
| 5. Certificate and CRL validation, issuance, renewal, revocation, overlap and retirement, unsupported relying-party refusal; reject unknown or mismatched algorithms, invalid chains and CSRs, wrong keys, tampered signatures | DONE | Independent validation by `crypto/x509`, `crypto/mldsa` directly and OpenSSL 4.0.3. Tampered leaf, CRL, CSR and root self-signature, swapped CSR key, unrelated root, mixed chains all refused. Overlap: version 2 issues, version 1 keeps signing its CRL, retire refuses while leaves are live |
| 6. Actual TLS with a Go client and server, chain and key exchange proved separately | DONE for Go 1.27.3 only | Mutual TLS on loopback with service-issued chains and `tlspolicy` strict. Group: `CurveID == X25519MLKEM768` on both ends. Authentication: client offered `MLDSA65`, ML-DSA-65 leaf keys, every certificate of both verified chains signed ML-DSA-65. The same chain over `X25519` works, a classical-group client is refused by a strict server. Refusals: classical trust anchor, PQ-requiring peer against a classical chain, and a client built with `GOFIPS140=v1.0.0` (no ML-DSA). Browsers, OpenSSH and other stacks: **not inferred, unqualified** |
| 7. DEK rotation and re-encryption, concurrent issue/rotate/revoke, multi-node and restart, restore hold, reconciliation, prior-issuer CRL on both engines | PARTIAL | DONE on both engines: `rotate-dek --instance` plus `ReencryptInstance` re-seals the ML-DSA key (ciphertext changes, it still signs under the same identity), restart (fresh service), concurrent issue/rotate/revoke (exactly one active version, every issued leaf verifies; PostgreSQL may exhaust serializable retries, accepted as a clean conflict), manual restore hold, release and CRL publication, retiring-issuer CRL. NOTRUN: a real archive restore with an ML-DSA issuer (the hold sets the flag on every row regardless of algorithm, `HoldRestoredPKIIssuers`), true multi-node takeover (single-process harness only), instance-wide corruption refusal beyond the existing classical test |
| 8. Reader-first deployment, CA trust distribution, classical compatibility, rollback, cross-signing only if approved | PARTIAL | Documented in the operations guide; classical compatibility tested (existing suites plus the classical path in the TLS test, a P-521 root chain). Cross-signing and hybrid chains are not offered (not approved). NOTRUN: an older Hikyo binary against a database holding an ML-DSA issuer; interrupted-activation rehearsal beyond "a refused install leaves the version pending" (tested) |
| 9. Benchmarks, offline-root procedure, compatibility matrix with exact versions | DONE (baseline); thresholds are the owner's | Tables below; guide validated against the code and OpenSSL. amd64 not measured |
| 10. Retain scope | DONE | No public CA, ACME, OCSP, HSM or KMIP, no production root activation, no SSH signature migration |

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
| Mutation: disabling the `VerifyCA` path check | `TestClassicalRootOverMLDSAIntermediateIsNotPQ` fails, as it should |
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
rollback work: relying parties still need the CRL until the last leaf expires, and
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
3. #1134 owns external-stack dispositions for browsers, OpenSSH and platform
   verifiers; #1135 aggregates qualification and needs the thresholds from the
   owner's reading of the baseline.

## Final checks

(Filled in after the last edit; see the PR body for the candidate SHA.)
