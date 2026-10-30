# #79 adapter move flow settlement

The browser lifecycle now observes successful old-route reconvergence before
starting a second origin move after cancellation. It captures the cancellation
response's move ID, reads that move through the authenticated browser request
context, validates each response with the generated schema, and requires the
fixture target's `converge` job to reach `succeeded`.

## Failure evidence

Full-main run [41116961360, attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/41116961360/attempts/1)
tested source `108a7cc12bcce20c2925b08665d6c8ff14417ba4`, tree
`c402d8f453ab02e62535724269f38c6e240df541`. Its mobile group 4 job
`124282321018` failed the adapter lifecycle's second origin move:

- Cancellation returned HTTP 202 at 20:20:32.196 UTC with move state
  `canceled` and its old-route `converge` job still `queued`.
- The test immediately continued to another origin move. Its PATCH returned
  HTTP 409 at 20:20:32.660 UTC. The retained response reports `conflict`.
- The following adapter read reported converged state. The test timed out
  waiting for the success notice from the refused move.

`cancelAdapterMove` queues reconvergence before returning the canceled move;
the move state is therefore not a job-completion barrier. The origin-change
path preserves provider and generation fences while asynchronous work settles.
The trace does not expose which internal refusal branch produced this 409.
One independent local run of the unmodified two-flow mobile sequence passed,
which does not invalidate the recorded intermittent failure.

The repair adds an explicit job-outcome postcondition to the existing browser
flow. A missing, failed or unfinished reconvergence cannot satisfy it. The
subsequent origin move still has all its original success, URL and lifecycle
assertions. No mutation is retried, and existing timeouts remain unchanged.

## Validation

Current validation results are recorded in the PR description. The focused
commands, using the pinned contributor tools, are:

```sh
GOFLAGS='-p=2' GOMAXPROCS=2 pnpm --dir web run build
GOFLAGS='-p=2' GOMAXPROCS=2 pnpm --dir web exec playwright test \
  --project=mobile --workers=1 e2e/flows/machine-access.spec.ts \
  --grep 'creates an adapter behind|probes, plans'
GOFLAGS='-p=2' GOMAXPROCS=2 pnpm --dir web exec playwright test \
  --project=desktop --workers=1 e2e/flows/machine-access.spec.ts \
  --grep 'creates an adapter behind|probes, plans'
VITEST_MAX_WORKERS=2 scripts/ci/fast-feedback.sh --working-tree
```

The first flow supplies the adapter consumed by the second. These positional,
filtered runs deliberately do not establish the full flow-registry execution
closure. They exercise real local instances and the synthetic development
provider, not a live external provider.

## Qualification disposition

The original full-main result remains FAILURE: 44 successful jobs, two failures
including the aggregate gate, and a skipped browser-closure job. Its complete
attempt logs and 25 original artifact archives are retained with actual
source/run/attempt/uploader and digest bindings. Those observations remain
bound to `108a7cc`.

Main subsequently advanced through PR #1108 to
`142b0916e3479d4a6e40e1e1602d23cdf197a2ae`. That three-path nightly producer,
CI-test and handoff delta does not change production Go, web or module inputs,
but it changes source and producer identity. This repair uses that integrated
base. Neither this local validation nor the old source run selects or qualifies
a replacement release candidate.

After protected integration and deliberate source selection, refresh the exact
source/test/build bindings and execute applicable qualification. Race, native
recovery and ops-floor dispatches remain on hold for the old candidate. Provider,
public-artifact, disclosure, Apple, publication and deployment approvals retain
their existing scope. Keep #79 open.
