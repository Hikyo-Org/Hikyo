# Sidebar: instance tier at the foot

Issue: none, owner request
PR: #1219

## Result

The web sidebar now reads narrow to wide: project, organisation, instance. The
instance scope is a persistent tier ("This instance") at the foot, above the
version line. Remotes moved from the organisation block to the instance section
because it is instance-wide (`/api/v1/instance/remotes`, capability
`instance-directory`). The desktop rail cog is gone; the tier replaces it as the
way into instance pages.

## What changed

- Routes: `/remotes` is now `/instance/remotes` (`web/src/app/navigation.ts`,
  section `instance`, first in the table so tier rows do not jump). The
  catch-all redirects legacy `/remotes` to the new path keeping query and hash
  (`NotFoundOrMoved` in `web/src/app/App.tsx`); it is not a surface.
- Sidebar model (`web/src/routes/sidebar-model.ts`): `context` is project-only;
  `instance` is the tier, collapsed to Remotes plus one entry row off instance
  routes and expanded in place on them. Blocks carry `qualifiers` for headings.
- Shell (`web/src/routes/Shell.tsx`): `InstanceContext`, the rail cog and the
  `context-sidebar__org` row are removed. The organisation heading is
  `Organisation · <name>`. The prototype-only org role label is gone (the
  prototype header already states it). Tier and version sit in one
  `.sidebar__foot` group, pinned to the bottom on desktop viewports at least
  640px tall and static in the drawer and on shorter screens. The tier's region
  is named `Instance pages` because the Remotes page has its own `This instance`
  card. `FleetUpdateNotice` now links to `surfaceById('remotes').path` (it hard-coded
  `/instance/remotes`, which matched no route before the move).
- Remote workspace: organisation links with the org id in the path are disabled
  with the project block's local-only reason (they used to open a local page
  with the remote's org id). Headings name the boundary. The tier stays local,
  and a local row is never marked current on a remote page at the same path.
  The breadcrumb's project crumb keeps `?remote=`.
- Whoami: additive `capabilities.instance_directory` (OpenAPI, service, server
  mapping, generated Go and TypeScript). It is evaluated with
  `authz.OpRemoteList`, the operation behind `GET /api/v1/instance/remotes`,
  and reflects what the session can exercise: the grant and its second factor.
  `instance_operator` and `instance_audit_read` are documented the same way, and
  the wire test covers a password-only session for all three.
  The web session fingerprint now
  also tracks `instance_audit_read` and `instance_directory`.
- Docs: `DESIGN.md` Side menu, `browser-operations.mdx`.

## Decisions

- Remotes row: `isInstanceOperator || canReadInstanceDirectory`. An operator
  holds `instance-config`, not `instance-directory`, so their directory read is
  refused by the server, but add remote, credential minting and the allowlist
  (all `instance-config`) live on that page and the page handles the refusal.
- Other tier rows: settings, configuration and members are operator-only;
  audit is operator or audit reader. No instance grants means no tier.
- Foreign-origin links in `Remotes.tsx` keep the legacy `/remotes#...` path on
  purpose: the other instance may predate the move, newer ones redirect it.
- `instance_operator` still gates the operator polls; unchanged.
- A password-only directory holder sees no tier until they step up, because the
  hint reads false without the second factor.

## Evidence

Local, on the final tree unless noted:

- `scripts/ci/fast-feedback.sh --working-tree` on Node 26.12.1: passed, including
  2264 web unit tests, client tests, typecheck, lint, Go compile and docs.
- Playwright `shell.spec.ts` and `workspace.spec.ts`: 45 passed and 2 skipped on
  desktop. `shell.spec.ts` on mobile: 26 passed and 3 skipped.
- Static demo suite (`build:demo`, `test:demo`): 91 passed.
- `instance-admin.spec.ts`: passed on the candidate before the last review
  repairs (83 desktop and 64 mobile across the three specs). Those repairs
  changed the focus scroll padding, an empty `?remote=` and hint wording.
- `TestInstanceAuditWire` passed on SQLite. The Postgres variant is skipped
  locally, so hosted CI is its first run.
- Screenshots are on the PR at matching routes and viewports. They come from the
  prototype mode, which seeds an operator; `?remote=ew` shows the unknown-remote
  error because the prototype configures no remote, so a live remote workspace
  was not exercised in a browser.

## Open points for the owner

- The tier is pinned only on desktop viewports at least 640px tall. It scrolls
  with the list in the drawer and on shorter screens.
- A password-only `instance-directory` holder sees no tier until step-up.

## Remaining work

- Revisit the foreign-origin `/remotes` links once the minimum supported remote
  version includes this change.
