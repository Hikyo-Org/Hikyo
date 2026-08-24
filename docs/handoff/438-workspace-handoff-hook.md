# Issue #438 — shared workspace handoff launcher

## Outcome

- `useWorkspaceHandoff` owns establishment, step-up, unavailable, contacting,
  ready, authorising, failure, retry, and unmount-safe completion state.
- `WorkspaceStepUp`, `RemoteCard`, and `Reconnect` use the shared controller and
  shared button-state mapper.
- Popup launch remains synchronous to the ready action's click; authorising
  launchers stay disabled until completion.
- Failed preparation renders `Try again`; `Contacting…` is rendered only while
  `prepareWorkspace` is running.

## Validation

- `pnpm exec vitest run src/routes/useWorkspaceHandoff.test.tsx src/routes/WorkspaceHandoffConsumers.test.tsx` — 2 files, 8 tests passed.
- `pnpm run typecheck` — passed with TypeScript 7.0.2 on Node 26.7.0.
- `pnpm test` — 44 files, 339 tests passed.
- `pnpm run build` — production Vite build passed.

## Delivery guard

Before any branch refresh or merge, re-query open non-draft pull requests and
proceed only when this pull request has the lowest number. Stop after GitHub
reports the exact reviewed head merged; post-merge CI is outside this task.
