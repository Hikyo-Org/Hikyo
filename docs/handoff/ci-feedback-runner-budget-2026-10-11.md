# CI feedback runner budget follow-up

This CI-only extraction targets main `82113c81ef4ac9ed1a4e4653350120728d605a7d`. Its 11 implementation files match the independently reviewed CI state from PR #883 head `91b085944d461e8fd4c74de65f4019a7a5f16b11`, with the three test-only helper names subsequently repaired to Reuse terminology. Auth, API generation, application changes and invitation documentation stay in PR #883.

## Behavior

Each useful PR commit runs separate Go/tooling and frontend jobs, with `fail-fast: false`, a four-minute job limit and a shared 220-second deadline within each job. A frontend failure cannot cancel the other runner. Each job cleans up its own unfinished descendant processes and reports failure when its selected checks are incomplete. Metadata-only edits keep separate concurrency groups and check names; they cannot cancel, evict or obscure useful commit feedback.

The Go lane retains changed-file formatting, batched compilation of selected owning packages including tests, shell validation, workflow lint and read-only documentation preparation. The frontend lane retains selected client typecheck/tests and web typecheck/lint/all unit tests. The local command defaults to both lanes; unknown lane arguments fail.

Go feedback restores only the existing compiled-build archive, read-only. The exact key includes OS, architecture, runner image and immutable tooling manifest hashes; the fallback keeps the same OS/architecture/image bucket. Go validates each content-addressed entry and the actual selected compilation always runs on cache hits and misses. The large module archive is excluded. A cold miss may still exceed the deadline; one observed pass is not a universal five-minute guarantee.

Feedback harness refusal, lane and deadline fixtures execute unconditionally in the mandatory full lint job. The conservation test requires that exact additive step and rejects its removal, skipping or fail-open behavior before comparing the remaining job configuration to the unchanged historical baseline. All original full-suite domains and tests remain mandatory before merge.

## Evidence

Before runner separation, frontend and Go competed on one runner and useful feedback repeatedly failed at roughly 228 seconds. After separation, frontend passed in 134 seconds but isolated Go still failed with its 12-package batch unfinished after 201 seconds. Restoring the compatible build archive then produced [successful useful feedback in 169 seconds](https://github.com/Hikyo-Org/Hikyo/actions/runs/38155041999), from creation at 16:20:24 UTC to completion at 16:23:13 UTC, on PR #883 head `91b085944d461e8fd4c74de65f4019a7a5f16b11`. Queue and setup are included. The CI-only extraction requires its own hosted proof.

That head's full candidate correctly failed [isolation shard 0](https://github.com/Hikyo-Org/Hikyo/actions/runs/38155416851/job/114517566905): the registry invariant interpreted three YAML mutation helper names containing Cache as runtime caches. Only those declaration names and callsites changed to Reuse. The focused invariant reproduced all three errors before repair and passed afterward; the registry, invariant and every negative YAML mutation remain unchanged.

Local validation and independent review are tracked in the accompanying PR. The extracted CI files match a frozen review manifest; the frozen coverage baseline and domain registry are unchanged. The extraction is isolated from PR #883's application code, so its local and hosted results must be assessed on its own exact head.

## Delivery and merge boundary

No protection settings change is part of this PR. `ci-required-next` remains required and strict. After signed/DCO delivery, admit exactly one full candidate using `ci:full`. Merge only after the trusted publisher proves the complete passing suite for the exact head combined with the live main tip. Then update PR #883 to the new main and validate its remaining application change separately.

Reversing this change restores prior feedback execution; it does not change data or application behavior. The blast radius is contributor feedback timing and CI harness validation.
