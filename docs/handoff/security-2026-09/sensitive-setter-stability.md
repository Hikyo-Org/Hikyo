# Handoff: sensitive-state setter stability (CI-red regression)

## Symptom

On this PR the web unit suite went from EXIT 0 on baseline (HEAD `5002d852`) to
EXIT 1, with two PR-untouched test files crashing a vitest worker:

- `web/src/routes/Values.ceremony-task.test.tsx` (hung, EXIT 124)
- `web/src/routes/Values.write-feedback.test.tsx` (EXIT 1, 2 unhandled)

The worker died with a V8 heap OOM in `RunMicrotasks` after ~42s: an unbounded
promise/setState loop, not a network or memory-tuning issue. Baseline forks vs.
`--no-file-parallelism` gave identical counts, killing the memory theory.

## Root cause (causal chain)

1. F22 (adversarial review 2026-09-17) enabled the oxlint `react` plugin,
   including `react/exhaustive-deps` as an error.
2. The sweep mechanically added the missing dependencies it flagged. In
   `web/src/routes/Values.tsx` that added `setDisclosed` to three effect/memo
   dependency arrays (L130, L148, L291).
3. `setDisclosed` is a setter returned by `useSensitiveState`
   (`web/src/api/sensitiveMutation.ts`). It was a plain arrow rebuilt on every
   render, so it had a new identity every render. `exhaustive-deps` cannot see
   through a custom hook, so it required the setter in the array and could not
   know it was unstable.
4. The effect at Values.tsx L144-148 calls `setDisclosed` unconditionally. New
   setter identity every render -> effect re-runs every render -> setState ->
   re-render -> unbounded loop -> heap OOM. The updater also allocates a new
   object each run, which is what turned "runs every render" into a hard loop.

Bisection was empirical: swapping baseline `Values.tsx` into the PR worktree made
`write-feedback` pass in 663ms; the PR version OOMed. Swapping baseline
`useCeremonyTask.ts` or `queryClient.ts` did not help, isolating the cause to
`Values.tsx`'s new dependency, i.e. the unstable setter at its source.

## Fix

Stabilise the setter and the transfer factory at the source, matching React's own
`useState` setter contract, in `useSensitiveState`
(`web/src/api/sensitiveMutation.ts`):

- `set` -> `useCallback(..., [lifetime, generation])`
- `prepareTransfer` -> `useCallback(..., [queries, lifetime, generation])`

Identity only changes when the retirement generation does, and each closure still
captures the generation current when it was created, so every revocation check
(mounted, revision, generation, session/principal, client) is unchanged. The
existing `useEffect(..., [lifetime])` already proves `lifetime` is render-stable
(it is green on baseline). The Values.tsx dependency additions are kept: with a
stable setter they are now correct, not a trap. Reverting them or adding a disable
directive would be the band-aid.

## Verification

- Both former crashers: 12 tests pass in ~0.7s, EXIT 0.
- Full unit suite: 125 files, 1069 tests, EXIT 0, zero unhandled.
- `oxlint --deny-warnings`: 0. `tsc --noEmit`: 0.
- Regression check added to `sensitiveMutation.test.tsx`: renders a
  `useSensitiveState` surface, forces two re-renders, asserts the setter and the
  transfer factory keep one identity across renders. Without it the only guard was
  a 60s OOM hang, a terrible failure signal.

## Sensitivity pin

`sensitiveMutation.ts` is governance-pinned (`sensitiveInventory.json` +
`sensitiveInventory.test.ts`). Renewed sensitivity review for this change:

> setter and transfer identities stabilised via `useCallback` keyed on
> lifetime/generation; the revocation checks and the cached-payload surface are
> unchanged. No new mutation, no new cached payload class, plaintext still never
> registered with TanStack.

Hash refreshed to
`35e37cd6f77f2af83e25b9851b230bee52cae6c5ce1d6f490423328c65c71430`.

## Known latent (out of scope, not triggered)

`useSensitiveMutation` returns `mutate` / `mutateAsync` / `reset` as inline
function declarations, also unstable per render. This is pre-existing (identical on
baseline) and no caller currently puts one of them in a dependency array, so it
does not loop today. If a future `exhaustive-deps` sweep adds one to a dep array,
apply the same `useCallback`-at-source fix. Flagged so the next lint sweep checks
custom-hook return identity before adding dependencies.

## Review lesson

A lint-rule enablement that auto-adds dependencies is only safe if every value it
adds has a stable identity. Custom hooks that return functions must memoise them,
or the sweep converts a passing suite into an every-render loop that only surfaces
as an OOM in an unrelated test file.
