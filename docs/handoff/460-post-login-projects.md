# Issue #460: post-login projects landing

## State

Implemented in this PR. Authenticated visits to `/login` now land on the real,
state-aware Projects surface instead of the inert Overview placeholder. The
deferred Overview dashboard remains unchanged.

## Contract

- An authenticated visit to `/login` redirects to `/projects` with history
  replacement.
- The Projects page owns the zero-organisation, zero-project, and populated
  states.
- The Overview route remains available from the sidebar pending its dedicated
  dashboard ticket.
- The zero-organisation state never renders the stale “choose a project”
  instruction beside “No organisations yet.”

## Evidence

- `web/e2e/flows/shell.spec.ts` asserts the authenticated `/login` redirect URL
  and Projects heading.
- The shell zero-organisation test asserts the stale Overview instruction is
  absent.

## Validation

Run from the repository root with the pinned Node version:

```sh
fnm exec --using 26 -- corepack pnpm --dir web run typecheck
fnm exec --using 26 -- corepack pnpm --dir web run test
fnm exec --using 26 -- corepack pnpm --dir web exec playwright test web/e2e/flows/shell.spec.ts
```
