# Design tooling

Decision date: 2026-09-16. Owner chose option B of the
[OpenPencil ↔ Storybook design spec](../superpowers/specs/2026-09-16-openpencil-storybook-design.md).
Operative once that spec's implementation merges.

## Decision

1. **OpenPencil is the design tool.** MIT licensed, desktop, `.pen` JSON
   files, MCP server and headless CLI. Chosen over Figma (proprietary,
   cloud-only, agent access paid) and Penpot (needs a hosted server, weaker
   agent authoring). Rationale in the spec.
2. **One design file in the repo:** `web/design/hikyo.pen`. It ships in the
   same PR as the code it describes and is reviewed as a diff.
3. **Tokens are owned by code.** `web/src/styles/tokens.css` and `DESIGN.md`
   are the source of truth; the design file mirrors them, name for name, and
   CI fails on drift. A design change to a token is applied to the CSS first.
4. **Stories link to designs by node name path** in `parameters.design`.
   Storybook renders the design next to the story from a CI-time headless
   export; nothing rendered is committed.
5. **The design app's local RPC token never reaches a browser.** In dev,
   Storybook talks to the running app through a Vite middleware (interim).
   The desktop app is reached by an `openpencil://open` link once that scheme
   ships; the published Storybook's production fallback becomes the web app
   link, `https://app.openpencil.dev/?file=<https url>&node=<name>` against the
   raw `hikyo.pen`, once upstream PR #708's browser route ships. Neither link
   carries any authority beyond open + select. The scheme is contributed
   upstream to open-pencil; the middleware is removed once a tagged release
   ships it. This holds because local dev servers bind `0.0.0.0` for LAN
   device testing.
6. **Zero-telemetry stance is unchanged.** The headless export fetches fonts
   from Fontsource during a CI build; the server binary and the SPA still
   phone nowhere.

## Consequences

- `@open-pencil/cli` and `@storybook/addon-designs` become pinned web
  devDependencies.
- The Storybook build gains a design export and token drift step; a story
  that points at a deleted design node fails the build.
- Bootstrapping a design from an existing component is done by authoring the
  frames in the app or by copying and renaming an existing node's JSON;
  `openpencil import` is not usable under pnpm in `@open-pencil/cli` 0.14.0.
  Sweeping all existing stories is a separate decision.
- `.pen` is read-only in OpenPencil 0.14.0 (`openpencil formats` reports
  `pen: support: read`; `save_file` writes a `.fig` container even for a `.pen`
  path). Decision: the JSON file stays the source of truth and the app is a
  viewer and inspection surface until a `.pen` writer ships upstream. Design
  edits are made in the JSON, or prototyped in the app and transcribed back.
- `.pen` 0.14.0 also has no pages: the reader creates one implicit page named
  after the first frame, so a wrapper node is read as a frame and a `pages`
  array crashes it. Decision: a single file, `web/design/hikyo.pen`, organised
  by node names `Title/Variant`. Real pages, plus the `.pen` writer they need
  to round-trip, are an upstream follow-up in `packages/pen` once open-pencil
  PR #708 lands.
