import type { Parameters } from '@storybook/react-vite';

// Stories that must NOT render inline on a Docs page, and so get their own
// iframe there (`inline: false`), for one of two reasons:
//
// - They mount a native `<dialog showModal()>`, popover, or `position: fixed`
//   overlay. Those render into the browser top layer / viewport, not the story's
//   flow, so inline they cover the whole document with no way to dismiss them.
//   A frame re-roots the top layer, so the overlay stays boxed.
// - They run a real screen against the app harness (`parameters.app`). The
//   harness answers the screen's requests by replacing `globalThis.fetch` for
//   the story's lifetime; inline, every example on the page would take turns
//   replacing the SAME fetch and read each other's fixtures. A frame gives each
//   example its own window and its own fetch. `installAppFetch` refuses to run
//   inline on Docs, so forgetting this spread fails loud rather than silently
//   cross-wiring the examples.
export const topLayerDocs = {
  docs: { story: { inline: false, height: '720px' } },
} satisfies Parameters;
