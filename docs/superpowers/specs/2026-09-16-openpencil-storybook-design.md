# OpenPencil ↔ Storybook design loop — design

Date: 2026-09-16. Status: approved by Marc (option B, then option b for the
production open action). Next: implementation plan.

## Problem

The SPA has 44 stories (`web/src/ui/*.stories.tsx` for atoms,
`web/src/routes/*.stories.tsx` for screens) but no design source. Visual work
happens directly in code, and there is no place to sketch an atom or a screen
before committing to an implementation. The intended loop is:

1. Design an atom or screen in OpenPencil.
2. Implement it as a component with a story in Storybook.
3. Ship it in the app.

Each step must link to the previous one so a reader in Storybook can open the
design it was built from, and so the design file and the code cannot drift on
tokens without CI noticing.

## Decisions already taken

- **Tool: OpenPencil** (MIT, desktop, MCP + headless CLI, `.pen` JSON on
  disk). Chosen over Figma (proprietary, cloud-only, MCP paid) and Penpot
  (self-hosted server, weaker agent authoring). Recorded in the
  design-tooling ADR.
- **Tokens stay in code.** `web/src/styles/tokens.css` and `DESIGN.md`
  remain the source of truth; the Playwright pinned assertion set already
  enforces them against computed styles. The design file mirrors them and is
  checked for drift; it never owns them.
- **Design file lives in the repo** at `web/design/hikyo.pen`. The MCP's
  `save_file`/`open_file` are sandboxed to the worktree, the format is JSON
  and diffs in review, and the loop needs code and design in one PR.

## Verified facts about OpenPencil 0.14.0

Checked against the source checkout at `~/code/homelab/open-pencil` and the
published `@open-pencil/cli@0.14.0` / `@open-pencil/mcp@0.14.0`.

- No custom URL scheme. `desktop/tauri.conf.json` registers only the updater
  plugin; `desktop/Info.plist` has no `CFBundleURLTypes`. A plain
  `openpencil://` link from Storybook is not possible.
- The running app serves a local RPC: Unix socket on macOS/Linux plus
  `http://127.0.0.1:<port>/rpc`, `POST {command, args}`, bearer token. The
  discovery file (`~/Library/Application Support/OpenPencil/mcp.json` on
  macOS, `$XDG_RUNTIME_DIR/openpencil/` or `~/.openpencil/` on Linux) holds
  `socketPath`, `httpPort`, `authToken`. Its own header warns: any process as
  the same user can read the plaintext token. RPC commands: `open_file`,
  `list_documents`, `new_document`, `save_file`, and `tool` (any MCP tool,
  including `select_nodes` and `viewport_zoom_to_fit`).
- Headless export works without the app. Verified locally: `.pen` → PNG and
  SVG, `--node <id>` targets one node, `--scale` for 2x. Fonts resolve
  through Fontsource/Google at export time, so Instrument Sans and IBM Plex
  Mono render in CI with network access.
- `.pen` variables are named like CSS custom properties (`--primary`) with
  per-mode values (`theme: { Mode: 'Dark' }`). Colour strings parse through
  culori, so `oklch(...)` is accepted on input, but values are stored as sRGB
  floats. Round-trip is lossy: a drift check must compare with tolerance.
- `openpencil import page.html --css tokens.css -o out.pen` converts rendered
  HTML/CSS into editable layers. This is the cheap way to bootstrap a design
  from an existing story.
- Node IDs are short random strings (`T3Um0`). They do not survive deleting
  and recreating a node. Stories must reference nodes by **name path**, not
  ID.

## Design

### 1. Link key: `parameters.design` on a story

```ts
export const Primary: Story = {
  args: { variant: 'primary' },
  parameters: { design: { node: 'Button/Primary' } },
};
```

- `node` is the OpenPencil node name path inside `web/design/hikyo.pen`. The
  file path is fixed, not a per-story parameter; one design file per app.
- A Zod schema in `web/.storybook/design.ts` parses the parameter; a story
  with a malformed `design` fails the Storybook build, not silently.
