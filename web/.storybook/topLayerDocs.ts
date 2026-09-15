import type { Parameters } from '@storybook/react-vite';

// Stories that mount a native `<dialog showModal()>`, popover, or `position:
// fixed` overlay escape the inline autodocs preview: those elements render into
// the browser top layer / viewport, not the story's flow, so on a Docs page they
// cover the whole document with no way to dismiss them. Rendering the story in
// its own iframe (`inline: false`) re-roots the top layer to that frame, so the
// overlay stays boxed and the surrounding docs remain readable.
export const topLayerDocs = {
  docs: { story: { inline: false, height: '720px' } },
} satisfies Parameters;
