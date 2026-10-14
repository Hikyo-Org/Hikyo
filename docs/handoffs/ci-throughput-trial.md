# CI throughput trial

Status: local validation passed; hosted measurements pending. This draft is an experiment, not merge authorization.

The production change adds a complete shard-planning command, preserving the existing twelve race, three fuzz and three isolation assignments. It discovers package metadata once. CI continues loading its planner from the trusted base. The initial candidate accepts only the exact known legacy source digest; once the reviewed bulk planner becomes the base, CI uses the new protocol. Unknown sources, failed bulk execution and malformed or empty plans fail closed.

The temporary `ci-throughput-trial` workflow runs only on pushes to `dunky13/speed-up-pr-merge-ci`. Four sequential jobs use the existing standard GitHub-hosted pool. They compare actual old/new planner workflow steps, restored versus empty compiler caches, repeated race-test commands versus one compiled binary, and artifact transfer to a separate runner. Cache operations restore only; no cache producer or capacity settings are changed. The binary consumer has read-only repository permissions and cannot publish canonical merge proof.

Baseline: `5aac8053815433b2ed39e8b6a7ffddf9253803a4`. Both planner variants inspect the same candidate inventory. The new variant uses the candidate as a future trusted base solely for measurement; this does not activate it in production. First invocations are not a cold A/B pair. Warm comparisons alternate order. Cache comparison is one compiler-cache pair with shared downloaded modules. The race pilot exercises one real custody test, not the whole race suite. Include setup, transfer, queue and execution costs when interpreting hosted results.

Local evidence: both `scripts/ci` and `scripts/ci/analysis-shards-go` reached terminal package passes, with 1,376 passing test/subtest events and zero failures. Existing shard fixtures, new equivalence/refusal cases and a checkout with no Git history passed. Actionlint, shellcheck, cache policy and whitespace checks passed. Independent Sol review found no remaining material issues. Graph structure was refreshed.

After measurements: retain raw run/attempt identities, logs, artifacts and failures; update this handoff with results; remove the temporary workflow, experiment script and temporary cache-policy classification before making the PR ready. Preserve the planner regression tests and trusted-base migration. Full current merge validation and independent review remain required. Do not merge merely because the experiment succeeds.
