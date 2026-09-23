# Issue #247 — CI job registry

Issue: https://github.com/Hikyo-Org/Hikyo/issues/247.

**State: implemented.** Changed-path classification, workflow job conditions,
and required-job verification now share one checked registry of exact static
workflow job IDs.

## Contract

- `scripts/ci/ci-job-registry.json` owns path-class selections for all 13
  selectable validation jobs and gate behavior for all 21 static `ci.yml` jobs.
- Plan v2 keys are exact workflow job IDs. The five underscore-to-hyphen aliases
  and the checker's `gsub` conversion are removed. Later, folding jobs to save
  runner slots left `test`, `race`, `fuzz`, `freeze-guard` and `generated` as
  domain keys without a same-named job; the Go test now requires every key to
  select a check that `ci-required` gates.
- Pull-request classification and aggregation fetch the registry from
  `BASE_SHA` beside their trusted scripts. Missing trusted metadata fails the
  required gate closed.
- Plan v2 is an explicit compatibility boundary. A base checker without
  `--supports-plan-v2` is refused instead of receiving an incompatible plan.
- The Go metadata test parses workflow YAML, proves the registry/workflow job
  bijection, checks each plan condition and aggregate `needs`, and rejects
  added, removed, renamed, or unregistered jobs.
- Generated outputs: none.

## Validation

- `./scripts/ci/classify-changed-paths_test.sh`: exact-ID fixture matrix passed.
- `./scripts/ci/check-required-jobs_test.sh`: planned success/skip and refusal
  fixtures passed.
- `./scripts/ci/check-trusted-ci-scripts_test.sh`: base-SHA trust fixtures passed.
- `go test -count=1 ./scripts/ci -run '^TestCIJobRegistry'`: 7 passed.
- `./scripts/ci/run-go-tool.sh actionlint`: passed.
- `go vet ./...`: passed.
- `go test -count=1 ./...`: 3,350 passed in 58 packages.
