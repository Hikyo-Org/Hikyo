# Hikyo member access rules: scoped rules, rule-local exceptions, key-level limits (ADR, decisions owner-approved 2026-09-28)

> **Status: declared amendment to [permission-model.md](./permission-model.md), operative with the
> implementing PR ([#838](https://github.com/Hikyo-Org/Hikyo/pull/838)).** The owner took every decision below on
> 2026-09-28 while iterating the [member-access prototype](../site/public/prototypes/member-access/)
> (iterations 1 to 6; iteration 6 is the reference, its components now in the WebUI, Storybook *Members/Access rules*),
> and on the same night directed the full-stack implementation and merge on green ahead of the review.
> [#15](https://github.com/Hikyo-Org/Hikyo/issues/15#issuecomment-6216662798) was reopened on
> 2026-10-16 for the [oss-mechanics.md](./oss-mechanics.md) amendment procedure.
> For #15 only, the owner explicitly accepted completed Astra-high verification plus
> independent Sol 6.1-high source review as the formal review disposition on 2026-10-16.
> The quota-gated cross-provider pass remains skipped, not retroactively CLEAN; this does
> not change permission policy or the general review/quota rules. The
> [disposition and limitations](../handoff/15-member-access-review.md) preserve that boundary.
> The dated first implementation slice below is historical. The
> [October 5 delivery handoff](../handoff/members-unified-access-cards.md) records the later
> implementation, and the [October 16 reconciliation](../handoff/15-member-access-review.md)
> distinguishes implemented behavior, remaining target slices, and the scoped review disposition.
> Where implementation is narrower than these decisions, the decisions remain the target.
>
> **Owner decision record (2026-11-20, [#1446](https://github.com/Hikyo-Org/Hikyo/issues/1446),
> [#1447](https://github.com/Hikyo-Org/Hikyo/issues/1447), [#1473](https://github.com/Hikyo-Org/Hikyo/issues/1473),
> [#1474](https://github.com/Hikyo-Org/Hikyo/issues/1474); operative when the recording pull request merges;
> cross-provider review was not requested and did not run, independent Claude Standards and Spec review is
> recorded on that pull request).** The owner settled four open points; see
> [Owner decisions, 2026-11-20](#owner-decisions-2026-11-20). They decide what this ADR left open
> or to an implementing ticket and change no D1 to D11 clause.
>
> **Owner decision record (2026-11-25, [#1446](https://github.com/Hikyo-Org/Hikyo/issues/1446);
> operative when the implementing pull request merges).** The owner chose how all-projects rules
> ship (options A, S1 and R2); see [Owner decisions, 2026-11-25](#owner-decisions-2026-11-25).

## Context

The permission ADR fixes a grant as `(principal, capability, scope)` with scope
`instance → org → project → environment`, and records key- and folder-scoped
grants as *deferred (fog)*: "the natural first extension when someone hits the
wall", blocked on a resolver UI because "why can't I see this?" becomes
unanswerable without one. The owner hit that wall: members need access to some
keys, folders or environments of a project without the Members page drowning in
capability rows, and need to say "everything in this project, except …".

Six prototype iterations settled the shape. Layout alone (iteration 1: inline
chips, allowlists on the folder, a per-member profile) did not make access
clearer. What did: reading access as rules with one vocabulary, a three-axis
"where", and exceptions that stay local to their rule.

## Decisions

### D1. The rule is the unit a person reads and edits; the grant stays the unit stored

A **rule** is a set of grants to one principal that share one `where`
(D3), created and edited together. Each permission in it is still its own grant:
unticking a permission revokes exactly that grant, and audit records one event
per grant. The stored primitive widens from `(principal, capability, scope)` to
`(principal, capability, where)`; nothing stores a role, and the permission
ADR's "roles are templates, expanded at grant time" stance is unchanged.

### D2. One vocabulary, used by every surface

Every capability atom that appears on the access surfaces has exactly one
human name, and the editor, the rule list, the resolver and the glossary all
read it from one table. No surface invents its own verb ("view", "see",
"change definitions of") for the same atom.

| Name | Atom | Group | Narrowest `where` it can be held on |
|---|---|---|---|
| See | `read` | Values | a key (but see D5) |
| Edit | `edit` | Values | a key |
| Publish | `publish` | Values | a key |
| Pin | `pin` | Values | all keys of an environment |
| Reveal | `reveal` | Secrets | a key |
| Reveal history | `reveal-history` | Secrets | a key |
| Define keys | `definitions-edit` | Administration | a folder or key |
| Manage access | `manage-members` | Administration | a folder or key |
| Manage machines | `manage-identities` | Administration | a whole project |
| Manage deploys | `manage-adapters` | Administration | a whole project |
| Change settings | `project-settings` | Administration | a whole project |
| Manage projects | `manage-projects` | Administration | all projects |

Atoms added by other amendments (`crypto-use`, `crypto-manage`,
`issue-certificate`, `report-delivery-status`) get a row in the same table,
with a name and a narrowest `where`, before they appear on these surfaces.

**Presets** (Viewer, Editor, Publisher, Admin) only tick boxes; the rule stores
the ticked grants, and the rule list names a preset only when a rule matches it
exactly. The **Admin preset does not tick Reveal or Reveal history**: secrets
are always an explicit, visible choice. This changes the interactive preset
only. The first-administrator bootstrap keeps seeding `reveal` and
`reveal-history` as separate grants, exactly as the permission ADR and the
human-auth ADR require.

### D3. Where = projects › environments › keys

- **Projects:** *all projects* (including projects created later) or a list.
- **Environments:** *all, except* named environments (including environments
  created later) or *only* named environments. Names match across the chosen
  projects: "all environments except `prod`" on `payments, web` excludes both
  `prod` environments. Rules store the names, so they also match
  environments created or renamed later (owner decisions, 2026-11-26).
- **Keys:** *all, except* folders or single keys (including keys added later)
  or *only* folders or single keys.

The permission ADR's four scope levels are the special cases with no key
narrowing; an existing grant migrates to a `where` with the same reach.

### D4. Except is rule-local; there are no deny rules

An *except* narrows only the grants of its own rule. Effective access is the
union of everything a principal's grants reach; if another rule reaches the
same place, the principal still has it. Grants stay purely additive and the
permission ADR's "absence of a grant is denial, no precedence puzzle" holds.
The resolver says so explicitly when a person is left out by one rule but
admitted by another.

*Rejected: global deny* ("Bob may never reveal `DB_PASSWORD`", overriding every
rule including organisation-wide and admin rules). It reintroduces the
grant-versus-deny precedence the permission ADR rejected, and lets one row lock
an administrator out of their own organisation.

### D5. What a narrow rule can carry

- **See is never narrowed by keys.** The key catalogue, descriptions, schemas
  and `config` values are environment-wide by the permission ADR's own reasoning
  ("classification is the sensitivity boundary"). A key limit narrows every
  other permission in the rule; an environment except removes See too.
- A permission is held only on a `where` at least as wide as its row in D2.
  The editor shows such a permission disabled, with the reason inline ("needs a
  whole project"); the API refuses it. Nothing is silently dropped from a
  preset.

### D6. Permissions stay independent; disclosure formulas do not change

No permission requires another (the permission ADR's "no prerequisite
chaining" stands: Edit without Reveal is blind replacement). The disclosure
formulas are unchanged, so showing a secret still needs `read ∧ reveal`; the
resolver reports "has Reveal but not See" rather than a false yes.

### D7. Folder-scoped administration

- **Define keys** on a folder creates, renames and removes keys inside that
  folder and edits their rules. Creation keeps the existing formula
  (`definitions-edit` plus `publish` in every environment given a value), so a
  new key gets values only in the rule's environments.
- **Manage access** on a narrowed rule grants only inside its own `where` and
  only permissions the grantor holds there, extending the permission ADR's
  project-scope `manage-members` bound to folders and environments.

### D8. How selectors follow keys

- A **folder** selector covers whatever keys are in the folder when access is
  evaluated: a key added to the folder joins, a key moved out leaves, a key
  renamed inside it is unaffected.
- A **single-key** selector binds to the key's stable id: it survives renames
  and moves; a deleted key drops out. Deleting a key and creating a new one with
  the same name makes a new key, which an except naming the old key's id does
  not cover (an "all, except" rule includes keys added later). Exclusions meant
  to outlast the key belong on a folder. Creating that key already needs Define
  keys, and a folder rename does not move keys (their folder path is unchanged).

### D9. A move that widens access needs confirmation

Moving a key between folders can give people access they did not have: an
except that named the old folder no longer covers the key, or an *only* pick
of the destination folder now does. Before a move commits, the server computes
the principals who gain any permission on the key, per environment. If that set
is non-empty the move needs explicit confirmation naming them; for a machine
principal the confirmation is a field in the plan, exactly like the
protected-environment confirmation. Moves that only narrow access, renames and
additions commit without it. The confirmation and its named set are audited.

### D10. The resolver ships with the feature

"Who can…?" takes one key in one environment and one permission, and answers
who can, who is left out by an except (naming the rule and the except), who
has Reveal without See, and who no rule reaches. This is the resolver the
permission ADR made a precondition for key-level access.

### D11. People on Members, machines on Machine access

Members lists people only. A service account's rules live with its
credentials on Machine access, which reuses the same rule editor; the editor
disables every permission the permission ADR's machine allowlists forbid,
with the reason. "Who can…?" answers across people and machines, marking
machines, because "who can reach this key?" must include deploy accounts.

## Consequences and open points

- **Evaluation** stays one chokepoint with no cache; matching a `where` adds a
  folder and key-id lookup per evaluation.
- **Machine principals:** their allowlists are unchanged; whether a machine
  rule may be key-narrowed is left to the implementing ticket (no prototype
  decision was taken). *Decided 2026-11-20: no key narrowing for machines (below).*
- **Folder rename:** whether folder selectors bind to the folder path or a
  folder id is open. Path binding makes a folder rename silently narrow every
  folder pick; id binding requires folders to become identified entities.
  *Still open after 2026-11-20;* an all-projects rule's folder picks match by path in each project
  whichever binding is chosen for named projects.
- **API and CLI** need a spelling for `where`; the api-cli-surface ADR owns it.
- **MVP boundary:** this moves "key-scoped reveal" from fog to a decided
  design; scheduling is a separate call.

## Owner decisions, 2026-11-20

The owner took these decisions after the 2026-11-19 implementation-status audit. Each applies when
its implementing pull request merges; until then current behaviour, as the
[October 16 reconciliation](../handoff/15-member-access-review.md) and the first slice's restrictions
on these points describe, continues.

- **All-projects rules allow folder selectors only
  ([#1446](https://github.com/Hikyo-Org/Hikyo/issues/1446)).** A rule whose project axis is *all
  projects* may narrow keys by folder path (*only* or *except*), because a path means the same in
  every project; single-key items are refused on such a rule, because a key id belongs to one
  project. Creating a project needs no notice or confirmation: D3's "including projects created
  later" is the intended reach.
- **Environment rename and create that widen access need confirmation
  ([#1447](https://github.com/Hikyo-Org/Hikyo/issues/1447)).** Once environments match by name
  (D3), renaming an environment can take it out of an *except* or into an *only*, and creating one
  can put it inside an *only*. Before either commits, the server computes who gains a permission
  through such a name match, people and machines alike, as D9 does for folder moves. If anyone gains,
  the change needs explicit confirmation naming them. Who may see the named set and confirm follows
  the folder-move confirmation as implemented (see the move-widening bullet below), and the
  confirmation and its named set are audited. A new environment's inclusion in an *all* or *all,
  except* rule that does not name it is D3's intended reach and needs no confirmation, like project
  creation above. Changes that only narrow commit without it.
- **Machine principals get rules without key narrowing
  ([#1473](https://github.com/Hikyo-Org/Hikyo/issues/1473)).** A service account's rule may use the
  environment axis but not the key axis, and stays inside what its grants may reach: its owning
  project only ([machine-identities.md](./machine-identities.md)), and its class's scope depth
  ([permission-model.md](./permission-model.md)). A workload rule therefore names explicit
  environments; an automation rule may also use *all* or *all, except* in its project. Without key
  narrowing, the machine widening gate's per-environment count stays exact when a rule is written;
  whether the gate's reauthentication conjunct also applies to a later environment rename was
  settled 2026-11-26, below: it does. Key narrowing for machines would need the gate to count per key and is not decided. The
  permission ADR's machine allowlists are unchanged, and D11's placement on Machine access stands.
- **The resolver gets a server-side operation
  ([#1474](https://github.com/Hikyo-Org/Hikyo/issues/1474)).** D10's "Who can…?" becomes an additive `/api/v1`
  operation with a CLI spelling, declared as an amendment to
  [api-cli-surface.md](./api-cli-surface.md) in its implementing pull request. Its evaluation is the
  `authorize()` chokepoint's, so the answer cannot drift from enforcement. It is gated by
  `manage-members@project` and lists the project's grant lines, organisation-level grant lines of the asked
  permission, and the project rules the caller may manage; never sibling projects, unmanageable rules or
  instance operators. Showing organisation-level holders to a project manager is decided in #1481 and
  confirmed by the owner 2026-11-23; see the api-cli-surface amendment.

## Owner decisions, 2026-11-25

The owner chose options A, S1 and R2 for all-projects rules
([#1446](https://github.com/Hikyo-Org/Hikyo/issues/1446)). Each applies when the implementing pull
request merges.

- **Environment axis, option A.** A rule over all projects covers every environment. Environment
  items (*only* or *except*) are refused on such a rule until environment selectors bind by name
  ([#1447](https://github.com/Hikyo-Org/Hikyo/issues/1447)); #1447 lifts this restriction. Folder
  *only* and *except* items stay allowed, and single-key items stay refused (2026-11-20 above).
  Machine principals never hold an all-projects rule: a machine rule names its owning project.
- **API spelling, option S1, additive only.** A rule's `where` gains an optional
  `project_mode: all|only`, default `only`. Storage keeps only the mode and never expands it into a
  project list. A response for an all-projects rule lists the organization's current projects in
  `projects` at read time, with its folder items repeated under each; an older client that saves
  that list back gets a narrower rule, which fails closed. The API revision is 21.
- **Create spelling, option R2.** `POST /orgs/{org}/rules` (and the creates of a rule replacement)
  keeps the single `where` shape; `where.project_mode: all` is its only addition, and no schema
  reference changes, so the frozen v1 document stays valid. The client sends `project_mode: all`
  with the organization's current projects in `projects`, environment axis `all` without items,
  and the same folder items repeated under every listed project. The server compares the list with
  the organization's project set inside the create transaction, and refuses anything but the exact
  set (a missing, extra, deleted or duplicated project) with a conflict whose detail names the
  difference. The set is read again under a project-row lock once Manage access is proved in every
  current project, so the detail discloses nothing new; write transactions are serializable on
  PostgreSQL and serialized on SQLite, so the comparison cannot race a project deletion. Storage
  keeps only the mode. The `where` of an organization listing or of a create response is therefore
  itself a valid create request; a project listing shows only its own project, so its `where` fails
  closed as a stale list. A project created concurrently is covered as any later project is, under
  the future-project authority below. The CLI's `access rule add --all-projects` and the
  Members editor's "All projects" choice list the projects first.
- **Zero projects.** No v1 response may carry an empty `projects`, so the frozen `minItems: 1`
  stays true for older clients. Creating an all-projects rule in an organization with no project is
  refused with a conflict. Deleting an organization's last project while an all-projects rule exists
  there is refused with a conflict naming the blocking rule ids, so they can be revoked first.
  Project deletion is the only path that removes a project; an organization is deleted only once
  empty. The deletion locks the organization's projects so concurrent deletions cannot both pass.
- **Listing bound (owner principle extended, 2026-11-25).** The zero-projects principle, refusing
  the change that would make a v1 response invalid, also covers the frozen 500 bound of a rule's
  `where` (`domain.MaxRuleItems`, projects, environments and keys together). An all-projects rule is
  listed as the organization's current projects plus its folder items repeated under each, so its
  listing counts projects × (1 + folders). A create that would list more than 500 is refused with
  `limit_exceeded`. A project creation that would list any existing all-projects rule past 500 is
  refused with a conflict naming the blocking rule ids to a member manager, as the last-project
  deletion is (a `limit_exceeded` body carries no detail). The project creation takes the same
  project-row lock as a deletion, within the same serializable transaction, so it cannot race a rule
  create. A listing is never truncated and never leaves a rule out.
- **Authority (D7).** Creating or revoking an all-projects rule needs Manage access over the
  complete selector in every current project and over projects created later. Only an
  organization- or instance-scope grant, or another all-projects rule, covers a future project, so a
  manager holding `manage-members` in each current project only, by grants or by project-bound
  Manage-access rules, is refused. The conferred capability follows the same bound, with the
  unheld-capability allowance of organization authority.
- **Project listings.** On a project's Members page an all-projects rule is listed as manageable
  when the viewer manages that project, the same as any rule over several projects. The listing
  only offers the action: every create, revoke or replacement is proved again on the server over
  all current projects and the future-project witness (D7).
- **Key moves and pruning.** The D9 move-widening census includes all-projects rules. Deleting a
  project, environment or key never revokes an all-projects rule, which names none of them.

## Owner decisions, 2026-11-26

Environment selectors bind by name ([#1447](https://github.com/Hikyo-Org/Hikyo/issues/1447)). These
apply when the implementing pull request merges.

- **Names are stored and matched.** A rule's environment axis stores names, not environment ids. A
  name matches in every project the rule covers, including environments created or renamed to it
  later. Matching happens only in the `authorize()` chokepoint, reading the current environments
  in the same transaction, with no cache. An API response still lists, under each listed project,
  the environments the names match at read time.
- **Option A lifted.** A rule over all projects may carry environment names, *only* or *except*, as
  any rule may. Single-key items stay refused on such a rule, and machines never hold one. The 500
  bound counts the rule as a v1 response can list it: projects × (1 + folders + names), names
  being a revision-22 field with their own bound. A rule over named projects counts
  projects × (1 + names) + key items, and the upgrade splits any rule it would convert above
  500, so every listed rule can be saved again.
- **D7 with names.** Only an organization- or instance-scope grant, or another all-projects rule
  whose selector holds the names, covers a future project. A manager holding Manage access only
  through project-bound rules, or holding an all-projects rule over other names, is refused.
- **Widening renames and creates (2026-11-20 decision).** An environment rename, create or clone,
  or a definitions apply that renames or creates environments, is refused with a conflict while it
  gives any principal, person or machine, a permission through a name match. It commits only when
  `confirm_widening` names exactly those principals and the caller holds legacy `manage-members`
  on the project. It is audited as `rule.env_widening_confirmed`, with the environment, its old
  and new names and the confirmed set. Self-configuration provisioning carries no confirmation, so
  a widening refuses it. A new environment that an *all* or *all, except* rule reaches because it
  does not name it needs no confirmation, and nor does a change that only narrows.
- **Machines that gain (#1473, #1480).** Each confirmed machine gainer also passes the machine
  widening gate: `manage-identities` on the project, disclosure authority, and a live
  reauthentication window over every environment it newly reaches in plaintext, audited as
  `identity.grant_widened`. This settles the 2026-11-20 question: the reauthentication conjunct
  applies to a later rename. **Limitation, disclosed:** a create or clone whose name gives a machine
  plaintext access to the *new* environment through an *only* rule always fails closed. A machine
  whose `all` rule reaches it because it does not except the name gains nothing by name, so that
  create needs no confirmation and no workaround. The window must name the new
  environment's id, and no session can hold one before the environment exists. The way through is
  to create the environment under another name and rename it. A gain without plaintext, such as a
  machine's read without reveal, passes the gate with nothing to reauthenticate. One option for a
  later decision: exempt a plain create, since an empty environment holds no plaintext. A clone
  copies values and cannot take that exemption.
- **Deleting an environment** no longer touches rules: a name stays the rule's. An *only* list
  whose environment is deleted keeps the name and reaches nothing there, where id binding revoked
  an emptied list. A recreated environment of that name is a create, confirmed as above.
  Deleting an environment an *all, except* rule names fails closed: see the owner decision of
  2026-11-27, which replaces the limitation first disclosed here.
- **Migration, option (c) (owner, 2026-11-26).** The upgrade (schema 102, both engines) keeps
  the reach of every rule it converts or splits exactly, and fails closed on the rest, which can
  remove access:
  - A rule whose projects all select the same names, and whose names in every project it covers
    match only its own items, is converted in place, with one name per distinct name.
  - Any other rule is split into one rule per project, as is a consistent rule that would list
    more than `MaxRuleItems` once its names match in every project. A project where it reached
    nothing gets no rule. Each split is audited as `rule.split`, with the old rule id and the new rule ids.
    Projects selecting different names split even when name matching would reach the same
    environments: converted, each project would carry a name none of its environments has, and a
    manager whose grants cover only the rule's environments could no longer revoke it.
  - A rule that cannot be split deterministically fails closed. It loses its environment items and
    reaches no environment. It carries a manager-facing `fault` naming the reason, and is audited as
    `rule.disabled`. This covers items naming a missing environment, a project outside the rule, or
    the same name twice in a project, and an all-projects rule with environment items. Only a
    corrupted database holds such a rule.

  The id columns and the id-matching code are removed. A historical recovery below schema 102
  drops id-bound environment rules rather than read them by name, which fails closed.
- **API.** Additive under the v1.0.0 freeze, revision 22, with no schema `default:` on a new field:
  - `names` on the environment axis;
  - `fault` on a rule;
  - `confirm_widening` on environment create, rename and clone, with `widening` on their refusal;
    on environment deletion it is a query parameter (2026-11-27).

  A client before revision 22 still creates by `items`; their names become the rule's. Such a
  create is refused (`ErrRuleEnvItemsStale`) when the items are not exactly the environments their
  names match in the rule's projects, and an `all` create or replacement sent without names is
  refused (`ErrRuleUnmatchedNamesDropped`) while the principal's `all` rules except a name no
  environment of the project carries, which the old client cannot show.

## Owner decisions, 2026-11-27

Deleting an environment an *all, except* rule names
([#1447](https://github.com/Hikyo-Org/Hikyo/issues/1447)). This applies when the implementing pull
request merges. It replaces the limitation the 2026-11-26 decisions disclosed. Deleting an
environment must fail closed: it must never widen a rule silently.

- **Refuse unless confirmed (option a).** A delete is refused with a conflict while it gives any
  principal, person or machine, a permission through its rules. Once no environment of the
  project carries a name the rule excepts, the rule reaches the project-wide atoms. The refusal is
  the rename's widening refusal, worded for a delete. It names the principals who gain only to a
  caller holding legacy `manage-members` on the project, and gives anyone else only their count.
  The delete commits only when `confirm_widening` names exactly those principals and the caller
  holds `manage-members`. It is audited as `rule.env_widening_confirmed`, with the environment, its
  name as `from_name` and an empty `to_name`. Each machine gainer also passes the machine widening
  gate (#1473, #1480). A delete makes no remaining environment newly plaintext-reachable, so the
  gate asks for no reauthentication, but a machine must still be confirmed. The rule identifiers are not listed: the principals are what the manager
  confirms, as for a rename. This covers the single delete (`DELETE …/environments/{env}`, with
  `confirm_widening` as a query parameter), the CLI (`hikyo env delete --confirm-widening`), the
  web settings page, and a definitions apply that deletes environments (its `confirm_widening`).
- **Why not disable the rules (option b).** Disabling would remove every permission the rule
  gives across all its projects in order to stop a gain in one project. It would also discard the
  intent the rule states. A confirmation keeps both, and it is the mechanism a widening rename
  already uses, so delete and rename behave the same.
- **A later environment with the same name.** A confirmed delete changes no rule: the rule keeps
  the name it excepts. An environment later created, cloned or renamed to that name is excepted
  again, so it widens nothing through that rule. Other rules are counted as for any create or
  rename: an *only* rule naming it still needs a confirmation for its gainers. This is the reason to confirm
  rather than to make the manager remove the exception first: removing it would let the later
  environment be reached.
- **Sibling paths.** Only an environment delete removes an `environments` row: the service delete
  and the definitions apply delete, through the one store query on each engine. Both are covered.
  A project or organization deletion never cascades. It is refused while the project holds
  environments, so each environment goes first through a covered delete. A backup restore
  replaces rules and environments together and changes no binding. A rename was already
  confirmed.

## First implementation slice (2026-09-29, historical)

This section records the original delivery floor, not current feature status.
The October 5 implementation supersedes its statements about See, rule-based
Manage access, per-key Publish, definition mutations, and navigation. See the
[delivery handoff](../handoff/members-unified-access-cards.md) and
[reconciliation](../handoff/15-member-access-review.md).

The implementation is built so that its worst possible bug is a narrowed rule wrongly denied, never access
widened. Rules are stored in their own tables (`rules`, `rule_items`, migration 00070) and read only inside
the authorization chokepoint; every other coverage check (delivery, SSH, federation, the disclosure gate,
the lockout census, the grantor bound, navigation) stays blind to them and therefore conservative. Where
this slice is narrower than D1 to D11:

- **Humans only.** Rules for machine principals are refused; machines keep legacy grants. The machine
  widening gate counts reach per environment and would undercount a key-narrowed machine reveal.
  *Superseded by the machine rules section below.*
- **Key-aware operations are an explicit short list:** single-value reveal, staging and declaring one
  value, and key create, rename, delete and move (a move is authorized on both folders). Every other
  operation, including bulk reveal, export, diff, copy, publish, pins, delivery and definitions apply, can
  be satisfied only by a rule that does not narrow keys. A proof obtained through a key-narrowed rule is
  bound to that key.
- **See (`read`) cannot be key-narrowed** on a rule (refused, rather than silently widened to the
  environment), per D5.
- **Manage access on a rule is inert:** it is stored and shown but grants nothing, cannot create rules or
  grants, and never counts in the lockout census, until delegation containment (D7) is implemented.
  Creating and revoking rules needs legacy `manage-members` on every project the rule names.
- **Environments bind by id,** not by name: "all, except `prod`" names that environment, and environments
  created later are included. **Folders bind by path:** an "Only…" folder pick matches keys in exactly
  that folder, while an except covers the folder and every folder below it (both directions fail
  closed). A folder rename therefore narrows folder picks (the open point below).
- **Publishing stays environment-wide:** a rule carrying Publish cannot be narrowed by keys (refused on write), and a folder-scoped
  Define keys rule cannot yet create or delete keys in a project that has environments unless publish is
  held environment-wide.
- **No "all projects" on rules** in this slice; a rule names its projects. *Superseded by the
  [owner decisions of 2026-11-25](#owner-decisions-2026-11-25).*
- **The move-widening confirmation (D9)** counts gains through rules only (legacy grants are
  folder-blind), so it may name someone who already had access through a grant, never fewer people.
  Only a holder of legacy `manage-members` on the project is told who gains and may confirm; anyone
  else learns the count and cannot confirm, even with the exact set. A machine plan therefore cannot
  commit a widening move; a member manager has to.
- **Resolver (D10)** answers in the web client over the org's grants, rules and key catalogue; a
  server-side resolver operation is owed. (2026-11-20: `rule.resolve` adds it to `/api/v1`, see the
  api-cli-surface amendment of that date; the CLI verb follows in #1474.)

## Machine rules (2026-11-20)

This settles the machine open point above for the API, the CLI and the
editor on Machine access (D11).

- **Who and what.** Workload and automation principals may hold rules. The
  capability must be on the class's allowlist (`MachineMayHold`). `reveal` and
  `reveal-history` stay on legacy grants, because their machine admission
  depends on a live project opt-in and pin that a rule does not carry. Other
  machine classes hold no rules.
- **Never key-narrowed.** A machine rule that names keys or folders is
  refused by name (`ErrRuleMachineKeys`). The widening gate counts reach per
  environment, so a key-narrowed machine rule would be undercounted there.
  Because of this, a key move never changes a machine's reach, and the D9
  census stays human-only.
- **Depth and project.** A machine rule names exactly one project, the
  machine's owning project, so a rule over all projects is never a machine's.
  A workload rule lists its environments explicitly ("only"), matching
  `machineDepths`; an automation rule may cover every environment, including
  later ones.
- **Widening gate.** Rule creation and the atomic card replacement go through
  the machine widening gate: the reach before and after the complete change is
  diffed per authority class, and a non-empty delta needs
  `manage-identities` on the project, the matching disclosure capability and a
  live reauthentication window, and records `identity.grant_widened` on the
  rule. The grant widening gate and the credential mint gate count the
  machine's rules too, so a rule-held `read` cannot hide what a `reveal` grant
  newly reaches.
- **Service account deletion.** Deleting a service account removes its rules
  without a count or confirmation, as its grants are removed.
- **Evaluation.** The chokepoint reads machine rules through their own
  lookup and applies the same class check on every read: a stored machine
  rule that fails it reaches nothing. Delivery's per-key projection and SSH
  stay blind to rules, as for people; federation's binding gate counts a
  machine's rules in the reach it checks, as the mint gate does.
- **Editor (D11).** A service account's side panel on Machine access lists its
  rules and opens the Members rule editor for its class. The editor disables
  each permission the class may not hold and shows the reason on the row. It
  does not offer key limits, and for a workload it offers only "Only..."
  environments. A save that the widening gate refuses for a missing
  reauthentication runs one passkey reauthentication over the named
  environment and sends the same change again. Members and "Who can...?" show
  machine rules with the machine mark, read-only, and point to Machine access.
- **Environment rename (decided in #1480, confirmed by the owner
  2026-11-23).** While rule items bind environments by id, a rename cannot
  widen a machine's reach, so the reauthentication step does not apply to it.
  #1447, which binds environment selectors by name, must apply the
  reauthentication step to machine principals gaining reach through a rename
  or create. Creating an environment already widens an automation *all* or
  *all, except* rule without the gate, the same as a project-scope grant.

## Rejected alternatives

- **Levels as stored roles** (iteration 2 and 3's View / Edit / Publish /
  Admin buttons): hid which atoms a level carried, and "Admin" on a folder
  silently lost project-wide powers.
- **One include / except tree** over org › project › environment › folder ›
  key (iteration 2): one tap per environment for a folder in several
  environments.
- **Allowlists on the resource** (iteration 1b): can say "only these people
  may reveal `db/`", cannot say "this person may only edit `stripe/`".
- **Expanding selectors to key lists at write time:** exceptions would fail
  open (a new secret in an excepted folder becomes reachable).
