import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import type { WhoAmI } from '../api/session.ts';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { StepUpBanner } from './StepUpBanner.tsx';

// A browser session minted at password assurance: the login floor enrols no
// second factor, so the banner's whole reason to exist is this shape.
const passwordOnly: WhoAmI = {
  ...authenticatedIdentity,
  session: {
    ...authenticatedIdentity.session,
    assurance: { ...authenticatedIdentity.session.assurance, factors: ['password'] },
  },
};

const totpConfirmed = { url: '/api/v1/auth/totp', body: { confirmed: true, pending: false } };
const noTotp = { url: '/api/v1/auth/totp', body: { confirmed: false, pending: false } };
const noPasskeys = { url: '/api/v1/auth/webauthn/credentials', body: { passkeys: [] } };
const onePasskey = {
  url: '/api/v1/auth/webauthn/credentials',
  body: {
    passkeys: [
      {
        id: 'pk_01989abc-def0-7123-8123-000000000001',
        label: 'YubiKey 5C',
        discoverable: true,
        disabled: false,
        created_at: '2026-08-22T10:00:00Z',
        last_used_at: '2026-09-01T09:00:00Z',
      },
    ],
  },
};

const meta = {
  component: StepUpBanner,
  tags: ['ai-generated'],
  args: { session: passwordOnly },
} satisfies Meta<typeof StepUpBanner>;

export default meta;
type Story = StoryObj<typeof meta>;

// A confirmed authenticator with no passkey: the banner offers the code form.
export const AuthenticatorCode: Story = {
  parameters: { app: { auth: true, responses: [totpConfirmed, noPasskeys] } },
  play: async ({ canvas }) => {
    await expect(
      await canvas.findByRole('heading', { name: /this session has no second factor/i }),
    ).toBeVisible();
    await expect(canvas.getByLabelText(/authenticator code/i)).toBeVisible();
    await expect(canvas.getByRole('button', { name: /present code/i })).toBeDisabled();
  },
};

// A registered passkey and no authenticator: the banner offers the passkey button.
export const Passkey: Story = {
  parameters: { app: { auth: true, responses: [noTotp, onePasskey] } },
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('button', { name: /use a passkey/i })).toBeVisible();
  },
};
