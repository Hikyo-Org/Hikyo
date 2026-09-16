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
5. **The design app's local RPC token never reaches a browser.** Any
   Storybook feature that talks to the running app goes through a Vite dev
   middleware; the static Storybook build has no such feature. This holds
   because local dev servers bind `0.0.0.0` for LAN device testing.
6. **Zero-telemetry stance is unchanged.** The headless export fetches fonts
   from Fontsource during a CI build; the server binary and the SPA still
   phone nowhere.

## Consequences

- `@open-pencil/cli` and `@storybook/addon-designs` become pinned web
  devDependencies.
- The Storybook build gains a design export and token drift step; a story
  that points at a deleted design node fails the build.
- Bootstrapping a design from an existing component is supported through
  `openpencil import`; sweeping all existing stories is a separate decision.
