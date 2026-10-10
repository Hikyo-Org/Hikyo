import type { Meta, StoryObj } from '@storybook/react-vite';
import { zAdapterList, zEnvironmentList, zKeyList } from '@hikyo/zod';
import { expect, userEvent } from 'storybook/test';
import type { z } from 'zod';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { authenticatedIdentity } from '../testkit/identity.ts';
import { ORG, PRJ } from '../testkit/ids.ts';
import { Adapters } from './Adapters.tsx';

const base = `/api/v1/orgs/${ORG}/projects/${PRJ}`;
const url = `${base}/adapters`;
const responses = [
  { url: `${base}/environments`, body: { items: [], count: 0 } satisfies z.input<typeof zEnvironmentList> },
  { url: `${base}/keys`, body: { items: [], count: 0, schema_revision: 1 } satisfies z.input<typeof zKeyList> },
];
const meta = {
  title: 'Pages/Adapters',
  component: Adapters,
  tags: ['ai-generated'],
  parameters: { ...topLayerDocs, app: {
    auth: true, identity: { ...authenticatedIdentity, capabilities: { ...authenticatedIdentity.capabilities, instance_operator: false } },
    path: `/orgs/${ORG}/projects/${PRJ}/adapters`, routePath: '/orgs/:org/projects/:project/adapters',
    responses: [{ url, body: { items: [] } satisfies z.input<typeof zAdapterList> }, ...responses],
  }, docs: { ...topLayerDocs.docs, description: { component: 'Real deployment adapters page through its production system-scope gate. Browser passkey ceremonies remain human-only; isolated adapter state catalogues cover existing targets.' } } },
} satisfies Meta<typeof Adapters>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Empty: Story = { play: async ({ canvas }) => { await expect(await canvas.findByText(/No adapters yet/)).toBeVisible(); } };
export const Failed: Story = { parameters: { app: { responses: [{ url, status: 500, body: { error: { code: 'internal', message: 'Internal' } } }, ...responses] } }, play: async ({ canvas }) => { await expect(await canvas.findByRole('alert')).toHaveTextContent(/500|could not|failed/i); } };
export const Adding: Story = { play: async ({ canvas }) => {
  await canvas.findByText(/No adapters yet/);
  await userEvent.click(canvas.getByRole('button', { name: 'Add adapter' }));
  await expect(canvas.getByRole('textbox', { name: 'Origin' })).toBeVisible();
} };