- Convention: OpenPencil top-level component name == story title; variant
  names == story export names. `design_to_component_map` already splits
  components from screens, mirroring `ui/` vs `routes/`.

### 2. Design tab in Storybook (`@storybook/addon-designs`)

- Add `@storybook/addon-designs@11.1.4` (peer range includes Storybook
  10.6.0, verified on npm). Registered in `web/.storybook/main.ts`.
- The addon reads `parameters.design`; a preview-side loader in
  `preview.tsx` rewrites `{ node }` into the addon's
  `{ type: 'image', url: '/design/<slug>.png' }` where `<slug>` is the node
  path with `/` → `--`. The story author never writes the image path.
- `web/design/exports/` is gitignored and served through
  `staticDirs: [{ from: '../design/exports', to: '/design' }]`.
- Script `web/scripts/design-export.mjs`:
  1. Collects every story file's `parameters.design.node` (regex over
     `web/src/**/*.stories.tsx` is enough; a story index is not needed).
  2. Resolves each name path to a node ID with
     `openpencil query web/design/hikyo.pen "//*[@name='…']"`.
  3. Runs `openpencil export web/design/hikyo.pen --node <id> -s 2 -o
     web/design/exports/<slug>.png`.
  4. Fails loud on an unresolved node (exit 1, name printed). A story that
     points at a deleted design breaks the build.
- `pnpm run design:export` is wired into `build-storybook` and the
  `storybook` dev script (`design:export && storybook dev`). CI
  (`ci.yml` storybook job, `docs.yml` Pages build) picks it up through those
  scripts; no new workflow step.
- `@open-pencil/cli@0.14.0` pinned in `web/package.json` devDependencies.

### 3. "Open in OpenPencil" (dev and production)

Goal: one click in Storybook opens the right design node in the running
app, both from the local dev server and from the published
hikyo.app/storybook. Decision 2026-09-16 (option b): a URL scheme is the
production mechanism and is contributed upstream; the dev middleware is the
interim mechanism and is deleted once a released OpenPencil carries the
scheme.

- **Browser side.** A manager-side addon (`web/.storybook/openpencil-addon.ts`,
  registered via `managerEntries` in every build) adds a toolbar button.
  On click it reads the current story's `parameters.design.node` and:
  1. In a static build: navigates to
     `openpencil://open?file=web/design/hikyo.pen&node=<name path>`. If the
     scheme is not registered the browser does nothing; the button's tooltip
     says "Needs OpenPencil ≥ <version> installed".
  2. In the dev server (`configType === 'DEVELOPMENT'`, passed to the addon
     as a manager global): `fetch('/__openpencil/open', { method: 'POST',
     body: { node } })`, falling back to the scheme link on 404 (middleware
     removed) so the switch-over needs no addon change.
- **URL scheme (upstream contribution to open-pencil).** Separate PR in
  `~/code/homelab/open-pencil`, tracked as its own task:
  - `tauri-plugin-deep-link` registers `openpencil`. `RunEvent::Opened` and
    the single-instance handler already funnel file URLs into
    `queue_open_paths`; the scheme handler parses `open?file=…&node=…` and
    reuses it.
  - `file` is repo-relative. The app resolves it against open documents and
    recent files by path suffix; on no match it shows the file picker once
    and remembers the chosen root per suffix. Absolute paths are refused.
  - After opening, the app selects the node by name path and zooms to fit.
    Unknown node: document opens, status line says "node not found".
  - No other command is exposed through the scheme. A link can open a file
    the user has already opened or explicitly picks; it cannot read, write,
    export, or run anything.
- **Server side (interim).** A Vite middleware added through `viteFinal` in
  `main.ts`, only when `configType === 'DEVELOPMENT'`, handles
  `/__openpencil/open`:
  1. Reads the discovery file with `readDiscoveryFile()` from
     `@open-pencil/mcp/discovery`.
  2. Sends `open_file { path: web/design/hikyo.pen }` (idempotent, the app
     focuses the tab if already open).
  3. Resolves the node ID with the `find_nodes` tool and sends
     `select_nodes` then `viewport_zoom_to_fit`.
  4. Returns 503 with a plain message when the app is not running.
  Removal condition: the upstream scheme is in a tagged OpenPencil release
  and the skill's minimum version is bumped to it. Tracked as a follow-up
  issue opened in the implementation PR.
