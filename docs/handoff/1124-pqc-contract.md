# #1124: post-quantum contract draft handoff

Written 2026-11-02, revised the same day after review and after the owner's
decision. It records what the draft
contract delivers and what it does not prove, as of that date; it is evidence for
one draft, not a live status board. The source pin is in the
[contract README](../security/pqc/README.md#source-pin).

## Result

[#1124](https://github.com/Hikyo-Org/Hikyo/issues/1124) asks for an
owner-approvable contract so dependent implementers need not invent
cryptographic policy. This draft supplies the documents. On 2026-11-02 the owner
accepted DEC-01 to DEC-20 as recommended, replying "As recommended" to the register
at `383290434d2cf614cd8aa31600c1f79d1e53f4d7` after being told its final repairs were not re-reviewed. **Maintainer
security approval is on HOLD and no ADR amendment has landed**, so nothing here is
operative. The issue may close only after those gates or an explicit scoped owner
disposition.

| File | Content |
| --- | --- |
| [README](../security/pqc/README.md) | Entry point, source pin, claim rules |
| [Inventory](../security/pqc/inventory.md) | 32 boundaries (I01 to I32), census, trace, bounds |
| [Profiles](../security/pqc/profiles.md) | Two claims, strength, parameters, bounds, symmetric floor, profiles, composition |
| [Migration](../security/pqc/migration.md) | Ladder, keys, mixed paths, retirement, floor, recovery, dispositions, exceptions E-01 to E-20 |
| [Qualification](../security/pqc/qualification.md) | Test matrix, scope, estimates, critical path |
| [Proposed ADR](../adr/post-quantum-migration.md) | Register DEC-01 to DEC-20, waiver, governance route, proposed amendments; no locked ADR edited |

Owner decisions of 2026-11-02 and the amendment route are recorded once, in the
[ADR](../adr/post-quantum-migration.md#owner-decisions-already-made-2026-11-02).

## Acceptance criteria map

| # | #1124 criterion | Answered in | Status |
| --- | --- | --- | --- |
| 1 | Inventory fields and listed surfaces | [Inventory](../security/pqc/inventory.md): boundaries table (purpose, parameters, custody, lifetime, horizon, recovery, provenance), trace table (writers, readers, verifiers), bounds, census | Drafted; maintainer review of the data-flow and exception matrix HOLD |
| 2 | Confidentiality and authenticity separate; strength; compliance | [Profiles](../security/pqc/profiles.md#two-claims-defined-separately), [strength](../security/pqc/profiles.md#required-strength-and-compliance-obligations); DEC-01, 02 | Proposal; HOLD |
| 3 | Parameter sets, encodings, fingerprints, domain separation, size maxima, identifiers, vectors, errors, maintenance | Profiles: parameters, encodings, bounds, vectors and errata; DEC-01, 02, 07, 08, 09 | Proposal; HOLD |
| 4 | Symmetric retention and assumptions; PQ versus FIPS | [Profiles: symmetric floor](../security/pqc/profiles.md#symmetric-floor-and-assumptions); DEC-04 | Proposal; usage cap NOTRUN |
| 5 | Compatibility and strict profiles | [Profiles](../security/pqc/profiles.md#compatibility-and-strict-profiles); DEC-05, 06, 10 | Proposal; HOLD |
| 6 | Reader-first rollout, keys, retirement, floor, history, recovery, mixed-path refusal | [Migration](../security/pqc/migration.md); DEC-11, 12 | Proposal; HOLD |
| 7 | Signature composition and stripping | [Profiles](../security/pqc/profiles.md#signature-composition-and-stripping-resistance-dec-03); DEC-03 | Proposal; HOLD |
| 8 | Dispositions: SRP/DPoP, release root, Sigstore, SSH, federation, authenticators, custody | [Migration](../security/pqc/migration.md#dispositions-dec-13-to-dec-16-dec-19-dec-20); DEC-12 to DEC-16, 19, 20 | Proposal; HOLD |
| 9 | ADR amendments, decisions, evidence; #26/#79 recheck | [ADR](../adr/post-quantum-migration.md) | Owner decisions recorded; security approval and amendments HOLD |
| 10 | Test matrix; scope, effort, critical path; unresolved choices | [Qualification](../security/pqc/qualification.md) | Drafted; estimates are estimates |
| 11 | Commit approved inventory, ADRs and handoff with receipts | This handoff and the files above | Not yet: security approval and amendments outstanding |

## State on 2026-11-02

Rechecked with `gh`. PRs
[#1138](https://github.com/Hikyo-Org/Hikyo/pull/1138) and
[#1139](https://github.com/Hikyo-Org/Hikyo/pull/1139) are merged into the base;
[#1140](https://github.com/Hikyo-Org/Hikyo/pull/1140) (release trust sync) is open
and not a draft; [#1137](https://github.com/Hikyo-Org/Hikyo/pull/1137) and
[#1113](https://github.com/Hikyo-Org/Hikyo/pull/1113) are open drafts;
[#1141](https://github.com/Hikyo-Org/Hikyo/pull/1141) (PKI ML-DSA proposal) and
[#1148](https://github.com/Hikyo-Org/Hikyo/pull/1148) are open drafts. Issues
#1123, #1124 and #1126 to #1135 are open and unassigned. None of those PRs is PQ
acceptance and this package amends none. #26 is closed; #79 and #527 are open;
the release-governance recheck is recorded in the ADR.

## Evidence by class (2026-11-02)

| Class | Status |
| --- | --- |
| Source inspection | Performed at the pin: `rg` census (search 1: 179 non-test Go files; search 2 for SDK-owned TLS and search 3 for `chart`, `deploy` and `install`, and searches 5 and 6 for release tooling and source supply chain, added after review; completeness beyond these searches is not claimed), per-row checks of constants and constructors, Go 1.27.3 standard library, age v1.3.2, `filippo.io/hpke` v0.4.0, `x/crypto/ssh` v0.58.0, `go-webauthn` v0.18.2, `go-jose` v4.1.6, sigstore v1.11.1, vendored gitleaks rule |
| Local checks | New Markdown: no em-dash, no machine-local path, no task-list checkbox under `docs/adr`, relative links and anchors resolve. `scripts/ci/check-doc-status.mjs --check` and `check-doc-status_test.sh` pass. `scripts/ci/fast-feedback.sh --plan --working-tree` selects no lanes for these paths. Docs site verify results are in the PR description |
| Independent review | Independent Standards, Spec and adversarial security reviews ran on earlier unpublished candidates. Confirmed findings were repaired by narrowing claims, registering gaps or stating child requirements; the last repairs were not re-reviewed. Final coverage is in the PR description. Human security review: NOTRUN |
| Cross-provider review | SKIPPED by explicit owner instruction (see ADR); not CLEAN |
| Hosted CI | NOTRUN for this revision |
| Tests, handshakes, vectors, fuzz, measurements, both-engine drills | NOTRUN. Sizes are source constants or arithmetic; estimates are labelled |
| Provider, artifact, runtime | NOTRUN. No key was generated and no provider action taken |
| UI | SKIPPED: no rendered UI change |

## Primary documents

Fetched 2026-11-02; the web tool returns a model-made summary of HTML pages, so
"read" means that summary unless noted.

| Document | What was read | Not read |
| --- | --- | --- |
| Go 1.27 release notes | ML-DSA package, x509 and TLS ML-DSA support, `MLKEM1024`, `CurvePreferences` and GODEBUG notes | Full text |
| NIST IR 8547 (final, October 2026) | Landing page and the full PDF body text, extracted locally from the retrieved file: Tables 1 to 8, sec 3.2, sec 4.1 (deprecate after 2030, disallow after 2035; EO 14412 and OMB M-26-15 dates), symmetric statement | Remaining sections only by keyword search |
| NIST FIPS 203, 204, 205 | Landing pages only: dates (2024-08-13), final status, errata notes (FIPS 203: 2025-11-17; FIPS 204: 2026-07-31) | Standards text, parameter tables, errata sheets. Categories come from IR 8547; sizes from Go source |
| NIST PQC FAQ | Entry on doubling AES key length | Rest |
| GitHub secret-encryption docs | Requires libsodium; examples use sealed boxes | Per-endpoint pages |
| OpenSSH PQ page | Key agreement history and default; PQ signatures not yet supported | Release notes |
| age, hpke, Go source | Read from the module cache and GOROOT | n/a |

Not fetched: SP 800-227, RFC 9881 and other IETF documents, ACVP and Wycheproof
vector files, libsodium docs, Sigstore PQ status, SLH-DSA text. No claim relies
on them. Go's `Sign` hedging was read from `crypto/internal/fips140/mldsa`.

## Where source differs from the issue or the earlier draft

| Item | Finding |
| --- | --- |
| #1123 links a "versioned TLS policy API" | The link is a version-pinned docs URL for `tls.Config`; Go 1.27.3 offers `MinVersion`, `MaxVersion` and `CurvePreferences` and no other group or version policy setting was found |
| #1131 says to bootstrap PQ trust "through the existing authenticated authority" | DEC-20 option (a) is that wording and lets a classical forger install its own ML-DSA key; the recommended option (c), operator re-enrollment, departs from it. The departure is an owner decision, HOLD |
| Earlier README: #1138, #1139 open | Both merged; #1140 is open and not a draft |
| Earlier inventory: `charts` | The draft searched the wrong path. The real `chart/hikyo` exists at the pin and is now I31; open PR #1113 is a separate Helm distribution change |
| Earlier census | Search 1 alone missed `internal/storagehealth` (kubelet TLS with a bearer token) and the Kubernetes, OpenBao and default-transport clients; now I30 |
| ML-DSA and FIPS | Certified module alias v1.0.0 has no ML-DSA; v1.26.0 with it is in process |
| Library ML-DSA capability | Present in Go `x509`, `tls`, `x/crypto/ssh` (certificates under unspecified vendor names), `sigstore`, `go-webauthn`; absent in `go-jose`, `go-srp`, `go-oidc`, SAML libraries. Not product readiness |
| Hybrid sizes | `age1pq1` recipient 1959 characters; about 168 hybrid recipients fit the 256 KiB header [est] but only 33 fit one managed value (65536 bytes) |
| Wycheproof | Go's tests fetch the vectors at runtime; they are not in the tree |

## Remaining HOLD and NOTRUN

DEC-01 to DEC-20 are owner decided and not operative; maintainer security approval
and every ADR amendment are HOLD. Largest uncertainties:
pairing feasibility (#1130), release-root custody and field installers (#1132),
external peers (#1134), and every measurement. The 22 to 38 engineer-week total
is a low-confidence estimate above the issue's range; reasons are in
[Qualification](../security/pqc/qualification.md#refined-scope-effort-and-critical-path).
Key retention and rollback requirements are proposals in
[Migration](../security/pqc/migration.md).

## Next steps (state of 2026-11-02)

1. Marc records maintainer security approval or holds it; the
   [register](../adr/post-quantum-migration.md#decision-register) has the owner
   decisions. The final repairs had no independent re-review; cross-provider
   review stays SKIPPED.
2. For each locked ADR follow the governance route in the ADR.
3. Dependent children (#1126 to #1135) stay HOLD until that approval and their
   amendments are recorded; recheck the pin, open PRs and ownership before taking
   one.

No step here authorizes key generation, provider action, release or deployment.
