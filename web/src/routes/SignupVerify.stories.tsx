import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { SignupVerify } from './SignupVerify.tsx';

const path = '/signup/verify#token=storybook-inert-token&email=alex%40example.com&landing=none';
const meta = {
  title: 'Pages/SignupVerify',
  component: SignupVerify,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    docs: { ...topLayerDocs.docs, story: { ...topLayerDocs.docs.story, autoplay: false }, description: { component: 'Real verification route with an inert local fragment. Success navigates to login and is covered by unit regression; stories retain validation, busy and refused views without signing in.' } },
    app: { path, routePath: '/signup/verify', responses: [] },
  },
} satisfies Meta<typeof SignupVerify>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Entry: Story = {};
export const FreshOrganisation: Story = { parameters: { app: { path: path.replace('landing=none', 'landing=fresh-org') } } };
export const MissingLink: Story = { parameters: { app: { path: '/signup/verify' } } };
export const Validation: Story = {
  play: async ({ canvas, step }) => {
    await step('Reject missing names', async () => {
      await userEvent.click(await canvas.findByRole('button', { name: 'Create account' }));
      await expect(await canvas.findByRole('alert')).toHaveTextContent('Enter your display name');
    });
    await step('Reject a short password', async () => {
      await userEvent.type(canvas.getByRole('textbox', { name: 'Display name' }), 'Alex');
      await userEvent.click(canvas.getByRole('button', { name: 'Create account' }));
      await expect(await canvas.findByRole('alert')).toHaveTextContent('at least 12 characters');
    });
  },
};
export const Busy: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/auth/signup/verify', method: 'POST', pending: true }] } },
  play: async ({ canvas }) => {
    await userEvent.type(await canvas.findByRole('textbox', { name: 'Display name' }), 'Alex');
    await userEvent.type(canvas.getByLabelText('Password', { exact: true }), 'twelvecharacters');
    await userEvent.type(canvas.getByLabelText('Repeat password'), 'twelvecharacters');
    await userEvent.click(canvas.getByRole('button', { name: 'Create account' }));
    await expect(await canvas.findByRole('button', { name: 'Creating account…' })).toBeDisabled();
  },
};
export const Refused: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/auth/signup/verify', method: 'POST', status: 401, body: { error: { code: 'refused', message: 'Refused' } } }] } },
  play: async ({ canvas }) => {
    await userEvent.type(await canvas.findByRole('textbox', { name: 'Display name' }), 'Alex');
    await userEvent.type(canvas.getByLabelText('Password', { exact: true }), 'twelvecharacters');
    await userEvent.type(canvas.getByLabelText('Repeat password'), 'twelvecharacters');
    await userEvent.click(canvas.getByRole('button', { name: 'Create account' }));
    await expect(await canvas.findByRole('alert')).toHaveTextContent("This link can't be used");
    await expect(canvas.queryByLabelText('Password', { exact: true })).toBeNull();
  },
};
