# #930: managed backup retention and honest erasure

Status: owner-approved policy B; owning-ticket decision records and latest-source
local acceptance and scoped qualifying review are complete. Signed delivery,
exact-head CI and verified merge remain pending. Local passes are not merge proof.

## Problem and accepted decision

[#930](https://github.com/Hikyo-Org/Hikyo/issues/930) found a contradiction
between the locked 180-day managed-backup/erasure promise and the shipped
pruner. Retained count protected arbitrarily old archives, including directories
below the count. The persisted last successful archive also remained protected.
A no-deletion run could advance successful-prune health despite retaining an
expired recovery copy. The [#145 handoff](./145-dr-hardening.md) records the
historical deviation; it was not an approved amendment of the owning ADRs.

The owner selected **B: expire managed archives after 180 days except for the
last durably recorded successful recovery archive; retain candidates when
recovery state is uncertain; report persistent breaches; amend the erasure
promise**. The decision applies to both SQLite and PostgreSQL.

The owner separately authorized reopening [operations decision #32](https://github.com/Hikyo-Org/Hikyo/issues/32)
and [encryption decision #14](https://github.com/Hikyo-Org/Hikyo/issues/14), with
**Astra plus independent GPT-6.1 Sol high adversarial review qualifying only for
these #930 amendments**. This does not waive other ADR review gates. The
authorization is not a review verdict. Record the owning-ticket reopening and
completed review evidence before calling the governance work complete.

## Normative policy

1. An independent **180-day ceiling overrides retained count**. An archive
   exactly at the cutoff does not exceed this ceiling, but the earlier soft rule
   still applies. Older managed archives are eligible even
   when the directory contains fewer than `HIKYO_BACKUP_RETAIN_COUNT` archives.
2. Within that ceiling, retain-count protection applies. Outside the retained
   count, `HIKYO_BACKUP_RETAIN_DAYS` can prune earlier. Count is not a maximum
   that deletes otherwise young archives. No unlimited configuration exists.
3. Preserve the last durably recorded successful archive until a replacement is
   durably recorded, including after clock rollback. An unrecorded filename,
   partial publication or failed export is not replacement evidence. Export
   success does not replace the separate readable-restore drill.
   Successful records bind the SHA-256 digest of ciphertext; legacy success
   state without a digest remains uncertain until a fresh successful export.
4. Missing/unreadable recovery state, an absent recorded artifact or uncertain
   clock/custody retains recovery candidates and reports uncertainty. More than
   one expired archive may remain. Destructive pruning must not infer recovery
   safety from filename order.
5. Retained expired archives are an immediate retention breach. Persist the
   latest prune attempt, bounded failure reason and retained-expired count
   separately from export outcomes. Expose breach, unverified and stale state
   through health API, doctor, bounded metrics and logs. Do not advance
   successful-prune health until compliance is established. A failure to record
   an outcome cannot become a successful compliance assertion.
6. One policy covers all managed complete current-engine archives, including
   scheduled, manual and pre-migration exports. This explicitly supersedes the
   unimplemented separate pre-migration count-three promise. Other engines,
   staging files, unrecognized files and externally managed copies are outside
   this pruner's destructive scope. Backup directories remain instance-owned.
7. Erasure depends on actual successful live collection/deletion and the **last
   containing archive**, not eligibility. A snapshot can predate collection and
   publish later. Account for snapshot/export delay, scheduling, successful
   unlink, recovery exceptions and independently managed copies. Recovery
   exceptions can persist indefinitely; neither “GC eligibility + 180 days”
   nor an approximate 270-day erasure deadline is promised. Live key destruction
   still cannot erase the wrapped hierarchy inside an existing backup.

## Finalized health and coordination contract

The backup object on `GET /instance/retention-health` contains boolean
`retention_breach` (expired archives or unverified compliance), bounded string
`retention_reason`, last-observed count `retained_expired_archives` and nullable
`last_prune_attempt_at`. Stale checks count as unverified; a zero observed count
does not assert current compliance. Doctor's `backup-retention` finding has
severity `error` when breached or unverified. Label-free gauges
`hikyo_backup_retention_breach` and `hikyo_backup_retained_expired_archives`
expose the verdict and last-observed count. The existing
`hikyo_last_backup_prune_success_timestamp_seconds` stays unchanged on failed
checks.

Final publication, success recording and pruning serialize filesystem changes, verification/deletion and
their datastore result under a directory advisory file lock. Shared mounts must
support reliable cross-process advisory locks for every publication/record/prune writer,
including writers on different HA nodes. Ciphertext SHA-256 verification binds
the durable success record to the actual archive rather than a reusable name
and byte count. No custody keys are loaded. This coordination and digest
verification do not establish that an archive can be read with separately held
recovery keys; the readable-restore drill remains required.

An absent schedule alone is a configuration warning (`not_scheduled`, `retention_breach=false`), not a mandatory-backup requirement. This does not certify an unchecked installation or discard a fresh manual prune result. Observed expiry, failed/incomplete or uncertain pruning, and clock uncertainty remain unhealthy when scheduling is disabled; missing/stale checks of a configured schedule remain unhealthy.

## Documentation and implementation map

- [Operations ADR](../adr/ops-spec.md): explicit scoped amendment; historical
  absolute-bound, erasure and pre-migration count-three decisions remain marked
  as superseded rather than rewritten as prior policy.
- [Encryption ADR](../adr/encryption-model.md): qualifies mandatory retention
  without changing key hierarchy, separate custody or restore admission.
- [Operations catalogue](../spec/ops-catalogue.md): separates earlier age
  threshold, retained-count protection, independent ceiling and recovery
  exceptions.
- [Backup runbook](../site/src/content/docs/docs/backup-and-restore.mdx) and
  [privacy guide](../site/src/content/docs/docs/privacy.mdx): explain exceptions,
  health interpretation, actual-collection timing and external copies.
- Service/store/config/transport/doctor/metric implementation and generated
  contracts: crosscheck the final patch against every item above before merge.
  Verify the finalized health fields and metric names above against the final
  generated contracts and implementation.

## Acceptance coverage

The final local results below exercise this matrix. Remote exact-head gates and
verified merge remain delivery requirements.

| Requirement | Required evidence |
| --- | --- |
| Independent ceiling and count semantics | Under-count and over-count expired archives; exact cutoff; lower configured days; count does not delete young archives |
| Last-success custody | Ancient sole success; fresh durable replacement; unrecorded newer filename; clock rollback; missing/unreadable state and missing artifact; legacy digest-less success and ciphertext digest mismatch |
| Publication ordering | An unrecorded publication cannot replace the protected durable success; a deleted or changed pending artifact cannot later be certified by stale success recording; cross-process directory locking spans filesystem work and datastore outcome |
| Failure semantics | Failed unlink stops deletion safely, records a bounded durable outcome and leaves success unchanged; successful recovery clears current breach/unverified state |
| Shared reporting | API, doctor and metrics agree on breach, unverified and stale state; no false success after a failed outcome write or retained expired exception |
| Engine and restore coverage | Affected service/config checks plus actual SQLite and PostgreSQL encrypted export/restore drills; existing custody and restored-admission guarantees preserved |
| Governance and delivery | Owning #32/#14 reopening records; scoped independent adversarial review and fixes; mandatory exact-head CI/review gates; verified PR merge SHA and main ancestry |

The earlier baseline reproduced the contradictory retention behavior. Baseline
passes are not acceptance evidence for this amended contract. No new tests,
builds or runtime checks were run by the documentation worker during the shared
resource hold. The implementation owner records subsequent checks and review findings below.
PR and verified merge evidence remain outstanding. Release publication and production deployment are outside scope.


## Recorded owner decision

The owner decision is recorded on [ops #32](https://github.com/Hikyo-Org/Hikyo/issues/32#issuecomment-6220018436), [reopened encryption #14](https://github.com/Hikyo-Org/Hikyo/issues/14#issuecomment-6220017891), and [implementation #930](https://github.com/Hikyo-Org/Hikyo/issues/930#issuecomment-6220018904). The separate #612 amendment retains its own qualifying review gate. Paired migration76 adds the persistent prune attempt/outcome and ciphertext identity. Its canonical declaration is generated; latest-source runtime acceptance and the scoped qualifying verdict are recorded below.


## Initial implementation validation and review repairs

Canonical `go tool sqlc generate`, `go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml`, and development compatibility declaration generation completed for migration76 on both engines. The first focused service run passed PostgreSQL but failed SQLite: the generator left an unexpanded parameter macro inside an `IN` expression. Rewriting the source expression as equivalent equality predicates and regenerating corrected the emitted SQL. The original failure was preserved.

The identical focused service rerun passed in 36.835 seconds with `GOMAXPROCS=2`, `GOFLAGS=-p=1` and both engines enabled. It covered real encrypted archives under count beyond180 days, protected-copy persistence and fresh replacement, missing/legacy/corrupt recovery identity, clock rollback across repeated attempts, failed unlink/directory sync and no-op retry, inaccessible directory, canceled deletion ownership and publication durability. This pass predates the later cancellation-aware hash-read repair and extra exact-cutoff assertion; their subsequent revalidation passed in the latest-source run below.

The qualifying independent GPT-6.1 Sol high review returned changes required: regenerate the TypeScript health contract; stop hashing between reads after cancellation; qualify exact180-day wording for earlier soft expiry; and document that pruning reads/hashes the protected ciphertext. The owner independently confirmed all four findings. Those repairs and the final qualifying review are now complete, as recorded below. The review authorization applies only to these #930 amendments, not #612 or other decisions.


### Parent scheduling counterexample

The parent identified an overbroad draft default: `not_scheduled` implied a breach even with no managed backup state or a fresh successful manual export/prune. Approved B did not authorize mandatory backups; baseline configuration permits an absent schedule and doctor warns. The source now treats that condition alone as a configuration warning, preserves recorded prune evidence, and keeps expiry/failure/incomplete/clock findings unhealthy after scheduling is disabled. Nine pure-health and six doctor countercases passed in the latest-source runs recorded below; independent source repair verification also passed. The strict doctor fixture validator was not weakened. Migration76 and its canonical declaration are unchanged.


Existing but incomplete recovery identity, including a legacy success record without a ciphertext digest, remains `recovery_uncertain` even without a schedule. This differs from an empty unconfigured installation and is checked before the configuration warning. The independent review confirmed the scheduling correction and identified this additional case; its source repair and regression cases passed in the latest-source runs below.


### Generated client and web validation

Pinned Node 26.7.0 and pnpm 11.24.0 frozen installs completed without lockfile changes. Canonical TypeScript generation, client typecheck and all 37 client tests passed. Web typecheck and lint passed. The first full web unit run passed 1763 tests and failed one prototype fixture contract test because that inferred fixture lacked the four new health fields. Updating the fixture preserved the generated schema requirement; the focused prototype, retention parser and diagnostic banner rerun passed all 18 tests. The full run was preserved rather than represented as wholly green. The unit suite used a source-compatible prebuilt OIDC helper, avoiding an implicit Go compilation during the Node-only resource slot.


## Latest-source local acceptance

All Go commands used `GOMAXPROCS=2`, `GOFLAGS=-p=1`, explicit `-p 1`, and `-count=1`. Engine-dependent tests used a dedicated PostgreSQL 18.6 fixture with UTC sessions through `HIKYO_TEST_POSTGRES_DSN`; both engine branches executed. The timezone-sensitive signed-schema digest defect is tracked separately by #941. This does not declare UTC a supported-runtime prerequisite or claim #941 fixed.

| Scope | Result |
| --- | --- |
| Canonical OpenAPI Go regeneration | Passed after the scheduling-description correction |
| Service retention, publication, ciphertext cancellation and cutoff cases | Passed, 37.405 s; both-engine real encrypted archives; all nine scheduling-health countercases |
| CLI doctor | Passed, 1.825 s; all six scheduling-doctor countercases. The first run preserved two failures in older finding-count/index assertions, then corrected those assertions for the added `backup-retention` row |
| Backup config defaults and bounds | Passed, 0.328 s |
| App backup jobs, custody, public admission and upgrade recovery | Passed, 68.883 s; SQLite and PostgreSQL restore paths and private-custody refusals |
| Server retention metrics and failure omission | Passed, 1.172 s |
| Metric registry and cardinality | Passed, 1.018 s |
| Isolation and live retention surfaces | Passed, 53.064 s; both-engine `TestRetentionCLIStartupSweep`, `TestBackupRestoreDrill` and `TestRestoreDrillVerb`, strict doctor fixture and operation/formula/query pins |

The live startup-sweep tests create a real encrypted archive dated 181 days earlier, durably record it, and require pruning to refuse a false success. The real CLI obtains retention health from the running API; its `backup-retention` finding must be an error with `protected_expired`, and operational metrics must report breach 1 and retained-expired count 1. A fresh durably recorded export followed by pruning must clear the doctor finding and both gauges. These assertions ran on both engines. The strict validator still rejects unrelated doctor errors.

Migration 76 and the development declaration retain their original generated identities. Original SQLite SQL-generation, PostgreSQL timezone, web-fixture and doctor-assertion failures remain preserved in the local evidence bundle. Later passing runs do not erase those failures or turn the original false-success reproduction into a fixed-source pass. No production or release action was performed.


## Scoped qualifying review

On 2026-10-16 the individual GPT-6 Astra high implementation owner completed the source and runtime assessment, and independent GPT-6.1 Sol high adversarial review returned **SOUND, no remaining findings**. The independent reviewer checked the current source, final handoff, preserved original failures and passing raw logs, including both-engine named cases and the live breach-to-recovery flow. It did not run duplicate builds or tests. The earlier TypeScript, cancellation, cutoff, resource-cost, scheduling and legacy-identity findings are resolved. Exact doctor assertions and the strict unrelated-error validator remain intact.

This is the owner's explicitly authorized qualifying path **only for #930 amendments to operations #32 and encryption #14**. It is not a global waiver and does not qualify #612. The reviewer task identity is `retention_contract_review`; the dated owner decision is linked above. Local focused acceptance does not certify a production shared filesystem, hosted exact-head CI, merge or deployment. Mandatory remote checks and required reviews still gate merge.

API parity additionally passed in 1.517 s; canonical import formatting passed for 1992 handwritten Go files. The API freeze script exited successfully with its guard dormant because tag `v1.0.0` does not exist; this is not a freeze-comparison pass.

## Rebased-head review repairs

CodeRabbit's rebased-head review identified a legacy-server compatibility gap:
omitted retention fields decoded to zero values and made doctor label a
scheduled, unmeasured installation as verified. A regression decoded a legacy
JSON response with an existing prune-success timestamp and reproduced that
false `ok`. Doctor now warns that this server did not measure retention and
needs upgrading. Explicit breaches still refuse, measured healthy state stays
healthy, and unscheduled handling is unchanged. The complete CLI suite passed
in 9.204 seconds after the fix; CLI vet also passed.

The runbook now explains that an alternate `--out` directory can replace the
recorded recovery point while leaving scheduled pruning unable to verify it in
`HIKYO_BACKUP_DIR`. It gives the default-directory export as the recovery step.
It also documents the intentional `in_progress` breach signal during hashing
and pruning, doctor refusal during that interval, and alert timing based on
measured run duration. The fail-closed service behavior is unchanged. Read-only
documentation preparation and its regression test passed. Hosted validation of
the follow-up commit remains separate from these local results.
