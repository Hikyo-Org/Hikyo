# #1143: SPA pacing under the source allowance

PR: <https://github.com/Hikyo-Org/Hikyo/pull/1143>. The authorized endpoint is a
reviewable PR. Merge, release and deployment are not claimed.

## Problem

Operators saw 429 responses in the web UI and many repeated requests in the
network tab. The refusals come from Hikyo, not an intermediary: they carry
`X-Hikyo-Request-Id`, and the audit log records `auth.request_refused` with stage
`admission`.

Since #1040 every credentialed request charges `Limiter.AdmitDiscovery`, 60 per
minute per source, shared with public discovery. It sits in front of the 300 per
minute session budget in `internal/service/budget_api.go`, so 60 is the effective
ceiling for a browser. The SPA was paced for the larger budget.

## Result

The SPA is paced for the allowance, and a 429 is treated as pacing rather than as
lost contact. The summary table and measurements are in the PR. Decisions worth
keeping:

- Only `InstanceConfig` asks for live self-config status. Timers on one query
  reset together, so the shortest interval among mounted observers sets the rate.
- The signals fallback is `max(30s, 6s x environments)`, which holds it to 20
  requests per minute at any project size.
- A burst of stream events flushes on the leading edge, so a single event keeps
  its latency and stays inside the `e2e/performance` budget.
- A stream recovery refetches topology, signals and pending drafts, `3 + 2 x environments`
  requests. It used to reload the whole working set, `3 + 4 x environments`, on every
  reconnect. Values follow from a signals revision that advanced, and no event refreshes
  settings. A value or settings read that had failed is retried, unless it was a denial.
  Topology stays in recovery as #931 decided.
- A cold load reads values after each environment's first signals response, or its
  failure. Values carry no revision, so a values read that finished before signals
  answered was repeated, adding one request per environment. The cost is one round
  trip before values render, and a signals read that never settles now holds its
  environment's values back.
- A background whoami 429 uses the existing `degraded` path, with `paced` keeping
  it from raising the fence. An earlier separate retry timer survived logout,
  looped at session expiry and bypassed the enrolment gate.
- A blocking whoami 429 still fences, because the UI is waiting on that answer.

`api/sensitiveInventory.json` was re-pinned for `api/matrix.ts`, `api/rules.ts`,
`api/selfConfig.ts` and `routes/InstanceConfig.tsx`. No operation, cache rule or
plaintext lifetime changed.

## Production evidence

A 26 second HAR of an idle Matrix with 3 environments, recorded before this PR
deployed: `instance/config` every 2.1 seconds, 30 per minute, and `runtime/status`
every 15 seconds. With the 30 second topology poll from code, one idle tab costs about
40 of the 60 per minute, so two tabs behind one source exceed it. No reconnect burst
occurred in that window. The HAR holds session data and is not committed.

## Validation

Local checks ran directly on focused files with two workers: Vitest, `tsc` and
`oxlint`. The full unit suite, `fast-feedback.sh`, Storybook and Playwright did
not run locally because the shared heavy-job queue was paused. Hosted CI is the
first run of the two rewritten flows in `web/e2e/flows/matrix.spec.ts`.

The request counts come from a temporary harness, not committed: the real Shell
and Matrix bundle over a stubbed transport with a sliding 60 per minute limiter.
They were measured before the final rework of the 429 session path, which those
runs did not exercise because they saw no 429. That path is covered by unit tests
only, and has not been observed in a browser or on a deployed instance.

## Remaining work

Each of these still lets a browser reach or amplify a 429. None is fixed here.

1. **Decide whether a live session should charge the source allowance.** This is
   the real ceiling. A Matrix cold load costs about `4 x environments + 7`
   requests, so about 13 environments exceed 60 alone, and tabs or operators behind
   one NAT share the allowance. Changing it reopens the #1040 security review.
2. **Check source attribution behind chained proxies.** The per-source bucket only
   isolates clients if `HIKYO_TRUSTED_PROXY_CIDRS` and each proxy preserve the
   client address. If every hop is trusted and none supplies one, all clients share
   one bucket keyed on the proxy. Public discovery charges the same bucket.
3. **Fenced recovery at 60 or more active queries never completes.** It requires
   every active query to succeed within 8 seconds. Simulated at 70 active queries,
   the fence did not lift in 600 seconds; at 40 it lifted after 70 seconds.
4. **A 429 on a rotation check takes the failure wall.** The epoch is already
   blocked, so the check cannot use the `degraded` path, and a display-once result
   can be lost. This also applies to a peer tab's blocking check.
5. **`refresh-and-publish` publishes unconditionally.** A session refresh in one
   tab makes peer tabs drop their cache and reload. Found by reading, not measured.
6. **HA and quiet healthy streams.** SSE fan-out is per node and a healthy stream
   has no signals poll, so cross-node value changes may not arrive while the stream
   stays up, unlike `docs/site/src/content/docs/docs/high-availability.mdx`. This is
   not caused by this PR and deserves its own ticket.
7. **Smaller cadences left alone:** the 15 second status poll, the 30 second topology
   poll (deliberate for HA), the 1 second focus whoami guard, and one charged, audited
   self-config denial per Matrix mount for non-operators. Gating that last one on
   `useInstanceOperator` hides the notice in stories that render without
   `AuthProvider`.

8. **Unbounded signals refetch under sustained publishing.** Coalescing limits a
   burst of `cell.changed` events to about one refetch per key every 250 ms, with no
   per-minute bound. This predates the PR and an idle page never triggers it.

## Next steps

1. Confirm hosted CI on the final head, including the two rewritten flows.
2. Merge after review, then verify on a deployed instance that the Matrix steady
   state is about 11 requests per minute and that no 429s appear.
3. Decide item 1, then item 2 for each deployment.
