import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { AppRoutes } from '../app/App.tsx';
import { InvitationAccept } from './InvitationAccept.tsx';

const PREVIEW_URL = '/api/v1/me/invitations/preview';
const ACCEPT_URL = '/api/v1/me/invitations/accept';
const path = '/invitations/accept#token=storybook-inert-token';
const preview = {
  org: 'org_123e4567-e89b-12d3-a456-426614174001',
  org_name: 'Acme',
  template: 'editor',
  expires_at: '2026-11-13T10:00:00Z',
};
const uniform404 = { status: 404, body: { error: { code: 'not_found', message: 'Not found' } } };

// Real acceptance route with an inert local fragment. The signed-in states use
// the real AuthProvider answering whoami; the signed-out state does not.
const meta = {
  title: 'Pages/InvitationAccept',
  id: 'routes-invitationaccept',
  component: InvitationAccept,
  tags: ['ai-generated'],
  // The real route tree, so the story settles authentication exactly as the app
  // does before the page mounts and reads its fragment.
  render: () => <AppRoutes />,
  beforeEach: async () => {
    await import('../app/route-groups/auth.ts');
  },
  parameters: {
    ...topLayerDocs,
    docs: { ...topLayerDocs.docs, description: { component: 'Accepts an email invitation while signed in. The token travels in the URL fragment and is removed from the address once read.' } },
    app: {
      auth: true,
      identity: authenticatedIdentity,
      path,
      routeTree: true,
      responses: [{ url: PREVIEW_URL, method: 'POST', body: preview }],
    },
  },
} satisfies Meta<typeof InvitationAccept>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Preview: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('Acme')).toBeVisible();
    await expect(canvas.getByText(/you will be signed out on every device/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Accept' })).toBeEnabled();
    await expect(canvas.getByRole('button', { name: 'Not now' })).toBeEnabled();
    // Primary first (DESIGN.md, Action rows).
    await expect(canvas.getAllByRole('button').map((b) => b.textContent)).toEqual(['Accept', 'Not now']);
  },
};

export const NotSignedIn: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/auth/whoami', status: 401, body: { error: { code: 'unauthenticated', message: 'Unauthenticated' } } }] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/open the link from your email again/)).toBeVisible();
    await expect(canvas.getByRole('link', { name: 'Go to sign in' })).toBeVisible();
  },
};

export const NotValid: Story = {
  parameters: { app: { responses: [{ url: PREVIEW_URL, method: 'POST', ...uniform404 }] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText('This invitation is not valid for this account or has expired.')).toBeVisible();
  },
};

export const MissingLink: Story = {
  parameters: { app: { path: '/invitations/accept' } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByText(/needs the link from your invitation email/)).toBeVisible();
  },
};

export const Accepted: Story = {
  parameters: { app: { responses: [
    { url: PREVIEW_URL, method: 'POST', body: preview },
    { url: ACCEPT_URL, method: 'POST', body: { org: preview.org, org_name: 'Acme', template: 'editor', grants_created: 3, signed_out: true } },
  ] } },
  play: async ({ canvas, userEvent }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Accept' }));
    await expect(await canvas.findByText(/You joined Acme as editor/)).toBeVisible();
    await expect(canvas.getByRole('link', { name: 'Sign in' })).toBeVisible();
  },
};
