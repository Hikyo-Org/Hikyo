# Handoff: member access rules

Finer-than-scope member access (rules with a Where of projects, environments
and keys, rule-local excepts), from prototype to a working, deliberately
fail-closed first slice. Normative contract:
[member-access-rules.md](../adr/member-access-rules.md), especially
*First implementation slice*.

## Current implementation (2026-10-05)

This update supersedes the historical first-slice limits and edit sequence below.

- Members presents legacy grants and human rules as access cards. Legacy grants
  keep their existing authority; machines and all-future-project access remain
  on that path. Human Edit, Publish, Reveal/history, Define keys and Manage
  access honor key selectors. See remains environment-wide.
- Rule edits and removals use one atomic replacement operation. A delegated
  manager sees only manageable rules; without See, existing stable ids remain
  editable without disclosing project, environment or key names. A partial rule
  listing does not produce a global Who can...? census.
- Definitions are shared across the project. Selected-environment Define keys
  can create an empty key outside a linked key group. Changing an existing
  definition requires Define keys and Publish in every project environment,
  irrespective of value presence. Creating a key in a linked key group requires
  the same all-environment authority before inspecting the group. These checks
  avoid revealing which
  environments contain values or which keys belong to a group.
- Supplied first values need Define keys and Publish at each destination.
  `POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/values/{key}/initialize`
  stages a draft while the published cell is absent. The owner may revise their
  initial draft; another owner's draft blocks initialization. The new-key form
  and scanner acknowledgement retries use this path. Publishing stays explicit.
- Who can...? retains Yes, rule-local Except, and missing-See reasons. Its
  no-rule-reaches outcome is visible and collapsed by default, matching
  iteration 6.

## What shipped in this PR (#838)

- **Prototypes, iterations 1 to 6**, at `docs/site/public/prototypes/member-access/`
  (published under `/prototypes/`); `member-access/index.html` lists each
  verdict. Iteration 6 is the design reference.
- **ADR:** [member-access-rules.md](../adr/member-access-rules.md) (declared
  amendment, operative with this PR by owner direction) with a banner on
  [permission-model.md](../adr/permission-model.md); **DESIGN.md** gains the
  *Review rules* distilled from the owner's feedback.
- **Backend (stage A):** migration 00070 (`rules`, `rule_items`); rules read only
  inside the authorization chokepoint, humans only; key-aware authorization for
  single-value reveal, staging a value, key create, rename, delete and move
  (both folders); proofs from key-narrowed rules bound to their key; deletion
  paths prune rule items; audit events `rule.created`, `rule.revoked`,
  `rule.move_widening_confirmed`.
- **API and CLI (stage B):** `GET /orgs/{org}/rules`,
  `GET /orgs/{org}/projects/{project}/rules`, `POST /orgs/{org}/rules`,
  `DELETE /orgs/{org}/rules/{rule}`; `confirm_widening` on key metadata update
  and definitions apply with a 409 `WideningRefusal`; `hikyo access rule
  list|add|remove`; regenerated clients; docs pages and ledger entry
  `CAP-MEMBER-ACCESS-RULES`.
- **Web (stage C):** on Members, an *Access rules* panel (editor, search),
  *Who can reach one key?* over grants and rules, a glossary; the key folder
  move paths show the widening confirmation. Stories under *Members/Access
  rules*; new atoms `ToggleChip`, `Disclosure`, Dialog `pinActions`.

## Conservative decisions taken overnight (owner asleep, "trust your judgement")

Each is the fail-closed reading; widen only by owner decision.

- **Humans only:** rules for machines are refused (the machine widening gate
  counts per environment and would undercount a key-narrowed machine reveal).
  The API, the CLI and the Machine access rule editor accept
  workload and automation rules under the conditions in the ADR's *Machine
  rules* section.
- **Publish cannot be key-narrowed:** publishing is environment-wide, so a
  key-narrowed publish could never take effect.
- **See cannot be key-narrowed** on a rule (refused rather than widened).
- **Manage access on a rule is inert** until delegation containment exists;
  creating and revoking rules needs legacy manage-members on every named project.
- **No "all projects"** on rules.
- **Only project member managers may confirm a widening key move**, and only
  they are told who gains; a machine plan can never commit one.
- **Folder excepts cover subfolders; only-picks match exactly.**

