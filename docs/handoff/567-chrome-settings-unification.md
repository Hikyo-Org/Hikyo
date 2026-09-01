# #567 — Chrome + settings unification (handoff)

Spec: `docs/superpowers/specs/2026-09-01-chrome-settings-invite-design.md` (PR1 section).
Plan: `docs/superpowers/plans/2026-09-01-chrome-settings-unification.md`.
Follow-up: #568 (member invitation) stacks on this branch.

## What changed

**One navigation table.** `web/src/app/navigation.ts` now types `section` as
`'project' | 'instance' | 'organisation' | 'account' | null` and exports
`sectionsFor(section)` and `needsProject(surface)`. The matrix, machine-access
and project-settings surfaces are `project`-sectioned; `instance-admin` and the
new `instance-members` are `instance`-sectioned. Table order is sidebar order,
which is why `project-settings` moved after `machine-access` in the table.

The comments on the project surfaces used to justify `section: null` with "no
static sidebar entry could know which project". That premise is reversed on
purpose: the sidebar has project context now and fills `:org`/`:project` from
the route exactly as it filled `:org` before.

**Sidebar model.** `web/src/routes/sidebar-model.ts` turns (matched surface,
active org, route project, remote, operator flag) into `SidebarModel`:
`context` (project or instance block), `organisation` (never hidden), `instance`
(operator-only, mobile drawer, null when it already is the context) and
`account` (mobile drawer). `Shell.tsx` renders those blocks for desktop and the
drawer alike; `ProjectNavigation` and `MobileAccountNavigation` are gone.
`isLinkActive` decides the members/`?project=` projection pair, which share a
pathname and would both light up under NavLink.

CSS: `.project-sidebar*` → `.context-sidebar*` (same measured values, so the
pinned geometry holds). The organisation block is `.sidebar__section--organisation`.

**Every scope is a {Members, Settings} pair.** `Members` takes
`scope: { kind: 'org' } | { kind: 'instance' }`. Instance mode reads
`useInstanceGrants`, offers the single instance scope option, and renders the
403/404 refusals as their own states. `/instance/members` is the new surface;
`/instance` is "Instance settings" and lost its inline grants panel, the
"Connected instances · exploration" card, the prototype-only fake rows and the
design-rationale lede. Prototype mode renders the real panels; the mock gained
empty-list fixtures for OIDC/SAML/SP-key/federation and stubs for the rotation
and re-encryption POSTs.

**Shared modifiers replace per-id CSS.** `#instance-*`, `#account-profile`,
`#account-factors > .settings-row`, `#project-metadata` overrides are gone;
`.settings-row--compact`, `.settings-row__title--link`, `.panel--tight`
(`Panel tight`) carry the intent. `.origin-chips` had no remaining user.
Left as-is: `#account-factors > h3:first-of-type { display: none }` — a content
decision, not a density drift; fold it into a class if that panel is next touched.

**Spelling:** en-GB "Organisation" in every user-visible string.

## Pins that moved (and why)

- `shell.spec.ts`: heading `Organisation`; Machine access and Members now
  present in the project nav; Audit visible in project mode; no Account or
  Instance links in the desktop sidebar; rail action is `Instance settings`;
  new desktop test for the instance context block.
- `members.spec.ts`: project nav link is `Members`; h1 `Members · payments`;
  mobile region `Organisations`.
- `matrix.spec.ts` / `scanning.spec.ts`: `.context-sidebar*` selectors; the
  members click is scoped to the Project nav (two `Members` links exist on a
  project route by design).
- `instance-admin.spec.ts`: h1 `Instance settings`; the two instance-grant
  tests moved to `/instance/members` and drive the Members dialog; the
  second-factor panel loop lost `#instance-grants` and gained a `/instance/members`
  check; pinned-set targets `.factor__meta`/`.badge` → `.settings-row__detail`,
  `.instance-cli`, `.settings-tag`; new pinned block for `instance-members`
  (rides this spec — closure rule: a new surface's flow must be in a `ci.yml`
  group on main).
- `settings.spec.ts`: `Organisation settings ·`.
- `web/e2e/registry.ts`: the instance-admin flow claims `instance-members`.

## Screens (prototype mode, 2026-09-01)

- `567-screens/567-desktop-org.png` — org route: organisation block only, rail cog + account foot.
- `567-screens/567-desktop-project.png` — project route: project context block (matrix + groups, machine access, members, settings) stacked above the organisation block.
- `567-screens/567-desktop-instance-members.png` — `/instance/members`: instance context block above the organisation block; Who-can + Members panels; breadcrumb `hikyo / Instance / members`.
- `567-screens/567-desktop-instance-settings.png` — `/instance` as Instance settings with the Members entry-point panel.
- `567-screens/567-mobile-drawer-project.png` — mobile drawer on a project route: switcher, project block, organisation block (Projects / Instance / You sections follow below the fold).

## Seam for #568

Both Members scopes render `panel__actions` with `New grant`; the Invite button
and the Reset-credential row action go there. Instance mode already knows its
scope option and names; org mode has `org`/`projectId`. A display-once panel
shared by invite and reset belongs beside `Sections.tsx`.

## Running it

```bash
eval "$(fnm env)" && fnm use 24
pnpm --dir clients/ts install --frozen-lockfile
pnpm --dir web install --frozen-lockfile
pnpm --dir web typecheck && pnpm --dir web test && pnpm --dir web build
export NODE_OPTIONS=--dns-result-order=ipv4first
# Seven ports per run (defaults 45789–45795); claim a free block per session.
HIKYO_E2E_PORT=45900 HIKYO_E2E_PORT_B=45901 HIKYO_E2E_PORT_TLS=45902 \
HIKYO_E2E_PORT_OIDC=45903 HIKYO_E2E_PORT_OPERATIONAL=45904 \
HIKYO_E2E_PORT_OPERATIONAL_B=45905 HIKYO_E2E_PORT_OIDC2=45906 \
  pnpm --dir web exec playwright test --project=desktop
# same block again for --project=mobile (runs are sequential)
```

Pick ports nobody else on the machine is using (`lsof -nP -iTCP -sTCP:LISTEN`).
