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
`dunky13/pqc-adr-amendments`). First review round head: `310eb9ffa`. Repaired
code candidate: commit `b04a641c2d1b3dc967e4ae762cbaba3dce16e7ea`. The handoff commit that follows it changes
only documentation.

## Review repair round (2026-11-03)

An independent review of `310eb9ffa` found no authority bypass and asked for
repairs. Each finding was checked first. All were confirmed by code or by a test;
none was refuted.

| Finding | Verdict | Disposition |
| --- | --- | --- |
| S1 Restore "keeps working" | Confirmed: `RestoreDeployment` signs through `DecisionCommand`, which requires `validSigned(prepared)` on the pre-switch row; Restore needs a `partial` job; `kubectl rollout restart` changes the Deployment spec and `Kubernetes.Restore` then returns `ErrConflict` | Claim removed. Gap G-3. Runbook no longer uses `rollout restart` or `helm upgrade`. Helm's effect on a live Deployment is recorded as unverified. Provider-level test added |
| S2 open job across a rollover | Confirmed at service level (test): the job becomes `partial` with `transport_failed`, the runtime is cleared, Restore is refused. The fatal boot half is from reading `app.go`, not run | Gap G-2 with residual risk and owner. Runbook reworded ("do not start a new Apply", no pause control exists) |
| S3 anchoring of a v1 entry | Confirmed by test: Ed25519 rotation over a v1 entry, and PQ-only straight from compatibility, return `ErrTrust` | Precondition "one strict command accepted" stated in all three places. Test from a compatibility journal |
| S4 G-1 too narrow | Confirmed by test: incarnation, owner and enrollment id changes fail `validCommandShape`; a removed alias is read from `validBootstrap` | G-1 widened. Database-restore and new-journal-Secret consequences added to the runbook |
| S5 default profile changed | Confirmed by a test that fails on the old code: a compatibility `Run` returned on a journal that does not verify, where the base kept running | Start-up anchor check now runs only under the strict profile, and a compatibility start ignores an unreadable journal as before. Only a persisted strict floor refuses it |
| S6 id change remedy | Confirmed by test: `Mailbox.Send` still returns `ErrConflict` at sequence 1 and 99 after `journal.json` is cleared | Id taken out of the remedy; G-4 holds the facts |
| S7 "refused" overstated | Confirmed: an accepted but unanswered v1 command resumes under the dual controller (digest equality with the journal) | Reworded to queued, not yet accepted; the resume is stated |
| S8 "no object written" | Confirmed: the custody Lease epoch is written before the trust check | Reworded |
| S9 profile id in signed bytes | Confirmed: only `version` and the required set were signed | `authority.profile = "pqc-auth/1"` replaces `version` in the enrollment and in the signed binding; matrix case and test added. The format is unshipped, so no compatibility cost. No DEC row edited |
| T1 probe skips `validSigned` | Confirmed | Claim corrected below; app test runs every provider operation on a pre-rollover row; service test runs the open-job consequence |
| T2 consumer coverage | Confirmed | Strict prepare then cancel-unseen-submit then Restore case added; upgrade-source commands under strict recorded NOTRUN |
| T3 interrupted-rollover test | Confirmed: it skipped the start-up floor write | Comment corrected |
| T4 matrix through `reconcile` only | Confirmed | A tampered-signature case runs through `Run` and asserts only the Lease, the admission dry runs and the floor are written |
| T5 zeroing claim | Confirmed: undecoded blocks were not cleared on an early error and the stage helper stopped clearing the parsed Ed25519 key | `pemBlocks` returns decoded blocks on refusal and `ParseSigners` clears them on every path; the stage helper clears the Ed25519 key again; the comment says what cannot be cleared |

## Result

Unchanged by default. An enrollment without the new `authority` object keeps one
Ed25519 key and the v1 command bytes. A test pins the signed command's SHA-256
to a value produced by the code at the base commit, so "byte-compatible" is
measured, not asserted. The chart renders byte-identically for default values
(compared against the base chart with the same values).