## Reviews

- Two same-provider (Claude) adversarial security reviews: stage A (five
  findings, all fixed with tests) and stage B (one finding: a count-only caller
  could confirm a widening move; fixed, mutation-checked). Stage C was reviewed
  by the orchestrator (sensitive inventory note, 409 parsing through zod,
  partial-edit failure messages).
- **Cross-provider adversarial review: skipped (Codex quota below threshold), owed.**
  Not CLEAN.

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

## Owed at the first-slice handoff (historical)

For the October 16 disposition, see the
[review reconciliation](15-member-access-review.md). #15 has been reopened.
The October 5 delivery supersedes the delegation, key-aware publication,
definition-mutation and navigation items below. On 2026-10-16 the owner accepted
completed Astra-high verification plus independent Sol 6.1-high source review as
the formal review disposition for #15 only. The quota-gated pass remains skipped,
not retroactively CLEAN; the linked reconciliation records the exact scope and
limitations. This historical list does not reverse the operative owner direction.

1. **Reopen #15** and record the amendment there, per the
   [oss-mechanics.md](../adr/oss-mechanics.md) locked-decision procedure.
2. **Cross-provider adversarial review** of `member-access-rules.md`. Not run in
   this PR: it was skipped, not passed. Focus points: D1 (grant widened to
   `where`), D4 (rule-local except as the only negative), D8 (live folder
   selectors), D9 (widening-move confirmation as the only guard against folder
   laundering).
3. **Open points** listed in the ADR: machine rules; folder selectors bound by
   path or by folder id; the API / CLI spelling of `where` beyond this slice;
   MVP-boundary scheduling.
4. **Widening the slice** (each needs its own review): delegation containment
   for rule-based Manage access; key-aware publish (enumerate draft keys before
   the proof); key-aware update-declaration, reclassify and set-group; "all
   projects" rules; rules visible to navigation (`OrgsForPrincipal`,
   `project.list`); a server-side resolver operation.

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
  subfolders, only-picks exact, See, Pin and Publish never key-narrowed,
  Manage access inert on rules, Define keys at project level, key-narrowed
  rules count only for reveal, edit and definitions-edit asked about one key); its
  unit tests pin that parity. An edit diffs the old rule: same Where moves
  only changed permissions, a new Where creates the new rows first and
  revokes the old ones after, and a refused create rolls back what it made.
  Key names come from `listKeys` (See on the project); without it the editor
  cannot pick folders or keys there and Who can...? asks for the whole
  environment, both saying so. Every key folder move (key detail folder
  field, Matrix Cleanup, definitions apply) answers the 409 `widening` with
  `KeyMoveConfirmDialog` and resends with `confirm_widening`; a count-only
  refusal offers no confirm. Parity rows flipped to `webui: members`.
- **Publish, reconciled:** Publish is environment-wide in this slice. The
  server refuses a rule that carries Publish with any key narrowing, and the
  web treats Publish as needing all keys of an environment, so neither the
  editor nor Who can...? can present a key-narrowed Publish.
