# Issue #158 completion handoff, 2026-10-26

Issue: [#158, AWS Secrets Manager synchronization adapter](https://github.com/Hikyo-Org/Hikyo/issues/158).
Application PR: [#969](https://github.com/Hikyo-Org/Hikyo/pull/969).
Earlier source, authorization and resource history:
[AWS Secrets Manager handoff](158-aws-secrets-manager.md).

**Issue #158 remains open.** The application implementation, finite non-AWS
application acceptance and exact build from the signed application commit are
available. Official AWS login and the expected IAM identity were verified.
The separate local validation controller still fails its original five-second
publication-prelaunch bound. Complete lifecycle, same-controller cleanup and
real AWS acceptance remain unqualified.

This document is stacked on application commit
`327dec159d680fab7b33a251ab46a2d1c16e9fa1` in PR #969. It adds a completion
snapshot without changing application or validation code. The local frozen
validation packets and their offline HTML reports are operator artifacts outside
the repository. They are not introduced or adopted by this documentation PR.

## Readiness breakdown

The overall planning estimate is **5/10**. These scores describe readiness;
they are neither a measured completion percentage nor a time forecast.

| Step | Readiness | Current evidence or gap |
| --- | --- | --- |
| Source and verification repairs | 7/10 | Latest narrow optimization implemented; genuine-owner qualification failed before transport. |
| Focused regression checks | 7/10 | Latest 22 data-parity cases passed; 23 dependent controls remain NOTRUN. |
| Source and delivery reviews | 7/10 | Bounded component reviews exist; the current remaining cycle has used round 1 of 3. |
| Local CLI and subprocess acceptance | 4/10 | Partial inert transport execution; complete acceptance remains unqualified. |
| Complete startup-to-result lifecycle | 3/10 | No complete positive run within the retained bounds. |
| Same-controller cleanup cycle | 3/10 | Historical individual groups passed; the complete aggregate remains unqualified. |
| Application acceptance criteria | 8/10 | Finite non-AWS application inventory passed; real-provider criteria remain open. |
| Exact signed-commit candidate build | 10/10 | Local binaries built from the signed commit; no release publication implied. |
| Real AWS validation | 2/10 | Login and non-root IAM identity verified; resource validation remains unrun for this candidate. |
| Final CI, merge and closure | 3/10 | Configured required queue gate passed; final runtime, AWS and merge proof remain open. |

## Application work and passing local evidence

The application implements one-way synchronization from Hikyo into AWS Secrets
Manager. It supports explicit region/account routing, JSON-object and per-key
destinations, optional KMS, workload and role authentication, and explicitly
selected protected static credentials. Hikyo remains the source of truth.
The writer's typed API surface excludes reads of destination secret values.

Completed repairs include stale adoption-witness recovery across service, store
and worker consumers, protected full-descriptor CLI input, canonical destination
name and JSON-member collision regressions, and shared CLI test-runner reuse.
The final protected-input tests preserve all 90 rejection cases and their
privacy and no-effect probes. Scoped Standards and Spec reviews reported no
remaining findings for that final application increment. Those reviews do not
qualify the later local validation-controller changes.

The finite non-AWS application acceptance recorded:

| Inventory | Recorded result |
| --- | --- |
| Shared adapter and Secrets Manager race selection | 121 top-level tests, 356 passing records |
| CLI selection | 29 top-level tests, 151 passing records |
| Server, service and configuration selection | 51 top-level tests, 95 passing records |
| Store and migration checks, both SQL engines | 73 top-level tests, 167 passing records |
| Authenticated API/CLI lifecycle | 29 top-level tests, 224 passing records, including all eight surface/kind/engine combinations |
| App and build compatibility | 12/46 and 2/11 top-level/passing records |
| Required isolated TLS Moto harness | 1 top-level test, 10 passing records |
| AWS browser journeys | Desktop 5/5 and mobile 5/5, zero retries; ten screenshot/trace pairs retained |
| Repository invariants | 86 top-level tests, 377 passing records; one intentional mutating-updater skip |

Docs, feedback, import and vet components also passed their finite inventory.
Frontend feedback reused an exact unchanged 839-input and dependency-lock
comparison. Browser execution deliberately selected the AWS journeys; full
browser-registry execution coverage remains partial.

Broad application acceptance originally used the frozen working-tree digest
`796b0347a55474aff9eae24a0fd660a1f5df19a5a4463a03b1af420310b0c56f`.
The final test-runner correction changed one of 4,847 paths, with production,
docs and dependency inputs unchanged, and received fresh focused CLI evidence.
Its source digest is
`ee608634ad87e142ac78920abfe204fbd2b8c121c1345d69c9b4c608aebd0dfc`.
Do not relabel the original broad result as a new full run on another digest.

Original aggregate jobs 1700 and 1702 remain failed: the first used an
unsupported screenshot CLI flag, and the second lacked GNU checksum tooling
in its owned PATH. Confined harness/environment corrections and successful
job 1707 closed the remaining components without changing product assertions.
Similarly, job 1715's offline import-launcher failure remains failed; job 1717
is the distinct successful cached import check.

Job 1746 built the exact application candidate from the signed commit using
Go 1.27.3, CGO and embedded UI. These are local build artifacts, not
platform-signed or published release artifacts:

| Artifact | SHA-256 |
| --- | --- |
| Application binary | `4ff6805ef2c4e2f1dd54a23d8f5b8091b099d7d4646ec342ddeb6ab8ac7107bd` |
| Isolation test executable | `79e79a9235c164309bb65c4dbaa469478350ecf43a2b99cb1382e26afe995653` |
| Signed-candidate build checkpoint | `4ff65c87f93c3c0fd5823da8059b742ef23ee2fbbe5cc50604f85cba15b9509c` |
| Finite application acceptance checkpoint | `029872bd237f504c7bc53ba5ef6e08b692b5323235dab14f3d7fb2ca171401d9` |

## Local controller repairs and the current blocker

The separate frozen validation system binds source identity, native evidence,
publication, controller actions and cleanup to one owner graph. Its work has
covered sticky failure lifetime, archived prior-release replay, cleanup and
reader controls, module/codec/bootstrap custody, bounded nonblocking checkpoint
reads, immutable committed envelope plans, rollback, future-start consumers,
callback re-entry, canonical ledger locking, native descriptor continuity and
installed launcher custody.

Historical collector and consumer lanes passed on their exact source manifests.
Those passes do not qualify the current cumulative source. Component reviews
likewise retain their actual static or executed scope. Provider-screened and
skipped reviews are not technical clean verdicts.

The latest local candidate has 219 source/index entries, including 11 integration
modules, 177 frozen-package files and the already reviewed AWS CLI 2.38.4
launcher-custody repair. Public signatures, the shared owner graph, complete
native/source authentication and original deadlines remain retained.

| Latest local packet identity | SHA-256 |
| --- | --- |
| CODE index | `c67bace64d218e5f0638c5973818d50e3aa1eaf9465ecf6c95416638cf84f30b` |
| API index | `aae4b3b73cd13d58e3981d9639923840e2e6f1831b4c348ff2bf02a448f0bc9c` |
| Source index | `012aa0b709f2f2f6a13f84ab3631d3071bb032033b69f569ac5359ec55449028` |
| Repair checkpoint | `4702a0f95db3bdfa4a1fdc53585dc7c1216f2e18a486c11964771a80c320e045` |

The latest attempt reuses typed immutable journal-graph and decoded-plan data
inside existing bounded record/transport scopes, with complete current-tree
comparison and the original canonical-byte fallback. Its 22 expression-parity
cases passed. It caches neither review verdicts nor native results or grants.

Its actual unprofiled owner run, **job 1898**, failed after **91.077 seconds**
at provision prelaunch. All **23 dependent controls are NOTRUN**. The last
returned semantic stage was the recorded bound component at 84.901 seconds.
Transport, own-CAS and callback assertions were never reached. Engine calls
and runtime intents/outcomes were zero. All processes and cleanup were awaited.

The failure path is:

```text
register_phase_prelaunch
  -> original_bound_checked
  -> reconstruct_original_for_cleanup
  -> verify_record -> historical -> _originals
  -> SqlReader.exact_original -> bounded
  -> native-five-second-deadline
```

This call already has the publication parent. The source-record scope correctly
retains that parent's route rather than opening a nested scope. The latest
record/transport optimization therefore does not cover the earlier failing gate.
The final SQL frame shows where expiry was detected, not a measured SQL hot spot.
No transport improvement or regression was established by this run.

Previous job 1877 preserved a 12.920-second profiled first metadata call before
its returned transport guard expired. Original reviews and journal
classifications dominated the measured work; inclusive timings overlap and
profiling overhead was not isolated. Job 1870 separately hit the whole
180-second watchdog without a final result. Both failures remain failures.

**Next engineering task:** resolve the actual publication-owned prelaunch
reconstruction cost, then reach and verify the transport seam. Keep the
five-second source bound, setup-inclusive 180-second target, five-second grace,
one-attempt policy and original assertions. Do not add a nested replay scope,
cache an authentication verdict, omit a pass or extend a deadline to get green.

## Remaining execution and review order

1. Freeze a new owned successor that addresses the prelaunch gate, preserving
   earlier source and failed results unchanged. Run the genuine unprofiled
   component and all 23 reachable controls. Preserve every failure and NOTRUN.
2. Bind the prepared complete lifecycle and same-controller cleanup fixtures
   to accepted final source/API/delivery pins. The current final fixture remains
   unbound. Execute both complete obligations with the original owner graph,
   343 assertions, five cleanup bodies and setup-inclusive bounds.
3. Finish final cumulative Standards and Spec reviews. The current remaining
   cycle has used round 1 of a maximum 3; round 2 follows positive composed
   lifecycle/cleanup evidence, and round 3 is for necessary corrections only.
   No automatic fourth round or reset for small increments. Cosmetic-only
   changes were explicitly deferred; skipped cross-provider review stays skipped.
4. Finalize the source/API/delivery graph, native owners, signed application
   artifacts, actual lifecycle/cleanup receipts, reviews and invocation bindings.
   The prepared operator packet still defaults to HOLD/exit 2 with 39 missing
   bindings. Its 77 metadata controls are not execution-readiness proof.
5. Obtain a valid AWS execution window, recheck identity and effective MFA,
   reconcile prior inventory, cost and cleanup, and bind the protected invocation.
   Authentication success alone does not clear these gates.
6. Run real-provider acceptance and exact cleanup. Reconcile all six issue
   criteria, final head/base CI and review evidence, then apply the existing
   merge-authorization conditions and verify merge and issue closure.

## AWS boundary and the six issue criteria

Official login completed on 2026-10-26. At 08:31:33 UTC,
`sts get-caller-identity` verified account `099256073061` and non-root IAM user
`hikyo-158-setup`; the user completed approval in Chrome through Playwright.
Only authentication and the own-identity check occurred in this continuation.
Earlier expired attempts remain failed. `GetCallerIdentity` does not itself
prove effective MFA, and session validity must be rechecked before validation.
Credentials, codes and the private login cache are excluded from this document.

| Issue criterion | Remaining real-provider proof |
| --- | --- |
| Browser/CLI/API configuration | Exact approved source, destination, region/account and KMS correspondence |
| Workload/role/static authentication | Effective identity, bounded authentic role sessions, expiry and protected input |
| Ownership lifecycle and replay | Actual lifecycle, adoption and same-store replay without destination value reads |
| JSON and per-key results | Independent positive oracle for canonical synthetic values and partial outcomes |
| Provider failure handling | Genuine KMS denial, authentic expiry/identity refusal and replay; label injected faults as injected |
| Isolated harness plus real smoke | Both SQL engines, effective no-read denials, durable process replay and exact cleanup |

The retained plan targets account `099256073061` in `eu-central-1`, gross and
billable exposure strictly below USD15, and at most 32 new synthetic secret
creations including failed attempts, one new key, eight roles, one temporary
boundary and at most one OIDC provider. Historical reservations remain
176 plus 32, or 208 lifetime. These ceilings are not a fresh resource grant.
Do not refund reservations, reset claims, reuse consumed plans or cancel the
four historical key-deletion schedules.

The retained resource horizon is **2026-10-26T12:22:56Z**. Four hours of cleanup
runway made **08:22:56Z** its latest nominal start. This snapshot is after that
time. Once the local packet is concretely ready, obtain and bind an explicitly
renewed window; do not silently extend the old horizon or change freshness clocks.

Historical real AWS attempts did occur and failed. The present candidate has
no successful real-provider acceptance receipt. Real smoke must prove effective
writer `GetSecretValue` denial with a separate positive oracle, durable replay
without duplicate versions, and exact cleanup of recorded synthetic ARNs.
Scheduled deletion, API absence, KMS PendingDeletion and eventual physical
deletion are distinct results. Preserve ambiguity and required cleanup holds.

## CI, evidence availability and handoff limits

At the 2026-10-26 snapshot, PR #969's configured `ci-queue-required` gate passed.
That is admission evidence, not complete-suite or AWS proof. A separate
`ci-required-next` check remained pending. Benchmark run `40232421613`, attempt
2, completed successfully; CodSpeed was neutral with an acknowledged 4.09%
regression. Its comparison used main `059902e` rather than PR base `14678ba6`,
so it does not establish exact-base performance qualification. CodeRabbit's
retained review was paused before current application head.

The local consolidated handoff contains the full reports, failed stacks, source
and artifact pins, 50 evidence references and readback of all 219 latest source
entries. It and its standalone HTML were delivered separately to the operator.
There is no portable repository path for those local packets. Obtain them from
the operator if needed; if unavailable, the local source-only claims cannot be
independently reproduced from this repository document alone. Do not invent a
replacement path or treat these hashes as execution receipts.

The local implementation task ended with a completed author turn and failed
qualification. No owned source-author children or test/authentication handles
remained active at that handoff. This document does not launch tests, adopt
source, authorize AWS operations, merge a PR or close #158. Preserve signed
commits and DCO sign-off, and record the actual final source and test outcome.
