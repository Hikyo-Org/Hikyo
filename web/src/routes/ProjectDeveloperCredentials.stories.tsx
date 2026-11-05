import type { Meta, StoryObj } from '@storybook/react-vite';
import type { DeveloperCredential } from '@hikyo/client';
import { expect, userEvent, within } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { ORG, PRJ, STAGING } from '../testkit/ids.ts';
import { ProjectDeveloperCredentials } from './ProjectDeveloperCredentials.tsx';

const url = `/api/v1/orgs/${ORG}/projects/${PRJ}/developer-credentials`;
const credential: DeveloperCredential = {
  id: 'dev_123e4567-e89b-12d3-a456-426614174001',
  principal_id: 'prn_123e4567-e89b-12d3-a456-426614174002',
  authority_principal_id: 'prn_123e4567-e89b-12d3-a456-426614174003',
  authority_principal_name: 'Dana Jacobs',
  org_id: ORG, project_id: PRJ, environment_id: STAGING,
  created_at: '2026-10-16T08:00:00Z', expires_at: '2026-10-16T16:00:00Z', prefix_hint: 'dev_example',
};
const environments = {
  url: `/api/v1/orgs/${ORG}/projects/${PRJ}/environments`,
  body: { items: [{ id: STAGING, org_id: ORG, project_id: PRJ, name: 'staging', display_order: 0, created_at: '2026-10-01T08:00:00Z' }], count: 1 },
};
const meta = {
  title: 'Features/Access/ProjectDeveloperCredentials',
  component: ProjectDeveloperCredentials,
  tags: ['ai-generated'],
  args: { org: ORG, project: PRJ },
  parameters: {
    ...topLayerDocs,
    docs: { ...topLayerDocs.docs, description: { component: 'Project incident-response metadata. Requires manage-identities; revocation names the exact credential and its permanent consequence. Personal mint/list/revoke stays in the CLI. No bearer is returned or rendered.' } },
    app: { responses: [{ url, body: { items: [credential], count: 1 } }, environments] },
  },
} satisfies Meta<typeof ProjectDeveloperCredentials>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Metadata: Story = { play: async ({ canvas }) => {
  const table = await canvas.findByRole('table');
  await expect(table).toHaveTextContent('Dana Jacobs');
  await expect(table).toHaveTextContent('dev_example…');
  await expect(await within(table).findByText('staging')).toBeVisible();
  await expect(canvas.getByRole('button', { name: `Revoke ${credential.id}` })).toBeVisible();
} };
export const Phone: Story = { ...Metadata, globals: { viewport: { value: 'phone', isRotated: false } } };
export const Empty: Story = { parameters: { app: { responses: [{ url, body: { items: [], count: 0 } }] } }, play: async ({ canvas }) => {
  await expect(await canvas.findByText('No developer credentials in this project.')).toBeVisible();
} };
export const NotDisclosed: Story = { parameters: { app: { responses: [{ url, status: 404, body: { error: { code: 'not_found', message: 'Not found' } } }] } }, play: async ({ canvas }) => {
  await expect(await canvas.findByRole('alert')).toHaveTextContent('not disclosed');
  await expect(canvas.queryByRole('table')).toBeNull();
} };
export const ConfirmRevocation: Story = { play: async ({ canvas }) => {
  await userEvent.click(await canvas.findByRole('button', { name: `Revoke ${credential.id}` }));
  const confirmation = await canvas.findByRole('dialog');
  await expect(confirmation).toHaveTextContent('This is permanent.');
  await expect(confirmation).toHaveTextContent('staging');
  await userEvent.click(within(confirmation).getByRole('button', { name: 'Cancel' }));
  await expect(canvas.queryByRole('dialog')).toBeNull();
} };
