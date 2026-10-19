# Queue proof: transient reads and lagging job metadata

On 2026-10-19 two merge-group runs for #1008 passed every job, but the
required `ci-queue-required` verdict on the group commit was a terminal
failure. The queue ejected the PR both times.

| Group | Run | Cause | Old verdict |
| --- | --- | --- | --- |
| `30898938` | `39273206516` | The jobs read returned HTTP 502 at 15:34:19Z, 15 minutes before GitHub's declared Actions incident (15:49 to 16:56Z). pr-gate did not retry reads and logged only `command exit status 1`. | "Current proof could not be safely established" |
| `a5e94191` | `39282633485` | During the incident the jobs API listed nine finished jobs as running, `merge-group-proof` among them, for 7 to 16 minutes after the run completed. | "Required merge-group validation failed" |

Once a merge-group run has completed, nothing retriggers the publisher except
schedules, which arrive hours late on this repository. A pending record would
therefore sit until the queue's 60-minute timeout while holding the single
build slot. Commit `e4894962` made finished-group outcomes terminal for that
reason, and this change keeps that property.

## Behaviour

### Read retries (queue publishers only)

`retryingAPI` wraps reads for `--queue` invocations other than
`--dispatch-workers`.

- It retries on HTTP 429, 500, 502, 503 and 504, and when a `gh` process
  fails without a status. Authentication-required (exit 4) and cancelled
  (exit 2) `gh` failures are not retried.
- It makes up to three attempts, with 1 s and 3 s backoff, and never waits
  past the context deadline.
- The HTTP status is taken from `gh` stderr when the response body carries
  none, so errors now report `HTTP 502`.

The plain `ghAPI.read` stays single-attempt. That keeps discovery dispatch,
retirement discovery and the legacy gate unchanged, and keeps retirement
discovery's request counting exact. Writes are never retried.

The merge-queue report client retries its GETs with the same attempt count and
base backoff. It honours `Retry-After` up to 10 seconds. When the server asks
for a longer wait than that, or longer than the remaining time, it stops
instead of retrying sooner.

### Group re-reads

These apply only to the queue-gate group callback and the group head worker,
which share one predicate. Targeted admission, the admission and ambiguous
workers, repair and scheduled invocations never wait.

**When proof jobs lag.** The run concluded success, but `validation /
ci-required` or `merge-group-proof` is still listed as unfinished, or not
listed at all.
- The publisher polls only that run's jobs endpoint, every 20 seconds, with
  the existing run, attempt and head checks.
- Once both proof jobs are final, it runs one complete reassessment
  immediately. That pass re-reads identity, policy, queue snapshot,
  membership, base, refs and jobs, then publishes the real verdict.
- A lag of ten polls cost 12 jobs reads and two full passes in tests, instead
  of a full pass every 20 seconds.

**When a read fails transiently.** A read elsewhere in the reassessment fails
transiently after its own retries, including the owned-check list read. The
publisher repeats the complete reassessment after 20, 40, 80 and then 120
seconds, which bounds API use to a handful of full passes. A rate limit (HTTP
429) ends the waiting at once and goes to the expiry outcome, so the publisher
does not keep calling a rate-limited API.

**Identity.** The first finished-group identity is pinned. A different
identity on a later pass cannot be proved. No verdict is written, the record
stays pending, and existing retirement handles a moved group.

**Writes.** No intermediate verdict is written. An invocation does not wait
when the newest owned record is already completed for the same run and
attempt, or is the expiry refusal itself, which is written onto the run-less
pending barrier and recognised by its title. A worker that runs after the
callback's expiry refusal therefore does not wait again. Other run-less
completed records, such as a retired barrier, do not stop the waiting.

### Time bounds

The group job has a 10-minute budget, of which about 30 seconds goes to
checkout, setup and compile before the process starts.

- No wait or pass starts after the 6-minute window. Whether the window is
  still open is checked immediately before each wait, using the actual next
  delay. The pass that would cross the window is the final decision.
- All looped work, including read retries, runs under a hard deadline of loop
  start plus 7 minutes 30 seconds.
- The terminal write uses its own reserve of up to 60 seconds, derived from
  the parent context and capped at loop start plus 9 minutes. Non-looping
  invocations keep the 5-second reserve. A test pins these production values
  against the 10-minute job timeout.
- A success verdict is written inside the fenced pass. Passes start before 6
  minutes and the fence is at 7 minutes 30 seconds, so a fence cut during that
  write is unlikely. If it happens, the existing fail-closed supersede path
  applies.

### Outcomes

- **Expiry.** If proof metadata is still incomplete or unreadable when the
  window closes, the publisher writes a distinct terminal refusal. Its title
  is "Merge group proof metadata unavailable", and it asks for a re-queue. It
  does not leave a pending record until the queue's own timeout.
- **Unchanged.** A failed, mismatched or duplicated proof job is refused
  immediately, as before.
- **Changed for repair paths.** Schedule, push and dispatch repair reach group
  proof without looping. When they meet these transient causes, they now leave
  the record pending instead of publishing a terminal failure. A transient
  read is not evidence of a failed group. Nothing re-queues it automatically,
  but a later callback, worker or repair can still finish it.

## Limits

- **Long lag.** A 16-minute lag like the incident's is longer than the
  window. It now ends in a clear, re-queue-able refusal instead of a
  misleading one.
- **Identity never proven.** If no pass gets as far as proving the
  finished-group identity, for example because policy or queue reads fail
  for the whole window, the record stays pending. This is the pre-existing
  rule that a terminal group verdict needs a proven identity.
- **Permanently missing proof job.** A proof job that will never appear, such
  as one renamed by an authorized workflow change, is treated as lag for the
  window and then refused with the re-queue message.

## Rollout

This changes trusted default-branch code and no workflow file. Its own
merge-group run is still published by the old controller on main. If GitHub is
healthy that does not matter; if it is refused once, re-queue it. It lands
before #1009, which carries the feedback-lane repair for #1008.

## Local verification

These ran with GNU userland and pinned Go 1.27.2 on macOS/arm64:

- `go test -count=1 ./scripts/ci/...`
- `go test -count=1 -race ./scripts/ci/pr-gate/ ./scripts/ci/queue-report/`
- `go vet ./scripts/ci/...` and `gofmt -l scripts/ci`
- `bash scripts/ci/check-cache-policy_test.sh`,
  `sh scripts/ci/check-trusted-ci-scripts_test.sh` and
  `./scripts/ci/run-go-tool.sh actionlint`

Removing each behaviour in turn (the jobs-only polling, the separate reserve,
the settled-record check, loop continuation on deferrable barrier errors, the
window check and the fence) made at least one test fail.

These checks do not prove hosted GitHub API behaviour under a real incident.
