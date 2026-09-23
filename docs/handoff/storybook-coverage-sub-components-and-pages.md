# Storybook coverage — route sub-components + page state-catalogues

Follow-on to PR #752 (top-layer docs-frame fix). Adds Storybook stories for the
presentational sub-components living inside route files, plus a state-catalogue
pattern for data-driven pages.

## What was added

### Sub-component tier (one commit)
Stories for the storyable sub-components that had no Storybook entry, grouped by
source file:

- **Atoms** — `HealthChip`, `SidebarVersion`, `ProfileUpdateBadge`,
  `FleetUpdateNotice`, `UpdateJobStatus`, `StepUpBanner`, `SidebarLinkItem`,
  `ThemeToggle`.
- **Confirm / mint dialogs** — `RevokeConnectionDialog`, `ConnectionMintDialog`,
  `RevokeCredentialDialog`, `DeleteAdapterDialog`. Each renders a native
  `<dialog>.showModal()`, so each sets `parameters: topLayerDocs` (the PR #752
  shared param) to keep the modal inside its docs frame.
- **Cards / rows / forms** — `RemoteCard`, `ConnectionRow`, `MintConnectionForm`,
  `CredentialForm`, `TargetForm`.
- **Refusal state** — `SystemScopeRefusal` (the deep-link answer the system-scope
  gate renders for a refused surface).

Storying these required adding `export` to the previously file-private
components. No behaviour change beyond visibility, with one exception:

- **a11y fix**: `ProfileUpdateBadge` was a role-less `<span aria-label title>`,
  which trips axe `aria-prohibited-attr` at error level. Gave it `role="img"`.
  The badge also sits inside a labelled `<button>` in `AccountEntry`; button
  children are presentational per ARIA, so there is no double announcement.

### Page state-catalogue tier
Data-driven pages are storied only where they have **meaningful states worth
designing** — loading / empty / error / populated — not blanket happy-path
coverage of every route.

- `Audit` — `Populated`, `Empty`, `Loading`, `Failed`.
- `ChangeApprovals` — same shape (see the commit).

## The state-catalogue pattern (how to add more pages)

`web/.storybook/withApp.tsx` is the mock-transport decorator (the Storybook
analogue of the test suite's `inShell`). A page story opts in with
`parameters.app`:

```ts
parameters: {
  app: {
    auth: true,                     // if the page calls useAuth
    path: '/orgs/acme/audit',       // MemoryRouter entry
    routePath: '/orgs/:org/audit',  // so useParams resolves
    responses: [{ url: /\/audit(\?|$)/, body: page }],
  },
}
```

- `responses` is a fail-loud table: any unmatched fetch 404s with the URL in the
  body, so you discover the exact endpoints by running the story and reading the
  `no story route for …` messages. A broad `RegExp` per endpoint is fine.
- **Loading** state: `{ url: /…/, pending: true }` — the row returns a
  never-settling promise, and the retry-free query client holds the screen in
  its loading state. This is the one state the old synchronous mock could not
  reach; the `pending` flag was added for it.
- **Empty**: `body` with an empty list. **Error**: `{ status: 500 }`.
- **Fixtures**: lift URL + body *shape* from the page's `*.test.tsx`, then widen
  for design density (many rows, long names, one item per status). When a test
  mocks by operation rather than raw URL, resolve the URL via the generated
  client: hook → op → `clients/ts/src/generated/sdk.gen.ts` `url:` field, and the
  response zod in `zod.gen.ts` (see `StepUpBanner.stories.tsx`).

### Gotchas that cost time
- Never name a story `export const Error` — it shadows the global `Error`
  constructor inside the module. Use `Failed`.
- A plain `await canvas.findByText(...)` + `toBeVisible()` can hit a
  detached-node race while `AuthProvider` mounts. Wrap in
  `waitFor(() => expect(canvas.getByText(...)).toBeVisible())` — a fresh query
  each retry survives the swap. (`Audit`'s `Loading` story shows this.)
- Split text (`⊘ denied` = an aria-hidden glyph span + text) is not matchable by
  `getByText('⊘ denied')`; assert via the row's accessible name
  (`getByRole('button', { name: /Dana Jacobs/ })`) instead.
- Wire bodies in `responses[].body` are `z.input` shapes: int64 fields as JSON
  numbers, because the harness runs `JSON.stringify` and a bigint throws;
  `parsed()` coerces them back. Type fixtures `satisfies z.input<typeof zX>`
  from `@hikyo/zod`. Shared ids and fixtures live in `web/src/testkit/`.
- POST-create rows need the contract's status (`status: 201`); a default-200
  row on a `[201]`-only operation is treated as a refusal.
- Never name a story after a global (`Error`, `Set`): use `Failed`,
  `FirstCredential`.
- A `Failed` story asserts the status-specific sentence, never a bare
  `role="alert"`: the harness answers a mis-routed request with 404, which many
  screens render as a plausible refusal.
- A click on a control that exists before whoami settles is lost when
  `AuthProvider` swaps the tree; wait for signed-in content first.
- Pages that read the URL in a `useState` initialiser (CLIReauth,
  WorkspaceApprove) get their query string from a per-story `beforeEach` that
  rewrites the frame URL and restores it.
- Pages with an advisory `/events` stream (Matrix) hold that row `pending` so
  the story sits in "connecting" instead of a reconnect loop.
- `getByRole(..., { hidden: true })` is needed to assert a `display:none` pane
  is not visible (HistoryDrawer phone stories).
- Story `move`/callback spies must honour the callee's contract: a bare `fn()`
  returning undefined where the component reads `result.state` throws from
  the navigation guard's popstate path, not from the play.
- Bare `$FILES` in zsh does not word-split for `vitest run`; an unquoted
  expansion silently finds no tests.

## House rules honoured
No `as` casts; `satisfies Meta<typeof C>`; `StoryObj<typeof meta>`;
`tags: ['ai-generated']`; `fn()` callbacks from `storybook/test`; import grouping
(third-party / blank / local with explicit extensions); bigint literals for
int64 fixture fields.

## Local-env traps (same machine as #752)
- **Node**: this shell defaults to Node v20, but the repo pins v26.7.0 (root
  `.nvmrc`) and `cd web` does not auto-switch, so pnpm fails with
  `ERR_UNKNOWN_BUILTIN_MODULE: node:sqlite`. Prefix commands with
  `fnm exec --using=26.7.0 --`.
- `clients/ts` must be installed before a storybook build (`web` links it).
- Run the story tests via `pnpm exec vitest run --project storybook <files>`.

## Deferred (needs a separate pass)
- **graphify graph regen**: `graphify update .` rewrites `graphify-out/graph.json`
  with a ~1.6M-line churn (the graph is stale from an older commit — far beyond
  this change's real footprint of a few added exports). Reverted here to keep the
  PR reviewable; regenerate the graph in its own dedicated commit/PR.
- More page state-catalogues can be added cheaply with the pattern above; only
  the pages with meaningful design states are worth it.