- **Security invariant.** The bearer token never leaves the Node process.
  Dev servers on this machine bind `0.0.0.0` (see WORKSTYLE), so anything
  in the browser bundle is readable by the LAN. The middleware is the only
  reader of the discovery file; `OPENPENCIL_MCP_CORS_ORIGIN` is not set and
  the browser never talks to port 7600. The middleware accepts only POST,
  only from `Origin` matching the Storybook dev server's own origin, and
  only the `node` field (Zod). It is not mounted in `storybook build`. The
  URL scheme carries no token because it carries no authority: it can only
  open and select.

### 4. Token bridge

- `web/design/hikyo.pen` carries one variable per custom property in
  `tokens.css`, same name, two modes `Light` and `Dark` matching
  `[data-theme]`. Values are seeded once from the CSS via the MCP
  (`create_variable`) in an agent session with the app running.
- `web/scripts/design-tokens-check.mjs` runs `openpencil variables
  web/design/hikyo.pen --json`, converts both sides to OKLCH with culori (a
  transitive dep of the CLI; add it explicitly), and fails when any channel
  differs by more than a fixed tolerance (L 0.005, C 0.005, H 1°) or a
  token is missing on either side. Non-colour tokens (radius, sizes) must
  match exactly.
- Wired into `pnpm run design:export` so the Storybook build fails on drift.
  Direction of fix is always CSS → design; the script prints the CSS value
  as the expected one.

### 5. Project skill: `.claude/skills/design-loop/SKILL.md`

Documents the loop for an agent session, all through already-installed
MCPs (`open-pencil`, `@storybook/addon-mcp`):

1. Design: create or edit a node in `hikyo.pen`, bind fills and radii to
   the token variables (never raw values). Name it `Title/Variant`.
2. Implement: `get_codegen_prompt` then `get_jsx` for the node, write the
   component using existing tokens, write the story with
   `parameters.design.node`.
3. Verify: `pnpm run design:export`, open Storybook, compare the Design tab
   image with the rendered story; a11y and story tests already run.
4. Ship: normal PR. The `.pen` diff is part of the review.

Bootstrapping an existing component the other way: render the story's
HTML, `openpencil import story.html --css tokens.css`, then tidy in the app.

### 6. Pilot: Button

- Bootstrap `Button` variants from `Button.stories.tsx` via `import`, tidy
  in the app, bind to variables.
- Add `parameters.design` to the four Button stories.
- Run the full loop once end to end, including "Open in OpenPencil" and a
  deliberate token drift to prove the check fails.
- No sweep over the other 43 stories in this PR.

## Error handling

- Missing `.pen` file, unresolved node, malformed `design` parameter, token
  drift: build fails with the offending story or token named.
- App not running: dev shows the 503 message; a static build click is a
  browser no-op. Nothing else in Storybook depends on the app.
- Fontsource unreachable in CI: export fails loud; do not fall back to a
  system font, a wrong-font design image is worse than no build.

## Testing

- `design-export.mjs` and `design-tokens-check.mjs`: one Vitest each in
  `web/scripts/*.test.ts` using a tiny fixture `.pen` (copied from the
  OpenPencil repo's `tests/fixtures/pencil_button.pen`): resolution of a
  name path, failure on a missing node, drift detection on one colour and
  one number.
- Middleware: one Vitest with a fake discovery file and a stub RPC server,
  asserting the token is sent upstream and never in the response, and that a
  non-matching `Origin` is refused.
- Storybook story test for Button unchanged; a11y still `error`.

## Out of scope

- Reverse drift check (design vs rendered story pixels).
- Sweeping existing stories into the design file.
- Live embed of the design (no URL to embed).
- Any scheme command beyond open + select.
- Multi-file designs or per-story design files.

## Open questions for the review

None blocking. The token tolerance values are a first guess and are the
one knob expected to change after the pilot.
