# Handoff: member access rules (prototype, proposed ADR amendment, Storybook)

Design work only: nothing here changes the running product. It records how the
Members surface should present finer-than-scope access, the decisions the
owner took, and what is owed before any of it becomes operative.

## What shipped in this PR

- **Prototype, iterations 1 to 6**, at
  `docs/site/public/prototypes/member-access/` (published under `/prototypes/`).
  Each iteration is frozen; `member-access/index.html` lists them with the
  verdict that led to the next. **Iteration 6 is the reference.**
- **Proposed ADR amendment** [member-access-rules.md](../adr/member-access-rules.md),
  with a pointer banner on [permission-model.md](../adr/permission-model.md)
  and a row in the ADR index. Status: proposed, not operative.
- **Storybook port of iteration 6** under *Prototypes/Member access*
  (`web/src/routes/accessRules/`): a framework-free model with unit tests, a
  fixture, the components, and stories with play assertions. Fixture data only;
  no API calls.

## Decision trail (owner, 2026-09-28)

| Iteration | Question | Verdict |
|---|---|---|
| 1 | Where does fine grain live: inline chips (a), allowlists on the folder (b), member profile (c)? | None clearer; add negative permissions |
| 2 | Rules (level, where, except) with one include / except tree | Wants Admin on one folder in some environments |
| 3 | Three axes: projects › environments › keys, each "all, except" or "only" | Words disagree across surfaces; Admin opaque; wants several permissions at once |
| 4 | One vocabulary, tickable permissions, presets, glossary | 1B See not forced; 2B Admin preset without secrets; 3 folder admins create keys in the folder, folder picks follow the folder, single-key picks follow the key id |
| 5 | Key add / move / rename simulator | A: a widening move needs confirmation |
| 6 | Widening move confirmation | Final. Except stays rule-local (no global deny) |
| Storybook review | Editor, Members, Who can...? | Where before permissions; search on Members; Members lists people only, machines' rules move to Machine access (D11); Who can...? form ordered Permission, Project, Environment, Key with descriptions; simulator dropped, key-move confirmation kept as its own component |

## Owed before the amendment is operative

1. **Reopen #15** and record the amendment there, per the
   [oss-mechanics.md](../adr/oss-mechanics.md) locked-decision procedure.
2. **Cross-provider adversarial review** of `member-access-rules.md`. Not run in
   this PR: it was skipped, not passed. Focus points: D1 (grant widened to
   `where`), D4 (rule-local except as the only negative), D8 (live folder
   selectors), D9 (widening-move confirmation as the only guard against folder
   laundering).
3. **Open points** listed in the ADR: machine principals and key-narrowed
   rules; folder selectors bound by path or by folder id; the API / CLI
   spelling of `where`; MVP-boundary scheduling.

## Implementation notes for the next ticket

- The model in `web/src/routes/accessRules/model.ts` is the executable
  statement of D3 to D9 (matching, shape limits, Reveal-needs-See, selector
  binding, access diff). Server-side evaluation must reproduce it inside the
  single `authorize()` chokepoint, with no cache.
- Existing grants migrate to a `where` with no key narrowing and the same
  scope; nothing about current reach changes.
- Audit: one event per grant as today, plus the widening-move confirmation
  with its named set.

## How to look at it

- Prototype: `python3 -m http.server -d docs/site/public/prototypes` then open
  `/member-access/6/` (or `/member-access/` for all iterations).
- Storybook: `pnpm --dir web run storybook`, group *Members/Access rules* (the pieces) and *Routes/Members* (`WithAccessRules`, `ProjectWithAccessRules`, the page).

## Implementation (stages A and B, branch feat/member-access-rules)

The first line above predates the implementation: stages A and B change the
running product.

- **Stage A** (f119e733, 431a6205): rule tables (migration 00070), evaluation
  inside `authorize()` only, key-aware authorization, deletion pruning, the D9
  folder-move confirmation. **Review fixes** (d5dd971c): folder excepts cover
  subfolders, the D9 census ignores privacy gates, rule-decided key writes
  conceal out-of-rule objects, gainers are named only to member managers, a
  missing rule revokes exactly like an unreachable one.
- **Stage B** (feat commit after d5dd971c): `GET/POST /orgs/{org}/rules`,
  `DELETE /orgs/{org}/rules/{rule}`, `GET /orgs/{org}/projects/{project}/rules`,
  `confirm_widening` on key metadata update and definitions apply, the 409
  `error.widening` member, `hikyo access rule list|add|remove`,
  `--confirm-widening`, regenerated clients. Spellings in
  [api-cli-spellings.md](../spec/api-cli-spellings.md).
- **Deviations from the brief:** listings carry environment and key ids and
  folder paths, not names (names need `read`, which a member manager may
  lack); the project listing shows only its own project's part of a rule.
- **Stage C (WebUI):** the Members page (org and `?project=` projection)
  gains three panels under the unchanged grant surface: *Who can reach one
  key?* (Permission, Project, Environment, Key over grants AND rules), *Access
  rules* (one card per rule per person, Add / Edit / Remove through
  `RuleEditorDialog`) and a folding *Glossary*; a *Find a member* search
  narrows the grant table and the rules. The scope inspector keeps its form
  and grant sentence and adds the people a rule gives the capability on the
  whole scope. Machines get no rule editor (humans-only slice). The web model
  (`web/src/routes/accessRules/model.ts`) groups server rules by principal and
  Where (one capability per server row), maps legacy grants into the same
  evaluation, and ports `domain.Rule.Reaches` (folder excepts cover
  subfolders, only-picks exact, See and Pin never key-narrowed, Manage access
  inert on rules, Define keys at project level, key-narrowed rules count only
  for reveal, edit, publish and definitions-edit asked about one key); its
  unit tests pin that parity. An edit diffs the old rule: same Where moves
  only changed permissions, a new Where creates the new rows first and
  revokes the old ones after, and a refused create rolls back what it made.
  Key names come from `listKeys` (See on the project); without it the editor
  cannot pick folders or keys there and Who can...? asks for the whole
  environment, both saying so. Every key folder move (key detail folder
  field, Matrix Cleanup, definitions apply) answers the 409 `widening` with
  `KeyMoveConfirmDialog` and resends with `confirm_widening`; a count-only
  refusal offers no confirm. Parity rows flipped to `webui: members`.
- **Publish, reconciled:** the ADR slice says a key-narrowed rule never
  satisfies publish; that holds for revision publish, rollback and apply
  (no key is named), but `value.set` authorizes edit and publish against one
  key, so a key-narrowed Publish rule does let its holder set that key's
  value. The web model counts it for a Who can...? question about one key
  (over-reporting is the safe direction for that question); no server test
  pins either reading yet.
- **Still open after stage C:** a person with neither a grant nor a rule is
  not listed on Members, so the first access for an invitee comes from the
  invite template or a grant; a server-side resolver operation (ADR
  *First implementation slice*); Machine access rules (refused server-side).

