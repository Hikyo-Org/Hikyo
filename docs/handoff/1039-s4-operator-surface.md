# #1039 slice 4: operator surface (service, API, CLI, web)

Owner: [#1039](https://github.com/Hikyo-Org/Hikyo/issues/1039), with
[#1471](https://github.com/Hikyo-Org/Hikyo/issues/1471) for the rotation item.
Stacked on slice 3 (`feat/1039-s3-owner-functions`, PR #1538); merge that first.

## Result

- API (revision 22, additive): `openIncident`, `getIncident`, `listIncidentItems`,
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

The routes carry `x-hikyo-class: unauthenticated` with the human-session requirement, an
owner-accepted exception (follow-up [#1569](https://github.com/Hikyo-Org/Hikyo/issues/1569)
adds a dedicated class). The rationale is narrowed: only an account-kind incident and an
instance incident have an authority that is decided dynamically by the service from the
target, so no tenant or instance class fits those; the ADRs record it. The route probe is
`TestIncidentWireRefusalsAreUniformAcrossTenants`: byte-identical `404` for a foreign and a
nonexistent incident on get, items, resume, attest, close, and for opening a foreign account.

## Resume

- Resume performs the containment the ledger enumerated, so it needs a fresh account-security
  proof in the request (`proof`): a TOTP code (spent by the call, so a replay is refused) or,
  for an account with no confirmed factor, the password (checked fresh each time, not spent).
- The request also carries `items_digest`, the SHA-256 of the sorted ids of the open items the
  operator confirmed. The server reads every open id in the write transaction that spends the
  proof and answers `409` if the set differs (same size included), leaving the proof untouched.
  A matching call runs exactly those ids. CLI and web both send the digest of what they
  displayed; the web dialog, on a `409`, refreshes its list and asks again (no auto-retry).
- A wrong proof on the web reads as a refused proof, not "Your session ended".
- Limitation: a federated-only account has no password or enrolled factor, so it has no proof
  path and cannot resume. Owner decision (#1590 tracks step-up): the server refuses with `400`
  detail `no_proof_credential`, an empty proof included, and there is no proof-less path.

## Evidence

- Real stack: `TestIncidentWireLifecycle` (six routes, paging, attest, resume, close),
  `TestIncidentCLIExercise` (the verbs over TLS), both engines.
- Service: `TestIncidentAuthorizationRefusals` (including item views),
  `TestIncidentRotationViewGoneAndUnreadable`, the owner tests on `pending/owner_failed`.
- Resume: `TestIncidentResumeNeedsAFreshProof`, `TestIncidentResumeRefusesAChangedOpenSetWithoutSpendingTheProof`,
  `TestIncidentResumeOverHTTPWithAValidProof` (valid TOTP code, a digest below and above the real one, replay), both engines; the same-size swap is in `TestIncidentResumeRefusesAChangedOpenSetWithoutSpendingTheProof`;
  the wire tests assert the weak session's `grant.denied` row and a late-added item (`Added == 1`).
- Web: unit tests (including the refused count, the digest the resume request carries, and a wrong proof), Storybook stories in dark and
  light. The Playwright flow `incident containment` (open, ledger, resume with a valid TOTP
  proof, close, reload) is described in the PR body with its actual last run result.
- Before and after screenshots stay out of Git, kept locally in the task scratchpad
  (`incidents-{before,after,open-form}-{desktop,mobile}.png`).

## Not done

- No instance-scope or list-incidents route: the page is driven by an `?incident=` id.
- Release, restore reconciliation and the recovery rehearsal are slice 5.
- Maintainer security review and the human merge gate remain required. Cross-provider review
  did not run and is not CLEAN.
