# Handoff: OpenPencil and Storybook design loop

Spec: docs/superpowers/specs/2026-09-16-openpencil-storybook-design.md
Plan: docs/superpowers/plans/2026-09-16-openpencil-storybook.md
ADR: docs/adr/design-tooling.md
PR: https://github.com/Hikyo-Org/Hikyo/pull/758 (branch feat/openpencil-storybook)
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
  because `query` (XPath) is broken under Node. Still broken in
  `@open-pencil/cli` 0.15.0: `openpencil query design/hikyo.pen "//*" --json`
  answers `XPath error: evaluateXPathToNodes is not a function`. It fails the
  build on not-found, ambiguous, or result-cap-hit.
- Pencil toolbar button in `web/.storybook/openpencil-addon.tsx`. Its tooltip
  says dev-server-only, because the openpencil:// scheme is not in any released
  OpenPencil yet. It posts to the dev middleware and falls back
  to `openpencil://open?file=web%2Fdesign%2Fhikyo.pen&node=...` only on a 405 or
  a reply that is not the middleware's `{ message }` shape.
- Dev-only middleware `web/.storybook/openpencil-middleware.ts` with
  `openpencil-middleware.test.ts`: the RPC token never reaches the browser;
  POST only, same-origin check, Zod-parsed body, 4 KiB cap, 10 s timeout.
  Mounted from the `viteFinal` block in `web/.storybook/main.ts`, and only for
  the real Storybook dev server. Checked live against the running desktop app:
  the success path returned 200 `Opened Button/Primary`, and an unknown node
  returned 404 with the app's real message.
- Button pilot linked: 4 variants in `web/src/ui/Button.stories.tsx`.

## Known limitations

- `.pen` is read-only in OpenPencil 0.15.0 as it was in 0.14.0: `openpencil formats` reports
  `pen: support: read`, and the app's `save_file` writes a `.fig` zip container
  even when given a `.pen` path. Owner decision: the JSON stays the source of
  truth and the app is a viewer. Design changes are made by editing
  `web/design/hikyo.pen` directly, or by prototyping with the MCP write tools
  and transcribing the node back via `get_jsx` / `node_tree`. Never `save_file`
  onto the repo file. A `.pen` writer contributed upstream would restore the
  round trip.
- The app's Tauri fs scope uses `**`, which skips dot-directories. A repo
  checked out under a hidden directory (T3 worktrees live under `~/.t3/`)
  cannot be opened by the middleware until the user has opened the file once
  through the app's own dialog in that session. Normal checkouts under `~/code`
  are unaffected.
- The `.pen` format in OpenPencil 0.14.0 has no page node and no pages array,
  so a `.pen` document is always exactly one page.
  `packages/pen/src/read.ts:497` creates one page named after
  `children[0].name` and parents every root child to it; a `type: "page"` or
  `type: "canvas"` wrapper falls through `mapNodeType` and is read as a frame;
  a top-level `pages` array throws in
  `collectComponentIds` ("nodes is not iterable"). Owner decision: keep one
  file, `web/design/hikyo.pen`, and organise by node names `Title/Variant`;
  the implicit page is named after the first frame (`Button/Secondary`) and
  that is accepted. Page support, together with the missing `.pen` writer, is a
  future upstream contribution to `packages/pen`, after open-pencil PR #708
  lands. Trap for a future attempt: `openpencil pages` prints a wrapper frame's
  name as the page name, so a wrapper looks like it worked. Verify with
  `openpencil tree`, not `pages`.
- Storybook dev with `-h 0.0.0.0` makes every `staticDirs` path 404, `/design`
  included, so the Design panel loses its images. Verified against a control
  run without the flag; the `storybook` script does not pass a host flag. LAN
  device testing therefore uses localhost plus a TCP proxy. Worth an upstream
  Storybook issue (10.6.0).
- Exports render the design file's default mode, which is Dark, matching the
  app default. Light is only visible by switching the Mode in the app.
- `Button/Icon` renders as an empty headless frame: no bundled font covers the
  moon glyph used in that variant. The app's real theme toggle is an SVG
  (`.theme-icon__*` in `web/src/styles/app.css`), not a glyph, so the fix is to
  give both the story and the design node that SVG. Issue: #757.
- `openpencil import` ran into `Bun is not defined` under pnpm in
  `@open-pencil/cli` 0.14.0; 0.15.0 fixed that ("Run `openpencil import` on Node
  so npm-installed CLI users no longer encounter `Bun is not defined`", #575)
  and it now converts an HTML file here. It is still not a bootstrapping route
  for `hikyo.pen`: `-f` takes `fig` or `json`, where `json` is a DOM/CSS dump,
  not a `.pen` document. Bootstrapping a design still means authoring the frames
  in the app or copying and renaming an existing node's JSON; the skill says so.
- `web/tsconfig.json` `include` was widened to `.storybook/**/*`; typecheck
  previously skipped that folder entirely.

## Sidecar version match

The Storybook middleware talks to the MCP sidecar the desktop app starts, so the
sidecar has to match the app: `npm i -g @open-pencil/mcp@<app version>`. On a
mismatch the sidecar dies, the discovery file it wrote stays behind, and the
middleware reports the pencil button as "OpenPencil is not running". The web
devDependency pin (`@open-pencil/mcp` 0.15.0) only supplies the `./discovery`
reader in Node; it does not start or replace the global sidecar.

## Open

- Upstream `openpencil://open?file=&node=` URL scheme
  (docs/superpowers/plans/2026-09-16-openpencil-url-scheme.md) is up as
  https://github.com/open-pencil/open-pencil/pull/708, branch `feat/url-scheme`
  on `Dunky13/open-pencil`. It now also gains a browser open route,
  `?file=<https url>&node=` for app.openpencil.dev (in progress). Once that
  ships, the Storybook button's production fallback becomes a plain https link
  to the raw `hikyo.pen`, alongside the `openpencil://` scheme for the desktop
  app. Removal checklist, in order: switch the addon fallback in
  `web/.storybook/openpencil-addon.tsx` to the web link and restore the
  minimum-version tooltip with the real version, then delete
  `web/.storybook/openpencil-middleware.ts`, its test, and the `viteFinal`
  block in `web/.storybook/main.ts`, and drop `@open-pencil/mcp` from web
  devDependencies. Issue: #756.
- The remaining stories are unlinked by design; link them as they are
  touched.
