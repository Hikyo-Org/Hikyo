import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { ORG } from '../testkit/ids.ts';
import { EmailInviteDialog } from './EmailInviteDialog.tsx';

const SEND_URL = `/api/v1/orgs/${ORG}/email-invitations`;
const refusal = (status: number, code: string, detail?: string) => ({
  url: SEND_URL,
  method: 'POST',
  status,
  body: { error: { code, message: 'Refused', ...(detail === undefined ? {} : { detail }) } },
});

// Each state submits the real dialog against a stubbed send, so the copy
// shown is exactly what an administrator reads for that answer.
const meta = {
  title: 'Features/Members/EmailInviteDialog',
  id: 'routes-emailinvitedialog',
  component: EmailInviteDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: { responses: [] } },
  args: { org: ORG, scopeName: 'Acme', onDone: fn(), onCancel: fn() },
} satisfies Meta<typeof EmailInviteDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

type PlayContext = Parameters<NonNullable<Story['play']>>[0];

async function submit({ canvas, userEvent }: PlayContext) {
  await userEvent.type(await canvas.findByLabelText('Email address'), 'dana@example.com');
  await userEvent.selectOptions(canvas.getByLabelText('Role template'), 'editor');
  await userEvent.click(canvas.getByRole('button', { name: 'Invite by email' }));
}

export const Entry: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/verified email can be reached/)).toBeVisible();
    await expect(canvas.getByLabelText('Role template')).toHaveValue('');
  },
};

export const Recorded: Story = {
  parameters: { app: { responses: [{
    url: SEND_URL, method: 'POST', status: 202,
    body: { id: 'eiv_123e4567-e89b-12d3-a456-426614174090', email: 'dana@example.com', template: 'editor', created_at: '2026-11-06T10:00:00Z', expires_at: '2026-11-13T10:00:00Z' },
  }] } },
  play: async (context) => {
    await submit(context);
    await expect(await context.canvas.findByText(/is recorded/)).toBeVisible();
    await expect(context.canvas.queryByText(/dana@example.com/)).toBeNull();
  },
};

export const MailerUnconfigured: Story = {
  parameters: { app: { responses: [refusal(400, 'bad_request', 'mailer-unconfigured')] } },
  play: async (context) => {
    await submit(context);
    await expect(await context.canvas.findByRole('alert')).toHaveTextContent('need a configured mail server');
  },
};

export const Throttled: Story = {
  parameters: { app: { responses: [refusal(429, 'too_many_requests')] } },
  play: async (context) => {
    await submit(context);
    await expect(await context.canvas.findByRole('alert')).toHaveTextContent('Too many requests right now');
  },
};
