# Handoff: OpenPencil and Storybook design loop

Spec: docs/superpowers/specs/2026-09-16-openpencil-storybook-design.md
Plan: docs/superpowers/plans/2026-09-16-openpencil-storybook.md
ADR: docs/adr/design-tooling.md
Skill: .claude/skills/design-loop/SKILL.md

## Done

- `web/design/hikyo.pen` seeded from `web/src/styles/tokens.css`. Scripts are
  plain `.ts` under `web/scripts/design/` (`lib.ts`, `lib.test.ts`,
  `tokens-seed.ts`, `tokens-check.ts`, `export.ts`), run by Node 26 directly; npm scripts
  `design:seed`, `design:check`, `design:export`, and both `storybook` and
  `build-storybook` run `design:export` first.
- Drift check is hex against hex, no tolerance. Deliberately not mirrored:
  `--ease`, `--dur`, `--font-ui`, `--font-mono`, and any `color-mix()` token
  (currently the `-soft` set).
- `@storybook/addon-designs` panel fed by a headless CLI export at build time
  into `web/design/exports/`; its contents are gitignored (only `.gitkeep` is
  tracked) and the folder is served at `/design`.
- Stories link by node name path through `design()` in
  `web/.storybook/design.ts`: `Title/Variant`, letters, digits and single
  spaces, at least two segments.
- `export.ts` resolves names with `openpencil find --name` and an exact filter,
  because `query` (XPath) is broken under Node in `@open-pencil/cli` 0.14.0. It
  fails the build on not-found, ambiguous, or result-cap-hit.
- Pencil toolbar button in `web/.storybook/openpencil-addon.tsx`
  (`MIN_APP_VERSION = '0.15.0'`). It posts to the dev middleware and falls back
  to `openpencil://open?file=web%2Fdesign%2Fhikyo.pen&node=...` only on a 405 or
  a reply that is not the middleware's `{ message }` shape.
- Dev-only middleware `web/.storybook/openpencil-middleware.ts` with
  `openpencil-middleware.test.ts`: the RPC token never reaches the browser;
  POST only, same-origin check, Zod-parsed body, 4 KiB cap, 10 s timeout.
  Mounted from the `viteFinal` block in `web/.storybook/main.ts`, and only for
  the real Storybook dev server.
- Button pilot linked: 4 variants in `web/src/ui/Button.stories.tsx`.

## Known limitations

- Exports render the design file's default mode, which is Dark, matching the
  app default. Light is only visible by switching the Mode in the app.
- `Button/Icon` renders as an empty headless frame: no bundled font covers the
  moon glyph used in that variant. The app's real theme toggle is an SVG
  (`.theme-icon__*` in `web/src/styles/app.css`), not a glyph, so the fix is to
  give both the story and the design node that SVG. Issue: #757.
- `openpencil import` is unusable under pnpm in `@open-pencil/cli` 0.14.0 (it
  reads the input with `Bun.file`). Bootstrapping a design means authoring the
  frames in the app or copying and renaming an existing node's JSON; the skill
  says so.
- `web/tsconfig.json` `include` was widened to `.storybook/**/*`; typecheck
  previously skipped that folder entirely.

## Open

- Upstream `openpencil://open?file=&node=` URL scheme
  (docs/superpowers/plans/2026-09-16-openpencil-url-scheme.md) is implemented on
  branch `feat/url-scheme` in
  `~/code/homelab/open-pencil/.claude/worktrees/url-scheme` (15 commits over
  origin/master 42225c6, reviewed clean) but is not pushed, forked, or PR'd:
  that is an owner decision. PR text is at
  `.superpowers/sdd/2026-09-16-openpencil-url-scheme/pr-body.md` (gitignored
  workspace). Once it lands in a tagged OpenPencil release: bump
  `MIN_APP_VERSION` in `web/.storybook/openpencil-addon.tsx`, delete
  `web/.storybook/openpencil-middleware.ts`, its test, and the `viteFinal`
  block in `web/.storybook/main.ts`, and drop `@open-pencil/mcp` from web
  devDependencies. Issue: #756.
- The remaining stories are unlinked by design; link them as they are
  touched.
