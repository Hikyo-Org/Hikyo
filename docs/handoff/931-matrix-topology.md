# Issue 931: Matrix environment topology

The Matrix now refetches its environment list on stream recovery and on an
opted-in `environments.changed` advisory event. A successful list reconciles
identity-bound query rows and retires removed environments' cached data,
including pending requests whose late responses must not refill the cache.
Failed list refreshes retain the last usable topology.

The original reproduction mounted the real hook with production query defaults,
added a backend environment, waited beyond stale time, then reconnected. It
observed no environment-list request and still rendered the two old rows.

## Contract

- Create, clone, rename, reorder and delete notify after commit. Definitions
  apply uses the same notification when it changes environment topology.
- A topology event contains exactly `type` and `project_id`. Each delivery
  requires current project read authority, not the weaker filtered navigation
  permission. Existing environment events retain their own projection.
- `Accept: text/event-stream; hikyo-topology=1` opts in. Legacy clients receive
  their existing event types and never encounter the new project-only envelope.
- Recovery includes the environment listing, once per recovery and never on
  first connection. A healthy stream still polls the small topology list and
  project catalogue every 30 seconds because the HA advisory bus is per node. This does not restore
  healthy per-environment signals polling or add a cross-node bus.
- New rows and reordered responses remain bound to environment IDs. Successful
  removal clears values, signals, pending drafts, settings, reveal-window
  metadata, revision lists/details/pins, approvals, access requests, SSH and PKI
  resources, delivery targets and dynamic leases from that query client's project
  scope. Other projects and workspace clients keep
  their own data. Secret reveals remain uncached one-shot operations.
- Stable query combination and memoized row projection remain intact.

## Reproduction and regression checks

From the repository root with its pinned Node version selected:

```sh
pnpm --dir clients/ts run verify
pnpm --dir web run typecheck
pnpm --dir web run lint
pnpm --dir web run test
pnpm --dir web run build
pnpm --dir web exec playwright test --project=desktop e2e/flows/matrix.spec.ts --grep 'matrix topology synchronization'
pnpm --dir web exec playwright test --project=mobile e2e/flows/matrix.spec.ts --grep 'matrix topology synchronization'
pnpm --dir web exec playwright test --config e2e/performance.config.ts
```

Real-hook tests cover healthy events and reconnect recovery for local and remote
transports, addition/rename/reorder/deletion, cross-client/project isolation,
late responses, failed-list recovery, empty topology, and healthy polling. The
browser regressions use separate authenticated principals and cookie jars,
change topology through the settings UI, and inspect the idle Matrix without
reloading it. Screenshots are Playwright attachments for each viewport/state.

## Local validation

- Pinned Node 26.7.0: 202 web suites, 1,773 tests pass. Web typecheck, lint and
  production build pass; generated TypeScript client checks pass.
- Two independent authenticated browser sessions: healthy and recovery flows
  pass on desktop and mobile. Both also verify that deleting a selected copy
  destination or source closes the real parent-owned editor. No page errors.
- The unchanged 1,000-key, 20-environment performance harness passes all four
  desktop/mobile checks, including timing negative controls. Worst measured
  post-warmup interaction: 75.7 ms desktop, 47 ms mobile. The unchanged
  ceilings are median <200 ms and maximum <500 ms.
- SQLite/PostgreSQL advisory conformance passes for direct mutations,
  definitions apply, rollback, current authorization, isolation and revoked
  browser/CLI sessions. Opt-in/legacy wire and existing SSE timing/cap tests pass.
- Independent Sol review found no remaining actionable findings after 44
  focused tests. The first review identified seven missing retirement families;
  all seven original probes now pass. Its separate editor finding was withdrawn
  after five actual-parent probes confirmed the existing lifetime boundary.
- Full-file sensitivity review verified the changed Matrix/settings data paths
  before updating their inventory hashes. No new plaintext cache is introduced.

Hosted exact-candidate and full merge-group validation remain required. These
local browser timings are not hosted benchmarks, and no multi-node HA runtime
was exercised. Real-hook bounded-polling tests cover a missing local topology
notification without introducing a new cross-node bus.
