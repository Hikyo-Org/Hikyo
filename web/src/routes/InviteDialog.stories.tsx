import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import type { InviteScope } from '../api/access.ts';
import { InviteDialog } from './InviteDialog.tsx';

// The invite submits through a real API call the story cannot mock without a
// harness, so plays only exercise the form's rendered state, never Invite.
const meta = {
  component: InviteDialog,
  tags: ['ai-generated'],
  args: {
    scope: { kind: 'org', org: 'org_01989abc-def0-7123-8123-000000000000' } satisfies InviteScope,
    scopeName: 'Acme',
    origin: 'https://hikyo.example',
    onDone: fn(),
    onCancel: fn(),
  },
} satisfies Meta<typeof InviteDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

export const OrgScope: Story = {
  play: async ({ canvas, userEvent }) => {
    await expect(canvas.getByRole('heading', { name: /invite a member to acme/i })).toBeVisible();
    const username = canvas.getByLabelText('Username');
    await userEvent.type(username, 'new-operator');
    await expect(username).toHaveValue('new-operator');
    await expect(canvas.getByRole('button', { name: /^invite$/i })).toBeEnabled();
  },
};

// The instance scope offers a different, narrower set of role templates
// (templatesAt('instance') vs. templatesAt('org')).
export const InstanceScope: Story = {
  args: {
    scope: { kind: 'instance' } satisfies InviteScope,
    scopeName: 'this instance',
  },
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('heading', { name: /invite a member to this instance/i }),
    ).toBeVisible();
  },
};
