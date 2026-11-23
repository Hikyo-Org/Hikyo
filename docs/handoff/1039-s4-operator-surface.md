# #1039 slice 4: operator surface (service, API, CLI, web)

Owner: [#1039](https://github.com/Hikyo-Org/Hikyo/issues/1039), with
[#1471](https://github.com/Hikyo-Org/Hikyo/issues/1471) for the rotation item.
Stacked on slice 3 (`feat/1039-s3-owner-functions`, PR #1538); merge that first.

## Result

- API (revision 21, additive): `openIncident`, `getIncident`, `listIncidentItems`,
  `resumeIncident`, `attestIncidentItem`, `closeIncident` under `/api/v1/incidents`.
- CLI: `hikyo incident open|show|items|resume|attest|close`, human session only.
- Web: Incidents at `/orgs/:org/incidents` with stories aligned to the route.
- An item its owner refused is stored on the row as `pending/owner_failed`, apart from
  `pending/` (not yet run). No migration: the reason vocabulary held the word. PR #1538's
  SQLite tests that asserted `pending/` for a refused item now assert `pending/owner_failed`.
- A rotation item's state (`active`, `paused`, `halted`, `disabled`, `gone`) is read live
  when the page is built and never stored. A caller without authority over the rotation's
  scope gets no state, so a deleted rotation does not leak to them.
- `attestable` on an item is the server's verdict; the web offers Attest only where it is true.

## Probe class

The routes carry `x-hikyo-class: unauthenticated` (credential-reset precedent). No tenant or
instance class fits: the authority is org for a single-org account and instance otherwise,
decided by the service from the target, and an instance incident has no org. The reasoning is
recorded in the incident ADR and in the tenant-isolation ADR. The route probe is
`TestIncidentWireRefusalsAreUniformAcrossTenants`: byte-identical `404` for a foreign and a
nonexistent incident on get, items, resume, attest, close, and for opening a foreign account.

## Evidence

- Real stack: `TestIncidentWireLifecycle` (six routes, paging, attest, resume, close),
  `TestIncidentCLIExercise` (the verbs over TLS), both engines.
- Service: `TestIncidentAuthorizationRefusals` (including item views),
  `TestIncidentRotationViewGoneAndUnreadable`, the owner tests on `pending/owner_failed`.
- Web: unit tests, Storybook stories in dark and light, and the Playwright flow
  `incident containment` (open, ledger, resume, close, reload) at desktop and mobile on a real
  instance.
- Before and after screenshots stay out of Git, kept locally in the task scratchpad
  (`incidents-{before,after,open-form}-{desktop,mobile}.png`).

## Not done

- No instance-scope or list-incidents route: the page is driven by an `?incident=` id.
- Release, restore reconciliation and the recovery rehearsal are slice 5.
- Maintainer security review and the human merge gate remain required. Cross-provider review
  did not run and is not CLEAN.