- **Still open after stage C:** a person with neither a grant nor a rule is
  not listed on Members, so the first access for an invitee comes from the
  invite template or a grant; a server-side resolver operation (ADR
  *First implementation slice*). The Machine access rule editor exists
  (#1482).

## All-projects rules (#1446, owner decisions A, S1 and R2, 2026-11-25)

Branch `feat/1446-all-projects-rules`. The decision is recorded in the ADR's
[Owner decisions, 2026-11-25](../adr/member-access-rules.md#owner-decisions-2026-11-25).

- **Storage (migration 00099, both engines).** `rules.project_mode` (`all` or
  `only`, default `only`) and a nullable `rule_items.project_id`, allowed only
  for folder items; SQLite rebuilds `rule_items`. An all-projects rule stores
  no project item; its folder items carry no project.
- **Reads.** Every `rule_items` read is a LEFT JOIN, so an item-less
  all-projects rule is still loaded, listed and revocable. `ListRulesForProject`
  and the project listing include `project_mode = 'all'` rules, so evaluation,
  the D9 move-widening census and navigation (`project.list`, org reach) see
  them. Project, environment and key pruning match `project_id = ?` and never
  touch an all-projects rule.
- **Domain.** `Where.AllProjects`, `Names`, `KeysIn` and `AnyProject`;
  `Validate` refuses environment items (`ErrRuleAllProjectsEnvironments`, until
  #1447), single keys (`ErrRuleAllProjectsKeys`) and named projects.
  Containment evaluates the future-project witness `~future-project`.
- **Authority (D7).** Create and revoke prove the caller in every current
  project (`AuthorizeRule`, `RuleCapabilityHeld`) and in the witness
  (`RuleFutureProjectsHeld`): only an org or instance grant, or another
  all-projects rule, reaches it.
- **Zero projects.** Create in an org without projects is a 409
  (`ErrRuleAllProjectsNoProject`). Project deletion locks the org's projects
  FOR UPDATE (rule creation takes them FOR SHARE) and refuses the last one
  while all-projects rules exist, naming their ids in the 409 detail.
- **Wire.** API revision 21. Responses carry `project_mode` and expand an
  all-projects rule over the org's current projects (only the addressed one on
  a project listing, with `other_projects`). The CLI table shows it once.
- **Create spelling (R2).** The create keeps `where`; `project_mode: all`
  comes with the org's current projects and the folder items repeated under
  each. The server collapses that to the stored folder list (uneven folders
  are a 400) and `service.checkClaimedProjects` compares the list with the
  org's projects, locked FOR SHARE in the create transaction once Manage is
  proved in each: a missing, extra, deleted or duplicate project is a 409
  (`ErrRuleAllProjectsStale`) naming the difference. An org listing's or a
  create response's `where` is a valid create; a project listing's is not (it
  shows one project). Outside the org-wide Members page the editor cannot save
  an all-projects rule as is. The last-project 409 names the blocking rule ids
  only to a caller who also manages the org's members. `hikyo access rule add --all-projects` and the Members editor's
  "All projects" choice list the projects first.
- **Tests.** `internal/isolation/member_access_rules_all_projects_test.go`
  (both engines; the concurrent-deletion test is PostgreSQL only).

## Environments by name (#1447, owner decision of 2026-11-26)

Branch `feat/1447-env-name-binding`. The decisions are recorded in the ADR's
[Owner decisions, 2026-11-26](../adr/member-access-rules.md#owner-decisions-2026-11-26).

- **Storage (migration 00102, both engines).** `rule_items.env_name` replaces
  `env_id`; name items carry no project. `rules.env_fault` holds the
  manager-facing reason of a rule the upgrade disabled. The id columns and
  every id-matching query are gone.
- **Migration, option (c).** Consistent rules, whose projects select the same
  names, convert in place. Others split
  into one rule per project that reached something, audited as `rule.split`
  with the old and new ids. Undecidable rules (an item naming a missing
  environment, a project outside the rule, a repeated name, an all-projects
  rule holding environment items, or a whole-project capability such as
  `project-settings` holding environment items, which never validated and
  which a split would have made live, and any other rule that did not pass
  `Rule.Validate` or `CheckMachineRule` before the upgrade and that a split
  could lift: more than `MaxRuleItems` items, a repeated project, an item
  naming an empty project, environment or key id, a key or folder repeated
  in a project, a key or folder item outside the rule's projects,
  `key_mode` only with no key, key narrowing on `pin` or a whole-project
  capability, a machine rule over several projects) keep their row, reach
  no environment and carry `env_fault`, audited as `rule.disabled` with the
  removed `env_mode`, `env_ids` and `env_names` (each the first 1,000 in
  byte order) and `env_items` (the full count). Every rejection is
  tabulated under "00102 and pre-upgrade validity" below. Tests:
  `TestRuleEnvNamesMigrationSQLite` and `TestRuleEnvNamesMigrationPostgres`,
  including the 99/102 recovery boundary;
  `TestRuleEnvNamesMigrationShapeProjectList` pins the migration's
  whole-project and pin capability lists to `domain`, and its item cap to
  `domain.MaxRuleItems`. A faulted rule is never evaluated,
  whatever items it holds: the rule resolver skips it and `GetRule` returns
  it as not valid, so only a manager over its whole scope revokes it
  (`TestDisabledRuleListsAndRevokes`).
- **Domain.** `Where.EnvNames` is authoritative; `BindEnvs(refs)` derives the
  environment ids inside `authorize()` on every check, with no cache. A rule
  create binds its names before the caller is proved, reading one project's
  environments for a rule over one named project and the org's otherwise
  (`TestRuleCreateReadsOneProjectsEnvironmentsBeforeAuthorization`).
  All-projects rules take names (restriction A lifted); single keys stay
  refused there. Names are stored once per rule, but the item bound counts
  them per project, as `RuleWhere` lists them: projects × (1 + names) + key
  items, or projects × (1 + folders + names) for an all-projects rule. Sent names take
  the environment name grammar. Rule lifecycle audit events record
  environments as `name:<name>`.
- **Widening (like D9).** `internal/service/rule_env_widening.go`. Environment
  rename, create, clone and definitions apply compute who gains through their
  rules and refuse a 409 unless `confirm_widening` names exactly them; the
  confirmation is audited as `rule.env_widening_confirmed`. Self-config
  provisioning carries no confirmation, so it refuses a widening. A created
  environment that an `all` rule does not except needs no confirmation. The
  census covers every environment- and key-shaped capability (derived from
  the rule shapes, so Pin is included).
- **Old clients.** A pre-revision-22 create naming ids is refused when the ids
  are not every environment their names match (`ErrRuleEnvItemsStale`). An
  `all` create or replacement without names is refused when the principal's
  `all` rules except a name no environment of one of its projects carries
  (`ErrRuleUnmatchedNamesDropped`), since that client cannot show the name.
  The web and CLI therefore always send names to a revision-22 server (`[]`
  for every environment). Two explicit steps (revoke, then create) still drop
  such an exception; each is a proved manager's own action.
- **Machines.** A gaining machine passes the #1473/#1480 gate. A create or
  clone whose name gives a machine secret plaintext in the new environment
  through an *only* rule fails closed: refused for want of `manage-identities`
  or disclosure authority, or else with reauthentication required, because
  no window can name the new id; the workaround is to create it under a name
  no *only* list names and rename it. An `all` rule that does not except the
  name reaches the new environment by default, which needs no confirmation and
  no workaround, so the workaround's create succeeds for such a machine too
  (the `uat` control in `TestEnvironmentNameWideningGatesMachines`).
  The gate's reauthentication covers secret plaintext only (`read` with
  `reveal` or `reveal-history`); a machine gaining `edit`, or `read` alone,
  needs the confirmation and no window. That config-plaintext gap is the
  follow-up [#1630](https://github.com/Hikyo-Org/Hikyo/issues/1630). A machine
  rule create racing a rename into its name takes no project lock: on
  PostgreSQL the SERIALIZABLE write transactions abort one side (each reads
  what the other writes), and the retry meets its gate; a row lock taken after
  the first statement would not change that.
  `TestMachineRuleCreateRacingEnvironmentRenameFailsClosed` holds the create
  after its name binding, and the rename after its census, with two serial
  controls. With `writePostgresOnce` at REPEATABLE READ or READ COMMITTED,
  both held subtests fail with the create and the rename committed.
- **Deletion (owner decision 2026-11-27).** Deleting the last environment
  carrying a name an `all` rule excepts lets that rule reach the project as a
  whole; it already reached the remaining environments. Deleting one of
  several excepted environments gains nothing. A delete that gains fails
  closed: the service delete and a definitions apply delete are refused like a
  widening rename until `confirm_widening` names exactly the gainers, audited
  as `rule.env_widening_confirmed` with an empty `to_name`. The rule keeps the
  name, so a later environment of it is excepted again and widens nothing
  through that rule; another rule naming it, such as an *only* rule, still
  needs its gainers confirmed.
  Project and organization deletions never cascade into environments.
- **Wire.** API revision 22, additive: `where.environments.names` and
  `fault`, `confirm_widening` on environment create, rename and clone, and as a
  query parameter on environment delete, and `widening.gainers[].keys`. CLI:
  `--only-env-name`/`--except-env-name`, refused before revision 22;
  `env delete --confirm-widening`.
- **Web.** The rule editor selects environments by name in every project,
  including all-projects rules; Project settings confirm a widening rename,
  create, clone or delete and run the reauthentication ceremony for a
  widening rename only: a delete reveals no plaintext that was hidden before,
  so it asks for none.
  `KeyMoveConfirmDialog` is now `WideningConfirmDialog`, shared by key moves
  and environment changes.
- **Gainers name their key selector.** A widening refusal's gainer carries
  `keys` (a `RuleKeyAxis`) when the gain reaches only the keys a narrowed
  rule selects, so a rule narrowed to folder F that gains environment E reads
  as E over F, not the whole environment ("Edit (staging, only db/)"); See
  is never key-narrowed, so a See gain carries none.
  Gainers merge per principal, capability, project-or-environment and
  selector (`gainsOf`, `mergeGains`); a gain over every key absorbs a
  narrowed one for the same environment, and different selectors stay
  separate. A key folder move's gain leaves `keys` absent: it concerns the
  moved key, which the refusal names. Display precision only: who must
  confirm (`gainedPrincipals`) and the machine widening gate are unchanged.
- **Tests.** `internal/isolation/rule_env_widening_test.go` (both engines):
  `TestEnvironmentNamesWidenRules`, `TestEnvironmentNameWideningGatesMachines`,
  `TestAllProjectsRulesFutureProjectsHoldNames`,
  `TestAllProjectsRulesBoundCountsNames`,
  `TestDefinitionsApplyEnvironmentNameWidening`,
  `TestEnvironmentDeleteWideningExceptRules`,
  `TestDefinitionsApplyEnvironmentDeleteWidening`,
  `TestEnvironmentDeleteWideningShapes`,
  `TestDefinitionsApplyEnvironmentDeleteAndRenameNarrows`, and the machine
  delete case in `TestEnvironmentNameWideningGatesMachines`; web
  `ProjectSettings.test.tsx` "confirms a delete that widens access", web `model.test.ts`
  "environments by name (#1447)" and the AccessRules stories.
- **Open.** An owner may later exempt machine create and clone from the
  fail-closed reauthentication (ADR option). #1474 remains. Found in review, older than #1447:
  a delegation check resolves each key item of a held manage rule in an
  existing project, which the missing-project decoy does not, so a caller
  holding such a rule can tell the two apart by query work.
- **Known limits, fail-closed by design.** Each of these asks for a
  confirmation, or refuses, where an exact check would not. Loosening any of
  them would admit something refused today, so they stay.
  - A definitions plan that moves a key and deletes the environment the move
    gave access in still asks to confirm that gain, though it never becomes
    reachable.
  - A pre-revision-22 `all` create without names is refused while any of the
    principal's manageable `all` rules over the project excepts a name no
    environment carries, even when the new rule would not replace it.
  - A plan's environment census reads the rules before the plan runs, so a
    rule the plan revokes or prunes (for example by deleting its last key)
    still counts, and its principal must be confirmed.
- **Accepted limits.** These cost capacity or query work, not access; none
  admits anything an exact check would refuse. The ADR sanctions both
  (owner, 2026-11-28; ADR "Names are stored and matched" and "Migration, option
  (c)").
  - The upgrade's per-project split (00102) does not check the 1,000-rule
    `MaxRulesPerOrg` cap, so an org can exceed it after the upgrade; new
    creates are refused until rules are removed.
  - A rule-authorized check binds names within the chain's project
    (`RulesWithin`), so it reads that project's environments: one extra
    environment read per check, however many orgs the caller's rules span
    (`TestProjectCheckBindsRulesFromOneEnvironmentRead`).
- **Refusal uniformity.** Checks before the caller is proved read only the
  request (one stand-in name per sent environment id); a refused caller gets
  the same not-found whatever ids exist
  (`TestRuleCreateRefusalDoesNotProbeEnvironmentIDs`). The missing-scope decoy
  runs the same per-project environment read a refusal does.

### 00102 and pre-upgrade validity

A row that failed base validation reached nothing, so 00102 must not let a
split part or a converted row validate. Sources are at the merge base
`2dd3f0c19`: `internal/domain/rule.go` (R) and `internal/store/authn/rules.go`
(L). Fault clauses are in the `env_fault` statements of both engines'
00102. "Holds" means the defect survives in every split part and in the
converted row. Two facts carry most proofs:

- Split parts copy `org_id`, `principal_id` and `capability`.
- A rule over one project is never split. Environment names are unique per
  project (00003), so every environment carrying one of its names is one of
  its items. Neither split predicate matches, and its row is converted with
  only its environment items rewritten.

Seeds are in `testRuleEnvNamesMigration`. Every mutant failed on both
engines with rc=1 and was restored.

| Source | Rejection | 00102 disposition | Seed | Mutant |
|---|---|---|---|---|
| R:194 | org empty | Holds: copied to every part | none | n/a |
| R:198 | capability not held on rules | Holds: copied; `ruleShapes` unchanged | none | n/a |
| R:205 | no project | Holds: both split predicates join a project item, so the row is converted and still names none | none | n/a |
| R:210 | empty project id | Fault `z.project_id = ''` | `rul_pemp` | `pemp` |
| R:210 | project named twice | Fault (duplicate project) | `rul_dupp` | `dupp` |
| R:215, R:218, R:286 | unknown env or key mode | Impossible: `CHECK` (00070) | none | n/a |
| R:221 | key item with no project | Key: cannot be stored with a NULL project (`rule_items_project_bound`, 00099); a key under an empty project id is storable and faults as R:210 (`z.project_id = ''`). Folder: fault (outside the rule's projects) | `rul_knull` | `kout` |
| R:226 | env item outside the rule's projects | Fault (outside the rule) | `rul_out` | `out` |
| R:230 | empty env id | Fault `z.env_id = ''` | `rul_eemp` | `eemp` |
| R:230 | env named twice in a project | Fault (name twice) | `rul_twice` | `twice` |
| FK (00070) | env item naming a missing env | Fault (missing environment) | `rul_gone` | `gone` |
| R:236 | env `only` with no item | Holds: no name, so no split; converted `only` with no name fails current `validateEnvNames` | none | n/a |
| R:241 | key or folder outside the rule's projects | Fault | `rul_kout` | `kout` |
| R:245 | key item with empty key id | Fault `z.key_id = ''` | `rul_kemp` | `kemp` |
| R:245 | folder item carrying a key id | Impossible: `CHECK` (folder ⇒ `key_id IS NULL`), and L:101 builds folders without one | none | n/a |
| R:245 | key, or folder, named twice in a project (struct equality, so a key never equals a folder) | Fault (duplicate key or folder) | `rul_dupk`, `rul_dupf` | `dupk`, `dupf` |
| R:251 | key `only` with no key item | Fault | `rul_konly` | `konly` |
| R:254 | more than 500 items (projects + env items by id + keys) | Fault `COUNT(*) > 500`; the 500 is pinned to `domain.MaxRuleItems` | `rul_big`, `rul_b501`; `rul_b500` stays live | `big`, `cap501`, `cap499` |
| R:264 | Pin narrowed by keys | Fault | `rul_pin` | `pin` |
| R:268 | whole-project capability narrowed by keys | Fault | `rul_psk` | `psk` |
| R:268 | whole-project capability with env items | Fault (M2) | `rul_ps`, `rul_psc` | `ps` |
| R:268 | whole-project capability, env `only` with no item | Holds: as R:236 | none | n/a |
| R:280–309 | all-projects shape: project items, keys, folders bound to a project, malformed or duplicate folders, key `only` with no folder, env `only` with no item, more than 500 folders | Holds: no split predicate matches `project_mode = 'all'`, the row is unchanged, and current `validateAllProjects` keeps each check | none | n/a |
| R:283 | all-projects rule with env items | Fault (all projects) | `rul_allp` | `allp` |
| R:420 | machine class or capability not allowed | Holds: principal and capability copied | none | n/a |
| R:424 | machine rule narrowed by keys | Over one project: holds (converted, keys kept). Over several: fault (below) | `rul_mch` | `mch` |
| R:428 | machine rule not over exactly one project | More than one: fault. None: holds as R:205. All projects: holds as R:280 | `rul_mch` | `mch` |
| R:437, R:441 | machine depth not met | Holds: over one project only (above); mode and names are kept | none | n/a |
| L:103 | unknown axis | Impossible: `CHECK` (00070) | none | n/a |

The `rule.split` audit lists at most one rule per project. A split rule
passed the cap, so it has at most 500 projects, under `rule_ids`'s
`MaxLen` of 1,000. `rul_wide` holds 1,001 env items; its `rule.disabled`
payload passes the registry with the first 1,000 ids and names in byte
order and `env_items` 1001 (mutants `nolimit`, `desc`, `noitems`, `noorder`
on SQLite, `nocollate` on Postgres).
