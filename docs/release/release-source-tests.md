# Release source test scheduling

The stable build runs `scripts/release/test-source.sh` after creating its
dedicated PostgreSQL test database. The separate empty schema-generation
database remains untouched.

[Release run 41239870691](https://github.com/Hikyo-Org/Hikyo/actions/runs/41239870691)
passed all 47 validation jobs and standalone docs/floor jobs, then failed its
unpartitioned `go test ./...` release gate. App, isolation and lint exhausted
Go's cumulative ten-minute package alarms. Passing isolation partitions in the
same run took 572 to 728 seconds each. The dump named recently started app and
isolation tests; it did not establish a persistent deadlock in those tests.

The release driver preserves coverage while using the repository's existing
test scheduling policy:

1. Compile the isolation binary's runnable test, fuzz-seed and example inventory.
2. Plan three isolation partitions and require their union to equal that
   inventory exactly once. Empty, duplicate, missing and unsafe targets fail
   before execution. The current inventory contains 739 roots; the count is not
   hardcoded.
3. Run the existing core driver, which prepares all four lint contexts and runs
   app and lint separately from concurrent database suites. It covers every
   package except isolation, including ordinary examples and fuzz seeds.
4. Execute every isolation partition with uncached results and the existing
   18-minute per-partition policy. These are ordinary tests, matching the
   replaced release gate; the separate race validation remains required.
5. Run the unchanged release fixture suite. A failed core group or partition
   does not suppress later groups or fixture evidence. Any failed group still
   fails release acceptance.

`GOMAXPROCS=2`, Go package parallelism two and isolation test parallelism two
bound both direct work and nested lint compiler loads. No test roots, product
deadlines, signing checks or release trust constraints are removed.

The workflow retains `release-source-tests-RUN-ATTEMPT` even when source tests
fail. It contains the compiled inventory, each planned partition, core and
isolation output, release fixture output and `results.tsv`. A supplied
`RELEASE_SOURCE_TEST_OUTPUT` directory must be new; existing evidence is never
overwritten. Without that setting, the driver removes its temporary evidence
directory when it exits.

The lightweight scheduling regressions do not run the product suites or create
database fixtures:

```sh
scripts/ci/test-core-packages_test.sh
scripts/release/test-source_test.sh
go test -p 2 -count=1 ./scripts/ci -run '^TestStableSigningAndPublicationStaySeparate$'
```

They cover preparation and ordering, complete partitions including fuzz/example
targets, failure propagation, planner/inventory refusal and evidence retention
wiring. Actual hosted execution is separate acceptance evidence. An existing
tag still uses the workflow committed at that tag; changing main does not
change the workflow used by a retry of an older tagged run.
