import type { Meta, StoryObj } from '@storybook/react-vite';
import type { ComponentProps } from 'react';
import { expect, fn, userEvent } from 'storybook/test';

import type { DynamicProvider } from '../api/dynamic.ts';
import { LeaseMintDialog } from './MachineAccess.tsx';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';

// The display-once lease mint. Unlike its sibling dialogs its busy and failed
// states are NOT internal: they come from the `lifecycle` prop the page's mint
// state machine owns, so, exactly as `MintDialog.stories.tsx`, the states are
// the machine's own variants and `move` is a spy; no ceremony runs. The dialog
// reads a query client for its post-mint refresh, so an empty `parameters.app`
// mounts the harness providers; it fires no request on render.
const ORG = 'org_123e4567-e89b-12d3-a456-426614174001';
const PRJ = 'prj_123e4567-e89b-12d3-a456-426614174000';
const PROD = 'env_123e4567-e89b-12d3-a456-426614174010';
const STAGING = 'env_123e4567-e89b-12d3-a456-426614174011';

const provider: DynamicProvider = {
  id: 'dpv_123e4567-e89b-12d3-a456-426614174050',
  kind: 'postgres',
  origin: 'db.internal.example.com:5432',
  tls_mode: 'verify-full',
  grant_role: 'hikyo_leases',
  credential_present: true,
  credential_set_at: '2026-09-01T00:00:00Z',
  authority_principal_id: 'prn_123e4567-e89b-12d3-a456-426614174010',
  state: 'active',
  created_at: '2026-08-20T00:00:00Z',
};

type Lifecycle = ComponentProps<typeof LeaseMintDialog>['lifecycle'];
const request: Extract<Lifecycle, { kind: 'submitting' }>['request'] = {
  id: 1,
  sessionId: 'ses_123e4567-e89b-12d3-a456-426614174000',
  org: ORG,
  project: PRJ,
  providerId: provider.id,
  providerLabel: provider.origin,
  environmentId: PROD,
  environmentName: 'production',
  maxTtlSeconds: 3600,
};

const disclosed: Extract<Lifecycle, { kind: 'disclosed' }> = {
  kind: 'disclosed',
  request,
  result: {
    username: 'hk_lease_7f3a1c7e',
    password: 'pgpw_2d84a06f1e93c7d0a5b8e2f4c6d8e0a2b4c6d8e0f2a4b6c8d0e2f4',
    expires_at: '2026-09-23T08:00:00Z',
  },
  stored: false,
  heldBack: false,
  copyStatus: null,
};

const meta = {
  component: LeaseMintDialog,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {} },
  args: {
    project: { org: ORG, project: PRJ },
    sessionId: request.sessionId,
    providers: [provider],
    environments: [
      { id: PROD, name: 'production' },
      { id: STAGING, name: 'staging' },
    ],
    lifecycle: { kind: 'idle' },
    // The machine's contract: every event answers with a state. A bare spy
    // would hand `dismiss` undefined, and the navigation guard routes Back
    // presses through it.
    move: fn<ComponentProps<typeof LeaseMintDialog>['move']>(() => ({ accepted: true, state: { kind: 'idle' } })),
    isSubmitting: fn(() => false),
    nextRequestId: fn(() => 1),
    onClose: fn(),
  },
} satisfies Meta<typeof LeaseMintDialog>;

export default meta;
type Story = StoryObj<typeof meta>;

// The form: provider, environment and ceiling, behind the step-up notice.
export const Idle: Story = {
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('dialog', { name: 'Mint dynamic-secret lease' })).toBeVisible();
    await expect(canvas.getByLabelText('Provider')).toHaveValue(provider.id);
    await expect(canvas.getByLabelText('Environment')).toHaveValue(PROD);
    await expect(canvas.getByLabelText('Maximum lifetime (seconds)')).toHaveValue('3600');
    await expect(canvas.getByRole('button', { name: 'Use a passkey and mint' })).toBeEnabled();
  },
};

// A ceiling that is not a whole number of seconds: refused client-side, the
// lifecycle never moves.
export const CeilingRefused: Story = {
  play: async ({ args, canvas }) => {
    await userEvent.clear(canvas.getByLabelText('Maximum lifetime (seconds)'));
    await userEvent.type(canvas.getByLabelText('Maximum lifetime (seconds)'), '1h');
    await userEvent.click(canvas.getByRole('button', { name: 'Use a passkey and mint' }));
    await expect(
      await canvas.findByText(/enter the maximum lifetime as a whole number of seconds/i),
    ).toBeVisible();
    await expect(args.move).not.toHaveBeenCalled();
  },
};

// The live session could not be read: the mint refuses before any ceremony.
export const NoSession: Story = {
  args: { sessionId: null },
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Use a passkey and mint' }));
    await expect(await canvas.findByText(/the current session could not be read/i)).toBeVisible();
  },
};

// In flight: the form latched, both actions held, the primary relabelled.
export const Submitting: Story = {
  args: { lifecycle: { kind: 'submitting', request } },
  play: async ({ canvas }) => {
    await expect(canvas.getByRole('button', { name: 'Minting…' })).toBeDisabled();
    await expect(canvas.getByRole('button', { name: 'Cancel' })).toBeDisabled();
    await expect(canvas.getByLabelText('Provider')).toBeDisabled();
  },
};

// The mint refused before it issued: the form stays, with the reason.
export const Failed: Story = {
  args: {
    lifecycle: {
      kind: 'failed',
      request,
      error: 'The passkey prompt was dismissed or timed out. Nothing was minted.',
    },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/passkey prompt was dismissed/)).toBeVisible();
    await expect(canvas.getByRole('button', { name: 'Use a passkey and mint' })).toBeEnabled();
  },
};

// The role and its password, shown exactly once: copy offered, expiry named,
// stored-confirmation unchecked.
export const Disclosed: Story = {
  args: { lifecycle: disclosed },
  play: async ({ canvas }) => {
    await expect(
      canvas.getByRole('dialog', { name: 'Lease minted, shown exactly once' }),
    ).toBeVisible();
    await expect(canvas.getByText(disclosed.result.username)).toBeVisible();
    await expect(canvas.getByText(disclosed.result.password)).toBeVisible();
    await expect(canvas.getByText(/this lease expires/i)).toBeVisible();
    await expect(
      canvas.getByRole('checkbox', { name: /i have stored this password/i }),
    ).not.toBeChecked();
  },
};

// Done pressed without the confirmation, after a copy: the copy receipt and
// the held-back refusal both stand.
export const HeldBack: Story = {
  args: {
    lifecycle: {
      ...disclosed,
      result: { ...disclosed.result, expires_at: null },
      heldBack: true,
      copyStatus: 'Copied. The clipboard is now the only copy outside its target system.',
    },
  },
  play: async ({ canvas }) => {
    await expect(canvas.getByText(/confirm you have stored it/i)).toBeVisible();
    await expect(canvas.getByText(/^Copied\./)).toBeVisible();
    await expect(canvas.queryByText(/this lease expires/i)).not.toBeInTheDocument();
  },
};