Opt-in strict profile, installed only by you, out of band (DEC-20 option c):

- The enrollment gains `authority {profile, generation, required, key_ids}` (`profile` is `pqc-auth/1`).
  `required` is exactly `[ed25519, mldsa65]` (dual window) or `[mldsa65]`
  (PQ-only). `key_ids` pin the public keys.
- The controller derives the required set from that record, never from the
  command. Every required algorithm must carry exactly one valid signature, in
  order, with the pinned key id and exact length. The server signs with every
  required key; it refuses signers that are not the enrolled keys, and a
  compatibility enrollment refuses an ML-DSA signer.
- All signatures cover identical bytes: `LP("hikyo/config-rollout/command/v2") ||
  LP(command JSON)`. The label is also the ML-DSA context. The command JSON
  carries `authority {profile, generation, required, key_ids, target_digest}`,
  so the profile id, the suite, the key ids, the trust generation and the
  enrolled target are inside the signed bytes (DEC-03). A v1 domain signature, another label, another suite, a
  stripped or extra signature or a command from another installation fails.
- Key ids are `<alg>-sha256:` plus SHA-256 over `LP(label) || LP(alg) ||
  LP(raw public key)` (DEC-07). `hikyo config-rollout --authority-public-key F
  --print-authority-ids` prints them and contacts no cluster.
- The controller persists a generation floor in the journal Secret under
  `trust.json`, in the same Secret update as the accepted command. At start it
  refuses a stale enrollment (lower generation, rollback to the compatibility
  profile, other keys at the same generation), refuses a journal entry the
  installed keys cannot anchor (strict profile only), raises the floor, and only
  then reads commands. The same floor check runs on every reconcile tick. The
  compatibility profile keeps its historical start: an unreadable or
  unverifiable journal never stops it, and only a persisted strict floor does.

## Inventory (first acceptance item)

Re-pinned at base `e5539c280`; each row names how it changed.

