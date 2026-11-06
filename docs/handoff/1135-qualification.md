# #1135: integrated PQ qualification packet, handoff

Written 2026-11-04 for the PR that refs
[#1135](https://github.com/Hikyo-Org/Hikyo/issues/1135). **Result: HOLD.** The
packet exists; the qualification it describes does not. The PR does not close
#1135 or #1123, which stays open. Security approval: the owner's 2026-11-04
merge authorization applies to each PR that merges under it, #1172 (Transit,
migration 00083) excepted for this round; nothing wider. Independent
adversarial review is required; cross-provider review is SKIPPED by explicit owner instruction
(2026-11-02) and is not CLEAN. These documents had independent reviews on 2026-11-04, recorded below, with their findings repaired.

Deliverables: [packet](../security/pqc/qualification-packet.md) (requirement and
exception maps, per-child table, ladder, rollback, budgets, contradictions,
capability statement, release and blocked) and the
[runbook](../operations/pqc-staged-rollout.md) (not executable). No code, test,
schema or configuration changed.

## Exact candidate

PR #1195, branch `dunky13/pqc-1135-candidate`, commit
`68b2673dea9eb7053edd77eca2e585146bb5ad2c`, tree
`db58307e5a8aa54d684c52fed9148b9649dc9683`; Go 1.27.3 darwin/arm64; PostgreSQL
18.6 from `postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941`.
The candidate was unchanged when fetched and when last read. If it moves,
nothing here transfers. It contains #1172, which did not merge with this round, so it is not evidence
for the tree that lands; that tree has hosted CI only.

## Integrated run (the only test run, once, through the shared queue)

| Item | Result |
| --- | --- |
| Isolation, SQLite and PostgreSQL 18.6: `TestAdapterSealedWebhookProtocolTwoEndToEnd`, `TestPKIMLDSALifecycle`, `TestPKICLISQLite`, `TestPKICLIPostgres`, `TestTransitMLDSA65EndToEnd`, `TestTransitPublicSurfaceHoldsMLDSA65{SQLite,Postgres}`, `TestBackupRestoreDrillHybrid{SQLite,Postgres}`, `TestRestoreDrillVerbHybrid{SQLite,Postgres}`; `-p 2 -parallel 2 -timeout 40m -count=1`, queue job 3261 | **exit 0**, `ok ... 149.003s`; 55 PASS lines (11 top-level), 0 SKIP, 0 FAIL. The webhook, PKI lifecycle and Transit end-to-end entries each ran `sqlite` and `postgres` subtests; the CLI and hold tests are one engine per function |
| Focused: `./internal/configrollout/... ./internal/releasetrust/... ./internal/crypto/... ./internal/tlspolicy/... ./internal/backupreceipt/...`, same job | **exit 0**; all packages `ok` |
| `go run ./scripts/release/compatibility --development --check internal/buildcompat/development.json` against an empty scratch database on the same PostgreSQL, queue job 3263 | **exit 0**, no output |
| Hosted `gh` read on #1195 at 2026-11-04T02:04Z | `repository invariants (boundary, lint, CI policy)` success with `Repository invariant tests` and `Lint driver tests` success; `feedback` (go, tooling, frontend) and `ops-floor` (amd64, arm64) success; `ci-queue-required` fails by design ("Only the protected default branch can enter this merge queue."). No integration finding in `reviewed_pins.json` |
| Integration facts | Migrations: only `00083` (both engines). `development.json` matches. Doc links resolve. No PQ verifier has a non-test caller outside `internal/releasetrust`. Contradictions C-01 to C-14 are in the packet |

Not run, so not claimed: `internal/lint` locally (instructed), the full app,
service, server, store, pairing, selfupdate, sshca, pki and cli suites, fuzz,
benchmarks, `fast-feedback.sh` on the candidate, anything on amd64.

## Ticket acceptance, in the issue's order

