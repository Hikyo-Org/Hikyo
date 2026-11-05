# Requirement-to-test matrix, scope and critical path, draft v0.7

Status: **owner decided and security approved, not operative**. This tells dependent owners what evidence each
surface needs so no one invents policy. Every row is a plan: execution is
NOTRUN, and dependence on a missing ADR amendment is HOLD. Integrated execution,
measured budgets and the operator runbook belong to #1135. Inventory IDs are in
[inventory](inventory.md); policy in [profiles](profiles.md) and
[migration](migration.md); decisions `DEC-nn` in the
[register](../../adr/post-quantum-migration.md#decision-register). Parent
requirement IDs `PQ-01` to `PQ-15` are defined in the
[program draft](../../design/post-quantum-program.md#parent-requirement-to-evidence-ledger).

## Evidence classes

One row per requirement, surface and class; classes never substitute for each
other.

| Class | Meaning |
| --- | --- |
| source | Inspected code or library capability at a named revision |
| local | Test or fixture run on a named candidate with named tools |
| hosted | Exact-head CI jobs |
| peer | Real consuming stack or terminator, named with version |
| provider | Real external provider (needs resource authorization) |
| artifact | Published or built artifact digests and verification |
| runtime | Observed deployment (needs environment authorization) |

Status: PASS (observed, scoped), FAIL, HOLD (missing decision or prerequisite),
NOTRUN (no execution evidence), SKIPPED (with reason). The program draft's
evidence record template applies.

## Claim statement template

Per profile (`pqc-conf/1` or `pqc-auth/1`), surface, direction and peer version,
only what the rows prove: "Surface S, direction D, candidate C, peer P:
confidentiality: software capability | enforced policy | observed on hop H;
authenticity: classical | dual | PQ-only with anchor T; exceptions: E-nn." Every
authenticity statement names E-11 and E-15 until #1132 closes them. Never
"PQ-ready" for a mix.

## Per-surface requirement-to-test matrix

"Starts from" lists existing test locations at the pin to extend; none covers PQ
today. All status cells are HOLD (amendment) and NOTRUN
(execution).

| Surface (IDs) | Requirement | Starts from | New evidence required | PQ IDs | Owner |
| --- | --- | --- | --- | --- | --- |
| Contract and approval (all IDs) | Inventory reconciled at a pinned revision; owner and maintainer security receipts for the decision register | The [census commands](inventory.md) | Reconciliation rerun at each child's base; recorded decisions | PQ-01, 02 | #1124 |
| Backups (I07, I08) | Hybrid generate, export, receipt, custody drill, restore on both engines; legacy archives recover; classical or mixed header refused; count and byte bounds refuse deterministically before output | `internal/crypto/backup/*_test.go` (incl. fuzz), `internal/backupreceipt/*_test.go`, `internal/upgradecustody/vault_test.go`, `internal/isolation/backup_drill_e2e_test.go` | Hybrid vectors; stanza-allowlist refusal; bound tests at 32 and just over (count versus bytes, including the 65536-byte managed value); frozen legacy archives with digests, restored under compatibility; `operator.age` per the DEC-12 outcome, including what happens to old vault copies; secret-scanner rule for `AGE-SECRET-KEY-PQ-1` (a #1126 requirement; the rule file is not edited here); every export path emits a receipt; signed receipt binding; restore and drill input for receipts and quarantine hold tests; vault rotation and re-seal during root rotation; SQLite and PostgreSQL scratch restore executed, not skipped | PQ-03, 08, 09, 10 | #1126 |
| TLS hops, incl. database and proxy hops (I09-I14, I16, I17, I30), starting profiles per the [hop table](profiles.md#hop-starting-profiles) | Effective policy per hop; actual negotiated group; on strict-marked hops only: classical-only peer refused before data and certificate authentication refused through `VerifyConnection`; on compat hops the group is observed and nothing is refused (certificate authentication reported separately) | `internal/app/tls_test.go`, `internal/tlspolicy`, `internal/isolation/cli_tls_consumer_test.go` | Per-hop table: consumer stack, terminator, `ConnectionState.CurveID`, negative handshake; managed-generation reload; stale-node fencing is not built (E-22), so the test records that a stale node keeps serving rather than a refusal; resumption; platform verifier on each claimed OS; baseline then strict handshake cost on amd64 and arm64; single-certificate listener behavior | PQ-04, 10, 11 | #1127 |
| Sealed webhook (I21) | New version: hybrid encryption plus all-required PQ signatures; stripped signature, wrong key, other-algorithm signature, mixed recipients rejected; receiver-first skew | `internal/crypto/sealedhook/sealedhook_test.go`, `internal/sealedreceiver`, `internal/adapter/sealedwebhook` | Vectors; ack lifecycle; replay and idempotency across restart; bounds at the new field sizes; version-skew matrix | PQ-05, 08, 09 | #1128 |
| Transit (I22) | ML-DSA key identity distinct from Ed25519; seed derived by HKDF from version material; rotation never changes algorithm; verification consumers first | `internal/crypto/transit_test.go`, `internal/transit`, store tests for 00065 | Both-engine migration for closed CHECKs; API, CLI, client and UI enum coverage; restore hold, re-encryption, multi-node rotation race | PQ-05, 08, 10 | #1129 |
| Pairing (I20) | Qualifies at its current classical profile only (SRP-2048, Ed25519 DPoP), with no PQ claim, once the drill runs (NOTRUN); HOLD on PQ pairing; E-01 | `internal/pairing/{srp,dpop}_test.go` | Pairing and revocation drill of the existing classical flow on SQLite and PostgreSQL, reported as HOLD with E-01 in the capability statement. #1130 stays open as the residual gap and no longer blocks #1135 (decided 2026-11-04) | PQ-05, 12 | #1130 |
| Rollout (I23) | PQ command signature verified under the DEC-20 outcome; stale generation and stripped signature refused with zero mutation; journal-reset and key-change behavior against the existing journal stated (E-14) | `internal/configrollout/*_test.go` | Real producer-to-controller path; journal and restore floor; bounds at the 512 KiB record | PQ-05, 08, 09 | #1131 |
| Release, install and update paths, chart (I26, I27, I31) | Every binary-replacement path (stable, nightly, `sudo hikyo upgrade`, unattended image replacement, chart image policy, fresh install) verified or registered; no classical-only bypass on a path claimed PQ | `internal/releasetrust/*_test.go`, `internal/selfupdate`, `install/upgrade-nightly_test.sh`, `scripts/release` fixtures, chart render tests | Parser accepting ML-DSA; sequence-only progression and rollback attacks; persisted all-required verification before any claim; feasibility result for reaching existing installs | PQ-07, 09, 13 | #1132 |
| Private PKI (I24) | ML-DSA issuer, leaf, CRL, chain on named relying parties; profile narrowing decidable; restore holds issuers | `internal/pki/pki_test.go`, `sign_ski_test.go`, store tests for 00064 | Relying-party matrix incl. platform verifiers; seed-only PKCS#8 import and rejection of expanded forms; bounds; both-engine restore | PQ-06, 08, 10 | #1133 |
| External routes and source supply chain (I14-I19, I25, I32) | Each route has exact protocol and version evidence, classification, owner, effect per profile, recheck trigger | `internal/sshca/openssh_e2e_test.go`, `internal/samlsp`, `internal/oidc*`, `internal/webauthnrp`, adapter tests | Register E-02 to E-07 and E-13 populated with peer evidence; no provider writes without authorization | PQ-06, 12 | #1134 |
| Import and evidence (I28, I29) | Non-claims recorded: SOPS sources are external (E-13); a digest has no signer | `internal/importer`, `internal/backupreceipt` | Each capability record states that no PQ claim covers them; any countersigning the owning child chooses ([migration](migration.md#rollback-floor-and-historical-verification)) | PQ-06, 12 | #1134, #1135 |
| Symmetric hierarchy (I01-I06) | Retained; category-1 floor objects named; AEAD usage cap derivation NOTRUN | `internal/crypto/*_test.go`, `internal/isolation` | No migration step weakens re-encryption coverage or nonce discipline | PQ-10 | each owner |
| Operator handoff and closure (PQ-14, PQ-15) | Portable runbooks validated against the tested binary; honest closure with remaining exceptions and separately authorized release status | none yet | Runbook commands run on the final binary; closure record by the parent maintainer | PQ-14, 15 | #1135, parent |
| Cross-cutting parsers | Pinned-version vectors, fuzz and negative cases, algorithm and scope binding, no private material in evidence | fuzz files in `internal/crypto/backup`, `internal/samlsp`, `internal/scimproto` | Per new parser: vector source, version and digest; corpus, seed, duration | PQ-08 | each owner, #1135 |
| Platform budgets | Size, latency, CPU, memory, bounded concurrency on amd64 and arm64 | none | Thresholds (DEC-17) then measurements; emulation alone is HOLD | PQ-11 | #1127 baseline, #1135 verifies |
| Final candidate | Complete-diff local checks, exact-head CI, Standards and Spec reviews, adversarial security review, maintainer approval; cross-provider review per the [waiver](../../adr/post-quantum-migration.md#owner-decisions-already-made-2026-11-02) | `docs/development/ci.md` | Candidate packet per the program evidence record | PQ-13 | #1135 |

## Refined scope, effort and critical path

Refinements following the cited decisions.

| Child | Refinement | Decision |
| --- | --- | --- |
| #1126 | Hybrid recipients only, at most 32, no re-encryption tool unless asked; every export path emits a receipt, a signed record binds it (a receipt alone is unsigned), and restore and drill gain receipt input; PQ signing of that record follows in #1132, so strict backup authenticity and receipt-bound provenance are HOLD until then (E-19) | DEC-05, 08, 11, 12 |
| #1127 | One strict group, TLS 1.3, tickets off; hops per the hop table; baseline measurement is its first deliverable | DEC-06, 17 |
| #1128 | New protocol version, receiver first, numeric field bounds, all-required signatures | DEC-03, 08 |
| #1129 | ML-DSA-65 only at first, new key identity | DEC-02 |
| #1130 | HOLD, E-01 | DEC-13 |
| #1131 | ML-DSA-65 command signature; DEC-20 outcome; journal requirements stated in migration | DEC-02, 20 |
| #1132 | PQ-capable trust documents, sequence-only progression, every replacement path closed or registered, anchor feasibility, signer per document and track, PQ signature on receipts and operator attestation | DEC-14, 15 |
| #1133 | ML-DSA-65 issuer and leaf, offline PQ root, seed-only PKCS#8 | DEC-02, 19 |
| #1134 | Populate E-02 to E-07 and E-13 with peer evidence | DEC-16 |
| #1135 | One packet per surface, not one universal statement; scope narrowed under DEC-13, with #1130 as linked residual work | DEC-13 |

Effort **estimates** (engineer-weeks, one primary engineer, low confidence, not
measured; from the number of consumer packages in the inventory trace and the
both-engine and client/UI work each needs; excludes #1130 and owner review time):

| Child | Estimate | Main drivers |
| --- | --- | --- |
| #1124 | 1 to 2 | Owner decisions and amendment review |
| #1126 | 3 to 5 | Writer, reader, custody, receipts, both-engine drills |
| #1127 | 2 to 4 | Per-hop policy, managed generation, peer evidence |
| #1128 | 2 to 3 | New protocol, receiver and sender |
| #1129 | 2 to 3 | Migration, API, CLI, clients, UI |
| #1131 | 2 to 3 | Trust transition and journal |
| #1132 | 3 to 5 | Schema, bridge, field installers, recovery |
| #1133 | 3 to 5 | Chain, CRL, relying parties |
| #1134 | 1 to 3 | Peer evidence |
| #1135 | 3 to 5 | Harness and runbooks |
| **Total** | **22 to 38** | About 5 to 9 calendar months for one engineer [est] |

This exceeds the issue's preliminary 9 to 17 engineer-weeks because that range
covered broad first-party work and this count adds UI/client consumers,
both-engine drills and qualification. The owner can narrow scope (for example
defer #1133 and #1129); that keeps those surfaces HOLD, not removed.

**Budget cycle (DEC-17, owner decided):** #1127 needs budgets and #1135
depends on #1127. Proposed: byte bounds from the derived sizes in profiles, and
latency, CPU and memory thresholds set by the owner from each child's own
baseline before that child's acceptance (#1127 TLS, #1129 Transit, #1132
release, #1133 PKI), with #1135 verifying. No threshold number exists yet.

Critical path (parent dependency order): #1124 approval, #1126, #1132 (blocked by
#1126), #1135; in parallel #1127, then #1133 and #1128. Uncertainty concentrates
in #1132 (field installers, PQ custody), #1130 (no feasible
protocol identified) and #1134 (external peers). No date for universal
third-party PQ support is promised.

## Decisions and what they govern

Owner for all: Marc Went (sole maintainer; security review is a separate gate by
the same person plus adversarial review). Accepted as recommended and security
approved on 2026-11-02. Options are in the register.

| Decision | Governs |
| --- | --- |
| DEC-01, 02, 03, 07, 09 | every implementation child |
| DEC-04 | #1126 wording, symmetric claims |
| DEC-05, 10 | #1126, #1127 |
| DEC-06 | #1127, #1133 |
| DEC-08, 17 | all (bounds, budgets) |
| DEC-11, 12 | every writer enablement |
| DEC-13 | #1130 |
| DEC-14, 15 | #1132 |
| DEC-16 | #1134 |
| DEC-18 | the amendment route for every locked ADR |
| DEC-19 | #1133 |
| DEC-20 | #1131 |
