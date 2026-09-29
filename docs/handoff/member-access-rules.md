# Handoff: member access rules

Finer-than-scope member access (rules with a Where of projects, environments
and keys, rule-local excepts), from prototype to a working, deliberately
fail-closed first slice. Normative contract:
[member-access-rules.md](../adr/member-access-rules.md), especially
*First implementation slice*.

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

## Owed

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
  *First implementation slice*); Machine access rules (refused server-side).

