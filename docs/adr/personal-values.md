# Hikyo personal values: one shared environment, one value per developer (ADR, accepted 2026-11-06)

> **Status: accepted and operative.** The owner took decisions O1 to O4 below on 2026-11-03 and O5 to O8 on 2026-11-06,
> and accepted the remaining details with the implementation in the same PR (#1167, issue #1235). The
> [implementation handoff](../handoff/1235-personal-values-implementation.md) maps every
> [Implementation acceptance](#implementation-acceptance) item to its test or records it as remaining work.
>
> **Review disposition:** the [amendment procedure](./oss-mechanics.md) asks for a reopened ticket and adversarial
> cross-model review. Cross-provider review did not run and is not CLEAN. The owner waived it on 2026-11-06 (O8). The reopened
> tickets are still owed. Independent review ran on one provider only, and the [handoff](../handoff/personal-values-design.md)
> records what actually ran.

## Problem

A team wants one central place for local development variables and secrets. One environment per developer does not
scale, and the matrix is the smallest reason why:

- A project holds at most 50 environments, and the matrix is legible to about 15
  ([ops-spec.md](./ops-spec.md) § Max environments per project).
- A schema publish validates every environment and needs publish authority on each affected one
  ([schema-model.md](./schema-model.md)), so one developer's unfilled environment could block a production schema change.
- Creating an environment needs `definitions-edit@project`, so developers cannot self-serve without schema authority.
- Copies are independent ([flat-model.md](./flat-model.md)), so changing a shared value means copying it N times.
- Rules on "all environments" include later ones, so org-wide `reveal` reads every personal environment.

## Owner decisions

- **O1. Shape.** One shared environment plus per-developer values. Developers are never matrix columns.
- **O2. Encryption.** Values are encrypted by the server, so the instance operator can technically read them. The owner
  rejected encrypting on the developer's machine in order to keep one complete experience on either surface.
- **O3. One complete surface.** A person who chooses the browser never has to switch to the CLI to manage personal
  values, and the reverse.
- **O4. Scope.** Administrator-assigned per-person values are out of v1. A secret the whole team shares is an ordinary
  value in the shared environment.

On 2026-11-06 the owner accepted the remaining recommendations:

- **O5. Declared limits.** The two limits in [What this guarantee is](#what-this-guarantee-is) are accepted as declared: anyone
  with `edit` and `publish` on the shared environment can capture a personal value through a proxy setting or a changed
  endpoint, and an administrator who prepares an account before handover can keep its password and authenticator.
- **O6. Account rules.** The rules in [Which accounts qualify](#which-accounts-qualify) stand as written: network
  `credential-reset` refuses accounts with a linked external identity, and losing the only factor, including through a recovery
  code, deletes personal values.
- **O7. No browser `.env` download.** Running a local program needs the CLI.
- **O8. Review waiver.** Cross-provider review is waived. The other provider is unavailable, and the owner's current policy makes
  cross-provider review optional. It did not run and is not CLEAN.

On 2026-11-07 the owner decided:

- **O9. Running locally needs a local factor.** The developer-credentials rule stands: minting a developer credential needs a
  passkey or authenticator app ([developer-credentials.md](./developer-credentials.md)), even for an account that
  [qualifies](#which-accounts-qualify) through a federated identity. Such a developer manages personal values in the browser or
  the CLI but cannot run a program locally with them until they enrol one. Who can mint does not change. The gap is explicit
  instead: the `hikyo dev session` refusal and the personal values page say that running locally needs an enrolled passkey or
  authenticator app and point to Settings > Security, and the CLI reference and the local development documentation state it.

The requirement behind them: an administrator must not be able to see a developer's personal secrets, such as a GitHub
token. [What this guarantee is](#what-this-guarantee-is) states how far that holds.

## The model

A **personal value** attaches to `(key, environment, owner)`, where the owner is a human principal. It is not a Value.

- **Resolution for the owner is one lookup:** their personal value, else the environment's published entry, else
  unresolved. There is no chain, no precedence setting and no second layer.
- **For every other reader the flat model is unchanged.** Personal values never enter a Resolved snapshot, revision, pin,
  pending change, diff, adapter payload, copy, clone, restore, offline receipt or definitions bundle.
- **Unprotected environments only.** A developer credential is already refused on a protected environment, so protecting one
  stops delivery. Values remain until the owner or retention removes them. Protection terminally revokes developer credentials,
  so removing it restores the values but not the credential.
- **Declared keys only.** A personal value needs a declared key. Undeclared keys would have no place on the onboarding checklist.
- **Always secret, never history.** A personal value is masked and delivered as secret whatever the key's classification, and
  writes skip secret scanning, so no fingerprint exists. Replacing or deleting one destroys the row. Reclassifying a key has no
  effect on it.

### Required personal values

A key declaration gains `personal_required_in`, using the existing presence shape restricted to `mode: none | explicit`, so
portable bundles keep working. `mode: all` is refused because it would reach environments created later. Editing it needs
`definitions-edit` and no per-environment publish authority, because it moves no delivered content.

A declaration is rejected when the key is also `required_in` or `forbidden_in` that environment, or is in a key group, because
group all-or-none is evaluated at publish and a personal value cannot satisfy it.

A shared value does not satisfy the requirement. It is checked in one place, the owner's own delivery: a missing value fails that
developer's run, names the key, and never affects a publish.

## Who can do what

There is no capability atom for personal values, so no grant, preset, wildcard rule or org-scoped role can reach them. For human
sessions, the authorization chokepoint compares the caller to the owner. A developer credential reaches personal values only through
the [delivery](#delivery) overlay, for its recorded human, and the `personal` operations refuse it. A workspace session from another
instance's page ([multi-instance.md](./multi-instance.md)) is refused too, because that ADR already declares its bearer extractable by
the viewing page. Every other artifact, and MCP, is excluded per operation. Grants stay additive: this is not a deny rule, it is a resource with no
grantable read.

| Actor | Can | Cannot |
|---|---|---|
| Owner holding `read(E)` and `reveal(E)`, on a [qualifying account](#which-accounts-qualify) | Set, replace, get, see their checklist | See another person's value |
| Owner while the account lives, whatever their grants | List masked and delete their own values | Set or get without the line above |
| Holder of `manage-members` covering that key in `E` | See counts (`3 personal`, `11/14`), who holds a value and when it changed, delete it | Read, overwrite or copy it |
| Holder of `definitions-edit` | Delete values indirectly, through key rename, key delete or `forbidden_in` | Read them |
| Holder of audit read | See who holds a value, from events | Read it |

Set and get use the existing `reveal` reauthentication gate for that environment. `--member NAME` resolves only a principal holding a
value or notice for that key in `E`; any other name is answered exactly as a nonexistent one, as
[tenant-isolation.md](./tenant-isolation.md) requires of writers that name a principal. The `11/14` denominator is humans currently
holding both `read(E)` and `reveal(E)`.

## Closing the paths to a value

Authorization alone is not enough, because an administrator can act as the developer if they can sign in as them.

### Which accounts qualify

An account **qualifies** when it can complete the existing `reveal` reauthentication gate with something other than a password:

- a standing possession factor (TOTP or passkey), or
- on a passwordless account, a federated identity that passes that gate. A local password in the current epoch disqualifies federated
  proof. OAuth2 sign-in and providers with no assurance policy never pass the gate, so those people enrol a factor. SAML identities
  do not qualify until a SAML reauthentication window can open a personal value.

Access checks `read(E)`, `reveal(E)` and qualification live. Two rules keep this safe:

1. **Losing qualification through a credential change deletes values at once**, in the same transaction, with a notice. That covers
   removing the last factor, consuming a recovery code (which retires factors) and a password being established on a factorless account.
   Otherwise an administrator could use a planted recovery code to clear the developer's factor, enrol their own and read the values. A
   person who loses their only factor re-enters their values. TOTP cannot be replaced in place today, so someone changing phones keeps
   their values only by adding a passkey first. Changes with no credential write, such as a disabled provider or a provider losing its assurance policy,
   only suspend access, and the [retention sweep](#offboarding) handles them. While a value is suspended, delivery sends only its key
name and `hikyo run` refuses the key, as for a notice.
2. **Network `credential-reset` refuses an account with any linked external identity**, at mint and again at consumption, with the
   existing uniform refusal, so the response does not reveal that the account uses SSO. Audit records the cause, and the documentation
   points to the identity provider. Otherwise an administrator could reset an SSO developer, set a password and enrol their own factor, all
   invisible to a developer who still signs in through SSO. A linked identity counts even when its provider is disabled, so that case
   needs break-glass.

A factor-holding account needs nothing more. A standing factor is never skippable at password sign-in
([human-auth.md](./human-auth.md)), so its values survive a reset, and a forgotten password costs nothing.

### Other paths

| Path | Rule |
|---|---|
| Admin sets a schema rule and watches the outcome ([oracle](./schema-model.md)) | No value-dependent rule ever refuses a personal value, at write or delivery. The owner gets a warning, and audit events have the same shape either way. Counts report presence. |
| Restore brings back deleted values | Restore purges all personal values and records notices. A rewound database cannot tell a live value from one deleted at offboarding. |
| Ciphertext moved to another person's row | The envelope binds the owner principal (below). |
| Admin deletes a value so the developer silently falls back to a shared value they control | Removal notice (below). Administrators can delete but never overwrite. |
| Admin renames or deletes a key, then re-declares that name with a shared value | Renaming or deleting removes the key's personal values and leaves a notice on the name, so a re-declared key inherits it. |

### Removal notices

Whenever a value disappears other than by its owner deleting it (`administrator`, `retention`, `restore`, `key_renamed`, `key_deleted`,
`forbidden`, `qualification_lost`), a notice records the cause, keyed by owner, environment and key name. Until the owner sets a new value or dismisses the notice, on
either surface, `hikyo run` refuses that key and names the cause. Falling through to a shared value is otherwise invisible, and a
local `DATABASE_URL` could silently become the shared one.

### Encryption

Sealed under the project DEK with a new envelope kind, **`personal_value` (kind byte 9)**. Its AAD is `org_id`, `project_id`,
`env_id`, `key_id` and `owner_principal_id`. The rows join project DEK rotation, reencrypt coverage and crypto-shred on project
delete. The encryption ADR's statement that the server must be able to decrypt is unchanged.

### What this guarantee is

Protected: viewing a personal value through any product surface, API or CLI, by an org or project administrator.

Not protected. The product must say so where values are entered: "Hikyo does not show this to administrators. Your instance
operator could read it, and anyone who can change shared values here could capture it once your program runs. If someone gave you your
password, authenticator or recovery codes, replace them, regenerate your recovery codes and check your linked identities first."

- **Anyone with `edit` and `publish` on the environment** can obtain a value from any process that receives it, without value
  access. Publishing `HTTPS_PROXY`, `NODE_TLS_REJECT_UNAUTHORIZED=0` or a changed `API_BASE_URL` is enough. Loader-control refusal
  does not cover these, and [compose-integration.md](./compose-integration.md) already says a name list cannot be the boundary.
  This includes any administrator who can grant themselves those rights, such as an org administrator holding `manage-members`: the
  grant and the publish are audited, not prevented. Keep publish rights on the shared environment narrow.
- **An administrator who prepared the account before handover.** An administrator can consume an invitation or a reset of a factorless
  local account, enrol a factor or link an identity, and hand over a password and authenticator they kept a copy of, keeping the
  recovery codes too. Every step relies on a credential obtained before handover, so no server-side check can close this. A kept
  recovery code still works weeks later: consuming it deletes the values and locks the developer out, and if the same administrator
  hands over the replacement, the values the developer re-enters are exposed again. A developer should enrol their own factor, replace
  anything handed to them, regenerate their recovery codes, and check linked identities before entering a value.
- **The instance operator**, a running-server compromise, or a root key combined with a database or backup copy. The
  [threat model](./threat-model.md) already trusts these.
- **An identity provider administrator** who can authenticate as the developer, and a **holder of instance `instance-config`**, who
  can confirm new signing certificates or endpoints for an existing provider ([saml-sp.md](./saml-sp.md)) and so authenticate as a
  developer who qualifies through federated sign-in. A developer with a standing possession factor is not exposed this way.
- **Any process running as the developer's OS user**, including an agent ([developer-credentials.md](./developer-credentials.md)).

## Delivery

Personal values reach their owner only through `fetchDelivery` with a developer credential. The overlay checks that artifact class
itself. The route's contract refuses human sessions today, so the human-session `run` path does not receive them. Machine credentials, adapters, `exportValues`, `compose render`,
`compose sync`, the Kubernetes path and MCP never do.

- The server overlays the owner's current values on the committed snapshot at fetch time, the one declared exception to delivery
  reading only snapshots ([revision-model.md](./revision-model.md)). The existing loader-control, collision and execution-limit
  checks in `hikyo run` still apply.
- The delivery cursor binds the owner's personal state (each value row, notice and withheld or missing marker), so no change,
  including deleting the last personal value, is answered as `current`.
- `hikyo run` names overridden keys on stderr, never values, so the person can tell which value they got.
- `--config-only` and the human-session path never receive personal values and skip the requirement check. They get the names of keys
  where the caller holds a value or notice (the human-session path through `hikyo personal list`), and `hikyo run` refuses those keys
  rather than silently serving the shared value. Other keys are served from the shared entries.
- A credential's consent extends to the owner's current and future personal values in that environment, and the mint prompt says so.
  A credential minted without that consent gets only the names of keys where its owner holds a value or notice, and `hikyo run` refuses
  those keys and asks for a new credential, rather than silently using the shared value.

## One complete surface (O3)

Every management operation is a `webui` row in [`api/parity.yaml`](../../api/parity.yaml) and has a CLI verb. No new parity
exception class is added.

| Task | Browser | CLI |
|---|---|---|
| See my checklist and values, masked | My personal values page | `hikyo personal list` |
| Set or replace | Same page, or the matrix cell editor | `hikyo personal set KEY` (value from stdin) |
| Get my value | Existing reveal ceremony | `hikyo personal get KEY --reveal` |
| Delete my value, dismiss a notice | Same page | `hikyo personal set KEY --clear` |
| See who holds a value | Matrix side panel, Members | `hikyo personal members KEY` |
| Delete someone's value | Same panel | `hikyo personal set KEY --clear --member NAME` |
| Holder counts per key | Matrix marks | `hikyo personal counts` |
| Offboarding report | Members | `hikyo personal offboarding-report NAME` |

Spellings belong to [api-cli-spellings.md](../spec/api-cli-spellings.md). Three things are outside this rule, and all predate this
ADR:

- Launching a local program needs the CLI, because a browser cannot start a process (`client-local-delivery`).
- The developer credential's own lifecycle is CLI-only (`bounded-developer-delegation`).
- CLI sign-in and reauthentication for federated or passkey accounts open a browser
  ([api-cli-surface.md](./api-cli-surface.md)).

A browser download of a `.env` containing personal values was considered and left out (O7). It would create a plaintext copy that
bypasses loader-control and goes stale on every change.

### The matrix

The environment stays one column. Personal values add marks, never columns, and these marks are shown to `manage-members` holders.
A developer sees only their own state.

```
KEY            development          staging   production
DATABASE_URL   ••••  · 3 personal   ••••      ••••
GITHUB_TOKEN   personal · 11/14     · absent  ••••
LOG_LEVEL      debug                info      warn
```

Every mark carries text, per [PRODUCT.md](../../PRODUCT.md).

## Onboarding

1. An administrator invites the developer, or SCIM provisions them. Both exist.
2. A rule grants See and Reveal on only the shared environment, using the existing Members editor or a SCIM group mapping. Shared
   values, including any shared secret, arrive through this rule with no per-person step.
3. The developer enrols their own factor, unless their passwordless federated sign-in already passes the `reveal` gate.
4. They see their checklist, from `personal_required_in`, on either surface, and set each value there.
5. To run locally they mint a developer credential and use `hikyo run`.

## Offboarding

1. **Access ends.** Removing the principal ends sessions and credentials atomically. SCIM deprovisioning releases only `scim` grant
   origins, so a manually created rule keeps working until an administrator revokes it, and the existing attention state flags
   this. Use SCIM group mappings for the onboarding rule where SCIM is in use.
2. **Values are suspended, then deleted.** Delivery checks access live, so it stops as soon as access is lost. A
   retention sweep records when it first sees access missing, clears that record if they return, and deletes the value after 14 days.
   The window stops an identity provider mistake from wiping a team's values. An administrator can delete sooner, and the owner can
   always delete their own. Key, environment, project and account deletion cascade at once. Personal values never block
   environment deletion, and a deletion plan reports how many it removes. Developer credentials revoked by the loss do not return.
3. **The report lists what to rotate.** The offboarding report names shared keys the person is recorded as receiving, by environment,
   whether each value changed since, and the service accounts they created. It matches recorded human authority as well as actor,
   because a developer credential's actor is the developer principal. It lists names only, needs `manage-members` and audit read over
   that scope, and states its evidence window: audit retention limits it, and only audited disclosures appear.
4. **Hikyo cannot undo disclosure.** It cannot revoke a GitHub token or erase what already left the server, and the report says so.
   Deleted values persist in backups as [ops-spec.md § 2, Retention & erasure](./ops-spec.md#2-retention--erasure) describes.

A person who rejoins after deletion starts empty.

## Audit and limits

New events: `personal_value.set`, `.revealed`, `.delivered` (one per key per delivery, committed before plaintext leaves, in the
access retention class under the fetch envelope) and `.deleted` (with actor, owner and cause, security class). None carry plaintext
or a value fingerprint.

Each value is at most 64 KiB, the existing limit. A project holds at most 100 000 personal values, matching the resolved-cell budget,
with a loud refusal. A fetch carries at most 1 MiB of the owner's personal values, a cap of its own apart from the shared rows' 1 MiB
render limit, so a shared value's length never decides whether a personal value crosses. Over it, the fetch is refused with a detail
naming the owner's personal values.

## Rejected

- **One environment per developer.** See Problem.
- **Encrypting on the developer's machine.** It would also protect against the operator and make erasure immediate, but O2 chose one
  complete experience on either surface.
- **Administrator-assigned values.** Out of v1 by O4.
- **Sweeping all credentials on administrator reset.** It would let an administrator strip another person's factors, weakening every
  account to protect one feature.
- **Retiring recovery codes and links when an account gets its first factor.** It narrows the prepared-account residual but cannot close
  it, because a handed-over authenticator still works. It also needs a new session-to-identity column and can lock out people who lose a
  factor with no codes.
- **Deleting values on every administrator reset.** A factor-holding account is already immune, so a forgotten password would cost
  every value for no security gain, and values entered after the reset would not be protected anyway.
- **Personal values on undeclared keys.** No schema or checklist.
- **A global deny for administrators.** [member-access-rules.md](./member-access-rules.md) D4 rejected it. Having no grantable read
  avoids the precedence puzzle.

## Declared owning amendments

Each ADR below carries a banner pointing here. Each becomes operative with the implementation PR.

- [flat-model.md](./flat-model.md): personal values are outside the value model, and owner resolution is the one exception.
- [revision-model.md](./revision-model.md): the owner overlay at fetch and the cursor binding.
- [threat-model.md](./threat-model.md): narrows "org admins are trusted" for viewing personal values and adds the residuals above.
- [encryption-model.md](./encryption-model.md): envelope kind 9.
- [permission-model.md](./permission-model.md): owner comparison, metadata authority, no new atom.
- [schema-model.md](./schema-model.md): `personal_required_in`, its authority exception, and no value-dependent refusal.
- [source-of-truth.md](./source-of-truth.md): the bundle carries `personal_required_in`, never values.
- [human-auth.md](./human-auth.md): qualifying accounts, value deletion on credential change, reset refusal on linked accounts.
- [developer-credentials.md](./developer-credentials.md): consent extends to the owner's personal values.
- [compose-integration.md](./compose-integration.md): `hikyo run` overlay and naming.
- [api-cli-surface.md](./api-cli-surface.md): the `personal` verb family and full parity.
- [audit-model.md](./audit-model.md): the events above.
- [ops-spec.md](./ops-spec.md): 14-day deletion, restore purge, the per-project limit.
- [mvp-boundary.md](./mvp-boundary.md): admits personal values on demand.

## Implementation acceptance

SQLite and PostgreSQL must both prove:

- No grant, preset or org-wide rule reads another person's value, and no administrator can overwrite one.
- Network reset refuses an account with a linked identity at mint and at consumption, with the uniform refusal. A factor-holding account keeps its values after
  reset, and its password alone reaches nothing.
- Set and get refuse an account that does not qualify, and a federated proof does not count while a local password stands.
- Last factor removed, recovery code consumed, and a password established on a factorless account each delete values in that transaction.
- A planted recovery code can destroy values but never read them: consuming it deletes them before any new factor can be enrolled.
- Machine delivery, adapters, snapshots, revisions, diffs, copy, clone, offline receipts, `exportValues`, MCP and definitions export
  contain no personal value.
- A schema publish never evaluates a personal value. An owner whose value breaks a rule is warned and still receives it, and the audit
  events have the same shape.
- A missing required personal value fails only the owner's run and never a publish.
- AAD binding refuses a row moved to another owner, and DEK rotation and reencrypt cover the rows.
- Loss of the predicate stops delivery at once, deletion happens at 14 days, and regaining access within the window restores values.
- Restore, rename, key deletion followed by re-declaration, `forbidden_in`, lost qualification and administrator deletion each leave a
  notice that makes `hikyo run` refuse the key until dismissed.
- A credential minted without consent gets names only, and `hikyo run` refuses those keys. Human-session delivery and `--config-only`
  have no personal value, and `hikyo run` refuses keys where the caller holds a value or notice on those paths too. A suspended value
  makes `hikyo run` refuse its key.
- Developer credentials, workspace sessions and MCP are refused by every `personal` operation, and `--member` naming someone without a
  value or notice for that key is answered as a nonexistent principal.
- A personal change or deletion is never answered as `current`.
- The report matches disclosures made through a developer credential, states its evidence window, and contains no values.

The browser must complete every management task with no CLI, and the CLI must complete every one with no browser after sign-in. Prove
both in Playwright and CLI tests, and add matrix stories for the new marks at desktop and phone width.
