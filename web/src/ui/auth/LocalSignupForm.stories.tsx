import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent, waitFor } from 'storybook/test';

import { topLayerDocs } from '../../../.storybook/topLayerDocs.ts';
import { LocalSignupForm } from './LocalSignupForm.tsx';

const meta = {
  title: 'Design system/Auth/LocalSignupForm',
  component: LocalSignupForm,
  tags: ['ai-generated'],
  args: { landing: 'You’ll join this organisation.', org: 'org_acme', onBack: fn() },
  parameters: {
    ...topLayerDocs,
    docs: { ...topLayerDocs.docs, story: { ...topLayerDocs.docs.story, autoplay: false }, description: { component: 'Email sign-up request and resend. A 202 acknowledges the request, never delivery or account existence. Docs starts before submission for manual exploration.' } },
    app: { responses: [{ url: '/api/v1/auth/signup', method: 'POST', status: 202 }] },
  },
} satisfies Meta<typeof LocalSignupForm>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Entry: Story = {};
export const NoOrganisation: Story = { args: { landing: null, org: undefined } };
export const RequestAndResend: Story = {
  play: async ({ canvas, step }) => {
    await step('Request an email link', async () => {
      await userEvent.type(canvas.getByRole('textbox', { name: 'Email' }), 'alex@example.com');
      await userEvent.click(canvas.getByRole('button', { name: 'Send sign-up link' }));
      await waitFor(() => expect(canvas.getByRole('heading', { name: 'Check your mail' })).toHaveFocus());
      await expect(canvas.getByText(/If this address can sign up/)).toBeVisible();
    });
    await step('Request a replacement link', async () => {
      await userEvent.click(canvas.getByRole('button', { name: 'Send it again' }));
      await expect(await canvas.findByRole('status')).toHaveTextContent('Use the newest mail');
    });
  },
};
export const Busy: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/auth/signup', method: 'POST', pending: true }] } },
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByRole('textbox', { name: 'Email' }), 'alex@example.com');
    await userEvent.click(canvas.getByRole('button', { name: 'Send sign-up link' }));
    await expect(await canvas.findByRole('button', { name: 'Requesting…' })).toBeDisabled();
    await expect(canvas.getByRole('textbox', { name: 'Email' })).toBeDisabled();
  },
};
export const Refused: Story = {
  parameters: { app: { responses: [{ url: '/api/v1/auth/signup', method: 'POST', status: 429, body: { error: { code: 'too-many-requests', message: 'Too many requests' } } }] } },
  play: async ({ canvas }) => {
    await userEvent.type(canvas.getByRole('textbox', { name: 'Email' }), 'alex@example.com');
    await userEvent.click(canvas.getByRole('button', { name: 'Send sign-up link' }));
    await expect(await canvas.findByRole('alert')).toHaveTextContent(/too many/i);
  },
};
