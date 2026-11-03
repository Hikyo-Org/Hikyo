# #1131: rollout commands and controller trust under ML-DSA-65, implementation handoff

Written 2026-11-03 for the draft PR that refs
[#1131](https://github.com/Hikyo-Org/Hikyo/issues/1131). It records what this
round implements, what it proves and what it leaves. It is evidence for one
candidate, not a live status board. The governing decisions are the
[post-quantum register](../adr/post-quantum-migration.md#decision-register)
(DEC-02, 03, 05, 07, 08, 09, 10, 17, 20), the
[profiles](../security/pqc/profiles.md#signature-composition-and-stripping-resistance-dec-03)
and [migration](../security/pqc/migration.md#rollout-authority-i23-and-pki-root-i24)
text. **security approval: not given.** This changes authentication of commands
that alter a running deployment and needs maintainer security approval and an
independent adversarial review before merge. Cross-provider review is SKIPPED by
explicit owner instruction (2026-11-02) and is not CLEAN. Operator procedure:
[config-rollout-authority](../operations/config-rollout-authority.md).

Stacked on PR #1155 (base `e5539c280b37ea3f6e3a8371a949aad8190ff0ad`, branch
`dunky13/pqc-adr-amendments`). Code candidate: commit `cef0c55876c2457a0b2c8663a20b4bfe68fe0c40`. The handoff
commit that follows it changes only this file.

## Result

Unchanged by default. An enrollment without the new `authority` object keeps one
Ed25519 key and the v1 command bytes. A test pins the signed command's SHA-256
to a value produced by the code at the base commit, so "byte-compatible" is
measured, not asserted. The chart renders byte-identically for default values
(compared against the base chart with the same values).

Opt-in strict profile, installed only by you, out of band (DEC-20 option c):

- The enrollment gains `authority {version, generation, required, key_ids}`.
  `required` is exactly `[ed25519, mldsa65]` (dual window) or `[mldsa65]`
  (PQ-only). `key_ids` pin the public keys.
- The controller derives the required set from that record, never from the
  command. Every required algorithm must carry exactly one valid signature, in
  order, with the pinned key id and exact length. The server signs with every
  required key; it refuses signers that are not the enrolled keys, and a
  compatibility enrollment refuses an ML-DSA signer.
- All signatures cover identical bytes: `LP("hikyo/config-rollout/command/v2") ||
  LP(command JSON)`. The label is also the ML-DSA context. The command JSON
  carries `authority {generation, required, key_ids, target_digest}`, so the
  suite, the key ids, the trust generation and the enrolled target are inside
  the signed bytes. A v1 domain signature, another label, another suite, a
  stripped or extra signature or a command from another installation fails.
- Key ids are `<alg>-sha256:` plus SHA-256 over `LP(label) || LP(alg) ||
  LP(raw public key)` (DEC-07). `hikyo config-rollout --authority-public-key F
  --print-authority-ids` prints them and contacts no cluster.
- The controller persists a generation floor in the journal Secret under
  `trust.json`, in the same Secret update as the accepted command. At start it
  refuses a stale enrollment (lower generation, rollback to the compatibility
  profile, other keys at the same generation), refuses a journal entry the
  installed keys cannot anchor, raises the floor, and only then reads commands.
  The same floor check runs on every reconcile tick.

## Inventory (first acceptance item)

Re-pinned at base `e5539c280`; each row names how it changed.

| Item | Where | Change |
| --- | --- | --- |
| Producer | `bootstrapDeployment.sign` via `PrepareCommand`, `DecisionCommand`, `RenewCommand` (`internal/app/self_config_deployment.go`); callers in `internal/service/self_config_deployment*.go` (prepare, submit, observe, restore, renew, root, topology repair) | `sign` uses `SignCommandFor`; `validSigned` uses `VerifySignedCommandKeys` |
| Signer custody | `--config-rollout-signing-key`, `readDeploymentSigners`; chart Secret `rollout.authorityExistingSecret` and `authorityKey`, init container `rollout-authority-stage` (`__hikyo-stage-rollout-authority`, `stageRolloutAuthority`), memory volume `/run/hikyo/rollout/authority/authority.key` mode 0400 | File may hold one Ed25519 block or Ed25519 plus a seed-only ML-DSA-65 block; `ParseSigners` closes the set; stage validates it before copying; no new flag, Secret key or mount |
| Enrollment and bootstrap records | `Enrollment` (`transport.go`), chart `hikyo.rollout.enrollment` into the immutable ConfigMap `<name>-enrollment` (`enrollment.json`, `authority.pub`); `ParseEnrollment` callers: `cmd/hikyo/config_rollout.go`, `configureBootstrapDeployment`, `internal/app/next_root_source.go`; `SecretSource`, `SourceProof`, `BootstrapChanges` (`bootstrap.go`) | Optional `Authority`; `validEnrollment` closes it; `cloneEnrollment` copies it; source aliases and proofs untouched |
| Controller verifier | `Controller.reconcile`, `verifyCommand`, journal re-verification at the former `controller.go:60`, `Run` and custody (`controller.go`, `custody.go`) | Key set `PublicKeys`, `NewControllerKeys`, `anchored`, `reconcileTrust`, floor |
| Command and receipt serializers | `Command`, `SignedCommand`, `Response`, `Receipt`, `controllerJournal`, `record`; `Mailbox.Send`, `Response`; service `encodeRolloutCommand` and `decodeRolloutCommand` (canonical round trip required); `self_config_rollouts.command_json TEXT NOT NULL`, no CHECK, on both engines; `rolloutTopology` decodes leniently | `Command.Authority`, `SignedCommand.Signatures` (both `omitempty`, so v1 bytes do not move); `Mailbox.Send` checks the envelope by shape instead of `ed25519.SignatureSize` |
| Upgrade, cancellation, recovery consumers | `cancel_prepared.go`, `Kubernetes.Submit/Observe/Restore`, `upgrade.go` (`UpgradeCustodySource`, upgrade `SourceProof`), service restore, renew, root and topology repair, `internal/app/upgrade_custody_source.go` | Not edited. Exercised under the strict profile: submit, restore and cancel paths run through the same controller, and renew and restore run through the service on both engines |
| CLI, chart, docs | `cmd/hikyo/config_rollout.go` flag text and `--print-authority-ids`, `cmd/hikyo/main.go` help, `chart/hikyo` values, schema and `_rollout.tpl`, `scripts/ci/check-config-rollout-chart.py`, site `configuration-rollouts.mdx` | Optional `rollout.authority`; chart check extended; `check-config-rollout-kind.py` unchanged (see NOTRUN) |
| Locked ADRs | `tenant-isolation.md` describes the rollout doors and a "signed journal", `machine-identities.md` mentions rollout only in passing | Neither pins an algorithm; no amendment is proposed for #1131 by the register, so none is made |

## Acceptance checklist

| Criterion | Status | Notes |
| --- | --- | --- |
| Inventory before fixing one signature call | DONE | Table above |
| Versioned signing and key identifiers, canonical domain-separated encoding binding enrollment, instance, target, generation, resource versions, action, expiry and replay context, expected authority | DONE | Enrollment id, owner and incarnation (Intent), target digest, Intent generations and trust generation, resource versions through the plan digest, action, sequence, issue and expiry, required set and key ids are all inside the signed bytes. The encoding follows DEC-07 and the profiles rule; the choice of label and framing is this PR's and needs the maintainer's security approval |
| Bootstrap PQ trust through the existing authenticated authority and approved transition policy; reject self-announced keys, rollback, substitution, stripped signatures | DONE for DEC-20 (c), which is a **departure from #1131's wording, accepted by the owner** | There is no in-band transition, so a command cannot announce a key. Rollback, substitution and stripping are refused and tested. The anchor travels over classical cluster access (E-14) |
| Signer import and storage, fingerprints, enrollment and configuration validation, bounds, controller verification, compatibility checks for actual sizes, maintained implementation | DONE | Go 1.27.3 `crypto/mldsa`, `crypto/x509`; ML-DSA-44, ML-DSA-87, expanded private keys, duplicates and extras refused; sizes asserted: public key 1952, signature 3309, strict dual command about 5.7 KB against the unchanged 512 KiB cap; chart and CLI validation |
| Sign, deliver, verify, fence, execute, durable result and reconcile on the actual controller path; invalid signatures or trust metadata cause no Kubernetes mutation | DONE on an instrumented fake API; real cluster NOTRUN | Every negative case asserts that the controller made no create, update, patch or delete call, and structural cases assert the mailbox wrote nothing |
| Preserve namespace and topology restrictions, CAS, least privilege, idempotency, cancellation, durable INTENT and OUTCOME audit, redaction | DONE | No existing behavior or test changed. Audit lives in the service layer and is untouched. Refusals are closed classes (`ErrInvalid`, `ErrConflict`, `ErrTrust`); no key or signature material in errors or the CLI messages |
| Old and new controller and signer combinations, accepted compatibility mode, strict refusal, replay and expiry, wrong enrollment, target, action or generation, tampered payload, concurrent rollout, key rotation | DONE | See the test table |
| Restart, takeover, interrupted trust rollover, restore of stale enrollment and state, post-restore reconciliation, rollback floor without resurrecting authority | DONE for what the floor can see; PARTIAL overall | Floor, anchor and takeover are tested. The floor lives only in the journal Secret, so a restore of that Secret restores an older floor, and clearing it is break-glass (E-14, E-17) |
| Fake and local fixtures for ordinary checks; how to rehearse and measure overhead | DONE | Runbook gives the benchmark command; numbers below. A kind rehearsal is NOTRUN |
| Operator runbook and exact candidate evidence; independent review of public signing key migration from payload encryption | PARTIAL | Runbook written and its key-generation commands validated by test. Independent review not yet done (HOLD). This PR touches no payload encryption |

## Handling E-14: new key or id against the existing journal

The register said a new key or id fails the journal check and that emptying the
journal loses the plan Restore needs. The design states each case and invents no
re-anchoring mechanism.

| Change at re-enrollment | Result | Test |
| --- | --- | --- |
| Compatibility to dual, Ed25519 key kept | The journal's v1 entry anchors through the Ed25519 key, which is required in the dual window. The plan survives and Restore works under the strict profile | `TestTrustRolloverFromCompatibilityRetainsPlanForRestore` |
| Dual to PQ-only | The retained v2 entry anchors through its ML-DSA signature | covered by the anchor rule; runbook |
| Ed25519 key rotated, ML-DSA key kept | Anchors through the ML-DSA signature; plan survives | `TestKeyRotationAndJournalResetBehaviorIsStated` |
| ML-DSA key replaced, or enrollment id changed, with a retained entry | **Cannot be anchored.** The controller exits at start with `authority trust state refused`, and per-command reconcile returns `ErrTrust`, both with no write. Registered gap G-1 | same test |
| Only `journal.json` removed | Plan and accepted sequence are gone, so Restore is refused; the floor in `trust.json` remains, so a stale enrollment is still refused | same test |
| Whole journal Secret emptied | The floor is gone too: a stale enrollment would be accepted again. Registered residual, stated in the runbook as break-glass | same test |
| A command signed before the switch | Refused by the new controller; the server cannot renew it (no authority is resurrected). A fresh decision is needed | `TestInterruptedRolloverRefusesPreRolloverCommandsWithoutMutation`, `TestBootstrapDeploymentStrictProfileSignsEveryDecisionAndRefusesOldRegime` |

Replay after a journal reset: a command signed under an older regime fails the
signature and the key-id and generation binding, so an emptied accepted
sequence does not reopen it. A command signed under the current regime cannot
predate the re-enrollment. The remaining replay window is a current-regime
command still inside its 10 minute cap after a journal reset, which needs
journal write (E-14).

## Tests and results

Run from the repository root with a throwaway PostgreSQL 18 container
(`HIKYO_TEST_POSTGRES_DSN`), removed afterwards.

| Command | Result |
| --- | --- |
| `go test -p 2 -count=1 ./internal/configrollout/` | PASS: existing suite plus the strict matrix |
| `go test -p 2 -count=1 ./cmd/hikyo/` | PASS |
| `go test -p 2 -count=1 -run BootstrapDeployment ./internal/app/` | PASS |
| `go test -p 2 -count=1 -run 'TestCommittedDeploymentCarriesStrictEnvelope\|TestCommittedDeploymentRenews\|TestDeploymentRenewalRejects' ./internal/service/` | PASS on SQLite and PostgreSQL, including the strict envelope persisted, returned canonically and renewed |
| `python3 scripts/ci/check-config-rollout-chart.py` | PASS: default render has no `authority`; strict render changes only that object; seven broken `authority` values are refused |
| Default chart render compared with the base chart | Byte-identical |
| Mutation checks (skip ML-DSA verify; drop the floor check; take the required set from the command) | Each made named tests fail, then was reverted |
| `go test -p 2 -count=1 -run 'SelfConfig\|Deployment\|Rollout\|NextRoot\|Candidate' ./internal/app/` | PASS |
| `go test -p 2 -count=1 -run 'Deployment\|Rollout\|SelfConfig' ./internal/service/` with PostgreSQL | PASS (218 s), both engines |
| `FAST_FEEDBACK_BASE=origin/dunky13/pqc-adr-amendments scripts/ci/fast-feedback.sh --working-tree`, pinned Node 26.12.1, through the shared queue | PASS (exit 0): diff check, Go format, test compile, docs content check. Hosted CI NOT RUN |
| `scripts/ci/check-commit-signatures.sh` and `check-dco.sh` against the PR base | PASS at the first code commit; re-run on the final head before push (see PR) |
| `go test -p 2 ./internal/lint/...` | NOT COMPLETE: two runs hit the Go test timeout (10 min direct, 40 min queued) on unrelated slow tests while the host load average was about 14 to 19; a third queued `-v` run sat on the unrelated `TestAlertCollectorSnapshotProtocolRejectsDrift` for over 30 minutes and was cancelled. No assertion failure was observed. Treat as NOTRUN to completion |
| `scripts/ci/check-doc-status.mjs` | NOTRUN: needs `docs/site` dependencies (`zod`), not installed in this worktree |

Matrix (`internal/configrollout/authority_test.go`), run for dual and PQ-only
through `SignCommandFor`, `Mailbox.Send` and `Controller.reconcile`, asserting
zero mutations:

- Sign, deliver, verify, execute, restart (a second controller process),
  durable journal and floor, restore: `TestStrictProfileSignDeliverVerifyExecuteAndRestart`,
  `TestStrictRolloutOverTheNewestExecutorPathIsRestorable`.
- Refused with zero mutation: v1 signature only, stripped ML-DSA, stripped
  Ed25519, wrong algorithm set, duplicate, reordered, short ML-DSA signature,
  tampered ML-DSA, tampered Ed25519, tampered payload, wrong key id, other key
  pair, wrong generation, wrong target digest, missing binding, wrong
  enrollment, wrong instance, wrong action, sequence zero, expired, future,
  legacy field beside strict, unknown algorithm: `TestStrictProfileRefusesWithZeroMutation`.
- Old and new combinations and compatibility mode: `TestOldAndNewControllerAndSignerCombinations`,
  `TestCompatibilityProfileKeepsV1BytesAndSignerPath`.
- Suite binding, context separation, v1 domain cannot be replayed as v2:
  `TestSignaturesAreSuiteBoundAcrossProfiles`.
- Replay, expiry, sequence: `TestReplayExpiryAndSequenceUnderStrictProfile`.
- Concurrent rollout and takeover controller: `TestConcurrentRolloutCannotDisplaceAcceptedOperation`.
- Floor, stale enrollment, rollback to compatibility, malformed floor, higher
  generation: `TestStartupFloorRaisesAndRefusesStaleEnrollment`,
  `TestCompatibilityStartupWritesNothingAndRefusesRollbackBelowFloor`.
- Takeover with the custody epoch on the floor write: `TestControllerTakeoverRunsTrustReconciliationBeforeCommands`.
- Closed sets and parsers: `TestEnrollmentAuthorityIsClosedAndParsedStrictly`,
  `TestKeyFilesAreClosedSets`, `TestExpandedMLDSAPrivateKeyFormsAreRefused`,
  `TestCheckKeysPinsExactlyTheEnrolledKeys`, `TestKeyIDsAreAlgorithmBearingAndDomainSeparated`.
- Runbook key generation with the installed OpenSSL: `TestRunbookOpenSSLKeyFilesParse`
  (skips when OpenSSL lacks ML-DSA seed-only).

## Compatibility and negative evidence

- Old controller, new signer: a decoder without the new fields rejects the strict
  command and the strict enrollment as unknown fields. The test models those
  decoders with local copies of the previous types, not with an old binary
  (NOTRUN against an old build). That refusal is the intended failure.
- New controller, old signer: a v1 command is refused under the strict profile.
- Rolling the controller image back after a strict command was accepted fails
  closed (the old decoder refuses the journal entry). It is not a supported
  rollback.
- A compatibility controller still completes prepare, submit and the response
  path unchanged.

## Overhead (local, no budget)

Apple M2 Pro, arm64 darwin, Go 1.27.3,
`go test -run '^$' -bench BenchmarkSignAndVerify -benchmem -benchtime 200x`:
Ed25519 sign 41 us and verify 74 us; dual sign 0.91 ms and verify 0.24 ms;
PQ-only sign 0.54 ms and verify 0.20 ms. Signing allocates about 15 KB (dual).
Command size: about 5.7 KB (dual) against a 512 KiB cap. DEC-17 thresholds are
the owner's; none are invented here. amd64 is NOTRUN.

## Key retention and rollback

- Keep the Ed25519 private key until the PQ-only generation is confirmed, then
  destroy it. Keep the ML-DSA private key under the same custody as the Ed25519
  one (the same Secret, separate Hikyo root key).
- Retired classical signatures remain forgeable after a quantum break (E-12).
- The floor blocks rollback. The only way back is clearing `trust.json`, an
  owner-recorded break-glass act that resurrects whatever generation you reinstall.

## Registered gaps, HOLD and NOTRUN

| ID | Item | Status |
| --- | --- | --- |
| G-1 | ML-DSA key or enrollment id replaced with a retained journal entry: no in-band re-anchor; operator clears `journal.json` and loses Restore of that plan. Not built because the register decided no mechanism | Gap; owner #1131; propose an owner decision if a smoother path is wanted |
| E-14 | Anchor over classical cluster access; journal write is floor write and replay write | Existing exception; this PR changes none of it |
| E-11, E-15 | Release and image path; no PQ claim covers how the controller binary arrived | Existing, #1132 |
| | Enrollment id change on the server side (rollout rows keyed by id, per-enrollment sequence) not traced | HOLD, unsupported in the runbook |
| | Real cluster, kind rehearsal with a strict enrollment, immutable ConfigMap recreation, Pod restarts, admission policy on the floor write | NOTRUN. `scripts/ci/check-config-rollout-kind.py` still generates only an Ed25519 authority |
| | Old binary against new data | NOTRUN (modeled) |
| | DEC-17 budgets, amd64 measurements | NOTRUN |
| | Hosted CI | NOTRUN |
| | Independent adversarial review, maintainer security approval | HOLD, pending |

## Proposed revision route for the self-configuration specs

DEC-18 does not state one. Proposal: the owner accepts the revision of
`docs/spec/self-configuration-full-catalogue.md` (signed-command paragraph) in
the implementation PR that merges, recorded by the owner's decision on #1131,
with no reopened ADR because no locked ADR pins the algorithm. This PR adds only
a clearly marked **proposed, not operative** banner to that spec and one
sentence to the site page. Nothing else in the specs changes.

## Next authorized step

Independent adversarial review (Standards, Spec and security) and the
maintainer's security approval. Then, on a disposable kind cluster: extend the
kind rehearsal to enroll the dual profile, restart both consumers, send a
strict command, a v1 command and a stale-generation enrollment, and record the
results. Production enrollment, key rotation and any release remain separately
authorized.
