---
name: design-loop
description: Design an atom or screen in OpenPencil, implement it with a Storybook story, ship it. Use when asked to design, mock up, or add a component or screen, or to link a story to its design.
---

# Design loop (OpenPencil <-> Storybook)

Source of truth for tokens: `web/src/styles/tokens.css`. Design file:
`web/design/hikyo.pen` (JSON, committed). Both MCPs are installed:
`open-pencil` (app must be running with the file open) and Storybook's
`addon-mcp` (dev server on 6006).

## 1. Design
- `open_file` `web/design/hikyo.pen`. Never `new_document`.
- Build the node with `create_shape` / `set_layout` / `set_text`. Bind every
  fill, stroke, and radius to a variable with `bind_variable`; raw values are
  a review finding.
- Name it `Title/Variant` (`rename_node`) and make it a component
  (`node_to_component`). Screens follow the same rule (`Members/Empty`).
  `design()` rejects anything else: letters, digits and single spaces, at
  least two segments.
- `save_file` to the same path.

## 2. Implement
- `get_codegen_prompt`, then `get_jsx` for the node.
- Write the component in `web/src/ui/` (atom) or `web/src/routes/` (screen)
  using the same custom properties.
- Story: `parameters: { design: design('Title/Variant') }` with
  `import { design } from '../../.storybook/design.ts'`.

## 3. Verify
- `pnpm --dir web run design:export` (runs the token check first).
- Open Storybook, compare the Design panel with the rendered story, flip the
  theme toolbar for light and dark.
- The exported image is the design file's default (Light) mode while the app
  defaults to dark; comparing dark against dark is a pending follow-up.
- The pencil button in the toolbar opens the node in the app.

## 4. Ship
- Normal PR. The `.pen` diff is reviewed like code.

## Token changes
Edit `tokens.css` and `DESIGN.md`, then `pnpm --dir web run design:seed`.
Never edit variables in the app; the check compares hex for hex and fails the build.
Not mirrored into the design file, so nothing can bind to them: `--ease`,
`--dur`, `--font-ui`, `--font-mono`, and the `color-mix` derived `-soft` tokens.

## Bootstrapping from an existing component
Author the frames in the app (`create_shape` and friends over the
`open-pencil` MCP), or copy the JSON of an existing node in `hikyo.pen`,
paste it as a sibling and rename it. `openpencil import` is not usable here:
`@open-pencil/cli` 0.14.0 reads the input with `Bun.file`, which does not
exist under Node, so it throws under pnpm.
