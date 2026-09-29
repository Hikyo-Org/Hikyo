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
- Storybook: `pnpm --dir web run storybook`, group *Prototypes/Member access*.

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
- **Owed by stage C:** the Members and Who can...? WebUI on these routes
  (names through the `read`-gated catalogue routes), and flipping the four
  `issue: 838` rows in `api/parity.yaml` to `webui`. 838 is this PR, so
  `scripts/ci/check-parity-issues.sh` fails until then (or until a real
  implementation issue replaces it).

