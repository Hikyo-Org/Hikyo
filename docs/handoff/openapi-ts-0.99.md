# Handoff: `@hey-api/openapi-ts` 0.97.3 to 0.99.0

## Decision

Lift the 0.97.3 hold recorded in `upgrade-go-1.27-dependencies.md` and
`dependabot-5-js-yaml.md`. The hold said 0.99.0 adds `as` assertions and an
ESLint suppression. Measured on the regenerated `clients/ts/src/generated`:

- `as` assertions fall from 25 to 24. 0.97.3 already emitted them in the
  bundled runtime; 0.99.0 replaces four `as Record<string, unknown>` slot
  writes in `core/params.gen.ts` with one `writeSlot` helper holding two.
- The only new suppression is
  `// eslint-disable-next-line @typescript-eslint/no-empty-object-type` on the
  empty `ClientMeta` interface in `core/types.gen.ts`. Upstream added it on
  purpose (hey-api/hey-api#3996): the empty interface is the declaration-merging
  point for typed `meta` values and must stay an empty interface. `clients/ts`
  runs no ESLint, so the comment is inert here.

Staying on 0.97.3 therefore never kept the generated client free of `as`
assertions; 0.99.0 slightly reduces them. Upstream `main` (checked 2026-11-13)
carries the same code and no upstream issue reports it. Making the generated
client fully assertion-free would be a separate upstream change to the bundled
runtime, not a reason to hold this version.

## Still in force

- The `js-yaml` override from `dependabot-5-js-yaml.md`.
- `typescript` stays aliased to `@typescript/typescript6` for the generator's
  compiler API (`upgrade-typescript-7.md`).
