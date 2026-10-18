# Required-check completion and recovery

A completed merge-group validation could leave `ci-queue-required` queued when
member authority remained pending or the group changed an unapproved validation
contract. Admission also depended on delayed mergeability metadata, and scheduled
retirement discovery traversed foreign app objects through GraphQL.

## Resulting behavior

- Admission runs canonical DCO verification and immutable `git merge-tree`
  conflict proof without checking out PR content. Metadata may be reused within
  an unchanged admission tuple; DCO and conflict proof run again before success.
- Completed group refusals finish the authenticated blocking check with failure.
  A fresh policy, complete source workflow identity and queue membership must
  match before that write. Changed or unprovable identities surface an error.
- Bootstrap reads the authenticated Jobs API identity once. A queued status can
  lag the executing step, but the exact run, attempt, head, job identity and
  original start deadline remain required. There is no status polling.
- Retirement discovery uses a complete bounded REST check inventory while
  preserving ownership, source binding and worker-side validation.

Full merge-candidate validation, signatures, DCO, contributor authority and review
requirements remain unchanged. Admission success is not merge proof.

## Local verification

```sh
go test ./scripts/ci/... -count=1
go test -race ./scripts/ci/pr-gate ./scripts/ci/queue-report -count=1
go vet ./scripts/ci/pr-gate ./scripts/ci/queue-report
./scripts/ci/run-go-tool.sh actionlint .github/workflows/ci-queue-gate.yml .github/workflows/ci-queue-worker.yml
shellcheck scripts/ci/check-fork-validation.sh scripts/ci/check-fork-validation_test.sh
sh scripts/ci/check-fork-validation_test.sh
```

All passed. Regressions cover signed conflicting heads, queued Jobs API lag,
completed source refusal, policy refusal, changed workflow/branch/conclusion,
queue movement, filtered Git object hydration and foreign-app discovery.

The Git transport fixture is offline. Live GitHub HTTPS hydration, hosted
publisher permissions and exact-candidate CI still require hosted acceptance.

## Optimization boundary

Impact selection remains advisory. A measured eight-file backend change required
full fallback for generated SQL and unknown ownership; package counts alone do
not justify skipping validation. Cross-base proof reuse, increased merge
concurrency and event pruning were not enabled.

The historical upgrade timeout showed a SQLite cancellation test still executing
a recursive statement after cancellation. One focused run and 100 local repeats
passed. The hosted hang remains unexplained; no timeout increase, removed test or
unproven job isolation is included.
