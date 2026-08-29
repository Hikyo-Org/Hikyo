# #510 — Matrix advisory stream (SSE) with polling fallback

Implementing ticket: [#510](https://github.com/Hikyo-Org/Hikyo/issues/510) —
[P1] Adopt SSE advisory stream for matrix signals; demote 2s per-env polling to fallback.

## What shipped

Frontend-only. The server needed nothing: the advisory channel was already
end-to-end (`internal/server/revisions.go` `WatchProjectEvents`, fan-out in
`internal/service/advisory.go`, generated `watchProjectEventsOp`).

- `web/src/api/advisory.ts` (new) — the stream boundary:
  - `parseAdvisoryEvent` parses each stream payload against the wire shape of
    `wireAdvisory` (`internal/server/revisions.go`). Unknown event types are
    SKIPPED (advisory channel is additive; an unknown fact must not kill
    delivery); a known type with a malformed body throws (parse-don't-cast).
  - `watchProjectAdvisoryStream` drives the generated op with an
    `AbortController`. Two reconnection layers: hey-api's fetch-SSE client
    retries failed attempts internally (`onSseError` per failure); a CLEAN
    server close (slow-client drop, shutdown) ends the iteration and this
    loop re-subscribes with jittered backoff (1s base, 10s cap). Abort is the
    whole teardown — route leave aborts, nothing lingers.
  - `useAdvisoryStream` owns the subscription per `useMatrixProject` mount:
    opened on mount, aborted on unmount/route change; connection state is
    React state.
- `web/src/api/matrix.ts`: `advisoryInvalidations(ref, event)` maps events to
  exactly the cache prefixes named: published → values + signals +
  pendingDrafts of the event's environment; cell.changed → signals (the
  signals queryFn's `signalsRequireValuesRefresh` cascade still decides when
  values must follow); pending.staged → pending drafts + signals. The
  signals queries read `refetchInterval: signalsPollInterval(state)` — poll
  2s only while the stream is not healthy.
- `web/prototype/mock-api.ts`: a never-ending heartbeat stream for
  `/events`, so the prototype goes healthy instead of reconnecting forever
  against a 404.
- `web/e2e/flows/matrix.spec.ts`: three flows — zero periodic signals
  requests on an idle healthy tab + teardown on client-side route leave;
  fallback poll activates on a blocked stream and stops on recovery;
  a publish on a SECOND page reaches an idle page without a reload.

## Decisions worth remembering

- **Fetch-based SSE over native `EventSource`**: the generated op is
  hey-api's fetch transport, which runs request interceptors — that is what
  lets the workspace tier's bearer header ride the stream (native
  EventSource cannot carry an Authorization header, the same reason
  remotes.ts stays on polling). Cookie sessions need nothing extra.
- **Unknown event types are skipped, known types are strict.** The channel
  is advisory-only and additive; the fallback refetch covers anything a
  skipped event would have conveyed.
- **No replay handling in the client.** There is no `Last-Event-ID` resume
  anywhere; freshness during any gap belongs to the fallback poll.
- `pending.staged` is wired too (draft list + signals), beyond the ticket's
  two event types — it is the same stream, one extra invalidation, and it
  keeps other-principal write-presence live while polling is off.

## Verification at time of writing

- `pnpm --dir web run typecheck`, `pnpm --dir web run test` (392 tests, 58
  files) green locally.
- Playwright flows (desktop + mobile) run in CI; the new tests assert the
  request counter, fallback activation/recovery, and cross-tab delivery
  against the real Go binary.

## Non-goals / follow-ups

- The workspace matrix's SSE was not additionally asserted in the
  two-instance flow (CORS + bearer are proven generally; a workspace failure
  degrades to the fallback poll, which is today's behavior).
- The prototype mock never emits events; mutations still self-invalidate.
