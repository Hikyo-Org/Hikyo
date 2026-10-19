# Queue snapshot recovery

[Run 39171701078](https://github.com/Hikyo-Org/Hikyo/actions/runs/39171701078)
failed after PR #991 merged. Its callback observed missing queue positions and
created a fresh blocking check over an existing success. Earlier callbacks also
rejected queued successors whose group and base commits had not materialized.

## Resulting behavior

- Missing positions and a queue identity change between pages restart the entire
  traversal, at most three times, within the caller's existing deadline. Backoff
  is 100 ms before the second attempt and 200 ms before the third. Partial pages
  never supply proof. Invalid identities and transport failures are not retried.
- An absent tested group can have a queued successor without group commits.
  Membership identities remain required; an unmaterialized predecessor still
  cannot supply an active group's proof.
- Queue movement during group reconciliation renews retirement evidence before
  the generic blocking fallback. Only freshly verified retirement makes the
  callback benign. Completed verdicts remain intact, obsolete pending records
  are cancelled, and active same-SHA PRs remain protected.
- Persistent inconsistencies remain blocking. Diagnostics include the page,
  missing position, entry count and at most sixteen positions and PR numbers.
  The opaque queue ID is fingerprinted rather than logged directly.

## Verification

The new regression cases failed on the original implementation and passed with
the fix. They cover single-page and multi-page recovery, deadline exhaustion,
persistent gaps, queued successors, final membership changes, same-SHA open PRs,
active prefixes and bounded diagnostics.

The following local checks passed:

```sh
GOMAXPROCS=2 go test -p 2 -parallel 2 ./scripts/ci/... -count=1
GOMAXPROCS=2 go test -race -p 2 -parallel 2 ./scripts/ci/pr-gate ./scripts/ci/queue-report -count=1
GOMAXPROCS=2 go vet -p 2 ./scripts/ci/...
git diff --check
```

Heavy validation used the shared agent queue. Hosted CI and a live callback
after publication have not been run. The original response bodies were not
logged; regression coverage reproduces the observed controller behavior without
claiming GitHub's internal update sequence is known.

## PR #1002 review follow-through, 2026-10-19

The merge babysitting request also covers review responses and CI repairs.
Two source findings were confirmed with failing regressions before repair:

- A repeated entry ID, PR number or position from an earlier page returned an
  ordinary error, bypassing snapshot recovery. Six regression cases reproduced
  this behavior for transient and persistent repetition. The reader now validates
  the whole current page first, then classifies inter-page repetition as queue
  movement. Malformed identities and duplicates within a single page remain
  fatal, including a malformed page whose first entry also repeats across pages.
  Persistent repetition still exhausts the same three-attempt read bound and
  cannot supply a partial snapshot or mutate checks.
- A group head worker could exhaust its initial role-read retries before reaching
  `groupProof`, bypassing that callback's retirement recovery. A regression through
  the production `runHead` entry point created a prior success, observed three
  position gaps, then removed the obsolete group on the fourth queue read. It
  failed before repair. The callback and the worker now share `retireMovedGroup`;
  only fresh verified retirement clears the movement error. A companion active
  same-head PR case continues to require a blocking verdict.

The focused regression batch passes after repair. Independent Standards and
Spec source reviews found no remaining actionable findings in the repair diff.
The CodeRabbit 80% docstring warning is not an established repository rule;
the new recovery logic includes explanatory comments. No check was disabled.
Qodo remained billing-blocked and is not counted as a completed review.

The full `scripts/ci/...` test suite, race checks for `pr-gate` and `queue-report`,
and scoped vet all passed again after repair, using the same commands above.
Structural graph refresh completed with the existing seventeen Astro parser
warnings; documentation semantics were not regenerated. Hosted acceptance and
live callback behavior still require protected merge-group evidence after
publication. No release or deployment is part of the babysitting request.
