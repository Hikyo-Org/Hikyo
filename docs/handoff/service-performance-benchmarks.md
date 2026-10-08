# Service performance benchmarks

`internal/service/workflows_benchmark_test.go` adds nine read benchmark cases to the
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
native one-iteration smoke checks from CodSpeed walltime reporting. Walltime now
runs daily on main and on explicit, budget-admitted PR requests from a bot
comment, using `codspeed-macro-arm64-graviton-ubuntu-22-04`. The request controller
executes trusted main tooling; PR code runs in its original unprivileged
`pull_request` context. Both measurement lanes use the same hardware and disable
shared Go caches. The first daily main run establishes the new baseline;
previous hosted x64 timings are not comparable. PR smoke remains GitHub-hosted.
The closed runner policy allows only the daily/manual main and requested PR
measurement jobs to use this exact Macro label.

CodSpeed lists 600 included ARM64 Macro minutes per month, then $0.032/minute.
Account runner access must be enabled before the first main run. This change
does not enroll the organisation or alter billing settings. Manual dispatch
includes an admitted walltime run on main; branch dispatch remains smoke only.
See [Daily and requested PR wallclock benchmarks](codspeed-daily-pr-benchmarks.md)
for the allowance, credit option, trust boundaries and activation steps.

Reference: <https://codspeed.io/docs/benchmarks/go>.

Four fixed publish/import/history/diff cases and a separate large-matrix browser
check extend this coverage. See [UX performance benchmark additions](ux-performance-benchmarks.md)
for fixture reset semantics, CI wiring, provisional browser ceilings and local evidence.
