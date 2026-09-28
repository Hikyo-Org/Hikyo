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