| Item | Where | Change |
| --- | --- | --- |
| Producer | `bootstrapDeployment.sign` via `PrepareCommand`, `DecisionCommand`, `RenewCommand` (`internal/app/self_config_deployment.go`); callers in `internal/service/self_config_deployment*.go` (prepare, submit, observe, restore, renew, root, topology repair) | `sign` uses `SignCommandFor`; `validSigned` uses `VerifySignedCommandKeys` |
| Signer custody | `--config-rollout-signing-key`, `readDeploymentSigners`; chart Secret `rollout.authorityExistingSecret` and `authorityKey`, init container `rollout-authority-stage` (`__hikyo-stage-rollout-authority`, `stageRolloutAuthority`), memory volume `/run/hikyo/rollout/authority/authority.key` mode 0400 | File may hold one Ed25519 block or Ed25519 plus a seed-only ML-DSA-65 block; `ParseSigners` closes the set; stage validates it before copying; no new flag, Secret key or mount |
| Enrollment and bootstrap records | `Enrollment` (`transport.go`), chart `hikyo.rollout.enrollment` into the immutable ConfigMap `<name>-enrollment` (`enrollment.json`, `authority.pub`); `ParseEnrollment` callers: `cmd/hikyo/config_rollout.go`, `configureBootstrapDeployment`, `internal/app/next_root_source.go`; `SecretSource`, `SourceProof`, `BootstrapChanges` (`bootstrap.go`) | Optional `Authority`; `validEnrollment` closes it; `cloneEnrollment` copies it; source aliases and proofs untouched |
| Controller verifier | `Controller.reconcile`, `verifyCommand`, journal re-verification at the former `controller.go:60`, `Run` and custody (`controller.go`, `custody.go`) | Key set `PublicKeys`, `NewControllerKeys`, `anchored`, `reconcileTrust`, floor |
| Command and receipt serializers | `Command`, `SignedCommand`, `Response`, `Receipt`, `controllerJournal`, `record`; `Mailbox.Send`, `Response`; service `encodeRolloutCommand` and `decodeRolloutCommand` (canonical round trip required); `self_config_rollouts.command_json TEXT NOT NULL`, no CHECK, on both engines; `rolloutTopology` decodes leniently | `Command.Authority`, `SignedCommand.Signatures` (both `omitempty`, so v1 bytes do not move); `Mailbox.Send` checks the envelope by shape instead of `ed25519.SignatureSize` |
| Upgrade, cancellation, recovery consumers | `cancel_prepared.go`, `Kubernetes.Submit/Observe/Restore`, `upgrade.go` (`UpgradeCustodySource`, upgrade `SourceProof`), service restore, renew, root and topology repair, `internal/app/upgrade_custody_source.go` | Not edited. Under the strict profile the controller runs prepare, submit, restore and the cancel-unseen-submit path (`cancelPrepared`, one prepare-then-Restore case). The service persists and renews a strict envelope on both engines. **Restore through the service was exercised only within one regime**; across a re-enrollment it is refused (G-3). No upgrade-source command was run under the strict profile (NOTRUN) |
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
| Preserve namespace and topology restrictions, CAS, least privilege, idempotency, cancellation, durable INTENT and OUTCOME audit, redaction | DONE | Default-profile behavior is unchanged (the one change found in review, a start-up refusal on an unverifiable journal, is repaired and pinned by a test). Existing tests were edited only where the signer type or the probe changed. Audit lives in the service layer and is untouched. Refusals are closed classes (`ErrInvalid`, `ErrConflict`, `ErrTrust`); no key or signature material in errors or the CLI messages |
| Old and new controller and signer combinations, accepted compatibility mode, strict refusal, replay and expiry, wrong enrollment, target, action or generation, tampered payload, concurrent rollout, key rotation | DONE | See the test table |
| Restart, takeover, interrupted trust rollover, restore of stale enrollment and state, post-restore reconciliation, rollback floor without resurrecting authority | PARTIAL | Floor, anchor, takeover and `Run` are tested at the controller. Restore through Hikyo and open jobs do not cross a re-enrollment (G-2, G-3). The floor lives only in the journal Secret, so a restore of that Secret restores an older floor, and clearing it is break-glass (E-14, E-17) |
| Fake and local fixtures for ordinary checks; how to rehearse and measure overhead | DONE | Runbook gives the benchmark command; numbers below. A kind rehearsal is NOTRUN |
| Operator runbook and exact candidate evidence; independent review of public signing key migration from payload encryption | PARTIAL | Runbook written and its key-generation commands validated by test. Independent review not yet done (HOLD). This PR touches no payload encryption |

## Handling E-14: new key or id against the existing journal

The register said a new key or id fails the journal check and that emptying the
journal loses the plan Restore needs. The design states each case and invents no
re-anchoring mechanism. "Anchored" means the controller can authenticate its
retained entry and keep starting. It does **not** mean Restore works through
Hikyo, which needs the server to sign a fresh decision on the stored row and
refuses a pre-rollover row (G-3).

