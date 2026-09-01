# Chrome, settings family and member invitation — design

Date: 2026-09-01. Status: approved by Marc (design questions answered
2026-09-01), awaiting implementation plan.

## Problem

Three consistency gaps in the SPA (`web/`):

1. **Sidebar has two sources of truth.** Org-scoped routes render `SECTIONS`
   derived from `web/src/app/navigation.ts`; project-scoped routes render the
   hand-written `ProjectNavigation` JSX in `web/src/routes/Shell.tsx`; the
   mobile drawer adds a third hand-written list (`MobileAccountNavigation`).
   Drift that follows: labels differ per mode ("Members" / "Org members &
   grants" / "Members & access"; "Organisation settings" / "Org settings"),
   spelling differs (en-GB `Organisation` in the table, `Organization` in
   several headings), project mode loses Overview, Projects, Remotes, SCIM,
   Audit, Instance and Account on desktop, `machine-access` is in no sidebar,
   instance administration is reachable three ways and account & security
   three ways.
2. **Member settings differ per scope.** Org members live on
   `/orgs/:org/members` (list, Who-can inspector, New-grant modal). Instance
   grants live as an inline panel on `/instance` drawn with the account page's
   `factors`/`factor`/`badge` classes and a bare principal-id form. Project
   members are the org page with `?project=`.
3. **Nothing invites.** The org Members "✉ invite member" button is
   prototype-mode only and toasts "not implemented in the API yet".
   `internal/cli/access.go` records `member invite` as unimplemented because
   claim flow, delivery and expiry were undecided; the seam is
   `service.ErrNoInvitationPath`. The human-auth ADR names invitation as THE
   account-creation path ("Accounts are created by invitation from a
   `manage-members` holder, or by the bootstrap path"). Today a second human
   exists only via SCIM, OIDC JIT, or nothing.

Also in scope (campsite rule): `InstanceAdmin.tsx` ships prototype fiction on
production paths (fake rotation dates, fake connected instances, hardcoded
policy rows, a lede that is internal design rationale) and per-id CSS
overrides (`#instance-orgs`, `#instance-settings`, `#account-profile`,
`#project-metadata`, `app.css` ~4246–4298) that let pages drift.

## Decisions (locked 2026-09-01)

| Question | Decision |
|---|---|
| Invite mechanism | **A. Local-credential invite.** Create human account + mint a credential-establishment authority (bootstrap minus the first-account check), display-once like `credential-reset`, claimed by the existing public `establishCredential`. Both org and instance scope. OIDC-identity invitation (pending-invitation artifact claimed at callback) stays a separate future decision; `ErrNoInvitationPath` remains its seam. |
| Sidebar shape | **A. Stacked context.** Context block (project or instance) on top when the route is so scoped; Organisation block always below. Instance admin and Account leave the desktop sidebar (rail ⚙ + rail-foot account menu are canonical); the mobile drawer keeps "Instance" and "You" sections derived from the same table. |
| Instance members | **A. Same page family per scope.** New surface `/instance/members` = the Members page at instance scope. `/instance` becomes "Instance settings". Every scope is a {Members, Settings} pair. |
| Spelling | en-GB **Organisation** everywhere (130 vs 11 occurrences in `web/src` today). |
| Delivery | Two PRs. PR1 web-only (chrome + settings + instance members) — [#567](https://github.com/Hikyo-Org/Hikyo/issues/567). PR2 stacked on PR1 (invite vertical) — [#568](https://github.com/Hikyo-Org/Hikyo/issues/568). |

## PR1 — chrome and settings unification (web only)

### Navigation table

`navigation.ts` `section` becomes a closed union:
`'project' | 'instance' | 'organisation' | 'account' | null`.

| Surface | section | Sidebar label |
|---|---|---|
| overview | organisation | Overview |
| projects | organisation | Projects |
| remotes | organisation | Remotes |
| members | organisation | Members |
| audit | organisation | Audit |
| scim | organisation | SCIM provisioning |
| org-settings | organisation | Organisation settings |
| matrix | project | Environment matrix |
| machine-access | project | Machine access |
| project-settings | project | Project settings |
| instance-admin (`/instance`) | instance | Instance settings |
| instance-members (`/instance/members`, NEW) | instance | Instance members |
| settings (`/settings`) | account | Account & security |
| history, key-detail, values | null | reached from the matrix |
| login, ceremonies, callbacks | null | — |

The project block also carries a synthetic "Members" link to
`/orgs/:org/members?project=:project` (the existing filtered projection; not
a surface). The comments on `matrix`, `machine-access` and `project-settings`
that justify `section: null` with "no static sidebar entry could know which
project" are rewritten: the sidebar now has project context and fills the
parameters exactly as it fills `:org` today.

`SECTIONS` becomes `sectionsFor(section)` and the mobile drawer lists are
derived from the same table. `needsOrg`/`needsProject` are path-derived as
today.

### Shell rendering

```
desktop sidebar            mobile drawer
─────────────────          ─────────────────
[context block]            Organisations (switcher)
  · project OR instance    [context block]
[Organisation block]       [Organisation block]
version                    Projects (switcher)
                           Instance (if operator)
                           You
                           version
```

- Context block header keeps the locked geometry: org avatar 28px + org name
  + role line; `<h2>Project · {name}</h2>` or `<h2>Instance</h2>`; links
  38px min-height, 13px. `.project-sidebar*` classes rename to
  `.context-sidebar*`; measured values unchanged so the pinned assertion set
  holds.
- Project block: Environment matrix (+ the groups jump list published by
  `useProjectSidebar`, unchanged), Machine access, Members, Project settings.
  Remote workspace (`?remote=`) keeps the honest disabled "local only"
  rendering for Members and Project settings; Machine access joins that list.
- Organisation block is never hidden. The entry is absent (not dead) while no
  organisation is active, as today.
- Instance block is rendered only for `instance_operator` and only while the
  route is instance-scoped. Desktop entry point stays the rail ⚙ (`/instance`).
- Account: the desktop sidebar no longer lists it; the header "Signed in as"
  link and the rail-foot account menu are canonical. Mobile drawer "You"
  section stays.
- Breadcrumb: `hikyo / {org} / [{project}] / {surface}` and
  `hikyo / Instance / {surface}` for instance routes.
- `chooseOrg` while on an instance route: no navigation (instance routes carry
  no `:org`). `chooseProject` unchanged.

### Settings family

Page pattern is already shared (`page page--chrome` · `<h1>` · `page__lede` ·
`JumpIndex` · `Panel`s · `settings-row`). Changes:

- **`Members.tsx` parametrised by scope**:
  `{ kind: 'org', org } | { kind: 'org', org, project } | { kind: 'instance' }`.
  Instance reads `useInstanceGrants()`, scope options `[instance]`, names from
  `useInstanceOrgs()`; Who-can inspector and `GrantModal` unchanged in shape.
  `h1` = `Members · {org name | project name | Instance}`. The prototype-only
  invite button is removed in PR1 (PR2 brings the real one).
- **`/instance/members`** is a new surface (`ELEMENTS`, `navigation.ts`,
  e2e registry). Its flow rides `instance-admin.spec.ts` (already in a
  `ci.yml` group on main), per the closure rule.
- **`InstanceAdmin.tsx` → Instance settings**: Organisations, Policy
  (credential ceiling), Identity providers, Federation, Retention health,
  Keys & crypto, SAML providers, SP signing keys. Removed: the instance grants
  panel (moved), the "Connected instances · exploration" card, the
  prototype-only fake rotation rows, the hardcoded policy rows, and the lede
  ("Full CLI ↔ UI parity (decided round 3)…") replaced by one plain sentence.
  Prototype mode renders the real components; `web/prototype/mock-api.ts`
  gains any instance route the real components read that it does not serve
  yet (credential-policy, retention-health, rotation stubs).
- **CSS**: per-id overrides folded into shared modifiers (`.panel--list`,
  `.settings-row--stacked` already exists, etc.). No new tokens.
- **Spelling sweep** to Organisation in user-visible strings and headings;
  identifiers untouched.

### Tests (PR1)

- Vitest: `navigation.test.ts` — `sectionsFor` per section; every
  `chrome: 'shell'` surface with a non-null section appears exactly once; no
  `null`-section surface appears.
- Playwright `shell.spec.ts`: desktop composition pins updated (heading
  "Organisation"; Machine access present in the project nav; Organisation
  block visible while on a project route; no "Instance"/"Account & security"
  link in the desktop sidebar; rail ⚙ still present). Mobile drawer pins
  unchanged plus Instance members visible for the operator.
- Playwright `instance-admin.spec.ts`: the two instance-grant tests move to
  the new surface; pinned assertion set runs on `instance-members`.
- Playwright `members.spec.ts`: unchanged behaviour; label pins updated.
- Typecheck, lint, axe on every touched surface in both schemes.

## PR2 — member invitation (stacked on PR1)

### Contract

```
POST /api/v1/orgs/{org}/invitations       x-hikyo-formula: [manage-members@org]
POST /api/v1/instance/invitations         x-hikyo-formula: [manage-members@instance]
```

Request `InviteMemberRequest` (`additionalProperties: false`):

| field | type | notes |
|---|---|---|
| username | string, required | account username, unique among live accounts |
| display_name | string, optional | defaults to username |
| template | string, optional | a role template id valid at the invite's level; expands to grants at that scope |

Response `201 InvitationResult`: `{ principal_id, authority, expires_at }`.
`authority` is returned once, never re-displayed. Errors: 400 validation,
401, 403 (uniform refusal), 409 `conflict` on username collision, 429, 500.

`establishCredential`'s description, which lists issuers exhaustively
("bootstrap, credential-reset, or local break-glass"), gains "invitation".

### Service

`Auth.InviteMember(ctx, actor, scope domain.Scope, username, displayName, template, delivery) (InvitationResult, error)`
— one `tx.Write`:

1. `actor.resolve`; `az.Authorize(OpMemberInviteOrg | OpMemberInviteInstance, scope)`.
2. `az.CredentialEpoch`; `CreateHumanPrincipal`; `CreateAccount` (collision →
   `ErrAccountExists` → 409).
3. If `template` given: `domain.ExpandTemplate(template, level)`; one grant
   row per capability at `scope`; origin `manual` with subject = caller
   principal (the ADR's "any initial grants" in the same transaction).
4. `MintAuthority{Purpose: establish-credential, IssuedBy: "invitation", ExpiresAt: now + ResetLifetime}`.
5. Events: `auth.authority_minted` (`issued_by` enum += `invitation`) on the
   instance trail; new `member.invited` on the org trail (org invite) or
   instance trail (instance invite) with payload
   `{principal_id, username, template?, granted: [capabilities]}`. Both are
   emitted here, so the registry gains no unemittable event.

Authz registry: `OpMemberInviteOrg` (ClassTenant, LevelOrg,
`manage-members@org`) and `OpMemberInviteInstance` (ClassInstance,
`manage-members@instance`), events declared as above.

Username is validated with the bootstrap path's rule. No email, no delivery
channel: the authority is handed over out of band, exactly as
`credential-reset`. Expiry = `ResetLifetime`.

### CLI parity

`hikyo access member invite <username> [--display-name NAME] [--template ID] [--org ORG | --instance-scope] [--output-file PATH | --dangerously-print]`
through the same `disclose` sink as `account reset-credential`. Stderr
afterwards prints the establish hint:
`hikyo account establish-credential --instance <origin> --as <username>`.
The "member invite is NOT implemented" paragraph in `internal/cli/access.go`
is removed; help golden updated; `docs/spec/api-cli-spellings.md` and the
api-cli-surface ADR row already name the verb.

### Web

- **Invite** button on Members at org and instance scope (`panel__actions`
  next to New grant). Dialog (`ceremony` class): username, display name,
  role template select (templates at the invite level, "no initial grants"
  default). Submit → display-once result panel: authority (copy via the
  shared clipboard helper), expiry, the CLI establish hint, and a link to
  `/establish`. Plain async + parsed pick, not `useMutation` (display-once
  discipline from #498). Membership listing invalidated on success.
- **Reset credential** row action on Members (both scopes) calls the
  existing `resetCredential` and reuses the same display-once panel. Row
  action visible only for human principals.
- **`/establish`** — new public, chromeless surface `establish-credential`:
  authority + password + repeat → `establishCredential` → success copy
  ("credential set; sign in") with a link to `/login`. Uniform failure text.
  Login page's lede links to it ("Have a setup authority? Establish your
  credential").
- Prototype mock: the two invitation POSTs and `establish`.

### Pins and generated code

`api/noproxy_test.go`: `pinnedContractSurface` gains the two invitation
POSTs (the establish route is already pinned). oapi-codegen (`api/`),
TS client (`clients/ts/src/generated`), `@hikyo/operations` ops, e2e
registry (`establish-credential` rides `login.spec.ts`; invite and reset
assertions ride `members.spec.ts` and `instance-admin.spec.ts`).

### Tests (PR2)

- Go: service unit tests (authorize at both scopes, refusal without
  `manage-members`, username collision, template expansion with caller
  origin, epoch stamped, event payloads); isolation/conformance pins; CLI
  help golden and a verb test through the fake server.
- Web: vitest for the invite dialog state and display-once discipline;
  Playwright: invite at org and instance, establish through `/establish`,
  sign in as the invitee, reset credential, axe in both schemes.

## Out of scope

- OIDC-identity invitation (pending-invitation artifact, ADR § Identity
  linking). Named future decision; `ErrNoInvitationPath` stays.
- Email delivery of invitations.
- "Connected instances" management — its card is removed; the open question
  lives in the wayfinder ticket (#1), not in production chrome.

## Gates

Every commit DCO signed. Per PR: typecheck + lint + Go tests + Playwright
desktop and mobile; Codex review R1–R3 at high effort; preview verified;
human merge gate. Handoff doc committed in each PR under `docs/handoff/`.
