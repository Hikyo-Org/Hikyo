# Service performance benchmarks

`internal/service/workflows_benchmark_test.go` adds nine benchmark cases to the
existing CodSpeed service package selection. Each operation runs at 10, 100 and
1,000 keys. These are synthetic scale points, not measured production workloads.

| Operation | Measured work |
| --- | --- |
| `BenchmarkValuesList` | Real authorization, SQLite catalogue/value reads, config decryption and secret masking |
| `BenchmarkRevisionExport` | Real authorization, published snapshot reads, config decryption and schema resolution |
| `BenchmarkEnvironmentSignals` | Real authorization, catalogue, latest revision and pending-draft queries |

Fixtures use a private disk-backed SQLite datastore admitted through the signed
development gate, a real keyring and a bootstrapped local operator. Organisation,
project, catalogue, environment and populated snapshot are created through the
production service methods. Twenty percent of keys are secrets; ten percent
have pending drafts. Benchmarks read the published state, not those drafts.

`b.Loop()` excludes fixture construction and initial full result checks from
timing. The loops check errors, cardinality and revision identity where relevant.
Initial checks verify sorted key names, exact config plaintext, secret masking
and pending version identities. `b.ReportAllocs()` reports native Go allocation
costs. Reads do not accumulate revisions, drafts or secret disclosure audit rows.

These are warm service measurements. They do not measure HTTP middleware,
session admission, cold startup, PostgreSQL, adapter calls or concurrent load.
Rate/concurrency budgets and advisory notification callbacks are intentionally
unwired; a throughput benchmark must not turn into a rate-limit refusal test.
Secret disclosure and its ceremony/audit writes are excluded: secret cells stay
masked. Existing router, crypto, dotenv and scanning benchmarks remain separate.

Native smoke check:

```sh
go test ./internal/service -run '^$' \
  -bench 'Benchmark(ValuesList|RevisionExport|EnvironmentSignals)' \
  -benchtime=1x -count=1
```

For repeated local measurements, stop other test processes and use
`-benchtime=200ms -count=3`. Laptop timings are diagnostic, not deployment-floor
certification or a portable performance threshold.

The CodSpeed command remains `go test -bench=.` over the workflow's existing
package paths. Do not copy native `-run`, `-count` or `-benchtime` flags there:
the integration supports only `-bench`. The workflow separates
native one-iteration smoke checks (PRs and manual dispatch) from CodSpeed
walltime reporting (pushes to main only). The latter uses
`codspeed-macro-arm64-graviton-ubuntu-22-04`; no PR uses Macro minutes or uploads
shared-runner timings. Performance regressions are detected after merge, not
as a reliable premerge comparison. The first Macro main run establishes a new
hardware baseline; previous hosted x64 timings are not comparable.
The runner/cache policy permits only this exact Macro label in CodSpeed's
main-push job. PR smoke jobs remain GitHub-hosted and the Macro job cannot use
shared Go caches. Validation includes refusals for PR Macro execution, a different
Macro label, a Macro smoke runner and a shared Go cache on the Macro job.

CodSpeed lists 600 included ARM64 Macro minutes per month, then $0.032/minute.
Account runner access must be enabled before the first main run. This change
does not enroll the organisation or alter billing settings. Manual dispatch
deliberately runs smoke only, so it cannot spend Macro minutes on a PR branch.

Reference: <https://codspeed.io/docs/benchmarks/go>.
