import type { Meta, StoryObj } from '@storybook/react-vite';
import type { ComponentProps } from 'react';
import { expect, fn } from 'storybook/test';

import { ORG, PRJ, PROD, STAGING } from '../testkit/ids.ts';
import { MintDialog } from './MachineAccess.tsx';
import type { MintLifecycle, MintRequest } from './mintLifecycle.ts';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The display-once mint, storied by lifecycle state rather than by driving a
// ceremony: `move` is a spy, and the states are the machine's own variants. The
// dialog reads a query client for its post-mint refresh, so an empty
// `parameters.app` mounts the harness providers; it fires no request on render.
const request: MintRequest = {
  id: 1,
  sessionId: 'ses_123e4567-e89b-12d3-a456-426614174000',
  org: ORG,
  project: PRJ,
  accountId: 'msa_123e4567-e89b-12d3-a456-426614174020',
  accountName: 'api-gateway',
  rotating: false,
  reach: [
    { id: PROD, name: 'production' },
    { id: STAGING, name: 'staging' },
  ],
};

const disclosed: Extract<MintLifecycle, { kind: 'disclosed' }> = {
  kind: 'disclosed',
  request,
  result: {
    value: 'hk_9f3a1c7e5b2d84a06f1e93c7d0a5b8e2f4c6d8e0a2b4c6d8e0f2a4b6c8d0e2f4',
    expires_at: '2026-12-22T10:00:00Z',
    clamped: false,
  },
  stored: false,
  heldBack: false,
  copyStatus: null,
};

const meta = {
  component: MintDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {} },
  args: {
    lifecycle: { kind: 'reviewing', request },
    // The machine's contract: every event answers with a state. A bare spy
    // would hand `dismiss` undefined, and the navigation guard routes Back
    // presses through it.
    move: fn<ComponentProps<typeof MintDialog>['move']>(() => ({ accepted: true, state: { kind: 'idle' } })),
    isSubmitting: fn(() => false),
  },
} satisfies Meta<typeof MintDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// Review: the step-up names the post-state formula and the environments each
// passkey ceremony will cover.
export const Reviewing: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Mint credential · api-gateway' })).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Use a passkey and mint' })).toBeEnabled();
    await expect(canvas.getByText(/decrypts production, staging/)).toBeVisible();
  },
};

// An account reaching no plaintext: no ceremony, the plain mint button.
export const ReviewingNoReach: Story = {
  args: { lifecycle: { kind: 'reviewing', request: { ...request, reach: [] } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Mint credential' })).toBeEnabled();
    await expect(canvas.getByText(/no reauthentication are required/)).toBeVisible();
  },
};

// Rotation is the same flow with the predecessor warning: the prior value is
// never returned and keeps authenticating until revoked.
export const Rotating: Story = {
  args: { lifecycle: { kind: 'reviewing', request: { ...request, rotating: true } } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Rotate credential · api-gateway' })).toBeVisible();
    await expect(canvas.getByText(/the prior value is never returned/i)).toBeVisible();
  },
};

// In flight: both actions held, the primary relabelled.
export const Submitting: Story = {
  args: { lifecycle: { kind: 'submitting', request } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Minting…' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Cancel' })).toBeDisabled();
  },
};

// The mint refused before it issued: the review stays, with the reason.
export const Failed: Story = {
  args: {
    lifecycle: {
      kind: 'failed',
      request,
      error: 'The passkey ceremony was cancelled, so nothing was minted.',
    },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/ceremony was cancelled/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Use a passkey and mint' })).toBeEnabled();
  },
};

// The value, shown exactly once: copy offered, stored-confirmation unchecked.
export const Disclosed: Story = {
  args: { lifecycle: disclosed },
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('dialog', { name: 'Credential minted, shown exactly once' }),
    ).toBeVisible();
    await expect(canvas.getByText(disclosed.result.value)).toBeVisible();
    await expect(canvas.getByRole('checkbox', { name: /i have stored this credential/i })).not.toBeChecked();
  },
};

// Done pressed without the confirmation, after a copy, on a clamped credential:
// the copy receipt, the ceiling notice and the held-back refusal all stand.
export const HeldBack: Story = {
  args: {
    lifecycle: {
      ...disclosed,
      result: { ...disclosed.result, clamped: true },
      heldBack: true,
      copyStatus: 'Copied. The clipboard is now the only copy outside its target system.',
    },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/confirm you have stored it/i)).toBeVisible();
    await expect(canvas.getByText(/lifetime ceiling shortened/i)).toBeVisible();
    await expect(canvas.getByText(/^Copied\./)).toBeVisible();
  },
};
