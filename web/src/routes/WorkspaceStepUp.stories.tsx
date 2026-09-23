import type { Meta, StoryObj } from '@storybook/react-vite';
import { zMeta, zWorkspaceHandoffStarted } from '@hikyo/zod';
import { useRef, type ComponentProps } from 'react';
import { expect, fn, waitFor } from 'storybook/test';
import type { z } from 'zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { forgetWorkspace, rememberWorkspace } from '../api/workspace.ts';
import { WorkspaceStepUp } from './Ceremony.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The ceremony modal's executor for a remote's disclosure. On mount it opens
// the elevation transaction on the REMOTE origin (GET <origin>/api/v1/meta,
// then POST <origin>/api/v1/auth/workspace/start), so the button that opens
// the popup is synchronous to the click. The click itself is never fired
// here: it would open a real window on the remote. The workspace bearer lives
// in an in-memory store, installed per story and forgotten afterwards.
const ORIGIN = 'https://hikyo.example.com';

const withBearer = () => {
  rememberWorkspace({
    origin: ORIGIN,
    value: 'wsb_story_bearer',
    session: 'ses_01989abc-def0-7123-8123-000000000010',
    idleExpiresAt: '2099-08-22T10:30:00Z',
    absoluteExpiresAt: '2099-08-22T18:00:00Z',
  });
  return () => {
    forgetWorkspace(ORIGIN);
  };
};

const meta_ = (rest: Partial<MockRoute>): MockRoute => ({ url: /\/api\/v1\/meta$/, ...rest });
const metaBody = {
  server_version: '1.4.0',
  api_revision: 1,
  protocol_capabilities: [],
} satisfies z.infer<typeof zMeta>;
const started: MockRoute = {
  url: /\/api\/v1\/auth\/workspace\/start$/,
  method: 'POST',
  body: {
    handoff: 'wsh_01989abc-def0-7123-8123-000000000001',
    state: 'state-195',
    expires_at: '2099-08-23T12:00:00Z',
  } satisfies z.infer<typeof zWorkspaceHandoffStarted>,
};

/**
 * The modal owns the ref its primary button takes for initial focus. A ref
 * is not a serialisable arg (its `.current` becomes the DOM node, a cycle for
 * the args channel), so the story creates it here, as the modal would.
 */
function StepUp(props: Omit<ComponentProps<typeof WorkspaceStepUp>, 'firstRef'>) {
  const first = useRef<HTMLButtonElement>(null);
  return <WorkspaceStepUp {...props} firstRef={first} />;
}

const meta = {
  component: StepUp,
  tags: ['ai-generated'],
  args: {
    origin: ORIGIN,
    operation: 'reveal',
    environmentId: 'env_01989abc-def0-7123-8123-000000000001',
    keyIds: ['key_01989abc-def0-7123-8123-000000000001'],
    onAuthorised: fn(),
    onCancel: fn(),
  },
  parameters: { ...topLayerDocs, app: { responses: [] } },
} satisfies Meta<typeof StepUp>;

export default meta;
type Story = StoryObj<typeof meta>;

// No bearer for the origin: the workspace was disconnected under the modal.
export const Disconnected: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/this workspace is no longer connected/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Try again' })).toBeEnabled();
  },
};

// The remote has not answered the meta read yet.
export const Contacting: Story = {
  beforeEach: withBearer,
  parameters: { app: { responses: [meta_({ pending: true })] } },
  play: async ({ canvas }) => {
    await waitFor(() => expect(canvas.getByRole('button', { name: 'Contacting…' })).toBeDisabled());
  },
};

// The transaction is open on the remote: one click away from its popup.
export const Ready: Story = {
  beforeEach: withBearer,
  parameters: { app: { responses: [meta_({ body: metaBody }), started] } },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('button', { name: `Continue to ${ORIGIN} to authorise` }),
    ).toBeEnabled();
  },
};

// The remote answered an error: the message names it, with a retry.
export const Failed: Story = {
  beforeEach: withBearer,
  parameters: { app: { responses: [meta_({ status: 500, body: { error: 'remote down' } })] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(`${ORIGIN} answered 500.`)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Try again' })).toBeEnabled();
  },
};
