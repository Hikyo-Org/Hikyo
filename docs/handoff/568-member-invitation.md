# #568 — Member invitation: local-credential invite at org and instance scope (handoff)

Spec: `docs/superpowers/specs/2026-09-01-chrome-settings-invite-design.md` (PR2 section).
Plan: `docs/superpowers/plans/2026-09-02-member-invitation.md`.
Stacks on #567 (PR #569, merged).

## Decision (locked 2026-09-01)

Local-credential invitation: a `manage-members` holder creates the human
account and mints a credential-establishment authority for it, bootstrap minus
the first-account check, display-once like `credential-reset`, claimed through
the existing public `establishCredential`. OIDC-identity invitation stays a
future decision; `service.ErrNoInvitationPath` remains its seam, untouched.

## What landed, per layer

**Contract.** `POST /api/v1/orgs/{org}/invitations` (`manage-members@org`,
tenant class) and `POST /api/v1/instance/invitations`
(`manage-members@instance`). Body `InviteMemberRequest {username, display_name?,
template?}`; `201 InvitationResult {principal_id, account_id, authority,
expires_at}`; `409 conflict` on a taken username; uniform 403/404 for a scope
the caller may not invite into. `establishCredential`'s issuer list gains
"invitation". Pins: `api/noproxy_test.go` `pinnedContractSurface` (+2),
oapi-codegen, the TS client and `@hikyo/operations`.

**Authz and audit.** Operations `member.invite-org` / `member.invite-instance`
(`OpMemberInviteOrg` / `OpMemberInviteInstance`), each a superset of the
template-apply operation at its depth because the optional template rides the
same writer. New event `member.invited` on the scope's trail (org trail for an
org invite, instance trail for an instance invite) with
`{principal_id, account_id, username, scope, template?, grants_created,
authority_id, delivery}`. `auth.credential_authority_minted`'s `issued_by`
enum gains `invitation`. Route classification, the wire-registry snapshot and
`internal/isolation/testdata/operation_formulas.json` moved with them.

**Store.** Migration `00037_invitation_issuer` (both dialects) rebuilds
`credential_authorities` so the `issued_by` CHECK admits `invitation`; the
first isolation run against the sibling's uncommitted service caught this
(sqlite cannot alter a CHECK, hence the 00006-style table rebuild).
`authn.Resolver.CreateAccount` now folds the `accounts.username` UNIQUE
violation onto `domain.ErrConflict` by typed extended code on both engines,
so every account creator (bootstrap, SCIM, invitation) answers one refusal
instead of a driver string. That is what makes the 409 real.

**Service.** `Grants.InviteMember(ctx, actor, InviteSpec)` in one `tx.Write`:
resolve, authorize, create principal + account, expand the optional template
through `applyTemplate` (origin `manual`, subject = caller), mint the
authority (`IssuedBy: invitation`, `ResetLifetime`), record the mint on the
instance trail and `member.invited` on the scope's trail. A project scope is
`ErrInvalid`; a blank username is `ErrInvalid`.

**Server.** `InviteOrgMember` / `InviteInstanceMember` on `*API`
(`internal/server/access.go`), `GrantService.InviteMember`, stub in
`contract_test.go`, the org route in the uniform-refusal table.

**CLI.** `hikyo access member invite <username> [--display-name NAME]
[--template T] [--org O | --instance-scope] [--output-file PATH |
--dangerously-print]` through the `disclose` sink, prepared before the request.
Stderr prints the establish hint (`hikyo account establish-credential
--instance <origin> --as <username>`) and the browser URL `<origin>/establish`.
A project address is a usage error. The "member invite is NOT implemented"
paragraph in `internal/cli/access.go` is gone; help golden updated.