| Change at re-enrollment | Result | Test |
| --- | --- | --- |
| Compatibility to dual, Ed25519 key kept | The journal's v1 entry anchors through the Ed25519 key, which is required in the dual window. A hand-signed Restore under the new regime still finds the stored plan at the controller | `TestTrustRolloverFromCompatibilityKeepsTheJournalAnchoredAtTheController` |
| Compatibility to dual with a new Ed25519 key, or straight to PQ-only | **Not anchorable** while the entry is still v1: `ErrTrust`, no write | `TestRetainedV1EntryAnchorsOnlyThroughTheKeptClassicalKey` |
| Dual to PQ-only, after a strict command | The retained v2 entry anchors through its ML-DSA signature | `TestDualToPQOnlyKeepsTheRetainedPlanAndRefusesTheRetiredAlgorithm` |
| Ed25519 key rotated, ML-DSA key kept, after a strict command | Anchors through the ML-DSA signature | `TestKeyRotationAndJournalResetBehaviorIsStated` |
| ML-DSA key replaced with a retained entry | **Cannot be anchored.** Start refuses with `authority trust state refused`; per-command reconcile returns `ErrTrust`; only the custody Lease is written. G-1 | same test |
| Incarnation, owner or enrollment id changed with a retained entry | Not anchorable (shape check). G-1 | `TestRetainedEntryIsUnanchorableAfterIdentityOrAliasChanges` |
| Enrollment id changed, `journal.json` cleared | Still refused: the command Secret holds the old id's command and `Mailbox.Send` conflicts at any sequence. G-4 | `TestEnrollmentIDChangeStaysRefusedAfterClearingTheJournal` |
| Only `journal.json` removed | Plan and accepted sequence are gone, so Restore is refused; the floor in `trust.json` remains | `TestKeyRotationAndJournalResetBehaviorIsStated` |
| Whole journal Secret emptied | The floor is gone too: a stale enrollment would be accepted again. Registered residual, break-glass | same test |
| A command signed before the switch and still queued, not yet accepted | Refused by the new controller; the server cannot renew it. No authority is resurrected | `TestInterruptedRolloverRefusesPreRolloverCommandsWithoutMutation`, `TestBootstrapDeploymentRefusesEveryOperationOnAPreRolloverRow` |
| A v1 command already accepted but not yet answered | Resumed by the dual controller from its journal (digest equality with the journal), which may write to the Deployment. It needs no new authority; it is why the runbook requires a terminal state | observed by the reviewer |
| An open job (`applying` or `partial`) | Cannot cross: the job becomes `partial`, the runtime is cleared, Restore is refused. G-2 | `TestOpenJobAcrossAReEnrollmentStaysOpenAndClearsTheRuntime`, `TestBootstrapDeploymentRefusesEveryOperationOnAPreRolloverRow` |

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
| `go test -p 2 -count=1 -run BootstrapDeployment ./internal/app/` | PASS, including every provider operation on a pre-rollover row |
| `go test -p 2 -count=1 -run 'TestCommittedDeploymentCarriesStrictEnvelope\|TestCommittedDeploymentRenews\|TestDeploymentRenewalRejects' ./internal/service/` | PASS on SQLite and PostgreSQL, including the strict envelope persisted, returned canonically and renewed |
| Repair round, same commands re-run on `b04a641c2` | `internal/configrollout`, `cmd/hikyo`, the app self-config subset and the service tests `TestOpenJobAcrossAReEnrollment*`, `TestCommittedDeploymentCarriesStrictEnvelope*`, `TestCommittedDeploymentRenews*`, `TestDeploymentRenewalRejects*` on SQLite and PostgreSQL: PASS. The broad service suite was not re-run after the repair (it passed at the first candidate) |
| `python3 scripts/ci/check-config-rollout-chart.py` | PASS: default render has no `authority`; strict render changes only that object; seven broken `authority` values are refused |
| Default chart render compared with the base chart | Byte-identical |
| Mutation checks (skip ML-DSA verify; drop the floor check; take the required set from the command) | Each made named tests fail, then was reverted |
| `go test -p 2 -count=1 -run 'SelfConfig\|Deployment\|Rollout\|NextRoot\|Candidate' ./internal/app/` | PASS |
| `go test -p 2 -count=1 -run 'Deployment\|Rollout\|SelfConfig' ./internal/service/` with PostgreSQL | PASS (218 s), both engines |
| `FAST_FEEDBACK_BASE=origin/dunky13/pqc-adr-amendments scripts/ci/fast-feedback.sh --working-tree`, pinned Node 26.12.1, through the shared queue | PASS (exit 0): diff check, Go format, test compile, docs content check. Hosted CI NOT RUN |
| `scripts/ci/check-commit-signatures.sh` and `check-dco.sh` against the PR base | PASS at the first code commit; re-run on the final head before push (see PR) |
| `go test -p 2 ./internal/lint/...` locally | NOT COMPLETE, three runs: the 10 minute default and a 40 minute queued run timed out on unrelated slow tests under a host load average of 14 to 19, and a third sat on `TestAlertCollectorSnapshotProtocolRejectsDrift` for over 30 minutes and was cancelled. No assertion failed. No further local run was made in the repair round |
| Hosted CI on `310eb9ffa` (first review head) | `repository invariants (boundary, lint, CI policy)` pass (7 m 16 s), `feedback (go)`, `feedback (tooling)`, `feedback (frontend)`, `ops-floor` amd64 and arm64 pass. `ci-queue-required` fails by design for this PR: "Only the protected default branch can enter this merge queue", because the PR is stacked on #1155 and not on `main`. It is not a code failure. The repaired head's hosted result is in the PR |
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
- Added in the repair round: profile id inside the signed bytes
  (`TestProfileIDIsInsideTheSignedBytes`, matrix case `wrong profile`);
  compatibility `Run` keeps running with an unverifiable journal
  (`TestCompatibilityRunKeepsRunningWithAJournalThatDoesNotVerify`, fails on the
  old code); a tampered signature through `Run` writes only start-up state
  (`TestRunRefusesATamperedSignatureWritingOnlyStartUpState`); strict prepare,
  unseen submit, cancel (`TestStrictRestoreCancelsAnUnseenSubmit`).
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