| # | Criterion | Status | Why |
| --- | --- | --- | --- |
| 1 | Map every requirement and exception to candidate, versions, digests, topology, configuration, peers, evidence | PARTIAL | Maps done by evidence class. Artifact digests, topology, effective configuration and peer versions do not exist: nothing was built for release or deployed |
| 2 | Legacy, compatible reader, new writes, strict enforcement with the actual consumers | PARTIAL | Ladder in the packet. Strict is NOT BUILT for backups, unreachable for Transit, PKI and release, HOLD for pairing; TLS strict per process only; the rest is local or fake-API |
| 3 | Integrated negative tests (stripping, fallback, unknown version, malformed, wrong scope, stale trust, replay, exhaustion) with vectors and fuzz | PARTIAL | Children have per-surface negatives; the union ran only the listed isolation and focused tests. Wycheproof NOTRUN, no long fuzz, no integrated cross-surface negative suite exists |
| 4 | Backup/restore and custody drills, trust rollover, issuer lifecycle, pairing/revocation, webhook reconciliation, controller failover on both engines | PARTIAL | Backup drills, webhook takeover simulation, PKI/Transit lifecycle on both engines at the candidate. The manual-route upgrade-export and upgrade-drill with a hybrid identity passed on both engines in #1126's own run (not rerun here); no hybrid vault (automatic custody) drill exists, because `sudo hikyo upgrade` and the unattended upgrade refuse a hybrid policy. Trust-root rollover, pairing, real failover: NOTRUN or HOLD |
| 5 | Rollback at every migration phase incl. rejection of a binary below the floor; interrupted writes; restored stale state; retirement | PARTIAL | Rollback table in the packet. A pre-83 binary against an 83 database and a pre-migration-6 binary: NOTRUN. No retirement drill. Webhook has no floor |
| 6 | Real TLS groups and certificate authentication across ingress, backend and client graph | NOTRUN | Only in-process handshakes. No real deployment observed |
| 7 | amd64 and arm64 sizes, latency, CPU, memory, concurrency against approved budgets | HOLD | Recorded child numbers (M2 Pro arm64 only, loaded host). DEC-17 thresholds unset; amd64 NOTRUN; no measurement taken here |
| 8 | Final local checks, hosted CI, independent Standards and Spec review, security approval, gates | HOLD | Partial local and hosted evidence above; merge-group graph never ran; no review of the union; security approval only as the 2026-11-04 merge authorization, none for #1172 |
| 9 | Operator runbook with prerequisites, commands, secret loading, escrow, cohorts, abort, success, rollback, recovery; validated against the binary | PARTIAL | Written and marked not executable. Commands validated by `--help` of a candidate-built binary; two runs with throwaway input (`--print-authority-ids`, OpenSSL key generation) and one `backup keygen` run that established its datastore requirement. Every real-environment step NOTRUN |
| 10 | Surface-specific capability statement; exceptions, owners, triggers, HOLDs | DONE (as a statement) | Packet. E-01, E-06 and E-22 carry all four fields; no other row does (peers, versions and expiry are the owner's) |
| 11 | Parent closure requires all mandatory work and real evidence, or approved scope reduction | HOLD | Not met. The pairing narrowing is accepted (2026-11-04) with #1130 as the residual gap; the mandatory work and real evidence are not complete |
| 12 | Record what can be released and what is blocked, plus remaining human and environment actions | DONE | Packet, "Release and blocked", lists only, no recommendation |

## Findings for the owning children (no code changed)

| Finding | Child | Note |
| --- | --- | --- |
| Hybrid scanner claim in `migration.md` is too narrow (C-03) | #1126 | Neither age identity form is matched by the compiled scanner |
| DEC-07 id spellings and hash inputs differ across surfaces; #1129 and #1133 depart (C-01) | #1128, #1129, #1131, #1132, #1133 | Owner decision |
| No defect was found in the integrated run. A defect would be reported against its child, not patched here | | |

## Checks on this PR's documents

The first four rows ran once, on the branch as it stood after its first three
commits (the packet, the runbook and this handoff), before the review repair.
They were not rerun on the repair or on the later repair rounds.

| Check | Result |
| --- | --- |
| `scripts/ci/check-commit-signatures.sh` and `scripts/ci/check-dco.sh` against `origin/dunky13/pqc-1135-candidate` | exit 0 and exit 0: 3 commits verified, 3 signed off |
| `node scripts/ci/check-doc-status.mjs --check` (dependencies borrowed from another checkout through a temporary link, removed) | exit 0, 35 ledger entries verified |
| `scripts/ci/fast-feedback.sh` with `FAST_FEEDBACK_BASE=origin/dunky13/pqc-1135-candidate`, queue job 3270 | exit 0: diff check only, no lane selected for docs |
| New files searched for the em dash character and for machine-local paths | none found |
| Review repair rounds | Documents only. Checked again for the em dash character and machine-local paths in the files each round changed; the signature, DCO, doc-status and fast-feedback checks were not rerun locally. Hosted CI on the PR is the evidence for the head |
| Hosted CI on this PR | not read at the time of writing; see the PR |

## Environment and cleanup

A disposable PostgreSQL 18.6 container (`pqc1135-pg`, `--rm`, loopback port) and
a scratch database `hikyo_release_schema` were used. `DROP DATABASE` succeeded
and `docker rm -f pqc1135-pg` printed the container name; `docker ps` then
listed none. Docker Desktop later failed to start ("Docker Desktop is unable to
start"), so removal cannot be re-confirmed now. Scratch binaries, help
captures and logs stay in the session scratch directory and are not committed.
Throwaway keys used for `--print-authority-ids` were deleted. No production key
material, provider, cluster or receiver was touched.

## Review repair (independent review of `6336263`)

An independent review of the first version found 21 findings (3 high, 7 medium,
11 low). Each was checked against source or the cited handoff first; none was
wrong. Repairs, in one commit: the stage 1.1 abort is now "restore the
pre-upgrade backup" and the `migration.md` step 2 conflict is in C-10; opt-in
wording on every capability row; the #1129 round-3 review claim; manual-route
hybrid upgrade commands; the missing receipt on plain exports (E-19, #1126);
attribution of keygen and receipt v2; #1128 qualifiers; #1133 ownership by
default; three further contradictions (C-12 to C-14); the `backup keygen`
datastore requirement (checked on a candidate-built binary); and the low items.
The README gained a row for the packet. It was reviewed independently on 2026-11-04.

A second repair, after an independent review of the repaired head, corrects
the `backup keygen --help` claim, the old-binary refusal claims (NOTRUN), the
`escrow verify` description, the step 1.4 command and its surface runbook, the
stale handoff and packet text, the "default behavior is unchanged" overstatement
(the packet now lists the default-path changes), and the 2026-11-04 owner
decisions (merge status, #1172 held, evidence pin). It was reviewed independently on 2026-11-04.

## Retention and rollback of this change

Revert the four files: the three documents and the README index row. Nothing
else is created or changed.

## Next authorized step

Green hosted CI, then merge to `dev` as the owner authorized on 2026-11-04, then the owner's
decisions still open in the packet's "Release and blocked", then a separately
authorized environment. Issue closure, release and deployment are not
authorized.
