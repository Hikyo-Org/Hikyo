# PQ qualification packet for candidate 68b2673de (#1135)

Verdict: **HOLD, by construction.** This packet does not qualify the post-quantum
program and cannot close #1123. The ticket says a test plan, a mocked provider
or a source-only checklist cannot satisfy runtime acceptance, and parent closure
needs all mandatory scoped work with real qualification evidence, or an
explicit owner-approved scope reduction. No real environment, deployment, real
provider, real cluster or real receiver was authorized, and none was used.
Mandatory children are PARTIAL or HOLD, the hosted merge-group graph never ran
on the candidate's stacked base, the only maintainer security approval is the
owner's 2026-11-04 merge authorization (see Merge status), and no independent
review covers the integrated candidate. Nothing below
is a pass of a parent requirement.

Refs [#1135](https://github.com/Hikyo-Org/Hikyo/issues/1135), parent
[#1123](https://github.com/Hikyo-Org/Hikyo/issues/1123). Companion files: the
[staged rollout runbook](../../operations/pqc-staged-rollout.md) (not
executable) and the [handoff](../../handoff/1135-qualification.md).

## Merge status (owner decisions of 2026-11-04)

Marc Went, the owner and maintainer, said "review them once more, and merge when
green", then "as recommended" to the recommendations below. This section states
what that makes true at merge and no more. The verdict above does not change.

- **Authorization.** Merge to `dev` is authorized when green, bottom-up, and
  publication in the next nightly is accepted. That instruction is the
  maintainer security approval for each PR that merges under it. It is not
  approval for release or deployment, enables nothing by default, and does not
  close #1123, which stays open.
- **Held.** #1172 (Transit, migration 00083) did not merge with this round. The
  owner decided the `/api/v1` freeze on 2026-11-04 (below), which ends that hold;
  #1172 still needs its public surface and its own review. Migration 00083 and Transit ML-DSA were not in what merged. Every statement below about 00083, Transit or the "two floors"
  describes the candidate, which contains #1172, not the tree that lands.
- **As built.** #1141, #1192 and #1190 merge as built. Their open owner
  choices are since decided or deferred (below).
- **Evidence.** All local results here are pinned to candidate `68b2673de`.
  That candidate is not evidence for the tree that lands: the tree that lands
  has hosted CI only. No local run, integrated run or independent review covers
  it.
- **Decided.** C-10: the rollback policy is accepted for the next nightly.
  Stale-node fence: accepted as a recorded limit; DEC-10 is amended so strict
  is per process and E-22 is registered, in `migration.md` on branch
  `dunky13/pqc-adr-amendments` (#1155), and the fence stays #1127 work.
  `backup-receipt/v2`: kept as built, banner extended, E-19 gap registered.
  Revision route for non-ADR specs: the declared-amendment banner. The
  `api-cli-surface` banner and the three narrowed banners are relabeled as not
  owner-locked in #1155.
- **Decided 2026-11-04 ("all open points as recommended").** #1130: option A,
  HOLD under E-01, with the E-01 fields accepted (expiry 2027-05-03). #1135 qualifies I20 at
  its classical profile only, and #1130 no longer blocks it (C-08). #1183
  (F-09): narrowed under DEC-06, with strict on the listener and owned clients and no strict
  claim on any external route. #1175 (F-01): strict does not disable the GitHub Actions
  secret route; it stays exception E-06, now effective. #1178 (F-04): `ssh-mldsa44-ed25519` is not
  adopted. #1179 (F-05): classical WebAuthn credentials under E-05. The register's other
  dispositions (F-01 to F-11) are accepted.
- **Deferred by the owner, still open.** #1133's five choices, until ML-DSA is
  enabled for PKI. #1132's key roles, sidecar floor, banner wording, pin delivery
  and recovery custody, until a signer exists. DEC-17 thresholds, until an amd64
  baseline exists.
- **Decided 2026-11-04, API freeze and items confirmed as built.** The `/api/v1` freeze: option A, the
  closed Transit and PKI enums grow once for `mldsa65` only. Transit signed bytes: the departure is
  accepted. Confirmed as built: the #1126 profile selector and vault path, the #1127 proxy leg, #1132
  signing granularity, and #1133 keeping the `VerifyConnection` walk and platform verifiers (C-05).
  C-10's step 2 abort is "restore the pre-upgrade backup".
- **Still open.** See "Pending owner decisions" under Release and blocked.

## Candidate and toolchain

| Item | Value |
| --- | --- |
| Candidate | PR [#1195](https://github.com/Hikyo-Org/Hikyo/pull/1195), branch `dunky13/pqc-1135-candidate`, commit `68b2673dea9eb7053edd77eca2e585146bb5ad2c`, tree `db58307e5a8aa54d684c52fed9148b9649dc9683`. Evidence only, not for merging. Every statement here is pinned to this commit; the packet's own commit adds documents only |
| Candidate base | `dunky13/pqc-adr-amendments` at `76143506a` (PR #1155). Nine child branches merged without conflict |
| Toolchain | Go 1.27.3 darwin/arm64 (`go.mod` `go 1.27.3`), OpenSSL 4.0.3 (local, used by children; #1128's signature cross-check used OpenSSL 3.5.9), no amd64 host |
| Dependencies (`go.mod`) | `filippo.io/age` v1.3.2, `filippo.io/hpke` v0.4.0 (indirect), `golang.org/x/crypto` v0.58.0, `github.com/go-jose/go-jose/v4` v4.1.6 (no ML-DSA at this pin), `github.com/go-webauthn/webauthn` v0.18.2, `github.com/sigstore/sigstore` v1.11.1, `github.com/sigstore/sigstore-go` v1.3.0, `github.com/jackc/pgx/v5` v5.11.0 |
| Datastores in the run | SQLite (embedded) and a disposable PostgreSQL 18.6 container from the CI-pinned image `postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941` (image id equals that digest) |
| Candidate PR table | #1127 #1156 `d3d662967`; #1126 #1158 `9c01e77e7`; #1134 #1171 `912b4f5c8`; #1129 #1172 `afda0f96b`; #1131 #1186 `5e7bbef32`; #1128 #1188 `d70889f82`; #1130 #1190 `35e057451`; #1133 #1141 `c904100cd`; #1132 #1192 `07eb2a721` (heads as listed in the #1195 body; children may have moved since) |
| Artifact digests, topology, peers | None exist. No artifact was built for release, published or signed. This packet contacted no peer, receiver, cluster or ingress. The only external contact any child made is #1134's credential-free SSH handshake to github.com (no key offered, nothing written). Peer versions: reference receiver built from the candidate (`cmd/hikyo-receiver-example`), `age` CLI 1.3.2, OpenSSL 4.0.3 (3.5.9 for #1128) and OpenSSH 10.3p1 only where a child used them |

Review provenance, stated once: the candidate merge `68b2673de` has no review of
any kind. Each child's last repair has no independent re-review (per-child
table). Cross-provider review is SKIPPED by explicit owner instruction
(2026-11-02) and is not CLEAN. Maintainer security approval: the owner's
2026-11-04 merge authorization for each child that merges, none for #1172.
Owner and maintainer are one person, so none of this is independent
human review.

## What this packet ran at the candidate

Evidence classes are separate and never substitute for each other. Local
commands ran once through the shared job queue, no rerun.

| # | Command (explicit `-timeout 40m -count=1 -p 2 -parallel 2`) | Result |
| --- | --- | --- |
| L1 | `go test -v -run 'TestAdapterSealedWebhookProtocolTwoEndToEnd\|TestPKIMLDSALifecycle\|TestPKICLISQLite\|TestPKICLIPostgres\|TestTransitMLDSA65EndToEnd\|TestTransitPublicSurfaceHoldsMLDSA65SQLite\|TestTransitPublicSurfaceHoldsMLDSA65Postgres\|TestBackupRestoreDrillHybrid\|TestRestoreDrillVerbHybrid' ./internal/isolation/` with `HIKYO_TEST_POSTGRES_DSN` set | **exit 0**, `ok ... 149.003s`. 55 PASS lines (11 top-level), 0 SKIP, 0 FAIL |
| L2 | `go test ./internal/configrollout/... ./internal/releasetrust/... ./internal/crypto/... ./internal/tlspolicy/... ./internal/backupreceipt/...` | **exit 0**. `ok` for configrollout, releasetrust, crypto, crypto/backup, crypto/sealedhook, tlspolicy, backupreceipt; `releasetrust/testfixture` has no test files |
| L3 | `go run ./scripts/release/compatibility --development --check internal/buildcompat/development.json` against an empty scratch database on the same PostgreSQL 18.6 | **exit 0**, no output (queue job 3263). `development.json` matches the regenerated bytes |

What L1 actually covered (read from its log, not assumed):

| Test | Engines and subtests that ran |
| --- | --- |
| `TestAdapterSealedWebhookProtocolTwoEndToEnd` | `sqlite` and `postgres`, both PASS. Reference receiver, protocol 2 sender, rotation, takeover |
| `TestPKIMLDSALifecycle` | `sqlite` and `postgres`, both PASS. Service level, not the API |
| `TestTransitMLDSA65EndToEnd` | `sqlite` and `postgres`, each with `software`, `external`, `authorization_before_crypto`, `external_unavailable_fails_closed`, `concurrent_rotation`, `restart_dek_rotation_and_reencrypt`, `creation_refusals`, `floors_do_not_end_classical_signing`, `audit_never_carries_signatures_or_keys`, all PASS. "Restart" is a new service struct, not a process restart |
| `TestPKICLISQLite`, `TestPKICLIPostgres` | One engine each. Real `app.Boot` and real CLI: the ML-DSA refusals and a classical positive control |
| `TestTransitPublicSurfaceHoldsMLDSA65SQLite`, `...Postgres` | One engine each. Real boot and CLI: `mldsa65` refused, Ed25519 unchanged |
| `TestBackupRestoreDrillHybrid{SQLite,Postgres}`, `TestRestoreDrillVerbHybrid{SQLite,Postgres}` | One engine each, plus the drill's subtests (truncated archive refused before state is committed at chunk boundary and mid-chunk, custody separation, instance came back, credentials dead, grants inert, one-principal reconcile). Added by me beyond the minimum list because backup/restore is a ticket requirement |

Hosted, read with `gh` at 2026-11-04T02:04Z for #1195 at `68b2673de` (run
41756773433 and siblings):

| Check | Result |
| --- | --- |
| `repository invariants (boundary, lint, CI policy)` | **success**, 8m38s. Steps `Repository invariant tests` and `Lint driver tests` both success. `reviewed_pins.json` auto-merged across children and did not fail lint |
| `feedback (go)`, `feedback (tooling)`, `feedback (frontend)` | success. Per-commit selective feedback (formatting, compile, selected tests), not the full graph |
| `ops-floor (amd64)`, `ops-floor (arm64)` | success. Operator diagnostics floor; **not** a PQ benchmark or PQ test and not counted as amd64 evidence |
| `ci-queue-required` | **fail by design**: "Only the protected default branch can enter this merge queue." The base is a stacked branch. The complete merge-group graph (16 domains) therefore never ran on any PQ code |
| `Check benchmark request`, `comment`, `report` | success; `CodeRabbit`, `Requested PR walltime`, `Requested PR browser performance`, `automatic`, `request`, `report` skipped |

Not run by this packet: `internal/lint` locally (instructed; hosted result
above), the full `internal/app`, `internal/service`, `internal/server`,
`internal/store`, `internal/pairing`, `internal/selfupdate`, `internal/sshca`,
`internal/pki`, `internal/cli` suites, any fuzz campaign, `fast-feedback.sh`
on the candidate, `go build ./...` of every tool, any benchmark, any real host.
Their results exist only as each child's own handoff (per-child table), at the
child's own head, not at the candidate.

### Integration facts a single child could not see

| Check | Finding |
| --- | --- |
| Migrations | The union adds exactly one SQL migration, `00083_transit_mldsa65.sql`, on both engines (#1129). No duplicate number; highest version on both engines is 83 and `main` ends at 82. #1133 added none (confirmed: no CHECK on `pki_issuers.key_algorithm`). #1128 added none. #1127's "migration 6" is a separate runtimeconfig sequence (`internal/runtimeconfig/migrations.go`, `MigrationVersion = 6`), not a SQL number; the collision #1127 warned about did not occur |
| Build identity | `internal/buildcompat/development.json` changed in the union only by the version-83 entry and schema digests (#1129); L3 exit 0 confirms it still matches against PostgreSQL 18.6. All isolation tests also boot against it (L1) |
| Lint pins | `internal/lint/testdata/reviewed_pins.json` is touched by #1127 (five hashes for runtimeconfig migration 6) and #1126 (two helpers, one hash). The merged file passes hosted `Lint driver tests` at the candidate |
| Docs links | All relative links in the PQ docs, handoffs and the ADR resolve at the candidate (scripted check over 29 files). `docs/adr/pki-ml-dsa.md` is gone, as #1133 intended |
| Callers | The only non-test callers of `VerifyPQ*`, `NewPQPin` and `PQFloor` are inside `internal/releasetrust` and its `testfixture`. `service.PKI.MLDSAEnabled` is never set outside tests |

## Cross-child contradictions and ownership gaps

Reported, not repaired. The register text is the owner's and no child edited it.

| ID | Finding | Sources | Owner action |
| --- | --- | --- | --- |
| C-01 | **DEC-07 is applied inconsistently.** Conform: #1126 (`age-hybrid-mlkem768x25519-sha256:`), #1128 (`ed25519-sha256:`, `mldsa65-sha256:`, `mlkem768x25519-sha256:`, `swh2-...`), #1131 (`<alg>-sha256:` over label, algorithm and raw public key). Depart: #1129 (signed bytes are the raw message with the label only as context and `0x0905` outside the signature) and #1133 (`sha256:` fingerprint spelling over the SPKI). #1132 conforms in intent but spells ids `<alg>:` plus SHA-256 over the SPKI, a third spelling. So "algorithm-bearing identifier" has at least five constructions: #1126 hashes the canonical recipient text with no domain label; #1128 and #1131 hash a domain label, the algorithm and the raw public key under different labels; #1132 hashes a domain, the algorithm and the canonical SPKI under a bare `<alg>:` prefix; #1133 hashes the SPKI with no domain. The same `age1pq1` recipient therefore gets two ids, `age-hybrid-mlkem768x25519-sha256:` (#1126) and `mlkem768x25519-sha256:` (#1128), although both are listed as conforming | handoffs 1129, 1133; `crypto/backup/recipients.go`, `sealedhook/v2.go`, `configrollout/authority.go`, `releasetrust/pq.go`, `pki/keys.go` | **Decided 2026-11-04:** the #1129 departure is accepted; #1133's is deferred with its other choices. Open: whether one id construction is required across surfaces |
| C-02 | **Register premises are stale.** DEC-13 said `go-jose` lacks ML-DSA (row corrected 2026-11-04), but v4.1.8 has it for Go 1.27 (#1130) while the repository pin v4.1.6 does not (#1134, tracked in #1191); DEC-16 and migration say OpenSSH has no PQ signature, but 10.6 enables a composite one (#1134); the JOSE ML-DSA identifier status "not checked" is now RFC 9964 | handoffs 1130, 1134 | Owner's next register version for DEC-16 and the JOSE status |
| C-03 | **Scanner statement conflict.** `migration.md` says the vendored rule matches `AGE-SECRET-KEY-1` only, so only the PQ form is unmatched. #1126 found the vendored `age-secret-key` rule is not on the allowlist and is not compiled, so **neither** form is matched. #1128 repeats the narrower statement | `migration.md` (Scanning row); handoff 1126; handoff 1128 | #1126 requirement and a secret-scanning ADR change (64-rule ceiling) |
| C-04 | **E-18 names two unrelated limits** (the single listener certificate pair, #1127; the sole ML-DSA recovery key, #1132 R-12). The capability statement below disambiguates them | `migration.md`; `release-trust-exceptions.md` | Owner may split the ID |
| C-05 | **Ownership gaps.** The `VerifyConnection` ML-DSA walk: #1127 says #1133, `profiles.md` says #1127 and #1133, #1133 says not built (G3) and asks the owner. Platform-verifier and browser qualification: `qualification.md` assigns #1133, #1133 proposes moving it to #1134, #1134 does not mention it. #1133 holds both (its G3 and G4 name #1133) | handoffs 1127, 1133, 1134; `profiles.md`; `qualification.md` | **Decided 2026-11-04:** the owner confirmed #1133 keeps both |
| C-06 | **Exception ID spaces collide in style.** E-nn (migration), R-01 to R-17 (release trust), R-1128-A/B (webhook), G-1 to G-4 (rollout), G1 to G7 (PKI), F-01 to F-11 (external), X01 to X24 (routes). `G-1` and `G1` mean different things | the exception documents | Owner may normalise before any row is made effective |
| C-07 | **No E-nn row was effective at the candidate.** DEC-10 needs exact peer, version, expiry and recheck trigger. #1134 proposes terms for E-02 to E-07 and E-13 and says none is effective; the owner accepted #1130's E-01 terms on 2026-11-04; `release-trust-exceptions.md` is "drafted for review, not operative". No document at the candidate carries all four fields for any E row. E-22, E-01 and E-06, set on 2026-11-04 after the candidate, do | `external-compatibility.md`, `pairing-feasibility.md`, `release-trust-exceptions.md` | Owner names peers and accepts terms |
| C-08 | **Pairing scope text, accepted 2026-11-04.** DEC-13 says #1135 was blocked by #1130 and that scope is narrowed with #1130 as residual work. The owner accepted the narrowing text in the #1130 handoff: #1135 qualifies I20 at its classical profile only, and #1130 no longer blocks it. The `qualification.md` pairing row, the DEC-13 row and this packet now say so. The pairing and revocation drill the narrowing requires is NOTRUN. The issue links are not edited here | handoff 1130; `qualification.md` | Remove #1130 from #1135's "Blocked by" list (parent) |
| C-09 | **Index gaps.** `release-trust-exceptions.md` is not listed in `README.md`; the pairing banner is operative "when this docs PR merges" (#1130) while every other banner is operative when the implementation merges; the proposed `deployment-adapter` banner for the webhook revision route and the threat-model "quantum forger" class have not landed; `README.md` says "no ADR amendment has landed", stale at the candidate because banners have landed on locked ADRs (operative only when their implementation merges). The sentence is the contract's text and is not edited here | `README.md`, ADR banners | Owner |
| C-10 | **Two floors move on the candidate that the register does not name together.** Migration 83 (#1129) and runtimeconfig migration 6 (#1127) both reach every upgraded instance whether or not the feature is used. See Rollback. `migration.md` step 2 gave only "Revert binary" as the abort for deploying readers, and now names the backup for this case: a moved floor is not shown to be survivable by an old binary. Whether an old binary refuses migration 83 or runtimeconfig migration 6 was not run (PQ-09, Rollback; `migrate.Check` has no production caller). For a reader build carrying these two floors the abort in the runbook is "restore the pre-upgrade backup", because the refusal is NOTRUN | `migration.md`; handoffs 1127, 1129 | **Decided 2026-11-04 for this round:** the rollback policy is accepted for the next nightly. #1172 is held, so migration 83 is not in what merges and only runtimeconfig migration 6 moves a floor on merge. **Decided 2026-11-04:** the step 2 abort for such a build is "restore the pre-upgrade backup" |
| C-11 | **Banner wording differs** (`api-cli-surface` says adversarial review "is recorded on the amendment PR", others say "is required"; `threat-model`, `encryption-model` and pairing carry the sentence once in different forms). Wording inconsistency, not a missing banner | ADR banners | Cosmetic |
| C-12 | **DEC-09 closed error classes are not one set.** The register names six classes (unsupported, malformed, over bound, wrong key or scope, invalid signature, missing required signature). `releasetrust` has those six plus `ErrPQRollback`; `sealedhook` has eight sentinels (`ErrMalformed`, `ErrSignature`, `ErrUnknownKey`, `ErrExpired`, `ErrPayload`, `ErrAckMismatch`, `ErrTooLarge`, `ErrRecipient`); `configrollout` uses `ErrInvalid`, `ErrConflict`, `ErrTrust`. Each is closed per surface; none maps to the register's six | `releasetrust/pq.go`, `sealedhook/sealedhook.go`, `sealedhook/v2.go`, `configrollout/kubernetes.go`, `authority.go` | Owner says whether DEC-09 means one shared vocabulary |
| C-13 | **DEC-12 key generation is applied unevenly.** `migration.md` says entropy is `crypto/rand` and imported keys are accepted for new strict material only if generated by the maintained tool. The #1131 and #1133 guides generate ML-DSA keys with OpenSSL (`openssl genpkey -algorithm ML-DSA-65`), and the code accepts them (webhook keys by path; the webhook handoff says the rule is not enforced). The runbook's key-loading rule names webhook keys as unenforced and the rollout and PKI guides as generating ML-DSA seeds with OpenSSL | `migration.md`; `config-rollout-authority.md`; `pki-ml-dsa.md` | Owner says whether OpenSSL-generated seeds are the "maintained tool" |
| C-14 | **#1132 registers a departure from "any trust change requires every required algorithm of the current pinned set".** In the verifier a higher PQ generation passes the floor check with no signature and `NewPQPin` verifies none: the root is whatever the operator installs (E-11). It is registered in the #1132 handoff, not in the register | `migration.md` (Compromise recovery); handoff 1132 | Owner accepts or rejects the departure |

## Requirement map, PQ-01 to PQ-15

Criteria are the parent's fifteen, from the program design. Cells use PASS
(observed, scoped), PARTIAL, HOLD (missing decision or prerequisite), NOTRUN,
n/a. Hosted means the PR feedback and invariants above only. Peer and provider
combine real consuming stacks and external providers: this packet contacted none;
the only external contact any child made is #1134's credential-free SSH handshake to
github.com (no key, no write), besides the stand-alone peers named.

| ID | Children | Source | Local | Hosted | Peer, provider | Runtime, deployment | Overall and why |
| --- | --- | --- | --- | --- | --- | --- | --- |
| PQ-01 inventory | #1124 | PARTIAL: I01 to I32 at a pin; completeness beyond the stated searches not claimed; I18 omits PS algorithms and I25 is stale (#1134) | n/a | n/a | n/a | n/a | PARTIAL. Maintainer review of the data-flow and exception matrix HOLD |
| PQ-02 approved profiles | #1124 | Owner decided DEC-01 to DEC-20 and approved the policy text on 2026-11-02, after being told final repairs were not re-reviewed | n/a | n/a | n/a | n/a | PARTIAL. Decided, not operative: each ADR amendment becomes operative only when its implementation merges, and each needs its own security approval, which the owner's 2026-11-04 merge authorization supplies for each PR that merges, #1172 excepted. Stale premises C-02 |
| PQ-03 hybrid backups | #1126 (#1158) | PASS: hybrid recipient, receipt v2, drill | PASS: L1 hybrid drills on both engines, L2 `crypto/backup` and `backupreceipt` | compile and invariants | `age` CLI 1.3.2 opens hybrid archives (same upstream, not independent) | NOTRUN | PARTIAL. Strict profile NOT BUILT; strict restore provenance HOLD (E-19); vault reads but does not generate hybrid identities; no preflight reader floor; rollback documented not demonstrated; restart of a process not tested; `RunAutomaticUpgrade` NOTRUN |
| PQ-04 controlled-hop confidentiality | #1127 (#1156) | PASS for listener, CLI bundle, operator client library option | PASS per child (real `Boot`); at the candidate only L2 `tlspolicy` ran | compile and invariants | None | NOTRUN: no production group observed | PARTIAL. Stale-node fence not built, accepted limit E-22; strict reaches no external hop; no deployed Kubernetes operator can select strict; proxy, database, adapter, federation hops are compat and NOTRUN; authentication stays classical (E-20) |
| PQ-05 delivery, command and proof lifecycles | #1128, #1129, #1130, #1131 | webhook v2 PASS; Transit mechanism PARTIAL; rollout PASS; pairing none | PASS at candidate: webhook v2 e2e on both engines, Transit e2e on both engines. Rollout per child with an instrumented fake Kubernetes API; L2 `configrollout` ok | compile and invariants | Reference receiver only; no real receiver, no real cluster | NOTRUN | **HOLD.** Pairing HOLD (#1130); Transit and PKI unreachable through the API at the candidate; rollout kind rehearsal NOTRUN; open job and restore cannot cross a re-enrollment (G-2, G-3) |
| PQ-06 certificates and external identities | #1133, #1134 | PKI mechanism PASS below the API; register drafted | PASS at candidate: PKI lifecycle on both engines, CLI refusal on each engine. Go and OpenSSL 4.0.3 relying parties per child | compile and invariants | No browser, platform verifier, Java, NSS; no external route verified | NOTRUN | **HOLD.** API/CLI/WebUI refuse `mldsa65`; G1 to G7 open; F-09 narrowed under DEC-06 (no strict claim on external routes); no route is "verified supported" |
| PQ-07 installer and updater | #1132 (#1192) | Readers and verifiers only | PASS per child; L2 `releasetrust` ok, test-local sequencer | compile and invariants | None | NOTRUN | **HOLD.** No signer, no root, no shipping path calls the verifiers, no bridge can close E-11 and E-15, ML-DSA recovery custody not named (DEC-14) |
| PQ-08 bounded, checked formats | all | Closed sets and bounds in each parser | L2 and L1 ok. Short fuzz runs per child (8 to 15 s, no corpus kept). Frozen vectors per child | n/a | OpenSSL cross-checks (4.0.3 for Transit and PKI, 3.5.9 for webhook signatures; local only) | n/a | PARTIAL. Pinned Wycheproof vectors (DEC-09) NOTRUN everywhere; no long fuzz campaign; second independent age implementation NOTRUN. This packet ran no fuzz |
| PQ-09 safe transition and recovery | #1126, #1128, #1129, #1131, #1132, #1133 | Floors designed per surface | Partial per child (see Rollback). L1 backup drills on both engines | n/a | n/a | NOTRUN | PARTIAL. Interrupted-write and restored-stale-state coverage is per child and incomplete; a pre-83 binary against an 83 database is NOTRUN |
| PQ-10 preserved invariants | all | Authorization before crypto, redacted audit, re-encryption coverage kept | PASS on both engines at candidate for webhook, Transit (restart, DEK rotation, re-encryption, concurrent rotation), PKI service; backup drills | compile and invariants | n/a | NOTRUN | PARTIAL. Restore holds with an ML-DSA key (Transit, PKI), real process restart (PKI G6), true multi-node takeover (Transit, PKI) NOTRUN. Webhook takeover is a two-node simulation |
| PQ-11 measured budgets | #1127, #1128, #1129, #1131, #1132, #1133 | n/a | Apple M2 Pro arm64 only, loaded host (Budgets) | n/a | n/a | NOTRUN | **HOLD.** DEC-17 thresholds are unset by the owner; amd64 NOTRUN everywhere; no compliance statement is possible. This packet took no measurement |
| PQ-12 owned external register | #1134 (#1171) | 24 routes X01 to X24 drafted | PASS (children): OpenSSH 10.3p1 negotiation, libsodium 1.0.22 | compile and invariants | NOTRUN: OpenSSH 10.6, real providers, PostgreSQL hybrid, federation, mail | NOTRUN | **HOLD.** Dispositions recorded 2026-11-04; F-09 narrowed under DEC-06; only E-06 has effective terms among the #1134 rows (C-07) |
| PQ-13 validated final candidate | #1135 | n/a | L1 to L3 above, partial | invariants and feedback pass; ci-queue-required fails by design; full graph never ran | n/a | n/a | **HOLD.** No complete-diff local checks of the union, no independent Standards, Spec or adversarial review of the candidate, maintainer security approval only as the 2026-11-04 merge authorization, none for #1172 |
| PQ-14 operator handoff | #1135 and owners | Runbook drafted | Commands checked against `--help` of a binary built from the candidate; no command executed against an instance except `config-rollout --print-authority-ids` with throwaway keys; `backup keygen` ran once (no database created) and OpenSSL key generation once, both throwaway | n/a | n/a | NOTRUN | **HOLD.** Not executable while the owner's open decisions stand |
| PQ-15 honest closure | parent maintainer | n/a | n/a | n/a | n/a | n/a | **HOLD.** Mandatory children incomplete; the DEC-13 narrowing for pairing is decided and its text accepted on 2026-11-04 (C-08), with #1130 as the residual gap; no other scope reduction exists |

## Exception map, E-01 to E-22

"Evidence" is what the candidate shows. "Effective" is whether the row carries
peer, version, expiry and recheck trigger. Only E-01, E-06 and E-22, set by the owner's decisions after the
candidate, do (C-07).

| ID | Limit (short) | Child | Evidence at the candidate | State |
| --- | --- | --- | --- | --- |
| E-01 | Pairing classical; active forger; recorder of a pairing or directory exchange; pin read over classical chain | #1130 | Docs only; classical flow untouched; terms accepted 2026-11-04 (expiry 2027-05-03) | HOLD, effective as an exception |
| E-02 | SSH CA classical | #1134 | Register X07, X08; local OpenSSH 10.3p1 test (child run) | Registered, not effective. OpenSSH 10.6 composite: NOTRUN |
| E-03 | Sigstore, platform, Kyverno, chart policy, commit signing, Go checksum DB | #1134, #1132 | X16, R-01, R-04 to R-08 (not operative) | Registered, not effective |
| E-04 | OIDC, SAML IdPs | #1134 | X10 to X12 | Registered, not effective |
| E-05 | WebAuthn | #1134 | X09 | Registered, not effective |
| E-06 | GitHub secret delivery (libsodium sealed box) | #1134 | X01; sealed-box shape test (child run) | Effective 2026-11-04 (expiry 2027-05-03); strict does not disable the route |
| E-07 | Adapters, mail, re-crossing rotated credentials | #1134 | X02 to X06, X17, X19, X23 | Registered, not effective |
| E-08 | Hops marked compat | #1127 | Hop matrix: every production group UNKNOWN | Registered; observed NOTRUN |
| E-09 | Combined local upgrade custody | #1126 | Vault unchanged; automatic/unattended upgrade refuses a hybrid policy by design | Registered |
| E-10 | Classical archive copies kept | #1126 | No re-encryption tool; documented | Registered |
| E-11 | PQ trust-anchor bootstrap, fresh-install channel | #1132 | Bridge analysed, not closable; no shipping path calls a verifier | **HOLD.** Limits every authenticity claim |
| E-12 | Historical classical signatures; audit exports | #1132, #1135 | Verifiers refuse classical-only history for PQ claims. **Audit export non-claim, recorded here as required by `migration.md`:** this packet and the candidate give no integrity, provenance or non-repudiation claim for any audit export, classical or PQ; `migration.md` says exports carry no chain and are forgeable by a datastore writer; I did not independently verify the audit ADR text | Recorded |
| E-13 | Imports, ambient SDK sources | #1134 | X15 | Registered, not effective |
| E-14 | Rollout enrollment and journal on classical cluster access | #1131 | G-1 to G-4; table of re-enrollment cases; fake API only | Registered |
| E-15 | Nightly, `sudo hikyo upgrade`, image replacement | #1132 | R-02, R-03; no PQ signer | **HOLD.** Limits every authenticity claim |
| E-16 | Operator pin rotation and break-glass P-256 | #1126, #1132 | Unchanged | Registered |
| E-17 | Unobservable readers, verifiers and local floors | #1126, #1132, #1135 | PQ floor persistence on a real host NOT BUILT; R-11 old binaries cannot refuse; no fleet-wide claim is possible. **#1135 verification: not possible by construction**, third-party and offline copies cannot be measured | Registered, unclosable |
| E-18a | Single listener certificate pair excludes non-ML-DSA clients | #1127 | Not built; no strict ML-DSA listener certificate exists | Registered |
| E-18b | Sole ML-DSA recovery key (R-12) | #1132 | Custody not named (DEC-14) | **HOLD** |
| E-19 | Archive provenance without a signed receipt | #1126 | Receipts unsigned; only an upgrade export builds a receipt at the candidate (`internal/service/backup.go`), so a plain `hikyo backup export` archive has none. **The requirement that every export path emits a receipt (`migration.md`, `qualification.md`) is unmet.** The candidate predates it; #1126's repair registers it as NOT BUILT under E-19 in its handoff. Residual risk: no provenance claim for any plain-export archive; owner #1126. Header-versus-inventory binding is also a requirement | **HOLD.** Strict backup claims unavailable |
| E-20 | Pin-based clients on strict hops | #1127 | Documented; strict gives confidentiality with no authenticity claim | Registered |
| E-21 | Host-authority recovery below strict; repair boot runs previous snapshot | #1127 | Registered, not redesigned | Registered |
| E-22 | Stale-node fencing under strict: strict is per process, and a stale node keeps serving | #1127 | Added 2026-11-04 after the candidate; no fence built | Effective: peer, version, expiry 2027-05-03 and recheck trigger are in `migration.md` |

Child-registered limits that #1135 or an owner must carry: R-01 to R-17
(release; R-13 freshness "#1135 verifies": not verifiable, no clock exists),
R-1128-A (classical write path while v1 is served) and R-1128-B (no webhook
rollback floor), with owners #1128 and #1135 for closure; #1133 G1 (response and
list size: `cert list` past about 355 ML-DSA rows fails), G2 (gate off is not a
rollback), G3, G4, G5, G6 ("#1135 verifies": NOTRUN); #1131 G-1 to G-4.

## Per-child acceptance, as each handoff states it

Statuses are copied from the handoffs, not improved. "Last repair" is what has
no independent review according to the child PR body or handoff.

| Child, PR, ticket | Handoff status | HOLD, PARTIAL and NOTRUN items | Last repair without independent review |
| --- | --- | --- | --- |
| #1124 contract, #1155 | Owner decided, security approved for policy text, not operative; amendments HOLD | Maintainer review of data-flow matrix HOLD; every measurement NOTRUN | Repairs after review at `6263944a2` (HIGH 1, MEDIUM 4, LOW 6, all repaired by narrowing); the `api-cli-surface` banner |
| #1126 backups, #1158 | Criteria 1 PASS (vault PARTIAL), 2 PASS (offline tooling PARTIAL), 3 PARTIAL, 4 PASS, 5 PASS, 6 PARTIAL, 7 PASS except restart, 8 PASS (automatic custody PARTIAL), 9 PASS, 10 PARTIAL; strict provenance HOLD | Strict profile, signed receipts, a receipt on every export path (only upgrade exports build one; registered as NOT BUILT, E-19, in #1126's repaired handoff), hybrid vault generation, multi-identity vault, vault re-seal on root rotation (#1154), scanner rule: NOT BUILT or HOLD; `backup-receipt/v2` owner accepted on 2026-11-04; policy race window; `RunAutomaticUpgrade` NOTRUN; no recovery for a source frozen at `backup-preparing` whose policy became hybrid | `4bad16b59` |
| #1127 transport, #1156 | Core path only. Listener PASS; stale-node fence HOLD; evidence PARTIAL; hop coverage PARTIAL; budgets NOTRUN | CLI `--tls-profile` flag, CRD field, metric series, proxy-to-backend, database, mixed-version old binary, platform verifiers, multi-node Apply under load, migration 6 end to end with two release identities: NOTRUN or not built | `266436c47`, `f5d549182` |
| #1128 webhook v2, #1188 | Specification and identifiers: DONE (text), second review NOTRUN; hybrid recipient, signatures and key ownership: PARTIAL (the probe does not prove recipient ownership); key generation, configuration and tooling: PARTIAL (no API, CLI or UI consumes the registry; no product-binary generator); reject classical-only, stripping and substitution: DONE; bounds: DONE; authorization, egress, audit and fences preserved: DONE; end-to-end lifecycle: DONE (local); keep v1 targets and no downgrade on error: DONE; skew, reader-first, overlap and rollback: DONE (local), with limits; vectors, migration instructions and evidence: PARTIAL (instructions not followed hands-on on a clean host) | Real receiver and push NOTRUN; Wycheproof NOTRUN; revision route HOLD; R-1128-A, R-1128-B; PQ-only suite not built | Second-round repairs (`8bad510a5` onward) need re-review |
| #1129 Transit, #1172 (not merged in this round) | Mechanism complete below the API. API, OpenAPI, clients, CLI, WebUI not built at the candidate; the freeze is decided (option A) | Restore hold with ML-DSA key, instance re-encrypt, multi-node takeover, pre-83 binary against 83 database, Wycheproof pin: NOTRUN; signed-bytes departure accepted 2026-11-04 | Round-3 repairs at `8e11e9a36` have no independent review; `afda0f96b` is documentation |
| #1130 pairing, #1190 | HOLD. Criterion 1 DONE; 6 PARTIAL; 2, 3, 4, 5, 7, 8, 9, 10 HOLD | No PQ PAKE; ML-DSA-65 DPoP is 8252 bytes against the 8192 cap; E-01 effective 2026-11-04; narrowing text accepted | Repair answering the first review; re-review pending |
| #1131 rollout, #1186 | DONE for the controller and signer on a fake API; PARTIAL for restart/restore, runbook; independent review of repaired head HOLD | Real cluster and kind rehearsal, upgrade-source commands under strict, old binary against new data, `helm upgrade` effect: NOTRUN; G-1 to G-4 | Repair after `2ceda38b2`; "needs review of the repaired head" |
| #1132 release verifiers, #1192 | Criteria 4 DONE in the verifier; 1, 2, 3, 7, 8, 9, 10, 11 PARTIAL; 5 NOT BUILT or HOLD; 6 DONE for release trust, HOLD for receipts | Bridge HOLD; verification on every official path NOT BUILT; persisted PQ floor on a real host NOT BUILT; R-01 to R-17 not operative; runbook not executable | Round-three repairs |
| #1133 PKI, #1141 | Criteria 5 and 10 DONE; 2 and 4 DONE at service level with API/CLI/WebUI HOLD; 1, 3, 6, 7, 8, 9 PARTIAL | Five choices deferred by the owner until ML-DSA is enabled for PKI; G1 to G7; browsers and platform verifiers unqualified | Final repair round; reviewer reports hosted lint at earlier heads |
| #1134 external, #1171 | Documentation DONE; inventory, dispositions, actual-client qualification PARTIAL; strict refusal narrowed under DEC-06 (F-09, 2026-11-04): no strict claim on external routes | OpenSSH 10.6, real providers, PostgreSQL hybrid, federation, mail: NOTRUN; the F-09 disposition #1134 needed is recorded | `9b4517d66` onward (coordinator fixes) |

## Staged ladder per surface

Legend: D = demonstrated locally at the candidate or in the child's own run;
child-run results are not rerun here. NOTRUN is explicit. Strict enforcement of a
fleet, a real hop or a real artifact is NOTRUN everywhere.

| Surface | Legacy | Compatible reader first | New writes | Strict enforcement |
| --- | --- | --- | --- | --- |
| Backups | D: frozen legacy archives open under the new reader (child) | D: reader allowlist, mixed stanzas refused (child, L2) | D: hybrid keygen, export and receipt v2 (child run; L2 `backupreceipt` covers the receipt only); hybrid drill and restore on both engines (L1, whose drill tests call `backup.GenerateHybridIdentity` directly) | NOT BUILT (no selector); provenance HOLD (E-19) |
| TLS listener | D: `compat` default, migration 6 records it (child) | n/a | D: `strict` via managed config in a real `Boot` (child) | D per process only; no stale-node fence (accepted limit E-22); NOTRUN on a real hop and as a fleet |
| Sealed webhook | D: v1 unchanged, byte-compatible (child, L1) | D: reference receiver serves v1 and v2 independently; receiver-first (child, L1) | D: protocol 2 sender, dual signatures, both engines (L1) | D with the reference receiver omitting v1 (404); R-1128-A, R-1128-B; real receiver NOTRUN |
| Transit | D: Ed25519 unchanged through the real server (L1) | n/a (verification consumers are the same service) | D below the API only (L1 service level); the real server refuses `mldsa65` (L1) | Not applicable; no public surface |
| Rollout | D: v1 command bytes pinned to a base-commit value (child) | D: compatibility to dual, key kept (child, fake API) | D: dual and PQ-only signing, delivery, verification (child, fake API) | D: floor and anchor at the controller (fake API); kind and real cluster NOTRUN |
| Private PKI | D: classical suites unchanged (child) | n/a | D below the API at service level on both engines (L1); API/CLI refuse (L1) | NOT BUILT: product `VerifyConnection` walk; mixed paths refused at install (child) |
| Release trust | D: legacy installers unchanged (child shell suites) | D: verifiers exist, test-local sequencer (child) | **None.** No signer, no root, no caller on any shipping path | HOLD (E-11, E-15) |
| Pairing | Unchanged classical flow | None | None | HOLD (E-01); pairing and revocation drill NOTRUN |

## Rollback, per phase

| Phase or surface | Rollback path | Proven | Not proven |
| --- | --- | --- | --- |
| Backups: hybrid recipients | Configure X25519 recipients again; keep every identity that wrote a retained archive | Legacy archives open with the new reader (child); hybrid drill (L1) | Old binary against a hybrid archive was run against pre-change code once by the child, not by me; reader floor not enforced by preflight; automatic upgrade of a frozen source cannot recover |
| TLS strict | Publish and apply `compat`. **Format floor:** runtimeconfig migration 6 writes `HIKYO_TLS_PROFILE=compat` into a new desired revision; from reading the source an older binary refuses it, which was not run, so binary rollback is a backup restore | Refusal of unknown values; recovery baseline keeps strict (child) | A real old binary against migration 6 data; migration 6 with two real release identities; E-21 repair boot below strict; CLI older than the closed-set decoder silently drops `tls_profile` |
| Webhook v2 | Restore the v1 registry entry and remove `signing_key_file_mldsa65` | Sender rollback and receiver skew with the reference receiver (child, L1) | **No floor exists** (R-1128-B): an administrator can restore v1, which is a downgrade of the property; receiver rollback only to a v2-capable build |
| Transit ML-DSA | **Migration 00083 (#1172, held and not in what merges) is roll-forward only and moves the floor for every instance that upgrades**, whether or not an `mldsa65` key exists; downgrade means restoring a pre-83 archive | Migration rebuilds and keeps CHECKs on both engines (child); L1 | Whether a release binary older than this change refuses an 83 database is NOTRUN (`migrate.Check` has no production caller); restore-hold with an ML-DSA key NOTRUN |
| PKI ML-DSA | Never delete an ML-DSA issuer, CRL or key to roll back; turning `MLDSAEnabled` off is not a rollback (G2) | Retirement and CRL continuity on both engines at service level (L1) | Older Hikyo build against a database holding an ML-DSA issuer; real archive restore with an ML-DSA issuer; real process restart (G6) |
| Rollout strict | Floor in the journal Secret `trust.json` blocks rollback; the only way back is clearing it, an owner break-glass act that resurrects whatever you reinstall; rolling the controller image back after a strict command fails closed | Floor, stale-enrollment and takeover tests (child, fake API) | Real cluster; old binary against new data (modelled); a restore of the journal Secret restores an older floor |
| Release trust | None to roll back: nothing is written. When a signer exists, never lower a floor or re-enable a revoked key | Revert only | PQ floor persistence NOT BUILT; a floor is an untrusted local claim (E-17) |
| Retention and retirement | Keep old keys and archives until the owner-recorded eligibility holds; re-encryption cannot revoke copies already held | Documented | No retirement drill was run anywhere |

## Budgets

All numbers below were recorded by the child, not by this packet. Machine for
every row: Apple M2 Pro (12 cores), darwin/arm64, Go 1.27.3, host under load
(load average roughly 10 to 17 from other jobs), unless stated. **amd64 was not
measured by any child. DEC-17 thresholds for latency, CPU and memory are unset
by the owner and this packet invents none.** A budget statement is therefore
impossible: PQ-11 is HOLD.

| Child | Measurement |
| --- | --- |
| #1126 | Hybrid recipient 1959 characters, identity 77; 32 recipients 62,719 bytes (managed value cap 65,536); header with 32 recipients 49,846 bytes; 33 refused. No latency, CPU or memory figure recorded |
| #1127 | Handshake benchmark, in-memory pipe, P-256 test chain, `-benchtime 200x`: compat 452 us, 873 allocs, 3799 bytes; strict 446 us, 865 allocs, 3723 bytes. Go's default already offers the hybrid group, so strict adds no bytes on this toolchain. No concurrency, memory or resumption figure |
| #1128 | One-value envelope 7,873 bytes, ack 4,961; seal 0.86 ms, open 0.50 ms (both signatures and hybrid wrap); largest legal envelope 1,478,120 of 1,529,176 bytes, largest ack 5,038; two-pass string cost 49 MiB for one 1.5 MB invalid-UTF-8 string, 41 MiB for 256 large names (registered); empty-name body under 4 MiB (was 99 MB). Three runs, load 10 to 12 |
| #1129 | Medians of three, `-benchtime 300x`: open key 16 us (Ed25519) vs 160 us (ML-DSA-65); sign 256 B 35 vs 575 us; sign 64 KiB 126 vs 650 us; verify 256 B 41 vs 113 us; verify 64 KiB 86 vs 188 us; public key 32 vs 1952 bytes (1974 SPKI); signature 64 vs 3311 bytes; wire value 95 vs 4424 characters. ML-DSA sign is about 16 times Ed25519 |
| #1131 | `-benchtime 200x`: Ed25519 sign 41 us, verify 74 us; dual sign 0.91 ms, verify 0.24 ms; PQ-only sign 0.54 ms, verify 0.20 ms; about 15 KB allocated per dual sign; dual command about 5.7 KB against a 512 KiB cap |
| #1132 | `-benchtime=300x -count=3`: ML-DSA public key 1,952 B (SPKI 1,974, PEM 2,726); signature 3,309; signature set 4,592 B (release) or about 4,827 B (recovery); verify keyset 0.26 to 0.30 ms; verify snapshot 0.76 to 0.80 ms; verify manifest set 0.145 ms; verify one artifact 0.16 to 0.22 ms; about 4.6 KB added per manifest-level signature, about 165 KB if every artifact (estimate). Fixture CA is test-local |
| #1133 | `-benchtime 200x -count 3`, medians: sign leaf 139 us (P-256) vs 903 us (ML-DSA-65); verify 3-certificate chain 156 vs 342 us; parse CSR 95 vs 179 us; CRL 0 entries 158 vs 760 us; CRL 1000 entries 2.76 vs 3.25 ms; full TLS 1.3 handshake in memory 0.81 vs 1.51 ms; server handshake bytes 2.3 KB (classical chain) vs 15.8 KB (ML-DSA-65 chain); CRL +3.2 KB constant |
| #1130 | bcrypt cost 10 at 72.1 ms per hash on one core; ML-DSA-44/65/87 DPoP proofs built with `go-jose` v4.1.8 in a scratch module: 5,929, 8,252 and 11,147 bytes. Single runs |

Not measured by anyone: amd64, Linux ARM Pi-class devices, bounded-concurrency
and memory under the target workload, TLS resumption, any production-sized
release, any real network path. The hosted `ops-floor` jobs are not PQ benchmarks.

## Capability statement, surface by surface

Never read this as "PQ-ready". Every row below is opt-in (DEC-05), but default
behavior is not entirely unchanged. Without any opt-in, the tree that lands
also does these: origin lookup in the CLI trust store (`lookupByOrigin`,
`internal/cli/compose.go`) matches equivalent origin spellings, so a store
holding `https://h` and `https://h:443` at different pins or policies is now
refused as ambiguous until `--instance` names one; deleting developer
credentials for an origin (`deleteDeveloperCredentials`,
`internal/cli/developer_state.go`) also removes those stored under equivalent
spellings; `RunAutomaticUpgrade` (`internal/app/automatic_upgrade.go`) runs a
new custody-versus-policy pre-check on each fresh `sudo hikyo upgrade`, before the journal, and
the unattended path runs one when the run would export; and runtimeconfig migration 6 moves the
rollback floor for every upgrading instance (C-10). Migration 00083 does the
same on the candidate only, since #1172 did not merge. Each row separates five things: **confidentiality**
(a recording adversary), **authenticity** (a future forger), **software
capability** (the code can do it, locally), **enforced policy** (something
refuses the weaker path) and **observed deployment** (a real hop or host was
seen doing it). Observed deployment is NOTRUN on every row.

| Surface | Confidentiality | Authenticity | Software capability | Enforced policy | Remaining exceptions and HOLD |
| --- | --- | --- | --- | --- | --- |
| Backups and upgrade exports | Hybrid age recipients (X25519 + ML-KEM-768) protect new archives **only when hybrid recipients are configured; the default stays X25519 and `keygen` mints X25519 without `--hybrid`; automatic and unattended upgrade exports stay X25519 (a hybrid policy makes them refuse)**. Payload and metadata stay at the 16-byte age file key (category 1). The custody vault is age-scrypt and holds root escrow: no PQ claim | Classical (P-256 attestation; receipts unsigned) | Hybrid keygen, export and receipt v2 (child run; L2 `backupreceipt` for the receipt only); drill and restore on both engines (L1) | None. No selector; a classical-only set is still accepted; mixed and over-bound sets are refused | E-09, E-10, E-16, E-19 HOLD, E-17; vault re-seal #1154 |
| Native TLS listener and owned clients | Handshake: TLS 1.3 with exactly `X25519MLKEM768`, tickets off, when `strict` is selected | Classical certificate chain or SPKI pin; none PQ | Real `Boot` with strict (child) | Per process, opt-in, via managed configuration; no fleet claim without a stale-node fence | E-08, E-18a, E-20, E-21, E-22 (fence not built, accepted limit). **Strict TLS reaches no external hop**: no adapter, federation, mail, SAML, database, update or pairing transport reads a selector, and no deployed Kubernetes operator can select strict (a human operator selects it for the listener through managed configuration) |
| Sealed webhook protocol 2 | Hybrid age recipient for the payload; the TLS hop is TLS 1.2 or newer with a classical certificate and strict does not reach it | Opt-in per target (the default is protocol 1). Ed25519 and ML-DSA-65, both required, on envelope and acknowledgement. Holds only after v1 is retired on the receiver | Reference receiver and sender, both engines (L1) | None that survives an administrator restoring the v1 entry (R-1128-B) | R-1128-A, R-1128-B, recipient ownership unproven by the probe, E-17 (third-party receivers), reference tool not production software, X19 |
| Transit | No change | At the candidate, ML-DSA-65 signing exists **below the API only**, and the real server, CLI, OpenAPI, clients and WebUI refuse or do not list `mldsa65`. On the tree that lands there is no Transit mechanism until #1172: the freeze amendment (option A) adds the value to the contract and the service refuses it | Service, store, custody on both engines (L1) | Public-surface refusal (L1) | #1172 held: not in what merges. Migration 83 floor; restore, multi-node NOTRUN |
| Private PKI | The Go TLS test pairs the strict group with an ML-DSA chain in a harness; no product hop does | ML-DSA-65 issuer, leaf and CRL exist **below the API only** and are refused through the real server and CLI. Relying parties tested: Go 1.27.3 and OpenSSL 4.0.3 only | Service level on both engines (L1) | `MLDSAEnabled` false in production wiring; install refuses mixed paths | G1 to G7; five unconfirmed choices (deferred); E-18a; browsers and platform verifiers unqualified |
| Rollout authority | None | Opt-in per enrollment (without the `authority` object the controller keeps one Ed25519 key and v1 bytes). Dual Ed25519 + ML-DSA-65 or ML-DSA-65 only, enrolled out of band by the operator; the anchor travels over classical cluster access (E-14) | Controller and signer on a fake API | Only under a strict enrollment: the controller refuses stripped, stale or wrong-key commands with no change to the target (fake API); a refusing start still writes the custody Lease and start-up state | G-1 to G-4; kind and real cluster NOTRUN; upgrade-source commands NOTRUN |
| Release and update | None claimed | **No claim.** Verifiers exist in `internal/releasetrust`; no shipping path calls them, there is no signer, root or PQ floor on a real host | Verifier fixtures only | None | E-11, E-15, E-17, E-18b, R-01 to R-17. Authenticity of every other surface is conditional on these |
| Pairing | None | Classical SRP-2048 and Ed25519 DPoP; no PQ claim | None | None | E-01 HOLD; pairing and revocation drill NOTRUN; #1130 remains the residual gap |
| External routes | Go's default hybrid group may be offered; whether it is negotiated is the peer's choice and was never observed | Peer-controlled classical signatures | None | None | E-02 to E-07, E-13 registered; of these only E-06 is effective, since 2026-11-04, and none at the candidate; F-09 narrowed (no strict claim on external routes). No route is verified supported |
| Symmetric hierarchy, tokens, passwords | Retained; category-1 floor objects named; AEAD usage cap not re-derived | Out of scope | Unchanged | Unchanged | Usage cap NOTRUN |

Unreachable today, stated plainly: PKI has ML-DSA below the API only, and Transit
has it only at the candidate, because #1172 did not merge; strict TLS reaches no external hop; release verifiers are called by no
shipping path; hybrid vault generation, a backup strict profile and a PQ
webhook key generator in the product binary do not exist.

## Release and blocked

Lists only; the coordinator presents options and recommendations.

**What could be released if the owner and maintainer chose to, each only through
its own PR, review and security approval, none of it activating anything by
default (DEC-05):** hybrid backup recipients (#1126); opt-in strict TLS listener
and CLI bundle field (#1127); webhook protocol 2 sender and registry fields
(#1128); the rollout strict enrollment profile (#1131); the unreachable
mechanisms and verifiers (#1133, #1132; #1129 is not merged and not releasable); register and feasibility
documents (#1134, #1130). Each carries the floors and refusals in Rollback.

**What stays blocked:** parent closure (#1123); every `pqc-auth` claim (E-11,
E-15); Transit and PKI public surface; the strict backup profile (E-19);
fleet-wide strict TLS; pairing; release signing, bridge, roots and artifact
verification on every path; any budget statement.

**Decided on 2026-11-04 (see Merge status):** `backup-receipt/v2` kept as
built; revision routes for non-ADR specs by the declared-amendment banner;
stale-node fencing accepted as a recorded limit (E-22); the C-10 rollback
policy accepted for the next nightly.

**Pending owner decisions:** the #1133 fingerprint and unconditional-leaf
departures and the other #1133 choices (deferred by the owner until ML-DSA is
enabled for PKI); C-01 key-id construction; DEC-17 thresholds and the target machines
(deferred until an amd64 baseline exists); release key versus recovery key (#1132's key roles, sidecar floor, banner wording, pin delivery and recovery custody are deferred until a signer exists, as is DEC-14 custody); nightly PQ signer;
peers, versions and
expiry for E-02 to E-05, E-07 and E-13; whether
to split E-18; owners for the #1133 G-items; C-12 to C-14.

**Human and environment actions remaining (all NOTRUN today):** a named
disposable environment and resource-specific authorization; the pairing and revocation drill of the classical flow on SQLite and PostgreSQL (I20); amd64 and arm64
measurement hosts; a kind cluster with a strict enrollment; a real non-production
receiver; peer captures for external routes (F-02); an old-binary run against
migration 83 (candidate only) and migration 6 data; a process-restart and archive-restore run with
an ML-DSA issuer and key; maintainer security approval for enablement, release and deployment (the 2026-11-04 merge authorization covers merging only); independent
Standards, Spec and adversarial review of each final head and of the integrated
candidate; the merge-group CI graph on a `dev`-based PR; pinned Wycheproof
vectors and a longer fuzz campaign; a second independent age implementation.

## What this packet cannot establish

It cannot show interoperability, runtime enforcement, budgets, recovery on a real
host, fleet behavior, release authenticity or any external provider's behavior.
It repeats the children's local results without rerunning them except L1 to L3.
Its summary of the children is only as accurate as their handoffs, which it
read but did not re-derive; their last repairs are unreviewed. If the
coordinator moves the candidate, none of this transfers without a new pin.
