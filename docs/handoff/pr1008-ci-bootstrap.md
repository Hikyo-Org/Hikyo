# CI bootstrap for PR #1008

PR #1008 remains unmerged. Its Go feedback repeatedly exhausted the existing
220-second setup-inclusive deadline, even after omitting linked debug data and
separating application, tooling and frontend runners. Tooling and frontend
passed; application compilation still timed out in run `39296581879`.

## Cache gap and repair

The existing compiler seed covers CLI and CI tooling. Source dependency closure
inspection found 323 backend dependency packages outside that seed, including
SQLite, pgx and Goose. A reported cache hit therefore did not establish a warm
backend compiler.

The new `ci-feedback-backend-cache` workflow prepares a separate version-2
compiler cache from immutable trusted main. It includes application, CLI and
CI tooling exports and uses `-gcflags=all=-dwarf=false`. Feedback uses the same
compiler flags and a separate cache directory. Both source formatting and
actual test compilation/linking remain mandatory. No test, init or TestMain
executes in this compile-only profile.

| Cache | Consumers | Compiler ABI | Publication |
| --- | --- | --- | --- |
| Existing `go-feedback-v1` | Existing production-fixture validation | Existing compiler defaults | Existing main-only workflow |
| New `go-feedback-v2` | Short application/tooling feedback | DWARF omitted from compiler exports | New main-only workflow |

Both caches retain the existing maximum of 1,610,612,736 expanded bytes and
12,000 files. The new publisher starts with an absent cache directory, never
restores historical entries, and publishes only after compilation and size
validation succeed. Setup diagnostics use a separate bootstrap directory.
PR feedback only reads caches. Its four-minute runner limit and 220-second
absolute deadline remain unchanged.

Application Go, tooling Go and frontend remain independent mandatory lanes.
The actual helper fixtures conserve the original package-owner and check
unions. Local unscoped feedback retains its complete graph. Whole-workflow
success remains required for queue admission.

## Queue completion repair

The prerequisite also contains the CI repairs already prepared for #1008.
A successful merge workflow had still exposed `merge-group-proof` as running
through the jobs API, causing required check `ci-queue-required` to fail.
The publisher now keeps exact-head nonterminal proof blocking and pending,
then repeats its complete proof before finalizing the same record. Terminal
failures and invalid identities remain refused.

A read-only successor to the two existing native proof jobs waits for exact
run, attempt and head metadata to become visible as successful. This allows
the repair to land while the privileged publisher still executes default-branch
code. Full validation, required proof jobs, membership, authority, source and
signature/DCO checks are preserved.

## Measurements and proof boundaries

Measured locally with pinned Go 1.27.2 on macOS/arm64 and conservative resource
settings, using a fresh trusted-main checkout:

- Backend-inclusive compact seed: 1,476,466,202 expanded bytes and 10,522 files.
  Initial export preparation took 100.94 seconds.
- The actual application feedback helper on #1008 head `bf10af500`, using that
  seed in the same source directory, completed in 74.23 seconds. It compiled
  and linked all 18 selected application package owners.

These are local measurements. They do not establish Linux runner timing,
hosted cache publication, protected queue acceptance or merge. The failed
hosted attempts remain recorded.

## Required delivery sequence

1. Validate and merge this CI prerequisite through the protected merge queue.
2. Run `gh workflow run ci-feedback-backend-cache.yml --repo Hikyo-Org/Hikyo --ref main`.
3. Verify the main-only seed job succeeds and publishes the bounded version-2
   cache with its exact main revision, platform and module/tool manifest key.
4. Incorporate current main into #1008, preserving its feature changes and
   signed/DCO history, then verify hosted feedback against the new cache.
5. Admit #1008 only after its required gates pass. Verify complete merge-group
   proof and actual merge to main independently from admission.

No release or deployment is authorized by these CI changes.
