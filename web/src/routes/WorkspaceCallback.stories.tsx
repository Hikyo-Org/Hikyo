import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { WorkspaceCallback } from './WorkspaceCallback.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The viewing instance's return leg: read `code` and `state` off the real
// location, broadcast them, close. A story frame is not a window this script
// opened, so the browser refuses `close()` and the page says so, which is the
// visible state. The broadcast goes to a channel nobody listens on. The URL
// is rewritten per story, so Docs renders these framed rather than inline.
const withResult = () => {
  const original = globalThis.location.href;
  const next = new URL(original);
  next.searchParams.set('code', 'code-195');
  next.searchParams.set('state', 'state-195');
  globalThis.history.replaceState(null, '', next);
  return () => {
    globalThis.history.replaceState(null, '', original);
  };
};

const meta = {
  component: WorkspaceCallback,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
} satisfies Meta<typeof WorkspaceCallback>;

export default meta;
type Story = StoryObj<typeof meta>;

// Opened without a handoff result: nothing to hand back.
export const NoResult: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('alert')).toHaveTextContent(/without a handoff result/i);
    await expect(canvas.getByRole('button', { name: /close this window/i })).toBeEnabled();
  },
};

// The result was handed back but the browser refused to close the window.
export const CouldNotClose: Story = {
  beforeEach: withResult,
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('status')).toHaveTextContent(/could not close itself/i);
    await expect(canvas.getByRole('button', { name: /close this window/i })).toBeEnabled();
  },
};
