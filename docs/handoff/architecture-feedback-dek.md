# Feedback planning and DEK preparation

## Accepted scope

Deepen two existing modules without changing their product contracts:

- Feedback planning owns selected checks, their order, lane placement and
  prerequisites. Local execution and hosted setup consume that policy.
- DEK preparation owns positional rejection, explicit selector validation,
  context resolution, Project/Instance scope validation and authenticated target
  selection, in that order. Each command retains parsing, output-format
  validation, its request, confirmation and rendering.

The full-candidate classifier, protected merge queue, authorization formulas,
encryption hierarchy and release/deployment behavior are outside this change.

## Implementation

`scripts/ci/feedback-plan.sh` owns the feedback policy. It emits the original
eight domain booleans plus additive hosted-lane details and keeps planning
usable on Bash 3.2 before Go, Node, pnpm or jq setup. The driver dispatches
stable check identifiers through explicit commands. Hosted setup uses planned
prerequisites and lane/scope mappings, loading policy from immutable tooling.
Pinned tools, cache ownership, complete lane coverage and the setup-inclusive
deadline remain enforced. Node and pnpm version probes now reject failed
commands even if their output happens to match the required version.

`internal/cli/dek_target.go` owns the preparation sequence shared by
`rotate-dek` and `reencrypt`. Existing context, trust, session and transport
machinery remains in use. Empty explicit selectors still differ from omission;
invalid scope fails before context, pin, trust or session lookup. `--instance`
remains a target reference, while `--instance-scope` selects Instance key scope.
Explicit `--env` remains invalid for both commands. Rotation confirmation and
re-encryption behavior remain with their respective command owners.

## Review and regression evidence

Two fresh GPT-6.1 Sol xhigh contexts implemented the disjoint scopes. Parent
inspection and local validation preceded a fresh GPT-6 Astra xhigh review.
That review found one weakened workflow-test invariant: an unrecognized cache
restore could escape prerequisite validation under a different step name.

The parent added the exact negative mutation, reproduced its failure, and
restored validation for every cache restore. Approved readers retain their
existing prerequisite and count checks. The affected workflow, concurrency and
cache-seed tests passed after the repair. No actionable DEK finding was reported.
This automated review does not represent independent human security review.

## Local validation

The implementation was validated against base
`d02242217502de6f23f528396be572072c34c085` before publication:

```sh
go test -p 2 ./internal/cli -run '^(TestDEKRotation.*|TestDEKHandlersRejectUnknownFlags)$' -count=1 -parallel=2
go test -p 2 ./internal/cli/... -count=1 -parallel=2
GOMAXPROCS=2 GOFLAGS=-p=2 bash scripts/ci/fast-feedback_test.sh
/bin/bash scripts/ci/fast-feedback-plan_test.sh
GOMAXPROCS=2 go test -p 2 -parallel 2 -count=1 ./scripts/ci ./scripts/ci/pr-gate
bash scripts/ci/check-cache-policy_test.sh
GOMAXPROCS=2 go test -p 2 -parallel 2 -count=1 ./scripts/ci -run '^TestFeedback(Workflow|Concurrency|Seed)'
FAST_FEEDBACK_BASE=HEAD GOMAXPROCS=2 GOFLAGS=-p=2 scripts/ci/fast-feedback.sh --working-tree
```

These checks passed. The complete CI/queue package tests preceded the final
validator-test repair; affected feedback tests and working-tree feedback were
rerun afterward. The driver checked formatting, changed-owner compilation,
ShellCheck and pinned Actionlint over the current workflows. The feedback shell
suite includes lane conservation, prerequisite consistency, immutable-tooling
ownership, failure propagation, deadlines and dependency compatibility.

Later current-base checks and exact-head reviews belong in the pull request's
evidence. Local fixtures are not hosted CI, real-provider acceptance or release
proof. Queue admission alone is not complete merge-group validation.

## Rollback

Revert the refactor together with its direct consumers and tests. It introduces
no data migration or durable format change; the additive feedback plan and
private DEK preparation have no independent deployment step.