Owner for every row: Marc Went, on #1131.

| ID | Item | Residual risk | Claim limited to |
| --- | --- | --- | --- |
| G-1 | The journal anchor requires the retained entry to match the enrollment: the ML-DSA key (the Ed25519 key for a v1 entry), the generation and the shape (enrollment id, owner, incarnation, source aliases). Replaced ML-DSA key, changed incarnation or owner, removed alias and an Ed25519 rotation over a v1 entry all fail. Visible refusal at start; remedy is clearing `journal.json`. No re-anchoring mechanism was built because the register decided none | Availability; the stored plan is lost | Plans survive only the transitions in the table above |
| G-2 | An open job cannot cross a re-enrollment (provider refuses `Send`, `Response`, `RenewCommand`, `DecisionCommand`; runtime cleared; job `partial`/`transport_failed`; boot fatal unless sources are pending, from source reading; recovery refuses unresolved jobs). Once the floor rises the old enrollment is refused. The key-swap behavior existed at base; the floor closing the way back and the fresh-decision claim were new and are corrected | A job stranded across the switch has no in-band way out | Enroll only with no open job |
| G-3 | Restore through Hikyo is not available across a re-enrollment, and `kubectl rollout restart` or a Helm upgrade that re-applies rendered fields would make the controller's Restore refuse | Manual restore from the receipt | The journal entry stays anchored; the plan is not restorable through Hikyo |
| G-4 | Enrollment id change: clearing `journal.json` is not enough (the command Secret still holds the old id's command); the per-enrollment sequence restarts at 1; rows for the old id are refused. Fails closed | Availability | Unsupported |
| E-14 | Anchor over classical cluster access; journal write is floor, sequence and replay write | Stale or forged state by an actor with journal write | As claimed in the runbook |
| E-11, E-15 | Release and image path; no PQ claim covers how the controller binary arrived | Existing, #1132 | |
| | `helm upgrade` against a live Deployment changed by a rollout: reverting executor-applied fields was reasoned from the chart and Helm's merge rules, not run | Possible silent revert | UNVERIFIED; the runbook avoids it |
| | Real cluster, kind rehearsal with a strict enrollment, ConfigMap recreation, Pod deletion, admission policy on the floor write, `app.Boot` with an open job across a switch | | NOTRUN. `scripts/ci/check-config-rollout-kind.py` still generates only an Ed25519 authority |
| | Upgrade-source commands under the strict profile | | NOTRUN |
| | Old binary against new data | | NOTRUN (modeled) |
| | DEC-17 budgets, amd64 measurements | | NOTRUN |
| | Independent adversarial review of the repaired head, maintainer security approval | | HOLD, pending |

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