**Web.**
- `web/src/routes/InviteDialog.tsx`: Invite ceremony (username, display name,
  role template with "No initial grants" default, templates admitted at the
  scope) and the exported `IssuedAuthorityDialog` (display-once panel:
  authority, principal id, expiry, copy, CLI hint, `.btn` link to
  `/establish`). `inviteMember` and `resetCredential` in `web/src/api/access.ts`
  are plain async calls with `parsedPick`, never a TanStack mutation (a
  mutation cache retains the value; #498 discipline).
- `Members.tsx`: Invite button at org and instance scope (not on the project
  projection: a project has no accounts of its own); Reset credential row
  action reusing the same panel, shown for human principals only (`mch_` is a
  machine), never for yourself (a reset revokes your own sessions), once per
  principal. Escape on either dialog is a cancel/close, not a zombie.
- `web/src/routes/EstablishCredential.tsx` at `/establish` (registry surface
  `establish-credential`, public, chromeless): authority + password + repeat,
  local refusal on mismatch and on a password under 12 characters, uniform
  refusal text for anything the server refuses, success state with a Sign in
  link. The login card links to it with a `.btn` (44px mobile pin).
- `DisplayOnceCopy` moved from `AccountSecurity.tsx` to `Sections.tsx`.
- Prototype mock serves both invitation POSTs (username `taken` answers 409),
  the credential reset and the establish route.

## Pins and fixtures that moved

- `web/e2e/registry.ts`: the `login` flow claims `establish-credential`; it
  rides `login.spec.ts` (group 1 on main) because a spec file a PR adds to a
  `ci.yml` group never runs on that PR. `e2e/registry.test.ts` expectation
  updated.
- `web/src/app/navigation.test.ts`: `establish-credential:public:none` and the
  anonymous-reachable list.
- `web/e2e/fixtures/seed.ts`: the seeded administrator gains `credential-reset`
  (break-glass, instance scope, before any session is minted). Bootstrap is
  the operator template plus `manage-members`, which does not include
  `credential-reset`, so the reset row action was untestable without it.
- `Login.oidc.test.tsx` mounts Login under a `MemoryRouter` (Login renders a
  `Link` now).
- `internal/authz/wire_registry_test.go` counts (292 → 294 wire entries,
  202 → 204 operation-linked).
- `internal/isolation/grants_e2e_test.go` `runGrantLifecycle` invites once so
  the runtime "every registered type is emitted" gate sees `member.invited`.
- `internal/config/config_test.go`: pre-existing gofmt drift fixed in passing.

## Tests

- Go: `internal/isolation/invite_e2e_test.go` (both engines): org invite with
  a template (grants, both trails, trimmed handle), establish + login, spent
  authority refused, duplicate username is `ErrConflict` with nothing left
  behind, instance invite without a template, project scope and blank username
  invalid, an editor cannot invite. Contract test covers the uniform refusal on
  the org route; CLI tests cover the display-once destination and the
  positional grammar.
- Vitest: `InviteDialog.test.tsx` (template options per scope, local empty
  refusal, invite renders the authority outside every cache, 409 keeps the
  form, Escape cancels), `EstablishCredential.test.tsx` (mismatch and short
  password refused locally, 204 success clears the authority, 401 voiced
  uniformly).
- Playwright: `members.spec.ts` invites with `viewer`, claims on `/establish`
  from a cookie-less context, signs in as the invitee, is refused on a second
  claim, resets from the row action and claims again; invite dialog pinned set
  both schemes. `instance-admin.spec.ts` invites an `operator`, asserts the
  inviter's `manual` origin on every line, refuses the same username inline,
  revokes the lines afterwards. `login.spec.ts` pins `/establish` in both
  schemes, the local mismatch refusal and the uniform unknown-authority text.

## How to run

```
go test -count=1 ./...
pnpm --dir clients/ts install --frozen-lockfile   # fresh worktree, else zod is unresolved
cd web && pnpm run typecheck && pnpm vitest run && pnpm run build
NODE_OPTIONS=--dns-result-order=ipv4first HIKYO_E2E_PORT=45910 ... pnpm exec playwright test --project=desktop
pnpm exec playwright test --project=mobile   # sequential: desktop and mobile share web/e2e/.auth
```

## Notes for the next person

- A template-less invite produces NO membership row: rows derive from grants.
  The success notice says so. Invite with a template when a row must appear.
- A target holding an instance capability has no network reset path
  (break-glass only), so the instance flow never resets its operator invitee.
- Reset refusals are one uniform 401 on the wire; the UI voices them as one
  sentence and never distinguishes "no such account" from "not yours".
- The invitee account persists in the e2e datastore (there is no account
  deletion); usernames are unique per run and project.
- Session split: two t3code sessions worked this ticket on one branch. The
  first built the contract, registries, service, server and CLI (its plan
  Tasks 1–5) and went silent with that work uncommitted in its worktree; the
  second (web + e2e) adopted the uncommitted hunks verbatim, added the
  migration and the conflict folding the isolation run demanded, and owns the
  PR, the Codex gate and the merge.
