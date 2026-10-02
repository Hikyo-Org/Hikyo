# CodSpeed Go runner compatibility

The CodSpeed job in run 36993203753 failed before executing benchmarks.
Its Go runner ignored the unsupported `-run` flag but interpreted the following
`^$` value as a package path, producing `malformed import path "^$"`.

The workflow now uses `go test -bench=.` with the existing five package paths.
CodSpeed generates its own benchmark runner, so the usual Go flag for skipping
tests is unnecessary. Keep native Go test flags out of this command unless the
CodSpeed Go integration explicitly supports them.

Compatibility reference: <https://codspeed.io/docs/benchmarks/go#compatibility>.

Validation: Actionlint, existing workflow action-pin tests, and the CodSpeed job
on the pull request. The hosted job is the integration check for instrumentation
and upload; native `go test` alone cannot validate CodSpeed's argument parsing.
