# Issue #446 — environment lifecycle UI

Issue: https://github.com/Hikyo-Org/Hikyo/issues/446

**State: implemented; exact-head verification passed before the queue-gated
main merge.** Project settings now exposes the
four existing environment lifecycle operations: rename, delete, whole-set
reorder, and atomic clone-at-creation.

## Contract

- Each environment row owns rename, move up/down, clone, and typed-name delete
  controls under one `Manage environment` disclosure.
- Reorder sends every current environment id exactly once.
- Clone reports both copied values and secrets the source-material gate omitted.
- Lifecycle refusals preserve caller-safe details and keep uniform 403/404
  responses indistinguishable.
- Successful mutations invalidate the environment list plus every project
  matrix cache family affected by topology changes.
- Environment deletion is destructive by backend contract: it removes the
  environment and its values, drafts, revision history, pins, and snapshots.
  The UI states this before enabling typed-name confirmation.

## Migration and generated outputs

No schema migration or generated output is needed. The SPA consumes the four
operations already present in the generated TypeScript client.

## Validation

Focused Vitest coverage exercises all four requests, refusal mapping, and the
rename/delete confirmation flow. The settings Playwright flow proves create,
rename, delete, then project delete through the SPA. Host memory pressure stayed
elevated, so no local Node suite was started. Trusted CI run `32731450349` on
head `fd38288dddff441f69684b6a8a1272ca566e0662` passed the SPA verification
(typecheck, full Vitest suite, and build), app build, docs, no-egress, and both
desktop and mobile Playwright flow suites. The mobile pass verifies the 44 px
touch target added after the preceding run exposed it. The queue-gated merge
head must repeat the repository's required gates before landing.

## Review disposition

First two-axis review found four issues: centralize matrix cache prefixes,
remove a new type assertion, deduplicate lifecycle refusal mapping, and add
this handoff. The spec review then found missing matrix-key invalidation and a
delete-success callback that could be lost when invalidation unmounted its row.
Both were fixed. Final standards and issue-spec rechecks returned `CLEAN`.
