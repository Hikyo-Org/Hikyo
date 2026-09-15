import type { Meta, StoryObj } from '@storybook/react-vite';
import { useEffect } from 'react';
import { expect, waitFor } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { ToastViewport, clearNotification } from '../app/notifications.tsx';
import { FleetUpdateNotice } from './Shell.tsx';

type Props = Parameters<typeof FleetUpdateNotice>[0];
type Status = NonNullable<Props['local']>;

// FleetUpdateNotice renders nothing; its whole job is to publish an update toast
// into the module-global store, so the story mounts a viewport to observe it and
// clears the store on unmount (as notifications.stories does for the same store).
function ToastHost() {
  useEffect(() => clearNotification, []);
  return <ToastViewport />;
}

// Dismissing a notice records its version key in localStorage; a re-render then
// suppresses the toast. That would make a re-visited story show nothing, so drop
// those keys before each story. Distinct versions per story guard the same thing.
const DISMISSED_PREFIX = 'hikyo:update-dismissed:';
function clearDismissals(): void {
  const storage = globalThis.localStorage;
  if (storage === undefined) {
    return;
  }
  for (const key of Object.keys(storage)) {
    if (key.startsWith(DISMISSED_PREFIX)) {
      storage.removeItem(key);
    }
  }
}

const meta = {
  component: FleetUpdateNotice,
  tags: ['ai-generated'],
  // The toast is position:fixed, so give the docs preview its own frame.
  parameters: topLayerDocs,
  beforeEach: () => {
    clearDismissals();
    return clearDismissals;
  },
  render: (args) => (
    <>
      <FleetUpdateNotice {...args} />
      <ToastHost />
    </>
  ),
} satisfies Meta<typeof FleetUpdateNotice>;

export default meta;
type Story = StoryObj<typeof meta>;

// A local update with a release URL: the action links straight to that release.
export const LocalUpdate: Story = {
  args: {
    principalId: 'user-local',
    remotes: [],
    local: {
      channel: 'stable',
      current_version: '1.2.0',
      latest_version: '1.3.0',
      release_url: 'https://example.com/release/1.3.0',
      available: true,
      prerelease: false,
      apply_supported: false,
    } satisfies Status,
  },
  play: async ({ canvas }) => {
    await waitFor(() =>
      expect(canvas.getByRole('status')).toHaveTextContent(
        'Hikyo 1.3.0 is available on the stable channel.',
      ),
    );
    await expect(canvas.getByRole('link', { name: /view release/i })).toHaveAttribute(
      'href',
      'https://example.com/release/1.3.0',
    );
  },
};

// A single remote update: no release URL to deep-link, so it points at the
// remotes review screen and reads "Review updates".
export const RemoteUpdate: Story = {
  args: {
    principalId: 'user-remote',
    local: null,
    remotes: [
      {
        origin: 'edge.example.com',
        status: {
          channel: 'stable',
          current_version: '1.2.0',
          latest_version: '1.3.1',
          available: true,
          prerelease: false,
          apply_supported: false,
        } satisfies Status,
      },
    ],
  },
  play: async ({ canvas }) => {
    await waitFor(() =>
      expect(canvas.getByRole('status')).toHaveTextContent(
        'Hikyo 1.3.1 is available for edge.example.com.',
      ),
    );
    await expect(canvas.getByRole('link', { name: /review updates/i })).toHaveAttribute(
      'href',
      '/instance/remotes',
    );
  },
};

// Two or more updates collapse into an environment count.
export const MultipleUpdates: Story = {
  args: {
    principalId: 'user-multi',
    local: {
      channel: 'stable',
      current_version: '1.2.0',
      latest_version: '1.4.0',
      available: true,
      prerelease: false,
      apply_supported: false,
    } satisfies Status,
    remotes: [
      {
        origin: 'edge.example.com',
        status: {
          channel: 'stable',
          current_version: '1.2.0',
          latest_version: '1.4.1',
          available: true,
          prerelease: false,
          apply_supported: false,
        } satisfies Status,
      },
    ],
  },
  play: async ({ canvas }) => {
    await waitFor(() =>
      expect(canvas.getByRole('status')).toHaveTextContent(
        '2 Hikyo environments have updates available.',
      ),
    );
    await expect(canvas.getByRole('link', { name: /review updates/i })).toBeVisible();
  },
};

// Nothing available (no `latest_version` anywhere): no toast is published.
export const NoUpdates: Story = {
  args: {
    principalId: 'user-none',
    local: null,
    remotes: [],
  },
  play: async ({ canvas, canvasElement }) => {
    await expect(canvas.getByRole('status')).toBeEmptyDOMElement();
    await expect(canvasElement.querySelector('.toast')).toBeNull();
  },
};
