# Benchmark admission refusal repair

## Failure and cause

[Benchmark-control run 40526446034](https://github.com/Hikyo-Org/Hikyo/actions/runs/40526446034),
attempt 1 at main `9d620dd451a56408ccab8360283dfeef0320eac7`, failed while
admitting PR #969 head `361e02e3915eef840429d88589c39689f4a58dfb`.
The PR carried an older `pr-benchmark.yml` than current main. The workflow
identity guard correctly refused execution, but the controller returned that
expected automatic refusal as an error and failed the optional job.

## Behavior

`scripts/ci/benchmark/control.go` distinguishes confirmed workflow mismatches
from missing content identities and GitHub API errors. Automatic mismatches
use the existing race-aware skip path and report **not run** without collecting
budget or triggering a measurement. Manual mismatches still fail. API errors,
malformed responses and missing identities still fail in both modes.

Update an older PR from main to restore workflow compatibility. Intentional
workflow changes must land on main before the changed workflow can receive
admission. All other admission checks still apply.

## Verification

Both automatic mismatch regression cases failed before the fix. After the fix,
all 20 cases in `TestWorkflowAdmissionRefusals` passed. They cover both protected
workflows and both request modes, including malformed or incomplete API data.
Existing successful-admission and cancellation tests remain in the owner suite.

Local checks passed on the final code change:

```sh
scripts/ci/fast-feedback.sh --working-tree
go test -race -p 2 -parallel 2 ./scripts/ci/benchmark ./scripts/ci/codspeed-budget ./scripts/ci/pr-benchmark-control ./scripts/ci -count=1
go vet -p 2 ./scripts/ci/benchmark ./scripts/ci/codspeed-budget ./scripts/ci/pr-benchmark-control
git diff --check
```

These checks do not prove hosted execution, paid measurement or publication.

## Separate recovered failure

[Queue-gate run 40527052751](https://github.com/Hikyo-Org/Hikyo/actions/runs/40527052751)
exhausted its setup-inclusive admission work deadline during a final feedback
jobs read. DCO and the three feedback lanes passed.
[Run 40527055672](https://github.com/Hikyo-Org/Hikyo/actions/runs/40527055672)
subsequently admitted the same head and base without a source change.
That recovery establishes admission only. This patch changes no queue deadline
or queue proof requirements.
