# Hikyo member access rules: scoped rules, rule-local exceptions, key-level limits (ADR, decisions owner-approved 2026-09-28)

> **Status: declared amendment to [permission-model.md](./permission-model.md), operative with the
> implementing PR ([#838](https://github.com/Hikyo-Org/Hikyo/pull/838)).** The owner took every decision below on
> 2026-09-28 while iterating the [member-access prototype](../site/public/prototypes/member-access/)
> (iterations 1 to 6; iteration 6 is the reference, mirrored in Storybook under *Prototypes/Member access*),
> and on the same night directed the full-stack implementation and merge on green ahead of the review.
> The [oss-mechanics.md](./oss-mechanics.md) amendment procedure still owes the reopening of
> [#15](https://github.com/Hikyo-Org/Hikyo/issues/15) and the cross-provider adversarial review; both are
> recorded as open in [the handoff](../handoff/member-access-rules.md). The first slice is deliberately
> fail-closed (see *First implementation slice*): where it is narrower than the decisions, the decisions
> are the target and the slice is the floor.

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
  `prod` environments.
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
  and moves; a deleted key drops out.

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
  decision was taken).
- **Folder rename:** whether folder selectors bind to the folder path or a
  folder id is open. Path binding makes a folder rename silently narrow every
  folder pick; id binding requires folders to become identified entities.
- **API and CLI** need a spelling for `where`; the api-cli-surface ADR owns it.
- **MVP boundary:** this moves "key-scoped reveal" from fog to a decided
  design; scheduling is a separate call.

## First implementation slice (2026-09-29)

The implementation is built so that its worst possible bug is a narrowed rule wrongly denied, never access
widened. Rules are stored in their own tables (`rules`, `rule_items`, migration 00070) and read only inside
the authorization chokepoint; every other coverage check (delivery, SSH, federation, the disclosure gate,
the lockout census, the grantor bound, navigation) stays blind to them and therefore conservative. Where
this slice is narrower than D1 to D11:

- **Humans only.** Rules for machine principals are refused; machines keep legacy grants. The machine
  widening gate counts reach per environment and would undercount a key-narrowed machine reveal.
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
  created later are included. **Folders bind by exact path,** so a folder rename narrows folder picks
  (the open point below).
- **Publishing stays environment-wide:** a key-narrowed rule never satisfies publish, and a folder-scoped
  Define keys rule cannot yet create or delete keys in a project that has environments unless publish is
  held environment-wide.
- **No "all projects" on rules** in this slice; a rule names its projects.
- **The move-widening confirmation (D9)** counts gains through rules only (legacy grants are
  folder-blind), so it may name someone who already had access through a grant, never fewer people.
- **Resolver (D10)** answers in the web client over the org's grants, rules and key catalogue; a
  server-side resolver operation is owed.

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
