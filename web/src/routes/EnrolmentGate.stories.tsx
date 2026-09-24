import type { Meta, StoryObj } from '@storybook/react-vite';
import type { ReactNode } from 'react';
import { expect, userEvent } from 'storybook/test';

import { useAuth, type WhoAmI } from '../app/AuthProvider.tsx';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { EnrolmentGate } from './EnrolmentGate.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The sign-in enrolment gate (#785): what a session flagged `enrolment_required`
// sees instead of the app. The gate opens on the password proof; proving it
// issues the recovery codes (POST /api/v1/auth/recovery-codes/regenerate) and
// the later steps are the SecondFactorSetup states, storied on their own. The
// codes step itself needs the verified-remint handshake with whoami, which no
// canned response can complete, so the gate is storied up to the refusal.
const gated: WhoAmI = { ...authenticatedIdentity, enrolment_required: true };

/**
 * AuthProvider paints its children while whoami is still in flight, and the
 * gate refuses to render without a session (in the app the root router only
 * mounts it once whoami has flagged the session). This stands in for that
 * router: nothing but a status until the identity has settled.
 */
function OnceSignedIn({ children }: { children: ReactNode }) {
  const auth = useAuth();
  return auth.identity === null ? <p role="status">Checking session…</p> : children;
}

const meta = {
  component: EnrolmentGate,
  tags: ['ai-generated'],
  decorators: [(Story) => <OnceSignedIn><Story /></OnceSignedIn>],
  parameters: {
    ...topLayerDocs,
    app: { auth: true, identity: gated, path: '/login', responses: [] },
  },
} satisfies Meta<typeof EnrolmentGate>;

export default meta;
type Story = StoryObj<typeof meta>;

// The password proof, naming the account, with sign-out the only other way out.
export const Password: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'Set up a second factor' })).toBeVisible();
    await expect(canvas.getByText('Test operator')).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Continue' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Sign out' })).toBeEnabled();
  },
};

// The password was not accepted: the refusal names the password alone.
export const PasswordRefused: Story = {
  parameters: {
    app: {
      auth: true,
      identity: gated,
      path: '/login',
      responses: [
        {
          url: '/api/v1/auth/recovery-codes/regenerate',
          method: 'POST',
          status: 401,
          body: { error: 'unauthenticated' },
        },
      ],
    },
  },
  play: async ({ canvas }) => {
    // The gate mounts only from a settled identity (see OnceSignedIn), so the
    // heading is proof the session has settled and the form holds its state.
    await expect(await canvas.findByRole('heading', { name: 'Set up a second factor' })).toBeVisible();
    await userEvent.type(canvas.getByLabelText('Password'), 'wrong password');
    await userEvent.click(canvas.getByRole('button', { name: 'Continue' }));
    await expect(await canvas.findByRole('alert')).toHaveTextContent(/that password was not accepted/i);
  },
};
