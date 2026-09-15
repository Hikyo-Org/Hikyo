import type { Meta, StoryObj } from '@storybook/react-vite';
import { useEffect } from 'react';
import { expect, userEvent, waitFor } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import {
  ToastViewport,
  clearNotification,
  notifyFailure,
  notifySuccess,
  notifyUpdate,
} from './notifications.tsx';

// The toast store is module state holding one live notification, so a story
// publishes on mount and clears on unmount — otherwise its tone would leak into
// the next story exactly as a stray notification would leak between app pages.
function Emit({ kind }: { kind: 'error' | 'success' | 'info' }) {
  useEffect(() => {
    if (kind === 'error') {
      notifyFailure('Could not save changes. Check your connection and try again.');
    } else if (kind === 'success') {
      notifySuccess('Changes saved.');
    } else {
      notifyUpdate('A new release is available.', 'https://example.com/release', () => {});
    }
    return clearNotification;
  }, [kind]);
  return <ToastViewport />;
}

const meta = {
  component: ToastViewport,
  tags: ['ai-generated'],
  parameters: topLayerDocs,
} satisfies Meta<typeof ToastViewport>;

export default meta;
type Story = StoryObj<typeof meta>;

// Not `Error` — that shadows the global constructor (a bug an earlier pass hit).
export const Failure: Story = { render: () => <Emit kind="error" /> };
export const Success: Story = { render: () => <Emit kind="success" /> };
export const Info: Story = { render: () => <Emit kind="info" /> };

export const Dismisses: Story = {
  render: () => <Emit kind="success" />,
  play: async ({ canvas, canvasElement }) => {
    // The message also renders in a visually-hidden live region, so assert on
    // the visible `.toast` node itself rather than the ambiguous text.
    await expect(canvasElement.querySelector('.toast')).not.toBeNull();
    await userEvent.click(canvas.getByRole('button', { name: /dismiss notification/i }));
    await waitFor(() => expect(canvasElement.querySelector('.toast')).toBeNull());
  },
};
