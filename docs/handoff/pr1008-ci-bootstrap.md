# CI bootstrap for PR #1008

PR #1008 remains unmerged. Two independent CI problems blocked it:

- Its application Go feedback repeatedly exhausted the 220-second
  setup-inclusive deadline (for example run `39296581879`).
- Two merge-group runs that passed every job still received a failed
  `ci-queue-required` verdict.

This PR repairs the feedback lane only. The queue proof repair is a separate
publisher-only PR, [#1011](https://github.com/Hikyo-Org/Hikyo/pull/1011), because it changes trusted default-branch code and must land
first.

## Feedback: cause

All four failed `feedback (go)` jobs on #1008 restored the same 239 MB
`go-feedback-v1` entry through its restore-keys fallback. That seed only holds
`./internal/cli ./scripts/ci` compiler exports. #1008's backend owners need 341
further packages, 164 of them `k8s.io/client-go`, so a reported cache hit did
not mean a warm backend compiler.

Two further costs sat inside the deadline:

- `go test -c` vets every dependency, and a `go list -export` seed stores no
  vet results.
- `-p 2` capped build and link concurrency at two, without any recorded
  memory pressure.

Module download was minor: 13 to 15 seconds in three runs and 33 seconds in
one. The passing head `f743c2f3` used 159 of 187 seconds. Heads `96c66697` and
`bf10af50` were killed at about 190 seconds, with only `internal/store/upgrade`
left to build.

## Feedback: repair

- The new `ci-feedback-backend-cache` workflow prepares a version-2 compiler
  cache from immutable trusted main. It holds application, CLI and CI tooling
  exports built with `-gcflags=all=-dwarf=false`. Feedback uses the same
  compiler flags and a separate cache directory.
- The feedback compile is now `go test -c -vet=off`, at the default
  parallelism. Full validation still runs `go vet ./...` in `test_core`, so the
  fast lane gives up only early vet findings on changed packages. The seed
  publisher keeps `go list -p 2`: it has a 15-minute budget and no feedback
  deadline, so bounding its memory costs nothing that matters.
- Application Go, tooling Go and frontend run as three mandatory lanes, each
  with its own runner and the unchanged 220-second deadline. Whole-workflow
  success remains required for queue admission.
- Source formatting and test compilation and linking remain mandatory. No test,
  init or `TestMain` executes in this compile-only profile.

| Cache | Consumers | Compiler ABI | Publication |
| --- | --- | --- | --- |
| `go-feedback-v1` | Production-fixture validation in `ci.yml` (`source-artifact-security`) | Default compiler flags, DWARF included | Existing main-only `ci-feedback-cache` |
| `go-feedback-v2` | Application and tooling feedback lanes | DWARF omitted from compiler exports | New main-only `ci-feedback-backend-cache` |

Both caches keep the existing limits of 1,610,612,736 expanded bytes and 12,000
files. The new publisher starts from an absent cache directory, never restores
historical entries, and publishes only after compilation and size validation
succeed. PR feedback only reads caches.

## Measurements

Local measurements of #1008's 18 application owners with the version-2 seed.
These are linux/amd64 cross-builds with `GOMAXPROCS=4`, single runs, not hosted
timings:

| Configuration | Local time |
| --- | --- |
| `-p 2`, vet on | 84 s |
| Default `-p`, vet on | 67 s |
| `-p 2`, `-vet=off` | 49 s |
| Default `-p`, `-vet=off` (this PR) | 45 s |

The only hosted pass took about 1.4 times the local time, so this PR should
leave roughly 90 seconds of margin against the deadline. #1009's own feedback
run does not measure this: it changes CI tooling, not backend packages. The
first real measurement is #1008's feedback after it is rebased.

## Queue proof (handled separately)

The two refused merge groups had different causes:

- **Group `30898938` (run `39273206516`).** The publisher's jobs read returned
  HTTP 502 at 15:34:19Z. pr-gate does not retry reads, and it turned the read
  error into the terminal "Current proof could not be safely established". This
  was 15 minutes before GitHub's declared Actions incident window (15:49 to
  16:56Z).
- **Group `a5e94191` (run `39282633485`).** During that incident, the jobs API
  still listed nine finished jobs as running, including `merge-group-proof`,
  for 7 to 16 minutes after the run completed.

[#1011](https://github.com/Hikyo-Org/Hikyo/pull/1011) retries transient reads. It also re-reads a completed
group's proof in place for a bounded window. If the proof is still not readable
after that window, it refuses with a distinct, re-queue-able reason. An earlier
draft of this PR added a waiting job inside the merge-group workflow; it was
dropped, because its 8-minute deadline was shorter than the observed lag and it
failed the validation run instead.

## Required delivery sequence

1. Merge the publisher-only queue proof PR, [#1011](https://github.com/Hikyo-Org/Hikyo/pull/1011), through the protected merge queue.
2. Merge this PR through the queue.
3. Run `gh workflow run ci-feedback-backend-cache.yml --repo Hikyo-Org/Hikyo --ref main`,
   and verify that the seed job publishes the bounded version-2 cache.
4. Rebase #1008 on main. Drop its own copies of the CI commits (`d71801f5`,
   `96c66697`, `bf10af50`) in favour of the merged versions, keep its signed
   and DCO history, and verify hosted feedback against the new cache.
5. Queue #1008 only after its required gates pass, then verify the complete
   merge-group proof and the merge independently.
